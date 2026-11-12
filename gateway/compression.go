package gateway

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const maxLiteCompressMessageBytes = 128 * 1024

var (
	// Regex patterns for conversational filler and hedging (Caveman mode)
	politeOpeners = regexp.MustCompile(`(?i)\b(could you please|would you kindly|would you please|can you please|can you help me with|i would like you to|i want you to|kindly|please)\b\s*`)
	politeClosers = regexp.MustCompile(`(?i)\b(thank you in advance|thanks in advance|thank you very much|thank you so much|thank you|thanks)\b[.!?,]?\s*`)
	greetings     = regexp.MustCompile(`(?i)^(hello|hi there|hi|hey|greetings)[.!?,]?\s*`)
	aiDisclaimers = regexp.MustCompile(`(?i)\b(as an ai language model|as an ai assistant|as an ai)\b[.!?,]?\s*`)
	hedgePhrases  = regexp.MustCompile(`(?i)\b(in my opinion|basically|essentially|needless to say|it is important to note that|to be honest|frankly speaking|it should be noted that)\b[.!?,]?\s*`)
	multiNewlines = regexp.MustCompile(`\n{3,}`)
	multiSpaces   = regexp.MustCompile(`[ \t]{2,}`)

	// Global in-memory stats cache for compression (lock-free hot path)
	globalTokensSaved      int64
	globalCallsCompressed  int64
	pendingTokensSaved     atomic.Int64
	pendingCallsCompressed atomic.Int64
	compSyncScheduled      atomic.Bool
	gzipEnabledFast        atomic.Bool
)

func init() {
	gzipEnabledFast.Store(true)
}

// RecordCompressionSavings records cumulative token savings and request count
// using lock-free atomic increments and a 2-second debounced background config sync.
func RecordCompressionSavings(savedTokens int) {
	if savedTokens <= 0 {
		return
	}
	atomic.AddInt64(&globalTokensSaved, int64(savedTokens))
	atomic.AddInt64(&globalCallsCompressed, 1)
	pendingTokensSaved.Add(int64(savedTokens))
	pendingCallsCompressed.Add(1)

	if compSyncScheduled.CompareAndSwap(false, true) {
		scheduledPath := GatewayConfigFilePath()
		time.AfterFunc(2*time.Second, func() {
			compSyncScheduled.Store(false)
			flushCompressionSavingsForPath(scheduledPath)
		})
	}
}

// FlushCompressionSavings immediately flushes any pending in-memory compression counters to config.
func FlushCompressionSavings() {
	compSyncScheduled.Store(false)
	flushCompressionSavingsForPath(GatewayConfigFilePath())
}

func flushCompressionSavingsForPath(expectedPath string) {
	deltaTokens := pendingTokensSaved.Swap(0)
	deltaCalls := pendingCallsCompressed.Swap(0)
	if deltaTokens <= 0 && deltaCalls <= 0 {
		return
	}

	configMu.Lock()
	defer configMu.Unlock()
	if expectedPath != "" && GatewayConfigFilePath() != expectedPath {
		return
	}
	cfg := loadSavedAuthConfigLocked()
	if cfg.CompressionConfig == nil {
		cfg.CompressionConfig = &CompressionConfig{
			Mode:            CompressionLite,
			CompressCode:    false,
			MaxHistoryTurns: 10,
			EnableGzip:      true,
		}
	}
	cfg.CompressionConfig.TotalTokensSaved += deltaTokens
	cfg.CompressionConfig.TotalCallsHandled += deltaCalls
	saveAuthConfigAsyncLocked(cfg)
}

// GetCompressionConfig returns the current compression configuration and live stats.
func GetCompressionConfig() CompressionConfig {
	configMu.Lock()
	defer configMu.Unlock()
	cfg := loadSavedAuthConfigLocked()
	if cfg.CompressionConfig == nil {
		res := CompressionConfig{
			Mode:              CompressionLite,
			CompressCode:      false,
			MaxHistoryTurns:   10,
			EnableGzip:        true,
			TotalTokensSaved:  pendingTokensSaved.Load(),
			TotalCallsHandled: pendingCallsCompressed.Load(),
		}
		gzipEnabledFast.Store(true)
		return res
	}
	res := *cfg.CompressionConfig
	res.TotalTokensSaved += pendingTokensSaved.Load()
	res.TotalCallsHandled += pendingCallsCompressed.Load()

	memSaved := atomic.LoadInt64(&globalTokensSaved)
	if memSaved > res.TotalTokensSaved && res.TotalTokensSaved > 0 {
		res.TotalTokensSaved = memSaved
	}
	memCalls := atomic.LoadInt64(&globalCallsCompressed)
	if memCalls > res.TotalCallsHandled && res.TotalCallsHandled > 0 {
		res.TotalCallsHandled = memCalls
	}
	gzipEnabledFast.Store(res.EnableGzip)
	return res
}

