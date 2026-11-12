package gateway

import (
	"context"
	"encoding/json"
	"errors"
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

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func indexOfString(slice []string, target string) int {
	for i, s := range slice {
		if s == target {
			return i
		}
	}
	return -1
}

func TestOmniEmployeeKeyNeverForwardedToAlibaba(t *testing.T) {
	tmpCfg := filepath.Join(t.TempDir(), "gateway_config.json")
	t.Setenv("GATEWAY_CONFIG_FILE", tmpCfg)
	resetAuthCacheForTest(t)
	ResetCCPAModelCaches()
	GetGlobalCache().Clear()
	t.Cleanup(func() {
		resetAuthCacheForTest(t)
		ResetCCPAModelCaches()
		GetGlobalCache().Clear()
	})

	var outboundHTTPCalls atomic.Int32
	upstream := NewUpstream(GatewayConfig{})
	upstream.client = &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			outboundHTTPCalls.Add(1)
			return nil, fmt.Errorf("unexpected outbound HTTP call to %s with Authorization=%q", r.URL.String(), r.Header.Get("Authorization"))
		}),
	}

	req := OpenAIChatRequest{
		Model: "alibaba/qwen3.8-max",
		Messages: []OpenAIChatMessage{
			{Role: "user", Content: json.RawMessage(`"Check sk-omni leak protection"`)},
		},
		Stream: false,
	}
	resolved := ResolveModel(req.Model, nil)

	// 1. Direct tryTarget on ProviderAlibaba with sk-omni-* must fail immediately as missing account without HTTP call
	err := upstream.tryTarget(context.Background(), httptest.NewRecorder(), req, resolved, "sk-omni-test123456")
	if err == nil || !isMissingAccountErr(err) {
		t.Fatalf("expected missing Alibaba account error for sk-omni-* key, got: %v", err)
	}
	if got := outboundHTTPCalls.Load(); got != 0 {
		t.Fatalf("expected 0 outbound Alibaba HTTP calls, got %d", got)
	}

	// 2. Full HandleChatCompletion with Authorization: Bearer sk-omni-test123456 and no accounts configured
	rec := httptest.NewRecorder()
	err = upstream.HandleChatCompletion(context.Background(), rec, req, resolved, "Bearer sk-omni-test123456")
	if err == nil {
		t.Fatalf("expected error when no accounts are configured, got nil")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected HTTP 401 authentication_required, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := outboundHTTPCalls.Load(); got != 0 {
		t.Fatalf("sk-omni-* employee key leaked to outbound HTTP upstream (%d calls)", got)
	}
}

