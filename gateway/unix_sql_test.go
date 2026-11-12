package gateway

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type unixSQLMockState struct {
	mu               sync.Mutex
	ddlStatements    []string
	insertedBatches  [][]GatewayLogEntry
	storedRows       []GatewayLogEntry
	failUpstreamOnce bool
}

type unixSQLMockDriver struct {
	state *unixSQLMockState
}

type unixSQLMockConn struct {
	state *unixSQLMockState
}

type unixSQLMockRows struct {
	cols []string
	rows [][]driver.Value
	idx  int
}

func (r *unixSQLMockRows) Columns() []string { return r.cols }
func (r *unixSQLMockRows) Close() error      { return nil }
func (r *unixSQLMockRows) Next(dest []driver.Value) error {
	if r.idx >= len(r.rows) {
		return io.EOF
	}
	row := r.rows[r.idx]
	r.idx++
	for i := range dest {
		if i < len(row) {
			dest[i] = row[i]
		}
	}
	return nil
}

func (d *unixSQLMockDriver) Open(_ string) (driver.Conn, error) {
	return &unixSQLMockConn{state: d.state}, nil
}

func (c *unixSQLMockConn) Prepare(_ string) (driver.Stmt, error) {
	return nil, errors.New("prepare not used")
}

func (c *unixSQLMockConn) Close() error { return nil }

func (c *unixSQLMockConn) Begin() (driver.Tx, error) {
	return mockPGTx{}, nil
}

func (c *unixSQLMockConn) BeginTx(_ context.Context, _ driver.TxOptions) (driver.Tx, error) {
	return mockPGTx{}, nil
}

func (c *unixSQLMockConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	qTrim := strings.TrimSpace(query)
	c.state.mu.Lock()
	defer c.state.mu.Unlock()

	if strings.HasPrefix(qTrim, "CREATE ") || strings.HasPrefix(qTrim, "ALTER ") {
		c.state.ddlStatements = append(c.state.ddlStatements, qTrim)
		return driver.RowsAffected(0), nil
	}

	if strings.HasPrefix(qTrim, "INSERT INTO omni_prompt_logs") {
		hasUpstreamCol := strings.Contains(qTrim, "upstream_model")
		if hasUpstreamCol && c.state.failUpstreamOnce {
			c.state.failUpstreamOnce = false
			return nil, errors.New(`pq: column "upstream_model" of relation "omni_prompt_logs" does not exist`)
		}
		cols := 20
		if hasUpstreamCol {
			cols = 21
		}
		if len(args)%cols != 0 {
			return nil, fmt.Errorf("arg count %d not divisible by cols %d", len(args), cols)
		}
		nRows := len(args) / cols
		batch := make([]GatewayLogEntry, 0, nRows)
		for r := 0; r < nRows; r++ {
			base := r * cols
			var e GatewayLogEntry
			e.ID = fmt.Sprint(args[base+0].Value)
			e.Timestamp = fmt.Sprint(args[base+1].Value)
			e.APIKeyID = fmt.Sprint(args[base+2].Value)
			e.EmployeeName = fmt.Sprint(args[base+3].Value)
			e.MaskedKey = fmt.Sprint(args[base+4].Value)
			e.Provider = ProviderID(fmt.Sprint(args[base+5].Value))
			e.Account = fmt.Sprint(args[base+6].Value)
			e.RequestedModel = fmt.Sprint(args[base+7].Value)
			e.ResolvedModel = fmt.Sprint(args[base+8].Value)
			offset := 9
			if hasUpstreamCol {
				e.UpstreamModel = fmt.Sprint(args[base+9].Value)
				offset = 10
			}
			e.PromptText = fmt.Sprint(args[base+offset+0].Value)
			e.ResponseText = fmt.Sprint(args[base+offset+1].Value)
			if v, ok := args[base+offset+2].Value.(int64); ok {
				e.PromptTokens = int(v)
			}
			if v, ok := args[base+offset+3].Value.(int64); ok {
				e.CompletionTokens = int(v)
			}
			if v, ok := args[base+offset+4].Value.(int64); ok {
				e.TotalTokens = int(v)
			}
			if v, ok := args[base+offset+5].Value.(int64); ok {
				e.TokensSaved = int(v)
			}
			if v, ok := args[base+offset+6].Value.(int64); ok {
				e.LatencyMs = v
			}
			if v, ok := args[base+offset+7].Value.(int64); ok {
				e.StatusCode = int(v)
			}
			if v, ok := args[base+offset+8].Value.(bool); ok {
				e.IsError = v
			}
			e.ErrorMessage = fmt.Sprint(args[base+offset+9].Value)
			e.ErrorDetail = fmt.Sprint(args[base+offset+10].Value)

			batch = append(batch, e)
			c.state.storedRows = append(c.state.storedRows, e)
		}
		c.state.insertedBatches = append(c.state.insertedBatches, batch)
		return driver.RowsAffected(int64(nRows)), nil
	}

	return driver.RowsAffected(0), nil
}

