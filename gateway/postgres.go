package gateway

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	_ "github.com/lib/pq"
)

type auditCtxKeyType string

const (
	auditContextKey        auditCtxKeyType = "omni_audit_ctx"
	MaxPostgresPromptLogs  int             = 50000
	pgPruneEveryInserts    uint64          = 500
	promptLogQueueCapacity int             = 2048
	promptLogBatchSize     int             = 32
	promptLogFlushInterval                 = 200 * time.Millisecond
)

// AuditContext carries employee API key metadata, prompt text, and AI response text across request execution.
type AuditContext struct {
	mu               sync.Mutex
	APIKeyID         string
	EmployeeName     string
	MaskedKey        string
	PromptText       string
	ResponseText     string
	PromptTokens     int
	CompletionTokens int
}

// WithAuditContext attaches an AuditContext populated with employee API key metadata and prompt text.
func WithAuditContext(ctx context.Context, key *GatewayAPIKey, req OpenAIChatRequest) context.Context {
	ac := &AuditContext{
		PromptText: extractPromptTextFromMessages(req.Messages),
	}
	if key != nil {
		ac.APIKeyID = key.ID
		ac.EmployeeName = key.Name
		ac.MaskedKey = key.MaskedKey
	} else {
		ac.EmployeeName = "Admin / Local"
	}
	return context.WithValue(ctx, auditContextKey, ac)
}

// GetAuditContext retrieves the AuditContext from context if present.
func GetAuditContext(ctx context.Context) *AuditContext {
	if ctx == nil {
		return nil
	}
	if ac, ok := ctx.Value(auditContextKey).(*AuditContext); ok {
		return ac
	}
	return nil
}

// SetAuditResponse records the AI assistant response and token breakdown into the context's AuditContext.
func SetAuditResponse(ctx context.Context, responseText string, promptTokens int, completionTokens int) {
	ac := GetAuditContext(ctx)
	if ac == nil {
		return
	}
	ac.mu.Lock()
	defer ac.mu.Unlock()
	if responseText != "" {
		ac.ResponseText = TruncateUTF8(responseText, 16000)
	}
	if promptTokens > 0 {
		ac.PromptTokens = promptTokens
	}
	if completionTokens > 0 {
		ac.CompletionTokens = completionTokens
	}
}

// EnrichLogEntryFromContext populates employee identity, prompt text, and response text on a GatewayLogEntry.
func EnrichLogEntryFromContext(ctx context.Context, entry *GatewayLogEntry, req OpenAIChatRequest) {
	if entry == nil {
		return
	}
	if ac := GetAuditContext(ctx); ac != nil {
		ac.mu.Lock()
		if entry.APIKeyID == "" {
			entry.APIKeyID = ac.APIKeyID
		}
		if entry.EmployeeName == "" {
			entry.EmployeeName = ac.EmployeeName
		}
		if entry.MaskedKey == "" {
			entry.MaskedKey = ac.MaskedKey
		}
		if entry.PromptText == "" {
			entry.PromptText = ac.PromptText
		}
		if entry.ResponseText == "" {
			entry.ResponseText = ac.ResponseText
		}
		if entry.PromptTokens == 0 {
			entry.PromptTokens = ac.PromptTokens
		}
		if entry.CompletionTokens == 0 {
			entry.CompletionTokens = ac.CompletionTokens
		}
		ac.mu.Unlock()
	}
	if entry.PromptText == "" && len(req.Messages) > 0 {
		entry.PromptText = extractPromptTextFromMessages(req.Messages)
	}
	if entry.EmployeeName == "" {
		entry.EmployeeName = "Admin / Local"
	}
}

func extractPromptTextFromMessages(messages []OpenAIChatMessage) string {
	if len(messages) == 0 {
		return ""
	}
	var sb strings.Builder
	// Keep the last 6 messages or up to 8000 runes so the admin sees full context + the exact user prompt
	startIdx := 0
	if len(messages) > 6 {
		startIdx = len(messages) - 6
	}
	for i := startIdx; i < len(messages); i++ {
		m := messages[i]
		txt := strings.TrimSpace(m.ContentText())
		if txt == "" {
			continue
		}
		role := strings.ToUpper(strings.TrimSpace(m.Role))
		if role == "" {
			role = "USER"
		}
		if sb.Len() > 0 {
			sb.WriteString("\n\n")
		}
		sb.WriteString(fmt.Sprintf("[%s]: %s", role, txt))
	}
	return TruncateUTF8(sb.String(), 8000)
}

func truncateAuditText(s string, maxChars int) string {
	return TruncateUTF8(s, maxChars)
}

func computeKeySHA256Hex(rawKey string) string {
	rawKey = strings.TrimSpace(rawKey)
	if rawKey == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(rawKey))
	return hex.EncodeToString(sum[:])
}

func effectiveMaxPostgresLogs() int {
	if raw := strings.TrimSpace(os.Getenv("OMNI_PG_MAX_LOGS")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			return parsed
		}
	}
	return MaxPostgresPromptLogs
}

