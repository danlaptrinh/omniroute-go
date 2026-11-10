package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"
)

type mockEchoUpstream struct{}

func (m *mockEchoUpstream) HandleChatCompletion(
	ctx context.Context,
	w http.ResponseWriter,
	req OpenAIChatRequest,
	resolved ResolvedModel,
	incomingAuth string,
) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	return json.NewEncoder(w).Encode(OpenAIChatResponse{
		ID:      "chatcmpl-mock-echo",
		Object:  "chat.completion",
		Created: 1740000000,
		Model:   resolved.CanonicalID,
		Choices: []OpenAIChatChoice{
			{
				Index: 0,
				Message: &OpenAIOutputMessage{
					Role:    "assistant",
					Content: "echo:" + resolved.CanonicalID,
				},
				FinishReason: "stop",
			},
		},
		Usage: &OpenAIUsage{
			PromptTokens:     4,
			CompletionTokens: 6,
			TotalTokens:      10,
		},
	})
}

func TestGemini4CarbonResolutionAndCatalog(t *testing.T) {
	tmpCfg := filepath.Join(t.TempDir(), "gateway_config.json")
	t.Setenv("GATEWAY_CONFIG_FILE", tmpCfg)

	cases := []struct {
		input         string
		wantCanonical string
		wantThinking  ThinkingLevel
	}{
		{
			input:         "gemini-4-carbon-high",
			wantCanonical: "antigravity/gemini-4-carbon-high",
			wantThinking:  ThinkingLevelHigh,
		},
		{
			input:         "antigravity/gemini-4-carbon-high",
			wantCanonical: "antigravity/gemini-4-carbon-high",
			wantThinking:  ThinkingLevelHigh,
		},
		{
			input:         "gemini-4-carbon",
			wantCanonical: "antigravity/gemini-4-carbon-high",
			wantThinking:  ThinkingLevelHigh,
		},
		{
			input:         "carbon",
			wantCanonical: "antigravity/gemini-4-carbon-high",
			wantThinking:  ThinkingLevelHigh,
		},
		{
			input:         "carbon-high",
			wantCanonical: "antigravity/gemini-4-carbon-high",
			wantThinking:  ThinkingLevelHigh,
		},
		{
			input:         "gemini-4-carbon-medium",
			wantCanonical: "antigravity/gemini-4-carbon-medium",
			wantThinking:  ThinkingLevelMedium,
		},
		{
			input:         "gemini-4-carbon-low",
			wantCanonical: "antigravity/gemini-4-carbon-low",
			wantThinking:  ThinkingLevelLow,
		},
	}

	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got := ResolveModel(tc.input, nil)
			if got.Provider != ProviderAntigravity {
				t.Errorf("ResolveModel(%q).Provider = %q, want %q", tc.input, got.Provider, ProviderAntigravity)
			}
			if !got.IsCarbonFamily {
				t.Errorf("ResolveModel(%q).IsCarbonFamily = false, want true", tc.input)
			}
			if got.CanonicalID != tc.wantCanonical {
				t.Errorf("ResolveModel(%q).CanonicalID = %q, want %q", tc.input, got.CanonicalID, tc.wantCanonical)
			}
			if got.ThinkingLevel != tc.wantThinking {
				t.Errorf("ResolveModel(%q).ThinkingLevel = %q, want %q", tc.input, got.ThinkingLevel, tc.wantThinking)
			}
			if !got.IncludeThoughts {
				t.Errorf("ResolveModel(%q).IncludeThoughts = false, want true", tc.input)
			}
			if got.UpstreamModelID != "gemini-4-carbon" {
				t.Errorf("ResolveModel(%q).UpstreamModelID = %q, want gemini-4-carbon", tc.input, got.UpstreamModelID)
			}
			if len(got.FallbackChain) == 0 {
				t.Errorf("ResolveModel(%q).FallbackChain is empty, want non-empty fallback chain", tc.input)
			}
		})
	}

	// Verify catalog includes "antigravity/gemini-4-carbon-high" with Tier == "gen4-carbon"
	models := NewResolver().ListModels()
	foundCarbonCard := false
	for _, card := range models {
		if card.ID == "antigravity/gemini-4-carbon-high" {
			foundCarbonCard = true
			if card.Tier != "gen4-carbon" {
				t.Errorf("card %q Tier = %q, want %q", card.ID, card.Tier, "gen4-carbon")
			}
			if card.Provider != string(ProviderAntigravity) {
				t.Errorf("card %q Provider = %q, want %q", card.ID, card.Provider, ProviderAntigravity)
			}
			break
		}
	}
	if !foundCarbonCard {
		t.Fatalf("NewResolver().ListModels() missing antigravity/gemini-4-carbon-high")
	}

	// Verify model badge label
	badge := buildModelBadge("gemini-4-carbon-high")
	if !strings.Contains(badge, "Gemini 4 Carbon (High)") {
		t.Errorf("buildModelBadge(%q) = %q, want substring %q", "gemini-4-carbon-high", badge, "Gemini 4 Carbon (High)")
	}

	// Verify Carbon flagship cost estimation exceeds Argon
	carbonCost := EstimateModelCostUSD("antigravity/gemini-4-carbon-high", 1_000_000)
	argonCost := EstimateModelCostUSD("antigravity/gemini-4-argon-high", 1_000_000)
	if !(carbonCost > argonCost) {
		t.Errorf("EstimateModelCostUSD(carbon)=%.3f must be > EstimateModelCostUSD(argon)=%.3f", carbonCost, argonCost)
	}
}