// SetCompressionConfig saves updated compression parameters.
func SetCompressionConfig(newCfg CompressionConfig) error {
	deltaTokens := pendingTokensSaved.Swap(0)
	deltaCalls := pendingCallsCompressed.Swap(0)

	configMu.Lock()
	defer configMu.Unlock()
	cfg := loadSavedAuthConfigLocked()
	if cfg.CompressionConfig != nil {
		newCfg.TotalTokensSaved = cfg.CompressionConfig.TotalTokensSaved + deltaTokens
		newCfg.TotalCallsHandled = cfg.CompressionConfig.TotalCallsHandled + deltaCalls
	} else {
		newCfg.TotalTokensSaved += deltaTokens
		newCfg.TotalCallsHandled += deltaCalls
	}
	cfg.CompressionConfig = &newCfg
	cfg.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	gzipEnabledFast.Store(newCfg.EnableGzip)
	return saveAuthConfigLocked(cfg)
}

func isJSONArrayContent(raw json.RawMessage) bool {
	for _, b := range raw {
		switch b {
		case ' ', '\t', '\r', '\n':
			continue
		case '[':
			return true
		default:
			return false
		}
	}
	return false
}

// CompressRequest applies the configured compression pipeline to an incoming OpenAI chat request.
// Preserves multimodal array content blocks intact, skips expensive Lite regex passes on >128KB messages,
// and computes before/after token estimates in a single pass without redundant JSON unmarshaling.
func CompressRequest(req OpenAIChatRequest, cfg CompressionConfig) (OpenAIChatRequest, int, int) {
	tokensBefore := EstimateTokens(req.Messages)
	if cfg.Mode == CompressionOff || cfg.Mode == "" || len(req.Messages) == 0 {
		return req, tokensBefore, tokensBefore
	}

	cloned := req
	cloned.Messages = make([]OpenAIChatMessage, len(req.Messages))
	copy(cloned.Messages, req.Messages)

	// 1. History compaction (Ultra mode)
	if cfg.Mode == CompressionUltra {
		maxTurns := cfg.MaxHistoryTurns
		if maxTurns <= 0 {
			maxTurns = 10
		}
		cloned.Messages = compressUltraHistory(cloned.Messages, maxTurns)
	}

	// 2. Single-pass message-by-message compression + token char accumulation
	charsAfter := 0
	for i := range cloned.Messages {
		roleLen := len(cloned.Messages[i].Role)
		raw := cloned.Messages[i].Content
		if len(raw) == 0 {
			charsAfter += roleLen
			continue
		}

		// Preserve OpenAI/Anthropic multimodal content arrays (e.g. image_url / content blocks) intact!
		if isJSONArrayContent(raw) {
			charsAfter += roleLen + estimateRawContentChars(raw)
			continue
		}

		// Fast-path: skip expensive multi-pass regex/line splitting on >128KB code prompts in Lite mode
		if cfg.Mode == CompressionLite && len(raw) > maxLiteCompressMessageBytes {
			charsAfter += roleLen + estimateRawContentChars(raw)
			continue
		}

		var text string
		if err := json.Unmarshal(raw, &text); err != nil {
			charsAfter += roleLen + estimateRawContentChars(raw)
			continue
		}
		if text == "" {
			charsAfter += roleLen
			continue
		}

		var compressed string
		switch cfg.Mode {
		case CompressionLite:
			compressed = CompressTextLite(text, cfg.CompressCode)
		case CompressionCaveman:
			compressed = CompressTextCaveman(text, cfg.CompressCode)
		case CompressionRTK, CompressionUltra:
			compressed = CompressTextRTK(text, cfg.CompressCode)
			compressed = CompressTextCaveman(compressed, cfg.CompressCode)
		default:
			compressed = CompressTextLite(text, cfg.CompressCode)
		}

		if compressed == text {
			charsAfter += roleLen + len(text)
			continue
		}

		encoded, err := json.Marshal(compressed)
		if err == nil {
			cloned.Messages[i].Content = encoded
			charsAfter += roleLen + len(compressed)
		} else {
			charsAfter += roleLen + len(text)
		}
	}

	tokensAfter := charsToTokens(charsAfter)
	if tokensAfter > tokensBefore {
		tokensAfter = tokensBefore
	}
	return cloned, tokensBefore, tokensAfter
}

