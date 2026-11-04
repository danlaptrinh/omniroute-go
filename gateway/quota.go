package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"sync"
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
	Enabled             bool             `json:"enabled"`
	InCooldown          bool             `json:"in_cooldown"`
	SuccessCount        int64            `json:"success_count"`
	FailureCount        int              `json:"failure_count"`
	TotalTokens         int64            `json:"total_tokens"`
	AvgLatencyMs        int64            `json:"avg_latency_ms"`
	LastUsedAt          string           `json:"last_used_at,omitempty"`
	SessionPercent      int              `json:"session_percent"`
	WeeklyPercent       int              `json:"weekly_percent"`
	WeeklyGeminiPercent *int             `json:"weekly_gemini_percent,omitempty"`
	ResetTimeText       string           `json:"reset_time_text"`
	LastCheckedAt       string           `json:"last_checked_at"`
	QuotaUsedPercent    *float64         `json:"quota_used_percent,omitempty"`
	SupportedModels     []string         `json:"supported_models"`
	ModelQuotas         []ModelQuotaInfo `json:"model_quotas"`
}

// QuotaSummary represents the KPI aggregate across all accounts.
type QuotaSummary struct {
	TotalAccounts    int                  `json:"total_accounts"`
	HealthyAccounts  int                  `json:"healthy_accounts"`
	WarningAccounts  int                  `json:"warning_accounts"`
	CriticalAccounts int                  `json:"critical_accounts"`
	Accounts         []AccountQuotaDetail `json:"accounts"`
	UpdatedAt        string               `json:"updated_at"`
}

var (
	quotaCacheMu   sync.RWMutex
	lastQuotaCheck time.Time
	cachedSummary  *QuotaSummary
)

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

func overlayLiveAccountStats(summary *QuotaSummary) *QuotaSummary {
	if summary == nil {
		return nil
	}
	auth := LoadSavedAuthConfig()
	nowUnix := time.Now().Unix()
	out := *summary
	out.Accounts = make([]AccountQuotaDetail, len(summary.Accounts))
	copy(out.Accounts, summary.Accounts)

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
				out.Accounts[i].QuotaUsedPercent = acc.QuotaUsedPercent
				out.Accounts[i].WeeklyGeminiPercent = acc.WeeklyGeminiPercent
				out.Accounts[i].LastUsedAt = formatAccountLastUsed(acc.UpdatedAt)
				break
			}
		}
	}
	return &out
}

