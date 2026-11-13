package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const GatewayVersion = "2.0.0-omniroute-go"

var (
	cacheLogSeq                 atomic.Uint64
	fallbackAccountProxyByScope sync.Map
)

// Server exposes the OmniRoute-Go v2.0 HTTP API (/v1/models, /v1/chat/completions, /v1/messages,
// /v1/providers, /v1/providers/strategy, /v1/providers/alibaba, /v1/oauth/url, /v1/oauth/exchange,
// /v1/oauth/claude/url, /v1/oauth/claude/exchange, /v1/oauth/account, /callback, /v1/auth/config,
// /v1/keys, /v1/logs, /ui).
type Server struct {
	cfg      GatewayConfig
	resolver ModelResolver
	upstream UpstreamHandler
}

// NewServer constructs a new OmniRoute-Go HTTP server.
func NewServer(cfg GatewayConfig, resolver ModelResolver, upstream UpstreamHandler) *Server {
	s := &Server{
		cfg:      cfg,
		resolver: resolver,
		upstream: upstream,
	}
	RefreshVectorDBFastState()
	s.StartAutomationScheduler()
	return s
}

// IsManagementAuthRequired returns true when either an admin password or at least one API key is configured.
func IsManagementAuthRequired() bool {
	return IsAdminConfigured() || len(ListAPIKeys()) > 0
}

func isAdminAuthorized(r *http.Request) bool {
	if !IsManagementAuthRequired() {
		return true
	}
	if r == nil {
		return false
	}
	if IsAdminConfigured() {
		if authed, _ := IsAdminRequestAuthenticated(r); authed {
			return true
		}
	}
	if tok := GetTokenFromRequest(r); tok != "" && ValidateSession(tok) {
		return true
	}
	authHeader := r.Header.Get("Authorization")
	if strings.TrimSpace(authHeader) == "" {
		authHeader = r.Header.Get("X-API-Key")
	}
	if strings.TrimSpace(authHeader) == "" {
		authHeader = r.Header.Get("X-Admin-Token")
	}
	valid, matched := ValidateAPIKey(authHeader)
	if valid && matched != nil && matched.AdminAccess {
		return true
	}
	return false
}

func (s *Server) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !isAdminAuthorized(r) {
			writeOpenAIError(w, http.StatusUnauthorized, "admin authentication required", "unauthorized")
			return
		}
		next(w, r)
	}
}

// StartPDCAWatchdog launches the background 60-second PDCA self-healing watchdog loop bound to ctx.
func (s *Server) StartPDCAWatchdog(ctx context.Context) {
	StartPDCAWatchdog(ctx, 60*time.Second)
}

func (s *Server) handlePDCAStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost || r.URL.Query().Get("run") == "true" || r.URL.Query().Get("refresh") == "true" {
		if !isAdminAuthorized(r) {
			writeOpenAIError(w, http.StatusUnauthorized, "admin authentication required for on-demand PDCA cycle trigger", "unauthorized")
			return
		}
	}
	HandlePDCAStatus(w, r)
}

// Handler returns the root http.Handler with CORS and GZIP middleware applied.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleRootOrHealth)
	mux.HandleFunc("/health", s.handleRootOrHealth)
	mux.HandleFunc("/healthz", s.handleRootOrHealth)
	mux.HandleFunc("/v1/health", s.handleRootOrHealth)
	mux.HandleFunc("/api/pdca/status", s.handlePDCAStatus)
	mux.HandleFunc("/v1/pdca/status", s.handlePDCAStatus)
	mux.HandleFunc("/ui", s.handleUI)
	mux.HandleFunc("/dashboard", s.handleUI)
	mux.HandleFunc("/dashboard/", s.handleUI)
	mux.HandleFunc("/dashboard/endpoint", s.handleUI)
	mux.HandleFunc("/callback", s.handleOAuthCallback)
	mux.HandleFunc("/oauth/callback", s.handleOAuthCallback)
	mux.HandleFunc("/v1/endpoint", s.requireAdmin(s.handleEndpointAPI))
	mux.HandleFunc("/v1/endpoint/tunnel", s.requireAdmin(s.handleEndpointTunnel))
	mux.HandleFunc("/v1/a2a", s.handleA2ADiscovery)
	mux.HandleFunc("/.well-known/agent.json", s.handleA2ADiscovery)
	mux.HandleFunc("/v1/mcp/info", s.handleMCPInfo)
	mux.HandleFunc("/v1/oauth/url", s.requireAdmin(s.handleOAuthURL))
	mux.HandleFunc("/v1/oauth/exchange", s.requireAdmin(s.handleOAuthExchange))
	mux.HandleFunc("/v1/oauth/claude/url", s.requireAdmin(s.handleClaudeOAuthURL))
	mux.HandleFunc("/v1/oauth/claude/exchange", s.requireAdmin(s.handleClaudeOAuthExchange))
	mux.HandleFunc("/v1/oauth/account", s.requireAdmin(s.handleOAuthAccountDelete))
	mux.HandleFunc("/v1/providers/antigravity", s.requireAdmin(s.handleOAuthExchange))
	mux.HandleFunc("/v1/providers/claude", s.requireAdmin(s.handleClaudeOAuthExchange))
	mux.HandleFunc("/v1/providers/alibaba", s.requireAdmin(s.handleAlibabaProviderSave))
	mux.HandleFunc("/v1/providers/import-openclaw", s.requireAdmin(s.handleProvidersList))
	mux.HandleFunc("/v1/providers/strategy", s.requireAdmin(s.handleRoutingStrategy))
	mux.HandleFunc("/v1/providers/account-proxy", s.requireAdmin(s.handleAccountProxy))
	mux.HandleFunc("/v1/providers", s.requireAdmin(s.handleProvidersList))
	mux.HandleFunc("/v1/auth/config", s.requireAdmin(s.handleAuthConfig))
	mux.HandleFunc("/v1/auth/status", s.handleAuthStatus)
	mux.HandleFunc("/v1/auth/setup", s.handleAuthSetup)
	mux.HandleFunc("/v1/auth/login", s.handleAuthLogin)
	mux.HandleFunc("/v1/auth/logout", s.handleAuthLogout)
	mux.HandleFunc("/v1/auth/password", s.handleAuthPasswordChange)
	mux.HandleFunc("/v1/compression", s.requireAdmin(s.handleCompression))
	mux.HandleFunc("/v1/models", s.handleModels)
	mux.HandleFunc("/models", s.handleModels)
	mux.HandleFunc("/v1/models/diagnostics", s.requireAdmin(s.handleModelDiagnostics))
	mux.HandleFunc("/v1/models/test", s.requireAdmin(s.handleModelTest))
	mux.HandleFunc("/v1/models/toggle-visibility", s.requireAdmin(s.handleModelToggleVisibility))
	mux.HandleFunc("/v1/keys", s.requireAdmin(s.handleAPIKeys))
	mux.HandleFunc("/v1/logs", s.requireAdmin(s.handleLogs))
	mux.HandleFunc("/ui/api/logs", s.requireAdmin(s.handleLogs))
	mux.HandleFunc("/v1/database", s.requireAdmin(s.handleDatabaseConfig))
	mux.HandleFunc("/v1/vector", s.requireAdmin(s.handleVectorDB))
	mux.HandleFunc("/v1/quota", s.requireAdmin(s.handleQuotaSummary))
	mux.HandleFunc("/v1/quota/refresh", s.requireAdmin(s.handleQuotaRefresh))
	mux.HandleFunc("/v1/quota/toggle", s.requireAdmin(s.handleQuotaToggle))
	mux.HandleFunc("/v1/quota/threshold", s.requireAdmin(s.handleQuotaThreshold))
	mux.HandleFunc("/v1/cache", s.requireAdmin(s.handleCacheStats))
	mux.HandleFunc("/v1/aliases", s.requireAdmin(s.handleAliases))
	mux.HandleFunc("/v1/hedging", s.requireAdmin(s.handleHedgingConfig))
	mux.HandleFunc("/v1/combos", s.requireAdmin(s.handleCombos))
	mux.HandleFunc("/v1/subagents", s.requireAdmin(s.handleSubAgents))
	mux.HandleFunc("/v1/subagents/swarm", s.requireAdmin(s.handleSubAgents))
	mux.HandleFunc("/v1/subagents/automations", s.requireAdmin(s.handleSubAgents))
	mux.HandleFunc("/v1/images/generations", s.handleImageGenerations)
	mux.HandleFunc("/v1/chat/completions", s.handleChatCompletions)
	mux.HandleFunc("/chat/completions", s.handleChatCompletions)
	mux.HandleFunc("/v1/messages", s.handleAnthropicMessages)
	mux.HandleFunc("/messages", s.handleAnthropicMessages)

	return withGzip(withCORS(mux))
}

func (s *Server) handleQuotaRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost && r.Method != http.MethodHead {
		writeOpenAIError(w, http.StatusMethodNotAllowed, "method not allowed", "invalid_request_error")
		return
	}
	summary := GetQuotaSummary(r.Context(), true)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(summary)
}

