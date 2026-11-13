package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func setupPDCATestEnv(t *testing.T) (time.Time, string) {
	t.Helper()
	now := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)

	tmpCfg := filepath.Join(t.TempDir(), "gateway_config.json")
	t.Setenv("GATEWAY_CONFIG_FILE", tmpCfg)

	ResetPDCAState()
	GlobalLogger.ClearLogs()
	ResetCCPAModelCaches()
	ClearAccountModelRateLimits("")

	accountTierCache.Range(func(key, _ any) bool {
		accountTierCache.Delete(key)
		return true
	})
	validationIsolatedAccounts.Range(func(key, _ any) bool {
		validationIsolatedAccounts.Delete(key)
		return true
	})

	quotaCacheMu.Lock()
	cachedSummary = nil
	lastQuotaCheck = time.Time{}
	quotaCacheMu.Unlock()

	configMu.Lock()
	cachedAuthCfg = nil
	cachedAuthPath = ""
	cachedAuthMtime = time.Time{}
	cachedAuthStatAt = time.Time{}
	asyncFlushPending = false
	configMu.Unlock()

	t.Cleanup(func() {
		ResetPDCAState()
		GlobalLogger.ClearLogs()
		ResetCCPAModelCaches()
		ClearAccountModelRateLimits("")

		accountTierCache.Range(func(key, _ any) bool {
			accountTierCache.Delete(key)
			return true
		})
		validationIsolatedAccounts.Range(func(key, _ any) bool {
			validationIsolatedAccounts.Delete(key)
			return true
		})

		quotaCacheMu.Lock()
		cachedSummary = nil
		lastQuotaCheck = time.Time{}
		quotaCacheMu.Unlock()

		configMu.Lock()
		cachedAuthCfg = nil
		cachedAuthPath = ""
		cachedAuthMtime = time.Time{}
		cachedAuthStatAt = time.Time{}
		asyncFlushPending = false
		configMu.Unlock()
	})

	return now, tmpCfg
}

