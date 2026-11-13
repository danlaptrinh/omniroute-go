package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ModelQuotaInfo holds remaining quota percentage and reset info for a specific model on an account.
type ModelQuotaInfo struct {
	ModelID          string `json:"model_id"`
	DisplayName      string `json:"display_name"`
	RemainingPercent int    `json:"remaining_percent"` // 0 - 100
	UsedRequests     int    `json:"used_requests,omitempty"`
	LimitRequests    int    `json:"limit_requests,omitempty"`
	ResetTime        string `json:"reset_time"` // e.g. "⏱️ Đặt lại sau 1h 35m"
	Status           string `json:"status"`     // "good", "warning", "critical"
}

// AccountQuotaDetail represents an account card matching the Provider Quotas dashboard.
type AccountQuotaDetail struct {
	ID                  string           `json:"id"`
	Provider            ProviderID       `json:"provider"`
	Email               string           `json:"email"`
	MaskedEmail         string           `json:"masked_email"`
	Label               string           `json:"label"`
	Tier                string           `json:"tier"` // "Ultra", "Pro", "Lite", "Token Plan"
	Status              string           `json:"status"` // "healthy", "warning", "critical"
	ErrorMessage        string           `json:"error_message,omitempty"`
	ValidationURL       string           `json:"validation_url,omitempty"`
	Enabled             bool             `json:"enabled"`
	InCooldown          bool             `json:"in_cooldown"`
	SuccessCount        int64            `json:"success_count"`
	FailureCount        int              `json:"failure_count"`
	TotalTokens         int64            `json:"total_tokens"`
	AvgLatencyMs        int64            `json:"avg_latency_ms"`
	LastLatencyMs       int64            `json:"last_latency_ms,omitempty"`
	LastModel           string           `json:"last_model,omitempty"`
	InFlight            int              `json:"in_flight"`
	ActiveModel         string           `json:"active_model,omitempty"`
	LastUsedAt          string           `json:"last_used_at,omitempty"`
	SessionPercent      int              `json:"session_percent"`
	WeeklyPercent       int              `json:"weekly_percent"`
	WeeklyGeminiPercent *int             `json:"weekly_gemini_percent,omitempty"`
	ResetTimeText       string           `json:"reset_time_text"`
	LastCheckedAt       string           `json:"last_checked_at"`
	QuotaUsedPercent    *float64         `json:"quota_used_percent,omitempty"`
	EstimatedValueUSD   float64          `json:"estimated_value_usd"`
	SupportedModels     []string         `json:"supported_models"`
	ModelQuotas         []ModelQuotaInfo `json:"model_quotas"`
}

// QuotaSummary represents the KPI aggregate across all accounts.
type QuotaSummary struct {
	TotalAccounts          int                  `json:"total_accounts"`
	HealthyAccounts        int                  `json:"healthy_accounts"`
	WarningAccounts        int                  `json:"warning_accounts"`
	CriticalAccounts       int                  `json:"critical_accounts"`
	TotalEstimatedValueUSD float64              `json:"total_estimated_value_usd"`
	Accounts               []AccountQuotaDetail `json:"accounts"`
	UpdatedAt              string               `json:"updated_at"`
}

var (
	quotaCacheMu    sync.RWMutex
	lastQuotaCheck  time.Time
	cachedSummary   *QuotaSummary
	quotaRefreshing int32

	// CCPAFetchAvailableModelsEndpoint is optional override for v1internal:fetchAvailableModels;
	// when empty, derived automatically from CCPARetrieveUserQuotaEndpoint so mock tests stay local.
	CCPAFetchAvailableModelsEndpoint = ""

	// accountTierCache stores live CCPA loadCodeAssist tier ("Ultra", "Pro", "Lite") keyed by lowercase email/ID.
	accountTierCache sync.Map

	// validationIsolatedAccounts tracks accounts isolated specifically for VALIDATION_REQUIRED
	// so permanent 24h TOS_VIOLATION bans are never cleared when loadCodeAssist returns 200 OK.
	validationIsolatedAccounts sync.Map

	quotaWarmupOnce sync.Once
)

// RememberAccountTier caches the verified live tier for an account email and/or ID.
func RememberAccountTier(email, id, tier string) {
	t := strings.TrimSpace(tier)
	if t == "" {
		return
	}
	if e := strings.ToLower(strings.TrimSpace(email)); e != "" {
		accountTierCache.Store(e, t)
	}
	if cleanID := strings.ToLower(strings.TrimSpace(id)); cleanID != "" {
		accountTierCache.Store(cleanID, t)
	}
}

func findCachedAccountDetail(acc ConnectedAccount, cached *QuotaSummary) *AccountQuotaDetail {
	if cached == nil {
		return nil
	}
	for i := range cached.Accounts {
		cand := &cached.Accounts[i]
		if (acc.ID != "" && cand.ID == acc.ID) ||
			(acc.Email != "" && strings.EqualFold(cand.Email, acc.Email)) {
			return cand
		}
	}
	return nil
}

func inferTierFromLabel(label string) string {
	l := strings.ToLower(strings.TrimSpace(label))
	if l == "" {
		return ""
	}
	switch {
	case strings.Contains(l, "ultra"):
		return "Ultra"
	case strings.Contains(l, "lite") || strings.Contains(l, "free"):
		return "Lite"
	case strings.Contains(l, "pro") || strings.Contains(l, "premium"):
		return "Pro"
	default:
		return ""
	}
}

// LookupCachedAccountTier resolves an Antigravity account's tier from cached live summary,
// live tier cache, or explicit account label — never guessing from email substrings.
func LookupCachedAccountTier(acc ConnectedAccount, cached *QuotaSummary) string {
	if prev := findCachedAccountDetail(acc, cached); prev != nil && strings.TrimSpace(prev.Tier) != "" {
		return prev.Tier
	}
	if e := strings.ToLower(strings.TrimSpace(acc.Email)); e != "" {
		if v, ok := accountTierCache.Load(e); ok {
			if s, okStr := v.(string); okStr && s != "" {
				return s
			}
		}
	}
	if cleanID := strings.ToLower(strings.TrimSpace(acc.ID)); cleanID != "" {
		if v, ok := accountTierCache.Load(cleanID); ok {
			if s, okStr := v.(string); okStr && s != "" {
				return s
			}
		}
	}
	if persisted := strings.TrimSpace(acc.Tier); persisted != "" {
		return persisted
	}
	if fromLabel := inferTierFromLabel(acc.Label); fromLabel != "" {
		return fromLabel
	}
	return "Pro"
}

func syncAccountTierIfChanged(acc *ConnectedAccount, tier string) {
	cleanTier := strings.TrimSpace(tier)
	if acc == nil || cleanTier == "" {
		return
	}
	acc.Tier = cleanTier
	configMu.Lock()
	cfg := loadSavedAuthConfigLocked()
	changed := false
	for i, existing := range cfg.Accounts {
		existingProv := existing.Provider
		if existingProv == "" {
			existingProv = ProviderAntigravity
		}
		if (acc.ID != "" && existing.ID == acc.ID) ||
			(existingProv == ProviderAntigravity && acc.Email != "" && strings.EqualFold(existing.Email, acc.Email)) {
			if cfg.Accounts[i].Tier != cleanTier {
				cfg.Accounts[i].Tier = cleanTier
				changed = true
			}
			break
		}
	}
	if changed {
		cfg.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		_ = saveAuthConfigLocked(cfg)
	}
	configMu.Unlock()
}

func syncAccountClaudePercentIfChanged(acc *ConnectedAccount, claudePct int) {
	if acc == nil {
		return
	}
	configMu.Lock()
	cfg := loadSavedAuthConfigLocked()
	changed := false
	for i, existing := range cfg.Accounts {
		existingProv := existing.Provider
		if existingProv == "" {
			existingProv = ProviderAntigravity
		}
		if (acc.ID != "" && existing.ID == acc.ID) ||
			(existingProv == ProviderAntigravity && acc.Email != "" && strings.EqualFold(existing.Email, acc.Email)) {
			if cfg.Accounts[i].WeeklyClaudePercent == nil || *cfg.Accounts[i].WeeklyClaudePercent != claudePct {
				cfg.Accounts[i].WeeklyClaudePercent = &claudePct
				changed = true
			}
			break
		}
	}
	if changed {
		cfg.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		_ = saveAuthConfigLocked(cfg)
	}
	configMu.Unlock()
}

// MaskEmail masks email addresses for display e.g. "phongsaigon11@gmail.com" -> "pho***@gmail.com".
func MaskEmail(email string) string {
	parts := strings.Split(email, "@")
	if len(parts) != 2 {
		if len(email) > 6 {
			return email[:3] + "***" + email[len(email)-2:]
		}
		return email
	}
	name := parts[0]
	domain := parts[1]
	if len(name) <= 3 {
		return name + "***@" + domain
	}
	return name[:3] + "***@" + domain
}

func getQuotaStatus(pct int) string {
	if pct <= 20 {
		return "critical"
	}
	if pct <= 50 {
		return "warning"
	}
	return "good"
}

func formatResetDuration(rfc3339Time string, fallback string) string {
	if strings.TrimSpace(rfc3339Time) == "" {
		return fallback
	}
	t, err := time.Parse(time.RFC3339, rfc3339Time)
	if err != nil {
		return rfc3339Time
	}
	now := time.Now().UTC()
	diff := t.Sub(now)
	if diff <= 0 {
		return "⏱️ Đặt lại ngay"
	}
	if diff >= 24*time.Hour {
		days := int(diff.Hours()) / 24
		hours := int(diff.Hours()) % 24
		if hours == 0 {
			return fmt.Sprintf("⏱️ Đặt lại sau %dd", days)
		}
		return fmt.Sprintf("⏱️ Đặt lại sau %dd %dh", days, hours)
	}
	hours := int(diff.Hours())
	mins := int(diff.Minutes()) % 60
	return fmt.Sprintf("⏱️ Đặt lại sau %dh %02dm", hours, mins)
}

func formatAccountLastUsed(rfc3339Time string) string {
	if strings.TrimSpace(rfc3339Time) == "" {
		return ""
	}
	t, err := time.Parse(time.RFC3339, rfc3339Time)
	if err != nil {
		return rfc3339Time
	}
	return t.Local().Format("15:04:05")
}

