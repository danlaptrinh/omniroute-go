package gateway

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type streamCacheMockUpstream struct {
	calls atomic.Int64
}

func (u *streamCacheMockUpstream) HandleChatCompletion(
	ctx context.Context,
	w http.ResponseWriter,
	req OpenAIChatRequest,
	resolved ResolvedModel,
	incomingAuth string,
) error {
	u.calls.Add(1)

	resp := OpenAIChatResponse{
		ID:      "chatcmpl-mock-upstream",
		Object:  "chat.completion",
		Created: 1741000000,
		Model:   req.Model,
		Choices: []OpenAIChatChoice{
			{
				Index: 0,
				Message: &OpenAIOutputMessage{
					Role:             "assistant",
					Content:          "Sub-millisecond cached response!",
					ReasoningContent: "Checked invariants.",
				},
				FinishReason: "stop",
			},
		},
		Usage: &OpenAIUsage{
			PromptTokens:     18,
			CompletionTokens: 12,
			TotalTokens:      30,
		},
	}

	// Simulate translator.go populating ResponseCache for both stream:true and stream:false completions.
	cacheKeyReq := req
	cacheKeyReq.Stream = false
	GetGlobalCache().Put(cacheKeyReq, resp)

	w.Header().Set("X-Omni-Gateway-Tokens", "30")
	if req.Stream {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "data: {\"id\":\"chatcmpl-mock-upstream\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"Sub-millisecond cached response!\"}}]}\n\ndata: [DONE]\n\n")
		if fl, ok := w.(http.Flusher); ok {
			fl.Flush()
		}
		return nil
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	return json.NewEncoder(w).Encode(resp)
}

type nonCachingAnthropicUpstream struct {
	calls     atomic.Int64
	errorCode int
	errorBody string
}

func (u *nonCachingAnthropicUpstream) HandleChatCompletion(
	ctx context.Context,
	w http.ResponseWriter,
	req OpenAIChatRequest,
	resolved ResolvedModel,
	incomingAuth string,
) error {
	u.calls.Add(1)
	if u.errorCode >= 400 {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Retry-After", "3")
		w.WriteHeader(u.errorCode)
		_, _ = io.WriteString(w, u.errorBody)
		return nil
	}

	if req.Stream {
		w.Header().Set("X-Omni-Gateway-Tokens", "23")
		w.Header().Set("X-Omni-Gateway-Account", "ultra-1@gmail.com")
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		// Intentionally split SSE chunks across partial writes to verify real-time line framing.
		chunks := []string{
			": keepalive\n\n",
			"data: {\"id\":\"chatcmpl-claude-stream\",\"object\":\"chat.completion.chunk\",\"model\":\"" + req.Model + "\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"reasoning_content\":\"Step 1. \"}}]}\n\n",
			"data: {\"id\":\"chatcmpl-claude-stream\",\"object\":\"chat.completion.chunk\",\"model\":\"" + req.Model + "\",\"choices\":[{\"index\":0,\"delta\":{\"reasoning_content\":\"Step 2.\"}}]}\n",
			"\ndata: {\"id\":\"chatcmpl-claude-stream\",\"object\":\"chat.completion.chunk\",\"model\":\"" + req.Model + "\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Native Anthropic \"}}]}\n\n",
			"data: {\"id\":\"chatcmpl-claude-stream\",\"object\":\"chat.completion.chunk\",\"model\":\"" + req.Model + "\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"SSE on MISS!\"}}]}\n\n",
			"data: {\"id\":\"chatcmpl-claude-stream\",\"object\":\"chat.completion.chunk\",\"model\":\"" + req.Model + "\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":14,\"completion_tokens\":9,\"total_tokens\":23}}\n\n",
			"data: [DONE]\n\n",
		}
		for _, part := range chunks {
			if _, err := io.WriteString(w, part); err != nil {
				return err
			}
			if fl, ok := w.(http.Flusher); ok {
				fl.Flush()
			}
		}
		return nil
	}

	resp := OpenAIChatResponse{
		ID:      "chatcmpl-claude-provider",
		Object:  "chat.completion",
		Created: 1741000100,
		Model:   req.Model,
		Choices: []OpenAIChatChoice{
			{
				Index: 0,
				Message: &OpenAIOutputMessage{
					Role:             "assistant",
					Content:          "Anthropic wire format verified!",
					ReasoningContent: "Thinking block verified.",
				},
				FinishReason: "stop",
			},
		},
		Usage: &OpenAIUsage{
			PromptTokens:     14,
			CompletionTokens: 9,
			TotalTokens:      23,
		},
	}
	w.Header().Set("X-Omni-Gateway-Tokens", "23")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	return json.NewEncoder(w).Encode(resp)
}

