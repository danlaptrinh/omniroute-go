package main

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptrace"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	defaultGatewayURL = "https://omni.bucloud.vip"
	defaultMCPBinary  = "C:/Users/Tri Thien/.gemini/antigravity/bin/omni-mcp.exe"
)

var (
	defaultAPIModels = []string{
		"antigravity/gemini-3-flash",
		"antigravity/gemini-3.5-flash-lite",
		"antigravity/gemini-3.8-flash-high",
		"antigravity/gemini-4-carbon-high",
	}
	defaultAuditModels = []string{
		"antigravity/gemini-4-carbon-high",
		"antigravity/gemini-4-carbon-low",
		"antigravity/gemini-4-argon-high",
		"antigravity/gemini-3.1-pro-high",
		"antigravity/gemini-3.8-flash-high",
		"antigravity/gemini-3-flash",
		"antigravity/gemini-3.5-flash-lite",
		"antigravity/claude-opus-4-6-thinking",
		"antigravity/claude-sonnet-4-6",
		"auto/fast",
		"auto/best-coding",
	}
)

// BenchConfig holds CLI configuration for omni-bench.
type BenchConfig struct {
	BaseURL         string        `json:"base_url"`
	APIKey          string        `json:"-"`
	MaskedKey       string        `json:"masked_key"`
	Mode            string        `json:"mode"` // api, mcp, model-audit, all
	Models          []string      `json:"models"`
	ModelsExplicit  bool          `json:"models_explicit"`
	Accounts        []string      `json:"accounts,omitempty"`
	Concurrency     []int         `json:"concurrency"`
	ConcExplicit    bool          `json:"concurrency_explicit"`
	MaxTokens       int           `json:"max_tokens"`
	Stream          bool          `json:"stream"`
	Timeout         time.Duration `json:"timeout"`
	Prompt          string        `json:"prompt,omitempty"`
	MCPBin          string        `json:"mcp_bin"`
	MCPArgs         []string      `json:"mcp_args,omitempty"`
	MCPTasks        int           `json:"mcp_tasks,omitempty"`
	CorrelateLogs   bool          `json:"correlate_logs"`
	AllowCache      bool          `json:"allow_cache"`
	InsecureTLS     bool          `json:"insecure_tls,omitempty"`
	JSONOut         string        `json:"json_out,omitempty"`
}

// APIRequestSample captures detailed client-side HTTP trace + headers for a single /v1/chat/completions call.
type APIRequestSample struct {
	TraceTag             string    `json:"trace_tag"`
	SourceAngle          string    `json:"source_angle"` // "api" or "model-audit"
	StartedAt            time.Time `json:"started_at"`
	PinnedAccount        string    `json:"pinned_account,omitempty"`
	Account              string    `json:"account,omitempty"`
	RequestedModel       string    `json:"requested_model"`
	GatewayRequestedHdr  string    `json:"gateway_requested_hdr,omitempty"`
	ResolvedModel        string    `json:"resolved_model,omitempty"`
	UpstreamModelVersion string    `json:"upstream_model_version,omitempty"`
	StreamChunkModel     string    `json:"stream_chunk_model,omitempty"`
	CacheHeader          string    `json:"cache_header,omitempty"`
	ContentEncoding      string    `json:"content_encoding,omitempty"`
	HTTPStatus           int       `json:"http_status"`
	ConnReused           bool      `json:"conn_reused"`
	DNSMs                float64   `json:"dns_ms"`
	TCPConnectMs         float64   `json:"tcp_connect_ms"`
	TLSHandshakeMs       float64   `json:"tls_handshake_ms"`
	HeaderTTFBMs         float64   `json:"header_ttfb_ms"`
	TTFTMs               float64   `json:"ttft_ms"`
	StreamTransferMs     float64   `json:"stream_transfer_ms"`
	TotalWallMs          float64   `json:"total_wall_ms"`
	OutputBytes          int       `json:"output_bytes"`
	SSEChunks            int       `json:"sse_chunks"`
	Error                string    `json:"error,omitempty"`
}

// MetricSummary holds p50/p95/avg/min/max statistics in milliseconds.
type MetricSummary struct {
	Min float64 `json:"min"`
	P50 float64 `json:"p50"`
	P95 float64 `json:"p95"`
	Max float64 `json:"max"`
	Avg float64 `json:"avg"`
}

// ConcurrencyStepReport summarizes one concurrency level in Angle 1 (api mode).
type ConcurrencyStepReport struct {
	Concurrency         int                   `json:"concurrency"`
	BatchWallMs         float64               `json:"batch_wall_ms"`
	RPS                 float64               `json:"rps"`
	SuccessCount        int                   `json:"success_count"`
	ErrorCount          int                   `json:"error_count"`
	RateLimit429Count   int                   `json:"rate_limit_429_count"`
	ReusedConnCount     int                   `json:"reused_conn_count"`
	DNSStats            MetricSummary         `json:"dns_ms"`
	TCPStats            MetricSummary         `json:"tcp_connect_ms"`
	TLSStats            MetricSummary         `json:"tls_handshake_ms"`
	TTFTStats           MetricSummary         `json:"ttft_ms"`
	StreamTransferStats MetricSummary         `json:"stream_transfer_ms"`
	TotalWallStats      MetricSummary         `json:"total_wall_ms"`
	AccountDist         map[string]int        `json:"account_distribution"`
	ResolvedModelDist   map[string]int        `json:"resolved_model_distribution"`
	ContentEncodingDist map[string]int        `json:"content_encoding_distribution"`
	Samples             []APIRequestSample    `json:"samples"`
}

// ModelAuditRow represents one cell in Angle 2's Model Truth Matrix.
type ModelAuditRow struct {
	TraceTag             string  `json:"trace_tag"`
	PinnedAccount        string  `json:"pinned_account,omitempty"`
	Account              string  `json:"account"`
	Tier                 string  `json:"tier"`
	RequestedModel       string  `json:"requested_model"`
	ResolvedModel        string  `json:"resolved_model"`
	UpstreamModelVersion string  `json:"upstream_model_version"`
	StatusClassification string  `json:"status_classification"`
	Flagged              bool    `json:"flagged"`
	HTTPStatus           int     `json:"http_status"`
	ContentEncoding      string  `json:"content_encoding,omitempty"`
	TTFTMs               float64 `json:"ttft_ms"`
	TotalWallMs          float64 `json:"total_wall_ms"`
	Error                string  `json:"error,omitempty"`
}

// ModelAuditReport holds the complete Angle 2 Model Truth Matrix and summary counters.
type ModelAuditReport struct {
	QuotaDiscoveryStatus string          `json:"quota_discovery_status"`
	AccountsAudited      int             `json:"accounts_audited"`
	ModelsAudited        int             `json:"models_audited"`
	TotalChecks          int             `json:"total_checks"`
	ExactMatches         int             `json:"exact_matches"`
	AliasOrComboRouted   int             `json:"alias_or_combo_routed"`
	CrossFamilyFallbacks int             `json:"cross_family_fallbacks"`
	SilentDowngrades     int             `json:"silent_downgrades"`
	PinBypasses          int             `json:"pin_bypasses"`
	HTTPErrors           int             `json:"http_errors"`
	Rows                 []ModelAuditRow `json:"rows"`
}

// MCPBatchResult holds telemetry for a single Angle 3 MCP stdio child-process invocation.
type MCPBatchResult struct {
	RequestedTasks   int      `json:"requested_tasks"`
	VerifiedFiles    int      `json:"verified_files"`
	TotalBytes       int      `json:"total_bytes"`
	TotalLines       int      `json:"total_lines"`
	HandshakeMs      float64  `json:"handshake_ms"`
	ToolCallWallMs   float64  `json:"tool_call_wall_ms"`
	ProcessTotalMs   float64  `json:"process_total_ms"`
	TasksPerSec      float64  `json:"tasks_per_sec"`
	ServerName       string   `json:"server_name,omitempty"`
	ProtocolVersion  string   `json:"protocol_version,omitempty"`
	IsError          bool     `json:"is_error"`
	ToolSummary      string   `json:"tool_summary,omitempty"`
	TraceTags        []string `json:"trace_tags,omitempty"`
	Error            string   `json:"error,omitempty"`
}

// MCPBenchmarkReport holds all Angle 3 external MCP stdio runs.
type MCPBenchmarkReport struct {
	BinaryPath string           `json:"binary_path"`
	Batches    []MCPBatchResult `json:"batches"`
}

// CorrelatedRequestDiff pairs an external client request with its internal /v1/logs entry.
type CorrelatedRequestDiff struct {
	TraceTag               string  `json:"trace_tag"`
	SourceAngle            string  `json:"source_angle"`
	Account                string  `json:"account"`
	RequestedModel         string  `json:"requested_model"`
	ExternalResolvedModel  string  `json:"external_resolved_model"`
	InternalResolvedModel  string  `json:"internal_resolved_model"`
	ExternalUpstreamVer    string  `json:"external_upstream_version,omitempty"`
	InternalUpstreamModel  string  `json:"internal_upstream_model,omitempty"`
	ExternalTotalMs        float64 `json:"external_total_ms"`
	InternalGatewayMs      int64   `json:"internal_gateway_ms"`
	NetworkProxyOverheadMs float64 `json:"network_proxy_overhead_ms"`
	ModelTruthDiscrepancy  bool    `json:"model_truth_discrepancy"`
	DiscrepancyNote        string  `json:"discrepancy_note,omitempty"`
}

