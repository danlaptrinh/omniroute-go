package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

type stalledTrackingBody struct {
	ctx    context.Context
	closed atomic.Bool
}

func (b *stalledTrackingBody) Read(_ []byte) (int, error) {
	<-b.ctx.Done()
	return 0, b.ctx.Err()
}

func (b *stalledTrackingBody) Close() error {
	b.closed.Store(true)
	return nil
}

type hedgeOrderInspectorWriter struct {
	*httptest.ResponseRecorder
	onWriteHeader func(code int)
	wroteHdr      bool
}

func (w *hedgeOrderInspectorWriter) WriteHeader(code int) {
	if !w.wroteHdr {
		w.wroteHdr = true
		if w.onWriteHeader != nil {
			w.onWriteHeader(code)
		}
	}
	w.ResponseRecorder.WriteHeader(code)
}

func (w *hedgeOrderInspectorWriter) Flush() {
	w.ResponseRecorder.Flush()
}

func resetTestAccountInFlight(email string) {
	for GetAccountInFlight(email) > 0 {
		ReleaseAccountConcurrency(email)
	}
}

func clearTestCCPACaches() {
	workingCCPAModelCache.Range(func(k, _ any) bool {
		workingCCPAModelCache.Delete(k)
		return true
	})
	deadCCPAModelCache.Range(func(k, _ any) bool {
		deadCCPAModelCache.Delete(k)
		return true
	})
}

func saveTestGatewayAuth(cfg SavedGatewayAuth) error {
	configMu.Lock()
	err := saveAuthConfigLocked(cfg)
	configMu.Unlock()
	return err
}

func TestTierHedger_ModelClassificationPredicates(t *testing.T) {
	deepTrue := []string{
		"gemini-4-carbon-high",
		"antigravity/gemini-4-carbon-high",
		"gemini-4-carbon-medium",
		"antigravity/gemini-4-carbon-medium",
		"gemini-4-argon-high",
		"antigravity/gemini-4-argon-high",
		"gemini-4-argon-medium",
		"antigravity/gemini-4-argon-medium",
		"gemini-3.8-flash-high",
		"antigravity/gemini-3.8-flash-high",
		"gemini-3.8-flash-medium",
		"antigravity/gemini-3.8-flash-medium",
		"gemini-3.1-pro",
		"antigravity/gemini-3.1-pro-high",
		"gemini-2.5-pro",
		"antigravity/gemini-2.5-pro-preview",
	}
	for _, m := range deepTrue {
		if !IsDeepReasoningGeminiModel(m) {
			t.Errorf("expected IsDeepReasoningGeminiModel(%q) == true", m)
		}
		if IsFastSpeedGeminiModel(m) {
			t.Errorf("expected IsFastSpeedGeminiModel(%q) == false", m)
		}
	}

	fastTrue := []string{
		"gemini-3-flash",
		"antigravity/gemini-3-flash",
		"gemini-3.5-flash-lite",
		"antigravity/gemini-3.5-flash-lite",
		"gemini-3.1-flash-lite",
		"gemini-2.5-flash",
		"antigravity/gemini-2.5-flash-lite",
		"gemini-4-carbon-low",
		"antigravity/gemini-4-carbon-low",
		"gemini-4-argon-low",
		"gemini-3.8-flash-low",
		"auto/fast",
		"auto/max-speed",
	}
	for _, m := range fastTrue {
		if !IsFastSpeedGeminiModel(m) {
			t.Errorf("expected IsFastSpeedGeminiModel(%q) == true", m)
		}
		if IsDeepReasoningGeminiModel(m) {
			t.Errorf("expected IsDeepReasoningGeminiModel(%q) == false", m)
		}
	}
}

