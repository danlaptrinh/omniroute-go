package gateway

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var (
	// Regex patterns for conversational filler and hedging (Caveman mode)
	politeOpeners = regexp.MustCompile(`(?i)\b(could you please|would you kindly|would you please|can you please|can you help me with|i would like you to|i want you to|kindly|please)\b\s*`)
	politeClosers = regexp.MustCompile(`(?i)\b(thank you in advance|thanks in advance|thank you very much|thank you so much|thank you|thanks)\b[.!?,]?\s*`)
	greetings     = regexp.MustCompile(`(?i)^(hello|hi there|hi|hey|greetings)[.!?,]?\s*`)
	aiDisclaimers = regexp.MustCompile(`(?i)\b(as an ai language model|as an ai assistant|as an ai)\b[.!?,]?\s*`)
	hedgePhrases  = regexp.MustCompile(`(?i)\b(in my opinion|basically|essentially|needless to say|it is important to note that|to be honest|frankly speaking|it should be noted that)\b[.!?,]?\s*`)
	multiNewlines = regexp.MustCompile(`\n{3,}`)
	multiSpaces   = regexp.MustCompile(`[ \t]{2,}`)

	// Global in-memory stats cache for compression
	compStatsMu          sync.RWMutex
	globalTokensSaved    int64
	globalCallsCompressed int64
)

// RecordCompressionSavings records cumulative token savings and request count.
func RecordCompressionSavings(savedTokens int) {
	if savedTokens <= 0 {
		return
	}
	atomic.AddInt64(&globalTokensSaved, int64(savedTokens))
	atomic.AddInt64(&globalCallsCompressed, 1)

	// Persist asynchronously without blocking the request path
	configMu.Lock()
	defer configMu.Unlock()
	cfg := loadSavedAuthConfigLocked()
	if cfg.CompressionConfig != nil {
		cfg.CompressionConfig.TotalTokensSaved += int64(savedTokens)
		cfg.CompressionConfig.TotalCallsHandled++
		saveAuthConfigAsyncLocked(cfg)
	}
}

// GetCompressionConfig returns the current compression configuration and live stats.
func GetCompressionConfig() CompressionConfig {
	configMu.Lock()
	defer configMu.Unlock()
	cfg := loadSavedAuthConfigLocked()
	if cfg.CompressionConfig == nil {
		return CompressionConfig{
			Mode:            CompressionLite,
			CompressCode:    false,
			MaxHistoryTurns: 10,
			EnableGzip:      true,
		}
	}
	res := *cfg.CompressionConfig
	// Update with atomic runtime counts if higher
	memSaved := atomic.LoadInt64(&globalTokensSaved)
	if memSaved > res.TotalTokensSaved {
		res.TotalTokensSaved = memSaved
	}
	memCalls := atomic.LoadInt64(&globalCallsCompressed)
	if memCalls > res.TotalCallsHandled {
		res.TotalCallsHandled = memCalls
	}
	return res
}

// SetCompressionConfig saves updated compression parameters.
func SetCompressionConfig(newCfg CompressionConfig) error {
	configMu.Lock()
	defer configMu.Unlock()
	cfg := loadSavedAuthConfigLocked()
	if cfg.CompressionConfig != nil {
		newCfg.TotalTokensSaved = cfg.CompressionConfig.TotalTokensSaved
		newCfg.TotalCallsHandled = cfg.CompressionConfig.TotalCallsHandled
	}
	cfg.CompressionConfig = &newCfg
	cfg.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	return saveAuthConfigLocked(cfg)
}

