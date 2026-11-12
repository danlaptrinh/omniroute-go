package gateway

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type hotpathCaptureUpstream struct {
	lastPinnedAccount atomic.Value
	streamMode        bool
	calls             atomic.Int64
}

func (u *hotpathCaptureUpstream) HandleChatCompletion(
	ctx context.Context,
	w http.ResponseWriter,
	req OpenAIChatRequest,
	resolved ResolvedModel,
	incomingAuth string,
) error {
	u.calls.Add(1)
	u.lastPinnedAccount.Store(PinnedAccountFromContext(ctx))

	if req.Stream || u.streamMode {
		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "data: {\"id\":\"chatcmpl-1\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"FastSSE\"}}]}\n\n")
		if fl, ok := w.(http.Flusher); ok {
			fl.Flush()
		}
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
		if fl, ok := w.(http.Flusher); ok {
			fl.Flush()
		}
		return nil
	}

	resp := OpenAIChatResponse{
		ID:      "chatcmpl-nonstream",
		Object:  "chat.completion",
		Created: 1740000000,
		Model:   req.Model,
		Choices: []OpenAIChatChoice{
			{
				Index:   0,
				Message: &OpenAIOutputMessage{Role: "assistant", Content: "FastJSON"},
			},
		},
		Usage: &OpenAIUsage{PromptTokens: 12, CompletionTokens: 30, TotalTokens: 42},
	}
	GetGlobalCache().Put(req, resp)
	w.Header().Set("X-Omni-Gateway-Tokens", "42")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	return json.NewEncoder(w).Encode(resp)
}