// escapeILIKE escapes PostgreSQL ILIKE/LIKE wildcard characters (\, %, _) for use with ESCAPE '\'.
func escapeILIKE(s string) string {
	return strings.NewReplacer(
		`\`, `\\`,
		`%`, `\%`,
		`_`, `\_`,
	).Replace(s)
}

// EmployeeUsageSummary aggregates request, token, and prompt metrics per employee API key.
type EmployeeUsageSummary struct {
	APIKeyID      string `json:"api_key_id"`
	EmployeeName  string `json:"employee_name"`
	MaskedKey     string `json:"masked_key"`
	TotalRequests int64  `json:"total_requests"`
	TotalTokens   int64  `json:"total_tokens"`
	ErrorCount    int64  `json:"error_count"`
	LastModel     string `json:"last_model"`
	LastPrompt    string `json:"last_prompt"`
	LastUsedAt    string `json:"last_used_at"`
}

var (
	pgMu                sync.RWMutex
	pgDB                *sql.DB
	pgConnected         bool
	pgActiveDSN         string
	pgServerVersion     string
	pgLastErr           string
	pgInsertCounter     atomic.Uint64
	promptLogQueue      = make(chan GatewayLogEntry, promptLogQueueCapacity)
	promptLogFlushReqs  = make(chan chan struct{}, 16)
	promptLogWorkerOnce sync.Once
	promptLogDropped    atomic.Uint64
)

// ResolveEffectivePostgresDSN returns the active PostgreSQL connection string from env or saved config.
func ResolveEffectivePostgresDSN() string {
	if envDSN := strings.TrimSpace(os.Getenv("DATABASE_URL")); envDSN != "" {
		return envDSN
	}
	if envDSN := strings.TrimSpace(os.Getenv("POSTGRES_DSN")); envDSN != "" {
		return envDSN
	}
	cfg := LoadSavedAuthConfig()
	return strings.TrimSpace(cfg.PostgresDSN)
}

// MaskPostgresDSN hides password credentials in a PostgreSQL DSN for safe display in the UI.
func MaskPostgresDSN(dsn string) string {
	dsn = strings.TrimSpace(dsn)
	if dsn == "" {
		return ""
	}
	u, err := url.Parse(dsn)
	if err != nil || u.User == nil {
		return dsn
	}
	if _, hasPwd := u.User.Password(); hasPwd {
		u.User = url.UserPassword(u.User.Username(), "******")
	}
	return u.String()
}

// InitPostgresDB connects to PostgreSQL, runs auto-migrations for omni_api_keys and omni_prompt_logs,
// and synchronizes API keys and Vector DB memories between local config and PostgreSQL.
func InitPostgresDB(dsn string) error {
	dsn = strings.TrimSpace(dsn)
	if dsn == "" {
		dsn = ResolveEffectivePostgresDSN()
	}
	if dsn == "" {
		_ = ClosePostgres()
		pgMu.Lock()
		pgLastErr = ""
		pgMu.Unlock()
		return nil
	}

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		pgMu.Lock()
		if pgDB == nil {
			pgConnected = false
		}
		pgLastErr = err.Error()
		pgMu.Unlock()
		return err
	}

	db.SetMaxOpenConns(16)
	db.SetMaxIdleConns(8)
	db.SetConnMaxLifetime(15 * time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		pgMu.Lock()
		if pgDB == nil {
			pgConnected = false
		}
		pgLastErr = err.Error()
		pgMu.Unlock()
		return err
	}

	if err := migrateSchema(ctx, db); err != nil {
		_ = db.Close()
		pgMu.Lock()
		if pgDB == nil {
			pgConnected = false
		}
		pgLastErr = err.Error()
		pgMu.Unlock()
		return err
	}

	var serverVer string
	_ = db.QueryRowContext(ctx, `SELECT current_setting('server_version')`).Scan(&serverVer)
	if idx := strings.IndexByte(serverVer, ' '); idx > 0 {
		serverVer = serverVer[:idx]
	}

	pgMu.Lock()
	if pgDB != nil {
		_ = pgDB.Close()
	}
	pgDB = db
	pgConnected = true
	pgActiveDSN = dsn
	pgServerVersion = strings.TrimSpace(serverVer)
	pgLastErr = ""
	pgMu.Unlock()

	log.Printf("[omniroute-postgres] Connected to PostgreSQL %s (%s) and verified schema", pgServerVersion, MaskPostgresDSN(dsn))

	ensurePromptLogWorker()

	// Bidirectional sync of API keys and Vector DB memories on startup
	syncAPIKeysBidirectional(db)
	InitVectorDBSchema(db)
	return nil
}

// ClosePostgres flushes pending prompt logs, gracefully closes the active PostgreSQL connection pool, and resets connection flags.
func ClosePostgres() error {
	_ = FlushPromptLogs(2 * time.Second)

	pgMu.Lock()
	db := pgDB
	pgDB = nil
	pgConnected = false
	pgActiveDSN = ""
	pgServerVersion = ""
	var closeErr error
	if db != nil {
		closeErr = db.Close()
	}
	pgMu.Unlock()

	resetPgVectorState()
	return closeErr
}

// ClearPromptLogsFromPostgres truncates all persisted prompt logs in PostgreSQL when connected.
func ClearPromptLogsFromPostgres() error {
	pgMu.RLock()
	connected := pgConnected
	db := pgDB
	pgMu.RUnlock()

	if !connected || db == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := db.ExecContext(ctx, `TRUNCATE TABLE omni_prompt_logs`); err != nil {
		if _, delErr := db.ExecContext(ctx, `DELETE FROM omni_prompt_logs`); delErr != nil {
			return err
		}
	}
	return nil
}

func migratePostgresTables(ctx context.Context, db *sql.DB) error {
	return migrateSchema(ctx, db)
}

func migrateSchema(ctx context.Context, db *sql.DB) error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS omni_api_keys (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL DEFAULT '',
			full_key TEXT NOT NULL DEFAULT '',
			masked_key TEXT NOT NULL DEFAULT '',
			key_hash TEXT NOT NULL DEFAULT '',
			admin_access BOOLEAN NOT NULL DEFAULT FALSE,
			allow_self_usage BOOLEAN NOT NULL DEFAULT TRUE,
			allow_shared_quota BOOLEAN NOT NULL DEFAULT TRUE,
			allow_local_usage_cmd BOOLEAN NOT NULL DEFAULT TRUE,
			total_requests BIGINT NOT NULL DEFAULT 0,
			total_tokens BIGINT NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL DEFAULT '',
			last_used_at TEXT NOT NULL DEFAULT ''
		);`,
		`ALTER TABLE omni_api_keys ADD COLUMN IF NOT EXISTS key_hash TEXT NOT NULL DEFAULT '';`,
		`CREATE INDEX IF NOT EXISTS idx_omni_api_keys_hash ON omni_api_keys(key_hash);`,
		`CREATE TABLE IF NOT EXISTS omni_prompt_logs (
			id TEXT PRIMARY KEY,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			timestamp_str TEXT NOT NULL DEFAULT '',
			api_key_id TEXT NOT NULL DEFAULT '',
			employee_name TEXT NOT NULL DEFAULT '',
			masked_key TEXT NOT NULL DEFAULT '',
			provider TEXT NOT NULL DEFAULT '',
			account TEXT NOT NULL DEFAULT '',
			requested_model TEXT NOT NULL DEFAULT '',
			resolved_model TEXT NOT NULL DEFAULT '',
			prompt_text TEXT NOT NULL DEFAULT '',
			response_text TEXT NOT NULL DEFAULT '',
			prompt_tokens INT NOT NULL DEFAULT 0,
			completion_tokens INT NOT NULL DEFAULT 0,
			total_tokens INT NOT NULL DEFAULT 0,
			tokens_saved INT NOT NULL DEFAULT 0,
			latency_ms BIGINT NOT NULL DEFAULT 0,
			status_code INT NOT NULL DEFAULT 200,
			is_error BOOLEAN NOT NULL DEFAULT FALSE,
			error_message TEXT NOT NULL DEFAULT '',
			error_detail TEXT NOT NULL DEFAULT ''
		);`,
		`ALTER TABLE omni_prompt_logs ADD COLUMN IF NOT EXISTS error_detail TEXT NOT NULL DEFAULT '';`,
		`CREATE INDEX IF NOT EXISTS idx_omni_prompt_logs_created_at ON omni_prompt_logs(created_at DESC);`,
		`CREATE INDEX IF NOT EXISTS idx_omni_prompt_logs_api_key_id ON omni_prompt_logs(api_key_id);`,
		`CREATE INDEX IF NOT EXISTS idx_omni_prompt_logs_employee ON omni_prompt_logs(employee_name);`,
		`CREATE INDEX IF NOT EXISTS idx_omni_logs_key_created ON omni_prompt_logs(api_key_id, created_at DESC);`,
		`CREATE INDEX IF NOT EXISTS idx_omni_logs_err_created ON omni_prompt_logs(is_error, created_at DESC);`,
		`CREATE INDEX IF NOT EXISTS idx_omni_logs_prov_created ON omni_prompt_logs(provider, created_at DESC);`,
	}

	for _, q := range queries {
		if _, err := db.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("migration failed: %w", err)
		}
	}
	return nil
}