// EstimateModelCostUSD estimates the equivalent API cost in USD for a given model and token count,
// modeled after Orca's MODEL_PRICING (blended input/output per 1M tokens).
func EstimateModelCostUSD(modelID string, tokens int64) float64 {
	if tokens <= 0 {
		return 0
	}
	s := strings.ToLower(strings.TrimSpace(modelID))
	ratePerMillion := 5.0 // default blended rate ($5 / 1M tokens)
	switch {
	case strings.Contains(s, "fable-5-1"), strings.Contains(s, "mythos-5-1"), strings.Contains(s, "fable-5"), strings.Contains(s, "mythos-5"):
		ratePerMillion = 22.0 // Orca Fable/Mythos ($10 in / $50 out blended)
	case strings.Contains(s, "opus-5-5"):
		ratePerMillion = 9.5 // Orca Opus 5.5 ($4 in / $20 out blended)
	case strings.Contains(s, "opus-5"), strings.Contains(s, "opus-4-8"), strings.Contains(s, "opus-4-7"), strings.Contains(s, "opus-4-6"):
		ratePerMillion = 11.0 // Orca Opus 5 / 4.x ($5 in / $25 out blended)
	case strings.Contains(s, "sonnet-5-5"), strings.Contains(s, "sonnet-5"):
		ratePerMillion = 4.8 // Orca Sonnet 5.5 ($2 in / $10 out blended)
	case strings.Contains(s, "sonnet-4-6"):
		ratePerMillion = 6.5 // Orca Sonnet 4.6 ($3 in / $15 out blended)
	case strings.Contains(s, "haiku-5-5"):
		ratePerMillion = 0.35
	case strings.Contains(s, "haiku-4-5"):
		ratePerMillion = 2.2
	case strings.Contains(s, "gemini-4-carbon"), strings.Contains(s, "carbon"):
		ratePerMillion = 10.0 // Gemini 4 Carbon High flagship ($2.00 in / $10.00 out per 1M tokens)
	case strings.Contains(s, "gemini-4-argon"):
		ratePerMillion = 8.5 // Gemini 4 Argon High flagship
	case strings.Contains(s, "gemini-3.1-pro"):
		ratePerMillion = 5.5 // Gemini 3.1 Pro High
	case strings.Contains(s, "gemini-3.8-flash"), strings.Contains(s, "gemini-3.7-flash"):
		ratePerMillion = 1.6 // Gemini 3.8 Flash High
	case strings.Contains(s, "qwen3.8-max"), strings.Contains(s, "deepseek-v4-pro"), strings.Contains(s, "glm-5.3"):
		ratePerMillion = 2.4 // Alibaba Token Plan flagship
	case strings.Contains(s, "qwen3.8-flash"), strings.Contains(s, "deepseek-v4.1-flash"):
		ratePerMillion = 0.8
	}
	val := (float64(tokens) / 1_000_000.0) * ratePerMillion
	return math.Round(val*1000) / 1000
}

// EstimateAccountValueUSD estimates cumulative USD API value delivered by a connected subscription account.
func EstimateAccountValueUSD(acc ConnectedAccount) float64 {
	model := acc.LastModel
	if model == "" {
		switch acc.Provider {
		case ProviderClaudeCode:
			model = "claude-code/claude-opus-5-5"
		case ProviderAlibaba:
			model = "alibaba/qwen3.8-max"
		default:
			model = "antigravity/gemini-4-argon-high"
		}
	}
	return EstimateModelCostUSD(model, acc.TotalTokens)
}

func overlayLiveAccountStats(summary *QuotaSummary) *QuotaSummary {
	if summary == nil {
		return nil
	}
	auth := LoadSavedAuthConfig()
	nowUnix := time.Now().Unix()
	out := *summary
	out.Accounts = make([]AccountQuotaDetail, len(summary.Accounts))
	copy(out.Accounts, summary.Accounts)

	var totalUSD float64
	for i := range out.Accounts {
		for _, acc := range auth.Accounts {
			if (out.Accounts[i].ID != "" && out.Accounts[i].ID == acc.ID) ||
				(strings.EqualFold(out.Accounts[i].Email, acc.Email) && acc.Email != "") {
				out.Accounts[i].Enabled = !acc.Disabled
				out.Accounts[i].InCooldown = acc.CooldownUntil > nowUnix
				out.Accounts[i].SuccessCount = acc.SuccessCount
				out.Accounts[i].FailureCount = acc.FailureCount
				out.Accounts[i].TotalTokens = acc.TotalTokens
				out.Accounts[i].AvgLatencyMs = acc.AvgLatencyMs
				out.Accounts[i].LastLatencyMs = acc.LastLatencyMs
				out.Accounts[i].LastModel = acc.LastModel
				out.Accounts[i].InFlight = GetAccountInFlight(acc.Email)
				out.Accounts[i].ActiveModel = GetAccountActiveRole(acc.Email)
				out.Accounts[i].QuotaUsedPercent = acc.QuotaUsedPercent
				out.Accounts[i].WeeklyGeminiPercent = acc.WeeklyGeminiPercent
				out.Accounts[i].LastUsedAt = formatAccountLastUsed(acc.LastUsedAt)
				out.Accounts[i].EstimatedValueUSD = EstimateAccountValueUSD(acc)
				break
			}
		}
		totalUSD += out.Accounts[i].EstimatedValueUSD
	}
	out.TotalEstimatedValueUSD = math.Round(totalUSD*1000) / 1000
	return &out
}

// triggerBackgroundQuotaRefresh refreshes cachedSummary asynchronously without blocking HTTP readers.
func triggerBackgroundQuotaRefresh() {
	if !atomic.CompareAndSwapInt32(&quotaRefreshing, 0, 1) {
		return
	}
	go func() {
		defer atomic.StoreInt32(&quotaRefreshing, 0)
		bgCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		summary := inspectAllQuotasLocked(bgCtx)
		quotaCacheMu.Lock()
		cachedSummary = summary
		lastQuotaCheck = time.Now()
		quotaCacheMu.Unlock()
	}()
}

// WarmupAccountQuotasAndTiersAsync starts a singleton background loop that immediately refreshes all
// connected Antigravity accounts concurrently on startup and repeats every 60s so Tier and
// accountModelRateLimits stay warm even with zero UI tabs open.
func WarmupAccountQuotasAndTiersAsync() {
	quotaWarmupOnce.Do(func() {
		go func() {
			triggerBackgroundQuotaRefresh()
			ticker := time.NewTicker(60 * time.Second)
			defer ticker.Stop()
			for range ticker.C {
				triggerBackgroundQuotaRefresh()
			}
		}()
	})
}

// buildInstantQuotaSummary builds an immediate <1ms quota summary from saved config without waiting for external HTTP calls.
func buildInstantQuotaSummary(auth SavedGatewayAuth) *QuotaSummary {
	now := time.Now().UTC()
	nextMidnight := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC)
	diff := nextMidnight.Sub(now)
	resetText := fmt.Sprintf("⏱️ Đặt lại sau %dh %02dm", int(diff.Hours()), int(diff.Minutes())%60)

	quotaCacheMu.RLock()
	prevSummary := cachedSummary
	quotaCacheMu.RUnlock()

	summary := &QuotaSummary{
		TotalAccounts: len(auth.Accounts),
		Accounts:      make([]AccountQuotaDetail, len(auth.Accounts)),
		UpdatedAt:     now.Format("15:04:05"),
	}

	for i, acc := range auth.Accounts {
		email := acc.Email
		if email == "" {
			email = acc.Label
		}
		detail := AccountQuotaDetail{
			ID:                  acc.ID,
			Provider:            acc.Provider,
			Email:               email,
			MaskedEmail:         MaskEmail(email),
			Label:               acc.Label,
			Enabled:             !acc.Disabled,
			InCooldown:          acc.CooldownUntil > time.Now().Unix(),
			SuccessCount:        acc.SuccessCount,
			FailureCount:        acc.FailureCount,
			TotalTokens:         acc.TotalTokens,
			AvgLatencyMs:        acc.AvgLatencyMs,
			LastLatencyMs:       acc.LastLatencyMs,
			LastModel:           acc.LastModel,
			InFlight:            GetAccountInFlight(email),
			ActiveModel:         GetAccountActiveRole(email),
			LastUsedAt:          formatAccountLastUsed(acc.LastUsedAt),
			WeeklyGeminiPercent: acc.WeeklyGeminiPercent,
			ResetTimeText:       resetText,
			LastCheckedAt:       time.Now().Format("15:04:05"),
			Status:              "healthy",
			QuotaUsedPercent:    acc.QuotaUsedPercent,
			EstimatedValueUSD:   EstimateAccountValueUSD(acc),
		}
		if acc.Disabled {
			detail.Status = "warning"
		}
		switch acc.Provider {
		case ProviderClaudeCode:
			inspectClaudeCodeAccount(nil, &acc, &detail, resetText)
		case ProviderAlibaba:
			inspectAlibabaAccount(nil, &acc, &detail, resetText)
		default:
			detail.Tier = LookupCachedAccountTier(acc, prevSummary)
			if prev := findCachedAccountDetail(acc, prevSummary); prev != nil && len(prev.ModelQuotas) > 0 {
				detail.SessionPercent = prev.SessionPercent
				detail.WeeklyPercent = prev.WeeklyPercent
				if prev.ResetTimeText != "" {
					detail.ResetTimeText = prev.ResetTimeText
				}
				if len(prev.SupportedModels) > 0 {
					detail.SupportedModels = append([]string(nil), prev.SupportedModels...)
				}
				detail.ModelQuotas = append([]ModelQuotaInfo(nil), prev.ModelQuotas...)
			} else {
				geminiWeekly := 84
				if acc.WeeklyGeminiPercent != nil && *acc.WeeklyGeminiPercent > 0 {
					geminiWeekly = *acc.WeeklyGeminiPercent
				}
				detail.SessionPercent = 98
				detail.WeeklyPercent = geminiWeekly
				detail.SupportedModels = []string{
					"antigravity/gemini-4-carbon-high",
					"antigravity/gemini-4-argon-high",
					"antigravity/gemini-3.8-flash-high",
					"antigravity/gemini-3.8-flash-medium",
					"antigravity/gemini-3-flash",
					"antigravity/claude-opus-4-6-thinking",
					"antigravity/claude-sonnet-4-6",
					"antigravity/gemini-3.5-flash-lite",
				}
				detail.ModelQuotas = []ModelQuotaInfo{
					{ModelID: "group-gemini-5h", DisplayName: "Gemini Models (Giới hạn 5h)", RemainingPercent: 98, ResetTime: resetText, Status: "good"},
					{ModelID: "group-gemini-weekly", DisplayName: "Gemini Models (Giới hạn Tuần)", RemainingPercent: geminiWeekly, ResetTime: "⏱️ Đặt lại sau 5d", Status: getQuotaStatus(geminiWeekly)},
					{ModelID: "antigravity/gemini-4-carbon-high", DisplayName: "Gemini 4 Carbon (High)", RemainingPercent: 98, ResetTime: resetText, Status: "good"},
					{ModelID: "gemini-4-argon-high", DisplayName: "Gemini 4 Argon (High)", RemainingPercent: 98, ResetTime: resetText, Status: "good"},
					{ModelID: "gemini-3.8-flash-high", DisplayName: "Gemini 3.8 Flash (High)", RemainingPercent: 98, ResetTime: resetText, Status: "good"},
					{ModelID: "gemini-3-flash", DisplayName: "Gemini 3 Flash", RemainingPercent: 98, ResetTime: resetText, Status: "good"},
				}
			}
		}
		summary.Accounts[i] = detail
		summary.TotalEstimatedValueUSD = math.Round((summary.TotalEstimatedValueUSD+detail.EstimatedValueUSD)*1000) / 1000
		switch detail.Status {
		case "critical":
			summary.CriticalAccounts++
		case "warning":
			summary.WarningAccounts++
		default:
			summary.HealthyAccounts++
		}
	}
	return summary
}

