package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ResolveAlibabaEndpoint resolves the DashScope / Bailian API endpoint for Alibaba Token Plan.
// If customBaseURL is provided, it normalizes and appends /chat/completions.
// Otherwise, it selects the regional endpoint:
//   - "intl-coding" or "":  https://coding-intl.dashscope.aliyuncs.com/v1/chat/completions
//   - "intl-standard":      https://dashscope-intl.aliyuncs.com/compatible-mode/v1/chat/completions
//   - "cn-coding":          https://coding.dashscope.aliyuncs.com/v1/chat/completions
//   - "cn-standard":        https://dashscope.aliyuncs.com/compatible-mode/v1/chat/completions
func ResolveAlibabaEndpoint(region string, customBaseURL string) string {
	base := strings.TrimRight(strings.TrimSpace(customBaseURL), "/")
	if base != "" {
		if strings.HasSuffix(base, "/chat/completions") {
			return base
		}
		return base + "/chat/completions"
	}

	switch strings.ToLower(strings.TrimSpace(region)) {
	case "intl-standard":
		return "https://dashscope-intl.aliyuncs.com/compatible-mode/v1/chat/completions"
	case "cn-coding":
		return "https://coding.dashscope.aliyuncs.com/v1/chat/completions"
	case "cn-standard":
		return "https://dashscope.aliyuncs.com/compatible-mode/v1/chat/completions"
	case "intl-coding", "":
		fallthrough
	default:
		return "https://coding-intl.dashscope.aliyuncs.com/v1/chat/completions"
	}
}

// mapAlibabaTokenPlanModel translates legacy DashScope model names to the live Alibaba Token Plan
// (token-plan.ap-southeast-1.maas.aliyuncs.com) model IDs when using an sk-sp-* Token Plan key.
func mapAlibabaTokenPlanModel(modelID string) string {
	switch strings.ToLower(strings.TrimSpace(modelID)) {
	case "auto":
		return "auto"
	case "qwen3-coder-plus", "qwen3-max", "qwen-max", "qwen3-235b-a22b", "qwen3.8-max":
		return "qwen3.8-max"
	case "qwen3-coder-turbo", "qwen-plus", "qwen3-32b", "qwen3.8-flash":
		return "qwen3.8-flash"
	case "qwen3.7-max":
		return "qwen3.7-max"
	case "qwen3.7-plus":
		return "qwen3.7-plus"
	case "qwen3.6-flash":
		return "qwen3.6-flash"
	case "deepseek-v4.1-flash-1017":
		return "deepseek-v4.1-flash-1017"
	case "deepseek-v4-pro-0813":
		return "deepseek-v4-pro-0813"
	case "deepseek-v4-flash-0731":
		return "deepseek-v4-flash-0731"
	case "deepseek-r1", "qwq-plus", "deepseek-v4-pro":
		return "deepseek-v4-pro"
	case "deepseek-v3", "deepseek-v4.1-flash":
		return "deepseek-v4.1-flash"
	case "glm-5.3":
		return "glm-5.3"
	case "glm-5.2":
		return "glm-5.2"
	default:
		if modelID != "" {
			return modelID
		}
		return "qwen3.8-max"
	}
}

func buildAlibabaTargetModels(primary string) []string {
	mapped := mapAlibabaTokenPlanModel(primary)
	raw := []string{mapped}
	if primary != "" && primary != mapped {
		raw = append(raw, primary)
	}
	lower := strings.ToLower(mapped)
	switch {
	case strings.HasPrefix(lower, "deepseek-v4-pro"):
		raw = append(raw, "deepseek-v4-pro", "qwen3.8-max")
	case strings.HasPrefix(lower, "deepseek-v4"):
		raw = append(raw, "deepseek-v4.1-flash", "qwen3.8-flash")
	case lower == "auto":
		raw = append(raw, "qwen3.8-max", "qwen3.8-flash")
	default:
		raw = append(raw, "qwen3.8-max")
	}
	seen := make(map[string]bool, len(raw))
	out := make([]string, 0, len(raw))
	for _, m := range raw {
		if m != "" && !seen[m] {
			seen[m] = true
			out = append(out, m)
		}
	}
	return out
}