func TestPDCACycle_StaleCooldownHeal(t *testing.T) {
	now, _ := setupPDCATestEnv(t)

	pct85 := 85
	pastReset := now.Add(-2 * time.Minute).Format(time.RFC3339)
	futureReset := now.Add(2 * time.Hour).Format(time.RFC3339)

	accHealStale := ConnectedAccount{
		ID:                  "agy-healed@example.com",
		Provider:            ProviderAntigravity,
		Email:               "healed@example.com",
		AccessToken:         "tok-healed",
		Tier:                "Pro",
		Disabled:            false,
		CooldownUntil:       now.Add(20 * time.Minute).Unix(),
		WeeklyGeminiPercent: &pct85,
	}

	accHealPastReset := ConnectedAccount{
		ID:             "agy-pastreset@example.com",
		Provider:       ProviderAntigravity,
		Email:          "pastreset@example.com",
		AccessToken:    "tok-pastreset",
		Tier:           "Pro",
		Disabled:       false,
		CooldownUntil:  now.Add(3 * time.Minute).Unix(),
		QuotaResetTime: pastReset,
	}

	accActive401 := ConnectedAccount{
		ID:            "agy-authfail@example.com",
		Provider:      ProviderAntigravity,
		Email:         "authfail@example.com",
		AccessToken:   "tok-authfail",
		Tier:          "Pro",
		Disabled:      false,
		CooldownUntil: now.Add(15 * time.Minute).Unix(),
	}

	accValidationReq := ConnectedAccount{
		ID:            "agy-validation@example.com",
		Provider:      ProviderAntigravity,
		Email:         "validation@example.com",
		AccessToken:   "tok-validation",
		Tier:          "Pro",
		Disabled:      false,
		CooldownUntil: now.Add(30 * time.Minute).Unix(),
	}

	accBriefTransient := ConnectedAccount{
		ID:             "agy-transient@example.com",
		Provider:       ProviderAntigravity,
		Email:          "transient@example.com",
		AccessToken:    "tok-transient",
		Tier:           "Pro",
		Disabled:       false,
		CooldownUntil:  now.Add(2 * time.Minute).Unix(),
		QuotaResetTime: futureReset,
	}

	for _, acc := range []ConnectedAccount{accHealStale, accHealPastReset, accActive401, accValidationReq, accBriefTransient} {
		if _, err := UpsertConnectedAccount(acc); err != nil {
			t.Fatalf("failed to seed account %s: %v", acc.Email, err)
		}
	}

	validationIsolatedAccounts.Store("validation@example.com", true)

	GlobalLogger.RecordLogEntry(GatewayLogEntry{
		Account:      "authfail@example.com",
		Provider:     ProviderAntigravity,
		StatusCode:   http.StatusUnauthorized,
		ErrorMessage: "oauth token revoked (401)",
		IsError:      true,
		Timestamp:    now.Add(-1 * time.Minute).Format(time.RFC3339),
	})

	report := RunPDCACycle(now)

	if report.HealedStaleCooldowns < 2 {
		t.Fatalf("expected at least 2 healed stale cooldowns, got %d", report.HealedStaleCooldowns)
	}

	foundHealStaleAction := false
	foundHealPastResetAction := false
	for _, act := range report.Actions {
		if act.Type == "heal_stale_cooldown" {
			if strings.EqualFold(act.Account, "healed@example.com") {
				foundHealStaleAction = true
			}
			if strings.EqualFold(act.Account, "pastreset@example.com") {
				foundHealPastResetAction = true
			}
		}
	}
	if !foundHealStaleAction {
		t.Errorf("expected heal_stale_cooldown action for healed@example.com")
	}
	if !foundHealPastResetAction {
		t.Errorf("expected heal_stale_cooldown action for pastreset@example.com")
	}

	cfg := LoadSavedAuthConfig()
	for _, acc := range cfg.Accounts {
		switch acc.Email {
		case "healed@example.com":
			if acc.CooldownUntil != 0 {
				t.Errorf("healed@example.com: expected CooldownUntil=0, got %d", acc.CooldownUntil)
			}
		case "pastreset@example.com":
			if acc.CooldownUntil != 0 {
				t.Errorf("pastreset@example.com: expected CooldownUntil=0, got %d", acc.CooldownUntil)
			}
		case "authfail@example.com":
			if acc.CooldownUntil <= now.Unix() {
				t.Errorf("authfail@example.com: CooldownUntil should NOT be healed prematurely, got %d", acc.CooldownUntil)
			}
		case "validation@example.com":
			if acc.CooldownUntil <= now.Unix() {
				t.Errorf("validation@example.com: CooldownUntil should NOT be healed while validation-isolated, got %d", acc.CooldownUntil)
			}
		case "transient@example.com":
			if acc.CooldownUntil <= now.Unix() {
				t.Errorf("transient@example.com: transient CooldownUntil (<5m) with future reset should not be cleared, got %d", acc.CooldownUntil)
			}
		}
	}
}

