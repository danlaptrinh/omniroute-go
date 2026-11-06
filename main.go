package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"omni-parallel-mcp/gateway"
)

// --- JSON-RPC 2.0 & MCP Types ---

type RPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type RPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type ToolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type ParallelArgs struct {
	BaseDir        string       `json:"base_dir,omitempty"`
	SharedContext  string       `json:"shared_context,omitempty"`
	ContextFiles   []string     `json:"context_files,omitempty"`
	SystemPrompt   string       `json:"system_prompt,omitempty"`
	Model          string       `json:"model,omitempty"`
	Endpoint       string       `json:"endpoint,omitempty"`
	MaxConcurrency int          `json:"max_concurrency,omitempty"`
	Tasks          []WorkerTask `json:"tasks"`
}

type WorkerTask struct {
	FilePath string `json:"file_path"`
	Prompt   string `json:"prompt"`
	Model    string `json:"model,omitempty"`
}

type TaskResult struct {
	Index    int
	FilePath string
	Bytes    int
	Lines    int
	Duration time.Duration
	Err      error
}

// --- OpenAI / OmniRoute Chat Completion Types ---

type ChatRequest struct {
	Model           string                  `json:"model"`
	Messages        []ChatMessage           `json:"messages"`
	Temperature     float64                 `json:"temperature"`
	Stream          bool                    `json:"stream"`
	ReasoningEffort string                  `json:"reasoning_effort,omitempty"`
	ThinkingConfig  *gateway.ThinkingConfig `json:"thinking_config,omitempty"`
}

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatChoice struct {
	Message ChatMessage `json:"message"`
	Delta   ChatMessage `json:"delta"`
}

type ChatResponse struct {
	Choices []ChatChoice `json:"choices"`
	Error   *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

var httpClient = &http.Client{
	Timeout: 5 * time.Minute,
	Transport: &http.Transport{
		MaxIdleConns:        512,
		MaxIdleConnsPerHost: 256,
		IdleConnTimeout:     90 * time.Second,
	},
}

func main() {
	dec := json.NewDecoder(os.Stdin)
	var outMu sync.Mutex
	var inflight sync.WaitGroup

	send := func(resp RPCResponse) {
		outMu.Lock()
		defer outMu.Unlock()
		b, _ := json.Marshal(resp)
		os.Stdout.Write(append(b, '\n'))
	}

	for {
		var req RPCRequest
		if err := dec.Decode(&req); err != nil {
			if err == io.EOF {
				inflight.Wait()
				return
			}
			continue
		}

		// Ignore JSON-RPC notifications (no ID)
		if len(req.ID) == 0 || string(req.ID) == "null" {
			continue
		}

		switch req.Method {
		case "initialize":
			protoVer := "2024-11-05"
			var initParams struct {
				ProtocolVersion string `json:"protocolVersion"`
			}
			if json.Unmarshal(req.Params, &initParams) == nil && initParams.ProtocolVersion != "" {
				protoVer = initParams.ProtocolVersion
			}
			send(RPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result: map[string]any{
					"protocolVersion": protoVer,
					"capabilities":    map[string]any{"tools": map[string]any{}},
					"serverInfo": map[string]any{
						"name":    "omni-parallel-dispatcher",
						"version": "1.0.0",
					},
				},
			})

		case "ping":
			send(RPCResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{}})

		case "tools/list":
			send(RPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result: map[string]any{
					"tools": []map[string]any{toolDefinition()},
				},
			})

		case "tools/call":
			inflight.Add(1)
			go func(r RPCRequest) {
				defer inflight.Done()
				send(handleToolCall(r))
			}(req)

		default:
			send(RPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error:   &RPCError{Code: -32601, Message: "Method not found: " + req.Method},
			})
		}
	}
}