// LogCorrelationReport holds Angle 4 Inside-vs-Outside telemetry diff results.
type LogCorrelationReport struct {
	Enabled                bool                    `json:"enabled"`
	SkippedReason          string                  `json:"skipped_reason,omitempty"`
	LogsFetched            int                     `json:"logs_fetched"`
	OutsideTracesTotal     int                     `json:"outside_traces_total"`
	MatchedHTTPCount       int                     `json:"matched_http_count"`
	MatchedMCPCount        int                     `json:"matched_mcp_count"`
	ModelDiscrepancyCount  int                     `json:"model_discrepancy_count"`
	HTTPOverheadStats      MetricSummary           `json:"http_network_proxy_overhead_ms"`
	Diffs                  []CorrelatedRequestDiff `json:"diffs,omitempty"`
}

// BenchReport is the top-level JSON & CLI report across all 4 angles.
type BenchReport struct {
	RunID          string                  `json:"run_id"`
	GeneratedAt    string                  `json:"generated_at"`
	Config         BenchConfig             `json:"config"`
	APIBenchmarks  []ConcurrencyStepReport `json:"api_benchmarks,omitempty"`
	ModelAudit     *ModelAuditReport       `json:"model_audit,omitempty"`
	MCPBenchmark   *MCPBenchmarkReport     `json:"mcp_benchmark,omitempty"`
	LogCorrelation *LogCorrelationReport   `json:"log_correlation,omitempty"`
}

type gatewayLogItem struct {
	ID               string `json:"id"`
	Timestamp        string `json:"timestamp"`
	Provider         string `json:"provider"`
	Account          string `json:"account"`
	RequestedModel   string `json:"requested_model"`
	ResolvedModel    string `json:"resolved_model"`
	UpstreamModel    string `json:"upstream_model"`
	PromptText       string `json:"prompt_text"`
	StatusCode       int    `json:"status_code"`
	LatencyMs        int64  `json:"latency_ms"`
	TotalTokens      int    `json:"total_tokens"`
	IsError          bool   `json:"is_error"`
}

type quotaAccountItem struct {
	ID       string `json:"id"`
	Provider string `json:"provider"`
	Email    string `json:"email"`
	Tier     string `json:"tier"`
	Enabled  bool   `json:"enabled"`
	Status   string `json:"status"`
}

func main() {
	cfg, err := parseBenchFlags(os.Args[1:])
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(0)
		}
		fmt.Fprintf(os.Stderr, "[omni-bench] flag error: %v\n", err)
		os.Exit(2)
	}

	ctx := context.Background()
	report, runErr := executeBenchSuite(ctx, cfg, os.Stdout)
	if cfg.JSONOut != "" && report != nil {
		if writeErr := writeJSONReport(cfg.JSONOut, report); writeErr != nil {
			fmt.Fprintf(os.Stderr, "[omni-bench] failed writing JSON report to %s: %v\n", cfg.JSONOut, writeErr)
			if runErr == nil {
				runErr = writeErr
			}
		} else {
			fmt.Printf("\n[omni-bench] Saved JSON report -> %s\n", cfg.JSONOut)
		}
	}
	if runErr != nil {
		fmt.Fprintf(os.Stderr, "[omni-bench] finished with error: %v\n", runErr)
		os.Exit(1)
	}
}

func parseBenchFlags(args []string) (BenchConfig, error) {
	fs := flag.NewFlagSet("omni-bench", flag.ContinueOnError)

	defaultURL := firstNonEmptyStr(
		os.Getenv("OMNI_GATEWAY_URL"),
		os.Getenv("OMNIROUTE_BASE_URL"),
		defaultGatewayURL,
	)
	defaultKey := firstNonEmptyStr(
		os.Getenv("OMNI_API_KEY"),
		os.Getenv("OMNIROUTE_API_KEY"),
		os.Getenv("OPENAI_API_KEY"),
	)

	var (
		urlFlag        = fs.String("url", defaultURL, "OmniRoute Gateway base URL (e.g. https://omni.bucloud.vip)")
		keyFlag        = fs.String("key", defaultKey, "OmniRoute API key (defaults to OMNI_API_KEY env)")
		modeFlag       = fs.String("mode", "all", "Benchmark mode: api | mcp | model-audit | all")
		modelsFlag     = fs.String("models", "", "Comma-separated list of models/combos to test")
		accountsFlag   = fs.String("accounts", "", "Optional comma-separated account emails/IDs to pin via X-Omni-Pin-Account")
		concFlag       = fs.String("concurrency", "1,9,18,36", "Comma-separated concurrency levels (e.g. 1,9,18,36)")
		maxTokensFlag  = fs.Int("max-tokens", 96, "max_tokens per chat completion request")
		streamFlag     = fs.Bool("stream", true, "Use SSE stream:true to capture exact TTFT and Stream Transfer ms")
		timeoutFlag    = fs.Duration("timeout", 90*time.Second, "Per-request HTTP / MCP timeout")
		promptFlag     = fs.String("prompt", "", "Optional custom benchmark prompt")
		mcpBinFlag     = fs.String("mcp-bin", defaultMCPBinary, "Path to omni-mcp executable for Angle 3 stdio benchmark")
		mcpArgsFlag    = fs.String("mcp-args", "", "Optional comma-separated extra args passed to -mcp-bin")
		mcpTasksFlag   = fs.Int("mcp-tasks", 0, "Optional parallel file-generation task count for MCP mode (0 = auto)")
		correlateFlag  = fs.Bool("correlate-logs", true, "Correlate outside client telemetry with GET /v1/logs when key has admin access")
		allowCacheFlag = fs.Bool("allow-cache", false, "Allow identical prompts to hit gateway ResponseCache")
		insecureFlag   = fs.Bool("insecure", false, "Skip TLS certificate verification (for local self-signed test servers)")
		jsonOutFlag    = fs.String("json-out", "", "Optional file path to save full JSON benchmark report")
	)

	if err := fs.Parse(args); err != nil {
		return BenchConfig{}, err
	}

	explicitSet := make(map[string]bool)
	fs.Visit(func(f *flag.Flag) {
		explicitSet[f.Name] = true
	})

	mode := strings.ToLower(strings.TrimSpace(*modeFlag))
	switch mode {
	case "api", "mcp", "model-audit", "all":
	default:
		return BenchConfig{}, fmt.Errorf("unsupported -mode %q (expected api, mcp, model-audit, or all)", mode)
	}

	concList, err := parsePositiveIntList(*concFlag)
	if err != nil || len(concList) == 0 {
		return BenchConfig{}, fmt.Errorf("invalid -concurrency %q: %w", *concFlag, err)
	}

	models := splitCleanCSV(*modelsFlag)
	modelsExplicit := explicitSet["models"] && len(models) > 0
	if len(models) == 0 {
		if mode == "model-audit" {
			models = append([]string(nil), defaultAuditModels...)
		} else {
			models = append([]string(nil), defaultAPIModels...)
		}
	}

	maxTok := *maxTokensFlag
	if maxTok <= 0 {
		maxTok = 96
	}

	cleanURL := normalizeBaseURL(*urlFlag)
	key := strings.TrimSpace(*keyFlag)

	return BenchConfig{
		BaseURL:        cleanURL,
		APIKey:         key,
		MaskedKey:      maskSecret(key),
		Mode:           mode,
		Models:         models,
		ModelsExplicit: modelsExplicit,
		Accounts:       splitCleanCSV(*accountsFlag),
		Concurrency:    concList,
		ConcExplicit:   explicitSet["concurrency"],
		MaxTokens:      maxTok,
		Stream:         *streamFlag,
		Timeout:        *timeoutFlag,
		Prompt:         strings.TrimSpace(*promptFlag),
		MCPBin:         strings.TrimSpace(*mcpBinFlag),
		MCPArgs:        splitCleanCSV(*mcpArgsFlag),
		MCPTasks:       *mcpTasksFlag,
		CorrelateLogs:  *correlateFlag,
		AllowCache:     *allowCacheFlag,
		InsecureTLS:    *insecureFlag,
		JSONOut:        strings.TrimSpace(*jsonOutFlag),
	}, nil
}

func newBenchHTTPClient(cfg BenchConfig) *http.Client {
	tr := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          512,
		MaxIdleConnsPerHost:   256,
		MaxConnsPerHost:       0,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   15 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		WriteBufferSize:       64 * 1024,
		ReadBufferSize:        64 * 1024,
	}
	if cfg.InsecureTLS {
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
	return &http.Client{
		Transport: tr,
		Timeout:   cfg.Timeout,
	}
}