func TestPDCACycle_ExpiredModelCachePrune(t *testing.T) {
	now, _ := setupPDCATestEnv(t)

	endpoint := DefaultCCPAEndpoint
	expiredDeadKey := endpoint + "|acc:ultra@example.com|gemini-4-carbon-high"
	liveDeadKey := endpoint + "|acc:ultra@example.com|gemini-4-argon-high"

	deadCCPAModelCache.Store(expiredDeadKey, deadCCPAEntry{
		expiresAt:  now.Add(-1 * time.Minute),
		statusCode: http.StatusServiceUnavailable,
	})
	deadCCPAModelCache.Store(liveDeadKey, deadCCPAEntry{
		expiresAt:  now.Add(3 * time.Minute),
		statusCode: http.StatusTooManyRequests,
	})

	accountModelRLMu.Lock()
	accountModelRateLimits["ultra@example.com|gemini-4-carbon-high"] = now.Add(-30 * time.Second)
	accountModelRateLimits["pro@example.com|gemini-3-flash"] = now.Add(2 * time.Minute)
	accountModelRLMu.Unlock()

	report := RunPDCACycle(now)

	if report.PrunedDeadModelCacheEntries < 1 {
		t.Fatalf("expected PrunedDeadModelCacheEntries >= 1, got %d", report.PrunedDeadModelCacheEntries)
	}
	if report.PrunedAccountRateLimitEntries < 1 {
		t.Fatalf("expected PrunedAccountRateLimitEntries >= 1, got %d", report.PrunedAccountRateLimitEntries)
	}

	foundCacheAction := false
	for _, act := range report.Actions {
		if act.Type == "heal_expired_model_cache" {
			foundCacheAction = true
			break
		}
	}
	if !foundCacheAction {
		t.Errorf("expected heal_expired_model_cache in report.Actions")
	}

	if _, ok := deadCCPAModelCache.Load(expiredDeadKey); ok {
		t.Errorf("expired dead CCPA entry was not pruned from deadCCPAModelCache")
	}
	if _, ok := deadCCPAModelCache.Load(liveDeadKey); !ok {
		t.Errorf("valid live dead CCPA entry was prematurely removed from deadCCPAModelCache")
	}

	accountModelRLMu.RLock()
	_, expRLPresent := accountModelRateLimits["ultra@example.com|gemini-4-carbon-high"]
	_, liveRLPresent := accountModelRateLimits["pro@example.com|gemini-3-flash"]
	accountModelRLMu.RUnlock()

	if expRLPresent {
		t.Errorf("expired account model rate limit entry was not pruned")
	}
	if !liveRLPresent {
		t.Errorf("valid live account model rate limit entry was prematurely pruned")
	}
}

func TestPDCACycle_UltraTierLeakDetectionAndPinEviction(t *testing.T) {
	now, _ := setupPDCATestEnv(t)

	pct80 := 80
	ultraAcc := ConnectedAccount{
		ID:          "agy-ultra@example.com",
		Provider:    ProviderAntigravity,
		Email:       "ultra@example.com",
		AccessToken: "tok-ultra",
		Tier:        "Ultra",
		Disabled:    false,
	}
	proAcc := ConnectedAccount{
		ID:                  "agy-pro@example.com",
		Provider:            ProviderAntigravity,
		Email:               "pro@example.com",
		AccessToken:         "tok-pro",
		Tier:                "Pro",
		WeeklyGeminiPercent: &pct80,
		Disabled:            false,
	}

	if _, err := UpsertConnectedAccount(ultraAcc); err != nil {
		t.Fatalf("failed to insert ultra account: %v", err)
	}
	if _, err := UpsertConnectedAccount(proAcc); err != nil {
		t.Fatalf("failed to insert pro account: %v", err)
	}

	badPinKey := DefaultCCPAEndpoint + "|acc:ultra@example.com|antigravity/gemini-4-carbon-high"
	directFlashHighPinKey := DefaultCCPAEndpoint + "|acc:ultra@example.com|antigravity/gemini-3.8-flash-high"
	allowedCarbonBackendPinKey := DefaultCCPAEndpoint + "|acc:ultra@example.com|antigravity/gemini-4-carbon-medium"
	goodPinKey := DefaultCCPAEndpoint + "|acc:ultra@example.com|antigravity/gemini-4-argon-high"

	workingCCPAModelCache.Store(badPinKey, "gemini-3-flash")
	workingCCPAModelCache.Store(directFlashHighPinKey, "gemini-3.8-flash-high")
	workingCCPAModelCache.Store(allowedCarbonBackendPinKey, "gemini-3.8-flash-medium")
	workingCCPAModelCache.Store(goodPinKey, "gemini-4-argon-high")

	GlobalLogger.RecordLogEntry(GatewayLogEntry{
		Account:        "ultra@example.com",
		Provider:       ProviderAntigravity,
		RequestedModel: "antigravity/gemini-3-flash",
		ResolvedModel:  "antigravity/gemini-3-flash",
		UpstreamModel:  "gemini-3-flash",
		StatusCode:     http.StatusOK,
		Timestamp:      now.Add(-2 * time.Minute).Format(time.RFC3339),
	})

	report := RunPDCACycle(now)

	foundLeakAnomaly := false
	for _, anom := range report.Anomalies {
		if anom.Type == "ultra_tier_routing_leak" && strings.EqualFold(anom.Account, "ultra@example.com") {
			foundLeakAnomaly = true
			break
		}
	}
	if !foundLeakAnomaly {
		t.Errorf("expected ultra_tier_routing_leak anomaly for ultra@example.com")
	}

	if report.EvictedUltraNonFlagshipPins < 1 {
		t.Fatalf("expected EvictedUltraNonFlagshipPins >= 1, got %d", report.EvictedUltraNonFlagshipPins)
	}

	foundEvictAction := false
	for _, act := range report.Actions {
		if act.Type == "heal_ultra_working_model_pin" && strings.EqualFold(act.Account, "ultra@example.com") {
			foundEvictAction = true
			break
		}
	}
	if !foundEvictAction {
		t.Errorf("expected heal_ultra_working_model_pin action in report.Actions")
	}

	if _, ok := workingCCPAModelCache.Load(badPinKey); ok {
		t.Errorf("bad non-flagship pin was not evicted from workingCCPAModelCache")
	}
	if _, ok := workingCCPAModelCache.Load(directFlashHighPinKey); ok {
		t.Errorf("direct gemini-3.8-flash-high canonical pin on Ultra was not evicted from workingCCPAModelCache")
	}
	if val, ok := workingCCPAModelCache.Load(allowedCarbonBackendPinKey); !ok || val != "gemini-3.8-flash-medium" {
		t.Errorf("allowed Carbon->gemini-3.8-flash-medium Ultra backend pin should be preserved per #65")
	}
	if val, ok := workingCCPAModelCache.Load(goodPinKey); !ok || val != "gemini-4-argon-high" {
		t.Errorf("valid flagship pin in workingCCPAModelCache was incorrectly removed or altered")
	}
}