func toolDefinition() map[string]any {
	return map[string]any{
		"name":        "omni_parallel_workers",
		"description": "Spawn N parallel Goroutines (e.g. 100 workers at 100% concurrency) hitting OmniRoute (/v1/chat/completions) simultaneously to generate code and write files directly to disk in a single turn.",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"base_dir": map[string]any{
					"type":        "string",
					"description": "Optional base directory for resolving relative file_path and context_files.",
				},
				"shared_context": map[string]any{
					"type":        "string",
					"description": "Shared context (e.g. contracts.go content, interfaces, package conventions) passed to all parallel workers.",
				},
				"context_files": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "Optional list of file paths (e.g. ['contracts.go', 'go.mod']) read once from disk by the MCP server and injected into all workers.",
				},
				"system_prompt": map[string]any{
					"type":        "string",
					"description": "Optional system prompt override for all workers.",
				},
				"model": map[string]any{
					"type":        "string",
					"description": "Optional model name override for OmniRoute (defaults to OMNIROUTE_MODEL env var).",
				},
				"max_concurrency": map[string]any{
					"type":        "integer",
					"description": "Maximum concurrent HTTP requests (0 = unlimited 100% parallel goroutines for all tasks).",
				},
				"tasks": map[string]any{
					"type":        "array",
					"description": "List of file generation tasks executed simultaneously in separate goroutines.",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"file_path": map[string]any{
								"type":        "string",
								"description": "Target file path on disk where the generated code will be saved.",
							},
							"prompt": map[string]any{
								"type":        "string",
								"description": "Specific instruction for generating this file.",
							},
							"model": map[string]any{
								"type":        "string",
								"description": "Optional per-task model override.",
							},
						},
						"required": []string{"file_path", "prompt"},
					},
				},
			},
			"required": []string{"tasks"},
		},
	}
}

func handleToolCall(req RPCRequest) RPCResponse {
	var call ToolCallParams
	if err := json.Unmarshal(req.Params, &call); err != nil {
		return rpcErr(req.ID, -32602, "Invalid params: "+err.Error())
	}
	if call.Name != "omni_parallel_workers" {
		return rpcErr(req.ID, -32602, "Unknown tool: "+call.Name)
	}

	var args ParallelArgs
	if err := json.Unmarshal(call.Arguments, &args); err != nil {
		return rpcErr(req.ID, -32602, "Invalid arguments: "+err.Error())
	}
	if len(args.Tasks) == 0 {
		return toolTextResp(req.ID, "No tasks provided.", true)
	}

	startWall := time.Now()
	endpoint := resolveEndpoint(args.Endpoint)
	apiKey := firstNonEmpty(
		os.Getenv("OMNI_API_KEY"),
		os.Getenv("OMNIROUTE_API_KEY"),
		os.Getenv("AGY_OAUTH_TOKEN"),
		"sk-omni-1a3fbd3d0aaa37401bb556381d91796ad200219da8f3db1d04233062aed5d33d",
	)
	defaultModel := firstNonEmpty(
		args.Model,
		os.Getenv("OMNI_DEFAULT_MODEL"),
		os.Getenv("OMNIROUTE_MODEL"),
		"antigravity/gemini-4-argon-high",
	)
	sysPrompt := firstNonEmpty(args.SystemPrompt,
		"You are an elite software engineer worker. Output ONLY the raw, complete, compilable source code for the requested file. Do NOT wrap output in markdown fences (no ```go or ```), and do NOT include explanations outside code comments.")

	sharedCtx, err := buildSharedContext(args.BaseDir, args.SharedContext, args.ContextFiles)
	if err != nil {
		return toolTextResp(req.ID, "Failed to read context_files: "+err.Error(), true)
	}

	maxConc := args.MaxConcurrency
	if maxConc <= 0 {
		if envConc, err := strconv.Atoi(os.Getenv("OMNIROUTE_MAX_CONCURRENCY")); err == nil && envConc > 0 {
			maxConc = envConc
		}
	}
	var sem chan struct{}
	if maxConc > 0 {
		sem = make(chan struct{}, maxConc)
	}

	results := make([]TaskResult, len(args.Tasks))
	var wg sync.WaitGroup
	wg.Add(len(args.Tasks))

	for i, task := range args.Tasks {
		go func(idx int, t WorkerTask) {
			defer wg.Done()
			if sem != nil {
				sem <- struct{}{}
				defer func() { <-sem }()
			}

			tStart := time.Now()
			targetPath := resolvePath(args.BaseDir, t.FilePath)
			model := firstNonEmpty(t.Model, defaultModel)

			userPrompt := fmt.Sprintf("Target File: %s\n\nTask Instructions:\n%s", t.FilePath, t.Prompt)
			if sharedCtx != "" {
				userPrompt = fmt.Sprintf("=== SHARED CONTRACTS & CONTEXT ===\n%s\n\n=== TARGET FILE TASK ===\n%s", sharedCtx, userPrompt)
			}

			code, err := callOmniRoute(endpoint, apiKey, model, sysPrompt, userPrompt)
			if err != nil {
				results[idx] = TaskResult{Index: idx, FilePath: targetPath, Duration: time.Since(tStart), Err: err}
				return
			}

			cleanCode := stripCodeFences(code)
			if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
				results[idx] = TaskResult{Index: idx, FilePath: targetPath, Duration: time.Since(tStart), Err: err}
				return
			}
			if err := os.WriteFile(targetPath, []byte(cleanCode), 0644); err != nil {
				results[idx] = TaskResult{Index: idx, FilePath: targetPath, Duration: time.Since(tStart), Err: err}
				return
			}

			lines := strings.Count(cleanCode, "\n") + 1
			results[idx] = TaskResult{
				Index:    idx,
				FilePath: targetPath,
				Bytes:    len(cleanCode),
				Lines:    lines,
				Duration: time.Since(tStart),
			}
		}(i, task)
	}

	wg.Wait()
	wallElapsed := time.Since(startWall)

	var sb strings.Builder
	var okCount, errCount, totalBytes, totalLines int
	var sumDuration time.Duration

	for _, r := range results {
		sumDuration += r.Duration
		if r.Err != nil {
			errCount++
		} else {
			okCount++
			totalBytes += r.Bytes
			totalLines += r.Lines
		}
	}

	speedup := 1.0
	if wallElapsed > 0 {
		speedup = float64(sumDuration) / float64(wallElapsed)
	}

	fmt.Fprintf(&sb, "OmniRoute Parallel Dispatcher Summary:\n")
	fmt.Fprintf(&sb, "- Endpoint: %s (Model: %s)\n", endpoint, defaultModel)
	fmt.Fprintf(&sb, "- Workers Spawned: %d (%d succeeded, %d failed)\n", len(results), okCount, errCount)
	fmt.Fprintf(&sb, "- Total Output: %d lines (%d bytes)\n", totalLines, totalBytes)
	fmt.Fprintf(&sb, "- Wall-Clock Time: %s (Cumulative Worker Time: %s | Parallel Speedup: %.1fx)\n\n",
		wallElapsed.Round(time.Millisecond), sumDuration.Round(time.Millisecond), speedup)

	for _, r := range results {
		if r.Err != nil {
			fmt.Fprintf(&sb, "[ERR] #%03d %s (%s): %v\n", r.Index+1, r.FilePath, r.Duration.Round(time.Millisecond), r.Err)
		} else {
			fmt.Fprintf(&sb, "[OK]  #%03d %s (%d lines, %d bytes, %s)\n", r.Index+1, r.FilePath, r.Lines, r.Bytes, r.Duration.Round(time.Millisecond))
		}
	}

	return toolTextResp(req.ID, sb.String(), errCount == len(results))
}

