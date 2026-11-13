---
name: pdca-autonomous-dev
description: >-
  Autonomous PDCA (Plan-Do-Check-Act) self-detecting, self-fixing, and
  self-healing workflow for OmniRoute-Go and multi-agent engineering swarms.
  Use this skill when diagnosing routing leaks, stale cooldowns, quota/auth
  anomalies, auditing live gateway health, or codifying runtime self-healing
  detectors and regression tests.
---

# PDCA Autonomous Development & Self-Healing Runbook

This skill defines the autonomous **Plan-Do-Check-Act (PDCA)** engineering lifecycle for OmniRoute-Go and parallel sub-agent swarms. It ensures zero-downtime routing, rigorous tier isolation between Google Antigravity Ultra/Pro and third-party upstreams (Claude Code, Alibaba Bailian), and closed-loop codification of self-healing runtime routines.

---

## Quick Diagnostic Helper: `pdca_health_check.ps1`

Run the diagnostic helper script before opening an incident or dispatching code modifications. It performs both fast offline static checks and live gateway telemetry audits:

```powershell
# 1. Offline static analysis and PDCA test suite validation
powershell -ExecutionPolicy Bypass -File .agents/skills/pdca-autonomous-dev/scripts/pdca_health_check.ps1

# 2. Live gateway health & anomaly audit against a running gateway instance
powershell -ExecutionPolicy Bypass -File .agents/skills/pdca-autonomous-dev/scripts/pdca_health_check.ps1 -GatewayUrl "http://127.0.0.1:8787" -TriggerCycle
```

### Script Execution Scope
1. **Compilation & Linting**: Runs `go vet ./...` across all packages.
2. **PDCA Unit Suite**: Executes `go test -v -count=1 -run TestPDCA ./gateway`.
3. **Live Gateway Telemetry**:
   - `GET /healthz`: Verifies server up-state and database connection status.
   - `GET /api/pdca/status`: Checks active watchdog loop, recent auto-healed actions, active anomalies, and unresolved alerts.
   - `GET /v1/quota`: Audits per-account session/weekly percentages, active cooldown timers, and tier assignments.
   - `GET /v1/logs?limit=50&errors_only=true`: Evaluates upstream error spikes (`401`, `403`, `404`, `429`).

---

## Phase 1: Plan (Proactive Anomaly Detection & Evidence Collection)

Before modifying code, capture real-time telemetry and pinpoint failure mechanisms. Avoid speculative repairs without quantitative evidence.

### Live Telemetry Inspection Points
| Endpoint | Key Metrics to Extract | Expected Normal Baseline |
| :--- | :--- | :--- |
| `GET /healthz` | `status`, `database`, `uptime_sec` | `{"status":"ok","database":"connected"}` |
| `GET /api/pdca/status` | `watchdog_running`, `cross_provider_isolation_verified`, `alerts`, `recent_actions` | `watchdog_running: true`, `cross_provider_isolation_verified: true`, `alerts: []` |
| `GET /v1/quota` | `accounts[].tier`, `accounts[].in_cooldown`, `accounts[].session_percent`, `accounts[].weekly_percent` | No healthy accounts stuck in cooldown (`in_cooldown: false`), correct tier mapping (`Ultra` vs `Pro`) |
| `GET /v1/logs?errors_only=true` | `status_code`, `provider`, `account`, `resolved_model`, `error_message` | `status_code < 400`; isolated 429s without cascading cross-account failover |

### 4 Core Failure Classes Checklist

#### 1. Stale Account Cooldowns
- **Symptom**: Account has `CooldownUntil > now`, but `FailureCount == 0` or last error occurred `>5 minutes` ago without hard auth revocation (`401`/`403`).
- **Detection**: Check if transient HTTP 429 or network blips set an excessive cooldown window exceeding `PDCATransientCooldownCap` (5 minutes).
- **Remediation**: Reset `CooldownUntil = 0` and clear account in-flight state via `ResetAccountLatencyStats(accountID)`.

#### 2. Expired Dead / Rate-Limited CCPA Model Caches
- **Symptom**: High-tier models (e.g., `gemini-4-carbon-high`, `claude-opus-4-6-thinking`) immediately fail over to low-tier models without attempting upstream requests.
- **Detection**: Check `deadCCPAModelCache` and `accountModelRateLimits` for entries where `time.Now().After(entry.expiresAt)` or where entries were cached without TTL eviction.
- **Remediation**: Prune expired entries from `deadCCPAModelCache` and `accountModelRateLimits`. Ensure winning entries in `workingCCPAModelCache` match the account's available tier.

