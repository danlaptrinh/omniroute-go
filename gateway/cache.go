package gateway

import (
	"bytes"
	"container/list"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const MaxCacheablePromptBytes = 256 * 1024

// StreamOptions configures optional streaming behavior (such as including a final usage chunk).
type StreamOptions struct {
	IncludeUsage bool `json:"include_usage,omitempty"`
}

// CacheEntry represents a cached chat completion response with pre-serialized wire payloads.
type CacheEntry struct {
	Key         string             `json:"key"`
	Model       string             `json:"model"`
	Response    OpenAIChatResponse `json:"response"`
	CreatedAt   time.Time          `json:"created_at"`
	HitCount    int64              `json:"hit_count"`
	TokensSaved int64              `json:"tokens_saved"`

	// Pre-serialized wire bytes built once during Put for sub-millisecond near-zero-alloc HITs.
	PromptSummary      string `json:"-"`
	JSONBytes          []byte `json:"-"`
	SSEBytes           []byte `json:"-"`
	SSEUsageBytes      []byte `json:"-"`
	AnthropicJSONBytes []byte `json:"-"`
	AnthropicSSEBytes  []byte `json:"-"`
}

// SSEPayload returns the pre-serialized OpenAI SSE stream bytes, optionally including the usage chunk.
func (e *CacheEntry) SSEPayload(includeUsage bool) []byte {
	if includeUsage && len(e.SSEUsageBytes) > 0 {
		return e.SSEUsageBytes
	}
	return e.SSEBytes
}

// ContentText returns the primary assistant text from the cached response without allocating.
func (e *CacheEntry) ContentText() string {
	if e == nil || len(e.Response.Choices) == 0 || e.Response.Choices[0].Message == nil {
		return ""
	}
	return e.Response.Choices[0].Message.Content
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

// IsCacheable returns true if the request is deterministic (temperature nil or <= 0.3)
// and within the maximum cacheable prompt byte budget.
func IsCacheable(req OpenAIChatRequest) bool {
	if req.Temperature != nil && *req.Temperature > 0.3 {
		return false
	}
	if requestMessagesByteSize(req.Messages) > MaxCacheablePromptBytes {
		return false
	}
	return true
}

// ComputeCacheKey generates a deterministic SHA-256 hash for an OpenAIChatRequest,
// invariant to req.Stream / StreamOptions, whitespace around Model, and Temperature == nil vs 0.0.
// Uses a zero-alloc stack buffer with sha256.Sum256 for compact requests (<= 1KB) and streams larger payloads.
func ComputeCacheKey(req OpenAIChatRequest) string {
	model := strings.TrimSpace(req.Model)
	effMax := req.EffectiveMaxTokens(0)
	hasNonZeroTemp := req.Temperature != nil && *req.Temperature != 0

	approxLen := len(model) + 1 + 48
	for _, msg := range req.Messages {
		approxLen += len(msg.Role) + 1 + len(bytes.TrimSpace(msg.Content)) + 1
	}

	if approxLen <= 1024 {
		var stackBuf [1024]byte
		b := stackBuf[:0]
		b = append(b, model...)
		b = append(b, 0)
		for _, msg := range req.Messages {
			b = append(b, msg.Role...)
			b = append(b, 0)
			b = append(b, bytes.TrimSpace(msg.Content)...)
			b = append(b, 0)
		}
		if hasNonZeroTemp {
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
	hasher.Write([]byte(model))
	hasher.Write([]byte{0})

	for _, msg := range req.Messages {
		hasher.Write([]byte(msg.Role))
		hasher.Write([]byte{0})
		hasher.Write(bytes.TrimSpace(msg.Content))
		hasher.Write([]byte{0})
	}

	var numBuf [64]byte
	if hasNonZeroTemp {
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

// Has performs a side-effect-free lookup (without mutating hit/miss counters) to check whether
// a non-expired entry already exists for req.
func (c *ResponseCache) Has(req OpenAIChatRequest) bool {
	if !c.IsEnabled() || !IsCacheable(req) {
		return false
	}
	key := ComputeCacheKey(req)
	c.mu.RLock()
	elem, ok := c.entries[key]
	if !ok {
		c.mu.RUnlock()
		return false
	}
	entry := elem.Value.(*CacheEntry)
	valid := time.Since(entry.CreatedAt) <= c.ttl
	c.mu.RUnlock()
	return valid
}

// GetEntry retrieves the full CacheEntry (including pre-serialized JSON & SSE wire bytes)
// if available and not expired.
func (c *ResponseCache) GetEntry(req OpenAIChatRequest) (*CacheEntry, bool) {
	if !c.IsEnabled() {
		return nil, false
	}

	if !IsCacheable(req) {
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

	needMove := c.lru.Back() != elem
	c.mu.RUnlock()

	tokens := int64(50)
	if entry.Response.Usage != nil && entry.Response.Usage.TotalTokens > 0 {
		tokens = int64(entry.Response.Usage.TotalTokens)
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

	return entry, true
}

// Get retrieves a response from the cache if available and not expired.
// Keeps 100% backward-compatible signature for existing callers and tests.
func (c *ResponseCache) Get(req OpenAIChatRequest) (*OpenAIChatResponse, bool) {
	entry, ok := c.GetEntry(req)
	if !ok {
		return nil, false
	}
	res := entry.Response
	return &res, true
}

func buildPreSerializedCacheEntry(key string, req OpenAIChatRequest, resp OpenAIChatResponse) *CacheEntry {
	model := strings.TrimSpace(req.Model)
	if strings.TrimSpace(resp.Model) == "" {
		resp.Model = model
	}
	if model == "" {
		model = strings.TrimSpace(resp.Model)
	}
	if resp.ID == "" {
		shortKey := key
		if len(shortKey) > 12 {
			shortKey = shortKey[:12]
		}
		resp.ID = "chatcmpl-cache-" + shortKey
	}
	if resp.Object == "" {
		resp.Object = "chat.completion"
	}
	if resp.Created == 0 {
		resp.Created = time.Now().Unix()
	}

	content := ""
	reasoning := ""
	finishReason := "stop"
	if len(resp.Choices) > 0 {
		if resp.Choices[0].Message != nil {
			content = resp.Choices[0].Message.Content
			reasoning = resp.Choices[0].Message.ReasoningContent
		} else if resp.Choices[0].Delta != nil {
			content = resp.Choices[0].Delta.Content
			reasoning = resp.Choices[0].Delta.ReasoningContent
		}
		if strings.TrimSpace(resp.Choices[0].FinishReason) != "" {
			finishReason = resp.Choices[0].FinishReason
		}
	}

	promptToks := 0
	compToks := 0
	totalToks := 0
	if resp.Usage != nil {
		promptToks = resp.Usage.PromptTokens
		compToks = resp.Usage.CompletionTokens
		totalToks = resp.Usage.TotalTokens
	}
	if promptToks <= 0 {
		promptToks = EstimateTokens(req.Messages)
	}
	if compToks <= 0 && totalToks > promptToks {
		compToks = totalToks - promptToks
	} else if compToks <= 0 {
		compToks = (len(content) + len(reasoning) + 3) / 4
		if compToks <= 0 {
			compToks = 1
		}
	}
	if totalToks <= 0 {
		totalToks = promptToks + compToks
	}
	if resp.Usage == nil {
		resp.Usage = &OpenAIUsage{
			PromptTokens:     promptToks,
			CompletionTokens: compToks,
			TotalTokens:      totalToks,
		}
	} else {
		if resp.Usage.PromptTokens <= 0 {
			resp.Usage.PromptTokens = promptToks
		}
		if resp.Usage.CompletionTokens <= 0 {
			resp.Usage.CompletionTokens = compToks
		}
		if resp.Usage.TotalTokens <= 0 {
			resp.Usage.TotalTokens = totalToks
		}
	}

	// 1. Pre-serialize OpenAI non-stream JSON
	jsonRaw, _ := json.Marshal(resp)
	jsonBytes := make([]byte, 0, len(jsonRaw)+1)
	jsonBytes = append(jsonBytes, jsonRaw...)
	jsonBytes = append(jsonBytes, '\n')

	// 2. Pre-serialize OpenAI SSE wire bytes (standard + include_usage variant)
	deltaChunk := OpenAIChatResponse{
		ID:      resp.ID,
		Object:  "chat.completion.chunk",
		Created: resp.Created,
		Model:   resp.Model,
		Choices: []OpenAIChatChoice{
			{
				Index: 0,
				Delta: &OpenAIOutputMessage{
					Role:             "assistant",
					Content:          content,
					ReasoningContent: reasoning,
				},
			},
		},
	}
	stopChunk := OpenAIChatResponse{
		ID:      resp.ID,
		Object:  "chat.completion.chunk",
		Created: resp.Created,
		Model:   resp.Model,
		Choices: []OpenAIChatChoice{
			{
				Index:        0,
				Delta:        &OpenAIOutputMessage{},
				FinishReason: finishReason,
			},
		},
	}
	usageChunk := OpenAIChatResponse{
		ID:      resp.ID,
		Object:  "chat.completion.chunk",
		Created: resp.Created,
		Model:   resp.Model,
		Choices: []OpenAIChatChoice{},
		Usage: &OpenAIUsage{
			PromptTokens:     promptToks,
			CompletionTokens: compToks,
			TotalTokens:      totalToks,
		},
	}

	bDelta, _ := json.Marshal(deltaChunk)
	bStop, _ := json.Marshal(stopChunk)
	bUsage, _ := json.Marshal(usageChunk)

	var sseBuf bytes.Buffer
	sseBuf.Grow(len(bDelta) + len(bStop) + 32)
	sseBuf.WriteString("data: ")
	sseBuf.Write(bDelta)
	sseBuf.WriteString("\n\ndata: ")
	sseBuf.Write(bStop)
	sseBuf.WriteString("\n\ndata: [DONE]\n\n")
	sseBytes := make([]byte, sseBuf.Len())
	copy(sseBytes, sseBuf.Bytes())

	var sseUsageBuf bytes.Buffer
	sseUsageBuf.Grow(len(bDelta) + len(bStop) + len(bUsage) + 42)
	sseUsageBuf.WriteString("data: ")
	sseUsageBuf.Write(bDelta)
	sseUsageBuf.WriteString("\n\ndata: ")
	sseUsageBuf.Write(bStop)
	sseUsageBuf.WriteString("\n\ndata: ")
	sseUsageBuf.Write(bUsage)
	sseUsageBuf.WriteString("\n\ndata: [DONE]\n\n")
	sseUsageBytes := make([]byte, sseUsageBuf.Len())
	copy(sseUsageBytes, sseUsageBuf.Bytes())

	// 3. Pre-serialize Anthropic /v1/messages JSON and SSE wire bytes
	anthID := normalizeAnthropicMessageID(resp.ID, key)
	anthStopReason := mapOpenAIFinishReasonToAnthropic(finishReason)
	anthJSONBytes := buildAnthropicJSONBytesWithHint(resp, model, key)

	// Build Anthropic SSE sequence:
	// message_start -> content_block_start -> content_block_delta -> content_block_stop -> message_delta -> message_stop
	var anthSSE bytes.Buffer
	anthSSE.Grow(len(anthJSONBytes) + 512)

	emptyStr := ""
	writeAnthropicSSEEvent(&anthSSE, "message_start", anthropicSSEMessageStart{
		Type: "message_start",
		Message: anthropicMessageResponse{
			ID:           anthID,
			Type:         "message",
			Role:         "assistant",
			Model:        resp.Model,
			Content:      []anthWireContentBlock{},
			StopReason:   nil,
			StopSequence: nil,
			Usage: anthropicUsage{
				InputTokens:  promptToks,
				OutputTokens: 0,
			},
		},
	})

	blockIdx := 0
	if reasoning != "" {
		rCopy := reasoning
		writeAnthropicSSEEvent(&anthSSE, "content_block_start", anthropicSSEContentBlockStart{
			Type:         "content_block_start",
			Index:        blockIdx,
			ContentBlock: anthWireContentBlock{Type: "thinking", Thinking: &emptyStr},
		})
		writeAnthropicSSEEvent(&anthSSE, "content_block_delta", anthropicSSEContentBlockDelta{
			Type:  "content_block_delta",
			Index: blockIdx,
			Delta: anthWireContentBlock{Type: "thinking_delta", Thinking: &rCopy},
		})
		writeAnthropicSSEEvent(&anthSSE, "content_block_stop", anthropicSSEContentBlockStop{
			Type:  "content_block_stop",
			Index: blockIdx,
		})
		blockIdx++
	}

	cCopy := content
	writeAnthropicSSEEvent(&anthSSE, "content_block_start", anthropicSSEContentBlockStart{
		Type:         "content_block_start",
		Index:        blockIdx,
		ContentBlock: anthWireContentBlock{Type: "text", Text: &emptyStr},
	})
	writeAnthropicSSEEvent(&anthSSE, "content_block_delta", anthropicSSEContentBlockDelta{
		Type:  "content_block_delta",
		Index: blockIdx,
		Delta: anthWireContentBlock{Type: "text_delta", Text: &cCopy},
	})
	writeAnthropicSSEEvent(&anthSSE, "content_block_stop", anthropicSSEContentBlockStop{
		Type:  "content_block_stop",
		Index: blockIdx,
	})

	writeAnthropicSSEEvent(&anthSSE, "message_delta", anthropicSSEMessageDelta{
		Type: "message_delta",
		Delta: anthropicMessageDeltaBody{
			StopReason:   anthStopReason,
			StopSequence: nil,
		},
		Usage: anthropicOutputUsage{
			OutputTokens: compToks,
		},
	})
	writeAnthropicSSEEvent(&anthSSE, "message_stop", anthropicSSEMessageStop{
		Type: "message_stop",
	})

	anthSSEBytes := make([]byte, anthSSE.Len())
	copy(anthSSEBytes, anthSSE.Bytes())

	promptSummary := extractPromptTextFromMessages(req.Messages)
	if promptSummary == "" {
		promptSummary = "[cached]"
	}

	return &CacheEntry{
		Key:                key,
		Model:              model,
		Response:           resp,
		CreatedAt:          time.Now(),
		PromptSummary:      promptSummary,
		JSONBytes:          jsonBytes,
		SSEBytes:           sseBytes,
		SSEUsageBytes:      sseUsageBytes,
		AnthropicJSONBytes: anthJSONBytes,
		AnthropicSSEBytes:  anthSSEBytes,
	}
}

type anthWireContentBlock struct {
	Type     string  `json:"type"`
	Text     *string `json:"text,omitempty"`
	Thinking *string `json:"thinking,omitempty"`
}

type anthropicUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type anthropicOutputUsage struct {
	OutputTokens int `json:"output_tokens"`
}

type anthropicMessageResponse struct {
	ID           string                  `json:"id"`
	Type         string                  `json:"type"`
	Role         string                  `json:"role"`
	Model        string                  `json:"model"`
	Content      []anthWireContentBlock `json:"content"`
	StopReason   *string                 `json:"stop_reason"`
	StopSequence *string                 `json:"stop_sequence"`
	Usage        anthropicUsage          `json:"usage"`
}

type anthropicSSEMessageStart struct {
	Type    string                   `json:"type"`
	Message anthropicMessageResponse `json:"message"`
}

type anthropicSSEContentBlockStart struct {
	Type         string                `json:"type"`
	Index        int                   `json:"index"`
	ContentBlock anthWireContentBlock `json:"content_block"`
}

type anthropicSSEContentBlockDelta struct {
	Type  string                `json:"type"`
	Index int                   `json:"index"`
	Delta anthWireContentBlock `json:"delta"`
}

type anthropicSSEContentBlockStop struct {
	Type  string `json:"type"`
	Index int    `json:"index"`
}

type anthropicMessageDeltaBody struct {
	StopReason   string  `json:"stop_reason"`
	StopSequence *string `json:"stop_sequence"`
}

type anthropicSSEMessageDelta struct {
	Type  string                    `json:"type"`
	Delta anthropicMessageDeltaBody `json:"delta"`
	Usage anthropicOutputUsage      `json:"usage"`
}

type anthropicSSEMessageStop struct {
	Type string `json:"type"`
}

type anthropicSSEErrorDetail struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

type anthropicSSEError struct {
	Type  string                  `json:"type"`
	Error anthropicSSEErrorDetail `json:"error"`
}

var anthMsgSeq atomic.Uint64

func normalizeAnthropicMessageID(rawID, fallbackHint string) string {
	id := strings.TrimSpace(rawID)
	if strings.HasPrefix(id, "msg_") {
		return id
	}
	if trimmed := strings.TrimPrefix(id, "chatcmpl-"); trimmed != "" {
		return "msg_" + trimmed
	}
	if fallbackHint != "" {
		shortKey := fallbackHint
		if len(shortKey) > 16 {
			shortKey = shortKey[:16]
		}
		return "msg_cache_" + shortKey
	}
	return "msg_" + strconv.FormatUint(anthMsgSeq.Add(1), 36)
}

func mapOpenAIFinishReasonToAnthropic(finishReason string) string {
	switch strings.ToLower(strings.TrimSpace(finishReason)) {
	case "length", "max_tokens":
		return "max_tokens"
	case "tool_calls", "function_call":
		return "tool_use"
	case "stop_sequence":
		return "stop_sequence"
	default:
		return "end_turn"
	}
}

func writeAnthropicSSEEvent(buf *bytes.Buffer, event string, payload any) {
	b, _ := json.Marshal(payload)
	buf.WriteString("event: ")
	buf.WriteString(event)
	buf.WriteString("\ndata: ")
	buf.Write(b)
	buf.WriteString("\n\n")
}

func buildAnthropicJSONBytes(resp OpenAIChatResponse, fallbackModel string) []byte {
	return buildAnthropicJSONBytesWithHint(resp, fallbackModel, "")
}

func buildAnthropicJSONBytesWithHint(resp OpenAIChatResponse, fallbackModel, keyHint string) []byte {
	model := strings.TrimSpace(resp.Model)
	if model == "" {
		model = strings.TrimSpace(fallbackModel)
	}
	if model == "" {
		model = "unknown"
	}

	content := ""
	reasoning := ""
	finishReason := "stop"
	if len(resp.Choices) > 0 {
		if resp.Choices[0].Message != nil {
			content = resp.Choices[0].Message.Content
			reasoning = resp.Choices[0].Message.ReasoningContent
		} else if resp.Choices[0].Delta != nil {
			content = resp.Choices[0].Delta.Content
			reasoning = resp.Choices[0].Delta.ReasoningContent
		}
		if strings.TrimSpace(resp.Choices[0].FinishReason) != "" {
			finishReason = resp.Choices[0].FinishReason
		}
	}

	promptToks := 0
	compToks := 0
	if resp.Usage != nil {
		promptToks = resp.Usage.PromptTokens
		compToks = resp.Usage.CompletionTokens
		if compToks <= 0 && resp.Usage.TotalTokens > promptToks {
			compToks = resp.Usage.TotalTokens - promptToks
		}
	}
	if compToks <= 0 && (len(content) > 0 || len(reasoning) > 0) {
		compToks = (len(content) + len(reasoning) + 3) / 4
		if compToks <= 0 {
			compToks = 1
		}
	}

	blocks := make([]anthWireContentBlock, 0, 2)
	if reasoning != "" {
		rCopy := reasoning
		blocks = append(blocks, anthWireContentBlock{Type: "thinking", Thinking: &rCopy})
	}
	cCopy := content
	blocks = append(blocks, anthWireContentBlock{Type: "text", Text: &cCopy})

	stopReason := mapOpenAIFinishReasonToAnthropic(finishReason)
	anthMsg := anthropicMessageResponse{
		ID:           normalizeAnthropicMessageID(resp.ID, keyHint),
		Type:         "message",
		Role:         "assistant",
		Model:        model,
		Content:      blocks,
		StopReason:   &stopReason,
		StopSequence: nil,
		Usage: anthropicUsage{
			InputTokens:  promptToks,
			OutputTokens: compToks,
		},
	}
	anthRaw, _ := json.Marshal(anthMsg)
	anthJSONBytes := make([]byte, 0, len(anthRaw)+1)
	anthJSONBytes = append(anthJSONBytes, anthRaw...)
	anthJSONBytes = append(anthJSONBytes, '\n')
	return anthJSONBytes
}

// Put stores a response in the cache with pre-serialized JSON & SSE payloads and O(1) LRU eviction.
func (c *ResponseCache) Put(req OpenAIChatRequest, resp OpenAIChatResponse) {
	if !c.IsEnabled() || !IsCacheable(req) {
		return
	}
	if len(resp.Choices) > 0 && resp.Choices[0].Message != nil {
		if IsUnsupportedClientBanner(resp.Choices[0].Message.Content) {
			return
		}
	}

	// Compute SHA-256 key and pre-serialize wire bytes OUTSIDE any mutex
	key := ComputeCacheKey(req)
	entry := buildPreSerializedCacheEntry(key, req, resp)

	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.IsEnabled() {
		return
	}

	if elem, ok := c.entries[key]; ok {
		prev := elem.Value.(*CacheEntry)
		entry.HitCount = atomic.LoadInt64(&prev.HitCount)
		entry.TokensSaved = atomic.LoadInt64(&prev.TokensSaved)
		elem.Value = entry
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
		"enabled":            c.IsEnabled(),
		"items":              items,
		"total_entries":      items,
		"max_entries":        maxEntries,
		"max_capacity":       maxEntries,
		"hits":               hits,
		"misses":             misses,
		"hit_ratio":          ratio,
		"tokens_saved":       tokensSaved,
		"total_tokens_saved": tokensSaved,
		"ttl_hours":          ttlHours,
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
			Action  string `json:"action"`
			Enabled *bool  `json:"enabled"`
			Clear   bool   `json:"clear"`
		}
		if r.Body != nil {
			if err := json.NewDecoder(io.LimitReader(r.Body, 64*1024)).Decode(&req); err == nil {
				if req.Clear || strings.EqualFold(strings.TrimSpace(req.Action), "clear") {
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