func TestPDCAStatusHTTPHandler(t *testing.T) {
	now, _ := setupPDCATestEnv(t)

	alibabaAcc := ConnectedAccount{
		ID:          "ali-main@example.com",
		Provider:    ProviderAlibaba,
		Email:       "alibaba-main@example.com",
		APIKey:      "sk-alibaba-test-key",
		Disabled:    false,
		FailureCount: 0,
	}
	claudeAcc := ConnectedAccount{
		ID:          "claude-dev@example.com",
		Provider:    ProviderClaudeCode,
		Email:       "claude-dev@example.com",
		AccessToken: "sk-ant-oat-test",
		Disabled:    false,
	}
	if _, err := UpsertConnectedAccount(alibabaAcc); err != nil {
		t.Fatalf("failed to insert alibaba account: %v", err)
	}
	if _, err := UpsertConnectedAccount(claudeAcc); err != nil {
		t.Fatalf("failed to insert claude account: %v", err)
	}

	for i := 0; i < 4; i++ {
		GlobalLogger.RecordLogEntry(GatewayLogEntry{
			Account:      "claude-dev@example.com",
			Provider:     ProviderClaudeCode,
			StatusCode:   http.StatusUnauthorized,
			ErrorMessage: "claude authentication invalid (401)",
			IsError:      true,
			Timestamp:    now.Add(-time.Duration(i+1) * time.Minute).Format(time.RFC3339),
		})
	}
	for i := 0; i < 3; i++ {
		GlobalLogger.RecordLogEntry(GatewayLogEntry{
			Account:      "claude-dev@example.com",
			Provider:     ProviderClaudeCode,
			StatusCode:   http.StatusNotFound,
			ErrorMessage: "claude upstream model not found (404)",
			IsError:      true,
			Timestamp:    now.Add(-time.Duration(i+5) * time.Minute).Format(time.RFC3339),
		})
	}

	report := RunPDCACycle(now)
	if !report.CrossProviderIsolationVerified {
		t.Errorf("expected CrossProviderIsolationVerified to be true")
	}

	// 1. Test DELETE -> 405 Method Not Allowed
	reqDelete := httptest.NewRequest(http.MethodDelete, "/api/pdca/status", nil)
	recDelete := httptest.NewRecorder()
	HandlePDCAStatus(recDelete, reqDelete)
	if recDelete.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected HTTP 405 on DELETE, got %d", recDelete.Code)
	}

	// 2. Test GET -> 200 OK
	reqGet := httptest.NewRequest(http.MethodGet, "/api/pdca/status", nil)
	recGet := httptest.NewRecorder()
	HandlePDCAStatus(recGet, reqGet)

	if recGet.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200 on GET, got %d (body: %s)", recGet.Code, recGet.Body.String())
	}
	if ct := recGet.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("expected application/json Content-Type, got %s", ct)
	}

	var snap PDCAStatusSnapshot
	if err := json.NewDecoder(recGet.Body).Decode(&snap); err != nil {
		t.Fatalf("failed to decode PDCAStatusSnapshot: %v", err)
	}

	if snap.TotalCycles < 1 {
		t.Errorf("expected TotalCycles >= 1, got %d", snap.TotalCycles)
	}
	if len(snap.Alerts) < 1 {
		t.Fatalf("expected len(Alerts) >= 1, got %d", len(snap.Alerts))
	}
	for _, al := range snap.Alerts {
		if strings.TrimSpace(al.RecommendedRemediation) == "" {
			t.Errorf("alert %s missing RecommendedRemediation", al.ID)
		}
	}
	if !snap.CrossProviderIsolationVerified {
		t.Errorf("expected snapshot CrossProviderIsolationVerified=true")
	}

	// 3. Test Ring Buffer Bounded History: run 60 cycles with anomalies/actions
	for i := 0; i < 60; i++ {
		cycleTime := now.Add(time.Duration(i+1) * time.Minute)
		deadCCPAModelCache.Store(
			DefaultCCPAEndpoint+"|acc:ultra@example.com|expired-test-model",
			deadCCPAEntry{expiresAt: cycleTime.Add(-10 * time.Second), statusCode: 503},
		)
		RunPDCACycle(cycleTime)
	}

	finalSnap := GetPDCAStatus()
	if len(finalSnap.RecentCycles) > MaxPDCACycleHistory {
		t.Errorf("RecentCycles exceeded MaxPDCACycleHistory (%d): got %d", MaxPDCACycleHistory, len(finalSnap.RecentCycles))
	}
	if len(finalSnap.RecentActions) > MaxPDCAActionHistory {
		t.Errorf("RecentActions exceeded MaxPDCAActionHistory (%d): got %d", MaxPDCAActionHistory, len(finalSnap.RecentActions))
	}
	if len(finalSnap.RecentAnomalies) > MaxPDCAAnomalyHistory {
		t.Errorf("RecentAnomalies exceeded MaxPDCAAnomalyHistory (%d): got %d", MaxPDCAAnomalyHistory, len(finalSnap.RecentAnomalies))
	}
}