func TestUTF8SafeTruncationAndSecretMasking(t *testing.T) {
	rawVN := "Xin chào hệ thống bảo mật dữ liệu tiếng Việt\x00có null byte"
	out := TruncateUTF8(rawVN, 12)

	if !utf8.ValidString(out) {
		t.Fatalf("TruncateUTF8 produced invalid UTF-8 string: %q", out)
	}
	if strings.Contains(out, "\x00") {
		t.Fatalf("TruncateUTF8 left NUL byte in output: %q", out)
	}
	const wantSuffix = " ...[truncated]"
	if !strings.HasSuffix(out, wantSuffix) {
		t.Fatalf("TruncateUTF8 output %q missing expected suffix %q", out, wantSuffix)
	}
	prefix := strings.TrimSuffix(out, wantSuffix)
	if gotRunes := utf8.RuneCountInString(prefix); gotRunes != 12 {
		t.Errorf("TruncateUTF8 prefix %q rune count = %d, want 12", prefix, gotRunes)
	}
	if prefix != "Xin chào hệ " {
		t.Errorf("TruncateUTF8 prefix = %q, want %q", prefix, "Xin chào hệ ")
	}

	// Also verify invalid UTF-8 bytes and NUL bytes are stripped when shorter than maxRunes
	corruptShort := string([]byte{'V', 'i', 0xff, 0xfe, 't', 0x00, '!'})
	cleanShort := TruncateUTF8(corruptShort, 50)
	if !utf8.ValidString(cleanShort) || strings.Contains(cleanShort, "\x00") {
		t.Errorf("TruncateUTF8 failed to sanitize short invalid/NUL string: %q", cleanShort)
	}

	// Test MaskSecretToken
	if got := MaskSecretToken(""); got != "" {
		t.Errorf("MaskSecretToken(\"\") = %q, want \"\"", got)
	}
	if got := MaskSecretToken("short123"); got != "****" {
		t.Errorf("MaskSecretToken(\"short123\") = %q, want \"****\"", got)
	}
	longTok := "2f9AbCdEfGhIjKlMnOp1234"
	maskedLong := MaskSecretToken(longTok)
	if maskedLong != "2f9A****1234" {
		t.Errorf("MaskSecretToken(%q) = %q, want %q", longTok, maskedLong, "2f9A****1234")
	}
	if strings.Contains(maskedLong, "bCdEfGhIjKlMnOp") {
		t.Errorf("MaskSecretToken leaked middle secret characters: %q", maskedLong)
	}

	// Test escapeILIKE
	gotEscaped := escapeILIKE(`a%b_c\d`)
	wantEscaped := `a\%b\_c\\d`
	if gotEscaped != wantEscaped {
		t.Errorf("escapeILIKE(%q) = %q, want %q", `a%b_c\d`, gotEscaped, wantEscaped)
	}
}

