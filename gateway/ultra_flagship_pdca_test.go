package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func setupUltraFlagshipPDCATestEnv(t *testing.T) {
	t.Helper()
	tmpCfg := filepath.Join(t.TempDir(), "gateway_config.json")
	t.Setenv("GATEWAY_CONFIG_FILE", tmpCfg)
	resetAuthCacheForTest(t)
	ResetCCPAModelCaches()
	clearTestCCPACaches()
	ClearAccountModelRateLimits("")
	GetGlobalCache().Clear()
	ClearLogs()
	ResetPDCAState()
	validationIsolatedAccounts.Range(func(k, _ any) bool {
		validationIsolatedAccounts.Delete(k)
		return true
	})
	t.Cleanup(func() {
		ResetPDCAState()
		ResetCCPAModelCaches()
		clearTestCCPACaches()
		ClearAccountModelRateLimits("")
		GetGlobalCache().Clear()
		ClearLogs()
		validationIsolatedAccounts.Range(func(k, _ any) bool {
			validationIsolatedAccounts.Delete(k)
			return true
		})
		FlushPendingConfigSave()
		resetAuthCacheForTest(t)
	})
}

// 1. 0% Claude Ultra STILL serves Carbon/Argon via unthrottled gemini-3.8-flash-high [gemini-3.8-flash]
// without false lockout, and honestly records "antigravity/gemini-4-carbon-high ➔ antigravity/gemini-3.8-flash-high [gemini-3.8-flash]" in LastModel.
func TestZeroClaudeUltraStillServesCarbonArgonWithoutFalseLockout(t *testing.T) {
	setupUltraFlagshipPDCATestEnv(t)

	zeroClaude := 0
	gemini14 := 14
	gemini97 := 97
	exp := time.Now().Add(2 * time.Hour).Unix()

	if err := saveTestGatewayAuth(SavedGatewayAuth{
		RoutingStrategy: StrategyRoundRobin,
		Accounts: []ConnectedAccount{
			{
				ID:                  "u-ntky",
				Provider:            ProviderAntigravity,
				Tier:                "Ultra",
				Email:               "ntky152022@gmail.com",
				AccessToken:         "ya29.ultra-ntky",
				ProjectID:           "aicode-consumers",
				ExpiresAt:           exp,
				WeeklyGeminiPercent: &gemini14,
				WeeklyClaudePercent: &zeroClaude,
			},
			{
				ID:                  "p-advn",
				Provider:            ProviderAntigravity,
				Tier:                "Pro",
				Email:               "advietnam789@gmail.com",
				AccessToken:         "ya29.pro-advn",
				ProjectID:           "aicode-consumers",
				ExpiresAt:           exp,
				WeeklyGeminiPercent: &gemini97,
				WeeklyClaudePercent: &zeroClaude,
			},
		},
	}); err != nil {
		t.Fatalf("saveTestGatewayAuth: %v", err)
	}
	RecordAccountModelRateLimit("ntky152022@gmail.com", "claude-opus-4-6-thinking", 6*24*time.Hour)
	RecordAccountModelRateLimit("ntky152022@gmail.com", "claude-sonnet-4-6", 6*24*time.Hour)

	if IsAccountModelRateLimited("ntky152022@gmail.com", "antigravity/gemini-4-carbon-high") {
		t.Fatalf("0%% Claude quota must NEVER mark Ultra account rate-limited for gemini-4-carbon-high")
	}
	if IsAccountModelRateLimited("ntky152022@gmail.com", "antigravity/gemini-4-argon-high") {
		t.Fatalf("0%% Claude quota must NEVER mark Ultra account rate-limited for gemini-4-argon-high")
	}

	var proTouched atomic.Int32
	upstream := NewUpstream(GatewayConfig{CCPAEndpoint: DefaultCCPAEndpoint})
	upstream.client = &http.Client{
		Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			raw, _ := io.ReadAll(r.Body)
			var payload ccpaRequestPayload
			_ = json.Unmarshal(raw, &payload)
			auth := strings.TrimSpace(r.Header.Get("Authorization"))
			if strings.Contains(auth, "ya29.pro-advn") {
				proTouched.Add(1)
			}
			// Mirror live CCPA: gemini-4-carbon-high & gemini-4-argon-high return 404; gemini-3.8-flash-high returns 200 OK [gemini-3.8-flash]
			if strings.HasPrefix(payload.Model, "gemini-4-") {
				return &http.Response{
					StatusCode: http.StatusNotFound,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(`{"error":{"code":404,"message":"Requested entity was not found."}}`)),
				}, nil
			}
			if payload.Model == "gemini-3.8-flash-high" {
				sse := "data: {\"response\":{\"modelVersion\":\"gemini-3.8-flash\",\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"ultra-unthrottled-carbon\"}]},\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":10,\"candidatesTokenCount\":6,\"totalTokenCount\":16}}}\n\ndata: [DONE]\n\n"
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
					Body:       io.NopCloser(strings.NewReader(sse)),
				}, nil
			}
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"error":{"code":404,"message":"not found"}}`)),
			}, nil
		}),
	}

	rec := httptest.NewRecorder()
	resolved := ResolveModel("antigravity/gemini-4-carbon-high", nil)
	req := OpenAIChatRequest{
		Model:    "antigravity/gemini-4-carbon-high",
		Messages: []OpenAIChatMessage{{Role: "user", Content: json.RawMessage(`"Solve complex architecture"`)}},
	}
	if err := upstream.HandleChatCompletion(context.Background(), rec, req, resolved, ""); err != nil {
		t.Fatalf("HandleChatCompletion failed: %v", err)
	}
	if gotAcc := rec.Header().Get("X-Omni-Gateway-Account"); gotAcc != "ntky152022@gmail.com" {
		t.Fatalf("expected Ultra ntky152022@gmail.com to serve Carbon High, got %q", gotAcc)
	}
	if gotStepdown := rec.Header().Get("X-Omni-Gateway-Tier-Stepdown"); gotStepdown != "" {
		t.Fatalf("expected zero stepdown to Pro when Ultra Gemini > 0%%, got %q", gotStepdown)
	}
	if proTouched.Load() != 0 {
		t.Fatalf("expected Pro account to receive 0 requests for Carbon High while Ultra is healthy, got %d", proTouched.Load())
	}

	saved := LoadSavedAuthConfig()
	for _, a := range saved.Accounts {
		if a.Email == "ntky152022@gmail.com" {
			wantLastModel := "antigravity/gemini-4-carbon-high ➔ antigravity/gemini-3.8-flash-high [gemini-3.8-flash]"
			if a.LastModel != wantLastModel {
				t.Fatalf("expected honest LastModel %q, got %q", wantLastModel, a.LastModel)
			}
		}
	}
}

// 2. WeeklyClaudePercent remaining% semantics: 100% fresh is NOT exhausted while 0% IS exhausted,
// and Bucket B routes 0%-Claude fallback exclusively to Pro (never 0%-Claude Ultra).
func TestWeeklyClaudePercentRemainingSemanticsAndBucketB(t *testing.T) {
	setupUltraFlagshipPDCATestEnv(t)

	fresh100 := 100
	zero0 := 0
	exp := time.Now().Add(2 * time.Hour).Unix()

	if err := saveTestGatewayAuth(SavedGatewayAuth{
		RoutingStrategy: StrategyRoundRobin,
		Accounts: []ConnectedAccount{
			{
				ID:                  "u-fresh",
				Provider:            ProviderAntigravity,
				Tier:                "Ultra",
				Email:               "ultra-claude100@gmail.com",
				AccessToken:         "ya29.u100",
				ProjectID:           "aicode-consumers",
				ExpiresAt:           exp,
				WeeklyClaudePercent: &fresh100,
			},
			{
				ID:                  "u-zero",
				Provider:            ProviderAntigravity,
				Tier:                "Ultra",
				Email:               "ultra-claude0@gmail.com",
				AccessToken:         "ya29.u0",
				ProjectID:           "aicode-consumers",
				ExpiresAt:           exp,
				WeeklyClaudePercent: &zero0,
			},
			{
				ID:                  "p-zero",
				Provider:            ProviderAntigravity,
				Tier:                "Pro",
				Email:               "pro-claude0@gmail.com",
				AccessToken:         "ya29.p0",
				ProjectID:           "aicode-consumers",
				ExpiresAt:           exp,
				WeeklyClaudePercent: &zero0,
			},
		},
	}); err != nil {
		t.Fatalf("saveTestGatewayAuth: %v", err)
	}

	if isAccountClaudeQuotaExhaustedFast("ultra-claude100@gmail.com") {
		t.Fatalf("100%% remaining WeeklyClaudePercent was falsely treated as exhausted!")
	}
	if !isAccountClaudeQuotaExhaustedFast("ultra-claude0@gmail.com") {
		t.Fatalf("0%% remaining WeeklyClaudePercent must be treated as exhausted!")
	}

	// While ultra-claude100 has 100% Claude remaining, Claude Opus routes to ultra-claude100
	acc, err := SelectProviderAccountForModel(context.Background(), ProviderAntigravity, "antigravity/claude-opus-4-6-thinking")
	if err != nil || acc.Email != "ultra-claude100@gmail.com" {
		t.Fatalf("expected 100%% Claude Ultra to be selected for Claude Opus, got %+v (err=%v)", acc, err)
	}

	// When ALL accounts have 0% Claude remaining, fallback routes strictly to Pro (NEVER 0%-Claude Ultra!)
	if err := saveTestGatewayAuth(SavedGatewayAuth{
		RoutingStrategy: StrategyRoundRobin,
		Accounts: []ConnectedAccount{
			{ID: "u-zero", Provider: ProviderAntigravity, Tier: "Ultra", Email: "ultra-claude0@gmail.com", AccessToken: "ya29.u0", ExpiresAt: exp, WeeklyClaudePercent: &zero0},
			{ID: "p-zero", Provider: ProviderAntigravity, Tier: "Pro", Email: "pro-claude0@gmail.com", AccessToken: "ya29.p0", ExpiresAt: exp, WeeklyClaudePercent: &zero0},
		},
	}); err != nil {
		t.Fatalf("saveTestGatewayAuth: %v", err)
	}
	fallbackAcc, err := SelectProviderAccountForModel(context.Background(), ProviderAntigravity, "antigravity/claude-opus-4-6-thinking")
	if err != nil || fallbackAcc.Email != "pro-claude0@gmail.com" {
		t.Fatalf("expected 0%%-Claude fallback to route strictly to Pro pro-claude0@gmail.com, got %+v (err=%v)", fallbackAcc, err)
	}
}

// 3. Pro accounts on DefaultCCPAEndpoint prioritize "gemini-3.8-flash-high" FIRST
// ahead of "gemini-3.8-flash-tiered" for gemini-3.8-flash-high and legacy gemini-3.1-pro-high,
// and prioritize "gemini-3.8-flash-medium" FIRST for gemini-3.8-flash-medium.
func TestProAccountPrioritizesGemini38FlashHighFirst(t *testing.T) {
	setupUltraFlagshipPDCATestEnv(t)

	exp := time.Now().Add(2 * time.Hour).Unix()
	if err := saveTestGatewayAuth(SavedGatewayAuth{
		Accounts: []ConnectedAccount{
			{ID: "p-1", Provider: ProviderAntigravity, Tier: "Pro", Email: "advietnam789@gmail.com", AccessToken: "ya29.pro", ProjectID: "aicode-consumers", ExpiresAt: exp},
		},
	}); err != nil {
		t.Fatalf("saveTestGatewayAuth: %v", err)
	}

	for _, mod := range []string{
		"antigravity/gemini-3.8-flash-high",
		"antigravity/gemini-3.1-pro-high",
	} {
		cands := buildCCPACandidatesWithCache(DefaultCCPAEndpoint, "advietnam789@gmail.com", ResolveModel(mod, nil), false)
		if len(cands) < 2 {
			t.Fatalf("expected at least 2 candidates for %s on Pro account, got %v", mod, cands)
		}
		if cands[0] != "gemini-3.8-flash-high" {
			t.Fatalf("expected Pro account cands[0] for %s to be gemini-3.8-flash-high, got %q (all: %v)", mod, cands[0], cands)
		}
		if cands[1] != "gemini-3.8-flash-tiered" {
			t.Fatalf("expected Pro account cands[1] for %s to be gemini-3.8-flash-tiered, got %q (all: %v)", mod, cands[1], cands)
		}
		for _, c := range cands {
			if strings.Contains(c, "gemini-3.1-pro") {
				t.Fatalf("expected zero gemini-3.1-pro candidates for %s on discontinued Pro account, found %q (all: %v)", mod, c, cands)
			}
		}
	}

	candsMed := buildCCPACandidatesWithCache(DefaultCCPAEndpoint, "advietnam789@gmail.com", ResolveModel("antigravity/gemini-3.8-flash-medium", nil), false)
	if len(candsMed) < 2 {
		t.Fatalf("expected at least 2 candidates for antigravity/gemini-3.8-flash-medium on Pro account, got %v", candsMed)
	}
	if candsMed[0] != "gemini-3.8-flash-medium" {
		t.Fatalf("expected Pro account cands[0] for gemini-3.8-flash-medium to be gemini-3.8-flash-medium, got %q (all: %v)", candsMed[0], candsMed)
	}
	if candsMed[1] != "gemini-3.8-flash-high" {
		t.Fatalf("expected Pro account cands[1] for gemini-3.8-flash-medium to be gemini-3.8-flash-high, got %q (all: %v)", candsMed[1], candsMed)
	}
}

// 4. Zero Ultra leak across Bucket C (3.1-pro, 3.8-flash, auto/fast), TTFT hedge when 0 alternate Pro exists,
// Sub-Agents listActiveAccountPool, and cross-provider claude-code/* & alibaba/* isolation.
func TestZeroUltraLeakAcrossBucketCHedgeSubAgentsAndCrossProvider(t *testing.T) {
	setupUltraFlagshipPDCATestEnv(t)

	exp := time.Now().Add(2 * time.Hour).Unix()
	if err := saveTestGatewayAuth(SavedGatewayAuth{
		RoutingStrategy: StrategyRoundRobin,
		Accounts: []ConnectedAccount{
			{ID: "u-1", Provider: ProviderAntigravity, Tier: "Ultra", Email: "ultra1@gmail.com", AccessToken: "ya29.ultra1", ProjectID: "aicode-consumers", ExpiresAt: exp},
			{ID: "p-1", Provider: ProviderAntigravity, Tier: "Pro", Email: "pro1@gmail.com", AccessToken: "ya29.pro1", ProjectID: "aicode-consumers", ExpiresAt: exp},
			{ID: "cc-1", Provider: ProviderClaudeCode, Tier: "Max", Email: "cc@anthropic.local", AccessToken: "sk-ant-oat01-invalid", BaseURL: "http://127.0.0.1:1", ExpiresAt: exp},
		},
	}); err != nil {
		t.Fatalf("saveTestGatewayAuth: %v", err)
	}

	// Sub-Agents pool must return ONLY pro1 while pro1 is healthy
	pool := listActiveAccountPool()
	if len(pool) != 1 || pool[0].Email != "pro1@gmail.com" {
		t.Fatalf("expected listActiveAccountPool to return strictly [pro1@gmail.com], got %+v", pool)
	}

	// TTFT hedge pick for gemini-3.1-pro-high excluding the sole Pro account MUST abort rather than hijacking ultra1!
	hedgeCtx := withHedgeAlternateAccountContext(context.Background(), "pro1@gmail.com")
	if hijacked, _, err := AcquireProviderAccountForModel(hedgeCtx, ProviderAntigravity, "antigravity/gemini-3.1-pro-high", "hedge"); err == nil {
		t.Fatalf("expected Bucket C hedge with 0 remaining Pro accounts to abort, but hijacked %q", hijacked.Email)
	}

	// Cross-provider isolation: explicit claude-code/claude-opus-5-5 failure MUST NOT spill over onto Antigravity Ultra!
	mockClaude := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error":{"message":"claude upstream 502"}}`))
	}))
	defer mockClaude.Close()
	if _, err := UpsertConnectedAccount(ConnectedAccount{
		ID:          "cc-1",
		Provider:    ProviderClaudeCode,
		Tier:        "Max",
		Email:       "cc@anthropic.local",
		AccessToken: "sk-ant-api03-test",
		APIKey:      "sk-ant-api03-test",
		BaseURL:     mockClaude.URL,
	}); err != nil {
		t.Fatalf("UpsertConnectedAccount(claude): %v", err)
	}

	var agyCalls atomic.Int32
	upstream := NewUpstream(GatewayConfig{CCPAEndpoint: DefaultCCPAEndpoint})
	upstream.client = &http.Client{
		Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			if strings.Contains(r.URL.Host, "googleapis.com") {
				agyCalls.Add(1)
			}
			return http.DefaultTransport.RoundTrip(r)
		}),
	}

	rec := httptest.NewRecorder()
	resolvedCC := ResolveModel("claude-code/claude-opus-5-5", nil)
	_ = upstream.HandleChatCompletion(context.Background(), rec, OpenAIChatRequest{
		Model:    "claude-code/claude-opus-5-5",
		Messages: []OpenAIChatMessage{{Role: "user", Content: json.RawMessage(`"hello"`)}},
	}, resolvedCC, "")
	if agyCalls.Load() != 0 {
		t.Fatalf("explicit claude-code/* request leaked %d call(s) onto Antigravity CCPA!", agyCalls.Load())
	}
}