func TestStreamCache_ComputeKeyDeterminismAndToolsBypass(t *testing.T) {
	tmpCfg := filepath.Join(t.TempDir(), "gateway_config.json")
	t.Setenv("GATEWAY_CONFIG_FILE", tmpCfg)
	GetGlobalCache().Clear()
	defer func() {
		FlushPendingConfigSave()
		GetGlobalCache().Clear()
	}()

	zeroTemp := 0.0
	lowTemp := 0.25
	highTemp := 0.7

	reqBase := OpenAIChatRequest{
		Model: "antigravity/gemini-4-carbon-high",
		Messages: []OpenAIChatMessage{
			{Role: "user", Content: json.RawMessage(`"deterministic key check"`)},
		},
		Stream: false,
	}
	reqStreamSpaced := OpenAIChatRequest{
		Model: "  antigravity/gemini-4-carbon-high \t\n",
		Messages: []OpenAIChatMessage{
			{Role: "user", Content: json.RawMessage(` "deterministic key check" `)},
		},
		Temperature: &zeroTemp,
		Stream:      true,
	}

	k1 := ComputeCacheKey(reqBase)
	k2 := ComputeCacheKey(reqStreamSpaced)
	if k1 != k2 {
		t.Fatalf("expected identical SHA-256 key across stream/model-trim/nil-vs-0-temp: %q != %q", k1, k2)
	}

	reqLow := reqBase
	reqLow.Temperature = &lowTemp
	if !IsCacheable(reqLow) {
		t.Fatalf("expected temperature=0.25 (<=0.3) to be cacheable")
	}
	if ComputeCacheKey(reqLow) == k1 {
		t.Fatalf("expected non-zero temperature=0.25 to produce distinct deterministic key from temp=0")
	}

	reqHigh := reqBase
	reqHigh.Temperature = &highTemp
	if IsCacheable(reqHigh) {
		t.Fatalf("expected temperature=0.7 (>0.3) to bypass cache")
	}

	// Verify empty tools:[] hits cache while non-empty tools:[{...}] bypasses cache
	keyObj, err := CreateAPIKey("tools-check", true, true, true, false)
	if err != nil {
		t.Fatalf("CreateAPIKey failed: %v", err)
	}
	up := &streamCacheMockUpstream{}
	srv := NewServer(GatewayConfig{AgyOAuthToken: "ya29.test"}, NewResolver(), up)
	handler := srv.Handler()

	// 1. Prime cache with empty tools: []
	primeBody := `{"model":"antigravity/gemini-4-carbon-high","messages":[{"role":"user","content":"toolless ide call"}],"tools":[],"stream":false}`
	rec1 := httptest.NewRecorder()
	req1 := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(primeBody))
	req1.Header.Set("Authorization", "Bearer "+keyObj.Key)
	handler.ServeHTTP(rec1, req1)
	if rec1.Header().Get("X-Omni-Gateway-Cache") != "MISS" {
		t.Fatalf("expected initial MISS, got %q", rec1.Header().Get("X-Omni-Gateway-Cache"))
	}

	// 2. Hit with omitted tools and stream:true
	hitBody := `{"model":"antigravity/gemini-4-carbon-high","messages":[{"role":"user","content":"toolless ide call"}],"stream":true}`
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(hitBody))
	req2.Header.Set("Authorization", "Bearer "+keyObj.Key)
	handler.ServeHTTP(rec2, req2)
	if rec2.Header().Get("X-Omni-Gateway-Cache") != "HIT" {
		t.Fatalf("expected HIT when tools omitted vs tools:[], got %q", rec2.Header().Get("X-Omni-Gateway-Cache"))
	}

	// 3. Bypass when tools is non-empty
	withToolsBody := `{"model":"antigravity/gemini-4-carbon-high","messages":[{"role":"user","content":"toolless ide call"}],"tools":[{"type":"function","function":{"name":"exec"}}],"stream":true}`
	rec3 := httptest.NewRecorder()
	req3 := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(withToolsBody))
	req3.Header.Set("Authorization", "Bearer "+keyObj.Key)
	handler.ServeHTTP(rec3, req3)
	if rec3.Header().Get("X-Omni-Gateway-Cache") != "MISS" {
		t.Fatalf("expected MISS when tools array is non-empty, got %q", rec3.Header().Get("X-Omni-Gateway-Cache"))
	}
	if up.calls.Load() != 2 {
		t.Fatalf("expected upstream calls == 2 (initial MISS + tool-call MISS), got %d", up.calls.Load())
	}
}

