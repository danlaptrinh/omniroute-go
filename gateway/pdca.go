package gateway

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	DefaultPDCAWatchdogInterval = 60 * time.Second
	MaxPDCACycleHistory         = 50
	MaxPDCAActionHistory        = 100
	MaxPDCAAnomalyHistory       = 100
	PDCATransientCooldownCap    = 5 * time.Minute
)

type PDCAAction struct {
	ID        string     `json:"id"`
	CycleID   string     `json:"cycle_id"`
	Timestamp string     `json:"timestamp"`
	Type      string     `json:"type"` // "heal_stale_cooldown", "heal_expired_model_cache", "heal_ultra_working_model_pin", "heal_cross_provider_cooldown"
	Provider  ProviderID `json:"provider,omitempty"`
	Account   string     `json:"account,omitempty"`
	Target    string     `json:"target,omitempty"`
	Detail    string     `json:"detail"`
}

type PDCAAnomaly struct {
	ID        string     `json:"id"`
	CycleID   string     `json:"cycle_id"`
	Timestamp string     `json:"timestamp"`
	Type      string     `json:"type"` // "stale_account_cooldown", "expired_model_cache", "ultra_tier_routing_leak", "provider_auth_upstream_spike", "cross_provider_cooldown_contamination"
	Severity  string     `json:"severity"` // "info", "warning", "critical"
	Provider  ProviderID `json:"provider,omitempty"`
	Account   string     `json:"account,omitempty"`
	Model     string     `json:"model,omitempty"`
	Detail    string     `json:"detail"`
}

type PDCAAlert struct {
	ID                     string     `json:"id"`
	Severity               string     `json:"severity"` // "warning", "critical"
	Category               string     `json:"category"` // "auth_revocation_or_forbidden", "upstream_model_not_found", "cross_provider_contamination"
	Provider               ProviderID `json:"provider,omitempty"`
	Account                string     `json:"account,omitempty"`
	StatusCodes            []int      `json:"status_codes,omitempty"`
	FailureCount           int        `json:"failure_count"`
	CrossProviderIsolated  bool       `json:"cross_provider_isolated"`
	Summary                string     `json:"summary"`
	RecommendedRemediation string     `json:"recommended_remediation"`
	DetectedAt             string     `json:"detected_at"`
}

type PDCACycleReport struct {
	CycleID                        string        `json:"cycle_id"`
	StartedAt                      string        `json:"started_at"`
	DurationMs                     int64         `json:"duration_ms"`
	Status                         string        `json:"status"` // "healthy", "healed", "alert"
	InspectedAccounts              int           `json:"inspected_accounts"`
	InspectedLogs                  int           `json:"inspected_logs"`
	PrunedDeadModelCacheEntries    int           `json:"pruned_dead_model_cache_entries"`
	PrunedAccountRateLimitEntries  int           `json:"pruned_account_rate_limit_entries"`
	EvictedUltraNonFlagshipPins    int           `json:"evicted_ultra_non_flagship_pins"`
	HealedStaleCooldowns           int           `json:"healed_stale_cooldowns"`
	CrossProviderIsolationVerified bool          `json:"cross_provider_isolation_verified"`
	Anomalies                      []PDCAAnomaly `json:"anomalies"`
	Actions                        []PDCAAction  `json:"actions"`
	Alerts                         []PDCAAlert   `json:"alerts"`
}

type PDCAStatusSnapshot struct {
	Status                         string            `json:"status"`
	WatchdogRunning                bool              `json:"watchdog_running"`
	WatchdogIntervalSec            int64             `json:"watchdog_interval_sec"`
	LastCycleAt                    string            `json:"last_cycle_at,omitempty"`
	TotalCycles                    int64             `json:"total_cycles"`
	TotalAnomalies                 int64             `json:"total_anomalies"`
	TotalActions                   int64             `json:"total_actions"`
	CrossProviderIsolationVerified bool              `json:"cross_provider_isolation_verified"`
	Alerts                         []PDCAAlert       `json:"alerts"`
	RecentActions                  []PDCAAction      `json:"recent_actions"`
	RecentAnomalies                []PDCAAnomaly     `json:"recent_anomalies"`
	RecentCycles                   []PDCACycleReport `json:"recent_cycles"`
}

var (
	pdcaMu              sync.RWMutex
	pdcaWatchdogRunning atomic.Int64
	pdcaIntervalSec     atomic.Int64
	pdcaTotalCycles     int64
	pdcaTotalAnomalies  int64
	pdcaTotalActions    int64
	pdcaLastCycleAt     string
	pdcaLastVerified    bool = true

	pdcaRecentCycles    = make([]PDCACycleReport, 0, MaxPDCACycleHistory)
	pdcaRecentActions   = make([]PDCAAction, 0, MaxPDCAActionHistory)
	pdcaRecentAnomalies = make([]PDCAAnomaly, 0, MaxPDCAAnomalyHistory)
	pdcaActiveAlerts    = make([]PDCAAlert, 0, 32)
)

