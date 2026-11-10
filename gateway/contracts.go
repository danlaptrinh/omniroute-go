package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"unicode/utf8"
)

// ProviderID identifies one of the 3 supported providers in OmniRoute-Go.
type ProviderID string

const (
	ProviderAntigravity ProviderID = "antigravity"
	ProviderClaudeCode  ProviderID = "claude-code"
	ProviderAlibaba     ProviderID = "alibaba"
	ProviderAutoCombo   ProviderID = "auto"
)

// RoutingStrategy defines how OmniRoute-Go selects accounts across a provider pool.
type RoutingStrategy string

const (
	StrategyRoundRobin RoutingStrategy = "round-robin"
	StrategyFillFirst  RoutingStrategy = "fill-first"
	StrategyP2C        RoutingStrategy = "p2c"
	StrategyLeastUsed  RoutingStrategy = "least-used"
)

// ThinkingLevel represents the internal Antigravity thinking effort level
// mapped from suffixes like -high, -medium, -low.
type ThinkingLevel string

const (
	ThinkingLevelNone   ThinkingLevel = ""
	ThinkingLevelLow    ThinkingLevel = "LOW"
	ThinkingLevelMedium ThinkingLevel = "MEDIUM"
	ThinkingLevelHigh   ThinkingLevel = "HIGH"
)

// ComboMode defines how multiple models inside a ComboModel work together.
type ComboMode string

const (
	ComboModeFailover   ComboMode = "failover"    // Try Model 1 -> if fails -> Model 2 -> Model 3
	ComboModePipeline   ComboMode = "pipeline"    // Step 1 (Architect/Planner) -> Step 2 (Coder/Synthesizer)
	ComboModeConsensus  ComboMode = "consensus"   // Run models in parallel (Mixture-of-Agents) -> Synthesizer merges best answer
	ComboModeNightSaver ComboMode = "night-saver" // Prioritize Night-Discount models (40-50% Off Nights) + multi-provider fallback
)

// ComboModel defines an OmniRoute multi-provider fallback chain or collaborative multi-model pipeline (e.g. "auto/team-coding").
type ComboModel struct {
	ID            string    `json:"id"`                       // e.g. "auto/best-coding", "auto/team-coding"
	Name          string    `json:"name"`                     // e.g. "Best Coding (Argon 4 -> Claude Opus 5.5 -> Qwen 3.8 Max)"
	Mode          ComboMode `json:"mode,omitempty"`           // "failover", "pipeline", "consensus", "night-saver"
	Description   string    `json:"description"`              // Human-readable summary
	Models        []string  `json:"models"`                   // Ordered list of provider/model targets
	Roles         []string  `json:"roles,omitempty"`          // Optional role label per model step
	HandoffPrompt string    `json:"handoff_prompt,omitempty"` // Optional instructions passed between steps
	BuiltIn       bool      `json:"built_in,omitempty"`       // True if built-in preset
}

// ResolvedModel holds the canonical translation of an incoming model ID.
type ResolvedModel struct {
	RequestedID     string        `json:"requested_id"`
	Provider        ProviderID    `json:"provider"`          // "antigravity", "claude-code", "alibaba", or "auto"
	CanonicalID     string        `json:"canonical_id"`      // e.g. "antigravity/gemini-4-argon-high"
	UpstreamModelID string        `json:"upstream_model_id"` // e.g. "gemini-4-argon", "claude-opus-4-6", "qwen3-coder-plus"
	CCPAModelID     string        `json:"ccpa_model_id"`     // Backwards-compatible alias for UpstreamModelID on Antigravity
	OmniBaseModelID string        `json:"omni_base_model"`   // e.g. "antigravity/gemini-4-argon"
	ThinkingLevel   ThinkingLevel `json:"thinking_level"`    // "HIGH", "MEDIUM", "LOW", or ""
	ReasoningEffort string        `json:"reasoning_effort"`  // "high", "medium", "low", or ""
	IncludeThoughts bool          `json:"include_thoughts"`  // true when thinking/reasoning is active
	PlaceholderEnum string        `json:"placeholder_enum"`  // e.g. "MODEL_PLACEHOLDER_M417"
	FallbackChain   []string      `json:"fallback_chain"`    // Ordered fallback models across the 3 providers
	IsArgonFamily   bool          `json:"is_argon_family"`
	IsCarbonFamily  bool          `json:"is_carbon_family"`
	IsCombo         bool          `json:"is_combo"`
	ComboMode       ComboMode     `json:"combo_mode,omitempty"`
	ComboHandoff    string        `json:"combo_handoff,omitempty"`
}

