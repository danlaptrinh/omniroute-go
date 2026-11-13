package gateway

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSingleflightOAuthRefreshDeduplicates20ConcurrentCallers(t *testing.T) {
	tmpCfg := filepath.Join(t.TempDir(), "gateway_config.json")
	t.Setenv("GATEWAY_CONFIG_FILE", tmpCfg)
	resetAuthCacheForTest(t)

	origGoogleEndpoint := OAuthTokenEndpoint
	origClaudeEndpoint := ClaudeTokenEndpoint
	t.Cleanup(func() {
		WaitForPendingOAuthRefreshes(2 * time.Second)
		OAuthTokenEndpoint = origGoogleEndpoint
		ClaudeTokenEndpoint = origClaudeEndpoint
		FlushPendingConfigSave()
		resetAuthCacheForTest(t)
	})

	var googleRefreshHits atomic.Int32
	mockGoogleOAuth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		googleRefreshHits.Add(1)
		time.Sleep(45 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "ya29.singleflight-fresh-token",
			"refresh_token": "1//singleflight-rotated-rt",
			"expires_in":    3600,
			"token_type":    "Bearer",
		})
	}))
	defer mockGoogleOAuth.Close()
	OAuthTokenEndpoint = mockGoogleOAuth.URL

	var claudeRefreshHits atomic.Int32
	mockClaudeOAuth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claudeRefreshHits.Add(1)
		time.Sleep(40 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "sk-ant-oat01-singleflight-fresh",
			"refresh_token": "sk-ant-ort01-singleflight-next",
			"expires_in":    7200,
		})
	}))
	defer mockClaudeOAuth.Close()
	ClaudeTokenEndpoint = mockClaudeOAuth.URL

	if _, err := UpsertConnectedAccount(ConnectedAccount{
		Provider:     ProviderAntigravity,
		Email:        "sf-agy@gmail.com",
		AccessToken:  "ya29.expired-token",
		RefreshToken: "1//singleflight-seed-rt",
		ProjectID:    "proj-sf",
		ExpiresAt:    time.Now().Unix() - 30,
	}); err != nil {
		t.Fatalf("UpsertConnectedAccount(Antigravity) failed: %v", err)
	}

	if _, err := UpsertConnectedAccount(ConnectedAccount{
		Provider:     ProviderClaudeCode,
		Email:        "sf-claude@anthropic.local",
		AccessToken:  "sk-ant-oat01-expired",
		RefreshToken: "sk-ant-ort01-seed",
		ExpiresAt:    time.Now().Unix() - 20,
	}); err != nil {
		t.Fatalf("UpsertConnectedAccount(ClaudeCode) failed: %v", err)
	}

	t.Run("Antigravity_20_Concurrent_Callers", func(t *testing.T) {
		const workers = 20
		startBarrier := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(workers)
		tokens := make([]string, workers)
		errs := make([]error, workers)

		for i := 0; i < workers; i++ {
			go func(idx int) {
				defer wg.Done()
				<-startBarrier
				if idx%2 == 0 {
					tok, _, _, err := GetValidAccountToken(context.Background())
					tokens[idx] = tok
					errs[idx] = err
				} else {
					acc, rel, err := AcquireProviderAccountForModel(context.Background(), ProviderAntigravity, "antigravity/gemini-4-carbon-high", "sf-worker")
					if rel != nil {
						rel()
					}
					if acc != nil {
						tokens[idx] = acc.AccessToken
					}
					errs[idx] = err
				}
			}(i)
		}

		close(startBarrier)
		wg.Wait()

		for i := 0; i < workers; i++ {
			if errs[i] != nil {
				t.Fatalf("worker %d returned error: %v", i, errs[i])
			}
			if tokens[i] != "ya29.singleflight-fresh-token" {
				t.Fatalf("worker %d got token %q, want ya29.singleflight-fresh-token", i, tokens[i])
			}
		}
		if hits := googleRefreshHits.Load(); hits != 1 {
			t.Fatalf("expected exactly 1 outbound Google OAuth refresh call across 20 concurrent requests, got %d", hits)
		}
	})

	t.Run("ClaudeCode_20_Concurrent_Callers", func(t *testing.T) {
		const workers = 20
		startBarrier := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(workers)
		tokens := make([]string, workers)
		errs := make([]error, workers)

		for i := 0; i < workers; i++ {
			go func(idx int) {
				defer wg.Done()
				<-startBarrier
				acc, rel, err := AcquireProviderAccountForModel(context.Background(), ProviderClaudeCode, "claude-code/claude-opus-4-6-thinking", "sf-claude")
				if rel != nil {
					rel()
				}
				if acc != nil {
					tokens[idx] = acc.AccessToken
				}
				errs[idx] = err
			}(i)
		}

		close(startBarrier)
		wg.Wait()

		for i := 0; i < workers; i++ {
			if errs[i] != nil {
				t.Fatalf("claude worker %d returned error: %v", i, errs[i])
			}
			if tokens[i] != "sk-ant-oat01-singleflight-fresh" {
				t.Fatalf("claude worker %d got token %q, want sk-ant-oat01-singleflight-fresh", i, tokens[i])
			}
		}
		if hits := claudeRefreshHits.Load(); hits != 1 {
			t.Fatalf("expected exactly 1 outbound Claude OAuth refresh call across 20 concurrent requests, got %d", hits)
		}
	})
}