func TestTierHedger_UltraVsProRoutingAndSpillover(t *testing.T) {
	tmpCfg := filepath.Join(t.TempDir(), "gateway_config.json")
	t.Setenv("GATEWAY_CONFIG_FILE", tmpCfg)
	resetAuthCacheForTest(t)
	ClearAccountModelRateLimits("")
	t.Cleanup(func() {
		resetAuthCacheForTest(t)
		ClearAccountModelRateLimits("")
	})

	exp := time.Now().Add(2 * time.Hour).Unix()
	var accounts []ConnectedAccount
	for i := 1; i <= 4; i++ {
		email := fmt.Sprintf("ultra%d@gmail.com", i)
		resetTestAccountInFlight(email)
		accounts = append(accounts, ConnectedAccount{
			ID:          fmt.Sprintf("u-%d", i),
			Provider:    ProviderAntigravity,
			Tier:        "Ultra",
			Email:       email,
			AccessToken: fmt.Sprintf("tok-ultra-%d", i),
			ExpiresAt:   exp,
		})
	}
	for i := 1; i <= 5; i++ {
		email := fmt.Sprintf("pro%d@gmail.com", i)
		resetTestAccountInFlight(email)
		accounts = append(accounts, ConnectedAccount{
			ID:          fmt.Sprintf("p-%d", i),
			Provider:    ProviderAntigravity,
			Tier:        "Pro",
			Email:       email,
			AccessToken: fmt.Sprintf("tok-pro-%d", i),
			ExpiresAt:   exp,
		})
	}
	t.Cleanup(func() {
		for _, a := range accounts {
			resetTestAccountInFlight(a.Email)
		}
	})

	if err := saveTestGatewayAuth(SavedGatewayAuth{
		RoutingStrategy: StrategyRoundRobin,
		Accounts:        accounts,
	}); err != nil {
		t.Fatalf("SaveAuthConfig: %v", err)
	}

	// 1. N=12 concurrent deep-reasoning requests MUST route 100% to the 4 Ultra accounts (3 each).
	var releases []func()
	for i := 0; i < 12; i++ {
		acc, rel, err := AcquireProviderAccountForModel(context.Background(), ProviderAntigravity, "antigravity/gemini-4-carbon-high", "carbon-high")
		if err != nil {
			t.Fatalf("acquire #%d failed: %v", i+1, err)
		}
		releases = append(releases, rel)
		if !strings.HasPrefix(acc.Email, "ultra") {
			t.Fatalf("request #%d (<=12) routed to non-Ultra account %q (tier=%q)", i+1, acc.Email, acc.Tier)
		}
	}

	// 2. Request #13 has minUltraInFlight=3 > minOverallInFlight(0)+2, so it MUST spill to Pro!
	spillAcc, spillRel, err := AcquireProviderAccountForModel(context.Background(), ProviderAntigravity, "antigravity/gemini-4-carbon-high", "carbon-high")
	if err != nil {
		t.Fatalf("spillover acquire failed: %v", err)
	}
	releases = append(releases, spillRel)
	if !strings.HasPrefix(spillAcc.Email, "pro") {
		t.Fatalf("expected request #13 to spill over to Pro when Ultra has 3 in-flight vs Pro 0, got %q", spillAcc.Email)
	}

	// Fill remaining 4 Pro accounts to 1 in-flight so minOverallInFlight becomes 1;
	// then Ultra can take 1 more slot each (up to inFlight == 4), and at >=4 MUST spill to Pro again.
	for i := 0; i < 4; i++ {
		acc, rel, err := AcquireProviderAccountForModel(context.Background(), ProviderAntigravity, "antigravity/gemini-4-carbon-high", "carbon-high")
		if err != nil {
			t.Fatalf("pro balance acquire failed: %v", err)
		}
		releases = append(releases, rel)
		if !strings.HasPrefix(acc.Email, "pro") {
			t.Fatalf("expected remaining idle Pro slot to fill to 1, got %q", acc.Email)
		}
	}
	// Now Ultra=3, Pro=1 (minOverall=1 -> 3 <= 1+2 && 3 < 4), next 4 go to Ultra (reaching 4 in-flight each).
	for i := 0; i < 4; i++ {
		acc, rel, err := AcquireProviderAccountForModel(context.Background(), ProviderAntigravity, "antigravity/gemini-4-carbon-high", "carbon-high")
		if err != nil {
			t.Fatalf("ultra 4th slot acquire failed: %v", err)
		}
		releases = append(releases, rel)
		if !strings.HasPrefix(acc.Email, "ultra") {
			t.Fatalf("expected 4th Ultra slot before cap=4, got %q", acc.Email)
		}
	}
	// Now all 4 Ultra accounts have inFlight == 4 (>= 4 cap): next request MUST spill to Pro!
	capSpillAcc, capSpillRel, err := AcquireProviderAccountForModel(context.Background(), ProviderAntigravity, "antigravity/gemini-4-carbon-high", "carbon-high")
	if err != nil {
		t.Fatalf("cap>=4 spillover failed: %v", err)
	}
	releases = append(releases, capSpillRel)
	if !strings.HasPrefix(capSpillAcc.Email, "pro") {
		t.Fatalf("expected spillover to Pro when all Ultra accounts have >=4 in-flight, got %q", capSpillAcc.Email)
	}

	for _, rel := range releases {
		rel()
	}
	releases = nil

	// 3. Fast-speed models (auto/fast, gemini-3-flash, *-low) route preferentially to the 5 Pro accounts
	// while minProInFlight <= minOverallInFlight + 1 (i.e. up to 10 concurrent requests at 2 each on Pro, 0 on Ultra).
	fastModels := []string{"auto/fast", "antigravity/gemini-3-flash", "antigravity/gemini-4-carbon-low"}
	for i := 0; i < 10; i++ {
		m := fastModels[i%len(fastModels)]
		acc, rel, err := AcquireProviderAccountForModel(context.Background(), ProviderAntigravity, m, m)
		if err != nil {
			t.Fatalf("fast acquire #%d failed: %v", i+1, err)
		}
		releases = append(releases, rel)
		if !strings.HasPrefix(acc.Email, "pro") {
			t.Fatalf("fast request #%d (%s) consumed Ultra account %q instead of Pro", i+1, m, acc.Email)
		}
	}
	// 11th concurrent fast request (minPro=2 > minOverall(0)+1) spills cleanly to idle Ultra.
	fastSpill, fastSpillRel, err := AcquireProviderAccountForModel(context.Background(), ProviderAntigravity, "auto/fast", "auto/fast")
	if err != nil {
		t.Fatalf("fast spillover acquire failed: %v", err)
	}
	releases = append(releases, fastSpillRel)
	if !strings.HasPrefix(fastSpill.Email, "ultra") {
		t.Fatalf("expected 11th fast request (Pro=2 vs Ultra=0) to spill to Ultra, got %q", fastSpill.Email)
	}

	// 4. Explicit PinAccountContextKey MUST always be honored regardless of Tier.
	pinnedCtx := WithPinnedAccount(context.Background(), "pro3@gmail.com")
	pinnedAcc, err := SelectProviderAccountForModel(pinnedCtx, ProviderAntigravity, "antigravity/gemini-4-carbon-high")
	if err != nil || pinnedAcc.Email != "pro3@gmail.com" {
		t.Fatalf("expected pinned pro3@gmail.com, got %+v (err=%v)", pinnedAcc, err)
	}

	for _, rel := range releases {
		rel()
	}
}