func (c *unixSQLMockConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	qNorm := strings.ToLower(strings.TrimSpace(query))
	if !strings.Contains(qNorm, "from omni_prompt_logs") {
		return &unixSQLMockRows{}, nil
	}
	if !strings.Contains(qNorm, "coalesce(upstream_model, '')") {
		return nil, fmt.Errorf("expected SELECT to include COALESCE(upstream_model, ''), got: %s", query)
	}

	c.state.mu.Lock()
	snapshot := make([]GatewayLogEntry, len(c.state.storedRows))
	copy(snapshot, c.state.storedRows)
	c.state.mu.Unlock()

	limit := len(snapshot)
	if len(args) > 0 {
		if limVal, ok := args[len(args)-1].Value.(int64); ok && int(limVal) < limit {
			limit = int(limVal)
		}
	}

	var searchNeedle string
	if strings.Contains(qNorm, "upstream_model ilike") && len(args) >= 2 {
		rawPat := fmt.Sprint(args[len(args)-2].Value)
		searchNeedle = strings.ToLower(strings.Trim(rawPat, "%"))
	}

	// Return newest-first (matching ORDER BY created_at DESC)
	var outRows [][]driver.Value
	for i := len(snapshot) - 1; i >= 0 && len(outRows) < limit; i-- {
		e := snapshot[i]
		if searchNeedle != "" {
			hay := strings.ToLower(e.PromptText + " " + e.ResponseText + " " + e.EmployeeName + " " + e.RequestedModel + " " + e.ResolvedModel + " " + e.UpstreamModel)
			if !strings.Contains(hay, searchNeedle) {
				continue
			}
		}
		outRows = append(outRows, []driver.Value{
			e.ID, e.Timestamp, e.APIKeyID, e.EmployeeName, e.MaskedKey,
			string(e.Provider), e.Account, e.RequestedModel, e.ResolvedModel, e.UpstreamModel,
			e.PromptText, e.ResponseText, int64(e.PromptTokens), int64(e.CompletionTokens), int64(e.TotalTokens), int64(e.TokensSaved),
			e.LatencyMs, int64(e.StatusCode), e.IsError, e.ErrorMessage, e.ErrorDetail,
		})
	}

	cols := []string{
		"id", "timestamp_str", "api_key_id", "employee_name", "masked_key",
		"provider", "account", "requested_model", "resolved_model", "upstream_model",
		"prompt_text", "response_text", "prompt_tokens", "completion_tokens", "total_tokens", "tokens_saved",
		"latency_ms", "status_code", "is_error", "error_message", "error_detail",
	}
	return &unixSQLMockRows{cols: cols, rows: outRows}, nil
}

var (
	registerUnixSQLMockOnce sync.Once
	globalUnixSQLMockState  = &unixSQLMockState{}
)

func openUnixSQLMockDB(t *testing.T) *sql.DB {
	t.Helper()
	registerUnixSQLMockOnce.Do(func() {
		sql.Register("omni_unix_sql_mock", &unixSQLMockDriver{state: globalUnixSQLMockState})
	})
	db, err := sql.Open("omni_unix_sql_mock", "mock://unix-sql")
	if err != nil {
		t.Fatalf("sql.Open(omni_unix_sql_mock) failed: %v", err)
	}
	return db
}

