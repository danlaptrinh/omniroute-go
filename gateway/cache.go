package gateway

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// CacheEntry represents a cached chat completion response.
type CacheEntry struct {
	Key         string             `json:"key"`
	Model       string             `json:"model"`
	Response    OpenAIChatResponse `json:"response"`
	CreatedAt   time.Time          `json:"created_at"`
	HitCount    int64              `json:"hit_count"`
	TokensSaved int64              `json:"tokens_saved"`
}

// ResponseCache is an in-memory LRU cache with SHA-256 keys for exact LLM prompt matching.
type ResponseCache struct {
	mu          sync.RWMutex
	entries     map[string]*CacheEntry
	order       []string
	maxEntries  int
	ttl         time.Duration
	hits        int64
	misses      int64
	tokensSaved int64
	enabled     bool
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

// NewResponseCache creates a new ResponseCache.
func NewResponseCache(maxEntries int, ttl time.Duration) *ResponseCache {
	if maxEntries <= 0 {
		maxEntries = 1000
	}
	if ttl <= 0 {
		ttl = 6 * time.Hour
	}
	return &ResponseCache{
		entries:    make(map[string]*CacheEntry),
		order:      make([]string, 0, maxEntries),
		maxEntries: maxEntries,
		ttl:        ttl,
		enabled:    true,
	}
}

// ComputeCacheKey generates a deterministic SHA-256 hash for an OpenAIChatRequest.
func ComputeCacheKey(req OpenAIChatRequest) string {
	hasher := sha256.New()
	hasher.Write([]byte(req.Model))
	hasher.Write([]byte{0})

	for _, msg := range req.Messages {
		hasher.Write([]byte(msg.Role))
		hasher.Write([]byte{0})
		hasher.Write(msg.Content)
		hasher.Write([]byte{0})
	}

	if req.Temperature != nil {
		hasher.Write([]byte(fmt.Sprintf("t:%.2f;", *req.Temperature)))
	}
	if effMax := req.EffectiveMaxTokens(0); effMax > 0 {
		hasher.Write([]byte(fmt.Sprintf("m:%d;", effMax)))
	}

	return hex.EncodeToString(hasher.Sum(nil))
}

// Get retrieves a response from the cache if available and not expired.
func (c *ResponseCache) Get(req OpenAIChatRequest) (*OpenAIChatResponse, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.enabled {
		return nil, false
	}

	// Bypass caching for high temperature (creative non-deterministic) requests
	if req.Temperature != nil && *req.Temperature > 0.3 {
		c.misses++
		return nil, false
	}

	key := ComputeCacheKey(req)
	entry, ok := c.entries[key]
	if !ok {
		c.misses++
		return nil, false
	}

	if time.Since(entry.CreatedAt) > c.ttl {
		delete(c.entries, key)
		c.removeFromOrderLocked(key)
		c.misses++
		return nil, false
	}

	entry.HitCount++
	tokens := int64(50)
	if entry.Response.Usage != nil && entry.Response.Usage.TotalTokens > 0 {
		tokens = int64(entry.Response.Usage.TotalTokens)
	}
	entry.TokensSaved += tokens
	c.tokensSaved += tokens
	c.hits++

	// Move to back of LRU order
	c.touchOrderLocked(key)

	res := entry.Response
	return &res, true
}

// Put stores a response in the cache.
func (c *ResponseCache) Put(req OpenAIChatRequest, resp OpenAIChatResponse) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.enabled {
		return
	}

	if req.Temperature != nil && *req.Temperature > 0.3 {
		return
	}

	key := ComputeCacheKey(req)
	if _, ok := c.entries[key]; ok {
		c.entries[key].Response = resp
		c.touchOrderLocked(key)
		return
	}

	// Evict oldest if full
	for len(c.entries) >= c.maxEntries && len(c.order) > 0 {
		oldest := c.order[0]
		c.order = c.order[1:]
		delete(c.entries, oldest)
	}

	entry := &CacheEntry{
		Key:       key,
		Model:     req.Model,
		Response:  resp,
		CreatedAt: time.Now(),
	}
	c.entries[key] = entry
	c.order = append(c.order, key)
}

func (c *ResponseCache) touchOrderLocked(key string) {
	c.removeFromOrderLocked(key)
	c.order = append(c.order, key)
}

func (c *ResponseCache) removeFromOrderLocked(key string) {
	for i, k := range c.order {
		if k == key {
			c.order = append(c.order[:i], c.order[i+1:]...)
			break
		}
	}
}

// Clear flushes all entries.
func (c *ResponseCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = make(map[string]*CacheEntry)
	c.order = c.order[:0]
}

// SetEnabled toggles caching.
func (c *ResponseCache) SetEnabled(enabled bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.enabled = enabled
}

// Stats returns cache statistics.
func (c *ResponseCache) Stats() map[string]any {
	c.mu.RLock()
	defer c.mu.RUnlock()

	totalReqs := c.hits + c.misses
	ratio := "0.0%"
	if totalReqs > 0 {
		ratio = fmt.Sprintf("%.1f%%", float64(c.hits)/float64(totalReqs)*100.0)
	}

	return map[string]any{
		"enabled":      c.enabled,
		"items":        len(c.entries),
		"max_entries":  c.maxEntries,
		"hits":         c.hits,
		"misses":       c.misses,
		"hit_ratio":    ratio,
		"tokens_saved": c.tokensSaved,
		"ttl_hours":    c.ttl.Hours(),
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