func TestStreamCache_ChatCompletionsCrossHitSymmetryAndIncludeUsage(t *testing.T) {
	tmpCfg := filepath.Join(t.TempDir(), "gateway_config.json")
	t.Setenv("GATEWAY_CONFIG_FILE", tmpCfg)
	GetGlobalCache().Clear()
	ClearLogs()
	defer func() {
		FlushPendingConfigSave()
		GetGlobalCache().Clear()
		ClearLogs()
	}()

	adminKey, err := CreateAPIKey("stream-admin", true, true, true, false)
	if err != nil {
		t.Fatalf("CreateAPIKey failed: %v", err)
	}

	up := &streamCacheMockUpstream{}
	srv := NewServer(GatewayConfig{AgyOAuthToken: "ya29.test"}, NewResolver(), up)
	handler := srv.Handler()

	// Part A: stream:false MISS -> stream:true HIT -> stream:true + include_usage:true HIT
	nonStreamBody := `{"model":"antigravity/gemini-4-carbon-high","messages":[{"role":"user","content":"symmetry A"}],"stream":false}`
	r1 := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(nonStreamBody))
	r1.Header.Set("Authorization", "Bearer "+adminKey.Key)
	w1 := httptest.NewRecorder()
	handler.ServeHTTP(w1, r1)
	if w1.Code != http.StatusOK || w1.Header().Get("X-Omni-Gateway-Cache") != "MISS" {
		t.Fatalf("Part A step 1 failed: code=%d cache=%q", w1.Code, w1.Header().Get("X-Omni-Gateway-Cache"))
	}

	streamBody := `{"model":"  antigravity/gemini-4-carbon-high ","messages":[{"role":"user","content":"symmetry A"}],"stream":true}`
	r2 := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(streamBody))
	r2.Header.Set("Authorization", "Bearer "+adminKey.Key)
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, r2)
	if w2.Code != http.StatusOK {
		t.Fatalf("Part A step 2 status=%d", w2.Code)
	}
	if got := w2.Header().Get("X-Omni-Gateway-Cache"); got != "HIT" {
		t.Fatalf("expected X-Omni-Gateway-Cache=HIT, got %q", got)
	}
	if got := w2.Header().Get("X-Omni-Gateway-Account"); got != "in-memory-cache" {
		t.Fatalf("expected X-Omni-Gateway-Account=in-memory-cache, got %q", got)
	}
	if got := w2.Header().Get("X-Omni-Gateway-Upstream-Model-Version"); got != "cache-hit" {
		t.Fatalf("expected X-Omni-Gateway-Upstream-Model-Version=cache-hit, got %q", got)
	}
	if got := w2.Header().Get("X-Accel-Buffering"); got != "no" {
		t.Fatalf("expected X-Accel-Buffering=no, got %q", got)
	}
	sseStandard := w2.Body.String()
	if !strings.Contains(sseStandard, `"content":"Sub-millisecond cached response!"`) || !strings.HasSuffix(sseStandard, "data: [DONE]\n\n") {
		t.Fatalf("unexpected standard SSE wire payload:\n%s", sseStandard)
	}
	if strings.Contains(sseStandard, `"total_tokens":30`) {
		t.Fatalf("standard SSE without include_usage should not emit usage chunk:\n%s", sseStandard)
	}

	// StreamOptions.IncludeUsage == true
	usageStreamBody := `{"model":"antigravity/gemini-4-carbon-high","messages":[{"role":"user","content":"symmetry A"}],"stream":true,"stream_options":{"include_usage":true}}`
	r3 := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(usageStreamBody))
	r3.Header.Set("Authorization", "Bearer "+adminKey.Key)
	w3 := httptest.NewRecorder()
	handler.ServeHTTP(w3, r3)
	if w3.Header().Get("X-Omni-Gateway-Cache") != "HIT" {
		t.Fatalf("expected HIT with stream_options.include_usage=true, got %q", w3.Header().Get("X-Omni-Gateway-Cache"))
	}
	sseWithUsage := w3.Body.String()
	if !strings.Contains(sseWithUsage, `"total_tokens":30`) || !strings.Contains(sseWithUsage, `"choices":[]`) || !strings.HasSuffix(sseWithUsage, "data: [DONE]\n\n") {
		t.Fatalf("expected OpenAI include_usage chunk before [DONE], got:\n%s", sseWithUsage)
	}

	// Part B: stream:true MISS -> stream:false HIT
	streamFirstBody := `{"model":"antigravity/gemini-4-carbon-high","messages":[{"role":"user","content":"symmetry B"}],"stream":true}`
	r4 := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(streamFirstBody))
	r4.Header.Set("Authorization", "Bearer "+adminKey.Key)
	w4 := httptest.NewRecorder()
	handler.ServeHTTP(w4, r4)
	if w4.Header().Get("X-Omni-Gateway-Cache") != "MISS" {
		t.Fatalf("expected MISS on streamFirstBody, got %q", w4.Header().Get("X-Omni-Gateway-Cache"))
	}

	nonStreamHitBody := `{"model":"antigravity/gemini-4-carbon-high","messages":[{"role":"user","content":"symmetry B"}],"stream":false}`
	r5 := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(nonStreamHitBody))
	r5.Header.Set("Authorization", "Bearer "+adminKey.Key)
	w5 := httptest.NewRecorder()
	handler.ServeHTTP(w5, r5)
	if w5.Header().Get("X-Omni-Gateway-Cache") != "HIT" || w5.Header().Get("X-Omni-Gateway-Account") != "in-memory-cache" {
		t.Fatalf("expected stream:true -> stream:false cross-hit HIT, got cache=%q account=%q", w5.Header().Get("X-Omni-Gateway-Cache"), w5.Header().Get("X-Omni-Gateway-Account"))
	}
	var parsedHit OpenAIChatResponse
	if err := json.Unmarshal(w5.Body.Bytes(), &parsedHit); err != nil {
		t.Fatalf("failed to decode pre-serialized JSON hit: %v", err)
	}
	if len(parsedHit.Choices) != 1 || parsedHit.Choices[0].Message == nil || parsedHit.Choices[0].Message.Content != "Sub-millisecond cached response!" {
		t.Fatalf("unexpected JSON hit choices: %+v", parsedHit.Choices)
	}

	// Verify /ui/api/logs exposes upstream_model="cache-hit" and account="in-memory-cache"
	logsReq := httptest.NewRequest(http.MethodGet, "/ui/api/logs?limit=10", nil)
	logsReq.Header.Set("Authorization", "Bearer "+adminKey.Key)
	logsRec := httptest.NewRecorder()
	handler.ServeHTTP(logsRec, logsReq)
	if logsRec.Code != http.StatusOK {
		t.Fatalf("/ui/api/logs returned %d: %s", logsRec.Code, logsRec.Body.String())
	}
	var logsPayload struct {
		Data []GatewayLogEntry `json:"data"`
	}
	if err := json.Unmarshal(logsRec.Body.Bytes(), &logsPayload); err != nil {
		t.Fatalf("failed to decode /ui/api/logs: %v", err)
	}
	foundCacheLog := false
	for _, item := range logsPayload.Data {
		if item.Account == "in-memory-cache" && item.UpstreamModel == "cache-hit" {
			foundCacheLog = true
			break
		}
	}
	if !foundCacheLog {
		t.Fatalf("expected /ui/api/logs to contain Account=in-memory-cache & UpstreamModel=cache-hit, got %+v", logsPayload.Data)
	}

	// Verify UI HTML renders ⚡ CACHE HIT badge and Ultra/Pro tier badge formatter
	uiReq := httptest.NewRequest(http.MethodGet, "/ui", nil)
	uiRec := httptest.NewRecorder()
	handler.ServeHTTP(uiRec, uiReq)
	uiHTML := uiRec.Body.String()
	if !strings.Contains(uiHTML, "⚡ CACHE HIT") || !strings.Contains(uiHTML, "formatAccountTierBadge") {
		t.Fatalf("expected /ui HTML to include ⚡ CACHE HIT and formatAccountTierBadge")
	}
}

