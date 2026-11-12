package gateway

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
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
	AgyUserAgent               = "antigravity/2.6.0 windows/amd64"
	ClaudeOAuthClientID        = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
	ClaudeOAuthRedirectURI     = "https://console.anthropic.com/oauth/code/callback"
)

// GetAgyUserAgent returns OMNI_AGY_USER_AGENT if non-empty, otherwise AgyUserAgent.
func GetAgyUserAgent() string {
	if env := strings.TrimSpace(os.Getenv("OMNI_AGY_USER_AGENT")); env != "" {
		return env
	}
	return AgyUserAgent
}

// Package-level configurable endpoints (can be overridden by tests)
var (
	OAuthTokenEndpoint            = "https://oauth2.googleapis.com/token"
	OAuthUserInfoEndpoint         = "https://www.googleapis.com/oauth2/v2/userinfo"
	CCPALoadCodeAssistEndpoint          = "https://daily-cloudcode-pa.googleapis.com/v1internal:loadCodeAssist"
	CCPARetrieveUserQuotaEndpoint        = "https://daily-cloudcode-pa.googleapis.com/v1internal:retrieveUserQuota"
	CCPARetrieveUserQuotaSummaryEndpoint = "https://daily-cloudcode-pa.googleapis.com/v1internal:retrieveUserQuotaSummary"
	ClaudeTokenEndpoint                  = "https://platform.claude.com/v1/oauth/token"
	ClaudeOAuthUsageEndpoint             = "https://api.anthropic.com/api/oauth/usage"
)

// Internal mutex, in-memory config cache, and round-robin state
var (
	configMu          sync.Mutex
	cachedAuthCfg     *SavedGatewayAuth
	cachedAuthPath    string
	cachedAuthMtime   time.Time
	cachedAuthStatAt  time.Time
	asyncFlushPending bool

	oauthHTTPClient = &http.Client{
		Transport: SharedHTTPTransport,
		Timeout:   8 * time.Second,
	}

	accountSelectMu  sync.Mutex
	rrAntigravitySeq atomic.Uint64
	rrClaudeCodeSeq  atomic.Uint64
	rrAlibabaSeq     atomic.Uint64
	rrOtherSeqs      sync.Map // map[ProviderID]*atomic.Uint64

	accountModelRLMu       sync.RWMutex
	accountModelRateLimits = make(map[string]time.Time) // "email|normalized_model" -> expiresAt
)

// ConnectedAccount represents an account configured for one of the supported upstream providers.
type ConnectedAccount struct {
	ID                  string     `json:"id,omitempty"`
	Provider            ProviderID `json:"provider,omitempty"` // defaults to ProviderAntigravity if empty
	Tier                string     `json:"tier,omitempty"`     // "Ultra", "Pro", or "Lite"
	Label               string     `json:"label,omitempty"`
	Email               string     `json:"email"`
	AccessToken         string     `json:"access_token"`
	RefreshToken        string     `json:"refresh_token,omitempty"`
	APIKey              string     `json:"api_key,omitempty"`
	BaseURL             string     `json:"base_url,omitempty"`
	ProxyURL            string     `json:"proxy_url,omitempty"`
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
	if cachedAuthPath != "" && cachedAuthPath != path {
		ClearAccountModelRateLimits("")
	}
	if cachedAuthCfg != nil && cachedAuthPath == path && (asyncFlushPending || time.Since(cachedAuthStatAt) < 500*time.Millisecond) {
		return cloneAuthConfig(*cachedAuthCfg)
	}
	nowStat := time.Now()
	info, statErr := os.Stat(path)
	if statErr == nil && cachedAuthCfg != nil && cachedAuthPath == path && info.ModTime().Equal(cachedAuthMtime) {
		cachedAuthStatAt = nowStat
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
			PublicURL:    "https://omni.bucloud.vip/v1",
			CloudEnabled: true,
		}
	}
	if cfg.VectorDBConfig == nil {
		vc := defaultVectorDBConfig()
		cfg.VectorDBConfig = &vc
	}
	for i := range cfg.Accounts {
		if cfg.Accounts[i].Provider == "" {
			cfg.Accounts[i].Provider = ProviderAntigravity
		}
	}
	for i := range cfg.APIKeys {
		if cfg.APIKeys[i].KeyHash == "" && strings.TrimSpace(cfg.APIKeys[i].Key) != "" {
			cfg.APIKeys[i].KeyHash = ComputeAPIKeyHash(cfg.APIKeys[i].Key)
		}
	}

	cloned := cloneAuthConfig(cfg)
	cachedAuthCfg = &cloned
	cachedAuthPath = path
	cachedAuthStatAt = nowStat
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
	asyncFlushPending = false
	cachedAuthStatAt = time.Now()
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
		if !asyncFlushPending || cachedAuthCfg == nil {
			asyncFlushPending = false
			configMu.Unlock()
			return
		}
		asyncFlushPending = false
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