func TestCCPAUnsupportedBannerFailover(t *testing.T) {
	tmpCfg := filepath.Join(t.TempDir(), "gateway_config.json")
	t.Setenv("GATEWAY_CONFIG_FILE", tmpCfg)

	cache := GetGlobalCache()
	cache.Clear()
	defer cache.Clear()

	deprecationBanner := "This version of Antigravity is no longer supported. Please download and install the latest version at https://antigravity.google/download"
	if !IsUnsupportedClientBanner(deprecationBanner) {
		t.Fatalf("expected IsUnsupportedClientBanner to detect deprecation text")
	}

	mockCCPA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(
			w,
			"data: {\"response\":{\"candidates\":[{\"content\":{\"parts\":[{\"text\":%q}]}}]}}\n\ndata: [DONE]\n\n",
			deprecationBanner,
		)
	}))
	defer mockCCPA.Close()

	cfg := GatewayConfig{
		CCPAEndpoint:  mockCCPA.URL,
		AgyOAuthToken: "ya29.mock-ccpa-token",
		AgyProjectID:  "mock-ccpa-proj",
	}
	upstream := NewUpstream(cfg)

	for _, streamMode := range []bool{false, true} {
		modeName := "non_stream"
		if streamMode {
			modeName = "stream"
		}
		t.Run(modeName, func(t *testing.T) {
			cache.Clear()
			zeroTemp := 0.0
			req := OpenAIChatRequest{
				Model: "antigravity/gemini-4-carbon-high",
				Messages: []OpenAIChatMessage{
					{Role: "user", Content: json.RawMessage(`"ping"`)},
				},
				Temperature: &zeroTemp,
				Stream:      streamMode,
			}
			resolved := ResolveModel(req.Model, nil)
			resolved.FallbackChain = nil

			rec := httptest.NewRecorder()
			err := upstream.HandleChatCompletion(context.Background(), rec, req, resolved, "")
			if err == nil {
				t.Fatalf("expected GatewayUpstream to reject CCPA unsupported client banner (stream=%v), got nil error", streamMode)
			}
			if rec.Code == http.StatusOK {
				t.Fatalf("expected non-200 status when CCPA returns unsupported client banner (stream=%v), got 200 OK with body: %s", streamMode, rec.Body.String())
			}
			if rec.Code != http.StatusBadGateway {
				t.Errorf("expected HTTP 502 BadGateway (stream=%v), got %d", streamMode, rec.Code)
			}
			if strings.Contains(rec.Body.String(), "data: [DONE]") {
				t.Errorf("unexpected SSE completion stream emitted on rejected CCPA banner: %s", rec.Body.String())
			}
			var chatResp OpenAIChatResponse
			if decErr := json.Unmarshal(rec.Body.Bytes(), &chatResp); decErr == nil && len(chatResp.Choices) > 0 {
				t.Errorf("expected zero completion choices on rejected CCPA banner, got %+v", chatResp.Choices)
			}
			if _, cached := cache.Get(req); cached {
				t.Errorf("rejected CCPA deprecation banner must never be stored in ResponseCache (stream=%v)", streamMode)
			}
		})
	}
}

