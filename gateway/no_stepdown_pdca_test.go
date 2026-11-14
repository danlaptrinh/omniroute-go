package gateway

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestBuildStrictCCPACandidates_ZeroModelStepdown(t *testing.T) {
	tmpCfg := filepath.Join(t.TempDir(), "gateway_config.json")
	t.Setenv("GATEWAY_CONFIG_FILE", tmpCfg)
	resetAuthCacheForTest(t)
	ResetCCPAModelCaches()
	ClearAccountModelRateLimits("")
	t.Cleanup(func() {
		ResetCCPAModelCaches()
		ClearAccountModelRateLimits("")
		resetAuthCacheForTest(t)
	})

	pct100 := 100
	pct80 := 80
	exp := time.Now().Add(2 * time.Hour).Unix()

	cfg := SavedGatewayAuth{
		RoutingStrategy: StrategyRoundRobin,
		Accounts: []ConnectedAccount{
			{
				ID:                  "ultra-1",
				Provider:            ProviderAntigravity,
				Tier:                "Ultra",
				Email:               "ultra@gmail.com",
				AccessToken:         "ya29.ultra",
				ProjectID:           "proj-ultra",
				ExpiresAt:           exp,
				WeeklyClaudePercent: &pct100,
				WeeklyGeminiPercent: &pct80,
			},
			{
				ID:                  "pro-1",
				Provider:            ProviderAntigravity,
				Tier:                "Pro",
				Email:               "pro@gmail.com",
				AccessToken:         "ya29.pro",
				ProjectID:           "proj-pro",
				ExpiresAt:           exp,
				WeeklyClaudePercent: &pct100,
				WeeklyGeminiPercent: &pct80,
			},
		},
	}
	if err := saveAuthConfigLocked(cfg); err != nil {
		t.Fatalf("saveAuthConfigLocked failed: %v", err)
	}

	testCases := []struct {
		requestedModel string
		accountScope   string
		expected       []string
	}{
		{
			requestedModel: "antigravity/gemini-3.8-flash-high",
			accountScope:   "pro@gmail.com",
			expected:       []string{"gemini-3.8-flash-high"},
		},
		{
			requestedModel: "antigravity/gemini-3.8-flash-medium",
			accountScope:   "pro@gmail.com",
			expected:       []string{"gemini-3.8-flash-medium"},
		},
		{
			requestedModel: "antigravity/gemini-3.8-flash-low",
			accountScope:   "pro@gmail.com",
			expected:       []string{"gemini-3.8-flash-low"},
		},
		{
			requestedModel: "antigravity/gemini-3.8-flash-tiered",
			accountScope:   "pro@gmail.com",
			expected:       []string{"gemini-3.8-flash-tiered"},
		},
		{
			requestedModel: "antigravity/gemini-3.1-pro-high",
			accountScope:   "pro@gmail.com",
			expected:       []string{"gemini-3.8-flash-high"},
		},
		{
			requestedModel: "antigravity/gemini-3-flash",
			accountScope:   "pro@gmail.com",
			expected:       []string{"gemini-3-flash"},
		},
		{
			requestedModel: "antigravity/gemini-3.5-flash-lite",
			accountScope:   "pro@gmail.com",
			expected:       []string{"gemini-3.5-flash-lite"},
		},
		{
			requestedModel: "antigravity/claude-opus-4-6-thinking",
			accountScope:   "ultra@gmail.com",
			expected:       []string{"claude-opus-4-6-thinking"},
		},
		{
			requestedModel: "antigravity/claude-sonnet-4-6",
			accountScope:   "ultra@gmail.com",
			expected:       []string{"claude-sonnet-4-6"},
		},
	}

	for _, tc := range testCases {
		res := ResolveModel(tc.requestedModel, nil)
		cands := buildCCPACandidatesWithCache(DefaultCCPAEndpoint, tc.accountScope, res, false)
		if !reflect.DeepEqual(cands, tc.expected) {
			t.Fatalf("for model %s (acc: %s), expected candidates %v, got %v", tc.requestedModel, tc.accountScope, tc.expected, cands)
		}
	}

	// Verify gemini-4-carbon-high strictly isolates to Carbon/Argon and unthrottled flash-high, with zero stepdowns
	carbonRes := ResolveModel("antigravity/gemini-4-carbon-high", nil)
	carbonCands := buildCCPACandidatesWithCache(DefaultCCPAEndpoint, "ultra@gmail.com", carbonRes, false)
	expectedCarbon := []string{"gemini-4-carbon-high", "gemini-4-argon-high", "gemini-3.8-flash-high"}
	if !reflect.DeepEqual(carbonCands, expectedCarbon) {
		t.Fatalf("expected carbon candidates %v, got %v", expectedCarbon, carbonCands)
	}
	for _, c := range carbonCands {
		if strings.Contains(c, "tiered") || strings.EqualFold(c, "gemini-3-flash") || strings.HasPrefix(c, "claude-") {
			t.Fatalf("carbon candidates contaminated with lower tier/claude model %q: %v", c, carbonCands)
		}
	}

	// Verify auto/fast preserves combo candidates starting with gemini-3-flash
	fastRes := ResolveModel("auto/fast", nil)
	fastCands := buildCCPACandidatesWithCache(DefaultCCPAEndpoint, "pro@gmail.com", fastRes, false)
	if len(fastCands) == 0 || fastCands[0] != "gemini-3-flash" {
		t.Fatalf("expected auto/fast candidates to start with gemini-3-flash, got %v", fastCands)
	}
}