func executeBenchSuite(ctx context.Context, cfg BenchConfig, out io.Writer) (*BenchReport, error) {
	if out == nil {
		out = io.Discard
	}
	runID := randomHex(4)
	report := &BenchReport{
		RunID:       runID,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Config:      cfg,
	}

	fmt.Fprintf(out, "================================================================================\n")
	fmt.Fprintf(out, "  OmniRoute-Go External Multi-Angle Benchmark & Model-Truth Harness (run=%s)\n", runID)
	fmt.Fprintf(out, "  Target URL : %s | Mode: %s | Stream: %v | Key: %s\n",
		cfg.BaseURL, cfg.Mode, cfg.Stream, firstNonEmptyStr(cfg.MaskedKey, "(none)"))
	fmt.Fprintf(out, "================================================================================\n")

	client := newBenchHTTPClient(cfg)
	var allOutsideSamples []APIRequestSample
	var mcpTraceSamples []APIRequestSample
	var errs []string

	// Angle 1: External WAN API Key Client
	if cfg.Mode == "api" || cfg.Mode == "all" {
		steps, samples, apiErr := runAPIBenchmark(ctx, client, cfg, runID, out)
		report.APIBenchmarks = steps
		allOutsideSamples = append(allOutsideSamples, samples...)
		if apiErr != nil {
			errs = append(errs, "api: "+apiErr.Error())
		}
	}

	// Angle 2: Model Truth Verification Across Accounts
	if cfg.Mode == "model-audit" || cfg.Mode == "all" {
		auditCfg := cfg
		if cfg.Mode == "all" && !cfg.ModelsExplicit {
			auditCfg.Models = append([]string(nil), defaultAuditModels...)
		}
		auditRep, samples, auditErr := runModelAudit(ctx, client, auditCfg, runID, out)
		report.ModelAudit = auditRep
		allOutsideSamples = append(allOutsideSamples, samples...)
		if auditErr != nil {
			errs = append(errs, "model-audit: "+auditErr.Error())
		}
	}

	// Angle 3: Real External MCP JSON-RPC 2.0 Stdio Client
	if cfg.Mode == "mcp" || cfg.Mode == "all" {
		mcpRep, mcpSamples, mcpErr := runMCPBenchmark(ctx, cfg, runID, out)
		report.MCPBenchmark = mcpRep
		mcpTraceSamples = append(mcpTraceSamples, mcpSamples...)
		if mcpErr != nil {
			errs = append(errs, "mcp: "+mcpErr.Error())
		}
	}

	// Angle 4: Automatic Inside-vs-Outside Log Correlation
	if cfg.CorrelateLogs && (len(allOutsideSamples) > 0 || len(mcpTraceSamples) > 0) {
		// Brief pause (60ms) so async gateway ring-buffer/batch log entries settle
		time.Sleep(60 * time.Millisecond)
		corrRep := runLogCorrelation(ctx, client, cfg, allOutsideSamples, mcpTraceSamples, out)
		report.LogCorrelation = corrRep
	}

	if len(errs) > 0 {
		return report, errors.New(strings.Join(errs, "; "))
	}
	return report, nil
}

// --- Angle 1: External WAN API Key Client ---

func runAPIBenchmark(ctx context.Context, client *http.Client, cfg BenchConfig, runID string, out io.Writer) ([]ConcurrencyStepReport, []APIRequestSample, error) {
	fmt.Fprintf(out, "\n[Angle 1] External API Key Concurrency & HTTP Trace Benchmark (%s/v1/chat/completions)\n", cfg.BaseURL)
	fmt.Fprintf(out, "  Models      : %s\n", strings.Join(cfg.Models, ", "))
	fmt.Fprintf(out, "  Concurrency : %v | MaxTokens: %d\n\n", cfg.Concurrency, cfg.MaxTokens)

	var steps []ConcurrencyStepReport
	var collected []APIRequestSample
	var seq atomic.Uint64

	fmt.Fprintf(out, "%-6s | %-9s | %-7s | %-7s | %-9s | %-9s | %-9s | %-10s | %-10s | %-10s | %-12s\n",
		"Conc", "Wall ms", "RPS", "OK/Err", "DNS p50", "TCP p50", "TLS p50", "TTFT p50", "Xfer p50", "Total p95", "Encoding")
	fmt.Fprintf(out, "%s\n", strings.Repeat("-", 122))

	for _, conc := range cfg.Concurrency {
		samples := make([]APIRequestSample, conc)
		var wg sync.WaitGroup
		wg.Add(conc)
		batchStart := time.Now()

		for i := 0; i < conc; i++ {
			go func(idx int) {
				defer wg.Done()
				reqNum := seq.Add(1)
				model := cfg.Models[idx%len(cfg.Models)]
				var pinAcc string
				if len(cfg.Accounts) > 0 {
					pinAcc = cfg.Accounts[idx%len(cfg.Accounts)]
				}
				traceTag := fmt.Sprintf("bench-trace:%s-api-c%d-%d", runID, conc, reqNum)
				prompt := buildBenchPrompt(cfg.Prompt, traceTag, cfg.AllowCache, idx)
				samples[idx] = sendTracedChatRequest(ctx, client, cfg, model, pinAcc, prompt, traceTag, "api")
			}(i)
		}

		wg.Wait()
		batchWallMs := durationMs(time.Since(batchStart))
		rps := 0.0
		if batchWallMs > 0 {
			rps = round2(float64(conc) / (batchWallMs / 1000.0))
		}

		step := summarizeConcurrencyStep(conc, batchWallMs, rps, samples)
		steps = append(steps, step)
		collected = append(collected, samples...)

		encSummary := formatDistCompact(step.ContentEncodingDist)
		fmt.Fprintf(out, "%-6d | %9.1f | %7.2f | %3d/%-3d | %9.2f | %9.2f | %9.2f | %10.1f | %10.1f | %10.1f | %-12s\n",
			step.Concurrency,
			step.BatchWallMs,
			step.RPS,
			step.SuccessCount,
			step.ErrorCount,
			step.DNSStats.P50,
			step.TCPStats.P50,
			step.TLSStats.P50,
			step.TTFTStats.P50,
			step.StreamTransferStats.P50,
			step.TotalWallStats.P95,
			encSummary,
		)
	}

	return steps, collected, nil
}

func summarizeConcurrencyStep(conc int, batchWallMs, rps float64, samples []APIRequestSample) ConcurrencyStepReport {
	var (
		dnsVals, tcpVals, tlsVals, ttftVals, xferVals, totalVals []float64
		okCount, errCount, rl429Count, reusedCount               int
	)
	accDist := make(map[string]int)
	modDist := make(map[string]int)
	encDist := make(map[string]int)

	for _, s := range samples {
		if s.ConnReused {
			reusedCount++
		}
		encKey := s.ContentEncoding
		if encKey == "" {
			encKey = "identity"
		}
		encDist[encKey]++

		if s.HTTPStatus == http.StatusTooManyRequests {
			rl429Count++
		}
		if s.Error == "" && s.HTTPStatus >= 200 && s.HTTPStatus < 300 {
			okCount++
		} else {
			errCount++
		}

		if s.Account != "" {
			accDist[s.Account]++
		}
		if s.ResolvedModel != "" {
			modDist[s.ResolvedModel]++
		}

		dnsVals = append(dnsVals, s.DNSMs)
		tcpVals = append(tcpVals, s.TCPConnectMs)
		tlsVals = append(tlsVals, s.TLSHandshakeMs)
		ttftVals = append(ttftVals, s.TTFTMs)
		xferVals = append(xferVals, s.StreamTransferMs)
		totalVals = append(totalVals, s.TotalWallMs)
	}

	return ConcurrencyStepReport{
		Concurrency:         conc,
		BatchWallMs:         batchWallMs,
		RPS:                 rps,
		SuccessCount:        okCount,
		ErrorCount:          errCount,
		RateLimit429Count:   rl429Count,
		ReusedConnCount:     reusedCount,
		DNSStats:            computeMetrics(dnsVals),
		TCPStats:            computeMetrics(tcpVals),
		TLSStats:            computeMetrics(tlsVals),
		TTFTStats:           computeMetrics(ttftVals),
		StreamTransferStats: computeMetrics(xferVals),
		TotalWallStats:      computeMetrics(totalVals),
		AccountDist:         accDist,
		ResolvedModelDist:   modDist,
		ContentEncodingDist: encDist,
		Samples:             samples,
	}
}