func TestProactivePreExpiryRefreshReturnsSub2msAndPersistsInBackground(t *testing.T) {
	tmpCfg := filepath.Join(t.TempDir(), "gateway_config.json")
	t.Setenv("GATEWAY_CONFIG_FILE", tmpCfg)
	resetAuthCacheForTest(t)

	origGoogleEndpoint := OAuthTokenEndpoint
	t.Cleanup(func() {
		WaitForPendingOAuthRefreshes(2 * time.Second)
		OAuthTokenEndpoint = origGoogleEndpoint
		FlushPendingConfigSave()
		resetAuthCacheForTest(t)
	})

	var proactiveHits atomic.Int32
	mockGoogleOAuth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proactiveHits.Add(1)
		time.Sleep(85 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "ya29.proactive-background-fresh",
			"refresh_token": "1//proactive-rt-2",
			"expires_in":    3600,
			"token_type":    "Bearer",
		})
	}))
	defer mockGoogleOAuth.Close()
	OAuthTokenEndpoint = mockGoogleOAuth.URL

	// Expires in 200s: still valid (>60s safety margin), inside 300s proactive pre-expiry window.
	if _, err := UpsertConnectedAccount(ConnectedAccount{
		Provider:     ProviderAntigravity,
		Email:        "proactive@gmail.com",
		AccessToken:  "ya29.still-valid-instant",
		RefreshToken: "1//proactive-rt-1",
		ProjectID:    "proj-proactive",
		ExpiresAt:    time.Now().Unix() + 200,
	}); err != nil {
		t.Fatalf("UpsertConnectedAccount failed: %v", err)
	}
	// Warm in-memory config cache so we measure pure token path latency.
	_ = LoadSavedAuthConfig()

	start := time.Now()
	tok, _, email, err := GetValidAccountToken(context.Background())
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("GetValidAccountToken failed: %v", err)
	}
	if tok != "ya29.still-valid-instant" || email != "proactive@gmail.com" {
		t.Fatalf("unexpected instant token=%q email=%q", tok, email)
	}
	if elapsed >= 2*time.Millisecond {
		t.Fatalf("expected proactive pre-expiry token path to return in <2ms, took %v", elapsed)
	}

	// Additional concurrent callers during the 85ms background refresh must also return immediately and deduplicate.
	for i := 0; i < 8; i++ {
		cTok, _, _, cErr := GetValidAccountToken(context.Background())
		if cErr != nil || cTok != "ya29.still-valid-instant" {
			t.Fatalf("concurrent caller %d unexpected result tok=%q err=%v", i, cTok, cErr)
		}
	}

	if !WaitForPendingOAuthRefreshes(2 * time.Second) {
		t.Fatalf("timed out waiting for proactive background OAuth refresh")
	}
	if hits := proactiveHits.Load(); hits != 1 {
		t.Fatalf("expected exactly 1 background proactive refresh HTTP call, got %d", hits)
	}

	newTok, _, _, err := GetValidAccountToken(context.Background())
	if err != nil {
		t.Fatalf("post-refresh GetValidAccountToken failed: %v", err)
	}
	if newTok != "ya29.proactive-background-fresh" {
		t.Fatalf("expected persisted proactive token ya29.proactive-background-fresh, got %q", newTok)
	}
}