func callOmniRoute(endpoint, apiKey, model, sysPrompt, userPrompt string) (string, error) {
	resolved := gateway.ResolveModel(model, nil)
	candidates := resolved.FallbackChain
	if len(candidates) == 0 {
		candidates = []string{model}
	}

	var lastErr error

	for _, candidateModel := range candidates {
		reqObj := ChatRequest{
			Model: candidateModel,
			Messages: []ChatMessage{
				{Role: "system", Content: sysPrompt},
				{Role: "user", Content: userPrompt},
			},
			Temperature:     0.1,
			Stream:          false,
			ReasoningEffort: resolved.ReasoningEffort,
		}
		if resolved.IncludeThoughts || resolved.ThinkingLevel != gateway.ThinkingLevelNone {
			reqObj.ThinkingConfig = &gateway.ThinkingConfig{
				IncludeThoughts: resolved.IncludeThoughts,
				ThinkingLevel:   resolved.ThinkingLevel,
			}
		}

		payload, err := json.Marshal(reqObj)
		if err != nil {
			return "", err
		}

		maxRetries := 2
		for attempt := 0; attempt < maxRetries; attempt++ {
			if attempt > 0 {
				time.Sleep(time.Duration(attempt*300) * time.Millisecond)
			}

			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
			httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
			if err != nil {
				cancel()
				return "", err
			}
			httpReq.Header.Set("Content-Type", "application/json")
			if apiKey != "" {
				httpReq.Header.Set("Authorization", "Bearer "+apiKey)
			}

			resp, err := httpClient.Do(httpReq)
			if err != nil {
				cancel()
				lastErr = err
				continue
			}

			body, readErr := io.ReadAll(resp.Body)
			resp.Body.Close()
			cancel()

			if readErr != nil {
				lastErr = readErr
				continue
			}

			if resp.StatusCode == 404 || resp.StatusCode == 400 || resp.StatusCode == 503 {
				// Model not recognized or unavailable on this upstream; break inner retry loop and try next candidate in FallbackChain
				lastErr = fmt.Errorf("HTTP %d (%s): %s", resp.StatusCode, candidateModel, truncate(string(body), 200))
				break
			}
			if resp.StatusCode == 429 || resp.StatusCode >= 500 {
				lastErr = fmt.Errorf("HTTP %d (%s): %s", resp.StatusCode, candidateModel, truncate(string(body), 200))
				continue
			}
			if resp.StatusCode < 200 || resp.StatusCode >= 300 {
				return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(body), 300))
			}

			content, err := parseOmniRouteResponse(body)
			if err != nil {
				lastErr = err
				if strings.Contains(strings.ToLower(err.Error()), "404") || strings.Contains(strings.ToLower(err.Error()), "cooldown") {
					break
				}
				continue
			}
			return content, nil
		}
	}

	return "", fmt.Errorf("retries exhausted: %w", lastErr)
}

