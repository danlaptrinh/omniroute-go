package gateway

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

var claudeRefreshMu sync.Mutex

// RefreshClaudeToken exchanges a Claude Code OAuth refresh_token for a fresh access_token.
// Serialized via claudeRefreshMu because Anthropic refresh tokens are strictly single-use rotating.
func RefreshClaudeToken(ctx context.Context, acc *ConnectedAccount) error {
	if acc == nil {
		return errors.New("nil account")
	}
	claudeRefreshMu.Lock()
	defer claudeRefreshMu.Unlock()

	refreshTok := strings.TrimSpace(acc.RefreshToken)
	if refreshTok == "" {
		return errors.New("missing claude refresh_token")
	}
	if strings.TrimSpace(ClaudeTokenEndpoint) == "" {
		return errors.New("claude token endpoint is empty")
	}

	// Re-check persisted config in case a concurrent goroutine just rotated this single-use refresh_token.
	savedCfg := LoadSavedAuthConfig()
	for _, s := range savedCfg.Accounts {
		if s.Provider == ProviderClaudeCode && ((acc.Email != "" && strings.EqualFold(s.Email, acc.Email)) || (acc.ID != "" && s.ID == acc.ID)) {
			if st := strings.TrimSpace(s.RefreshToken); st != "" && st != refreshTok && strings.TrimSpace(s.AccessToken) != "" && s.ExpiresAt > time.Now().Unix()+60 {
				acc.AccessToken = strings.TrimSpace(s.AccessToken)
				acc.RefreshToken = st
				acc.ExpiresAt = s.ExpiresAt
				acc.UpdatedAt = s.UpdatedAt
				return nil
			}
			if st := strings.TrimSpace(s.RefreshToken); st != "" {
				refreshTok = st
			}
			break
		}
	}

	payload, err := json.Marshal(map[string]string{
		"grant_type":    "refresh_token",
		"client_id":     ClaudeOAuthClientID,
		"refresh_token": refreshTok,
	})
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ClaudeTokenEndpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "claude-code/2.1.0")

	resp, err := oauthHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("claude token refresh HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var tr struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
		Error        string `json:"error"`
		ErrorDesc    string `json:"error_description"`
	}
	if err := json.Unmarshal(body, &tr); err != nil {
		return fmt.Errorf("failed to parse claude refresh response: %w", err)
	}
	if strings.TrimSpace(tr.AccessToken) == "" {
		return fmt.Errorf("no access_token in claude refresh response: %s (%s)", tr.Error, tr.ErrorDesc)
	}

	acc.AccessToken = strings.TrimSpace(tr.AccessToken)
	if strings.TrimSpace(tr.RefreshToken) != "" {
		acc.RefreshToken = strings.TrimSpace(tr.RefreshToken)
	}
	if tr.ExpiresIn > 0 {
		acc.ExpiresAt = time.Now().Unix() + tr.ExpiresIn
	}
	acc.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	_, _ = UpsertConnectedAccount(*acc)
	return nil
}

// RefreshAccountTokenByProvider refreshes the OAuth token of acc based on its provider.
func RefreshAccountTokenByProvider(ctx context.Context, acc *ConnectedAccount) error {
	if acc == nil {
		return errors.New("nil account")
	}
	prov := acc.Provider
	if prov == "" {
		prov = ProviderAntigravity
	}
	switch prov {
	case ProviderAntigravity:
		return RefreshAntigravityToken(ctx, acc)
	case ProviderClaudeCode:
		return RefreshClaudeToken(ctx, acc)
	default:
		return fmt.Errorf("oauth refresh unsupported for provider %s", prov)
	}
}