// GetQuotaSummary returns the latest quota status without ever blocking on quotaCacheMu during network I/O.
func GetQuotaSummary(ctx context.Context, forceRefresh bool) *QuotaSummary {
	auth := LoadSavedAuthConfig()
	quotaCacheMu.RLock()
	cached := cachedSummary
	lastCheck := lastQuotaCheck
	quotaCacheMu.RUnlock()

	if !forceRefresh && cached != nil && len(cached.Accounts) == len(auth.Accounts) {
		if time.Since(lastCheck) >= 35*time.Second {
			triggerBackgroundQuotaRefresh()
		}
		return overlayLiveAccountStats(cached)
	}

	if !forceRefresh {
		// Wait at most 1.5s for initial live check; if upstream CCPA is slow, return instant summary immediately
		done := make(chan *QuotaSummary, 1)
		go func() {
			checkCtx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			defer cancel()
			s := inspectAllQuotasLocked(checkCtx)
			quotaCacheMu.Lock()
			cachedSummary = s
			lastQuotaCheck = time.Now()
			quotaCacheMu.Unlock()
			done <- s
		}()

		select {
		case s := <-done:
			return overlayLiveAccountStats(s)
		case <-time.After(1500 * time.Millisecond):
			return buildInstantQuotaSummary(auth)
		}
	}

	// Explicit forceRefresh == true: bounded 4.5s timeout without holding quotaCacheMu during network calls
	refreshCtx, cancel := context.WithTimeout(ctx, 4500*time.Millisecond)
	defer cancel()
	summary := inspectAllQuotasLocked(refreshCtx)
	quotaCacheMu.Lock()
	cachedSummary = summary
	lastQuotaCheck = time.Now()
	quotaCacheMu.Unlock()
	return overlayLiveAccountStats(summary)
}

func inspectAllQuotasLocked(ctx context.Context) *QuotaSummary {
	auth := LoadSavedAuthConfig()
	now := time.Now().UTC()

	// Calculate time remaining until midnight UTC for daily fallback
	nextMidnight := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC)
	diff := nextMidnight.Sub(now)
	hours := int(diff.Hours())
	mins := int(diff.Minutes()) % 60
	resetText := fmt.Sprintf("⏱️ Đặt lại sau %dh %02dm", hours, mins)

	summary := &QuotaSummary{
		TotalAccounts: len(auth.Accounts),
		Accounts:      make([]AccountQuotaDetail, len(auth.Accounts)),
		UpdatedAt:     now.Format("15:04:05"),
	}

	var wg sync.WaitGroup
	for i, acc := range auth.Accounts {
		wg.Add(1)
		go func(idx int, account ConnectedAccount) {
			defer wg.Done()
			summary.Accounts[idx] = inspectSingleAccountQuota(ctx, account, resetText)
		}(i, acc)
	}
	wg.Wait()

	for _, detail := range summary.Accounts {
		summary.TotalEstimatedValueUSD = math.Round((summary.TotalEstimatedValueUSD+detail.EstimatedValueUSD)*1000) / 1000
		switch detail.Status {
		case "critical":
			summary.CriticalAccounts++
		case "warning":
			summary.WarningAccounts++
		default:
			summary.HealthyAccounts++
		}
	}

	return summary
}

func inspectSingleAccountQuota(ctx context.Context, acc ConnectedAccount, resetText string) AccountQuotaDetail {
	email := acc.Email
	if email == "" {
		email = acc.Label
	}
	masked := MaskEmail(email)

	detail := AccountQuotaDetail{
		ID:                  acc.ID,
		Provider:            acc.Provider,
		Email:               email,
		MaskedEmail:         masked,
		Label:               acc.Label,
		Enabled:             !acc.Disabled,
		InCooldown:          acc.CooldownUntil > time.Now().Unix(),
		SuccessCount:        acc.SuccessCount,
		FailureCount:        acc.FailureCount,
		TotalTokens:         acc.TotalTokens,
		AvgLatencyMs:        acc.AvgLatencyMs,
		LastLatencyMs:       acc.LastLatencyMs,
		LastModel:           acc.LastModel,
		InFlight:            GetAccountInFlight(email),
		ActiveModel:         GetAccountActiveRole(email),
		LastUsedAt:          formatAccountLastUsed(acc.LastUsedAt),
		WeeklyGeminiPercent: acc.WeeklyGeminiPercent,
		ResetTimeText:       resetText,
		LastCheckedAt:       time.Now().Format("15:04:05"),
		Status:              "healthy",
		QuotaUsedPercent:    acc.QuotaUsedPercent,
		EstimatedValueUSD:   EstimateAccountValueUSD(acc),
	}

	if acc.Disabled {
		detail.Status = "warning"
	}

	switch acc.Provider {
	case ProviderClaudeCode:
		inspectClaudeCodeAccount(ctx, &acc, &detail, resetText)
	case ProviderAlibaba:
		inspectAlibabaAccount(ctx, &acc, &detail, resetText)
	default: // ProviderAntigravity
		inspectAntigravityAccount(ctx, &acc, &detail, resetText)
	}

	return detail
}

type claudeUsageWindowInput struct {
	Utilization    *float64 `json:"utilization,omitempty"`
	UsedPercentage *float64 `json:"used_percentage,omitempty"`
	Percent        *float64 `json:"percent,omitempty"`
	ResetsAt       any      `json:"resets_at,omitempty"`
}

type claudeUsageScopedLimit struct {
	Kind     string  `json:"kind"`
	Percent  float64 `json:"percent"`
	ResetsAt any     `json:"resets_at,omitempty"`
	Scope    *struct {
		Model *struct {
			DisplayName string `json:"display_name"`
		} `json:"model,omitempty"`
	} `json:"scope,omitempty"`
}

type claudeOAuthMoney struct {
	AmountMinor *float64 `json:"amount_minor,omitempty"`
	Currency    string   `json:"currency,omitempty"`
	Exponent    *int     `json:"exponent,omitempty"`
}

type claudeOAuthUsageResponse struct {
	FiveHour      *claudeUsageWindowInput  `json:"five_hour,omitempty"`
	SevenDay      *claudeUsageWindowInput  `json:"seven_day,omitempty"`
	FableWeekly   *claudeUsageWindowInput  `json:"fable_weekly,omitempty"`
	FableSevenDay *claudeUsageWindowInput  `json:"fable_seven_day,omitempty"`
	SevenDayFable *claudeUsageWindowInput  `json:"seven_day_fable,omitempty"`
	Limits        []claudeUsageScopedLimit `json:"limits,omitempty"`
	Spend         *struct {
		Used    *claudeOAuthMoney `json:"used,omitempty"`
		Limit   *claudeOAuthMoney `json:"limit,omitempty"`
		Percent *float64          `json:"percent,omitempty"`
		Enabled bool              `json:"enabled,omitempty"`
	} `json:"spend,omitempty"`
	ExtraUsage *struct {
		IsEnabled     bool     `json:"is_enabled,omitempty"`
		MonthlyLimit  *float64 `json:"monthly_limit,omitempty"`
		UsedCredits   *float64 `json:"used_credits,omitempty"`
		Utilization   *float64 `json:"utilization,omitempty"`
		DecimalPlaces *int     `json:"decimal_places,omitempty"`
	} `json:"extra_usage,omitempty"`
}

func parseClaudeResetAny(val any, fallback string) string {
	if val == nil {
		return fallback
	}
	switch v := val.(type) {
	case string:
		v = strings.TrimSpace(v)
		if v == "" {
			return fallback
		}
		return formatResetDuration(v, fallback)
	case float64:
		if v <= 0 {
			return fallback
		}
		sec := int64(v)
		if v > 10_000_000_000 {
			sec = int64(v / 1000)
		}
		return formatResetDuration(time.Unix(sec, 0).UTC().Format(time.RFC3339), fallback)
	default:
		return fallback
	}
}

func extractClaudeWindowRemaining(w *claudeUsageWindowInput, fallbackRemaining int, fallbackReset string) (int, string, bool) {
	if w == nil {
		return fallbackRemaining, fallbackReset, false
	}
	var used *float64
	if w.Utilization != nil {
		used = w.Utilization
	} else if w.UsedPercentage != nil {
		used = w.UsedPercentage
	} else if w.Percent != nil {
		used = w.Percent
	}
	if used == nil {
		return fallbackRemaining, fallbackReset, false
	}
	u := *used
	if u > 0 && u <= 1.0 {
		u = u * 100.0
	}
	rem := int(math.Round(100.0 - u))
	if rem < 0 {
		rem = 0
	} else if rem > 100 {
		rem = 100
	}
	return rem, parseClaudeResetAny(w.ResetsAt, fallbackReset), true
}