func generatePDCAID(prefix string) string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%s-%d-%s", prefix, time.Now().UnixNano()%1000000, hex.EncodeToString(b))
}

// ResetPDCAState resets PDCA bounded history, alerts, and counters cleanly for testing.
func ResetPDCAState() {
	pdcaMu.Lock()
	defer pdcaMu.Unlock()

	pdcaTotalCycles = 0
	pdcaTotalAnomalies = 0
	pdcaTotalActions = 0
	pdcaLastCycleAt = ""
	pdcaLastVerified = true
	pdcaRecentCycles = make([]PDCACycleReport, 0, MaxPDCACycleHistory)
	pdcaRecentActions = make([]PDCAAction, 0, MaxPDCAActionHistory)
	pdcaRecentAnomalies = make([]PDCAAnomaly, 0, MaxPDCAAnomalyHistory)
	pdcaActiveAlerts = make([]PDCAAlert, 0, 32)
}

// StartPDCAWatchdog initializes and runs the background PDCA cycle at the requested interval.
func StartPDCAWatchdog(ctx context.Context, interval time.Duration) {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return
	}
	if interval <= 0 {
		interval = DefaultPDCAWatchdogInterval
	}

	pdcaIntervalSec.Store(int64(interval / time.Second))
	pdcaWatchdogRunning.Add(1)

	// Execute immediate initial cycle synchronously so callers observe TotalCycles >= 1
	RunPDCACycle(time.Now().UTC())

	go func() {
		defer pdcaWatchdogRunning.Add(-1)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case tick := <-ticker.C:
				RunPDCACycle(tick.UTC())
			}
		}
	}()
}

func sanitizePDCAModelToken(modelID string) string {
	m := strings.ToLower(strings.TrimSpace(modelID))
	if idx := strings.Index(m, "➔"); idx >= 0 {
		m = strings.TrimSpace(m[idx+len("➔"):])
	}
	if idx := strings.Index(m, "["); idx > 0 {
		m = strings.TrimSpace(m[:idx])
	}
	if idx := strings.Index(m, "("); idx > 0 {
		m = strings.TrimSpace(m[:idx])
	}
	return stripKnownModelPrefixes(m)
}

func isFlagshipCCPAModel(modelID string) bool {
	m := sanitizePDCAModelToken(modelID)
	if m == "" || strings.HasSuffix(m, "-low") || strings.HasSuffix(m, ":low") || strings.Contains(m, "lite") {
		return false
	}
	if strings.HasPrefix(m, "gemini-4-carbon") || strings.HasPrefix(m, "carbon") ||
		strings.HasPrefix(m, "gemini-4-argon") || strings.HasPrefix(m, "argon") ||
		strings.HasPrefix(m, "claude-opus") || strings.HasPrefix(m, "claude-sonnet") {
		return true
	}
	return false
}

func isAllowedUltraPinForCanonical(canonicalID, pinnedModel string) bool {
	if isFlagshipCCPAModel(pinnedModel) {
		return true
	}
	canon := sanitizePDCAModelToken(canonicalID)
	pin := sanitizePDCAModelToken(pinnedModel)
	// Per architectural decision #65: when Carbon/Argon is requested on Ultra and upstream 404s,
	// Ultra's unthrottled gemini-3.8-flash-high / -medium backend is permitted ONLY for Carbon/Argon canonicals.
	if (strings.HasPrefix(canon, "gemini-4-carbon") || strings.HasPrefix(canon, "carbon") ||
		strings.HasPrefix(canon, "gemini-4-argon") || strings.HasPrefix(canon, "argon")) &&
		(pin == "gemini-3.8-flash-high" || pin == "gemini-3.8-flash-medium") {
		return true
	}
	return false
}

func isLowerTierGeminiModel(modelID string) bool {
	m := sanitizePDCAModelToken(modelID)
	if m == "" {
		return false
	}
	if strings.HasPrefix(m, "gemini-3.1-pro") ||
		strings.HasPrefix(m, "gemini-3-flash") ||
		strings.HasPrefix(m, "gemini-3.5-flash") ||
		strings.HasPrefix(m, "gemini-2.5") ||
		strings.HasSuffix(m, "-low") ||
		strings.Contains(m, "lite") {
		return true
	}
	return false
}