func TestZeroDeadHopProviderPruningSkipsUnconfiguredFallbacksAndHonorsEnvAndRAGGuard(t *testing.T) {
	tmpCfg := filepath.Join(t.TempDir(), "gateway_config.json")
	t.Setenv("GATEWAY_CONFIG_FILE", tmpCfg)
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "")
	t.Setenv("ALIBABA_API_KEY", "")
	t.Setenv("AGY_OAUTH_TOKEN", "")
	resetAuthCacheForTest(t)
	ResetCCPAModelCaches()
	GetGlobalCache().Clear()
	ClearLogs()

	t.Cleanup(func() {
		ResetCCPAModelCaches()
		GetGlobalCache().Clear()
		ClearLogs()
		FlushPendingConfigSave()
		resetAuthCacheForTest(t)
	})

	if _, err := UpsertConnectedAccount(ConnectedAccount{
		Provider:    ProviderAntigravity,
		Email:       "prune-only-agy@gmail.com",
		AccessToken: "ya29.prune-only-agy",
		ProjectID:   "proj-prune",
		ExpiresAt:   time.Now().Unix() + 3600,
	}); err != nil {
		t.Fatalf("UpsertConnectedAccount failed: %v", err)
	}
	_ = SaveVectorDBConfig(VectorDBConfig{
		Enabled:       false,
		AutoInjectRAG: false,
	})

	var ccpaHits atomic.Int32
	mockCCPA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ccpaHits.Add(1)
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error":{"code":502,"message":"upstream bad gateway"}}`))
	}))
	defer mockCCPA.Close()

	upstream := NewUpstream(GatewayConfig{
		CCPAEndpoint: mockCCPA.URL,
		FallbackModels: []string{
			"claude-code/claude-opus-4-6-thinking",
			"alibaba/qwen3-coder-plus",
		},
	})

	if !upstream.isProviderConfigured(ProviderAntigravity, "Bearer sk-omni-employee") {
		t.Fatalf("expected ProviderAntigravity to be configured")
	}
	if upstream.isProviderConfigured(ProviderClaudeCode, "Bearer sk-omni-employee") {
		t.Fatalf("expected ProviderClaudeCode to be unconfigured")
	}
	if upstream.isProviderConfigured(ProviderAlibaba, "Bearer sk-omni-employee") {
		t.Fatalf("expected ProviderAlibaba to be unconfigured")
	}

	resolved := ResolveModel("antigravity/gemini-4-carbon-high", []string{
		"claude-code/claude-opus-4-6-thinking",
		"alibaba/qwen3-coder-plus",
	})
	req := OpenAIChatRequest{
		Model: "antigravity/gemini-4-carbon-high",
		Messages: []OpenAIChatMessage{
			{Role: "user", Content: json.RawMessage(`"Check dead hop pruning"`)},
		},
	}
	rec := httptest.NewRecorder()
	_ = upstream.HandleChatCompletion(context.Background(), rec, req, resolved, "Bearer sk-omni-employee")

	if rec.Header().Get("X-Omni-Gateway-Vector-Matches") != "" {
		t.Fatalf("expected Vector RAG injection to be skipped when disabled")
	}
	for _, entry := range GetRecentLogs(50, "", false) {
		if entry.Provider == ProviderClaudeCode || entry.Provider == ProviderAlibaba {
			t.Fatalf("unconfigured fallback provider %s should have been pruned in 0ns, but logged: %+v", entry.Provider, entry)
		}
	}

	// Verify index == 0 primary unconfigured provider behavior is 100% preserved (returns 401 authentication_required),
	// while unconfigured index > 0 fallbacks are pruned in 0ns without hitting mockCCPA.
	_, _ = RemoveConnectedAccount("prune-only-agy@gmail.com")
	hitsBeforePrimary := ccpaHits.Load()
	alibabaPrimary := ResolveModel("alibaba/qwen3.8-max", nil)
	recPrimary := httptest.NewRecorder()
	_ = upstream.HandleChatCompletion(context.Background(), recPrimary, OpenAIChatRequest{
		Model:    "alibaba/qwen3.8-max",
		Messages: []OpenAIChatMessage{{Role: "user", Content: json.RawMessage(`"direct primary test"`)}},
	}, alibabaPrimary, "Bearer sk-omni-employee")
	if recPrimary.Code != http.StatusUnauthorized {
		t.Fatalf("expected HTTP 401 for unconfigured index==0 primary provider, got %d", recPrimary.Code)
	}
	if ccpaHits.Load() != hitsBeforePrimary {
		t.Fatalf("expected unconfigured Antigravity index>0 fallbacks to be pruned without hitting mockCCPA")
	}

	// Verify env var credentials immediately enable fallback providers.
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "sk-ant-oat01-env")
	t.Setenv("ALIBABA_API_KEY", "sk-dashscope-env")
	if !upstream.isProviderConfigured(ProviderClaudeCode, "") {
		t.Fatalf("expected CLAUDE_CODE_OAUTH_TOKEN env to mark ProviderClaudeCode configured")
	}
	if !upstream.isProviderConfigured(ProviderAlibaba, "") {
		t.Fatalf("expected ALIBABA_API_KEY env to mark ProviderAlibaba configured")
	}
}