// 5 & 6. RecordAccountModelResult preserves LastModel on errors, and 403 TOS_VIOLATION sets 86400s cooldown
// that survives inspectAntigravityAccount (loadCodeAssist 200 OK) while VALIDATION_REQUIRED sets 7200s + unlocks cleanly.
func TestLastModelPreservedOnErrorAnd403TOSViolationSurvivesQuotaSync(t *testing.T) {
	setupUltraFlagshipPDCATestEnv(t)

	exp := time.Now().Add(2 * time.Hour).Unix()
	if err := saveTestGatewayAuth(SavedGatewayAuth{
		Accounts: []ConnectedAccount{
			{
				ID:          "u-ml",
				Provider:    ProviderAntigravity,
				Tier:        "Ultra",
				Email:       "minh.long.ml427@gmail.com",
				AccessToken: "ya29.ml",
				ProjectID:   "aicode-consumers",
				LastModel:   "antigravity/gemini-4-carbon-high ➔ antigravity/gemini-3.8-flash-high [gemini-3.8-flash]",
				ExpiresAt:   exp,
			},
			{
				ID:          "p-tos",
				Provider:    ProviderAntigravity,
				Tier:        "Pro",
				Email:       "ngocthao4599s@gmail.com",
				AccessToken: "ya29.tos",
				ProjectID:   "aicode-consumers",
				ExpiresAt:   exp,
			},
			{
				ID:          "p-val",
				Provider:    ProviderAntigravity,
				Tier:        "Pro",
				Email:       "verify-me@gmail.com",
				AccessToken: "ya29.val",
				ProjectID:   "aicode-consumers",
				ExpiresAt:   exp,
			},
		},
	}); err != nil {
		t.Fatalf("saveTestGatewayAuth: %v", err)
	}

	// Failed 429 / 503 / 403 must NEVER overwrite LastModel
	RecordAccountModelResult("minh.long.ml427@gmail.com", ProviderAntigravity, "antigravity/gemini-3.1-pro-high", false, 250, 0, http.StatusTooManyRequests)
	savedAfterFail := LoadSavedAuthConfig()
	for _, a := range savedAfterFail.Accounts {
		if a.Email == "minh.long.ml427@gmail.com" {
			if strings.Contains(a.LastModel, "gemini-3.1-pro-high") {
				t.Fatalf("failed 429 overwrote LastModel to %q", a.LastModel)
			}
		}
	}

	// Apply 403 TOS_VIOLATION (86400s) vs VALIDATION_REQUIRED (7200s)
	ApplyAntigravityForbiddenCooldown("ngocthao4599s@gmail.com", `{"error":{"code":403,"status":"PERMISSION_DENIED","details":[{"reason":"TOS_VIOLATION"}]}}`)
	ApplyAntigravityForbiddenCooldown("verify-me@gmail.com", `{"error":{"code":403,"status":"PERMISSION_DENIED","details":[{"reason":"VALIDATION_REQUIRED"}]}}`)

	mockQuotaSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "loadCodeAssist"):
			_, _ = fmt.Fprintf(w, `{"currentTier":{"id":"g1-pro-tier","name":"Pro"},"cloudaicompanionProject":"aicode-consumers"}`)
		case strings.Contains(r.URL.Path, "retrieveUserQuotaSummary"):
			_, _ = io.WriteString(w, `{"groups":[{"displayName":"Claude","buckets":[{"bucketId":"3p-weekly","window":"weekly","remainingFraction":1.0}]}]}`)
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

	savedMid := LoadSavedAuthConfig()
	nowUnix := time.Now().Unix()
	for i := range savedMid.Accounts {
		acc := savedMid.Accounts[i]
		var d AccountQuotaDetail
		inspectAntigravityAccount(context.Background(), &acc, &d, "reset")
		switch acc.Email {
		case "ngocthao4599s@gmail.com":
			rem := acc.CooldownUntil - nowUnix
			if rem < 80000 {
				t.Fatalf("expected TOS_VIOLATION 86400s cooldown to survive loadCodeAssist 200 OK, got remaining=%ds", rem)
			}
			if acc.WeeklyClaudePercent == nil || *acc.WeeklyClaudePercent != 100 {
				t.Fatalf("expected 1.0 remainingFraction to store WeeklyClaudePercent=100, got %v", acc.WeeklyClaudePercent)
			}
		case "verify-me@gmail.com":
			if acc.CooldownUntil != 0 {
				t.Fatalf("expected verified VALIDATION_REQUIRED account to auto-unlock on clean loadCodeAssist 200 OK, got CooldownUntil=%d", acc.CooldownUntil)
			}
		}
	}
}

