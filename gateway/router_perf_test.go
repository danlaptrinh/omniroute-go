package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func cleanupInFlightForEmails(emails []string) {
	for _, em := range emails {
		for GetAccountInFlight(em) > 0 {
			ReleaseAccountInFlight(em)
		}
	}
}

func TestConcurrentSelectProviderAccountForModelNoPileUp(t *testing.T) {
	tmpCfg := filepath.Join(t.TempDir(), "gateway_config.json")
	t.Setenv("GATEWAY_CONFIG_FILE", tmpCfg)
	resetAuthCacheForTest(t)
	ClearAccountModelRateLimits("")

	const numAccounts = 9
	const concurrentCalls = 30

	emails := make([]string, numAccounts)
	for i := 0; i < numAccounts; i++ {
		em := fmt.Sprintf("router-acc-%d@example.com", i)
		emails[i] = em
		if _, err := UpsertConnectedAccount(ConnectedAccount{
			Provider:    ProviderAntigravity,
			Email:       em,
			AccessToken: fmt.Sprintf("ya29.router-tok-%d", i),
			ProjectID:   fmt.Sprintf("proj-%d", i),
			ExpiresAt:   time.Now().Unix() + 3600,
		}); err != nil {
			t.Fatalf("UpsertConnectedAccount(%s) failed: %v", em, err)
		}
	}

	t.Cleanup(func() {
		cleanupInFlightForEmails(emails)
		ClearAccountModelRateLimits("")
		FlushPendingConfigSave()
		resetAuthCacheForTest(t)
	})

	strategies := []RoutingStrategy{
		StrategyRoundRobin,
		StrategyLeastUsed,
		StrategyFillFirst,
		StrategyP2C,
	}

	for _, strat := range strategies {
		strat := strat
		t.Run(string(strat), func(t *testing.T) {
			if _, err := SetRoutingStrategy(strat); err != nil {
				t.Fatalf("SetRoutingStrategy(%s) failed: %v", strat, err)
			}
			cleanupInFlightForEmails(emails)

			startBarrier := make(chan struct{})
			var wg sync.WaitGroup
			var mu sync.Mutex
			counts := make(map[string]int, numAccounts)
			errs := make([]error, concurrentCalls)

			for g := 0; g < concurrentCalls; g++ {
				wg.Add(1)
				go func(idx int) {
					defer wg.Done()
					<-startBarrier
					acc, err := SelectProviderAccountForModel(
						context.Background(),
						ProviderAntigravity,
						"antigravity/gemini-4-carbon-high",
					)
					if err != nil {
						errs[idx] = err
						return
					}
					mu.Lock()
					counts[acc.Email]++
					mu.Unlock()
				}(g)
			}

			close(startBarrier)
			wg.Wait()

			for idx, err := range errs {
				if err != nil {
					t.Fatalf("goroutine %d SelectProviderAccountForModel failed: %v", idx, err)
				}
			}

			if len(counts) != numAccounts {
				t.Fatalf("strategy %s only used %d/%d accounts: %+v", strat, len(counts), numAccounts, counts)
			}

			// 30 calls across 9 accounts must distribute as 3 or 4 per account (3*6 + 4*3 = 30)
			total := 0
			for _, em := range emails {
				c := counts[em]
				total += c
				if c < 3 || c > 4 {
					t.Fatalf("strategy %s microsecond pile-up detected on %s: got %d calls (want 3 or 4), full=%+v", strat, em, c, counts)
				}
			}
			if total != concurrentCalls {
				t.Fatalf("expected %d total selections, got %d", concurrentCalls, total)
			}
		})
	}
}