type mockPGState struct {
	mu                 sync.Mutex
	batchInsertQueries []string
	batchRowCounts     []int
	deletedAccountArgs []string
	missingAccountTbl  bool
}

type mockPGDriver struct {
	state *mockPGState
}

type mockPGConn struct {
	state *mockPGState
}

type mockPGTx struct{}

func (mockPGTx) Commit() error   { return nil }
func (mockPGTx) Rollback() error { return nil }

func (d *mockPGDriver) Open(_ string) (driver.Conn, error) {
	return &mockPGConn{state: d.state}, nil
}

func (c *mockPGConn) Prepare(query string) (driver.Stmt, error) {
	return nil, errors.New("prepare not used")
}

func (c *mockPGConn) Close() error { return nil }

func (c *mockPGConn) Begin() (driver.Tx, error) {
	return mockPGTx{}, nil
}

func (c *mockPGConn) BeginTx(_ context.Context, _ driver.TxOptions) (driver.Tx, error) {
	return mockPGTx{}, nil
}

func (c *mockPGConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	qTrim := strings.TrimSpace(query)
	c.state.mu.Lock()
	defer c.state.mu.Unlock()

	if strings.HasPrefix(qTrim, "INSERT INTO omni_prompt_logs") {
		c.state.batchInsertQueries = append(c.state.batchInsertQueries, qTrim)
		c.state.batchRowCounts = append(c.state.batchRowCounts, len(args)/20)
		return driver.RowsAffected(int64(len(args) / 20)), nil
	}
	if strings.HasPrefix(qTrim, "DELETE FROM omni_accounts") {
		if len(args) > 0 {
			c.state.deletedAccountArgs = append(c.state.deletedAccountArgs, fmt.Sprint(args[0].Value))
		}
		if c.state.missingAccountTbl {
			return nil, errors.New(`pq: relation "omni_accounts" does not exist`)
		}
		return driver.RowsAffected(1), nil
	}
	return driver.RowsAffected(0), nil
}

var (
	registerMockPGOnce sync.Once
	globalMockPGState  = &mockPGState{}
)

func openMockPostgresForTest(t *testing.T) *sql.DB {
	t.Helper()
	registerMockPGOnce.Do(func() {
		sql.Register("omni_mock_pg", &mockPGDriver{state: globalMockPGState})
	})
	db, err := sql.Open("omni_mock_pg", "mock://omni")
	if err != nil {
		t.Fatalf("sql.Open(omni_mock_pg) failed: %v", err)
	}
	db.SetMaxOpenConns(16)
	db.SetMaxIdleConns(8)
	db.SetConnMaxLifetime(15 * time.Minute)
	return db
}