func TestHotpath_WithGzipSSENoGzipAndJSONPooledGzip(t *testing.T) {
	tmpCfg := filepath.Join(t.TempDir(), "gateway_config.json")
	t.Setenv("GATEWAY_CONFIG_FILE", tmpCfg)
	GetGlobalCache().Clear()
	defer GetGlobalCache().Clear()

	keyObj, err := CreateAPIKey("sse-tester", false, true, true, false)
	if err != nil {
		t.Fatalf("CreateAPIKey failed: %v", err)
	}

	up := &hotpathCaptureUpstream{}
	srv := NewServer(GatewayConfig{AgyOAuthToken: "ya29.test"}, NewResolver(), up)
	handler := srv.Handler()

	// 1. SSE on /v1/chat/completions with Accept-Encoding: gzip and Accept: application/json MUST NOT gzip!
	sseReq := httptest.NewRequest(
		http.MethodPost,
		"/v1/chat/completions",
		strings.NewReader(`{"model":"antigravity/gemini-3-flash","messages":[{"role":"user","content":"hi"}],"stream":true}`),
	)
	sseReq.Header.Set("Authorization", "Bearer "+keyObj.Key)
	sseReq.Header.Set("Content-Type", "application/json")
	sseReq.Header.Set("Accept", "application/json")
	sseReq.Header.Set("Accept-Encoding", "gzip, deflate, br")

	sseRec := httptest.NewRecorder()
	handler.ServeHTTP(sseRec, sseReq)

	if sseRec.Code != http.StatusOK {
		t.Fatalf("SSE status = %d, want 200", sseRec.Code)
	}
	if ce := sseRec.Header().Get("Content-Encoding"); ce != "" {
		t.Fatalf("SSE response must NOT set Content-Encoding: gzip, got %q", ce)
	}
	if !strings.HasPrefix(sseRec.Header().Get("Content-Type"), "text/event-stream") {
		t.Fatalf("unexpected SSE Content-Type: %q", sseRec.Header().Get("Content-Type"))
	}
	rawBody := sseRec.Body.String()
	if !strings.Contains(rawBody, "FastSSE") || !strings.Contains(rawBody, "data: [DONE]") {
		t.Fatalf("expected raw uncompressed SSE payload, got: %q", rawBody)
	}

	// 2. SSE on /v1/messages with Accept-Encoding: gzip MUST NOT gzip!
	antReq := httptest.NewRequest(
		http.MethodPost,
		"/v1/messages",
		strings.NewReader(`{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"hi"}],"stream":true}`),
	)
	antReq.Header.Set("x-api-key", keyObj.Key)
	antReq.Header.Set("Content-Type", "application/json")
	antReq.Header.Set("Accept-Encoding", "gzip")

	antRec := httptest.NewRecorder()
	handler.ServeHTTP(antRec, antReq)
	if ce := antRec.Header().Get("Content-Encoding"); ce != "" {
		t.Fatalf("/v1/messages SSE must NOT set Content-Encoding: gzip, got %q", ce)
	}

	// 3. 204 No Content (OPTIONS) & HEAD /v1/models & 304 Not Modified MUST NOT set Content-Encoding: gzip
	optReq := httptest.NewRequest(http.MethodOptions, "/v1/chat/completions", nil)
	optReq.Header.Set("Accept-Encoding", "gzip")
	optRec := httptest.NewRecorder()
	handler.ServeHTTP(optRec, optReq)
	if optRec.Code != http.StatusNoContent || optRec.Header().Get("Content-Encoding") != "" {
		t.Fatalf("204 OPTIONS unexpected status=%d Content-Encoding=%q", optRec.Code, optRec.Header().Get("Content-Encoding"))
	}

	headReq := httptest.NewRequest(http.MethodHead, "/v1/models", nil)
	headReq.Header.Set("Accept-Encoding", "gzip")
	headRec := httptest.NewRecorder()
	handler.ServeHTTP(headRec, headReq)
	if headRec.Header().Get("Content-Encoding") != "" {
		t.Fatalf("HEAD request must NOT set Content-Encoding: gzip, got %q", headRec.Header().Get("Content-Encoding"))
	}

	notModHandler := withGzip(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotModified)
	}))
	nmReq := httptest.NewRequest(http.MethodGet, "/ui", nil)
	nmReq.Header.Set("Accept-Encoding", "gzip")
	nmRec := httptest.NewRecorder()
	notModHandler.ServeHTTP(nmRec, nmReq)
	if nmRec.Header().Get("Content-Encoding") != "" {
		t.Fatalf("304 Not Modified must NOT set Content-Encoding: gzip")
	}

	// 4. Pooled Gzip for JSON (/v1/models) and HTML (/ui) under concurrent load
	var wg sync.WaitGroup
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			target := "/v1/models"
			if idx%2 == 1 {
				target = "/ui"
			}
			req := httptest.NewRequest(http.MethodGet, target, nil)
			req.Header.Set("Accept-Encoding", "gzip")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Errorf("GET %s status = %d", target, rec.Code)
				return
			}
			if rec.Header().Get("Content-Encoding") != "gzip" {
				t.Errorf("GET %s expected Content-Encoding=gzip, got %q", target, rec.Header().Get("Content-Encoding"))
				return
			}
			gzr, err := gzip.NewReader(bytes.NewReader(rec.Body.Bytes()))
			if err != nil {
				t.Errorf("GET %s invalid gzip stream: %v", target, err)
				return
			}
			decoded, err := io.ReadAll(gzr)
			_ = gzr.Close()
			if err != nil || len(decoded) == 0 {
				t.Errorf("GET %s failed reading gzip payload: err=%v len=%d", target, err, len(decoded))
			}
		}(i)
	}
	wg.Wait()
}

