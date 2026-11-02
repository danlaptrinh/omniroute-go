package gateway

import (
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const GatewayVersion = "2.0.0-omniroute-go"

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
	return &Server{
		cfg:      cfg,
		resolver: resolver,
		upstream: upstream,
	}
}

// Handler returns the root http.Handler with CORS and GZIP middleware applied.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleRootOrHealth)
	mux.HandleFunc("/healthz", s.handleRootOrHealth)
	mux.HandleFunc("/ui", s.handleUI)
	mux.HandleFunc("/dashboard", s.handleUI)
	mux.HandleFunc("/dashboard/endpoint", s.handleUI)
	mux.HandleFunc("/callback", s.handleOAuthCallback)
	mux.HandleFunc("/v1/endpoint", s.handleEndpointAPI)
	mux.HandleFunc("/v1/endpoint/tunnel", s.handleEndpointTunnel)
	mux.HandleFunc("/v1/a2a", s.handleA2ADiscovery)
	mux.HandleFunc("/.well-known/agent.json", s.handleA2ADiscovery)
	mux.HandleFunc("/v1/mcp/info", s.handleMCPInfo)
	mux.HandleFunc("/v1/oauth/url", s.handleOAuthURL)
	mux.HandleFunc("/v1/oauth/exchange", s.handleOAuthExchange)
	mux.HandleFunc("/v1/oauth/claude/url", s.handleClaudeOAuthURL)
	mux.HandleFunc("/v1/oauth/claude/exchange", s.handleClaudeOAuthExchange)
	mux.HandleFunc("/v1/oauth/account", s.handleOAuthAccountDelete)
	mux.HandleFunc("/v1/providers/alibaba", s.handleAlibabaProviderSave)
	mux.HandleFunc("/v1/providers/strategy", s.handleRoutingStrategy)
	mux.HandleFunc("/v1/providers", s.handleProvidersList)
	mux.HandleFunc("/v1/auth/config", s.handleAuthConfig)
	mux.HandleFunc("/v1/auth/status", s.handleAuthStatus)
	mux.HandleFunc("/v1/auth/setup", s.handleAuthSetup)
	mux.HandleFunc("/v1/auth/login", s.handleAuthLogin)
	mux.HandleFunc("/v1/auth/logout", s.handleAuthLogout)
	mux.HandleFunc("/v1/auth/password", s.handleAuthPasswordChange)
	mux.HandleFunc("/v1/compression", s.handleCompression)
	mux.HandleFunc("/v1/models", s.handleModels)
	mux.HandleFunc("/models", s.handleModels)
	mux.HandleFunc("/v1/models/diagnostics", s.handleModelDiagnostics)
	mux.HandleFunc("/v1/models/test", s.handleModelTest)
	mux.HandleFunc("/v1/models/toggle-visibility", s.handleModelToggleVisibility)
	mux.HandleFunc("/v1/keys", s.handleAPIKeys)
	mux.HandleFunc("/v1/logs", s.handleLogs)
	mux.HandleFunc("/v1/database", s.handleDatabaseConfig)
	mux.HandleFunc("/v1/vector", s.handleVectorDB)
	mux.HandleFunc("/v1/quota", s.handleQuotaSummary)
	mux.HandleFunc("/v1/quota/toggle", s.handleQuotaToggle)
	mux.HandleFunc("/v1/quota/threshold", s.handleQuotaThreshold)
	mux.HandleFunc("/v1/cache", s.handleCacheStats)
	mux.HandleFunc("/v1/aliases", s.handleAliases)
	mux.HandleFunc("/v1/hedging", s.handleHedgingConfig)
	mux.HandleFunc("/v1/images/generations", s.handleImageGenerations)
	mux.HandleFunc("/v1/chat/completions", s.handleChatCompletions)
	mux.HandleFunc("/chat/completions", s.handleChatCompletions)
	mux.HandleFunc("/v1/messages", s.handleAnthropicMessages)
	mux.HandleFunc("/messages", s.handleAnthropicMessages)

	return withGzip(withCORS(mux))
}