func syncAPIKeysBidirectional(db *sql.DB) {
	if db == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	cfg := LoadSavedAuthConfig()
	changedLocal := false
	localByID := make(map[string]GatewayAPIKey, len(cfg.APIKeys))
	for i := range cfg.APIKeys {
		if cfg.APIKeys[i].Key != "" {
			computed := computeKeySHA256Hex(cfg.APIKeys[i].Key)
			if cfg.APIKeys[i].KeyHash != computed {
				cfg.APIKeys[i].KeyHash = computed
				changedLocal = true
			}
		}
		localByID[cfg.APIKeys[i].ID] = cfg.APIKeys[i]
	}

	rows, err := db.QueryContext(ctx, `
		SELECT id, name, full_key, masked_key, key_hash, admin_access, allow_self_usage, allow_shared_quota, allow_local_usage_cmd, total_requests, total_tokens, created_at, last_used_at
		FROM omni_api_keys ORDER BY created_at ASC
	`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var k GatewayAPIKey
			if scanErr := rows.Scan(
				&k.ID, &k.Name, &k.Key, &k.MaskedKey, &k.KeyHash,
				&k.AdminAccess, &k.AllowSelfUsage, &k.AllowSharedQuota, &k.AllowLocalUsageCmd,
				&k.TotalRequests, &k.TotalTokens, &k.CreatedAt, &k.LastUsedAt,
			); scanErr == nil {
				if k.Key != "" && k.KeyHash == "" {
					k.KeyHash = computeKeySHA256Hex(k.Key)
				}
				if existing, ok := localByID[k.ID]; !ok {
					cfg.APIKeys = append(cfg.APIKeys, k)
					localByID[k.ID] = k
					changedLocal = true
				} else if k.TotalRequests > existing.TotalRequests || k.TotalTokens > existing.TotalTokens || (existing.KeyHash == "" && k.KeyHash != "") {
					for i := range cfg.APIKeys {
						if cfg.APIKeys[i].ID == k.ID {
							if k.TotalRequests > cfg.APIKeys[i].TotalRequests {
								cfg.APIKeys[i].TotalRequests = k.TotalRequests
							}
							if k.TotalTokens > cfg.APIKeys[i].TotalTokens {
								cfg.APIKeys[i].TotalTokens = k.TotalTokens
							}
							if k.LastUsedAt != "" {
								cfg.APIKeys[i].LastUsedAt = k.LastUsedAt
							}
							if cfg.APIKeys[i].KeyHash == "" && k.KeyHash != "" {
								cfg.APIKeys[i].KeyHash = k.KeyHash
							}
						}
					}
					changedLocal = true
				}
			}
		}
	}

	if changedLocal {
		configMu.Lock()
		_ = saveAuthConfigLocked(cfg)
		configMu.Unlock()
	}

	// Ensure all local keys (including populated key_hash) are upserted into PostgreSQL
	for _, k := range cfg.APIKeys {
		upsertAPIKeyToPostgres(ctx, db, k)
	}
}