// 7. Server /api/pdca/status HTTP route & StartPDCAWatchdog integration (GET/HEAD public read-only + admin-guarded trigger).
func TestServerPDCAStatusRouteAndWatchdogIntegration(t *testing.T) {
	setupUltraFlagshipPDCATestEnv(t)

	cfg := GatewayConfig{CCPAEndpoint: DefaultCCPAEndpoint}
	srv := NewServer(cfg, NewResolver(), NewUpstream(cfg))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv.StartPDCAWatchdog(ctx)

	h := srv.Handler()

	// 1. Public GET /api/pdca/status works even when an API key is configured (no secrets exposed)
	createdKey, err := CreateAPIKey("ops-admin", true, true, true, true)
	if err != nil {
		t.Fatalf("CreateAPIKey: %v", err)
	}

	reqGet := httptest.NewRequest(http.MethodGet, "/api/pdca/status", nil)
	recGet := httptest.NewRecorder()
	h.ServeHTTP(recGet, reqGet)
	if recGet.Code != http.StatusOK {
		t.Fatalf("expected unauthenticated GET /api/pdca/status to return 200 OK, got %d (%s)", recGet.Code, recGet.Body.String())
	}
	var snap PDCAStatusSnapshot
	if err := json.NewDecoder(recGet.Body).Decode(&snap); err != nil {
		t.Fatalf("decode /api/pdca/status: %v", err)
	}
	if !snap.WatchdogRunning || snap.WatchdogIntervalSec != 60 || snap.TotalCycles < 1 {
		t.Fatalf("unexpected PDCA watchdog snapshot: %+v", snap)
	}

	// 2. HEAD /api/pdca/status returns 200 OK
	reqHead := httptest.NewRequest(http.MethodHead, "/api/pdca/status", nil)
	recHead := httptest.NewRecorder()
	h.ServeHTTP(recHead, reqHead)
	if recHead.Code != http.StatusOK {
		t.Fatalf("expected HEAD /api/pdca/status 200 OK, got %d", recHead.Code)
	}

	// 3. Unauthenticated ?run=true is rejected when management auth is enabled, while admin Bearer key succeeds
	reqUnauthRun := httptest.NewRequest(http.MethodGet, "/api/pdca/status?run=true", nil)
	recUnauthRun := httptest.NewRecorder()
	h.ServeHTTP(recUnauthRun, reqUnauthRun)
	if recUnauthRun.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthenticated ?run=true to return 401 when auth is configured, got %d", recUnauthRun.Code)
	}

	reqAuthRun := httptest.NewRequest(http.MethodGet, "/api/pdca/status?run=true", nil)
	reqAuthRun.Header.Set("Authorization", "Bearer "+createdKey.Key)
	recAuthRun := httptest.NewRecorder()
	h.ServeHTTP(recAuthRun, reqAuthRun)
	if recAuthRun.Code != http.StatusOK {
		t.Fatalf("expected admin-authenticated ?run=true to return 200 OK, got %d", recAuthRun.Code)
	}
}