func TestPostgresUpstreamModelMigrationBatchSQLAndRestartHydration(t *testing.T) {
	// 1. Verify 1-row and 32-row batch SQL builder with UpstreamModel (21 cols -> $21 and $672)
	sql1, args1 := buildBatchInsertPromptLogsSQL([]GatewayLogEntry{
		{
			ID:             "up-1",
			Provider:       ProviderAntigravity,
			Account:        "ntky152022@gmail.com",
			RequestedModel: "antigravity/gemini-4-carbon-high",
			ResolvedModel:  "gemini-3.8-flash-high",
			UpstreamModel:  "gemini-3.8-flash\x00",
			StatusCode:     200,
			TotalTokens:    128,
		},
	})
	if len(args1) != 21 || !strings.Contains(sql1, "upstream_model") || !strings.Contains(sql1, "$21)") {
		t.Fatalf("expected 21 args and $21 placeholder with upstream_model, got args=%d sql=%s", len(args1), sql1)
	}
	if args1[9] != "gemini-3.8-flash" {
		t.Fatalf("expected sanitized upstream_model arg at index 9 == 'gemini-3.8-flash', got %q", args1[9])
	}

	batch32 := make([]GatewayLogEntry, promptLogBatchSize)
	for i := range batch32 {
		up := "gemini-3.8-flash"
		if i%2 == 1 {
			up = "" // mixed batch: verify empty UpstreamModel entries stay aligned in 21-col batch
		}
		batch32[i] = GatewayLogEntry{
			ID:             fmt.Sprintf("up32-%d", i),
			Provider:       ProviderAntigravity,
			RequestedModel: "auto/fast",
			ResolvedModel:  "gemini-3-flash",
			UpstreamModel:  up,
			StatusCode:     200,
		}
	}
	sql32, args32 := buildBatchInsertPromptLogsSQL(batch32)
	if len(args32) != 32*21 || !strings.Contains(sql32, "$672)") {
		t.Fatalf("expected 672 args (32*21) and $672 placeholder, got args=%d", len(args32))
	}

	// 2. Verify schema migration DDL + auto-migration retry + LoadPromptLogsFromPostgres & QueryPromptLogs
	globalUnixSQLMockState.mu.Lock()
	globalUnixSQLMockState.ddlStatements = nil
	globalUnixSQLMockState.insertedBatches = nil
	globalUnixSQLMockState.storedRows = nil
	globalUnixSQLMockState.failUpstreamOnce = true
	globalUnixSQLMockState.mu.Unlock()

	db := openUnixSQLMockDB(t)
	defer db.Close()

	if err := InitPostgresSchema(context.Background(), db); err != nil {
		t.Fatalf("InitPostgresSchema failed: %v", err)
	}

	globalUnixSQLMockState.mu.Lock()
	ddls := append([]string(nil), globalUnixSQLMockState.ddlStatements...)
	globalUnixSQLMockState.mu.Unlock()

	foundAlterUpstream := false
	for _, stmt := range ddls {
		if strings.Contains(stmt, "ALTER TABLE omni_prompt_logs ADD COLUMN IF NOT EXISTS upstream_model TEXT NOT NULL DEFAULT ''") {
			foundAlterUpstream = true
			break
		}
	}
	if !foundAlterUpstream {
		t.Fatalf("expected idempotent ALTER TABLE omni_prompt_logs ADD COLUMN IF NOT EXISTS upstream_model migration, got %v", ddls)
	}

	_ = FlushPromptLogs(2 * time.Second)
	pgMu.Lock()
	origDB := pgDB
	origConn := pgConnected
	pgDB = db
	pgConnected = true
	pgMu.Unlock()

	t.Cleanup(func() {
		_ = FlushPromptLogs(2 * time.Second)
		pgMu.Lock()
		pgDB = origDB
		pgConnected = origConn
		pgMu.Unlock()
	})

	// Test flushPromptLogsBatch (including automatic ALTER retry when failUpstreamOnce triggers)
	flushPromptLogsBatch([]GatewayLogEntry{
		{
			ID:             "pg-row-1",
			Timestamp:      "2026-11-12T13:00:00Z",
			Provider:       ProviderAntigravity,
			Account:        "ntky152022@gmail.com",
			RequestedModel: "antigravity/gemini-4-carbon-high",
			ResolvedModel:  "gemini-3.8-flash-high",
			UpstreamModel:  "gemini-3.8-flash",
			PromptText:     "Write Go ring buffer",
			StatusCode:     200,
			LatencyMs:      2450,
		},
		{
			ID:             "pg-row-2",
			Timestamp:      "2026-11-12T13:00:01Z",
			Provider:       ProviderAntigravity,
			Account:        "in-memory-cache",
			RequestedModel: "antigravity/gemini-3-flash",
			ResolvedModel:  "gemini-3-flash",
			UpstreamModel:  "cache-hit",
			PromptText:     "Cached prompt",
			StatusCode:     200,
			LatencyMs:      1,
		},
	})

	loaded, err := LoadPromptLogsFromPostgres(10)
	if err != nil {
		t.Fatalf("LoadPromptLogsFromPostgres failed: %v", err)
	}
	if len(loaded) != 2 {
		t.Fatalf("expected 2 loaded rows from Postgres, got %d", len(loaded))
	}
	if loaded[0].ID != "pg-row-2" || loaded[0].UpstreamModel != "cache-hit" {
		t.Errorf("expected newest row pg-row-2 with UpstreamModel='cache-hit', got %+v", loaded[0])
	}
	if loaded[1].ID != "pg-row-1" || loaded[1].UpstreamModel != "gemini-3.8-flash" {
		t.Errorf("expected second row pg-row-1 with UpstreamModel='gemini-3.8-flash', got %+v", loaded[1])
	}

	// Verify QueryPromptLogs (used by GET /v1/logs) preserves UpstreamModel and supports searching by upstream_model
	queried := QueryPromptLogs(10, "antigravity", false, "", "gemini-3.8-flash")
	if len(queried) != 1 || queried[0].UpstreamModel != "gemini-3.8-flash" {
		t.Fatalf("expected QueryPromptLogs search for 'gemini-3.8-flash' to return 1 matching row, got %+v", queried)
	}

	// Simulate container restart: clear GlobalLogger in RAM, then hydrate from Postgres
	GlobalLogger.ClearLogs()
	hydrateGlobalLoggerFromPostgres(db)
	memRestored := GlobalLogger.GetRecentLogs(10, "", false)
	if len(memRestored) != 2 || memRestored[0].UpstreamModel != "cache-hit" || memRestored[1].UpstreamModel != "gemini-3.8-flash" {
		t.Fatalf("expected hydrateGlobalLoggerFromPostgres to restore 2 logs with UpstreamModel intact, got %+v", memRestored)
	}
}