func (s *Server) handleRootOrHealth(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/" && r.URL.Query().Get("format") != "json" {
		s.handleUI(w, r)
		return
	}
	if r.URL.Path != "/" && r.URL.Path != "/health" && r.URL.Path != "/healthz" && r.URL.Path != "/v1/health" {
		writeOpenAIError(w, http.StatusNotFound, "endpoint not found: "+r.URL.Path, "invalid_request_error")
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeOpenAIError(w, http.StatusMethodNotAllowed, "method not allowed", "invalid_request_error")
		return
	}

	saved := LoadSavedAuthConfig()
	hasToken := strings.TrimSpace(s.cfg.AgyOAuthToken) != "" || saved.AgyOAuthToken != "" || len(saved.Accounts) > 0

	models := s.resolver.ListModels()
	modelIDs := make([]string, 0, len(models))
	for _, m := range models {
		modelIDs = append(modelIDs, m.ID)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":              "ok",
		"service":             "omniroute-go",
		"version":             GatewayVersion,
		"mode":                "standalone-direct-ccpa",
		"omniroute_disabled":  strings.TrimSpace(s.cfg.OmniRouteBaseURL) == "",
		"supported_providers": []string{string(ProviderAntigravity), string(ProviderClaudeCode), string(ProviderAlibaba)},
		"routing_strategy":    saved.RoutingStrategy,
		"ccpa_endpoint":       s.cfg.CCPAEndpoint,
		"ccpa_direct_enabled": hasToken,
		"connected_accounts":  len(saved.Accounts),
		"config_file":         GatewayConfigFilePath(),
		"ui_url":              "http://127.0.0.1:8787/ui",
		"supported_models":    modelIDs,
	})
}

func (s *Server) handleProvidersList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeOpenAIError(w, http.StatusMethodNotAllowed, "method not allowed", "invalid_request_error")
		return
	}
	saved := LoadSavedAuthConfig()
	now := time.Now().Unix()

	type accountView struct {
		ID            string     `json:"id"`
		Provider      ProviderID `json:"provider"`
		Tier          string     `json:"tier,omitempty"`
		Label         string     `json:"label"`
		Email         string     `json:"email"`
		ProjectID     string     `json:"project_id,omitempty"`
		Region        string     `json:"region,omitempty"`
		BaseURL       string     `json:"base_url,omitempty"`
		ProxyURL      string     `json:"proxy_url,omitempty"`
		InCooldown    bool       `json:"in_cooldown"`
		CooldownLeftS int64      `json:"cooldown_left_s"`
		SuccessCount  int64      `json:"success_count"`
		RequestsOK    int64      `json:"requests_ok"`
		FailureCount  int        `json:"failure_count"`
		TotalTokens   int64      `json:"total_tokens"`
		AvgLatencyMs  int64      `json:"avg_latency_ms"`
		UpdatedAt     string     `json:"updated_at"`
	}

	var list []accountView
	counts := map[ProviderID]int{
		ProviderAntigravity: 0,
		ProviderClaudeCode:  0,
		ProviderAlibaba:     0,
	}
	for _, a := range saved.Accounts {
		prov := a.Provider
		if prov == "" {
			prov = ProviderAntigravity
		}
		counts[prov]++
		cdLeft := int64(0)
		if a.CooldownUntil > now {
			cdLeft = a.CooldownUntil - now
		}
		tier := LookupCachedAccountTier(a, nil)
		if tier == "" {
			if prov == ProviderClaudeCode {
				tier = "Max"
			} else if prov == ProviderAlibaba {
				tier = "Coding Plan"
			} else {
				tier = "Pro"
			}
		}
		list = append(list, accountView{
			ID:            a.ID,
			Provider:      prov,
			Tier:          tier,
			Label:         a.Label,
			Email:         a.Email,
			ProjectID:     a.ProjectID,
			Region:        a.Region,
			BaseURL:       a.BaseURL,
			ProxyURL:      maskProxyURL(readAccountProxyURL(a)),
			InCooldown:    cdLeft > 0,
			CooldownLeftS: cdLeft,
			SuccessCount:  a.SuccessCount,
			RequestsOK:    a.SuccessCount,
			FailureCount:  a.FailureCount,
			TotalTokens:   a.TotalTokens,
			AvgLatencyMs:  a.AvgLatencyMs,
			UpdatedAt:     a.UpdatedAt,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"routing_strategy": saved.RoutingStrategy,
		"providers": []map[string]any{
			{
				"id":          ProviderAntigravity,
				"name":        "Google Antigravity (Gen 3, Gen 4 & Claude)",
				"auth_type":   "oauth2",
				"accounts":    counts[ProviderAntigravity],
				"description": "Chỉ giữ Gemini 3, Gemini 4 Argon (High/Med/Low) và Claude 4.5/4.6 trên hạ tầng Antigravity CCPA",
			},
			{
				"id":          ProviderClaudeCode,
				"name":        "Anthropic Claude Code (OAuth PKCE / API Key)",
				"auth_type":   "oauth2_pkce_or_apikey",
				"accounts":    counts[ProviderClaudeCode],
				"description": "Claude Opus 4.6 Thinking, Sonnet 4.6, Haiku 4.5 qua Claude Pro/Max OAuth hoặc API Key",
			},
			{
				"id":          ProviderAlibaba,
				"name":        "Alibaba Token Plan (Bailian Coding Plan / DashScope)",
				"auth_type":   "apikey",
				"accounts":    counts[ProviderAlibaba],
				"description": "Qwen3 Coder Plus, Qwen3 Max, QwQ Plus, DeepSeek R1/V3 trên Alibaba Token Plan",
			},
		},
		"accounts": list,
		"combos":   s.resolver.ListCombos(),
	})
}

func (s *Server) handleRoutingStrategy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeOpenAIError(w, http.StatusMethodNotAllowed, "only POST is supported", "invalid_request_error")
		return
	}
	var req struct {
		Strategy RoutingStrategy `json:"strategy"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 16*1024)).Decode(&req); err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "invalid JSON: "+err.Error(), "invalid_request_error")
		return
	}
	saved, err := SetRoutingStrategy(req.Strategy)
	if err != nil {
		writeOpenAIError(w, http.StatusInternalServerError, err.Error(), "server_error")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":           "ok",
		"routing_strategy": saved.RoutingStrategy,
	})
}

func readAccountProxyURL(acc ConnectedAccount) string {
	v := reflect.ValueOf(acc)
	if f := v.FieldByName("ProxyURL"); f.IsValid() && f.Kind() == reflect.String {
		return strings.TrimSpace(f.String())
	}
	if key := strings.ToLower(strings.TrimSpace(acc.Email)); key != "" {
		if raw, ok := fallbackAccountProxyByScope.Load(key); ok {
			if s, ok := raw.(string); ok {
				return s
			}
		}
	}
	if key := strings.ToLower(strings.TrimSpace(acc.ID)); key != "" {
		if raw, ok := fallbackAccountProxyByScope.Load(key); ok {
			if s, ok := raw.(string); ok {
				return s
			}
		}
	}
	return ""
}

func maskProxyURL(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	u, err := url.Parse(trimmed)
	if err != nil || u.User == nil {
		return trimmed
	}
	if _, hasPass := u.User.Password(); !hasPass {
		return trimmed
	}
	username := u.User.Username()
	u.User = nil
	base := u.String()
	schemeSep := strings.Index(base, "://")
	if schemeSep < 0 {
		return trimmed
	}
	return base[:schemeSep+3] + url.PathEscape(username) + ":***@" + base[schemeSep+3:]
}

func validateAccountProxyURL(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", nil
	}
	u, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("invalid proxy URL: %w", err)
	}
	scheme := strings.ToLower(strings.TrimSpace(u.Scheme))
	switch scheme {
	case "http", "https", "socks5", "socks5h":
	default:
		return "", fmt.Errorf("unsupported proxy scheme %q (allowed: http, https, socks5, socks5h)", u.Scheme)
	}
	if strings.TrimSpace(u.Host) == "" {
		return "", fmt.Errorf("proxy URL must include host:port")
	}
	return trimmed, nil
}

func (s *Server) handleAccountProxy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeOpenAIError(w, http.StatusMethodNotAllowed, "only POST is supported", "invalid_request_error")
		return
	}
	var req struct {
		ID       string `json:"id"`
		Email    string `json:"email"`
		ProxyURL string `json:"proxy_url"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 16*1024)).Decode(&req); err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "invalid JSON payload: "+err.Error(), "invalid_request_error")
		return
	}
	targetID := strings.TrimSpace(req.ID)
	targetEmail := strings.TrimSpace(req.Email)
	if targetID == "" && targetEmail == "" {
		writeOpenAIError(w, http.StatusBadRequest, "account id or email is required", "invalid_request_error")
		return
	}
	normalizedProxy, err := validateAccountProxyURL(req.ProxyURL)
	if err != nil {
		writeOpenAIError(w, http.StatusBadRequest, err.Error(), "invalid_request_error")
		return
	}

	configMu.Lock()
	cfg := loadSavedAuthConfigLocked()
	matchedIdx := -1
	for i := range cfg.Accounts {
		if (targetID != "" && strings.EqualFold(cfg.Accounts[i].ID, targetID)) ||
			(targetEmail != "" && strings.EqualFold(cfg.Accounts[i].Email, targetEmail)) {
			matchedIdx = i
			break
		}
	}
	if matchedIdx < 0 {
		configMu.Unlock()
		writeOpenAIError(w, http.StatusNotFound, "connected account not found", "not_found_error")
		return
	}

	nowStr := time.Now().UTC().Format(time.RFC3339)
	accPtr := &cfg.Accounts[matchedIdx]
	f := reflect.ValueOf(accPtr).Elem().FieldByName("ProxyURL")
	if f.IsValid() && f.CanSet() && f.Kind() == reflect.String {
		f.SetString(normalizedProxy)
	} else {
		for _, k := range []string{strings.ToLower(strings.TrimSpace(accPtr.Email)), strings.ToLower(strings.TrimSpace(accPtr.ID))} {
			if k == "" {
				continue
			}
			if normalizedProxy == "" {
				fallbackAccountProxyByScope.Delete(k)
			} else {
				fallbackAccountProxyByScope.Store(k, normalizedProxy)
			}
		}
	}
	accPtr.UpdatedAt = nowStr
	cfg.UpdatedAt = nowStr
	saveErr := saveAuthConfigLocked(cfg)
	updatedAcc := *accPtr
	configMu.Unlock()

	if saveErr != nil {
		writeOpenAIError(w, http.StatusInternalServerError, "failed to persist proxy configuration: "+saveErr.Error(), "server_error")
		return
	}

	InvalidateAccountHTTPClients(updatedAcc.Email)
	InvalidateAccountHTTPClients(updatedAcc.ID)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":    "ok",
		"id":        updatedAcc.ID,
		"email":     updatedAcc.Email,
		"proxy_url": maskProxyURL(readAccountProxyURL(updatedAcc)),
	})
}

