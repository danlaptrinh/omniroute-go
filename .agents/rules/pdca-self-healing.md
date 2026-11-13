---
trigger: always_on
---

# PDCA Self-Detecting & Self-Fixing Engineering Standard

All agents and workers operating within `omni-parallel-mcp` must adhere to this closed-loop **PDCA (Plan → Do → Check → Act)** engineering workflow. Anomaly detection, root-cause isolation, and permanent auto-healing must be strictly contract-driven, verifiable, and regression-tested.

---

## 1. P — Plan: Proactive Detection & Root-Cause Verification

1. **Audit Live Runtime Telemetry First**:
   - Inspect `/healthz` and `/api/pdca/status` for active cycle anomalies, alerts, and watchdog health.
   - Query recent audit & error logs (`/v1/logs`, `GetRecentLogs(limit, provider, errorsOnly)`).
   - Inspect live account tiers and balances (`/v1/quota`, `GetQuotaSummary()`).
2. **Strict Evidence Correlation (No Guessing)**:
   - Correlate observed failures to exact source paths:
     - Account selection, token refresh, and per-account cooldowns: `gateway/oauth.go`
     - CCPA upstream streaming, candidate fallbacks, and connection pools: `gateway/translator.go`
     - Model ID canonicalization, alias resolution, and combo routing: `gateway/resolver.go`
     - Live quota scraping and tier discovery: `gateway/quota.go`
     - Self-healing watchdog, anomaly tracking, and background repair: `gateway/pdca.go`
   - Formulate falsifiable hypotheses tied to concrete error codes (e.g., HTTP 401/403/404/429/503), transport socket exhaustion, or cache key contamination.

---

## 2. D — Do: Isolated, Contract-First Implementation

1. **Strict Tier Isolation (Ultra vs. Pro)**:
   - **Antigravity Ultra Accounts**: Strictly reserved for flagship requests (`gemini-4-carbon*`, `gemini-4-argon*`, and `claude-opus*` / `claude-sonnet*` when Claude quota `> 0%`; `gemini-3.8-flash-high`/`-medium` on Ultra is permitted solely as the unthrottled backend for explicit `Carbon/Argon` requests). Ultra accounts must never serve direct `gemini-3.1-pro*`, direct `gemini-3.8-flash*`, `gemini-3-flash*`, sub-agent workers, or non-flagship hedges when healthy Pro slots exist.
   - **Antigravity Pro Accounts**: Absorb `gemini-3.1-pro*`, direct `gemini-3.8-flash*` (preferring unthrottled `gemini-3.8-flash-tiered`), `gemini-3-flash*`, `gemini-3.5-flash-lite`, and `auto/fast` requests whenever Pro Gemini weekly quota is healthy (`> 10%`).
2. **Strict Cross-Provider & Transport Isolation**:
   - Upstream providers (`antigravity`, `claude-code`, `alibaba`) must maintain isolated credential storage, distinct HTTP/2 client session pools (`AccountHTTPTransport`), and independent cooldown states.
   - Rate limits (HTTP 429) on one provider or account must never contaminate other providers, accounts, or unrelated model families in `accountModelRateLimits` or `deadCCPAModelCache`.
   - Transient cooldowns must observe `PDCATransientCooldownCap` (5 minutes) unless explicitly marked for account-level security revocation (HTTP 401/403).
3. **Execution Protocol**:
   - Declare shared interfaces and data structures before writing logic.
   - Dispatch multi-file implementation tasks via parallel workers (`omni_parallel_workers`), assigning disjoint file ownership to prevent merge collisions.
   - Deliver comprehensive unit tests alongside every modification.

---

## 3. C — Check: Independent Multi-Agent Verification

1. **Zero-Tolerance Quality Gates**:
   - Every change must compile cleanly and pass full static analysis: `go vet ./...`
   - Test suites must execute deterministically without race conditions or cached results: `go test -v -count=1 ./...`
2. **Separation of Duties in Swarms**:
   - **Builder**: Writes production logic and unit test coverage.
   - **Reviewer**: Audits git diffs for memory leaks, goroutine leaks, lock contention (`sync.Mutex`/`sync.RWMutex`), secret leaks, and architectural contract compliance.
   - **Tester**: Validates live HTTP contract behavior against endpoints (`/healthz`, `/api/pdca/status`, `/v1/chat/completions`, `/v1/models`).

---

## 4. A — Act: Self-Healing & Permanent Invariant Codification

Whenever a production bug, routing leak, stale cooldown, or cache contamination is diagnosed, it must be codified into code before task completion:

1. **Runtime PDCA Healing Action in `gateway/pdca.go`**:
   - `heal_stale_cooldown`: Prune accounts trapped in expired or overdue transient cooldowns.
   - `heal_expired_model_cache`: Sweep expired entries from `deadCCPAModelCache` and `accountModelRateLimits`.
   - `heal_ultra_working_model_pin`: Purge degraded/non-flagship winning pins from `workingCCPAModelCache` for Ultra accounts.
   - `heal_cross_provider_cooldown`: Clear cross-provider cooldown leaks or validation deadlocks.
   - `PDCAAlert`: Raise critical/warning alerts for persistent upstream auth revocations or missing model definitions.
2. **Deterministic Regression Tests in `gateway/pdca_test.go`**:
   - Every resolved defect must have a dedicated test reproducing the anomaly condition and proving the PDCA cycle automatically detects, repairs, and reports the healed state.