func TestHotpath_CompressRequest_MultimodalAndLargeLiteFastPath(t *testing.T) {
	tmpCfg := filepath.Join(t.TempDir(), "gateway_config.json")
	t.Setenv("GATEWAY_CONFIG_FILE", tmpCfg)
	defer FlushPendingConfigSave()

	multimodalRaw := json.RawMessage(`[{"type":"text","text":"Could you please inspect this screenshot? Thank you!"},{"type":"image_url","image_url":{"url":"data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="}}]`)
	textRaw, _ := json.Marshal("Hello, could you please optimize this function? Basically in my opinion it has extra spaces.    Thank you in advance!")

	req := OpenAIChatRequest{
		Model: "antigravity/gemini-4-carbon-high",
		Messages: []OpenAIChatMessage{
			{Role: "user", Content: multimodalRaw},
			{Role: "user", Content: textRaw},
		},
	}

	for _, mode := range []CompressionMode{CompressionLite, CompressionCaveman, CompressionRTK} {
		out, before, after := CompressRequest(req, CompressionConfig{Mode: mode})
		if !bytes.Equal(out.Messages[0].Content, multimodalRaw) {
			t.Fatalf("mode=%s corrupted multimodal array Content:\ngot:  %s\nwant: %s", mode, string(out.Messages[0].Content), string(multimodalRaw))
		}
		if mode == CompressionCaveman && after >= before {
			t.Fatalf("expected Caveman compression to reduce tokens on text message: before=%d after=%d", before, after)
		}
	}

	// Verify >128KB Lite fast-path skips expensive regex passes and preserves message verbatim in <5ms
	hugeCode := "package main\n\n" + strings.Repeat("func computeItem() int { return 42 }   \n\n\n", 4200) // ~180KB
	hugeRaw, _ := json.Marshal(hugeCode)
	if len(hugeRaw) <= maxLiteCompressMessageBytes {
		t.Fatalf("expected hugeRaw > %d bytes, got %d", maxLiteCompressMessageBytes, len(hugeRaw))
	}
	hugeReq := OpenAIChatRequest{
		Model: "antigravity/gemini-3-flash",
		Messages: []OpenAIChatMessage{
			{Role: "user", Content: hugeRaw},
		},
	}
	t0 := time.Now()
	hugeOut, tokBefore, tokAfter := CompressRequest(hugeReq, CompressionConfig{Mode: CompressionLite})
	elapsed := time.Since(t0)
	if !bytes.Equal(hugeOut.Messages[0].Content, hugeRaw) {
		t.Fatalf("expected >128KB Lite fast-path to preserve raw JSON message untouched")
	}
	if tokBefore <= 0 || tokAfter != tokBefore {
		t.Fatalf("expected tokBefore == tokAfter > 0 on skipped >128KB message, got %d vs %d", tokBefore, tokAfter)
	}
	if elapsed > 25*time.Millisecond {
		t.Errorf("expected >128KB Lite fast-path to complete nearly instantaneously, took %v", elapsed)
	}

	// Verify lock-free RecordCompressionSavings concurrency + real-time visibility
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			RecordCompressionSavings(15)
		}()
	}
	wg.Wait()

	compCfg := GetCompressionConfig()
	if compCfg.TotalTokensSaved < 600 || compCfg.TotalCallsHandled < 40 {
		t.Fatalf("expected at least 600 saved tokens & 40 calls in GetCompressionConfig, got saved=%d calls=%d", compCfg.TotalTokensSaved, compCfg.TotalCallsHandled)
	}
	FlushCompressionSavings()
}

