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

// DefaultCombos returns the built-in OmniRoute-Go multi-provider combo stacks and collaborative multi-model pipelines.
func DefaultCombos() []ComboModel {
	return []ComboModel{
		{
			ID:          "auto/best-coding",
			Name:        "Best Coding Failover (Gemini 4 Argon ➔ Claude Opus ➔ Qwen3 Coder)",
			Mode:        ComboModeFailover,
			Description: "Ưu tiên Gemini 4 Argon High trên Antigravity, tự động failover sang Claude Code Opus và Alibaba Token Plan Qwen3 Coder Plus",
			Models: []string{
				"antigravity/gemini-4-argon-high",
				"claude-code/claude-opus-4-6-thinking",
				"alibaba/qwen3-coder-plus",
				"claude-code/claude-opus-5-5",
				"alibaba/qwen3.8-max",
				"antigravity/claude-opus-4-6-thinking",
			},
			Roles:   []string{"Primary Coder", "Fallback 1 (Claude Code)", "Fallback 2 (Alibaba)", "Fallback 3 (Opus 5.5)", "Fallback 4 (Qwen 3.8 Max)", "Fallback 5 (Antigravity Claude)"},
			BuiltIn: true,
		},
		{
			ID:          "auto/team-coding",
			Name:        "Team Coding Pipeline: Kiến Trúc Sư ➔ Lập Trình Viên (Opus 5.5 ➔ Argon 4 ➔ Qwen 3.8 Max)",
			Mode:        ComboModePipeline,
			Description: "Kết nối nhiều model cùng làm việc nối tiếp: Model 1 (Claude Opus 5.5 / DeepSeek V4 Pro) lập kiến trúc & phân tích thuật toán ➔ Model 2 (Gemini 4 Argon High / Qwen 3.8 Max) triển khai code hoàn chỉnh.",
			Models: []string{
				"claude-code/claude-opus-5-5",
				"antigravity/gemini-4-argon-high",
				"alibaba/qwen3.8-max",
				"alibaba/deepseek-v4-pro-0813",
			},
			Roles: []string{
				"Bước 1: Kiến trúc sư & Phân tích (Architect)",
				"Bước 2: Lập trình viên triển khai (Executor)",
				"Bước 3: Chuyên gia tối ưu & Dự phòng (Synthesizer)",
				"Bước 4: Suy luận dự phòng (DeepSeek V4 Pro)",
			},
			HandoffPrompt: "Dựa trên bản thiết kế kiến trúc và phân tích kỹ thuật từ Model Bước 1 ở trên, hãy viết mã nguồn hoàn chỉnh, sạch, tối ưu hiệu năng và trả lời đầy đủ yêu cầu của người dùng.",
			BuiltIn:       true,
		},
		{
			ID:          "auto/council-pro",
			Name:        "Hội Đồng Đa Mô Hình Song Song (Mixture of Agents: Argon 4 + Opus 5.5 + Qwen 3.8 Max)",
			Mode:        ComboModeConsensus,
			Description: "Chạy song song 100% bằng Goroutines qua cả 3 nhà cung cấp (Antigravity Argon 4 + Claude Opus 5.5 + Alibaba Qwen 3.8 Max), sau đó tổng hợp câu trả lời thông minh và chính xác nhất.",
			Models: []string{
				"antigravity/gemini-4-argon-high",
				"claude-code/claude-opus-5-5",
				"alibaba/qwen3.8-max",
				"alibaba/deepseek-v4-pro-0813",
			},
			Roles: []string{
				"Chuyên gia 1 & Tổng hợp (Antigravity Argon 4)",
				"Chuyên gia 2 (Claude Opus 5.5)",
				"Chuyên gia 3 (Alibaba Qwen 3.8 Max)",
				"Chuyên gia 4 (DeepSeek V4 Pro)",
			},
			HandoffPrompt: "Bạn là Chủ tịch Hội đồng AI (Synthesizer). Hãy tổng hợp các góc nhìn và lời giải từ các mô hình chuyên gia ở trên để đưa ra câu trả lời cuối cùng chính xác, toàn diện và tối ưu nhất.",
			BuiltIn:       true,
		},
		{
			ID:          "auto/night-coding",
			Name:        "Combo Tối Ưu Giảm Giá Ban Đêm (⚡ 40%-50% Off Nights: Qwen 3.8 Max ➔ DeepSeek V4.1 ➔ Argon 4)",
			Mode:        ComboModeNightSaver,
			Description: "Ưu tiên các model giảm 40%-50% token ban đêm của Alibaba Cloud (Qwen 3.8 Max, DeepSeek V4.1 Flash 1017, DeepSeek V4 Pro 0813) kết hợp dự phòng sang Gemini 4 Argon High và Claude Opus 5.5.",
			Models: []string{
				"alibaba/qwen3.8-max",
				"alibaba/deepseek-v4-pro-0813",
				"alibaba/deepseek-v4.1-flash-1017",
				"alibaba/qwen3.8-flash",
				"antigravity/gemini-4-argon-high",
				"claude-code/claude-opus-5-5",
			},
			Roles: []string{
				"Ưu tiên 1 (⚡ 40% Off Nights)",
				"Ưu tiên 2 (⚡ Night 50% Off)",
				"Ưu tiên 3 (⚡ 50% Off Nights)",
				"Ưu tiên 4 (⚡ 40% Off Nights)",
				"Dự phòng 5 (Antigravity Argon 4)",
				"Dự phòng 6 (Claude Opus 5.5)",
			},
			BuiltIn: true,
		},
		{
			ID:          "auto/fast",
			Name:        "Fast Coding (Gemini 3.8 Flash ➔ Qwen3 Coder / 3.8 Flash ➔ Claude Haiku 5.5/4.5)",
			Mode:        ComboModeFailover,
			Description: "Tốc độ phản hồi cực nhanh qua Gemini 3.8 Flash, Qwen 3.8 Flash / Coder Plus và Claude Haiku",
			Models: []string{
				"antigravity/gemini-3.8-flash-high",
				"alibaba/qwen3-coder-plus",
				"claude-code/claude-haiku-4-5",
				"alibaba/qwen3.8-flash",
				"claude-code/claude-haiku-5-5",
			},
			Roles:   []string{"Tốc độ cao 1", "Tốc độ cao 2", "Tốc độ cao 3", "Night 40% Off", "Haiku 5.5"},
			BuiltIn: true,
		},
		{
			ID:          "auto/reasoning",
			Name:        "Deep Reasoning Pipeline (Argon 4 High ➔ Claude Opus Thinking ➔ DeepSeek V4 Pro / R1)",
			Mode:        ComboModePipeline,
			Description: "Chuyên suy luận sâu đa tầng nối tiếp qua 3 nhà cung cấp",
			Models: []string{
				"antigravity/gemini-4-argon-high",
				"claude-code/claude-opus-4-6-thinking",
				"alibaba/qwq-plus",
				"alibaba/deepseek-r1",
				"alibaba/deepseek-v4-pro-0813",
				"claude-code/claude-opus-5-5-thinking",
			},
			Roles: []string{
				"Bước 1: Suy luận Toán & Logic (Argon 4)",
				"Bước 2: Phản biện & Tổng hợp (Claude Opus Thinking)",
				"Bước 3: Kiểm chứng (Alibaba QwQ)",
				"Bước 4: DeepSeek R1",
				"Bước 5: DeepSeek V4 Pro (0813)",
				"Bước 6: Claude Opus 5.5 Thinking",
			},
			HandoffPrompt: "Hãy kiểm chứng, bổ sung lập luận chặt chẽ và hoàn thiện câu trả lời dựa trên phân tích suy luận của Model Bước 1.",
			BuiltIn:       true,
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

	// 1. Check Combos first (both built-in and custom user-created combos)
	for _, combo := range r.ListCombos() {
		if strings.EqualFold(combo.ID, norm) ||
			strings.EqualFold(combo.ID, aliased) ||
			strings.EqualFold(strings.TrimPrefix(combo.ID, "auto/"), norm) {
			res.Provider = ProviderAutoCombo
			res.IsCombo = true
			res.CanonicalID = combo.ID
			mode := combo.Mode
			if mode == "" {
				mode = ComboModeFailover
			}
			res.ComboMode = mode
			res.ComboHandoff = combo.HandoffPrompt
			res.UpstreamModelID = "gemini-4-argon"
			res.CCPAModelID = "gemini-4-argon"
			if len(combo.Models) > 0 {
				res.OmniBaseModelID = combo.Models[0]
			}
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

	// 2. Provider: Alibaba Token Plan (Bailian / DashScope Coding Plan)
	if provHint == ProviderAlibaba ||
		strings.HasPrefix(norm, "qwen") ||
		strings.HasPrefix(norm, "qwq") ||
		strings.HasPrefix(norm, "deepseek-") ||
		strings.HasPrefix(norm, "glm-") ||
		strings.HasPrefix(norm, "wan") ||
		strings.HasPrefix(norm, "happyhorse-") {
		res.Provider = ProviderAlibaba
		switch norm {
		case "auto":
			res.CanonicalID = "alibaba/auto"
			res.UpstreamModelID = "auto"
			res.ThinkingLevel = ThinkingLevelHigh
			res.ReasoningEffort = "high"
		case "qwen3.8-max":
			res.CanonicalID = "alibaba/qwen3.8-max"
			res.UpstreamModelID = "qwen3.8-max"
			res.ThinkingLevel = ThinkingLevelHigh
			res.ReasoningEffort = "high"
		case "qwen3.8-flash":
			res.CanonicalID = "alibaba/qwen3.8-flash"
			res.UpstreamModelID = "qwen3.8-flash"
		case "qwen3.7-plus":
			res.CanonicalID = "alibaba/qwen3.7-plus"
			res.UpstreamModelID = "qwen3.7-plus"
		case "qwen3.7-max":
			res.CanonicalID = "alibaba/qwen3.7-max"
			res.UpstreamModelID = "qwen3.7-max"
			res.ThinkingLevel = ThinkingLevelHigh
			res.ReasoningEffort = "high"
		case "qwen3.6-flash":
			res.CanonicalID = "alibaba/qwen3.6-flash"
			res.UpstreamModelID = "qwen3.6-flash"
		case "deepseek-v4.1-flash-1017":
			res.CanonicalID = "alibaba/deepseek-v4.1-flash-1017"
			res.UpstreamModelID = "deepseek-v4.1-flash-1017"
		case "deepseek-v4.1-flash":
			res.CanonicalID = "alibaba/deepseek-v4.1-flash"
			res.UpstreamModelID = "deepseek-v4.1-flash"
		case "deepseek-v4-pro-0813":
			res.CanonicalID = "alibaba/deepseek-v4-pro-0813"
			res.UpstreamModelID = "deepseek-v4-pro-0813"
			res.ThinkingLevel = ThinkingLevelHigh
			res.ReasoningEffort = "high"
			res.IncludeThoughts = true
		case "deepseek-v4-pro":
			res.CanonicalID = "alibaba/deepseek-v4-pro"
			res.UpstreamModelID = "deepseek-v4-pro"
			res.ThinkingLevel = ThinkingLevelHigh
			res.ReasoningEffort = "high"
			res.IncludeThoughts = true
		case "deepseek-v4-flash-0731":
			res.CanonicalID = "alibaba/deepseek-v4-flash-0731"
			res.UpstreamModelID = "deepseek-v4-flash-0731"
		case "glm-5.3":
			res.CanonicalID = "alibaba/glm-5.3"
			res.UpstreamModelID = "glm-5.3"
		case "glm-5.2":
			res.CanonicalID = "alibaba/glm-5.2"
			res.UpstreamModelID = "glm-5.2"
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

	// 3. Provider: Claude Code (when explicitly prefixed with claude-code/, cc/, anthropic/, or bare claude-code models)
	if provHint == ProviderClaudeCode ||
		norm == "claude-haiku-4-5" ||
		norm == "claude-haiku-5-5" ||
		norm == "claude-opus-4-6" ||
		norm == "claude-opus-4-7" ||
		norm == "claude-opus-4-8" ||
		norm == "claude-opus-5" ||
		norm == "claude-opus-5-5" ||
		norm == "claude-sonnet-5" ||
		norm == "claude-sonnet-5-5" ||
		strings.HasPrefix(norm, "claude-fable-") ||
		strings.HasPrefix(norm, "fable-") ||
		norm == "claude-sonnet-4-6-thinking" {
		res.Provider = ProviderClaudeCode
		switch norm {
		case "claude-opus-5-5", "claude-opus-5.5", "opus-5.5":
			res.CanonicalID = "claude-code/claude-opus-5-5"
			res.UpstreamModelID = "claude-opus-5-5"
			res.ThinkingLevel = ThinkingLevelHigh
			res.ReasoningEffort = "high"
			res.IncludeThoughts = true
		case "claude-opus-5-5-thinking":
			res.CanonicalID = "claude-code/claude-opus-5-5-thinking"
			res.UpstreamModelID = "claude-opus-5-5"
			res.ThinkingLevel = ThinkingLevelHigh
			res.ReasoningEffort = "high"
			res.IncludeThoughts = true
		case "claude-fable-5-1", "claude-fable-5.1", "fable-5.1", "fable-5-1":
			res.CanonicalID = "claude-code/claude-fable-5-1"
			res.UpstreamModelID = "claude-fable-5-1"
			res.ThinkingLevel = ThinkingLevelHigh
			res.ReasoningEffort = "high"
			res.IncludeThoughts = true
		case "claude-sonnet-5-5", "claude-sonnet-5.5", "sonnet-5.5":
			res.CanonicalID = "claude-code/claude-sonnet-5-5"
			res.UpstreamModelID = "claude-sonnet-5-5"
		case "claude-sonnet-5-5-thinking":
			res.CanonicalID = "claude-code/claude-sonnet-5-5-thinking"
			res.UpstreamModelID = "claude-sonnet-5-5"
			res.ThinkingLevel = ThinkingLevelHigh
			res.ReasoningEffort = "high"
			res.IncludeThoughts = true
		case "claude-haiku-5-5", "claude-haiku-5.5", "haiku-5.5":
			res.CanonicalID = "claude-code/claude-haiku-5-5"
			res.UpstreamModelID = "claude-haiku-5-5"
		case "claude-sonnet-5", "sonnet-5":
			res.CanonicalID = "claude-code/claude-sonnet-5"
			res.UpstreamModelID = "claude-sonnet-5"
		case "claude-opus-5", "opus-5":
			res.CanonicalID = "claude-code/claude-opus-5"
			res.UpstreamModelID = "claude-opus-5"
			res.ThinkingLevel = ThinkingLevelHigh
			res.ReasoningEffort = "high"
			res.IncludeThoughts = true
		case "claude-fable-5", "fable-5":
			res.CanonicalID = "claude-code/claude-fable-5"
			res.UpstreamModelID = "claude-fable-5"
			res.ThinkingLevel = ThinkingLevelHigh
			res.ReasoningEffort = "high"
			res.IncludeThoughts = true
		case "claude-opus-4-8", "claude-opus-4.8", "opus-4.8":
			res.CanonicalID = "claude-code/claude-opus-4-8"
			res.UpstreamModelID = "claude-opus-4-8"
			res.ThinkingLevel = ThinkingLevelHigh
			res.ReasoningEffort = "high"
			res.IncludeThoughts = true
		case "claude-opus-4-7", "claude-opus-4.7", "opus-4.7":
			res.CanonicalID = "claude-code/claude-opus-4-7"
			res.UpstreamModelID = "claude-opus-4-7"
			res.ThinkingLevel = ThinkingLevelHigh
			res.ReasoningEffort = "high"
			res.IncludeThoughts = true
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

	case "gemini-3.1-pro-high", "gemini-3.1-pro:high", "gemini-3.1-pro", "gemini-3.1-pro-preview", "gemini 3.1 pro", "gemini 3.1 pro high":
		res.CanonicalID = "antigravity/gemini-3.1-pro-high"
		res.UpstreamModelID = "gemini-3.1-pro-preview"
		res.CCPAModelID = "gemini-3.1-pro-preview"
		res.OmniBaseModelID = "antigravity/gemini-3.1-pro-high"
		res.ThinkingLevel = ThinkingLevelHigh
		res.ReasoningEffort = "high"
		res.IncludeThoughts = true

	case "gemini-3.1-pro-medium", "gemini-3.1-pro:medium", "gemini 3.1 pro medium":
		res.CanonicalID = "antigravity/gemini-3.1-pro-medium"
		res.UpstreamModelID = "gemini-3.1-pro-preview"
		res.CCPAModelID = "gemini-3.1-pro-preview"
		res.OmniBaseModelID = "antigravity/gemini-3.1-pro-medium"
		res.ThinkingLevel = ThinkingLevelMedium
		res.ReasoningEffort = "medium"
		res.IncludeThoughts = true

	case "gemini-3.1-pro-low", "gemini-3.1-pro:low", "gemini 3.1 pro low":
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

// ListCombos returns all configured OmniRoute-Go combo models (built-in presets merged with user custom combos).
func (r *DefaultResolver) ListCombos() []ComboModel {
	defaults := DefaultCombos()
	saved := LoadSavedAuthConfig()
	if len(saved.CustomCombos) == 0 {
		return defaults
	}

	customMap := make(map[string]ComboModel, len(saved.CustomCombos))
	for _, c := range saved.CustomCombos {
		customMap[strings.ToLower(strings.TrimSpace(c.ID))] = c
	}

	out := make([]ComboModel, 0, len(defaults)+len(saved.CustomCombos))
	seen := make(map[string]bool)
	for _, d := range defaults {
		key := strings.ToLower(strings.TrimSpace(d.ID))
		if override, ok := customMap[key]; ok {
			if override.Mode == "" {
				override.Mode = d.Mode
			}
			out = append(out, override)
		} else {
			out = append(out, d)
		}
		seen[key] = true
	}
	for _, c := range saved.CustomCombos {
		key := strings.ToLower(strings.TrimSpace(c.ID))
		if !seen[key] {
			if c.Mode == "" {
				c.Mode = ComboModeFailover
			}
			out = append(out, c)
			seen[key] = true
		}
	}
	return out
}

// ListModels returns OpenAI-compatible model cards strictly for the 3 providers:
//  1. antigravity (Gemini 4 Argon, Gemini 3 Series, Antigravity Claude — NO legacy 1.5/2.0/2.5)
//  2. claude-code (Anthropic Claude Code Pro/Max & API)
//  3. alibaba (Alibaba Token Plan / Bailian Coding Plan)
//     + Combos (auto/best-coding, auto/team-coding, auto/council-pro, auto/night-coding, auto/fast, auto/reasoning)
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
		{"auto/team-coding", "omniroute-go", "auto", "combo"},
		{"auto/council-pro", "omniroute-go", "auto", "combo"},
		{"auto/night-coding", "omniroute-go", "auto", "combo"},
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
		{"gemini-3.1-pro-high", "antigravity", "antigravity", "gen3-pro"},
		{"gemini-3.1-pro-medium", "antigravity", "antigravity", "gen3-pro"},
		{"gemini-3.1-pro-low", "antigravity", "antigravity", "gen3-pro"},
		{"gemini-3.1-pro", "antigravity", "antigravity", "gen3-pro"},
		{"antigravity/gemini-3.1-pro-high", "antigravity", "antigravity", "gen3-pro"},
		{"antigravity/gemini-3.1-pro-medium", "antigravity", "antigravity", "gen3-pro"},
		{"antigravity/gemini-3.1-pro-low", "antigravity", "antigravity", "gen3-pro"},
		{"antigravity/gemini-3.1-pro", "antigravity", "antigravity", "gen3-pro"},
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
		{"claude-code/claude-opus-5-5", "claude-code", "claude-code", "claude-code"},
		{"claude-code/claude-opus-5-5-thinking", "claude-code", "claude-code", "claude-code"},
		{"claude-code/claude-fable-5-1", "claude-code", "claude-code", "claude-code"},
		{"claude-code/claude-sonnet-5-5", "claude-code", "claude-code", "claude-code"},
		{"claude-code/claude-sonnet-5-5-thinking", "claude-code", "claude-code", "claude-code"},
		{"claude-code/claude-haiku-5-5", "claude-code", "claude-code", "claude-code"},
		{"claude-code/claude-sonnet-5", "claude-code", "claude-code", "claude-code"},
		{"claude-code/claude-opus-5", "claude-code", "claude-code", "claude-code"},
		{"claude-code/claude-fable-5", "claude-code", "claude-code", "claude-code"},
		{"claude-code/claude-opus-4-8", "claude-code", "claude-code", "claude-code"},
		{"claude-code/claude-opus-4-7", "claude-code", "claude-code", "claude-code"},
		{"claude-code/claude-opus-4-6-thinking", "claude-code", "claude-code", "claude-code"},
		{"claude-code/claude-opus-4-6", "claude-code", "claude-code", "claude-code"},
		{"claude-code/claude-sonnet-4-6-thinking", "claude-code", "claude-code", "claude-code"},
		{"claude-code/claude-sonnet-4-6", "claude-code", "claude-code", "claude-code"},
		{"claude-code/claude-haiku-4-5", "claude-code", "claude-code", "claude-code"},

		// --- Provider 3: Alibaba Token Plan (Bailian Coding Plan / DashScope) ---
		{"alibaba/auto", "alibaba", "alibaba", "token-plan"},
		{"alibaba/qwen3.8-max", "alibaba", "alibaba", "token-plan"},
		{"alibaba/qwen3.8-flash", "alibaba", "alibaba", "token-plan"},
		{"alibaba/qwen3.7-plus", "alibaba", "alibaba", "token-plan"},
		{"alibaba/qwen3.7-max", "alibaba", "alibaba", "token-plan"},
		{"alibaba/qwen3.6-flash", "alibaba", "alibaba", "token-plan"},
		{"alibaba/deepseek-v4.1-flash-1017", "alibaba", "alibaba", "token-plan"},
		{"alibaba/deepseek-v4.1-flash", "alibaba", "alibaba", "token-plan"},
		{"alibaba/deepseek-v4-pro-0813", "alibaba", "alibaba", "token-plan"},
		{"alibaba/deepseek-v4-pro", "alibaba", "alibaba", "token-plan"},
		{"alibaba/deepseek-v4-flash-0731", "alibaba", "alibaba", "token-plan"},
		{"alibaba/glm-5.3", "alibaba", "alibaba", "token-plan"},
		{"alibaba/glm-5.2", "alibaba", "alibaba", "token-plan"},
		{"alibaba/qwen-image-3.0-pro", "alibaba", "alibaba", "token-plan"},
		{"alibaba/wan2.7-image", "alibaba", "alibaba", "token-plan"},
		{"alibaba/wan2.7-image-pro", "alibaba", "alibaba", "token-plan"},
		{"alibaba/wan3.0-video", "alibaba", "alibaba", "token-plan"},
		{"alibaba/happyhorse-1.1-r2v", "alibaba", "alibaba", "token-plan"},
		{"alibaba/happyhorse-1.1-t2v", "alibaba", "alibaba", "token-plan"},
		{"alibaba/happyhorse-1.1-i2v", "alibaba", "alibaba", "token-plan"},
		{"alibaba/qwen-audio-3.0-asr-flash", "alibaba", "alibaba", "token-plan"},
		{"alibaba/qwen-audio-3.0-realtime-plus", "alibaba", "alibaba", "token-plan"},
		{"alibaba/qwen-audio-3.0-tts-plus", "alibaba", "alibaba", "token-plan"},
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