func (s *Server) handleRootOrHealth(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" && r.URL.Path != "/healthz" {
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
		Label         string     `json:"label"`
		Email         string     `json:"email"`
		ProjectID     string     `json:"project_id,omitempty"`
		Region        string     `json:"region,omitempty"`
		BaseURL       string     `json:"base_url,omitempty"`
		InCooldown    bool       `json:"in_cooldown"`
		CooldownLeftS int64      `json:"cooldown_left_s"`
		SuccessCount  int64      `json:"success_count"`
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
		list = append(list, accountView{
			ID:            a.ID,
			Provider:      prov,
			Label:         a.Label,
			Email:         a.Email,
			ProjectID:     a.ProjectID,
			Region:        a.Region,
			BaseURL:       a.BaseURL,
			InCooldown:    cdLeft > 0,
			CooldownLeftS: cdLeft,
			SuccessCount:  a.SuccessCount,
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
		keys := ListAPIKeys()
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
		if q != "" {
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

func (s *Server) handleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	fullURL := fmt.Sprintf("http://%s%s", r.Host, r.URL.RequestURI())
	if r.Host == "" {
		fullURL = AgyOAuthDefaultRedirectURI + "?" + r.URL.RawQuery
	}

	acc, err := ExchangeOAuthInput(r.Context(), fullURL, AgyOAuthDefaultRedirectURI)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = fmt.Fprintf(w, `<!DOCTYPE html><html><body style="background:#0b0f19;color:#f87171;font-family:sans-serif;padding:40px;text-align:center;">
<h2>Kết nối OAuth thất bại</h2><p>%s</p><p><a href="http://127.0.0.1:8787/ui" style="color:#60a5fa;">Quay lại AI Gateway UI</a></p></body></html>`, html.EscapeString(err.Error()))
		return
	}

	s.cfg.AgyOAuthToken = acc.AccessToken
	if acc.ProjectID != "" {
		s.cfg.AgyProjectID = acc.ProjectID
	}

	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprintf(w, `<!DOCTYPE html><html><head><meta http-equiv="refresh" content="1;url=http://127.0.0.1:8787/ui?connected=1" /></head>
<body style="background:#0b0f19;color:#10b981;font-family:sans-serif;padding:40px;text-align:center;">
<h2>Đã kết nối thành công tài khoản Antigravity!</h2>
<p style="color:#e2e8f0;">Email: <strong>%s</strong> | Project ID: <code>%s</code></p>
<p><a href="http://127.0.0.1:8787/ui?connected=1" style="color:#60a5fa;">Đang chuyển hướng về AI Gateway Dashboard...</a></p>
</body></html>`, html.EscapeString(acc.Email), html.EscapeString(acc.ProjectID))
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

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeOpenAIError(w, http.StatusMethodNotAllowed, "method not allowed", "invalid_request_error")
		return
	}

	cards := s.resolver.ListModels()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"object": "list",
		"data":   cards,
	})
}