func (s *Server) handleCompression(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		cfg := GetCompressionConfig()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(cfg)
	case http.MethodPost:
		var req CompressionConfig
		if err := json.NewDecoder(io.LimitReader(r.Body, 16*1024)).Decode(&req); err != nil {
			writeOpenAIError(w, http.StatusBadRequest, "invalid JSON payload: "+err.Error(), "invalid_request_error")
			return
		}
		if err := SetCompressionConfig(req); err != nil {
			writeOpenAIError(w, http.StatusInternalServerError, err.Error(), "server_error")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "updated",
			"config": GetCompressionConfig(),
		})
	default:
		writeOpenAIError(w, http.StatusMethodNotAllowed, "method not allowed", "invalid_request_error")
	}
}

func (s *Server) handleClaudeOAuthURL(w http.ResponseWriter, r *http.Request) {
	authURL, verifier, state := BuildClaudeOAuthURL()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"auth_url":      authURL,
		"code_verifier": verifier,
		"state":         state,
		"client_id":     ClaudeOAuthClientID,
	})
}

func (s *Server) handleClaudeOAuthExchange(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeOpenAIError(w, http.StatusMethodNotAllowed, "only POST is supported", "invalid_request_error")
		return
	}
	var payload struct {
		Input        string `json:"input"`
		CodeVerifier string `json:"code_verifier"`
		Label        string `json:"label"`
		BaseURL      string `json:"base_url"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 64*1024)).Decode(&payload); err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "invalid JSON payload: "+err.Error(), "invalid_request_error")
		return
	}
	acc, err := ExchangeClaudeInput(r.Context(), payload.Input, payload.CodeVerifier, payload.Label, payload.BaseURL)
	if err != nil {
		writeOpenAIError(w, http.StatusBadRequest, err.Error(), "claude_exchange_error")
		return
	}
	saved := LoadSavedAuthConfig()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":         "connected",
		"provider":       ProviderClaudeCode,
		"id":             acc.ID,
		"email":          acc.Email,
		"total_accounts": len(saved.Accounts),
	})
}

func (s *Server) handleAlibabaProviderSave(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeOpenAIError(w, http.StatusMethodNotAllowed, "only POST is supported", "invalid_request_error")
		return
	}
	var payload struct {
		Label   string `json:"label"`
		APIKey  string `json:"api_key"`
		Region  string `json:"region"`
		BaseURL string `json:"base_url"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 64*1024)).Decode(&payload); err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "invalid JSON payload: "+err.Error(), "invalid_request_error")
		return
	}
	key := strings.TrimSpace(payload.APIKey)
	if key == "" {
		writeOpenAIError(w, http.StatusBadRequest, "api_key is required for Alibaba Token Plan", "invalid_request_error")
		return
	}
	region := strings.TrimSpace(payload.Region)
	if region == "" {
		region = "intl-coding"
	}
	label := strings.TrimSpace(payload.Label)
	if label == "" {
		label = fmt.Sprintf("alibaba-%s", region)
	}
	acc := ConnectedAccount{
		Provider: ProviderAlibaba,
		Label:    label,
		Email:    label,
		APIKey:   key,
		Region:   region,
		BaseURL:  strings.TrimSpace(payload.BaseURL),
	}
	saved, err := UpsertConnectedAccount(acc)
	if err != nil {
		writeOpenAIError(w, http.StatusInternalServerError, err.Error(), "server_error")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":         "connected",
		"provider":       ProviderAlibaba,
		"label":          label,
		"region":         region,
		"endpoint":       ResolveAlibabaEndpoint(region, acc.BaseURL),
		"total_accounts": len(saved.Accounts),
	})
}

func (s *Server) handleAPIKeys(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		keys := ListSanitizedAPIKeys()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"object":   "list",
			"data":     keys,
			"total":    len(keys),
			"postgres": GetPostgresStatus(),
		})
	case http.MethodPost:
		var req struct {
			Name               string `json:"name"`
			AdminAccess        bool   `json:"admin_access"`
			AllowSelfUsage     bool   `json:"allow_self_usage"`
			AllowSharedQuota   bool   `json:"allow_shared_quota"`
			AllowLocalUsageCmd bool   `json:"allow_local_usage_cmd"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 16*1024)).Decode(&req); err != nil {
			writeOpenAIError(w, http.StatusBadRequest, "invalid JSON payload: "+err.Error(), "invalid_request_error")
			return
		}
		keyObj, err := CreateAPIKey(req.Name, req.AdminAccess, req.AllowSelfUsage, req.AllowSharedQuota, req.AllowLocalUsageCmd)
		if err != nil {
			writeOpenAIError(w, http.StatusInternalServerError, err.Error(), "server_error")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "created",
			"key":    keyObj,
		})
	case http.MethodDelete:
		id := strings.TrimSpace(r.URL.Query().Get("id"))
		if id == "" {
			id = strings.TrimSpace(r.URL.Query().Get("key"))
		}
		if id == "" {
			writeOpenAIError(w, http.StatusBadRequest, "missing key id query parameter", "invalid_request_error")
			return
		}
		if err := DeleteAPIKey(id); err != nil {
			writeOpenAIError(w, http.StatusBadRequest, err.Error(), "key_delete_error")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "deleted",
			"id":     id,
		})
	default:
		writeOpenAIError(w, http.StatusMethodNotAllowed, "method not allowed", "invalid_request_error")
	}
}

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		limit := 100
		if lStr := r.URL.Query().Get("limit"); lStr != "" {
			if parsed, err := strconv.Atoi(lStr); err == nil && parsed > 0 {
				limit = parsed
			}
		}
		provider := r.URL.Query().Get("provider")
		errorsOnly := r.URL.Query().Get("errors_only") == "true" || r.URL.Query().Get("error") == "true"
		apiKeyFilter := r.URL.Query().Get("api_key_id")
		if apiKeyFilter == "" {
			apiKeyFilter = r.URL.Query().Get("employee")
		}
		searchQuery := r.URL.Query().Get("q")

		logs := QueryPromptLogs(limit, provider, errorsOnly, apiKeyFilter, searchQuery)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"object":    "list",
			"data":      logs,
			"total":     len(logs),
			"postgres":  GetPostgresStatus(),
			"employees": GetEmployeeUsageStats(),
		})
	case http.MethodDelete:
		ClearLogs()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "cleared",
		})
	default:
		writeOpenAIError(w, http.StatusMethodNotAllowed, "method not allowed", "invalid_request_error")
	}
}

func validatePostgresDSN(rawDSN string) error {
	dsn := strings.TrimSpace(rawDSN)
	if dsn == "" {
		return nil
	}
	lower := strings.ToLower(dsn)
	if !strings.HasPrefix(lower, "postgres://") && !strings.HasPrefix(lower, "postgresql://") {
		return fmt.Errorf("invalid DSN: must start with postgres:// or postgresql://")
	}
	u, err := url.Parse(dsn)
	if err != nil || u.Host == "" {
		return fmt.Errorf("invalid PostgreSQL DSN URL")
	}
	host := strings.ToLower(strings.TrimSpace(u.Hostname()))
	if host == "" {
		return fmt.Errorf("missing host in PostgreSQL DSN")
	}
	if host == "169.254.169.254" ||
		host == "metadata.google.internal" ||
		strings.HasSuffix(host, ".metadata.google.internal") ||
		host == "metadata" ||
		host == "100.100.100.200" {
		return fmt.Errorf("forbidden host in PostgreSQL DSN: cloud metadata endpoints are not allowed")
	}
	if ip := net.ParseIP(host); ip != nil {
		if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
			return fmt.Errorf("forbidden link-local address in PostgreSQL DSN")
		}
	}
	return nil
}

func (s *Server) handleDatabaseConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(GetPostgresStatus())
	case http.MethodPost:
		var req struct {
			DSN string `json:"dsn"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 16*1024)).Decode(&req); err != nil {
			writeOpenAIError(w, http.StatusBadRequest, "invalid JSON payload: "+err.Error(), "invalid_request_error")
			return
		}
		if err := validatePostgresDSN(req.DSN); err != nil {
			writeOpenAIError(w, http.StatusBadRequest, err.Error(), "invalid_dsn")
			return
		}
		err := ConfigurePostgresDSN(req.DSN)
		status := GetPostgresStatus()
		if err != nil {
			status["error"] = err.Error()
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(status)
	default:
		writeOpenAIError(w, http.StatusMethodNotAllowed, "method not allowed", "invalid_request_error")
	}
}