func TestStreamCache_AnthropicMessagesNonStreamAndSSEHit(t *testing.T) {
	tmpCfg := filepath.Join(t.TempDir(), "gateway_config.json")
	t.Setenv("GATEWAY_CONFIG_FILE", tmpCfg)
	GetGlobalCache().Clear()
	defer func() {
		FlushPendingConfigSave()
		GetGlobalCache().Clear()
	}()

	apiKey, err := CreateAPIKey("anthropic-cache-emp", false, true, true, false)
	if err != nil {
		t.Fatalf("CreateAPIKey failed: %v", err)
	}

	// Use nonCachingAnthropicUpstream to ALSO verify server.go's fallback Put on /v1/messages non-stream completion!
	up := &nonCachingAnthropicUpstream{}
	srv := NewServer(GatewayConfig{}, NewResolver(), up)
	handler := srv.Handler()

	// 1. Initial non-stream /v1/messages MISS (with system blocks array) MUST return native Anthropic JSON!
	missBody := `{"model":"claude-code/claude-sonnet-4-6","max_tokens":256,"system":[{"type":"text","text":"Be concise."}],"messages":[{"role":"user","content":"verify anthropic cache"}],"stream":false}`
	req1 := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(missBody))
	req1.Header.Set("x-api-key", apiKey.Key)
	req1.Header.Set("Content-Type", "application/json")
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusOK || rec1.Header().Get("X-Omni-Gateway-Cache") != "MISS" {
		t.Fatalf("expected initial /v1/messages MISS, got code=%d cache=%q", rec1.Code, rec1.Header().Get("X-Omni-Gateway-Cache"))
	}
	if strings.Contains(rec1.Body.String(), `"object":"chat.completion"`) || strings.Contains(rec1.Body.String(), `"choices"`) {
		t.Fatalf("/v1/messages non-stream MISS leaked raw OpenAI JSON instead of Anthropic JSON:\n%s", rec1.Body.String())
	}
	var missAnthJSON struct {
		ID         string `json:"id"`
		Type       string `json:"type"`
		Role       string `json:"role"`
		StopReason string `json:"stop_reason"`
		Content    []struct {
			Type     string `json:"type"`
			Text     string `json:"text"`
			Thinking string `json:"thinking"`
		} `json:"content"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(rec1.Body.Bytes(), &missAnthJSON); err != nil {
		t.Fatalf("invalid Anthropic JSON response on non-stream MISS: %v\nbody=%s", err, rec1.Body.String())
	}
	if !strings.HasPrefix(missAnthJSON.ID, "msg_") || missAnthJSON.Type != "message" || missAnthJSON.Role != "assistant" || missAnthJSON.StopReason != "end_turn" {
		t.Fatalf("unexpected Anthropic JSON envelope on MISS: %+v", missAnthJSON)
	}
	if len(missAnthJSON.Content) != 2 || missAnthJSON.Content[0].Type != "thinking" || missAnthJSON.Content[0].Thinking != "Thinking block verified." || missAnthJSON.Content[1].Text != "Anthropic wire format verified!" {
		t.Fatalf("unexpected Anthropic content blocks on MISS: %+v", missAnthJSON.Content)
	}
	if missAnthJSON.Usage.InputTokens != 14 || missAnthJSON.Usage.OutputTokens != 9 {
		t.Fatalf("unexpected Anthropic usage on MISS: %+v", missAnthJSON.Usage)
	}

	// 2. Non-stream /v1/messages HIT (matching system string equivalent!)
	hitJSONBody := `{"model":"claude-code/claude-sonnet-4-6","max_tokens":256,"system":"Be concise.","messages":[{"role":"user","content":"verify anthropic cache"}],"stream":false}`
	req2 := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(hitJSONBody))
	req2.Header.Set("x-api-key", apiKey.Key)
	req2.Header.Set("Content-Type", "application/json")
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("/v1/messages non-stream HIT failed: %d", rec2.Code)
	}
	if rec2.Header().Get("X-Omni-Gateway-Cache") != "HIT" || rec2.Header().Get("X-Omni-Gateway-Account") != "in-memory-cache" {
		t.Fatalf("expected /v1/messages non-stream HIT + in-memory-cache, got cache=%q acc=%q", rec2.Header().Get("X-Omni-Gateway-Cache"), rec2.Header().Get("X-Omni-Gateway-Account"))
	}
	var anthJSON struct {
		ID         string `json:"id"`
		Type       string `json:"type"`
		Role       string `json:"role"`
		StopReason string `json:"stop_reason"`
		Content    []struct {
			Type     string `json:"type"`
			Text     string `json:"text"`
			Thinking string `json:"thinking"`
		} `json:"content"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(rec2.Body.Bytes(), &anthJSON); err != nil {
		t.Fatalf("invalid Anthropic JSON response on HIT: %v\nbody=%s", err, rec2.Body.String())
	}
	if anthJSON.Type != "message" || anthJSON.Role != "assistant" || anthJSON.StopReason != "end_turn" {
		t.Fatalf("unexpected Anthropic JSON envelope: %+v", anthJSON)
	}
	if len(anthJSON.Content) != 2 || anthJSON.Content[0].Type != "thinking" || anthJSON.Content[1].Text != "Anthropic wire format verified!" {
		t.Fatalf("unexpected Anthropic content blocks: %+v", anthJSON.Content)
	}

	// 3. Streaming /v1/messages SSE HIT
	hitSSEBody := `{"model":"claude-code/claude-sonnet-4-6","max_tokens":256,"system":"Be concise.","messages":[{"role":"user","content":"verify anthropic cache"}],"stream":true}`
	req3 := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(hitSSEBody))
	req3.Header.Set("x-api-key", apiKey.Key)
	req3.Header.Set("Content-Type", "application/json")
	rec3 := httptest.NewRecorder()
	handler.ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusOK {
		t.Fatalf("/v1/messages SSE HIT failed: %d", rec3.Code)
	}
	if rec3.Header().Get("X-Omni-Gateway-Cache") != "HIT" || rec3.Header().Get("X-Omni-Gateway-Account") != "in-memory-cache" {
		t.Fatalf("expected /v1/messages SSE HIT + in-memory-cache, got cache=%q acc=%q", rec3.Header().Get("X-Omni-Gateway-Cache"), rec3.Header().Get("X-Omni-Gateway-Account"))
	}
	if !strings.HasPrefix(rec3.Header().Get("Content-Type"), "text/event-stream") {
		t.Fatalf("expected text/event-stream, got %q", rec3.Header().Get("Content-Type"))
	}
	sseWire := rec3.Body.String()
	requiredEvents := []string{
		"event: message_start\n",
		"event: content_block_start\n",
		"event: content_block_delta\n",
		"event: content_block_stop\n",
		"event: message_delta\n",
		"event: message_stop\n",
	}
	lastIdx := -1
	for _, ev := range requiredEvents {
		idx := strings.Index(sseWire, ev)
		if idx < 0 {
			t.Fatalf("Anthropic SSE missing %q in:\n%s", ev, sseWire)
		}
		if idx <= lastIdx && ev != "event: content_block_start\n" {
			t.Fatalf("Anthropic SSE event %q out of order in:\n%s", ev, sseWire)
		}
		lastIdx = idx
	}
	if !strings.Contains(sseWire, `"text":"Anthropic wire format verified!"`) {
		t.Fatalf("Anthropic SSE missing delta text in:\n%s", sseWire)
	}
	if up.calls.Load() != 1 {
		t.Fatalf("expected upstream called exactly once across MISS + JSON HIT + SSE HIT, got %d", up.calls.Load())
	}
}