func TestVectorDBConcurrentLoadAndPinnedProtection(t *testing.T) {
	tmpDir := t.TempDir()
	tmpCfg := filepath.Join(tmpDir, "gateway_config.json")
	t.Setenv("GATEWAY_CONFIG_FILE", tmpCfg)

	// Seed a local fallback JSON file with missing Embedding so cold-start loading exercises backfill under contention.
	seedEntries := []VectorMemoryEntry{
		{
			ID:         "mem-seed-1",
			Title:      "Kiến trúc bảo mật OmniRoute",
			Category:   "security",
			PromptText: "Cấu hình xác thực RBAC cho API Gateway",
			AnswerText: "Bắt buộc bật AdminAccess hoặc omni_session cho các endpoint quản trị.",
			Pinned:     true,
		},
	}
	if seedBytes, err := json.Marshal(seedEntries); err == nil {
		_ = os.WriteFile(filepath.Join(tmpDir, "omni_vector_memories.json"), seedBytes, 0o600)
	}

	vectorMu.Lock()
	vectorMemories = nil
	vectorLoadedLocal = false
	vectorMu.Unlock()

	defer func() {
		FlushPendingConfigSave()
		vectorMu.Lock()
		vectorMemories = nil
		vectorLoadedLocal = false
		vectorMu.Unlock()
	}()

	const workers = 20
	startCh := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(workers)

	for i := 0; i < workers; i++ {
		go func(idx int) {
			defer wg.Done()
			<-startCh
			switch idx % 4 {
			case 0:
				_ = SearchVectorMemories("bảo mật RBAC gateway", 5, 0.10)
			case 1:
				_ = ListVectorMemories(50)
			case 2:
				status := GetVectorDBStatus()
				if status["embedding_dim"] != VectorEmbeddingDim {
					t.Errorf("unexpected embedding_dim: %v", status["embedding_dim"])
				}
			case 3:
				_, err := AddVectorMemory(
					fmt.Sprintf("Concurrent Note %d", idx),
					"concurrency_test",
					fmt.Sprintf("Prompt kiểm thử đồng thời worker số %d", idx),
					fmt.Sprintf("Phản hồi kiểm thử đồng thời an toàn bộ nhớ số %d", idx),
					"QA Tester",
					"key-qa",
					"antigravity/gemini-4-carbon-high",
					idx%8 == 3,
				)
				if err != nil {
					t.Errorf("AddVectorMemory(%d) failed: %v", idx, err)
				}
			}
		}(i)
	}

	close(startCh)
	wg.Wait()
	// Allow asynchronous saveVectorMemoriesLocalAsync goroutines to finish writing inside tmpDir before cleanup.
	time.Sleep(80 * time.Millisecond)

	// Verify capVectorMemories never evicts Pinned == true entries even when total > MaxInMemoryVectors.
	const pinnedTarget = 450
	const unpinnedTarget = MaxInMemoryVectors - 100 // 1900 + 450 = 2350 > 2000
	oversized := make([]VectorMemoryEntry, 0, unpinnedTarget+pinnedTarget)
	for i := 0; i < unpinnedTarget; i++ {
		oversized = append(oversized, VectorMemoryEntry{
			ID:         fmt.Sprintf("auto-%d", i),
			Title:      fmt.Sprintf("Auto Memory %d", i),
			PromptText: "Auto prompt",
			AnswerText: "Auto answer",
			Pinned:     false,
		})
	}
	// Place all Pinned entries at the tail where naive slice truncation would drop them.
	for i := 0; i < pinnedTarget; i++ {
		oversized = append(oversized, VectorMemoryEntry{
			ID:         fmt.Sprintf("pinned-%d", i),
			Title:      fmt.Sprintf("Pinned SOP %d", i),
			PromptText: "Critical SOP",
			AnswerText: "Never evict",
			Pinned:     true,
		})
	}

	capped := capVectorMemories(oversized, MaxInMemoryVectors)
	if len(capped) != MaxInMemoryVectors {
		t.Fatalf("len(capped) = %d, want %d", len(capped), MaxInMemoryVectors)
	}
	preservedPinned := 0
	for _, item := range capped {
		if item.Pinned {
			preservedPinned++
		}
	}
	if preservedPinned != pinnedTarget {
		t.Fatalf("capVectorMemories preserved %d pinned entries, want all %d", preservedPinned, pinnedTarget)
	}

	// Boundary case: even if every single entry is Pinned and count > MaxInMemoryVectors, zero pinned entries are dropped.
	allPinnedTotal := MaxInMemoryVectors + 25
	allPinned := make([]VectorMemoryEntry, allPinnedTotal)
	for i := 0; i < allPinnedTotal; i++ {
		allPinned[i] = VectorMemoryEntry{
			ID:     fmt.Sprintf("strict-pin-%d", i),
			Pinned: true,
		}
	}
	cappedAllPinned := capVectorMemories(allPinned, MaxInMemoryVectors)
	if len(cappedAllPinned) != allPinnedTotal {
		t.Errorf("capVectorMemories with %d pinned entries returned %d, want all %d preserved", allPinnedTotal, len(cappedAllPinned), allPinnedTotal)
	}
}