func sendTracedChatRequest(
	parentCtx context.Context,
	client *http.Client,
	cfg BenchConfig,
	model string,
	pinAccount string,
	prompt string,
	traceTag string,
	angle string,
) APIRequestSample {
	sample := APIRequestSample{
		TraceTag:       traceTag,
		SourceAngle:    angle,
		StartedAt:      time.Now().UTC(),
		PinnedAccount:  pinAccount,
		RequestedModel: model,
	}

	temp := 0.1
	reqPayload := map[string]any{
		"model":       model,
		"temperature": temp,
		"max_tokens":  cfg.MaxTokens,
		"stream":      cfg.Stream,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	}
	rawBody, _ := json.Marshal(reqPayload)

	var (
		traceMu          sync.Mutex
		dnsStart         time.Time
		dnsDur           time.Duration
		connStart        time.Time
		connDur          time.Duration
		tlsStart         time.Time
		tlsDur           time.Duration
		firstRespByteAt  time.Time
		connReused       bool
	)

	reqStart := time.Now()
	trace := &httptrace.ClientTrace{
		DNSStart: func(_ httptrace.DNSStartInfo) {
			traceMu.Lock()
			dnsStart = time.Now()
			traceMu.Unlock()
		},
		DNSDone: func(_ httptrace.DNSDoneInfo) {
			traceMu.Lock()
			if !dnsStart.IsZero() {
				dnsDur = time.Since(dnsStart)
			}
			traceMu.Unlock()
		},
		ConnectStart: func(_, _ string) {
			traceMu.Lock()
			if connStart.IsZero() {
				connStart = time.Now()
			}
			traceMu.Unlock()
		},
		ConnectDone: func(_, _ string, err error) {
			traceMu.Lock()
			if err == nil && !connStart.IsZero() && connDur == 0 {
				connDur = time.Since(connStart)
			}
			traceMu.Unlock()
		},
		TLSHandshakeStart: func() {
			traceMu.Lock()
			tlsStart = time.Now()
			traceMu.Unlock()
		},
		TLSHandshakeDone: func(_ tls.ConnectionState, err error) {
			traceMu.Lock()
			if err == nil && !tlsStart.IsZero() {
				tlsDur = time.Since(tlsStart)
			}
			traceMu.Unlock()
		},
		GotConn: func(info httptrace.GotConnInfo) {
			traceMu.Lock()
			connReused = info.Reused
			traceMu.Unlock()
		},
		GotFirstResponseByte: func() {
			traceMu.Lock()
			if firstRespByteAt.IsZero() {
				firstRespByteAt = time.Now()
			}
			traceMu.Unlock()
		},
	}

	reqCtx, cancel := context.WithTimeout(httptrace.WithClientTrace(parentCtx, trace), cfg.Timeout)
	defer cancel()

	endpoint := chatCompletionsURL(cfg.BaseURL)
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, endpoint, bytes.NewReader(rawBody))
	if err != nil {
		sample.Error = err.Error()
		sample.TotalWallMs = durationMs(time.Since(reqStart))
		return sample
	}

	req.Header.Set("Content-Type", "application/json")
	// Explicitly set Accept-Encoding: gzip so Go's Transport preserves raw Content-Encoding header!
	req.Header.Set("Accept-Encoding", "gzip")
	if cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	}
	if pinAccount != "" {
		req.Header.Set("X-Omni-Pin-Account", pinAccount)
	}

	resp, err := client.Do(req)
	if err != nil {
		traceMu.Lock()
		sample.DNSMs = durationMs(dnsDur)
		sample.TCPConnectMs = durationMs(connDur)
		sample.TLSHandshakeMs = durationMs(tlsDur)
		sample.ConnReused = connReused
		traceMu.Unlock()
		sample.Error = err.Error()
		sample.TotalWallMs = durationMs(time.Since(reqStart))
		return sample
	}
	defer resp.Body.Close()

	sample.HTTPStatus = resp.StatusCode
	sample.ContentEncoding = strings.TrimSpace(resp.Header.Get("Content-Encoding"))
	sample.Account = strings.TrimSpace(resp.Header.Get("X-Omni-Gateway-Account"))
	sample.GatewayRequestedHdr = strings.TrimSpace(resp.Header.Get("X-Omni-Gateway-Requested-Model"))
	sample.ResolvedModel = strings.TrimSpace(resp.Header.Get("X-Omni-Gateway-Resolved-Model"))
	sample.UpstreamModelVersion = strings.TrimSpace(resp.Header.Get("X-Omni-Gateway-Upstream-Model-Version"))
	sample.CacheHeader = strings.TrimSpace(resp.Header.Get("X-Omni-Gateway-Cache"))

	var bodyReader io.Reader = resp.Body
	if strings.EqualFold(sample.ContentEncoding, "gzip") {
		gz, gzErr := gzip.NewReader(resp.Body)
		if gzErr != nil {
			sample.Error = "gzip decode error: " + gzErr.Error()
			sample.TotalWallMs = durationMs(time.Since(reqStart))
			return sample
		}
		defer gz.Close()
		bodyReader = gz
	}

	var firstTokenAt time.Time
	ct := strings.ToLower(resp.Header.Get("Content-Type"))

	if resp.StatusCode >= 200 && resp.StatusCode < 300 && (cfg.Stream || strings.Contains(ct, "text/event-stream")) {
		br := bufio.NewReaderSize(bodyReader, 32*1024)
		for {
			line, readErr := br.ReadString('\n')
			if len(line) > 0 {
				sample.OutputBytes += len(line)
				trimmed := strings.TrimSpace(line)
				if strings.HasPrefix(trimmed, "data:") {
					payload := strings.TrimSpace(strings.TrimPrefix(trimmed, "data:"))
					if payload != "" && payload != "[DONE]" {
						sample.SSEChunks++
						var chunk struct {
							Model   string `json:"model"`
							Choices []struct {
								Delta struct {
									Content          string `json:"content"`
									ReasoningContent string `json:"reasoning_content"`
								} `json:"delta"`
								Message struct {
									Content string `json:"content"`
								} `json:"message"`
							} `json:"choices"`
							Error *struct {
								Message string `json:"message"`
							} `json:"error"`
						}
						if json.Unmarshal([]byte(payload), &chunk) == nil {
							if chunk.Error != nil && chunk.Error.Message != "" {
								sample.Error = chunk.Error.Message
							}
							if sample.StreamChunkModel == "" && chunk.Model != "" {
								sample.StreamChunkModel = chunk.Model
							}
							hasToken := len(chunk.Choices) > 0 && (chunk.Choices[0].Delta.Content != "" ||
								chunk.Choices[0].Delta.ReasoningContent != "" ||
								chunk.Choices[0].Message.Content != "")
							if firstTokenAt.IsZero() && (hasToken || sample.SSEChunks == 1) {
								firstTokenAt = time.Now()
							}
						} else if firstTokenAt.IsZero() {
							firstTokenAt = time.Now()
						}
					}
				} else if !cfg.Stream && firstTokenAt.IsZero() {
					firstTokenAt = time.Now()
				}
			}
			if readErr != nil {
				if !errors.Is(readErr, io.EOF) && sample.Error == "" {
					sample.Error = readErr.Error()
				}
				break
			}
		}
	} else {
		rawResp, readErr := io.ReadAll(io.LimitReader(bodyReader, 4*1024*1024))
		sample.OutputBytes = len(rawResp)
		if readErr != nil {
			sample.Error = readErr.Error()
		} else if resp.StatusCode >= 400 {
			sample.Error = extractErrorText(rawResp, resp.StatusCode)
		} else {
			var nonStream struct {
				Model string `json:"model"`
			}
			if json.Unmarshal(rawResp, &nonStream) == nil && nonStream.Model != "" {
				sample.StreamChunkModel = nonStream.Model
			}
		}
	}

	doneAt := time.Now()
	traceMu.Lock()
	sample.DNSMs = durationMs(dnsDur)
	sample.TCPConnectMs = durationMs(connDur)
	sample.TLSHandshakeMs = durationMs(tlsDur)
	sample.ConnReused = connReused
	hdrAt := firstRespByteAt
	traceMu.Unlock()

	if !hdrAt.IsZero() {
		sample.HeaderTTFBMs = durationMs(hdrAt.Sub(reqStart))
	}
	if !firstTokenAt.IsZero() {
		sample.TTFTMs = durationMs(firstTokenAt.Sub(reqStart))
	} else if !hdrAt.IsZero() {
		sample.TTFTMs = sample.HeaderTTFBMs
	} else {
		sample.TTFTMs = durationMs(doneAt.Sub(reqStart))
	}

	sample.TotalWallMs = durationMs(doneAt.Sub(reqStart))
	if sample.TotalWallMs >= sample.TTFTMs {
		sample.StreamTransferMs = round2(sample.TotalWallMs - sample.TTFTMs)
	}

	return sample
}

// --- Angle 2: Model Truth Verification Across Accounts ---

type auditTargetAccount struct {
	Email string
	Tier  string
}

func discoverQuotaAccounts(ctx context.Context, client *http.Client, cfg BenchConfig) ([]quotaAccountItem, string) {
	reqCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, cfg.BaseURL+"/v1/quota", nil)
	if err != nil {
		return nil, "request_error: " + err.Error()
	}
	req.Header.Set("Accept-Encoding", "gzip")
	if cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, "network_error: " + err.Error()
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Sprintf("skipped_non_admin_key (HTTP %d)", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Sprintf("http_%d", resp.StatusCode)
	}

	var reader io.Reader = resp.Body
	if strings.EqualFold(resp.Header.Get("Content-Encoding"), "gzip") {
		gz, gzErr := gzip.NewReader(resp.Body)
		if gzErr != nil {
			return nil, "gzip_error"
		}
		defer gz.Close()
		reader = gz
	}

	var payload struct {
		Accounts []quotaAccountItem `json:"accounts"`
	}
	if err := json.NewDecoder(reader).Decode(&payload); err != nil {
		return nil, "json_decode_error"
	}
	return payload.Accounts, "ok"
}

