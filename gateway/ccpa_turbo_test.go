package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCCPA16ShardPoolConfigAndSameAccountConcurrencySpread(t *testing.T) {
	if ccpaClientShards != 16 {
		t.Fatalf("ccpaClientShards = %d, want 16", ccpaClientShards)
	}

	distinctClients := make(map[*http.Client]bool, ccpaClientShards)
	for i := 0; i < ccpaClientShards; i++ {
		cl := ccpaShardedClients[i]
		if cl == nil {
			t.Fatalf("ccpaShardedClients[%d] is nil", i)
		}
		distinctClients[cl] = true

		tr, ok := cl.Transport.(*http.Transport)
		if !ok || tr == nil {
			t.Fatalf("ccpaShardedClients[%d].Transport is %T, want *http.Transport", i, cl.Transport)
		}
		if tr.MaxIdleConns != 128 {
			t.Errorf("shard %d MaxIdleConns = %d, want 128", i, tr.MaxIdleConns)
		}
		if tr.MaxIdleConnsPerHost != 64 {
			t.Errorf("shard %d MaxIdleConnsPerHost = %d, want 64", i, tr.MaxIdleConnsPerHost)
		}
		if tr.MaxConnsPerHost != 128 {
			t.Errorf("shard %d MaxConnsPerHost = %d, want 128", i, tr.MaxConnsPerHost)
		}
		if tr.WriteBufferSize != 64*1024 {
			t.Errorf("shard %d WriteBufferSize = %d, want %d", i, tr.WriteBufferSize, 64*1024)
		}
		if tr.ReadBufferSize != 64*1024 {
			t.Errorf("shard %d ReadBufferSize = %d, want %d", i, tr.ReadBufferSize, 64*1024)
		}
		if tr.IdleConnTimeout != 180*time.Second {
			t.Errorf("shard %d IdleConnTimeout = %s, want 180s", i, tr.IdleConnTimeout)
		}
		if !tr.ForceAttemptHTTP2 {
			t.Errorf("shard %d ForceAttemptHTTP2 = false, want true", i)
		}
	}
	if len(distinctClients) != 16 {
		t.Fatalf("expected 16 distinct *http.Client instances, got %d", len(distinctClients))
	}

	upstream := NewUpstream(GatewayConfig{CCPAEndpoint: "https://test.ccpa.local"})
	const accEmail = "shard-test-same-acc@gmail.com"
	const tok = "ya29.same-account-shard-token"

	// 1. When inFlight <= 1 on the account, all calls deterministically hit the exact same warm shard.
	AcquireAccountInFlight(accEmail)
	firstClient := upstream.ccpaHTTPClientForScope(accEmail, tok)
	for i := 0; i < 8; i++ {
		got := upstream.ccpaHTTPClientForScope(accEmail, tok)
		if got != firstClient {
			ReleaseAccountInFlight(accEmail)
			t.Fatalf("expected deterministic shard reuse when inFlight == 1, got mismatch on call %d", i)
		}
	}

	// 2. When inFlight > 1 on the SAME account, consecutive calls spread across all private per-account shards
	// and never share *http.Client / *http.Transport instances with any other account!
	AcquireAccountInFlight(accEmail)
	t.Cleanup(func() {
		ReleaseAccountInFlight(accEmail)
		ReleaseAccountInFlight(accEmail)
	})

	spreadClients := make(map[*http.Client]int, accountTransportShards)
	for i := 0; i < accountTransportShards*4; i++ {
		cl := upstream.ccpaHTTPClientForScope(accEmail, tok)
		spreadClients[cl]++
	}
	if len(spreadClients) != accountTransportShards {
		t.Fatalf("expected concurrent calls on same account (inFlight=2) to spread across all %d private account shards, got %d distinct shards", accountTransportShards, len(spreadClients))
	}
	otherClient := upstream.ccpaHTTPClientForScope("shard-test-other-acc@gmail.com", "ya29.other-account-token")
	if _, shared := spreadClients[otherClient]; shared {
		t.Fatalf("expected per-account CCPA client pools to be strictly disjoint across accounts")
	}
}

