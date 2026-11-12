package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"omni-parallel-mcp/gateway"
)

const (
	DefaultFallbackModel   = "antigravity/gemini-4-argon-high"
	DefaultWorkerMaxTokens = 65536
)

func resolveDefaultModel(argModel string) string {
	return firstNonEmpty(
		argModel,
		os.Getenv("OMNI_DEFAULT_MODEL"),
		os.Getenv("OMNIROUTE_MODEL"),
		DefaultModel,
	)
}

func cleanStringSlice(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if s := strings.TrimSpace(item); s != "" {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func resolveModelPool(argPool []string, explicitModel string) []string {
	if cleaned := cleanStringSlice(argPool); len(cleaned) > 0 {
		return cleaned
	}
	if strings.TrimSpace(explicitModel) == "" {
		if envPool := strings.TrimSpace(os.Getenv("OMNI_MODEL_POOL")); envPool != "" {
			return cleanStringSlice(strings.Split(envPool, ","))
		}
	}
	return nil
}

func resolveWorkerPinnedAccount(idx int, taskPinned string, pinAccounts []string) string {
	if tp := strings.TrimSpace(taskPinned); tp != "" {
		return tp
	}
	if len(pinAccounts) > 0 {
		if idx < 0 {
			idx = 0
		}
		return pinAccounts[idx%len(pinAccounts)]
	}
	return ""
}

func deduplicateModelCandidates(raw []string) []string {
	seenRaw := make(map[string]bool, len(raw))
	seenCanonical := make(map[string]bool, len(raw))
	out := make([]string, 0, len(raw))

	for _, cand := range raw {
		c := strings.TrimSpace(cand)
		if c == "" {
			continue
		}
		lowerRaw := strings.ToLower(c)
		if seenRaw[lowerRaw] {
			continue
		}
		res := gateway.ResolveModel(c, nil)
		lowerCanon := strings.ToLower(strings.TrimSpace(res.CanonicalID))
		if lowerCanon != "" && seenCanonical[lowerCanon] {
			continue
		}
		seenRaw[lowerRaw] = true
		if lowerCanon != "" {
			seenCanonical[lowerCanon] = true
		}
		if (res.IsCarbonFamily || res.IsArgonFamily) && res.CanonicalID != "" {
			out = append(out, res.CanonicalID)
		} else {
			out = append(out, c)
		}
	}

	if len(out) == 0 {
		return []string{DefaultModel}
	}
	return out
}

// buildCandidateChain constructs a deduplicated model failover sequence where
// antigravity/gemini-4-carbon-high is primary and antigravity/gemini-4-argon-high
// is only invoked when Carbon fails, stalls, or hits rate/account limits.
func buildCandidateChain(primaryModel string) []string {
	trimmed := strings.TrimSpace(primaryModel)
	if trimmed == "" {
		trimmed = DefaultModel
	}

	primaryResolved := gateway.ResolveModel(trimmed, nil)
	firstCandidate := trimmed
	if (primaryResolved.IsCarbonFamily || primaryResolved.IsArgonFamily) && primaryResolved.CanonicalID != "" {
		firstCandidate = primaryResolved.CanonicalID
	}

	raw := []string{firstCandidate}
	if envFB := strings.TrimSpace(os.Getenv("OMNI_FALLBACK_MODELS")); envFB != "" {
		for _, part := range strings.Split(envFB, ",") {
			if m := strings.TrimSpace(part); m != "" {
				raw = append(raw, m)
			}
		}
	} else if primaryResolved.IsCarbonFamily {
		raw = append(raw, DefaultFallbackModel)
	} else if primaryResolved.IsArgonFamily {
		raw = append(raw, "antigravity/claude-opus-4-6-thinking")
	} else {
		emergencyFallback := "antigravity/claude-opus-4-6-thinking"
		if strings.EqualFold(firstCandidate, emergencyFallback) || strings.EqualFold(primaryResolved.CanonicalID, emergencyFallback) {
			emergencyFallback = "antigravity/gemini-3-flash"
		}
		raw = append(raw, emergencyFallback)
	}

	return deduplicateModelCandidates(raw)
}

// buildWorkerCandidateChain selects worker idx's primary model and automatic failover chain.
// When modelPool is non-empty and taskModel is unset, worker idx stripes across
// modelPool[idx % len(modelPool)] with the remaining pool models + DefaultFallbackModel as failover.
func buildWorkerCandidateChain(idx int, taskModel, defaultModel string, modelPool []string) []string {
	if idx < 0 {
		idx = 0
	}
	explicitTaskModel := strings.TrimSpace(taskModel)
	if len(modelPool) == 0 {
		return buildCandidateChain(firstNonEmpty(explicitTaskModel, defaultModel))
	}

	raw := make([]string, 0, len(modelPool)+3)
	if explicitTaskModel != "" {
		raw = append(raw, explicitTaskModel)
	}
	start := idx % len(modelPool)
	for offset := 0; offset < len(modelPool); offset++ {
		raw = append(raw, modelPool[(start+offset)%len(modelPool)])
	}
	if envFB := strings.TrimSpace(os.Getenv("OMNI_FALLBACK_MODELS")); envFB != "" {
		for _, part := range strings.Split(envFB, ",") {
			if m := strings.TrimSpace(part); m != "" {
				raw = append(raw, m)
			}
		}
	}
	raw = append(raw, DefaultFallbackModel)
	return deduplicateModelCandidates(raw)
}

func primaryFallbackLabel(primaryModel string) string {
	chain := buildCandidateChain(primaryModel)
	if len(chain) > 1 {
		return chain[1]
	}
	return "none"
}

func resolveWorkerMaxTokens() int {
	if raw := strings.TrimSpace(os.Getenv("OMNI_MAX_TOKENS")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			return n
		}
	}
	return DefaultWorkerMaxTokens
}

func resolveFirstChunkTimeout(isPrimaryWithFallback bool, promptChars int) time.Duration {
	base := 45 * time.Second
	if isPrimaryWithFallback {
		if raw := strings.TrimSpace(os.Getenv("OMNI_PRIMARY_TIMEOUT_SEC")); raw != "" {
			if sec, err := strconv.ParseFloat(raw, 64); err == nil && sec > 0 {
				base = time.Duration(sec * float64(time.Second))
			}
		}
	} else {
		if raw := strings.TrimSpace(os.Getenv("OMNI_FALLBACK_TIMEOUT_SEC")); raw != "" {
			if sec, err := strconv.ParseFloat(raw, 64); err == nil && sec > 0 {
				base = time.Duration(sec * float64(time.Second))
			}
		}
	}

	if base >= 10*time.Second {
		promptTokens := promptChars / 4
		if promptTokens > 16000 {
			extra := time.Duration((promptTokens-16000)/3000) * time.Second
			if extra > 45*time.Second {
				extra = 45 * time.Second
			}
			base += extra
		}
	}
	return base
}

func resolveStreamIdleTimeout() time.Duration {
	if raw := strings.TrimSpace(os.Getenv("OMNI_STREAM_IDLE_TIMEOUT_SEC")); raw != "" {
		if sec, err := strconv.ParseFloat(raw, 64); err == nil && sec > 0 {
			return time.Duration(sec * float64(time.Second))
		}
	}
	return 30 * time.Second
}

func callOmniRouteWithFailover(endpoint, apiKey, model, sysPrompt, userPrompt string) (string, string, error) {
	return callOmniRouteCandidates(endpoint, apiKey, buildCandidateChain(model), "", sysPrompt, userPrompt)
}

func callOmniRouteCandidates(endpoint, apiKey string, rawCandidates []string, pinnedAccount, sysPrompt, userPrompt string) (string, string, error) {
	if len(rawCandidates) == 0 {
		rawCandidates = buildCandidateChain(DefaultModel)
	}
	isOmniKey := strings.HasPrefix(strings.ToLower(strings.TrimSpace(apiKey)), "sk-omni-")
	candidates := make([]string, 0, len(rawCandidates))
	for _, c := range rawCandidates {
		if isOmniKey && (strings.HasPrefix(c, "claude-code/") || strings.HasPrefix(c, "alibaba/")) {
			continue
		}
		candidates = append(candidates, c)
	}
	if len(candidates) == 0 {
		candidates = []string{rawCandidates[0]}
	}
	trimmedPin := strings.TrimSpace(pinnedAccount)
	var lastErr error
	sawBannerErr := false
	maxOutTokens := resolveWorkerMaxTokens()
	promptChars := len(sysPrompt) + len(userPrompt)

	for idx, candidateModel := range candidates {
		candResolved := gateway.ResolveModel(candidateModel, nil)
		reqObj := ChatRequest{
			Model: candidateModel,
			Messages: []ChatMessage{
				{Role: "system", Content: sysPrompt},
				{Role: "user", Content: userPrompt},
			},
			Temperature:     0.1,
			MaxTokens:       maxOutTokens,
			Stream:          true,
			ReasoningEffort: candResolved.ReasoningEffort,
		}
		if candResolved.IncludeThoughts || candResolved.ThinkingLevel != gateway.ThinkingLevelNone {
			reqObj.ThinkingConfig = &gateway.ThinkingConfig{
				IncludeThoughts: candResolved.IncludeThoughts,
				ThinkingLevel:   candResolved.ThinkingLevel,
			}
		}

		payload, err := json.Marshal(reqObj)
		if err != nil {
			return "", "", err
		}

		hasMoreFallbacks := idx < len(candidates)-1
		firstChunkTimeout := resolveFirstChunkTimeout(hasMoreFallbacks, promptChars)
		streamIdleTimeout := resolveStreamIdleTimeout()
		maxCeiling := 10 * time.Minute
		if firstChunkTimeout > maxCeiling {
			maxCeiling = firstChunkTimeout + time.Minute
		}

		maxRetries := 2
		if hasMoreFallbacks {
			// Single attempt when backup candidate exists: if primary/pool model stalls, times out,
			// or hits rate/account errors, switch immediately to the next healthy candidate without delay.
			maxRetries = 1
		}

		for attempt := 0; attempt < maxRetries; attempt++ {
			if attempt > 0 {
				time.Sleep(250 * time.Millisecond)
			}

			ctx, cancel := context.WithTimeout(context.Background(), maxCeiling)
			idleTimer := time.AfterFunc(firstChunkTimeout, cancel)

			httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
			if err != nil {
				idleTimer.Stop()
				cancel()
				return "", "", err
			}
			httpReq.Header.Set("Content-Type", "application/json")
			httpReq.Header.Set("Accept", "text/event-stream, application/json")
			if apiKey != "" {
				httpReq.Header.Set("Authorization", "Bearer "+apiKey)
			}
			if trimmedPin != "" {
				httpReq.Header.Set("X-Omni-Pin-Account", trimmedPin)
			}

			resp, err := httpClient.Do(httpReq)
			if err != nil {
				idleTimer.Stop()
				cancel()
				lastErr = fmt.Errorf("request error (%s): %w", candidateModel, err)
				continue
			}

			if resp.StatusCode < 200 || resp.StatusCode >= 300 {
				errBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 16*1024))
				resp.Body.Close()
				idleTimer.Stop()
				cancel()
				errStr := string(errBytes)

				if resp.StatusCode == http.StatusUnauthorized {
					return "", "", fmt.Errorf("HTTP 401 unauthorized: %s", truncate(errStr, 300))
				}

				if resp.StatusCode == http.StatusBadRequest ||
					resp.StatusCode == http.StatusForbidden ||
					resp.StatusCode == http.StatusNotFound ||
					resp.StatusCode == http.StatusRequestTimeout ||
					resp.StatusCode == http.StatusServiceUnavailable {
					lastErr = fmt.Errorf("HTTP %d (%s): %s", resp.StatusCode, candidateModel, truncate(errStr, 200))
					break
				}

				if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
					lastErr = fmt.Errorf("HTTP %d (%s): %s", resp.StatusCode, candidateModel, truncate(errStr, 200))
					continue
				}

				lastErr = fmt.Errorf("HTTP %d (%s): %s", resp.StatusCode, candidateModel, truncate(errStr, 200))
				break
			}

			scanner := bufio.NewScanner(resp.Body)
			scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
			var rawBuf bytes.Buffer

			for scanner.Scan() {
				rawLine := scanner.Text()
				rawBuf.WriteString(rawLine)
				rawBuf.WriteByte('\n')

				trimmed := strings.TrimSpace(rawLine)
				if trimmed == "" ||
					strings.HasPrefix(trimmed, ":") ||
					strings.HasPrefix(trimmed, "event:") ||
					strings.HasPrefix(trimmed, "id:") ||
					strings.HasPrefix(trimmed, "retry:") {
					continue
				}

				if strings.HasPrefix(trimmed, "data:") {
					dataStr := strings.TrimSpace(strings.TrimPrefix(trimmed, "data:"))
					if dataStr == "[DONE]" {
						idleTimer.Reset(streamIdleTimeout)
						break
					}
					if dataStr != "" {
						var chunk ChatResponse
						if json.Unmarshal([]byte(dataStr), &chunk) == nil {
							if len(chunk.Choices) > 0 &&
								(chunk.Choices[0].Delta.Content != "" ||
									chunk.Choices[0].Delta.ReasoningContent != "" ||
									chunk.Choices[0].Message.Content != "") {
								idleTimer.Reset(streamIdleTimeout)
							}
						}
					}
				} else {
					idleTimer.Reset(streamIdleTimeout)
				}
			}

			scanErr := scanner.Err()
			idleTimer.Stop()
			resp.Body.Close()
			cancel()

			if scanErr != nil {
				lastErr = fmt.Errorf("read error (%s): %w", candidateModel, scanErr)
				continue
			}

			content, err := parseOmniRouteResponse(rawBuf.Bytes())
			if err != nil {
				lastErr = fmt.Errorf("parse error (%s): %w", candidateModel, err)
				if strings.Contains(strings.ToLower(err.Error()), "404") || strings.Contains(strings.ToLower(err.Error()), "cooldown") {
					break
				}
				continue
			}

			if strings.Contains(strings.ToLower(content), "this version of antigravity is no longer supported") {
				sawBannerErr = true
				lastErr = fmt.Errorf("upstream gateway returned unsupported Antigravity client banner; refusing to overwrite file")
				break
			}

			return content, candidateModel, nil
		}
	}

	if sawBannerErr {
		return "", "", fmt.Errorf("upstream gateway returned unsupported Antigravity client banner; refusing to overwrite file")
	}
	return "", "", fmt.Errorf("retries exhausted: %w", lastErr)
}
