package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func extractToolResultText(t *testing.T, resp RPCResponse) string {
	t.Helper()
	if resp.Error != nil {
		t.Fatalf("unexpected RPC error: %+v", resp.Error)
	}
	raw, err := json.Marshal(resp.Result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	var parsed struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if parsed.IsError {
		t.Fatalf("expected non-error tool result, got error text: %v", parsed.Content)
	}
	if len(parsed.Content) == 0 {
		return ""
	}
	return parsed.Content[0].Text
}

func TestDefaultModelIsCarbonWithoutTouchingArgon(t *testing.T) {
	t.Setenv("OMNI_DEFAULT_MODEL", "")
	t.Setenv("OMNIROUTE_MODEL", "")
	t.Setenv("OMNI_FALLBACK_MODELS", "")
	t.Setenv("OMNI_MODEL_POOL", "")

	if DefaultModel != "antigravity/gemini-4-carbon-high" {
		t.Fatalf("DefaultModel = %q, want antigravity/gemini-4-carbon-high", DefaultModel)
	}
	if DefaultFallbackModel != "antigravity/gemini-4-argon-high" {
		t.Fatalf("DefaultFallbackModel = %q, want antigravity/gemini-4-argon-high", DefaultFallbackModel)
	}

	chain := buildCandidateChain(DefaultModel)
	if len(chain) < 2 {
		t.Fatalf("expected at least 2 candidates in chain, got %v", chain)
	}
	if chain[0] != "antigravity/gemini-4-carbon-high" {
		t.Fatalf("chain[0] = %q, want antigravity/gemini-4-carbon-high", chain[0])
	}
	if chain[1] != "antigravity/gemini-4-argon-high" {
		t.Fatalf("chain[1] = %q, want antigravity/gemini-4-argon-high (full chain: %v)", chain[1], chain)
	}

	var mu sync.Mutex
	var requestedModels []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqObj ChatRequest
		_ = json.NewDecoder(r.Body).Decode(&reqObj)
		mu.Lock()
		requestedModels = append(requestedModels, reqObj.Model)
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ChatResponse{
			Choices: []ChatChoice{
				{
					Message: ChatMessage{
						Role:    "assistant",
						Content: "package sample\n\nfunc CarbonPrimary() string { return \"carbon\" }\n",
					},
				},
			},
		})
	}))
	defer srv.Close()

	tmpDir := t.TempDir()
	argsBytes, _ := json.Marshal(ParallelArgs{
		BaseDir:  tmpDir,
		Endpoint: srv.URL + "/v1/chat/completions",
		Tasks: []WorkerTask{
			{FilePath: "carbon_ok.go", Prompt: "Generate sample function"},
		},
	})
	paramsBytes, _ := json.Marshal(ToolCallParams{
		Name:      "omni_parallel_workers",
		Arguments: argsBytes,
	})
	resp := handleToolCall(RPCRequest{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "tools/call",
		Params:  paramsBytes,
	})

	summary := extractToolResultText(t, resp)
	if strings.Contains(summary, "[failover ->") {
		t.Fatalf("did not expect failover when Carbon succeeds, got summary:\n%s", summary)
	}

	mu.Lock()
	gotModels := append([]string(nil), requestedModels...)
	mu.Unlock()

	if len(gotModels) != 1 || gotModels[0] != "antigravity/gemini-4-carbon-high" {
		t.Fatalf("expected exactly 1 call to antigravity/gemini-4-carbon-high and 0 to Argon, got %v", gotModels)
	}
}