#### 3. Ultra-to-Low-Tier Routing Leaks
- **Symptom**: `gemini-3.1-pro-high`, `gemini-3.8-flash`, or `auto/fast` requests route to Antigravity `Ultra` accounts when healthy `Pro` accounts have `weekly_gemini_percent > 10%`.
- **Detection**: In `selectProviderAccountCore`, inspect candidate sorting when `modelHint` is not a Carbon/Argon flagship model. Ultra accounts must be reserved for deep reasoning (`gemini-4-carbon-*`, `gemini-4-argon-*`).
- **Remediation**: Evict non-flagship model pins pointing to Ultra accounts in `workingCCPAModelCache`. Enforce tier priority in `SelectProviderAccountForModel`.

#### 4. Upstream Spikes & Cross-Provider Cooldown Bleed
- **Symptom**: An upstream error on Antigravity (e.g. 403 `VALIDATION_REQUIRED` or CCPA 404) triggers cooldowns on Claude Code or Alibaba accounts, or vice-versa.
- **Detection**: Inspect `GatewayLogEntry` sequences across providers. Check if `RecordAccountResult` was invoked with a mismatched `ProviderID` or empty account scope.
- **Remediation**: Isolate transport pools and cooldown counters strictly per `(ProviderID, AccountEmail)`. Verify `cross_provider_isolation_verified` in `RunPDCACycle`.

---

## Phase 2: Do (Contract-First, Tier-Isolated Implementation)

Implement fixes with strict compliance to architecture contracts, maintaining separation of concerns across providers and account tiers.

### Invariant Rules
1. **Tier Separation**:
   - `gemini-4-carbon-*` and `gemini-4-argon-*` High/Medium MUST strictly route to Antigravity `Ultra` accounts unless ALL Ultra accounts are exhausted.
   - `gemini-3.1-pro-*`, `gemini-3.8-flash-*`, and `auto/fast` MUST prioritize `Pro` accounts to preserve Ultra concurrency slots for deep reasoning.
2. **Provider Isolation**:
   - Transport pools (`AccountHTTPTransport`), TLS session caches (`tls.NewLRUClientSessionCache`), and HTTP/2 connections MUST be scoped to `normalizeAccScope(accountScope) + "|" + cleanProxyURL`.
   - Cooldowns, rate limits, and failure counts MUST NEVER leak across different `ProviderID` values.
3. **Bounded State Retention**:
   - In-memory history collections inside `gateway/pdca.go` MUST remain strictly bounded:
     * `MaxPDCACycleHistory = 50`
     * `MaxPDCAActionHistory = 100`
     * `MaxPDCAAnomalyHistory = 100`
4. **Non-Blocking Execution**:
   - Watchdog and health inspection cycles must NEVER hold `configMu`, `accountModelRLMu`, or `quotaCacheMu` during network I/O or long-running scans.

### Parallel Sub-Agent Dispatch Pattern
When operating within a swarm via `omni_parallel_workers`, structure the task payload with explicit file ownership and shared contracts:

```yaml
workers:
  - task_id: "pdca-engine"
    subagent_type: "author"
    context_files:
      - "gateway/pdca.go"
      - "gateway/contracts.go"
    shared_context:
      objective: "Implement RunPDCACycle self-healing watchdog with bounded ring buffer."
      target_branch: "feature/pdca-watchdog"
  - task_id: "pdca-tests"
    subagent_type: "author"
    context_files:
      - "gateway/pdca_test.go"
      - "gateway/pdca.go"
    shared_context:
      objective: "Add deterministic unit test suite covering stale cooldown healing and CCPA cache pruning."
      target_branch: "feature/pdca-watchdog"
max_concurrency: 0
```

---

## Phase 3: Check (Automated Suite + Independent Reviewer & Tester Gates)

No task is marked complete without rigorous multi-gate verification.

### Step 1: Local Verification Commands
Execute all commands from the repository root:

```bash
# 1. Static type checking & vet
go vet ./...

# 2. Targeted PDCA unit test suite
go test -v -count=1 -run TestPDCA ./gateway

# 3. Full gateway package test suite
go test -v -count=1 ./gateway

# 4. Entire project regression suite
go test -count=1 ./...
```

### Step 2: Multi-Agent Handoff Protocol
When working with agent roles:
1. **Author**: Submits code changes accompanied by test results and git diff summary.
2. **@reviewer Gate**:
   - Reviews diff for race conditions, unbounded slices/maps, and mutex contention.
   - Verifies that `configMu` and `accountModelRLMu` lock durations remain minimal (`<1ms`).
   - Ensures error logs never output sensitive tokens or user messages.