func TestAdminRBACAndEndpointHardening(t *testing.T) {
	tmpCfg := filepath.Join(t.TempDir(), "gateway_config.json")
	t.Setenv("GATEWAY_CONFIG_FILE", tmpCfg)
	defer FlushPendingConfigSave()

	GetGlobalCache().Clear()

	srv := NewServer(GatewayConfig{}, NewResolver(), &mockEchoUpstream{})
	handler := srv.Handler()

	if err := SetupAdminPassword("StrongAdmin#2026"); err != nil {
		t.Fatalf("SetupAdminPassword failed: %v", err)
	}

	// 1. Unauthenticated requests to protected management endpoints must return 401 Unauthorized.
	unauthChecks := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodGet, "/v1/keys", ""},
		{http.MethodPost, "/v1/database", `{"dsn":"postgres://localhost:5432/omni"}`},
		{http.MethodGet, "/v1/logs", ""},
		{http.MethodGet, "/v1/endpoint", ""},
	}
	for _, check := range unauthChecks {
		req := httptest.NewRequest(check.method, check.path, bytes.NewBufferString(check.body))
		if check.body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("unauthenticated %s %s returned %d, want 401 Unauthorized (body: %s)", check.method, check.path, rec.Code, rec.Body.String())
		}
	}

	// 2. Employee (non-admin) API key CAN call /v1/chat/completions, but gets 401 on /v1/keys and /v1/database.
	empKey, err := CreateAPIKey("emp-1", false, true, true, false)
	if err != nil {
		t.Fatalf("CreateAPIKey(emp-1) failed: %v", err)
	}

	chatReq := httptest.NewRequest(
		http.MethodPost,
		"/v1/chat/completions",
		bytes.NewBufferString(`{"model":"gemini-4-carbon-high","messages":[{"role":"user","content":"hi"}]}`),
	)
	chatReq.Header.Set("Content-Type", "application/json")
	chatReq.Header.Set("Authorization", "Bearer "+empKey.Key)
	chatRec := httptest.NewRecorder()
	handler.ServeHTTP(chatRec, chatReq)
	if chatRec.Code != http.StatusOK {
		t.Fatalf("employee key POST /v1/chat/completions returned %d, want 200 OK: %s", chatRec.Code, chatRec.Body.String())
	}

	empKeysReq := httptest.NewRequest(http.MethodGet, "/v1/keys", nil)
	empKeysReq.Header.Set("Authorization", "Bearer "+empKey.Key)
	empKeysRec := httptest.NewRecorder()
	handler.ServeHTTP(empKeysRec, empKeysReq)
	if empKeysRec.Code != http.StatusUnauthorized {
		t.Errorf("employee key GET /v1/keys returned %d, want 401 Unauthorized", empKeysRec.Code)
	}

	empDBReq := httptest.NewRequest(http.MethodPost, "/v1/database", bytes.NewBufferString(`{"dsn":"postgres://localhost:5432/omni"}`))
	empDBReq.Header.Set("Content-Type", "application/json")
	empDBReq.Header.Set("Authorization", "Bearer "+empKey.Key)
	empDBRec := httptest.NewRecorder()
	handler.ServeHTTP(empDBRec, empDBReq)
	if empDBRec.Code != http.StatusUnauthorized {
		t.Errorf("employee key POST /v1/database returned %d, want 401 Unauthorized", empDBRec.Code)
	}

	// 3. Admin API key (AdminAccess: true) OR valid CreateSession() cookie (omni_session) CAN access GET /v1/keys,
	//    and the JSON payload omits plaintext "key" while including "masked_key".
	adminKey, err := CreateAPIKey("admin-1", true, true, true, true)
	if err != nil {
		t.Fatalf("CreateAPIKey(admin-1) failed: %v", err)
	}
	sessToken := CreateSession()
	defer RevokeSession(sessToken)

	assertSanitizedKeysResponse := func(t *testing.T, rec *httptest.ResponseRecorder, authMode string) {
		t.Helper()
		if rec.Code != http.StatusOK {
			t.Fatalf("%s GET /v1/keys returned %d, want 200 OK: %s", authMode, rec.Code, rec.Body.String())
		}
		rawBody := rec.Body.String()
		if strings.Contains(rawBody, empKey.Key) || strings.Contains(rawBody, adminKey.Key) {
			t.Fatalf("%s GET /v1/keys leaked plaintext API key secret in body: %s", authMode, rawBody)
		}
		var payload struct {
			Data []map[string]any `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Fatalf("%s GET /v1/keys invalid JSON: %v", authMode, err)
		}
		if len(payload.Data) < 2 {
			t.Fatalf("%s GET /v1/keys returned %d keys, want at least 2", authMode, len(payload.Data))
		}
		for i, item := range payload.Data {
			if rawVal, exists := item["key"]; exists && rawVal != "" {
				t.Errorf("%s GET /v1/keys data[%d] unexpectedly exposed \"key\": %v", authMode, i, rawVal)
			}
			maskedVal, _ := item["masked_key"].(string)
			if !strings.HasPrefix(maskedVal, "sk-omni-...") {
				t.Errorf("%s GET /v1/keys data[%d] missing valid \"masked_key\", got %q", authMode, i, maskedVal)
			}
		}
	}

	adminKeysReq := httptest.NewRequest(http.MethodGet, "/v1/keys", nil)
	adminKeysReq.Header.Set("Authorization", "Bearer "+adminKey.Key)
	adminKeysRec := httptest.NewRecorder()
	handler.ServeHTTP(adminKeysRec, adminKeysReq)
	assertSanitizedKeysResponse(t, adminKeysRec, "admin_api_key")

	cookieKeysReq := httptest.NewRequest(http.MethodGet, "/v1/keys", nil)
	cookieKeysReq.AddCookie(&http.Cookie{
		Name:  SessionCookieName,
		Value: sessToken,
	})
	cookieKeysRec := httptest.NewRecorder()
	handler.ServeHTTP(cookieKeysRec, cookieKeysReq)
	assertSanitizedKeysResponse(t, cookieKeysRec, "omni_session_cookie")

	// 4. POST /v1/database with SSRF metadata IP or non-PostgreSQL scheme is rejected with 400 Bad Request.
	ssrfTargets := []string{
		"postgres://u:p@169.254.169.254:5432/db",
		"http://evil.com",
	}
	for _, targetDSN := range ssrfTargets {
		bodyBytes, _ := json.Marshal(map[string]string{"dsn": targetDSN})
		dbReq := httptest.NewRequest(http.MethodPost, "/v1/database", bytes.NewBufferString(string(bodyBytes)))
		dbReq.Header.Set("Content-Type", "application/json")
		dbReq.Header.Set("Authorization", "Bearer "+adminKey.Key)
		dbRec := httptest.NewRecorder()
		handler.ServeHTTP(dbRec, dbReq)
		if dbRec.Code != http.StatusBadRequest {
			t.Errorf("POST /v1/database with DSN %q returned %d, want 400 Bad Request (body: %s)", targetDSN, dbRec.Code, dbRec.Body.String())
		}
	}

	// 5. GET /oauth/callback?code=secretcode&state=origin=https://evil.attacker.com must NOT issue a 302/307 redirect leaking secretcode.
	mockTokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"mocked rejection"}`))
	}))
	defer mockTokenSrv.Close()

	origTokenEP := OAuthTokenEndpoint
	OAuthTokenEndpoint = mockTokenSrv.URL
	defer func() { OAuthTokenEndpoint = origTokenEP }()

	cbReq := httptest.NewRequest(
		http.MethodGet,
		"http://127.0.0.1:20128/oauth/callback?code=secretcode&state=origin=https://evil.attacker.com",
		nil,
	)
	cbRec := httptest.NewRecorder()
	handler.ServeHTTP(cbRec, cbReq)

	if cbRec.Code == http.StatusFound || cbRec.Code == http.StatusTemporaryRedirect || (cbRec.Code >= 300 && cbRec.Code < 400) {
		t.Fatalf("GET /oauth/callback issued unexpected redirect %d (Location: %q)", cbRec.Code, cbRec.Header().Get("Location"))
	}
	if loc := cbRec.Header().Get("Location"); strings.Contains(loc, "evil.attacker.com") || strings.Contains(loc, "secretcode") {
		t.Fatalf("GET /oauth/callback leaked untrusted origin or code in Location header: %q", loc)
	}
	if strings.Contains(cbRec.Body.String(), "evil.attacker.com") {
		t.Errorf("GET /oauth/callback rendered untrusted attacker origin in HTML body: %s", cbRec.Body.String())
	}
}