func isDirectFlash38LeakOnUltra(requestedModel, resolvedOrUpstream string) bool {
	req := sanitizePDCAModelToken(requestedModel)
	isCarbonArgonReq := strings.HasPrefix(req, "gemini-4-carbon") || strings.HasPrefix(req, "carbon") ||
		strings.HasPrefix(req, "gemini-4-argon") || strings.HasPrefix(req, "argon")
	if isCarbonArgonReq {
		return false
	}
	if strings.HasPrefix(req, "gemini-3.8-flash") {
		return true
	}
	res := sanitizePDCAModelToken(resolvedOrUpstream)
	return strings.HasPrefix(res, "gemini-3.8-flash")
}

// RunPDCACycle runs all Plan-Do-Check-Act health checkers and self-healers thread-safely.
func RunPDCACycle(now time.Time) PDCACycleReport {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	start := time.Now()

	cycleID := generatePDCAID("cycle")
	report := PDCACycleReport{
		CycleID:                        cycleID,
		StartedAt:                      now.Format(time.RFC3339),
		CrossProviderIsolationVerified: true,
		Anomalies:                      []PDCAAnomaly{},
		Actions:                        []PDCAAction{},
		Alerts:                         []PDCAAlert{},
	}

	auth := LoadSavedAuthConfig()
	report.InspectedAccounts = len(auth.Accounts)

	recentLogs := GetRecentLogs(250, "", false)
	report.InspectedLogs = len(recentLogs)

	quotaCacheMu.RLock()
	cs := cachedSummary
	quotaCacheMu.RUnlock()

	nowUnix := now.Unix()
	nowRFC := now.Format(time.RFC3339)

	// -------------------------------------------------------------
	// 1. Plan-Do-Check-Act: Stale Cooldown Auto-Healer
	// -------------------------------------------------------------
	var accountsToHeal []string
	for _, acc := range auth.Accounts {
		if acc.Disabled {
			continue
		}
		prov := acc.Provider
		if prov == "" {
			prov = ProviderAntigravity
		}
		if prov != ProviderAntigravity && prov != ProviderClaudeCode {
			continue
		}
		if strings.TrimSpace(acc.APIKey) != "" {
			continue
		}

		if acc.CooldownUntil <= nowUnix {
			continue
		}

		// Check if quota has already reset
		quotaAlreadyReset := false
		if strings.TrimSpace(acc.QuotaResetTime) != "" {
			if rst, err := time.Parse(time.RFC3339, strings.TrimSpace(acc.QuotaResetTime)); err == nil {
				if !rst.After(now) {
					quotaAlreadyReset = true
				}
			}
		}

		// Check quota exhaustion
		quotaExhausted := false
		if !quotaAlreadyReset {
			if acc.QuotaUsedPercent != nil && *acc.QuotaUsedPercent >= 100.0 {
				quotaExhausted = true
			} else if prov == ProviderAntigravity && acc.WeeklyGeminiPercent != nil && *acc.WeeklyGeminiPercent <= 0 &&
				acc.WeeklyClaudePercent != nil && *acc.WeeklyClaudePercent <= 0 {
				quotaExhausted = true
			}
		}

		if quotaExhausted {
			continue
		}

		// Check active validation or 401/403 ban state
		hasActiveValidation := false
		acctScope := strings.ToLower(strings.TrimSpace(acc.Email))
		if acctScope != "" {
			if _, ok := validationIsolatedAccounts.Load(acctScope); ok {
				hasActiveValidation = true
			}
		}
		acctIDLower := strings.ToLower(strings.TrimSpace(acc.ID))
		if acctIDLower != "" {
			if _, ok := validationIsolatedAccounts.Load(acctIDLower); ok {
				hasActiveValidation = true
			}
		}

		if cs != nil {
			for _, d := range cs.Accounts {
				if (acc.ID != "" && d.ID == acc.ID) || (acc.Email != "" && strings.EqualFold(d.Email, acc.Email)) {
					if d.ValidationURL != "" || strings.Contains(d.ErrorMessage, "TOS_VIOLATION") ||
						strings.Contains(d.ErrorMessage, "VALIDATION_REQUIRED") ||
						strings.Contains(d.ErrorMessage, "invalid_grant") {
						hasActiveValidation = true
					}
					break
				}
			}
		}

		if hasActiveValidation {
			continue
		}

		// Check recent logs for unrecovered 401/403 within the last 30 minutes
		hasUnrecoveredAuthErr := false
		cutoff := now.Add(-30 * time.Minute)
		for _, entry := range recentLogs {
			if (entry.Account == acc.Email || entry.Account == acc.ID) && (entry.Provider == prov || string(entry.Provider) == "") {
				if entryTime, err := time.Parse(time.RFC3339, entry.Timestamp); err == nil && entryTime.After(cutoff) {
					if entry.StatusCode == 401 || entry.StatusCode == 403 {
						hasUnrecoveredAuthErr = true
					} else if entry.StatusCode >= 200 && entry.StatusCode < 300 {
						hasUnrecoveredAuthErr = false
					}
					break
				}
			}
		}

		if hasUnrecoveredAuthErr {
			continue
		}

		// Check cooldown expiration / cap; never wipe >=12h (e.g. 24h 86400s TOS_VIOLATION) bans unless quota reset explicitly passed
		remSec := acc.CooldownUntil - nowUnix
		if remSec > 12*3600 && !quotaAlreadyReset {
			continue
		}
		transientCapExceeded := remSec > int64(PDCATransientCooldownCap.Seconds())
		if transientCapExceeded || quotaAlreadyReset {
			ident := acc.ID
			if ident == "" {
				ident = acc.Email
			}
			accountsToHeal = append(accountsToHeal, ident)

			anom := PDCAAnomaly{
				ID:        generatePDCAID("anom-cd"),
				CycleID:   cycleID,
				Timestamp: nowRFC,
				Type:      "stale_account_cooldown",
				Severity:  "warning",
				Provider:  prov,
				Account:   acc.Email,
				Detail:    fmt.Sprintf("account %s had stale cooldown (%ds remaining, cap %ds, quotaReset=%v) without auth failure; cleared", acc.Email, acc.CooldownUntil-nowUnix, int64(PDCATransientCooldownCap.Seconds()), quotaAlreadyReset),
			}
			report.Anomalies = append(report.Anomalies, anom)

			act := PDCAAction{
				ID:        generatePDCAID("act-cd"),
				CycleID:   cycleID,
				Timestamp: nowRFC,
				Type:      "heal_stale_cooldown",
				Provider:  prov,
				Account:   acc.Email,
				Target:    acc.Email,
				Detail:    fmt.Sprintf("cleared CooldownUntil and FailureCount for account %s", acc.Email),
			}
			report.Actions = append(report.Actions, act)
		}
	}

	if len(accountsToHeal) > 0 {
		configMu.Lock()
		cfg := loadSavedAuthConfigLocked()
		healedCount := 0
		for i := range cfg.Accounts {
			matched := false
			for _, ident := range accountsToHeal {
				if cfg.Accounts[i].ID == ident || strings.EqualFold(cfg.Accounts[i].Email, ident) {
					matched = true
					break
				}
			}
			if matched {
				cfg.Accounts[i].CooldownUntil = 0
				cfg.Accounts[i].FailureCount = 0
				healedCount++
			}
		}
		if healedCount > 0 {
			cfg.UpdatedAt = nowRFC
			_ = saveAuthConfigLocked(cfg)
			report.HealedStaleCooldowns = healedCount
		}
		configMu.Unlock()
	}

	// -------------------------------------------------------------
	// 2. Plan-Do-Check-Act: Dead/Rate-Limited Model Cache Pruner & Flagship Recovery
	// -------------------------------------------------------------
	var deadKeysToDelete []any
	var flagshipEvictionCanons []string

	deadCCPAModelCache.Range(func(k, v any) bool {
		entry, ok := v.(deadCCPAEntry)
		if !ok {
			deadKeysToDelete = append(deadKeysToDelete, k)
			return true
		}
		if !now.Before(entry.expiresAt) {
			deadKeysToDelete = append(deadKeysToDelete, k)
			return true
		}

		// Cap overly long transient flagship locks on Ultra accounts (allow 15m 404 negative cache; cap >20m 404 to 15m and >10m 429/503 to 2m)
		ks, okStr := k.(string)
		if okStr && strings.Contains(ks, "|acc:") {
			parts := strings.Split(ks, "|")
			candidate := parts[len(parts)-1]
			if isFlagshipCCPAModel(candidate) {
				if (entry.statusCode == 0 || entry.statusCode == 429 || entry.statusCode == 503) && entry.expiresAt.Sub(now) > 10*time.Minute {
					deadCCPAModelCache.Store(k, deadCCPAEntry{
						expiresAt:  now.Add(2 * time.Minute),
						statusCode: entry.statusCode,
					})
				} else if entry.statusCode == 404 && entry.expiresAt.Sub(now) > 20*time.Minute {
					deadCCPAModelCache.Store(k, deadCCPAEntry{
						expiresAt:  now.Add(15 * time.Minute),
						statusCode: entry.statusCode,
					})
				}
			}
		}
		return true
	})

	for _, k := range deadKeysToDelete {
		deadCCPAModelCache.Delete(k)
		report.PrunedDeadModelCacheEntries++
		if ks, ok := k.(string); ok {
			parts := strings.Split(ks, "|")
			candidate := parts[len(parts)-1]
			if isFlagshipCCPAModel(candidate) {
				flagshipEvictionCanons = append(flagshipEvictionCanons, candidate)
			}
		}
	}

	accountModelRLMu.Lock()
	var rlKeysToDelete []string
	for k, exp := range accountModelRateLimits {
		if !now.Before(exp) {
			rlKeysToDelete = append(rlKeysToDelete, k)
		}
	}
	for _, k := range rlKeysToDelete {
		delete(accountModelRateLimits, k)
		report.PrunedAccountRateLimitEntries++
		parts := strings.Split(k, "|")
		if len(parts) >= 2 && isFlagshipCCPAModel(parts[1]) {
			flagshipEvictionCanons = append(flagshipEvictionCanons, parts[1])
		}
	}
	accountModelRLMu.Unlock()

	// Evict degraded non-flagship fallbacks pinned in workingCCPAModelCache for healed flagship models
	if len(flagshipEvictionCanons) > 0 {
		workingCCPAModelCache.Range(func(k, v any) bool {
			ks, okK := k.(string)
			vs, okV := v.(string)
			if okK && okV {
				for _, fs := range flagshipEvictionCanons {
					if !isFlagshipCCPAModel(vs) && strings.Contains(strings.ToLower(ks), strings.ToLower(fs)) {
						workingCCPAModelCache.Delete(k)
						break
					}
				}
			}
			return true
		})
	}

	if report.PrunedDeadModelCacheEntries > 0 || report.PrunedAccountRateLimitEntries > 0 {
		anom := PDCAAnomaly{
			ID:        generatePDCAID("anom-mc"),
			CycleID:   cycleID,
			Timestamp: nowRFC,
			Type:      "expired_model_cache",
			Severity:  "info",
			Detail:    fmt.Sprintf("pruned %d expired dead-model cache entries and %d expired account-model rate limits", report.PrunedDeadModelCacheEntries, report.PrunedAccountRateLimitEntries),
		}
		report.Anomalies = append(report.Anomalies, anom)

		act := PDCAAction{
			ID:        generatePDCAID("act-mc"),
			CycleID:   cycleID,
			Timestamp: nowRFC,
			Type:      "heal_expired_model_cache",
			Detail:    fmt.Sprintf("pruned dead model cache entries and flushed expired rate limits"),
		}
		report.Actions = append(report.Actions, act)
	}

	// -------------------------------------------------------------
	// 3. Plan-Do-Check-Act: Ultra Tier Routing Leak Detector & Cache Sanitizer
	// -------------------------------------------------------------
	var ultraAccounts []ConnectedAccount
	hasHealthyProAccount := false

	for _, acc := range auth.Accounts {
		if acc.Disabled {
			continue
		}
		p := acc.Provider
		if p == "" {
			p = ProviderAntigravity
		}
		if p != ProviderAntigravity {
			continue
		}

		tier := LookupCachedAccountTier(acc, cs)
		if strings.EqualFold(strings.TrimSpace(tier), "Ultra") {
			ultraAccounts = append(ultraAccounts, acc)
		} else {
			if acc.CooldownUntil <= nowUnix {
				proHealthy := true
				if cs != nil {
					for _, d := range cs.Accounts {
						if (acc.ID != "" && d.ID == acc.ID) || (acc.Email != "" && strings.EqualFold(d.Email, acc.Email)) {
							if d.SessionPercent <= 10 && d.WeeklyPercent <= 10 {
								proHealthy = false
							}
							break
						}
					}
				} else if acc.WeeklyGeminiPercent != nil && *acc.WeeklyGeminiPercent <= 10 {
					proHealthy = false
				} else if acc.QuotaUsedPercent != nil && (100.0-*acc.QuotaUsedPercent) <= 10 {
					proHealthy = false
				}
				if proHealthy {
					hasHealthyProAccount = true
				}
			}
		}
	}

	if len(ultraAccounts) > 0 && hasHealthyProAccount {
		seenLeak := make(map[string]bool)
		for _, entry := range recentLogs {
			if entry.Provider != ProviderAntigravity {
				continue
			}
			isUltraAcc := false
			for _, ua := range ultraAccounts {
				if strings.EqualFold(entry.Account, ua.Email) || (ua.ID != "" && strings.EqualFold(entry.Account, ua.ID)) {
					isUltraAcc = true
					break
				}
			}
			if !isUltraAcc {
				continue
			}

			var leakedModel string
			for _, cand := range []string{entry.UpstreamModel, entry.ResolvedModel, entry.RequestedModel} {
				if isLowerTierGeminiModel(cand) {
					leakedModel = cand
					break
				}
			}
			if leakedModel == "" && isDirectFlash38LeakOnUltra(entry.RequestedModel, entry.ResolvedModel) {
				leakedModel = entry.ResolvedModel
				if leakedModel == "" {
					leakedModel = entry.RequestedModel
				}
			}
			if leakedModel != "" {
				leakKey := fmt.Sprintf("%s|%s", strings.ToLower(entry.Account), strings.ToLower(leakedModel))
				if !seenLeak[leakKey] {
					seenLeak[leakKey] = true
					report.Anomalies = append(report.Anomalies, PDCAAnomaly{
						ID:        generatePDCAID("anom-leak"),
						CycleID:   cycleID,
						Timestamp: nowRFC,
						Type:      "ultra_tier_routing_leak",
						Severity:  "warning",
						Provider:  ProviderAntigravity,
						Account:   entry.Account,
						Model:     leakedModel,
						Detail:    fmt.Sprintf("Ultra account %s executed lower-tier model %s while healthy Pro accounts were available", entry.Account, leakedModel),
					})
				}
			}
		}

		// Cache sanitizer: evict any non-flagship model pinned for Ultra accounts
		evictedPins := 0
		for _, ua := range ultraAccounts {
			scope := normalizeAccScope(ua.Email)
			prefix := "|acc:" + scope + "|"
			accEvicted := 0
			var lastTarget string
			workingCCPAModelCache.Range(func(k, v any) bool {
				ks, okK := k.(string)
				vs, okV := v.(string)
				if okK && okV && strings.Contains(strings.ToLower(ks), prefix) {
					parts := strings.Split(ks, "|")
					canonPart := parts[len(parts)-1]
					if isLowerTierGeminiModel(canonPart) || isDirectFlash38LeakOnUltra(canonPart, vs) || !isAllowedUltraPinForCanonical(canonPart, vs) {
						workingCCPAModelCache.Delete(k)
						evictedPins++
						accEvicted++
						lastTarget = ks
					}
				}
				return true
			})
			if accEvicted > 0 {
				report.Actions = append(report.Actions, PDCAAction{
					ID:        generatePDCAID("act-ultra-pin"),
					CycleID:   cycleID,
					Timestamp: nowRFC,
					Type:      "heal_ultra_working_model_pin",
					Provider:  ProviderAntigravity,
					Account:   ua.Email,
					Target:    lastTarget,
					Detail:    fmt.Sprintf("evicted %d non-flagship CCPA model pin(s) for Ultra account %s", accEvicted, ua.Email),
				})
			}
		}

		if evictedPins > 0 {
			report.EvictedUltraNonFlagshipPins = evictedPins
		}
	}

	// -------------------------------------------------------------
	// 4. Plan-Do-Check-Act: Provider Auth & Upstream Spike Sentinel
	// -------------------------------------------------------------
	type accountFailStats struct {
		provider     ProviderID
		account      string
		codes        map[int]int
		totalFails   int
		latestStatus int
		latestTime   time.Time
	}

	statsMap := make(map[string]*accountFailStats)
	for _, entry := range recentLogs {
		if entry.StatusCode < 400 {
			continue
		}
		accKey := fmt.Sprintf("%s|%s", entry.Provider, strings.ToLower(strings.TrimSpace(entry.Account)))
		st, exists := statsMap[accKey]
		if !exists {
			st = &accountFailStats{
				provider: entry.Provider,
				account:  entry.Account,
				codes:    make(map[int]int),
			}
			statsMap[accKey] = st
		}
		st.codes[entry.StatusCode]++
		st.totalFails++
		if t, err := time.Parse(time.RFC3339, entry.Timestamp); err == nil {
			if t.After(st.latestTime) {
				st.latestTime = t
				st.latestStatus = entry.StatusCode
			}
		}
	}

	// Check cross-provider cooldown contamination:
	// If account on P2 is in cooldown, P2 has ZERO recent error logs, but P1 != P2 has recent error logs.
	crossProviderContaminationDetected := false
	for _, acc := range auth.Accounts {
		if acc.Disabled || acc.CooldownUntil <= nowUnix {
			continue
		}
		provP2 := acc.Provider
		if provP2 == "" {
			provP2 = ProviderAntigravity
		}
		emailNorm := strings.ToLower(strings.TrimSpace(acc.Email))

		p2HasErrors := false
		p1HasSpike := false
		var p1Provider ProviderID

		for _, st := range statsMap {
			if strings.EqualFold(strings.ToLower(strings.TrimSpace(st.account)), emailNorm) {
				if st.provider == provP2 {
					p2HasErrors = true
				} else {
					p1HasSpike = true
					p1Provider = st.provider
				}
			}
		}

		if !p2HasErrors && p1HasSpike {
			crossProviderContaminationDetected = true
			report.CrossProviderIsolationVerified = false

			alert := PDCAAlert{
				ID:                    generatePDCAID("alert-xprov"),
				Severity:              "critical",
				Category:              "cross_provider_contamination",
				Provider:              provP2,
				Account:               acc.Email,
				FailureCount:          acc.FailureCount,
				CrossProviderIsolated: false,
				Summary:               fmt.Sprintf("account %s on %s suffered cross-provider cooldown contamination leaked from %s", acc.Email, provP2, p1Provider),
				RecommendedRemediation: fmt.Sprintf("isolate per-provider cooldown pools and verify independent transport sessions"),
				DetectedAt:            nowRFC,
			}
			report.Alerts = append(report.Alerts, alert)

			anom := PDCAAnomaly{
				ID:        generatePDCAID("anom-xprov"),
				CycleID:   cycleID,
				Timestamp: nowRFC,
				Type:      "cross_provider_cooldown_contamination",
				Severity:  "critical",
				Provider:  provP2,
				Account:   acc.Email,
				Detail:    alert.Summary,
			}
			report.Anomalies = append(report.Anomalies, anom)

			// Auto-heal contaminated cooldown
			configMu.Lock()
			cfg := loadSavedAuthConfigLocked()
			for i := range cfg.Accounts {
				if (cfg.Accounts[i].ID == acc.ID || strings.EqualFold(cfg.Accounts[i].Email, acc.Email)) && cfg.Accounts[i].Provider == provP2 {
					cfg.Accounts[i].CooldownUntil = 0
					cfg.Accounts[i].FailureCount = 0
					break
				}
			}
			_ = saveAuthConfigLocked(cfg)
			configMu.Unlock()

			act := PDCAAction{
				ID:        generatePDCAID("act-xprov"),
				CycleID:   cycleID,
				Timestamp: nowRFC,
				Type:      "heal_cross_provider_cooldown",
				Provider:  provP2,
				Account:   acc.Email,
				Detail:    fmt.Sprintf("auto-healed cross-provider contaminated cooldown for %s on %s", acc.Email, provP2),
			}
			report.Actions = append(report.Actions, act)
		}
	}

	if !crossProviderContaminationDetected {
		report.CrossProviderIsolationVerified = true
	}

	// Generate alerts for repeated 401/403/404 failures in deterministic key order
	statKeys := make([]string, 0, len(statsMap))
	for k := range statsMap {
		statKeys = append(statKeys, k)
	}
	sort.Strings(statKeys)

	for _, k := range statKeys {
		st := statsMap[k]
		authCount := st.codes[401] + st.codes[403]
		notFoundCount := st.codes[404]

		if authCount >= 2 {
			var authCodes []int
			if st.codes[401] > 0 {
				authCodes = append(authCodes, 401)
			}
			if st.codes[403] > 0 {
				authCodes = append(authCodes, 403)
			}
			remediation := "Re-authenticate account via OAuth callback or update expired credentials"
			if st.codes[403] > 0 && st.codes[401] == 0 {
				remediation = "Complete browser identity verification challenge or verify Cloud Code API entitlement"
			} else if st.codes[401] > 0 && st.codes[403] > 0 {
				remediation = "Refresh OAuth token, check API key validity, and resolve any VALIDATION_REQUIRED challenge"
			}
			alert := PDCAAlert{
				ID:                     generatePDCAID("alert-auth"),
				Severity:               "critical",
				Category:               "auth_revocation_or_forbidden",
				Provider:               st.provider,
				Account:                st.account,
				StatusCodes:            authCodes,
				FailureCount:           authCount,
				CrossProviderIsolated:  report.CrossProviderIsolationVerified,
				Summary:                fmt.Sprintf("repeated auth/permission failures (HTTP %v, count=%d) on provider %s for %s", authCodes, authCount, st.provider, st.account),
				RecommendedRemediation: remediation,
				DetectedAt:             nowRFC,
			}
			report.Alerts = append(report.Alerts, alert)
			report.Anomalies = append(report.Anomalies, PDCAAnomaly{
				ID:        generatePDCAID("anom-auth"),
				CycleID:   cycleID,
				Timestamp: nowRFC,
				Type:      "provider_auth_upstream_spike",
				Severity:  "critical",
				Provider:  st.provider,
				Account:   st.account,
				Detail:    alert.Summary,
			})
		}

		if notFoundCount >= 2 {
			alert := PDCAAlert{
				ID:                     generatePDCAID("alert-404"),
				Severity:               "warning",
				Category:               "upstream_model_not_found",
				Provider:               st.provider,
				Account:                st.account,
				StatusCodes:            []int{404},
				FailureCount:           notFoundCount,
				CrossProviderIsolated:  report.CrossProviderIsolationVerified,
				Summary:                fmt.Sprintf("repeated 404 model not found errors (count=%d) on %s for %s", notFoundCount, st.provider, st.account),
				RecommendedRemediation: "Verify model aliases or check upstream CCPA model deployment availability",
				DetectedAt:             nowRFC,
			}
			report.Alerts = append(report.Alerts, alert)
			report.Anomalies = append(report.Anomalies, PDCAAnomaly{
				ID:        generatePDCAID("anom-404"),
				CycleID:   cycleID,
				Timestamp: nowRFC,
				Type:      "provider_auth_upstream_spike",
				Severity:  "warning",
				Provider:  st.provider,
				Account:   st.account,
				Detail:    alert.Summary,
			})
		}
	}

	report.DurationMs = time.Since(start).Milliseconds()
	if len(report.Alerts) > 0 {
		report.Status = "alert"
	} else if len(report.Actions) > 0 {
		report.Status = "healed"
	} else {
		report.Status = "healthy"
	}

	// Update in-memory state
	pdcaMu.Lock()
	defer pdcaMu.Unlock()

	pdcaTotalCycles++
	pdcaTotalAnomalies += int64(len(report.Anomalies))
	pdcaTotalActions += int64(len(report.Actions))
	pdcaLastCycleAt = report.StartedAt
	pdcaLastVerified = report.CrossProviderIsolationVerified

	// Insert report to pdcaRecentCycles (newest first)
	pdcaRecentCycles = append([]PDCACycleReport{report}, pdcaRecentCycles...)
	if len(pdcaRecentCycles) > MaxPDCACycleHistory {
		pdcaRecentCycles = pdcaRecentCycles[:MaxPDCACycleHistory]
	}

	// Insert actions (newest first)
	if len(report.Actions) > 0 {
		reversed := make([]PDCAAction, len(report.Actions))
		for i, a := range report.Actions {
			reversed[len(report.Actions)-1-i] = a
		}
		pdcaRecentActions = append(reversed, pdcaRecentActions...)
		if len(pdcaRecentActions) > MaxPDCAActionHistory {
			pdcaRecentActions = pdcaRecentActions[:MaxPDCAActionHistory]
		}
	}

	// Insert anomalies (newest first)
	if len(report.Anomalies) > 0 {
		reversed := make([]PDCAAnomaly, len(report.Anomalies))
		for i, an := range report.Anomalies {
			reversed[len(report.Anomalies)-1-i] = an
		}
		pdcaRecentAnomalies = append(reversed, pdcaRecentAnomalies...)
		if len(pdcaRecentAnomalies) > MaxPDCAAnomalyHistory {
			pdcaRecentAnomalies = pdcaRecentAnomalies[:MaxPDCAAnomalyHistory]
		}
	}

	pdcaActiveAlerts = append([]PDCAAlert(nil), report.Alerts...)

	return report
}