func TestDefaultCCPAEndpointSkipsColdStart404VirtualAliasesAndWarmup(t *testing.T) {
	ResetCCPAModelCaches()
	t.Cleanup(func() { ResetCCPAModelCaches() })

	// 1. Verify DefaultCCPAEndpoint preserves Carbon 4 / Argon 4 for coding while fast-pathing auto/fast and stripping virtual 404 aliases
	cases := []struct {
		modelID       string
		wantFirstReal string
	}{
		{"antigravity/gemini-4-carbon-high", "gemini-4-carbon-high"},
		{"antigravity/gemini-4-carbon-low", "gemini-4-carbon-low"},
		{"auto/fast", "gemini-3-flash"},
		{"antigravity/gemini-4-argon-high", "gemini-4-argon-high"},
		{"antigravity/gemini-3.1-pro-high", "gemini-3.8-flash-high"},
		{"antigravity/gemini-3.8-flash-high", "gemini-3.8-flash-high"},
		{"antigravity/gemini-3.8-flash-low", "gemini-3.8-flash-low"},
	}

	for _, tc := range cases {
		res := ResolveModel(tc.modelID, nil)
		prodCands := buildCCPACandidatesWithCache(DefaultCCPAEndpoint, "coldstart@gmail.com", res, false)
		if len(prodCands) == 0 {
			t.Fatalf("empty DefaultCCPAEndpoint candidates for %s", tc.modelID)
		}
		if prodCands[0] != tc.wantFirstReal {
			t.Errorf("DefaultCCPAEndpoint candidates[0] for %s = %q, want %q (all=%v)", tc.modelID, prodCands[0], tc.wantFirstReal, prodCands)
		}
		if isVirtualCCPAIDEAlias(prodCands[0]) {
			t.Errorf("DefaultCCPAEndpoint candidates[0] for %s is still a virtual 404 alias: %q", tc.modelID, prodCands[0])
		}
	}

	// 2. Verify custom test endpoints preserve original candidate ordering for backwards compatibility
	customRes := ResolveModel("antigravity/gemini-4-carbon-high", nil)
	customCands := buildCCPACandidatesWithCache("https://mock.ccpa.local", "coldstart@gmail.com", customRes, false)
	if len(customCands) == 0 || customCands[0] != "gemini-4-carbon-high" {
		t.Fatalf("expected custom endpoint to preserve gemini-4-carbon-high at index 0, got %v", customCands)
	}

	// 3. Verify WarmupCCPAEndpointAsync / warmupCCPAEndpoint pre-warms SharedHTTPClient + all 16 shards
	var headHits atomic.Int32
	mockOrigin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			headHits.Add(1)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer mockOrigin.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	warmed := warmupCCPAEndpoint(ctx, mockOrigin.URL+"/v1internal:streamGenerateContent?alt=sse")
	if warmed != ccpaClientShards+1 {
		t.Fatalf("warmupCCPAEndpoint warmed %d clients, want %d (SharedHTTPClient + 16 shards)", warmed, ccpaClientShards+1)
	}
	if got := int(headHits.Load()); got != ccpaClientShards+1 {
		t.Fatalf("expected %d HEAD warmup requests, got %d", ccpaClientShards+1, got)
	}

	// Also verify WarmupCCPAEndpointAsync is non-blocking
	var asyncHits atomic.Int32
	asyncSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asyncHits.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer asyncSrv.Close()

	start := time.Now()
	WarmupCCPAEndpointAsync(asyncSrv.URL + "/v1internal:streamGenerateContent?alt=sse")
	if elapsed := time.Since(start); elapsed > 50*time.Millisecond {
		t.Fatalf("WarmupCCPAEndpointAsync blocked caller for %s", elapsed)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && int(asyncHits.Load()) < ccpaClientShards+1 {
		time.Sleep(10 * time.Millisecond)
	}
	if got := int(asyncHits.Load()); got != ccpaClientShards+1 {
		t.Fatalf("WarmupCCPAEndpointAsync hit %d clients, want %d", got, ccpaClientShards+1)
	}
}

func TestComboModeStripedMaxSpeedAndParallelFlagship222AndFailover(t *testing.T) {
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

	resolver := NewResolver()

	// 1. Verify built-in presets in ListCombos() and ListModels()
	comboMap := make(map[string]ComboModel)
	for _, c := range resolver.ListCombos() {
		comboMap[c.ID] = c
	}

	wantMaxSpeedModels := []string{
		"antigravity/gemini-3-flash",
		"antigravity/gemini-3.5-flash-lite",
		"antigravity/claude-sonnet-4-6",
	}
	maxSpeedCombo, ok := comboMap["auto/max-speed"]
	if !ok {
		t.Fatalf("auto/max-speed missing from ListCombos()")
	}
	if maxSpeedCombo.Mode != ComboModeStriped {
		t.Fatalf("auto/max-speed Mode = %q, want %q", maxSpeedCombo.Mode, ComboModeStriped)
	}
	if !reflect.DeepEqual(maxSpeedCombo.Models, wantMaxSpeedModels) {
		t.Fatalf("auto/max-speed Models = %v, want %v", maxSpeedCombo.Models, wantMaxSpeedModels)
	}

	wantFlagshipModels := []string{
		"antigravity/claude-opus-4-6-thinking",
		"antigravity/claude-sonnet-4-6",
		"antigravity/gemini-3-flash",
	}
	flagshipCombo, ok := comboMap["auto/parallel-flagship"]
	if !ok {
		t.Fatalf("auto/parallel-flagship missing from ListCombos()")
	}
	if flagshipCombo.Mode != ComboModeStriped {
		t.Fatalf("auto/parallel-flagship Mode = %q, want %q", flagshipCombo.Mode, ComboModeStriped)
	}
	if !reflect.DeepEqual(flagshipCombo.Models, wantFlagshipModels) {
		t.Fatalf("auto/parallel-flagship Models = %v, want %v", flagshipCombo.Models, wantFlagshipModels)
	}

	modelSet := make(map[string]bool)
	for _, card := range resolver.ListModels() {
		modelSet[card.ID] = true
	}
	for _, id := range []string{"auto/max-speed", "auto/parallel-flagship"} {
		if !modelSet[id] {
			t.Fatalf("%s missing from ListModels()", id)
		}
	}

	// Configure 3 connected Antigravity accounts for realistic parallel multi-account + multi-model striping
	for i := 1; i <= 3; i++ {
		if _, err := UpsertConnectedAccount(ConnectedAccount{
			Provider:    ProviderAntigravity,
			Email:       fmt.Sprintf("stripe-acc-%d@gmail.com", i),
			AccessToken: fmt.Sprintf("ya29.stripe-token-%d", i),
			ProjectID:   fmt.Sprintf("proj-stripe-%d", i),
			ExpiresAt:   time.Now().Unix() + 3600,
		}); err != nil {
			t.Fatalf("UpsertConnectedAccount(%d) failed: %v", i, err)
		}
	}

	var mu sync.Mutex
	callsByModel := make(map[string]int)
	var failModel503 string
	var failModel429 string

	mockCCPA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload ccpaRequestPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		mu.Lock()
		callsByModel[payload.Model]++
		f503 := failModel503
		f429 := failModel429
		mu.Unlock()

		if f503 != "" && payload.Model == f503 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":{"code":503,"message":"MODEL_CAPACITY_EXHAUSTED"}}`))
			return
		}
		if f429 != "" && payload.Model == f429 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"code":429,"status":"RESOURCE_EXHAUSTED","message":"Model pool rate limit"}}`))
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, "data: {\"response\":{\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"ok-from-%s\"}]}}]}}\n\ndata: [DONE]\n\n", payload.Model)
	}))
	defer mockCCPA.Close()

	prodFallbacks := []string{
		"antigravity/gemini-4-carbon-high",
		"antigravity/gemini-4-argon-high",
		"claude-code/claude-opus-4-6-thinking",
		"alibaba/qwen3-coder-plus",
	}
	cfg := GatewayConfig{
		CCPAEndpoint:   mockCCPA.URL,
		FallbackModels: prodFallbacks,
	}
	upstream := NewUpstream(cfg)

	runConcurrentBurst := func(comboID string, n int) ([]string, error) {
		resolved := resolver.Resolve(comboID, prodFallbacks)
		startBarrier := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(n)
		winners := make([]string, n)
		errs := make([]error, n)

		for i := 0; i < n; i++ {
			go func(idx int) {
				defer wg.Done()
				<-startBarrier
				temp := 0.5
				req := OpenAIChatRequest{
					Model: comboID,
					Messages: []OpenAIChatMessage{
						{Role: "user", Content: json.RawMessage(fmt.Sprintf(`"Concurrent striped task #%d for %s"`, idx, comboID))},
					},
					Temperature: &temp,
				}
				rec := httptest.NewRecorder()
				err := upstream.HandleChatCompletion(context.Background(), rec, req, resolved, "")
				if err != nil {
					errs[idx] = err
					return
				}
				if rec.Code != http.StatusOK {
					errs[idx] = fmt.Errorf("HTTP %d: %s", rec.Code, rec.Body.String())
					return
				}
				if modeHdr := rec.Header().Get("X-Omni-Gateway-Combo-Mode"); modeHdr != string(ComboModeStriped) {
					errs[idx] = fmt.Errorf("expected X-Omni-Gateway-Combo-Mode=striped, got %q", modeHdr)
					return
				}
				winners[idx] = rec.Header().Get("X-Omni-Gateway-Resolved-Model")
			}(i)
		}
		close(startBarrier)
		wg.Wait()
		for i, err := range errs {
			if err != nil {
				return nil, fmt.Errorf("worker %d failed: %w", i, err)
			}
		}
		return winners, nil
	}

	// 2. Verify 6 concurrent requests on auto/max-speed stripe evenly 2-2-2 across all 3 models
	ResetCCPAModelCaches()
	mu.Lock()
	callsByModel = make(map[string]int)
	mu.Unlock()

	winnersMaxSpeed, err := runConcurrentBurst("auto/max-speed", 6)
	if err != nil {
		t.Fatalf("auto/max-speed 6-way burst failed: %v", err)
	}
	mu.Lock()
	gotMaxSpeedCounts := map[string]int{
		"gemini-3-flash":        callsByModel["gemini-3-flash"],
		"gemini-3.5-flash-lite": callsByModel["gemini-3.5-flash-lite"],
		"claude-sonnet-4-6":     callsByModel["claude-sonnet-4-6"],
	}
	totalCallsMaxSpeed := len(callsByModel)
	mu.Unlock()

	if gotMaxSpeedCounts["gemini-3-flash"] != 2 || gotMaxSpeedCounts["gemini-3.5-flash-lite"] != 2 || gotMaxSpeedCounts["claude-sonnet-4-6"] != 2 || totalCallsMaxSpeed != 3 {
		t.Fatalf("expected exact 2-2-2 striping on auto/max-speed (winners=%v), got counts=%+v", winnersMaxSpeed, gotMaxSpeedCounts)
	}

	// 3. Verify 6 concurrent requests on auto/parallel-flagship stripe evenly 2-2-2 across all 3 models
	ResetCCPAModelCaches()
	mu.Lock()
	callsByModel = make(map[string]int)
	mu.Unlock()

	winnersFlagship, err := runConcurrentBurst("auto/parallel-flagship", 6)
	if err != nil {
		t.Fatalf("auto/parallel-flagship 6-way burst failed: %v", err)
	}
	mu.Lock()
	gotFlagshipCounts := map[string]int{
		"claude-opus-4-6-thinking": callsByModel["claude-opus-4-6-thinking"],
		"claude-sonnet-4-6":        callsByModel["claude-sonnet-4-6"],
		"gemini-3-flash":           callsByModel["gemini-3-flash"],
	}
	totalCallsFlagship := len(callsByModel)
	mu.Unlock()

	if gotFlagshipCounts["claude-opus-4-6-thinking"] != 2 || gotFlagshipCounts["claude-sonnet-4-6"] != 2 || gotFlagshipCounts["gemini-3-flash"] != 2 || totalCallsFlagship != 3 {
		t.Fatalf("expected exact 2-2-2 striping on auto/parallel-flagship (winners=%v), got counts=%+v", winnersFlagship, gotFlagshipCounts)
	}

	// 4. Verify seamless per-account 503 failover on auto/max-speed (9 requests across 3 models x 3 accounts)
	ResetCCPAModelCaches()
	mu.Lock()
	callsByModel = make(map[string]int)
	failModel503 = "claude-sonnet-4-6"
	failModel429 = ""
	mu.Unlock()

	winners503, err := runConcurrentBurst("auto/max-speed", 9)
	if err != nil {
		t.Fatalf("auto/max-speed burst with 503 on claude-sonnet-4-6 failed: %v", err)
	}
	for _, wModel := range winners503 {
		if wModel != "gemini-3-flash" && wModel != "gemini-3.5-flash-lite" {
			t.Fatalf("unexpected winning model %q when claude-sonnet-4-6 returned 503", wModel)
		}
	}

	// Ensure second warmup sweep if concurrency scheduling left 1 account unprobed on claude-sonnet-4-6,
	// then verify once all 3 accounts recorded per-account 503, subsequent burst skips claude-sonnet-4-6 completely (0 extra calls!)
	if _, err := runConcurrentBurst("auto/max-speed", 6); err != nil {
		t.Fatalf("auto/max-speed warmup burst after 503 failed: %v", err)
	}
	mu.Lock()
	sonnet503Before := callsByModel["claude-sonnet-4-6"]
	mu.Unlock()
	if _, err := runConcurrentBurst("auto/max-speed", 6); err != nil {
		t.Fatalf("auto/max-speed second burst after per-account 503 cache failed: %v", err)
	}
	mu.Lock()
	sonnet503After := callsByModel["claude-sonnet-4-6"]
	mu.Unlock()
	if sonnet503After != sonnet503Before {
		t.Fatalf("expected per-account dead 503 model claude-sonnet-4-6 to be skipped once all accounts observed 503 (%d -> %d)", sonnet503Before, sonnet503After)
	}

	// 5. Verify seamless failover when 1 model returns HTTP 429 across accounts on auto/parallel-flagship
	ResetCCPAModelCaches()
	mu.Lock()
	callsByModel = make(map[string]int)
	failModel503 = ""
	failModel429 = "claude-opus-4-6-thinking"
	mu.Unlock()

	winners429, err := runConcurrentBurst("auto/parallel-flagship", 6)
	if err != nil {
		t.Fatalf("auto/parallel-flagship burst with 429 on claude-opus-4-6-thinking failed: %v", err)
	}
	for _, wModel := range winners429 {
		if !strings.EqualFold(wModel, "claude-sonnet-4-6") && !strings.EqualFold(wModel, "gemini-3-flash") {
			t.Fatalf("unexpected winning model %q when claude-opus-4-6-thinking returned 429", wModel)
		}
	}
}