func TestTierHedger_TierPersistenceOnlyOnChange(t *testing.T) {
	tmpCfg := filepath.Join(t.TempDir(), "gateway_config.json")
	t.Setenv("GATEWAY_CONFIG_FILE", tmpCfg)
	resetAuthCacheForTest(t)
	t.Cleanup(func() { resetAuthCacheForTest(t) })

	var currentTier atomic.Value
	currentTier.Store("g1-ultra-tier")

	mockQuotaSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "loadCodeAssist"):
			tierID := currentTier.Load().(string)
			_, _ = fmt.Fprintf(w, `{"currentTier":{"id":%q,"name":%q},"cloudaicompanionProject":"proj-1"}`, tierID, tierID)
		case strings.Contains(r.URL.Path, "retrieveUserQuotaSummary"):
			_, _ = io.WriteString(w, `{"groups":[]}`)
		case strings.Contains(r.URL.Path, "fetchAvailableModels"):
			_, _ = io.WriteString(w, `{"models":{}}`)
		default:
			_, _ = io.WriteString(w, `{"buckets":[]}`)
		}
	}))
	defer mockQuotaSrv.Close()

	origLoad := CCPALoadCodeAssistEndpoint
	origSum := CCPARetrieveUserQuotaSummaryEndpoint
	origQuota := CCPARetrieveUserQuotaEndpoint
	origFetch := CCPAFetchAvailableModelsEndpoint
	CCPALoadCodeAssistEndpoint = mockQuotaSrv.URL + "/v1internal:loadCodeAssist"
	CCPARetrieveUserQuotaSummaryEndpoint = mockQuotaSrv.URL + "/v1internal:retrieveUserQuotaSummary"
	CCPARetrieveUserQuotaEndpoint = mockQuotaSrv.URL + "/v1internal:retrieveUserQuota"
	CCPAFetchAvailableModelsEndpoint = mockQuotaSrv.URL + "/v1internal:fetchAvailableModels"
	defer func() {
		CCPALoadCodeAssistEndpoint = origLoad
		CCPARetrieveUserQuotaSummaryEndpoint = origSum
		CCPARetrieveUserQuotaEndpoint = origQuota
		CCPAFetchAvailableModelsEndpoint = origFetch
	}()

	acc := ConnectedAccount{
		ID:          "tier-sync-1",
		Provider:    ProviderAntigravity,
		Email:       "tiersync@gmail.com",
		AccessToken: "ya29.tiersync",
		ProjectID:   "proj-1",
		ExpiresAt:   time.Now().Add(2 * time.Hour).Unix(),
	}
	if err := saveTestGatewayAuth(SavedGatewayAuth{Accounts: []ConnectedAccount{acc}}); err != nil {
		t.Fatalf("SaveAuthConfig: %v", err)
	}

	// Poll 1: Tier transitions "" -> "Ultra", persisted to disk.
	var d1 AccountQuotaDetail
	inspectAntigravityAccount(context.Background(), &acc, &d1, "reset")
	saved1 := LoadSavedAuthConfig()
	if len(saved1.Accounts) != 1 || saved1.Accounts[0].Tier != "Ultra" {
		t.Fatalf("expected persisted Tier == Ultra after first poll, got %+v", saved1.Accounts)
	}

	// Set deterministic past mtime and run Poll 2 with unchanged "Ultra" tier -> disk file MUST NOT be rewritten.
	pastTime := time.Now().Add(-30 * time.Second).Truncate(time.Second)
	if err := os.Chtimes(tmpCfg, pastTime, pastTime); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}
	resetAuthCacheForTest(t)

	var d2 AccountQuotaDetail
	acc2 := saved1.Accounts[0]
	inspectAntigravityAccount(context.Background(), &acc2, &d2, "reset")
	stAfterUnchanged, err := os.Stat(tmpCfg)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if !stAfterUnchanged.ModTime().Equal(pastTime) {
		t.Fatalf("expected unchanged Tier poll NOT to rewrite config file (mtime=%v, want=%v)", stAfterUnchanged.ModTime(), pastTime)
	}

	// Poll 3: Tier changes "Ultra" -> "Pro" -> disk file MUST update.
	currentTier.Store("standard-pro-tier")
	var d3 AccountQuotaDetail
	inspectAntigravityAccount(context.Background(), &acc2, &d3, "reset")
	saved3 := LoadSavedAuthConfig()
	if saved3.Accounts[0].Tier != "Pro" {
		t.Fatalf("expected persisted Tier == Pro after tier change, got %q", saved3.Accounts[0].Tier)
	}
}

