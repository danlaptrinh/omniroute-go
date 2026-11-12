package gateway

import (
	"container/list"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

const MaxCacheablePromptBytes = 256 * 1024

// CacheEntry represents a cached chat completion response.
type CacheEntry struct {
	Key         string             `json:"key"`
	Model       string             `json:"model"`
	Response    OpenAIChatResponse `json:"response"`
	CreatedAt   time.Time          `json:"created_at"`
	HitCount    int64              `json:"hit_count"`
	TokensSaved int64              `json:"tokens_saved"`
}

// ResponseCache is an in-memory O(1) LRU cache with SHA-256 keys and RLock read fast-paths.
type ResponseCache struct {
	mu          sync.RWMutex
	entries     map[string]*list.Element
	lru         *list.List
	maxEntries  int
	ttl         time.Duration
	hits        atomic.Int64
	misses      atomic.Int64
	tokensSaved atomic.Int64
	enabled     atomic.Bool
}

var (
	globalCache     *ResponseCache
	globalCacheOnce sync.Once
)

// GetGlobalCache returns the singleton ResponseCache instance.
func GetGlobalCache() *ResponseCache {
	globalCacheOnce.Do(func() {
		globalCache = NewResponseCache(1000, 6*time.Hour)
	})
	return globalCache
}

// NewResponseCache creates a new ResponseCache with O(1) container/list LRU tracking.
func NewResponseCache(maxEntries int, ttl time.Duration) *ResponseCache {
	if maxEntries <= 0 {
		maxEntries = 1000
	}
	if ttl <= 0 {
		ttl = 6 * time.Hour
	}
	c := &ResponseCache{
		entries:    make(map[string]*list.Element, maxEntries),
		lru:        list.New(),
		maxEntries: maxEntries,
		ttl:        ttl,
	}
	c.enabled.Store(true)
	return c
}

func requestMessagesByteSize(msgs []OpenAIChatMessage) int {
	total := 0
	for _, m := range msgs {
		total += len(m.Role) + len(m.Content)
	}
	return total
}

// ComputeCacheKey generates a deterministic SHA-256 hash for an OpenAIChatRequest.
// Uses a zero-alloc stack buffer with sha256.Sum256 for compact requests (<= 1KB) and streams larger payloads.
func ComputeCacheKey(req OpenAIChatRequest) string {
	effMax := req.EffectiveMaxTokens(0)
	approxLen := len(req.Model) + 1 + 48
	for _, msg := range req.Messages {
		approxLen += len(msg.Role) + 1 + len(msg.Content) + 1
	}

	if approxLen <= 1024 {
		var stackBuf [1024]byte
		b := stackBuf[:0]
		b = append(b, req.Model...)
		b = append(b, 0)
		for _, msg := range req.Messages {
			b = append(b, msg.Role...)
			b = append(b, 0)
			b = append(b, msg.Content...)
			b = append(b, 0)
		}
		if req.Temperature != nil {
			b = append(b, 't', ':')
			b = strconv.AppendFloat(b, *req.Temperature, 'f', 2, 64)
			b = append(b, ';')
		}
		if effMax > 0 {
			b = append(b, 'm', ':')
			b = strconv.AppendInt(b, int64(effMax), 10)
			b = append(b, ';')
		}
		sum := sha256.Sum256(b)
		return hex.EncodeToString(sum[:])
	}

	hasher := sha256.New()
	hasher.Write([]byte(req.Model))
	hasher.Write([]byte{0})

	for _, msg := range req.Messages {
		hasher.Write([]byte(msg.Role))
		hasher.Write([]byte{0})
		hasher.Write(msg.Content)
		hasher.Write([]byte{0})
	}

	var numBuf [64]byte
	if req.Temperature != nil {
		b := append(numBuf[:0], 't', ':')
		b = strconv.AppendFloat(b, *req.Temperature, 'f', 2, 64)
		b = append(b, ';')
		hasher.Write(b)
	}
	if effMax > 0 {
		b := append(numBuf[:0], 'm', ':')
		b = strconv.AppendInt(b, int64(effMax), 10)
		b = append(b, ';')
		hasher.Write(b)
	}

	var sum [sha256.Size]byte
	return hex.EncodeToString(hasher.Sum(sum[:0]))
}

// IsEnabled returns true if response caching is enabled.
func (c *ResponseCache) IsEnabled() bool {
	return c.enabled.Load()
}

// Get retrieves a response from the cache if available and not expired.
// Fast-returns outside any lock when disabled, non-deterministic (temp > 0.3), or prompt > 256KB,
// computes SHA-256 outside locks, and uses RLock + atomic counters for concurrent misses and MRU hits.
func (c *ResponseCache) Get(req OpenAIChatRequest) (*OpenAIChatResponse, bool) {
	if !c.IsEnabled() {
		return nil, false
	}

	// Bypass caching for high temperature (creative non-deterministic) requests
	if req.Temperature != nil && *req.Temperature > 0.3 {
		c.misses.Add(1)
		return nil, false
	}

	// Bypass caching for huge context prompts (>256KB) to avoid CPU hashing & RAM bloat
	if requestMessagesByteSize(req.Messages) > MaxCacheablePromptBytes {
		c.misses.Add(1)
		return nil, false
	}

	// Compute SHA-256 key OUTSIDE any mutex
	key := ComputeCacheKey(req)

	c.mu.RLock()
	elem, ok := c.entries[key]
	if !ok {
		c.mu.RUnlock()
		c.misses.Add(1)
		return nil, false
	}

	entry := elem.Value.(*CacheEntry)
	if time.Since(entry.CreatedAt) > c.ttl {
		c.mu.RUnlock()
		c.mu.Lock()
		if curElem, stillExists := c.entries[key]; stillExists {
			curEntry := curElem.Value.(*CacheEntry)
			if time.Since(curEntry.CreatedAt) > c.ttl {
				c.lru.Remove(curElem)
				delete(c.entries, key)
			}
		}
		c.mu.Unlock()
		c.misses.Add(1)
		return nil, false
	}

	res := entry.Response
	needMove := c.lru.Back() != elem
	c.mu.RUnlock()

	tokens := int64(50)
	if res.Usage != nil && res.Usage.TotalTokens > 0 {
		tokens = int64(res.Usage.TotalTokens)
	}
	atomic.AddInt64(&entry.HitCount, 1)
	atomic.AddInt64(&entry.TokensSaved, tokens)
	c.tokensSaved.Add(tokens)
	c.hits.Add(1)

	// Only acquire write lock if element is not already at the MRU tail
	if needMove {
		c.mu.Lock()
		if curElem, stillExists := c.entries[key]; stillExists && c.lru.Back() != curElem {
			c.lru.MoveToBack(curElem)
		}
		c.mu.Unlock()
	}

	return &res, true
}

// Put stores a response in the cache with O(1) LRU insertion and eviction.
func (c *ResponseCache) Put(req OpenAIChatRequest, resp OpenAIChatResponse) {
	if !c.IsEnabled() {
		return
	}
	if req.Temperature != nil && *req.Temperature > 0.3 {
		return
	}
	if requestMessagesByteSize(req.Messages) > MaxCacheablePromptBytes {
		return
	}

	// Compute SHA-256 key OUTSIDE any mutex
	key := ComputeCacheKey(req)

	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.IsEnabled() {
		return
	}

	if elem, ok := c.entries[key]; ok {
		prev := elem.Value.(*CacheEntry)
		elem.Value = &CacheEntry{
			Key:         key,
			Model:       req.Model,
			Response:    resp,
			CreatedAt:   time.Now(),
			HitCount:    atomic.LoadInt64(&prev.HitCount),
			TokensSaved: atomic.LoadInt64(&prev.TokensSaved),
		}
		c.lru.MoveToBack(elem)
		return
	}

	// Evict oldest (front of list) in O(1) if full
	for len(c.entries) >= c.maxEntries {
		oldestElem := c.lru.Front()
		if oldestElem == nil {
			break
		}
		c.lru.Remove(oldestElem)
		oldestEntry := oldestElem.Value.(*CacheEntry)
		delete(c.entries, oldestEntry.Key)
	}

	entry := &CacheEntry{
		Key:       key,
		Model:     req.Model,
		Response:  resp,
		CreatedAt: time.Now(),
	}
	c.entries[key] = c.lru.PushBack(entry)
}

// Clear flushes all entries in O(1).
func (c *ResponseCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = make(map[string]*list.Element, c.maxEntries)
	c.lru.Init()
}

// SetEnabled toggles caching atomically without locking.
func (c *ResponseCache) SetEnabled(enabled bool) {
	c.enabled.Store(enabled)
}

// Stats returns cache statistics.
func (c *ResponseCache) Stats() map[string]any {
	c.mu.RLock()
	items := len(c.entries)
	maxEntries := c.maxEntries
	ttlHours := c.ttl.Hours()
	c.mu.RUnlock()

	hits := c.hits.Load()
	misses := c.misses.Load()
	tokensSaved := c.tokensSaved.Load()
	totalReqs := hits + misses
	ratio := "0.0%"
	if totalReqs > 0 {
		ratio = fmt.Sprintf("%.1f%%", float64(hits)/float64(totalReqs)*100.0)
	}

	return map[string]any{
		"enabled":      c.IsEnabled(),
		"items":        items,
		"max_entries":  maxEntries,
		"hits":         hits,
		"misses":       misses,
		"hit_ratio":    ratio,
		"tokens_saved": tokensSaved,
		"ttl_hours":    ttlHours,
	}
}

// --- HTTP API Handlers for Cache ---

func (s *Server) handleCacheStats(w http.ResponseWriter, r *http.Request) {
	cache := GetGlobalCache()
	switch r.Method {
	case http.MethodGet:
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(cache.Stats())
	case http.MethodPost:
		var req struct {
			Enabled *bool `json:"enabled"`
			Clear   bool  `json:"clear"`
		}
		if r.Body != nil {
			if err := json.NewDecoder(io.LimitReader(r.Body, 64*1024)).Decode(&req); err == nil {
				if req.Clear {
					cache.Clear()
				}
				if req.Enabled != nil {
					cache.SetEnabled(*req.Enabled)
				}
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "ok",
			"stats":  cache.Stats(),
		})
	default:
		writeOpenAIError(w, http.StatusMethodNotAllowed, "method not allowed", "invalid_request_error")
	}
}