func TestCarbonStuckFailsOverImmediatelyToArgon(t *testing.T) {
	cases := []struct {
		name       string
		carbonCode int
		hangDelay  time.Duration
	}{
		{name: "HTTP_429_RateLimit", carbonCode: http.StatusTooManyRequests},
		{name: "HTTP_403_AccountLocked", carbonCode: http.StatusForbidden},
		{name: "HTTP_503_ServiceUnavailable", carbonCode: http.StatusServiceUnavailable},
		{name: "Timeout_Stuck_Hang", hangDelay: 600 * time.Millisecond},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("OMNI_DEFAULT_MODEL", "antigravity/gemini-4-carbon-high")
			t.Setenv("OMNI_FALLBACK_MODELS", "antigravity/gemini-4-argon-high")
			t.Setenv("OMNI_MODEL_POOL", "")
			if tc.hangDelay > 0 {
				t.Setenv("OMNI_PRIMARY_TIMEOUT_SEC", "0.2")
			} else {
				t.Setenv("OMNI_PRIMARY_TIMEOUT_SEC", "5")
			}

			var mu sync.Mutex
			var calls []string

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var reqObj ChatRequest
				_ = json.NewDecoder(r.Body).Decode(&reqObj)

				mu.Lock()
				calls = append(calls, reqObj.Model)
				mu.Unlock()

				if reqObj.Model == "antigravity/gemini-4-carbon-high" {
					if tc.hangDelay > 0 {
						select {
						case <-time.After(tc.hangDelay):
						case <-r.Context().Done():
							return
						}
					}
					code := tc.carbonCode
					if code == 0 {
						code = http.StatusGatewayTimeout
					}
					w.WriteHeader(code)
					_, _ = w.Write([]byte(`{"error":{"message":"carbon stuck or overloaded"}}`))
					return
				}

				if reqObj.Model == "antigravity/gemini-4-argon-high" {
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(ChatResponse{
						Choices: []ChatChoice{
							{
								Message: ChatMessage{
									Role:    "assistant",
									Content: "```go\npackage fallbackpkg\n\nfunc FromArgon() string { return \"argon\" }\n```",
								},
							},
						},
					})
					return
				}

				w.WriteHeader(http.StatusNotFound)
			}))
			defer srv.Close()

			tmpDir := t.TempDir()
			argsBytes, _ := json.Marshal(ParallelArgs{
				BaseDir:  tmpDir,
				Endpoint: srv.URL + "/v1/chat/completions",
				Tasks: []WorkerTask{
					{FilePath: "fallback_out.go", Prompt: "Generate fallbackpkg"},
				},
			})
			paramsBytes, _ := json.Marshal(ToolCallParams{
				Name:      "omni_parallel_workers",
				Arguments: argsBytes,
			})

			resp := handleToolCall(RPCRequest{
				JSONRPC: "2.0",
				ID:      json.RawMessage(`2`),
				Method:  "tools/call",
				Params:  paramsBytes,
			})

			summary := extractToolResultText(t, resp)
			if !strings.Contains(summary, "[failover -> antigravity/gemini-4-argon-high]") {
				t.Fatalf("expected failover annotation in summary, got:\n%s", summary)
			}

			mu.Lock()
			gotCalls := append([]string(nil), calls...)
			mu.Unlock()

			if len(gotCalls) != 2 ||
				gotCalls[0] != "antigravity/gemini-4-carbon-high" ||
				gotCalls[1] != "antigravity/gemini-4-argon-high" {
				t.Fatalf("expected [antigravity/gemini-4-carbon-high, antigravity/gemini-4-argon-high], got %v", gotCalls)
			}

			written, err := os.ReadFile(filepath.Join(tmpDir, "fallback_out.go"))
			if err != nil {
				t.Fatalf("expected fallback_out.go to be written: %v", err)
			}
			if strings.Contains(string(written), "```") || !strings.Contains(string(written), "FromArgon") {
				t.Fatalf("unexpected generated content: %q", string(written))
			}
		})
	}
}

