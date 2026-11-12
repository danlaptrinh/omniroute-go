package main

import (
	"bytes"
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
	"unicode/utf8"

	"omni-parallel-mcp/gateway"
)

const DefaultModel = "antigravity/gemini-4-carbon-high"

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
	ModelPool      []string     `json:"model_pool,omitempty"`
	PinAccounts    []string     `json:"pin_accounts,omitempty"`
	Endpoint       string       `json:"endpoint,omitempty"`
	APIKey         string       `json:"api_key,omitempty"`
	MaxConcurrency int          `json:"max_concurrency,omitempty"`
	Tasks          []WorkerTask `json:"tasks"`
}

type WorkerTask struct {
	FilePath      string `json:"file_path"`
	Prompt        string `json:"prompt"`
	Model         string `json:"model,omitempty"`
	PinnedAccount string `json:"pinned_account,omitempty"`
}

type TaskResult struct {
	Index         int
	FilePath      string
	PrimaryModel  string
	ModelUsed     string
	PinnedAccount string
	Status        string
	Error         string
	Bytes         int
	Lines         int
	Duration      time.Duration
	Err           error
}

// --- OpenAI / OmniRoute Chat Completion Types ---

type ChatRequest struct {
	Model           string                  `json:"model"`
	Messages        []ChatMessage           `json:"messages"`
	Temperature     float64                 `json:"temperature"`
	MaxTokens       int                     `json:"max_tokens,omitempty"`
	Stream          bool                    `json:"stream"`
	ReasoningEffort string                  `json:"reasoning_effort,omitempty"`
	ThinkingConfig  *gateway.ThinkingConfig `json:"thinking_config,omitempty"`
}