// UpsertAPIKeyToPostgres upserts a single GatewayAPIKey (with SHA-256 key_hash) into PostgreSQL when connected.
func UpsertAPIKeyToPostgres(k GatewayAPIKey) {
	pgMu.RLock()
	connected := pgConnected
	db := pgDB
	pgMu.RUnlock()
	if !connected || db == nil {
		return
	}
	go func(item GatewayAPIKey) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		upsertAPIKeyToPostgres(ctx, db, item)
	}(k)
}

func upsertAPIKeyToPostgres(ctx context.Context, db *sql.DB, k GatewayAPIKey) {
	if db == nil {
		return
	}
	keyHash := strings.TrimSpace(k.KeyHash)
	if k.Key != "" {
		keyHash = computeKeySHA256Hex(k.Key)
	}
	_, _ = db.ExecContext(ctx, `
		INSERT INTO omni_api_keys (
			id, name, full_key, masked_key, key_hash, admin_access, allow_self_usage, allow_shared_quota, allow_local_usage_cmd, total_requests, total_tokens, created_at, last_used_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
		ON CONFLICT (id) DO UPDATE SET
			name = EXCLUDED.name,
			full_key = CASE WHEN EXCLUDED.full_key <> '' THEN EXCLUDED.full_key ELSE omni_api_keys.full_key END,
			masked_key = CASE WHEN EXCLUDED.masked_key <> '' THEN EXCLUDED.masked_key ELSE omni_api_keys.masked_key END,
			key_hash = CASE WHEN EXCLUDED.key_hash <> '' THEN EXCLUDED.key_hash ELSE omni_api_keys.key_hash END,
			admin_access = EXCLUDED.admin_access,
			allow_self_usage = EXCLUDED.allow_self_usage,
			allow_shared_quota = EXCLUDED.allow_shared_quota,
			allow_local_usage_cmd = EXCLUDED.allow_local_usage_cmd,
			total_requests = GREATEST(omni_api_keys.total_requests, EXCLUDED.total_requests),
			total_tokens = GREATEST(omni_api_keys.total_tokens, EXCLUDED.total_tokens),
			last_used_at = CASE WHEN EXCLUDED.last_used_at <> '' THEN EXCLUDED.last_used_at ELSE omni_api_keys.last_used_at END
	`,
		k.ID,
		TruncateUTF8(k.Name, 256),
		k.Key,
		k.MaskedKey,
		keyHash,
		k.AdminAccess,
		k.AllowSelfUsage,
		k.AllowSharedQuota,
		k.AllowLocalUsageCmd,
		k.TotalRequests,
		k.TotalTokens,
		k.CreatedAt,
		k.LastUsedAt,
	)
}

// SyncAPIKeysToPostgresAsync synchronizes the current API keys list to PostgreSQL in the background.
func SyncAPIKeysToPostgresAsync() {
	pgMu.RLock()
	connected := pgConnected
	db := pgDB
	pgMu.RUnlock()
	if !connected || db == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cfg := LoadSavedAuthConfig()
		for _, k := range cfg.APIKeys {
			upsertAPIKeyToPostgres(ctx, db, k)
		}
	}()
}