func parseOmniRouteResponse(body []byte) (string, error) {
	trimmed := bytes.TrimSpace(body)
	// Handle SSE streaming format ("data: {...}") if upstream returns event-stream
	if bytes.HasPrefix(trimmed, []byte("data:")) || bytes.Contains(trimmed, []byte("\ndata:")) {
		var sb strings.Builder
		for _, line := range strings.Split(string(trimmed), "\n") {
			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			dataStr := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if dataStr == "" || dataStr == "[DONE]" {
				continue
			}
			var chunk ChatResponse
			if err := json.Unmarshal([]byte(dataStr), &chunk); err != nil {
				continue
			}
			if chunk.Error != nil && chunk.Error.Message != "" {
				return "", fmt.Errorf("API stream error: %s", chunk.Error.Message)
			}
			if len(chunk.Choices) > 0 {
				if chunk.Choices[0].Delta.Content != "" {
					sb.WriteString(chunk.Choices[0].Delta.Content)
				} else if chunk.Choices[0].Message.Content != "" {
					sb.WriteString(chunk.Choices[0].Message.Content)
				}
			}
		}
		if sb.Len() == 0 {
			return "", fmt.Errorf("empty SSE stream content from OmniRoute")
		}
		return sb.String(), nil
	}

	var chatResp ChatResponse
	if err := json.Unmarshal(trimmed, &chatResp); err != nil {
		return "", fmt.Errorf("invalid JSON response: %w", err)
	}
	if chatResp.Error != nil && chatResp.Error.Message != "" {
		return "", fmt.Errorf("API error: %s", chatResp.Error.Message)
	}
	if len(chatResp.Choices) == 0 {
		return "", fmt.Errorf("empty choices from OmniRoute")
	}
	return chatResp.Choices[0].Message.Content, nil
}

func buildSharedContext(baseDir, inlineCtx string, files []string) (string, error) {
	var parts []string
	if strings.TrimSpace(inlineCtx) != "" {
		parts = append(parts, inlineCtx)
	}
	for _, f := range files {
		p := resolvePath(baseDir, f)
		b, err := os.ReadFile(p)
		if err != nil {
			return "", fmt.Errorf("%s: %w", p, err)
		}
		parts = append(parts, fmt.Sprintf("// --- File: %s ---\n%s", f, string(b)))
	}
	return strings.Join(parts, "\n\n"), nil
}

func resolveEndpoint(override string) string {
	base := firstNonEmpty(
		override,
		os.Getenv("OMNI_GATEWAY_URL"),
		os.Getenv("GATEWAY_BASE_URL"),
		os.Getenv("OMNIROUTE_BASE_URL"),
		"https://omni.bucloud.vip/v1",
	)
	base = strings.TrimRight(base, "/")
	if strings.HasSuffix(base, "/chat/completions") {
		return base
	}
	if strings.HasSuffix(base, "/v1") {
		return base + "/chat/completions"
	}
	return base + "/v1/chat/completions"
}

func resolvePath(baseDir, target string) string {
	if filepath.IsAbs(target) || baseDir == "" {
		return filepath.Clean(target)
	}
	return filepath.Join(baseDir, target)
}

func stripCodeFences(s string) string {
	trimmed := strings.TrimSpace(s)
	// Handle case where LLM wraps code in ```lang ... ```
	if strings.HasPrefix(trimmed, "```") {
		if firstNL := strings.IndexByte(trimmed, '\n'); firstNL != -1 {
			inner := trimmed[firstNL+1:]
			if lastFence := strings.LastIndex(inner, "```"); lastFence != -1 {
				inner = inner[:lastFence]
			}
			return strings.TrimSpace(inner) + "\n"
		}
	}
	return trimmed + "\n"
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

func rpcErr(id json.RawMessage, code int, msg string) RPCResponse {
	return RPCResponse{JSONRPC: "2.0", ID: id, Error: &RPCError{Code: code, Message: msg}}
}

func toolTextResp(id json.RawMessage, text string, isError bool) RPCResponse {
	return RPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result: map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": text},
			},
			"isError": isError,
		},
	}
}