func (s *Server) handleVectorDB(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		q := strings.TrimSpace(r.URL.Query().Get("q"))
		var memories []VectorMemoryEntry
		if !GetVectorDBConfig().Enabled {
			memories = []VectorMemoryEntry{}
		} else if q != "" {
			memories = SearchVectorMemories(q, 20, 0.05)
		} else {
			memories = ListVectorMemories(100)
		}
		res := GetVectorDBStatus()
		res["memories"] = memories
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(res)

	case http.MethodPost:
		var payload struct {
			Action           string  `json:"action"` // "config", "add", "search"
			Enabled          *bool   `json:"enabled,omitempty"`
			AutoLearnPrompts *bool   `json:"auto_learn_prompts,omitempty"`
			AutoInjectRAG    *bool   `json:"auto_inject_rag,omitempty"`
			TopK             int     `json:"top_k,omitempty"`
			MinSimilarity    float64 `json:"min_similarity,omitempty"`
			Title            string  `json:"title,omitempty"`
			Category         string  `json:"category,omitempty"`
			PromptText       string  `json:"prompt_text,omitempty"`
			AnswerText       string  `json:"answer_text,omitempty"`
			Pinned           bool    `json:"pinned,omitempty"`
			Query            string  `json:"query,omitempty"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 256*1024)).Decode(&payload); err != nil {
			writeOpenAIError(w, http.StatusBadRequest, "invalid JSON payload: "+err.Error(), "invalid_request_error")
			return
		}

		switch strings.ToLower(strings.TrimSpace(payload.Action)) {
		case "config":
			vc := GetVectorDBConfig()
			if payload.Enabled != nil {
				vc.Enabled = *payload.Enabled
			}
			if payload.AutoLearnPrompts != nil {
				vc.AutoLearnPrompts = *payload.AutoLearnPrompts
			}
			if payload.AutoInjectRAG != nil {
				vc.AutoInjectRAG = *payload.AutoInjectRAG
			}
			if payload.TopK > 0 {
				vc.TopK = payload.TopK
			}
			if payload.MinSimilarity > 0 {
				vc.MinSimilarity = payload.MinSimilarity
			}
			if err := SaveVectorDBConfig(vc); err != nil {
				writeOpenAIError(w, http.StatusInternalServerError, err.Error(), "vector_config_error")
				return
			}
			res := GetVectorDBStatus()
			res["status"] = "saved"
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(res)

		case "search":
			topK := payload.TopK
			if topK <= 0 {
				topK = 10
			}
			matches := SearchVectorMemories(payload.Query, topK, 0.05)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status":  "ok",
				"query":   payload.Query,
				"matches": matches,
				"total":   len(matches),
			})

		default: // "add"
			entry, err := AddVectorMemory(
				payload.Title,
				payload.Category,
				payload.PromptText,
				payload.AnswerText,
				"Admin Knowledge Base",
				"",
				"manual",
				payload.Pinned,
			)
			if err != nil {
				writeOpenAIError(w, http.StatusBadRequest, err.Error(), "vector_add_error")
				return
			}
			entry.Embedding = nil
			res := GetVectorDBStatus()
			res["status"] = "added"
			res["entry"] = entry
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(res)
		}

	case http.MethodDelete:
		id := strings.TrimSpace(r.URL.Query().Get("id"))
		if err := DeleteVectorMemory(id); err != nil {
			writeOpenAIError(w, http.StatusBadRequest, err.Error(), "vector_delete_error")
			return
		}
		res := GetVectorDBStatus()
		res["status"] = "deleted"
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(res)

	default:
		writeOpenAIError(w, http.StatusMethodNotAllowed, "method not allowed", "invalid_request_error")
	}
}

func (s *Server) handleAliases(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		aliases := GetModelAliases()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":  "ok",
			"aliases": aliases,
			"total":   len(aliases),
		})
	case http.MethodPost:
		var req struct {
			Alias  string `json:"alias"`
			Target string `json:"target"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 16*1024)).Decode(&req); err != nil {
			writeOpenAIError(w, http.StatusBadRequest, "invalid JSON payload: "+err.Error(), "invalid_request_error")
			return
		}
		if err := SetModelAlias(req.Alias, req.Target); err != nil {
			writeOpenAIError(w, http.StatusBadRequest, err.Error(), "alias_error")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "ok",
			"alias":  req.Alias,
			"target": req.Target,
		})
	case http.MethodDelete:
		alias := strings.TrimSpace(r.URL.Query().Get("alias"))
		if alias == "" {
			writeOpenAIError(w, http.StatusBadRequest, "missing alias query parameter", "invalid_request_error")
			return
		}
		if err := DeleteModelAlias(alias); err != nil {
			writeOpenAIError(w, http.StatusBadRequest, err.Error(), "alias_error")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "deleted",
			"alias":  alias,
		})
	default:
		writeOpenAIError(w, http.StatusMethodNotAllowed, "method not allowed", "invalid_request_error")
	}
}

func (s *Server) handleHedgingConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		enabled, delay := GetHedgedConfig()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"enabled":  enabled,
			"delay_ms": delay,
		})
	case http.MethodPost:
		var req struct {
			Enabled bool `json:"enabled"`
			DelayMs int  `json:"delay_ms"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 16*1024)).Decode(&req); err != nil {
			writeOpenAIError(w, http.StatusBadRequest, "invalid JSON payload: "+err.Error(), "invalid_request_error")
			return
		}
		if err := SetHedgedConfig(req.Enabled, req.DelayMs); err != nil {
			writeOpenAIError(w, http.StatusBadRequest, err.Error(), "hedging_error")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":   "ok",
			"enabled":  req.Enabled,
			"delay_ms": req.DelayMs,
		})
	default:
		writeOpenAIError(w, http.StatusMethodNotAllowed, "method not allowed", "invalid_request_error")
	}
}

func (s *Server) handleOAuthURL(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeOpenAIError(w, http.StatusMethodNotAllowed, "method not allowed", "invalid_request_error")
		return
	}
	redirectURI := strings.TrimSpace(r.URL.Query().Get("redirect_uri"))
	if redirectURI == "" {
		redirectURI = AgyOAuthDefaultRedirectURI
	}
	state := strings.TrimSpace(r.URL.Query().Get("state"))
	if state == "" {
		origin := strings.TrimSpace(r.URL.Query().Get("origin"))
		if origin == "" && r.Host != "" && !strings.HasPrefix(r.Host, "127.0.0.1:20128") {
			scheme := "http"
			if strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") || r.TLS != nil {
				scheme = "https"
			}
			origin = scheme + "://" + r.Host
		}
		if origin != "" {
			state = "origin=" + origin
		}
	}
	authURL := BuildOAuthAuthURL(redirectURI, state)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"auth_url":     authURL,
		"redirect_uri": redirectURI,
		"client_id":    AgyOAuthClientID,
	})
}

func (s *Server) handleOAuthExchange(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeOpenAIError(w, http.StatusMethodNotAllowed, "only POST is supported", "invalid_request_error")
		return
	}
	var payload struct {
		Input       string `json:"input"`
		RedirectURI string `json:"redirect_uri,omitempty"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 64*1024)).Decode(&payload); err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "invalid JSON payload: "+err.Error(), "invalid_request_error")
		return
	}
	acc, err := ExchangeOAuthInput(r.Context(), payload.Input, payload.RedirectURI)
	if err != nil {
		writeOpenAIError(w, http.StatusBadRequest, err.Error(), "oauth_exchange_error")
		return
	}

	s.cfg.AgyOAuthToken = acc.AccessToken
	if acc.ProjectID != "" {
		s.cfg.AgyProjectID = acc.ProjectID
	}
	saved := LoadSavedAuthConfig()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":         "connected",
		"email":          acc.Email,
		"project_id":     acc.ProjectID,
		"expires_at":     acc.ExpiresAt,
		"total_accounts": len(saved.Accounts),
		"config_file":    GatewayConfigFilePath(),
	})
}

func isLoopbackHostname(hostname string) bool {
	h := strings.ToLower(strings.TrimSpace(hostname))
	return h == "localhost" || h == "127.0.0.1" || h == "::1"
}

func parseSafeCallbackOrigin(rawOrigin string, requestHost string) (*url.URL, bool) {
	rawOrigin = strings.TrimSpace(rawOrigin)
	if rawOrigin == "" {
		return nil, false
	}
	parsedOrigin, err := url.Parse(rawOrigin)
	if err != nil || parsedOrigin.Host == "" || parsedOrigin.User != nil {
		return nil, false
	}
	scheme := strings.ToLower(parsedOrigin.Scheme)
	if scheme != "http" && scheme != "https" {
		return nil, false
	}
	if isLoopbackHostname(parsedOrigin.Hostname()) {
		return parsedOrigin, true
	}
	if requestHost != "" && strings.EqualFold(parsedOrigin.Host, requestHost) {
		return parsedOrigin, true
	}
	return nil, false
}