// EnsureAccountTokenFresh proactively refreshes expired/near-expiry OAuth tokens for Antigravity
// and Claude Code accounts, falling back to an alternate candidate when refresh fails.
func EnsureAccountTokenFresh(ctx context.Context, selected ConnectedAccount, candidates []ConnectedAccount) ConnectedAccount {
	prov := selected.Provider
	if prov == "" {
		prov = ProviderAntigravity
	}
	if prov != ProviderAntigravity && prov != ProviderClaudeCode {
		return selected
	}
	if selected.ExpiresAt <= 0 || selected.ExpiresAt > time.Now().Unix()+60 {
		return selected
	}

	if strings.TrimSpace(selected.RefreshToken) != "" {
		if err := RefreshAccountTokenByProvider(ctx, &selected); err == nil {
			_, _ = UpsertConnectedAccount(selected)
			return selected
		}
	}

	if len(candidates) > 1 {
		RecordAccountResult(selected.Email, prov, false, 0, 0, http.StatusUnauthorized)
		for _, alt := range candidates {
			sameEmail := selected.Email != "" && strings.EqualFold(alt.Email, selected.Email)
			sameID := selected.ID != "" && alt.ID == selected.ID
			if !sameEmail && !sameID {
				if alt.ExpiresAt > 0 && alt.ExpiresAt <= time.Now().Unix()+60 && strings.TrimSpace(alt.RefreshToken) != "" {
					if err := RefreshAccountTokenByProvider(ctx, &alt); err == nil {
						_, _ = UpsertConnectedAccount(alt)
					}
				}
				return alt
			}
		}
	}

	return selected
}

// ForceRefreshAccountToken forces an immediate OAuth token refresh for a specific account
// (even when ExpiresAt is still in the future, e.g., after an upstream HTTP 401) and clears cooldown.
func ForceRefreshAccountToken(ctx context.Context, provider ProviderID, emailOrID string) (*ConnectedAccount, error) {
	if provider == "" {
		provider = ProviderAntigravity
	}
	targetKey := strings.TrimSpace(emailOrID)
	if targetKey == "" {
		return nil, errors.New("empty account identifier")
	}

	auth := LoadSavedAuthConfig()
	var matched *ConnectedAccount
	for i := range auth.Accounts {
		a := auth.Accounts[i]
		if a.Disabled {
			continue
		}
		p := a.Provider
		if p == "" {
			p = ProviderAntigravity
		}
		if p != provider {
			continue
		}
		if strings.EqualFold(a.Email, targetKey) || a.ID == targetKey {
			cp := a
			matched = &cp
			break
		}
	}
	if matched == nil {
		return nil, fmt.Errorf("enabled account %q not found for provider %s", targetKey, provider)
	}
	if strings.TrimSpace(matched.RefreshToken) == "" {
		return nil, fmt.Errorf("account %q has no refresh_token", targetKey)
	}

	acc := *matched
	if err := RefreshAccountTokenByProvider(ctx, &acc); err != nil {
		return nil, err
	}

	acc.CooldownUntil = 0
	acc.FailureCount = 0
	_, upsertErr := UpsertConnectedAccount(acc)
	if upsertErr != nil {
		return &acc, nil
	}

	// Ensure CooldownUntil and FailureCount are explicitly cleared in persisted state
	// (UpsertConnectedAccount preserves non-zero existing cooldown when incoming value is 0).
	configMu.Lock()
	cfg := loadSavedAuthConfigLocked()
	var saved *ConnectedAccount
	for i := range cfg.Accounts {
		a := &cfg.Accounts[i]
		p := a.Provider
		if p == "" {
			p = ProviderAntigravity
		}
		if p == provider && (strings.EqualFold(a.Email, acc.Email) || (acc.ID != "" && a.ID == acc.ID)) {
			a.CooldownUntil = 0
			a.FailureCount = 0
			cp := *a
			saved = &cp
			break
		}
	}
	if saved != nil {
		_ = saveAuthConfigLocked(cfg)
	}
	configMu.Unlock()

	if saved != nil {
		return saved, nil
	}
	return &acc, nil
}

// HasConfiguredProviderAccount reports whether at least one enabled account is configured for provider.
func HasConfiguredProviderAccount(provider ProviderID) bool {
	if provider == "" {
		provider = ProviderAntigravity
	}
	auth := LoadSavedAuthConfig()
	for _, a := range auth.Accounts {
		if a.Disabled {
			continue
		}
		p := a.Provider
		if p == "" {
			p = ProviderAntigravity
		}
		if p == provider {
			return true
		}
	}
	return false
}

