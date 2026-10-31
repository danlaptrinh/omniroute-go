package gateway

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

type stubResolver struct{}

func (s *stubResolver) Resolve(modelID string, defaultFallbacks []string) ResolvedModel {
	fbs := append([]string{}, defaultFallbacks...)
	if len(fbs) == 0 {
		fbs = []string{"antigravity/claude-opus-4-6-thinking", "antigravity/gemini-pro-agent"}
	}
	return ResolvedModel{
		RequestedID:     modelID,
		Provider:        ProviderAntigravity,
		CanonicalID:     "antigravity/gemini-4-argon-high",
		UpstreamModelID: "gemini-4-argon",
		CCPAModelID:     "gemini-4-argon",
		OmniBaseModelID: "antigravity/gemini-4-argon",
		ThinkingLevel:   ThinkingLevelHigh,
		ReasoningEffort: "high",
		IncludeThoughts: true,
		PlaceholderEnum: "MODEL_PLACEHOLDER_M417",
		FallbackChain:   fbs,
		IsArgonFamily:   true,
	}
}

func (s *stubResolver) ListModels() []OpenAIModelCard {
	return []OpenAIModelCard{
		{ID: "gemini-4-argon-high", Object: "model", OwnedBy: "antigravity", Provider: string(ProviderAntigravity)},
		{ID: "antigravity/gemini-4-argon-high", Object: "model", OwnedBy: "antigravity", Provider: string(ProviderAntigravity)},
	}
}

func (s *stubResolver) ListCombos() []ComboModel {
	return []ComboModel{
		{ID: "auto/best-coding", Name: "Best Coding Combo", Description: "Argon to Claude to Qwen", Models: []string{"antigravity/gemini-4-argon-high"}},
	}
}