func TestAcquireProviderAccountEliminatesUnevenInFlightTOCTOU(t *testing.T) {
	tmpCfg := filepath.Join(t.TempDir(), "gateway_config.json")
	t.Setenv("GATEWAY_CONFIG_FILE", tmpCfg)
	resetAuthCacheForTest(t)
	ResetCCPAModelCaches()
	ClearAccountModelRateLimits("")
	GetGlobalCache().Clear()

	const numAccounts = 9
	emails := make([]string, numAccounts)
	tokenToEmail := make(map[string]string, numAccounts)
	for i := 0; i < numAccounts; i++ {
		em := fmt.Sprintf("burst-acc-%d@example.com", i)
		tok := fmt.Sprintf("ya29.burst-tok-%d", i)
		emails[i] = em
		tokenToEmail["Bearer "+tok] = em
		if _, err := UpsertConnectedAccount(ConnectedAccount{
			Provider:    ProviderAntigravity,
			Email:       em,
			AccessToken: tok,
			ProjectID:   fmt.Sprintf("proj-burst-%d", i),
			ExpiresAt:   time.Now().Unix() + 3600,
		}); err != nil {
			t.Fatalf("UpsertConnectedAccount(%s) failed: %v", em, err)
		}
	}
	if _, err := SetRoutingStrategy(StrategyRoundRobin); err != nil {
		t.Fatalf("SetRoutingStrategy failed: %v", err)
	}

	t.Cleanup(func() {
		cleanupInFlightForEmails(emails)
		ResetCCPAModelCaches()
		ClearAccountModelRateLimits("")
		GetGlobalCache().Clear()
		FlushPendingConfigSave()
		resetAuthCacheForTest(t)
	})

	// Simulate uneven in-flight state before burst: accounts 1..8 have inFlight=1, only account 0 has inFlight=0.
	// Without atomic select+acquire, 9 simultaneous requests would all see leastBusy=[acc-0] and pile all 9 onto acc-0!
	for i := 1; i < numAccounts; i++ {
		AcquireAccountInFlightWithRole(emails[i], "preexisting")
	}

	var mu sync.Mutex
	callsPerAccount := make(map[string]int, numAccounts)
	mockCCPA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		em := tokenToEmail[strings.TrimSpace(r.Header.Get("Authorization"))]
		mu.Lock()
		callsPerAccount[em]++
		mu.Unlock()
		// Hold each request briefly so all 9 burst requests overlap in-flight
		time.Sleep(25 * time.Millisecond)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: {\"response\":{\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"ok\"}]}}]}}\n\ndata: [DONE]\n\n"))
	}))
	defer mockCCPA.Close()

	upstream := NewUpstream(GatewayConfig{CCPAEndpoint: mockCCPA.URL})
	resolved := ResolveModel("antigravity/gemini-3-flash", nil)

	const burst = 9
	startBarrier := make(chan struct{})
	var wg sync.WaitGroup
	errs := make([]error, burst)

	for i := 0; i < burst; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			req := OpenAIChatRequest{
				Model: "antigravity/gemini-3-flash",
				Messages: []OpenAIChatMessage{
					{Role: "user", Content: json.RawMessage(fmt.Sprintf(`"burst prompt %d"`, idx))},
				},
			}
			rec := httptest.NewRecorder()
			<-startBarrier
			errs[idx] = upstream.HandleChatCompletion(context.Background(), rec, req, resolved, "")
		}(i)
	}

	close(startBarrier)
	wg.Wait()

	for i := 1; i < numAccounts; i++ {
		ReleaseAccountInFlight(emails[i])
	}

	for idx, err := range errs {
		if err != nil {
			t.Fatalf("burst request %d failed: %v", idx, err)
		}
	}

	// Account 0 had inFlight=0 while 1..8 had inFlight=1:
	// First arrival claims acc-0 (bringing all 9 accounts to inFlight=1), then remaining 8 spread across 8 distinct accounts (at most 2 calls on acc-0).
	if gotAcc0 := callsPerAccount[emails[0]]; gotAcc0 > 2 {
		t.Fatalf("TOCTOU pile-up on %s: got %d burst calls (want <= 2), full=%+v", emails[0], gotAcc0, callsPerAccount)
	}
	if len(callsPerAccount) < 8 {
		t.Fatalf("expected burst to spread across at least 8 accounts, got %d: %+v", len(callsPerAccount), callsPerAccount)
	}
}