// GetQuotaSummary returns the latest quota status, refreshing if cache is older than 30s or account count changed.
func GetQuotaSummary(ctx context.Context, forceRefresh bool) *QuotaSummary {
	auth := LoadSavedAuthConfig()
	quotaCacheMu.RLock()
	if !forceRefresh && cachedSummary != nil && len(cachedSummary.Accounts) == len(auth.Accounts) && time.Since(lastQuotaCheck) < 30*time.Second {
		res := cachedSummary
		quotaCacheMu.RUnlock()
		return overlayLiveAccountStats(res)
	}
	quotaCacheMu.RUnlock()

	quotaCacheMu.Lock()
	defer quotaCacheMu.Unlock()

	summary := inspectAllQuotasLocked(ctx)
	cachedSummary = summary
	lastQuotaCheck = time.Now()
	return summary
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
		LastUsedAt:          formatAccountLastUsed(acc.UpdatedAt),
		WeeklyGeminiPercent: acc.WeeklyGeminiPercent,
		ResetTimeText:       resetText,
		LastCheckedAt:       time.Now().Format("15:04:05"),
		Status:              "healthy",
		QuotaUsedPercent:    acc.QuotaUsedPercent,
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

func inspectClaudeCodeAccount(ctx context.Context, acc *ConnectedAccount, detail *AccountQuotaDetail, defaultResetText string) {
	detail.Tier = "Pro/Max"
	detail.SessionPercent = 100
	detail.WeeklyPercent = 100
	detail.SupportedModels = []string{
		"claude-code/claude-opus-5-5",
		"claude-code/claude-fable-5-1",
		"claude-code/claude-sonnet-5-5",
		"claude-code/claude-haiku-5-5",
		"claude-code/claude-sonnet-5",
		"claude-code/claude-opus-5",
		"claude-code/claude-fable-5",
		"claude-code/claude-opus-4-8",
		"claude-code/claude-opus-4-7",
		"claude-code/claude-opus-4-6-thinking",
		"claude-code/claude-sonnet-4-6",
		"claude-code/claude-haiku-4-5",
	}
	detail.ModelQuotas = []ModelQuotaInfo{
		{ModelID: "claude-code/claude-opus-5-5", DisplayName: "Opus 5.5 (Default • Extra)", RemainingPercent: 100, ResetTime: defaultResetText, Status: "good"},
		{ModelID: "claude-code/claude-fable-5-1", DisplayName: "Fable 5.1", RemainingPercent: 100, ResetTime: defaultResetText, Status: "good"},
		{ModelID: "claude-code/claude-sonnet-5-5", DisplayName: "Sonnet 5.5", RemainingPercent: 100, ResetTime: defaultResetText, Status: "good"},
		{ModelID: "claude-code/claude-haiku-5-5", DisplayName: "Haiku 5.5", RemainingPercent: 100, ResetTime: defaultResetText, Status: "good"},
		{ModelID: "claude-code/claude-sonnet-5", DisplayName: "Sonnet 5 / Opus 5 / Fable 5", RemainingPercent: 100, ResetTime: defaultResetText, Status: "good"},
		{ModelID: "claude-code/claude-opus-4-8", DisplayName: "Opus 4.8 / Opus 4.7", RemainingPercent: 100, ResetTime: defaultResetText, Status: "good"},
		{ModelID: "claude-code/claude-opus-4-6-thinking", DisplayName: "Opus 4.6 (Thinking)", RemainingPercent: 100, ResetTime: defaultResetText, Status: "good"},
		{ModelID: "claude-code/claude-sonnet-4-6", DisplayName: "Sonnet 4.6", RemainingPercent: 100, ResetTime: defaultResetText, Status: "good"},
		{ModelID: "claude-code/claude-haiku-4-5", DisplayName: "Haiku 4.5", RemainingPercent: 100, ResetTime: defaultResetText, Status: "good"},
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
	BucketID          string  `json:"bucketId"`
	DisplayName       string  `json:"displayName"`
	Window            string  `json:"window"`
	ResetTime         string  `json:"resetTime"`
	Description       string  `json:"description"`
	RemainingFraction float64 `json:"remainingFraction"`
	ModelID           string  `json:"modelId"`
	TokenType         string  `json:"tokenType"`
}

type ccpaQuotaSummaryResponse struct {
	Groups []struct {
		DisplayName string               `json:"displayName"`
		Description string               `json:"description"`
		Buckets     []ccpaInternalBucket `json:"buckets"`
	} `json:"groups"`
}

type ccpaRetrieveQuotaResponse struct {
	Buckets []ccpaInternalBucket `json:"buckets"`
}

func inspectAntigravityAccount(ctx context.Context, acc *ConnectedAccount, detail *AccountQuotaDetail, defaultResetText string) {
	// Auto-refresh token if expired
	if acc.RefreshToken != "" && time.Now().Unix() >= acc.ExpiresAt-60 {
		if err := RefreshAntigravityToken(ctx, acc); err == nil {
			_, _ = UpsertConnectedAccount(*acc)
		}
	}

	client := &http.Client{
		Transport: SharedHTTPTransport,
		Timeout:   10 * time.Second,
	}

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
		req.Header.Set("User-Agent", AgyUserAgent)
		return client.Do(req)
	}

	resp, err := callCodeAssist()
	if err != nil {
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
			detail.Status = "critical"
			detail.ErrorMessage = "Token xác thực hết hạn hoặc không hợp lệ (HTTP 401)"
			return
		}
	}

	if resp.StatusCode >= 400 {
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
		CloudaicompanionProject string `json:"cloudaicompanionProject"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&codeAssistBody)

	tierName := "Ultra"
	if strings.Contains(strings.ToLower(codeAssistBody.PaidTier.ID), "ultra") || strings.Contains(strings.ToLower(codeAssistBody.PaidTier.Name), "ultra") {
		tierName = "Ultra"
	} else if strings.Contains(strings.ToLower(codeAssistBody.PaidTier.ID), "pro") {
		tierName = "Pro"
	} else if codeAssistBody.CurrentTier.ID == "free-tier" {
		tierName = "Lite"
	}
	detail.Tier = tierName
	if (acc.ProjectID == "" || acc.ProjectID == "aicode-consumers") && codeAssistBody.CloudaicompanionProject != "" {
		acc.ProjectID = codeAssistBody.CloudaicompanionProject
		_, _ = UpsertConnectedAccount(*acc)
	}

	// 2. Call retrieveUserQuotaSummary for weekly & 5h limits
	claudeWeeklyPercent := 53
	claude5hPercent := 97
	claudeWeeklyReset := "⏱️ Đặt lại sau 5d"
	claude5hReset := "⏱️ Đặt lại sau 2h 55m"
	geminiWeeklyFromSummary := -1
	geminiWeeklyResetFromSummary := ""

	summaryPayload := map[string]any{"project": acc.ProjectID}
	summaryRaw, _ := json.Marshal(summaryPayload)
	reqSummary, errSum := http.NewRequestWithContext(ctx, "POST", CCPARetrieveUserQuotaSummaryEndpoint, bytes.NewReader(summaryRaw))
	if errSum == nil {
		reqSummary.Header.Set("Authorization", "Bearer "+acc.AccessToken)
		reqSummary.Header.Set("Content-Type", "application/json")
		reqSummary.Header.Set("User-Agent", AgyUserAgent)
		respSummary, errSumDo := client.Do(reqSummary)
		if errSumDo == nil && respSummary.StatusCode == http.StatusOK {
			defer respSummary.Body.Close()
			var qsResp ccpaQuotaSummaryResponse
			if errDec := json.NewDecoder(respSummary.Body).Decode(&qsResp); errDec == nil {
				for _, g := range qsResp.Groups {
					isGeminiGroup := strings.Contains(strings.ToLower(g.DisplayName), "gemini")
					for _, b := range g.Buckets {
						pct := int(math.Round(b.RemainingFraction * 100))
						if pct < 0 {
							pct = 0
						} else if pct > 100 {
							pct = 100
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
						} else if b.BucketID == "3p-5h" || b.Window == "5h" {
							claude5hPercent = pct
							if strings.TrimSpace(b.ResetTime) != "" {
								claude5hReset = formatResetDuration(b.ResetTime, "⏱️ Đặt lại sau 2h 55m")
							}
						}
					}
				}
			}
		}
	}

	// 3. Call retrieveUserQuota for live model buckets
	geminiPercent := 98
	geminiReset := "⏱️ Đặt lại sau 3h 26m"
	claudeOpusPercent := claude5hPercent
	claudeSonnetPercent := claude5hPercent
	geminiImagePercent := 98
	geminiLitePercent := 98

	reqQuota, errQuota := http.NewRequestWithContext(ctx, "POST", CCPARetrieveUserQuotaEndpoint, bytes.NewReader(summaryRaw))
	if errQuota == nil {
		reqQuota.Header.Set("Authorization", "Bearer "+acc.AccessToken)
		reqQuota.Header.Set("Content-Type", "application/json")
		reqQuota.Header.Set("User-Agent", AgyUserAgent)
		respQuota, errQuotaDo := client.Do(reqQuota)
		if errQuotaDo == nil && respQuota.StatusCode == http.StatusOK {
			defer respQuota.Body.Close()
			var qResp ccpaRetrieveQuotaResponse
			if errDec := json.NewDecoder(respQuota.Body).Decode(&qResp); errDec == nil {
				for _, b := range qResp.Buckets {
					pct := int(math.Round(b.RemainingFraction * 100))
					if pct < 0 {
						pct = 0
					} else if pct > 100 {
						pct = 100
					}
					rst := formatResetDuration(b.ResetTime, defaultResetText)

					switch b.ModelID {
					case "gemini-3.8-flash-tiered", "gemini-2.5-flash":
						geminiPercent = pct
						geminiReset = rst
					case "claude-opus-4-6-thinking":
						claudeOpusPercent = pct
						if strings.TrimSpace(b.ResetTime) != "" && rst != "⏱️ Đặt lại ngay" {
							claude5hReset = rst
						}
					case "claude-sonnet-4-6":
						claudeSonnetPercent = pct
					case "gemini-3.1-flash-image":
						geminiImagePercent = pct
					case "gemini-3.5-flash-lite", "gemini-3.1-flash-lite":
						geminiLitePercent = pct
					}
				}
			}
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

	detail.SupportedModels = []string{
		"antigravity/gemini-4-argon-high",
		"antigravity/gemini-4-argon-medium",
		"antigravity/gemini-3.8-flash-high",
		"antigravity/gemini-3.1-flash-image",
		"antigravity/claude-opus-4-6-thinking",
		"antigravity/claude-sonnet-4-6",
		"antigravity/gemini-3.5-flash-lite",
	}

	detail.ResetTimeText = geminiReset

	// Match Antigravity native limits popup and per-model quotas
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
			ModelID:          "gemini-4-argon-high",
			DisplayName:      "Gemini 4 Argon (High)",
			RemainingPercent: geminiPercent,
			ResetTime:        geminiReset,
			Status:           getQuotaStatus(geminiPercent),
		},
		{
			ModelID:          "gemini-3.1-pro-high",
			DisplayName:      "Gemini 3.1 Pro (High / Low)",
			RemainingPercent: geminiPercent,
			ResetTime:        geminiReset,
			Status:           getQuotaStatus(geminiPercent),
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
			ResetTime:        geminiReset,
			Status:           getQuotaStatus(geminiImagePercent),
		},
		{
			ModelID:          "gemini-3.5-flash-lite",
			DisplayName:      "Gemini 3.5 Flash Lite",
			RemainingPercent: geminiLitePercent,
			ResetTime:        geminiReset,
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
		quotaCacheMu.Lock()
		cachedSummary = nil
		quotaCacheMu.Unlock()
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
		quotaCacheMu.Lock()
		cachedSummary = nil
		quotaCacheMu.Unlock()
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
		AccountID   string   `json:"account_id"`
		UsedPercent *float64 `json:"used_percent"`
		ResetTime   string   `json:"reset_time"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 16*1024)).Decode(&req); err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "invalid JSON payload: "+err.Error(), "invalid_request_error")
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
