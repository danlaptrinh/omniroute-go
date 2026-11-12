package main

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"omni-parallel-mcp/gateway"
)

// TestHelperMCPProcess acts as a real external MCP JSON-RPC 2.0 stdio child binary
// when GO_WANT_HELPER_MCP_PROCESS=1 is set in the child environment.
func TestHelperMCPProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_MCP_PROCESS") != "1" {
		return
	}

	httpClient := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
		Timeout: 15 * time.Second,
	}

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	var outMu sync.Mutex
	writeJSONLine := func(v any) {
		raw, _ := json.Marshal(v)
		outMu.Lock()
		_, _ = os.Stdout.Write(append(raw, '\n'))
		outMu.Unlock()
	}

	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var req struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      json.RawMessage `json:"id"`
			Method  string          `json:"method"`
			Params  json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(line, &req); err != nil {
			continue
		}

		switch req.Method {
		case "initialize":
			writeJSONLine(map[string]any{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"result": map[string]any{
					"protocolVersion": "2024-11-05",
					"capabilities": map[string]any{
						"tools": map[string]any{},
					},
					"serverInfo": map[string]any{
						"name":    "omni-parallel-mcp-test",
						"version": "2.0.0-test",
					},
				},
			})

		case "notifications/initialized":
			// No-op notification

		case "tools/call":
			var callParams struct {
				Name      string `json:"name"`
				Arguments struct {
					BaseDir   string   `json:"base_dir"`
					Endpoint  string   `json:"endpoint"`
					APIKey    string   `json:"api_key"`
					ModelPool []string `json:"model_pool"`
					Tasks     []struct {
						FilePath string `json:"file_path"`
						Prompt   string `json:"prompt"`
					} `json:"tasks"`
				} `json:"arguments"`
			}
			if err := json.Unmarshal(req.Params, &callParams); err != nil {
				writeJSONLine(map[string]any{
					"jsonrpc": "2.0",
					"id":      req.ID,
					"error":   map[string]any{"code": -32602, "message": err.Error()},
				})
				continue
			}

			var wg sync.WaitGroup
			errCh := make(chan error, len(callParams.Arguments.Tasks))
			for idx, task := range callParams.Arguments.Tasks {
				wg.Add(1)
				go func(i int, filePath, prompt string) {
					defer wg.Done()
					model := "antigravity/gemini-4-carbon-high"
					if len(callParams.Arguments.ModelPool) > 0 {
						model = callParams.Arguments.ModelPool[i%len(callParams.Arguments.ModelPool)]
					}
					chatBody, _ := json.Marshal(map[string]any{
						"model":      model,
						"stream":     false,
						"max_tokens": 96,
						"messages": []map[string]string{
							{"role": "user", "content": prompt},
						},
					})
					hReq, err := http.NewRequest(http.MethodPost, callParams.Arguments.Endpoint, bytes.NewReader(chatBody))
					if err != nil {
						errCh <- err
						return
					}
					hReq.Header.Set("Content-Type", "application/json")
					if callParams.Arguments.APIKey != "" {
						hReq.Header.Set("Authorization", "Bearer "+callParams.Arguments.APIKey)
					}
					hResp, err := httpClient.Do(hReq)
					if err != nil {
						errCh <- err
						return
					}
					defer hResp.Body.Close()

					var parsed struct {
						Choices []struct {
							Message struct {
								Content string `json:"content"`
							} `json:"message"`
						} `json:"choices"`
					}
					_ = json.NewDecoder(hResp.Body).Decode(&parsed)
					code := fmt.Sprintf("package benchgen\n\nfunc Worker%d() int { return %d }\n", i+1, i+1)
					if len(parsed.Choices) > 0 && strings.TrimSpace(parsed.Choices[0].Message.Content) != "" {
						code = parsed.Choices[0].Message.Content
					}
					dest := filepath.Join(callParams.Arguments.BaseDir, filePath)
					if mkErr := os.MkdirAll(filepath.Dir(dest), 0755); mkErr != nil {
						errCh <- mkErr
						return
					}
					if wErr := os.WriteFile(dest, []byte(code), 0644); wErr != nil {
						errCh <- wErr
					}
				}(idx, task.FilePath, task.Prompt)
			}
			wg.Wait()
			close(errCh)

			var firstErr error
			for e := range errCh {
				if e != nil && firstErr == nil {
					firstErr = e
				}
			}
			isErr := firstErr != nil
			summary := fmt.Sprintf("generated %d files in parallel", len(callParams.Arguments.Tasks))
			if firstErr != nil {
				summary = firstErr.Error()
			}
			writeJSONLine(map[string]any{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"result": map[string]any{
					"isError": isErr,
					"content": []map[string]string{
						{"type": "text", "text": summary},
					},
				},
			})
		}
	}
	os.Exit(0)
}