func (s *Server) handleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	state := strings.TrimSpace(r.URL.Query().Get("state"))
	origin := ""
	if strings.HasPrefix(state, "origin=") {
		origin = strings.TrimSpace(strings.TrimPrefix(state, "origin="))
	}

	parsedOrigin, trustedOrigin := parseSafeCallbackOrigin(origin, r.Host)
	if trustedOrigin && r.URL.Query().Get("forwarded") != "1" && !strings.EqualFold(parsedOrigin.Host, r.Host) {
		baseOrigin := strings.ToLower(parsedOrigin.Scheme) + "://" + parsedOrigin.Host
		target := baseOrigin + "/callback?" + r.URL.RawQuery + "&forwarded=1"
		http.Redirect(w, r, target, http.StatusFound)
		return
	}

	returnUI := "/ui"
	if trustedOrigin && r.Host != "" && strings.EqualFold(parsedOrigin.Host, r.Host) {
		returnUI = strings.ToLower(parsedOrigin.Scheme) + "://" + parsedOrigin.Host + "/ui"
	}

	fullURL := fmt.Sprintf("http://%s%s", r.Host, r.URL.RequestURI())
	if r.Host == "" {
		fullURL = AgyOAuthDefaultRedirectURI + "?" + r.URL.RawQuery
	}

	escapedReturnUI := html.EscapeString(returnUI)
	acc, err := ExchangeOAuthInput(r.Context(), fullURL, AgyOAuthDefaultRedirectURI)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = fmt.Fprintf(w, `<!DOCTYPE html><html><body style="background:#0b0f19;color:#f87171;font-family:sans-serif;padding:40px;text-align:center;">
<h2>Kết nối OAuth thất bại</h2><p>%s</p><p><a href="%s" style="color:#60a5fa;">Quay lại AI Gateway UI</a></p></body></html>`, html.EscapeString(err.Error()), escapedReturnUI)
		return
	}

	s.cfg.AgyOAuthToken = acc.AccessToken
	if acc.ProjectID != "" {
		s.cfg.AgyProjectID = acc.ProjectID
	}

	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprintf(w, `<!DOCTYPE html><html><head><meta http-equiv="refresh" content="1;url=%s" /></head>
<body style="background:#0b0f19;color:#10b981;font-family:sans-serif;padding:40px;text-align:center;">
<h2>Đã kết nối thành công tài khoản Antigravity!</h2>
<p style="color:#e2e8f0;">Email: <strong>%s</strong> | Project ID: <code>%s</code></p>
<p><a href="%s" style="color:#60a5fa;">Đang chuyển hướng về AI Gateway Dashboard...</a></p>
</body></html>`, escapedReturnUI, html.EscapeString(acc.Email), html.EscapeString(acc.ProjectID), escapedReturnUI)
}

func (s *Server) handleOAuthAccountDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete && r.Method != http.MethodPost {
		writeOpenAIError(w, http.StatusMethodNotAllowed, "method not allowed", "invalid_request_error")
		return
	}
	ident := strings.TrimSpace(r.URL.Query().Get("email"))
	if ident == "" {
		ident = strings.TrimSpace(r.URL.Query().Get("id"))
	}
	if ident == "" {
		writeOpenAIError(w, http.StatusBadRequest, "missing email or id query parameter", "invalid_request_error")
		return
	}
	saved, err := RemoveConnectedAccount(ident)
	if err != nil {
		writeOpenAIError(w, http.StatusInternalServerError, err.Error(), "server_error")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":         "removed",
		"total_accounts": len(saved.Accounts),
	})
}

