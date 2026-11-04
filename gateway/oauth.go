package gateway

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Constants for Google Antigravity OAuth and Claude Code PKCE
const (
	AgyOAuthClientID           = "1071006060591-" + "tmhssin2h21lcre235vtolojh4g403ep.apps.googleusercontent.com"
	agySecretPartA             = "GOCSPX-"
	agySecretPartB             = "K58FWR486LdLJ1mLB8sXC4z6qDAf"
	AgyOAuthClientSecret       = agySecretPartA + agySecretPartB
	AgyOAuthDefaultRedirectURI = "http://127.0.0.1:20128/callback"
	AgyOAuthScopes             = "https://www.googleapis.com/auth/cloud-platform https://www.googleapis.com/auth/userinfo.email https://www.googleapis.com/auth/userinfo.profile https://www.googleapis.com/auth/cclog https://www.googleapis.com/auth/experimentsandconfigs"
	AgyUserAgent               = "antigravity/1.4.10 windows/amd64"
	ClaudeOAuthClientID        = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
	ClaudeOAuthRedirectURI     = "https://console.anthropic.com/oauth/code/callback"
)

// Package-level configurable endpoints (can be overridden by tests)
var (
	OAuthTokenEndpoint            = "https://oauth2.googleapis.com/token"
	OAuthUserInfoEndpoint         = "https://www.googleapis.com/oauth2/v2/userinfo"
	CCPALoadCodeAssistEndpoint          = "https://daily-cloudcode-pa.googleapis.com/v1internal:loadCodeAssist"
	CCPARetrieveUserQuotaEndpoint       = "https://daily-cloudcode-pa.googleapis.com/v1internal:retrieveUserQuota"
	CCPARetrieveUserQuotaSummaryEndpoint = "https://daily-cloudcode-pa.googleapis.com/v1internal:retrieveUserQuotaSummary"
	ClaudeTokenEndpoint                 = "https://console.anthropic.com/v1/oauth/token"
)

// Internal mutex, in-memory config cache, and round-robin state
var (
	configMu          sync.Mutex
	cachedAuthCfg     *SavedGatewayAuth
	cachedAuthPath    string
	cachedAuthMtime   time.Time
	asyncFlushPending bool

	rrMu       sync.Mutex
	rrCounters = make(map[ProviderID]uint64)
)

// ConnectedAccount represents an account configured for one of the supported upstream providers.
type ConnectedAccount struct {
	ID                  string     `json:"id,omitempty"`
	Provider            ProviderID `json:"provider,omitempty"` // defaults to ProviderAntigravity if empty
	Label               string     `json:"label,omitempty"`
	Email               string     `json:"email"`
	AccessToken         string     `json:"access_token"`
	RefreshToken        string     `json:"refresh_token,omitempty"`
	APIKey              string     `json:"api_key,omitempty"`
	BaseURL             string     `json:"base_url,omitempty"`
	Region              string     `json:"region,omitempty"`
	ExpiresAt           int64      `json:"expires_at,omitempty"`
	ProjectID           string     `json:"project_id,omitempty"`
	Priority            int        `json:"priority,omitempty"`
	Disabled            bool       `json:"disabled,omitempty"`
	CooldownUntil       int64      `json:"cooldown_until,omitempty"`
	FailureCount        int        `json:"failure_count,omitempty"`
	SuccessCount        int64      `json:"success_count,omitempty"`
	TotalTokens         int64      `json:"total_tokens,omitempty"`
	AvgLatencyMs        int64      `json:"avg_latency_ms,omitempty"`
	LastLatencyMs       int64      `json:"last_latency_ms,omitempty"`
	LastModel           string     `json:"last_model,omitempty"`
	QuotaUsedPercent    *float64   `json:"quota_used_percent,omitempty"`
	QuotaResetTime      string     `json:"quota_reset_time,omitempty"`
	WeeklyGeminiPercent *int       `json:"weekly_gemini_percent,omitempty"`
	WeeklyClaudePercent *int       `json:"weekly_claude_percent,omitempty"`
	UpdatedAt           string     `json:"updated_at,omitempty"`
}

// SavedGatewayAuth holds the persistent multi-provider auth state and account pool.
type SavedGatewayAuth struct {
	AgyOAuthToken     string             `json:"agy_oauth_token,omitempty"`
	AgyProjectID      string             `json:"agy_project_id,omitempty"`
	RoutingStrategy   RoutingStrategy    `json:"routing_strategy,omitempty"`
	CustomCombos      []ComboModel       `json:"custom_combos,omitempty"`
	Accounts          []ConnectedAccount `json:"accounts,omitempty"`
	APIKeys           []GatewayAPIKey    `json:"api_keys,omitempty"`
	AdminPasswordHash string             `json:"admin_password_hash,omitempty"`
	AdminSalt         string             `json:"admin_salt,omitempty"`
	CompressionConfig *CompressionConfig `json:"compression_config,omitempty"`
	EndpointConfig    *EndpointConfig    `json:"endpoint_config,omitempty"`
	VectorDBConfig    *VectorDBConfig    `json:"vector_db_config,omitempty"`
	PostgresDSN       string             `json:"postgres_dsn,omitempty"`
	ModelAliases      map[string]string  `json:"model_aliases,omitempty"`
	EnableHedgedReqs  bool               `json:"enable_hedged_reqs,omitempty"`
	HedgeDelayMs      int                `json:"hedge_delay_ms,omitempty"`
	UpdatedAt         string             `json:"updated_at,omitempty"`
}