func TestTierHedger_ThinkingLevelLowDirectFastPath(t *testing.T) {
	tmpCfg := filepath.Join(t.TempDir(), "gateway_config.json")
	t.Setenv("GATEWAY_CONFIG_FILE", tmpCfg)
	resetAuthCacheForTest(t)
	clearTestCCPACaches()
	t.Cleanup(func() {
		resetAuthCacheForTest(t)
		clearTestCCPACaches()
	})

	acc := ConnectedAccount{
		ID:          "fastpath-acc",
		Provider:    ProviderAntigravity,
		Tier:        "Pro",
		Email:       "fastpath@gmail.com",
		AccessToken: "ya29.fastpath",
		ProjectID:   "proj-fast",
		ExpiresAt:   time.Now().Add(2 * time.Hour).Unix(),
	}
	if err := saveTestGatewayAuth(SavedGatewayAuth{Accounts: []ConnectedAccount{acc}}); err != nil {
		t.Fatalf("SaveAuthConfig: %v", err)
	}

	lowModels := []string{
		"antigravity/gemini-4-carbon-low",
		"antigravity/gemini-4-argon-low",
		"antigravity/gemini-3.8-flash-low",
	}
	for _, modelID := range lowModels {
		res := ResolveModel(modelID, nil)
		cands := buildCCPACandidatesWithCache(DefaultCCPAEndpoint, acc.Email, res, false)
		if len(cands) < 2 {
			t.Fatalf("%s candidates too short: %v", modelID, cands)
		}
		if cands[0] != "gemini-3-flash" {
			t.Fatalf("%s primary candidate = %q, want \"gemini-3-flash\" (full=%v)", modelID, cands[0], cands)
		}
		if cands[1] != "gemini-3.8-flash-low" {
			t.Fatalf("%s secondary fallback = %q, want \"gemini-3.8-flash-low\" (full=%v)", modelID, cands[1], cands)
		}
	}

	// Verify auto/fast without touching resolver.go: Models[0] remains antigravity/gemini-4-carbon-low
	// and executes gemini-3-flash primary on DefaultCCPAEndpoint, falling back to gemini-3.8-flash-low on 503.
	autoFastRes := NewResolver().Resolve("auto/fast", nil)
	if len(autoFastRes.FallbackChain) == 0 || autoFastRes.FallbackChain[0] != "antigravity/gemini-4-carbon-low" {
		t.Fatalf("auto/fast FallbackChain[0] changed unexpectedly: %v", autoFastRes.FallbackChain)
	}

	var triedModels []string
	upstream := NewUpstream(GatewayConfig{CCPAEndpoint: DefaultCCPAEndpoint})
	upstream.client = &http.Client{
		Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			raw, _ := io.ReadAll(r.Body)
			var payload struct {
				Model string `json:"model"`
			}
			_ = json.Unmarshal(raw, &payload)
			triedModels = append(triedModels, payload.Model)
			if payload.Model == "gemini-3-flash" {
				return &http.Response{
					StatusCode: http.StatusServiceUnavailable,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(`{"error":"overloaded"}`)),
				}, nil
			}
			sse := "data: {\"response\":{\"modelVersion\":\"gemini-3.8-flash\",\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"fast-fallback-ok\"}]},\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":5,\"candidatesTokenCount\":3,\"totalTokenCount\":8}}}\n\ndata: [DONE]\n\n"
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body:       io.NopCloser(strings.NewReader(sse)),
			}, nil
		}),
	}

	rec := httptest.NewRecorder()
	req := OpenAIChatRequest{
		Model:    "auto/fast",
		Messages: []OpenAIChatMessage{{Role: "user", Content: json.RawMessage(`"ping"`)}},
	}
	if err := upstream.HandleChatCompletion(context.Background(), rec, req, autoFastRes, ""); err != nil {
		t.Fatalf("HandleChatCompletion(auto/fast) failed: %v", err)
	}
	if len(triedModels) != 2 || triedModels[0] != "gemini-3-flash" || triedModels[1] != "gemini-3.8-flash-low" {
		t.Fatalf("unexpected candidate sequence for auto/fast: %v", triedModels)
	}
}