func inspectClaudeCodeAccount(ctx context.Context, acc *ConnectedAccount, detail *AccountQuotaDetail, defaultResetText string) {
	detail.Tier = "Pro/Max"
	detail.EstimatedValueUSD = EstimateAccountValueUSD(*acc)

	sessionPct := 100
	weeklyPct := 100
	fableWeeklyPct := 100
	sessionReset := defaultResetText
	weeklyReset := "⏱️ Đặt lại sau 5d"
	fableReset := "⏱️ Đặt lại sau 5d"
	var extraSpendQuota *ModelQuotaInfo

	if acc.QuotaUsedPercent != nil {
		rem := int(math.Round(100.0 - *acc.QuotaUsedPercent))
		if rem < 0 {
			rem = 0
		} else if rem > 100 {
			rem = 100
		}
		sessionPct = rem
		weeklyPct = rem
		fableWeeklyPct = rem
	}
	if acc.QuotaResetTime != "" {
		sessionReset = acc.QuotaResetTime
	}

	// Orca Live Claude OAuth Usage Fetch (GET https://api.anthropic.com/api/oauth/usage)
	if resp, doErr := FetchClaudeUsageWithRefresh(ctx, acc); doErr == nil && resp != nil {
		if resp.StatusCode == http.StatusOK {
			var usageResp claudeOAuthUsageResponse
			if decErr := json.NewDecoder(resp.Body).Decode(&usageResp); decErr == nil {
				if rem, rst, ok := extractClaudeWindowRemaining(usageResp.FiveHour, sessionPct, sessionReset); ok {
					sessionPct = rem
					sessionReset = rst
				}
				if rem, rst, ok := extractClaudeWindowRemaining(usageResp.SevenDay, weeklyPct, weeklyReset); ok {
					weeklyPct = rem
					weeklyReset = rst
					fableWeeklyPct = rem
					fableReset = rst
				}
				// Check Fable / Mythos weekly scoped window (Orca mapFableWeeklyWindow)
				fableFound := false
				for _, lim := range usageResp.Limits {
					if lim.Kind == "weekly_scoped" && lim.Scope != nil && lim.Scope.Model != nil &&
						strings.EqualFold(strings.TrimSpace(lim.Scope.Model.DisplayName), "fable") {
						u := lim.Percent
						if u > 0 && u <= 1.0 {
							u *= 100.0
						}
						rem := int(math.Round(100.0 - u))
						if rem < 0 {
							rem = 0
						} else if rem > 100 {
							rem = 100
						}
						fableWeeklyPct = rem
						fableReset = parseClaudeResetAny(lim.ResetsAt, weeklyReset)
						fableFound = true
						break
					}
				}
				if !fableFound {
					for _, cand := range []*claudeUsageWindowInput{usageResp.FableWeekly, usageResp.FableSevenDay, usageResp.SevenDayFable} {
						if rem, rst, ok := extractClaudeWindowRemaining(cand, fableWeeklyPct, weeklyReset); ok {
							fableWeeklyPct = rem
							fableReset = rst
							break
						}
					}
				}
				// Check Extra Usage / Spend limit (Orca mapClaudeExtraUsage)
				if usageResp.Spend != nil && usageResp.Spend.Limit != nil && usageResp.Spend.Limit.AmountMinor != nil && *usageResp.Spend.Limit.AmountMinor > 0 {
					exp := 2
					if usageResp.Spend.Limit.Exponent != nil {
						exp = *usageResp.Spend.Limit.Exponent
					}
					div := math.Pow10(exp)
					limitUSD := *usageResp.Spend.Limit.AmountMinor / div
					usedUSD := 0.0
					if usageResp.Spend.Used != nil && usageResp.Spend.Used.AmountMinor != nil {
						usedUSD = *usageResp.Spend.Used.AmountMinor / div
					}
					remPct := 100
					if limitUSD > 0 {
						remPct = int(math.Round(100.0 - (usedUSD/limitUSD)*100.0))
						if remPct < 0 {
							remPct = 0
						} else if remPct > 100 {
							remPct = 100
						}
					}
					extraSpendQuota = &ModelQuotaInfo{
						ModelID:          "claude-extra-credits",
						DisplayName:      fmt.Sprintf("Extra Usage Credits ($%.2f / $%.2f)", usedUSD, limitUSD),
						RemainingPercent: remPct,
						ResetTime:        "⏱️ Hạn mức tín dụng tháng",
						Status:           getQuotaStatus(remPct),
					}
				}
			}
		} else if resp.StatusCode == http.StatusUnauthorized {
			if !acc.Disabled {
				detail.Status = "critical"
				detail.ErrorMessage = "OAuth token hết hạn — vui lòng kết nối lại Claude Code"
			}
		}
		_ = resp.Body.Close()
	}

	detail.SessionPercent = sessionPct
	detail.WeeklyPercent = weeklyPct
	detail.ResetTimeText = sessionReset
	minPct := sessionPct
	if weeklyPct < minPct {
		minPct = weeklyPct
	}
	if !acc.Disabled && detail.ErrorMessage == "" {
		if minPct <= 20 {
			detail.Status = "critical"
		} else if minPct <= 50 {
			detail.Status = "warning"
		} else {
			detail.Status = "healthy"
		}
	}

	fableEffectivePct := sessionPct
	if fableWeeklyPct < fableEffectivePct {
		fableEffectivePct = fableWeeklyPct
	}

	detail.SupportedModels = []string{
		"claude-code/claude-opus-5-5",
		"claude-code/claude-fable-5-1",
		"claude-code/claude-mythos-5-1",
		"claude-code/claude-sonnet-5-5",
		"claude-code/claude-haiku-5-5",
		"claude-code/claude-sonnet-5",
		"claude-code/claude-opus-5",
		"claude-code/claude-fable-5",
		"claude-code/claude-mythos-5",
		"claude-code/claude-opus-4-8",
		"claude-code/claude-opus-4-7",
		"claude-code/claude-opus-4-6-thinking",
		"claude-code/claude-sonnet-4-6",
		"claude-code/claude-haiku-4-5",
	}
	detail.ModelQuotas = []ModelQuotaInfo{
		{ModelID: "claude-window-5h", DisplayName: "Claude Session (Giới hạn 5h)", RemainingPercent: sessionPct, ResetTime: sessionReset, Status: getQuotaStatus(sessionPct)},
		{ModelID: "claude-window-7d", DisplayName: "Claude Weekly (Giới hạn 7 ngày)", RemainingPercent: weeklyPct, ResetTime: weeklyReset, Status: getQuotaStatus(weeklyPct)},
		{ModelID: "claude-code/claude-opus-5-5", DisplayName: "Opus 5.5 (Default • Extra)", RemainingPercent: sessionPct, ResetTime: sessionReset, Status: getQuotaStatus(sessionPct)},
		{ModelID: "claude-code/claude-fable-5-1", DisplayName: "Fable 5.1 / Mythos 5.1 (Weekly)", RemainingPercent: fableEffectivePct, ResetTime: fableReset, Status: getQuotaStatus(fableEffectivePct)},
		{ModelID: "claude-code/claude-sonnet-5-5", DisplayName: "Sonnet 5.5 / Haiku 5.5", RemainingPercent: sessionPct, ResetTime: sessionReset, Status: getQuotaStatus(sessionPct)},
		{ModelID: "claude-code/claude-sonnet-5", DisplayName: "Sonnet 5 / Opus 5 / Fable 5 / Mythos 5", RemainingPercent: sessionPct, ResetTime: sessionReset, Status: getQuotaStatus(sessionPct)},
		{ModelID: "claude-code/claude-opus-4-8", DisplayName: "Opus 4.8 / Opus 4.7", RemainingPercent: sessionPct, ResetTime: sessionReset, Status: getQuotaStatus(sessionPct)},
		{ModelID: "claude-code/claude-opus-4-6-thinking", DisplayName: "Opus 4.6 (Thinking) / Sonnet 4.6", RemainingPercent: sessionPct, ResetTime: sessionReset, Status: getQuotaStatus(sessionPct)},
		{ModelID: "claude-code/claude-haiku-4-5", DisplayName: "Haiku 4.5", RemainingPercent: sessionPct, ResetTime: sessionReset, Status: getQuotaStatus(sessionPct)},
	}
	if extraSpendQuota != nil {
		detail.ModelQuotas = append(detail.ModelQuotas, *extraSpendQuota)
	}
}

func inspectAlibabaAccount(ctx context.Context, acc *ConnectedAccount, detail *AccountQuotaDetail, defaultResetText string) {
	detail.Tier = "Token Plan"

	// Default usage is 72.24% if not explicitly calibrated by user
	used := 72.24
	if acc.QuotaUsedPercent != nil {
		used = *acc.QuotaUsedPercent
	}
	remainingPct := int(math.Round(100.0 - used))
	if remainingPct < 0 {
		remainingPct = 0
	} else if remainingPct > 100 {
		remainingPct = 100
	}

	resetText := "⏱️ Đặt lại: 2026-11-28 00:00 (UTC+8)"
	if acc.QuotaResetTime != "" {
		resetText = acc.QuotaResetTime
	}
	detail.ResetTimeText = resetText

	detail.SupportedModels = []string{
		"alibaba/auto",
		"alibaba/qwen3.8-max",
		"alibaba/qwen3.8-flash",
		"alibaba/qwen3.7-plus",
		"alibaba/qwen3.7-max",
		"alibaba/qwen3.6-flash",
		"alibaba/deepseek-v4.1-flash-1017",
		"alibaba/deepseek-v4.1-flash",
		"alibaba/deepseek-v4-pro-0813",
		"alibaba/deepseek-v4-pro",
		"alibaba/deepseek-v4-flash-0731",
		"alibaba/glm-5.3",
		"alibaba/glm-5.2",
		"alibaba/qwen3-coder-plus",
	}

	status := getQuotaStatus(remainingPct)

	detail.ModelQuotas = []ModelQuotaInfo{
		{
			ModelID:          "alibaba-plan-monthly",
			DisplayName:      fmt.Sprintf("Monthly Usage Plan (Đã dùng %.2f%%)", used),
			RemainingPercent: remainingPct,
			ResetTime:        resetText,
			Status:           status,
		},
		{
			ModelID:          "alibaba/qwen3.8-max",
			DisplayName:      "qwen3.8-max (⚡ 40% Off Nights)",
			RemainingPercent: remainingPct,
			ResetTime:        resetText,
			Status:           status,
		},
		{
			ModelID:          "alibaba/qwen3.8-flash",
			DisplayName:      "qwen3.8-flash (⚡ 40% Off Nights)",
			RemainingPercent: remainingPct,
			ResetTime:        resetText,
			Status:           status,
		},
		{
			ModelID:          "alibaba/deepseek-v4.1-flash-1017",
			DisplayName:      "deepseek-v4.1-flash-1017 (⚡ 50% Off Nights)",
			RemainingPercent: remainingPct,
			ResetTime:        resetText,
			Status:           status,
		},
		{
			ModelID:          "alibaba/deepseek-v4.1-flash",
			DisplayName:      "deepseek-v4.1-flash (⚡ 50% Off Nights)",
			RemainingPercent: remainingPct,
			ResetTime:        resetText,
			Status:           status,
		},
		{
			ModelID:          "alibaba/deepseek-v4-pro-0813",
			DisplayName:      "deepseek-v4-pro-0813 (⚡ Night 50% Off)",
			RemainingPercent: remainingPct,
			ResetTime:        resetText,
			Status:           status,
		},
		{
			ModelID:          "alibaba/deepseek-v4-flash-0731",
			DisplayName:      "deepseek-v4-flash-0731 (⚡ Night 50% Off)",
			RemainingPercent: remainingPct,
			ResetTime:        resetText,
			Status:           status,
		},
		{
			ModelID:          "alibaba/deepseek-v4-pro",
			DisplayName:      "deepseek-v4-pro / qwen3.7-max / plus",
			RemainingPercent: remainingPct,
			ResetTime:        resetText,
			Status:           status,
		},
		{
			ModelID:          "alibaba/glm-5.3",
			DisplayName:      "glm-5.3 / glm-5.2 / auto",
			RemainingPercent: remainingPct,
			ResetTime:        resetText,
			Status:           status,
		},
	}
}