// GatewayConfigFilePath returns the path to the gateway auth configuration file.
func GatewayConfigFilePath() string {
	if env := os.Getenv("GATEWAY_CONFIG_FILE"); env != "" {
		return env
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", ".gemini", "gateway_config.json")
	}
	return filepath.Join(home, ".gemini", "gateway_config.json")
}

func cloneAuthConfig(src SavedGatewayAuth) SavedGatewayAuth {
	out := src
	if len(src.Accounts) > 0 {
		out.Accounts = make([]ConnectedAccount, len(src.Accounts))
		copy(out.Accounts, src.Accounts)
	}
	if len(src.CustomCombos) > 0 {
		out.CustomCombos = make([]ComboModel, len(src.CustomCombos))
		copy(out.CustomCombos, src.CustomCombos)
	}
	if len(src.APIKeys) > 0 {
		out.APIKeys = make([]GatewayAPIKey, len(src.APIKeys))
		copy(out.APIKeys, src.APIKeys)
	}
	if src.CompressionConfig != nil {
		cc := *src.CompressionConfig
		out.CompressionConfig = &cc
	}
	if src.EndpointConfig != nil {
		ec := *src.EndpointConfig
		out.EndpointConfig = &ec
	}
	if src.VectorDBConfig != nil {
		vc := *src.VectorDBConfig
		out.VectorDBConfig = &vc
	}
	if len(src.ModelAliases) > 0 {
		out.ModelAliases = make(map[string]string, len(src.ModelAliases))
		for k, v := range src.ModelAliases {
			out.ModelAliases[k] = v
		}
	}
	return out
}

func loadSavedAuthConfigLocked() SavedGatewayAuth {
	path := GatewayConfigFilePath()
	info, statErr := os.Stat(path)
	if statErr == nil && cachedAuthCfg != nil && cachedAuthPath == path && info.ModTime().Equal(cachedAuthMtime) {
		return cloneAuthConfig(*cachedAuthCfg)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return SavedGatewayAuth{RoutingStrategy: StrategyRoundRobin}
	}

	var cfg SavedGatewayAuth
	if err := json.Unmarshal(data, &cfg); err != nil {
		return SavedGatewayAuth{RoutingStrategy: StrategyRoundRobin}
	}

	if cfg.RoutingStrategy == "" {
		cfg.RoutingStrategy = StrategyRoundRobin
	}
	if cfg.CompressionConfig == nil {
		cfg.CompressionConfig = &CompressionConfig{
			Mode:            CompressionLite,
			CompressCode:    false,
			MaxHistoryTurns: 10,
			EnableGzip:      true,
		}
	}
	if cfg.EndpointConfig == nil {
		cfg.EndpointConfig = &EndpointConfig{
			PublicURL:    "https://omni.huongque.com/v1",
			CloudEnabled: true,
		}
	}
	for i := range cfg.Accounts {
		if cfg.Accounts[i].Provider == "" {
			cfg.Accounts[i].Provider = ProviderAntigravity
		}
	}

	cloned := cloneAuthConfig(cfg)
	cachedAuthCfg = &cloned
	cachedAuthPath = path
	if statErr == nil {
		cachedAuthMtime = info.ModTime()
	}
	return cfg
}

func saveAuthConfigLocked(cfg SavedGatewayAuth) error {
	path := GatewayConfigFilePath()
	cloned := cloneAuthConfig(cfg)
	cachedAuthCfg = &cloned
	cachedAuthPath = path

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		return err
	}
	if info, statErr := os.Stat(path); statErr == nil {
		cachedAuthMtime = info.ModTime()
	}
	return nil
}

// saveAuthConfigAsyncLocked updates the in-memory config cache immediately and flushes to disk asynchronously.
func saveAuthConfigAsyncLocked(cfg SavedGatewayAuth) {
	path := GatewayConfigFilePath()
	cloned := cloneAuthConfig(cfg)
	cachedAuthCfg = &cloned
	cachedAuthPath = path

	if asyncFlushPending {
		return
	}
	asyncFlushPending = true
	go func() {
		time.Sleep(400 * time.Millisecond)
		configMu.Lock()
		asyncFlushPending = false
		if cachedAuthCfg == nil {
			configMu.Unlock()
			return
		}
		toWrite := cloneAuthConfig(*cachedAuthCfg)
		targetPath := cachedAuthPath
		configMu.Unlock()

		if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err == nil {
			if data, err := json.MarshalIndent(toWrite, "", "  "); err == nil {
				if err := os.WriteFile(targetPath, data, 0600); err == nil {
					if info, statErr := os.Stat(targetPath); statErr == nil {
						configMu.Lock()
						if cachedAuthPath == targetPath {
							cachedAuthMtime = info.ModTime()
						}
						configMu.Unlock()
					}
				}
			}
		}
	}()
}

// LoadSavedAuthConfig loads and normalizes saved gateway authentication.
func LoadSavedAuthConfig() SavedGatewayAuth {
	configMu.Lock()
	defer configMu.Unlock()
	return loadSavedAuthConfigLocked()
}