func TestHotpath_ResponseCache_LockFreeReadsO1LRUAndServerFixes(t *testing.T) {
	tmpCfg := filepath.Join(t.TempDir(), "gateway_config.json")
	t.Setenv("GATEWAY_CONFIG_FILE", tmpCfg)
	defer FlushPendingConfigSave()

	// 1. O(1) container/list LRU touch & eviction order + >256KB prompt bypass
	cache := NewResponseCache(3, 5*time.Minute)
	makeReq := func(msg string) OpenAIChatRequest {
		b, _ := json.Marshal(msg)
		return OpenAIChatRequest{
			Model:    "antigravity/gemini-3-flash",
			Messages: []OpenAIChatMessage{{Role: "user", Content: b}},
		}
	}
	sampleResp := func(txt string) OpenAIChatResponse {
		return OpenAIChatResponse{
			ID:      "id-" + txt,
			Choices: []OpenAIChatChoice{{Message: &OpenAIOutputMessage{Role: "assistant", Content: txt}}},
			Usage:   &OpenAIUsage{TotalTokens: 30},
		}
	}

	cache.Put(makeReq("k1"), sampleResp("v1"))
	cache.Put(makeReq("k2"), sampleResp("v2"))
	cache.Put(makeReq("k3"), sampleResp("v3"))

	// Touch k1 so k2 becomes the oldest LRU item
	if got, ok := cache.Get(makeReq("k1")); !ok || got.Choices[0].Message.Content != "v1" {
		t.Fatalf("expected k1 cache hit")
	}
	// Inserting k4 must evict k2 (oldest), keeping k1, k3, k4
	cache.Put(makeReq("k4"), sampleResp("v4"))
	if _, ok := cache.Get(makeReq("k2")); ok {
		t.Fatalf("expected k2 to be evicted as oldest LRU entry")
	}
	for _, k := range []string{"k1", "k3", "k4"} {
		if _, ok := cache.Get(makeReq(k)); !ok {
			t.Fatalf("expected %s to remain in cache", k)
		}
	}

	// Huge >256KB prompt must bypass cache without evicting existing entries
	oversizedPrompt := strings.Repeat("x", MaxCacheablePromptBytes+1024)
	cache.Put(makeReq(oversizedPrompt), sampleResp("huge"))
	if _, ok := cache.Get(makeReq(oversizedPrompt)); ok {
		t.Fatalf("expected >256KB prompt to bypass ResponseCache")
	}
	if stats := cache.Stats(); stats["items"].(int) != 3 {
		t.Fatalf("expected 3 items preserved after oversized bypass, got %v", stats["items"])
	}

	// Disabled fast-path
	cache.SetEnabled(false)
	if _, ok := cache.Get(makeReq("k1")); ok {
		t.Fatalf("expected cache miss when disabled")
	}
	cache.SetEnabled(true)

	// Deterministic ComputeCacheKey across <=1KB stack fast-path and >1KB streaming path
	tempVal := 0.2
	compactReq := makeReq("short prompt")
	compactReq.Temperature = &tempVal
	compactReq.MaxTokens = 512
	largeReq := makeReq(strings.Repeat("abcd", 600))
	largeReq.Temperature = &tempVal
	largeReq.MaxTokens = 512
	if kA, kB := ComputeCacheKey(compactReq), ComputeCacheKey(compactReq); len(kA) != 64 || kA != kB {
		t.Fatalf("compact ComputeCacheKey mismatch: %q vs %q", kA, kB)
	}
	if kA, kB := ComputeCacheKey(largeReq), ComputeCacheKey(largeReq); len(kA) != 64 || kA != kB {
		t.Fatalf("large ComputeCacheKey mismatch: %q vs %q", kA, kB)
	}
	if ComputeCacheKey(compactReq) == ComputeCacheKey(largeReq) {
		t.Fatalf("expected distinct cache keys for distinct prompts")
	}

	// 2. Server Fixes:
	//    a) Single RecordAPIKeyUsage call on non-stream cache hit (no double-counting!)
	//    b) X-Omni-Pin-Account honored in /v1/messages & exposed in CORS
	//    c) /v1/models?connected=1 filters unconfigured providers while /v1/models keeps full catalog
	GetGlobalCache().Clear()
	defer GetGlobalCache().Clear()

	empKey, err := CreateAPIKey("cache-accounting-emp", false, true, true, false)
	if err != nil {
		t.Fatalf("CreateAPIKey failed: %v", err)
	}

	up := &hotpathCaptureUpstream{}
	srv := NewServer(GatewayConfig{AgyOAuthToken: "ya29.connected-agy"}, NewResolver(), up)
	handler := srv.Handler()

	chatBody := `{"model":"antigravity/gemini-3-flash","messages":[{"role":"user","content":"exact cache accounting test"}],"stream":false}`
	for attempt := 1; attempt <= 2; attempt++ {
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(chatBody))
		req.Header.Set("Authorization", "Bearer "+empKey.Key)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("attempt %d returned %d: %s", attempt, rec.Code, rec.Body.String())
		}
		wantCacheHdr := "MISS"
		if attempt == 2 {
			wantCacheHdr = "HIT"
		}
		if got := rec.Header().Get("X-Omni-Gateway-Cache"); got != wantCacheHdr {
			t.Fatalf("attempt %d X-Omni-Gateway-Cache = %q, want %q", attempt, got, wantCacheHdr)
		}
	}

	keys := ListAPIKeys()
	var matchedKey *GatewayAPIKey
	for i := range keys {
		if keys[i].ID == empKey.ID {
			matchedKey = &keys[i]
			break
		}
	}
	if matchedKey == nil {
		t.Fatalf("API key %s not found", empKey.ID)
	}
	if matchedKey.TotalRequests != 2 {
		t.Fatalf("expected TotalRequests == 2 after 1 MISS + 1 HIT (no duplicate RecordAPIKeyUsage), got %d", matchedKey.TotalRequests)
	}
	if matchedKey.TotalTokens != 84 {
		t.Fatalf("expected TotalTokens == 84 (42 + 42), got %d", matchedKey.TotalTokens)
	}

	// Verify /v1/messages honors X-Omni-Pin-Account
	msgReq := httptest.NewRequest(
		http.MethodPost,
		"/v1/messages",
		strings.NewReader(`{"model":"antigravity/claude-sonnet-4-6","max_tokens":64,"messages":[{"role":"user","content":"pin check"}]}`),
	)
	msgReq.Header.Set("x-api-key", empKey.Key)
	msgReq.Header.Set("X-Omni-Pin-Account", "pinned-pro@gmail.com")
	msgReq.Header.Set("Content-Type", "application/json")
	msgRec := httptest.NewRecorder()
	handler.ServeHTTP(msgRec, msgReq)
	if msgRec.Code != http.StatusOK {
		t.Fatalf("/v1/messages failed with %d: %s", msgRec.Code, msgRec.Body.String())
	}
	if gotPin, _ := up.lastPinnedAccount.Load().(string); gotPin != "pinned-pro@gmail.com" {
		t.Fatalf("/v1/messages did not propagate X-Omni-Pin-Account: got %q, want %q", gotPin, "pinned-pro@gmail.com")
	}

	// Verify CORS headers include X-Omni-Pin-Account
	corsReq := httptest.NewRequest(http.MethodOptions, "/v1/messages", nil)
	corsRec := httptest.NewRecorder()
	handler.ServeHTTP(corsRec, corsReq)
	if !strings.Contains(corsRec.Header().Get("Access-Control-Allow-Headers"), "X-Omni-Pin-Account") {
		t.Fatalf("Access-Control-Allow-Headers missing X-Omni-Pin-Account: %q", corsRec.Header().Get("Access-Control-Allow-Headers"))
	}

	// Verify /v1/models vs /v1/models?connected=1
	allModelsReq := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	allModelsRec := httptest.NewRecorder()
	handler.ServeHTTP(allModelsRec, allModelsReq)
	var allPayload struct {
		Data []OpenAIModelCard `json:"data"`
	}
	_ = json.Unmarshal(allModelsRec.Body.Bytes(), &allPayload)

	connModelsReq := httptest.NewRequest(http.MethodGet, "/v1/models?connected=1", nil)
	connModelsRec := httptest.NewRecorder()
	handler.ServeHTTP(connModelsRec, connModelsReq)
	var connPayload struct {
		Data []OpenAIModelCard `json:"data"`
	}
	_ = json.Unmarshal(connModelsRec.Body.Bytes(), &connPayload)

	if len(allPayload.Data) <= len(connPayload.Data) || len(connPayload.Data) == 0 {
		t.Fatalf("expected /v1/models (%d) > /v1/models?connected=1 (%d) > 0", len(allPayload.Data), len(connPayload.Data))
	}
	for _, card := range connPayload.Data {
		if card.Provider == string(ProviderClaudeCode) || card.Provider == string(ProviderAlibaba) {
			t.Fatalf("unexpected unconfigured provider model %q in /v1/models?connected=1", card.ID)
		}
	}
}

