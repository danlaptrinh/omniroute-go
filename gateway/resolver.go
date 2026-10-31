package gateway

import (
	"strings"
)

// defaultFallbackModels defines the default cross-provider fallback models across the 3 supported providers.
var defaultFallbackModels = []string{
	"antigravity/gemini-4-argon-high",
	"claude-code/claude-opus-4-6-thinking",
	"alibaba/deepseek-v4-pro",
	"alibaba/qwen3.8-max",
	"antigravity/claude-opus-4-6-thinking",
	"antigravity/gemini-3.8-flash-high",
	"alibaba/qwen3.8-flash",
}

// DefaultCombos returns the built-in OmniRoute-Go multi-provider combo stacks.
func DefaultCombos() []ComboModel {
	return []ComboModel{
		{
			ID:          "auto/best-coding",
			Name:        "Best Coding (Gemini 4 Argon -> Claude Code Opus 4.6 -> Alibaba Qwen3 Coder)",
			Description: "Ưu tiên Gemini 4 Argon High trên Antigravity, tự động failover sang Claude Code Opus 4.6 và Alibaba Token Plan Qwen3 Coder Plus",
			Models: []string{
				"antigravity/gemini-4-argon-high",
				"claude-code/claude-opus-4-6-thinking",
				"alibaba/qwen3-coder-plus",
				"antigravity/claude-opus-4-6-thinking",
			},
		},
		{
			ID:          "auto/fast",
			Name:        "Fast Coding (Gemini 3.8 Flash -> Qwen3 Coder -> Claude Haiku 4.5)",
			Description: "Tốc độ phản hồi cực nhanh qua Gemini 3.8 Flash, Qwen3 Coder Plus và Claude Haiku 4.5",
			Models: []string{
				"antigravity/gemini-3.8-flash-high",
				"alibaba/qwen3-coder-plus",
				"claude-code/claude-haiku-4-5",
			},
		},
		{
			ID:          "auto/reasoning",
			Name:        "Deep Reasoning (Gemini 4 Argon High -> Claude Opus -> Alibaba QwQ/DeepSeek-R1)",
			Description: "Chuyên suy luận sâu đa tầng qua 3 nhà cung cấp",
			Models: []string{
				"antigravity/gemini-4-argon-high",
				"claude-code/claude-opus-4-6-thinking",
				"alibaba/qwq-plus",
				"alibaba/deepseek-r1",
			},
		},
	}
}

// DefaultResolver implements ModelResolver for OmniRoute-Go v2.0 (Antigravity Gen 3/4, Claude Code, Alibaba Token Plan).
type DefaultResolver struct{}

// NewResolver returns a new DefaultResolver instance.
func NewResolver() *DefaultResolver {
	return &DefaultResolver{}
}

// ResolveModel is a package-level helper that resolves modelID using DefaultResolver.
func ResolveModel(modelID string, defaultFallbacks []string) ResolvedModel {
	return NewResolver().Resolve(modelID, defaultFallbacks)
}

// detectProviderAndSlug splits an incoming model ID into an explicit provider hint (if any) and normalized model slug.
func detectProviderAndSlug(raw string) (ProviderID, string) {
	s := strings.ToLower(strings.TrimSpace(raw))
	switch {
	case strings.HasPrefix(s, "auto/"):
		return ProviderAutoCombo, s
	case strings.HasPrefix(s, "claude-code/"):
		return ProviderClaudeCode, strings.TrimSpace(strings.TrimPrefix(s, "claude-code/"))
	case strings.HasPrefix(s, "cc/"):
		return ProviderClaudeCode, strings.TrimSpace(strings.TrimPrefix(s, "cc/"))
	case strings.HasPrefix(s, "anthropic/"):
		return ProviderClaudeCode, strings.TrimSpace(strings.TrimPrefix(s, "anthropic/"))
	case strings.HasPrefix(s, "alibaba/"):
		return ProviderAlibaba, strings.TrimSpace(strings.TrimPrefix(s, "alibaba/"))
	case strings.HasPrefix(s, "dashscope/"):
		return ProviderAlibaba, strings.TrimSpace(strings.TrimPrefix(s, "dashscope/"))
	case strings.HasPrefix(s, "bailian/"):
		return ProviderAlibaba, strings.TrimSpace(strings.TrimPrefix(s, "bailian/"))
	case strings.HasPrefix(s, "antigravity/"):
		return ProviderAntigravity, strings.TrimSpace(strings.TrimPrefix(s, "antigravity/"))
	case strings.HasPrefix(s, "agy/"):
		return ProviderAntigravity, strings.TrimSpace(strings.TrimPrefix(s, "agy/"))
	case strings.HasPrefix(s, "google/"):
		return ProviderAntigravity, strings.TrimSpace(strings.TrimPrefix(s, "google/"))
	case strings.HasPrefix(s, "models/"):
		return ProviderAntigravity, strings.TrimSpace(strings.TrimPrefix(s, "models/"))
	default:
		return "", s
	}
}