func TestOAuthURLAndCallbackExchangeFlow(t *testing.T) {
	tmpCfg := filepath.Join(t.TempDir(), "gateway_config.json")
	t.Setenv("GATEWAY_CONFIG_FILE", tmpCfg)

	// 1. Verify BuildOAuthAuthURL matches the exact Antigravity client_id and scopes
	authURL := BuildOAuthAuthURL(AgyOAuthDefaultRedirectURI, "teststate123")
	if !strings.Contains(authURL, "client_id="+AgyOAuthClientID) {
		t.Fatalf("expected client_id in authURL, got %s", authURL)
	}
	if !strings.Contains(authURL, "redirect_uri=http%3A%2F%2F127.0.0.1%3A20128%2Fcallback") {
		t.Fatalf("expected encoded redirect_uri in authURL, got %s", authURL)
	}
	if !strings.Contains(authURL, "access_type=offline") || !strings.Contains(authURL, "prompt=consent") {
		t.Fatalf("expected access_type=offline&prompt=consent in authURL, got %s", authURL)
	}

	// 2. Mock Google OAuth token endpoint, userinfo endpoint, and CCPA loadCodeAssist
	mockGoogle := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			_ = r.ParseForm()
			if r.FormValue("code") != "4/0AeanS0testcode" {
				t.Errorf("unexpected code: %q", r.FormValue("code"))
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"ya29.oauth-exchanged-token","refresh_token":"1//0gtestrefresh","expires_in":3600,"token_type":"Bearer"}`))
		case "/userinfo":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"email":"ultra.user@gmail.com"}`))
		case "/loadCodeAssist":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"cloudaicompanionProject":"agy-ultra-proj-999"}`))
		case "/ccpaStream":
			if got := r.Header.Get("Authorization"); got != "Bearer ya29.oauth-exchanged-token" {
				t.Errorf("expected Bearer ya29.oauth-exchanged-token, got %q", got)
			}
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["project"] != "agy-ultra-proj-999" {
				t.Errorf("expected discovered project agy-ultra-proj-999, got %v", body["project"])
			}
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprintf(w, "data: {\"response\":{\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"OAuth Argon High Ready!\"}]}}]}}\n\n")
		}
	}))
	defer mockGoogle.Close()

	origTokenEP := OAuthTokenEndpoint
	origUserEP := OAuthUserInfoEndpoint
	origAssistEP := CCPALoadCodeAssistEndpoint
	OAuthTokenEndpoint = mockGoogle.URL + "/token"
	OAuthUserInfoEndpoint = mockGoogle.URL + "/userinfo"
	CCPALoadCodeAssistEndpoint = mockGoogle.URL + "/loadCodeAssist"
	defer func() {
		OAuthTokenEndpoint = origTokenEP
		OAuthUserInfoEndpoint = origUserEP
		CCPALoadCodeAssistEndpoint = origAssistEP
	}()

	cfg := GatewayConfig{
		CCPAEndpoint: mockGoogle.URL + "/ccpaStream",
	}
	srv := NewServer(cfg, &stubResolver{}, NewUpstream(cfg))
	gw := httptest.NewServer(srv.Handler())
	defer gw.Close()

	// Exchange pasted full redirect URL via POST /v1/oauth/exchange
	pastedURL := "http://127.0.0.1:20128/callback?code=4%2F0AeanS0testcode&scope=https://www.googleapis.com/auth/cloud-platform&state=teststate123"
	exReq, _ := json.Marshal(map[string]string{"input": pastedURL})
	exResp, err := http.Post(gw.URL+"/v1/oauth/exchange", "application/json", bytes.NewReader(exReq))
	if err != nil {
		t.Fatalf("POST /v1/oauth/exchange failed: %v", err)
	}
	defer exResp.Body.Close()
	if exResp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(exResp.Body)
		t.Fatalf("expected 200 OK from /v1/oauth/exchange, got %d: %s", exResp.StatusCode, string(b))
	}

	var exData map[string]any
	_ = json.NewDecoder(exResp.Body).Decode(&exData)
	if exData["email"] != "ultra.user@gmail.com" || exData["project_id"] != "agy-ultra-proj-999" {
		t.Fatalf("unexpected exchange response: %+v", exData)
	}

	// Verify /v1/chat/completions now automatically uses the exchanged OAuth account and project ID
	chatResp, err := http.Post(gw.URL+"/v1/chat/completions", "application/json", strings.NewReader(`{"model":"gemini-4-argon-high","messages":[{"role":"user","content":"Hello"}]}`))
	if err != nil {
		t.Fatalf("chat request failed: %v", err)
	}
	defer chatResp.Body.Close()
	if chatResp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(chatResp.Body)
		t.Fatalf("expected 200 OK, got %d: %s", chatResp.StatusCode, string(b))
	}
	if got := chatResp.Header.Get("X-Omni-Gateway-Account"); got != "ultra.user@gmail.com" {
		t.Errorf("expected X-Omni-Gateway-Account header ultra.user@gmail.com, got %q", got)
	}
}

func TestStandaloneModeUIAndAuthConfigReload(t *testing.T) {
	tmpCfg := filepath.Join(t.TempDir(), "gateway_config.json")
	t.Setenv("GATEWAY_CONFIG_FILE", tmpCfg)

	mockCCPA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer ya29.saved-from-ui-token" {
			t.Errorf("expected saved token from gateway_config.json, got %q", got)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, "data: {\"response\":{\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"Standalone Thinking\",\"thought\":true},{\"text\":\"Hello from Standalone AI Gateway!\"}]}}]}}\n\n")
	}))
	defer mockCCPA.Close()

	cfg := GatewayConfig{
		CCPAEndpoint: mockCCPA.URL,
	}
	srv := NewServer(cfg, &stubResolver{}, NewUpstream(cfg))
	gw := httptest.NewServer(srv.Handler())
	defer gw.Close()

	uiResp, err := http.Get(gw.URL + "/ui")
	if err != nil || uiResp.StatusCode != http.StatusOK {
		t.Fatalf("expected /ui 200 OK, got err=%v status=%d", err, uiResp.StatusCode)
	}
	_ = uiResp.Body.Close()

	reqBody := `{"model":"gemini-4-argon-high","messages":[{"role":"user","content":"Hi"}]}`
	unauthResp, err := http.Post(gw.URL+"/v1/chat/completions", "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("POST failed: %v", err)
	}
	_ = unauthResp.Body.Close()
	if unauthResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 when no token is configured in standalone mode, got %d", unauthResp.StatusCode)
	}

	saveBody := `{"agy_oauth_token":"ya29.saved-from-ui-token","agy_project_id":"test-proj"}`
	saveResp, err := http.Post(gw.URL+"/v1/auth/config", "application/json", strings.NewReader(saveBody))
	if err != nil || saveResp.StatusCode != http.StatusOK {
		t.Fatalf("POST /v1/auth/config failed: err=%v status=%d", err, saveResp.StatusCode)
	}
	_ = saveResp.Body.Close()

	chatHTTPResp, err := http.Post(gw.URL+"/v1/chat/completions", "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("POST chat failed: %v", err)
	}
	defer chatHTTPResp.Body.Close()
	if chatHTTPResp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(chatHTTPResp.Body)
		t.Fatalf("expected 200 OK after saving token, got %d: %s", chatHTTPResp.StatusCode, string(b))
	}

	var parsed OpenAIChatResponse
	if err := json.NewDecoder(chatHTTPResp.Body).Decode(&parsed); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if len(parsed.Choices) != 1 || parsed.Choices[0].Message.Content != "Hello from Standalone AI Gateway!" {
		t.Errorf("unexpected response choices: %+v", parsed.Choices)
	}
}

func TestServerOmniRouteFallbackAndSSEAggregation(t *testing.T) {
	t.Setenv("GATEWAY_CONFIG_FILE", filepath.Join(t.TempDir(), "nonexistent.json"))

	var attempts atomic.Int32
	mockOmni := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := attempts.Add(1)
		body, _ := io.ReadAll(r.Body)
		var req OpenAIChatRequest
		_ = json.Unmarshal(body, &req)

		if count == 1 {
			if req.Model != "antigravity/gemini-4-argon" {
				t.Errorf("expected first candidate antigravity/gemini-4-argon, got %s", req.Model)
			}
			if req.ThinkingConfig == nil || req.ThinkingConfig.ThinkingLevel != ThinkingLevelHigh {
				t.Errorf("expected ThinkingLevelHigh in upstream request, got %+v", req.ThinkingConfig)
			}
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":{"message":"Model antigravity/gemini-4-argon not found (404)"}}`))
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, "data: {\"id\":\"chatcmpl-123\",\"object\":\"chat.completion.chunk\",\"created\":1700000000,\"model\":%q,\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"reasoning_content\":\"Thinking deeply...\",\"content\":\"Hello from \"}}]}\n\n", req.Model)
		_, _ = fmt.Fprintf(w, "data: {\"id\":\"chatcmpl-123\",\"object\":\"chat.completion.chunk\",\"created\":1700000000,\"model\":%q,\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Argon Gateway!\"},\"finish_reason\":\"stop\"}]}\n\n", req.Model)
		_, _ = fmt.Fprintf(w, "data: [DONE]\n\n")
	}))
	defer mockOmni.Close()

	cfg := GatewayConfig{
		OmniRouteBaseURL: mockOmni.URL + "/v1",
		OmniRouteAPIKey:  "sk-test-key",
		FallbackModels:   []string{"antigravity/claude-opus-4-6-thinking"},
	}

	srv := NewServer(cfg, &stubResolver{}, NewUpstream(cfg))
	gw := httptest.NewServer(srv.Handler())
	defer gw.Close()

	reqBody := `{"model":"gemini-4-argon-high","messages":[{"role":"user","content":"Hi"}],"stream":false}`
	resp, err := http.Post(gw.URL+"/v1/chat/completions", "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("POST failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200 OK, got %d: %s", resp.StatusCode, string(b))
	}

	var chatResp OpenAIChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		t.Fatalf("failed to decode response JSON: %v", err)
	}
	if chatResp.Model != "gemini-4-argon-high" {
		t.Errorf("expected response model rewritten to gemini-4-argon-high, got %q", chatResp.Model)
	}
}