type ccpaInternalBucket struct {
	BucketID          string   `json:"bucketId"`
	DisplayName       string   `json:"displayName"`
	Window            string   `json:"window"`
	ResetTime         string   `json:"resetTime"`
	Description       string   `json:"description"`
	RemainingFraction *float64 `json:"remainingFraction"`
	IsExhausted       bool     `json:"isExhausted"`
	ModelID           string   `json:"modelId"`
	TokenType         string   `json:"tokenType"`
}

type ccpaQuotaSummaryResponse struct {
	Groups []struct {
		DisplayName string               `json:"displayName"`
		Description string               `json:"description"`
		Buckets     []ccpaInternalBucket `json:"buckets"`
	} `json:"groups"`
}

type ccpaRetrieveModelQuotaEntry struct {
	RemainingFraction *float64                     `json:"remainingFraction"`
	ResetTime         string                       `json:"resetTime"`
	IsExhausted       bool                         `json:"isExhausted"`
	QuotaInfo         *ccpaAvailableModelQuotaInfo `json:"quotaInfo"`
}

type ccpaRetrieveQuotaResponse struct {
	Buckets []ccpaInternalBucket                   `json:"buckets"`
	Models  map[string]ccpaRetrieveModelQuotaEntry `json:"models"`
}

type ccpaAvailableModelQuotaInfo struct {
	RemainingFraction *float64 `json:"remainingFraction"`
	ResetTime         string   `json:"resetTime"`
	IsExhausted       bool     `json:"isExhausted"`
}

type ccpaAvailableModelEntry struct {
	ModelID     string                       `json:"modelId"`
	Model       string                       `json:"model"`
	DisplayName string                       `json:"displayName"`
	QuotaInfo   *ccpaAvailableModelQuotaInfo `json:"quotaInfo"`
}

type ccpaFetchAvailableModelsResponse struct {
	Models json.RawMessage `json:"models"`
}

func resolveCCPAFetchAvailableModelsEndpoint() string {
	if explicit := strings.TrimSpace(CCPAFetchAvailableModelsEndpoint); explicit != "" {
		return explicit
	}
	qURL := strings.TrimSpace(CCPARetrieveUserQuotaEndpoint)
	switch {
	case strings.Contains(qURL, ":retrieveUserQuota"):
		return strings.Replace(qURL, ":retrieveUserQuota", ":fetchAvailableModels", 1)
	case strings.Contains(qURL, "/retrieveQuota"):
		return strings.Replace(qURL, "/retrieveQuota", "/fetchAvailableModels", 1)
	default:
		return ""
	}
}

func parseCCPARemainingPercent(frac *float64, isExhausted bool, resetTime string) (int, bool) {
	if isExhausted {
		return 0, true
	}
	if frac != nil {
		pct := int(math.Round(*frac * 100.0))
		if pct < 0 {
			pct = 0
		} else if pct > 100 {
			pct = 100
		}
		return pct, true
	}
	// In Google CCPA proto3 JSON, remainingFraction == 0.0 is omitted while resetTime is present.
	if strings.TrimSpace(resetTime) != "" {
		return 0, true
	}
	return 0, false
}

func detectAntigravityTierFromCodeAssist(currID, currName, currDesc, paidID, paidName, paidDesc, label string) string {
	paidAll := strings.ToLower(strings.TrimSpace(paidID + " " + paidName + " " + paidDesc))
	currAll := strings.ToLower(strings.TrimSpace(currID + " " + currName + " " + currDesc))
	combined := strings.TrimSpace(paidAll + " " + currAll)

	switch {
	case strings.Contains(combined, "ultra"):
		return "Ultra"
	case strings.Contains(combined, "pro") || strings.Contains(combined, "premium"):
		return "Pro"
	case strings.Contains(currAll, "free") || strings.Contains(currAll, "lite"):
		return "Lite"
	case strings.Contains(currAll, "standard"):
		return "Pro"
	default:
		if fromLbl := inferTierFromLabel(label); fromLbl != "" {
			return fromLbl
		}
		return ""
	}
}

type agyBucketTrack struct {
	pct     int
	reset   string
	hasLive bool
}

func (t *agyBucketTrack) update(pct int, rst string) {
	if !t.hasLive || pct < t.pct {
		t.pct = pct
		if strings.TrimSpace(rst) != "" && rst != "⏱️ Đặt lại ngay" {
			t.reset = rst
		} else if t.reset == "" {
			t.reset = rst
		}
		t.hasLive = true
	}
}

type agyLiveQuotaState struct {
	geminiPro    agyBucketTrack
	geminiFlash  agyBucketTrack
	claudeOpus   agyBucketTrack
	claudeSonnet agyBucketTrack
	geminiImage  agyBucketTrack
	geminiLite   agyBucketTrack
}

func applyCCPAModelBucketToState(rawModelID string, pct int, rst string, st *agyLiveQuotaState) {
	m := strings.ToLower(strings.TrimSpace(rawModelID))
	m = strings.TrimPrefix(m, "models/")
	m = strings.TrimPrefix(m, "antigravity/")
	if m == "" {
		return
	}
	switch {
	case strings.Contains(m, "claude-opus") || m == "model_placeholder_m26" || strings.Contains(m, "gpt-oss"):
		st.claudeOpus.update(pct, rst)
	case strings.Contains(m, "claude-sonnet") || m == "model_placeholder_m35":
		st.claudeSonnet.update(pct, rst)
	case strings.Contains(m, "image") || strings.Contains(m, "imagen"):
		st.geminiImage.update(pct, rst)
	case strings.Contains(m, "flash-lite"):
		st.geminiLite.update(pct, rst)
	case strings.Contains(m, "flash") || m == "model_placeholder_m18" || m == "model_placeholder_m47":
		st.geminiFlash.update(pct, rst)
	case strings.Contains(m, "carbon") || strings.Contains(m, "argon") ||
		strings.Contains(m, "-pro") || strings.HasSuffix(m, "pro") ||
		m == "model_placeholder_m37" || m == "model_placeholder_m417" || m == "model_placeholder_m420":
		st.geminiPro.update(pct, rst)
	}
}

func normalizeSupportedCCPAModel(rawID string) string {
	clean := strings.TrimSpace(rawID)
	clean = strings.TrimPrefix(clean, "models/")
	lower := strings.ToLower(clean)
	if lower == "" || strings.HasPrefix(lower, "chat_") || strings.HasPrefix(lower, "tab_") || strings.HasPrefix(lower, "text-embedding") {
		return ""
	}
	switch lower {
	case "model_placeholder_m26":
		return "antigravity/claude-opus-4-6-thinking"
	case "model_placeholder_m35":
		return "antigravity/claude-sonnet-4-6"
	}
	if strings.HasPrefix(lower, "model_placeholder_") {
		return clean
	}
	if strings.Contains(clean, "/") {
		return clean
	}
	return "antigravity/" + clean
}