func TestModelQuotaAwareSelectionBypassesRateLimitedModelOnly(t *testing.T) {
	tmpCfg := filepath.Join(t.TempDir(), "gateway_config.json")
	t.Setenv("GATEWAY_CONFIG_FILE", tmpCfg)
	resetAuthCacheForTest(t)
	ResetCCPAModelCaches()
	ClearAccountModelRateLimits("")
	GetGlobalCache().Clear()

	const numAccounts = 9
	emails := make([]string, numAccounts)
	for i := 0; i < numAccounts; i++ {
		em := fmt.Sprintf("quota-acc-%d@example.com", i)
		emails[i] = em
		if _, err := UpsertConnectedAccount(ConnectedAccount{
			Provider:    ProviderAntigravity,
			Email:       em,
			AccessToken: fmt.Sprintf("ya29.quota-tok-%d", i),
			ProjectID:   fmt.Sprintf("proj-quota-%d", i),
			ExpiresAt:   time.Now().Unix() + 3600,
		}); err != nil {
			t.Fatalf("UpsertConnectedAccount(%s) failed: %v", em, err)
		}
	}

	t.Cleanup(func() {
		cleanupInFlightForEmails(emails)
		ResetCCPAModelCaches()
		ClearAccountModelRateLimits("")
		GetGlobalCache().Clear()
		FlushPendingConfigSave()
		resetAuthCacheForTest(t)
	})

	rateLimitedAcc := emails[0]
	RecordAccountModelRateLimit(rateLimitedAcc, "claude-opus-4-6-thinking", 2*time.Minute)
	RecordAccountModelRateLimit(rateLimitedAcc, "antigravity/gemini-4-carbon-high", 2*time.Minute)

	// Verify helper predicates across Opus, Carbon-High, Argon-High, Sonnet, and Gemini-3-Flash
	if !IsAccountModelRateLimited(rateLimitedAcc, "claude-opus-4-6-thinking") {
		t.Fatalf("expected %s to be rate-limited on claude-opus-4-6-thinking", rateLimitedAcc)
	}
	if !IsAccountModelRateLimited(rateLimitedAcc, "antigravity/gemini-4-carbon-high") {
		t.Fatalf("expected %s to be rate-limited for antigravity/gemini-4-carbon-high", rateLimitedAcc)
	}
	if !IsAccountModelRateLimited(rateLimitedAcc, "antigravity/gemini-4-argon-high") {
		t.Fatalf("expected %s to be rate-limited for antigravity/gemini-4-argon-high", rateLimitedAcc)
	}
	if IsAccountModelRateLimited(rateLimitedAcc, "gemini-3-flash") {
		t.Fatalf("expected %s NOT to be rate-limited on gemini-3-flash", rateLimitedAcc)
	}
	if IsAccountModelRateLimited(rateLimitedAcc, "antigravity/gemini-3-flash") {
		t.Fatalf("expected %s NOT to be rate-limited on antigravity/gemini-3-flash", rateLimitedAcc)
	}

	// 1. Opus and Carbon-High MUST completely bypass rateLimitedAcc (0 selections out of 24 each)
	for _, modelHint := range []string{"antigravity/claude-opus-4-6-thinking", "antigravity/gemini-4-carbon-high"} {
		counts := make(map[string]int, numAccounts)
		for i := 0; i < 24; i++ {
			acc, err := SelectProviderAccountForModel(context.Background(), ProviderAntigravity, modelHint)
			if err != nil {
				t.Fatalf("SelectProviderAccountForModel(%s) failed: %v", modelHint, err)
			}
			counts[acc.Email]++
		}
		if counts[rateLimitedAcc] != 0 {
			t.Fatalf("expected %s to be bypassed for %s, but selected %d times (counts=%+v)", rateLimitedAcc, modelHint, counts[rateLimitedAcc], counts)
		}
		// Remaining 8 healthy accounts should each get exactly 24 / 8 = 3 calls!
		for i := 1; i < numAccounts; i++ {
			if counts[emails[i]] != 3 {
				t.Fatalf("expected healthy account %s to receive 3 calls for %s, got %d (counts=%+v)", emails[i], modelHint, counts[emails[i]], counts)
			}
		}
	}

	// 2. Meanwhile, gemini-3-flash MUST still treat rateLimitedAcc as 100% eligible (27 / 9 = 3 calls each)
	flashCounts := make(map[string]int, numAccounts)
	for i := 0; i < 27; i++ {
		acc, err := SelectProviderAccountForModel(context.Background(), ProviderAntigravity, "antigravity/gemini-3-flash")
		if err != nil {
			t.Fatalf("SelectProviderAccountForModel(gemini-3-flash) failed: %v", err)
		}
		flashCounts[acc.Email]++
	}
	if flashCounts[rateLimitedAcc] != 3 {
		t.Fatalf("expected %s to remain eligible for gemini-3-flash (want 3 calls), got %d (counts=%+v)", rateLimitedAcc, flashCounts[rateLimitedAcc], flashCounts)
	}

	// 3. Verify TTL expiration automatically restores Opus eligibility
	RecordAccountModelRateLimit(rateLimitedAcc, "claude-opus-4-6-thinking", 15*time.Millisecond)
	time.Sleep(25 * time.Millisecond)
	if IsAccountModelRateLimited(rateLimitedAcc, "claude-opus-4-6-thinking") {
		t.Fatalf("expected per-account model rate limit on %s to expire after TTL", rateLimitedAcc)
	}
}