// ExecuteClaudeWithReactiveRefresh executes a Claude Code target with automatic reactive HTTP 401
// OAuth token refresh and multi-account failover on 401/429.
func ExecuteClaudeWithReactiveRefresh(
	ctx context.Context,
	u *GatewayUpstream,
	w http.ResponseWriter,
	req OpenAIChatRequest,
	target ResolvedModel,
	cleanAuth string,
	startMs int64,
) error {
	acc, err := SelectProviderAccount(ctx, ProviderClaudeCode)
	if err != nil && strings.HasPrefix(cleanAuth, "sk-ant-") {
		acc = &ConnectedAccount{Provider: ProviderClaudeCode, Email: "header-token", AccessToken: cleanAuth}
	}
	if acc == nil {
		return fmt.Errorf("no Claude Code account configured")
	}

	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		AcquireAccountInFlightWithRole(acc.Email, target.CanonicalID)
		err = u.handleClaudeCodeProvider(ctx, w, req, target, *acc)
		ReleaseAccountInFlight(acc.Email)
		lat := time.Now().UnixMilli() - startMs

		if err == nil {
			usedTokens := parseRecordedTokens(w.Header().Get("X-Omni-Gateway-Tokens"), req)
			RecordAccountModelResult(acc.Email, ProviderClaudeCode, target.CanonicalID, true, lat, usedTokens, http.StatusOK)
			entry := GatewayLogEntry{
				Provider:       ProviderClaudeCode,
				Account:        acc.Email,
				RequestedModel: req.Model,
				ResolvedModel:  target.CanonicalID,
				StatusCode:     http.StatusOK,
				LatencyMs:      lat,
				TotalTokens:    usedTokens,
				IsError:        false,
				FallbackChain:  target.FallbackChain,
			}
			EnrichLogEntryFromContext(ctx, &entry, req)
			RecordLogEntry(entry)
			return nil
		}

		lastErr = err
		errMsg := err.Error()
		if attempt == 0 && strings.Contains(errMsg, "HTTP 401") && acc.Email != "" && acc.Email != "header-token" {
			if refreshed, refErr := ForceRefreshAccountToken(ctx, ProviderClaudeCode, acc.Email); refErr == nil && refreshed != nil {
				log.Printf("[omniroute-go] claude-code HTTP 401 on %s; force-refreshed OAuth token and retrying", acc.Email)
				acc = refreshed
				continue
			}
		}

		failCode := http.StatusBadGateway
		if strings.Contains(errMsg, "HTTP 401") || strings.Contains(errMsg, "HTTP 403") {
			failCode = http.StatusUnauthorized
		} else if strings.Contains(errMsg, "HTTP 429") {
			failCode = http.StatusTooManyRequests
		}

		RecordAccountModelResult(acc.Email, ProviderClaudeCode, target.CanonicalID, false, lat, 0, failCode)
		errEntry := GatewayLogEntry{
			Provider:       ProviderClaudeCode,
			Account:        acc.Email,
			RequestedModel: req.Model,
			ResolvedModel:  target.CanonicalID,
			StatusCode:     failCode,
			LatencyMs:      lat,
			IsError:        true,
			ErrorMessage:   errMsg,
			FallbackChain:  target.FallbackChain,
		}
		EnrichLogEntryFromContext(ctx, &errEntry, req)
		RecordLogEntry(errEntry)

		if attempt == 0 && (failCode == http.StatusUnauthorized || failCode == http.StatusTooManyRequests) {
			if nextAcc, nextErr := SelectProviderAccount(ctx, ProviderClaudeCode); nextErr == nil && nextAcc != nil && !strings.EqualFold(nextAcc.Email, acc.Email) {
				acc = nextAcc
				continue
			}
		}

		log.Printf("[omniroute-go] claude-code target %q failed: %v; trying next provider in chain", target.CanonicalID, err)
		return err
	}

	return lastErr
}