func inspectAntigravityAccount(ctx context.Context, acc *ConnectedAccount, detail *AccountQuotaDetail, defaultResetText string) {
	// Auto-refresh token if expired
	if acc.RefreshToken != "" && time.Now().Unix() >= acc.ExpiresAt-60 {
		if err := RefreshAntigravityToken(ctx, acc); err == nil {
			_, _ = UpsertConnectedAccount(*acc)
		}
	}

	scopeKey := strings.TrimSpace(acc.Email)
	if scopeKey == "" {
		scopeKey = strings.TrimSpace(acc.ID)
	}
	client := AccountHTTPClient(scopeKey, acc.ProxyURL, 4*time.Second)

	// 1. Call loadCodeAssist to detect tier and project
	callCodeAssist := func() (*http.Response, error) {
		projArg := acc.ProjectID
		if projArg == "aicode-consumers" {
			projArg = ""
		}
		payload := map[string]any{
			"cloudaicompanionProject": projArg,
			"metadata": map[string]any{
				"ideType": "ANTIGRAVITY",
			},
		}
		raw, _ := json.Marshal(payload)
		req, err := http.NewRequestWithContext(ctx, "POST", CCPALoadCodeAssistEndpoint, bytes.NewReader(raw))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+acc.AccessToken)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", GetAgyUserAgent())
		return client.Do(req)
	}

	resp, err := callCodeAssist()
	if err != nil {
		detail.Tier = LookupCachedAccountTier(*acc, nil)
		detail.Status = "warning"
		detail.ErrorMessage = err.Error()
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		if acc.RefreshToken != "" && RefreshAntigravityToken(ctx, acc) == nil {
			_, _ = UpsertConnectedAccount(*acc)
			resp2, err2 := callCodeAssist()
			if err2 == nil {
				defer resp2.Body.Close()
				if resp2.StatusCode == http.StatusOK {
					resp = resp2
				}
			}
		}
		if resp.StatusCode == http.StatusUnauthorized {
			RecordAccountResult(acc.Email, ProviderAntigravity, false, 0, 0, http.StatusUnauthorized)
			detail.Tier = LookupCachedAccountTier(*acc, nil)
			detail.Status = "critical"
			detail.ErrorMessage = "Token xác thực hết hạn hoặc không hợp lệ (HTTP 401)"
			return
		}
	}

	if resp.StatusCode >= 400 {
		detail.Tier = LookupCachedAccountTier(*acc, nil)
		detail.Status = "critical"
		detail.ErrorMessage = fmt.Sprintf("Lỗi kết nối CCPA (HTTP %d)", resp.StatusCode)
		return
	}

	var codeAssistBody struct {
		CurrentTier struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"currentTier"`
		PaidTier struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"paidTier"`
		IneligibleTiers []struct {
			ReasonCode             string `json:"reasonCode"`
			ReasonMessage          string `json:"reasonMessage"`
			ValidationErrorMessage string `json:"validationErrorMessage"`
			ValidationURL          string `json:"validationUrl"`
		} `json:"ineligibleTiers"`
		CloudaicompanionProject string `json:"cloudaicompanionProject"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&codeAssistBody)

	tierName := detectAntigravityTierFromCodeAssist(
		codeAssistBody.CurrentTier.ID, codeAssistBody.CurrentTier.Name, codeAssistBody.CurrentTier.Description,
		codeAssistBody.PaidTier.ID, codeAssistBody.PaidTier.Name, codeAssistBody.PaidTier.Description,
		acc.Label,
	)
	if tierName == "" {
		tierName = LookupCachedAccountTier(*acc, nil)
	}
	detail.Tier = tierName
	RememberAccountTier(acc.Email, acc.ID, tierName)
	syncAccountTierIfChanged(acc, tierName)

	if (acc.ProjectID == "" || acc.ProjectID == "aicode-consumers") && codeAssistBody.CloudaicompanionProject != "" {
		acc.ProjectID = codeAssistBody.CloudaicompanionProject
		_, _ = UpsertConnectedAccount(*acc)
	}

	acctKey := strings.TrimSpace(acc.Email)
	if acctKey == "" {
		acctKey = strings.TrimSpace(acc.ID)
	}
	acctKeyLower := strings.ToLower(acctKey)
	foundValidation := false
	foundPermanentBan := false
	for _, t := range codeAssistBody.IneligibleTiers {
		rc := strings.ToUpper(strings.TrimSpace(t.ReasonCode))
		if strings.Contains(rc, "TOS_VIOLATION") || strings.Contains(rc, "DISABLED") || strings.Contains(rc, "BANNED") {
			foundPermanentBan = true
			detail.ErrorMessage = "Tài khoản bị Google vô hiệu hóa dịch vụ (HTTP 403 TOS_VIOLATION)"
			detail.Status = "critical"
			detail.InCooldown = true
			break
		}
		if rc == "VALIDATION_REQUIRED" || strings.TrimSpace(t.ValidationURL) != "" {
			foundValidation = true
			detail.ValidationURL = strings.TrimSpace(t.ValidationURL)
			detail.ErrorMessage = "Google yêu cầu xác minh danh tính tài khoản (HTTP 403 VALIDATION_REQUIRED)"
			if !acc.Disabled {
				detail.Status = "warning"
				detail.InCooldown = true
			}
			if acctKeyLower != "" {
				validationIsolatedAccounts.Store(acctKeyLower, true)
			}
			SyncAccountValidationState(acctKey, true)
			break
		}
	}
	if !foundValidation && !foundPermanentBan && strings.TrimSpace(codeAssistBody.CurrentTier.ID) != "" {
		_, trackedVal := validationIsolatedAccounts.Load(acctKeyLower)
		// Only auto-unlock explicitly tracked VALIDATION_REQUIRED isolations; never wipe 24h (86400s) TOS_VIOLATION or streaming 403 bans!
		isValidationWindow := trackedVal
		if isValidationWindow {
			validationIsolatedAccounts.Delete(acctKeyLower)
			SyncAccountValidationState(acctKey, false)
			acc.CooldownUntil = 0
			acc.FailureCount = 0
			detail.InCooldown = false
			detail.FailureCount = 0
		}
	}

	// 2. Call retrieveUserQuotaSummary for weekly & 5h limits
	claudeWeeklyPercent := 53
	claude5hPercent := 97
	hasClaude5hFromSummary := false
	claudeWeeklyReset := "⏱️ Đặt lại sau 5d"
	claude5hReset := "⏱️ Đặt lại sau 2h 55m"
	geminiWeeklyFromSummary := -1
	geminiWeeklyResetFromSummary := ""

	summaryPayload := map[string]any{"project": acc.ProjectID}
	if strings.TrimSpace(acc.ProjectID) == "" {
		summaryPayload = map[string]any{}
	}
	summaryRaw, _ := json.Marshal(summaryPayload)
	reqSummary, errSum := http.NewRequestWithContext(ctx, "POST", CCPARetrieveUserQuotaSummaryEndpoint, bytes.NewReader(summaryRaw))
	if errSum == nil {
		reqSummary.Header.Set("Authorization", "Bearer "+acc.AccessToken)
		reqSummary.Header.Set("Content-Type", "application/json")
		reqSummary.Header.Set("User-Agent", GetAgyUserAgent())
		respSummary, errSumDo := client.Do(reqSummary)
		if errSumDo == nil && respSummary.StatusCode == http.StatusOK {
			defer respSummary.Body.Close()
			var qsResp ccpaQuotaSummaryResponse
			if errDec := json.NewDecoder(respSummary.Body).Decode(&qsResp); errDec == nil {
				for _, g := range qsResp.Groups {
					isGeminiGroup := strings.Contains(strings.ToLower(g.DisplayName), "gemini")
					for _, b := range g.Buckets {
						pct, okPct := parseCCPARemainingPercent(b.RemainingFraction, b.IsExhausted, b.ResetTime)
						if !okPct {
							continue
						}
						if isGeminiGroup || strings.Contains(strings.ToLower(b.BucketID), "gemini") {
							if b.Window == "weekly" || strings.Contains(strings.ToLower(b.BucketID), "weekly") {
								geminiWeeklyFromSummary = pct
								geminiWeeklyResetFromSummary = formatResetDuration(b.ResetTime, "⏱️ Đặt lại sau 5d")
							}
							continue
						}
						if b.BucketID == "3p-weekly" || b.Window == "weekly" {
							claudeWeeklyPercent = pct
							claudeWeeklyReset = formatResetDuration(b.ResetTime, "⏱️ Đặt lại sau 5d")
							if (pct <= 0 || b.IsExhausted) && strings.TrimSpace(b.ResetTime) != "" {
								if rAt, err := time.Parse(time.RFC3339, strings.TrimSpace(b.ResetTime)); err == nil && rAt.After(time.Now()) {
									RecordAccountModelRateLimitUntil(acc.Email, "claude-opus-4-6-thinking", rAt)
									RecordAccountModelRateLimitUntil(acc.Email, "claude-sonnet-4-6", rAt)
								}
							}
						} else if b.BucketID == "3p-5h" || b.Window == "5h" {
							claude5hPercent = pct
							hasClaude5hFromSummary = true
							if strings.TrimSpace(b.ResetTime) != "" {
								claude5hReset = formatResetDuration(b.ResetTime, "⏱️ Đặt lại sau 2h 55m")
								if pct <= 0 || b.IsExhausted {
									if rAt, err := time.Parse(time.RFC3339, strings.TrimSpace(b.ResetTime)); err == nil && rAt.After(time.Now()) {
										RecordAccountModelRateLimitUntil(acc.Email, "claude-opus-4-6-thinking", rAt)
										RecordAccountModelRateLimitUntil(acc.Email, "claude-sonnet-4-6", rAt)
									}
								}
							}
						}
					}
				}
			}
		}
	}

	if claudeWeeklyPercent <= 0 || (hasClaude5hFromSummary && claude5hPercent <= 0) {
		RecordAccountModelRateLimit(acc.Email, "claude-opus-4-6-thinking", 6*24*time.Hour)
		RecordAccountModelRateLimit(acc.Email, "claude-sonnet-4-6", 6*24*time.Hour)
		zeroPct := 0
		acc.WeeklyClaudePercent = &zeroPct
	} else {
		remPct := claudeWeeklyPercent
		if hasClaude5hFromSummary && claude5hPercent < remPct {
			remPct = claude5hPercent
		}
		acc.WeeklyClaudePercent = &remPct
		ClearAccountModelRateLimit(acc.Email, "claude-opus-4-6-thinking")
		ClearAccountModelRateLimit(acc.Email, "claude-sonnet-4-6")
	}
	syncAccountClaudePercentIfChanged(acc, *acc.WeeklyClaudePercent)

	// 3. Call fetchAvailableModels & retrieveUserQuota for live per-model buckets and supported model IDs
	var liveState agyLiveQuotaState
	discoveredSet := make(map[string]struct{})
	addDiscoveredModel := func(rawID string) {
		if norm := normalizeSupportedCCPAModel(rawID); norm != "" {
			discoveredSet[norm] = struct{}{}
		}
	}
	quotaSyncNow := time.Now()
	syncModelQuotaRateLimit := func(rawModelID string, remainingFrac *float64, isExhausted bool, resetTimeStr string) {
		if strings.TrimSpace(acc.Email) == "" {
			return
		}
		cleanID := strings.ToLower(strings.TrimSpace(rawModelID))
		cleanID = strings.TrimPrefix(cleanID, "models/")
		cleanID = strings.TrimPrefix(cleanID, "antigravity/")
		if cleanID == "" ||
			strings.HasPrefix(cleanID, "chat_") ||
			strings.HasPrefix(cleanID, "tab_") ||
			strings.HasPrefix(cleanID, "text-embedding") ||
			strings.HasPrefix(cleanID, "3p-") ||
			strings.HasPrefix(cleanID, "group-") {
			return
		}
		normID := normalizeRateLimitModelID(cleanID)
		if normID == "" || strings.HasPrefix(normID, "model_placeholder_") {
			return
		}
		trimmedReset := strings.TrimSpace(resetTimeStr)
		// Proto3 omits remainingFraction when 0: nil or <= 0 accompanied by future resetTime means exhausted.
		if (isExhausted || remainingFrac == nil || *remainingFrac <= 0) && trimmedReset != "" {
			resetAt, err := time.Parse(time.RFC3339, trimmedReset)
			if err != nil {
				resetAt, err = time.Parse(time.RFC3339Nano, trimmedReset)
			}
			if err == nil && resetAt.After(quotaSyncNow) {
				RecordAccountModelRateLimitUntil(acc.Email, normID, resetAt)
			}
			return
		}
		if !isExhausted && remainingFrac != nil && *remainingFrac > 0.01 {
			ClearAccountModelRateLimit(acc.Email, normID)
		}
	}

	if fetchModelsURL := resolveCCPAFetchAvailableModelsEndpoint(); fetchModelsURL != "" {
		reqModels, errMod := http.NewRequestWithContext(ctx, "POST", fetchModelsURL, bytes.NewReader(summaryRaw))
		if errMod == nil {
			reqModels.Header.Set("Authorization", "Bearer "+acc.AccessToken)
			reqModels.Header.Set("Content-Type", "application/json")
			reqModels.Header.Set("User-Agent", GetAgyUserAgent())
			respModels, errModDo := client.Do(reqModels)
			if errModDo == nil && respModels.StatusCode == http.StatusOK {
				defer respModels.Body.Close()
				var fmResp ccpaFetchAvailableModelsResponse
				if json.NewDecoder(respModels.Body).Decode(&fmResp) == nil && len(fmResp.Models) > 0 {
					var modelMap map[string]ccpaAvailableModelEntry
					if json.Unmarshal(fmResp.Models, &modelMap) == nil && len(modelMap) > 0 {
						keys := make([]string, 0, len(modelMap))
						for k := range modelMap {
							keys = append(keys, k)
						}
						sort.Strings(keys)
						for _, k := range keys {
							entry := modelMap[k]
							id := k
							if strings.TrimSpace(id) == "" {
								id = entry.ModelID
							}
							addDiscoveredModel(id)
							if entry.Model != "" {
								addDiscoveredModel(entry.Model)
							}
							if entry.QuotaInfo != nil {
								syncModelQuotaRateLimit(id, entry.QuotaInfo.RemainingFraction, entry.QuotaInfo.IsExhausted, entry.QuotaInfo.ResetTime)
								if pct, okPct := parseCCPARemainingPercent(entry.QuotaInfo.RemainingFraction, entry.QuotaInfo.IsExhausted, entry.QuotaInfo.ResetTime); okPct {
									rst := formatResetDuration(entry.QuotaInfo.ResetTime, defaultResetText)
									applyCCPAModelBucketToState(id, pct, rst, &liveState)
								}
							}
						}
					} else {
						var modelSlice []ccpaAvailableModelEntry
						if json.Unmarshal(fmResp.Models, &modelSlice) == nil {
							for _, entry := range modelSlice {
								id := entry.ModelID
								if id == "" {
									id = entry.Model
								}
								addDiscoveredModel(id)
								if entry.QuotaInfo != nil {
									syncModelQuotaRateLimit(id, entry.QuotaInfo.RemainingFraction, entry.QuotaInfo.IsExhausted, entry.QuotaInfo.ResetTime)
									if pct, okPct := parseCCPARemainingPercent(entry.QuotaInfo.RemainingFraction, entry.QuotaInfo.IsExhausted, entry.QuotaInfo.ResetTime); okPct {
										rst := formatResetDuration(entry.QuotaInfo.ResetTime, defaultResetText)
										applyCCPAModelBucketToState(id, pct, rst, &liveState)
									}
								}
							}
						}
					}
				}
			} else if errModDo == nil && respModels != nil {
				_ = respModels.Body.Close()
			}
		}
	}

	reqQuota, errQuota := http.NewRequestWithContext(ctx, "POST", CCPARetrieveUserQuotaEndpoint, bytes.NewReader(summaryRaw))
	if errQuota == nil {
		reqQuota.Header.Set("Authorization", "Bearer "+acc.AccessToken)
		reqQuota.Header.Set("Content-Type", "application/json")
		reqQuota.Header.Set("User-Agent", GetAgyUserAgent())
		respQuota, errQuotaDo := client.Do(reqQuota)
		if errQuotaDo == nil && respQuota.StatusCode == http.StatusOK {
			defer respQuota.Body.Close()
			var qResp ccpaRetrieveQuotaResponse
			if errDec := json.NewDecoder(respQuota.Body).Decode(&qResp); errDec == nil {
				// Authoritative per-bucket / per-model response overrides initial fetchAvailableModels values
				var quotaBucketsState agyLiveQuotaState
				for _, b := range qResp.Buckets {
					id := b.ModelID
					if id == "" {
						id = b.BucketID
					}
					addDiscoveredModel(id)
					syncModelQuotaRateLimit(id, b.RemainingFraction, b.IsExhausted, b.ResetTime)
					pct, okPct := parseCCPARemainingPercent(b.RemainingFraction, b.IsExhausted, b.ResetTime)
					if !okPct {
						continue
					}
					rst := formatResetDuration(b.ResetTime, defaultResetText)
					applyCCPAModelBucketToState(id, pct, rst, &quotaBucketsState)
				}
				if len(qResp.Models) > 0 {
					mKeys := make([]string, 0, len(qResp.Models))
					for mk := range qResp.Models {
						mKeys = append(mKeys, mk)
					}
					sort.Strings(mKeys)
					for _, modelID := range mKeys {
						info := qResp.Models[modelID]
						frac := info.RemainingFraction
						exhausted := info.IsExhausted
						rstRaw := info.ResetTime
						if info.QuotaInfo != nil {
							if frac == nil {
								frac = info.QuotaInfo.RemainingFraction
							}
							if !exhausted {
								exhausted = info.QuotaInfo.IsExhausted
							}
							if rstRaw == "" {
								rstRaw = info.QuotaInfo.ResetTime
							}
						}
						addDiscoveredModel(modelID)
						syncModelQuotaRateLimit(modelID, frac, exhausted, rstRaw)
						if pct, okPct := parseCCPARemainingPercent(frac, exhausted, rstRaw); okPct {
							rst := formatResetDuration(rstRaw, defaultResetText)
							applyCCPAModelBucketToState(modelID, pct, rst, &quotaBucketsState)
						}
					}
				}
				if quotaBucketsState.geminiPro.hasLive {
					liveState.geminiPro = quotaBucketsState.geminiPro
				}
				if quotaBucketsState.geminiFlash.hasLive {
					liveState.geminiFlash = quotaBucketsState.geminiFlash
				}
				if quotaBucketsState.claudeOpus.hasLive {
					liveState.claudeOpus = quotaBucketsState.claudeOpus
				}
				if quotaBucketsState.claudeSonnet.hasLive {
					liveState.claudeSonnet = quotaBucketsState.claudeSonnet
				}
				if quotaBucketsState.geminiImage.hasLive {
					liveState.geminiImage = quotaBucketsState.geminiImage
				}
				if quotaBucketsState.geminiLite.hasLive {
					liveState.geminiLite = quotaBucketsState.geminiLite
				}
			}
		} else if errQuotaDo == nil && respQuota != nil {
			_ = respQuota.Body.Close()
		}
	}

	geminiProPercent := 98
	geminiProReset := "⏱️ Đặt lại sau 3h 26m"
	if liveState.geminiPro.hasLive {
		geminiProPercent = liveState.geminiPro.pct
		if liveState.geminiPro.reset != "" {
			geminiProReset = liveState.geminiPro.reset
		}
	}

	geminiFlashPercent := 98
	geminiFlashReset := geminiProReset
	if liveState.geminiFlash.hasLive {
		geminiFlashPercent = liveState.geminiFlash.pct
		if liveState.geminiFlash.reset != "" {
			geminiFlashReset = liveState.geminiFlash.reset
		}
	} else if liveState.geminiPro.hasLive {
		geminiFlashPercent = geminiProPercent
	}

	geminiPercent := geminiProPercent
	geminiReset := geminiProReset
	if !liveState.geminiPro.hasLive && liveState.geminiFlash.hasLive {
		geminiProPercent = geminiFlashPercent
		geminiProReset = geminiFlashReset
		geminiPercent = geminiFlashPercent
		geminiReset = geminiFlashReset
	}

	claudeOpusPercent := claude5hPercent
	if liveState.claudeOpus.hasLive {
		claudeOpusPercent = liveState.claudeOpus.pct
		if liveState.claudeOpus.reset != "" && liveState.claudeOpus.reset != "⏱️ Đặt lại ngay" {
			claude5hReset = liveState.claudeOpus.reset
		}
	}
	claudeSonnetPercent := claude5hPercent
	if liveState.claudeSonnet.hasLive {
		claudeSonnetPercent = liveState.claudeSonnet.pct
		if !liveState.claudeOpus.hasLive && liveState.claudeSonnet.reset != "" && liveState.claudeSonnet.reset != "⏱️ Đặt lại ngay" {
			claude5hReset = liveState.claudeSonnet.reset
		}
	}
	if !hasClaude5hFromSummary && (liveState.claudeOpus.hasLive || liveState.claudeSonnet.hasLive) {
		switch {
		case liveState.claudeOpus.hasLive && liveState.claudeSonnet.hasLive:
			if claudeOpusPercent < claudeSonnetPercent {
				claude5hPercent = claudeOpusPercent
			} else {
				claude5hPercent = claudeSonnetPercent
			}
		case liveState.claudeOpus.hasLive:
			claude5hPercent = claudeOpusPercent
		default:
			claude5hPercent = claudeSonnetPercent
		}
	}

	geminiImagePercent := 98
	geminiImageReset := geminiReset
	if liveState.geminiImage.hasLive {
		geminiImagePercent = liveState.geminiImage.pct
		if liveState.geminiImage.reset != "" {
			geminiImageReset = liveState.geminiImage.reset
		}
	}
	geminiLitePercent := geminiFlashPercent
	geminiLiteReset := geminiFlashReset
	if liveState.geminiLite.hasLive {
		geminiLitePercent = liveState.geminiLite.pct
		if liveState.geminiLite.reset != "" {
			geminiLiteReset = liveState.geminiLite.reset
		}
	}

	geminiWeeklyPercent := 84
	if geminiWeeklyFromSummary >= 0 {
		geminiWeeklyPercent = geminiWeeklyFromSummary
	} else if acc.WeeklyGeminiPercent != nil && *acc.WeeklyGeminiPercent > 0 {
		geminiWeeklyPercent = *acc.WeeklyGeminiPercent
	} else if acc.QuotaUsedPercent != nil && *acc.QuotaUsedPercent > 0 {
		geminiWeeklyPercent = int(math.Round(100.0 - *acc.QuotaUsedPercent))
	} else if acc.TotalTokens > 25000 {
		deduct := int((acc.TotalTokens - 25000) / 50000)
		geminiWeeklyPercent = 84 - deduct
	}
	if geminiWeeklyPercent < 0 {
		geminiWeeklyPercent = 0
	} else if geminiWeeklyPercent > 100 {
		geminiWeeklyPercent = 100
	}
	geminiWeeklyReset := claudeWeeklyReset
	if geminiWeeklyResetFromSummary != "" {
		geminiWeeklyReset = geminiWeeklyResetFromSummary
	}
	detail.SessionPercent = geminiPercent
	detail.WeeklyPercent = geminiWeeklyPercent

	baseSupported := []string{
		"antigravity/gemini-4-carbon-high",
		"antigravity/gemini-4-carbon-medium",
		"antigravity/gemini-4-argon-high",
		"antigravity/gemini-3.8-flash-high",
		"antigravity/gemini-3.8-flash-medium",
		"antigravity/gemini-3-flash",
		"antigravity/gemini-3.1-flash-image",
		"antigravity/claude-opus-4-6-thinking",
		"antigravity/claude-sonnet-4-6",
		"antigravity/gemini-3.5-flash-lite",
	}
	seenSupp := make(map[string]struct{}, len(baseSupported)+len(discoveredSet))
	supported := make([]string, 0, len(baseSupported)+len(discoveredSet))
	for _, m := range baseSupported {
		seenSupp[strings.ToLower(m)] = struct{}{}
		supported = append(supported, m)
	}
	if len(discoveredSet) > 0 {
		extra := make([]string, 0, len(discoveredSet))
		for m := range discoveredSet {
			if _, exists := seenSupp[strings.ToLower(m)]; !exists {
				extra = append(extra, m)
			}
		}
		sort.Strings(extra)
		supported = append(supported, extra...)
	}
	detail.SupportedModels = supported

	detail.ResetTimeText = geminiReset

	// Match Antigravity native limits popup and real per-model quotas (Pro vs Flash vs Claude separated!)
	detail.ModelQuotas = []ModelQuotaInfo{
		{
			ModelID:          "group-gemini-5h",
			DisplayName:      "Gemini Models (Giới hạn 5h)",
			RemainingPercent: geminiPercent,
			ResetTime:        geminiReset,
			Status:           getQuotaStatus(geminiPercent),
		},
		{
			ModelID:          "group-gemini-weekly",
			DisplayName:      "Gemini Models (Giới hạn Tuần)",
			RemainingPercent: geminiWeeklyPercent,
			ResetTime:        geminiWeeklyReset,
			Status:           getQuotaStatus(geminiWeeklyPercent),
		},
		{
			ModelID:          "group-claude-5h",
			DisplayName:      "Claude & GPT (Giới hạn 5h)",
			RemainingPercent: claude5hPercent,
			ResetTime:        claude5hReset,
			Status:           getQuotaStatus(claude5hPercent),
		},
		{
			ModelID:          "group-claude-weekly",
			DisplayName:      "Claude & GPT (Giới hạn Tuần)",
			RemainingPercent: claudeWeeklyPercent,
			ResetTime:        claudeWeeklyReset,
			Status:           getQuotaStatus(claudeWeeklyPercent),
		},
		{
			ModelID:          "antigravity/gemini-4-carbon-high",
			DisplayName:      "Gemini 4 Carbon (High)",
			RemainingPercent: geminiProPercent,
			ResetTime:        geminiProReset,
			Status:           getQuotaStatus(geminiProPercent),
		},
		{
			ModelID:          "gemini-4-argon-high",
			DisplayName:      "Gemini 4 Argon (High)",
			RemainingPercent: geminiProPercent,
			ResetTime:        geminiProReset,
			Status:           getQuotaStatus(geminiProPercent),
		},
		{
			ModelID:          "gemini-3.8-flash-high",
			DisplayName:      "Gemini 3.8 Flash (High)",
			RemainingPercent: geminiFlashPercent,
			ResetTime:        geminiFlashReset,
			Status:           getQuotaStatus(geminiFlashPercent),
		},
		{
			ModelID:          "gemini-3-flash",
			DisplayName:      "Gemini 3 Flash",
			RemainingPercent: geminiFlashPercent,
			ResetTime:        geminiFlashReset,
			Status:           getQuotaStatus(geminiFlashPercent),
		},
		{
			ModelID:          "claude-opus-4-6-thinking",
			DisplayName:      "Claude Opus 4.6 (Thinking)",
			RemainingPercent: claudeOpusPercent,
			ResetTime:        claude5hReset,
			Status:           getQuotaStatus(claudeOpusPercent),
		},
		{
			ModelID:          "claude-sonnet-4-6",
			DisplayName:      "Claude Sonnet 4.6",
			RemainingPercent: claudeSonnetPercent,
			ResetTime:        claude5hReset,
			Status:           getQuotaStatus(claudeSonnetPercent),
		},
		{
			ModelID:          "gemini-3.1-flash-image",
			DisplayName:      "Gemini 3.1 Flash Image (Imagen 3)",
			RemainingPercent: geminiImagePercent,
			ResetTime:        geminiImageReset,
			Status:           getQuotaStatus(geminiImagePercent),
		},
		{
			ModelID:          "gemini-3.5-flash-lite",
			DisplayName:      "Gemini 3.5 Flash Lite",
			RemainingPercent: geminiLitePercent,
			ResetTime:        geminiLiteReset,
			Status:           getQuotaStatus(geminiLitePercent),
		},
	}
}

// ToggleAccountEnabled toggles an account's enabled state in configuration.
func ToggleAccountEnabled(accountID string, enabled bool) error {
	configMu.Lock()
	defer configMu.Unlock()

	cfg := loadSavedAuthConfigLocked()
	found := false
	for i := range cfg.Accounts {
		if cfg.Accounts[i].ID == accountID || cfg.Accounts[i].Email == accountID {
			cfg.Accounts[i].Disabled = !enabled
			cfg.Accounts[i].UpdatedAt = time.Now().UTC().Format(time.RFC3339)
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("account %s not found", accountID)
	}

	err := saveAuthConfigLocked(cfg)
	if err == nil {
		triggerBackgroundQuotaRefresh()
	}
	return err
}

// UpdateAccountThreshold updates custom quota used percentage and reset time for an account.
func UpdateAccountThreshold(accountID string, usedPercent *float64, resetTime string) error {
	configMu.Lock()
	defer configMu.Unlock()

	cfg := loadSavedAuthConfigLocked()
	found := false
	for i := range cfg.Accounts {
		if cfg.Accounts[i].ID == accountID || cfg.Accounts[i].Email == accountID {
			if usedPercent != nil {
				cfg.Accounts[i].QuotaUsedPercent = usedPercent
				if cfg.Accounts[i].Provider == "" || cfg.Accounts[i].Provider == ProviderAntigravity {
					rem := int(math.Round(100.0 - *usedPercent))
					if rem < 0 {
						rem = 0
					} else if rem > 100 {
						rem = 100
					}
					cfg.Accounts[i].WeeklyGeminiPercent = &rem
				}
			}
			if strings.TrimSpace(resetTime) != "" {
				cfg.Accounts[i].QuotaResetTime = strings.TrimSpace(resetTime)
			}
			cfg.Accounts[i].UpdatedAt = time.Now().UTC().Format(time.RFC3339)
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("account %s not found", accountID)
	}

	err := saveAuthConfigLocked(cfg)
	if err == nil {
		triggerBackgroundQuotaRefresh()
	}
	return err
}

// --- HTTP API Handlers for Quota ---

func (s *Server) handleQuotaSummary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeOpenAIError(w, http.StatusMethodNotAllowed, "only GET is supported", "invalid_request_error")
		return
	}

	force := r.URL.Query().Get("refresh") == "true"
	summary := GetQuotaSummary(r.Context(), force)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(summary)
}

func (s *Server) handleQuotaToggle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeOpenAIError(w, http.StatusMethodNotAllowed, "only POST is supported", "invalid_request_error")
		return
	}

	var req struct {
		AccountID string `json:"account_id"`
		Enabled   bool   `json:"enabled"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 16*1024)).Decode(&req); err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "invalid JSON payload: "+err.Error(), "invalid_request_error")
		return
	}

	if err := ToggleAccountEnabled(req.AccountID, req.Enabled); err != nil {
		writeOpenAIError(w, http.StatusBadRequest, err.Error(), "toggle_error")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":     "ok",
		"account_id": req.AccountID,
		"enabled":    req.Enabled,
	})
}

func (s *Server) handleQuotaThreshold(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeOpenAIError(w, http.StatusMethodNotAllowed, "only POST is supported", "invalid_request_error")
		return
	}

	var req struct {
		Action      string   `json:"action"`
		AccountID   string   `json:"account_id"`
		UsedPercent *float64 `json:"used_percent"`
		ResetTime   string   `json:"reset_time"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 16*1024)).Decode(&req); err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "invalid JSON payload: "+err.Error(), "invalid_request_error")
		return
	}

	if req.Action == "reset_latency" {
		if err := ResetAccountLatencyStats(req.AccountID); err != nil {
			writeOpenAIError(w, http.StatusBadRequest, err.Error(), "reset_latency_error")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":     "ok",
			"account_id": req.AccountID,
		})
		return
	}

	if err := UpdateAccountThreshold(req.AccountID, req.UsedPercent, req.ResetTime); err != nil {
		writeOpenAIError(w, http.StatusBadRequest, err.Error(), "threshold_error")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":     "ok",
		"account_id": req.AccountID,
	})
}
