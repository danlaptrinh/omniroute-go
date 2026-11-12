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
	"os"
	"strings"
	"sync"
	"time"
)

const (
	oauthExpirySafetyMarginSec     int64 = 60
	oauthProactiveRefreshWindowSec int64 = 300
)

type inflightRefresh struct {
	done chan struct{}
	acc  ConnectedAccount
	err  error
}

var (
	claudeRefreshMu sync.Mutex

	refreshFlightMu sync.Mutex
	refreshInFlight = make(map[string]*inflightRefresh)
)

// RefreshClaudeOAuthToken exchanges a Claude Code OAuth refresh_token for a fresh access_token.
func RefreshClaudeOAuthToken(ctx context.Context, acc *ConnectedAccount) error {
	return RefreshClaudeToken(ctx, acc)
}

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
		return RefreshOAuthToken(ctx, acc)
	case ProviderClaudeCode:
		return RefreshClaudeOAuthToken(ctx, acc)
	default:
		return fmt.Errorf("oauth refresh unsupported for provider %s", prov)
	}
}

func accountRefreshFlightKey(acc ConnectedAccount) string {
	prov := acc.Provider
	if prov == "" {
		prov = ProviderAntigravity
	}
	ident := strings.ToLower(strings.TrimSpace(acc.Email))
	if ident == "" {
		ident = strings.ToLower(strings.TrimSpace(acc.ID))
	}
	if ident == "" {
		ident = strings.TrimSpace(acc.RefreshToken)
	}
	return string(prov) + "|" + ident
}

func findSavedAccountMatch(prov ProviderID, acc ConnectedAccount) (ConnectedAccount, bool) {
	if prov == "" {
		prov = ProviderAntigravity
	}
	auth := LoadSavedAuthConfig()
	for _, s := range auth.Accounts {
		if s.Disabled {
			continue
		}
		sp := s.Provider
		if sp == "" {
			sp = ProviderAntigravity
		}
		if sp != prov {
			continue
		}
		if (acc.Email != "" && strings.EqualFold(s.Email, acc.Email)) || (acc.ID != "" && s.ID == acc.ID) {
			return s, true
		}
	}
	return ConnectedAccount{}, false
}

func persistAndResolveRefreshedAccount(prov ProviderID, working ConnectedAccount) ConnectedAccount {
	if persisted, upErr := UpsertConnectedAccount(working); upErr == nil {
		for _, s := range persisted.Accounts {
			sp := s.Provider
			if sp == "" {
				sp = ProviderAntigravity
			}
			if sp == prov && ((working.Email != "" && strings.EqualFold(s.Email, working.Email)) || (working.ID != "" && s.ID == working.ID)) {
				return s
			}
		}
	}
	return working
}

// refreshAccountSingleflight deduplicates concurrent OAuth token refreshes for the same account
// so a burst of requests hitting an expired token issues only a single outbound OAuth HTTP request.
func refreshAccountSingleflight(ctx context.Context, acc ConnectedAccount, force bool) (ConnectedAccount, error) {
	prov := acc.Provider
	if prov == "" {
		prov = ProviderAntigravity
	}
	if strings.TrimSpace(acc.RefreshToken) == "" {
		return acc, errors.New("missing refresh_token")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return acc, err
	}

	key := accountRefreshFlightKey(acc)
	refreshFlightMu.Lock()
	if existing, ok := refreshInFlight[key]; ok {
		refreshFlightMu.Unlock()
		select {
		case <-existing.done:
			return existing.acc, existing.err
		case <-ctx.Done():
			return acc, ctx.Err()
		}
	}
	flight := &inflightRefresh{done: make(chan struct{})}
	refreshInFlight[key] = flight
	refreshFlightMu.Unlock()

	go func(seed ConnectedAccount, targetProv ProviderID, flightKey string, fl *inflightRefresh, forceRefresh bool) {
		defer func() {
			refreshFlightMu.Lock()
			delete(refreshInFlight, flightKey)
			close(fl.done)
			refreshFlightMu.Unlock()
		}()

		if !forceRefresh {
			if saved, ok := findSavedAccountMatch(targetProv, seed); ok {
				if strings.TrimSpace(saved.AccessToken) != "" && saved.ExpiresAt > time.Now().Unix()+oauthExpirySafetyMarginSec {
					fl.acc = saved
					fl.err = nil
					return
				}
				if strings.TrimSpace(saved.RefreshToken) != "" {
					seed = saved
				}
			}
		}

		working := seed
		working.Provider = targetProv
		refreshCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()

		if err := RefreshAccountTokenByProvider(refreshCtx, &working); err != nil {
			fl.acc = seed
			fl.err = err
			return
		}
		fl.acc = persistAndResolveRefreshedAccount(targetProv, working)
		fl.err = nil
	}(acc, prov, key, flight, force)

	select {
	case <-flight.done:
		return flight.acc, flight.err
	case <-ctx.Done():
		return acc, ctx.Err()
	}
}