func TestTierHedger_PreFirstTokenTTFTHedging_WinnerAndLoserCleanup(t *testing.T) {
	tmpCfg := filepath.Join(t.TempDir(), "gateway_config.json")
	t.Setenv("GATEWAY_CONFIG_FILE", tmpCfg)
	resetAuthCacheForTest(t)
	clearTestCCPACaches()
	ClearAccountModelRateLimits("")
	GetGlobalCache().Clear()

	origProDelay := CCPAHedgeDelayPro
	origUltraDelay := CCPAHedgeDelayUltra
	CCPAHedgeDelayPro = 45 * time.Millisecond
	CCPAHedgeDelayUltra = 90 * time.Millisecond
	t.Cleanup(func() {
		CCPAHedgeDelayPro = origProDelay
		CCPAHedgeDelayUltra = origUltraDelay
		resetTestAccountInFlight("pro-slow@gmail.com")
		resetTestAccountInFlight("ultra-slow@gmail.com")
		resetTestAccountInFlight("ultra-fast@gmail.com")
		resetAuthCacheForTest(t)
		clearTestCCPACaches()
		ClearAccountModelRateLimits("")
		GetGlobalCache().Clear()
	})

	exp := time.Now().Add(2 * time.Hour).Unix()
	// Put Pro account at higher priority under StrategyFillFirst ONLY when Ultra is temporarily in cooldown?
	// No: to make primary pick Pro while keeping Ultra healthy for hedge, give Pro Tier="Pro" and Ultra Tier="Ultra",
	// and temporarily hold 4 in-flight on Ultra ONLY before Acquire? Even simpler: 2 Pro accounts or 1 Pro + 1 Ultra
	// where Ultra has 3 in-flight vs Pro 0 -> primary picks Pro (since minUltra=3 > minOverall(0)+2), while hedge
	// acquires healthy alternate account `ultra-fast@gmail.com`! Or 2 Ultra accounts (`ultra-slow` and `ultra-fast`)
	// OR 1 Pro (`pro-slow`) + 1 Pro/Ultra (`ultra-fast` with 3 synthetic in-flight <=4)!
	// Let's test Pro primary (45ms hedge) + Ultra secondary (inFlight=2 <= 2? Wait: if Ultra has 0 in-flight,
	// Tier-Aware scheduler picks Ultra primary!). So let's test BOTH:
	// Case 1: 2 accounts (`pro-slow@gmail.com` Tier="Pro", `pro-fast@gmail.com` Tier="Pro") -> primary is `pro-slow` (Pro 45ms hedgeDelay), secondary is `pro-fast`!
	// Case 2: 2 accounts (`ultra-slow@gmail.com` Tier="Ultra", `ultra-fast@gmail.com` Tier="Ultra") -> primary is `ultra-slow` (Ultra 90ms hedgeDelay), secondary is `ultra-fast` (Tier="Ultra", inFlight=0 <= 2)!
	if err := saveTestGatewayAuth(SavedGatewayAuth{
		RoutingStrategy: StrategyFillFirst,
		Accounts: []ConnectedAccount{
			{
				ID:          "u-slow",
				Provider:    ProviderAntigravity,
				Tier:        "Ultra",
				Priority:    100,
				Email:       "ultra-slow@gmail.com",
				AccessToken: "ya29.ultra-slow",
				ProjectID:   "proj-u1",
				ExpiresAt:   exp,
			},
			{
				ID:          "u-fast",
				Provider:    ProviderAntigravity,
				Tier:        "Ultra",
				Priority:    50,
				Email:       "ultra-fast@gmail.com",
				AccessToken: "ya29.ultra-fast",
				ProjectID:   "proj-u2",
				ExpiresAt:   exp,
			},
		},
	}); err != nil {
		t.Fatalf("SaveAuthConfig: %v", err)
	}
	resetTestAccountInFlight("ultra-slow@gmail.com")
	resetTestAccountInFlight("ultra-fast@gmail.com")

	for _, streamMode := range []bool{true, false} {
		t.Run(fmt.Sprintf("stream_%v", streamMode), func(t *testing.T) {
			GetGlobalCache().Clear()
			var slowCalls atomic.Int32
			var fastCalls atomic.Int32
			var slowBody *stalledTrackingBody
			var slowReqCtx context.Context

			upstream := NewUpstream(GatewayConfig{CCPAEndpoint: DefaultCCPAEndpoint})
			upstream.client = &http.Client{
				Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
					authHdr := r.Header.Get("Authorization")
					if strings.Contains(authHdr, "ya29.ultra-slow") {
						slowCalls.Add(1)
						slowReqCtx = r.Context()
						slowBody = &stalledTrackingBody{ctx: r.Context()}
						return &http.Response{
							StatusCode: http.StatusOK,
							Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
							Body:       slowBody,
						}, nil
					}
					fastCalls.Add(1)
					sse := "data: {\"response\":{\"modelVersion\":\"gemini-3.8-flash\",\"candidates\":[{\"content\":{\"parts\":[{\"thought\":true,\"text\":\"quick thought\"},{\"text\":\"hedged-ultra-winner\"}]},\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":10,\"candidatesTokenCount\":6,\"totalTokenCount\":16}}}\n\ndata: [DONE]\n\n"
					return &http.Response{
						StatusCode: http.StatusOK,
						Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
						Body:       io.NopCloser(strings.NewReader(sse)),
					}, nil
				}),
			}

			var (
				loserCanceledBeforeWrite  bool
				loserBodyClosedBeforeWrite bool
				loserInFlightBeforeWrite   int
			)
			rawRec := httptest.NewRecorder()
			inspWriter := &hedgeOrderInspectorWriter{
				ResponseRecorder: rawRec,
				onWriteHeader: func(_ int) {
					loserCanceledBeforeWrite = slowReqCtx != nil && slowReqCtx.Err() != nil
					loserBodyClosedBeforeWrite = slowBody != nil && slowBody.closed.Load()
					loserInFlightBeforeWrite = GetAccountInFlight("ultra-slow@gmail.com")
				},
			}

			resolved := NewResolver().Resolve("antigravity/gemini-4-carbon-high", nil)
			req := OpenAIChatRequest{
				Model:    "antigravity/gemini-4-carbon-high",
				Stream:   streamMode,
				Messages: []OpenAIChatMessage{{Role: "user", Content: json.RawMessage(fmt.Sprintf(`"solve task stream=%v"`, streamMode))}},
			}

			if err := upstream.HandleChatCompletion(context.Background(), inspWriter, req, resolved, ""); err != nil {
				t.Fatalf("HandleChatCompletion failed: %v", err)
			}

			if slowCalls.Load() != 1 || fastCalls.Load() != 1 {
				t.Fatalf("expected exactly 1 primary + 1 hedged call, got slow=%d fast=%d", slowCalls.Load(), fastCalls.Load())
			}
			if !loserCanceledBeforeWrite {
				t.Fatalf("expected stalled primary context to be canceled BEFORE WriteHeader")
			}
			if !loserBodyClosedBeforeWrite {
				t.Fatalf("expected stalled primary response body to be closed BEFORE WriteHeader")
			}
			if loserInFlightBeforeWrite != 0 {
				t.Fatalf("expected stalled primary inFlight == 0 BEFORE WriteHeader, got %d", loserInFlightBeforeWrite)
			}
			if gotAcc := rawRec.Header().Get("X-Omni-Gateway-Account"); gotAcc != "ultra-fast@gmail.com" {
				t.Fatalf("expected winning header account ultra-fast@gmail.com, got %q", gotAcc)
			}
			if gotHedge := rawRec.Header().Get("X-Omni-Gateway-Hedged-Racing"); gotHedge != "WIN" {
				t.Fatalf("expected X-Omni-Gateway-Hedged-Racing=WIN, got %q", gotHedge)
			}
			if GetAccountInFlight("ultra-slow@gmail.com") != 0 || GetAccountInFlight("ultra-fast@gmail.com") != 0 {
				t.Fatalf("leaked in-flight counters after completion: slow=%d fast=%d",
					GetAccountInFlight("ultra-slow@gmail.com"), GetAccountInFlight("ultra-fast@gmail.com"))
			}

			bodyStr := rawRec.Body.String()
			if !strings.Contains(bodyStr, "hedged-ultra-winner") {
				t.Fatalf("expected hedged winner content in response body, got: %s", bodyStr)
			}
			if streamMode && strings.Count(bodyStr, "data: [DONE]") != 1 {
				t.Fatalf("expected exactly 1 [DONE] marker, got body:\n%s", bodyStr)
			}
		})
	}

	// Pinned account MUST NOT hedge even if slower than hedgeDelay.
	t.Run("pinned_account_never_hedges", func(t *testing.T) {
		var calls atomic.Int32
		upstream := NewUpstream(GatewayConfig{CCPAEndpoint: DefaultCCPAEndpoint})
		upstream.client = &http.Client{
			Transport: roundTripperFunc(func(_ *http.Request) (*http.Response, error) {
				calls.Add(1)
				time.Sleep(110 * time.Millisecond)
				sse := "data: {\"response\":{\"modelVersion\":\"gemini-3.8-flash\",\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"pinned-ok\"}]},\"finishReason\":\"STOP\"}]}}\n\ndata: [DONE]\n\n"
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
					Body:       io.NopCloser(strings.NewReader(sse)),
				}, nil
			}),
		}
		pinnedCtx := WithPinnedAccount(context.Background(), "ultra-slow@gmail.com")
		if !IsPinnedAccount(pinnedCtx) {
			t.Fatalf("expected IsPinnedAccount(pinnedCtx) == true")
		}
		rec := httptest.NewRecorder()
		resolved := NewResolver().Resolve("antigravity/gemini-4-carbon-high", nil)
		req := OpenAIChatRequest{
			Model:    "antigravity/gemini-4-carbon-high",
			Messages: []OpenAIChatMessage{{Role: "user", Content: json.RawMessage(`"pinned test"`)}},
		}
		if err := upstream.HandleChatCompletion(pinnedCtx, rec, req, resolved, ""); err != nil {
			t.Fatalf("pinned HandleChatCompletion failed: %v", err)
		}
		if calls.Load() != 1 {
			t.Fatalf("expected exactly 1 call for pinned account (no hedge), got %d", calls.Load())
		}
	})

	// Pro primary uses CCPAHedgeDelayPro and hedge picker prefers healthy Ultra (inFlight <= 2) over idle Pro.
	t.Run("pro_primary_hedges_onto_ultra_with_low_inflight", func(t *testing.T) {
		CCPAHedgeDelayPro = 35 * time.Millisecond
		CCPAHedgeDelayUltra = 500 * time.Millisecond
		// Start with ultra-alt temporarily in 1s cooldown so primary acquires pro-slow@gmail.com;
		// once primary starts, clear cooldown on ultra-alt (with 1 inFlight <= 2) alongside idle pro-alt (0 inFlight),
		// proving hedge fires at Pro delay (35ms << 500ms) and prefers Ultra with inFlight <= 2!
		if err := saveTestGatewayAuth(SavedGatewayAuth{
			RoutingStrategy: StrategyRoundRobin,
			Accounts: []ConnectedAccount{
				{
					ID:          "p-slow",
					Provider:    ProviderAntigravity,
					Tier:        "Pro",
					Email:       "pro-slow@gmail.com",
					AccessToken: "ya29.pro-slow",
					ProjectID:   "proj-p1",
					ExpiresAt:   exp,
				},
				{
					ID:            "p-alt",
					Provider:      ProviderAntigravity,
					Tier:          "Pro",
					Email:         "pro-alt@gmail.com",
					AccessToken:   "ya29.pro-alt",
					ProjectID:     "proj-p2",
					CooldownUntil: time.Now().Unix() + 60,
					ExpiresAt:     exp,
				},
				{
					ID:            "u-alt",
					Provider:      ProviderAntigravity,
					Tier:          "Ultra",
					Email:         "ultra-alt@gmail.com",
					AccessToken:   "ya29.ultra-alt",
					ProjectID:     "proj-u3",
					CooldownUntil: time.Now().Unix() + 60,
					ExpiresAt:     exp,
				},
			},
		}); err != nil {
			t.Fatalf("SaveAuthConfig: %v", err)
		}
		resetTestAccountInFlight("pro-slow@gmail.com")
		resetTestAccountInFlight("pro-alt@gmail.com")
		resetTestAccountInFlight("ultra-alt@gmail.com")
		AcquireAccountInFlightWithRole("ultra-alt@gmail.com", "bg-task") // inFlight = 1 (<= 2)
		defer func() {
			resetTestAccountInFlight("pro-slow@gmail.com")
			resetTestAccountInFlight("pro-alt@gmail.com")
			resetTestAccountInFlight("ultra-alt@gmail.com")
		}()

		upstream := NewUpstream(GatewayConfig{CCPAEndpoint: DefaultCCPAEndpoint})
		upstream.client = &http.Client{
			Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
				authHdr := r.Header.Get("Authorization")
				if strings.Contains(authHdr, "ya29.pro-slow") {
					// Unlock both pro-alt (0 inFlight) and ultra-alt (1 inFlight <= 2) before 35ms hedge timer fires
					_ = saveTestGatewayAuth(SavedGatewayAuth{
						RoutingStrategy: StrategyRoundRobin,
						Accounts: []ConnectedAccount{
							{ID: "p-slow", Provider: ProviderAntigravity, Tier: "Pro", Email: "pro-slow@gmail.com", AccessToken: "ya29.pro-slow", ProjectID: "proj-p1", ExpiresAt: exp},
							{ID: "p-alt", Provider: ProviderAntigravity, Tier: "Pro", Email: "pro-alt@gmail.com", AccessToken: "ya29.pro-alt", ProjectID: "proj-p2", ExpiresAt: exp},
							{ID: "u-alt", Provider: ProviderAntigravity, Tier: "Ultra", Email: "ultra-alt@gmail.com", AccessToken: "ya29.ultra-alt", ProjectID: "proj-u3", ExpiresAt: exp},
						},
					})
					return &http.Response{
						StatusCode: http.StatusOK,
						Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
						Body:       &stalledTrackingBody{ctx: r.Context()},
					}, nil
				}
				sse := "data: {\"response\":{\"modelVersion\":\"gemini-3.8-flash\",\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"ultra-preferred-hedge\"}]},\"finishReason\":\"STOP\"}]}}\n\ndata: [DONE]\n\n"
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
					Body:       io.NopCloser(strings.NewReader(sse)),
				}, nil
			}),
		}

		start := time.Now()
		rec := httptest.NewRecorder()
		resolved := NewResolver().Resolve("antigravity/gemini-4-carbon-high", nil)
		req := OpenAIChatRequest{
			Model:    "antigravity/gemini-4-carbon-high",
			Messages: []OpenAIChatMessage{{Role: "user", Content: json.RawMessage(`"pro hedge test"`)}},
		}
		if err := upstream.HandleChatCompletion(context.Background(), rec, req, resolved, ""); err != nil {
			t.Fatalf("pro hedge HandleChatCompletion failed: %v", err)
		}
		elapsed := time.Since(start)
		if elapsed >= 300*time.Millisecond {
			t.Fatalf("expected Pro hedge delay (~35ms, not Ultra 500ms), took %v", elapsed)
		}
		if gotAcc := rec.Header().Get("X-Omni-Gateway-Account"); gotAcc != "ultra-alt@gmail.com" {
			t.Fatalf("expected hedge to prefer ultra-alt@gmail.com (inFlight=1<=2) over pro-alt@gmail.com, got %q", gotAcc)
		}
	})
}