// DeleteAPIKeyFromPostgresAsync removes an API key from PostgreSQL in the background.
func DeleteAPIKeyFromPostgresAsync(id string) {
	pgMu.RLock()
	connected := pgConnected
	db := pgDB
	pgMu.RUnlock()
	if !connected || db == nil || id == "" {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = db.ExecContext(ctx, `DELETE FROM omni_api_keys WHERE id = $1`, id)
	}()
}

// DeleteAccountFromPostgres permanently deletes a connected account matching ID or Email from PostgreSQL when connected.
func DeleteAccountFromPostgres(ident string) error {
	ident = strings.TrimSpace(ident)
	if ident == "" {
		return nil
	}
	pgMu.RLock()
	connected := pgConnected
	db := pgDB
	pgMu.RUnlock()
	if !connected || db == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := db.ExecContext(
		ctx,
		`DELETE FROM omni_accounts WHERE LOWER(id) = LOWER($1) OR LOWER(email) = LOWER($1)`,
		ident,
	)
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "does not exist") {
		return nil
	}
	return err
}

func ensurePromptLogWorker() {
	promptLogWorkerOnce.Do(func() {
		go runPromptLogBatchWorker()
	})
}

func sanitizePromptLogEntry(e GatewayLogEntry, seq int) GatewayLogEntry {
	if strings.TrimSpace(e.ID) == "" {
		e.ID = fmt.Sprintf("log-%d-%d", time.Now().UnixNano(), seq)
	}
	e.EmployeeName = TruncateUTF8(e.EmployeeName, 256)
	e.Account = TruncateUTF8(e.Account, 256)
	e.RequestedModel = TruncateUTF8(e.RequestedModel, 256)
	e.ResolvedModel = TruncateUTF8(e.ResolvedModel, 256)
	e.PromptText = TruncateUTF8(e.PromptText, 16000)
	e.ResponseText = TruncateUTF8(e.ResponseText, 16000)
	e.ErrorMessage = TruncateUTF8(e.ErrorMessage, 4000)
	e.ErrorDetail = TruncateUTF8(e.ErrorDetail, 4000)
	return e
}

func buildBatchInsertPromptLogsSQL(entries []GatewayLogEntry) (string, []any) {
	if len(entries) == 0 {
		return "", nil
	}
	const colsPerRow = 20
	var sb strings.Builder
	sb.Grow(280 + len(entries)*96)
	sb.WriteString(`INSERT INTO omni_prompt_logs (
	id, timestamp_str, api_key_id, employee_name, masked_key,
	provider, account, requested_model, resolved_model,
	prompt_text, response_text, prompt_tokens, completion_tokens, total_tokens, tokens_saved,
	latency_ms, status_code, is_error, error_message, error_detail
) VALUES `)

	args := make([]any, 0, len(entries)*colsPerRow)
	for i, raw := range entries {
		e := sanitizePromptLogEntry(raw, i)
		if i > 0 {
			sb.WriteByte(',')
		}
		base := i*colsPerRow + 1
		sb.WriteByte('(')
		for col := 0; col < colsPerRow; col++ {
			if col > 0 {
				sb.WriteByte(',')
			}
			sb.WriteByte('$')
			sb.WriteString(strconv.Itoa(base + col))
		}
		sb.WriteByte(')')

		args = append(args,
			e.ID, e.Timestamp, e.APIKeyID, e.EmployeeName, e.MaskedKey,
			string(e.Provider), e.Account, e.RequestedModel, e.ResolvedModel,
			e.PromptText, e.ResponseText, e.PromptTokens, e.CompletionTokens, e.TotalTokens, e.TokensSaved,
			e.LatencyMs, e.StatusCode, e.IsError, e.ErrorMessage, e.ErrorDetail,
		)
	}
	sb.WriteString(` ON CONFLICT (id) DO NOTHING`)
	return sb.String(), args
}