func TestParseBenchFlagsAndPureHelpers(t *testing.T) {
	t.Setenv("OMNI_GATEWAY_URL", "https://omni.bucloud.vip/v1/chat/completions/")
	t.Setenv("OMNI_API_KEY", "sk-omni-secret-abcdef123456")

	cfg, err := parseBenchFlags([]string{"-mode", "api", "-concurrency", "1,9,18,36", "-max-tokens", "96"})
	if err != nil {
		t.Fatalf("parseBenchFlags failed: %v", err)
	}
	if cfg.BaseURL != "https://omni.bucloud.vip" {
		t.Errorf("expected normalized BaseURL https://omni.bucloud.vip, got %q", cfg.BaseURL)
	}
	if cfg.MaskedKey == cfg.APIKey || !strings.Contains(cfg.MaskedKey, "...") {
		t.Errorf("expected masked secret key, got %q", cfg.MaskedKey)
	}
	if len(cfg.Concurrency) != 4 || cfg.Concurrency[3] != 36 {
		t.Errorf("unexpected concurrency slice: %v", cfg.Concurrency)
	}

	if _, err := parseBenchFlags([]string{"-mode", "invalid-mode"}); err == nil {
		t.Errorf("expected error for unsupported -mode")
	}
	if _, err := parseBenchFlags([]string{"-concurrency", "1,0,-5"}); err == nil {
		t.Errorf("expected error for non-positive -concurrency")
	}

	m := computeMetrics([]float64{10, 20, 30, 40, 100})
	if m.Min != 10 || m.P50 != 30 || m.Max != 100 || m.Avg != 40 || m.P95 < 80 {
		t.Errorf("unexpected MetricSummary: %+v", m)
	}
	if empty := computeMetrics(nil); empty.Max != 0 || empty.P50 != 0 {
		t.Errorf("expected zero MetricSummary on empty input, got %+v", empty)
	}

	truthCases := []struct {
		name      string
		tier      string
		pinned    string
		actual    string
		reqModel  string
		resModel  string
		upVer     string
		status    int
		wantClass string
		wantFlag  bool
	}{
		{
			name:      "exact_match_flash",
			tier:      "Pro",
			pinned:    "a@gmail.com",
			actual:    "a@gmail.com",
			reqModel:  "antigravity/gemini-3-flash",
			resModel:  "gemini-3-flash",
			upVer:     "gemini-3-flash-001",
			status:    200,
			wantClass: "EXACT_MATCH",
			wantFlag:  false,
		},
		{
			name:      "virtual_ide_alias_carbon_to_38_flash_high",
			tier:      "Ultra",
			pinned:    "ntky152022@gmail.com",
			actual:    "ntky152022@gmail.com",
			reqModel:  "antigravity/gemini-4-carbon-high",
			resModel:  "gemini-3.8-flash-high",
			upVer:     "gemini-3.8-flash-high",
			status:    200,
			wantClass: "VIRTUAL_ALIAS_AGENT",
			wantFlag:  false,
		},
		{
			name:      "cross_family_fallback_gemini_to_claude",
			tier:      "Ultra",
			pinned:    "ntky152022@gmail.com",
			actual:    "ntky152022@gmail.com",
			reqModel:  "antigravity/gemini-4-carbon-high",
			resModel:  "antigravity/claude-opus-4-6-thinking",
			upVer:     "claude-opus-4-6-thinking",
			status:    200,
			wantClass: "CROSS_FAMILY_FALLBACK",
			wantFlag:  true,
		},
		{
			name:      "silent_downgrade_ultra_flagship_to_flash",
			tier:      "Ultra",
			pinned:    "ntky152022@gmail.com",
			actual:    "ntky152022@gmail.com",
			reqModel:  "antigravity/gemini-4-carbon-high",
			resModel:  "antigravity/gemini-4-carbon-high ➔ gemini-3-flash",
			upVer:     "gemini-3-flash",
			status:    200,
			wantClass: "SILENT_DOWNGRADE_FLASH (Ultra)",
			wantFlag:  true,
		},
		{
			name:      "combo_best_coding_downgrade_flagged",
			tier:      "Pro",
			reqModel:  "auto/best-coding",
			resModel:  "gemini-3-flash",
			upVer:     "gemini-3-flash",
			status:    200,
			wantClass: "SILENT_DOWNGRADE_FLASH",
			wantFlag:  true,
		},
		{
			name:      "pin_bypassed_flagged",
			tier:      "Ultra",
			pinned:    "ntky152022@gmail.com",
			actual:    "other@gmail.com",
			reqModel:  "antigravity/gemini-3-flash",
			resModel:  "gemini-3-flash",
			status:    200,
			wantClass: "PIN_BYPASSED",
			wantFlag:  true,
		},
		{
			name:      "http_429_flagged",
			tier:      "Pro",
			reqModel:  "antigravity/gemini-3-flash",
			status:    429,
			wantClass: "HTTP_ERROR_429",
			wantFlag:  true,
		},
	}

	for _, tc := range truthCases {
		gotClass, gotFlag := classifyModelTruth(tc.tier, tc.pinned, tc.actual, tc.reqModel, tc.resModel, tc.upVer, tc.status)
		if gotClass != tc.wantClass || gotFlag != tc.wantFlag {
			t.Errorf("[%s] classifyModelTruth = (%q, %v), want (%q, %v)",
				tc.name, gotClass, gotFlag, tc.wantClass, tc.wantFlag)
		}
	}
}