func TestTierHedger_PreCompressionStreamCacheKeyMatch(t *testing.T) {
	tmpCfg := filepath.Join(t.TempDir(), "gateway_config.json")
	t.Setenv("GATEWAY_CONFIG_FILE", tmpCfg)
	resetAuthCacheForTest(t)
	clearTestCCPACaches()
	GetGlobalCache().Clear()
	GetGlobalCache().SetEnabled(true)
	t.Cleanup(func() {
		GetGlobalCache().Clear()
		resetAuthCacheForTest(t)
		clearTestCCPACaches()
	})

	var upstreamHits atomic.Int32
	mockCCPA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamHits.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"response\":{\"modelVersion\":\"gemini-3-flash\",\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"Cached stream answer!\"}]},\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":12,\"candidatesTokenCount\":4,\"totalTokenCount\":16}}}\n\ndata: [DONE]\n\n")
	}))
	defer mockCCPA.Close()

	if err := saveTestGatewayAuth(SavedGatewayAuth{
		RoutingStrategy: StrategyRoundRobin,
		CompressionConfig: &CompressionConfig{
			Mode:            CompressionLite,
			CompressCode:    false,
			MaxHistoryTurns: 10,
			EnableGzip:      true,
		},
		Accounts: []ConnectedAccount{
			{
				ID:          "cache-acc-1",
				Provider:    ProviderAntigravity,
				Tier:        "Pro",
				Email:       "cachetest@gmail.com",
				AccessToken: "ya29.cachetest",
				ProjectID:   "proj-cache",
				ExpiresAt:   time.Now().Add(2 * time.Hour).Unix(),
			},
		},
	}); err != nil {
		t.Fatalf("SaveAuthConfig: %v", err)
	}

	cfg := GatewayConfig{CCPAEndpoint: mockCCPA.URL}
	srv := NewServer(cfg, NewResolver(), NewUpstream(cfg))
	handler := srv.Handler()

	// Prompt deliberately contains redundant whitespace and blank lines that CompressRequest trims,
	// reproducing the exact Wave-2 pre- vs post-compression SHA-256 key mismatch!
	rawStreamPayload := `{"model":"antigravity/gemini-3-flash","temperature":0,"stream":true,"messages":[{"role":"user","content":"   Explain    quicksort   in   one   line  \n\n\n\n   please   "}]}`

	// 1. First stream:true request -> MISS, populates ResponseCache under pre-compression key.
	req1 := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(rawStreamPayload))
	req1.Header.Set("Content-Type", "application/json")
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusOK {
		t.Fatalf("call #1 status=%d body=%s", rec1.Code, rec1.Body.String())
	}
	if got := rec1.Header().Get("X-Omni-Gateway-Cache"); got != "MISS" {
		t.Fatalf("call #1 X-Omni-Gateway-Cache=%q, want MISS", got)
	}
	if upstreamHits.Load() != 1 {
		t.Fatalf("expected 1 upstream hit after call #1, got %d", upstreamHits.Load())
	}

	// 2. Second identical stream:true request -> MUST be X-Omni-Gateway-Cache: HIT with 0 new upstream calls!
	req2 := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(rawStreamPayload))
	req2.Header.Set("Content-Type", "application/json")
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("call #2 status=%d body=%s", rec2.Code, rec2.Body.String())
	}
	if got := rec2.Header().Get("X-Omni-Gateway-Cache"); got != "HIT" {
		t.Fatalf("call #2 (stream:true) X-Omni-Gateway-Cache=%q, want HIT", got)
	}
	if upstreamHits.Load() != 1 {
		t.Fatalf("expected 0 new upstream hits on cached stream:true call #2, got %d", upstreamHits.Load())
	}
	if !strings.Contains(rec2.Body.String(), "Cached stream answer!") {
		t.Fatalf("call #2 missing cached content: %s", rec2.Body.String())
	}

	// 3. Third identical prompt with stream:false -> MUST ALSO hit the exact same SHA-256 cache entry!
	rawNonStreamPayload := strings.Replace(rawStreamPayload, `"stream":true`, `"stream":false`, 1)
	req3 := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(rawNonStreamPayload))
	req3.Header.Set("Content-Type", "application/json")
	rec3 := httptest.NewRecorder()
	handler.ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusOK {
		t.Fatalf("call #3 status=%d body=%s", rec3.Code, rec3.Body.String())
	}
	if got := rec3.Header().Get("X-Omni-Gateway-Cache"); got != "HIT" {
		t.Fatalf("call #3 (stream:false) X-Omni-Gateway-Cache=%q, want HIT", got)
	}
	if upstreamHits.Load() != 1 {
		t.Fatalf("expected upstreamHits to remain 1, got %d", upstreamHits.Load())
	}
}

