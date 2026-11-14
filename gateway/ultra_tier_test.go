package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCarbonArgonExclusivelyUltraAccounts(t *testing.T) {
	resetAuthCacheForTest(t)
	ClearAccountModelRateLimits("")

	cfg := SavedGatewayAuth{
		RoutingStrategy: StrategyRoundRobin,
		Accounts: []ConnectedAccount{
			{
				Email:       "pro1@gmail.com",
				Provider:    ProviderAntigravity,
				Tier:        "Pro",
				AccessToken: "ya29.pro1",
				ProjectID:   "proj-pro1",
				ExpiresAt:   time.Now().Unix() + 3600,
			},
			{
				Email:       "ultra1@gmail.com",
				Provider:    ProviderAntigravity,
				Tier:        "Ultra",
				AccessToken: "ya29.ultra1",
				ProjectID:   "proj-ultra1",
				ExpiresAt:   time.Now().Unix() + 3600,
			},
			{
				Email:       "pro2@gmail.com",
				Provider:    ProviderAntigravity,
				Tier:        "Pro",
				AccessToken: "ya29.pro2",
				ProjectID:   "proj-pro2",
				ExpiresAt:   time.Now().Unix() + 3600,
			},
			{
				Email:       "ultra2@gmail.com",
				Provider:    ProviderAntigravity,
				Tier:        "Ultra",
				AccessToken: "ya29.ultra2",
				ProjectID:   "proj-ultra2",
				ExpiresAt:   time.Now().Unix() + 3600,
			},
		},
	}
	if err := saveAuthConfigLocked(cfg); err != nil {
		t.Fatalf("saveAuthConfigLocked failed: %v", err)
	}

	ctx := context.Background()

	// 1. Carbon High must strictly pick Ultra accounts, never Pro accounts
	for i := 0; i < 6; i++ {
		acc, rel, err := AcquireProviderAccountForModel(ctx, ProviderAntigravity, "antigravity/gemini-4-carbon-high", "antigravity/gemini-4-carbon-high")
		if err != nil {
			t.Fatalf("AcquireProviderAccountForModel(carbon-high) failed: %v", err)
		}
		if rel != nil {
			rel()
		}
		if !strings.EqualFold(strings.TrimSpace(acc.Tier), "Ultra") {
			t.Fatalf("expected Carbon High to pick Ultra account, got email=%s tier=%s", acc.Email, acc.Tier)
		}
		if acc.Email != "ultra1@gmail.com" && acc.Email != "ultra2@gmail.com" {
			t.Fatalf("Carbon High picked non-Ultra email: %s", acc.Email)
		}
	}

	// 2. Argon High must also strictly pick Ultra accounts, never Pro accounts
	for i := 0; i < 6; i++ {
		acc, rel, err := AcquireProviderAccountForModel(ctx, ProviderAntigravity, "antigravity/gemini-4-argon-high", "antigravity/gemini-4-argon-high")
		if err != nil {
			t.Fatalf("AcquireProviderAccountForModel(argon-high) failed: %v", err)
		}
		if rel != nil {
			rel()
		}
		if !strings.EqualFold(strings.TrimSpace(acc.Tier), "Ultra") {
			t.Fatalf("expected Argon High to pick Ultra account, got email=%s tier=%s", acc.Email, acc.Tier)
		}
		if acc.Email != "ultra1@gmail.com" && acc.Email != "ultra2@gmail.com" {
			t.Fatalf("Argon High picked non-Ultra email: %s", acc.Email)
		}
	}
}

