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
	"sync/atomic"
	"testing"
	"time"
)

func resetAuthCacheForTest(t *testing.T) {
	t.Helper()
	configMu.Lock()
	cachedAuthCfg = nil
	cachedAuthPath = ""
	cachedAuthMtime = time.Time{}
	asyncFlushPending = false
	configMu.Unlock()
}

func TestRefreshClaudeTokenAndSelectProviderAccount(t *testing.T) {
	tmpCfg := filepath.Join(t.TempDir(), "gateway_config.json")
	t.Setenv("GATEWAY_CONFIG_FILE", tmpCfg)
	resetAuthCacheForTest(t)
	t.Cleanup(func() { resetAuthCacheForTest(t) })

	origTokenEndpoint := ClaudeTokenEndpoint
	origUsageEndpoint := ClaudeOAuthUsageEndpoint
	t.Cleanup(func() {
		ClaudeTokenEndpoint = origTokenEndpoint
		ClaudeOAuthUsageEndpoint = origUsageEndpoint
	})

	var refreshCalls atomic.Int32
	mockTokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		refreshCalls.Add(1)
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if gotCT := r.Header.Get("Content-Type"); !strings.Contains(gotCT, "application/json") {
			t.Errorf("expected Content-Type application/json, got %q", gotCT)
		}
		if gotUA := r.Header.Get("User-Agent"); gotUA != "claude-code/2.1.0" {
			t.Errorf("expected User-Agent claude-code/2.1.0, got %q", gotUA)
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("failed to decode refresh payload: %v", err)
		}
		if body["grant_type"] != "refresh_token" || body["client_id"] != ClaudeOAuthClientID || body["refresh_token"] != "rt-claude-old" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"sk-ant-oat01-refreshed-ok","refresh_token":"rt-claude-rotated","expires_in":3600}`))
	}))
	defer mockTokenSrv.Close()
	ClaudeTokenEndpoint = mockTokenSrv.URL

	expiredAcc := ConnectedAccount{
		Provider:     ProviderClaudeCode,
		Email:        "pro-dev@anthropic.example",
		AccessToken:  "sk-ant-oat01-expired",
		RefreshToken: "rt-claude-old",
		ExpiresAt:    time.Now().Unix() - 120,
	}
	if _, err := UpsertConnectedAccount(expiredAcc); err != nil {
		t.Fatalf("UpsertConnectedAccount failed: %v", err)
	}

	selected, err := SelectProviderAccount(context.Background(), ProviderClaudeCode)
	if err != nil {
		t.Fatalf("SelectProviderAccount failed: %v", err)
	}
	if selected.AccessToken != "sk-ant-oat01-refreshed-ok" {
		t.Fatalf("expected refreshed access token, got %q", selected.AccessToken)
	}
	if selected.RefreshToken != "rt-claude-rotated" {
		t.Fatalf("expected rotated refresh token, got %q", selected.RefreshToken)
	}
	if selected.ExpiresAt <= time.Now().Unix()+3000 {
		t.Fatalf("expected updated ExpiresAt in the future, got %d", selected.ExpiresAt)
	}
	if refreshCalls.Load() != 1 {
		t.Fatalf("expected 1 refresh call, got %d", refreshCalls.Load())
	}

	// Verify persisted config on disk also has the refreshed token
	saved := LoadSavedAuthConfig()
	if len(saved.Accounts) != 1 || saved.Accounts[0].AccessToken != "sk-ant-oat01-refreshed-ok" {
		t.Fatalf("expected refreshed token persisted in gateway_config.json, got %+v", saved.Accounts)
	}

	// Also verify quota inspection marks critical when 401 persists and refresh fails
	mockUsage401 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
	}))
	defer mockUsage401.Close()
	ClaudeOAuthUsageEndpoint = mockUsage401.URL

	deadAcc := ConnectedAccount{
		ID:           "claude-dead",
		Provider:     ProviderClaudeCode,
		Email:        "expired@anthropic.example",
		AccessToken:  "sk-ant-oat01-revoked",
		RefreshToken: "bad-refresh-tok",
		ExpiresAt:    time.Now().Unix() + 3600,
	}
	detail := AccountQuotaDetail{ID: deadAcc.ID, Provider: ProviderClaudeCode}
	inspectClaudeCodeAccount(context.Background(), &deadAcc, &detail, "reset")
	if detail.Status != "critical" || detail.ErrorMessage == "" {
		t.Fatalf("expected critical status and Vietnamese error message on unrecoverable 401, got status=%q err=%q", detail.Status, detail.ErrorMessage)
	}
}

func TestClaudeCodeReactive401ForceRefreshRetry(t *testing.T) {
	tmpCfg := filepath.Join(t.TempDir(), "gateway_config.json")
	t.Setenv("GATEWAY_CONFIG_FILE", tmpCfg)
	resetAuthCacheForTest(t)
	t.Cleanup(func() { resetAuthCacheForTest(t) })

	GetGlobalCache().Clear()
	defer GetGlobalCache().Clear()

	origTokenEndpoint := ClaudeTokenEndpoint
	t.Cleanup(func() { ClaudeTokenEndpoint = origTokenEndpoint })

	var refreshCalls atomic.Int32
	mockTokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		refreshCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		// Intentionally return an OAuth access token without "sk-ant-oat" prefix to verify Bearer header handling
		_, _ = w.Write([]byte(`{"access_token":"claude-oauth-v2-fresh-token","refresh_token":"rt-claude-v2","expires_in":7200}`))
	}))
	defer mockTokenSrv.Close()
	ClaudeTokenEndpoint = mockTokenSrv.URL

	var upstreamCalls atomic.Int32
	mockClaudeAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callNum := upstreamCalls.Add(1)
		authHdr := r.Header.Get("Authorization")
		betaHdr := r.Header.Get("anthropic-beta")
		if !strings.Contains(betaHdr, "oauth-2025-04-20") {
			t.Errorf("call %d missing oauth anthropic-beta header: %q", callNum, betaHdr)
		}
		if authHdr == "Bearer sk-ant-oat01-revoked-early" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"type":"error","error":{"type":"authentication_error","message":"invalid bearer token"}}`))
			return
		}
		if authHdr != "Bearer claude-oauth-v2-fresh-token" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = fmt.Fprintf(w, `{"type":"error","error":{"type":"authentication_error","message":"unexpected auth %s"}}`, authHdr)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"id":"msg_reactive_ok",
			"type":"message",
			"role":"assistant",
			"content":[{"type":"text","text":"Claude reactive refresh succeeded!"}],
			"stop_reason":"end_turn",
			"usage":{"input_tokens":12,"output_tokens":8}
		}`))
	}))
	defer mockClaudeAPI.Close()

	// Account has ExpiresAt far in the future, simulating an early token revocation (HTTP 401)
	acc := ConnectedAccount{
		Provider:      ProviderClaudeCode,
		Email:         "team@claude.example",
		AccessToken:   "sk-ant-oat01-revoked-early",
		RefreshToken:  "rt-claude-v1",
		BaseURL:       mockClaudeAPI.URL,
		ExpiresAt:     time.Now().Unix() + 3600,
		CooldownUntil: 0,
	}
	if _, err := UpsertConnectedAccount(acc); err != nil {
		t.Fatalf("UpsertConnectedAccount failed: %v", err)
	}

	upstream := NewUpstream(GatewayConfig{})
	req := OpenAIChatRequest{
		Model: "claude-code/claude-sonnet-4-6",
		Messages: []OpenAIChatMessage{
			{Role: "user", Content: json.RawMessage(`"Ping Claude reactive refresh"`)},
		},
		Stream: false,
	}
	resolved := ResolveModel(req.Model, nil)
	rec := httptest.NewRecorder()

	if err := upstream.HandleChatCompletion(context.Background(), rec, req, resolved, ""); err != nil {
		t.Fatalf("HandleChatCompletion failed after reactive 401 refresh: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Claude reactive refresh succeeded!") {
		t.Fatalf("unexpected response body: %s", rec.Body.String())
	}
	if refreshCalls.Load() != 1 {
		t.Fatalf("expected 1 reactive token refresh, got %d", refreshCalls.Load())
	}
	if upstreamCalls.Load() != 2 {
		t.Fatalf("expected 2 Claude API calls (401 -> refresh -> 200), got %d", upstreamCalls.Load())
	}

	saved := LoadSavedAuthConfig()
	if len(saved.Accounts) != 1 || saved.Accounts[0].AccessToken != "claude-oauth-v2-fresh-token" {
		t.Fatalf("expected persisted refreshed token, got %+v", saved.Accounts)
	}
	if saved.Accounts[0].CooldownUntil != 0 {
		t.Fatalf("expected CooldownUntil == 0 after successful retry, got %d", saved.Accounts[0].CooldownUntil)
	}
}

func TestAntigravityCCPAReactive401BreaksCandidateLoopAndRefreshes(t *testing.T) {
	tmpCfg := filepath.Join(t.TempDir(), "gateway_config.json")
	t.Setenv("GATEWAY_CONFIG_FILE", tmpCfg)
	resetAuthCacheForTest(t)
	t.Cleanup(func() { resetAuthCacheForTest(t) })

	GetGlobalCache().Clear()
	defer GetGlobalCache().Clear()

	origOAuthTokenEndpoint := OAuthTokenEndpoint
	t.Cleanup(func() { OAuthTokenEndpoint = origOAuthTokenEndpoint })

	var refreshCalls atomic.Int32
	mockGoogleOAuth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		refreshCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"ya29.fresh-after-reactive-401","expires_in":3600}`))
	}))
	defer mockGoogleOAuth.Close()
	OAuthTokenEndpoint = mockGoogleOAuth.URL

	var ccpaCalls atomic.Int32
	mockCCPA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ccpaCalls.Add(1)
		authHdr := r.Header.Get("Authorization")
		if authHdr == "Bearer ya29.revoked-early-token" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"code":401,"message":"Request had invalid authentication credentials."}}`))
			return
		}
		if authHdr != "Bearer ya29.fresh-after-reactive-401" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = fmt.Fprintf(w, `{"error":{"code":401,"message":"unexpected token %s"}}`, authHdr)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: {\"response\":{\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"Antigravity Carbon recovered!\"}]}}]}}\n\ndata: [DONE]\n\n"))
	}))
	defer mockCCPA.Close()

	acc := ConnectedAccount{
		Provider:     ProviderAntigravity,
		Email:        "agy-engineer@gmail.com",
		AccessToken:  "ya29.revoked-early-token",
		RefreshToken: "1//0g-valid-refresh-token",
		ProjectID:    "agy-proj-123",
		ExpiresAt:    time.Now().Unix() + 3600,
	}
	if _, err := UpsertConnectedAccount(acc); err != nil {
		t.Fatalf("UpsertConnectedAccount failed: %v", err)
	}

	upstream := NewUpstream(GatewayConfig{
		CCPAEndpoint: mockCCPA.URL,
	})
	req := OpenAIChatRequest{
		Model: "gemini-4-carbon-high",
		Messages: []OpenAIChatMessage{
			{Role: "user", Content: json.RawMessage(`"Test Carbon 401 break and reactive refresh"`)},
		},
		Stream: false,
	}
	resolved := ResolveModel(req.Model, nil)
	rec := httptest.NewRecorder()

	if err := upstream.HandleChatCompletion(context.Background(), rec, req, resolved, ""); err != nil {
		t.Fatalf("HandleChatCompletion failed: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Antigravity Carbon recovered!") {
		t.Fatalf("unexpected response body: %s", rec.Body.String())
	}

	// gemini-4-carbon-high has 8 CCPA fallback candidates. Breaking immediately on HTTP 401
	// ensures exactly 2 CCPA requests total: 1st returns 401 -> break -> refresh -> 2nd returns 200!
	if got := ccpaCalls.Load(); got != 2 {
		t.Fatalf("expected exactly 2 CCPA calls (immediate break on 401 + 1 retry), got %d", got)
	}
	if got := refreshCalls.Load(); got != 1 {
		t.Fatalf("expected 1 Google OAuth refresh call, got %d", got)
	}

	saved := LoadSavedAuthConfig()
	if len(saved.Accounts) != 1 || saved.Accounts[0].AccessToken != "ya29.fresh-after-reactive-401" {
		t.Fatalf("expected refreshed Antigravity token persisted, got %+v", saved.Accounts)
	}
}

func TestVectorDBRejectsAndFiltersUnsupportedBanner(t *testing.T) {
	tmpDir := t.TempDir()
	tmpCfg := filepath.Join(tmpDir, "gateway_config.json")
	t.Setenv("GATEWAY_CONFIG_FILE", tmpCfg)
	resetAuthCacheForTest(t)

	vectorMu.Lock()
	origMemories := vectorMemories
	origLoaded := vectorLoadedLocal
	vectorMemories = nil
	vectorLoadedLocal = false
	vectorMu.Unlock()

	t.Cleanup(func() {
		vectorMu.Lock()
		vectorMemories = origMemories
		vectorLoadedLocal = origLoaded
		vectorMu.Unlock()
		resetAuthCacheForTest(t)
	})

	banner := "This version of Antigravity is no longer supported. Please download and install the latest version at https://antigravity.google/download"

	// 1. Seed a poisoned omni_vector_memories.json on disk and verify load strips poisoned entries
	seedDisk := []VectorMemoryEntry{
		{
			ID:         "vec-poisoned-1",
			CreatedAt:  time.Now().UTC().Format(time.RFC3339),
			Title:      "Poisoned Auto QA",
			Category:   "auto_qa",
			PromptText: "Làm sao tối ưu truy vấn SQL?",
			AnswerText: banner,
		},
		{
			ID:         "vec-clean-1",
			CreatedAt:  time.Now().UTC().Format(time.RFC3339),
			Title:      "Tối ưu HTTP/2 Transport trong Go",
			Category:   "coding",
			PromptText: "Cách cấu hình Go http.Transport tái sử dụng kết nối?",
			AnswerText: "Đặt MaxIdleConnsPerHost: 64 và ForceAttemptHTTP2: true trên SharedHTTPTransport.",
		},
	}
	rawSeed, err := json.MarshalIndent(seedDisk, "", "  ")
	if err != nil {
		t.Fatalf("failed to marshal seedDisk: %v", err)
	}
	if err := os.WriteFile(vectorFallbackFilePath(), rawSeed, 0o600); err != nil {
		t.Fatalf("failed to write seed vector file: %v", err)
	}

	loaded := ListVectorMemories(50)
	foundClean := false
	for _, m := range loaded {
		if m.ID == "vec-poisoned-1" || IsUnsupportedClientBanner(m.AnswerText) || IsUnsupportedClientBanner(m.PromptText) {
			t.Fatalf("poisoned deprecation banner memory leaked through ensureVectorMemoriesLoaded: %+v", m)
		}
		if m.ID == "vec-clean-1" {
			foundClean = true
		}
	}
	if !foundClean {
		t.Fatalf("expected clean memory vec-clean-1 to be preserved")
	}

	// 2. Direct AddVectorMemory must reject deprecation banner in answer or prompt
	if _, err := AddVectorMemory("Bad Memory", "auto_qa", "Valid question about Go", banner, "Tester", "", "gemini-4-carbon-high", false); err == nil {
		t.Fatalf("expected AddVectorMemory to reject unsupported client banner in answerText")
	}
	if _, err := AddVectorMemory("Bad Prompt", "auto_qa", banner, "Valid answer text that is long enough", "Tester", "", "gemini-4-carbon-high", false); err == nil {
		t.Fatalf("expected AddVectorMemory to reject unsupported client banner in promptText")
	}

	// 3. AutoLearnFromPromptLog must ignore poisoned responses
	beforeCount := len(ListVectorMemories(100))
	AutoLearnFromPromptLog(GatewayLogEntry{
		StatusCode:    http.StatusOK,
		IsError:       false,
		PromptText:    "[USER]: Hãy giải thích chi tiết kiến trúc microservices trong Go",
		ResponseText:  banner,
		EmployeeName:  "Dev",
		ResolvedModel: "antigravity/gemini-4-carbon-high",
	})
	time.Sleep(50 * time.Millisecond)
	afterCount := len(ListVectorMemories(100))
	if afterCount != beforeCount {
		t.Fatalf("expected AutoLearnFromPromptLog to skip poisoned banner (before=%d, after=%d)", beforeCount, afterCount)
	}
}