func runModelAudit(ctx context.Context, client *http.Client, cfg BenchConfig, runID string, out io.Writer) (*ModelAuditReport, []APIRequestSample, error) {
	fmt.Fprintf(out, "\n[Angle 2] Model Truth Audit Matrix Across Accounts (%s)\n", cfg.BaseURL)

	quotaAccs, quotaStatus := discoverQuotaAccounts(ctx, client, cfg)
	tierByEmail := make(map[string]string, len(quotaAccs))
	for _, qa := range quotaAccs {
		if qa.Email != "" && qa.Tier != "" {
			tierByEmail[strings.ToLower(strings.TrimSpace(qa.Email))] = qa.Tier
		}
		if qa.ID != "" && qa.Tier != "" {
			tierByEmail[strings.ToLower(strings.TrimSpace(qa.ID))] = qa.Tier
		}
	}

	var targets []auditTargetAccount
	if len(cfg.Accounts) > 0 {
		for _, a := range cfg.Accounts {
			tier := tierByEmail[strings.ToLower(strings.TrimSpace(a))]
			if tier == "" {
				tier = "Pinned"
			}
			targets = append(targets, auditTargetAccount{Email: a, Tier: tier})
		}
	} else if len(quotaAccs) > 0 {
		for _, qa := range quotaAccs {
			if !qa.Enabled {
				continue
			}
			prov := strings.ToLower(strings.TrimSpace(qa.Provider))
			if prov != "" && prov != "antigravity" {
				continue
			}
			ident := firstNonEmptyStr(qa.Email, qa.ID)
			if ident == "" {
				continue
			}
			tier := firstNonEmptyStr(qa.Tier, "Unknown")
			targets = append(targets, auditTargetAccount{Email: ident, Tier: tier})
		}
	}

	if len(targets) == 0 {
		targets = []auditTargetAccount{{Email: "", Tier: "auto-pool"}}
	}

	totalChecks := len(targets) * len(cfg.Models)
	rows := make([]ModelAuditRow, totalChecks)
	samples := make([]APIRequestSample, totalChecks)

	// Run 1 worker per account (bounded to 9 concurrent accounts) so each account executes models with in_flight=1!
	sem := make(chan struct{}, 9)
	var wg sync.WaitGroup
	var seq atomic.Uint64

	for accIdx, targetAcc := range targets {
		wg.Add(1)
		go func(aIdx int, tAcc auditTargetAccount) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			for mIdx, model := range cfg.Models {
				flatIdx := aIdx*len(cfg.Models) + mIdx
				n := seq.Add(1)
				traceTag := fmt.Sprintf("bench-trace:%s-audit-%d", runID, n)
				prompt := fmt.Sprintf("[%s] Reply with the single word OK.", traceTag)

				s := sendTracedChatRequest(ctx, client, cfg, model, tAcc.Email, prompt, traceTag, "model-audit")
				samples[flatIdx] = s

				dispAcc := firstNonEmptyStr(s.Account, tAcc.Email, "auto-pool")
				dispTier := tAcc.Tier
				if lookedUp := tierByEmail[strings.ToLower(dispAcc)]; lookedUp != "" {
					dispTier = lookedUp
				}
				upVer := firstNonEmptyStr(s.UpstreamModelVersion, s.StreamChunkModel, "-")
				resMod := firstNonEmptyStr(s.ResolvedModel, "-")
				cls, flagged := classifyModelTruth(dispTier, tAcc.Email, s.Account, model, s.ResolvedModel, s.UpstreamModelVersion, s.HTTPStatus)

				rows[flatIdx] = ModelAuditRow{
					TraceTag:             traceTag,
					PinnedAccount:        tAcc.Email,
					Account:              dispAcc,
					Tier:                 dispTier,
					RequestedModel:       model,
					ResolvedModel:        resMod,
					UpstreamModelVersion: upVer,
					StatusClassification: cls,
					Flagged:              flagged,
					HTTPStatus:           s.HTTPStatus,
					ContentEncoding:      s.ContentEncoding,
					TTFTMs:               s.TTFTMs,
					TotalWallMs:          s.TotalWallMs,
					Error:                s.Error,
				}
			}
		}(accIdx, targetAcc)
	}
	wg.Wait()

	rep := &ModelAuditReport{
		QuotaDiscoveryStatus: quotaStatus,
		AccountsAudited:      len(targets),
		ModelsAudited:        len(cfg.Models),
		TotalChecks:          totalChecks,
		Rows:                 rows,
	}

	fmt.Fprintf(out, "  Quota Discovery: %s | Accounts: %d | Models: %d\n\n", quotaStatus, len(targets), len(cfg.Models))
	fmt.Fprintf(out, "%-34s | %-34s | %-28s | %-24s | %-26s | %-15s\n",
		"Account (Tier)", "Requested Model", "Gateway Resolved CCPA Model", "Upstream modelVersion", "Match/Fallback Status", "TTFT / Total ms")
	fmt.Fprintf(out, "%s\n", strings.Repeat("-", 174))

	for _, r := range rows {
		switch {
		case r.HTTPStatus < 200 || r.HTTPStatus >= 300:
			rep.HTTPErrors++
		case strings.HasPrefix(r.StatusClassification, "PIN_BYPASSED"):
			rep.PinBypasses++
		case strings.HasPrefix(r.StatusClassification, "SILENT_DOWNGRADE"):
			rep.SilentDowngrades++
		case strings.HasPrefix(r.StatusClassification, "CROSS_FAMILY"):
			rep.CrossFamilyFallbacks++
		case strings.HasPrefix(r.StatusClassification, "EXACT_MATCH"):
			rep.ExactMatches++
		default:
			rep.AliasOrComboRouted++
		}

		accLabel := fmt.Sprintf("%s (%s)", truncateRunes(r.Account, 24), r.Tier)
		timingLabel := fmt.Sprintf("%.0f / %.0f ms", r.TTFTMs, r.TotalWallMs)
		statusLabel := r.StatusClassification
		if r.Flagged {
			statusLabel = "! " + statusLabel
		}
		fmt.Fprintf(out, "%-34s | %-34s | %-28s | %-24s | %-26s | %-15s\n",
			truncateRunes(accLabel, 34),
			truncateRunes(r.RequestedModel, 34),
			truncateRunes(r.ResolvedModel, 28),
			truncateRunes(r.UpstreamModelVersion, 24),
			truncateRunes(statusLabel, 26),
			timingLabel,
		)
	}

	fmt.Fprintf(out, "\n  Audit Summary: Exact=%d | Alias/Combo=%d | CrossFamily=%d | SilentDowngrade=%d | PinBypass=%d | HTTPErrors=%d\n",
		rep.ExactMatches, rep.AliasOrComboRouted, rep.CrossFamilyFallbacks, rep.SilentDowngrades, rep.PinBypasses, rep.HTTPErrors)

	return rep, samples, nil
}

func classifyModelTruth(tier, pinnedAcc, actualAcc, requestedModel, resolvedModel, upstreamVer string, httpStatus int) (string, bool) {
	if httpStatus < 200 || httpStatus >= 300 {
		return fmt.Sprintf("HTTP_ERROR_%d", httpStatus), true
	}
	if pinnedAcc != "" && actualAcc != "" && actualAcc != "in-memory-cache" && !strings.EqualFold(pinnedAcc, actualAcc) {
		return "PIN_BYPASSED", true
	}

	reqNorm := stripModelPrefix(requestedModel)
	resNorm := stripModelPrefix(extractRightSideArrow(resolvedModel))
	upNorm := stripModelPrefix(upstreamVer)

	effectiveActual := resNorm
	if effectiveActual == "" || effectiveActual == "-" {
		effectiveActual = upNorm
	}

	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(requestedModel)), "auto/") {
		isFlagshipCombo := strings.Contains(reqNorm, "best-coding") ||
			strings.Contains(reqNorm, "smart") ||
			strings.Contains(reqNorm, "flagship") ||
			strings.Contains(reqNorm, "reasoning")
		if isFlagshipCombo && isLightweightFlashModel(effectiveActual) {
			return "SILENT_DOWNGRADE_FLASH", true
		}
		return "COMBO_ROUTED", false
	}

	reqIsClaude := strings.Contains(reqNorm, "claude-")
	actIsClaude := strings.Contains(effectiveActual, "claude-") || (upNorm != "" && strings.Contains(upNorm, "claude-"))
	reqIsGemini := strings.Contains(reqNorm, "gemini-") || strings.Contains(reqNorm, "carbon") || strings.Contains(reqNorm, "argon")
	actIsGemini := strings.Contains(effectiveActual, "gemini-") || (upNorm != "" && strings.Contains(upNorm, "gemini-"))

	if (reqIsGemini && actIsClaude) || (reqIsClaude && actIsGemini) {
		return "CROSS_FAMILY_FALLBACK", true
	}

	reqIsFlagship := strings.Contains(reqNorm, "carbon") ||
		strings.Contains(reqNorm, "argon") ||
		strings.Contains(reqNorm, "-pro") ||
		strings.HasSuffix(reqNorm, "pro") ||
		strings.Contains(reqNorm, "opus") ||
		strings.Contains(reqNorm, "sonnet")

	if reqIsFlagship && (isLightweightFlashModel(effectiveActual) || isLightweightFlashModel(upNorm)) {
		return fmt.Sprintf("SILENT_DOWNGRADE_FLASH (%s)", firstNonEmptyStr(tier, "acc")), true
	}

	if isVirtualIDEAlias(reqNorm) && strings.HasPrefix(effectiveActual, "gemini-3.8-flash") {
		return "VIRTUAL_ALIAS_AGENT", false
	}

	if reqNorm == effectiveActual ||
		(upNorm != "" && strings.HasPrefix(upNorm, reqNorm)) ||
		(effectiveActual != "" && strings.HasPrefix(reqNorm, effectiveActual)) {
		return "EXACT_MATCH", false
	}

	if effectiveActual == "" {
		return "UNKNOWN_RESOLVED", true
	}
	return "REROUTED -> " + effectiveActual, true
}