func TestHotpath_DefaultVectorDBOffAndAutomationSchedulerOnce(t *testing.T) {
	tmpCfg := filepath.Join(t.TempDir(), "gateway_config.json")
	t.Setenv("GATEWAY_CONFIG_FILE", tmpCfg)
	vectorFastState.Store(0)
	defer func() {
		FlushPendingConfigSave()
		vectorFastState.Store(0)
	}()

	// 1. Default VectorDB config MUST be completely disabled out-of-the-box
	vc := GetVectorDBConfig()
	if vc.Enabled || vc.AutoLearnPrompts || vc.AutoInjectRAG {
		t.Fatalf("expected defaultVectorDBConfig all false, got %+v", vc)
	}
	if IsVectorDBEnabledFast() || IsVectorRAGEnabledFast() || IsVectorAutoLearnEnabledFast() {
		t.Fatalf("expected atomic fast-path flags all false by default")
	}

	// Seed a high-similarity memory and confirm ApplyVectorMemoryRAG / InjectVectorContextIntoMessages skip it when disabled
	_, err := AddVectorMemory(
		"Quy trình deploy server",
		"devops",
		"Làm sao deploy phần mềm lên server chuẩn?",
		"Sử dụng CI/CD nhánh main và symlink nguyên tử.",
		"Admin",
		"",
		"manual",
		true,
	)
	if err != nil {
		t.Fatalf("AddVectorMemory failed: %v", err)
	}

	promptRaw, _ := json.Marshal("Làm sao deploy phần mềm lên server chuẩn?")
	chatReq := OpenAIChatRequest{
		Model:    "antigravity/gemini-4-carbon-high",
		Messages: []OpenAIChatMessage{{Role: "user", Content: promptRaw}},
	}
	outDisabled, hitsDisabled := InjectVectorContextIntoMessages(chatReq)
	if hitsDisabled != 0 || len(outDisabled.Messages) != 1 {
		t.Fatalf("expected 0 RAG injections when VectorDB is disabled by default, got hits=%d msgs=%d", hitsDisabled, len(outDisabled.Messages))
	}

	// Enabling explicitly via SaveVectorDBConfig must immediately activate fast-path flags & RAG injection
	if err := SaveVectorDBConfig(VectorDBConfig{
		Enabled:          true,
		AutoInjectRAG:    true,
		AutoLearnPrompts: true,
		TopK:             3,
		MinSimilarity:    0.20,
	}); err != nil {
		t.Fatalf("SaveVectorDBConfig failed: %v", err)
	}
	if !IsVectorRAGEnabledFast() || !IsVectorAutoLearnEnabledFast() {
		t.Fatalf("expected atomic fast-path flags to turn true immediately after SaveVectorDBConfig")
	}
	outEnabled, hitsEnabled := ApplyVectorMemoryRAG(chatReq)
	if hitsEnabled == 0 || len(outEnabled.Messages) < 2 {
		t.Fatalf("expected RAG injection when explicitly enabled, got hits=%d msgs=%d", hitsEnabled, len(outEnabled.Messages))
	}

	// Turn back off and verify immediate <5ns fast-off transition
	_ = SaveVectorDBConfig(defaultVectorDBConfig())
	if IsVectorDBEnabledFast() || IsVectorRAGEnabledFast() || IsVectorAutoLearnEnabledFast() {
		t.Fatalf("expected atomic fast-path flags to turn false after disabling")
	}

	// 2. Verify StartAutomationScheduler uses sync.Once across multiple NewServer() calls and fast-skips when 0 enabled
	swarmHistoryMu.Lock()
	scheduledAutomations = nil
	vaultLoadedFromDisk = false
	vaultLoadedAtomic.Store(false)
	enabledAutomationsCount.Store(0)
	swarmHistoryMu.Unlock()

	for i := 0; i < 15; i++ {
		_ = NewServer(GatewayConfig{}, NewResolver(), &hotpathCaptureUpstream{})
	}
	if runs := automationSchedulerGoroRuns.Load(); runs != 1 {
		t.Fatalf("expected StartAutomationScheduler sync.Once to spawn exactly 1 background ticker goroutine, got %d", runs)
	}
	if cnt := EnabledAutomationsCountFast(); cnt != 0 {
		t.Fatalf("expected 0 enabled automations by default, got %d", cnt)
	}
	// Tick should fast-return cleanly with 0 enabled automations
	runAutomationSchedulerTick(time.Now())
}