func TestPDCAWatchdogCancel(t *testing.T) {
	_, _ = setupPDCATestEnv(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	StartPDCAWatchdog(ctx, 15*time.Millisecond)

	initialSnap := GetPDCAStatus()
	if initialSnap.TotalCycles < 1 {
		t.Fatalf("expected immediate initial cycle, got TotalCycles=%d", initialSnap.TotalCycles)
	}
	if !initialSnap.WatchdogRunning {
		t.Fatalf("expected WatchdogRunning=true right after start")
	}

	// Wait until at least cycle 2 runs
	deadline := time.Now().Add(1500 * time.Millisecond)
	reachedTwo := false
	for time.Now().Before(deadline) {
		if GetPDCAStatus().TotalCycles >= 2 {
			reachedTwo = true
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !reachedTwo {
		t.Fatalf("timed out waiting for PDCA watchdog to run second cycle")
	}

	cancel()

	stopDeadline := time.Now().Add(1500 * time.Millisecond)
	stopped := false
	for time.Now().Before(stopDeadline) {
		if !GetPDCAStatus().WatchdogRunning {
			stopped = true
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !stopped {
		t.Fatalf("expected WatchdogRunning=false after context cancellation")
	}

	snapAtStop := GetPDCAStatus()
	cyclesAtStop := snapAtStop.TotalCycles

	time.Sleep(40 * time.Millisecond)

	snapAfter := GetPDCAStatus()
	if snapAfter.TotalCycles != cyclesAtStop {
		t.Errorf("expected TotalCycles to freeze at %d after cancellation, but got %d", cyclesAtStop, snapAfter.TotalCycles)
	}
}