// ExecuteAntigravityWithReactiveRefresh executes an Antigravity CCPA target with reactive HTTP 401
// OAuth token refresh and multi-account failover.
func ExecuteAntigravityWithReactiveRefresh(
	ctx context.Context,
	u *GatewayUpstream,
	w http.ResponseWriter,
	req OpenAIChatRequest,
	target ResolvedModel,
	cleanAuth string,
	startMs int64,
) error {
	var lastErr error
	var forcedAcc *ConnectedAccount

	for attempt := 0; attempt < 2; attempt++ {
		var agyToken, projectID, accountEmail string
		if forcedAcc != nil {
			agyToken = strings.TrimSpace(forcedAcc.AccessToken)
			projectID = strings.TrimSpace(forcedAcc.ProjectID)
			if projectID == "" {
				projectID = strings.TrimSpace(u.cfg.AgyProjectID)
			}
			accountEmail = forcedAcc.Email
			forcedAcc = nil
		} else {
			if tok, proj, email, err := GetValidAccountToken(ctx); err == nil && tok != "" {
				agyToken = tok
				projectID = proj
				if projectID == "" {
					projectID = strings.TrimSpace(u.cfg.AgyProjectID)
				}
				accountEmail = email
			} else if !HasConfiguredProviderAccount(ProviderAntigravity) {
				agyToken = strings.TrimSpace(u.cfg.AgyOAuthToken)
				projectID = strings.TrimSpace(u.cfg.AgyProjectID)
			}
		}

		if strings.HasPrefix(cleanAuth, "ya29.") {
			agyToken = cleanAuth
		}
		if agyToken == "" {
			return fmt.Errorf("no Antigravity OAuth token configured")
		}

		if accountEmail != "" {
			w.Header().Set("X-Omni-Gateway-Account", accountEmail)
			AcquireAccountInFlightWithRole(accountEmail, target.CanonicalID)
		}
		w.Header().Set("X-Omni-Gateway-Provider", "antigravity")
		err := u.handleDirectCCPA(ctx, w, req, target, agyToken, projectID)
		if accountEmail != "" {
			ReleaseAccountInFlight(accountEmail)
		}
		lat := time.Now().UnixMilli() - startMs

		if err == nil {
			usedTokens := parseRecordedTokens(w.Header().Get("X-Omni-Gateway-Tokens"), req)
			if accountEmail != "" {
				RecordAccountModelResult(accountEmail, ProviderAntigravity, target.CanonicalID, true, lat, usedTokens, http.StatusOK)
			}
			entry := GatewayLogEntry{
				Provider:       ProviderAntigravity,
				Account:        accountEmail,
				RequestedModel: req.Model,
				ResolvedModel:  target.CanonicalID,
				StatusCode:     http.StatusOK,
				LatencyMs:      lat,
				TotalTokens:    usedTokens,
				IsError:        false,
				FallbackChain:  target.FallbackChain,
			}
			EnrichLogEntryFromContext(ctx, &entry, req)
			RecordLogEntry(entry)
			return nil
		}

		errMsg := err.Error()
		if attempt == 0 && strings.Contains(errMsg, "HTTP 401") && accountEmail != "" && accountEmail != "legacy-default" {
			if refreshed, refErr := ForceRefreshAccountToken(ctx, ProviderAntigravity, accountEmail); refErr == nil && refreshed != nil {
				log.Printf("[omniroute-go] antigravity CCPA HTTP 401 on %s; force-refreshed OAuth token and retrying", accountEmail)
				forcedAcc = refreshed
				continue
			}
		}

		failCode := http.StatusBadGateway
		if strings.Contains(errMsg, "HTTP 401") || strings.Contains(errMsg, "HTTP 403") {
			failCode = http.StatusUnauthorized
		} else if strings.Contains(errMsg, "HTTP 429") {
			failCode = http.StatusTooManyRequests
		}

		if accountEmail != "" {
			RecordAccountModelResult(accountEmail, ProviderAntigravity, target.CanonicalID, false, lat, 0, failCode)
		}
		errEntry := GatewayLogEntry{
			Provider:       ProviderAntigravity,
			Account:        accountEmail,
			RequestedModel: req.Model,
			ResolvedModel:  target.CanonicalID,
			StatusCode:     failCode,
			LatencyMs:      lat,
			IsError:        true,
			ErrorMessage:   errMsg,
			FallbackChain:  target.FallbackChain,
		}
		EnrichLogEntryFromContext(ctx, &errEntry, req)
		RecordLogEntry(errEntry)
		lastErr = err
		log.Printf("[omniroute-go] antigravity CCPA attempt %d for %q (%s, acc=%s) failed: %v", attempt+1, target.RequestedID, target.CCPAModelID, accountEmail, err)
		if failCode != http.StatusUnauthorized && failCode != http.StatusTooManyRequests {
			break
		}
	}

	return lastErr
}