func TestStreamingIdleTimerAllowsLongOutputAndSends65536MaxTokens(t *testing.T) {
	t.Setenv("OMNI_DEFAULT_MODEL", "antigravity/gemini-4-carbon-high")
	t.Setenv("OMNI_FALLBACK_MODELS", "antigravity/gemini-4-argon-high")
	t.Setenv("OMNI_MODEL_POOL", "")
	// Primary timeout 150ms — total streaming time (~280ms) exceeds this,
	// but the per-chunk idle timer (300ms) keeps the connection alive.
	t.Setenv("OMNI_PRIMARY_TIMEOUT_SEC", "0.15")
	t.Setenv("OMNI_STREAM_IDLE_TIMEOUT_SEC", "0.30")

	var mu sync.Mutex
	var calls []string
	var capturedStream bool
	var capturedMaxTokens int

	codeChunks := []string{
		"package streaming\n\n",
		"// IdleTimerTest verifies streaming idle resets.\n",
		"func StreamOK() string {\n",
		"\treturn \"idle-timer-works\"\n}\n",
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqObj ChatRequest
		_ = json.NewDecoder(r.Body).Decode(&reqObj)

		mu.Lock()
		calls = append(calls, reqObj.Model)
		if reqObj.Stream {
			capturedStream = true
		}
		capturedMaxTokens = reqObj.MaxTokens
		mu.Unlock()

		if reqObj.Model == "antigravity/gemini-4-carbon-high" {
			// Serve SSE streaming response: 4 chunks spaced 70ms apart
			flusher, ok := w.(http.Flusher)
			if !ok {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("Connection", "keep-alive")
			w.WriteHeader(http.StatusOK)

			for i, chunk := range codeChunks {
				if i > 0 {
					time.Sleep(70 * time.Millisecond)
				}
				sseChunk := ChatResponse{
					Choices: []ChatChoice{
						{
							Delta: ChatMessage{
								Role:    "assistant",
								Content: chunk,
							},
						},
					},
				}
				chunkJSON, _ := json.Marshal(sseChunk)
				_, _ = fmt.Fprintf(w, "data: %s\n\n", chunkJSON)
				flusher.Flush()
			}

			// Final done sentinel after a small pause
			time.Sleep(70 * time.Millisecond)
			_, _ = fmt.Fprintf(w, "data: [DONE]\n\n")
			flusher.Flush()
			return
		}

		// Argon fallback — should NOT be reached in this test
		if reqObj.Model == "antigravity/gemini-4-argon-high" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(ChatResponse{
				Choices: []ChatChoice{
					{
						Message: ChatMessage{
							Role:    "assistant",
							Content: "package streaming\n\nfunc ArgonFallback() string { return \"argon\" }\n",
						},
					},
				},
			})
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	tmpDir := t.TempDir()
	argsBytes, _ := json.Marshal(ParallelArgs{
		BaseDir:  tmpDir,
		Endpoint: srv.URL + "/v1/chat/completions",
		Tasks: []WorkerTask{
			{FilePath: "streaming_ok.go", Prompt: "Generate streaming package with StreamOK function"},
		},
	})
	paramsBytes, _ := json.Marshal(ToolCallParams{
		Name:      "omni_parallel_workers",
		Arguments: argsBytes,
	})

	resp := handleToolCall(RPCRequest{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`3`),
		Method:  "tools/call",
		Params:  paramsBytes,
	})

	summary := extractToolResultText(t, resp)

	// Verify no failover occurred — Carbon streaming should have succeeded
	if strings.Contains(summary, "[failover ->") {
		t.Fatalf("did not expect failover when streaming idle timer resets keep connection alive, got summary:\n%s", summary)
	}
	if strings.Contains(summary, "ArgonFallback") {
		t.Fatalf("did not expect Argon content in output, got summary:\n%s", summary)
	}

	// Verify only Carbon was called (no Argon)
	mu.Lock()
	gotCalls := append([]string(nil), calls...)
	gotStream := capturedStream
	gotMaxTokens := capturedMaxTokens
	mu.Unlock()

	if len(gotCalls) != 1 {
		t.Fatalf("expected exactly 1 call to Carbon, got %d calls: %v", len(gotCalls), gotCalls)
	}
	if gotCalls[0] != "antigravity/gemini-4-carbon-high" {
		t.Fatalf("expected call to antigravity/gemini-4-carbon-high, got %q", gotCalls[0])
	}

	// Verify Stream: true was sent in the outbound request
	if !gotStream {
		t.Fatalf("expected outbound ChatRequest.Stream = true, got false")
	}

	// Verify MaxTokens: 65536 was sent in the outbound request
	if gotMaxTokens != 65536 {
		t.Fatalf("expected outbound ChatRequest.MaxTokens = 65536, got %d", gotMaxTokens)
	}

	// Verify the file was written with correct streamed content
	written, err := os.ReadFile(filepath.Join(tmpDir, "streaming_ok.go"))
	if err != nil {
		t.Fatalf("expected streaming_ok.go to be written: %v", err)
	}
	content := string(written)
	if !strings.Contains(content, "StreamOK") {
		t.Fatalf("expected StreamOK function in output, got:\n%s", content)
	}
	if !strings.Contains(content, "idle-timer-works") {
		t.Fatalf("expected 'idle-timer-works' string in output, got:\n%s", content)
	}
	if strings.Contains(content, "ArgonFallback") {
		t.Fatalf("output should not contain ArgonFallback, got:\n%s", content)
	}
	// Ensure markdown fences were stripped if any
	if strings.Contains(content, "```") {
		t.Fatalf("output should not contain markdown fences, got:\n%s", content)
	}
}

func extractTargetFileFromMessages(msgs []ChatMessage) string {
	for _, m := range msgs {
		if m.Role != "user" {
			continue
		}
		for _, line := range strings.Split(m.Content, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "Target File:") {
				return strings.TrimSpace(strings.TrimPrefix(line, "Target File:"))
			}
		}
	}
	return ""
}