func (s *Server) handleAuthConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		saved := LoadSavedAuthConfig()
		token := strings.TrimSpace(s.cfg.AgyOAuthToken)
		if saved.AgyOAuthToken != "" {
			token = saved.AgyOAuthToken
		}
		projectID := strings.TrimSpace(s.cfg.AgyProjectID)
		if saved.AgyProjectID != "" {
			projectID = saved.AgyProjectID
		}
		masked := ""
		if len(token) > 12 {
			masked = token[:8] + "..." + token[len(token)-4:]
		} else if token != "" {
			masked = "configured"
		}

		type accountSummary struct {
			Email     string `json:"email"`
			ProjectID string `json:"project_id"`
			ExpiresAt int64  `json:"expires_at"`
			UpdatedAt string `json:"updated_at"`
		}
		var accountsList []accountSummary
		for _, a := range saved.Accounts {
			accountsList = append(accountsList, accountSummary{
				Email:     a.Email,
				ProjectID: a.ProjectID,
				ExpiresAt: a.ExpiresAt,
				UpdatedAt: a.UpdatedAt,
			})
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"configured":     token != "" || len(saved.Accounts) > 0,
			"masked_token":   masked,
			"agy_project_id": projectID,
			"accounts":       accountsList,
			"config_file":    GatewayConfigFilePath(),
			"updated_at":     saved.UpdatedAt,
		})
	case http.MethodPost:
		var req struct {
			AgyOAuthToken string `json:"agy_oauth_token"`
			AgyProjectID  string `json:"agy_project_id"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 64*1024)).Decode(&req); err != nil {
			writeOpenAIError(w, http.StatusBadRequest, "invalid JSON payload: "+err.Error(), "invalid_request_error")
			return
		}
		token := strings.TrimSpace(req.AgyOAuthToken)
		token = strings.TrimPrefix(token, "Bearer ")
		token = strings.TrimPrefix(token, "bearer ")
		token = strings.TrimSpace(token)

		if err := SaveAuthConfig(token, req.AgyProjectID); err != nil {
			writeOpenAIError(w, http.StatusInternalServerError, "failed to save gateway_config.json: "+err.Error(), "server_error")
			return
		}
		s.cfg.AgyOAuthToken = token
		s.cfg.AgyProjectID = strings.TrimSpace(req.AgyProjectID)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":         "saved",
			"configured":     token != "",
			"agy_project_id": s.cfg.AgyProjectID,
			"config_file":    GatewayConfigFilePath(),
		})
	default:
		writeOpenAIError(w, http.StatusMethodNotAllowed, "method not allowed", "invalid_request_error")
	}
}

func (s *Server) handleUI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeOpenAIError(w, http.StatusMethodNotAllowed, "method not allowed", "invalid_request_error")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(gatewayUIHTML))
}

func (s *Server) connectedProviderMap() map[string]bool {
	if strings.TrimSpace(s.cfg.OmniRouteBaseURL) != "" {
		return map[string]bool{
			string(ProviderAntigravity): true,
			string(ProviderClaudeCode):  true,
			string(ProviderAlibaba):     true,
			string(ProviderAutoCombo):   true,
		}
	}
	saved := LoadSavedAuthConfig()
	conn := make(map[string]bool, 4)
	if strings.TrimSpace(s.cfg.AgyOAuthToken) != "" ||
		strings.TrimSpace(saved.AgyOAuthToken) != "" ||
		strings.TrimSpace(os.Getenv("AGY_OAUTH_TOKEN")) != "" {
		conn[string(ProviderAntigravity)] = true
	}
	if strings.TrimSpace(os.Getenv("CLAUDE_CODE_OAUTH_TOKEN")) != "" ||
		strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY")) != "" {
		conn[string(ProviderClaudeCode)] = true
	}
	if strings.TrimSpace(os.Getenv("ALIBABA_API_KEY")) != "" ||
		strings.TrimSpace(os.Getenv("DASHSCOPE_API_KEY")) != "" {
		conn[string(ProviderAlibaba)] = true
	}
	for _, a := range saved.Accounts {
		if a.Disabled {
			continue
		}
		prov := a.Provider
		if prov == "" {
			prov = ProviderAntigravity
		}
		conn[string(prov)] = true
	}
	if len(conn) > 0 {
		conn[string(ProviderAutoCombo)] = true
	}
	return conn
}

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeOpenAIError(w, http.StatusMethodNotAllowed, "method not allowed", "invalid_request_error")
		return
	}

	cards := s.resolver.ListModels()
	if qConn := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("connected"))); qConn == "1" || qConn == "true" {
		connMap := s.connectedProviderMap()
		filtered := make([]OpenAIModelCard, 0, len(cards))
		for _, c := range cards {
			if connMap[c.Provider] || connMap[c.OwnedBy] {
				filtered = append(filtered, c)
			}
		}
		cards = filtered
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"object": "list",
		"data":   cards,
	})
}

func readRequestBodyFast(r *http.Request, maxBytes int64) ([]byte, error) {
	if r == nil || r.Body == nil {
		return nil, nil
	}
	defer r.Body.Close()
	if r.ContentLength > 0 && r.ContentLength <= maxBytes {
		buf := make([]byte, int(r.ContentLength))
		n, err := io.ReadFull(r.Body, buf)
		if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
			return nil, err
		}
		return buf[:n], nil
	}
	return io.ReadAll(io.LimitReader(r.Body, maxBytes))
}

func recordCacheHitGatewayLog(
	entry *CacheEntry,
	req OpenAIChatRequest,
	resolved ResolvedModel,
	apiKeyObj *GatewayAPIKey,
	totalTokens int,
) {
	empName := "Admin / Local"
	apiKeyID := ""
	maskedKey := ""
	if apiKeyObj != nil {
		apiKeyID = apiKeyObj.ID
		maskedKey = apiKeyObj.MaskedKey
		if strings.TrimSpace(apiKeyObj.Name) != "" {
			empName = apiKeyObj.Name
		}
	}
	promptToks := 0
	compToks := 0
	if entry.Response.Usage != nil {
		promptToks = entry.Response.Usage.PromptTokens
		compToks = entry.Response.Usage.CompletionTokens
	}
	var idBuf [32]byte
	idBytes := append(idBuf[:0], "log-cache-"...)
	idBytes = strconv.AppendUint(idBytes, cacheLogSeq.Add(1), 36)
	var tsBuf [32]byte
	tsBytes := time.Now().UTC().AppendFormat(tsBuf[:0], time.RFC3339)
	RecordLogEntry(GatewayLogEntry{
		ID:               string(idBytes),
		Timestamp:        string(tsBytes),
		Provider:         resolved.Provider,
		Account:          "in-memory-cache",
		RequestedModel:   req.Model,
		ResolvedModel:    resolved.CanonicalID,
		UpstreamModel:    "cache-hit",
		StatusCode:       http.StatusOK,
		LatencyMs:        0,
		PromptTokens:     promptToks,
		CompletionTokens: compToks,
		TotalTokens:      totalTokens,
		APIKeyID:         apiKeyID,
		EmployeeName:     empName,
		MaskedKey:        maskedKey,
		PromptText:       entry.PromptSummary,
		ResponseText:     entry.ContentText(),
		IsError:          false,
	})
}

type chatCompletionEnvelope struct {
	OpenAIChatRequest
	StreamOptions *StreamOptions    `json:"stream_options,omitempty"`
	Tools         []json.RawMessage `json:"tools,omitempty"`
}

type anthropicNonStreamBufferWriter struct {
	hdr        http.Header
	statusCode int
	buf        bytes.Buffer
}

func (bw *anthropicNonStreamBufferWriter) Header() http.Header {
	return bw.hdr
}

func (bw *anthropicNonStreamBufferWriter) WriteHeader(code int) {
	if bw.statusCode == 0 {
		bw.statusCode = code
	}
}

func (bw *anthropicNonStreamBufferWriter) Write(p []byte) (int, error) {
	if bw.statusCode == 0 {
		bw.statusCode = http.StatusOK
	}
	return bw.buf.Write(p)
}

func (bw *anthropicNonStreamBufferWriter) Flush() {}

type anthropicSSEAdapterWriter struct {
	w               http.ResponseWriter
	fallbackModel   string
	estimatedInput  int
	statusCode      int
	wroteHeader     bool
	passthrough     bool
	started         bool
	doneEmitted     bool
	sawStreamError  bool
	hasEmittedBlock bool
	activeBlockKind string
	nextBlockIndex  int
	msgID           string
	model           string
	finishReason    string
	inputTokens     int
	outputTokens    int
	lineBuf         bytes.Buffer
	outBuf          bytes.Buffer
	accContent      strings.Builder
	accReasoning    strings.Builder
}

func newAnthropicSSEAdapterWriter(w http.ResponseWriter, fallbackModel string, estimatedInput int) *anthropicSSEAdapterWriter {
	if estimatedInput <= 0 {
		estimatedInput = 1
	}
	return &anthropicSSEAdapterWriter{
		w:              w,
		fallbackModel:  strings.TrimSpace(fallbackModel),
		estimatedInput: estimatedInput,
		inputTokens:    estimatedInput,
	}
}

func (sw *anthropicSSEAdapterWriter) Header() http.Header {
	return sw.w.Header()
}

func (sw *anthropicSSEAdapterWriter) WriteHeader(code int) {
	if sw.wroteHeader {
		return
	}
	sw.wroteHeader = true
	sw.statusCode = code
	if code != http.StatusOK {
		sw.passthrough = true
		sw.w.WriteHeader(code)
		return
	}
	hdr := sw.w.Header()
	hdr.Set("Content-Type", "text/event-stream")
	hdr.Set("Cache-Control", "no-cache")
	hdr.Set("Connection", "keep-alive")
	hdr.Set("X-Accel-Buffering", "no")
	hdr.Del("Content-Length")
	sw.w.WriteHeader(http.StatusOK)
}

func (sw *anthropicSSEAdapterWriter) Write(p []byte) (int, error) {
	if !sw.wroteHeader {
		sw.WriteHeader(http.StatusOK)
	}
	if sw.passthrough {
		return sw.w.Write(p)
	}
	_, _ = sw.lineBuf.Write(p)
	sw.outBuf.Reset()

	for {
		idx := bytes.IndexByte(sw.lineBuf.Bytes(), '\n')
		if idx < 0 {
			break
		}
		line := sw.lineBuf.Next(idx + 1)
		sw.processSSELine(line)
	}

	if sw.outBuf.Len() > 0 {
		if _, err := sw.w.Write(sw.outBuf.Bytes()); err != nil {
			return 0, err
		}
	}
	return len(p), nil
}

func (sw *anthropicSSEAdapterWriter) Flush() {
	if fl, ok := sw.w.(http.Flusher); ok {
		fl.Flush()
	}
}

func (sw *anthropicSSEAdapterWriter) processSSELine(line []byte) {
	trimmed := bytes.TrimSpace(line)
	if len(trimmed) == 0 {
		return
	}
	if trimmed[0] == ':' {
		sw.outBuf.Write(trimmed)
		sw.outBuf.WriteString("\n\n")
		return
	}
	if bytes.HasPrefix(trimmed, []byte("data:")) {
		payload := bytes.TrimSpace(trimmed[5:])
		sw.processSSEPayload(payload)
		return
	}
	if trimmed[0] == '{' {
		sw.processSSEPayload(trimmed)
	}
}

func (sw *anthropicSSEAdapterWriter) ensureMessageStart() {
	if sw.started {
		return
	}
	sw.started = true
	if sw.msgID == "" {
		sw.msgID = normalizeAnthropicMessageID("", "")
	}
	model := sw.model
	if model == "" {
		model = sw.fallbackModel
	}
	if model == "" {
		model = "unknown"
	}
	writeAnthropicSSEEvent(&sw.outBuf, "message_start", anthropicSSEMessageStart{
		Type: "message_start",
		Message: anthropicMessageResponse{
			ID:           sw.msgID,
			Type:         "message",
			Role:         "assistant",
			Model:        model,
			Content:      []anthWireContentBlock{},
			StopReason:   nil,
			StopSequence: nil,
			Usage: anthropicUsage{
				InputTokens:  sw.inputTokens,
				OutputTokens: 0,
			},
		},
	})
}

func (sw *anthropicSSEAdapterWriter) closeActiveBlock() {
	if sw.activeBlockKind == "" {
		return
	}
	writeAnthropicSSEEvent(&sw.outBuf, "content_block_stop", anthropicSSEContentBlockStop{
		Type:  "content_block_stop",
		Index: sw.nextBlockIndex,
	})
	sw.activeBlockKind = ""
	sw.nextBlockIndex++
}

func (sw *anthropicSSEAdapterWriter) processSSEPayload(payload []byte) {
	if len(payload) == 0 || sw.doneEmitted {
		return
	}
	if bytes.Equal(payload, []byte("[DONE]")) {
		sw.emitStreamEnd()
		return
	}

	var chunk OpenAIChatResponse
	if err := json.Unmarshal(payload, &chunk); err != nil {
		return
	}

	if chunk.Error != nil && len(chunk.Choices) == 0 {
		sw.sawStreamError = true
		errType := strings.TrimSpace(chunk.Error.Type)
		if errType == "" {
			errType = "api_error"
		}
		writeAnthropicSSEEvent(&sw.outBuf, "error", anthropicSSEError{
			Type: "error",
			Error: anthropicSSEErrorDetail{
				Type:    errType,
				Message: chunk.Error.Message,
			},
		})
		return
	}

	if sw.msgID == "" && strings.TrimSpace(chunk.ID) != "" {
		sw.msgID = normalizeAnthropicMessageID(chunk.ID, "")
	}
	if strings.TrimSpace(chunk.Model) != "" {
		sw.model = strings.TrimSpace(chunk.Model)
	}
	if chunk.Usage != nil {
		if chunk.Usage.PromptTokens > 0 {
			sw.inputTokens = chunk.Usage.PromptTokens
		}
		if chunk.Usage.CompletionTokens > 0 {
			sw.outputTokens = chunk.Usage.CompletionTokens
		} else if chunk.Usage.TotalTokens > sw.inputTokens {
			sw.outputTokens = chunk.Usage.TotalTokens - sw.inputTokens
		}
	}

	sw.ensureMessageStart()

	emptyStr := ""
	for _, choice := range chunk.Choices {
		var reasoningDelta, textDelta string
		if choice.Delta != nil {
			reasoningDelta = choice.Delta.ReasoningContent
			textDelta = choice.Delta.Content
		} else if choice.Message != nil {
			reasoningDelta = choice.Message.ReasoningContent
			textDelta = choice.Message.Content
		}

		if reasoningDelta != "" {
			if sw.activeBlockKind != "thinking" {
				sw.closeActiveBlock()
				writeAnthropicSSEEvent(&sw.outBuf, "content_block_start", anthropicSSEContentBlockStart{
					Type:         "content_block_start",
					Index:        sw.nextBlockIndex,
					ContentBlock: anthWireContentBlock{Type: "thinking", Thinking: &emptyStr},
				})
				sw.activeBlockKind = "thinking"
				sw.hasEmittedBlock = true
			}
			rCopy := reasoningDelta
			writeAnthropicSSEEvent(&sw.outBuf, "content_block_delta", anthropicSSEContentBlockDelta{
				Type:  "content_block_delta",
				Index: sw.nextBlockIndex,
				Delta: anthWireContentBlock{Type: "thinking_delta", Thinking: &rCopy},
			})
			sw.accReasoning.WriteString(reasoningDelta)
		}

		if textDelta != "" {
			if sw.activeBlockKind != "text" {
				sw.closeActiveBlock()
				writeAnthropicSSEEvent(&sw.outBuf, "content_block_start", anthropicSSEContentBlockStart{
					Type:         "content_block_start",
					Index:        sw.nextBlockIndex,
					ContentBlock: anthWireContentBlock{Type: "text", Text: &emptyStr},
				})
				sw.activeBlockKind = "text"
				sw.hasEmittedBlock = true
			}
			tCopy := textDelta
			writeAnthropicSSEEvent(&sw.outBuf, "content_block_delta", anthropicSSEContentBlockDelta{
				Type:  "content_block_delta",
				Index: sw.nextBlockIndex,
				Delta: anthWireContentBlock{Type: "text_delta", Text: &tCopy},
			})
			sw.accContent.WriteString(textDelta)
		}

		if strings.TrimSpace(choice.FinishReason) != "" {
			sw.finishReason = strings.TrimSpace(choice.FinishReason)
		}
	}
}

func (sw *anthropicSSEAdapterWriter) emitStreamEnd() {
	if sw.doneEmitted {
		return
	}
	sw.ensureMessageStart()
	emptyStr := ""
	if !sw.hasEmittedBlock {
		writeAnthropicSSEEvent(&sw.outBuf, "content_block_start", anthropicSSEContentBlockStart{
			Type:         "content_block_start",
			Index:        sw.nextBlockIndex,
			ContentBlock: anthWireContentBlock{Type: "text", Text: &emptyStr},
		})
		sw.activeBlockKind = "text"
		sw.hasEmittedBlock = true
	}
	sw.closeActiveBlock()

	outToks := sw.outputTokens
	if outToks <= 0 {
		totalLen := sw.accContent.Len() + sw.accReasoning.Len()
		if totalLen > 0 {
			outToks = (totalLen + 3) / 4
			if outToks <= 0 {
				outToks = 1
			}
		}
	}

	stopReason := mapOpenAIFinishReasonToAnthropic(sw.finishReason)
	writeAnthropicSSEEvent(&sw.outBuf, "message_delta", anthropicSSEMessageDelta{
		Type: "message_delta",
		Delta: anthropicMessageDeltaBody{
			StopReason:   stopReason,
			StopSequence: nil,
		},
		Usage: anthropicOutputUsage{
			OutputTokens: outToks,
		},
	})
	writeAnthropicSSEEvent(&sw.outBuf, "message_stop", anthropicSSEMessageStop{
		Type: "message_stop",
	})
	sw.doneEmitted = true
}

func (sw *anthropicSSEAdapterWriter) Finalize() {
	if !sw.wroteHeader || sw.passthrough || sw.statusCode != http.StatusOK {
		return
	}
	sw.outBuf.Reset()
	if sw.lineBuf.Len() > 0 {
		rem := sw.lineBuf.Bytes()
		sw.lineBuf.Reset()
		sw.processSSELine(rem)
	}
	if !sw.doneEmitted && !sw.sawStreamError {
		sw.emitStreamEnd()
	}
	if sw.outBuf.Len() > 0 {
		_, _ = sw.w.Write(sw.outBuf.Bytes())
		sw.outBuf.Reset()
		sw.Flush()
	}
}

func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeOpenAIError(w, http.StatusMethodNotAllowed, "only POST is supported for /v1/chat/completions", "invalid_request_error")
		return
	}

	bodyBytes, err := readRequestBodyFast(r, 32*1024*1024)
	if err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "failed to read request body: "+err.Error(), "invalid_request_error")
		return
	}

	var env chatCompletionEnvelope
	if err := json.Unmarshal(bodyBytes, &env); err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "invalid JSON payload: "+err.Error(), "invalid_request_error")
		return
	}
	req := env.OpenAIChatRequest

	if strings.TrimSpace(req.Model) == "" {
		req.Model = "antigravity/gemini-4-carbon-high"
	}

	resolved := s.resolver.Resolve(req.Model, s.cfg.FallbackModels)
	incomingAuth := r.Header.Get("Authorization")
	if incomingAuth == "" {
		incomingAuth = r.Header.Get("X-API-Key")
	}

	valid, apiKeyObj := ValidateAPIKey(incomingAuth)
	if !valid {
		writeOpenAIError(w, http.StatusUnauthorized, "invalid or missing OmniRoute API key", "invalid_api_key")
		return
	}

	if IsImageModel(req.Model) {
		ctx := WithAuditContext(r.Context(), apiKeyObj, req)
		if pinAcc := strings.TrimSpace(r.Header.Get("X-Omni-Pin-Account")); pinAcc != "" {
			ctx = WithPinnedAccount(ctx, pinAcc)
		}
		prompt := ""
		for i := len(req.Messages) - 1; i >= 0; i-- {
			if req.Messages[i].Role == "user" {
				prompt = req.Messages[i].ContentText()
				break
			}
		}
		if prompt == "" && len(req.Messages) > 0 {
			prompt = req.Messages[len(req.Messages)-1].ContentText()
		}
		imgResp, err := GenerateImageChatCompletion(ctx, req.Model, prompt)
		if err != nil {
			writeOpenAIError(w, http.StatusBadGateway, "failed to generate image: "+err.Error(), "image_generation_error")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(imgResp)
		if apiKeyObj != nil {
			RecordAPIKeyUsage(apiKeyObj.Key, 100)
		}
		return
	}

	// Check In-Memory Pre-Serialized Prompt & Response Cache (only when toolless)
	if len(env.Tools) == 0 {
		if entry, ok := GetGlobalCache().GetEntry(req); ok {
			toks := 0
			if entry.Response.Usage != nil && entry.Response.Usage.TotalTokens > 0 {
				toks = entry.Response.Usage.TotalTokens
			} else {
				toks = EstimateTokens(req.Messages)
				if toks <= 0 {
					toks = 50
				}
			}
			if apiKeyObj != nil {
				RecordAPIKeyUsage(apiKeyObj.Key, toks)
			}
			recordCacheHitGatewayLog(entry, req, resolved, apiKeyObj, toks)

			hdr := w.Header()
			hdr.Set("X-Omni-Gateway-Cache", "HIT")
			hdr.Set("X-Omni-Gateway-Account", "in-memory-cache")
			hdr.Set("X-Omni-Gateway-Provider", string(resolved.Provider))
			hdr.Set("X-Omni-Gateway-Resolved-Model", resolved.CanonicalID)
			hdr.Set("X-Omni-Gateway-Upstream-Model-Version", "cache-hit")
			hdr.Set("X-Omni-Gateway-Tokens", strconv.Itoa(toks))

			if req.Stream {
				hdr.Set("Content-Type", "text/event-stream")
				hdr.Set("Cache-Control", "no-cache")
				hdr.Set("Connection", "keep-alive")
				hdr.Set("X-Accel-Buffering", "no")
				w.WriteHeader(http.StatusOK)
				includeUsage := env.StreamOptions != nil && env.StreamOptions.IncludeUsage
				_, _ = w.Write(entry.SSEPayload(includeUsage))
				if flusher, ok := w.(http.Flusher); ok {
					flusher.Flush()
				}
				return
			}

			hdr.Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(entry.JSONBytes)
			return
		}
	}

	w.Header().Set("X-Omni-Gateway-Cache", "MISS")
	ctx := WithAuditContext(r.Context(), apiKeyObj, req)
	if pinAcc := strings.TrimSpace(r.Header.Get("X-Omni-Pin-Account")); pinAcc != "" {
		ctx = WithPinnedAccount(ctx, pinAcc)
	}

	_ = s.upstream.HandleChatCompletion(ctx, w, req, resolved, incomingAuth)
	if apiKeyObj != nil {
		usedTokens := parseRecordedTokens(w.Header().Get("X-Omni-Gateway-Tokens"), req)
		RecordAPIKeyUsage(apiKeyObj.Key, usedTokens)
	}
}

func extractAnthropicSystemText(raw json.RawMessage) string {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return ""
	}
	var sysStr string
	if err := json.Unmarshal(trimmed, &sysStr); err == nil {
		return strings.TrimSpace(sysStr)
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(trimmed, &blocks); err == nil && len(blocks) > 0 {
		var parts []string
		for _, b := range blocks {
			if t := strings.TrimSpace(b.Text); t != "" {
				parts = append(parts, t)
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

func (s *Server) handleAnthropicMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeOpenAIError(w, http.StatusMethodNotAllowed, "only POST is supported for /v1/messages", "invalid_request_error")
		return
	}
	bodyBytes, err := readRequestBodyFast(r, 32*1024*1024)
	if err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "failed to read request body: "+err.Error(), "invalid_request_error")
		return
	}

	var antReq struct {
		Model       string              `json:"model"`
		System      json.RawMessage     `json:"system,omitempty"`
		Messages    []OpenAIChatMessage `json:"messages"`
		MaxTokens   int                 `json:"max_tokens,omitempty"`
		Temperature *float64            `json:"temperature,omitempty"`
		Stream      bool                `json:"stream,omitempty"`
		Tools       []json.RawMessage   `json:"tools,omitempty"`
		Thinking    map[string]any      `json:"thinking,omitempty"`
		Metadata    map[string]string   `json:"metadata,omitempty"`
	}
	if err := json.Unmarshal(bodyBytes, &antReq); err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "invalid JSON payload: "+err.Error(), "invalid_request_error")
		return
	}

	var msgs []OpenAIChatMessage
	if sysStr := extractAnthropicSystemText(antReq.System); sysStr != "" {
		rawSys, _ := json.Marshal(sysStr)
		msgs = make([]OpenAIChatMessage, 0, len(antReq.Messages)+1)
		msgs = append(msgs, OpenAIChatMessage{Role: "system", Content: rawSys})
		msgs = append(msgs, antReq.Messages...)
	} else {
		msgs = antReq.Messages
	}

	model := strings.TrimSpace(antReq.Model)
	if model == "" {
		model = "claude-code/claude-sonnet-4-6"
	}

	maxTok := antReq.MaxTokens
	if maxTok <= 0 {
		maxTok = 0
	}

	oaiReq := OpenAIChatRequest{
		Model:       model,
		Messages:    msgs,
		Temperature: antReq.Temperature,
		MaxTokens:   maxTok,
		Stream:      antReq.Stream,
	}

	resolved := s.resolver.Resolve(oaiReq.Model, s.cfg.FallbackModels)
	incomingAuth := r.Header.Get("Authorization")
	if incomingAuth == "" {
		incomingAuth = r.Header.Get("x-api-key")
	}

	valid, apiKeyObj := ValidateAPIKey(incomingAuth)
	if !valid {
		writeOpenAIError(w, http.StatusUnauthorized, "invalid or missing OmniRoute API key", "invalid_api_key")
		return
	}

	// Sub-millisecond pre-serialized Anthropic JSON & SSE cache lookup when toolless & deterministic
	if len(antReq.Tools) == 0 {
		if entry, ok := GetGlobalCache().GetEntry(oaiReq); ok {
			toks := 0
			if entry.Response.Usage != nil && entry.Response.Usage.TotalTokens > 0 {
				toks = entry.Response.Usage.TotalTokens
			} else {
				toks = EstimateTokens(oaiReq.Messages)
				if toks <= 0 {
					toks = 50
				}
			}
			if apiKeyObj != nil {
				RecordAPIKeyUsage(apiKeyObj.Key, toks)
			}
			recordCacheHitGatewayLog(entry, oaiReq, resolved, apiKeyObj, toks)

			hdr := w.Header()
			hdr.Set("X-Omni-Gateway-Cache", "HIT")
			hdr.Set("X-Omni-Gateway-Account", "in-memory-cache")
			hdr.Set("X-Omni-Gateway-Provider", string(resolved.Provider))
			hdr.Set("X-Omni-Gateway-Resolved-Model", resolved.CanonicalID)
			hdr.Set("X-Omni-Gateway-Upstream-Model-Version", "cache-hit")
			hdr.Set("X-Omni-Gateway-Tokens", strconv.Itoa(toks))

			if antReq.Stream {
				hdr.Set("Content-Type", "text/event-stream")
				hdr.Set("Cache-Control", "no-cache")
				hdr.Set("Connection", "keep-alive")
				hdr.Set("X-Accel-Buffering", "no")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(entry.AnthropicSSEBytes)
				if flusher, ok := w.(http.Flusher); ok {
					flusher.Flush()
				}
				return
			}

			hdr.Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(entry.AnthropicJSONBytes)
			return
		}
	}

	w.Header().Set("X-Omni-Gateway-Cache", "MISS")
	ctx := WithAuditContext(r.Context(), apiKeyObj, oaiReq)
	if pinAcc := strings.TrimSpace(r.Header.Get("X-Omni-Pin-Account")); pinAcc != "" {
		ctx = WithPinnedAccount(ctx, pinAcc)
	}

	if !antReq.Stream {
		bw := &anthropicNonStreamBufferWriter{hdr: make(http.Header)}
		upErr := s.upstream.HandleChatCompletion(ctx, bw, oaiReq, resolved, incomingAuth)

		for k, vals := range bw.hdr {
			if strings.EqualFold(k, "Content-Length") {
				continue
			}
			for i, v := range vals {
				if i == 0 {
					w.Header().Set(k, v)
				} else {
					w.Header().Add(k, v)
				}
			}
		}
		w.Header().Set("X-Omni-Gateway-Cache", "MISS")

		code := bw.statusCode
		if code == 0 {
			if upErr != nil && bw.buf.Len() == 0 {
				code = http.StatusBadGateway
			} else {
				code = http.StatusOK
			}
		}

		if code != http.StatusOK {
			w.WriteHeader(code)
			if bw.buf.Len() > 0 {
				_, _ = w.Write(bw.buf.Bytes())
			}
		} else {
			var parsed OpenAIChatResponse
			if decErr := json.Unmarshal(bw.buf.Bytes(), &parsed); decErr == nil && parsed.Error == nil && (len(parsed.Choices) > 0 || parsed.Object == "chat.completion") {
				if parsed.Usage == nil {
					promptEst := EstimateTokens(oaiReq.Messages)
					parsed.Usage = &OpenAIUsage{
						PromptTokens: promptEst,
						TotalTokens:  promptEst,
					}
				} else if parsed.Usage.PromptTokens <= 0 {
					parsed.Usage.PromptTokens = EstimateTokens(oaiReq.Messages)
				}

				if len(antReq.Tools) == 0 && IsCacheable(oaiReq) && !GetGlobalCache().Has(oaiReq) {
					if len(parsed.Choices) == 0 || parsed.Choices[0].FinishReason != "length" {
						GetGlobalCache().Put(oaiReq, parsed)
					}
				}

				anthBytes := buildAnthropicJSONBytes(parsed, oaiReq.Model)
				w.Header().Set("Content-Type", "application/json")
				w.Header().Del("Content-Length")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(anthBytes)
			} else {
				w.WriteHeader(http.StatusOK)
				if bw.buf.Len() > 0 {
					_, _ = w.Write(bw.buf.Bytes())
				}
			}
		}
	} else {
		sw := newAnthropicSSEAdapterWriter(w, oaiReq.Model, EstimateTokens(oaiReq.Messages))
		upErr := s.upstream.HandleChatCompletion(ctx, sw, oaiReq, resolved, incomingAuth)
		sw.Finalize()

		if upErr == nil && sw.statusCode == http.StatusOK && !sw.passthrough && !sw.sawStreamError &&
			len(antReq.Tools) == 0 && IsCacheable(oaiReq) && sw.finishReason != "length" &&
			(sw.accContent.Len() > 0 || sw.accReasoning.Len() > 0) && !GetGlobalCache().Has(oaiReq) {
			finish := sw.finishReason
			if finish == "" {
				finish = "stop"
			}
			aggModel := sw.model
			if aggModel == "" {
				aggModel = oaiReq.Model
			}
			outToks := sw.outputTokens
			if outToks <= 0 {
				outToks = (sw.accContent.Len() + sw.accReasoning.Len() + 3) / 4
				if outToks <= 0 {
					outToks = 1
				}
			}
			cacheKeyReq := oaiReq
			cacheKeyReq.Stream = false
			GetGlobalCache().Put(cacheKeyReq, OpenAIChatResponse{
				ID:      sw.msgID,
				Object:  "chat.completion",
				Created: time.Now().Unix(),
				Model:   aggModel,
				Choices: []OpenAIChatChoice{{
					Index: 0,
					Message: &OpenAIOutputMessage{
						Role:             "assistant",
						Content:          sw.accContent.String(),
						ReasoningContent: sw.accReasoning.String(),
					},
					FinishReason: finish,
				}},
				Usage: &OpenAIUsage{
					PromptTokens:     sw.inputTokens,
					CompletionTokens: outToks,
					TotalTokens:      sw.inputTokens + outToks,
				},
			})
		}
	}

	if apiKeyObj != nil {
		usedTokens := parseRecordedTokens(w.Header().Get("X-Omni-Gateway-Tokens"), oaiReq)
		RecordAPIKeyUsage(apiKeyObj.Key, usedTokens)
	}
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS, HEAD")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Accept, X-API-Key, x-api-key, X-Admin-Token, X-Omni-Pin-Account, anthropic-version, anthropic-beta, OpenAI-Organization, OpenAI-Project")
		w.Header().Set("Access-Control-Expose-Headers", "X-Omni-Gateway-Provider, X-Omni-Gateway-Resolved-Model, X-Omni-Gateway-Requested-Model, X-Omni-Gateway-Route, X-Omni-Gateway-Thinking-Level, X-Omni-Gateway-Account, X-Omni-Gateway-Tokens, X-Omni-Gateway-Cache, X-Omni-Gateway-Upstream-Model-Version")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