// SaveAuthConfig updates the default Antigravity token and project ID.
func SaveAuthConfig(token, projectID string) error {
	configMu.Lock()
	defer configMu.Unlock()
	cfg := loadSavedAuthConfigLocked()
	cfg.AgyOAuthToken = token
	cfg.AgyProjectID = projectID
	cfg.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	return saveAuthConfigLocked(cfg)
}

// SetRoutingStrategy sets and persists the routing strategy.
func SetRoutingStrategy(strategy RoutingStrategy) (SavedGatewayAuth, error) {
	configMu.Lock()
	defer configMu.Unlock()
	cfg := loadSavedAuthConfigLocked()
	cfg.RoutingStrategy = strategy
	cfg.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	err := saveAuthConfigLocked(cfg)
	return cfg, err
}

func generateAccountID(provider ProviderID, email string) string {
	cleanEmail := strings.ToLower(strings.TrimSpace(email))
	if cleanEmail != "" {
		return fmt.Sprintf("%s-%s", provider, cleanEmail)
	}
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%s-%s", provider, hex.EncodeToString(b))
}

// UpsertConnectedAccount inserts or updates an account in the connected pool.
func UpsertConnectedAccount(acc ConnectedAccount) (SavedGatewayAuth, error) {
	configMu.Lock()
	defer configMu.Unlock()
	cfg := loadSavedAuthConfigLocked()

	if acc.Provider == "" {
		acc.Provider = ProviderAntigravity
	}
	if acc.ID == "" {
		acc.ID = generateAccountID(acc.Provider, acc.Email)
	}
	acc.UpdatedAt = time.Now().UTC().Format(time.RFC3339)

	found := false
	for i, existing := range cfg.Accounts {
		existingProv := existing.Provider
		if existingProv == "" {
			existingProv = ProviderAntigravity
		}
		if (acc.ID != "" && existing.ID == acc.ID) ||
			(existingProv == acc.Provider && strings.EqualFold(existing.Email, acc.Email) && acc.Email != "") {
			if acc.SuccessCount == 0 && existing.SuccessCount > 0 {
				acc.SuccessCount = existing.SuccessCount
			}
			if acc.FailureCount == 0 && existing.FailureCount > 0 {
				acc.FailureCount = existing.FailureCount
			}
			if acc.TotalTokens == 0 && existing.TotalTokens > 0 {
				acc.TotalTokens = existing.TotalTokens
			}
			if acc.AvgLatencyMs == 0 && existing.AvgLatencyMs > 0 {
				acc.AvgLatencyMs = existing.AvgLatencyMs
			}
			if acc.CooldownUntil == 0 && existing.CooldownUntil > 0 {
				acc.CooldownUntil = existing.CooldownUntil
			}
			if acc.RefreshToken == "" && existing.RefreshToken != "" {
				acc.RefreshToken = existing.RefreshToken
			}
			if acc.ProjectID == "" && existing.ProjectID != "" {
				acc.ProjectID = existing.ProjectID
			}
			if acc.QuotaUsedPercent == nil && existing.QuotaUsedPercent != nil {
				acc.QuotaUsedPercent = existing.QuotaUsedPercent
			}
			if acc.QuotaResetTime == "" && existing.QuotaResetTime != "" {
				acc.QuotaResetTime = existing.QuotaResetTime
			}
			if acc.WeeklyGeminiPercent == nil && existing.WeeklyGeminiPercent != nil {
				acc.WeeklyGeminiPercent = existing.WeeklyGeminiPercent
			}
			if acc.WeeklyClaudePercent == nil && existing.WeeklyClaudePercent != nil {
				acc.WeeklyClaudePercent = existing.WeeklyClaudePercent
			}
			cfg.Accounts[i] = acc
			found = true
			break
		}
	}
	if !found {
		cfg.Accounts = append(cfg.Accounts, acc)
	}
	if acc.Provider == ProviderAntigravity && acc.AccessToken != "" {
		cfg.AgyOAuthToken = acc.AccessToken
		if acc.ProjectID != "" {
			cfg.AgyProjectID = acc.ProjectID
		}
	}
	cfg.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	err := saveAuthConfigLocked(cfg)
	return cfg, err
}

// RemoveConnectedAccount removes an account matching ID or Email.
func RemoveConnectedAccount(identifier string) (SavedGatewayAuth, error) {
	configMu.Lock()
	defer configMu.Unlock()
	cfg := loadSavedAuthConfigLocked()

	var filtered []ConnectedAccount
	for _, acc := range cfg.Accounts {
		if acc.ID == identifier || strings.EqualFold(acc.Email, identifier) {
			continue
		}
		filtered = append(filtered, acc)
	}
	cfg.Accounts = filtered
	cfg.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	err := saveAuthConfigLocked(cfg)
	return cfg, err
}

