package gateway

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestAlibabaGLM53InvalidAPIKeyNeverFailsOverToGoogle(t *testing.T) {
	tmpCfg := filepath.Join(t.TempDir(), "gateway_config.json")
	t.Setenv("GATEWAY_CONFIG_FILE", tmpCfg)
	resetAuthCacheForTest(t)
	ResetCCPAModelCaches()
	ClearAccountModelRateLimits("")
	ClearLogs()
	t.Cleanup(func() {
		ResetCCPAModelCaches()
		ClearAccountModelRateLimits("")
		ClearLogs()
		resetAuthCacheForTest(t)
	})

	mockAlibaba := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":{"code":"invalid_api_key","message":"invalid access token or token expired"}}`)
	}))
	defer mockAlibaba.Close()

	var googleCalls atomic.Int32
	mockGoogleCCPA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		googleCalls.Add(1)
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"response":{"candidates":[{"content":{"parts":[{"text":"leaked-to-google"}]}}]}}`)
	}))
	defer mockGoogleCCPA.Close()

	exp := time.Now().Add(2 * time.Hour).Unix()
	cfg := SavedGatewayAuth{
		RoutingStrategy: StrategyRoundRobin,
		Accounts: []ConnectedAccount{
			{
				ID:       "ali-1",
				Provider: ProviderAlibaba,
				Email:    "alibaba-coding",
				APIKey:   "sk-sp-expired-key",
				BaseURL:  mockAlibaba.URL,
			},
			{
				ID:          "u-1",
				Provider:    ProviderAntigravity,
				Tier:        "Ultra",
				Email:       "ultra1@gmail.com",
				AccessToken: "ya29.ultra1",
				ProjectID:   "proj-u1",
				ExpiresAt:   exp,
			},
			{
				ID:          "p-1",
				Provider:    ProviderAntigravity,
				Tier:        "Pro",
				Email:       "pro1@gmail.com",
				AccessToken: "ya29.pro1",
				ProjectID:   "proj-p1",
				ExpiresAt:   exp,
			},
		},
	}
	if err := saveTestGatewayAuth(cfg); err != nil {
		t.Fatalf("saveTestGatewayAuth failed: %v", err)
	}

	crossFallbacks := []string{
		"antigravity/gemini-4-carbon-high",
		"antigravity/gemini-4-argon-high",
		"alibaba/qwen3-coder-plus",
	}
	resolved := ResolveModel("alibaba/glm-5.3", crossFallbacks)
	if resolved.Provider != ProviderAlibaba {
		t.Fatalf("expected resolved.Provider == %s, got %s", ProviderAlibaba, resolved.Provider)
	}

	for _, fb := range resolved.FallbackChain {
		provHint, _ := detectProviderAndSlug(ResolveModelAlias(strings.TrimSpace(fb)))
		if provHint == ProviderAntigravity || provHint == ProviderClaudeCode {
			t.Fatalf("fallback chain leaked cross-provider model %q: %v", fb, resolved.FallbackChain)
		}
	}

	upstream := NewUpstream(GatewayConfig{CCPAEndpoint: mockGoogleCCPA.URL})
	rec := httptest.NewRecorder()
	req := OpenAIChatRequest{
		Model: "alibaba/glm-5.3",
		Messages: []OpenAIChatMessage{
			{Role: "user", Content: json.RawMessage(`"test alibaba failover boundary"`)},
		},
	}

	err := upstream.HandleChatCompletion(context.Background(), rec, req, resolved, "")
	if err == nil {
		t.Fatalf("expected HandleChatCompletion to return error on 401 invalid api key")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected HTTP 401 Unauthorized, got %d (body: %s)", rec.Code, rec.Body.String())
	}
	if googleCalls.Load() != 0 {
		t.Fatalf("Alibaba request leaked %d calls to Google Antigravity CCPA endpoint!", googleCalls.Load())
	}

	savedCfg := LoadSavedAuthConfig()
	for _, acc := range savedCfg.Accounts {
		if acc.Provider == ProviderAntigravity {
			if acc.LastModel != "" {
				t.Fatalf("Antigravity account %s has LastModel set unexpectedly: %q", acc.Email, acc.LastModel)
			}
			if acc.SuccessCount != 0 {
				t.Fatalf("Antigravity account %s has SuccessCount != 0: %d", acc.Email, acc.SuccessCount)
			}
		}
	}

	logs := GetRecentLogs(50, "", false)
	for _, l := range logs {
		if l.Provider == ProviderAntigravity {
			t.Fatalf("found unexpected Antigravity log entry during Alibaba execution: %+v", l)
		}
	}
}

func TestCarbonStepdownToProOnDefaultEndpointUsesGemini38FlashHighWithoutCooldown(t *testing.T) {
	tmpCfg := filepath.Join(t.TempDir(), "gateway_config.json")
	t.Setenv("GATEWAY_CONFIG_FILE", tmpCfg)
	resetAuthCacheForTest(t)
	ResetCCPAModelCaches()
	clearTestCCPACaches()
	ClearAccountModelRateLimits("")
	t.Cleanup(func() {
		ResetCCPAModelCaches()
		clearTestCCPACaches()
		ClearAccountModelRateLimits("")
		resetAuthCacheForTest(t)
	})

	exp := time.Now().Add(2 * time.Hour).Unix()
	cfg := SavedGatewayAuth{
		RoutingStrategy: StrategyRoundRobin,
		Accounts: []ConnectedAccount{
			{
				ID:          "u-1",
				Provider:    ProviderAntigravity,
				Tier:        "Ultra",
				Email:       "ultra1@gmail.com",
				AccessToken: "ya29.ultra1",
				ProjectID:   "proj-u1",
				ExpiresAt:   exp,
			},
			{
				ID:          "p-1",
				Provider:    ProviderAntigravity,
				Tier:        "Pro",
				Email:       "pro1@gmail.com",
				AccessToken: "ya29.pro1",
				ProjectID:   "proj-p1",
				ExpiresAt:   exp,
			},
			{
				ID:          "p-2",
				Provider:    ProviderAntigravity,
				Tier:        "Pro",
				Email:       "pro2@gmail.com",
				AccessToken: "ya29.pro2",
				ProjectID:   "proj-p2",
				ExpiresAt:   exp,
			},
		},
	}
	if err := saveTestGatewayAuth(cfg); err != nil {
		t.Fatalf("saveTestGatewayAuth failed: %v", err)
	}

	workingCCPAModelCache.Store(DefaultCCPAEndpoint+"|acc:pro1@gmail.com|antigravity/gemini-3.8-flash-high", "gemini-3.5-flash-lite")

	upstream := NewUpstream(GatewayConfig{CCPAEndpoint: DefaultCCPAEndpoint})
	upstream.client = &http.Client{
		Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			raw, _ := io.ReadAll(r.Body)
			var payload ccpaRequestPayload
			_ = json.Unmarshal(raw, &payload)
			auth := strings.TrimSpace(r.Header.Get("Authorization"))

			if auth == "Bearer ya29.ultra1" {
				return &http.Response{
					StatusCode: http.StatusTooManyRequests,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(`{"error":{"code":429,"message":"resource exhausted on ultra"}}`)),
				}, nil
			}

			if strings.Contains(payload.Model, "gemini-3.1-pro") || strings.Contains(payload.Model, "gemini-4-") {
				return &http.Response{
					StatusCode: http.StatusNotFound,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(`{"error":{"code":404,"message":"model not found on pro"}}`)),
				}, nil
			}

			if payload.Model == "gemini-3.8-flash-high" {
				sse := "data: {\"response\":{\"modelVersion\":\"gemini-3.8-flash\",\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"pro-flash-high-ok\"}]},\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":8,\"candidatesTokenCount\":4,\"totalTokenCount\":12}}}\n\ndata: [DONE]\n\n"
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
					Body:       io.NopCloser(strings.NewReader(sse)),
				}, nil
			}

			return &http.Response{
				StatusCode: http.StatusNotFound,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"error":{"code":404,"message":"unknown candidate"}}`)),
			}, nil
		}),
	}

	resolved := ResolveModel("antigravity/gemini-4-carbon-high", nil)
	rec := httptest.NewRecorder()
	req := OpenAIChatRequest{
		Model: "antigravity/gemini-4-carbon-high",
		Messages: []OpenAIChatMessage{
			{Role: "user", Content: json.RawMessage(`"Write robust code"`)},
		},
	}

	err := upstream.HandleChatCompletion(context.Background(), rec, req, resolved, "")
	if err == nil && rec.Code == http.StatusOK {
		t.Fatalf("expected error when Ultra is exhausted on Carbon High (Zero Stepdown Policy), got HTTP 200 OK (body: %s)", rec.Body.String())
	}

	stepdownHdr := rec.Header().Get("X-Omni-Gateway-Tier-Stepdown")
	if stepdownHdr != "" {
		t.Fatalf("expected empty X-Omni-Gateway-Tier-Stepdown under Zero Stepdown Policy, got %q", stepdownHdr)
	}

	winningAcc := rec.Header().Get("X-Omni-Gateway-Account")
	if winningAcc == "pro1@gmail.com" || winningAcc == "pro2@gmail.com" {
		t.Fatalf("expected Carbon High NEVER to spill over onto Pro account, got %q", winningAcc)
	}

	savedCfg := LoadSavedAuthConfig()
	for _, acc := range savedCfg.Accounts {
		if strings.HasPrefix(acc.Email, "pro") {
			if acc.CooldownUntil != 0 {
				t.Fatalf("Pro account %s unexpectedly put into cooldown: CooldownUntil=%d", acc.Email, acc.CooldownUntil)
			}
		}
	}
}