// triggerProactiveAccountRefresh launches a deduplicated background OAuth refresh when an account's
// current AccessToken is still valid (>60s remaining) but within 5 minutes of expiry (<=300s).
func triggerProactiveAccountRefresh(acc ConnectedAccount) bool {
	prov := acc.Provider
	if prov == "" {
		prov = ProviderAntigravity
	}
	if prov != ProviderAntigravity && prov != ProviderClaudeCode {
		return false
	}
	if strings.TrimSpace(acc.RefreshToken) == "" {
		return false
	}

	key := accountRefreshFlightKey(acc)
	refreshFlightMu.Lock()
	if _, busy := refreshInFlight[key]; busy {
		refreshFlightMu.Unlock()
		return false
	}
	flight := &inflightRefresh{done: make(chan struct{})}
	refreshInFlight[key] = flight
	refreshFlightMu.Unlock()

	go func(seed ConnectedAccount, targetProv ProviderID, flightKey string, fl *inflightRefresh) {
		defer func() {
			refreshFlightMu.Lock()
			delete(refreshInFlight, flightKey)
			close(fl.done)
			refreshFlightMu.Unlock()
		}()

		if saved, ok := findSavedAccountMatch(targetProv, seed); ok {
			if strings.TrimSpace(saved.AccessToken) != "" && saved.ExpiresAt-time.Now().Unix() > oauthProactiveRefreshWindowSec {
				fl.acc = saved
				fl.err = nil
				return
			}
			if strings.TrimSpace(saved.RefreshToken) != "" {
				seed = saved
			}
		}

		working := seed
		working.Provider = targetProv
		refreshCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()

		if err := RefreshAccountTokenByProvider(refreshCtx, &working); err != nil {
			fl.acc = seed
			fl.err = err
			return
		}
		fl.acc = persistAndResolveRefreshedAccount(targetProv, working)
		fl.err = nil
	}(acc, prov, key, flight)

	return true
}

// WaitForPendingOAuthRefreshes waits up to timeout for any in-flight OAuth token refreshes to complete.
func WaitForPendingOAuthRefreshes(timeout time.Duration) bool {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	deadline := time.Now().Add(timeout)
	for {
		refreshFlightMu.Lock()
		channels := make([]<-chan struct{}, 0, len(refreshInFlight))
		for _, fl := range refreshInFlight {
			channels = append(channels, fl.done)
		}
		refreshFlightMu.Unlock()

		if len(channels) == 0 {
			return true
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return false
		}
		timer := time.NewTimer(remaining)
		select {
		case <-channels[0]:
			timer.Stop()
		case <-timer.C:
			return false
		}
	}
}