func BenchmarkHotpath_SSENoGzipAllocs(b *testing.B) {
	sseChunk := []byte("data: {\"choices\":[{\"delta\":{\"content\":\"tok\"}}]}\n\n")
	sseHandler := withGzip(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(sseChunk)
	}))

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	req.Header.Set("Accept", "application/json")

	rw := &discardResponseWriter{hdr: http.Header{"Content-Type": []string{"text/event-stream"}}}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sseHandler.ServeHTTP(rw, req)
	}
}

type discardResponseWriter struct {
	hdr http.Header
}

func (d *discardResponseWriter) Header() http.Header         { return d.hdr }
func (d *discardResponseWriter) Write(p []byte) (int, error) { return len(p), nil }
func (d *discardResponseWriter) WriteHeader(statusCode int)  {}
func (d *discardResponseWriter) Flush()                      {}

func BenchmarkHotpath_CacheGetConcurrentRLock(b *testing.B) {
	cache := NewResponseCache(1024, 10*time.Minute)
	hitReq := OpenAIChatRequest{
		Model:    "antigravity/gemini-3-flash",
		Messages: []OpenAIChatMessage{{Role: "user", Content: json.RawMessage(`"benchmark concurrent rlock hit"`)}},
	}
	missReq := OpenAIChatRequest{
		Model:    "antigravity/gemini-3-flash",
		Messages: []OpenAIChatMessage{{Role: "user", Content: json.RawMessage(`"benchmark concurrent rlock miss"`)}},
	}
	cache.Put(hitReq, OpenAIChatResponse{
		ID:      "bench-hit",
		Choices: []OpenAIChatChoice{{Message: &OpenAIOutputMessage{Role: "assistant", Content: "ok"}}},
		Usage:   &OpenAIUsage{TotalTokens: 20},
	})

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			if i&1 == 0 {
				_, _ = cache.Get(hitReq)
			} else {
				_, _ = cache.Get(missReq)
			}
			i++
		}
	})
}

func BenchmarkHotpath_VectorDBDisabledFastPath(b *testing.B) {
	storeVectorFastState(defaultVectorDBConfig())
	req := OpenAIChatRequest{
		Model:    "antigravity/gemini-4-carbon-high",
		Messages: []OpenAIChatMessage{{Role: "user", Content: json.RawMessage(`"write a fast HTTP/2 router in Go"`)}},
	}
	logEntry := GatewayLogEntry{
		StatusCode:   200,
		PromptText:   "[USER]: write a fast HTTP/2 router in Go",
		ResponseText: "Here is the implementation...",
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ApplyVectorMemoryRAG(req)
		AutoLearnFromPromptLog(logEntry)
	}
	_ = fmt.Sprint(req.Model)
}