func isVirtualIDEAlias(normSlug string) bool {
	return strings.Contains(normSlug, "gemini-4-carbon") ||
		strings.Contains(normSlug, "gemini-4-argon") ||
		strings.Contains(normSlug, "gemini-3.1-pro")
}

func isLightweightFlashModel(normSlug string) bool {
	s := strings.ToLower(strings.TrimSpace(normSlug))
	if s == "" {
		return false
	}
	return s == "gemini-3-flash" ||
		strings.HasPrefix(s, "gemini-3-flash-0") ||
		strings.HasPrefix(s, "gemini-2.5-flash") ||
		strings.Contains(s, "flash-lite")
}

// --- Angle 3: Real External MCP JSON-RPC 2.0 Stdio Client ---

func runMCPBenchmark(ctx context.Context, cfg BenchConfig, runID string, out io.Writer) (*MCPBenchmarkReport, []APIRequestSample, error) {
	fmt.Fprintf(out, "\n[Angle 3] External MCP JSON-RPC 2.0 Stdio Benchmark (%s)\n", cfg.MCPBin)

	rep := &MCPBenchmarkReport{
		BinaryPath: cfg.MCPBin,
	}
	if _, statErr := os.Stat(cfg.MCPBin); statErr != nil {
		errMsg := fmt.Sprintf("MCP binary not found at %q: %v", cfg.MCPBin, statErr)
		rep.Batches = append(rep.Batches, MCPBatchResult{IsError: true, Error: errMsg})
		fmt.Fprintf(out, "  [ERR] %s\n", errMsg)
		return rep, nil, errors.New(errMsg)
	}

	var batchSizes []int
	switch {
	case cfg.MCPTasks > 0:
		batchSizes = []int{cfg.MCPTasks}
	case cfg.Mode == "mcp" && cfg.ConcExplicit:
		batchSizes = append([]int(nil), cfg.Concurrency...)
	case cfg.Mode == "mcp":
		batchSizes = []int{4}
	default: // "all" mode
		n := 4
		if len(cfg.Concurrency) > 0 && cfg.Concurrency[0] > 1 && cfg.Concurrency[0] <= 12 {
			n = cfg.Concurrency[0]
		}
		batchSizes = []int{n}
	}

	var allTraces []APIRequestSample
	var batchErrs []string

	for bIdx, taskCount := range batchSizes {
		bRes, traces, bErr := executeSingleMCPBatch(ctx, cfg, runID, bIdx+1, taskCount)
		rep.Batches = append(rep.Batches, bRes)
		allTraces = append(allTraces, traces...)

		if bErr != nil {
			batchErrs = append(batchErrs, bErr.Error())
			fmt.Fprintf(out, "  Batch #%d (tasks=%d): FAILED (%v)\n", bIdx+1, taskCount, bErr)
		} else {
			fmt.Fprintf(out, "  Batch #%d (tasks=%d): Verified %d/%d files (%d bytes, %d lines) | Handshake=%.1fms | ToolWall=%.1fms | Throughput=%.2f files/s\n",
				bIdx+1, bRes.RequestedTasks, bRes.VerifiedFiles, bRes.RequestedTasks,
				bRes.TotalBytes, bRes.TotalLines, bRes.HandshakeMs, bRes.ToolCallWallMs, bRes.TasksPerSec)
		}
	}

	if len(batchErrs) > 0 {
		return rep, allTraces, errors.New(strings.Join(batchErrs, "; "))
	}
	return rep, allTraces, nil
}