// 8. PDCA polish invariants (#70): per-account flagshipEvictions scoping & preservation of TOS_VIOLATION / VALIDATION_REQUIRED
// during Step 4 cross-provider cooldown healing.
func TestPDCAPolishAccountScopedEvictionAndBanPreservation(t *testing.T) {
	setupUltraFlagshipPDCATestEnv(t)

	now := time.Now()
	exp := now.Add(2 * time.Hour).Unix()
	if err := saveTestGatewayAuth(SavedGatewayAuth{
		Accounts: []ConnectedAccount{
			{
				ID:            "tos-1",
				Provider:      ProviderAntigravity,
				Tier:          "Pro",
				Email:         "tos@gmail.com",
				AccessToken:   "ya29.tos",
				CooldownUntil: now.Add(24 * time.Hour).Unix(),
				ExpiresAt:     exp,
			},
			{
				ID:            "val-1",
				Provider:      ProviderAntigravity,
				Tier:          "Pro",
				Email:         "val@gmail.com",
				AccessToken:   "ya29.val",
				CooldownUntil: now.Add(2 * time.Hour).Unix(),
				ExpiresAt:     exp,
			},
			{
				ID:            "cc-transient",
				Provider:      ProviderClaudeCode,
				Tier:          "Max",
				Email:         "cc@anthropic.local",
				AccessToken:   "sk-ant-test",
				CooldownUntil: now.Add(15 * time.Minute).Unix(),
				ExpiresAt:     exp,
			},
		},
	}); err != nil {
		t.Fatalf("saveTestGatewayAuth: %v", err)
	}
	validationIsolatedAccounts.Store("val@gmail.com", now.Add(2*time.Hour).Unix())

	// Seed expired dead flagship entry ONLY on ultraA; ultraB holds an active working pin.
	deadCCPAModelCache.Store(DefaultCCPAEndpoint+"|acc:ultra-a@gmail.com|claude-opus-4-6-thinking", now.Add(-1*time.Minute))
	workingCCPAModelCache.Store(DefaultCCPAEndpoint+"|acc:ultra-a@gmail.com|claude-opus-4-6-thinking", "gemini-3.1-pro-high")
	workingCCPAModelCache.Store(DefaultCCPAEndpoint+"|acc:ultra-b@gmail.com|claude-opus-4-6-thinking", "claude-opus-4-6-thinking")

	RunPDCACycle(now)

	if _, existsA := workingCCPAModelCache.Load(DefaultCCPAEndpoint + "|acc:ultra-a@gmail.com|claude-opus-4-6-thinking"); existsA {
		t.Fatalf("expected ultra-a's working pin to be evicted when ultra-a's dead flagship entry expired")
	}
	if gotB, existsB := workingCCPAModelCache.Load(DefaultCCPAEndpoint + "|acc:ultra-b@gmail.com|claude-opus-4-6-thinking"); !existsB || gotB != "claude-opus-4-6-thinking" {
		t.Fatalf("expected peer ultra-b's working pin to survive ultra-a's eviction, got exists=%v val=%v", existsB, gotB)
	}

	after := LoadSavedAuthConfig()
	for _, a := range after.Accounts {
		switch a.Email {
		case "tos@gmail.com":
			if a.CooldownUntil-now.Unix() < 20*3600 {
				t.Fatalf("expected 24h TOS_VIOLATION ban to survive PDCA Step 4 cross-provider sweep, got remaining=%ds", a.CooldownUntil-now.Unix())
			}
		case "val@gmail.com":
			if a.CooldownUntil-now.Unix() < 3600 {
				t.Fatalf("expected VALIDATION_REQUIRED isolation cooldown to survive PDCA Step 4 cross-provider sweep, got remaining=%ds", a.CooldownUntil-now.Unix())
			}
		case "cc@anthropic.local":
			if a.CooldownUntil-now.Unix() > int64(PDCATransientCooldownCap.Seconds())+5 {
				t.Fatalf("expected transient Claude Code >5m cooldown to be capped by PDCA Step 4, got remaining=%ds", a.CooldownUntil-now.Unix())
			}
		}
	}
}