func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeOpenAIError(w, http.StatusMethodNotAllowed, "only POST is supported for /v1/chat/completions", "invalid_request_error")
		return
	}

	bodyBytes, err := io.ReadAll(io.LimitReader(r.Body, 32*1024*1024))
	if err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "failed to read request body: "+err.Error(), "invalid_request_error")
		return
	}
	defer r.Body.Close()

	var req OpenAIChatRequest
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "invalid JSON payload: "+err.Error(), "invalid_request_error")
		return
	}

	if strings.TrimSpace(req.Model) == "" {
		req.Model = "gemini-4-argon-high"
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

	ctx := WithAuditContext(r.Context(), apiKeyObj, req)

	if IsImageModel(req.Model) {
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

	// Check In-Memory Prompt & Response Cache
	if cachedResp, ok := GetGlobalCache().Get(req); ok {
		w.Header().Set("X-Omni-Gateway-Cache", "HIT")
		if apiKeyObj != nil {
			RecordAPIKeyUsage(apiKeyObj.Key, 1)
		}
		tokens := int64(50)
		if cachedResp.Usage != nil {
			tokens = int64(cachedResp.Usage.TotalTokens)
		}
		cachedContent := ""
		if len(cachedResp.Choices) > 0 && cachedResp.Choices[0].Message != nil {
			cachedContent = cachedResp.Choices[0].Message.Content
		}
		SetAuditResponse(ctx, cachedContent, EstimateTokens(req.Messages), int(tokens))
		cacheEntry := GatewayLogEntry{
			Provider:       resolved.Provider,
			Account:        "in-memory-cache",
			RequestedModel: req.Model,
			ResolvedModel:  resolved.CanonicalID,
			StatusCode:     http.StatusOK,
			LatencyMs:      0,
			TotalTokens:    int(tokens),
			IsError:        false,
		}
		EnrichLogEntryFromContext(ctx, &cacheEntry, req)
		RecordLogEntry(cacheEntry)
		if req.Stream {
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("Connection", "keep-alive")
			w.WriteHeader(http.StatusOK)
			flusher, _ := w.(http.Flusher)
			respID := fmt.Sprintf("chatcmpl-cache-%d", time.Now().UnixNano())
			content := ""
			reasoning := ""
			if len(cachedResp.Choices) > 0 && cachedResp.Choices[0].Message != nil {
				content = cachedResp.Choices[0].Message.Content
				reasoning = cachedResp.Choices[0].Message.ReasoningContent
			}
			chunk := OpenAIChatResponse{
				ID:      respID,
				Object:  "chat.completion.chunk",
				Created: time.Now().Unix(),
				Model:   req.Model,
				Choices: []OpenAIChatChoice{
					{
						Index: 0,
						Delta: &OpenAIOutputMessage{
							Role:             "assistant",
							Content:          content,
							ReasoningContent: reasoning,
						},
					},
				},
			}
			b, _ := json.Marshal(chunk)
			_, _ = fmt.Fprintf(w, "data: %s\n\n", b)
			finalChunk := OpenAIChatResponse{
				ID:      respID,
				Object:  "chat.completion.chunk",
				Created: time.Now().Unix(),
				Model:   req.Model,
				Choices: []OpenAIChatChoice{
					{
						Index:        0,
						Delta:        &OpenAIOutputMessage{},
						FinishReason: "stop",
					},
				},
			}
			bFinal, _ := json.Marshal(finalChunk)
			_, _ = fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", bFinal)
			if flusher != nil {
				flusher.Flush()
			}
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(cachedResp)
		if apiKeyObj != nil {
			toks := EstimateTokens(req.Messages)
			if cachedResp.Usage != nil && cachedResp.Usage.TotalTokens > 0 {
				toks = cachedResp.Usage.TotalTokens
			}
			RecordAPIKeyUsage(apiKeyObj.Key, toks)
		}
		return
	}
	w.Header().Set("X-Omni-Gateway-Cache", "MISS")

	_ = s.upstream.HandleChatCompletion(ctx, w, req, resolved, incomingAuth)
	if apiKeyObj != nil {
		usedTokens := parseRecordedTokens(w.Header().Get("X-Omni-Gateway-Tokens"), req)
		RecordAPIKeyUsage(apiKeyObj.Key, usedTokens)
	}
}

func (s *Server) handleAnthropicMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeOpenAIError(w, http.StatusMethodNotAllowed, "only POST is supported for /v1/messages", "invalid_request_error")
		return
	}
	bodyBytes, err := io.ReadAll(io.LimitReader(r.Body, 32*1024*1024))
	if err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "failed to read request body: "+err.Error(), "invalid_request_error")
		return
	}
	defer r.Body.Close()

	var antReq struct {
		Model     string              `json:"model"`
		System    any                 `json:"system,omitempty"`
		Messages  []OpenAIChatMessage `json:"messages"`
		MaxTokens int                 `json:"max_tokens,omitempty"`
		Stream    bool                `json:"stream,omitempty"`
		Thinking  map[string]any      `json:"thinking,omitempty"`
		Metadata  map[string]string   `json:"metadata,omitempty"`
	}
	if err := json.Unmarshal(bodyBytes, &antReq); err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "invalid JSON payload: "+err.Error(), "invalid_request_error")
		return
	}

	var msgs []OpenAIChatMessage
	if sysStr, ok := antReq.System.(string); ok && strings.TrimSpace(sysStr) != "" {
		rawSys, _ := json.Marshal(sysStr)
		msgs = append(msgs, OpenAIChatMessage{Role: "system", Content: rawSys})
	}
	msgs = append(msgs, antReq.Messages...)

	model := strings.TrimSpace(antReq.Model)
	if model == "" {
		model = "claude-code/claude-sonnet-4-6"
	}

	oaiReq := OpenAIChatRequest{
		Model:     model,
		Messages:  msgs,
		MaxTokens: antReq.MaxTokens,
		Stream:    antReq.Stream,
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

	ctx := WithAuditContext(r.Context(), apiKeyObj, oaiReq)
	_ = s.upstream.HandleChatCompletion(ctx, w, oaiReq, resolved, incomingAuth)
	if apiKeyObj != nil {
		usedTokens := parseRecordedTokens(w.Header().Get("X-Omni-Gateway-Tokens"), oaiReq)
		RecordAPIKeyUsage(apiKeyObj.Key, usedTokens)
	}
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS, HEAD")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Accept, X-API-Key, x-api-key, anthropic-version, anthropic-beta, OpenAI-Organization, OpenAI-Project")
		w.Header().Set("Access-Control-Expose-Headers", "X-Omni-Gateway-Provider, X-Omni-Gateway-Resolved-Model, X-Omni-Gateway-Requested-Model, X-Omni-Gateway-Route, X-Omni-Gateway-Thinking-Level, X-Omni-Gateway-Account, X-Omni-Gateway-Tokens")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