func TestServerDirectCCPAStreamBridge(t *testing.T) {
	t.Setenv("GATEWAY_CONFIG_FILE", filepath.Join(t.TempDir(), "nonexistent.json"))

	mockCCPA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer ya29.test-oauth-token" {
			t.Errorf("unexpected Authorization header: %q", got)
		}
		var payload map[string]any
		_ = json.NewDecoder(r.Body).Decode(&payload)
		if payload["model"] != "gemini-4-argon" {
			t.Errorf("expected CCPA model gemini-4-argon, got %v", payload["model"])
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, "data: {\"response\":{\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"Step 1\",\"thought\":true},{\"text\":\"Direct Argon Output\"}]}}]}}\n\n")
	}))
	defer mockCCPA.Close()

	cfg := GatewayConfig{
		CCPAEndpoint: mockCCPA.URL,
	}
	srv := NewServer(cfg, &stubResolver{}, NewUpstream(cfg))
	gw := httptest.NewServer(srv.Handler())
	defer gw.Close()

	reqBody := bytes.NewBufferString(`{"model":"gemini-4-argon-high","messages":[{"role":"system","content":"Be concise"},{"role":"user","content":"Ping"}],"stream":true}`)
	httpReq, _ := http.NewRequest(http.MethodPost, gw.URL+"/v1/chat/completions", reqBody)
	httpReq.Header.Set("Authorization", "Bearer ya29.test-oauth-token")
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	rawSSE, _ := io.ReadAll(resp.Body)
	sseStr := string(rawSSE)
	if !strings.Contains(sseStr, "Direct Argon Output") {
		t.Errorf("expected SSE output to contain 'Direct Argon Output', got:\n%s", sseStr)
	}
	if !strings.Contains(sseStr, "data: [DONE]") {
		t.Errorf("expected SSE output to end with data: [DONE], got:\n%s", sseStr)
	}
}