func TestStreamCache_AnthropicMessagesStreamingMissAdapterErrorPassthroughAndProxyAPI(t *testing.T) {
	tmpCfg := filepath.Join(t.TempDir(), "gateway_config.json")
	t.Setenv("GATEWAY_CONFIG_FILE", tmpCfg)
	GetGlobalCache().Clear()
	defer func() {
		FlushPendingConfigSave()
		GetGlobalCache().Clear()
	}()

	adminKey, err := CreateAPIKey("anthropic-stream-miss-admin", true, true, true, false)
	if err != nil {
		t.Fatalf("CreateAPIKey failed: %v", err)
	}

	up := &nonCachingAnthropicUpstream{}
	srv := NewServer(GatewayConfig{}, NewResolver(), up)
	handler := srv.Handler()

	// 1. /v1/messages stream:true MISS -> real-time Anthropic SSE adapter
	streamMissBody := `{"model":"claude-code/claude-sonnet-4-6","max_tokens":256,"temperature":0,"messages":[{"role":"user","content":"stream miss anthropic sse"}],"stream":true}`
	r1 := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(streamMissBody))
	r1.Header.Set("x-api-key", adminKey.Key)
	r1.Header.Set("Content-Type", "application/json")
	w1 := httptest.NewRecorder()
	handler.ServeHTTP(w1, r1)

	if w1.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on /v1/messages stream:true MISS, got %d: %s", w1.Code, w1.Body.String())
	}
	if w1.Header().Get("X-Omni-Gateway-Cache") != "MISS" {
		t.Fatalf("expected X-Omni-Gateway-Cache=MISS, got %q", w1.Header().Get("X-Omni-Gateway-Cache"))
	}
	if w1.Header().Get("X-Accel-Buffering") != "no" {
		t.Fatalf("expected X-Accel-Buffering=no on /v1/messages stream:true MISS, got %q", w1.Header().Get("X-Accel-Buffering"))
	}
	if !strings.HasPrefix(w1.Header().Get("Content-Type"), "text/event-stream") {
		t.Fatalf("expected Content-Type text/event-stream, got %q", w1.Header().Get("Content-Type"))
	}

	missSSE := w1.Body.String()
	if strings.Contains(missSSE, "chat.completion.chunk") || strings.Contains(missSSE, "[DONE]") {
		t.Fatalf("/v1/messages stream:true MISS leaked raw OpenAI SSE wire bytes:\n%s", missSSE)
	}
	for _, mustHave := range []string{
		"event: message_start\n",
		"event: content_block_start\n",
		`"type":"thinking_delta"`,
		`"thinking":"Step 1. "`,
		`"thinking":"Step 2."`,
		`"type":"text_delta"`,
		`"text":"Native Anthropic "`,
		`"text":"SSE on MISS!"`,
		"event: content_block_stop\n",
		"event: message_delta\n",
		`"stop_reason":"end_turn"`,
		`"output_tokens":9`,
		"event: message_stop\n",
	} {
		if !strings.Contains(missSSE, mustHave) {
			t.Fatalf("/v1/messages stream:true MISS missing %q in:\n%s", mustHave, missSSE)
		}
	}

	// 2. Subsequent stream:true HIT and stream:false HIT must hit GetGlobalCache() with 0 extra upstream calls!
	r2 := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(streamMissBody))
	r2.Header.Set("x-api-key", adminKey.Key)
	r2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, r2)
	if w2.Code != http.StatusOK || w2.Header().Get("X-Omni-Gateway-Cache") != "HIT" {
		t.Fatalf("expected stream:true HIT after stream:true MISS, got code=%d cache=%q", w2.Code, w2.Header().Get("X-Omni-Gateway-Cache"))
	}
	if !strings.Contains(w2.Body.String(), `"text":"Native Anthropic SSE on MISS!"`) || !strings.Contains(w2.Body.String(), "event: message_stop\n") {
		t.Fatalf("unexpected stream:true HIT SSE body:\n%s", w2.Body.String())
	}

	nonStreamHitBody := `{"model":"claude-code/claude-sonnet-4-6","max_tokens":256,"temperature":0,"messages":[{"role":"user","content":"stream miss anthropic sse"}],"stream":false}`
	r3 := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(nonStreamHitBody))
	r3.Header.Set("x-api-key", adminKey.Key)
	r3.Header.Set("Content-Type", "application/json")
	w3 := httptest.NewRecorder()
	handler.ServeHTTP(w3, r3)
	if w3.Code != http.StatusOK || w3.Header().Get("X-Omni-Gateway-Cache") != "HIT" {
		t.Fatalf("expected stream:false HIT after stream:true MISS, got code=%d cache=%q", w3.Code, w3.Header().Get("X-Omni-Gateway-Cache"))
	}
	if !strings.Contains(w3.Body.String(), `"type":"message"`) || !strings.Contains(w3.Body.String(), `"Native Anthropic SSE on MISS!"`) {
		t.Fatalf("unexpected stream:false HIT JSON body:\n%s", w3.Body.String())
	}
	if up.calls.Load() != 1 {
		t.Fatalf("expected upstream calls == 1 across stream MISS -> stream HIT -> JSON HIT, got %d", up.calls.Load())
	}

	// 3. Non-200 error passthrough on BOTH stream:true and stream:false
	up.errorCode = http.StatusTooManyRequests
	up.errorBody = `{"error":{"message":"upstream rate limit","type":"rate_limit_error"}}`
	for _, streamFlag := range []string{"true", "false"} {
		errPayload := `{"model":"claude-code/claude-sonnet-4-6","max_tokens":128,"temperature":0,"messages":[{"role":"user","content":"unseen prompt ` + streamFlag + `"}],"stream":` + streamFlag + `}`
		re := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(errPayload))
		re.Header.Set("x-api-key", adminKey.Key)
		re.Header.Set("Content-Type", "application/json")
		we := httptest.NewRecorder()
		handler.ServeHTTP(we, re)
		if we.Code != http.StatusTooManyRequests {
			t.Fatalf("expected 429 passthrough for stream=%s, got %d", streamFlag, we.Code)
		}
		if we.Header().Get("Retry-After") != "3" {
			t.Fatalf("expected Retry-After header preserved for stream=%s, got %q", streamFlag, we.Header().Get("Retry-After"))
		}
		if we.Body.String() != up.errorBody {
			t.Fatalf("expected exact error body passthrough for stream=%s, got %q", streamFlag, we.Body.String())
		}
	}

	// 4. Per-account Proxy API (/v1/providers/account-proxy + masked /v1/providers + UI modal)
	if _, err := UpsertConnectedAccount(ConnectedAccount{
		ID:          "acc-ultra-1",
		Provider:    ProviderAntigravity,
		Email:       "ultra-proxy@gmail.com",
		AccessToken: "ya29.ultra",
	}); err != nil {
		t.Fatalf("UpsertConnectedAccount failed: %v", err)
	}

	// Reject unsupported scheme
	badProxyReq := httptest.NewRequest(http.MethodPost, "/v1/providers/account-proxy", strings.NewReader(`{"email":"ultra-proxy@gmail.com","proxy_url":"ftp://1.2.3.4:21"}`))
	badProxyReq.Header.Set("Authorization", "Bearer "+adminKey.Key)
	badProxyRec := httptest.NewRecorder()
	handler.ServeHTTP(badProxyRec, badProxyReq)
	if badProxyRec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on unsupported proxy scheme, got %d", badProxyRec.Code)
	}

	// Save valid SOCKS5H proxy with credentials and verify masked password in response + GET /v1/providers
	setProxyReq := httptest.NewRequest(http.MethodPost, "/v1/providers/account-proxy", strings.NewReader(`{"email":"ultra-proxy@gmail.com","proxy_url":"socks5h://omniuser:supersecret@198.51.100.25:1080"}`))
	setProxyReq.Header.Set("Authorization", "Bearer "+adminKey.Key)
	setProxyRec := httptest.NewRecorder()
	handler.ServeHTTP(setProxyRec, setProxyReq)
	if setProxyRec.Code != http.StatusOK {
		t.Fatalf("POST /v1/providers/account-proxy failed (%d): %s", setProxyRec.Code, setProxyRec.Body.String())
	}
	if !strings.Contains(setProxyRec.Body.String(), `"socks5h://omniuser:***@198.51.100.25:1080"`) || strings.Contains(setProxyRec.Body.String(), "supersecret") {
		t.Fatalf("expected masked proxy credentials in response, got %s", setProxyRec.Body.String())
	}

	provReq := httptest.NewRequest(http.MethodGet, "/v1/providers", nil)
	provReq.Header.Set("Authorization", "Bearer "+adminKey.Key)
	provRec := httptest.NewRecorder()
	handler.ServeHTTP(provRec, provReq)
	if provRec.Code != http.StatusOK || !strings.Contains(provRec.Body.String(), `"socks5h://omniuser:***@198.51.100.25:1080"`) {
		t.Fatalf("expected GET /v1/providers to expose masked proxy_url, got %s", provRec.Body.String())
	}

	uiReq := httptest.NewRequest(http.MethodGet, "/ui", nil)
	uiRec := httptest.NewRecorder()
	handler.ServeHTTP(uiRec, uiReq)
	if !strings.Contains(uiRec.Body.String(), "/v1/providers/account-proxy") || !strings.Contains(uiRec.Body.String(), "openAccountProxyModal") {
		t.Fatalf("expected /ui HTML to include per-account proxy modal & /v1/providers/account-proxy")
	}
}