// handleAlibabaProvider handles chat completions for Alibaba Token Plan (Bailian Coding Plan / DashScope).
func (u *GatewayUpstream) handleAlibabaProvider(
	ctx context.Context,
	w http.ResponseWriter,
	req OpenAIChatRequest,
	resolved ResolvedModel,
	acc ConnectedAccount,
) error {
	token := strings.TrimSpace(acc.APIKey)
	if token == "" {
		token = strings.TrimSpace(acc.AccessToken)
	}
	if strings.HasPrefix(strings.ToLower(token), "bearer ") {
		token = strings.TrimSpace(token[7:])
	}

	var endpoints []string
	var targetModels []string

	if strings.TrimSpace(acc.BaseURL) != "" {
		endpoints = []string{ResolveAlibabaEndpoint(acc.Region, acc.BaseURL)}
		targetModels = []string{resolved.UpstreamModelID}
	} else if strings.HasPrefix(token, "sk-sp-") {
		// Alibaba Cloud Model Studio Token Plan (sk-sp-*) uses the dedicated MaaS endpoint
		endpoints = []string{
			"https://token-plan.ap-southeast-1.maas.aliyuncs.com/compatible-mode/v1/chat/completions",
			ResolveAlibabaEndpoint(acc.Region, acc.BaseURL),
		}
		targetModels = buildAlibabaTargetModels(resolved.UpstreamModelID)
	} else {
		endpoints = []string{
			ResolveAlibabaEndpoint(acc.Region, acc.BaseURL),
			"https://token-plan.ap-southeast-1.maas.aliyuncs.com/compatible-mode/v1/chat/completions",
		}
		targetModels = buildAlibabaTargetModels(resolved.UpstreamModelID)
	}

	client := u.client
	if client == nil {
		client = SharedHTTPClient
	}

	var lastErr error
	for _, ep := range endpoints {
		for _, targetModel := range targetModels {
			outReq := req
			outReq.Model = targetModel
			outReq.ThinkingConfig = nil
			outReq.ReasoningEffort = ""

			payloadBytes, err := json.Marshal(outReq)
			if err != nil {
				lastErr = fmt.Errorf("failed to marshal alibaba request: %w", err)
				continue
			}

			httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, ep, bytes.NewReader(payloadBytes))
			if err != nil {
				lastErr = fmt.Errorf("failed to create alibaba request: %w", err)
				continue
			}
			httpReq.Header.Set("Content-Type", "application/json")
			if req.Stream {
				httpReq.Header.Set("Accept", "text/event-stream")
			} else {
				httpReq.Header.Set("Accept", "application/json")
			}
			if token != "" {
				httpReq.Header.Set("Authorization", "Bearer "+token)
			}

			resp, err := client.Do(httpReq)
			if err != nil {
				lastErr = fmt.Errorf("alibaba upstream transport error: %w", err)
				continue
			}

			bodyBytes, err := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if err != nil {
				lastErr = fmt.Errorf("failed to read alibaba response: %w", err)
				continue
			}

			if resp.StatusCode >= 400 {
				lastErr = fmt.Errorf("alibaba HTTP %d (%s): %s", resp.StatusCode, targetModel, strings.TrimSpace(string(bodyBytes)))
				if resp.StatusCode == http.StatusNotFound {
					continue
				}
				break
			}

			w.Header().Set("X-Omni-Gateway-Provider", "alibaba")
			w.Header().Set("X-Omni-Gateway-Resolved-Model", targetModel)
			if acc.Email != "" {
				w.Header().Set("X-Omni-Gateway-Account", acc.Email)
			}
			alibabaTokens := extractTokensFromOpenAIBody(bodyBytes, req)
			w.Header().Set("X-Omni-Gateway-Tokens", fmt.Sprintf("%d", alibabaTokens))
			merged := aggregateSSEToResponse(bodyBytes, resolved.RequestedID)
			respText := ""
			if len(merged.Choices) > 0 && merged.Choices[0].Message != nil {
				respText = merged.Choices[0].Message.Content
			}
			pToks := EstimateTokens(req.Messages)
			cToks := alibabaTokens - pToks
			if cToks < 0 {
				cToks = 0
			}
			SetAuditResponse(ctx, respText, pToks, cToks)

			if req.Stream {
				return writeStreamingResponse(w, bodyBytes, resolved.RequestedID)
			}
			return writeNonStreamingResponse(w, bodyBytes, resolved.RequestedID)
		}
	}

	return lastErr
}