func TestModelPoolStripes6TasksEvenlyAndFailsOverOn503(t *testing.T) {
	pool := []string{
		"antigravity/claude-opus-4-6-thinking",
		"antigravity/claude-sonnet-4-6",
		"antigravity/gemini-3-flash",
	}

	t.Run("Even_2_2_2_Parallel_Striping", func(t *testing.T) {
		t.Setenv("OMNI_DEFAULT_MODEL", "")
		t.Setenv("OMNI_FALLBACK_MODELS", "")
		t.Setenv("OMNI_MODEL_POOL", "")

		var mu sync.Mutex
		modelCounts := make(map[string]int)
		taskModels := make(map[string]string)
		var active int32
		var peakConcurrent int32

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cur := atomic.AddInt32(&active, 1)
			for {
				prev := atomic.LoadInt32(&peakConcurrent)
				if cur <= prev || atomic.CompareAndSwapInt32(&peakConcurrent, prev, cur) {
					break
				}
			}
			defer atomic.AddInt32(&active, -1)

			var reqObj ChatRequest
			_ = json.NewDecoder(r.Body).Decode(&reqObj)
			targetFile := extractTargetFileFromMessages(reqObj.Messages)

			// Hold briefly so all 6 parallel goroutines overlap in flight
			time.Sleep(50 * time.Millisecond)

			mu.Lock()
			modelCounts[reqObj.Model]++
			taskModels[targetFile] = reqObj.Model
			mu.Unlock()

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(ChatResponse{
				Choices: []ChatChoice{
					{
						Message: ChatMessage{
							Role:    "assistant",
							Content: fmt.Sprintf("package striped\n\n// Model: %s\nfunc File() string { return %q }\n", reqObj.Model, targetFile),
						},
					},
				},
			})
		}))
		defer srv.Close()

		tmpDir := t.TempDir()
		tasks := make([]WorkerTask, 6)
		for i := 0; i < 6; i++ {
			tasks[i] = WorkerTask{
				FilePath: fmt.Sprintf("worker_%d.go", i),
				Prompt:   fmt.Sprintf("Generate worker %d", i),
			}
		}

		argsBytes, _ := json.Marshal(ParallelArgs{
			BaseDir:   tmpDir,
			Endpoint:  srv.URL + "/v1/chat/completions",
			ModelPool: pool,
			Tasks:     tasks,
		})
		paramsBytes, _ := json.Marshal(ToolCallParams{
			Name:      "omni_parallel_workers",
			Arguments: argsBytes,
		})

		resp := handleToolCall(RPCRequest{
			JSONRPC: "2.0",
			ID:      json.RawMessage(`10`),
			Method:  "tools/call",
			Params:  paramsBytes,
		})

		summary := extractToolResultText(t, resp)
		if strings.Contains(summary, "[failover ->") {
			t.Fatalf("did not expect failover when all 3 models succeed, got summary:\n%s", summary)
		}

		if gotPeak := atomic.LoadInt32(&peakConcurrent); gotPeak < 2 {
			t.Fatalf("expected parallel execution across workers (peakConcurrent >= 2), got %d", gotPeak)
		}

		mu.Lock()
		defer mu.Unlock()
		for _, m := range pool {
			if modelCounts[m] != 2 {
				t.Fatalf("expected model %q to receive exactly 2 requests (2-2-2 striping), got counts=%v", m, modelCounts)
			}
		}
		for i := 0; i < 6; i++ {
			fName := fmt.Sprintf("worker_%d.go", i)
			wantModel := pool[i%len(pool)]
			if got := taskModels[fName]; got != wantModel {
				t.Fatalf("task %s routed to %q, want %q", fName, got, wantModel)
			}
			written, err := os.ReadFile(filepath.Join(tmpDir, fName))
			if err != nil {
				t.Fatalf("expected %s on disk: %v", fName, err)
			}
			if !strings.Contains(string(written), wantModel) {
				t.Fatalf("expected %s to contain %q, got:\n%s", fName, wantModel, string(written))
			}
		}
	})

	t.Run("Stripes_2_2_2_And_FailsOver_Cleanly_When_One_Model_Returns_503", func(t *testing.T) {
		t.Setenv("OMNI_DEFAULT_MODEL", "")
		t.Setenv("OMNI_FALLBACK_MODELS", "")
		t.Setenv("OMNI_MODEL_POOL", "")

		var mu sync.Mutex
		attemptsByTask := make(map[string][]string)
		primaryAttempts := make(map[string]int)

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var reqObj ChatRequest
			_ = json.NewDecoder(r.Body).Decode(&reqObj)
			targetFile := extractTargetFileFromMessages(reqObj.Messages)

			mu.Lock()
			if len(attemptsByTask[targetFile]) == 0 {
				primaryAttempts[reqObj.Model]++
			}
			attemptsByTask[targetFile] = append(attemptsByTask[targetFile], reqObj.Model)
			mu.Unlock()

			if reqObj.Model == "antigravity/claude-sonnet-4-6" {
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte(`{"error":{"message":"sonnet capacity exhausted"}}`))
				return
			}

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(ChatResponse{
				Choices: []ChatChoice{
					{
						Message: ChatMessage{
							Role:    "assistant",
							Content: fmt.Sprintf("package striped503\n\n// ServedBy: %s\nfunc OK() {}\n", reqObj.Model),
						},
					},
				},
			})
		}))
		defer srv.Close()

		tmpDir := t.TempDir()
		tasks := make([]WorkerTask, 6)
		for i := 0; i < 6; i++ {
			tasks[i] = WorkerTask{
				FilePath: fmt.Sprintf("failover_worker_%d.go", i),
				Prompt:   fmt.Sprintf("Generate failover worker %d", i),
			}
		}

		argsBytes, _ := json.Marshal(ParallelArgs{
			BaseDir:   tmpDir,
			Endpoint:  srv.URL + "/v1/chat/completions",
			ModelPool: pool,
			Tasks:     tasks,
		})
		paramsBytes, _ := json.Marshal(ToolCallParams{
			Name:      "omni_parallel_workers",
			Arguments: argsBytes,
		})

		resp := handleToolCall(RPCRequest{
			JSONRPC: "2.0",
			ID:      json.RawMessage(`11`),
			Method:  "tools/call",
			Params:  paramsBytes,
		})

		summary := extractToolResultText(t, resp)
		if strings.Count(summary, "[failover -> antigravity/gemini-3-flash]") != 2 {
			t.Fatalf("expected exactly 2 workers (indices 1 & 4) to annotate failover -> gemini-3-flash, got summary:\n%s", summary)
		}

		mu.Lock()
		defer mu.Unlock()

		// Primary attempts across the 6 workers must still be 2-2-2
		for _, m := range pool {
			if primaryAttempts[m] != 2 {
				t.Fatalf("expected initial primary attempts for %q to be 2, got %v", m, primaryAttempts)
			}
		}

		// Workers 1 and 4 hit sonnet (503) then failed over to flash (200)
		for _, failedIdx := range []int{1, 4} {
			fName := fmt.Sprintf("failover_worker_%d.go", failedIdx)
			wantSeq := []string{"antigravity/claude-sonnet-4-6", "antigravity/gemini-3-flash"}
			gotSeq := attemptsByTask[fName]
			if len(gotSeq) != 2 || gotSeq[0] != wantSeq[0] || gotSeq[1] != wantSeq[1] {
				t.Fatalf("task %s attempt sequence = %v, want %v", fName, gotSeq, wantSeq)
			}
		}

		// Ensure all 6 files were written cleanly
		for i := 0; i < 6; i++ {
			fName := fmt.Sprintf("failover_worker_%d.go", i)
			if _, err := os.ReadFile(filepath.Join(tmpDir, fName)); err != nil {
				t.Fatalf("expected %s to exist after 503 failover: %v", fName, err)
			}
		}
	})

	t.Run("Env_OMNI_MODEL_POOL_And_AllPool503_Reaches_DefaultFallbackModel", func(t *testing.T) {
		t.Setenv("OMNI_DEFAULT_MODEL", "")
		t.Setenv("OMNI_FALLBACK_MODELS", "")
		t.Setenv("OMNI_MODEL_POOL", strings.Join(pool, ", "))

		var mu sync.Mutex
		var seq []string

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var reqObj ChatRequest
			_ = json.NewDecoder(r.Body).Decode(&reqObj)

			mu.Lock()
			seq = append(seq, reqObj.Model)
			mu.Unlock()

			// Return 503 for all 3 pool models; succeed on DefaultFallbackModel (Argon)
			if reqObj.Model != DefaultFallbackModel && reqObj.Model != "antigravity/gemini-3.5-flash-lite" {
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte(`{"error":{"message":"pool model 503"}}`))
				return
			}

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(ChatResponse{
				Choices: []ChatChoice{
					{
						Message: ChatMessage{
							Role:    "assistant",
							Content: fmt.Sprintf("package envpool\n\n// Model: %s\n", reqObj.Model),
						},
					},
				},
			})
		}))
		defer srv.Close()

		tmpDir := t.TempDir()
		argsBytes, _ := json.Marshal(ParallelArgs{
			BaseDir:  tmpDir,
			Endpoint: srv.URL + "/v1/chat/completions",
			Tasks: []WorkerTask{
				{FilePath: "pool_exhausted.go", Prompt: "Falls through pool to DefaultFallbackModel"},
				{FilePath: "task_override.go", Prompt: "Uses explicit task.Model", Model: "antigravity/gemini-3.5-flash-lite"},
			},
		})
		paramsBytes, _ := json.Marshal(ToolCallParams{
			Name:      "omni_parallel_workers",
			Arguments: argsBytes,
		})

		resp := handleToolCall(RPCRequest{
			JSONRPC: "2.0",
			ID:      json.RawMessage(`12`),
			Method:  "tools/call",
			Params:  paramsBytes,
		})

		summary := extractToolResultText(t, resp)
		if !strings.Contains(summary, "[failover -> "+DefaultFallbackModel+"]") {
			t.Fatalf("expected pool exhaustion to fail over to %s, got summary:\n%s", DefaultFallbackModel, summary)
		}

		overrideBytes, err := os.ReadFile(filepath.Join(tmpDir, "task_override.go"))
		if err != nil || !strings.Contains(string(overrideBytes), "antigravity/gemini-3.5-flash-lite") {
			t.Fatalf("expected task_override.go to use explicit task.Model, got err=%v content=%q", err, string(overrideBytes))
		}
	})
}