func writePromptLogBatch(entries []GatewayLogEntry) {
	if len(entries) == 0 {
		return
	}
	pgMu.RLock()
	connected := pgConnected
	db := pgDB
	pgMu.RUnlock()
	if !connected || db == nil {
		return
	}

	query, args := buildBatchInsertPromptLogsSQL(entries)
	if query == "" {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()

	var execErr error
	tx, txErr := db.BeginTx(ctx, nil)
	if txErr == nil && tx != nil {
		if _, err := tx.ExecContext(ctx, query, args...); err != nil {
			_ = tx.Rollback()
			execErr = err
		} else if commitErr := tx.Commit(); commitErr != nil {
			execErr = commitErr
		}
	} else {
		_, execErr = db.ExecContext(ctx, query, args...)
	}

	if execErr != nil {
		log.Printf("[omniroute-postgres] batch insert prompt logs (%d rows) warning: %v", len(entries), execErr)
		return
	}

	added := uint64(len(entries))
	next := pgInsertCounter.Add(added)
	prev := next - added
	if prev/pgPruneEveryInserts < next/pgPruneEveryInserts {
		pruneCtx, pruneCancel := context.WithTimeout(context.Background(), 10*time.Second)
		_, pruneErr := db.ExecContext(
			pruneCtx,
			`DELETE FROM omni_prompt_logs WHERE id IN (SELECT id FROM omni_prompt_logs ORDER BY created_at DESC OFFSET $1)`,
			effectiveMaxPostgresLogs(),
		)
		pruneCancel()
		if pruneErr != nil {
			log.Printf("[omniroute-postgres] auto-retention prune warning: %v", pruneErr)
		}
	}
}

func runPromptLogBatchWorker() {
	ticker := time.NewTicker(promptLogFlushInterval)
	defer ticker.Stop()

	batch := make([]GatewayLogEntry, 0, promptLogBatchSize)

	flushNow := func() {
		if len(batch) == 0 {
			return
		}
		writePromptLogBatch(batch)
		clear(batch)
		batch = batch[:0]
	}

	drainAndFlushAll := func() {
		for {
			select {
			case e := <-promptLogQueue:
				batch = append(batch, e)
				if len(batch) >= promptLogBatchSize {
					flushNow()
				}
			default:
				flushNow()
				return
			}
		}
	}

	for {
		select {
		case e := <-promptLogQueue:
			batch = append(batch, e)
			for len(batch) < promptLogBatchSize {
				select {
				case next := <-promptLogQueue:
					batch = append(batch, next)
				default:
					goto checkReady
				}
			}
		checkReady:
			if len(batch) >= promptLogBatchSize {
				flushNow()
			}

		case <-ticker.C:
			flushNow()

		case doneCh := <-promptLogFlushReqs:
			drainAndFlushAll()
			close(doneCh)
		}
	}
}

// InsertPromptLogAsync enqueues a GatewayLogEntry onto the bounded batch queue without blocking
// or spawning per-request goroutines, dropping non-blockingly only if the 2048-entry buffer is full.
func InsertPromptLogAsync(entry GatewayLogEntry) {
	pgMu.RLock()
	connected := pgConnected
	db := pgDB
	pgMu.RUnlock()
	if !connected || db == nil {
		return
	}
	ensurePromptLogWorker()
	select {
	case promptLogQueue <- entry:
	default:
		promptLogDropped.Add(1)
	}
}

// FlushPromptLogs drains and commits all buffered prompt log entries to PostgreSQL within timeout.
func FlushPromptLogs(timeout time.Duration) error {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	pgMu.RLock()
	connected := pgConnected
	db := pgDB
	pgMu.RUnlock()
	if !connected || db == nil {
		for {
			select {
			case <-promptLogQueue:
			default:
				return nil
			}
		}
	}

	ensurePromptLogWorker()
	doneCh := make(chan struct{})
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case promptLogFlushReqs <- doneCh:
	case <-timer.C:
		return fmt.Errorf("timed out enqueuing prompt log flush after %s", timeout)
	}

	select {
	case <-doneCh:
		return nil
	case <-timer.C:
		return fmt.Errorf("timed out waiting for prompt log flush after %s", timeout)
	}
}

// QueryPromptLogs queries prompt audit logs from PostgreSQL when connected, or falls back to in-memory ring buffer.
func QueryPromptLogs(limit int, providerFilter string, errorsOnly bool, apiKeyFilter string, searchQuery string) []GatewayLogEntry {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	apiKeyFilter = strings.TrimSpace(apiKeyFilter)
	searchQuery = strings.TrimSpace(searchQuery)
	providerFilter = strings.TrimSpace(providerFilter)

	pgMu.RLock()
	connected := pgConnected
	db := pgDB
	pgMu.RUnlock()

	if connected && db != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		var where []string
		var args []any
		argIdx := 1

		if providerFilter != "" && !strings.EqualFold(providerFilter, "all") {
			where = append(where, fmt.Sprintf("LOWER(provider) = LOWER($%d)", argIdx))
			args = append(args, providerFilter)
			argIdx++
		}
		if errorsOnly {
			where = append(where, "is_error = TRUE")
		}
		if apiKeyFilter != "" && !strings.EqualFold(apiKeyFilter, "all") {
			where = append(where, fmt.Sprintf("(api_key_id = $%d OR LOWER(employee_name) = LOWER($%d))", argIdx, argIdx))
			args = append(args, apiKeyFilter)
			argIdx++
		}
		if searchQuery != "" {
			escapedPattern := "%" + escapeILIKE(searchQuery) + "%"
			where = append(where, fmt.Sprintf(
				"(prompt_text ILIKE $%d ESCAPE '\\' OR response_text ILIKE $%d ESCAPE '\\' OR employee_name ILIKE $%d ESCAPE '\\' OR requested_model ILIKE $%d ESCAPE '\\')",
				argIdx, argIdx, argIdx, argIdx,
			))
			args = append(args, escapedPattern)
			argIdx++
		}

		query := `
			SELECT id, timestamp_str, api_key_id, employee_name, masked_key,
			       provider, account, requested_model, resolved_model,
			       prompt_text, response_text, prompt_tokens, completion_tokens, total_tokens, tokens_saved,
			       latency_ms, status_code, is_error, error_message, error_detail
			FROM omni_prompt_logs
		`
		if len(where) > 0 {
			query += " WHERE " + strings.Join(where, " AND ")
		}
		query += fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d", argIdx)
		args = append(args, limit)

		rows, err := db.QueryContext(ctx, query, args...)
		if err == nil {
			defer rows.Close()
			out := make([]GatewayLogEntry, 0)
			for rows.Next() {
				var e GatewayLogEntry
				var prov string
				if err := rows.Scan(
					&e.ID, &e.Timestamp, &e.APIKeyID, &e.EmployeeName, &e.MaskedKey,
					&prov, &e.Account, &e.RequestedModel, &e.ResolvedModel,
					&e.PromptText, &e.ResponseText, &e.PromptTokens, &e.CompletionTokens, &e.TotalTokens, &e.TokensSaved,
					&e.LatencyMs, &e.StatusCode, &e.IsError, &e.ErrorMessage, &e.ErrorDetail,
				); err == nil {
					e.Provider = ProviderID(prov)
					out = append(out, e)
				}
			}
			hasFilter := apiKeyFilter != "" || searchQuery != "" || errorsOnly || (providerFilter != "" && !strings.EqualFold(providerFilter, "all"))
			if len(out) > 0 || hasFilter {
				return out
			}
		}
	}

	// Fallback to in-memory logger with filtering
	memLogs := GlobalLogger.GetRecentLogs(limit*2, providerFilter, errorsOnly)
	filtered := make([]GatewayLogEntry, 0)
	qLower := strings.ToLower(searchQuery)
	for _, item := range memLogs {
		if apiKeyFilter != "" && !strings.EqualFold(apiKeyFilter, "all") {
			if !strings.EqualFold(item.APIKeyID, apiKeyFilter) && !strings.EqualFold(item.EmployeeName, apiKeyFilter) {
				continue
			}
		}
		if qLower != "" {
			haystack := strings.ToLower(item.PromptText + " " + item.ResponseText + " " + item.EmployeeName + " " + item.RequestedModel)
			if !strings.Contains(haystack, qLower) {
				continue
			}
		}
		filtered = append(filtered, item)
		if len(filtered) >= limit {
			break
		}
	}
	return filtered
}