func TestBatchPostgresPromptLogWorkerPoolAndDeleteAccountFromPostgres(t *testing.T) {
	// 1. Deterministic SQL builder verification (1 row & 32 rows)
	singleSQL, singleArgs := buildBatchInsertPromptLogsSQL([]GatewayLogEntry{
		{
			ID:             "log-1",
			Provider:       ProviderAntigravity,
			Account:        "ultra@gmail.com\x00",
			RequestedModel: "antigravity/gemini-4-carbon-high",
			ResolvedModel:  "antigravity/gemini-3.8-flash-high [gemini-3.8-flash]",
			StatusCode:     200,
			TotalTokens:    512,
		},
	})
	if len(singleArgs) != 20 || !strings.Contains(singleSQL, "($1,$2,") || !strings.Contains(singleSQL, "$20)") || !strings.Contains(singleSQL, "ON CONFLICT (id) DO NOTHING") {
		t.Fatalf("unexpected 1-row batch SQL (args=%d): %s", len(singleArgs), singleSQL)
	}
	if strings.Contains(fmt.Sprint(singleArgs[3]), "\x00") {
		t.Fatalf("expected NUL byte sanitization in account arg, got %q", singleArgs[3])
	}

	batch32 := make([]GatewayLogEntry, promptLogBatchSize)
	for i := range batch32 {
		batch32[i] = GatewayLogEntry{
			ID:             fmt.Sprintf("batch-row-%d", i),
			Provider:       ProviderAntigravity,
			RequestedModel: "antigravity/gemini-4-carbon-high",
			ResolvedModel:  "antigravity/gemini-3.8-flash-high",
			StatusCode:     200,
		}
	}
	sql32, args32 := buildBatchInsertPromptLogsSQL(batch32)
	if len(args32) != 32*20 || !strings.Contains(sql32, "$640)") {
		t.Fatalf("expected 640 args and $640 placeholder for 32-row batch, got args=%d", len(args32))
	}

	// 2. End-to-end bounded queue + batch worker under 65 concurrent InsertPromptLogAsync calls
	tmpCfg := filepath.Join(t.TempDir(), "gateway_config.json")
	t.Setenv("GATEWAY_CONFIG_FILE", tmpCfg)
	resetAuthCacheForTest(t)

	_ = FlushPromptLogs(2 * time.Second)
	globalMockPGState.mu.Lock()
	globalMockPGState.batchInsertQueries = nil
	globalMockPGState.batchRowCounts = nil
	globalMockPGState.deletedAccountArgs = nil
	globalMockPGState.missingAccountTbl = false
	globalMockPGState.mu.Unlock()

	mockDB := openMockPostgresForTest(t)
	pgMu.Lock()
	origDB := pgDB
	origConn := pgConnected
	pgDB = mockDB
	pgConnected = true
	pgMu.Unlock()

	t.Cleanup(func() {
		_ = FlushPromptLogs(2 * time.Second)
		pgMu.Lock()
		pgDB = origDB
		pgConnected = origConn
		pgMu.Unlock()
		_ = mockDB.Close()
		FlushPendingConfigSave()
		resetAuthCacheForTest(t)
	})

	const totalConcurrentLogs = 65
	var wg sync.WaitGroup
	wg.Add(totalConcurrentLogs)
	for i := 0; i < totalConcurrentLogs; i++ {
		go func(idx int) {
			defer wg.Done()
			InsertPromptLogAsync(GatewayLogEntry{
				ID:             fmt.Sprintf("conc-log-%d", idx),
				Provider:       ProviderAntigravity,
				Account:        "thuctapsinh316@gmail.com",
				RequestedModel: "antigravity/gemini-4-carbon-high",
				ResolvedModel:  "antigravity/gemini-3.8-flash-high [gemini-3.8-flash]",
				StatusCode:     200,
				LatencyMs:      420,
				TotalTokens:    128,
			})
		}(i)
	}
	wg.Wait()

	if err := FlushPromptLogs(3 * time.Second); err != nil {
		t.Fatalf("FlushPromptLogs failed: %v", err)
	}

	globalMockPGState.mu.Lock()
	rowCounts := append([]int(nil), globalMockPGState.batchRowCounts...)
	globalMockPGState.mu.Unlock()

	sumRows := 0
	maxBatchSeen := 0
	for _, c := range rowCounts {
		sumRows += c
		if c > maxBatchSeen {
			maxBatchSeen = c
		}
		if c > promptLogBatchSize {
			t.Fatalf("batch size %d exceeded promptLogBatchSize (%d)", c, promptLogBatchSize)
		}
	}
	if sumRows != totalConcurrentLogs {
		t.Fatalf("expected all %d concurrent logs to be batch-inserted, got %d (batches=%v)", totalConcurrentLogs, sumRows, rowCounts)
	}
	if len(rowCounts) >= totalConcurrentLogs {
		t.Fatalf("expected multi-row batching (<%d queries), got %d queries: %v", totalConcurrentLogs, len(rowCounts), rowCounts)
	}

	// 3. Verify RemoveConnectedAccount removes from both gateway_config.json and PostgreSQL (including missing-table guard)
	if _, err := UpsertConnectedAccount(ConnectedAccount{
		ID:          "antigravity-cloudflarepro789@gmail.com",
		Provider:    ProviderAntigravity,
		Email:       "cloudflarepro789@gmail.com",
		AccessToken: "ya29.banned-tos",
	}); err != nil {
		t.Fatalf("UpsertConnectedAccount failed: %v", err)
	}

	afterRemove, err := RemoveConnectedAccount("cloudflarepro789@gmail.com")
	if err != nil {
		t.Fatalf("RemoveConnectedAccount failed: %v", err)
	}
	if len(afterRemove.Accounts) != 0 {
		t.Fatalf("expected 0 accounts in gateway_config.json after RemoveConnectedAccount, got %d", len(afterRemove.Accounts))
	}

	globalMockPGState.mu.Lock()
	delArgs := append([]string(nil), globalMockPGState.deletedAccountArgs...)
	globalMockPGState.missingAccountTbl = true
	globalMockPGState.mu.Unlock()

	if len(delArgs) == 0 {
		t.Fatalf("expected DeleteAccountFromPostgres SQL execution on RemoveConnectedAccount")
	}
	if err := DeleteAccountFromPostgres("cloudflarepro789@gmail.com"); err != nil {
		t.Fatalf("expected DeleteAccountFromPostgres to ignore missing omni_accounts table gracefully, got: %v", err)
	}
}