func TestCarbonArgonExhaustionNeverStepsDownToProAccounts(t *testing.T) {
	resetAuthCacheForTest(t)
	ClearAccountModelRateLimits("")

	cfg := SavedGatewayAuth{
		RoutingStrategy: StrategyRoundRobin,
		Accounts: []ConnectedAccount{
			{
				Email:       "pro1@gmail.com",
				Provider:    ProviderAntigravity,
				Tier:        "Pro",
				AccessToken: "ya29.pro1",
				ProjectID:   "proj-pro1",
				ExpiresAt:   time.Now().Unix() + 3600,
			},
			{
				Email:       "ultra1@gmail.com",
				Provider:    ProviderAntigravity,
				Tier:        "Ultra",
				AccessToken: "ya29.ultra1",
				ProjectID:   "proj-ultra1",
				ExpiresAt:   time.Now().Unix() + 3600,
			},
			{
				Email:       "ultra2@gmail.com",
				Provider:    ProviderAntigravity,
				Tier:        "Ultra",
				AccessToken: "ya29.ultra2",
				ProjectID:   "proj-ultra2",
				ExpiresAt:   time.Now().Unix() + 3600,
			},
		},
	}
	if err := saveAuthConfigLocked(cfg); err != nil {
		t.Fatalf("saveAuthConfigLocked failed: %v", err)
	}

	ctx := context.Background()

	// Rate-limit all Ultra accounts for Carbon High
	RecordAccountModelRateLimit("ultra1@gmail.com", "antigravity/gemini-4-carbon-high", 2*time.Minute)
	RecordAccountModelRateLimit("ultra2@gmail.com", "antigravity/gemini-4-carbon-high", 2*time.Minute)

	// Direct Carbon/Argon is 100% locked to Ultra accounts; zero stepdown to Pro is strictly enforced
	acc, rel, err := AcquireProviderAccountForModel(ctx, ProviderAntigravity, "antigravity/gemini-4-carbon-high", "antigravity/gemini-4-carbon-high")
	if rel != nil {
		rel()
	}
	if err == nil {
		t.Fatalf("expected error acquiring account when all Ultra accounts are exhausted, got account: email=%s tier=%s", acc.Email, acc.Tier)
	}
	if acc != nil && acc.Email == "pro1@gmail.com" {
		t.Fatalf("security violation: Carbon High stepped down to Pro account pro1@gmail.com")
	}

	// Verify IsCarbonOrArgonFamilyModel helper correctly detects Carbon & Argon
	if !IsCarbonOrArgonFamilyModel("antigravity/gemini-4-carbon-high") {
		t.Fatalf("expected IsCarbonOrArgonFamilyModel(gemini-4-carbon-high) = true")
	}
	if !IsCarbonOrArgonFamilyModel("antigravity/gemini-4-argon-high") {
		t.Fatalf("expected IsCarbonOrArgonFamilyModel(gemini-4-argon-high) = true")
	}
	if IsCarbonOrArgonFamilyModel("antigravity/gemini-3.1-pro-high") {
		t.Fatalf("expected IsCarbonOrArgonFamilyModel(gemini-3.1-pro-high) = false")
	}
}

func TestGemini31ProPrefersProAccounts(t *testing.T) {
	resetAuthCacheForTest(t)
	ClearAccountModelRateLimits("")

	cfg := SavedGatewayAuth{
		RoutingStrategy: StrategyRoundRobin,
		Accounts: []ConnectedAccount{
			{
				Email:       "ultra1@gmail.com",
				Provider:    ProviderAntigravity,
				Tier:        "Ultra",
				AccessToken: "ya29.ultra1",
				ProjectID:   "proj-ultra1",
				ExpiresAt:   time.Now().Unix() + 3600,
			},
			{
				Email:       "pro1@gmail.com",
				Provider:    ProviderAntigravity,
				Tier:        "Pro",
				AccessToken: "ya29.pro1",
				ProjectID:   "proj-pro1",
				ExpiresAt:   time.Now().Unix() + 3600,
			},
		},
	}
	if err := saveAuthConfigLocked(cfg); err != nil {
		t.Fatalf("saveAuthConfigLocked failed: %v", err)
	}

	ctx := context.Background()

	// Gemini 3.1 Pro High should prefer Pro accounts to save Ultra tokens for Carbon/Argon
	acc, rel, err := AcquireProviderAccountForModel(ctx, ProviderAntigravity, "antigravity/gemini-3.1-pro-high", "antigravity/gemini-3.1-pro-high")
	if err != nil {
		t.Fatalf("AcquireProviderAccountForModel(gemini-3.1-pro-high) failed: %v", err)
	}
	if rel != nil {
		rel()
	}
	if acc.Email != "pro1@gmail.com" {
		t.Fatalf("expected gemini-3.1-pro-high to prefer Pro account pro1@gmail.com, got %s", acc.Email)
	}
}