func TestCCPAFirstChunkTimeoutFailsOverAndCachesDeadModel(t *testing.T) {
	tmpCfg := filepath.Join(t.TempDir(), "gateway_config.json")
	t.Setenv("GATEWAY_CONFIG_FILE", tmpCfg)
	resetAuthCacheForTest(t)
	ResetCCPAModelCaches()
	GetGlobalCache().Clear()

	origFirstChunk := CCPAFirstChunkTimeout
	origIdle := CCPAStreamIdleTimeout
	origMax := CCPACandidateMaxTimeout
	CCPAFirstChunkTimeout = 80 * time.Millisecond
	CCPAStreamIdleTimeout = 250 * time.Millisecond
	CCPACandidateMaxTimeout = 2 * time.Second

	t.Cleanup(func() {
		CCPAFirstChunkTimeout = origFirstChunk
		CCPAStreamIdleTimeout = origIdle
		CCPACandidateMaxTimeout = origMax
		ResetCCPAModelCaches()
		GetGlobalCache().Clear()
		resetAuthCacheForTest(t)
	})

	var mu sync.Mutex
	callsByModel := make(map[string]int)

	mockCCPA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload ccpaRequestPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		mu.Lock()
		callsByModel[payload.Model]++
		mu.Unlock()

		switch payload.Model {
		case "claude-opus-4-6-thinking":
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			if fl, ok := w.(http.Flusher); ok {
				fl.Flush()
			}
			// Stall after 200 OK headers without sending any SSE chunks
			select {
			case <-r.Context().Done():
			case <-time.After(500 * time.Millisecond):
			}
		case "claude-sonnet-4-6":
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("data: {\"response\":{\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"Fast failover success!\"}]}}]}}\n\ndata: [DONE]\n\n"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer mockCCPA.Close()

	acc := ConnectedAccount{
		Provider:    ProviderAntigravity,
		Email:       "fast-watchdog@gmail.com",
		AccessToken: "ya29.watchdog-token",
		ProjectID:   "proj-watchdog",
		ExpiresAt:   time.Now().Unix() + 3600,
	}
	if _, err := UpsertConnectedAccount(acc); err != nil {
		t.Fatalf("UpsertConnectedAccount failed: %v", err)
	}

	upstream := NewUpstream(GatewayConfig{CCPAEndpoint: mockCCPA.URL})
	resolved := ResolveModel("antigravity/claude-opus-4-6-thinking", nil)

	// Request 1: 80ms TTFT timeout on claude-opus-4-6-thinking -> 200 OK on claude-sonnet-4-6
	req1 := OpenAIChatRequest{
		Model: "antigravity/claude-opus-4-6-thinking",
		Messages: []OpenAIChatMessage{
			{Role: "user", Content: json.RawMessage(`"First request with stalled model"`)},
		},
		Stream: false,
	}
	rec1 := httptest.NewRecorder()
	start1 := time.Now()
	if err := upstream.HandleChatCompletion(context.Background(), rec1, req1, resolved, ""); err != nil {
		t.Fatalf("Request 1 failed: %v", err)
	}
	elapsed1 := time.Since(start1)
	if elapsed1 >= 350*time.Millisecond {
		t.Fatalf("Request 1 took %s, expected < 350ms via 80ms first-chunk watchdog", elapsed1)
	}
	if rec1.Code != http.StatusOK || !strings.Contains(rec1.Body.String(), "Fast failover success!") {
		t.Fatalf("unexpected Request 1 response (%d): %s", rec1.Code, rec1.Body.String())
	}
	if gotModel := rec1.Header().Get("X-Omni-Gateway-Resolved-Model"); gotModel != "claude-sonnet-4-6" {
		t.Fatalf("expected X-Omni-Gateway-Resolved-Model=claude-sonnet-4-6, got %q", gotModel)
	}

	mu.Lock()
	opusAfter1 := callsByModel["claude-opus-4-6-thinking"]
	sonnetAfter1 := callsByModel["claude-sonnet-4-6"]
	mu.Unlock()

	if opusAfter1 != 1 || sonnetAfter1 != 1 {
		t.Fatalf("unexpected Request 1 call counts: %+v", map[string]int{
			"claude-opus-4-6-thinking": opusAfter1,
			"claude-sonnet-4-6":        sonnetAfter1,
		})
	}

	// Request 2 (distinct prompt so response cache is bypassed): must skip stalled opus model (0 new calls!)
	req2 := OpenAIChatRequest{
		Model: "antigravity/claude-opus-4-6-thinking",
		Messages: []OpenAIChatMessage{
			{Role: "user", Content: json.RawMessage(`"Second request must hit cached winning model directly"`)},
		},
		Stream: false,
	}
	rec2 := httptest.NewRecorder()
	if err := upstream.HandleChatCompletion(context.Background(), rec2, req2, resolved, ""); err != nil {
		t.Fatalf("Request 2 failed: %v", err)
	}
	if rec2.Code != http.StatusOK || !strings.Contains(rec2.Body.String(), "Fast failover success!") {
		t.Fatalf("unexpected Request 2 response (%d): %s", rec2.Code, rec2.Body.String())
	}

	mu.Lock()
	defer mu.Unlock()
	if callsByModel["claude-opus-4-6-thinking"] != opusAfter1 {
		t.Fatalf("Request 2 did not skip stalled opus model! counts=%+v", callsByModel)
	}
	if callsByModel["claude-sonnet-4-6"] != 2 {
		t.Fatalf("expected claude-sonnet-4-6 to be called immediately on Request 2 (total=2), got %d", callsByModel["claude-sonnet-4-6"])
	}
}

func TestCCPA503CachedAndCarbonCandidateOrder(t *testing.T) {
	tmpCfg := filepath.Join(t.TempDir(), "gateway_config.json")
	t.Setenv("GATEWAY_CONFIG_FILE", tmpCfg)
	resetAuthCacheForTest(t)
	ResetCCPAModelCaches()
	GetGlobalCache().Clear()
	t.Cleanup(func() {
		ResetCCPAModelCaches()
		GetGlobalCache().Clear()
		resetAuthCacheForTest(t)
	})

	// 1. Verify Carbon High & Argon High prioritize Opus, Sonnet, and Gemini 3 Flash BEFORE gemini-3.8-flash-tiered
	for _, modelID := range []string{"antigravity/gemini-4-carbon-high", "antigravity/gemini-4-argon-high"} {
		res := ResolveModel(modelID, nil)
		cands := buildCCPACandidatesWithCache("https://test.ccpa.local", "", res, false)
		tieredIdx := indexOfString(cands, "gemini-3.8-flash-tiered")
		if tieredIdx == -1 {
			t.Fatalf("%s candidates missing gemini-3.8-flash-tiered: %v", modelID, cands)
		}
		for _, fastModel := range []string{"gemini-3.8-flash-high", "gemini-3-flash"} {
			idx := indexOfString(cands, fastModel)
			if idx == -1 || idx >= tieredIdx {
				t.Fatalf("%s: expected %q (idx=%d) before gemini-3.8-flash-tiered (idx=%d) in %v", modelID, fastModel, idx, tieredIdx, cands)
			}
		}
	}

	// 2. Verify gemini-3.8-flash-low places gemini-3-flash BEFORE gemini-3.8-flash-tiered (and never puts tiered at front)
	flashLowRes := ResolveModel("antigravity/gemini-3.8-flash-low", nil)
	flashLowCands := buildCCPACandidatesWithCache("https://test.ccpa.local", "", flashLowRes, false)
	if len(flashLowCands) == 0 || flashLowCands[0] == "gemini-3.8-flash-tiered" {
		t.Fatalf("gemini-3.8-flash-low must not place gemini-3.8-flash-tiered first: %v", flashLowCands)
	}
	g3Idx := indexOfString(flashLowCands, "gemini-3-flash")
	tieredLowIdx := indexOfString(flashLowCands, "gemini-3.8-flash-tiered")
	if g3Idx == -1 || tieredLowIdx == -1 || g3Idx >= tieredLowIdx {
		t.Fatalf("gemini-3.8-flash-low: expected gemini-3-flash (idx=%d) before gemini-3.8-flash-tiered (idx=%d): %v", g3Idx, tieredLowIdx, flashLowCands)
	}

	// 3. Verify HTTP 503 is cached in deadCCPAModelCache so subsequent requests skip 503 models
	var c4High503Calls atomic.Int32
	var c4FallbackCalls atomic.Int32

	mockCCPA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload ccpaRequestPayload
		_ = json.NewDecoder(r.Body).Decode(&payload)
		if payload.Model == "gemini-4-carbon-high" {
			c4High503Calls.Add(1)
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":{"code":503,"message":"MODEL_CAPACITY_EXHAUSTED"}}`))
			return
		}
		c4FallbackCalls.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: {\"response\":{\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"Recovered after 503\"}]}}]}}\n\ndata: [DONE]\n\n"))
	}))
	defer mockCCPA.Close()

	upstream := NewUpstream(GatewayConfig{
		CCPAEndpoint:  mockCCPA.URL,
		AgyOAuthToken: "ya29.test-503-cache",
		AgyProjectID:  "proj-503",
	})
	carbonRes := ResolveModel("antigravity/gemini-4-carbon-high", nil)

	for i := 1; i <= 2; i++ {
		req := OpenAIChatRequest{
			Model: "antigravity/gemini-4-carbon-high",
			Messages: []OpenAIChatMessage{
				{Role: "user", Content: json.RawMessage(fmt.Sprintf(`"503 cache probe #%d"`, i))},
			},
		}
		rec := httptest.NewRecorder()
		if err := upstream.HandleChatCompletion(context.Background(), rec, req, carbonRes, ""); err != nil {
			t.Fatalf("probe #%d failed: %v", i, err)
		}
		if rec.Code != http.StatusOK {
			t.Fatalf("probe #%d status = %d, want 200", i, rec.Code)
		}
	}

	if !isDeadCCPAModel(mockCCPA.URL, "gemini-4-carbon-high", time.Now()) {
		t.Fatalf("expected gemini-4-carbon-high to be marked dead in deadCCPAModelCache after HTTP 503")
	}
	if got := c4High503Calls.Load(); got != 1 {
		t.Fatalf("expected gemini-4-carbon-high (503) to be called only once and skipped on probe #2, got %d", got)
	}
	if got := c4FallbackCalls.Load(); got != 2 {
		t.Fatalf("expected fallback model to serve both probes (2 calls), got %d", got)
	}
}