// GetEmployeeUsageStats returns per-employee API key usage and prompt summary using SQL aggregation
// when PostgreSQL is connected, falling back seamlessly to in-memory GlobalLogger when disconnected.
func GetEmployeeUsageStats() []EmployeeUsageSummary {
	keys := ListAPIKeys()
	byKeyID := make(map[string]*EmployeeUsageSummary, len(keys)+2)
	var order []string

	for _, k := range keys {
		s := &EmployeeUsageSummary{
			APIKeyID:      k.ID,
			EmployeeName:  k.Name,
			MaskedKey:     k.MaskedKey,
			TotalRequests: k.TotalRequests,
			TotalTokens:   k.TotalTokens,
			LastUsedAt:    k.LastUsedAt,
		}
		byKeyID[k.ID] = s
		order = append(order, k.ID)
	}

	pgMu.RLock()
	connected := pgConnected
	db := pgDB
	pgMu.RUnlock()

	usedSQL := false
	if connected && db != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		rows, err := db.QueryContext(ctx, `
			WITH agg AS (
				SELECT
					COALESCE(NULLIF(api_key_id, ''), 'admin_local') AS eff_key_id,
					COUNT(*) AS log_requests,
					COALESCE(SUM(total_tokens), 0) AS log_tokens,
					COUNT(*) FILTER (WHERE is_error = TRUE) AS error_count
				FROM omni_prompt_logs
				GROUP BY COALESCE(NULLIF(api_key_id, ''), 'admin_local')
			),
			latest AS (
				SELECT DISTINCT ON (COALESCE(NULLIF(api_key_id, ''), 'admin_local'))
					COALESCE(NULLIF(api_key_id, ''), 'admin_local') AS eff_key_id,
					employee_name,
					masked_key,
					CASE WHEN resolved_model <> '' THEN resolved_model ELSE requested_model END AS last_model,
					LEFT(prompt_text, 300) AS last_prompt,
					timestamp_str AS last_used_at
				FROM omni_prompt_logs
				ORDER BY COALESCE(NULLIF(api_key_id, ''), 'admin_local'), created_at DESC
			)
			SELECT
				agg.eff_key_id,
				COALESCE(latest.employee_name, ''),
				COALESCE(latest.masked_key, ''),
				agg.log_requests,
				agg.log_tokens,
				agg.error_count,
				COALESCE(latest.last_model, ''),
				COALESCE(latest.last_prompt, ''),
				COALESCE(latest.last_used_at, '')
			FROM agg
			LEFT JOIN latest ON latest.eff_key_id = agg.eff_key_id
			ORDER BY agg.log_requests DESC
		`)
		if err == nil {
			defer rows.Close()
			usedSQL = true
			for rows.Next() {
				var (
					effKeyID   string
					empName    string
					maskedKey  string
					logReqs    int64
					logToks    int64
					errCount   int64
					lastModel  string
					lastPrompt string
					lastUsedAt string
				)
				if scanErr := rows.Scan(
					&effKeyID, &empName, &maskedKey,
					&logReqs, &logToks, &errCount,
					&lastModel, &lastPrompt, &lastUsedAt,
				); scanErr != nil {
					continue
				}

				s, exists := byKeyID[effKeyID]
				if !exists {
					if empName == "" {
						empName = "Admin / Local"
					}
					s = &EmployeeUsageSummary{
						APIKeyID:      effKeyID,
						EmployeeName:  empName,
						MaskedKey:     maskedKey,
						TotalRequests: logReqs,
						TotalTokens:   logToks,
						ErrorCount:    errCount,
						LastModel:     lastModel,
						LastPrompt:    TruncateUTF8(lastPrompt, 180),
						LastUsedAt:    lastUsedAt,
					}
					byKeyID[effKeyID] = s
					order = append(order, effKeyID)
				} else {
					if logReqs > s.TotalRequests {
						s.TotalRequests = logReqs
					}
					if logToks > s.TotalTokens {
						s.TotalTokens = logToks
					}
					s.ErrorCount = errCount
					if lastModel != "" {
						s.LastModel = lastModel
					}
					if lastPrompt != "" {
						s.LastPrompt = TruncateUTF8(lastPrompt, 180)
					}
					if s.LastUsedAt == "" && lastUsedAt != "" {
						s.LastUsedAt = lastUsedAt
					}
				}
			}
		}
	}

	if !usedSQL {
		memLogs := GlobalLogger.GetRecentLogs(0, "", false)
		memReqs := make(map[string]int64, len(keys)+2)
		memToks := make(map[string]int64, len(keys)+2)

		for _, l := range memLogs {
			id := l.APIKeyID
			if id == "" {
				id = "admin_local"
			}
			memReqs[id]++
			memToks[id] += int64(l.TotalTokens)

			s, exists := byKeyID[id]
			if !exists {
				name := l.EmployeeName
				if name == "" {
					name = "Admin / Local"
				}
				s = &EmployeeUsageSummary{
					APIKeyID:     id,
					EmployeeName: name,
					MaskedKey:    l.MaskedKey,
					LastUsedAt:   l.Timestamp,
				}
				byKeyID[id] = s
				order = append(order, id)
			}
			if l.IsError {
				s.ErrorCount++
			}
			if s.LastModel == "" {
				if l.ResolvedModel != "" {
					s.LastModel = l.ResolvedModel
				} else if l.RequestedModel != "" {
					s.LastModel = l.RequestedModel
				}
			}
			if s.LastPrompt == "" && l.PromptText != "" {
				s.LastPrompt = TruncateUTF8(l.PromptText, 180)
			}
			if s.LastUsedAt == "" && l.Timestamp != "" {
				s.LastUsedAt = l.Timestamp
			}
		}

		for id, count := range memReqs {
			if s, ok := byKeyID[id]; ok {
				if count > s.TotalRequests {
					s.TotalRequests = count
				}
				if memToks[id] > s.TotalTokens {
					s.TotalTokens = memToks[id]
				}
			}
		}
	}

	out := make([]EmployeeUsageSummary, 0, len(order))
	for _, id := range order {
		if s, ok := byKeyID[id]; ok {
			out = append(out, *s)
		}
	}
	return out
}

