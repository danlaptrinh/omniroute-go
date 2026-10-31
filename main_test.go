package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestOmniParallelWorkers100Goroutines(t *testing.T) {
	var activeRequests int32
	var maxConcurrent int32
	var totalCalls int32

	// Mock OmniRoute /v1/chat/completions server
	mockOmniRoute := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cur := atomic.AddInt32(&activeRequests, 1)
		for {
			prevMax := atomic.LoadInt32(&maxConcurrent)
			if cur <= prevMax || atomic.CompareAndSwapInt32(&maxConcurrent, prevMax, cur) {
				break
			}
		}
		defer atomic.AddInt32(&activeRequests, -1)
		atomic.AddInt32(&totalCalls, 1)

		// Simulate upstream LLM generation latency (60ms)
		time.Sleep(60 * time.Millisecond)

		w.Header().Set("Content-Type", "application/json")
		resp := ChatResponse{
			Choices: []ChatChoice{
				{
					Message: ChatMessage{
						Role:    "assistant",
						Content: "```go\npackage workers\n\nfunc RunWorker() string {\n\treturn \"ok\"\n}\n```",
					},
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer mockOmniRoute.Close()

	tmpDir := t.TempDir()
	contractsFile := filepath.Join(tmpDir, "contracts.go")
	if err := os.WriteFile(contractsFile, []byte("package workers\n\ntype Worker interface{ Run() }\n"), 0644); err != nil {
		t.Fatalf("failed to write contracts.go: %v", err)
	}

	const numTasks = 100
	tasks := make([]WorkerTask, numTasks)
	for i := 0; i < numTasks; i++ {
		tasks[i] = WorkerTask{
			FilePath: fmt.Sprintf("pkg/worker_%03d.go", i+1),
			Prompt:   fmt.Sprintf("Implement worker %d adhering to contracts.go", i+1),
		}
	}

	argsBytes, _ := json.Marshal(ParallelArgs{
		BaseDir:      tmpDir,
		ContextFiles: []string{"contracts.go"},
		Endpoint:     mockOmniRoute.URL + "/v1/chat/completions",
		Model:        "omni-fast-coder",
		Tasks:        tasks,
	})

	paramsBytes, _ := json.Marshal(ToolCallParams{
		Name:      "omni_parallel_workers",
		Arguments: argsBytes,
	})

	req := RPCRequest{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "tools/call",
		Params:  paramsBytes,
	}

	start := time.Now()
	resp := handleToolCall(req)
	elapsed := time.Since(start)

	if resp.Error != nil {
		t.Fatalf("unexpected RPC error: %+v", resp.Error)
	}

	if got := atomic.LoadInt32(&totalCalls); got != numTasks {
		t.Fatalf("expected %d calls to OmniRoute, got %d", numTasks, got)
	}

	t.Logf("100 parallel tasks completed in %s (peak concurrency: %d)", elapsed, atomic.LoadInt32(&maxConcurrent))

	// Verify all 100 files were written cleanly without markdown fences
	for i := 0; i < numTasks; i++ {
		fp := filepath.Join(tmpDir, fmt.Sprintf("pkg/worker_%03d.go", i+1))
		data, err := os.ReadFile(fp)
		if err != nil {
			t.Fatalf("missing generated file %s: %v", fp, err)
		}
		content := string(data)
		if strings.Contains(content, "```") {
			t.Fatalf("file %s still contains markdown fences: %s", fp, content)
		}
		if !strings.Contains(content, "package workers") {
			t.Fatalf("unexpected content in %s: %s", fp, content)
		}
	}
}