func TestTierHedger_ProactiveQuotaResetTimeRateLimitSync(t *testing.T) {
	tmpCfg := filepath.Join(t.TempDir(), "gateway_config.json")
	t.Setenv("GATEWAY_CONFIG_FILE", tmpCfg)
	resetAuthCacheForTest(t)
	ClearAccountModelRateLimits("")
	t.Cleanup(func() {
		resetAuthCacheForTest(t)
		ClearAccountModelRateLimits("")
	})

	// 1. Verify prefix normalization & Opus thinking <-> non-thinking mirroring.
	futureReset := time.Now().UTC().Add(45 * time.Minute).Format(time.RFC3339)
	var claudeReplenished atomic.Bool

	mockQuotaSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "loadCodeAssist"):
			_, _ = io.WriteString(w, `{"currentTier":{"id":"g1-ultra-tier","name":"Google AI Ultra"},"cloudaicompanionProject":"proj-rl"}`)
		case strings.Contains(r.URL.Path, "retrieveUserQuotaSummary"):
			_, _ = io.WriteString(w, `{"groups":[]}`)
		case strings.Contains(r.URL.Path, "fetchAvailableModels"):
			_, _ = io.WriteString(w, `{"models":{}}`)
		default:
			if !claudeReplenished.Load() {
				// Proto3 omits remainingFraction when 0!
				_, _ = fmt.Fprintf(w, `{
					"models": {
						"claude-sonnet-4-6": {"resetTime": %q},
						"claude-opus-4-6-thinking": {"resetTime": %q},
						"gemini-3.8-flash-high": {"remainingFraction": 0.98, "resetTime": %q}
					}
				}`, futureReset, futureReset, futureReset)
				return
			}
			_, _ = fmt.Fprintf(w, `{
				"models": {
					"claude-sonnet-4-6": {"remainingFraction": 0.92, "resetTime": %q},
					"claude-opus-4-6-thinking": {"remainingFraction": 0.85, "resetTime": %q},
					"gemini-3.8-flash-high": {"remainingFraction": 0.98, "resetTime": %q}
				}
			}`, futureReset, futureReset, futureReset)
		}
	}))
	defer mockQuotaSrv.Close()

	origLoad := CCPALoadCodeAssistEndpoint
	origSum := CCPARetrieveUserQuotaSummaryEndpoint
	origQuota := CCPARetrieveUserQuotaEndpoint
	origFetch := CCPAFetchAvailableModelsEndpoint
	CCPALoadCodeAssistEndpoint = mockQuotaSrv.URL + "/v1internal:loadCodeAssist"
	CCPARetrieveUserQuotaSummaryEndpoint = mockQuotaSrv.URL + "/v1internal:retrieveUserQuotaSummary"
	CCPARetrieveUserQuotaEndpoint = mockQuotaSrv.URL + "/v1internal:retrieveUserQuota"
	CCPAFetchAvailableModelsEndpoint = mockQuotaSrv.URL + "/v1internal:fetchAvailableModels"
	defer func() {
		CCPALoadCodeAssistEndpoint = origLoad
		CCPARetrieveUserQuotaSummaryEndpoint = origSum
		CCPARetrieveUserQuotaEndpoint = origQuota
		CCPAFetchAvailableModelsEndpoint = origFetch
	}()

	acc := ConnectedAccount{
		ID:          "rl-sync-1",
		Provider:    ProviderAntigravity,
		Email:       "rlsync@gmail.com",
		AccessToken: "ya29.rlsync",
		ProjectID:   "proj-rl",
		ExpiresAt:   time.Now().Add(2 * time.Hour).Unix(),
	}
	if err := saveTestGatewayAuth(SavedGatewayAuth{Accounts: []ConnectedAccount{acc}}); err != nil {
		t.Fatalf("SaveAuthConfig: %v", err)
	}

	var d1 AccountQuotaDetail
	inspectAntigravityAccount(context.Background(), &acc, &d1, "reset")

	// Exhausted models with omitted remainingFraction MUST be marked rate-limited with and without antigravity/ prefix.
	for _, mod := range []string{
		"claude-sonnet-4-6",
		"antigravity/claude-sonnet-4-6",
		"claude-opus-4-6-thinking",
		"antigravity/claude-opus-4-6-thinking",
		"claude-opus-4-6",
		"antigravity/claude-opus-4-6",
	} {
		if !IsAccountModelRateLimited(acc.Email, mod) {
			t.Fatalf("expected IsAccountModelRateLimited(%q, %q) == true after proto3 zero remainingFraction sync", acc.Email, mod)
		}
	}
	// Healthy Gemini model (remainingFraction=0.98) MUST NOT be rate-limited.
	if IsAccountModelRateLimited(acc.Email, "gemini-3.8-flash-high") {
		t.Fatalf("expected gemini-3.8-flash-high (0.98 remaining) NOT to be rate-limited")
	}

	// Simulate quota replenishment (> 0.01) -> rate limits MUST be cleared immediately.
	claudeReplenished.Store(true)
	var d2 AccountQuotaDetail
	inspectAntigravityAccount(context.Background(), &acc, &d2, "reset")

	for _, mod := range []string{
		"claude-sonnet-4-6",
		"antigravity/claude-sonnet-4-6",
		"claude-opus-4-6-thinking",
		"antigravity/claude-opus-4-6-thinking",
		"claude-opus-4-6",
		"antigravity/claude-opus-4-6",
	} {
		if IsAccountModelRateLimited(acc.Email, mod) {
			t.Fatalf("expected IsAccountModelRateLimited(%q, %q) == false after quota replenished > 0.01", acc.Email, mod)
		}
	}
}