func TestCCPAModelVersionHeaderLiveActiveRoleHonestLogsAndMultiAccountFlagshipFailover(t *testing.T) {
	tmpCfg := filepath.Join(t.TempDir(), "gateway_config.json")
	t.Setenv("GATEWAY_CONFIG_FILE", tmpCfg)
	resetAuthCacheForTest(t)
	ResetCCPAModelCaches()
	ClearAccountModelRateLimits("")
	GetGlobalCache().Clear()
	ClearLogs()

	t.Cleanup(func() {
		ResetCCPAModelCaches()
		ClearAccountModelRateLimits("")
		GetGlobalCache().Clear()
		ClearLogs()
		FlushPendingConfigSave()
		resetAuthCacheForTest(t)
	})

	t.Run("Live_DefaultCCPAEndpoint_ConnectedAccount_ModelTruthMapping", func(t *testing.T) {
		exhaustedClaude := 0
		freshClaude := 15
		if _, err := UpsertConnectedAccount(ConnectedAccount{
			Provider:            ProviderAntigravity,
			Email:               "thuctapsinh316@gmail.com",
			AccessToken:         "ya29.ultra-tok",
			ProjectID:           "aicode-consumers",
			ExpiresAt:           time.Now().Unix() + 3600,
			WeeklyClaudePercent: &exhaustedClaude,
		}); err != nil {
			t.Fatalf("UpsertConnectedAccount(ultra) failed: %v", err)
		}
		if _, err := UpsertConnectedAccount(ConnectedAccount{
			Provider:            ProviderAntigravity,
			Email:               "claude-fresh@gmail.com",
			AccessToken:         "ya29.claude-fresh-tok",
			ProjectID:           "aicode-consumers",
			ExpiresAt:           time.Now().Unix() + 3600,
			WeeklyClaudePercent: &freshClaude,
		}); err != nil {
			t.Fatalf("UpsertConnectedAccount(claude-fresh) failed: %v", err)
		}

		// Carbon High on connected account preserves gemini-4-carbon-high at index 0 and NEVER injects Claude models
		carbonCands := buildCCPACandidatesWithCache(DefaultCCPAEndpoint, "thuctapsinh316@gmail.com", ResolveModel("antigravity/gemini-4-carbon-high", nil), false)
		if len(carbonCands) < 2 || carbonCands[0] != "gemini-4-carbon-high" {
			t.Fatalf("expected connected Carbon High = [gemini-4-carbon-high, ...], got %v", carbonCands)
		}
		for _, c := range carbonCands {
			if strings.HasPrefix(c, "claude-") {
				t.Fatalf("expected zero Claude models in Gemini Carbon candidates, found %q in %v", c, carbonCands)
			}
			if isVirtualCCPAIDEAlias(c) {
				t.Fatalf("expected zero 404/503 virtual aliases on DefaultCCPAEndpoint, found %q in %v", c, carbonCands)
			}
		}
		carbonFresh := buildCCPACandidatesWithCache(DefaultCCPAEndpoint, "claude-fresh@gmail.com", ResolveModel("antigravity/gemini-4-carbon-high", nil), false)
		for _, c := range carbonFresh {
			if strings.HasPrefix(c, "claude-") {
				t.Fatalf("expected zero Claude models in Gemini Carbon even on fresh Claude account, found %q in %v", c, carbonFresh)
			}
		}

		// Gemini 3.1 Pro High maps primary to gemini-3.8-flash-high, NEVER to Claude
		proCands := buildCCPACandidatesWithCache(DefaultCCPAEndpoint, "thuctapsinh316@gmail.com", ResolveModel("antigravity/gemini-3.1-pro-high", nil), false)
		if len(proCands) < 1 || proCands[0] != "gemini-3.8-flash-high" {
			t.Fatalf("expected connected Gemini 3.1 Pro High = [gemini-3.8-flash-high, ...], got %v", proCands)
		}
		for _, c := range proCands {
			if strings.HasPrefix(c, "claude-") {
				t.Fatalf("expected zero Claude models in Gemini 3.1 Pro candidates, found %q in %v", c, proCands)
			}
		}

		// Claude Opus Thinking preserves claude-opus-4-6-thinking on fresh Claude account, but is pruned on exhausted Claude account
		opusFresh := buildCCPACandidatesWithCache(DefaultCCPAEndpoint, "claude-fresh@gmail.com", ResolveModel("antigravity/claude-opus-4-6-thinking", nil), false)
		if len(opusFresh) < 1 || opusFresh[0] != "claude-opus-4-6-thinking" {
			t.Fatalf("expected Claude Opus on fresh Claude account = [claude-opus-4-6-thinking, ...], got %v", opusFresh)
		}
		opusExhausted := buildCCPACandidatesWithCache(DefaultCCPAEndpoint, "thuctapsinh316@gmail.com", ResolveModel("antigravity/claude-opus-4-6-thinking", nil), false)
		for _, c := range opusExhausted {
			if strings.HasPrefix(c, "claude-") {
				t.Fatalf("expected Claude models to be pruned on Claude-exhausted account, found %q in %v", c, opusExhausted)
			}
		}
	})

	t.Run("MultiAccount_FlagshipFailover_Before_FlashDowngrade_And_HonestModelVersion", func(t *testing.T) {
		resetAuthCacheForTest(t)
		ResetCCPAModelCaches()
		ClearAccountModelRateLimits("")
		ClearLogs()

		for _, a := range []ConnectedAccount{
			{
				Provider:    ProviderAntigravity,
				Email:       "acc1-opus429@gmail.com",
				AccessToken: "ya29.acc1-opus429",
				ProjectID:   "proj-1",
				Priority:    100,
				ExpiresAt:   time.Now().Unix() + 3600,
			},
			{
				Provider:    ProviderAntigravity,
				Email:       "acc2-opus200@gmail.com",
				AccessToken: "ya29.acc2-opus200",
				ProjectID:   "proj-2",
				Priority:    90,
				ExpiresAt:   time.Now().Unix() + 3600,
			},
		} {
			if _, err := UpsertConnectedAccount(a); err != nil {
				t.Fatalf("UpsertConnectedAccount(%s) failed: %v", a.Email, err)
			}
		}
		_, _ = SetRoutingStrategy(StrategyFillFirst)

		var flashDowngradeCalls atomic.Int32
		var allAccountsExhaustedOpus atomic.Bool
		var observedActiveRole atomic.Value

		mockCCPA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var payload ccpaRequestPayload
			_ = json.NewDecoder(r.Body).Decode(&payload)
			auth := strings.TrimSpace(r.Header.Get("Authorization"))

			if isVirtualCCPAIDEAlias(payload.Model) || strings.HasPrefix(payload.Model, "gemini-4-") {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			if payload.Model == "gemini-3-flash" {
				flashDowngradeCalls.Add(1)
			}

			if payload.Model == "claude-opus-4-6-thinking" || payload.Model == "claude-sonnet-4-6" {
				if auth == "Bearer ya29.acc1-opus429" || allAccountsExhaustedOpus.Load() {
					w.WriteHeader(http.StatusTooManyRequests)
					_, _ = w.Write([]byte(`{"error":{"code":429,"status":"RESOURCE_EXHAUSTED","message":"QUOTA_EXHAUSTED"}}`))
					return
				}
			}

			modelVer := payload.Model
			if payload.Model == "claude-opus-4-6-thinking" {
				modelVer = "claude-opus-4-6-202605"
			} else if payload.Model == "gemini-3.8-flash-high" {
				modelVer = "gemini-3.8-flash"
			}

			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			fl, _ := w.(http.Flusher)
			_, _ = fmt.Fprintf(w, "data: {\"response\":{\"modelVersion\":%q,\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"chunk-1 \"}]}}]}}\n\n", modelVer)
			if fl != nil {
				fl.Flush()
			}
			// Give client scanner a moment to process chunk 1 and update live active role before chunk 2 finishes.
			time.Sleep(15 * time.Millisecond)
			if auth == "Bearer ya29.acc2-opus200" {
				observedActiveRole.Store(GetAccountActiveRole("acc2-opus200@gmail.com"))
			}
			_, _ = fmt.Fprintf(w, "data: {\"response\":{\"modelVersion\":%q,\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"chunk-2\"}]},\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":12,\"candidatesTokenCount\":8,\"totalTokenCount\":20}}}\n\ndata: [DONE]\n\n", modelVer)
		}))
		defer mockCCPA.Close()

		upstream := NewUpstream(GatewayConfig{CCPAEndpoint: mockCCPA.URL})
		opusRes := ResolveModel("antigravity/claude-opus-4-6-thinking", nil)

		// 1. Unpinned flagship request hits acc1 (priority 100) -> gets 429 on Opus -> MUST fail over to acc2 on Opus instead of downgrading to Flash on acc1!
		rec1 := httptest.NewRecorder()
		err := upstream.HandleChatCompletion(context.Background(), rec1, OpenAIChatRequest{
			Model:    "antigravity/claude-opus-4-6-thinking",
			Stream:   true,
			Messages: []OpenAIChatMessage{{Role: "user", Content: json.RawMessage(`"Flagship failover streaming test"`)}},
		}, opusRes, "")
		if err != nil {
			t.Fatalf("HandleChatCompletion failed: %v", err)
		}
		if gotAcc := rec1.Header().Get("X-Omni-Gateway-Account"); gotAcc != "acc2-opus200@gmail.com" {
			t.Fatalf("expected failover to healthy flagship account acc2-opus200@gmail.com, got %q", gotAcc)
		}
		if gotMod := rec1.Header().Get("X-Omni-Gateway-Resolved-Model"); gotMod != "claude-opus-4-6-thinking" {
			t.Fatalf("expected flagship model claude-opus-4-6-thinking on acc2, got %q", gotMod)
		}
		if gotVer := rec1.Header().Get("X-Omni-Gateway-Upstream-Model-Version"); gotVer != "claude-opus-4-6-202605" {
			t.Fatalf("expected X-Omni-Gateway-Upstream-Model-Version=claude-opus-4-6-202605, got %q", gotVer)
		}
		if flashDowngradeCalls.Load() != 0 {
			t.Fatalf("expected 0 silent downgrades to gemini-3-flash while acc2 had flagship quota, got %d", flashDowngradeCalls.Load())
		}
		if roleVal, _ := observedActiveRole.Load().(string); !strings.Contains(roleVal, "claude-opus-4-6-thinking") {
			t.Fatalf("expected live Dashboard active role to show real backend model, got %q", roleVal)
		}

		// Verify acc1 was NOT globally cooled down (still 0 CooldownUntil so it can serve Flash requests!)
		for _, a := range LoadSavedAuthConfig().Accounts {
			if a.Email == "acc1-opus429@gmail.com" && a.CooldownUntil != 0 {
				t.Fatalf("expected acc1 CooldownUntil == 0 on flagship-only 429 failover, got %d", a.CooldownUntil)
			}
		}

		// Verify GatewayLogEntry.ResolvedModel honestly recorded actual CCPA model + modelVersion instead of lying!
		logs := GetRecentLogs(5, "", false)
		if len(logs) == 0 {
			t.Fatalf("expected recorded GatewayLogEntry")
		}
		if logs[0].ResolvedModel != "antigravity/claude-opus-4-6-thinking [claude-opus-4-6-202605]" {
			t.Fatalf("expected honest GatewayLogEntry.ResolvedModel, got %q", logs[0].ResolvedModel)
		}

		// 2. When ALL accounts have exhausted Opus/Sonnet, verify graceful fallback to gemini-3.8-flash-high (before gemini-3-flash!)
		allAccountsExhaustedOpus.Store(true)
		ClearLogs()
		argonRes := ResolveModel("antigravity/gemini-4-argon-high", nil)
		rec2 := httptest.NewRecorder()
		err = upstream.HandleChatCompletion(context.Background(), rec2, OpenAIChatRequest{
			Model:    "antigravity/gemini-4-argon-high",
			Stream:   false,
			Messages: []OpenAIChatMessage{{Role: "user", Content: json.RawMessage(`"All accounts Opus exhausted fallback test"`)}},
		}, argonRes, "")
		if err != nil {
			t.Fatalf("all-exhausted fallback HandleChatCompletion failed: %v", err)
		}
		if gotMod := rec2.Header().Get("X-Omni-Gateway-Resolved-Model"); gotMod != "gemini-3.8-flash-high" {
			t.Fatalf("expected graceful fallback to gemini-3.8-flash-high before gemini-3-flash, got %q", gotMod)
		}
		if gotVer := rec2.Header().Get("X-Omni-Gateway-Upstream-Model-Version"); gotVer != "gemini-3.8-flash" {
			t.Fatalf("expected X-Omni-Gateway-Upstream-Model-Version=gemini-3.8-flash, got %q", gotVer)
		}
		logs2 := GetRecentLogs(5, "", false)
		if len(logs2) == 0 || logs2[0].ResolvedModel != "antigravity/gemini-3.8-flash-high [gemini-3.8-flash]" {
			t.Fatalf("expected honest fallback GatewayLogEntry.ResolvedModel, got %+v", logs2)
		}
	})
}