// buildFallbackChain constructs a deduplicated fallback chain starting with primary candidates
// followed by defaultFallbacks (or defaultFallbackModels if empty).
func buildFallbackChain(candidates []string, defaultFallbacks []string) []string {
	fallbacks := defaultFallbacks
	if len(fallbacks) == 0 {
		fallbacks = defaultFallbackModels
	}

	seen := make(map[string]struct{}, len(candidates)+len(fallbacks))
	chain := make([]string, 0, len(candidates)+len(fallbacks))

	add := func(m string) {
		m = strings.TrimSpace(m)
		if m == "" {
			return
		}
		if _, ok := seen[m]; !ok {
			seen[m] = struct{}{}
			chain = append(chain, m)
		}
	}

	for _, c := range candidates {
		add(c)
	}
	for _, f := range fallbacks {
		add(f)
	}
	return chain
}

// Resolve translates an incoming model ID into its canonical representation across the 3 providers.
func (r *DefaultResolver) Resolve(modelID string, defaultFallbacks []string) ResolvedModel {
	trimmed := strings.TrimSpace(modelID)
	// Check Model Aliases first (e.g. gpt-4o -> antigravity/claude-sonnet-4-6, cursor-fast -> alibaba/qwen3.8-flash)
	aliased := ResolveModelAlias(trimmed)
	provHint, norm := detectProviderAndSlug(aliased)

	res := ResolvedModel{
		RequestedID: trimmed,
		Provider:    provHint,
	}

	// 1. Check Combos first
	if provHint == ProviderAutoCombo || norm == "auto/best-coding" || norm == "best-coding" || norm == "auto/fast" || norm == "auto/reasoning" {
		for _, combo := range r.ListCombos() {
			if strings.EqualFold(combo.ID, norm) || strings.EqualFold(strings.TrimPrefix(combo.ID, "auto/"), norm) {
				res.Provider = ProviderAutoCombo
				res.IsCombo = true
				res.CanonicalID = combo.ID
				res.UpstreamModelID = "gemini-4-argon"
				res.CCPAModelID = "gemini-4-argon"
				res.OmniBaseModelID = combo.Models[0]
				if norm == "auto/fast" || norm == "fast" {
					res.ThinkingLevel = ""
					res.ReasoningEffort = ""
					res.IncludeThoughts = false
				} else {
					res.ThinkingLevel = ThinkingLevelHigh
					res.ReasoningEffort = "high"
					res.IncludeThoughts = true
				}
				res.FallbackChain = buildFallbackChain(combo.Models, defaultFallbacks)
				return res
			}
		}
	}

	// 2. Provider: Alibaba Token Plan (Bailian / DashScope Coding Plan)
	if provHint == ProviderAlibaba ||
		strings.HasPrefix(norm, "qwen") ||
		strings.HasPrefix(norm, "qwq") ||
		strings.HasPrefix(norm, "deepseek-") ||
		strings.HasPrefix(norm, "glm-") {
		res.Provider = ProviderAlibaba
		switch norm {
		case "qwen3.8-max":
			res.CanonicalID = "alibaba/qwen3.8-max"
			res.UpstreamModelID = "qwen3.8-max"
			res.ThinkingLevel = ThinkingLevelHigh
			res.ReasoningEffort = "high"
		case "qwen3.8-flash":
			res.CanonicalID = "alibaba/qwen3.8-flash"
			res.UpstreamModelID = "qwen3.8-flash"
		case "deepseek-v4-pro":
			res.CanonicalID = "alibaba/deepseek-v4-pro"
			res.UpstreamModelID = "deepseek-v4-pro"
			res.ThinkingLevel = ThinkingLevelHigh
			res.ReasoningEffort = "high"
			res.IncludeThoughts = true
		case "deepseek-v4.1-flash":
			res.CanonicalID = "alibaba/deepseek-v4.1-flash"
			res.UpstreamModelID = "deepseek-v4.1-flash"
		case "glm-5.3":
			res.CanonicalID = "alibaba/glm-5.3"
			res.UpstreamModelID = "glm-5.3"
		case "qwen3-coder-plus", "qwen3-coder", "qwen-coder-plus":
			res.CanonicalID = "alibaba/qwen3-coder-plus"
			res.UpstreamModelID = "qwen3-coder-plus"
		case "qwen3-coder-turbo", "qwen-coder-turbo":
			res.CanonicalID = "alibaba/qwen3-coder-turbo"
			res.UpstreamModelID = "qwen3-coder-turbo"
		case "qwen3-max":
			res.CanonicalID = "alibaba/qwen3-max"
			res.UpstreamModelID = "qwen3-max"
			res.ThinkingLevel = ThinkingLevelHigh
			res.ReasoningEffort = "high"
		case "qwen3-235b-a22b":
			res.CanonicalID = "alibaba/qwen3-235b-a22b"
			res.UpstreamModelID = "qwen3-235b-a22b"
			res.ThinkingLevel = ThinkingLevelHigh
			res.ReasoningEffort = "high"
			res.IncludeThoughts = true
		case "qwen3-32b":
			res.CanonicalID = "alibaba/qwen3-32b"
			res.UpstreamModelID = "qwen3-32b"
		case "qwq-plus", "qwq-32b":
			res.CanonicalID = "alibaba/qwq-plus"
			res.UpstreamModelID = "qwq-plus"
			res.ThinkingLevel = ThinkingLevelHigh
			res.ReasoningEffort = "high"
			res.IncludeThoughts = true
		case "qwen-max", "qwen-max-latest":
			res.CanonicalID = "alibaba/qwen-max"
			res.UpstreamModelID = "qwen-max"
		case "qwen-plus", "qwen-plus-latest":
			res.CanonicalID = "alibaba/qwen-plus"
			res.UpstreamModelID = "qwen-plus"
		case "deepseek-r1":
			res.CanonicalID = "alibaba/deepseek-r1"
			res.UpstreamModelID = "deepseek-r1"
			res.ThinkingLevel = ThinkingLevelHigh
			res.ReasoningEffort = "high"
			res.IncludeThoughts = true
		case "deepseek-v3":
			res.CanonicalID = "alibaba/deepseek-v3"
			res.UpstreamModelID = "deepseek-v3"
		default:
			res.CanonicalID = "alibaba/" + norm
			res.UpstreamModelID = norm
		}
		res.CCPAModelID = res.UpstreamModelID
		res.OmniBaseModelID = res.CanonicalID
		res.FallbackChain = buildFallbackChain([]string{res.CanonicalID, res.UpstreamModelID}, defaultFallbacks)
		return res
	}

	// 3. Provider: Claude Code (when explicitly prefixed with claude-code/, cc/, anthropic/, or bare claude-haiku-4-5 / claude-opus-4-6)
	if provHint == ProviderClaudeCode ||
		norm == "claude-haiku-4-5" ||
		norm == "claude-opus-4-6" ||
		norm == "claude-sonnet-4-6-thinking" {
		res.Provider = ProviderClaudeCode
		switch norm {
		case "claude-opus-4-6-thinking", "claude-opus-4-6-high":
			res.CanonicalID = "claude-code/claude-opus-4-6-thinking"
			res.UpstreamModelID = "claude-opus-4-6"
			res.ThinkingLevel = ThinkingLevelHigh
			res.ReasoningEffort = "high"
			res.IncludeThoughts = true
		case "claude-opus-4-6":
			res.CanonicalID = "claude-code/claude-opus-4-6"
			res.UpstreamModelID = "claude-opus-4-6"
		case "claude-sonnet-4-6-thinking", "claude-sonnet-4-6-high":
			res.CanonicalID = "claude-code/claude-sonnet-4-6-thinking"
			res.UpstreamModelID = "claude-sonnet-4-6"
			res.ThinkingLevel = ThinkingLevelHigh
			res.ReasoningEffort = "high"
			res.IncludeThoughts = true
		case "claude-sonnet-4-6":
			res.CanonicalID = "claude-code/claude-sonnet-4-6"
			res.UpstreamModelID = "claude-sonnet-4-6"
		case "claude-opus-5-5-thinking", "claude-opus-5-5", "claude-opus-5.5":
			res.CanonicalID = "claude-code/claude-opus-5-5-thinking"
			res.UpstreamModelID = "claude-opus-5-5-20261001"
			res.ThinkingLevel = ThinkingLevelHigh
			res.ReasoningEffort = "high"
			res.IncludeThoughts = true
		case "claude-sonnet-5-5-thinking", "claude-sonnet-5-5", "claude-sonnet-5.5":
			res.CanonicalID = "claude-code/claude-sonnet-5-5-thinking"
			res.UpstreamModelID = "claude-sonnet-5-5-20261001"
			res.ThinkingLevel = ThinkingLevelHigh
			res.ReasoningEffort = "high"
			res.IncludeThoughts = true
		case "claude-haiku-4-5", "claude-haiku-4-5-20251001":
			res.CanonicalID = "claude-code/claude-haiku-4-5"
			res.UpstreamModelID = "claude-haiku-4-5-20251001"
		default:
			res.CanonicalID = "claude-code/" + norm
			res.UpstreamModelID = norm
		}
		res.CCPAModelID = res.UpstreamModelID
		res.OmniBaseModelID = res.CanonicalID
		res.FallbackChain = buildFallbackChain([]string{res.CanonicalID, res.UpstreamModelID}, defaultFallbacks)
		return res
	}

	// 4. Provider: Antigravity (Strictly Gemini 4, Gemini 3, and Claude on Antigravity)
	res.Provider = ProviderAntigravity
	var extraCandidates []string

	switch norm {
	// -------------------------------------------------------------------------
	// Gemini 4 Argon Family
	// -------------------------------------------------------------------------
	case "gemini-4-argon-high", "gemini-4-argon:high", "gemini 4 argon high", "model_placeholder_m417", "m417",
		"gemini-4-argon", "gemini 4 argon":
		res.CanonicalID = "antigravity/gemini-4-argon-high"
		res.UpstreamModelID = "gemini-4-argon"
		res.CCPAModelID = "gemini-4-argon"
		res.OmniBaseModelID = "antigravity/gemini-4-argon"
		res.ThinkingLevel = ThinkingLevelHigh
		res.ReasoningEffort = "high"
		res.IncludeThoughts = true
		res.PlaceholderEnum = "MODEL_PLACEHOLDER_M417"
		res.IsArgonFamily = true

	case "gemini-4-argon-medium", "gemini-4-argon:medium", "gemini 4 argon medium", "model_placeholder_m418", "m418":
		res.CanonicalID = "antigravity/gemini-4-argon-medium"
		res.UpstreamModelID = "gemini-4-argon"
		res.CCPAModelID = "gemini-4-argon"
		res.OmniBaseModelID = "antigravity/gemini-4-argon"
		res.ThinkingLevel = ThinkingLevelMedium
		res.ReasoningEffort = "medium"
		res.IncludeThoughts = true
		res.PlaceholderEnum = "MODEL_PLACEHOLDER_M418"
		res.IsArgonFamily = true

	case "gemini-4-argon-low", "gemini-4-argon:low", "gemini 4 argon low", "model_placeholder_m419", "m419":
		res.CanonicalID = "antigravity/gemini-4-argon-low"
		res.UpstreamModelID = "gemini-4-argon"
		res.CCPAModelID = "gemini-4-argon"
		res.OmniBaseModelID = "antigravity/gemini-4-argon"
		res.ThinkingLevel = ThinkingLevelLow
		res.ReasoningEffort = "low"
		res.IncludeThoughts = true
		res.PlaceholderEnum = "MODEL_PLACEHOLDER_M419"
		res.IsArgonFamily = true

	// -------------------------------------------------------------------------
	// Gemini Image Generation Models (Gemini 3.1 Flash Image / Imagen 3)
	// -------------------------------------------------------------------------
	case "gemini-3.1-flash-image", "gemini-3-pro-image", "gemini-image", "imagen-3", "imagen-3.0-generate-002", "imagen", "imagen3":
		res.CanonicalID = "antigravity/gemini-3-pro-image"
		res.UpstreamModelID = "gemini-3.1-flash-image"
		res.CCPAModelID = "gemini-3.1-flash-image"
		res.OmniBaseModelID = "antigravity/gemini-3-pro-image"
		res.Provider = ProviderAntigravity

	// -------------------------------------------------------------------------
	// Gemini 3.8 / 3.7 / 3.6 / 3.5 / 3.1 / 3.0 Series (Antigravity Gen 3)
	// -------------------------------------------------------------------------
	case "gemini-3.8-flash-high", "gemini-3.8-flash:high", "gemini 3.8 flash high", "model_placeholder_m421", "m421",
		"gemini-3.8-flash", "gemini 3.8 flash", "gemini-3.8-flash-tiered", "model_placeholder_m322", "m322":
		res.CanonicalID = "antigravity/gemini-3.8-flash-high"
		res.UpstreamModelID = "gemini-3.8-flash"
		res.CCPAModelID = "gemini-3.8-flash"
		res.OmniBaseModelID = "antigravity/gemini-3.8-flash"
		res.ThinkingLevel = ThinkingLevelHigh
		res.ReasoningEffort = "high"
		res.IncludeThoughts = true
		res.PlaceholderEnum = "MODEL_PLACEHOLDER_M421"

	case "gemini-3.8-flash-medium", "gemini-3.8-flash:medium", "gemini 3.8 flash medium", "model_placeholder_m422", "m422":
		res.CanonicalID = "antigravity/gemini-3.8-flash-medium"
		res.UpstreamModelID = "gemini-3.8-flash"
		res.CCPAModelID = "gemini-3.8-flash"
		res.OmniBaseModelID = "antigravity/gemini-3.8-flash"
		res.ThinkingLevel = ThinkingLevelMedium
		res.ReasoningEffort = "medium"
		res.IncludeThoughts = true
		res.PlaceholderEnum = "MODEL_PLACEHOLDER_M422"

	case "gemini-3.8-flash-low", "gemini-3.8-flash:low", "gemini 3.8 flash low", "model_placeholder_m423", "m423":
		res.CanonicalID = "antigravity/gemini-3.8-flash-low"
		res.UpstreamModelID = "gemini-3.8-flash"
		res.CCPAModelID = "gemini-3.8-flash"
		res.OmniBaseModelID = "antigravity/gemini-3.8-flash"
		res.ThinkingLevel = ThinkingLevelLow
		res.ReasoningEffort = "low"
		res.IncludeThoughts = true
		res.PlaceholderEnum = "MODEL_PLACEHOLDER_M423"

	case "gemini-3.7-flash-high", "gemini-3.7-flash":
		res.CanonicalID = "antigravity/gemini-3.7-flash-high"
		res.UpstreamModelID = "gemini-3.7-flash"
		res.CCPAModelID = "gemini-3.7-flash"
		res.OmniBaseModelID = "antigravity/gemini-3.7-flash"
		res.ThinkingLevel = ThinkingLevelHigh
		res.ReasoningEffort = "high"
		res.IncludeThoughts = true

	case "gemini-3.6-flash-high", "gemini-3.6-flash":
		res.CanonicalID = "antigravity/gemini-3.6-flash-high"
		res.UpstreamModelID = "gemini-3.6-flash"
		res.CCPAModelID = "gemini-3.6-flash"
		res.OmniBaseModelID = "antigravity/gemini-3.6-flash"
		res.ThinkingLevel = ThinkingLevelHigh
		res.ReasoningEffort = "high"
		res.IncludeThoughts = true

	case "gemini-3.5-flash-lite", "model_placeholder_m198", "m198":
		res.CanonicalID = "antigravity/gemini-3.5-flash-lite"
		res.UpstreamModelID = "gemini-3.5-flash-lite"
		res.CCPAModelID = "gemini-3.5-flash-lite"
		res.OmniBaseModelID = "antigravity/gemini-3.5-flash-lite"
		res.ThinkingLevel = ThinkingLevelLow
		res.ReasoningEffort = "low"
		res.IncludeThoughts = true
		res.PlaceholderEnum = "MODEL_PLACEHOLDER_M198"

	case "gemini-3.1-pro-high", "gemini-3.1-pro:high", "gemini-3.1-pro":
		res.CanonicalID = "antigravity/gemini-3.1-pro-high"
		res.UpstreamModelID = "gemini-3.1-pro-preview"
		res.CCPAModelID = "gemini-3.1-pro-preview"
		res.OmniBaseModelID = "antigravity/gemini-3.1-pro-high"
		res.ThinkingLevel = ThinkingLevelHigh
		res.ReasoningEffort = "high"
		res.IncludeThoughts = true

	case "gemini-3.1-pro-low", "gemini-3.1-pro:low":
		res.CanonicalID = "antigravity/gemini-3.1-pro-low"
		res.UpstreamModelID = "gemini-3.1-pro-preview"
		res.CCPAModelID = "gemini-3.1-pro-preview"
		res.OmniBaseModelID = "antigravity/gemini-3.1-pro-low"
		res.ThinkingLevel = ThinkingLevelLow
		res.ReasoningEffort = "low"
		res.IncludeThoughts = true

	case "gemini-3.1-flash-lite", "model_placeholder_m50", "m50":
		res.CanonicalID = "antigravity/gemini-3.1-flash-lite"
		res.UpstreamModelID = "gemini-3.1-flash-lite"
		res.CCPAModelID = "gemini-3.1-flash-lite"
		res.OmniBaseModelID = "antigravity/gemini-3.1-flash-lite"
		res.ThinkingLevel = ThinkingLevelLow
		res.ReasoningEffort = "low"
		res.IncludeThoughts = true
		res.PlaceholderEnum = "MODEL_PLACEHOLDER_M50"

	case "gemini-3-flash", "model_placeholder_m18", "m18":
		res.CanonicalID = "antigravity/gemini-3-flash"
		res.UpstreamModelID = "gemini-3-flash"
		res.CCPAModelID = "gemini-3-flash"
		res.OmniBaseModelID = "antigravity/gemini-3-flash"
		res.ThinkingLevel = ThinkingLevelHigh
		res.ReasoningEffort = "high"
		res.IncludeThoughts = true
		res.PlaceholderEnum = "MODEL_PLACEHOLDER_M18"

	// -------------------------------------------------------------------------
	// Claude on Antigravity (5.5 & 4.6 Thinking)
	// -------------------------------------------------------------------------
	case "claude-opus-5-5-thinking", "claude-opus-5-5-high", "claude-opus-5-5", "claude-opus-5.5", "model_placeholder_m412", "m412":
		res.CanonicalID = "antigravity/claude-opus-5-5-thinking"
		res.UpstreamModelID = "claude-opus-5-5@default"
		res.CCPAModelID = "claude-opus-5-5@default"
		res.OmniBaseModelID = "antigravity/claude-opus-5-5-thinking"
		res.ThinkingLevel = ThinkingLevelHigh
		res.ReasoningEffort = "high"
		res.IncludeThoughts = true
		res.PlaceholderEnum = "MODEL_PLACEHOLDER_M412"

	case "claude-sonnet-5-5-thinking", "claude-sonnet-5-5", "claude-sonnet-5.5", "model_placeholder_m420", "m420":
		res.CanonicalID = "antigravity/claude-sonnet-5-5-thinking"
		res.UpstreamModelID = "claude-sonnet-5-5@default"
		res.CCPAModelID = "claude-sonnet-5-5@default"
		res.OmniBaseModelID = "antigravity/claude-sonnet-5-5-thinking"
		res.ThinkingLevel = ThinkingLevelHigh
		res.ReasoningEffort = "high"
		res.IncludeThoughts = true
		res.PlaceholderEnum = "MODEL_PLACEHOLDER_M420"

	case "claude-opus-4-6-thinking", "model_placeholder_m26", "m26":
		res.CanonicalID = "antigravity/claude-opus-4-6-thinking"
		res.UpstreamModelID = "claude-opus-4-6-thinking"
		res.CCPAModelID = "claude-opus-4-6-thinking"
		res.OmniBaseModelID = "antigravity/claude-opus-4-6-thinking"
		res.ThinkingLevel = ThinkingLevelHigh
		res.ReasoningEffort = "high"
		res.IncludeThoughts = true
		res.PlaceholderEnum = "MODEL_PLACEHOLDER_M26"

	case "claude-sonnet-4-6", "model_placeholder_m35", "m35":
		res.CanonicalID = "antigravity/claude-sonnet-4-6"
		res.UpstreamModelID = "claude-sonnet-4-6"
		res.CCPAModelID = "claude-sonnet-4-6"
		res.OmniBaseModelID = "antigravity/claude-sonnet-4-6"
		res.ThinkingLevel = ThinkingLevelHigh
		res.ReasoningEffort = "high"
		res.IncludeThoughts = true
		res.PlaceholderEnum = "MODEL_PLACEHOLDER_M35"

	// -------------------------------------------------------------------------
	// Generic / Other Models
	// -------------------------------------------------------------------------
	default:
		res.CanonicalID = trimmed
		res.UpstreamModelID = norm
		res.CCPAModelID = norm
		res.OmniBaseModelID = trimmed

		suffixes := []struct {
			suffix string
			level  ThinkingLevel
			effort string
		}{
			{"-high", ThinkingLevelHigh, "high"},
			{"-medium", ThinkingLevelMedium, "medium"},
			{"-low", ThinkingLevelLow, "low"},
		}

		for _, s := range suffixes {
			if strings.HasSuffix(norm, s.suffix) {
				strippedNorm := strings.TrimSuffix(norm, s.suffix)
				strippedRaw := trimmed
				if len(trimmed) > len(s.suffix) && strings.EqualFold(trimmed[len(trimmed)-len(s.suffix):], s.suffix) {
					strippedRaw = trimmed[:len(trimmed)-len(s.suffix)]
				}
				res.UpstreamModelID = strippedNorm
				res.CCPAModelID = strippedNorm
				res.ThinkingLevel = s.level
				res.ReasoningEffort = s.effort
				res.IncludeThoughts = true
				extraCandidates = append(extraCandidates, strippedRaw, strippedNorm)
				break
			}
		}
	}

	primaryCandidates := []string{res.OmniBaseModelID, res.CCPAModelID}
	primaryCandidates = append(primaryCandidates, extraCandidates...)
	res.FallbackChain = buildFallbackChain(primaryCandidates, defaultFallbacks)

	return res
}