// anthropicMessage represents an input message in Anthropic /v1/messages format.
type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// anthropicMessagesRequest represents the request payload for Anthropic /v1/messages.
type anthropicMessagesRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    string             `json:"system,omitempty"`
	Messages  []anthropicMessage `json:"messages"`
}

// anthropicContentBlock represents a single content block in Anthropic /v1/messages response.
type anthropicContentBlock struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	Thinking string `json:"thinking,omitempty"`
}

// anthropicMessagesResponse represents the response payload from Anthropic /v1/messages.
type anthropicMessagesResponse struct {
	ID         string                  `json:"id"`
	Type       string                  `json:"type"`
	Role       string                  `json:"role"`
	Content    []anthropicContentBlock `json:"content"`
	Model      string                  `json:"model"`
	StopReason string                  `json:"stop_reason"`
	Usage      struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func buildClaudeCodeCandidates(primary string) []string {
	raw := []string{primary}
	lower := strings.ToLower(strings.TrimSpace(primary))
	switch {
	case strings.Contains(lower, "haiku"):
		raw = append(raw, "claude-haiku-4-5-20251001", "claude-sonnet-4-6")
	case strings.Contains(lower, "sonnet"):
		raw = append(raw, "claude-sonnet-4-6", "claude-opus-4-6")
	default:
		raw = append(raw, "claude-opus-4-6", "claude-sonnet-4-6")
	}
	seen := make(map[string]bool, len(raw))
	out := make([]string, 0, len(raw))
	for _, m := range raw {
		if m != "" && !seen[m] {
			seen[m] = true
			out = append(out, m)
		}
	}
	return out
}

// handleClaudeCodeProvider handles chat completions for Claude Code (OAuth & Anthropic API).
func (u *GatewayUpstream) handleClaudeCodeProvider(
	ctx context.Context,
	w http.ResponseWriter,
	req OpenAIChatRequest,
	resolved ResolvedModel,
	acc ConnectedAccount,
) error {
	endpoint := strings.TrimRight(strings.TrimSpace(acc.BaseURL), "/")
	if endpoint == "" {
		endpoint = "https://api.anthropic.com/v1/messages"
	} else if !strings.HasSuffix(endpoint, "/messages") {
		if !strings.HasSuffix(endpoint, "/v1") {
			endpoint += "/v1"
		}
		endpoint += "/messages"
	}

	token := strings.TrimSpace(acc.AccessToken)
	if token == "" {
		token = strings.TrimSpace(acc.APIKey)
	}
	if strings.HasPrefix(strings.ToLower(token), "bearer ") {
		token = strings.TrimSpace(token[7:])
	}

	var sysParts []string
	var anthMessages []anthropicMessage

	for _, m := range req.Messages {
		text := m.ContentText()
		if text == "" {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(m.Role)) {
		case "system", "developer":
			sysParts = append(sysParts, text)
		case "assistant", "model":
			anthMessages = append(anthMessages, anthropicMessage{
				Role:    "assistant",
				Content: text,
			})
		default:
			anthMessages = append(anthMessages, anthropicMessage{
				Role:    "user",
				Content: text,
			})
		}
	}

	if len(anthMessages) == 0 {
		anthMessages = append(anthMessages, anthropicMessage{
			Role:    "user",
			Content: "Hello",
		})
	}

	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 8192
	}

	client := u.client
	if client == nil {
		client = http.DefaultClient
	}

	candidates := buildClaudeCodeCandidates(resolved.UpstreamModelID)
	var bodyBytes []byte
	var lastErr error
	var succeeded bool

	for _, candModel := range candidates {
		anthReq := anthropicMessagesRequest{
			Model:     candModel,
			MaxTokens: maxTokens,
			System:    strings.Join(sysParts, "\n\n"),
			Messages:  anthMessages,
		}

		payloadBytes, err := json.Marshal(anthReq)
		if err != nil {
			lastErr = fmt.Errorf("failed to marshal claude-code request: %w", err)
			continue
		}

		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payloadBytes))
		if err != nil {
			lastErr = fmt.Errorf("failed to create claude-code request: %w", err)
			continue
		}

		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Accept", "application/json")
		httpReq.Header.Set("anthropic-version", "2023-06-01")

		if strings.HasPrefix(token, "sk-ant-oat") {
			httpReq.Header.Set("Authorization", "Bearer "+token)
			httpReq.Header.Set("anthropic-beta", "claude-code-20250219,oauth-2025-04-20,interleaved-thinking-2025-05-14")
			httpReq.Header.Set("User-Agent", "claude-cli/1.0.0 (external, cli)")
		} else if token != "" {
			httpReq.Header.Set("x-api-key", token)
		}

		resp, err := client.Do(httpReq)
		if err != nil {
			lastErr = fmt.Errorf("claude-code upstream transport error: %w", err)
			continue
		}

		b, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			lastErr = fmt.Errorf("failed to read claude-code response: %w", err)
			continue
		}

		if resp.StatusCode >= 400 {
			lastErr = fmt.Errorf("claude-code HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
			if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusBadRequest {
				continue
			}
			break
		}

		bodyBytes = b
		succeeded = true
		break
	}

	if !succeeded {
		return lastErr
	}

	var anthResp anthropicMessagesResponse
	if err := json.Unmarshal(bodyBytes, &anthResp); err != nil {
		return fmt.Errorf("failed to parse claude-code response: %w (body: %s)", err, truncateLog(string(bodyBytes), 200))
	}

	if anthResp.Error != nil {
		return fmt.Errorf("claude-code error: %s (%s)", anthResp.Error.Message, anthResp.Error.Type)
	}

	var textContent strings.Builder
	var thinkingContent strings.Builder

	for _, block := range anthResp.Content {
		switch block.Type {
		case "thinking":
			thinkingContent.WriteString(block.Thinking)
		case "text":
			textContent.WriteString(block.Text)
		}
	}

	created := time.Now().Unix()
	respID := anthResp.ID
	if respID == "" {
		respID = fmt.Sprintf("chatcmpl-claude-%d", time.Now().UnixNano())
	}

	finishReason := "stop"
	switch anthResp.StopReason {
	case "max_tokens":
		finishReason = "length"
	case "stop_sequence", "end_turn":
		finishReason = "stop"
	}

	promptToks := anthResp.Usage.InputTokens
	if promptToks <= 0 {
		promptToks = EstimateTokens(req.Messages)
	}
	compToks := anthResp.Usage.OutputTokens
	if compToks <= 0 {
		compToks = (textContent.Len() + thinkingContent.Len() + 3) / 4
	}
	totalToks := promptToks + compToks
	if totalToks <= 0 {
		totalToks = 1
	}

	openAIResp := OpenAIChatResponse{
		ID:      respID,
		Object:  "chat.completion",
		Created: created,
		Model:   resolved.RequestedID,
		Choices: []OpenAIChatChoice{
			{
				Index: 0,
				Message: &OpenAIOutputMessage{
					Role:             "assistant",
					Content:          textContent.String(),
					ReasoningContent: thinkingContent.String(),
				},
				FinishReason: finishReason,
			},
		},
		Usage: &OpenAIUsage{
			PromptTokens:     promptToks,
			CompletionTokens: compToks,
			TotalTokens:      totalToks,
		},
	}

	w.Header().Set("X-Omni-Gateway-Provider", "claude-code")
	w.Header().Set("X-Omni-Gateway-Resolved-Model", resolved.UpstreamModelID)
	w.Header().Set("X-Omni-Gateway-Tokens", fmt.Sprintf("%d", totalToks))
	SetAuditResponse(ctx, textContent.String(), promptToks, compToks)
	if acc.Email != "" {
		w.Header().Set("X-Omni-Gateway-Account", acc.Email)
	}

	if req.Stream {
		respJSON, err := json.Marshal(openAIResp)
		if err != nil {
			return fmt.Errorf("failed to serialize chat completion chunk: %w", err)
		}
		return writeStreamingResponse(w, respJSON, resolved.RequestedID)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	return json.NewEncoder(w).Encode(openAIResp)
}
