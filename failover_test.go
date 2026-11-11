package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
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