func TestUnixDomainSocketHTTPServingPermissionsAndCleanup(t *testing.T) {
	tmpCfg := filepath.Join(t.TempDir(), "gateway_config.json")
	t.Setenv("GATEWAY_CONFIG_FILE", tmpCfg)

	// Use short path in os.TempDir() so sun_path stays well below 104/108-byte AF_UNIX limit on all OSes.
	sockPath := filepath.Join(os.TempDir(), fmt.Sprintf("omni-ut-%d.sock", time.Now().UnixNano()%1_000_000_000))
	_ = os.Remove(sockPath)
	t.Cleanup(func() { _ = os.Remove(sockPath) })

	// Create a stale file first to verify stale-socket removal before bind.
	if err := os.WriteFile(sockPath, []byte("stale"), 0600); err != nil {
		t.Fatalf("failed creating stale marker file at %s: %v", sockPath, err)
	}
	_ = os.Remove(sockPath)

	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Skipf("AF_UNIX not supported in this test environment (%s): %v", sockPath, err)
	}
	_ = os.Chmod(sockPath, 0666)

	gwSrv := NewServer(GatewayConfig{ListenAddr: "127.0.0.1:0"}, NewResolver(), nil)
	unixSrv := &http.Server{
		Handler:           gwSrv.Handler(),
		ReadHeaderTimeout: 15 * time.Second,
		ReadTimeout:       5 * time.Minute,
		WriteTimeout:      0,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	serveDone := make(chan error, 1)
	go func() {
		serveDone <- unixSrv.Serve(ln)
	}()

	unixClient := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", sockPath)
			},
			MaxIdleConnsPerHost: 16,
		},
		Timeout: 5 * time.Second,
	}

	resp, err := unixClient.Get("http://localhost/health")
	if err != nil {
		_ = unixSrv.Close()
		t.Fatalf("GET /health over unix socket %s failed: %v", sockPath, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected HTTP 200 over unix socket, got %d", resp.StatusCode)
	}
	var healthPayload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&healthPayload); err != nil {
		t.Fatalf("failed decoding /health JSON over unix socket: %v", err)
	}
	if healthPayload["status"] != "ok" {
		t.Errorf("expected health status 'ok', got %v", healthPayload["status"])
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := unixSrv.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("unixSrv.Shutdown failed: %v", err)
	}
	_ = os.Remove(sockPath)

	if err := <-serveDone; err != nil && !errors.Is(err, http.ErrServerClosed) {
		t.Fatalf("unexpected Serve exit error: %v", err)
	}
	if _, statErr := os.Stat(sockPath); !os.IsNotExist(statErr) {
		t.Errorf("expected unix socket file %s to be removed after shutdown, statErr=%v", sockPath, statErr)
	}
}