func TestCanceledContextStopsFallbackAndProtectsAccountMetrics(t *testing.T) {
	tmpCfg := filepath.Join(t.TempDir(), "gateway_config.json")
	t.Setenv("GATEWAY_CONFIG_FILE", tmpCfg)
	resetAuthCacheForTest(t)
	ResetCCPAModelCaches()
	GetGlobalCache().Clear()
	t.Cleanup(func() {
		ResetCCPAModelCaches()
		GetGlobalCache().Clear()
		resetAuthCacheForTest(t)
	})

	var alibabaCalls atomic.Int32
	mockAlibaba := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		alibabaCalls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer mockAlibaba.Close()

	var ccpaCalls atomic.Int32
	started := make(chan struct{})
	serverStop := make(chan struct{})
	var once sync.Once
	mockCCPA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		ccpaCalls.Add(1)
		once.Do(func() { close(started) })
		select {
		case <-r.Context().Done():
		case <-serverStop:
		case <-time.After(2 * time.Second):
		}
	}))
	defer mockCCPA.Close()
	defer close(serverStop)

	if _, err := UpsertConnectedAccount(ConnectedAccount{
		Provider:     ProviderAntigravity,
		Email:        "cancel-safe@gmail.com",
		AccessToken:  "ya29.cancel-safe",
		ProjectID:    "proj-cancel",
		AvgLatencyMs: 820,
		ExpiresAt:    time.Now().Unix() + 3600,
	}); err != nil {
		t.Fatalf("UpsertConnectedAccount(antigravity) failed: %v", err)
	}
	RecordAccountModelResult("cancel-safe@gmail.com", ProviderAntigravity, "antigravity/gemini-4-carbon-high", true, 820, 50, http.StatusOK)

	if _, err := UpsertConnectedAccount(ConnectedAccount{
		Provider: ProviderAlibaba,
		Email:    "alibaba-fallback@aliyun.com",
		APIKey:   "sk-sp-valid-alibaba-key",
		BaseURL:  mockAlibaba.URL,
	}); err != nil {
		t.Fatalf("UpsertConnectedAccount(alibaba) failed: %v", err)
	}

	upstream := NewUpstream(GatewayConfig{CCPAEndpoint: mockCCPA.URL})
	req := OpenAIChatRequest{
		Model: "auto/best-coding",
		Messages: []OpenAIChatMessage{
			{Role: "user", Content: json.RawMessage(`"Cancel mid-flight test"`)},
		},
	}
	resolved := ResolveModel(req.Model, nil)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-started
		time.Sleep(15 * time.Millisecond)
		cancel()
	}()

	rec := httptest.NewRecorder()
	err := upstream.HandleChatCompletion(ctx, rec, req, resolved, "")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got: %v", err)
	}

	if got := ccpaCalls.Load(); got != 1 {
		t.Fatalf("expected exactly 1 CCPA call before context cancellation stopped fallback, got %d", got)
	}
	if got := alibabaCalls.Load(); got != 0 {
		t.Fatalf("expected 0 Alibaba fallback calls after context cancellation, got %d", got)
	}

	saved := LoadSavedAuthConfig()
	for _, a := range saved.Accounts {
		if a.Email == "cancel-safe@gmail.com" {
			if a.FailureCount != 0 {
				t.Fatalf("expected FailureCount=0 on client-canceled request, got %d", a.FailureCount)
			}
			if a.CooldownUntil != 0 {
				t.Fatalf("expected CooldownUntil=0 on client-canceled request, got %d", a.CooldownUntil)
			}
			if a.AvgLatencyMs != 820 {
				t.Fatalf("expected AvgLatencyMs=820 preserved on client-canceled request, got %d", a.AvgLatencyMs)
			}
		}
	}
}