func TestGeminiNeverFallsBackToClaudeAndClaudeExhaustionRespected(t *testing.T) {
	tmpCfg := filepath.Join(t.TempDir(), "gateway_config.json")
	t.Setenv("GATEWAY_CONFIG_FILE", tmpCfg)
	resetAuthCacheForTest(t)
	ResetCCPAModelCaches()
	ClearAccountModelRateLimits("")
	t.Cleanup(func() {
		ResetCCPAModelCaches()
		ClearAccountModelRateLimits("")
		resetAuthCacheForTest(t)
	})

	zeroPct := 0
	freshPct := 50
	cfg := SavedGatewayAuth{
		RoutingStrategy: StrategyFillFirst,
		Accounts: []ConnectedAccount{
			{
				Email:               "ultra-fresh@gmail.com",
				Provider:            ProviderAntigravity,
				Tier:                "Ultra",
				AccessToken:         "ya29.ultra-fresh",
				ProjectID:           "proj-ultra-fresh",
				ExpiresAt:           time.Now().Unix() + 3600,
				WeeklyClaudePercent: &freshPct,
			},
			{
				Email:               "ultra-exhausted@gmail.com",
				Provider:            ProviderAntigravity,
				Tier:                "Ultra",
				AccessToken:         "ya29.ultra-exhausted",
				ProjectID:           "proj-ultra-exhausted",
				ExpiresAt:           time.Now().Unix() + 3600,
				WeeklyClaudePercent: &zeroPct,
			},
		},
	}
	if err := saveAuthConfigLocked(cfg); err != nil {
		t.Fatalf("saveAuthConfigLocked failed: %v", err)
	}

	// 1. Verify Gemini models NEVER produce Claude candidates
	for _, m := range []string{
		"antigravity/gemini-4-carbon-high",
		"antigravity/gemini-4-argon-high",
		"antigravity/gemini-3.1-pro-high",
	} {
		res := ResolveModel(m, nil)
		cands := buildCCPACandidatesWithCache(DefaultCCPAEndpoint, "ultra-fresh@gmail.com", res, false)
		for _, c := range cands {
			if strings.HasPrefix(c, "claude-") {
				t.Fatalf("model %s produced Claude candidate %q on fresh account: %v", m, c, cands)
			}
		}
	}

	// 2. Verify Claude exhausted account is recognized
	if !isAccountClaudeQuotaExhaustedFast("ultra-exhausted@gmail.com") {
		t.Fatalf("expected ultra-exhausted@gmail.com to be flagged as Claude quota exhausted")
	}
	if isAccountClaudeQuotaExhaustedFast("ultra-fresh@gmail.com") {
		t.Fatalf("expected ultra-fresh@gmail.com NOT to be flagged as Claude quota exhausted")
	}
}

func TestEveningAlibabaQwen38MaxCodingSupport(t *testing.T) {
	// Verify auto/best-coding has alibaba/qwen3.8-max in top 4 models
	res := ResolveModel("auto/best-coding", nil)
	if len(res.FallbackChain) < 3 {
		t.Fatalf("auto/best-coding FallbackChain too short: %v", res.FallbackChain)
	}
	foundQwen := false
	for i := 0; i < 4 && i < len(res.FallbackChain); i++ {
		if res.FallbackChain[i] == "alibaba/qwen3.8-max" {
			foundQwen = true
			break
		}
	}
	if !foundQwen {
		t.Fatalf("expected alibaba/qwen3.8-max in top 4 coding fallback models, got %v", res.FallbackChain)
	}
}