func TestVectorDB_DisabledNeverTouchesDiskOrPostgresAndLazyEnables(t *testing.T) {
	tmpDir := t.TempDir()
	tmpCfg := filepath.Join(tmpDir, "gateway_config.json")
	t.Setenv("GATEWAY_CONFIG_FILE", tmpCfg)
	vectorFastState.Store(0)

	vectorMu.Lock()
	origMemories := vectorMemories
	origLoaded := vectorLoadedLocal
	vectorMemories = nil
	vectorLoadedLocal = false
	vectorMu.Unlock()

	defer func() {
		FlushPendingConfigSave()
		vectorMu.Lock()
		vectorMemories = origMemories
		vectorLoadedLocal = origLoaded
		vectorMu.Unlock()
		vectorFastState.Store(0)
	}()

	vecFile := filepath.Join(tmpDir, "omni_vector_memories.json")

	// 1. Startup, status, admin GET /v1/vector, schema/postgres hooks, and disabled Add/Save must NEVER create omni_vector_memories.json
	adminKey, err := CreateAPIKey("vec-guard-admin", true, true, true, false)
	if err != nil {
		t.Fatalf("CreateAPIKey failed: %v", err)
	}
	srv := NewServer(GatewayConfig{}, NewResolver(), &streamCacheMockUpstream{})
	handler := srv.Handler()

	InitVectorDBSchema(nil)
	LoadVectorMemoriesFromPostgres(nil)
	_ = GetVectorDBStatus()
	saveVectorMemoriesLocalAsync()
	vectorMu.Lock()
	loadLocalVectorMemoriesLocked()
	saveLocalVectorMemoriesLocked()
	vectorMu.Unlock()

	vecReq := httptest.NewRequest(http.MethodGet, "/v1/vector", nil)
	vecReq.Header.Set("Authorization", "Bearer "+adminKey.Key)
	vecRec := httptest.NewRecorder()
	handler.ServeHTTP(vecRec, vecReq)
	if vecRec.Code != http.StatusOK {
		t.Fatalf("GET /v1/vector returned %d", vecRec.Code)
	}

	_, err = AddVectorMemory(
		"Disabled In-RAM Only",
		"architecture",
		"Kiểm tra VectorDB tắt mặc định không ghi file JSON",
		"Khi Enabled=false, tuyệt đối không tạo omni_vector_memories.json trên ổ cứng.",
		"Admin",
		"",
		"manual",
		true,
	)
	if err != nil {
		t.Fatalf("AddVectorMemory while disabled failed: %v", err)
	}
	time.Sleep(40 * time.Millisecond)

	if _, statErr := os.Stat(vecFile); !os.IsNotExist(statErr) {
		t.Fatalf("omni_vector_memories.json MUST NOT be created when VectorDBConfig.Enabled == false, got statErr=%v", statErr)
	}

	// 2. Enabling VectorDB via SetVectorDBConfig lazily initializes and preserves memories cleanly
	if err := SetVectorDBConfig(VectorDBConfig{
		Enabled:          true,
		AutoInjectRAG:    true,
		AutoLearnPrompts: true,
		TopK:             3,
		MinSimilarity:    0.20,
	}); err != nil {
		t.Fatalf("SetVectorDBConfig(Enabled=true) failed: %v", err)
	}

	matches := SearchVectorMemories("VectorDB tắt mặc định không ghi file JSON", 3, 0.20)
	if len(matches) == 0 {
		t.Fatalf("expected lazy initialization on Enabled=true to find memory")
	}
}