// FlushPendingConfigSave synchronously writes any pending asynchronous config update to disk.
func FlushPendingConfigSave() {
	configMu.Lock()
	defer configMu.Unlock()
	if cachedAuthCfg == nil || !asyncFlushPending {
		return
	}
	_ = saveAuthConfigLocked(*cachedAuthCfg)
	asyncFlushPending = false
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
			if acc.Tier == "" && existing.Tier != "" {
				acc.Tier = existing.Tier
			}
			if acc.ProxyURL == "" && existing.ProxyURL != "" {
				acc.ProxyURL = existing.ProxyURL
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

// RemoveConnectedAccount removes an account matching ID or Email from local config and PostgreSQL.
func RemoveConnectedAccount(identifier string) (SavedGatewayAuth, error) {
	ident := strings.TrimSpace(identifier)
	var extraIdentifiers []string

	configMu.Lock()
	cfg := loadSavedAuthConfigLocked()

	var filtered []ConnectedAccount
	removedDefaultToken := false
	for _, acc := range cfg.Accounts {
		if acc.ID == ident || strings.EqualFold(acc.ID, ident) || strings.EqualFold(acc.Email, ident) {
			if acc.ID != "" && !strings.EqualFold(acc.ID, ident) {
				extraIdentifiers = append(extraIdentifiers, acc.ID)
			}
			if acc.Email != "" && !strings.EqualFold(acc.Email, ident) {
				extraIdentifiers = append(extraIdentifiers, acc.Email)
			}
			if acc.AccessToken != "" && cfg.AgyOAuthToken == acc.AccessToken {
				removedDefaultToken = true
			}
			continue
		}
		filtered = append(filtered, acc)
	}
	cfg.Accounts = filtered

	hasRemainingAgy := false
	var replacementToken, replacementProject string
	for _, rem := range filtered {
		prov := rem.Provider
		if prov == "" {
			prov = ProviderAntigravity
		}
		if prov == ProviderAntigravity && strings.TrimSpace(rem.AccessToken) != "" {
			hasRemainingAgy = true
			if !rem.Disabled && replacementToken == "" {
				replacementToken = rem.AccessToken
				replacementProject = rem.ProjectID
			}
		}
	}
	if removedDefaultToken || !hasRemainingAgy {
		cfg.AgyOAuthToken = replacementToken
		cfg.AgyProjectID = replacementProject
	}
	cfg.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	err := saveAuthConfigLocked(cfg)
	configMu.Unlock()

	if ident != "" {
		EvictAccountTransportPool(ident)
		_ = DeleteAccountFromPostgres(ident)
		for _, extra := range extraIdentifiers {
			EvictAccountTransportPool(extra)
			_ = DeleteAccountFromPostgres(extra)
		}
	}
	return cfg, err
}

const accountTransportShards = 4

type accountIsolatedPool struct {
	key          string
	proxyURL     string
	tlsCache     tls.ClientSessionCache
	transports   [accountTransportShards]*http.Transport
	ccpaClients  [accountTransportShards]*http.Client
	shardCounter atomic.Uint64
}

var (
	accountTransportPools sync.Map       // key: normScope + "|" + cleanProxyURL -> *accountIsolatedPool
	testAccountRootCAs    *x509.CertPool // nil in production; used by local TLS isolation unit tests
)

// ValidateAccountProxyURL validates an optional per-account outbound proxy URL (http, https, socks5, socks5h).
func ValidateAccountProxyURL(raw string) (string, error) {
	clean := strings.TrimSpace(raw)
	if clean == "" {
		return "", nil
	}
	u, err := url.Parse(clean)
	if err != nil {
		return "", fmt.Errorf("invalid proxy URL: %w", err)
	}
	scheme := strings.ToLower(strings.TrimSpace(u.Scheme))
	switch scheme {
	case "http", "https", "socks5", "socks5h":
	default:
		return "", fmt.Errorf("unsupported proxy scheme %q (expected http, https, socks5, or socks5h)", u.Scheme)
	}
	if strings.TrimSpace(u.Host) == "" {
		return "", errors.New("proxy URL host cannot be empty")
	}
	return clean, nil
}

// GetAccountProxyURL returns the configured ProxyURL for an account matching emailOrID, or "" if unset.
func GetAccountProxyURL(emailOrID string) string {
	target := strings.TrimSpace(emailOrID)
	if target == "" || strings.EqualFold(target, "anon") {
		return ""
	}
	for _, a := range readAuthAccountsSnapshotFast() {
		if (a.Email != "" && strings.EqualFold(a.Email, target)) || (a.ID != "" && strings.EqualFold(a.ID, target)) {
			return strings.TrimSpace(a.ProxyURL)
		}
	}
	return ""
}

// SetAccountProxyURL updates (or clears when proxyURL == "") the outbound ProxyURL for emailOrID,
// persists the config synchronously, and evicts that account's transport pool so new connections take effect immediately.
func SetAccountProxyURL(emailOrID, proxyURL string) error {
	ident := strings.TrimSpace(emailOrID)
	if ident == "" {
		return errors.New("account identifier cannot be empty")
	}
	cleanProxy, err := ValidateAccountProxyURL(proxyURL)
	if err != nil {
		return err
	}

	var matchedEmail, matchedID string
	configMu.Lock()
	cfg := loadSavedAuthConfigLocked()
	found := false
	for i := range cfg.Accounts {
		if (cfg.Accounts[i].ID != "" && strings.EqualFold(cfg.Accounts[i].ID, ident)) ||
			(cfg.Accounts[i].Email != "" && strings.EqualFold(cfg.Accounts[i].Email, ident)) {
			cfg.Accounts[i].ProxyURL = cleanProxy
			matchedEmail = cfg.Accounts[i].Email
			matchedID = cfg.Accounts[i].ID
			found = true
			break
		}
	}
	if !found {
		configMu.Unlock()
		return fmt.Errorf("account %q not found", ident)
	}
	cfg.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	saveErr := saveAuthConfigLocked(cfg)
	configMu.Unlock()

	EvictAccountTransportPool(ident)
	if matchedEmail != "" {
		EvictAccountTransportPool(matchedEmail)
	}
	if matchedID != "" {
		EvictAccountTransportPool(matchedID)
	}
	return saveErr
}

func closeIsolatedPool(p *accountIsolatedPool) {
	if p == nil {
		return
	}
	for _, tr := range p.transports {
		if tr != nil {
			tr.CloseIdleConnections()
		}
	}
}

// EvictAccountTransportPool closes idle connections and removes cached transport pools for emailOrID.
func EvictAccountTransportPool(emailOrID string) {
	norm := strings.ToLower(strings.TrimSpace(emailOrID))
	if norm == "" {
		return
	}
	prefixes := []string{norm + "|"}
	for _, a := range readAuthAccountsSnapshotFast() {
		if strings.EqualFold(a.ID, norm) || strings.EqualFold(a.Email, norm) {
			if em := strings.ToLower(strings.TrimSpace(a.Email)); em != "" {
				prefixes = append(prefixes, em+"|")
			}
			if id := strings.ToLower(strings.TrimSpace(a.ID)); id != "" {
				prefixes = append(prefixes, id+"|")
			}
		}
	}
	accountTransportPools.Range(func(k, v any) bool {
		ks, ok := k.(string)
		if !ok {
			return true
		}
		for _, pfx := range prefixes {
			if strings.HasPrefix(ks, pfx) {
				accountTransportPools.Delete(k)
				if pool, okPool := v.(*accountIsolatedPool); okPool {
					closeIsolatedPool(pool)
				}
				break
			}
		}
		return true
	})
}

// InvalidateAccountHTTPClients closes and evicts cached HTTP/2 transports + TLS session caches for emailOrID
// so proxy_url updates take effect immediately on the very next request.
func InvalidateAccountHTTPClients(emailOrID string) {
	EvictAccountTransportPool(emailOrID)
}

// ResetAccountTransportPools closes and clears all per-account transport pools (used in tests and config reloads).
func ResetAccountTransportPools() {
	accountTransportPools.Range(func(k, v any) bool {
		accountTransportPools.Delete(k)
		if pool, okPool := v.(*accountIsolatedPool); okPool {
			closeIsolatedPool(pool)
		}
		return true
	})
}

func getOrCreateAccountIsolatedPool(accountScope, proxyURL string) *accountIsolatedPool {
	normScope := normalizeAccScope(accountScope)
	if normScope == "" || normScope == "anon" {
		return nil
	}
	cleanProxy := strings.TrimSpace(proxyURL)
	if cleanProxy == "" {
		cleanProxy = GetAccountProxyURL(normScope)
	}
	poolKey := normScope + "|" + cleanProxy
	if existing, ok := accountTransportPools.Load(poolKey); ok {
		return existing.(*accountIsolatedPool)
	}

	proxyFn := http.ProxyFromEnvironment
	if cleanProxy != "" {
		if parsedProxy, err := url.Parse(cleanProxy); err == nil && parsedProxy.Host != "" {
			proxyFn = http.ProxyURL(parsedProxy)
		}
	}

	acctTLSCache := tls.NewLRUClientSessionCache(32)
	fresh := &accountIsolatedPool{
		key:      poolKey,
		proxyURL: cleanProxy,
		tlsCache: acctTLSCache,
	}
	for i := 0; i < accountTransportShards; i++ {
		dialer := &net.Dialer{
			Timeout:   8 * time.Second,
			KeepAlive: 30 * time.Second,
		}
		tr := &http.Transport{
			Proxy:                 proxyFn,
			DialContext:           dialer.DialContext,
			MaxIdleConns:          32,
			MaxIdleConnsPerHost:   16,
			MaxConnsPerHost:       32,
			WriteBufferSize:       64 * 1024,
			ReadBufferSize:        64 * 1024,
			IdleConnTimeout:       180 * time.Second,
			TLSHandshakeTimeout:   8 * time.Second,
			ResponseHeaderTimeout: 90 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
			ForceAttemptHTTP2:     true,
			TLSClientConfig: &tls.Config{
				MinVersion:         tls.VersionTLS12,
				ClientSessionCache: acctTLSCache,
				RootCAs:            testAccountRootCAs,
			},
		}
		fresh.transports[i] = tr
		fresh.ccpaClients[i] = &http.Client{Transport: tr}
	}

	actual, loaded := accountTransportPools.LoadOrStore(poolKey, fresh)
	if loaded {
		closeIsolatedPool(fresh)
		return actual.(*accountIsolatedPool)
	}
	return fresh
}

// AccountHTTPTransport returns shard #0 of the dedicated per-account HTTP/2 transport pool for (email, proxyURL),
// guaranteeing strict TCP socket + TLS ClientSessionCache + proxy isolation between accounts.
func AccountHTTPTransport(email, proxyURL string) *http.Transport {
	pool := getOrCreateAccountIsolatedPool(email, proxyURL)
	if pool != nil && pool.transports[0] != nil {
		return pool.transports[0]
	}
	return SharedHTTPTransport
}

// AccountHTTPClient returns an *http.Client backed by AccountHTTPTransport(email, proxyURL) with timeout.
func AccountHTTPClient(email, proxyURL string, timeout time.Duration) *http.Client {
	return &http.Client{
		Transport: AccountHTTPTransport(email, proxyURL),
		Timeout:   timeout,
	}
}

func oauthClientForAccount(emailOrID, proxyURL string, timeout time.Duration) *http.Client {
	if oauthHTTPClient != nil && oauthHTTPClient.Transport != nil && oauthHTTPClient.Transport != SharedHTTPTransport {
		return oauthHTTPClient
	}
	scope := strings.TrimSpace(emailOrID)
	if scope != "" && !strings.EqualFold(scope, "anon") {
		return AccountHTTPClient(scope, proxyURL, timeout)
	}
	return oauthHTTPClient
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

	resp, err := oauthHTTPClient.Do(httpReq)
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
		if infoResp, err := oauthHTTPClient.Do(infoReq); err == nil {
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
			ccpaReq.Header.Set("User-Agent", GetAgyUserAgent())
			ccpaReq.Header.Set("Content-Type", "application/json")
			acctCl := oauthHTTPClient
			if userEmail != "" && userEmail != "antigravity-user" {
				acctCl = oauthClientForAccount(userEmail, "", 8*time.Second)
			}
			if ccpaResp, err := acctCl.Do(ccpaReq); err == nil {
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

	resp, err := oauthHTTPClient.Do(httpReq)
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

// RefreshOAuthToken exchanges an Antigravity Google OAuth refresh_token for a fresh access_token.
func RefreshOAuthToken(ctx context.Context, acc *ConnectedAccount) error {
	return RefreshAntigravityToken(ctx, acc)
}

func RefreshAntigravityToken(ctx context.Context, acc *ConnectedAccount) error {
	if acc == nil {
		return errors.New("nil account")
	}
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

	scopeKey := strings.TrimSpace(acc.Email)
	if scopeKey == "" {
		scopeKey = strings.TrimSpace(acc.ID)
	}
	resp, err := oauthClientForAccount(scopeKey, acc.ProxyURL, 8*time.Second).Do(req)
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

func nextProviderRRSeq(provider ProviderID) uint64 {
	switch provider {
	case "", ProviderAntigravity:
		return rrAntigravitySeq.Add(1) - 1
	case ProviderClaudeCode:
		return rrClaudeCodeSeq.Add(1) - 1
	case ProviderAlibaba:
		return rrAlibabaSeq.Add(1) - 1
	default:
		if v, ok := rrOtherSeqs.Load(provider); ok {
			return v.(*atomic.Uint64).Add(1) - 1
		}
		actual, _ := rrOtherSeqs.LoadOrStore(provider, &atomic.Uint64{})
		return actual.(*atomic.Uint64).Add(1) - 1
	}
}

func stripKnownModelPrefixes(raw string) string {
	m := strings.ToLower(strings.TrimSpace(raw))
	for m != "" {
		trimmed := false
		for _, pfx := range []string{"antigravity/", "agy/", "google/", "models/", "claude-code/", "alibaba/"} {
			if strings.HasPrefix(m, pfx) {
				m = strings.TrimSpace(strings.TrimPrefix(m, pfx))
				trimmed = true
				break
			}
		}
		if !trimmed {
			break
		}
	}
	return m
}

// IsDeepReasoningGeminiModel reports whether modelID represents a deep-reasoning Gemini or Carbon/Argon
// flagship tier where Google routes Ultra accounts to fast gemini-3.8-flash rather than gemini-3.8-flash-n.
func IsDeepReasoningGeminiModel(modelID string) bool {
	raw := strings.ToLower(strings.TrimSpace(modelID))
	if raw == "" {
		return false
	}
	m := stripKnownModelPrefixes(raw)
	if m == "" || strings.HasSuffix(m, "-low") || strings.HasSuffix(m, ":low") {
		return false
	}
	switch m {
	case "gemini-4-carbon-high", "gemini-4-carbon", "gemini-4-carbon:high", "carbon", "carbon-high",
		"gemini-4-carbon-medium", "gemini-4-carbon:medium", "carbon-medium",
		"gemini-4-argon-high", "gemini-4-argon", "gemini-4-argon:high",
		"gemini-4-argon-medium", "gemini-4-argon:medium",
		"gemini-3.8-flash-high", "gemini-3.8-flash", "gemini-3.8-flash-tiered", "gemini-3.8-flash:high",
		"gemini-3.8-flash-medium", "gemini-3.8-flash:medium":
		return true
	}
	if strings.HasPrefix(m, "gemini-3.1-pro") || strings.HasPrefix(m, "gemini-2.5-pro") {
		return true
	}
	return false
}

// HasConfiguredUltraAccount reports whether the saved auth config has at least one active Antigravity account with Tier == "Ultra".
func HasConfiguredUltraAccount() bool {
	accounts := readAuthAccountsSnapshotFast()
	for _, a := range accounts {
		if !a.Disabled && strings.EqualFold(strings.TrimSpace(a.Tier), "Ultra") {
			return true
		}
	}
	return false
}

// IsCarbonOrArgonHighOrMedium reports whether modelID represents Gemini 4 Carbon or Gemini 4 Argon High/Medium flagship models
// which must strictly be executed on Antigravity Ultra accounts without touching Pro accounts.
func IsCarbonOrArgonHighOrMedium(modelID string) bool {
	raw := strings.ToLower(strings.TrimSpace(modelID))
	if raw == "" || raw == "auto/fast" || raw == "auto/max-speed" {
		return false
	}
	m := stripKnownModelPrefixes(raw)
	if m == "auto/fast" || m == "auto/max-speed" || strings.HasSuffix(m, "-low") || strings.HasSuffix(m, ":low") || m == "carbon-low" {
		return false
	}
	return strings.HasPrefix(m, "gemini-4-carbon") ||
		strings.HasPrefix(m, "carbon") ||
		strings.HasPrefix(m, "gemini-4-argon") ||
		strings.HasPrefix(m, "argon")
}

// IsCarbonOrArgonFamilyModel reports whether modelID represents Gemini 4 Carbon or Gemini 4 Argon models
// which must strictly be executed on Antigravity Ultra accounts without touching Pro accounts.
func IsCarbonOrArgonFamilyModel(modelID string) bool {
	return IsCarbonOrArgonHighOrMedium(modelID)
}

// IsFastSpeedGeminiModel reports whether modelID targets low-latency flash / flash-lite / *-low / auto/fast pools
// where Pro accounts deliver ~0.8s latency while preserving Ultra concurrency slots for deep reasoning.
func IsFastSpeedGeminiModel(modelID string) bool {
	raw := strings.ToLower(strings.TrimSpace(modelID))
	if raw == "" {
		return false
	}
	if raw == "auto/fast" || raw == "auto/max-speed" {
		return true
	}
	m := stripKnownModelPrefixes(raw)
	if m == "" {
		return false
	}
	if m == "auto/fast" || m == "auto/max-speed" ||
		strings.HasSuffix(m, "-low") || strings.HasSuffix(m, ":low") || m == "carbon-low" {
		return true
	}
	if strings.HasPrefix(m, "gemini-3-flash") ||
		strings.HasPrefix(m, "gemini-3.5-flash-lite") ||
		strings.HasPrefix(m, "gemini-3.1-flash-lite") ||
		strings.HasPrefix(m, "gemini-2.5-flash") {
		return true
	}
	return false
}

func isGeminiPrimaryDeepReasoningModel(modelID string) bool {
	if !IsDeepReasoningGeminiModel(modelID) {
		return false
	}
	m := stripKnownModelPrefixes(modelID)
	return strings.HasPrefix(m, "gemini-3.8-flash") || strings.HasPrefix(m, "gemini-2.5-pro")
}

// TotalAntigravityInFlight returns the total concurrent in-flight requests across all enabled Antigravity accounts.
func TotalAntigravityInFlight() int {
	total := 0
	for _, a := range readAuthAccountsSnapshotFast() {
		if a.Disabled {
			continue
		}
		p := a.Provider
		if p == "" {
			p = ProviderAntigravity
		}
		if p != ProviderAntigravity || strings.TrimSpace(a.Email) == "" {
			continue
		}
		total += GetAccountInFlight(a.Email)
	}
	return total
}

// IsPinnedAccount reports whether ctx carries an explicit account pin via PinAccountContextKey.
func IsPinnedAccount(ctx context.Context) bool {
	return strings.TrimSpace(PinnedAccountFromContext(ctx)) != ""
}

// ReleaseAccountConcurrency decrements the active in-flight request counter for email.
func ReleaseAccountConcurrency(email string) {
	ReleaseAccountInFlight(email)
}

type hedgeSelectContextKey string

const (
	excludeAccountCtxKey hedgeSelectContextKey = "omni_exclude_account"
	hedgePickCtxKey      hedgeSelectContextKey = "omni_hedge_pick"
)

func withHedgeAlternateAccountContext(ctx context.Context, excludeEmail string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx = context.WithValue(ctx, excludeAccountCtxKey, strings.ToLower(strings.TrimSpace(excludeEmail)))
	return context.WithValue(ctx, hedgePickCtxKey, true)
}

func excludedAccountFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(excludeAccountCtxKey).(string); ok {
		return strings.ToLower(strings.TrimSpace(v))
	}
	return ""
}

func isHedgePickFromContext(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	v, ok := ctx.Value(hedgePickCtxKey).(bool)
	return ok && v
}

func normalizeRateLimitModelID(model string) string {
	m := strings.ToLower(strings.TrimSpace(model))
	if m == "" {
		return ""
	}
	if idx := strings.Index(m, "➔"); idx >= 0 {
		m = strings.TrimSpace(m[idx+len("➔"):])
	}
	if idx := strings.Index(m, "["); idx > 0 {
		m = strings.TrimSpace(m[:idx])
	}
	if idx := strings.Index(m, "("); idx > 0 {
		m = strings.TrimSpace(m[:idx])
	}
	if aliased, ok := defaultBuiltinAliases[m]; ok {
		m = strings.ToLower(strings.TrimSpace(aliased))
	}
	m = stripKnownModelPrefixes(m)
	if aliased, ok := defaultBuiltinAliases[m]; ok {
		m = stripKnownModelPrefixes(aliased)
	}
	switch m {
	case "model_placeholder_m26":
		return "claude-opus-4-6-thinking"
	case "model_placeholder_m35":
		return "claude-sonnet-4-6"
	}
	return m
}

func rateLimitProbeKeysForHint(modelHint string) []string {
	m := normalizeRateLimitModelID(modelHint)
	if m == "" {
		return nil
	}
	keys := []string{m}
	addUnique := func(extra string) {
		if extra != "" && extra != m {
			for _, existing := range keys {
				if existing == extra {
					return
				}
			}
			keys = append(keys, extra)
		}
	}
	switch {
	case m == "gemini-4-carbon-high" || m == "gemini-4-carbon" ||
		strings.HasPrefix(m, "gemini-3.1-pro"):
		addUnique("gemini-3.8-flash-high")
		addUnique("claude-opus-4-6-thinking")
	case m == "gemini-4-argon-high" || m == "gemini-4-argon" ||
		strings.HasPrefix(m, "claude-opus"):
		addUnique("claude-opus-4-6-thinking")
		addUnique("claude-opus-4-6")
	case m == "gemini-4-carbon-medium":
		addUnique("gemini-3.8-flash-medium")
		addUnique("claude-sonnet-4-6")
	case m == "gemini-4-argon-medium" ||
		strings.HasPrefix(m, "claude-sonnet"):
		addUnique("claude-sonnet-4-6")
	case m == "gemini-4-carbon-low" || m == "gemini-4-argon-low":
		addUnique("gemini-3.8-flash-low")
		addUnique("gemini-3-flash")
	case m == "gemini-3.8-flash-high" || m == "gemini-3.8-flash":
		addUnique("gemini-3.8-flash-high")
		addUnique("gemini-3-flash")
	case m == "gemini-3.8-flash-medium":
		addUnique("gemini-3.8-flash-medium")
		addUnique("gemini-3-flash")
	case strings.HasPrefix(m, "gemini-3.8-flash"):
		addUnique("gemini-3.8-flash-low")
		addUnique("gemini-3-flash")
	}
	return keys
}

func isClaudePrimaryModelID(normModel string) bool {
	return strings.HasPrefix(normModel, "claude-") ||
		normModel == "gemini-4-carbon-high" ||
		normModel == "gemini-4-carbon" ||
		normModel == "gemini-4-carbon-medium" ||
		normModel == "gemini-4-argon-high" ||
		normModel == "gemini-4-argon" ||
		normModel == "gemini-4-argon-medium"
}

func mirroredRateLimitModelKeys(normModel string) []string {
	switch normModel {
	case "claude-opus-4-6-thinking":
		return []string{"claude-opus-4-6-thinking", "claude-opus-4-6"}
	case "claude-opus-4-6":
		return []string{"claude-opus-4-6", "claude-opus-4-6-thinking"}
	default:
		return []string{normModel}
	}
}

// RecordAccountModelRateLimit marks a specific model (or CCPA backend model tier) as rate-limited (HTTP 429)
// on a single account for ttl without globally disabling the account for unrelated model families.
func RecordAccountModelRateLimit(email, model string, ttl time.Duration) {
	if ttl <= 0 {
		ttl = 2 * time.Minute
	}
	RecordAccountModelRateLimitUntil(email, model, time.Now().Add(ttl))
}

// RecordAccountModelRateLimitUntil marks a specific model as rate-limited on email until an explicit reset timestamp.
func RecordAccountModelRateLimitUntil(email, modelID string, until time.Time) {
	normEmail := strings.ToLower(strings.TrimSpace(email))
	normModel := normalizeRateLimitModelID(modelID)
	if normEmail == "" || normModel == "" || !until.After(time.Now()) {
		return
	}
	keys := mirroredRateLimitModelKeys(normModel)
	accountModelRLMu.Lock()
	for _, k := range keys {
		accountModelRateLimits[normEmail+"|"+k] = until
	}
	accountModelRLMu.Unlock()
}

// ClearAccountModelRateLimit clears any active per-account rate-limit marker for modelID on email.
func ClearAccountModelRateLimit(email, modelID string) {
	normEmail := strings.ToLower(strings.TrimSpace(email))
	normModel := normalizeRateLimitModelID(modelID)
	if normEmail == "" || normModel == "" {
		return
	}
	keys := mirroredRateLimitModelKeys(normModel)
	accountModelRLMu.Lock()
	for _, k := range keys {
		delete(accountModelRateLimits, normEmail+"|"+k)
	}
	accountModelRLMu.Unlock()
}

func isDirectGeminiTierRateLimited(email, modelHint string) bool {
	normEmail := strings.ToLower(strings.TrimSpace(email))
	normModel := normalizeRateLimitModelID(modelHint)
	if normEmail == "" || normModel == "" {
		return false
	}
	probes := []string{normModel}
	if strings.HasSuffix(normModel, "-medium") {
		if normModel != "gemini-3.8-flash-medium" {
			probes = append(probes, "gemini-3.8-flash-medium")
		}
	} else if normModel != "gemini-3.8-flash-high" {
		probes = append(probes, "gemini-3.8-flash-high")
	}
	now := time.Now()
	accountModelRLMu.RLock()
	limited := false
	for _, p := range probes {
		if exp, ok := accountModelRateLimits[normEmail+"|"+p]; ok && now.Before(exp) {
			limited = true
			break
		}
	}
	accountModelRLMu.RUnlock()
	if limited {
		return true
	}
	for _, p := range probes {
		if isAccountRateLimitedCCPAModel(DefaultCCPAEndpoint, normEmail, p, now) {
			return true
		}
	}
	return false
}

// IsAccountModelRateLimited reports whether email is currently in per-account HTTP 429 cooldown
// for model (including underlying Claude Opus / Sonnet tiers for Carbon/Argon High/Medium).
func IsAccountModelRateLimited(email, model string) bool {
	normEmail := strings.ToLower(strings.TrimSpace(email))
	if normEmail == "" {
		return false
	}
	normModel := normalizeRateLimitModelID(model)
	probes := rateLimitProbeKeysForHint(model)
	if len(probes) == 0 {
		return false
	}
	now := time.Now()

	accountModelRLMu.RLock()
	var expiredKeys []string
	limited := false
	for _, p := range probes {
		key := normEmail + "|" + p
		if exp, ok := accountModelRateLimits[key]; ok {
			if now.Before(exp) {
				limited = true
				break
			}
			expiredKeys = append(expiredKeys, key)
		}
	}
	accountModelRLMu.RUnlock()

	if len(expiredKeys) > 0 {
		accountModelRLMu.Lock()
		for _, k := range expiredKeys {
			if exp, ok := accountModelRateLimits[k]; ok && !now.Before(exp) {
				delete(accountModelRateLimits, k)
			}
		}
		accountModelRLMu.Unlock()
	}
	if limited {
		return true
	}

	if isClaudePrimaryModelID(normModel) && isAccountClaudeQuotaExhaustedFast(normEmail) {
		return true
	}

	for _, p := range probes {
		if isAccountRateLimitedCCPAModel(DefaultCCPAEndpoint, normEmail, p, now) {
			return true
		}
	}
	return false
}

// ClearAccountModelRateLimits clears per-account model 429 cooldown entries for email (or all accounts if empty/"all").
func ClearAccountModelRateLimits(email string) {
	normEmail := strings.ToLower(strings.TrimSpace(email))
	accountModelRLMu.Lock()
	defer accountModelRLMu.Unlock()
	if normEmail == "" || normEmail == "all" {
		accountModelRateLimits = make(map[string]time.Time)
		return
	}
	prefix := normEmail + "|"
	for k := range accountModelRateLimits {
		if strings.HasPrefix(k, prefix) {
			delete(accountModelRateLimits, k)
		}
	}
}

func clearSingleAccountModelRateLimit(email, model string) {
	normEmail := strings.ToLower(strings.TrimSpace(email))
	normModel := normalizeRateLimitModelID(model)
	if normEmail == "" || normModel == "" {
		return
	}
	accountModelRLMu.Lock()
	delete(accountModelRateLimits, normEmail+"|"+normModel)
	accountModelRLMu.Unlock()
}

// setAccountLiveRole updates the live active role displayed on the Dashboard ("ĐANG XỬ LÝ" pill)
// for an account that currently has at least one in-flight request.
func setAccountLiveRole(email string, role string) {
	cleanEmail := strings.TrimSpace(email)
	cleanRole := strings.TrimSpace(role)
	if cleanEmail == "" || cleanRole == "" {
		return
	}
	if GetAccountInFlight(cleanEmail) <= 0 {
		return
	}
	AcquireAccountInFlightWithRole(cleanEmail, cleanRole)
	ReleaseAccountInFlight(cleanEmail)
}

func readAuthAccountsSnapshotFast() []ConnectedAccount {
	configMu.Lock()
	path := GatewayConfigFilePath()
	var cfg SavedGatewayAuth
	if cachedAuthCfg != nil && cachedAuthPath == path && (asyncFlushPending || time.Since(cachedAuthStatAt) < 500*time.Millisecond) {
		cfg = *cachedAuthCfg
	} else {
		cfg = loadSavedAuthConfigLocked()
	}
	var out []ConnectedAccount
	if len(cfg.Accounts) > 0 {
		out = make([]ConnectedAccount, len(cfg.Accounts))
		copy(out, cfg.Accounts)
	}
	configMu.Unlock()
	return out
}

func lookupConnectedAntigravityAccountFast(emailOrID string) (ConnectedAccount, bool) {
	target := strings.TrimSpace(emailOrID)
	if target == "" || target == "anon" {
		return ConnectedAccount{}, false
	}
	configMu.Lock()
	path := GatewayConfigFilePath()
	var cfg SavedGatewayAuth
	if cachedAuthCfg != nil && cachedAuthPath == path && (asyncFlushPending || time.Since(cachedAuthStatAt) < 500*time.Millisecond) {
		cfg = *cachedAuthCfg
	} else {
		cfg = loadSavedAuthConfigLocked()
	}
	var matched ConnectedAccount
	found := false
	for _, a := range cfg.Accounts {
		if a.Disabled {
			continue
		}
		p := a.Provider
		if p == "" {
			p = ProviderAntigravity
		}
		if p != ProviderAntigravity {
			continue
		}
		if strings.EqualFold(a.Email, target) || strings.EqualFold(a.ID, target) {
			matched = a
			found = true
			break
		}
	}
	configMu.Unlock()
	return matched, found
}

func isAccountClaudeQuotaExhaustedFast(emailOrID string) bool {
	norm := strings.ToLower(strings.TrimSpace(emailOrID))
	if norm == "" {
		return false
	}
	acc, ok := lookupConnectedAntigravityAccountFast(norm)
	if !ok || acc.WeeklyClaudePercent == nil {
		return false
	}
	return *acc.WeeklyClaudePercent >= 100 || *acc.WeeklyClaudePercent <= 0
}

func hasAlternateFlagshipAccount(endpoint, currentEmail, modelHint string, flagshipCandidates []string) bool {
	if len(flagshipCandidates) == 0 {
		return false
	}
	if endpoint == "" {
		endpoint = DefaultCCPAEndpoint
	}
	accounts := readAuthAccountsSnapshotFast()
	if len(accounts) <= 1 {
		return false
	}
	nowUnix := time.Now().Unix()
	nowTime := time.Now()
	normCurrent := strings.ToLower(strings.TrimSpace(currentEmail))

	for _, a := range accounts {
		if a.Disabled {
			continue
		}
		p := a.Provider
		if p == "" {
			p = ProviderAntigravity
		}
		if p != ProviderAntigravity {
			continue
		}
		otherEmail := strings.ToLower(strings.TrimSpace(a.Email))
		if otherEmail == "" || otherEmail == normCurrent || strings.EqualFold(a.ID, normCurrent) {
			continue
		}
		if a.CooldownUntil > nowUnix {
			continue
		}
		if HasConfiguredUltraAccount() && IsCarbonOrArgonHighOrMedium(modelHint) && !strings.EqualFold(strings.TrimSpace(a.Tier), "Ultra") {
			// Pro accounts do not host Carbon/Argon Ultra flagship
			continue
		}
		if modelHint != "" && IsAccountModelRateLimited(otherEmail, modelHint) {
			continue
		}
		for _, cand := range flagshipCandidates {
			cand = strings.TrimSpace(cand)
			if cand == "" {
				continue
			}
			if strings.HasPrefix(cand, "claude-") && a.WeeklyClaudePercent != nil && *a.WeeklyClaudePercent >= 100 {
				continue
			}
			if !isDeadCCPAModel(endpoint, cand, nowTime) && !isAccountDeadCCPAModel(endpoint, otherEmail, cand, nowTime) {
				return true
			}
		}
	}
	return false
}

func buildInFlightReleaseFn(email string) func() {
	if strings.TrimSpace(email) == "" {
		return func() {}
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			ReleaseAccountInFlight(email)
		})
	}
}

// SelectProviderAccount selects an account for provider using the configured RoutingStrategy,
// skipping cooled down accounts (unless all are in cooldown) and refreshing expired Antigravity OAuth tokens.
func SelectProviderAccount(ctx context.Context, provider ProviderID) (*ConnectedAccount, error) {
	return SelectProviderAccountForModel(ctx, provider, "")
}

// SelectProviderAccountForModel selects an optimal healthy account for provider and modelHint,
// deprioritizing accounts whose modelHint (or underlying Claude Opus/Sonnet tier) is in per-account 429 cooldown
// and spreading simultaneous microsecond bursts deterministically across least-busy candidates via atomic round-robin.
func SelectProviderAccountForModel(ctx context.Context, provider ProviderID, modelHint string) (*ConnectedAccount, error) {
	acc, _, err := selectProviderAccountCore(ctx, provider, modelHint, "")
	return acc, err
}

// AcquireProviderAccountForModel atomically selects an optimal account for (provider, modelHint) and increments
// its in-flight counter inside the same critical section, eliminating TOCTOU burst pile-up across goroutines.
func AcquireProviderAccountForModel(ctx context.Context, provider ProviderID, modelHint string, role string) (*ConnectedAccount, func(), error) {
	if strings.TrimSpace(role) == "" {
		role = modelHint
	}
	if strings.TrimSpace(role) == "" {
		role = string(provider)
	}
	return selectProviderAccountCore(ctx, provider, modelHint, role)
}

func selectProviderAccountCore(ctx context.Context, provider ProviderID, modelHint string, acquireRole string) (*ConnectedAccount, func(), error) {
	if provider == "" {
		provider = ProviderAntigravity
	}
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
		return nil, func() {}, fmt.Errorf("no account configured for provider %s", provider)
	}

	now := time.Now().Unix()
	var active []ConnectedAccount
	for _, a := range matching {
		if a.CooldownUntil <= now {
			active = append(active, a)
		}
	}

	baseCandidates := active
	if len(baseCandidates) == 0 {
		baseCandidates = matching
	}

	// 1. Check if this Sub-Agent / Request explicitly pinned a specific account (1-to-1 Sub-Agent slot binding)
	if pinnedEmail := PinnedAccountFromContext(ctx); pinnedEmail != "" {
		for _, c := range baseCandidates {
			if strings.EqualFold(c.Email, pinnedEmail) || strings.EqualFold(c.ID, pinnedEmail) {
				acquiredEmail := ""
				if acquireRole != "" && c.Email != "" {
					accountSelectMu.Lock()
					AcquireAccountInFlightWithRole(c.Email, acquireRole)
					acquiredEmail = c.Email
					accountSelectMu.Unlock()
				}
				c = EnsureAccountTokenFresh(ctx, c, nil)
				return &c, buildInFlightReleaseFn(acquiredEmail), nil
			}
		}
	}

	if excludedEmail := excludedAccountFromContext(ctx); excludedEmail != "" {
		filtered := make([]ConnectedAccount, 0, len(active))
		for _, c := range active {
			if !strings.EqualFold(strings.TrimSpace(c.Email), excludedEmail) &&
				!strings.EqualFold(strings.TrimSpace(c.ID), excludedEmail) {
				filtered = append(filtered, c)
			}
		}
		if len(filtered) == 0 {
			return nil, func() {}, fmt.Errorf("no healthy alternate account available excluding %s", excludedEmail)
		}
		baseCandidates = filtered
	}

	accountSelectMu.Lock()
	candidates := make([]ConnectedAccount, len(baseCandidates))
	copy(candidates, baseCandidates)

	hasAnyTier := false
	if provider == ProviderAntigravity {
		for _, c := range candidates {
			if strings.TrimSpace(c.Tier) != "" {
				hasAnyTier = true
				break
			}
		}
	}

	// 2. Model-Quota Aware Filter: for Antigravity, deprioritize accounts where modelHint
	// (or underlying Claude Opus / Sonnet tier for Carbon/Argon High/Medium) is in per-account 429 cooldown.
	if provider == ProviderAntigravity && len(candidates) > 1 && strings.TrimSpace(modelHint) != "" {
		if hasAnyTier && HasConfiguredUltraAccount() && IsCarbonOrArgonHighOrMedium(modelHint) {
			// Carbon 4 / Argon 4 High requests must strictly route to Antigravity Ultra accounts without touching Pro accounts.
			ultraFresh := make([]ConnectedAccount, 0, len(candidates))
			for _, c := range candidates {
				if strings.EqualFold(strings.TrimSpace(c.Tier), "Ultra") &&
					!isDirectGeminiTierRateLimited(c.Email, modelHint) &&
					!IsAccountModelRateLimited(c.Email, modelHint) {
					ultraFresh = append(ultraFresh, c)
				}
			}
			if len(ultraFresh) > 0 {
				// Healthy Ultra accounts available: restrict candidates ONLY to Ultra accounts (Pro accounts NEVER touched)
				candidates = ultraFresh
			} else {
				// All Ultra accounts exhausted for Carbon/Argon: fall back to Pro accounts for Gemini 3.1 Pro High coding
				proFresh := make([]ConnectedAccount, 0, len(candidates))
				for _, c := range candidates {
					if !strings.EqualFold(strings.TrimSpace(c.Tier), "Ultra") &&
						!IsAccountModelRateLimited(c.Email, "gemini-3.1-pro-high") {
						proFresh = append(proFresh, c)
					}
				}
				if len(proFresh) > 0 {
					candidates = proFresh
				}
			}
		} else {
			modelFresh := make([]ConnectedAccount, 0, len(candidates))
			useDirectGeminiFilter := hasAnyTier && isGeminiPrimaryDeepReasoningModel(modelHint)
			for _, c := range candidates {
				limited := false
				if useDirectGeminiFilter {
					limited = isDirectGeminiTierRateLimited(c.Email, modelHint)
				} else {
					limited = IsAccountModelRateLimited(c.Email, modelHint)
				}
				if !limited {
					modelFresh = append(modelFresh, c)
				}
			}
			if len(modelFresh) > 0 {
				candidates = modelFresh
			} else if isHedgePickFromContext(ctx) {
				accountSelectMu.Unlock()
				return nil, func() {}, fmt.Errorf("no rate-limit-free alternate account for %s", modelHint)
			}
		}
	}

	// 2.5 Tier-Aware Scheduler (Ultra vs Pro): preserve 100% legacy behavior when no candidate has Tier set.
	if provider == ProviderAntigravity && hasAnyTier && len(candidates) > 1 {
		ultraCands := make([]ConnectedAccount, 0, len(candidates))
		nonUltraCands := make([]ConnectedAccount, 0, len(candidates))
		minOverallInFlight := -1
		minUltraInFlight := -1
		minProInFlight := -1

		for _, c := range candidates {
			inf := GetAccountInFlight(c.Email)
			if minOverallInFlight < 0 || inf < minOverallInFlight {
				minOverallInFlight = inf
			}
			if strings.EqualFold(strings.TrimSpace(c.Tier), "Ultra") {
				ultraCands = append(ultraCands, c)
				if minUltraInFlight < 0 || inf < minUltraInFlight {
					minUltraInFlight = inf
				}
			} else {
				nonUltraCands = append(nonUltraCands, c)
				if minProInFlight < 0 || inf < minProInFlight {
					minProInFlight = inf
				}
			}
		}

		if HasConfiguredUltraAccount() && IsCarbonOrArgonHighOrMedium(modelHint) {
			// Carbon 4 / Argon 4 strictly routes to Ultra accounts unless ALL Ultra accounts are exhausted
			if len(ultraCands) > 0 {
				candidates = ultraCands
			} else if len(nonUltraCands) > 0 {
				candidates = nonUltraCands
			}
		} else if isHedgePickFromContext(ctx) {
			if IsFastSpeedGeminiModel(modelHint) {
				if len(nonUltraCands) > 0 {
					proLowLoad := make([]ConnectedAccount, 0, len(nonUltraCands))
					for _, c := range nonUltraCands {
						if GetAccountInFlight(c.Email) <= 3 {
							proLowLoad = append(proLowLoad, c)
						}
					}
					if len(proLowLoad) > 0 {
						candidates = proLowLoad
					} else if len(ultraCands) > 0 && minProInFlight > 3 {
						candidates = ultraCands
					} else {
						candidates = nonUltraCands
					}
				}
			} else if len(ultraCands) > 0 {
				ultraLowLoad := make([]ConnectedAccount, 0, len(ultraCands))
				for _, c := range ultraCands {
					if GetAccountInFlight(c.Email) <= 2 {
						ultraLowLoad = append(ultraLowLoad, c)
					}
				}
				if len(ultraLowLoad) > 0 {
					candidates = ultraLowLoad
				} else if minUltraInFlight < 4 {
					candidates = ultraCands
				}
			}
		} else if len(ultraCands) > 0 && len(nonUltraCands) > 0 {
			if strings.HasPrefix(stripKnownModelPrefixes(strings.ToLower(modelHint)), "gemini-3.1-pro") {
				// Gemini 3.1 Pro High prioritizes Pro accounts to preserve Ultra accounts for Carbon & Argon
				if minProInFlight <= minOverallInFlight+3 {
					candidates = nonUltraCands
				}
			} else if IsDeepReasoningGeminiModel(modelHint) {
				if minUltraInFlight <= minOverallInFlight+2 && minUltraInFlight < 4 {
					candidates = ultraCands
				} else if minProInFlight <= minUltraInFlight {
					candidates = nonUltraCands
				}
			} else if IsFastSpeedGeminiModel(modelHint) {
				if minProInFlight <= minOverallInFlight+1 {
					candidates = nonUltraCands
				}
			}
		}
	}

	// 2.6 Per-Account 503/5xx Isolation Preference: if one account stepped down its primary model due to a 5xx,
	// prefer peers whose primary model is still healthy while keeping the stepped-down account eligible if needed.
	if provider == ProviderAntigravity && len(candidates) > 1 && strings.TrimSpace(modelHint) != "" {
		nowCheck := time.Now()
		nonDegraded := make([]ConnectedAccount, 0, len(candidates))
		for _, c := range candidates {
			if !isAccountOverloadedForHint(DefaultCCPAEndpoint, c.Email, modelHint, nowCheck) {
				nonDegraded = append(nonDegraded, c)
			}
		}
		if len(nonDegraded) > 0 {
			candidates = nonDegraded
		}
	}

	// 3. Filter to least in-flight accounts so concurrent Sub-Agents / requests never collide on the same account
	minInFlight := 0
	if len(candidates) > 1 {
		minInFlight = GetAccountInFlight(candidates[0].Email)
		for _, c := range candidates[1:] {
			if inf := GetAccountInFlight(c.Email); inf < minInFlight {
				minInFlight = inf
			}
		}
		leastBusy := make([]ConnectedAccount, 0, len(candidates))
		for _, c := range candidates {
			if GetAccountInFlight(c.Email) == minInFlight {
				leastBusy = append(leastBusy, c)
			}
		}
		if len(leastBusy) > 0 {
			candidates = leastBusy
		}
	}

	// 4. Extreme-Outlier Latency Guard: only bypass severely stalled accounts (> 45000ms avg) when idle accounts
	// exist (minInFlight == 0) and a majority of idle candidates are healthy, preventing normal 8s-28s long-output
	// Opus streaming averages from starving healthy accounts or causing +1 burst pile-ups.
	if len(candidates) > 1 && minInFlight == 0 {
		fastCandidates := make([]ConnectedAccount, 0, len(candidates))
		for _, c := range candidates {
			if c.AvgLatencyMs == 0 || c.AvgLatencyMs <= 45000 {
				fastCandidates = append(fastCandidates, c)
			}
		}
		if len(fastCandidates)*2 >= len(candidates) && len(fastCandidates) > 0 {
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
		topPriority := candidates[0].Priority
		tied := candidates[:1]
		for k := 1; k < len(candidates); k++ {
			if candidates[k].Priority == topPriority {
				tied = candidates[:k+1]
			} else {
				break
			}
		}
		if len(tied) == 1 {
			selected = tied[0]
		} else {
			seq := nextProviderRRSeq(provider)
			selected = tied[int(seq%uint64(len(tied)))]
		}

	case StrategyP2C:
		if len(candidates) == 1 {
			selected = candidates[0]
		} else {
			seq := nextProviderRRSeq(provider)
			n := uint64(len(candidates))
			i := int(seq % n)
			j := int((seq + 1 + (seq/n)%(n-1)) % n)
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
		topTokens := candidates[0].TotalTokens
		topSuccess := candidates[0].SuccessCount
		tied := candidates[:1]
		for k := 1; k < len(candidates); k++ {
			if candidates[k].TotalTokens == topTokens && candidates[k].SuccessCount == topSuccess {
				tied = candidates[:k+1]
			} else {
				break
			}
		}
		if len(tied) == 1 {
			selected = tied[0]
		} else {
			seq := nextProviderRRSeq(provider)
			selected = tied[int(seq%uint64(len(tied)))]
		}

	case StrategyRoundRobin:
		fallthrough
	default:
		seq := nextProviderRRSeq(provider)
		selected = candidates[int(seq%uint64(len(candidates)))]
	}

	acquiredEmail := ""
	if acquireRole != "" && selected.Email != "" {
		AcquireAccountInFlightWithRole(selected.Email, acquireRole)
		acquiredEmail = selected.Email
	}
	fallbackPool := make([]ConnectedAccount, len(candidates))
	copy(fallbackPool, candidates)
	accountSelectMu.Unlock()

	// Auto-refresh Antigravity and Claude Code OAuth tokens if expired or expiring within 60s
	selected = EnsureAccountTokenFresh(ctx, selected, fallbackPool)
	if acquireRole != "" && !strings.EqualFold(acquiredEmail, selected.Email) {
		if acquiredEmail != "" {
			ReleaseAccountInFlight(acquiredEmail)
		}
		acquiredEmail = ""
		if selected.Email != "" {
			AcquireAccountInFlightWithRole(selected.Email, acquireRole)
			acquiredEmail = selected.Email
		}
	}

	return &selected, buildInFlightReleaseFn(acquiredEmail), nil
}

// RecordAccountResult records token usage, latency, and failure/cooldown state for an account.
func RecordAccountResult(emailOrID string, provider ProviderID, success bool, latencyMs int64, tokens int, statusCode int) {
	RecordAccountModelResult(emailOrID, provider, "", success, latencyMs, tokens, statusCode)
}

// RecordAccountModelResult records token usage, latency, model ID, and failure/cooldown state for an account.
func RecordAccountModelResult(emailOrID string, provider ProviderID, modelID string, success bool, latencyMs int64, tokens int, statusCode int) {
	var matchedEmail string
	configMu.Lock()
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
			matchedEmail = acc.Email
			acc.TotalTokens += int64(tokens)
			if strings.TrimSpace(modelID) != "" {
				acc.LastModel = strings.TrimSpace(modelID)
			}
			if success && latencyMs > 0 {
				acc.LastLatencyMs = latencyMs
				if acc.AvgLatencyMs == 0 || (acc.AvgLatencyMs > 6000 && latencyMs < 4000) {
					acc.AvgLatencyMs = latencyMs
				} else {
					acc.AvgLatencyMs = (acc.AvgLatencyMs*3 + latencyMs) / 4
				}
			}
			if success {
				acc.SuccessCount++
				acc.FailureCount = 0
				acc.CooldownUntil = 0
			} else {
				acc.FailureCount++
				if statusCode == http.StatusTooManyRequests || statusCode == http.StatusServiceUnavailable || statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden || statusCode == 429 || statusCode == 503 || statusCode == 401 || statusCode == 403 {
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
					} else if statusCode == http.StatusForbidden || statusCode == 403 {
						backoffSec = 7200 // 2h cooldown for forbidden / VALIDATION_REQUIRED
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
	configMu.Unlock()

	targetEmail := matchedEmail
	if targetEmail == "" {
		targetEmail = emailOrID
	}
	if strings.TrimSpace(modelID) != "" && strings.TrimSpace(targetEmail) != "" {
		if !success && (statusCode == http.StatusTooManyRequests || statusCode == 429) {
			RecordAccountModelRateLimit(targetEmail, modelID, 2*time.Minute)
		} else if success && !isAccountDeadCCPAModel(DefaultCCPAEndpoint, targetEmail, normalizeRateLimitModelID(modelID), time.Now()) {
			clearSingleAccountModelRateLimit(targetEmail, modelID)
		}
	}
}

// ResetAccountLatencyStats clears historical latency spikes across all accounts (or a specific account).
func ResetAccountLatencyStats(accountID string) error {
	configMu.Lock()
	cfg := loadSavedAuthConfigLocked()
	var clearedEmails []string
	for i := range cfg.Accounts {
		if accountID == "" || accountID == "all" || cfg.Accounts[i].ID == accountID || strings.EqualFold(cfg.Accounts[i].Email, accountID) {
			cfg.Accounts[i].AvgLatencyMs = 0
			cfg.Accounts[i].LastLatencyMs = 0
			cfg.Accounts[i].CooldownUntil = 0
			cfg.Accounts[i].FailureCount = 0
			clearedEmails = append(clearedEmails, cfg.Accounts[i].Email)
		}
	}
	cfg.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	err := saveAuthConfigLocked(cfg)
	configMu.Unlock()

	if accountID == "" || accountID == "all" {
		ClearAccountModelRateLimits("")
	} else {
		for _, em := range clearedEmails {
			ClearAccountModelRateLimits(em)
		}
	}
	return err
}

// GetValidAccountToken returns a valid OAuth access token, project ID, and email for Antigravity,
// preferring an active connected account and falling back to saved single-account settings.
func GetValidAccountToken(ctx context.Context) (string, string, string, error) {
	return GetValidAccountTokenForModel(ctx, "")
}

// GetValidAccountTokenForModel returns a valid OAuth access token, project ID, and email for Antigravity
// while taking modelHint into account to bypass per-account model 429 cooldowns.
func GetValidAccountTokenForModel(ctx context.Context, modelHint string) (string, string, string, error) {
	acc, err := SelectProviderAccountForModel(ctx, ProviderAntigravity, modelHint)
	if err == nil && acc != nil && acc.AccessToken != "" {
		return acc.AccessToken, acc.ProjectID, acc.Email, nil
	}
	if !HasConfiguredProviderAccount(ProviderAntigravity) {
		saved := LoadSavedAuthConfig()
		if tok := strings.TrimSpace(saved.AgyOAuthToken); tok != "" {
			email := "legacy-default"
			if len(saved.Accounts) > 0 && saved.Accounts[0].Email != "" {
				email = saved.Accounts[0].Email
			}
			return tok, saved.AgyProjectID, email, nil
		}
		if envTok := strings.TrimSpace(os.Getenv("AGY_OAUTH_TOKEN")); envTok != "" {
			return envTok, strings.TrimSpace(os.Getenv("AGY_PROJECT_ID")), "env-default", nil
		}
	}
	return "", "", "", fmt.Errorf("no valid Antigravity account or token configured")
}

func hasSavedAntigravityTokenFast() bool {
	configMu.Lock()
	path := GatewayConfigFilePath()
	var cfg SavedGatewayAuth
	if cachedAuthCfg != nil && cachedAuthPath == path && (asyncFlushPending || time.Since(cachedAuthStatAt) < 500*time.Millisecond) {
		cfg = *cachedAuthCfg
	} else {
		cfg = loadSavedAuthConfigLocked()
	}
	configMu.Unlock()

	if strings.TrimSpace(cfg.AgyOAuthToken) != "" {
		return true
	}
	for _, a := range cfg.Accounts {
		if a.Disabled {
			continue
		}
		p := a.Provider
		if p == "" {
			p = ProviderAntigravity
		}
		if p == ProviderAntigravity {
			return true
		}
	}
	return false
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
	"qwen-max":              "alibaba/qwen3.8-max",
	"auto/smart":            "antigravity/claude-opus-4-6-thinking",
	"model_placeholder_m26": "antigravity/claude-opus-4-6-thinking",
	"model_placeholder_m35": "antigravity/claude-sonnet-4-6",
	"claude-mythos-5-1":     "claude-code/claude-mythos-5-1",
	"claude-mythos-5":       "claude-code/claude-mythos-5",
	"claude-fable-5-1":      "claude-code/claude-fable-5-1",
	"claude-opus-5-5":       "claude-code/claude-opus-5-5",
	"carbon":                "antigravity/gemini-4-carbon-high",
	"carbon-high":           "antigravity/gemini-4-carbon-high",
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