func TestCarbonArgonStuckFailsOverToUltraPeerAndNeverStepsDownToPro(t *testing.T) {
	tmpCfg := filepath.Join(t.TempDir(), "gateway_config.json")
	t.Setenv("GATEWAY_CONFIG_FILE", tmpCfg)
	resetAuthCacheForTest(t)
	ResetCCPAModelCaches()
	ClearAccountModelRateLimits("")
	t.Cleanup(func() {
		ResetCCPAModelCaches()
		ClearAccountModelRateLimits("")
		resetAuthCacheForTest(t)
	})

	var ultra1Calls atomic.Int32
	var ultra2Calls atomic.Int32
	var pro1Calls atomic.Int32
	var failUltra2 atomic.Bool

	mockCCPA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload ccpaRequestPayload
		_ = json.NewDecoder(r.Body).Decode(&payload)
		authHdr := strings.TrimSpace(r.Header.Get("Authorization"))

		// Ultra1 is stuck / returns 503
		if strings.Contains(authHdr, "ya29.ultra1") {
			ultra1Calls.Add(1)
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":{"code":503,"status":"UNAVAILABLE","message":"upstream model congested on ultra1"}}`))
			return
		}

		// Ultra2 succeeds in sub-check 1, or fails with 503 in sub-check 2
		if strings.Contains(authHdr, "ya29.ultra2") {
			ultra2Calls.Add(1)
			if failUltra2.Load() {
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte(`{"error":{"code":503,"status":"UNAVAILABLE","message":"upstream model congested on ultra2"}}`))
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprintf(w, "data: {\"response\":{\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"served-by-ultra2\"}]}}]}}\n\ndata: [DONE]\n\n")
			return
		}

		// Pro account should NEVER be invoked for Carbon/Argon requests
		if strings.Contains(authHdr, "ya29.pro1") {
			pro1Calls.Add(1)
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprintf(w, "data: {\"response\":{\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"served-by-pro1-forbidden\"}]}}]}}\n\ndata: [DONE]\n\n")
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer mockCCPA.Close()

	cfg := SavedGatewayAuth{
		RoutingStrategy: StrategyRoundRobin,
		Accounts: []ConnectedAccount{
			{
				Email:       "ultra1@gmail.com",
				Provider:    ProviderAntigravity,
				Tier:        "Ultra",
				AccessToken: "ya29.ultra1",
				ProjectID:   "proj-ultra1",
				ExpiresAt:   time.Now().Unix() + 3600,
			},
			{
				Email:       "ultra2@gmail.com",
				Provider:    ProviderAntigravity,
				Tier:        "Ultra",
				AccessToken: "ya29.ultra2",
				ProjectID:   "proj-ultra2",
				ExpiresAt:   time.Now().Unix() + 3600,
			},
			{
				Email:       "pro1@gmail.com",
				Provider:    ProviderAntigravity,
				Tier:        "Pro",
				AccessToken: "ya29.pro1",
				ProjectID:   "proj-pro1",
				ExpiresAt:   time.Now().Unix() + 3600,
			},
		},
	}
	if err := saveAuthConfigLocked(cfg); err != nil {
		t.Fatalf("saveAuthConfigLocked failed: %v", err)
	}

	upstream := NewUpstream(GatewayConfig{CCPAEndpoint: mockCCPA.URL})
	resolved := ResolveModel("antigravity/gemini-4-carbon-high", nil)

	req := OpenAIChatRequest{
		Model: "antigravity/gemini-4-carbon-high",
		Messages: []OpenAIChatMessage{
			{Role: "user", Content: json.RawMessage(`"Write production code"`)},
		},
	}

	// Sub-check 1: Ultra1 stuck (503) -> fails over to Ultra peer (ultra2). Pro account must receive 0 calls.
	rec1 := httptest.NewRecorder()
	if err := upstream.HandleChatCompletion(context.Background(), rec1, req, resolved, ""); err != nil {
		t.Fatalf("HandleChatCompletion sub-check 1 failed: %v", err)
	}

	if rec1.Code != http.StatusOK {
		t.Fatalf("expected status 200 OK, got %d, body: %s", rec1.Code, rec1.Body.String())
	}
	if gotAcc := rec1.Header().Get("X-Omni-Gateway-Account"); gotAcc != "ultra2@gmail.com" {
		t.Fatalf("expected failover to Ultra peer ultra2@gmail.com, got %q", gotAcc)
	}
	if gotStepdown := rec1.Header().Get("X-Omni-Gateway-Tier-Stepdown"); gotStepdown != "" {
		t.Fatalf("expected empty X-Omni-Gateway-Tier-Stepdown header, got %q", gotStepdown)
	}
	if pro1Calls.Load() != 0 {
		t.Fatalf("security violation: pro1 received %d calls during Ultra-to-Ultra failover", pro1Calls.Load())
	}
	if !strings.Contains(rec1.Body.String(), "served-by-ultra2") {
		t.Fatalf("expected response served by ultra2, got: %s", rec1.Body.String())
	}

	// Sub-check 2: Both Ultra1 and Ultra2 return 503 while Pro1 is healthy.
	// Request must error out and NEVER step down or spill over onto pro1@gmail.com.
	failUltra2.Store(true)
	rec2 := httptest.NewRecorder()
	err := upstream.HandleChatCompletion(context.Background(), rec2, req, resolved, "")
	if err == nil && rec2.Code == http.StatusOK {
		t.Fatalf("expected error when all Ultra accounts are congested/exhausted, got HTTP 200 OK")
	}
	if pro1Calls.Load() != 0 {
		t.Fatalf("security violation: pro1 received %d calls when all Ultra accounts failed", pro1Calls.Load())
	}
	if gotStepdown := rec2.Header().Get("X-Omni-Gateway-Tier-Stepdown"); gotStepdown != "" {
		t.Fatalf("expected empty X-Omni-Gateway-Tier-Stepdown header, got %q", gotStepdown)
	}
}