func TestPinAccountsAndTaskPinnedAccountSetHeaderAccurately(t *testing.T) {
	t.Setenv("OMNI_MODEL_POOL", "")

	var mu sync.Mutex
	pinnedByFile := make(map[string]string)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqObj ChatRequest
		_ = json.NewDecoder(r.Body).Decode(&reqObj)
		targetFile := extractTargetFileFromMessages(reqObj.Messages)
		pinHdr := r.Header.Get("X-Omni-Pin-Account")

		mu.Lock()
		pinnedByFile[targetFile] = pinHdr
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ChatResponse{
			Choices: []ChatChoice{
				{
					Message: ChatMessage{
						Role:    "assistant",
						Content: "package pinned\n\nfunc Pinned() {}\n",
					},
				},
			},
		})
	}))
	defer srv.Close()

	tmpDir := t.TempDir()
	pinPool := []string{"acc-01@gmail.com", "acc-02@gmail.com", "acc-03@gmail.com"}
	tasks := []WorkerTask{
		{FilePath: "pin_0.go", Prompt: "Task 0"},
		{FilePath: "pin_1.go", Prompt: "Task 1"},
		{FilePath: "pin_2.go", Prompt: "Task 2"},
		{FilePath: "pin_3.go", Prompt: "Task 3"},
		{FilePath: "pin_4.go", Prompt: "Task 4 override", PinnedAccount: "dedicated-vip@gmail.com"},
		{FilePath: "pin_5.go", Prompt: "Task 5"},
	}

	argsBytes, _ := json.Marshal(ParallelArgs{
		BaseDir:     tmpDir,
		Endpoint:    srv.URL + "/v1/chat/completions",
		PinAccounts: pinPool,
		Tasks:       tasks,
	})
	paramsBytes, _ := json.Marshal(ToolCallParams{
		Name:      "omni_parallel_workers",
		Arguments: argsBytes,
	})

	resp := handleToolCall(RPCRequest{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`13`),
		Method:  "tools/call",
		Params:  paramsBytes,
	})
	_ = extractToolResultText(t, resp)

	wantPins := map[string]string{
		"pin_0.go": "acc-01@gmail.com",
		"pin_1.go": "acc-02@gmail.com",
		"pin_2.go": "acc-03@gmail.com",
		"pin_3.go": "acc-01@gmail.com",
		"pin_4.go": "dedicated-vip@gmail.com",
		"pin_5.go": "acc-03@gmail.com",
	}

	mu.Lock()
	for file, want := range wantPins {
		if got := pinnedByFile[file]; got != want {
			mu.Unlock()
			t.Fatalf("task %s sent X-Omni-Pin-Account = %q, want %q (all=%v)", file, got, want, pinnedByFile)
		}
	}
	mu.Unlock()

	// Also verify header is omitted when neither pin_accounts nor pinned_account is set
	unpinnedArgs, _ := json.Marshal(ParallelArgs{
		BaseDir:  tmpDir,
		Endpoint: srv.URL + "/v1/chat/completions",
		Tasks: []WorkerTask{
			{FilePath: "unpinned.go", Prompt: "Unpinned task"},
		},
	})
	unpinnedParams, _ := json.Marshal(ToolCallParams{
		Name:      "omni_parallel_workers",
		Arguments: unpinnedArgs,
	})
	unpinnedResp := handleToolCall(RPCRequest{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`14`),
		Method:  "tools/call",
		Params:  unpinnedParams,
	})
	_ = extractToolResultText(t, unpinnedResp)

	mu.Lock()
	gotUnpinned := pinnedByFile["unpinned.go"]
	mu.Unlock()
	if gotUnpinned != "" {
		t.Fatalf("expected empty X-Omni-Pin-Account when unpinned, got %q", gotUnpinned)
	}
}