// GetPDCAStatus returns a deep copy of current PDCA health and status snapshot.
func GetPDCAStatus() PDCAStatusSnapshot {
	pdcaMu.RLock()
	defer pdcaMu.RUnlock()

	status := "healthy"
	if len(pdcaActiveAlerts) > 0 {
		status = "alert"
	} else if len(pdcaRecentActions) > 0 && len(pdcaRecentCycles) > 0 && pdcaRecentCycles[0].Status == "healed" {
		status = "healed"
	}

	alertsCopy := make([]PDCAAlert, len(pdcaActiveAlerts))
	copy(alertsCopy, pdcaActiveAlerts)

	actionsCopy := make([]PDCAAction, len(pdcaRecentActions))
	copy(actionsCopy, pdcaRecentActions)

	anomaliesCopy := make([]PDCAAnomaly, len(pdcaRecentAnomalies))
	copy(anomaliesCopy, pdcaRecentAnomalies)

	cyclesCopy := make([]PDCACycleReport, len(pdcaRecentCycles))
	copy(cyclesCopy, pdcaRecentCycles)

	return PDCAStatusSnapshot{
		Status:                         status,
		WatchdogRunning:                pdcaWatchdogRunning.Load() > 0,
		WatchdogIntervalSec:            pdcaIntervalSec.Load(),
		LastCycleAt:                    pdcaLastCycleAt,
		TotalCycles:                    pdcaTotalCycles,
		TotalAnomalies:                 pdcaTotalAnomalies,
		TotalActions:                   pdcaTotalActions,
		CrossProviderIsolationVerified: pdcaLastVerified,
		Alerts:                         alertsCopy,
		RecentActions:                  actionsCopy,
		RecentAnomalies:                anomaliesCopy,
		RecentCycles:                   cyclesCopy,
	}
}

// HandlePDCAStatus provides the HTTP endpoint for PDCA inspection and on-demand trigger.
func HandlePDCAStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodPost {
		writeOpenAIError(w, http.StatusMethodNotAllowed, "only GET, HEAD, and POST methods are supported", "invalid_request_error")
		return
	}

	if r.Method == http.MethodPost || r.URL.Query().Get("run") == "true" || r.URL.Query().Get("refresh") == "true" {
		RunPDCACycle(time.Now().UTC())
	}

	snapshot := GetPDCAStatus()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_ = json.NewEncoder(w).Encode(snapshot)
	}
}