// FetchClaudeUsageWithRefresh fetches Claude OAuth live quota usage, proactively refreshing
// near-expiry tokens and retrying once on HTTP 401 when a refresh_token is available.
func FetchClaudeUsageWithRefresh(ctx context.Context, acc *ConnectedAccount) (*http.Response, error) {
	if ctx == nil || acc == nil || strings.TrimSpace(acc.AccessToken) == "" || ClaudeOAuthUsageEndpoint == "" {
		return nil, nil
	}

	if strings.TrimSpace(acc.RefreshToken) != "" && acc.ExpiresAt > 0 && time.Now().Unix() >= acc.ExpiresAt-60 {
		if err := RefreshClaudeToken(ctx, acc); err == nil {
			_, _ = UpsertConnectedAccount(*acc)
		}
	}

	client := &http.Client{
		Transport: SharedHTTPTransport,
		Timeout:   3500 * time.Millisecond,
	}

	doGet := func() (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, ClaudeOAuthUsageEndpoint, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(acc.AccessToken))
		req.Header.Set("anthropic-beta", "oauth-2025-04-20")
		req.Header.Set("User-Agent", "claude-code/2.1.0")
		req.Header.Set("Accept", "application/json")
		return client.Do(req)
	}

	resp, err := doGet()
	if err != nil {
		return nil, err
	}

	if resp != nil && resp.StatusCode == http.StatusUnauthorized && strings.TrimSpace(acc.RefreshToken) != "" {
		_ = resp.Body.Close()
		if refErr := RefreshClaudeToken(ctx, acc); refErr == nil {
			_, _ = UpsertConnectedAccount(*acc)
			return doGet()
		}
		resp.Body = io.NopCloser(bytes.NewReader(nil))
	}

	return resp, nil
}

// IsPoisonedVectorEntry returns true if promptText or answerText contains upstream client-deprecation
// banners or CCPA rejection error strings that must never pollute Vector DB RAG memory.
func IsPoisonedVectorEntry(promptText, answerText string) bool {
	if IsUnsupportedClientBanner(promptText) || IsUnsupportedClientBanner(answerText) {
		return true
	}
	lowerPrompt := strings.ToLower(strings.TrimSpace(promptText))
	lowerAns := strings.ToLower(strings.TrimSpace(answerText))
	if strings.Contains(lowerPrompt, "ccpa client version rejected by upstream") ||
		strings.Contains(lowerAns, "ccpa client version rejected by upstream") {
		return true
	}
	return false
}

// FilterPoisonedVectorMemories removes poisoned entries from items and returns the sanitized slice
// along with the number of removed entries.
func FilterPoisonedVectorMemories(items []VectorMemoryEntry) ([]VectorMemoryEntry, int) {
	if len(items) == 0 {
		return items, 0
	}
	cleaned := make([]VectorMemoryEntry, 0, len(items))
	removed := 0
	for _, m := range items {
		if IsPoisonedVectorEntry(m.PromptText, m.AnswerText) || IsUnsupportedClientBanner(m.Title) {
			removed++
			continue
		}
		cleaned = append(cleaned, m)
	}
	return cleaned, removed
}

// PurgePoisonedPostgresVectors deletes any previously persisted CCPA deprecation-banner rows from PostgreSQL.
func PurgePoisonedPostgresVectors(ctx context.Context, db *sql.DB) {
	if db == nil {
		return
	}
	if ctx == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
	}
	res, err := db.ExecContext(ctx, `
		DELETE FROM omni_vector_memories
		WHERE LOWER(answer_text) LIKE '%this version of antigravity is no longer supported%'
		   OR LOWER(prompt_text) LIKE '%this version of antigravity is no longer supported%'
		   OR LOWER(title) LIKE '%this version of antigravity is no longer supported%'
	`)
	if err == nil && res != nil {
		if n, rErr := res.RowsAffected(); rErr == nil && n > 0 {
			log.Printf("[omniroute-vectordb] purged %d poisoned deprecation-banner vector memories from PostgreSQL", n)
		}
	}
}