type ChatMessage struct {
	Role             string `json:"role"`
	Content          string `json:"content"`
	ReasoningContent string `json:"reasoning_content,omitempty"`
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
	Timeout: 12 * time.Minute,
	Transport: &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          512,
		MaxIdleConnsPerHost:   256,
		MaxConnsPerHost:       0,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		WriteBufferSize:       64 * 1024,
		ReadBufferSize:        64 * 1024,
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
		"description": "Spawn N parallel Goroutines (e.g. 100 workers at 100% concurrency) hitting OmniRoute (/v1/chat/completions) simultaneously using antigravity/gemini-4-carbon-high by default (falling back to antigravity/gemini-4-argon-high only when Carbon is stuck or rate-limited) to generate code and write files directly to disk in a single turn.",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"base_dir": map[string]any{
					"type":        "string",
					"description": "Optional base directory for resolving and confining relative file_path and context_files.",
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
					"description": "Optional model name override for OmniRoute (e.g. 'antigravity/gemini-4-carbon-high' or 'antigravity/gemini-4-argon-high'; defaults to OMNI_DEFAULT_MODEL / OMNIROUTE_MODEL env var).",
				},
				"model_pool": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "Optional list of models to stripe parallel workers across in round-robin order (worker idx uses model_pool[idx % len(model_pool)] as primary, with remaining pool models + antigravity/gemini-4-argon-high as automatic failover; defaults to OMNI_MODEL_POOL comma-separated env var when unset).",
				},
				"pin_accounts": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "Optional list of OmniRoute account identifiers/emails to stripe across workers via the X-Omni-Pin-Account header (worker idx uses pin_accounts[idx % len(pin_accounts)] unless overridden by task.pinned_account).",
				},
				"endpoint": map[string]any{
					"type":        "string",
					"description": "Optional OmniRoute /v1/chat/completions endpoint URL override (defaults to OMNI_GATEWAY_URL / OMNIROUTE_BASE_URL).",
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
							"pinned_account": map[string]any{
								"type":        "string",
								"description": "Optional per-task OmniRoute account identifier/email sent via the X-Omni-Pin-Account header.",
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
	apiKey := resolveAPIKey(args.APIKey)
	defaultModel := resolveDefaultModel(args.Model)
	modelPool := resolveModelPool(args.ModelPool, args.Model)
	pinAccounts := cleanStringSlice(args.PinAccounts)
	sysPrompt := firstNonEmpty(args.SystemPrompt,
		"You are an elite software engineer worker. Output ONLY the raw, complete, compilable source code for the requested file. Do NOT wrap output in markdown fences (no ```go or ```), and do NOT include explanations outside code comments.")

	sharedCtx, err := buildSharedContext(args.BaseDir, args.SharedContext, args.ContextFiles)
	if err != nil {
		return toolTextResp(req.ID, "Failed to read context_files: "+err.Error(), true)
	}

	maxConc := args.MaxConcurrency
	if maxConc <= 0 {
		if envConc, err := strconv.Atoi(firstNonEmpty(os.Getenv("OMNI_MAX_CONCURRENCY"), os.Getenv("OMNIROUTE_MAX_CONCURRENCY"))); err == nil && envConc > 0 {
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
			results[idx] = executeWorkerTaskWithPool(idx, args.BaseDir, t, endpoint, apiKey, defaultModel, modelPool, pinAccounts, sysPrompt, sharedCtx)
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
	if len(modelPool) > 0 {
		fmt.Fprintf(&sb, "- Endpoint: %s (Model Pool [%d]: %s | Fallback: %s)\n", endpoint, len(modelPool), strings.Join(modelPool, ", "), DefaultFallbackModel)
	} else {
		fmt.Fprintf(&sb, "- Endpoint: %s (Primary: %s | Fallback: %s)\n", endpoint, defaultModel, primaryFallbackLabel(defaultModel))
	}
	if len(pinAccounts) > 0 {
		fmt.Fprintf(&sb, "- Account Pin Pool [%d]: %s\n", len(pinAccounts), strings.Join(pinAccounts, ", "))
	}
	fmt.Fprintf(&sb, "- Workers Spawned: %d (%d succeeded, %d failed)\n", len(results), okCount, errCount)
	fmt.Fprintf(&sb, "- Total Output: %d lines (%d bytes)\n", totalLines, totalBytes)
	fmt.Fprintf(&sb, "- Wall-Clock Time: %s (Cumulative Worker Time: %s | Parallel Speedup: %.1fx)\n\n",
		wallElapsed.Round(time.Millisecond), sumDuration.Round(time.Millisecond), speedup)

	for _, r := range results {
		expectedPrimary := firstNonEmpty(r.PrimaryModel, defaultModel)
		if r.Err != nil {
			fmt.Fprintf(&sb, "[ERR] #%03d %s (%s): %v\n", r.Index+1, r.FilePath, r.Duration.Round(time.Millisecond), r.Err)
		} else if r.ModelUsed != "" && !strings.EqualFold(r.ModelUsed, expectedPrimary) {
			fmt.Fprintf(&sb, "[OK]  #%03d %s (%d lines, %d bytes, %s) [failover -> %s]\n", r.Index+1, r.FilePath, r.Lines, r.Bytes, r.Duration.Round(time.Millisecond), r.ModelUsed)
		} else if len(modelPool) > 0 && r.ModelUsed != "" {
			fmt.Fprintf(&sb, "[OK]  #%03d %s (%d lines, %d bytes, %s) [model: %s]\n", r.Index+1, r.FilePath, r.Lines, r.Bytes, r.Duration.Round(time.Millisecond), r.ModelUsed)
		} else {
			fmt.Fprintf(&sb, "[OK]  #%03d %s (%d lines, %d bytes, %s)\n", r.Index+1, r.FilePath, r.Lines, r.Bytes, r.Duration.Round(time.Millisecond))
		}
	}

	return toolTextResp(req.ID, sb.String(), errCount == len(results))
}

func callOmniRoute(endpoint, apiKey, model, sysPrompt, userPrompt string) (string, error) {
	content, _, err := callOmniRouteWithFailover(endpoint, apiKey, model, sysPrompt, userPrompt)
	return content, err
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

func buildUserPrompt(filePath, taskPrompt, sharedCtx string) string {
	userPrompt := fmt.Sprintf("Target File: %s\n\nTask Instructions:\n%s", filePath, taskPrompt)
	if sharedCtx != "" {
		userPrompt = fmt.Sprintf("=== SHARED CONTRACTS & CONTEXT ===\n%s\n\n=== TARGET FILE TASK ===\n%s", sharedCtx, userPrompt)
	}
	return userPrompt
}

func executeWorkerTask(idx int, baseDir string, t WorkerTask, endpoint, apiKey, defaultModel, sysPrompt, sharedCtx string) TaskResult {
	return executeWorkerTaskWithPool(idx, baseDir, t, endpoint, apiKey, defaultModel, nil, nil, sysPrompt, sharedCtx)
}

func executeWorkerTaskWithPool(idx int, baseDir string, t WorkerTask, endpoint, apiKey, defaultModel string, modelPool, pinAccounts []string, sysPrompt, sharedCtx string) TaskResult {
	tStart := time.Now()
	targetPath, err := resolvePathSafe(baseDir, t.FilePath)
	if err != nil {
		return TaskResult{
			Index:    idx,
			FilePath: t.FilePath,
			Status:   "error",
			Error:    err.Error(),
			Duration: time.Since(tStart),
			Err:      err,
		}
	}

	candidates := buildWorkerCandidateChain(idx, t.Model, defaultModel, modelPool)
	primaryModel := defaultModel
	if len(candidates) > 0 {
		primaryModel = candidates[0]
	}
	pinnedAcc := resolveWorkerPinnedAccount(idx, t.PinnedAccount, pinAccounts)
	userPrompt := buildUserPrompt(t.FilePath, t.Prompt, sharedCtx)

	rawOutput, usedModel, err := callOmniRouteCandidates(endpoint, apiKey, candidates, pinnedAcc, sysPrompt, userPrompt)
	if err != nil {
		return TaskResult{
			Index:         idx,
			FilePath:      targetPath,
			PrimaryModel:  primaryModel,
			PinnedAccount: pinnedAcc,
			Status:        "error",
			Error:         err.Error(),
			Duration:      time.Since(tStart),
			Err:           err,
		}
	}

	cleanCode := stripMarkdownFences(rawOutput)
	if strings.Contains(strings.ToLower(cleanCode), "this version of antigravity is no longer supported") {
		bannerErr := fmt.Errorf("upstream gateway returned unsupported Antigravity client banner; refusing to overwrite file")
		return TaskResult{
			Index:         idx,
			FilePath:      targetPath,
			PrimaryModel:  primaryModel,
			ModelUsed:     usedModel,
			PinnedAccount: pinnedAcc,
			Status:        "error",
			Error:         bannerErr.Error(),
			Duration:      time.Since(tStart),
			Err:           bannerErr,
		}
	}

	if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
		return TaskResult{
			Index:         idx,
			FilePath:      targetPath,
			PrimaryModel:  primaryModel,
			ModelUsed:     usedModel,
			PinnedAccount: pinnedAcc,
			Status:        "error",
			Error:         err.Error(),
			Duration:      time.Since(tStart),
			Err:           err,
		}
	}
	if err := os.WriteFile(targetPath, []byte(cleanCode), 0644); err != nil {
		return TaskResult{
			Index:         idx,
			FilePath:      targetPath,
			PrimaryModel:  primaryModel,
			ModelUsed:     usedModel,
			PinnedAccount: pinnedAcc,
			Status:        "error",
			Error:         err.Error(),
			Duration:      time.Since(tStart),
			Err:           err,
		}
	}

	lines := strings.Count(cleanCode, "\n") + 1
	return TaskResult{
		Index:         idx,
		FilePath:      targetPath,
		PrimaryModel:  primaryModel,
		ModelUsed:     usedModel,
		PinnedAccount: pinnedAcc,
		Status:        "ok",
		Bytes:         len(cleanCode),
		Lines:         lines,
		Duration:      time.Since(tStart),
	}
}

func buildSharedContext(baseDir, inlineCtx string, files []string) (string, error) {
	var parts []string
	if strings.TrimSpace(inlineCtx) != "" {
		parts = append(parts, inlineCtx)
	}
	for _, f := range files {
		var p string
		if strings.TrimSpace(baseDir) != "" && !filepath.IsAbs(filepath.FromSlash(strings.TrimSpace(f))) {
			safePath, err := resolvePathSafe(baseDir, f)
			if err != nil {
				return "", err
			}
			p = safePath
		} else {
			p = resolvePath(baseDir, f)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return "", fmt.Errorf("%s: %w", p, err)
		}
		parts = append(parts, fmt.Sprintf("// --- File: %s ---\n%s", f, string(b)))
	}
	return strings.Join(parts, "\n\n"), nil
}

func resolveAPIKey(argsKey string) string {
	return firstNonEmpty(
		argsKey,
		os.Getenv("OMNI_API_KEY"),
		os.Getenv("OMNIROUTE_API_KEY"),
		os.Getenv("OPENAI_API_KEY"),
		os.Getenv("AGY_OAUTH_TOKEN"),
	)
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
	if filepath.IsAbs(target) || strings.TrimSpace(baseDir) == "" {
		return filepath.Clean(target)
	}
	return filepath.Join(baseDir, target)
}

func resolvePathSafe(baseDir, p string) (string, error) {
	trimmedPath := strings.TrimSpace(p)
	if trimmedPath == "" {
		return "", fmt.Errorf("file path cannot be empty")
	}
	normalized := filepath.FromSlash(trimmedPath)
	if strings.TrimSpace(baseDir) == "" {
		return filepath.Clean(normalized), nil
	}

	var target string
	if filepath.IsAbs(normalized) {
		target = filepath.Clean(normalized)
	} else {
		target = filepath.Join(baseDir, normalized)
	}

	absBase, err := filepath.Abs(baseDir)
	if err != nil {
		return "", fmt.Errorf("invalid base_dir %q: %w", baseDir, err)
	}
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return "", fmt.Errorf("invalid target path %q: %w", p, err)
	}

	rel, err := filepath.Rel(absBase, absTarget)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("security error: path %q escapes base_dir %q", p, baseDir)
	}
	return absTarget, nil
}

func stripMarkdownFences(s string) string {
	return stripCodeFences(s)
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

func truncate(s string, maxLen int) string {
	if maxLen < 0 {
		maxLen = 0
	}
	if utf8.RuneCountInString(s) <= maxLen {
		return s
	}
	count := 0
	for idx := range s {
		if count == maxLen {
			return s[:idx] + "..."
		}
		count++
	}
	return s
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