func TestBuildResolvedChatTargets_NonComboNeverWalksFallbackChain(t *testing.T) {
	// Direct Carbon model: must not walk FallbackChain into secondary models/providers
	carbonRes := ResolveModel("antigravity/gemini-4-carbon-high", nil)
	carbonTargets := buildResolvedChatTargets(carbonRes)
	if len(carbonTargets) != 1 {
		t.Fatalf("expected exactly 1 target for direct carbon model, got %d: %+v", len(carbonTargets), carbonTargets)
	}
	if carbonTargets[0].CanonicalID != "antigravity/gemini-4-carbon-high" {
		t.Fatalf("expected target CanonicalID 'antigravity/gemini-4-carbon-high', got %q", carbonTargets[0].CanonicalID)
	}

	// Direct Flash High model: must not walk FallbackChain
	flashHighRes := ResolveModel("antigravity/gemini-3.8-flash-high", nil)
	flashHighTargets := buildResolvedChatTargets(flashHighRes)
	if len(flashHighTargets) != 1 {
		t.Fatalf("expected exactly 1 target for direct flash-high model, got %d: %+v", len(flashHighTargets), flashHighTargets)
	}
	if flashHighTargets[0].CanonicalID != "antigravity/gemini-3.8-flash-high" {
		t.Fatalf("expected target CanonicalID 'antigravity/gemini-3.8-flash-high', got %q", flashHighTargets[0].CanonicalID)
	}

	// Combo model: auto/best-coding MUST build multiple targets from FallbackChain
	comboRes := ResolveModel("auto/best-coding", nil)
	if !comboRes.IsCombo {
		t.Fatalf("expected auto/best-coding to have IsCombo=true")
	}
	comboTargets := buildResolvedChatTargets(comboRes)
	if len(comboTargets) <= 1 {
		t.Fatalf("expected multiple targets for combo auto/best-coding, got %d", len(comboTargets))
	}
}