func executeSingleMCPBatch(parentCtx context.Context, cfg BenchConfig, runID string, batchNum, taskCount int) (MCPBatchResult, []APIRequestSample, error) {
	res := MCPBatchResult{
		RequestedTasks: taskCount,
	}

	tmpDir, err := os.MkdirTemp("", "omni-bench-mcp-*")
	if err != nil {
		res.IsError = true
		res.Error = err.Error()
		return res, nil, err
	}
	defer os.RemoveAll(tmpDir)

	procCtx, cancel := context.WithTimeout(parentCtx, cfg.Timeout)
	defer cancel()

	procStart := time.Now()
	cmd := exec.CommandContext(procCtx, cfg.MCPBin, cfg.MCPArgs...)
	cmd.Env = buildMCPEnv(cfg)

	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		res.IsError = true
		res.Error = err.Error()
		return res, nil, err
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		res.IsError = true
		res.Error = err.Error()
		return res, nil, err
	}
	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	if err := cmd.Start(); err != nil {
		res.IsError = true
		res.Error = err.Error()
		return res, nil, err
	}

	scanner := bufio.NewScanner(stdoutPipe)
	scanner.Buffer(make([]byte, 64*1024), 8*1024*1024)

	writeRPC := func(v any) error {
		b, mErr := json.Marshal(v)
		if mErr != nil {
			return mErr
		}
		_, wErr := stdinPipe.Write(append(b, '\n'))
		return wErr
	}

	readRPCByID := func(expectedID int) (json.RawMessage, *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}, error) {
		for scanner.Scan() {
			line := bytes.TrimSpace(scanner.Bytes())
			if len(line) == 0 || line[0] != '{' {
				continue
			}
			var env struct {
				JSONRPC string          `json:"jsonrpc"`
				ID      json.RawMessage `json:"id"`
				Result  json.RawMessage `json:"result"`
				Error   *struct {
					Code    int    `json:"code"`
					Message string `json:"message"`
				} `json:"error"`
			}
			if json.Unmarshal(line, &env) != nil || len(env.ID) == 0 {
				continue
			}
			var gotID int
			if json.Unmarshal(env.ID, &gotID) == nil && gotID == expectedID {
				return env.Result, env.Error, nil
			}
		}
		if sErr := scanner.Err(); sErr != nil {
			return nil, nil, sErr
		}
		return nil, nil, fmt.Errorf("EOF waiting for JSON-RPC id=%d (stderr: %s)", expectedID, strings.TrimSpace(stderrBuf.String()))
	}

	// 1. initialize
	hsStart := time.Now()
	initReq := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{},
			"clientInfo": map[string]any{
				"name":    "omni-bench",
				"version": "1.0.0",
			},
		},
	}
	if err := writeRPC(initReq); err != nil {
		_ = stdinPipe.Close()
		_ = cmd.Wait()
		res.IsError = true
		res.Error = "write initialize failed: " + err.Error()
		return res, nil, errors.New(res.Error)
	}

	initResRaw, initRPCErr, err := readRPCByID(1)
	res.HandshakeMs = durationMs(time.Since(hsStart))
	if err != nil || initRPCErr != nil {
		_ = stdinPipe.Close()
		_ = cmd.Wait()
		res.IsError = true
		if initRPCErr != nil {
			res.Error = fmt.Sprintf("initialize RPC error %d: %s", initRPCErr.Code, initRPCErr.Message)
		} else {
			res.Error = "read initialize failed: " + err.Error()
		}
		return res, nil, errors.New(res.Error)
	}

	var initParsed struct {
		ProtocolVersion string `json:"protocolVersion"`
		ServerInfo      struct {
			Name string `json:"name"`
		} `json:"serverInfo"`
	}
	_ = json.Unmarshal(initResRaw, &initParsed)
	res.ProtocolVersion = initParsed.ProtocolVersion
	res.ServerName = initParsed.ServerInfo.Name

	// 2. notifications/initialized
	_ = writeRPC(map[string]any{
		"jsonrpc": "2.0",
		"method":  "notifications/initialized",
		"params":  map[string]any{},
	})

	// 3. tools/call omni_parallel_workers
	type mcpTaskItem struct {
		FilePath string `json:"file_path"`
		Prompt   string `json:"prompt"`
	}
	tasks := make([]mcpTaskItem, taskCount)
	traceTags := make([]string, taskCount)
	expectedPaths := make([]string, taskCount)

	for i := 0; i < taskCount; i++ {
		relName := fmt.Sprintf("worker_%03d.go", i+1)
		expectedPaths[i] = filepath.Join(tmpDir, relName)
		tag := fmt.Sprintf("bench-trace:%s-mcp-b%d-t%d", runID, batchNum, i+1)
		traceTags[i] = tag
		tasks[i] = mcpTaskItem{
			FilePath: relName,
			Prompt:   fmt.Sprintf("[%s] Write a minimal compilable Go file in package benchgen defining func Worker%d() int { return %d }.", tag, i+1, i+1),
		}
	}
	res.TraceTags = traceTags

	callArgs := map[string]any{
		"base_dir":        tmpDir,
		"endpoint":        chatCompletionsURL(cfg.BaseURL),
		"api_key":         cfg.APIKey,
		"max_concurrency": 0,
		"model_pool":      cfg.Models,
		"tasks":           tasks,
	}
	if len(cfg.Accounts) > 0 {
		callArgs["pin_accounts"] = cfg.Accounts
	}

	toolStart := time.Now()
	callReq := map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "omni_parallel_workers",
			"arguments": callArgs,
		},
	}
	if err := writeRPC(callReq); err != nil {
		_ = stdinPipe.Close()
		_ = cmd.Wait()
		res.IsError = true
		res.Error = "write tools/call failed: " + err.Error()
		return res, nil, errors.New(res.Error)
	}

	toolResRaw, toolRPCErr, err := readRPCByID(2)
	res.ToolCallWallMs = durationMs(time.Since(toolStart))
	_ = stdinPipe.Close()
	_ = cmd.Wait()
	res.ProcessTotalMs = durationMs(time.Since(procStart))

	if err != nil || toolRPCErr != nil {
		res.IsError = true
		if toolRPCErr != nil {
			res.Error = fmt.Sprintf("tools/call RPC error %d: %s", toolRPCErr.Code, toolRPCErr.Message)
		} else {
			res.Error = "read tools/call failed: " + err.Error()
		}
		return res, nil, errors.New(res.Error)
	}

	var toolParsed struct {
		IsError bool `json:"isError"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	_ = json.Unmarshal(toolResRaw, &toolParsed)
	res.IsError = toolParsed.IsError
	if len(toolParsed.Content) > 0 {
		res.ToolSummary = toolParsed.Content[0].Text
	}

	for _, fullPath := range expectedPaths {
		data, rErr := os.ReadFile(fullPath)
		if rErr != nil || len(bytes.TrimSpace(data)) == 0 {
			continue
		}
		lower := strings.ToLower(string(data))
		if strings.Contains(lower, "this version of antigravity is no longer supported") {
			continue
		}
		res.VerifiedFiles++
		res.TotalBytes += len(data)
		res.TotalLines += bytes.Count(data, []byte("\n")) + 1
	}

	if res.ToolCallWallMs > 0 {
		res.TasksPerSec = round2(float64(res.VerifiedFiles) / (res.ToolCallWallMs / 1000.0))
	}

	var mcpSamples []APIRequestSample
	for i, tag := range traceTags {
		mod := cfg.Models[i%len(cfg.Models)]
		mcpSamples = append(mcpSamples, APIRequestSample{
			TraceTag:       tag,
			SourceAngle:    "mcp",
			StartedAt:      toolStart.UTC(),
			RequestedModel: mod,
			TotalWallMs:    res.ToolCallWallMs,
			HTTPStatus:     http.StatusOK,
		})
	}

	if res.IsError || res.VerifiedFiles != taskCount {
		errMsg := fmt.Sprintf("verified %d/%d MCP generated files (isError=%v)", res.VerifiedFiles, taskCount, res.IsError)
		res.Error = errMsg
		return res, mcpSamples, errors.New(errMsg)
	}

	return res, mcpSamples, nil
}

func buildMCPEnv(cfg BenchConfig) []string {
	base := os.Environ()
	filtered := make([]string, 0, len(base)+4)
	skipPrefixes := []string{"OMNI_GATEWAY_URL=", "OMNIROUTE_BASE_URL=", "OMNI_API_KEY=", "OMNIROUTE_API_KEY="}
	for _, kv := range base {
		skip := false
		for _, p := range skipPrefixes {
			if strings.HasPrefix(kv, p) {
				skip = true
				break
			}
		}
		if !skip {
			filtered = append(filtered, kv)
		}
	}
	filtered = append(filtered,
		"OMNI_GATEWAY_URL="+cfg.BaseURL+"/v1",
		"OMNIROUTE_BASE_URL="+cfg.BaseURL+"/v1",
	)
	if cfg.APIKey != "" {
		filtered = append(filtered,
			"OMNI_API_KEY="+cfg.APIKey,
			"OMNIROUTE_API_KEY="+cfg.APIKey,
		)
	}
	return filtered
}

// --- Angle 4: Inside-vs-Outside Log Correlation ---

func runLogCorrelation(
	ctx context.Context,
	client *http.Client,
	cfg BenchConfig,
	httpSamples []APIRequestSample,
	mcpSamples []APIRequestSample,
	out io.Writer,
) *LogCorrelationReport {
	fmt.Fprintf(out, "\n[Angle 4] Inside-vs-Outside Gateway Log Correlation (%s/v1/logs?limit=100)\n", cfg.BaseURL)

	rep := &LogCorrelationReport{
		Enabled:            true,
		OutsideTracesTotal: len(httpSamples) + len(mcpSamples),
	}

	reqCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, cfg.BaseURL+"/v1/logs?limit=100", nil)
	if err != nil {
		rep.SkippedReason = "request_build_error: " + err.Error()
		fmt.Fprintf(out, "  Skipping correlation: %s\n", rep.SkippedReason)
		return rep
	}
	req.Header.Set("Accept-Encoding", "gzip")
	if cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	}

	resp, err := client.Do(req)
	if err != nil {
		rep.SkippedReason = "network_error: " + err.Error()
		fmt.Fprintf(out, "  Skipping correlation: %s\n", rep.SkippedReason)
		return rep
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		rep.SkippedReason = fmt.Sprintf("unauthorized_non_admin_key (HTTP %d)", resp.StatusCode)
		fmt.Fprintf(out, "  Gracefully skipped /v1/logs correlation: API key lacks admin access (HTTP %d)\n", resp.StatusCode)
		return rep
	}
	if resp.StatusCode != http.StatusOK {
		rep.SkippedReason = fmt.Sprintf("unexpected_status_%d", resp.StatusCode)
		fmt.Fprintf(out, "  Skipping correlation: /v1/logs returned HTTP %d\n", resp.StatusCode)
		return rep
	}

	var reader io.Reader = resp.Body
	if strings.EqualFold(resp.Header.Get("Content-Encoding"), "gzip") {
		gz, gzErr := gzip.NewReader(resp.Body)
		if gzErr != nil {
			rep.SkippedReason = "gzip_decode_error"
			return rep
		}
		defer gz.Close()
		reader = gz
	}

	var logPayload struct {
		Data []gatewayLogItem `json:"data"`
	}
	if err := json.NewDecoder(reader).Decode(&logPayload); err != nil {
		rep.SkippedReason = "json_decode_error: " + err.Error()
		fmt.Fprintf(out, "  Skipping correlation: %s\n", rep.SkippedReason)
		return rep
	}

	logs := logPayload.Data
	rep.LogsFetched = len(logs)
	usedLogIdx := make(map[int]bool, len(logs))

	findMatchingLog := func(s APIRequestSample) *gatewayLogItem {
		// 1. Exact deterministic match on [bench-trace:...] embedded in PromptText
		if s.TraceTag != "" {
			for i := range logs {
				if usedLogIdx[i] {
					continue
				}
				if strings.Contains(logs[i].PromptText, s.TraceTag) {
					usedLogIdx[i] = true
					return &logs[i]
				}
			}
		}
		// 2. Fallback match on (Account, RequestedModel) within 15s window
		for i := range logs {
			if usedLogIdx[i] {
				continue
			}
			if s.Account != "" && logs[i].Account != "" && !strings.EqualFold(s.Account, logs[i].Account) {
				continue
			}
			if stripModelPrefix(s.RequestedModel) != stripModelPrefix(logs[i].RequestedModel) {
				continue
			}
			if logTime, tErr := time.Parse(time.RFC3339, logs[i].Timestamp); tErr == nil && !s.StartedAt.IsZero() {
				delta := logTime.Sub(s.StartedAt)
				if delta < -5*time.Second || delta > 20*time.Second {
					continue
				}
			}
			usedLogIdx[i] = true
			return &logs[i]
		}
		return nil
	}

	var httpOverheads []float64
	allCandidates := make([]APIRequestSample, 0, len(httpSamples)+len(mcpSamples))
	allCandidates = append(allCandidates, httpSamples...)
	allCandidates = append(allCandidates, mcpSamples...)

	for _, s := range allCandidates {
		if s.HTTPStatus < 200 || s.HTTPStatus >= 300 {
			continue
		}
		matched := findMatchingLog(s)
		if matched == nil {
			continue
		}

		overhead := round2(s.TotalWallMs - float64(matched.LatencyMs))
		if s.SourceAngle == "mcp" {
			rep.MatchedMCPCount++
		} else {
			rep.MatchedHTTPCount++
			httpOverheads = append(httpOverheads, overhead)
		}

		var discrepancy bool
		var noteParts []string
		if s.ResolvedModel != "" && matched.ResolvedModel != "" &&
			stripModelPrefix(extractRightSideArrow(s.ResolvedModel)) != stripModelPrefix(extractRightSideArrow(matched.ResolvedModel)) {
			discrepancy = true
			noteParts = append(noteParts, fmt.Sprintf("header_resolved=%s != log_resolved=%s", s.ResolvedModel, matched.ResolvedModel))
		}
		if stripModelPrefix(matched.RequestedModel) != stripModelPrefix(extractRightSideArrow(matched.ResolvedModel)) && matched.ResolvedModel != "" {
			noteParts = append(noteParts, fmt.Sprintf("rerouted:%s->%s", matched.RequestedModel, matched.ResolvedModel))
		}
		if discrepancy {
			rep.ModelDiscrepancyCount++
		}

		rep.Diffs = append(rep.Diffs, CorrelatedRequestDiff{
			TraceTag:               s.TraceTag,
			SourceAngle:            s.SourceAngle,
			Account:                firstNonEmptyStr(matched.Account, s.Account),
			RequestedModel:         firstNonEmptyStr(matched.RequestedModel, s.RequestedModel),
			ExternalResolvedModel:  s.ResolvedModel,
			InternalResolvedModel:  matched.ResolvedModel,
			ExternalUpstreamVer:    s.UpstreamModelVersion,
			InternalUpstreamModel:  matched.UpstreamModel,
			ExternalTotalMs:        s.TotalWallMs,
			InternalGatewayMs:      matched.LatencyMs,
			NetworkProxyOverheadMs: overhead,
			ModelTruthDiscrepancy:  discrepancy,
			DiscrepancyNote:        strings.Join(noteParts, "; "),
		})
	}

	rep.HTTPOverheadStats = computeMetrics(httpOverheads)

	fmt.Fprintf(out, "  Logs Fetched: %d | Matched HTTP: %d | Matched MCP: %d | Header-vs-Log Discrepancies: %d\n",
		rep.LogsFetched, rep.MatchedHTTPCount, rep.MatchedMCPCount, rep.ModelDiscrepancyCount)
	if rep.MatchedHTTPCount > 0 {
		fmt.Fprintf(out, "  Network + Reverse-Proxy Overhead (External TotalMs - Internal Gateway LatencyMs): min=%.1fms | p50=%.1fms | p95=%.1fms | avg=%.1fms | max=%.1fms\n",
			rep.HTTPOverheadStats.Min, rep.HTTPOverheadStats.P50, rep.HTTPOverheadStats.P95, rep.HTTPOverheadStats.Avg, rep.HTTPOverheadStats.Max)
	}

	showLimit := len(rep.Diffs)
	if showLimit > 12 {
		showLimit = 12
	}
	if showLimit > 0 {
		fmt.Fprintf(out, "\n  %-7s | %-26s | %-28s | %-24s | %-10s | %-10s | %-11s\n",
			"Angle", "Account", "Requested Model", "Internal Resolved", "Outside ms", "Inside ms", "Overhead ms")
		fmt.Fprintf(out, "  %s\n", strings.Repeat("-", 132))
		for i := 0; i < showLimit; i++ {
			d := rep.Diffs[i]
			inModel := d.InternalResolvedModel
			if d.InternalUpstreamModel != "" && stripModelPrefix(d.InternalUpstreamModel) != stripModelPrefix(inModel) {
				inModel = fmt.Sprintf("%s [%s]", inModel, d.InternalUpstreamModel)
			}
			fmt.Fprintf(out, "  %-7s | %-26s | %-28s | %-24s | %10.1f | %10d | %+11.1f\n",
				d.SourceAngle,
				truncateRunes(d.Account, 26),
				truncateRunes(d.RequestedModel, 28),
				truncateRunes(inModel, 24),
				d.ExternalTotalMs,
				d.InternalGatewayMs,
				d.NetworkProxyOverheadMs,
			)
		}
	}

	return rep
}

// --- Pure Utility Helpers ---

func buildBenchPrompt(customPrompt, traceTag string, allowCache bool, idx int) string {
	base := customPrompt
	if base == "" {
		base = "Write a concise idiomatic Go function ClampInt(v, lo, hi int) int without markdown explanation."
	}
	if allowCache {
		return base
	}
	return fmt.Sprintf("[%s] %s (case=%d)", traceTag, base, idx+1)
}

func computeMetrics(vals []float64) MetricSummary {
	if len(vals) == 0 {
		return MetricSummary{}
	}
	sorted := make([]float64, len(vals))
	copy(sorted, vals)
	sort.Float64s(sorted)

	var sum float64
	for _, v := range sorted {
		sum += v
	}
	return MetricSummary{
		Min: round2(sorted[0]),
		P50: round2(percentileSorted(sorted, 0.50)),
		P95: round2(percentileSorted(sorted, 0.95)),
		Max: round2(sorted[len(sorted)-1]),
		Avg: round2(sum / float64(len(sorted))),
	}
}

func percentileSorted(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	if len(sorted) == 1 {
		return sorted[0]
	}
	pos := p * float64(len(sorted)-1)
	lo := int(math.Floor(pos))
	hi := int(math.Ceil(pos))
	if lo == hi {
		return sorted[lo]
	}
	frac := pos - float64(lo)
	return sorted[lo]*(1.0-frac) + sorted[hi]*frac
}

func normalizeBaseURL(raw string) string {
	u := strings.TrimSpace(raw)
	u = strings.TrimRight(u, "/")
	u = strings.TrimSuffix(u, "/chat/completions")
	u = strings.TrimSuffix(u, "/v1")
	if u == "" {
		return defaultGatewayURL
	}
	return u
}

func chatCompletionsURL(baseURL string) string {
	return normalizeBaseURL(baseURL) + "/v1/chat/completions"
}

func parsePositiveIntList(csv string) ([]int, error) {
	parts := splitCleanCSV(csv)
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		v, err := strconv.Atoi(p)
		if err != nil || v <= 0 {
			return nil, fmt.Errorf("invalid positive integer %q", p)
		}
		out = append(out, v)
	}
	return out, nil
}

func splitCleanCSV(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	raw := strings.Split(s, ",")
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if t := strings.TrimSpace(item); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func stripModelPrefix(m string) string {
	s := strings.ToLower(strings.TrimSpace(m))
	for _, p := range []string{"antigravity/", "claude-code/", "alibaba/", "models/", "google/", "agy/", "anthropic/", "cc/"} {
		if strings.HasPrefix(s, p) {
			return strings.TrimSpace(strings.TrimPrefix(s, p))
		}
	}
	return s
}

func extractRightSideArrow(m string) string {
	for _, sep := range []string{"➔", "->"} {
		if idx := strings.LastIndex(m, sep); idx >= 0 {
			return strings.TrimSpace(m[idx+len(sep):])
		}
	}
	return strings.TrimSpace(m)
}

func extractErrorText(body []byte, statusCode int) string {
	var parsed struct {
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &parsed) == nil && parsed.Error != nil && parsed.Error.Message != "" {
		return fmt.Sprintf("HTTP %d: %s", statusCode, parsed.Error.Message)
	}
	txt := strings.TrimSpace(string(body))
	if len(txt) > 180 {
		txt = txt[:180] + "..."
	}
	if txt == "" {
		return fmt.Sprintf("HTTP %d", statusCode)
	}
	return fmt.Sprintf("HTTP %d: %s", statusCode, txt)
}

func formatDistCompact(dist map[string]int) string {
	if len(dist) == 0 {
		return "-"
	}
	keys := make([]string, 0, len(dist))
	for k := range dist {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s:%d", k, dist[k]))
	}
	return strings.Join(parts, ",")
}

func writeJSONReport(path string, report *BenchReport) error {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}
	raw, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(raw, '\n'), 0644)
}

func maskSecret(k string) string {
	k = strings.TrimSpace(k)
	if k == "" {
		return ""
	}
	if len(k) <= 10 {
		return "****"
	}
	return k[:7] + "..." + k[len(k)-4:]
}

func randomHex(nBytes int) string {
	b := make([]byte, nBytes)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func durationMs(d time.Duration) float64 {
	return round2(float64(d) / float64(time.Millisecond))
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}

func truncateRunes(s string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= maxRunes {
		return s
	}
	if maxRunes <= 2 {
		return string(r[:maxRunes])
	}
	return string(r[:maxRunes-2]) + ".."
}

func firstNonEmptyStr(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