func TestHTTPClientTransportTuningAndToolSchema(t *testing.T) {
	tr, ok := httpClient.Transport.(*http.Transport)
	if !ok || tr == nil {
		t.Fatalf("expected httpClient.Transport to be *http.Transport, got %T", httpClient.Transport)
	}
	if !tr.ForceAttemptHTTP2 {
		t.Fatalf("expected ForceAttemptHTTP2 = true")
	}
	if tr.MaxIdleConns != 512 {
		t.Fatalf("MaxIdleConns = %d, want 512", tr.MaxIdleConns)
	}
	if tr.MaxIdleConnsPerHost != 256 {
		t.Fatalf("MaxIdleConnsPerHost = %d, want 256", tr.MaxIdleConnsPerHost)
	}
	if tr.WriteBufferSize != 64*1024 {
		t.Fatalf("WriteBufferSize = %d, want %d", tr.WriteBufferSize, 64*1024)
	}
	if tr.ReadBufferSize != 64*1024 {
		t.Fatalf("ReadBufferSize = %d, want %d", tr.ReadBufferSize, 64*1024)
	}

	def := toolDefinition()
	schema, _ := def["inputSchema"].(map[string]any)
	props, _ := schema["properties"].(map[string]any)
	if _, ok := props["model_pool"]; !ok {
		t.Fatalf("toolDefinition inputSchema missing model_pool")
	}
	if _, ok := props["pin_accounts"]; !ok {
		t.Fatalf("toolDefinition inputSchema missing pin_accounts")
	}
	tasksProp, _ := props["tasks"].(map[string]any)
	itemsProp, _ := tasksProp["items"].(map[string]any)
	taskItemProps, _ := itemsProp["properties"].(map[string]any)
	if _, ok := taskItemProps["pinned_account"]; !ok {
		t.Fatalf("toolDefinition tasks.items.properties missing pinned_account")
	}
}