type zeroAllocBenchResponseWriter struct {
	hdr  http.Header
	code int
	n    int
}

func (w *zeroAllocBenchResponseWriter) Header() http.Header {
	return w.hdr
}

func (w *zeroAllocBenchResponseWriter) WriteHeader(statusCode int) {
	w.code = statusCode
}

func (w *zeroAllocBenchResponseWriter) Write(p []byte) (int, error) {
	w.n += len(p)
	return len(p), nil
}

func (w *zeroAllocBenchResponseWriter) Flush() {}

func BenchmarkResponseCache_GetEntryPreSerializedSSE(b *testing.B) {
	cache := NewResponseCache(1024, 10*time.Minute)
	req := OpenAIChatRequest{
		Model: "antigravity/gemini-4-carbon-high",
		Messages: []OpenAIChatMessage{
			{Role: "user", Content: json.RawMessage(`"ultra fast pre-serialized sse lookup"`)},
		},
		Stream: true,
	}
	cache.Put(req, OpenAIChatResponse{
		ID:      "chatcmpl-fast",
		Object:  "chat.completion",
		Created: 1741000000,
		Model:   req.Model,
		Choices: []OpenAIChatChoice{{
			Index:        0,
			Message:      &OpenAIOutputMessage{Role: "assistant", Content: "Instant SSE bytes!"},
			FinishReason: "stop",
		}},
		Usage: &OpenAIUsage{PromptTokens: 10, CompletionTokens: 6, TotalTokens: 16},
	})

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		entry, ok := cache.GetEntry(req)
		if !ok || len(entry.SSEPayload(false)) == 0 {
			b.Fatal("expected pre-serialized SSE cache hit")
		}
	}
}