// OpenAIChatRequest represents an OpenAI-compatible /v1/chat/completions request.
type OpenAIChatRequest struct {
	Model           string              `json:"model"`
	Messages        []OpenAIChatMessage `json:"messages"`
	Temperature     *float64            `json:"temperature,omitempty"`
	MaxTokens       int                 `json:"max_tokens,omitempty"`
	Stream          bool                `json:"stream,omitempty"`
	ReasoningEffort string              `json:"reasoning_effort,omitempty"`
	ThinkingConfig  *ThinkingConfig     `json:"thinking_config,omitempty"`
}

// ThinkingConfig represents Antigravity / Gemini / Claude thinking configuration.
type ThinkingConfig struct {
	IncludeThoughts bool          `json:"includeThoughts"`
	ThinkingLevel   ThinkingLevel `json:"thinkingLevel,omitempty"`
	ThinkingBudget  int           `json:"thinkingBudget,omitempty"`
}

// OpenAIChatMessage represents a single message in OpenAI format.
type OpenAIChatMessage struct {
	Role             string          `json:"role"`
	Content          json.RawMessage `json:"content"`
	ReasoningContent string          `json:"reasoning_content,omitempty"`
}

// ContentText extracts plain text from Content (handling string or array of content parts).
func (m OpenAIChatMessage) ContentText() string {
	if len(m.Content) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(m.Content, &s); err == nil {
		return s
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(m.Content, &parts); err == nil {
		var out string
		for _, p := range parts {
			if p.Text != "" {
				if out != "" {
					out += "\n"
				}
				out += p.Text
			}
		}
		return out
	}
	return string(m.Content)
}

// OpenAIChatResponse represents a non-streaming /v1/chat/completions response.
type OpenAIChatResponse struct {
	ID      string             `json:"id"`
	Object  string             `json:"object"`
	Created int64              `json:"created"`
	Model   string             `json:"model"`
	Choices []OpenAIChatChoice `json:"choices"`
	Usage   *OpenAIUsage       `json:"usage,omitempty"`
	Error   *OpenAIError       `json:"error,omitempty"`
}

// OpenAIChatChoice represents a single completion choice.
type OpenAIChatChoice struct {
	Index        int                  `json:"index"`
	Message      *OpenAIOutputMessage `json:"message,omitempty"`
	Delta        *OpenAIOutputMessage `json:"delta,omitempty"`
	FinishReason string               `json:"finish_reason,omitempty"`
}

// OpenAIOutputMessage represents a response/delta message in OpenAI format.
type OpenAIOutputMessage struct {
	Role             string `json:"role,omitempty"`
	Content          string `json:"content"`
	ReasoningContent string `json:"reasoning_content,omitempty"`
}

// OpenAIUsage represents token usage statistics.
type OpenAIUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// OpenAIError represents an OpenAI-formatted error payload.
type OpenAIError struct {
	Message string `json:"message"`
	Type    string `json:"type,omitempty"`
	Code    any    `json:"code,omitempty"`
}

// OpenAIModelCard represents a model entry in /v1/models.
type OpenAIModelCard struct {
	ID       string `json:"id"`
	Object   string `json:"object"`
	Created  int64  `json:"created"`
	OwnedBy  string `json:"owned_by"`
	Provider string `json:"provider,omitempty"`
	Tier     string `json:"tier,omitempty"`
}

// GatewayConfig holds runtime configuration for OmniRoute-Go.
type GatewayConfig struct {
	ListenAddr       string          // e.g. "127.0.0.1:8787"
	OmniRouteBaseURL string          // Optional external upstream URL
	OmniRouteAPIKey  string          // Optional external upstream API key
	CCPAEndpoint     string          // Default: "https://daily-cloudcode-pa.googleapis.com/v1internal:streamGenerateContent?alt=sse"
	AgyOAuthToken    string          // Optional Google OAuth token (ya29....)
	AgyProjectID     string          // Optional Cloud Code project ID
	RoutingStrategy  RoutingStrategy // Default: StrategyRoundRobin
	FallbackModels   []string        // Default multi-provider fallback chain
}

// ModelResolver resolves arbitrary model names into ResolvedModel specs across the 3 providers.
type ModelResolver interface {
	Resolve(modelID string, defaultFallbacks []string) ResolvedModel
	ListModels() []OpenAIModelCard
	ListCombos() []ComboModel
}

// GatewayAPIKey represents a client API key generated for authentication to OmniRoute-Go.
type GatewayAPIKey struct {
	ID                 string `json:"id"`
	Name               string `json:"name"`
	Key                string `json:"key,omitempty"`      // Full key (sk-omni-...), omitted in sanitized list views
	MaskedKey          string `json:"masked_key"`         // Masked key (sk-omni-...xxxx)
	KeyHash            string `json:"key_hash,omitempty"` // SHA-256 hex digest of Key
	AdminAccess        bool   `json:"admin_access"`
	AllowSelfUsage     bool   `json:"allow_self_usage"`
	AllowSharedQuota   bool   `json:"allow_shared_quota"`
	AllowLocalUsageCmd bool   `json:"allow_local_usage_cmd"`
	TotalRequests      int64  `json:"total_requests"`
	TotalTokens        int64  `json:"total_tokens"`
	CreatedAt          string `json:"created_at"`
	LastUsedAt         string `json:"last_used_at,omitempty"`
}

// GatewayLogEntry records upstream requests, latency, tokens, employee API key, prompt, and AI response across providers.
type GatewayLogEntry struct {
	ID               string     `json:"id"`
	Timestamp        string     `json:"timestamp"`
	APIKeyID         string     `json:"api_key_id,omitempty"`
	EmployeeName     string     `json:"employee_name,omitempty"`
	MaskedKey        string     `json:"masked_key,omitempty"`
	Provider         ProviderID `json:"provider"`
	Account          string     `json:"account,omitempty"`
	RequestedModel   string     `json:"requested_model"`
	ResolvedModel    string     `json:"resolved_model"`
	PromptText       string     `json:"prompt_text,omitempty"`
	ResponseText     string     `json:"response_text,omitempty"`
	PromptTokens     int        `json:"prompt_tokens,omitempty"`
	CompletionTokens int        `json:"completion_tokens,omitempty"`
	StatusCode       int        `json:"status_code"`
	LatencyMs        int64      `json:"latency_ms"`
	TotalTokens      int        `json:"total_tokens,omitempty"`
	TokensSaved      int        `json:"tokens_saved,omitempty"`
	ErrorMessage     string     `json:"error_message,omitempty"`
	ErrorDetail      string     `json:"error_detail,omitempty"`
	IsError          bool       `json:"is_error"`
	FallbackChain    []string   `json:"fallback_chain,omitempty"`
}

// CompressionMode defines the compression strategy for prompt and context reduction.
type CompressionMode string

const (
	CompressionOff     CompressionMode = "off"
	CompressionLite    CompressionMode = "lite"
	CompressionCaveman CompressionMode = "caveman"
	CompressionRTK     CompressionMode = "rtk"
	CompressionUltra   CompressionMode = "ultra"
)

// CompressionConfig holds settings and metrics for token prompt compression.
type CompressionConfig struct {
	Mode              CompressionMode `json:"mode"`
	CompressCode      bool            `json:"compress_code"`
	MaxHistoryTurns   int             `json:"max_history_turns"`
	EnableGzip        bool            `json:"enable_gzip"`
	TotalTokensSaved  int64           `json:"total_tokens_saved"`
	TotalCallsHandled int64           `json:"total_calls_handled"`
}

// EndpointConfig holds public/local endpoint URLs, tunnel states (Cloud, Cloudflare, Tailscale, Ngrok),
// and global Custom System Prompt injection settings for VPS and local deployments.
type EndpointConfig struct {
	PublicURL            string `json:"public_url,omitempty"`              // e.g. "https://omni.huongque.com/v1"
	CloudEnabled         bool   `json:"cloud_enabled"`                     // Cloud OmniRoute / VPS custom domain enabled
	CloudflareEnabled    bool   `json:"cloudflare_enabled"`                // Cloudflare Quick Tunnel enabled
	CloudflareTunnelURL  string `json:"cloudflare_tunnel_url,omitempty"`   // Live *.trycloudflare.com URL
	TailscaleEnabled     bool   `json:"tailscale_enabled"`                 // Tailscale Funnel enabled
	TailscaleURL         string `json:"tailscale_url,omitempty"`           // Live *.ts.net URL
	NgrokEnabled         bool   `json:"ngrok_enabled"`                     // Ngrok tunnel enabled
	NgrokAuthToken       string `json:"ngrok_auth_token,omitempty"`        // Ngrok authtoken
	NgrokURL             string `json:"ngrok_url,omitempty"`               // Live *.ngrok-free.app URL
	CustomSystemPromptOn bool   `json:"custom_system_prompt_on"`           // Inject custom system prompt into all requests
	CustomSystemPrompt   string `json:"custom_system_prompt,omitempty"`    // Custom system prompt text
}

// VectorDBConfig holds settings for the Long-Term Semantic Vector Database & RAG Memory Engine.
type VectorDBConfig struct {
	Enabled          bool    `json:"enabled"`             // Enable Vector Database & Long-Term Memory
	AutoLearnPrompts bool    `json:"auto_learn_prompts"`  // Automatically embed & store employee prompts + AI answers
	AutoInjectRAG    bool    `json:"auto_inject_rag"`     // Automatically retrieve & inject Top-K similar memories into prompts
	TopK             int     `json:"top_k"`               // Number of similar memories to retrieve (default: 3)
	MinSimilarity    float64 `json:"min_similarity"`      // Minimum Cosine Similarity threshold (default: 0.25)
	TotalRAGHits     int64   `json:"total_rag_hits"`      // Total times Vector RAG enriched an incoming prompt
}

// VectorMemoryEntry represents a single semantic vector memory (either pinned knowledge or auto-learned Q&A).
type VectorMemoryEntry struct {
	ID           string    `json:"id"`
	CreatedAt    string    `json:"created_at"`
	Title        string    `json:"title"`
	Category     string    `json:"category"` // e.g. "knowledge", "auto_qa", "bucloud", "coding"
	EmployeeName string    `json:"employee_name,omitempty"`
	APIKeyID     string    `json:"api_key_id,omitempty"`
	Model        string    `json:"model,omitempty"`
	PromptText   string    `json:"prompt_text"`
	AnswerText   string    `json:"answer_text"`
	Pinned       bool      `json:"pinned"`
	HitCount     int64     `json:"hit_count"`
	Similarity   float64   `json:"similarity,omitempty"` // Populated during semantic search
	Embedding    []float32 `json:"embedding,omitempty"`
}

// UpstreamHandler executes a resolved request across Antigravity, Claude Code, and Alibaba Token Plan.
type UpstreamHandler interface {
	HandleChatCompletion(ctx context.Context, w http.ResponseWriter, req OpenAIChatRequest, resolved ResolvedModel, incomingAuth string) error
}

// TruncateUTF8 sanitizes invalid UTF-8 sequences and NUL bytes (which PostgreSQL TEXT rejects)
// and truncates s safely on UTF-8 rune boundaries up to maxRunes runes.
func TruncateUTF8(s string, maxRunes int) string {
	clean := strings.ToValidUTF8(s, "")
	if strings.IndexByte(clean, 0) >= 0 {
		clean = strings.ReplaceAll(clean, "\x00", "")
	}
	clean = strings.TrimSpace(clean)
	if maxRunes <= 0 || utf8.RuneCountInString(clean) <= maxRunes {
		return clean
	}
	count := 0
	for idx := range clean {
		if count == maxRunes {
			return clean[:idx] + " ...[truncated]"
		}
		count++
	}
	return clean
}

// MaskSecretToken masks a sensitive token for safe UI/API display while keeping 4 prefix/suffix chars.
func MaskSecretToken(tok string) string {
	tok = strings.TrimSpace(tok)
	if tok == "" {
		return ""
	}
	if len(tok) <= 8 {
		return "****"
	}
	return tok[:4] + "****" + tok[len(tok)-4:]
}

// IsUnsupportedClientBanner detects Google CCPA's HTTP 200 deprecation text when User-Agent is outdated.
func IsUnsupportedClientBanner(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	return strings.Contains(lower, "this version of antigravity is no longer supported")
}