func TestEndToEndCCPA429AutoRecordsPerAccountModelCooldownForNextRequest(t *testing.T) {
	tmpCfg := filepath.Join(t.TempDir(), "gateway_config.json")
	t.Setenv("GATEWAY_CONFIG_FILE", tmpCfg)
	resetAuthCacheForTest(t)
	ResetCCPAModelCaches()
	ClearAccountModelRateLimits("")
	GetGlobalCache().Clear()

	t.Cleanup(func() {
		ResetCCPAModelCaches()
		ClearAccountModelRateLimits("")
		GetGlobalCache().Clear()
		FlushPendingConfigSave()
		resetAuthCacheForTest(t)
	})

	for _, a := range []ConnectedAccount{
		{
			Provider:    ProviderAntigravity,
			Email:       "opus-429@example.com",
			AccessToken: "ya29.opus-429-tok",
			ProjectID:   "proj-429",
			ExpiresAt:   time.Now().Unix() + 3600,
		},
		{
			Provider:    ProviderAntigravity,
			Email:       "opus-fresh@example.com",
			AccessToken: "ya29.opus-fresh-tok",
			ProjectID:   "proj-fresh",
			ExpiresAt:   time.Now().Unix() + 3600,
		},
	} {
		if _, err := UpsertConnectedAccount(a); err != nil {
			t.Fatalf("UpsertConnectedAccount(%s) failed: %v", a.Email, err)
		}
	}

	var opus429Hits atomic.Int32
	mockCCPA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload ccpaRequestPayload
		_ = json.NewDecoder(r.Body).Decode(&payload)
		if payload.Model == "gemini-4-carbon-high" || payload.Model == "gemini-4-carbon" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if strings.TrimSpace(r.Header.Get("Authorization")) == "Bearer ya29.opus-429-tok" && payload.Model == "claude-opus-4-6-thinking" {
			opus429Hits.Add(1)
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"code":429,"status":"RESOURCE_EXHAUSTED"}}`))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, "data: {\"response\":{\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"ok-%s\"}]}}]}}\n\ndata: [DONE]\n\n", payload.Model)
	}))
	defer mockCCPA.Close()

	upstream := NewUpstream(GatewayConfig{CCPAEndpoint: mockCCPA.URL})
	resolved := ResolveModel("antigravity/claude-opus-4-6-thinking", nil)

	// Seed 1st request pinned to opus-429@example.com so it discovers the 429 on claude-opus-4-6-thinking (no Gemini stepdown under Zero Stepdown Policy)
	seedReq := OpenAIChatRequest{
		Model:    "antigravity/claude-opus-4-6-thinking",
		Messages: []OpenAIChatMessage{{Role: "user", Content: json.RawMessage(`"seed 429 discovery"`)}},
	}
	seedRec := httptest.NewRecorder()
	if err := upstream.HandleChatCompletion(WithPinnedAccount(context.Background(), "opus-429@example.com"), seedRec, seedReq, resolved, ""); err == nil {
		t.Fatalf("expected HTTP 429 error on pinned exhausted opus-429@example.com without Gemini stepdown")
	}
	if opus429Hits.Load() != 1 {
		t.Fatalf("expected 1 initial 429 hit on opus-429@example.com, got %d", opus429Hits.Load())
	}

	// Next 6 UNPINNED Claude Opus requests must all bypass opus-429@example.com and land directly on opus-fresh@example.com!
	for i := 0; i < 6; i++ {
		req := OpenAIChatRequest{
			Model:    "antigravity/claude-opus-4-6-thinking",
			Messages: []OpenAIChatMessage{{Role: "user", Content: json.RawMessage(fmt.Sprintf(`"unpinned opus req %d"`, i))}},
		}
		rec := httptest.NewRecorder()
		if err := upstream.HandleChatCompletion(context.Background(), rec, req, resolved, ""); err != nil {
			t.Fatalf("unpinned request %d failed: %v", i, err)
		}
		if gotAcc := rec.Header().Get("X-Omni-Gateway-Account"); gotAcc != "opus-fresh@example.com" {
			t.Fatalf("unpinned request %d routed to %q, want opus-fresh@example.com", i, gotAcc)
		}
		if gotMod := rec.Header().Get("X-Omni-Gateway-Resolved-Model"); gotMod != "claude-opus-4-6-thinking" {
			t.Fatalf("unpinned request %d resolved to %q, want claude-opus-4-6-thinking", i, gotMod)
		}
	}
	// Also verify direct antigravity/claude-opus-4-6-thinking requests bypass opus-429@example.com
	opusResolved := ResolveModel("antigravity/claude-opus-4-6-thinking", nil)
	for i := 0; i < 4; i++ {
		req := OpenAIChatRequest{
			Model:    "antigravity/claude-opus-4-6-thinking",
			Messages: []OpenAIChatMessage{{Role: "user", Content: json.RawMessage(fmt.Sprintf(`"direct opus req %d"`, i))}},
		}
		rec := httptest.NewRecorder()
		if err := upstream.HandleChatCompletion(context.Background(), rec, req, opusResolved, ""); err != nil {
			t.Fatalf("direct opus request %d failed: %v", i, err)
		}
		if gotAcc := rec.Header().Get("X-Omni-Gateway-Account"); gotAcc != "opus-fresh@example.com" {
			t.Fatalf("direct opus request %d routed to %q, want opus-fresh@example.com", i, gotAcc)
		}
		if gotMod := rec.Header().Get("X-Omni-Gateway-Resolved-Model"); gotMod != "claude-opus-4-6-thinking" {
			t.Fatalf("direct opus request %d resolved to %q, want claude-opus-4-6-thinking", i, gotMod)
		}
	}
	if opus429Hits.Load() != 1 {
		t.Fatalf("expected 0 extra 429 hops after per-account model cooldown recorded, got total=%d", opus429Hits.Load())
	}

	// Verify 7 accounts with 8s-28s long-stream AvgLatencyMs are NOT starved by 2 accounts with 1.7s Flash AvgLatencyMs
	for i := 0; i < 9; i++ {
		avgLat := int64(18000)
		if i < 2 {
			avgLat = 1787
		}
		_, _ = UpsertConnectedAccount(ConnectedAccount{
			Provider:     ProviderAntigravity,
			Email:        fmt.Sprintf("prod-lat-%d@example.com", i),
			AccessToken:  fmt.Sprintf("ya29.prod-lat-%d", i),
			ProjectID:    fmt.Sprintf("proj-lat-%d", i),
			AvgLatencyMs: avgLat,
			ExpiresAt:    time.Now().Unix() + 3600,
		})
	}
	latCounts := make(map[string]int, 9)
	for i := 0; i < 45; i++ {
		acc, err := SelectProviderAccountForModel(context.Background(), ProviderAntigravity, "antigravity/gemini-3-flash")
		if err != nil {
			t.Fatalf("SelectProviderAccountForModel failed: %v", err)
		}
		latCounts[acc.Email]++
	}
	for i := 0; i < 9; i++ {
		em := fmt.Sprintf("prod-lat-%d@example.com", i)
		if latCounts[em] < 4 || latCounts[em] > 5 {
			t.Fatalf("account %s (AvgLatencyMs>6000) was starved or piled up: got %d, counts=%+v", em, latCounts[em], latCounts)
		}
	}
}

func TestConcurrentRecordAPIKeyUsageZeroDiskLockContention(t *testing.T) {
	tmpCfg := filepath.Join(t.TempDir(), "gateway_config.json")
	t.Setenv("GATEWAY_CONFIG_FILE", tmpCfg)
	resetAuthCacheForTest(t)

	t.Cleanup(func() {
		FlushPendingConfigSave()
		resetAuthCacheForTest(t)
	})

	created, err := CreateAPIKey("High-Throughput-Bench-Key", false, true, true, false)
	if err != nil {
		t.Fatalf("CreateAPIKey failed: %v", err)
	}

	// Verify CreateAPIKey remains synchronous on disk immediately
	if _, statErr := os.Stat(tmpCfg); statErr != nil {
		t.Fatalf("expected CreateAPIKey to persist synchronously on disk: %v", statErr)
	}

	const concurrentUpdates = 100
	const tokensPerCall = 125

	startBarrier := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < concurrentUpdates; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-startBarrier
			if idx%2 == 0 {
				RecordAPIKeyUsage("Bearer "+created.Key, tokensPerCall)
			} else {
				RecordAPIKeyUsage(created.Key, tokensPerCall)
			}
		}(i)
	}

	start := time.Now()
	close(startBarrier)
	wg.Wait()
	elapsed := time.Since(start)

	if elapsed >= 50*time.Millisecond {
		t.Fatalf("100 concurrent RecordAPIKeyUsage calls took %v, expected < 50ms", elapsed)
	}

	keys := ListAPIKeys()
	if len(keys) != 1 {
		t.Fatalf("expected 1 API key, got %d", len(keys))
	}
	if keys[0].TotalRequests != concurrentUpdates {
		t.Fatalf("TotalRequests = %d, want %d", keys[0].TotalRequests, concurrentUpdates)
	}
	wantTokens := int64(concurrentUpdates * tokensPerCall)
	if keys[0].TotalTokens != wantTokens {
		t.Fatalf("TotalTokens = %d, want %d", keys[0].TotalTokens, wantTokens)
	}

	// Flush pending debounced write and verify persisted JSON on disk matches 100%
	FlushPendingConfigSave()
	resetAuthCacheForTest(t)
	reloaded := ListAPIKeys()
	if len(reloaded) != 1 || reloaded[0].TotalRequests != concurrentUpdates || reloaded[0].TotalTokens != wantTokens {
		t.Fatalf("persisted disk state mismatch after FlushPendingConfigSave: %+v", reloaded)
	}

	// Verify DeleteAPIKey remains synchronous on disk
	if err := DeleteAPIKey(created.ID); err != nil {
		t.Fatalf("DeleteAPIKey failed: %v", err)
	}
	resetAuthCacheForTest(t)
	if afterDel := ListAPIKeys(); len(afterDel) != 0 {
		t.Fatalf("expected 0 keys on disk immediately after synchronous DeleteAPIKey, got %d", len(afterDel))
	}
}

func BenchmarkSelectProviderAccountForModelParallel(b *testing.B) {
	tmpCfg := filepath.Join(b.TempDir(), "gateway_config.json")
	b.Setenv("GATEWAY_CONFIG_FILE", tmpCfg)
	configMu.Lock()
	cachedAuthCfg = nil
	cachedAuthPath = ""
	asyncFlushPending = false
	configMu.Unlock()
	ClearAccountModelRateLimits("")

	for i := 0; i < 9; i++ {
		_, _ = UpsertConnectedAccount(ConnectedAccount{
			Provider:    ProviderAntigravity,
			Email:       fmt.Sprintf("bench-%d@example.com", i),
			AccessToken: fmt.Sprintf("ya29.bench-%d", i),
			ProjectID:   fmt.Sprintf("proj-%d", i),
			ExpiresAt:   time.Now().Unix() + 3600,
		})
	}
	RecordAccountModelRateLimit("bench-0@example.com", "claude-opus-4-6-thinking", 5*time.Minute)

	ctx := context.Background()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = SelectProviderAccountForModel(ctx, ProviderAntigravity, "antigravity/gemini-4-carbon-high")
		}
	})
}

func BenchmarkRecordAPIKeyUsageParallel(b *testing.B) {
	tmpCfg := filepath.Join(b.TempDir(), "gateway_config.json")
	b.Setenv("GATEWAY_CONFIG_FILE", tmpCfg)
	configMu.Lock()
	cachedAuthCfg = nil
	cachedAuthPath = ""
	asyncFlushPending = false
	configMu.Unlock()

	k, err := CreateAPIKey("bench-key", false, true, true, false)
	if err != nil {
		b.Fatalf("CreateAPIKey failed: %v", err)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			RecordAPIKeyUsage(k.Key, 64)
		}
	})
	b.StopTimer()
	FlushPendingConfigSave()
}