func TestAllFourAnglesEndToEndTLSAndJSONReport(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_MCP_PROCESS", "1")

	var (
		logsMu       sync.Mutex
		recordedLogs []gatewayLogItem
		rrSeq        int
	)

	accounts := []quotaAccountItem{
		{ID: "acc-ultra-1", Provider: "antigravity", Email: "ntky152022@gmail.com", Tier: "Ultra", Enabled: true, Status: "healthy"},
		{ID: "acc-pro-1", Provider: "antigravity", Email: "advietnam789@gmail.com", Tier: "Pro", Enabled: true, Status: "healthy"},
		{ID: "acc-disabled", Provider: "antigravity", Email: "disabled@gmail.com", Tier: "Pro", Enabled: false, Status: "warning"},
	}

	tlsSrv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer sk-admin-bench-key-9999" {
			http.Error(w, `{"error":{"message":"unauthorized"}}`, http.StatusUnauthorized)
			return
		}

		switch r.URL.Path {
		case "/v1/quota":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"accounts": accounts})

		case "/v1/chat/completions":
			bodyBytes, _ := io.ReadAll(r.Body)
			var chatReq struct {
				Model    string `json:"model"`
				Stream   bool   `json:"stream"`
				Messages []struct {
					Content string `json:"content"`
				} `json:"messages"`
			}
			_ = json.Unmarshal(bodyBytes, &chatReq)
			promptText := ""
			if len(chatReq.Messages) > 0 {
				promptText = chatReq.Messages[0].Content
			}

			pinned := strings.TrimSpace(r.Header.Get("X-Omni-Pin-Account"))
			chosenAcc := pinned
			logsMu.Lock()
			if chosenAcc == "" {
				if rrSeq%2 == 0 {
					chosenAcc = "ntky152022@gmail.com"
				} else {
					chosenAcc = "advietnam789@gmail.com"
				}
				rrSeq++
			}
			logsMu.Unlock()

			resolved := "gemini-3-flash"
			upstreamVer := "gemini-3-flash-001"
			if strings.Contains(chatReq.Model, "carbon") {
				if strings.EqualFold(chosenAcc, "advietnam789@gmail.com") {
					// Simulate silent Pro fallback to Flash so Angle 2 Model Truth Matrix flags it!
					resolved = "gemini-3-flash"
					upstreamVer = "gemini-3-flash-001"
				} else {
					resolved = "gemini-3.8-flash-high"
					upstreamVer = "gemini-3.8-flash-high"
				}
			}

			logsMu.Lock()
			recordedLogs = append(recordedLogs, gatewayLogItem{
				ID:             fmt.Sprintf("log-%d", len(recordedLogs)+1),
				Timestamp:      time.Now().UTC().Format(time.RFC3339),
				Provider:       "antigravity",
				Account:        chosenAcc,
				RequestedModel: chatReq.Model,
				ResolvedModel:  resolved,
				UpstreamModel:  upstreamVer,
				PromptText:     promptText,
				StatusCode:     http.StatusOK,
				LatencyMs:      6,
				TotalTokens:    42,
			})
			logsMu.Unlock()

			w.Header().Set("X-Omni-Gateway-Account", chosenAcc)
			w.Header().Set("X-Omni-Gateway-Requested-Model", chatReq.Model)
			w.Header().Set("X-Omni-Gateway-Resolved-Model", resolved)
			w.Header().Set("X-Omni-Gateway-Upstream-Model-Version", upstreamVer)
			w.Header().Set("X-Omni-Gateway-Cache", "MISS")

			if chatReq.Stream {
				// SSE responses MUST NOT set Content-Encoding: gzip
				w.Header().Set("Content-Type", "text/event-stream")
				flusher, _ := w.(http.Flusher)
				_, _ = fmt.Fprintf(w, "data: {\"model\":%q,\"choices\":[{\"delta\":{\"content\":\"package benchgen\\n\"}}]}\n\n", upstreamVer)
				if flusher != nil {
					flusher.Flush()
				}
				time.Sleep(5 * time.Millisecond)
				_, _ = fmt.Fprintf(w, "data: {\"model\":%q,\"choices\":[{\"delta\":{\"content\":\"func ClampInt(v, lo, hi int) int { return v }\\n\"}}]}\n\n", upstreamVer)
				_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
				if flusher != nil {
					flusher.Flush()
				}
				return
			}

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"model": upstreamVer,
				"choices": []map[string]any{
					{
						"message": map[string]string{
							"role":    "assistant",
							"content": "package benchgen\n\nfunc GeneratedOK() int { return 42 }\n",
						},
					},
				},
			})

		case "/v1/logs":
			logsMu.Lock()
			snapshot := make([]gatewayLogItem, len(recordedLogs))
			copy(snapshot, recordedLogs)
			logsMu.Unlock()

			rawJSON, _ := json.Marshal(map[string]any{"data": snapshot})
			if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Content-Encoding", "gzip")
				gz := gzip.NewWriter(w)
				_, _ = gz.Write(rawJSON)
				_ = gz.Close()
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(rawJSON)

		default:
			http.NotFound(w, r)
		}
	}))
	defer tlsSrv.Close()

	exePath, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable failed: %v", err)
	}
	jsonOutPath := filepath.Join(t.TempDir(), "nested", "omni_bench_report.json")

	cfg, err := parseBenchFlags([]string{
		"-url", tlsSrv.URL,
		"-key", "sk-admin-bench-key-9999",
		"-mode", "all",
		"-models", "antigravity/gemini-4-carbon-high,antigravity/gemini-3-flash",
		"-concurrency", "1,4",
		"-max-tokens", "64",
		"-mcp-bin", exePath,
		"-mcp-args", "-test.run=^TestHelperMCPProcess$",
		"-mcp-tasks", "3",
		"-correlate-logs=true",
		"-insecure=true",
		"-json-out", jsonOutPath,
	})
	if err != nil {
		t.Fatalf("parseBenchFlags failed: %v", err)
	}
	// When ModelsExplicit is true, "all" mode audits the explicit models list for fast deterministic execution.
	var outBuf bytes.Buffer
	report, runErr := executeBenchSuite(context.Background(), cfg, &outBuf)
	if runErr != nil {
		t.Fatalf("executeBenchSuite failed: %v\nOutput:\n%s", runErr, outBuf.String())
	}

	// Verify Angle 1: API Concurrency + HTTP Trace + No SSE Gzip
	if len(report.APIBenchmarks) != 2 {
		t.Fatalf("expected 2 API concurrency steps, got %d", len(report.APIBenchmarks))
	}
	for _, step := range report.APIBenchmarks {
		if step.SuccessCount != step.Concurrency || step.ErrorCount != 0 {
			t.Errorf("step conc=%d expected %d successes and 0 errors, got ok=%d err=%d",
				step.Concurrency, step.Concurrency, step.SuccessCount, step.ErrorCount)
		}
		if step.TTFTStats.P50 <= 0 || step.TotalWallStats.P95 < step.TTFTStats.P50 {
			t.Errorf("step conc=%d invalid TTFT/Total metrics: ttft=%+v total=%+v",
				step.Concurrency, step.TTFTStats, step.TotalWallStats)
		}
		if step.ContentEncodingDist["identity"] != step.Concurrency {
			t.Errorf("expected SSE responses to have identity encoding (no gzip), got %v", step.ContentEncodingDist)
		}
	}

	// Verify Angle 2: Model Truth Matrix Across Accounts
	if report.ModelAudit == nil {
		t.Fatalf("expected ModelAudit report")
	}
	if report.ModelAudit.AccountsAudited != 2 || report.ModelAudit.TotalChecks != 4 {
		t.Errorf("expected 2 enabled accounts * 2 models = 4 checks, got accounts=%d checks=%d",
			report.ModelAudit.AccountsAudited, report.ModelAudit.TotalChecks)
	}
	if report.ModelAudit.SilentDowngrades != 1 {
		t.Errorf("expected 1 flagged SILENT_DOWNGRADE_FLASH on Pro account, got %d", report.ModelAudit.SilentDowngrades)
	}
	if report.ModelAudit.AliasOrComboRouted != 1 || report.ModelAudit.ExactMatches != 2 {
		t.Errorf("expected 1 VIRTUAL_ALIAS_AGENT and 2 EXACT_MATCH, got alias=%d exact=%d",
			report.ModelAudit.AliasOrComboRouted, report.ModelAudit.ExactMatches)
	}

	// Verify Angle 3: Real External MCP Stdio JSON-RPC 2.0 Client
	if report.MCPBenchmark == nil || len(report.MCPBenchmark.Batches) != 1 {
		t.Fatalf("expected 1 MCP batch report, got %+v", report.MCPBenchmark)
	}
	mcpBatch := report.MCPBenchmark.Batches[0]
	if mcpBatch.VerifiedFiles != 3 || mcpBatch.RequestedTasks != 3 || mcpBatch.IsError {
		t.Errorf("expected 3/3 verified MCP files without error, got %+v", mcpBatch)
	}
	if mcpBatch.ServerName != "omni-parallel-mcp-test" || mcpBatch.TasksPerSec <= 0 {
		t.Errorf("unexpected MCP server metadata/throughput: %+v", mcpBatch)
	}

	// Verify Angle 4: Inside-vs-Outside Log Correlation (including gzipped /v1/logs)
	if report.LogCorrelation == nil || !report.LogCorrelation.Enabled {
		t.Fatalf("expected enabled LogCorrelation report")
	}
	// Total outside requests: API (1 + 4 = 5) + Audit (4) = 9 HTTP + 3 MCP
	if report.LogCorrelation.MatchedHTTPCount != 9 || report.LogCorrelation.MatchedMCPCount != 3 {
		t.Errorf("expected MatchedHTTPCount=9 and MatchedMCPCount=3, got http=%d mcp=%d",
			report.LogCorrelation.MatchedHTTPCount, report.LogCorrelation.MatchedMCPCount)
	}

	// Verify JSON report persistence and secret redaction
	if err := writeJSONReport(jsonOutPath, report); err != nil {
		t.Fatalf("writeJSONReport failed: %v", err)
	}
	rawReportBytes, err := os.ReadFile(jsonOutPath)
	if err != nil {
		t.Fatalf("failed reading saved JSON report: %v", err)
	}
	if strings.Contains(string(rawReportBytes), "sk-admin-bench-key-9999") {
		t.Errorf("JSON report leaked raw API key!")
	}
}

