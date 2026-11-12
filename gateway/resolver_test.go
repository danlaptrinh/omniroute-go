package gateway

import (
	"strings"
	"testing"
)

func TestResolve(t *testing.T) {
	r := NewResolver()

	tests := []struct {
		name                 string
		input                string
		customFallbacks      []string
		wantProvider         ProviderID
		wantCanonicalID      string
		wantUpstreamModelID  string
		wantCCPAModelID      string
		wantThinkingLevel    ThinkingLevel
		wantReasoningEffort  string
		wantIncludeThoughts  bool
		wantPlaceholderEnum  string
		wantIsArgonFamily    bool
		wantIsCombo          bool
		checkFallbackPrefix  []string
		checkFallbackContain []string
	}{
		// ---------------------------------------------------------------------
		// 1. Antigravity Gen 4
		// ---------------------------------------------------------------------
		{
			name:                "Antigravity Gen 4 - gemini-4-argon-high",
			input:               "gemini-4-argon-high",
			wantProvider:        ProviderAntigravity,
			wantCanonicalID:     "antigravity/gemini-4-argon-high",
			wantUpstreamModelID: "gemini-4-argon",
			wantCCPAModelID:     "gemini-4-argon",
			wantThinkingLevel:   ThinkingLevelHigh,
			wantReasoningEffort: "high",
			wantIncludeThoughts: true,
			wantPlaceholderEnum: "MODEL_PLACEHOLDER_M417",
			wantIsArgonFamily:   true,
			wantIsCombo:         false,
		},
		{
			name:                "Antigravity Gen 4 - antigravity/gemini-4-argon-medium",
			input:               "antigravity/gemini-4-argon-medium",
			wantProvider:        ProviderAntigravity,
			wantCanonicalID:     "antigravity/gemini-4-argon-medium",
			wantUpstreamModelID: "gemini-4-argon",
			wantCCPAModelID:     "gemini-4-argon",
			wantThinkingLevel:   ThinkingLevelMedium,
			wantReasoningEffort: "medium",
			wantIncludeThoughts: true,
			wantPlaceholderEnum: "MODEL_PLACEHOLDER_M418",
			wantIsArgonFamily:   true,
			wantIsCombo:         false,
		},
		{
			name:                "Antigravity Gen 4 - gemini-4-argon-low",
			input:               "gemini-4-argon-low",
			wantProvider:        ProviderAntigravity,
			wantCanonicalID:     "antigravity/gemini-4-argon-low",
			wantUpstreamModelID: "gemini-4-argon",
			wantCCPAModelID:     "gemini-4-argon",
			wantThinkingLevel:   ThinkingLevelLow,
			wantReasoningEffort: "low",
			wantIncludeThoughts: true,
			wantPlaceholderEnum: "MODEL_PLACEHOLDER_M419",
			wantIsArgonFamily:   true,
			wantIsCombo:         false,
		},
		{
			name:                "Antigravity Gen 4 - MODEL_PLACEHOLDER_M417 placeholder enum",
			input:               "MODEL_PLACEHOLDER_M417",
			wantProvider:        ProviderAntigravity,
			wantCanonicalID:     "antigravity/gemini-4-argon-high",
			wantUpstreamModelID: "gemini-4-argon",
			wantCCPAModelID:     "gemini-4-argon",
			wantThinkingLevel:   ThinkingLevelHigh,
			wantReasoningEffort: "high",
			wantIncludeThoughts: true,
			wantPlaceholderEnum: "MODEL_PLACEHOLDER_M417",
			wantIsArgonFamily:   true,
			wantIsCombo:         false,
		},

		// ---------------------------------------------------------------------
		// 2. Antigravity Gen 3
		// ---------------------------------------------------------------------
		{
			name:                "Antigravity Gen 3 - gemini-3.8-flash-high",
			input:               "gemini-3.8-flash-high",
			wantProvider:        ProviderAntigravity,
			wantCanonicalID:     "antigravity/gemini-3.8-flash-high",
			wantUpstreamModelID: "gemini-3.8-flash",
			wantCCPAModelID:     "gemini-3.8-flash",
			wantThinkingLevel:   ThinkingLevelHigh,
			wantReasoningEffort: "high",
			wantIncludeThoughts: true,
			wantPlaceholderEnum: "MODEL_PLACEHOLDER_M421",
			wantIsArgonFamily:   false,
		},
		{
			name:                "Antigravity Gen 3 - gemini-3.7-flash-high",
			input:               "gemini-3.7-flash-high",
			wantProvider:        ProviderAntigravity,
			wantCanonicalID:     "antigravity/gemini-3.7-flash-high",
			wantUpstreamModelID: "gemini-3.7-flash",
			wantCCPAModelID:     "gemini-3.7-flash",
			wantThinkingLevel:   ThinkingLevelHigh,
			wantReasoningEffort: "high",
			wantIncludeThoughts: true,
			wantIsArgonFamily:   false,
		},
		{
			name:                "Antigravity Gen 3 - gemini-3.5-flash-lite",
			input:               "gemini-3.5-flash-lite",
			wantProvider:        ProviderAntigravity,
			wantCanonicalID:     "antigravity/gemini-3.5-flash-lite",
			wantUpstreamModelID: "gemini-3.5-flash-lite",
			wantCCPAModelID:     "gemini-3.5-flash-lite",
			wantThinkingLevel:   ThinkingLevelLow,
			wantReasoningEffort: "low",
			wantIncludeThoughts: true,
			wantPlaceholderEnum: "MODEL_PLACEHOLDER_M198",
			wantIsArgonFamily:   false,
		},
		{
			name:                "Antigravity Gen 3 - gemini-3.1-pro-high",
			input:               "gemini-3.1-pro-high",
			wantProvider:        ProviderAntigravity,
			wantCanonicalID:     "antigravity/gemini-3.1-pro-high",
			wantUpstreamModelID: "gemini-3.1-pro-preview",
			wantCCPAModelID:     "gemini-3.1-pro-preview",
			wantThinkingLevel:   ThinkingLevelHigh,
			wantReasoningEffort: "high",
			wantIncludeThoughts: true,
			wantIsArgonFamily:   false,
		},
		{
			name:                "Antigravity Gen 3 - gemini-3-flash",
			input:               "gemini-3-flash",
			wantProvider:        ProviderAntigravity,
			wantCanonicalID:     "antigravity/gemini-3-flash",
			wantUpstreamModelID: "gemini-3-flash",
			wantCCPAModelID:     "gemini-3-flash",
			wantThinkingLevel:   ThinkingLevelHigh,
			wantReasoningEffort: "high",
			wantIncludeThoughts: true,
			wantPlaceholderEnum: "MODEL_PLACEHOLDER_M18",
			wantIsArgonFamily:   false,
		},

		// ---------------------------------------------------------------------
		// 3. Antigravity Claude
		// ---------------------------------------------------------------------
		{
			name:                "Antigravity Claude - claude-opus-5-5-thinking",
			input:               "claude-opus-5-5-thinking",
			wantProvider:        ProviderAntigravity,
			wantCanonicalID:     "antigravity/claude-opus-5-5-thinking",
			wantUpstreamModelID: "claude-opus-5-5@default",
			wantCCPAModelID:     "claude-opus-5-5@default",
			wantThinkingLevel:   ThinkingLevelHigh,
			wantReasoningEffort: "high",
			wantIncludeThoughts: true,
			wantPlaceholderEnum: "MODEL_PLACEHOLDER_M412",
			wantIsArgonFamily:   false,
		},
		{
			name:                "Antigravity Claude - antigravity/claude-opus-4-6-thinking",
			input:               "antigravity/claude-opus-4-6-thinking",
			wantProvider:        ProviderAntigravity,
			wantCanonicalID:     "antigravity/claude-opus-4-6-thinking",
			wantUpstreamModelID: "claude-opus-4-6-thinking",
			wantCCPAModelID:     "claude-opus-4-6-thinking",
			wantThinkingLevel:   ThinkingLevelHigh,
			wantReasoningEffort: "high",
			wantIncludeThoughts: true,
			wantPlaceholderEnum: "MODEL_PLACEHOLDER_M26",
			wantIsArgonFamily:   false,
		},

		// ---------------------------------------------------------------------
		// 4. Claude Code
		// ---------------------------------------------------------------------
		{
			name:                "Claude Code - claude-code/claude-opus-4-6-thinking",
			input:               "claude-code/claude-opus-4-6-thinking",
			wantProvider:        ProviderClaudeCode,
			wantCanonicalID:     "claude-code/claude-opus-4-6-thinking",
			wantUpstreamModelID: "claude-opus-4-6",
			wantCCPAModelID:     "claude-opus-4-6",
			wantThinkingLevel:   ThinkingLevelHigh,
			wantReasoningEffort: "high",
			wantIncludeThoughts: true,
			wantIsArgonFamily:   false,
		},
		{
			name:                "Claude Code - claude-code/claude-sonnet-4-6",
			input:               "claude-code/claude-sonnet-4-6",
			wantProvider:        ProviderClaudeCode,
			wantCanonicalID:     "claude-code/claude-sonnet-4-6",
			wantUpstreamModelID: "claude-sonnet-4-6",
			wantCCPAModelID:     "claude-sonnet-4-6",
			wantThinkingLevel:   ThinkingLevelNone,
			wantIsArgonFamily:   false,
		},
		{
			name:                "Claude Code - claude-code/claude-haiku-4-5",
			input:               "claude-code/claude-haiku-4-5",
			wantProvider:        ProviderClaudeCode,
			wantCanonicalID:     "claude-code/claude-haiku-4-5",
			wantUpstreamModelID: "claude-haiku-4-5-20251001",
			wantCCPAModelID:     "claude-haiku-4-5-20251001",
			wantThinkingLevel:   ThinkingLevelNone,
			wantIsArgonFamily:   false,
		},

		// ---------------------------------------------------------------------
		// 5. Alibaba Token Plan
		// ---------------------------------------------------------------------
		{
			name:                "Alibaba - alibaba/qwen3-coder-plus",
			input:               "alibaba/qwen3-coder-plus",
			wantProvider:        ProviderAlibaba,
			wantCanonicalID:     "alibaba/qwen3-coder-plus",
			wantUpstreamModelID: "qwen3-coder-plus",
			wantCCPAModelID:     "qwen3-coder-plus",
			wantIsArgonFamily:   false,
		},
		{
			name:                "Alibaba - alibaba/qwen3-max",
			input:               "alibaba/qwen3-max",
			wantProvider:        ProviderAlibaba,
			wantCanonicalID:     "alibaba/qwen3-max",
			wantUpstreamModelID: "qwen3-max",
			wantCCPAModelID:     "qwen3-max",
			wantThinkingLevel:   ThinkingLevelHigh,
			wantReasoningEffort: "high",
			wantIsArgonFamily:   false,
		},
		{
			name:                "Alibaba - alibaba/qwq-plus",
			input:               "alibaba/qwq-plus",
			wantProvider:        ProviderAlibaba,
			wantCanonicalID:     "alibaba/qwq-plus",
			wantUpstreamModelID: "qwq-plus",
			wantCCPAModelID:     "qwq-plus",
			wantThinkingLevel:   ThinkingLevelHigh,
			wantReasoningEffort: "high",
			wantIncludeThoughts: true,
			wantIsArgonFamily:   false,
		},
		{
			name:                "Alibaba - alibaba/deepseek-r1",
			input:               "alibaba/deepseek-r1",
			wantProvider:        ProviderAlibaba,
			wantCanonicalID:     "alibaba/deepseek-r1",
			wantUpstreamModelID: "deepseek-r1",
			wantCCPAModelID:     "deepseek-r1",
			wantThinkingLevel:   ThinkingLevelHigh,
			wantReasoningEffort: "high",
			wantIncludeThoughts: true,
			wantIsArgonFamily:   false,
		},

		// ---------------------------------------------------------------------
		// 6. Combos
		// ---------------------------------------------------------------------
		{
			name:                "Combo - auto/best-coding",
			input:               "auto/best-coding",
			wantProvider:        ProviderAutoCombo,
			wantCanonicalID:     "auto/best-coding",
			wantUpstreamModelID: "gemini-4-carbon",
			wantCCPAModelID:     "gemini-4-carbon",
			wantThinkingLevel:   ThinkingLevelHigh,
			wantReasoningEffort: "high",
			wantIncludeThoughts: true,
			wantIsCombo:         true,
			checkFallbackPrefix: []string{
				"antigravity/gemini-4-carbon-high",
				"antigravity/gemini-4-argon-high",
				"alibaba/qwen3.8-max",
				"antigravity/gemini-3.1-pro-high",
			},
		},
		{
			name:                "Combo - auto/fast",
			input:               "auto/fast",
			wantProvider:        ProviderAutoCombo,
			wantCanonicalID:     "auto/fast",
			wantUpstreamModelID: "gemini-4-carbon",
			wantCCPAModelID:     "gemini-4-carbon",
			wantIsCombo:         true,
			checkFallbackPrefix: []string{
				"antigravity/gemini-4-carbon-low",
				"antigravity/gemini-3.8-flash-low",
				"alibaba/qwen3-coder-plus",
				"alibaba/qwen3.8-flash",
			},
		},
		{
			name:                "Combo - auto/reasoning",
			input:               "auto/reasoning",
			wantProvider:        ProviderAutoCombo,
			wantCanonicalID:     "auto/reasoning",
			wantUpstreamModelID: "gemini-4-carbon",
			wantCCPAModelID:     "gemini-4-carbon",
			wantThinkingLevel:   ThinkingLevelHigh,
			wantReasoningEffort: "high",
			wantIncludeThoughts: true,
			wantIsCombo:         true,
			checkFallbackPrefix: []string{
				"antigravity/gemini-4-carbon-high",
				"claude-code/claude-opus-4-6-thinking",
				"antigravity/gemini-4-argon-high",
				"alibaba/deepseek-v4-pro-0813",
			},
		},

		// ---------------------------------------------------------------------
		// Custom Default Fallbacks
		// ---------------------------------------------------------------------
		{
			name:                 "Custom fallback list passed to resolver",
			input:                "gemini-4-argon-high",
			customFallbacks:      []string{"alibaba/custom-model-1", "claude-code/custom-model-2"},
			wantProvider:         ProviderAntigravity,
			wantCanonicalID:      "antigravity/gemini-4-argon-high",
			wantUpstreamModelID:  "gemini-4-argon",
			wantCCPAModelID:      "gemini-4-argon",
			wantThinkingLevel:    ThinkingLevelHigh,
			wantReasoningEffort:  "high",
			wantIncludeThoughts:  true,
			wantPlaceholderEnum:  "MODEL_PLACEHOLDER_M417",
			wantIsArgonFamily:    true,
			checkFallbackContain: []string{"alibaba/custom-model-1", "claude-code/custom-model-2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := r.Resolve(tt.input, tt.customFallbacks)

			if got.RequestedID != tt.input {
				t.Errorf("RequestedID = %q, want %q", got.RequestedID, tt.input)
			}
			if got.Provider != tt.wantProvider {
				t.Errorf("Provider = %q, want %q", got.Provider, tt.wantProvider)
			}
			if got.CanonicalID != tt.wantCanonicalID {
				t.Errorf("CanonicalID = %q, want %q", got.CanonicalID, tt.wantCanonicalID)
			}
			if got.UpstreamModelID != tt.wantUpstreamModelID {
				t.Errorf("UpstreamModelID = %q, want %q", got.UpstreamModelID, tt.wantUpstreamModelID)
			}
			if tt.wantCCPAModelID != "" && got.CCPAModelID != tt.wantCCPAModelID {
				t.Errorf("CCPAModelID = %q, want %q", got.CCPAModelID, tt.wantCCPAModelID)
			}
			if got.ThinkingLevel != tt.wantThinkingLevel {
				t.Errorf("ThinkingLevel = %q, want %q", got.ThinkingLevel, tt.wantThinkingLevel)
			}
			if tt.wantReasoningEffort != "" && got.ReasoningEffort != tt.wantReasoningEffort {
				t.Errorf("ReasoningEffort = %q, want %q", got.ReasoningEffort, tt.wantReasoningEffort)
			}
			if got.IncludeThoughts != tt.wantIncludeThoughts {
				t.Errorf("IncludeThoughts = %v, want %v", got.IncludeThoughts, tt.wantIncludeThoughts)
			}
			if tt.wantPlaceholderEnum != "" && got.PlaceholderEnum != tt.wantPlaceholderEnum {
				t.Errorf("PlaceholderEnum = %q, want %q", got.PlaceholderEnum, tt.wantPlaceholderEnum)
			}
			if got.IsArgonFamily != tt.wantIsArgonFamily {
				t.Errorf("IsArgonFamily = %v, want %v", got.IsArgonFamily, tt.wantIsArgonFamily)
			}
			if got.IsCombo != tt.wantIsCombo {
				t.Errorf("IsCombo = %v, want %v", got.IsCombo, tt.wantIsCombo)
			}

			if len(got.FallbackChain) == 0 {
				t.Errorf("FallbackChain should not be empty")
			}

			for i, p := range tt.checkFallbackPrefix {
				if i >= len(got.FallbackChain) {
					t.Fatalf("FallbackChain too short: len = %d, expected at least %d elements", len(got.FallbackChain), len(tt.checkFallbackPrefix))
				}
				if got.FallbackChain[i] != p {
					t.Errorf("FallbackChain[%d] = %q, want %q", i, got.FallbackChain[i], p)
				}
			}

			for _, reqModel := range tt.checkFallbackContain {
				found := false
				for _, m := range got.FallbackChain {
					if m == reqModel {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("FallbackChain %v does not contain expected model %q", got.FallbackChain, reqModel)
				}
			}
		})
	}
}

func TestResolveModelHelper(t *testing.T) {
	modelsToTest := []string{
		"gemini-4-argon-high",
		"claude-code/claude-haiku-4-5",
		"alibaba/qwen3-coder-plus",
		"auto/best-coding",
		"gemini-3.8-flash-high",
	}

	resolver := NewResolver()

	for _, modelID := range modelsToTest {
		t.Run(modelID, func(t *testing.T) {
			direct := resolver.Resolve(modelID, nil)
			helper := ResolveModel(modelID, nil)

			if direct.CanonicalID != helper.CanonicalID {
				t.Errorf("helper CanonicalID = %q, direct CanonicalID = %q", helper.CanonicalID, direct.CanonicalID)
			}
			if direct.Provider != helper.Provider {
				t.Errorf("helper Provider = %q, direct Provider = %q", helper.Provider, direct.Provider)
			}
			if direct.UpstreamModelID != helper.UpstreamModelID {
				t.Errorf("helper UpstreamModelID = %q, direct UpstreamModelID = %q", helper.UpstreamModelID, direct.UpstreamModelID)
			}
			if direct.ThinkingLevel != helper.ThinkingLevel {
				t.Errorf("helper ThinkingLevel = %q, direct ThinkingLevel = %q", helper.ThinkingLevel, direct.ThinkingLevel)
			}
			if direct.IsArgonFamily != helper.IsArgonFamily {
				t.Errorf("helper IsArgonFamily = %v, direct IsArgonFamily = %v", helper.IsArgonFamily, direct.IsArgonFamily)
			}
			if direct.IsCombo != helper.IsCombo {
				t.Errorf("helper IsCombo = %v, direct IsCombo = %v", helper.IsCombo, direct.IsCombo)
			}
			if len(direct.FallbackChain) != len(helper.FallbackChain) {
				t.Errorf("FallbackChain len mismatch: helper = %d, direct = %d", len(helper.FallbackChain), len(direct.FallbackChain))
			}
		})
	}
}

func TestListModels(t *testing.T) {
	r := NewResolver()
	models := r.ListModels()

	if len(models) == 0 {
		t.Fatalf("ListModels() returned 0 models")
	}

	modelMap := make(map[string]OpenAIModelCard)
	for _, card := range models {
		if card.ID == "" {
			t.Errorf("Found model card with empty ID: %+v", card)
		}
		if card.Object != "model" {
			t.Errorf("Model card %q has object = %q, want 'model'", card.ID, card.Object)
		}
		if card.Created <= 0 {
			t.Errorf("Model card %q has invalid created timestamp: %d", card.ID, card.Created)
		}
		if card.Provider == "" {
			t.Errorf("Model card %q has empty provider field", card.ID)
		}
		modelMap[card.ID] = card

		// 7. Strict Requirement: Verify ListModels() contains NO legacy gemini-1.5 or gemini-2.5 models
		lowerID := strings.ToLower(card.ID)
		if strings.Contains(lowerID, "1.5") {
			t.Errorf("Found forbidden legacy model 1.5 in catalog: %q", card.ID)
		}
		if strings.Contains(lowerID, "2.5") {
			t.Errorf("Found forbidden legacy model 2.5 in catalog: %q", card.ID)
		}
		if strings.Contains(lowerID, "gemini-2.0") || strings.Contains(lowerID, "gemini-2-") {
			t.Errorf("Found forbidden legacy model 2.0 in catalog: %q", card.ID)
		}
		if strings.Contains(lowerID, "gemini-1.0") || strings.Contains(lowerID, "gemini-pro-vision") {
			t.Errorf("Found forbidden legacy model 1.0 in catalog: %q", card.ID)
		}
	}

	// Verify key representative models are present across the 3 providers and combos
	expectedModels := []struct {
		id       string
		provider string
	}{
		// Combos
		{"auto/best-coding", "auto"},
		{"auto/fast", "auto"},
		{"auto/reasoning", "auto"},

		// Provider 1: Antigravity Gen 4 & 3
		{"gemini-4-argon-high", "antigravity"},
		{"antigravity/gemini-4-argon-high", "antigravity"},
		{"gemini-3.8-flash-high", "antigravity"},
		{"antigravity/gemini-3.7-flash-high", "antigravity"},
		{"antigravity/gemini-3.1-pro-high", "antigravity"},
		{"claude-opus-5-5-thinking", "antigravity"},
		{"antigravity/claude-opus-4-6-thinking", "antigravity"},

		// Provider 2: Claude Code
		{"claude-code/claude-opus-4-6-thinking", "claude-code"},
		{"claude-code/claude-sonnet-4-6", "claude-code"},
		{"claude-code/claude-haiku-4-5", "claude-code"},

		// Provider 3: Alibaba Token Plan
		{"alibaba/qwen3-coder-plus", "alibaba"},
		{"alibaba/qwen3-max", "alibaba"},
		{"alibaba/qwq-plus", "alibaba"},
		{"alibaba/deepseek-r1", "alibaba"},
	}

	for _, em := range expectedModels {
		card, ok := modelMap[em.id]
		if !ok {
			t.Errorf("Expected model %q was not found in ListModels() catalog", em.id)
			continue
		}
		if card.Provider != em.provider {
			t.Errorf("Model %q provider = %q, want %q", em.id, card.Provider, em.provider)
		}
	}
}

func TestListCombos(t *testing.T) {
	r := NewResolver()

	defaultCombos := DefaultCombos()
	if len(defaultCombos) < 6 {
		t.Fatalf("DefaultCombos() length = %d, want at least 6", len(defaultCombos))
	}

	resolverCombos := r.ListCombos()
	if len(resolverCombos) < 6 {
		t.Fatalf("ListCombos() returned %d combos, want at least 6", len(resolverCombos))
	}

	comboMap := make(map[string]ComboModel)
	for _, c := range resolverCombos {
		if c.ID == "" {
			t.Errorf("Found combo with empty ID: %+v", c)
		}
		if c.Name == "" {
			t.Errorf("Combo %q has empty Name", c.ID)
		}
		if c.Description == "" {
			t.Errorf("Combo %q has empty Description", c.ID)
		}
		if len(c.Models) == 0 {
			t.Errorf("Combo %q has empty Models slice", c.ID)
		}
		comboMap[c.ID] = c
	}

	requiredCombos := []string{
		"auto/seo-master",
		"auto/best-coding",
		"auto/team-coding",
		"auto/council-pro",
		"auto/night-coding",
		"auto/fast",
		"auto/reasoning",
	}

	for _, reqID := range requiredCombos {
		combo, ok := comboMap[reqID]
		if !ok {
			t.Errorf("Required combo %q not found in ListCombos()", reqID)
			continue
		}
		if len(combo.Models) < 2 {
			t.Errorf("Combo %q should have at least 2 models in fallback chain, got %d", reqID, len(combo.Models))
		}
	}

	// Verify every built-in combo in DefaultCombos() includes at least one gemini-4-carbon model
	defaultComboMap := make(map[string]ComboModel, len(defaultCombos))
	for _, dc := range defaultCombos {
		defaultComboMap[dc.ID] = dc
		hasCarbon := false
		for _, m := range dc.Models {
			if strings.Contains(m, "gemini-4-carbon") {
				hasCarbon = true
				break
			}
		}
		if !hasCarbon {
			t.Errorf("DefaultCombos() combo %q does not include any gemini-4-carbon model: %v", dc.ID, dc.Models)
		}
	}

	// Verify primary Carbon 4 model placement in best-coding, reasoning, and fast combos
	if bestCoding := comboMap["auto/best-coding"]; len(bestCoding.Models) == 0 || bestCoding.Models[0] != "antigravity/gemini-4-carbon-high" {
		t.Errorf("auto/best-coding primary model = %v, want antigravity/gemini-4-carbon-high", bestCoding.Models)
	}
	if reasoning := comboMap["auto/reasoning"]; len(reasoning.Models) == 0 || reasoning.Models[0] != "antigravity/gemini-4-carbon-high" {
		t.Errorf("auto/reasoning primary model = %v, want antigravity/gemini-4-carbon-high", reasoning.Models)
	}
	if fastCombo := comboMap["auto/fast"]; len(fastCombo.Models) == 0 || fastCombo.Models[0] != "antigravity/gemini-4-carbon-low" {
		t.Errorf("auto/fast primary model = %v, want antigravity/gemini-4-carbon-low", fastCombo.Models)
	}
}