func BenchmarkStreamCacheHit_ChatCompletionsSSE(b *testing.B) {
	tmpCfg := filepath.Join(b.TempDir(), "gateway_config.json")
	b.Setenv("GATEWAY_CONFIG_FILE", tmpCfg)
	GetGlobalCache().Clear()
	defer func() {
		FlushPendingConfigSave()
		GetGlobalCache().Clear()
	}()

	keyObj, err := CreateAPIKey("bench-sse", false, true, true, false)
	if err != nil {
		b.Fatalf("CreateAPIKey failed: %v", err)
	}
	authHdr := "Bearer " + keyObj.Key

	up := &streamCacheMockUpstream{}
	srv := NewServer(GatewayConfig{AgyOAuthToken: "ya29.bench"}, NewResolver(), up)

	body := []byte(`{"model":"antigravity/gemini-4-carbon-high","messages":[{"role":"user","content":"benchmark sub-ms stream cache hit"}],"stream":true}`)

	// Prime cache once
	primeReq := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(string(body)))
	primeReq.Header.Set("Authorization", authHdr)
	primeRec := httptest.NewRecorder()
	srv.handleChatCompletions(primeRec, primeReq)
	if primeRec.Code != http.StatusOK {
		b.Fatalf("prime failed: %d", primeRec.Code)
	}

	rw := &zeroAllocBenchResponseWriter{hdr: make(http.Header, 8)}
	reader := strings.NewReader("")
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", reader)
	req.Header.Set("Authorization", authHdr)
	req.ContentLength = int64(len(body))

	b.ReportAllocs()
	b.ResetTimer()
	start := time.Now()
	for i := 0; i < b.N; i++ {
		for k := range rw.hdr {
			delete(rw.hdr, k)
		}
		rw.code = 0
		rw.n = 0
		reader.Reset(string(body))
		req.Body = io.NopCloser(reader)
		srv.handleChatCompletions(rw, req)
		if rw.code != http.StatusOK || rw.hdr.Get("X-Omni-Gateway-Cache") != "HIT" || rw.n == 0 {
			b.Fatalf("unexpected cache status: code=%d hdr=%q n=%d", rw.code, rw.hdr.Get("X-Omni-Gateway-Cache"), rw.n)
		}
	}
	elapsed := time.Since(start)
	if b.N > 0 {
		avg := elapsed / time.Duration(b.N)
		if avg >= time.Millisecond {
			b.Fatalf("expected < 1ms per stream:true cache HIT, got %v", avg)
		}
	}
}

func BenchmarkStreamCacheHit_AnthropicMessagesSSE(b *testing.B) {
	tmpCfg := filepath.Join(b.TempDir(), "gateway_config.json")
	b.Setenv("GATEWAY_CONFIG_FILE", tmpCfg)
	GetGlobalCache().Clear()
	defer func() {
		FlushPendingConfigSave()
		GetGlobalCache().Clear()
	}()

	keyObj, err := CreateAPIKey("bench-anth-sse", false, true, true, false)
	if err != nil {
		b.Fatalf("CreateAPIKey failed: %v", err)
	}

	up := &nonCachingAnthropicUpstream{}
	srv := NewServer(GatewayConfig{}, NewResolver(), up)

	primeBody := `{"model":"claude-code/claude-sonnet-4-6","max_tokens":128,"messages":[{"role":"user","content":"benchmark anthropic sse"}],"stream":false}`
	primeReq := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(primeBody))
	primeReq.Header.Set("x-api-key", keyObj.Key)
	primeRec := httptest.NewRecorder()
	srv.handleAnthropicMessages(primeRec, primeReq)
	if primeRec.Code != http.StatusOK {
		b.Fatalf("prime failed: %d", primeRec.Code)
	}

	streamBody := `{"model":"claude-code/claude-sonnet-4-6","max_tokens":128,"messages":[{"role":"user","content":"benchmark anthropic sse"}],"stream":true}`
	rw := &zeroAllocBenchResponseWriter{hdr: make(http.Header, 8)}
	reader := strings.NewReader("")
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", reader)
	req.Header.Set("x-api-key", keyObj.Key)
	req.ContentLength = int64(len(streamBody))

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for k := range rw.hdr {
			delete(rw.hdr, k)
		}
		rw.code = 0
		rw.n = 0
		reader.Reset(streamBody)
		req.Body = io.NopCloser(reader)
		srv.handleAnthropicMessages(rw, req)
		if rw.code != http.StatusOK || rw.hdr.Get("X-Omni-Gateway-Cache") != "HIT" || rw.n == 0 {
			b.Fatalf("unexpected cache status: code=%d hdr=%q n=%d", rw.code, rw.hdr.Get("X-Omni-Gateway-Cache"), rw.n)
		}
	}
}