// ListCombos returns all configured OmniRoute-Go combo models.
func (r *DefaultResolver) ListCombos() []ComboModel {
	saved := LoadSavedAuthConfig()
	if len(saved.CustomCombos) > 0 {
		return saved.CustomCombos
	}
	return DefaultCombos()
}

// ListModels returns OpenAI-compatible model cards strictly for the 3 providers:
//  1. antigravity (Gemini 4 Argon, Gemini 3 Series, Antigravity Claude — NO legacy 1.5/2.0/2.5)
//  2. claude-code (Anthropic Claude Code Pro/Max & API)
//  3. alibaba (Alibaba Token Plan / Bailian Coding Plan)
//     + Combos (auto/best-coding, auto/fast, auto/reasoning)
func (r *DefaultResolver) ListModels() []OpenAIModelCard {
	const createdTimestamp int64 = 1740000000

	models := []struct {
		id       string
		ownedBy  string
		provider string
		tier     string
	}{
		// --- Combos ---
		{"auto/best-coding", "omniroute-go", "auto", "combo"},
		{"auto/fast", "omniroute-go", "auto", "combo"},
		{"auto/reasoning", "omniroute-go", "auto", "combo"},

		// --- Provider 1: Antigravity (ONLY Gen 4, Gen 3 & Claude) ---
		{"gemini-4-argon-high", "antigravity", "antigravity", "gen4-argon"},
		{"gemini-4-argon-medium", "antigravity", "antigravity", "gen4-argon"},
		{"gemini-4-argon-low", "antigravity", "antigravity", "gen4-argon"},
		{"gemini-4-argon", "antigravity", "antigravity", "gen4-argon"},
		{"antigravity/gemini-4-argon-high", "antigravity", "antigravity", "gen4-argon"},
		{"antigravity/gemini-4-argon-medium", "antigravity", "antigravity", "gen4-argon"},
		{"antigravity/gemini-4-argon-low", "antigravity", "antigravity", "gen4-argon"},
		{"antigravity/gemini-4-argon", "antigravity", "antigravity", "gen4-argon"},
		{"gemini-3.8-flash-high", "antigravity", "antigravity", "gen3-flash"},
		{"gemini-3.8-flash-medium", "antigravity", "antigravity", "gen3-flash"},
		{"gemini-3.8-flash-low", "antigravity", "antigravity", "gen3-flash"},
		{"antigravity/gemini-3.8-flash-tiered", "antigravity", "antigravity", "gen3-flash"},
		{"antigravity/gemini-3.7-flash-high", "antigravity", "antigravity", "gen3-flash"},
		{"antigravity/gemini-3.6-flash-high", "antigravity", "antigravity", "gen3-flash"},
		{"antigravity/gemini-3.5-flash-lite", "antigravity", "antigravity", "gen3-flash"},
		{"antigravity/gemini-3.1-pro-high", "antigravity", "antigravity", "gen3-pro"},
		{"antigravity/gemini-3.1-pro-low", "antigravity", "antigravity", "gen3-pro"},
		{"antigravity/gemini-3.1-flash-lite", "antigravity", "antigravity", "gen3-flash"},
		{"antigravity/gemini-3-flash", "antigravity", "antigravity", "gen3-flash"},
		{"claude-opus-5-5-thinking", "antigravity", "antigravity", "claude-thinking"},
		{"claude-sonnet-5-5-thinking", "antigravity", "antigravity", "claude-thinking"},
		{"antigravity/claude-opus-4-6-thinking", "antigravity", "antigravity", "claude-thinking"},
		{"antigravity/claude-sonnet-4-6", "antigravity", "antigravity", "claude-thinking"},
		{"gemini-3-pro-image", "antigravity", "antigravity", "image-generation"},
		{"imagen-3", "antigravity", "antigravity", "image-generation"},
		{"imagen-3.0-generate-002", "antigravity", "antigravity", "image-generation"},
		{"antigravity/gemini-3-pro-image", "antigravity", "antigravity", "image-generation"},
		{"antigravity/imagen-3", "antigravity", "antigravity", "image-generation"},

		// --- Provider 2: Claude Code (Anthropic Pro/Max OAuth & API) ---
		{"claude-code/claude-opus-4-6-thinking", "claude-code", "claude-code", "claude-code"},
		{"claude-code/claude-opus-4-6", "claude-code", "claude-code", "claude-code"},
		{"claude-code/claude-sonnet-4-6-thinking", "claude-code", "claude-code", "claude-code"},
		{"claude-code/claude-sonnet-4-6", "claude-code", "claude-code", "claude-code"},
		{"claude-code/claude-opus-5-5-thinking", "claude-code", "claude-code", "claude-code"},
		{"claude-code/claude-sonnet-5-5-thinking", "claude-code", "claude-code", "claude-code"},
		{"claude-code/claude-haiku-4-5", "claude-code", "claude-code", "claude-code"},

		// --- Provider 3: Alibaba Token Plan (Bailian Coding Plan / DashScope) ---
		{"alibaba/qwen3.8-max", "alibaba", "alibaba", "token-plan"},
		{"alibaba/qwen3.8-flash", "alibaba", "alibaba", "token-plan"},
		{"alibaba/deepseek-v4-pro", "alibaba", "alibaba", "token-plan"},
		{"alibaba/deepseek-v4.1-flash", "alibaba", "alibaba", "token-plan"},
		{"alibaba/glm-5.3", "alibaba", "alibaba", "token-plan"},
		{"alibaba/qwen3-coder-plus", "alibaba", "alibaba", "token-plan"},
		{"alibaba/qwen3-coder-turbo", "alibaba", "alibaba", "token-plan"},
		{"alibaba/qwen3-max", "alibaba", "alibaba", "token-plan"},
		{"alibaba/qwen3-235b-a22b", "alibaba", "alibaba", "token-plan"},
		{"alibaba/qwen3-32b", "alibaba", "alibaba", "token-plan"},
		{"alibaba/qwq-plus", "alibaba", "alibaba", "token-plan"},
		{"alibaba/qwen-max", "alibaba", "alibaba", "token-plan"},
		{"alibaba/qwen-plus", "alibaba", "alibaba", "token-plan"},
		{"alibaba/deepseek-r1", "alibaba", "alibaba", "token-plan"},
		{"alibaba/deepseek-v3", "alibaba", "alibaba", "token-plan"},
	}

	cards := make([]OpenAIModelCard, 0, len(models))
	for _, m := range models {
		cards = append(cards, OpenAIModelCard{
			ID:       m.id,
			Object:   "model",
			Created:  createdTimestamp,
			OwnedBy:  m.ownedBy,
			Provider: m.provider,
			Tier:     m.tier,
		})
	}
	return cards
}