// CompressTextLite normalizes redundant whitespace, collapses blank lines,
// and trims whitespace outside code blocks.
func CompressTextLite(text string, compressCode bool) string {
	if text == "" {
		return ""
	}
	if !strings.Contains(text, "```") || compressCode {
		return cleanLiteWhitespace(text)
	}

	// Split by markdown code fences to preserve code syntax
	parts := strings.Split(text, "```")
	for i := 0; i < len(parts); i++ {
		if i%2 == 0 { // Outside code fence
			parts[i] = cleanLiteWhitespace(parts[i])
		}
	}
	return strings.Join(parts, "```")
}

func cleanLiteWhitespace(s string) string {
	if strings.Contains(s, "\n\n\n") {
		s = multiNewlines.ReplaceAllString(s, "\n\n")
	}
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		trimmed := strings.TrimRight(l, " \t")
		// Do not collapse indentation if leading spaces exist
		if len(trimmed) > 0 && trimmed[0] != ' ' && trimmed[0] != '\t' &&
			(strings.Contains(trimmed, "  ") || strings.IndexByte(trimmed, '\t') >= 0) {
			trimmed = multiSpaces.ReplaceAllString(trimmed, " ")
		}
		lines[i] = trimmed
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// CompressTextCaveman removes conversational filler, hedging, and polite boilerplate.
func CompressTextCaveman(text string, compressCode bool) string {
	text = CompressTextLite(text, compressCode)
	if text == "" {
		return ""
	}
	if !strings.Contains(text, "```") || compressCode {
		return applyCavemanRules(text)
	}

	parts := strings.Split(text, "```")
	for i := 0; i < len(parts); i++ {
		if i%2 == 0 { // Outside code fences
			parts[i] = applyCavemanRules(parts[i])
		}
	}
	return strings.Join(parts, "```")
}

func applyCavemanRules(s string) string {
	s = politeOpeners.ReplaceAllString(s, "")
	s = politeClosers.ReplaceAllString(s, "")
	s = greetings.ReplaceAllString(s, "")
	s = aiDisclaimers.ReplaceAllString(s, "")
	s = hedgePhrases.ReplaceAllString(s, "")
	s = multiSpaces.ReplaceAllString(s, " ")
	s = multiNewlines.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}

// CompressTextRTK condenses massive terminal, build, test, and git outputs.
func CompressTextRTK(text string, compressCode bool) string {
	// If text is short, no RTK pruning needed
	if len(text) < 1200 {
		return text
	}

	lines := strings.Split(text, "\n")
	if len(lines) < 35 {
		return text
	}

	// Detect if this is likely a terminal / test / git log
	isLog := strings.Contains(text, "PASS") || strings.Contains(text, "FAIL") ||
		strings.Contains(text, "diff --git") || strings.Contains(text, "npm ") ||
		strings.Contains(text, "go test") || strings.Contains(text, "error") ||
		strings.Contains(text, "warning") || strings.Contains(text, "exit status")

	if !isLog {
		return text
	}

	var kept []string
	var omittedCount int

	for i, l := range lines {
		// Always keep head (first 8 lines) and tail (last 12 lines)
		if i < 8 || i >= len(lines)-12 {
			if omittedCount > 0 {
				kept = append(kept, fmt.Sprintf("[... %d lines omitted by RTK log trimmer ...]", omittedCount))
				omittedCount = 0
			}
			kept = append(kept, l)
			continue
		}

		// Keep lines with errors, failures, or summary keywords
		lower := strings.ToLower(l)
		if strings.Contains(lower, "fail") || strings.Contains(lower, "error") ||
			strings.Contains(lower, "panic") || strings.Contains(lower, "exception") ||
			strings.Contains(lower, "warning") || strings.Contains(lower, "fatal") ||
			strings.Contains(lower, "assert") {
			if omittedCount > 0 {
				kept = append(kept, fmt.Sprintf("[... %d lines omitted by RTK log trimmer ...]", omittedCount))
				omittedCount = 0
			}
			kept = append(kept, l)
		} else {
			omittedCount++
		}
	}
	if omittedCount > 0 {
		kept = append(kept, fmt.Sprintf("[... %d lines omitted by RTK log trimmer ...]", omittedCount))
	}

	return strings.Join(kept, "\n")
}

// compressUltraHistory condenses long multi-turn conversations:
// Retains system prompts, the first user prompt (core goal), and the most recent N turns.
func compressUltraHistory(msgs []OpenAIChatMessage, maxTurns int) []OpenAIChatMessage {
	if len(msgs) <= maxTurns || maxTurns < 4 {
		return msgs
	}

	var systemMsgs []OpenAIChatMessage
	var nonSystem []OpenAIChatMessage

	for _, m := range msgs {
		if m.Role == "system" {
			systemMsgs = append(systemMsgs, m)
		} else {
			nonSystem = append(nonSystem, m)
		}
	}

	if len(nonSystem) <= maxTurns {
		return msgs
	}

	// Keep initial prompt (turn 0)
	initialTurn := nonSystem[0]

	// Keep last (maxTurns - 2) turns
	tailCount := maxTurns - 2
	if tailCount < 2 {
		tailCount = 2
	}
	tailTurns := nonSystem[len(nonSystem)-tailCount:]

	// Middle turns are condensed into a summary placeholder
	middleCount := len(nonSystem) - 1 - tailCount
	summaryMsg := OpenAIChatMessage{
		Role:    "system",
		Content: []byte(fmt.Sprintf(`"[OmniRoute Ultra-Compaction: %d previous conversational turns summarized and pruned to save context token budget]"`, middleCount)),
	}

	var result []OpenAIChatMessage
	result = append(result, systemMsgs...)
	result = append(result, initialTurn)
	result = append(result, summaryMsg)
	result = append(result, tailTurns...)
	return result
}

func charsToTokens(totalChars int) int {
	if totalChars <= 0 {
		return 0
	}
	// Roughly 3.8 characters per token for mixed English / code
	tokens := int(float64(totalChars) / 3.8)
	if tokens < 1 {
		return 1
	}
	return tokens
}

// unescapedJSONStringLen counts the exact unmarshaled byte length of an inner JSON string slice (without quotes)
// in a single pass with zero heap allocations.
func unescapedJSONStringLen(inner []byte) int {
	if bytes.IndexByte(inner, '\\') < 0 {
		return len(inner)
	}
	n := 0
	for i := 0; i < len(inner); i++ {
		if inner[i] == '\\' && i+1 < len(inner) {
			i++
			if inner[i] == 'u' && i+4 < len(inner) {
				i += 4
				n += 2
				continue
			}
		}
		n++
	}
	return n
}

var jsonTextFieldKey = []byte(`"text"`)

// estimateJSONArrayTextChars scans a JSON multimodal array for "text":"..." string fields with zero heap allocations.
func estimateJSONArrayTextChars(raw []byte) int {
	total := 0
	parts := 0
	idx := 0
	for idx < len(raw) {
		pos := bytes.Index(raw[idx:], jsonTextFieldKey)
		if pos < 0 {
			break
		}
		idx += pos + len(jsonTextFieldKey)
		for idx < len(raw) && (raw[idx] == ' ' || raw[idx] == '\t' || raw[idx] == '\r' || raw[idx] == '\n') {
			idx++
		}
		if idx >= len(raw) || raw[idx] != ':' {
			continue
		}
		idx++
		for idx < len(raw) && (raw[idx] == ' ' || raw[idx] == '\t' || raw[idx] == '\r' || raw[idx] == '\n') {
			idx++
		}
		if idx >= len(raw) || raw[idx] != '"' {
			continue
		}
		idx++ // skip opening quote
		start := idx
		for idx < len(raw) {
			if raw[idx] == '\\' {
				idx += 2
				continue
			}
			if raw[idx] == '"' {
				break
			}
			idx++
		}
		if idx <= len(raw) {
			segLen := unescapedJSONStringLen(raw[start:idx])
			if segLen > 0 {
				if parts > 0 {
					total++ // newline separator matching ContentText()
				}
				total += segLen
				parts++
			}
			idx++ // skip closing quote
		}
	}
	return total
}

func estimateRawContentChars(raw json.RawMessage) int {
	b := bytes.TrimSpace(raw)
	if len(b) == 0 || bytes.Equal(b, []byte("null")) {
		return 0
	}
	if b[0] == '"' && len(b) >= 2 && b[len(b)-1] == '"' {
		return unescapedJSONStringLen(b[1 : len(b)-1])
	}
	if b[0] == '[' {
		return estimateJSONArrayTextChars(b)
	}
	return len(b)
}

// EstimateTokens calculates an approximate token count without allocating or re-unmarshaling JSON strings.
func EstimateTokens(msgs []OpenAIChatMessage) int {
	totalChars := 0
	for _, m := range msgs {
		totalChars += len(m.Role) + estimateRawContentChars(m.Content)
	}
	return charsToTokens(totalChars)
}

// --- HTTP Transport Gzip Middleware (Pooled + Lazy SSE Sniffing) ---

var gzipWriterPool = sync.Pool{
	New: func() any {
		gz, _ := gzip.NewWriterLevel(io.Discard, gzip.BestSpeed)
		return gz
	},
}

var lazyGzipWriterPool = sync.Pool{
	New: func() any {
		return &lazyGzipResponseWriter{}
	},
}

func hasSSEMediaType(v string) bool {
	v = strings.TrimSpace(v)
	const ssePrefix = "text/event-stream"
	return len(v) >= len(ssePrefix) && strings.EqualFold(v[:len(ssePrefix)], ssePrefix)
}

func addVaryAcceptEncoding(h http.Header) {
	existing := h.Get("Vary")
	if existing == "" {
		h.Set("Vary", "Accept-Encoding")
		return
	}
	if !strings.Contains(strings.ToLower(existing), "accept-encoding") {
		h.Set("Vary", existing+", Accept-Encoding")
	}
}

type lazyGzipResponseWriter struct {
	http.ResponseWriter
	gz          *gzip.Writer
	decided     bool
	passthrough bool
}

func (lw *lazyGzipResponseWriter) Unwrap() http.ResponseWriter {
	return lw.ResponseWriter
}

func (lw *lazyGzipResponseWriter) WriteHeader(status int) {
	if lw.decided {
		return
	}
	lw.decided = true

	h := lw.ResponseWriter.Header()
	if status == http.StatusNoContent ||
		status == http.StatusNotModified ||
		(status >= 100 && status < 200) ||
		hasSSEMediaType(h.Get("Content-Type")) ||
		h.Get("Content-Encoding") != "" {
		lw.passthrough = true
		lw.ResponseWriter.WriteHeader(status)
		return
	}

	lw.passthrough = false
	h.Del("Content-Length")
	h.Set("Content-Encoding", "gzip")
	addVaryAcceptEncoding(h)

	gz := gzipWriterPool.Get().(*gzip.Writer)
	gz.Reset(lw.ResponseWriter)
	lw.gz = gz
	lw.ResponseWriter.WriteHeader(status)
}

func (lw *lazyGzipResponseWriter) Write(b []byte) (int, error) {
	if !lw.decided {
		h := lw.ResponseWriter.Header()
		if h.Get("Content-Type") == "" && len(b) > 0 {
			trimmed := bytes.TrimLeft(b, " \t\r\n")
			if bytes.HasPrefix(trimmed, []byte("data:")) || bytes.HasPrefix(trimmed, []byte("event:")) {
				h.Set("Content-Type", "text/event-stream")
			} else {
				h.Set("Content-Type", http.DetectContentType(b))
			}
		}
		lw.WriteHeader(http.StatusOK)
	}
	if lw.passthrough {
		return lw.ResponseWriter.Write(b)
	}
	return lw.gz.Write(b)
}

func (lw *lazyGzipResponseWriter) Flush() {
	if !lw.decided {
		lw.WriteHeader(http.StatusOK)
	}
	if !lw.passthrough && lw.gz != nil {
		_ = lw.gz.Flush()
	}
	if flusher, ok := lw.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (lw *lazyGzipResponseWriter) finishAndRecycle() {
	if lw.gz != nil {
		_ = lw.gz.Close()
		lw.gz.Reset(io.Discard)
		gzipWriterPool.Put(lw.gz)
		lw.gz = nil
	}
	lw.ResponseWriter = nil
	lw.decided = false
	lw.passthrough = false
	lazyGzipWriterPool.Put(lw)
}

// withGzip wraps an http.Handler to lazily compress JSON/HTML responses via a sync.Pool of *gzip.Writer
// while passing Server-Sent Events (text/event-stream), 204/304, and HEAD responses raw with zero gzip allocation.
func withGzip(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !gzipEnabledFast.Load() ||
			r.Method == http.MethodHead ||
			r.Method == http.MethodOptions ||
			!strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") ||
			hasSSEMediaType(r.Header.Get("Accept")) ||
			strings.Contains(r.URL.Path, "stream") {
			next.ServeHTTP(w, r)
			return
		}

		lw := lazyGzipWriterPool.Get().(*lazyGzipResponseWriter)
		lw.ResponseWriter = w
		defer lw.finishAndRecycle()

		next.ServeHTTP(lw, r)
	})
}