3. **@tester Gate**:
   - Runs `pdca_health_check.ps1` against both clean mock environments and running gateways.
   - Correlates `/api/pdca/status` output with synthetic failure injection (e.g., stale rate limits, dead model cache entries).
   - Validates that HTTP response codes and headers match OpenAI API standards.

---

## Phase 4: Act (Runtime Self-Healing & Regression Codification)

Transform discovered bugs into automated runtime self-healing actions within `gateway/pdca.go` and permanent regression tests within `gateway/pdca_test.go`.

### Adding a Detector + Auto-Healer to `RunPDCACycle`

1. **Define Action and Anomaly Types**:
   Ensure action/anomaly type strings are descriptive and documented:
   - `PDCAAction.Type`: `"heal_stale_cooldown"`, `"heal_expired_model_cache"`, `"heal_ultra_working_model_pin"`, `"heal_cross_provider_cooldown"`
   - `PDCAAnomaly.Type`: `"stale_account_cooldown"`, `"expired_model_cache"`, `"ultra_tier_routing_leak"`, `"provider_auth_upstream_spike"`, `"cross_provider_cooldown_contamination"`

2. **Detector & Healer Implementation Structure**:
   Add detection logic into `RunPDCACycle(now time.Time) PDCACycleReport`:

   ```go
   // Example: Detect and heal stale cooldowns
   for i := range accounts {
       acc := &accounts[i]
       if acc.CooldownUntil > 0 && acc.CooldownUntil <= nowUnix {
           // Cooldown has naturally expired; clear it cleanly
           acc.CooldownUntil = 0
           acc.FailureCount = 0
           report.HealedStaleCooldowns++
           report.Actions = append(report.Actions, PDCAAction{
               ID:        fmt.Sprintf("act-%d", time.Now().UnixNano()),
               CycleID:   report.CycleID,
               Timestamp: now.UTC().Format(time.RFC3339),
               Type:      "heal_stale_cooldown",
               Provider:  acc.Provider,
               Account:   acc.Email,
               Detail:    "Cleared naturally expired cooldown",
           })
       } else if acc.CooldownUntil > nowUnix && (acc.CooldownUntil-nowUnix) > int64(PDCATransientCooldownCap.Seconds()) {
           // Cooldown exceeds cap for transient errors without 401/403
           if acc.FailureCount < 5 {
               report.Anomalies = append(report.Anomalies, PDCAAnomaly{
                   ID:        fmt.Sprintf("anom-%d", time.Now().UnixNano()),
                   CycleID:   report.CycleID,
                   Timestamp: now.UTC().Format(time.RFC3339),
                   Type:      "stale_account_cooldown",
                   Severity:  "warning",
                   Provider:  acc.Provider,
                   Account:   acc.Email,
                   Detail:    fmt.Sprintf("Cooldown duration %ds exceeds cap %ds", acc.CooldownUntil-nowUnix, int64(PDCATransientCooldownCap.Seconds())),
               })
               acc.CooldownUntil = nowUnix + int64(PDCATransientCooldownCap.Seconds())
               report.Actions = append(report.Actions, PDCAAction{
                   ID:        fmt.Sprintf("act-%d", time.Now().UnixNano()),
                   CycleID:   report.CycleID,
                   Timestamp: now.UTC().Format(time.RFC3339),
                   Type:      "heal_stale_cooldown",
                   Provider:  acc.Provider,
                   Account:   acc.Email,
                   Detail:    "Capped excessive transient cooldown to PDCATransientCooldownCap",
               })
               report.HealedStaleCooldowns++
           }
       }
   }
   ```

3. **Pair with Unit Regression Test**:
   Every new healer MUST have a corresponding unit test in `gateway/pdca_test.go`:

   ```go
   func TestPDCACycle_HealStaleAccountCooldown(t *testing.T) {
       ResetPDCAState()
       defer ResetPDCAState()

       // Setup mock state with stale cooldown
       now := time.Now()
       // Populate test account with CooldownUntil in the past ...

       report := RunPDCACycle(now)
       if report.HealedStaleCooldowns == 0 {
           t.Fatalf("expected healed stale cooldowns, got 0")
       }
       if report.Status != "healed" {
           t.Errorf("expected report status 'healed', got %q", report.Status)
       }
   }
   ```

4. **Verify Bounded History**:
   Ensure `pdcaHistoryMu` maintains sliding window semantics so long-running gateway processes never leak memory:
   - `pdcaCycleHistory` trimmed to `MaxPDCACycleHistory` (50).
   - `pdcaActionHistory` trimmed to `MaxPDCAActionHistory` (100).
   - `pdcaAnomalyHistory` trimmed to `MaxPDCAAnomalyHistory` (100).