// EnsureAccountTokenFresh serves still-valid OAuth tokens in 0ms (triggering background proactive refresh
// when within 5 minutes of expiry) and deduplicates expired refreshes via singleflight, falling back to
// an alternate candidate when refresh fails.
func EnsureAccountTokenFresh(ctx context.Context, selected ConnectedAccount, candidates []ConnectedAccount) ConnectedAccount {
	prov := selected.Provider
	if prov == "" {
		prov = ProviderAntigravity
	}
	if prov != ProviderAntigravity && prov != ProviderClaudeCode {
		return selected
	}
	if selected.ExpiresAt <= 0 {
		return selected
	}

	now := time.Now().Unix()
	if now+oauthExpirySafetyMarginSec < selected.ExpiresAt {
		if strings.TrimSpace(selected.AccessToken) != "" &&
			selected.ExpiresAt-now <= oauthProactiveRefreshWindowSec &&
			strings.TrimSpace(selected.RefreshToken) != "" {
			triggerProactiveAccountRefresh(selected)
		}
		return selected
	}

	if strings.TrimSpace(selected.RefreshToken) != "" {
		if refreshed, err := refreshAccountSingleflight(ctx, selected, false); err == nil && strings.TrimSpace(refreshed.AccessToken) != "" {
			return refreshed
		}
	}

	if len(candidates) > 1 {
		RecordAccountResult(selected.Email, prov, false, 0, 0, http.StatusUnauthorized)
		for _, alt := range candidates {
			sameEmail := selected.Email != "" && strings.EqualFold(alt.Email, selected.Email)
			sameID := selected.ID != "" && alt.ID == selected.ID
			if !sameEmail && !sameID {
				if alt.ExpiresAt > 0 {
					altNow := time.Now().Unix()
					if altNow+oauthExpirySafetyMarginSec < alt.ExpiresAt {
						if strings.TrimSpace(alt.AccessToken) != "" &&
							alt.ExpiresAt-altNow <= oauthProactiveRefreshWindowSec &&
							strings.TrimSpace(alt.RefreshToken) != "" {
							triggerProactiveAccountRefresh(alt)
						}
					} else if strings.TrimSpace(alt.RefreshToken) != "" {
						if refreshedAlt, err := refreshAccountSingleflight(ctx, alt, false); err == nil && strings.TrimSpace(refreshedAlt.AccessToken) != "" {
							alt = refreshedAlt
						}
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

	acc, err := refreshAccountSingleflight(ctx, *matched, true)
	if err != nil {
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
	configMu.Lock()
	path := GatewayConfigFilePath()
	var auth SavedGatewayAuth
	if cachedAuthCfg != nil && cachedAuthPath == path && (asyncFlushPending || time.Since(cachedAuthStatAt) < 500*time.Millisecond) {
		auth = *cachedAuthCfg
	} else {
		auth = loadSavedAuthConfigLocked()
	}
	configMu.Unlock()

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
	acc, releaseInFlight, err := AcquireProviderAccountForModel(ctx, ProviderClaudeCode, target.CanonicalID, target.CanonicalID)
	if err != nil && strings.HasPrefix(cleanAuth, "sk-ant-") {
		acc = &ConnectedAccount{Provider: ProviderClaudeCode, Email: "header-token", AccessToken: cleanAuth}
		AcquireAccountInFlightWithRole(acc.Email, target.CanonicalID)
		releaseInFlight = buildInFlightReleaseFn(acc.Email)
	} else if err != nil {
		if envTok := strings.TrimSpace(os.Getenv("CLAUDE_CODE_OAUTH_TOKEN")); envTok != "" {
			acc = &ConnectedAccount{Provider: ProviderClaudeCode, Email: "env-token", AccessToken: envTok}
			AcquireAccountInFlightWithRole(acc.Email, target.CanonicalID)
			releaseInFlight = buildInFlightReleaseFn(acc.Email)
		}
	}
	if acc == nil {
		if releaseInFlight != nil {
			releaseInFlight()
		}
		return fmt.Errorf("no Claude Code account configured")
	}

	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		if ctx.Err() != nil {
			if releaseInFlight != nil {
				releaseInFlight()
			}
			return ctx.Err()
		}
		if releaseInFlight == nil {
			AcquireAccountInFlightWithRole(acc.Email, target.CanonicalID)
			releaseInFlight = buildInFlightReleaseFn(acc.Email)
		}
		err = u.handleClaudeCodeProvider(ctx, w, req, target, *acc)
		releaseInFlight()
		releaseInFlight = nil
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

		if ctx.Err() != nil {
			return ctx.Err()
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
			nextAcc, nextRel, nextErr := AcquireProviderAccountForModel(ctx, ProviderClaudeCode, target.CanonicalID, target.CanonicalID)
			if nextErr == nil && nextAcc != nil && !strings.EqualFold(nextAcc.Email, acc.Email) {
				acc = nextAcc
				releaseInFlight = nextRel
				continue
			}
			if nextRel != nil {
				nextRel()
			}
		}

		log.Printf("[omniroute-go] claude-code target %q failed: %v; trying next provider in chain", target.CanonicalID, err)
		return err
	}

	if releaseInFlight != nil {
		releaseInFlight()
	}
	return lastErr
}

// ExecuteAntigravityWithReactiveRefresh executes an Antigravity CCPA target with reactive HTTP 401
// OAuth token refresh, atomic in-flight reservation, and model-quota-aware multi-account failover.
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

	maxAttempts := 0
	for _, a := range readAuthAccountsSnapshotFast() {
		if a.Disabled {
			continue
		}
		p := a.Provider
		if p == "" {
			p = ProviderAntigravity
		}
		if p == ProviderAntigravity {
			maxAttempts++
		}
	}
	if maxAttempts < 2 {
		maxAttempts = 2
	} else if maxAttempts > 10 {
		maxAttempts = 10
	}

	modelHint := strings.TrimSpace(target.CanonicalID)
	if modelHint == "" {
		modelHint = strings.TrimSpace(target.CCPAModelID)
	}
	if modelHint == "" {
		modelHint = strings.TrimSpace(target.RequestedID)
	}

	for attempt := 0; attempt < maxAttempts; attempt++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var agyToken, projectID, accountEmail string
		var releaseInFlight func()
		if forcedAcc != nil {
			agyToken = strings.TrimSpace(forcedAcc.AccessToken)
			projectID = strings.TrimSpace(forcedAcc.ProjectID)
			if projectID == "" {
				projectID = strings.TrimSpace(u.cfg.AgyProjectID)
			}
			accountEmail = forcedAcc.Email
			forcedAcc = nil
			if accountEmail != "" {
				AcquireAccountInFlightWithRole(accountEmail, target.CanonicalID)
				releaseInFlight = buildInFlightReleaseFn(accountEmail)
			}
		} else {
			if acc, rel, err := AcquireProviderAccountForModel(ctx, ProviderAntigravity, modelHint, target.CanonicalID); err == nil && acc != nil && strings.TrimSpace(acc.AccessToken) != "" {
				agyToken = strings.TrimSpace(acc.AccessToken)
				projectID = strings.TrimSpace(acc.ProjectID)
				if projectID == "" {
					projectID = strings.TrimSpace(u.cfg.AgyProjectID)
				}
				accountEmail = acc.Email
				releaseInFlight = rel
			} else {
				if rel != nil {
					rel()
				}
				if !HasConfiguredProviderAccount(ProviderAntigravity) {
					saved := LoadSavedAuthConfig()
					if strings.TrimSpace(saved.AgyOAuthToken) != "" {
						agyToken = strings.TrimSpace(saved.AgyOAuthToken)
						projectID = strings.TrimSpace(saved.AgyProjectID)
						accountEmail = "legacy-default"
						if len(saved.Accounts) > 0 && saved.Accounts[0].Email != "" {
							accountEmail = saved.Accounts[0].Email
						}
					} else {
						agyToken = strings.TrimSpace(u.cfg.AgyOAuthToken)
						projectID = strings.TrimSpace(u.cfg.AgyProjectID)
						if agyToken == "" {
							agyToken = strings.TrimSpace(os.Getenv("AGY_OAUTH_TOKEN"))
						}
						if projectID == "" {
							projectID = strings.TrimSpace(os.Getenv("AGY_PROJECT_ID"))
						}
					}
					if projectID == "" {
						projectID = strings.TrimSpace(u.cfg.AgyProjectID)
					}
				}
			}
		}

		if strings.HasPrefix(cleanAuth, "ya29.") {
			agyToken = cleanAuth
		}
		if agyToken == "" {
			if releaseInFlight != nil {
				releaseInFlight()
			}
			return fmt.Errorf("no Antigravity OAuth token configured")
		}

		var primaryRelOnce sync.Once
		rawPrimaryRel := releaseInFlight
		releasePrimaryOnce := func() {
			primaryRelOnce.Do(func() {
				if rawPrimaryRel != nil {
					rawPrimaryRel()
				}
			})
		}

		if accountEmail != "" {
			w.Header().Set("X-Omni-Gateway-Account", accountEmail)
			if rawPrimaryRel == nil {
				AcquireAccountInFlightWithRole(accountEmail, target.CanonicalID)
				rawPrimaryRel = buildInFlightReleaseFn(accountEmail)
			}
		}
		w.Header().Set("X-Omni-Gateway-Provider", "antigravity")
		lease := &ccpaPrimaryLease{
			accountEmail:    accountEmail,
			modelHint:       modelHint,
			releaseInFlight: releasePrimaryOnce,
		}
		ccpaCtx := withCCPAPrimaryLease(ctx, lease)
		err := u.handleDirectCCPA(ccpaCtx, w, req, target, agyToken, projectID)
		releasePrimaryOnce()
		if lease.winnerRelease != nil {
			lease.winnerRelease()
		}
		if lease.hedgedWon && lease.winnerEmail != "" {
			accountEmail = lease.winnerEmail
		}
		lat := time.Now().UnixMilli() - startMs

		if err == nil {
			usedTokens := parseRecordedTokens(w.Header().Get("X-Omni-Gateway-Tokens"), req)
			resolvedHdr := strings.TrimSpace(w.Header().Get("X-Omni-Gateway-Resolved-Model"))
			upstreamVer := strings.TrimSpace(w.Header().Get("X-Omni-Gateway-Upstream-Model-Version"))

			actualModel := target.CanonicalID
			if resolvedHdr != "" {
				actualModel = resolvedHdr
			}
			loggedModel := actualModel
			if loggedModel != "" && !strings.Contains(loggedModel, "/") {
				loggedModel = "antigravity/" + loggedModel
			}
			if upstreamVer != "" && !strings.EqualFold(upstreamVer, resolvedHdr) && !strings.EqualFold("antigravity/"+upstreamVer, loggedModel) {
				loggedModel = fmt.Sprintf("%s [%s]", loggedModel, upstreamVer)
			}

			if accountEmail != "" {
				RecordAccountModelResult(accountEmail, ProviderAntigravity, loggedModel, true, lat, usedTokens, http.StatusOK)
				nowCheck := time.Now()
				for _, bm := range []string{
					"gemini-3.8-flash-high",
					"gemini-3.8-flash-medium",
					"gemini-3.8-flash-low",
					"gemini-3.8-flash-tiered",
					"claude-opus-4-6-thinking",
					"claude-sonnet-4-6",
					"gemini-3-flash",
					"gemini-3.5-flash-lite",
					"gemini-3.1-flash-lite",
				} {
					if isAccountDeadCCPAModel(u.cfg.CCPAEndpoint, accountEmail, bm, nowCheck) {
						RecordAccountModelRateLimit(accountEmail, bm, 2*time.Minute)
					}
				}
			}
			entry := GatewayLogEntry{
				Provider:       ProviderAntigravity,
				Account:        accountEmail,
				RequestedModel: req.Model,
				ResolvedModel:  loggedModel,
				UpstreamModel:  upstreamVer,
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

		if ctx.Err() != nil {
			return ctx.Err()
		}

		errMsg := err.Error()
		if attempt == 0 && strings.Contains(errMsg, "HTTP 401") && accountEmail != "" && accountEmail != "legacy-default" {
			if refreshed, refErr := ForceRefreshAccountToken(ctx, ProviderAntigravity, accountEmail); refErr == nil && refreshed != nil {
				log.Printf("[omniroute-go] antigravity CCPA HTTP 401 on %s; force-refreshed OAuth token and retrying", accountEmail)
				forcedAcc = refreshed
				continue
			}
		}

		isFlagshipFailover := strings.Contains(errMsg, "[flagship-failover]")
		failCode := http.StatusBadGateway
		if strings.Contains(errMsg, "HTTP 401") {
			failCode = http.StatusUnauthorized
		} else if strings.Contains(errMsg, "HTTP 403") {
			failCode = http.StatusForbidden
		} else if strings.Contains(errMsg, "HTTP 429") || isFlagshipFailover {
			failCode = http.StatusTooManyRequests
		}

		if isFlagshipFailover && accountEmail != "" && attempt+1 < maxAttempts {
			if modelHint != "" {
				RecordAccountModelRateLimit(accountEmail, modelHint, 2*time.Minute)
			}
			lastErr = err
			log.Printf("[omniroute-go] antigravity flagship quota exhausted on %s for %q; failing over to alternate flagship account (attempt %d/%d)",
				accountEmail, modelHint, attempt+1, maxAttempts)
			continue
		}

		if accountEmail != "" {
			if failCode == http.StatusTooManyRequests && modelHint != "" {
				RecordAccountModelRateLimit(accountEmail, modelHint, 2*time.Minute)
			}
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
		if failCode != http.StatusUnauthorized && failCode != http.StatusForbidden && failCode != http.StatusTooManyRequests {
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
		if refreshed, err := refreshAccountSingleflight(ctx, *acc, false); err == nil {
			*acc = refreshed
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
		if refreshed, refErr := refreshAccountSingleflight(ctx, *acc, true); refErr == nil {
			*acc = refreshed
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

// SyncAccountValidationState isolates an Antigravity account with a 2-hour cooldown when Google
// reports VALIDATION_REQUIRED, or immediately clears a long validation cooldown once verified.
func SyncAccountValidationState(emailOrID string, requiresValidation bool) {
	target := strings.TrimSpace(emailOrID)
	if target == "" {
		return
	}

	configMu.Lock()
	defer configMu.Unlock()
	cfg := loadSavedAuthConfigLocked()
	now := time.Now().Unix()

	for i := range cfg.Accounts {
		acc := &cfg.Accounts[i]
		prov := acc.Provider
		if prov == "" {
			prov = ProviderAntigravity
		}
		if prov != ProviderAntigravity {
			continue
		}
		if acc.ID == target || strings.EqualFold(acc.Email, target) {
			if requiresValidation {
				if acc.CooldownUntil < now+1800 {
					acc.CooldownUntil = now + 7200
					acc.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
					cfg.UpdatedAt = acc.UpdatedAt
					saveAuthConfigAsyncLocked(cfg)
				}
			} else {
				if acc.CooldownUntil > now+600 {
					acc.CooldownUntil = 0
					acc.FailureCount = 0
					acc.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
					cfg.UpdatedAt = acc.UpdatedAt
					asyncFlushPending = false
					_ = saveAuthConfigLocked(cfg)
				}
			}
			return
		}
	}
}