func TestAccountLastUsedAtNotOverwrittenByQuotaSyncUpdatedAt(t *testing.T) {
	tmpCfg := filepath.Join(t.TempDir(), "gateway_config.json")
	t.Setenv("GATEWAY_CONFIG_FILE", tmpCfg)
	resetAuthCacheForTest(t)
	ClearAccountModelRateLimits("")
	t.Cleanup(func() {
		ClearAccountModelRateLimits("")
		resetAuthCacheForTest(t)
	})

	initialUpdatedAt := "2026-11-13T03:12:06Z"
	acc := ConnectedAccount{
		ID:          "sync-1",
		Provider:    ProviderAntigravity,
		Email:       "sync-check@gmail.com",
		AccessToken: "ya29.sync-check",
		ProjectID:   "proj-sync",
		Tier:        "Pro",
		UpdatedAt:   initialUpdatedAt,
		ExpiresAt:   time.Now().Add(2 * time.Hour).Unix(),
	}
	if err := saveTestGatewayAuth(SavedGatewayAuth{Accounts: []ConnectedAccount{acc}}); err != nil {
		t.Fatalf("saveTestGatewayAuth failed: %v", err)
	}

	RecordAccountModelResult("sync-check@gmail.com", ProviderAntigravity, "antigravity/gemini-3.8-flash-high", true, 500, 100, http.StatusOK)

	cfgAfterResult := LoadSavedAuthConfig()
	if len(cfgAfterResult.Accounts) != 1 {
		t.Fatalf("expected 1 account after result recording, got %d", len(cfgAfterResult.Accounts))
	}
	savedLastUsedAt := cfgAfterResult.Accounts[0].LastUsedAt
	if strings.TrimSpace(savedLastUsedAt) == "" {
		t.Fatalf("expected LastUsedAt to be non-empty after RecordAccountModelResult")
	}

	quotaSyncUpdatedAt := "2026-11-13T04:12:06Z"
	configMu.Lock()
	cfgToModify := loadSavedAuthConfigLocked()
	cfgToModify.Accounts[0].UpdatedAt = quotaSyncUpdatedAt
	if err := saveAuthConfigLocked(cfgToModify); err != nil {
		configMu.Unlock()
		t.Fatalf("saveAuthConfigLocked failed: %v", err)
	}
	configMu.Unlock()

	summary := QuotaSummary{
		Accounts: []AccountQuotaDetail{
			{Email: "sync-check@gmail.com"},
		},
	}
	overlay := overlayLiveAccountStats(&summary)
	if overlay == nil || len(overlay.Accounts) != 1 {
		t.Fatalf("overlayLiveAccountStats returned invalid summary: %+v", overlay)
	}

	outDetail := overlay.Accounts[0]
	if outDetail.LastUsedAt == "" {
		t.Fatalf("expected overlay LastUsedAt to be non-empty")
	}
	unwantedOverlayTime := formatAccountLastUsed(quotaSyncUpdatedAt)
	if outDetail.LastUsedAt == unwantedOverlayTime && unwantedOverlayTime != "" {
		t.Fatalf("LastUsedAt in quota overlay was overwritten by UpdatedAt: %q", outDetail.LastUsedAt)
	}
	expectedFormatted := formatAccountLastUsed(savedLastUsedAt)
	if outDetail.LastUsedAt != expectedFormatted {
		t.Fatalf("expected overlay LastUsedAt == %q, got %q", expectedFormatted, outDetail.LastUsedAt)
	}

	reSyncAcc := ConnectedAccount{
		ID:          "sync-1",
		Provider:    ProviderAntigravity,
		Email:       "sync-check@gmail.com",
		AccessToken: "ya29.sync-check-refreshed",
		ProjectID:   "proj-sync",
		Tier:        "Pro",
		LastUsedAt:  "",
		UpdatedAt:   "2026-11-13T05:12:06Z",
	}
	cfgAfterUpsert, err := UpsertConnectedAccount(reSyncAcc)
	if err != nil {
		t.Fatalf("UpsertConnectedAccount failed: %v", err)
	}
	if len(cfgAfterUpsert.Accounts) != 1 {
		t.Fatalf("expected 1 account after upsert, got %d", len(cfgAfterUpsert.Accounts))
	}
	if cfgAfterUpsert.Accounts[0].LastUsedAt != savedLastUsedAt {
		t.Fatalf("UpsertConnectedAccount wiped existing LastUsedAt: got %q, want %q",
			cfgAfterUpsert.Accounts[0].LastUsedAt, savedLastUsedAt)
	}
}