// CompressRequest applies the configured compression pipeline to an incoming OpenAI chat request.
// Returns the compressed request, estimated tokens before, and estimated tokens after.
func CompressRequest(req OpenAIChatRequest, cfg CompressionConfig) (OpenAIChatRequest, int, int) {
	tokensBefore := EstimateTokens(req.Messages)
	if cfg.Mode == CompressionOff || cfg.Mode == "" {
		return req, tokensBefore, tokensBefore
	}

	cloned := req
	cloned.Messages = make([]OpenAIChatMessage, len(req.Messages))
	for i, m := range req.Messages {
		cloned.Messages[i] = m
	}

	// 1. History compaction (Ultra mode)
	if cfg.Mode == CompressionUltra {
		maxTurns := cfg.MaxHistoryTurns
		if maxTurns <= 0 {
			maxTurns = 10
		}
		cloned.Messages = compressUltraHistory(cloned.Messages, maxTurns)
	}

	// 2. Message-by-message compression
	for i := range cloned.Messages {
		text := cloned.Messages[i].ContentText()
		if text == "" {
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

		// Re-encode message content as JSON string
		raw, err := json.Marshal(compressed)
		if err == nil {
			cloned.Messages[i].Content = raw
		}
	}

	tokensAfter := EstimateTokens(cloned.Messages)
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
	s = multiNewlines.ReplaceAllString(s, "\n\n")
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		trimmed := strings.TrimRight(l, " \t")
		// Do not collapse indentation if leading spaces exist
		if len(trimmed) > 0 && trimmed[0] != ' ' && trimmed[0] != '\t' {
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
				kept = append(kept, fmtSprintf("[... %d lines omitted by RTK log trimmer ...]", omittedCount))
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
				kept = append(kept, fmtSprintf("[... %d lines omitted by RTK log trimmer ...]", omittedCount))
				omittedCount = 0
			}
			kept = append(kept, l)
		} else {
			omittedCount++
		}
	}
	if omittedCount > 0 {
		kept = append(kept, fmtSprintf("[... %d lines omitted by RTK log trimmer ...]", omittedCount))
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
		Role: "system",
		Content: []byte(fmtSprintf(`"[OmniRoute Ultra-Compaction: %d previous conversational turns summarized and pruned to save context token budget]"`, middleCount)),
	}

	var result []OpenAIChatMessage
	result = append(result, systemMsgs...)
	result = append(result, initialTurn)
	result = append(result, summaryMsg)
	result = append(result, tailTurns...)
	return result
}

// EstimateTokens calculates an approximate token count based on standard character/word heuristic.
func EstimateTokens(msgs []OpenAIChatMessage) int {
	totalChars := 0
	for _, m := range msgs {
		totalChars += len(m.Role) + len(m.ContentText())
	}
	// Roughly 3.8 characters per token for mixed English / code
	tokens := int(float64(totalChars) / 3.8)
	if tokens < 1 && totalChars > 0 {
		return 1
	}
	return tokens
}

func fmtSprintf(format string, args ...any) string {
	b := &bytes.Buffer{}
	var aIdx int
	for i := 0; i < len(format); i++ {
		if format[i] == '%' && i+1 < len(format) {
			spec := format[i+1]
			if spec == 'd' && aIdx < len(args) {
				if d, ok := args[aIdx].(int); ok {
					b.WriteString(intToString(d))
					aIdx++
					i++
					continue
				}
			} else if spec == 's' && aIdx < len(args) {
				if s, ok := args[aIdx].(string); ok {
					b.WriteString(s)
					aIdx++
					i++
					continue
				}
			}
		}
		b.WriteByte(format[i])
	}
	return b.String()
}

func intToString(n int) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	var buf [20]byte
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + (n % 10))
		n /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}

// --- HTTP Transport Gzip Middleware ---

type gzipResponseWriter struct {
	http.ResponseWriter
	writer *gzip.Writer
}

func (w *gzipResponseWriter) Write(b []byte) (int, error) {
	return w.writer.Write(b)
}

func (w *gzipResponseWriter) Flush() {
	_ = w.writer.Flush()
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// withGzip wraps an http.Handler to automatically compress responses when requested by client.
func withGzip(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Do not gzip for Server-Sent Events (streaming) or if client does not support gzip
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") ||
			r.Header.Get("Accept") == "text/event-stream" ||
			strings.Contains(r.URL.Path, "stream") {
			next.ServeHTTP(w, r)
			return
		}

		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Vary", "Accept-Encoding")

		gz, err := gzip.NewWriterLevel(w, gzip.BestSpeed)
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}
		defer gz.Close()

		gzw := &gzipResponseWriter{ResponseWriter: w, writer: gz}
		next.ServeHTTP(gzw, r)
	})
}