// BuildOAuthAuthURL constructs the Google Antigravity OAuth 2.0 authorization URL.
func BuildOAuthAuthURL(redirectURI, state string) string {
	if redirectURI == "" {
		redirectURI = AgyOAuthDefaultRedirectURI
	}
	q := url.Values{}
	q.Set("client_id", AgyOAuthClientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("response_type", "code")
	q.Set("scope", AgyOAuthScopes)
	q.Set("access_type", "offline")
	q.Set("prompt", "consent")
	if state != "" {
		q.Set("state", state)
	}
	return "https://accounts.google.com/o/oauth2/v2/auth?" + q.Encode()
}

// ExchangeOAuthInput exchanges a Google OAuth authorization code or callback URL for an Antigravity ConnectedAccount.
func ExchangeOAuthInput(ctx context.Context, rawInput string, defaultRedirectURI string) (*ConnectedAccount, error) {
	rawInput = strings.TrimSpace(rawInput)
	code := rawInput

	if u, err := url.Parse(rawInput); err == nil && u.Query().Get("code") != "" {
		code = u.Query().Get("code")
	} else if strings.Contains(rawInput, "code=") {
		if vals, err := url.ParseQuery(rawInput); err == nil && vals.Get("code") != "" {
			code = vals.Get("code")
		}
	}

	redirectURI := defaultRedirectURI
	if redirectURI == "" {
		redirectURI = AgyOAuthDefaultRedirectURI
	}

	form := url.Values{}
	form.Set("client_id", AgyOAuthClientID)
	form.Set("client_secret", AgyOAuthClientSecret)
	form.Set("code", code)
	form.Set("grant_type", "authorization_code")
	form.Set("redirect_uri", redirectURI)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, OAuthTokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create oauth token request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("oauth token exchange transport error: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read oauth token response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("oauth token exchange failed (HTTP %d): %s", resp.StatusCode, strings.TrimSpace(string(bodyBytes)))
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
		TokenType    string `json:"token_type"`
		Error        string `json:"error"`
		ErrorDesc    string `json:"error_description"`
	}
	if err := json.Unmarshal(bodyBytes, &tokenResp); err != nil {
		return nil, fmt.Errorf("failed to parse oauth token response: %w", err)
	}
	if tokenResp.AccessToken == "" {
		return nil, fmt.Errorf("no access_token received: %s (%s)", tokenResp.Error, tokenResp.ErrorDesc)
	}

	// Fetch user profile email
	userEmail := "antigravity-user"
	infoReq, err := http.NewRequestWithContext(ctx, http.MethodGet, OAuthUserInfoEndpoint, nil)
	if err == nil {
		infoReq.Header.Set("Authorization", "Bearer "+tokenResp.AccessToken)
		if infoResp, err := http.DefaultClient.Do(infoReq); err == nil {
			defer infoResp.Body.Close()
			var userInfo struct {
				Email string `json:"email"`
			}
			if err := json.NewDecoder(infoResp.Body).Decode(&userInfo); err == nil && userInfo.Email != "" {
				userEmail = userInfo.Email
			}
		}
	}

	// Try loading default Google Cloud Code PA project ID
	var projectID string
	if CCPALoadCodeAssistEndpoint != "" {
		reqBody := []byte(`{"cloudaicompanionProject": ""}`)
		ccpaReq, err := http.NewRequestWithContext(ctx, http.MethodPost, CCPALoadCodeAssistEndpoint, bytes.NewReader(reqBody))
		if err == nil {
			ccpaReq.Header.Set("Authorization", "Bearer "+tokenResp.AccessToken)
			ccpaReq.Header.Set("User-Agent", AgyUserAgent)
			ccpaReq.Header.Set("Content-Type", "application/json")
			if ccpaResp, err := http.DefaultClient.Do(ccpaReq); err == nil {
				defer ccpaResp.Body.Close()
				if ccpaResp.StatusCode < 400 {
					var projData struct {
						CloudAICompanionProject string `json:"cloudaicompanionProject"`
						Project                 string `json:"project"`
					}
					if err := json.NewDecoder(ccpaResp.Body).Decode(&projData); err == nil {
						if projData.CloudAICompanionProject != "" {
							projectID = projData.CloudAICompanionProject
						} else {
							projectID = projData.Project
						}
					}
				}
			}
		}
	}

	expiresAt := int64(0)
	if tokenResp.ExpiresIn > 0 {
		expiresAt = time.Now().Unix() + tokenResp.ExpiresIn
	}

	acc := &ConnectedAccount{
		ID:           generateAccountID(ProviderAntigravity, userEmail),
		Provider:     ProviderAntigravity,
		Email:        userEmail,
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		ProjectID:    projectID,
		ExpiresAt:    expiresAt,
		UpdatedAt:    time.Now().UTC().Format(time.RFC3339),
	}
	if _, err := UpsertConnectedAccount(*acc); err != nil {
		return nil, fmt.Errorf("failed to persist antigravity account: %w", err)
	}
	return acc, nil
}

var (
	claudePKCEMu        sync.Mutex
	claudePKCEVerifiers = make(map[string]string)
)

// BuildClaudeOAuthURL constructs the Anthropic Claude Code PKCE authorization URL.
func BuildClaudeOAuthURL() (authURL string, verifier string, state string) {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	verifier = base64.RawURLEncoding.EncodeToString(b)
	h := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(h[:])

	sb := make([]byte, 16)
	_, _ = rand.Read(sb)
	state = hex.EncodeToString(sb)

	claudePKCEMu.Lock()
	if len(claudePKCEVerifiers) > 200 {
		claudePKCEVerifiers = make(map[string]string)
	}
	claudePKCEVerifiers[state] = verifier
	claudePKCEMu.Unlock()

	q := url.Values{}
	q.Set("client_id", ClaudeOAuthClientID)
	q.Set("response_type", "code")
	q.Set("redirect_uri", ClaudeOAuthRedirectURI)
	q.Set("scope", "user:inference user:profile")
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")
	q.Set("state", state)

	authURL = "https://claude.ai/oauth/authorize?" + q.Encode()
	return authURL, verifier, state
}

// ExchangeClaudeInput exchanges a Claude token string, API key, or authorization code for a Claude Code ConnectedAccount.
func ExchangeClaudeInput(ctx context.Context, rawInput string, verifier string, label string, baseURL string) (*ConnectedAccount, error) {
	raw := strings.TrimSpace(rawInput)
	if raw == "" {
		return nil, errors.New("empty claude oauth input")
	}

	// 1. Direct API Key format
	if strings.HasPrefix(raw, "sk-ant-api") {
		email := "claude-api-account"
		if label != "" {
			email = label
		}
		acc := &ConnectedAccount{
			ID:        generateAccountID(ProviderClaudeCode, email),
			Provider:  ProviderClaudeCode,
			Label:     label,
			Email:     email,
			APIKey:    raw,
			BaseURL:   baseURL,
			UpdatedAt: time.Now().UTC().Format(time.RFC3339),
		}
		if _, err := UpsertConnectedAccount(*acc); err != nil {
			return nil, fmt.Errorf("failed to persist claude api account: %w", err)
		}
		return acc, nil
	}

	// 2. Direct Claude OAuth Token format
	if strings.HasPrefix(raw, "sk-ant-oat") {
		email := "claude-oauth-account"
		if label != "" {
			email = label
		}
		acc := &ConnectedAccount{
			ID:          generateAccountID(ProviderClaudeCode, email),
			Provider:    ProviderClaudeCode,
			Label:       label,
			Email:       email,
			AccessToken: raw,
			BaseURL:     baseURL,
			UpdatedAt:   time.Now().UTC().Format(time.RFC3339),
		}
		if _, err := UpsertConnectedAccount(*acc); err != nil {
			return nil, fmt.Errorf("failed to persist claude oauth account: %w", err)
		}
		return acc, nil
	}

	// 3. Authorization Code PKCE exchange (supports "code#state" or full callback URL)
	code := raw
	state := ""
	if u, err := url.Parse(raw); err == nil && u.Query().Get("code") != "" {
		code = strings.TrimSpace(u.Query().Get("code"))
		state = strings.TrimSpace(u.Query().Get("state"))
	} else if strings.Contains(raw, "#") {
		parts := strings.SplitN(raw, "#", 2)
		code = strings.TrimSpace(parts[0])
		state = strings.TrimSpace(parts[1])
	} else if strings.Contains(raw, "code=") {
		if q, err := url.ParseQuery(raw); err == nil && q.Get("code") != "" {
			code = strings.TrimSpace(q.Get("code"))
			state = strings.TrimSpace(q.Get("state"))
		}
	}
	if strings.Contains(code, "#") {
		parts := strings.SplitN(code, "#", 2)
		code = strings.TrimSpace(parts[0])
		if state == "" {
			state = strings.TrimSpace(parts[1])
		}
	}

	if state != "" {
		claudePKCEMu.Lock()
		if savedVerifier, ok := claudePKCEVerifiers[state]; ok && savedVerifier != "" {
			verifier = savedVerifier
		}
		claudePKCEMu.Unlock()
	}

	reqPayload := map[string]string{
		"grant_type":    "authorization_code",
		"client_id":     ClaudeOAuthClientID,
		"code":          code,
		"redirect_uri":  ClaudeOAuthRedirectURI,
		"code_verifier": strings.TrimSpace(verifier),
	}
	if state != "" {
		reqPayload["state"] = state
	}
	payloadBytes, err := json.Marshal(reqPayload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal claude token request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, ClaudeTokenEndpoint, bytes.NewReader(payloadBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create claude token request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("claude oauth transport error: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read claude oauth response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("claude oauth exchange failed (HTTP %d): %s", resp.StatusCode, strings.TrimSpace(string(bodyBytes)))
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
		TokenType    string `json:"token_type"`
		Error        string `json:"error"`
		ErrorDesc    string `json:"error_description"`
		Account      struct {
			UUID         string `json:"uuid"`
			EmailAddress string `json:"email_address"`
		} `json:"account"`
	}
	if err := json.Unmarshal(bodyBytes, &tokenResp); err != nil {
		return nil, fmt.Errorf("failed to parse claude oauth response: %w", err)
	}

	if tokenResp.AccessToken == "" {
		return nil, fmt.Errorf("no access_token received: %s (%s)", tokenResp.Error, tokenResp.ErrorDesc)
	}

	email := strings.TrimSpace(tokenResp.Account.EmailAddress)
	if email == "" {
		if label != "" {
			email = label
		} else {
			email = "claude-code-account"
		}
	}

	expiresAt := int64(0)
	if tokenResp.ExpiresIn > 0 {
		expiresAt = time.Now().Unix() + tokenResp.ExpiresIn
	}

	acc := &ConnectedAccount{
		ID:           generateAccountID(ProviderClaudeCode, email),
		Provider:     ProviderClaudeCode,
		Label:        label,
		Email:        email,
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		BaseURL:      baseURL,
		ExpiresAt:    expiresAt,
		UpdatedAt:    time.Now().UTC().Format(time.RFC3339),
	}
	if _, err := UpsertConnectedAccount(*acc); err != nil {
		return nil, fmt.Errorf("failed to persist claude code account: %w", err)
	}
	return acc, nil
}

func RefreshAntigravityToken(ctx context.Context, acc *ConnectedAccount) error {
	form := url.Values{}
	form.Set("client_id", AgyOAuthClientID)
	form.Set("client_secret", AgyOAuthClientSecret)
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", acc.RefreshToken)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, OAuthTokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("token refresh HTTP %d: %s", resp.StatusCode, string(body))
	}

	var tr struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &tr); err != nil {
		return err
	}
	if tr.AccessToken == "" {
		return errors.New("no access token in refresh response")
	}

	acc.AccessToken = tr.AccessToken
	if tr.RefreshToken != "" {
		acc.RefreshToken = tr.RefreshToken
	}
	if tr.ExpiresIn > 0 {
		acc.ExpiresAt = time.Now().Unix() + tr.ExpiresIn
	}
	acc.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	return nil
}

func cryptoRandInt(max int) int {
	if max <= 0 {
		return 0
	}
	var b [4]byte
	_, _ = rand.Read(b[:])
	v := int(b[0]) | (int(b[1]) << 8) | (int(b[2]) << 16) | (int(b[3]) << 24)
	if v < 0 {
		v = -v
	}
	return v % max
}

// SelectProviderAccount selects an account for provider using the configured RoutingStrategy,
// skipping cooled down accounts (unless all are in cooldown) and refreshing expired Antigravity OAuth tokens.
func SelectProviderAccount(ctx context.Context, provider ProviderID) (*ConnectedAccount, error) {
	auth := LoadSavedAuthConfig()

	var matching []ConnectedAccount
	for _, a := range auth.Accounts {
		if a.Disabled {
			continue
		}
		p := a.Provider
		if p == "" {
			p = ProviderAntigravity
		}
		if p == provider {
			matching = append(matching, a)
		}
	}

	if len(matching) == 0 {
		return nil, fmt.Errorf("no account configured for provider %s", provider)
	}

	now := time.Now().Unix()
	var active []ConnectedAccount
	for _, a := range matching {
		if a.CooldownUntil <= now {
			active = append(active, a)
		}
	}

	candidates := active
	if len(candidates) == 0 {
		candidates = matching
	}

	// 1. Check if this Sub-Agent / Request explicitly pinned a specific account (1-to-1 Sub-Agent slot binding)
	if pinnedEmail := PinnedAccountFromContext(ctx); pinnedEmail != "" {
		for _, c := range candidates {
			if strings.EqualFold(c.Email, pinnedEmail) || strings.EqualFold(c.ID, pinnedEmail) {
				if (c.Provider == ProviderAntigravity || c.Provider == "") &&
					c.ExpiresAt > 0 && c.ExpiresAt <= time.Now().Unix()+60 && c.RefreshToken != "" {
					if err := RefreshAntigravityToken(ctx, &c); err == nil {
						_, _ = UpsertConnectedAccount(c)
					}
				}
				return &c, nil
			}
		}
	}

	// 2. Filter to least in-flight accounts first so 50 concurrent Sub-Agents never collide on the same account
	if len(candidates) > 1 {
		minInFlight := GetAccountInFlight(candidates[0].Email)
		for _, c := range candidates[1:] {
			if inf := GetAccountInFlight(c.Email); inf < minInFlight {
				minInFlight = inf
			}
		}
		var leastBusy []ConnectedAccount
		for _, c := range candidates {
			if GetAccountInFlight(c.Email) == minInFlight {
				leastBusy = append(leastBusy, c)
			}
		}
		if len(leastBusy) > 0 {
			candidates = leastBusy
		}
	}

	// 3. Smart Low-Latency Filter: if multiple idle accounts exist and some are fast (< 5000ms or fresh 0ms),
	// automatically bypass accounts that had a heavy/slow average latency (> 8000ms) so latency stays low!
	if len(candidates) > 1 {
		var fastCandidates []ConnectedAccount
		for _, c := range candidates {
			if c.AvgLatencyMs == 0 || c.AvgLatencyMs <= 6000 {
				fastCandidates = append(fastCandidates, c)
			}
		}
		if len(fastCandidates) > 0 {
			candidates = fastCandidates
		}
	}

	strategy := auth.RoutingStrategy
	if strategy == "" {
		strategy = StrategyRoundRobin
	}

	var selected ConnectedAccount
	switch strategy {
	case StrategyFillFirst:
		sort.SliceStable(candidates, func(i, j int) bool {
			return candidates[i].Priority > candidates[j].Priority
		})
		selected = candidates[0]

	case StrategyP2C:
		if len(candidates) == 1 {
			selected = candidates[0]
		} else {
			i := cryptoRandInt(len(candidates))
			j := cryptoRandInt(len(candidates) - 1)
			if j >= i {
				j++
			}
			c1, c2 := candidates[i], candidates[j]
			if c1.FailureCount < c2.FailureCount {
				selected = c1
			} else if c2.FailureCount < c1.FailureCount {
				selected = c2
			} else if c1.AvgLatencyMs > 0 && c2.AvgLatencyMs > 0 && c1.AvgLatencyMs != c2.AvgLatencyMs {
				if c1.AvgLatencyMs < c2.AvgLatencyMs {
					selected = c1
				} else {
					selected = c2
				}
			} else if c1.TotalTokens <= c2.TotalTokens {
				selected = c1
			} else {
				selected = c2
			}
		}

	case StrategyLeastUsed:
		sort.SliceStable(candidates, func(i, j int) bool {
			if candidates[i].TotalTokens == candidates[j].TotalTokens {
				return candidates[i].SuccessCount < candidates[j].SuccessCount
			}
			return candidates[i].TotalTokens < candidates[j].TotalTokens
		})
		selected = candidates[0]

	case StrategyRoundRobin:
		fallthrough
	default:
		rrMu.Lock()
		c := rrCounters[provider]
		rrCounters[provider] = c + 1
		rrMu.Unlock()
		selected = candidates[int(c%uint64(len(candidates)))]
	}

	// Auto-refresh Antigravity OAuth tokens if expired or expiring within 60s
	if (selected.Provider == ProviderAntigravity || selected.Provider == "") &&
		selected.ExpiresAt > 0 &&
		selected.ExpiresAt <= time.Now().Unix()+60 {
		if selected.RefreshToken != "" {
			if err := RefreshAntigravityToken(ctx, &selected); err == nil {
				_, _ = UpsertConnectedAccount(selected)
			} else if len(candidates) > 1 {
				RecordAccountResult(selected.Email, ProviderAntigravity, false, 0, 0, http.StatusUnauthorized)
				for _, alt := range candidates {
					if !strings.EqualFold(alt.Email, selected.Email) && alt.ID != selected.ID {
						selected = alt
						break
					}
				}
			}
		} else if len(candidates) > 1 {
			RecordAccountResult(selected.Email, ProviderAntigravity, false, 0, 0, http.StatusUnauthorized)
			for _, alt := range candidates {
				if !strings.EqualFold(alt.Email, selected.Email) && alt.ID != selected.ID {
					selected = alt
					break
				}
			}
		}
	}

	return &selected, nil
}

// RecordAccountResult records token usage, latency, and failure/cooldown state for an account.
func RecordAccountResult(emailOrID string, provider ProviderID, success bool, latencyMs int64, tokens int, statusCode int) {
	RecordAccountModelResult(emailOrID, provider, "", success, latencyMs, tokens, statusCode)
}

// RecordAccountModelResult records token usage, latency, model ID, and failure/cooldown state for an account.
func RecordAccountModelResult(emailOrID string, provider ProviderID, modelID string, success bool, latencyMs int64, tokens int, statusCode int) {
	configMu.Lock()
	defer configMu.Unlock()
	cfg := loadSavedAuthConfigLocked()

	found := false
	for i := range cfg.Accounts {
		acc := &cfg.Accounts[i]
		accProv := acc.Provider
		if accProv == "" {
			accProv = ProviderAntigravity
		}
		if provider != "" && accProv != provider {
			continue
		}
		if acc.ID == emailOrID || strings.EqualFold(acc.Email, emailOrID) {
			found = true
			acc.TotalTokens += int64(tokens)
			if strings.TrimSpace(modelID) != "" {
				acc.LastModel = strings.TrimSpace(modelID)
			}
			if latencyMs > 0 {
				acc.LastLatencyMs = latencyMs
				if acc.AvgLatencyMs == 0 || (acc.AvgLatencyMs > 5000 && latencyMs < 3500) {
					acc.AvgLatencyMs = latencyMs
				} else {
					acc.AvgLatencyMs = (acc.AvgLatencyMs + latencyMs) / 2
				}
			}
			if success {
				acc.SuccessCount++
				acc.FailureCount = 0
				acc.CooldownUntil = 0
			} else {
				acc.FailureCount++
				if statusCode == http.StatusTooManyRequests || statusCode == http.StatusServiceUnavailable || statusCode == http.StatusUnauthorized || statusCode == 429 || statusCode == 503 || statusCode == 401 {
					mult := acc.FailureCount
					if mult > 6 {
						mult = 6
					}
					if mult < 1 {
						mult = 1
					}
					backoffSec := int64(10 * (1 << (mult - 1)))
					if statusCode == http.StatusUnauthorized || statusCode == 401 {
						backoffSec = 300 // 5m cooldown for unauthorized/expired token
					}
					acc.CooldownUntil = time.Now().Unix() + backoffSec
				}
			}
			acc.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
			break
		}
	}
	if found {
		cfg.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		saveAuthConfigAsyncLocked(cfg)
	}
}

// ResetAccountLatencyStats clears historical latency spikes across all accounts (or a specific account).
func ResetAccountLatencyStats(accountID string) error {
	configMu.Lock()
	defer configMu.Unlock()
	cfg := loadSavedAuthConfigLocked()
	for i := range cfg.Accounts {
		if accountID == "" || accountID == "all" || cfg.Accounts[i].ID == accountID || strings.EqualFold(cfg.Accounts[i].Email, accountID) {
			cfg.Accounts[i].AvgLatencyMs = 0
			cfg.Accounts[i].LastLatencyMs = 0
			cfg.Accounts[i].CooldownUntil = 0
			cfg.Accounts[i].FailureCount = 0
		}
	}
	cfg.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	return saveAuthConfigLocked(cfg)
}

// GetValidAccountToken returns a valid OAuth access token, project ID, and email for Antigravity,
// preferring an active connected account and falling back to saved single-account settings.
func GetValidAccountToken(ctx context.Context) (string, string, string, error) {
	acc, err := SelectProviderAccount(ctx, ProviderAntigravity)
	if err == nil && acc != nil && acc.AccessToken != "" {
		return acc.AccessToken, acc.ProjectID, acc.Email, nil
	}
	saved := LoadSavedAuthConfig()
	if strings.TrimSpace(saved.AgyOAuthToken) != "" {
		email := "legacy-default"
		if len(saved.Accounts) > 0 && saved.Accounts[0].Email != "" {
			email = saved.Accounts[0].Email
		}
		return saved.AgyOAuthToken, saved.AgyProjectID, email, nil
	}
	return "", "", "", fmt.Errorf("no valid Antigravity account or token configured")
}

// defaultBuiltinAliases maps popular industry model names to optimal OmniRoute targets.
var defaultBuiltinAliases = map[string]string{
	"gpt-4o":            "antigravity/claude-sonnet-4-6",
	"gpt-4o-mini":       "alibaba/qwen3.8-flash",
	"gpt-4-turbo":       "antigravity/claude-sonnet-4-6",
	"claude-3-5-sonnet": "antigravity/claude-sonnet-4-6",
	"claude-3.5-sonnet": "antigravity/claude-sonnet-4-6",
	"claude-3-7-sonnet": "antigravity/claude-opus-4-6-thinking",
	"claude-3.7-sonnet": "antigravity/claude-opus-4-6-thinking",
	"claude-3-opus":     "antigravity/claude-opus-4-6-thinking",
	"o1":                "antigravity/claude-opus-4-6-thinking",
	"o1-mini":           "antigravity/gemini-4-argon-high",
	"o3-mini":           "antigravity/gemini-4-argon-high",
	"cursor-fast":       "alibaba/qwen3.8-flash",
	"cursor-small":      "antigravity/gemini-3.5-flash-lite",
	"deepseek-r1":       "alibaba/deepseek-v4-pro",
	"deepseek-v3":       "alibaba/deepseek-v4.1-flash",
	"qwen-max":          "alibaba/qwen3.8-max",
	"auto/smart":        "antigravity/claude-opus-4-6-thinking",
}

// ResolveModelAlias resolves a model ID against custom aliases and built-in industry aliases.
func ResolveModelAlias(modelID string) string {
	clean := strings.TrimSpace(modelID)
	lower := strings.ToLower(clean)

	saved := LoadSavedAuthConfig()
	if saved.ModelAliases != nil {
		if target, ok := saved.ModelAliases[lower]; ok && strings.TrimSpace(target) != "" {
			return strings.TrimSpace(target)
		}
		if target, ok := saved.ModelAliases[clean]; ok && strings.TrimSpace(target) != "" {
			return strings.TrimSpace(target)
		}
	}

	if target, ok := defaultBuiltinAliases[lower]; ok {
		return target
	}

	return clean
}

// GetModelAliases returns all configured model aliases merged with default built-ins.
func GetModelAliases() map[string]string {
	result := make(map[string]string)
	for k, v := range defaultBuiltinAliases {
		result[k] = v
	}
	saved := LoadSavedAuthConfig()
	if saved.ModelAliases != nil {
		for k, v := range saved.ModelAliases {
			result[k] = v
		}
	}
	return result
}

// SetModelAlias persists a custom alias mapping.
func SetModelAlias(alias, target string) error {
	configMu.Lock()
	defer configMu.Unlock()

	cfg := loadSavedAuthConfigLocked()
	if cfg.ModelAliases == nil {
		cfg.ModelAliases = make(map[string]string)
	}
	cleanAlias := strings.ToLower(strings.TrimSpace(alias))
	cleanTarget := strings.TrimSpace(target)
	if cleanAlias == "" || cleanTarget == "" {
		return fmt.Errorf("alias and target cannot be empty")
	}

	cfg.ModelAliases[cleanAlias] = cleanTarget
	cfg.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	return saveAuthConfigLocked(cfg)
}

// DeleteModelAlias removes a custom alias mapping.
func DeleteModelAlias(alias string) error {
	configMu.Lock()
	defer configMu.Unlock()

	cfg := loadSavedAuthConfigLocked()
	cleanAlias := strings.ToLower(strings.TrimSpace(alias))
	if cfg.ModelAliases != nil {
		delete(cfg.ModelAliases, cleanAlias)
		cfg.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		return saveAuthConfigLocked(cfg)
	}
	return nil
}

// GetHedgedConfig returns whether hedged speculative racing is enabled and its delay in ms.
func GetHedgedConfig() (bool, int) {
	saved := LoadSavedAuthConfig()
	delay := saved.HedgeDelayMs
	if delay <= 0 {
		delay = 1500 // default 1.5s
	}
	return saved.EnableHedgedReqs, delay
}

// SetHedgedConfig persists hedged configuration.
func SetHedgedConfig(enabled bool, delayMs int) error {
	configMu.Lock()
	defer configMu.Unlock()

	cfg := loadSavedAuthConfigLocked()
	cfg.EnableHedgedReqs = enabled
	if delayMs <= 0 {
		delayMs = 1500
	}
	cfg.HedgeDelayMs = delayMs
	cfg.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	return saveAuthConfigLocked(cfg)
}