func TestLogCorrelationGracefulSkipOnUnauthorized401(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/chat/completions":
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("X-Omni-Gateway-Account", "worker@gmail.com")
			w.Header().Set("X-Omni-Gateway-Resolved-Model", "gemini-3-flash")
			_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"OK\"}}]}\n\ndata: [DONE]\n\n")
		case "/v1/logs":
			http.Error(w, `{"error":{"message":"admin authentication required"}}`, http.StatusUnauthorized)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cfg, err := parseBenchFlags([]string{
		"-url", srv.URL,
		"-key", "sk-non-admin-worker-key",
		"-mode", "api",
		"-models", "antigravity/gemini-3-flash",
		"-concurrency", "2",
		"-correlate-logs=true",
	})
	if err != nil {
		t.Fatalf("parseBenchFlags failed: %v", err)
	}

	report, runErr := executeBenchSuite(context.Background(), cfg, io.Discard)
	if runErr != nil {
		t.Fatalf("expected graceful skip on 401 /v1/logs without error, got: %v", runErr)
	}
	if report.LogCorrelation == nil || !strings.Contains(report.LogCorrelation.SkippedReason, "401") {
		t.Fatalf("expected SkippedReason mentioning HTTP 401, got %+v", report.LogCorrelation)
	}
}

func TestGatewayHonestTierQuotaBucketsAndUITruth(t *testing.T) {
	tmpCfg := filepath.Join(t.TempDir(), "gateway_config.json")
	t.Setenv("GATEWAY_CONFIG_FILE", tmpCfg)

	// 1. Verify backward-compatible UpstreamModel on GatewayLogEntry
	withUpstream := gateway.GatewayLogEntry{
		ID:             "1",
		RequestedModel: "antigravity/gemini-4-carbon-high",
		ResolvedModel:  "gemini-3.8-flash-high",
		UpstreamModel:  "gemini-3.8-flash-high-001",
	}
	rawWith, _ := json.Marshal(withUpstream)
	if !strings.Contains(string(rawWith), `"upstream_model":"gemini-3.8-flash-high-001"`) {
		t.Errorf("expected upstream_model in marshaled GatewayLogEntry, got %s", string(rawWith))
	}
	withoutUpstream := gateway.GatewayLogEntry{ID: "2", RequestedModel: "antigravity/gemini-3-flash"}
	rawWithout, _ := json.Marshal(withoutUpstream)
	if strings.Contains(string(rawWithout), "upstream_model") {
		t.Errorf("expected omitempty on empty UpstreamModel, got %s", string(rawWithout))
	}

	// 2. Mock CCPA loadCodeAssist + quotaSummary + fetchAvailableModels + retrieveQuota
	futureReset := time.Now().UTC().Add(3 * time.Hour).Format(time.RFC3339)
	mockCCPA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHdr := r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/v1internal:loadCodeAssist":
			switch authHdr {
			case "Bearer tok-ntky-ultra":
				// Real production case: currentTier=standard-tier, paidTier=g1-ultra-tier
				_ = json.NewEncoder(w).Encode(map[string]any{
					"currentTier":             map[string]any{"id": "standard-tier", "name": "Standard"},
					"paidTier":                map[string]any{"id": "g1-ultra-tier", "name": "Google AI Ultra"},
					"cloudaicompanionProject": "proj-ultra-ntky",
				})
			case "Bearer tok-tos-banned":
				// Real production bug (#27): loadCodeAssist returns 200 OK even though streamGenerateContent gave 403 TOS_VIOLATION (24h ban)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"currentTier":             map[string]any{"id": "standard-tier", "name": "Standard"},
					"paidTier":                map[string]any{"id": "g1-pro-tier", "name": "Google AI Pro"},
					"cloudaicompanionProject": "proj-banned",
				})
			default:
				_ = json.NewEncoder(w).Encode(map[string]any{
					"currentTier":             map[string]any{"id": "standard-tier", "name": "Standard"},
					"paidTier":                map[string]any{"id": "g1-pro-tier", "name": "Google AI Pro"},
					"cloudaicompanionProject": "proj-pro-1",
				})
			}

		case "/v1internal:retrieveUserQuotaSummary":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"groups": []map[string]any{
					{
						"displayName": "Gemini Models",
						"buckets": []map[string]any{
							{"bucketId": "gemini-weekly", "window": "weekly", "remainingFraction": 0.91, "resetTime": futureReset},
						},
					},
				},
			})

		case "/v1internal:fetchAvailableModels":
			// Map format returned by real CCPA fetchAvailableModels; proto3 omits remainingFraction when 0.0!
			_ = json.NewEncoder(w).Encode(map[string]any{
				"models": map[string]any{
					"claude-opus-4-6-thinking": map[string]any{
						"displayName": "Claude Opus 4.6 Thinking",
						"quotaInfo":   map[string]any{"resetTime": futureReset},
					},
					"gemini-3.8-flash-high": map[string]any{
						"displayName": "Gemini 3.8 Flash High",
						"quotaInfo":   map[string]any{"remainingFraction": 0.88, "resetTime": futureReset},
					},
					"gemini-3-flash": map[string]any{
						"displayName": "Gemini 3 Flash",
						"quotaInfo":   map[string]any{"remainingFraction": 1.0, "resetTime": futureReset},
					},
				},
			})

		case "/v1internal:retrieveUserQuota":
			// Proto3 omits remainingFraction on exhausted claude-opus-4-6-thinking while gemini-3-flash is 1.0 (100%)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"buckets": []map[string]any{
					{"modelId": "claude-opus-4-6-thinking", "resetTime": futureReset},
					{"modelId": "gemini-3.8-flash-high", "remainingFraction": 0.88, "resetTime": futureReset},
					{"modelId": "gemini-3-flash", "remainingFraction": 1.0, "resetTime": futureReset},
				},
			})

		default:
			http.NotFound(w, r)
		}
	}))
	defer mockCCPA.Close()

	origLoad := gateway.CCPALoadCodeAssistEndpoint
	origSum := gateway.CCPARetrieveUserQuotaSummaryEndpoint
	origQuota := gateway.CCPARetrieveUserQuotaEndpoint
	origFetchModels := gateway.CCPAFetchAvailableModelsEndpoint
	defer func() {
		gateway.CCPALoadCodeAssistEndpoint = origLoad
		gateway.CCPARetrieveUserQuotaSummaryEndpoint = origSum
		gateway.CCPARetrieveUserQuotaEndpoint = origQuota
		gateway.CCPAFetchAvailableModelsEndpoint = origFetchModels
	}()

	gateway.CCPALoadCodeAssistEndpoint = mockCCPA.URL + "/v1internal:loadCodeAssist"
	gateway.CCPARetrieveUserQuotaSummaryEndpoint = mockCCPA.URL + "/v1internal:retrieveUserQuotaSummary"
	gateway.CCPARetrieveUserQuotaEndpoint = mockCCPA.URL + "/v1internal:retrieveUserQuota"
	gateway.CCPAFetchAvailableModelsEndpoint = "" // Derived automatically from CCPARetrieveUserQuotaEndpoint

	tosBanUntil := time.Now().Unix() + 86400
	_, _ = gateway.UpsertConnectedAccount(gateway.ConnectedAccount{
		ID:          "ntky-1",
		Provider:    gateway.ProviderAntigravity,
		Email:       "ntky152022@gmail.com",
		AccessToken: "tok-ntky-ultra",
		ExpiresAt:   time.Now().Unix() + 3600,
	})
	_, _ = gateway.UpsertConnectedAccount(gateway.ConnectedAccount{
		ID:          "pro-1",
		Provider:    gateway.ProviderAntigravity,
		Email:       "advietnam789@gmail.com",
		AccessToken: "tok-pro-1",
		ExpiresAt:   time.Now().Unix() + 3600,
	})
	_, _ = gateway.UpsertConnectedAccount(gateway.ConnectedAccount{
		ID:            "tos-1",
		Provider:      gateway.ProviderAntigravity,
		Email:         "cloudflarepro789@gmail.com",
		AccessToken:   "tok-tos-banned",
		ExpiresAt:     time.Now().Unix() + 3600,
		CooldownUntil: tosBanUntil,
		FailureCount:  3,
	})

	summary := gateway.GetQuotaSummary(context.Background(), true)
	if summary == nil || len(summary.Accounts) != 3 {
		t.Fatalf("expected 3 accounts in QuotaSummary, got %+v", summary)
	}

	byEmail := make(map[string]gateway.AccountQuotaDetail)
	for _, a := range summary.Accounts {
		byEmail[a.Email] = a
	}

	ultraAcc := byEmail["ntky152022@gmail.com"]
	if ultraAcc.Tier != "Ultra" {
		t.Errorf("expected ntky152022@gmail.com live tier Ultra (from paidTier=g1-ultra-tier), got %q", ultraAcc.Tier)
	}
	// Verify cached tier lookup also returns Ultra without any email-substring hardcoding
	if cachedTier := gateway.LookupCachedAccountTier(gateway.ConnectedAccount{Email: "ntky152022@gmail.com"}, nil); cachedTier != "Ultra" {
		t.Errorf("expected LookupCachedAccountTier=Ultra for ntky152022@gmail.com, got %q", cachedTier)
	}

	// Verify proto3 0% Claude Opus bucket was NOT masked by 100% Gemini 3 Flash
	foundOpusZero := false
	for _, mq := range ultraAcc.ModelQuotas {
		if mq.ModelID == "claude-opus-4-6-thinking" {
			if mq.RemainingPercent != 0 || mq.Status != "critical" {
				t.Errorf("expected claude-opus-4-6-thinking RemainingPercent=0 (critical), got %+v", mq)
			}
			foundOpusZero = true
		}
	}
	if !foundOpusZero {
		t.Errorf("expected claude-opus-4-6-thinking quota bucket in %+v", ultraAcc.ModelQuotas)
	}

	if proAcc := byEmail["advietnam789@gmail.com"]; proAcc.Tier != "Pro" {
		t.Errorf("expected advietnam789@gmail.com tier Pro, got %q", proAcc.Tier)
	}

	// Verify 24h TOS_VIOLATION ban on cloudflarepro789@gmail.com was preserved when loadCodeAssist returned 200 OK
	bannedAcc := byEmail["cloudflarepro789@gmail.com"]
	if !bannedAcc.InCooldown {
		t.Errorf("expected 24h TOS_VIOLATION account cloudflarepro789@gmail.com to remain InCooldown=true!")
	}
	savedAuth := gateway.LoadSavedAuthConfig()
	for _, saved := range savedAuth.Accounts {
		if saved.Email == "cloudflarepro789@gmail.com" && saved.CooldownUntil < time.Now().Unix()+80000 {
			t.Errorf("24h TOS_VIOLATION CooldownUntil was wiped! got %d, want ~%d", saved.CooldownUntil, tosBanUntil)
		}
	}

	// 3. Verify /ui HTML exposes honest model-truth helpers & "thực chạy:" indicator
	gwSrv := gateway.NewServer(gateway.GatewayConfig{}, gateway.NewResolver(), nil)
	uiReq := httptest.NewRequest(http.MethodGet, "/ui", nil)
	uiRec := httptest.NewRecorder()
	gwSrv.Handler().ServeHTTP(uiRec, uiReq)
	if uiRec.Code != http.StatusOK {
		t.Fatalf("GET /ui returned status %d", uiRec.Code)
	}
	uiHTML := uiRec.Body.String()
	for _, needle := range []string{
		"function formatModelRoleBadge(",
		"function formatLogModelTruth(",
		"latestAccountLogByEmail",
		"➔ thực chạy:",
	} {
		if !strings.Contains(uiHTML, needle) {
			t.Errorf("expected /ui HTML to contain %q", needle)
		}
	}
}