func TestPDCACycle_HealsSteppedDownWorkingPinsAndValidationIsolation(t *testing.T) {
	tmpCfg := filepath.Join(t.TempDir(), "gateway_config.json")
	t.Setenv("GATEWAY_CONFIG_FILE", tmpCfg)
	resetAuthCacheForTest(t)
	ResetCCPAModelCaches()
	ClearAccountModelRateLimits("")
	t.Cleanup(func() {
		ResetCCPAModelCaches()
		ClearAccountModelRateLimits("")
		resetAuthCacheForTest(t)
	})

	pct100 := 100
	exp := time.Now().Add(2 * time.Hour).Unix()
	cfg := SavedGatewayAuth{
		RoutingStrategy: StrategyRoundRobin,
		Accounts: []ConnectedAccount{
			{
				ID:                  "u-pdca-1",
				Provider:            ProviderAntigravity,
				Tier:                "Ultra",
				Email:               "ultra@gmail.com",
				AccessToken:         "ya29.ultra",
				ProjectID:           "proj-ultra",
				ExpiresAt:           exp,
				WeeklyClaudePercent: &pct100,
			},
			{
				ID:                  "p-pdca-1",
				Provider:            ProviderAntigravity,
				Tier:                "Pro",
				Email:               "pro@gmail.com",
				AccessToken:         "ya29.pro",
				ProjectID:           "proj-pro",
				ExpiresAt:           exp,
				WeeklyClaudePercent: &pct100,
			},
		},
	}
	if err := saveAuthConfigLocked(cfg); err != nil {
		t.Fatalf("saveAuthConfigLocked failed: %v", err)
	}

	// 1. Inject stepped-down / contaminated cache entries into workingCCPAModelCache
	proFlashHighPin := DefaultCCPAEndpoint + "|acc:pro@gmail.com|antigravity/gemini-3.8-flash-high"
	proFlashLowPin := DefaultCCPAEndpoint + "|acc:pro@gmail.com|antigravity/gemini-3.8-flash-low"
	ultraClaudePin := DefaultCCPAEndpoint + "|acc:ultra@gmail.com|antigravity/claude-opus-4-6-thinking"
	ultraCarbonValidPin := DefaultCCPAEndpoint + "|acc:ultra@gmail.com|antigravity/gemini-4-carbon-high"

	workingCCPAModelCache.Store(proFlashHighPin, "gemini-3.8-flash-tiered")
	workingCCPAModelCache.Store(proFlashLowPin, "gemini-3-flash")
	workingCCPAModelCache.Store(ultraClaudePin, "gemini-3.8-flash-high")
	workingCCPAModelCache.Store(ultraCarbonValidPin, "gemini-3.8-flash-high")

	// 2. Run PDCA Cycle to detect and heal step-down pins
	report := RunPDCACycle(time.Now().UTC())

	// 3. Verify contaminated pins are evicted
	if _, found := workingCCPAModelCache.Load(proFlashHighPin); found {
		t.Fatalf("expected proFlashHighPin to be evicted by PDCA, still found in cache")
	}
	if _, found := workingCCPAModelCache.Load(proFlashLowPin); found {
		t.Fatalf("expected proFlashLowPin to be evicted by PDCA, still found in cache")
	}
	if _, found := workingCCPAModelCache.Load(ultraClaudePin); found {
		t.Fatalf("expected ultraClaudePin to be evicted by PDCA, still found in cache")
	}

	// 4. Verify valid Ultra Carbon pin is retained
	val, found := workingCCPAModelCache.Load(ultraCarbonValidPin)
	if !found || val.(string) != "gemini-3.8-flash-high" {
		t.Fatalf("expected valid ultraCarbonValidPin to be retained, got val=%v found=%v", val, found)
	}

	// 5. Verify action report contains heal_model_stepdown_pin
	var foundHealAction bool
	for _, act := range report.Actions {
		if act.Type == "heal_model_stepdown_pin" {
			foundHealAction = true
			break
		}
	}
	if !foundHealAction {
		t.Fatalf("expected PDCA action 'heal_model_stepdown_pin' in report, got: %+v", report.Actions)
	}

	// 6. Test Account Validation Isolation
	SyncAccountValidationState("pro@gmail.com", true)
	savedAfterValidating := LoadSavedAuthConfig()
	var proAcc *ConnectedAccount
	for i := range savedAfterValidating.Accounts {
		if savedAfterValidating.Accounts[i].Email == "pro@gmail.com" {
			proAcc = &savedAfterValidating.Accounts[i]
			break
		}
	}
	if proAcc == nil || proAcc.CooldownUntil <= time.Now().Unix() {
		t.Fatalf("expected pro@gmail.com to have CooldownUntil in the future during validation, got %+v", proAcc)
	}

	// Run PDCA Cycle while validating; ensure PDCA does NOT treat validation cooldown as stale
	RunPDCACycle(time.Now().UTC())
	savedMidPDCA := LoadSavedAuthConfig()
	for _, a := range savedMidPDCA.Accounts {
		if a.Email == "pro@gmail.com" {
			if a.CooldownUntil <= time.Now().Unix() {
				t.Fatalf("PDCA erroneously cleared active validation CooldownUntil for pro@gmail.com")
			}
		}
	}

	// Finish validation, verify cooldown is cleanly reset
	SyncAccountValidationState("pro@gmail.com", false)
	savedAfterDone := LoadSavedAuthConfig()
	for _, a := range savedAfterDone.Accounts {
		if a.Email == "pro@gmail.com" {
			if a.CooldownUntil != 0 {
				t.Fatalf("expected CooldownUntil to be 0 after finishing validation, got %d", a.CooldownUntil)
			}
		}
	}
}