func TestFailedRequestDoesNotPoisonAccountAvgLatency(t *testing.T) {
	tmpCfg := filepath.Join(t.TempDir(), "gateway_config.json")
	t.Setenv("GATEWAY_CONFIG_FILE", tmpCfg)
	resetAuthCacheForTest(t)
	t.Cleanup(func() { resetAuthCacheForTest(t) })

	if _, err := UpsertConnectedAccount(ConnectedAccount{
		Provider:    ProviderAntigravity,
		Email:       "latency-clean@gmail.com",
		AccessToken: "ya29.latency-clean",
		ProjectID:   "proj-lat",
		ExpiresAt:   time.Now().Unix() + 3600,
	}); err != nil {
		t.Fatalf("UpsertConnectedAccount failed: %v", err)
	}

	// Seed healthy AvgLatencyMs = 950ms
	RecordAccountModelResult("latency-clean@gmail.com", ProviderAntigravity, "antigravity/gemini-4-carbon-high", true, 950, 120, http.StatusOK)

	// Record a failed 180s timeout result — must NOT poison AvgLatencyMs!
	RecordAccountModelResult("latency-clean@gmail.com", ProviderAntigravity, "antigravity/gemini-4-carbon-high", false, 180004, 0, http.StatusBadGateway)

	saved := LoadSavedAuthConfig()
	if len(saved.Accounts) != 1 {
		t.Fatalf("expected 1 account, got %d", len(saved.Accounts))
	}
	if saved.Accounts[0].AvgLatencyMs != 950 {
		t.Fatalf("AvgLatencyMs was poisoned by failed request: got %d, want 950", saved.Accounts[0].AvgLatencyMs)
	}
	if saved.Accounts[0].LastLatencyMs != 950 {
		t.Fatalf("LastLatencyMs was overwritten by failed request: got %d, want 950", saved.Accounts[0].LastLatencyMs)
	}
	if saved.Accounts[0].FailureCount != 1 {
		t.Fatalf("expected FailureCount=1 after failed request, got %d", saved.Accounts[0].FailureCount)
	}

	// Subsequent success updates weighted EMA: (950*3 + 750) / 4 = 900
	RecordAccountModelResult("latency-clean@gmail.com", ProviderAntigravity, "antigravity/gemini-4-carbon-high", true, 750, 100, http.StatusOK)
	savedAfter := LoadSavedAuthConfig()
	if savedAfter.Accounts[0].AvgLatencyMs != 900 {
		t.Fatalf("expected weighted EMA AvgLatencyMs=900, got %d", savedAfter.Accounts[0].AvgLatencyMs)
	}
}