// ConfigurePostgresDSN saves the PostgreSQL DSN into gateway_config.json and attempts live connection.
func ConfigurePostgresDSN(dsn string) error {
	dsn = strings.TrimSpace(dsn)
	configMu.Lock()
	cfg := loadSavedAuthConfigLocked()
	cfg.PostgresDSN = dsn
	cfg.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	err := saveAuthConfigLocked(cfg)
	configMu.Unlock()
	if err != nil {
		return err
	}
	return InitPostgresDB(dsn)
}

// GetPostgresStatus returns the current PostgreSQL connection state and row counts.
func GetPostgresStatus() map[string]any {
	pgMu.RLock()
	db := pgDB
	dsn := pgActiveDSN
	serverVer := pgServerVersion
	lastErr := pgLastErr
	pgMu.RUnlock()

	if dsn == "" {
		dsn = ResolveEffectivePostgresDSN()
	}

	connected := false
	var totalLogs int64
	var totalKeys int64

	if db != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := db.PingContext(ctx); err == nil {
			connected = true
			_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM omni_prompt_logs`).Scan(&totalLogs)
			_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM omni_api_keys`).Scan(&totalKeys)
		} else {
			lastErr = err.Error()
		}
	}

	pgMu.Lock()
	if pgDB == db && db != nil {
		pgConnected = connected
		if !connected && lastErr != "" {
			pgLastErr = lastErr
		}
	}
	pgMu.Unlock()

	mode := "local_json"
	if connected {
		mode = "postgresql"
	}

	return map[string]any{
		"connected":         connected,
		"mode":              mode,
		"server_version":    serverVer,
		"dsn_masked":        MaskPostgresDSN(dsn),
		"has_dsn":           dsn != "",
		"total_prompt_logs": totalLogs,
		"total_keys_in_db":  totalKeys,
		"max_prompt_logs":   effectiveMaxPostgresLogs(),
		"last_error":        lastErr,
	}
}