func TestPerAccount429DoesNotDowngradeOtherAccounts(t *testing.T) {
	tmpCfg := filepath.Join(t.TempDir(), "gateway_config.json")
	t.Setenv("GATEWAY_CONFIG_FILE", tmpCfg)
	resetAuthCacheForTest(t)
	ResetCCPAModelCaches()
	GetGlobalCache().Clear()
	t.Cleanup(func() {
		ResetCCPAModelCaches()
		GetGlobalCache().Clear()
		resetAuthCacheForTest(t)
	})

	var mu sync.Mutex
	var global404Calls int
	callsByTokenAndModel := make(map[string]int)

	mockCCPA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload ccpaRequestPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		authHdr := strings.TrimSpace(r.Header.Get("Authorization"))

		mu.Lock()
		callsByTokenAndModel[authHdr+"|"+payload.Model]++
		if payload.Model == "gemini-4-carbon-high" || payload.Model == "gemini-4-carbon" {
			global404Calls++
		}
		mu.Unlock()

		if payload.Model == "gemini-4-carbon-high" || payload.Model == "gemini-4-carbon" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":{"code":404,"message":"model not yet rolled out"}}`))
			return
		}

		// Account 1 (exhausted@gmail.com) has exhausted its per-account Claude weekly quota (HTTP 429)
		if authHdr == "Bearer ya29.exhausted-claude-quota" &&
			(payload.Model == "claude-opus-4-6-thinking" || payload.Model == "claude-sonnet-4-6") {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"code":429,"status":"RESOURCE_EXHAUSTED","message":"Per-user Claude quota exhausted"}}`))
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, "data: {\"response\":{\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"served-by-%s\"}]}}]}}\n\ndata: [DONE]\n\n", payload.Model)
	}))
	defer mockCCPA.Close()

	accounts := []ConnectedAccount{
		{
			Provider:    ProviderAntigravity,
			Email:       "exhausted@gmail.com",
			AccessToken: "ya29.exhausted-claude-quota",
			ProjectID:   "proj-1",
			ExpiresAt:   time.Now().Unix() + 3600,
		},
		{
			Provider:    ProviderAntigravity,
			Email:       "fresh@gmail.com",
			AccessToken: "ya29.fresh-claude-quota",
			ProjectID:   "proj-2",
			ExpiresAt:   time.Now().Unix() + 3600,
		},
	}
	for _, a := range accounts {
		if _, err := UpsertConnectedAccount(a); err != nil {
			t.Fatalf("UpsertConnectedAccount(%s) failed: %v", a.Email, err)
		}
	}

	upstream := NewUpstream(GatewayConfig{CCPAEndpoint: mockCCPA.URL})
	resolved := ResolveModel("antigravity/claude-opus-4-6-thinking", nil)

	// 1. Account 1 (exhausted@gmail.com): hits 429 on opus & sonnet, succeeds on gemini-3-flash
	req1 := OpenAIChatRequest{
		Model: "antigravity/claude-opus-4-6-thinking",
		Messages: []OpenAIChatMessage{
			{Role: "user", Content: json.RawMessage(`"Request from exhausted Claude account"`)},
		},
	}
	rec1 := httptest.NewRecorder()
	ctx1 := WithPinnedAccount(context.Background(), "exhausted@gmail.com")
	if err := upstream.HandleChatCompletion(ctx1, rec1, req1, resolved, ""); err != nil {
		t.Fatalf("Account 1 request failed: %v", err)
	}
	if gotAcc := rec1.Header().Get("X-Omni-Gateway-Account"); gotAcc != "exhausted@gmail.com" {
		t.Fatalf("Account 1 header = %q, want exhausted@gmail.com", gotAcc)
	}
	if gotModel := rec1.Header().Get("X-Omni-Gateway-Resolved-Model"); gotModel != "gemini-3.8-flash-high" && gotModel != "gemini-3-flash" {
		t.Fatalf("Account 1 resolved model = %q, want gemini-3.8-flash-high or gemini-3-flash", gotModel)
	}

	mu.Lock()
	after1Global404s := global404Calls
	mu.Unlock()

	// 2. Account 2 (fresh@gmail.com): hits claude-opus-4-6-thinking directly!
	req2 := OpenAIChatRequest{
		Model: "antigravity/claude-opus-4-6-thinking",
		Messages: []OpenAIChatMessage{
			{Role: "user", Content: json.RawMessage(`"Request from fresh Claude account"`)},
		},
	}
	rec2 := httptest.NewRecorder()
	ctx2 := WithPinnedAccount(context.Background(), "fresh@gmail.com")
	if err := upstream.HandleChatCompletion(ctx2, rec2, req2, resolved, ""); err != nil {
		t.Fatalf("Account 2 request failed: %v", err)
	}
	if gotAcc := rec2.Header().Get("X-Omni-Gateway-Account"); gotAcc != "fresh@gmail.com" {
		t.Fatalf("Account 2 header = %q, want fresh@gmail.com", gotAcc)
	}
	if gotModel := rec2.Header().Get("X-Omni-Gateway-Resolved-Model"); gotModel != "claude-opus-4-6-thinking" {
		t.Fatalf("Account 2 was downgraded to %q instead of claude-opus-4-6-thinking!", gotModel)
	}
	if !strings.Contains(rec2.Body.String(), "served-by-claude-opus-4-6-thinking") {
		t.Fatalf("unexpected Account 2 response body: %s", rec2.Body.String())
	}

	mu.Lock()
	after2Global404s := global404Calls
	freshOpusCalls := callsByTokenAndModel["Bearer ya29.fresh-claude-quota|claude-opus-4-6-thinking"]
	freshFlashCalls := callsByTokenAndModel["Bearer ya29.fresh-claude-quota|gemini-3-flash"]
	mu.Unlock()

	if after2Global404s != after1Global404s {
		t.Fatalf("Account 2 re-probed globally dead 404 models (%d -> %d)", after1Global404s, after2Global404s)
	}
	if freshOpusCalls != 1 || freshFlashCalls != 0 {
		t.Fatalf("expected fresh@gmail.com to call claude-opus-4-6-thinking=1 and gemini-3-flash=0, got opus=%d flash=%d", freshOpusCalls, freshFlashCalls)
	}
}

func TestMaxCompletionTokensAnd65536DefaultUnlocked(t *testing.T) {
	tmpCfg := filepath.Join(t.TempDir(), "gateway_config.json")
	t.Setenv("GATEWAY_CONFIG_FILE", tmpCfg)
	resetAuthCacheForTest(t)
	ResetCCPAModelCaches()
	GetGlobalCache().Clear()
	t.Cleanup(func() {
		ResetCCPAModelCaches()
		GetGlobalCache().Clear()
		resetAuthCacheForTest(t)
	})

	var mu sync.Mutex
	var recordedMaxOut []int
	var returnMaxTokensFinish atomic.Bool

	mockCCPA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload ccpaRequestPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		mu.Lock()
		recordedMaxOut = append(recordedMaxOut, payload.Request.GenerationConfig.MaxOutputTokens)
		mu.Unlock()

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if returnMaxTokensFinish.Load() {
			_, _ = w.Write([]byte("data: {\"response\":{\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"partial-truncated-code\"}]},\"finishReason\":\"MAX_TOKENS\"}]}}\n\ndata: [DONE]\n\n"))
			return
		}
		_, _ = w.Write([]byte("data: {\"response\":{\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"complete-65k-output\"}]},\"finishReason\":\"STOP\"}]}}\n\ndata: [DONE]\n\n"))
	}))
	defer mockCCPA.Close()

	cfg := GatewayConfig{
		CCPAEndpoint:  mockCCPA.URL,
		AgyOAuthToken: "ya29.test-65k-unlock",
		AgyProjectID:  "proj-65k",
	}
	srv := NewServer(cfg, NewResolver(), NewUpstream(cfg))
	gw := httptest.NewServer(srv.Handler())
	defer gw.Close()

	// 1. Incoming request with NO max_tokens must send maxOutputTokens: 65536 to CCPA (never 0 / implicit 8192!)
	resp1, err := http.Post(
		gw.URL+"/v1/chat/completions",
		"application/json",
		strings.NewReader(`{"model":"antigravity/claude-opus-4-6-thinking","messages":[{"role":"user","content":"Generate 200KB module"}]}`),
	)
	if err != nil {
		t.Fatalf("POST #1 failed: %v", err)
	}
	defer resp1.Body.Close()
	if resp1.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp1.Body)
		t.Fatalf("POST #1 status = %d: %s", resp1.StatusCode, string(b))
	}
	var parsed1 OpenAIChatResponse
	if err := json.NewDecoder(resp1.Body).Decode(&parsed1); err != nil {
		t.Fatalf("decode #1 failed: %v", err)
	}
	if len(parsed1.Choices) != 1 || parsed1.Choices[0].FinishReason != "stop" {
		t.Fatalf("expected finish_reason='stop' on #1, got %+v", parsed1.Choices)
	}

	// 2. Incoming request with "max_completion_tokens": 48000 must send maxOutputTokens: 48000 to CCPA
	resp2, err := http.Post(
		gw.URL+"/v1/chat/completions",
		"application/json",
		strings.NewReader(`{"model":"antigravity/claude-opus-4-6-thinking","max_completion_tokens":48000,"messages":[{"role":"user","content":"Generate 48k module"}]}`),
	)
	if err != nil {
		t.Fatalf("POST #2 failed: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp2.Body)
		t.Fatalf("POST #2 status = %d: %s", resp2.StatusCode, string(b))
	}

	// Also verify flash-lite caps maxOutputTokens at 32768
	respLite, err := http.Post(
		gw.URL+"/v1/chat/completions",
		"application/json",
		strings.NewReader(`{"model":"antigravity/gemini-3.5-flash-lite","messages":[{"role":"user","content":"Lite ceiling check"}]}`),
	)
	if err != nil {
		t.Fatalf("POST lite failed: %v", err)
	}
	_ = respLite.Body.Close()

	mu.Lock()
	gotMaxOut := append([]int(nil), recordedMaxOut...)
	mu.Unlock()
	if len(gotMaxOut) != 3 {
		t.Fatalf("expected 3 recorded CCPA calls, got %d (%v)", len(gotMaxOut), gotMaxOut)
	}
	if gotMaxOut[0] != 65536 {
		t.Fatalf("call #1 (no max_tokens) sent maxOutputTokens=%d, want 65536", gotMaxOut[0])
	}
	if gotMaxOut[1] != 48000 {
		t.Fatalf("call #2 (max_completion_tokens=48000) sent maxOutputTokens=%d, want 48000", gotMaxOut[1])
	}
	if gotMaxOut[2] != 32768 {
		t.Fatalf("call #3 (gemini-3.5-flash-lite) sent maxOutputTokens=%d, want 32768", gotMaxOut[2])
	}

	// 3. When CCPA returns finishReason: "MAX_TOKENS", OmniRoute returns finish_reason: "length"
	//    (in both non-streaming and streaming) and does NOT cache the truncated output in GetGlobalCache().
	returnMaxTokensFinish.Store(true)
	GetGlobalCache().Clear()

	zeroTemp := 0.0
	truncReq := OpenAIChatRequest{
		Model: "antigravity/claude-opus-4-6-thinking",
		Messages: []OpenAIChatMessage{
			{Role: "user", Content: json.RawMessage(`"Hit MAX_TOKENS limit non-stream"`)},
		},
		Temperature: &zeroTemp,
		Stream:      false,
	}
	truncBody, _ := json.Marshal(truncReq)
	resp3, err := http.Post(gw.URL+"/v1/chat/completions", "application/json", strings.NewReader(string(truncBody)))
	if err != nil {
		t.Fatalf("POST #3 failed: %v", err)
	}
	defer resp3.Body.Close()
	var parsed3 OpenAIChatResponse
	if err := json.NewDecoder(resp3.Body).Decode(&parsed3); err != nil {
		t.Fatalf("decode #3 failed: %v", err)
	}
	if len(parsed3.Choices) != 1 || parsed3.Choices[0].FinishReason != "length" {
		t.Fatalf("expected finish_reason='length' on MAX_TOKENS, got %+v", parsed3.Choices)
	}
	if _, cached := GetGlobalCache().Get(truncReq); cached {
		t.Fatalf("truncated finish_reason='length' response must NOT be stored in GetGlobalCache() (non-streaming)")
	}

	// Streaming MAX_TOKENS check
	truncStreamReq := truncReq
	truncStreamReq.Messages = []OpenAIChatMessage{
		{Role: "user", Content: json.RawMessage(`"Hit MAX_TOKENS limit stream"`)} ,
	}
	truncStreamReq.Stream = true
	truncStreamBody, _ := json.Marshal(truncStreamReq)
	resp4, err := http.Post(gw.URL+"/v1/chat/completions", "application/json", strings.NewReader(string(truncStreamBody)))
	if err != nil {
		t.Fatalf("POST #4 (stream) failed: %v", err)
	}
	rawSSE, _ := io.ReadAll(resp4.Body)
	_ = resp4.Body.Close()
	if !strings.Contains(string(rawSSE), `"finish_reason":"length"`) {
		t.Fatalf("expected streaming final chunk to report finish_reason='length', got:\n%s", string(rawSSE))
	}
	if _, cached := GetGlobalCache().Get(truncStreamReq); cached {
		t.Fatalf("truncated finish_reason='length' response must NOT be stored in GetGlobalCache() (streaming)")
	}

	// Verify adaptive large-input prefill timeout scaling (545 KB input -> capped <= 75s)
	hugeText := strings.Repeat("a", 545*1024)
	hugeRaw, _ := json.Marshal(hugeText)
	hugeReq := OpenAIChatRequest{
		Model:    "antigravity/gemini-4-carbon-high",
		Messages: []OpenAIChatMessage{{Role: "user", Content: hugeRaw}},
	}
	gotAdaptive := effectiveCCPAFirstChunkTimeout(hugeReq)
	if gotAdaptive <= CCPAFirstChunkTimeout || gotAdaptive > 75*time.Second {
		t.Fatalf("expected adaptive first-chunk timeout in (20s, 75s] for 545KB prompt, got %s", gotAdaptive)
	}
}

func TestCCPA403TOSViolationBreaksCandidateLoopImmediately(t *testing.T) {
	tmpCfg := filepath.Join(t.TempDir(), "gateway_config.json")
	t.Setenv("GATEWAY_CONFIG_FILE", tmpCfg)
	resetAuthCacheForTest(t)
	ResetCCPAModelCaches()
	GetGlobalCache().Clear()
	t.Cleanup(func() {
		ResetCCPAModelCaches()
		GetGlobalCache().Clear()
		resetAuthCacheForTest(t)
	})

	accounts := []ConnectedAccount{
		{
			Provider:    ProviderAntigravity,
			Email:       "suspended-tos@gmail.com",
			AccessToken: "ya29.suspended-tos-403",
			ProjectID:   "proj-suspended",
			Priority:    100,
			ExpiresAt:   time.Now().Unix() + 3600,
		},
		{
			Provider:    ProviderAntigravity,
			Email:       "healthy-backup@gmail.com",
			AccessToken: "ya29.healthy-backup-200",
			ProjectID:   "proj-healthy",
			Priority:    90,
			ExpiresAt:   time.Now().Unix() + 3600,
		},
	}
	for _, a := range accounts {
		if _, err := UpsertConnectedAccount(a); err != nil {
			t.Fatalf("UpsertConnectedAccount(%s) failed: %v", a.Email, err)
		}
	}
	if _, err := SetRoutingStrategy(StrategyFillFirst); err != nil {
		t.Fatalf("SetRoutingStrategy failed: %v", err)
	}

	var mu sync.Mutex
	callsByToken := make(map[string]int)
	var totalCCPACalls atomic.Int32

	mockCCPA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		totalCCPACalls.Add(1)
		authHdr := strings.TrimSpace(r.Header.Get("Authorization"))
		mu.Lock()
		callsByToken[authHdr]++
		mu.Unlock()

		if authHdr == "Bearer ya29.suspended-tos-403" {
			w.WriteHeader(http.StatusForbidden)
			// Note: returns TOS_VIOLATION / PERMISSION_DENIED WITHOUT "VALIDATION_REQUIRED"
			_, _ = w.Write([]byte(`{"error":{"code":403,"status":"PERMISSION_DENIED","message":"Account suspended due to TOS_VIOLATION"}}`))
			return
		}
		if authHdr != "Bearer ya29.healthy-backup-200" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: {\"response\":{\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"Healthy account #2 succeeded immediately!\"}]}}]}}\n\ndata: [DONE]\n\n"))
	}))
	defer mockCCPA.Close()

	upstream := NewUpstream(GatewayConfig{CCPAEndpoint: mockCCPA.URL})
	req := OpenAIChatRequest{
		Model: "antigravity/gemini-4-carbon-high",
		Messages: []OpenAIChatMessage{
			{Role: "user", Content: json.RawMessage(`"Verify immediate 403 TOS_VIOLATION failover"`)},
		},
	}
	resolved := ResolveModel(req.Model, nil)

	rec := httptest.NewRecorder()
	if err := upstream.HandleChatCompletion(context.Background(), rec, req, resolved, ""); err != nil {
		t.Fatalf("HandleChatCompletion failed: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if gotAcc := rec.Header().Get("X-Omni-Gateway-Account"); gotAcc != "healthy-backup@gmail.com" {
		t.Fatalf("expected X-Omni-Gateway-Account=healthy-backup@gmail.com, got %q", gotAcc)
	}
	if !strings.Contains(rec.Body.String(), "Healthy account #2 succeeded immediately!") {
		t.Fatalf("unexpected response body: %s", rec.Body.String())
	}

	mu.Lock()
	deadAccCalls := callsByToken["Bearer ya29.suspended-tos-403"]
	healthyAccCalls := callsByToken["Bearer ya29.healthy-backup-200"]
	mu.Unlock()

	if deadAccCalls != 1 {
		t.Fatalf("expected suspended 403 TOS_VIOLATION account #1 to break after 1 CCPA call, got %d calls", deadAccCalls)
	}
	if healthyAccCalls != 1 || totalCCPACalls.Load() != 2 {
		t.Fatalf("expected 2 total CCPA calls (1 on 403 account #1 + 1 on healthy account #2), got total=%d healthy=%d", totalCCPACalls.Load(), healthyAccCalls)
	}
}
