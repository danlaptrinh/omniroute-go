# OmniRoute Self-Healing & PDCA (Plan-Do-Check-Act) Autonomous Governance

## Zero Model Stepdown Policy (Strict SLA Enforcement)

OmniRoute enforces an absolute **Zero Model Stepdown Policy**. Degrading a requested frontier or tier model to a lower model (such as silently converting `gemini-4-carbon` or `gemini-4-argon` to `tiered`, `gemini-3-flash`, or any lesser tier) is strictly prohibited.

### Core Tenets

1. **Abolition of Silent Downgrade Headers**:
   - The legacy header `X-Omni-Gateway-Tier-Stepdown` is permanently abolished and eliminated from gateway response pipelines.
   - If a requested model cannot be served at its requested SLA and caliber, the gateway returns a descriptive, actionable error (HTTP 429/503) rather than returning lower-grade synthetic completions under the requested model's name.

2. **Non-Combo Isolation**:
   - Standard, non-combo requests (`resolved.IsCombo == false`) MUST NEVER walk `FallbackChain` to step down to lesser models.
   - Fallback chains are exclusively reserved for explicitly configured virtual combos (`auto/*`, `combo/*`) where the user deliberately requested multi-model failover or pipeline orchestration.

3. **Carbon & Argon Ultra Locking**:
   - `gemini-4-carbon`, `gemini-4-argon`, and their `-high`/`-medium` thinking variants are locked 100% to verified Antigravity Ultra accounts.
   - Under no circumstances will a Carbon or Argon request fall back to a standard Pro account or a generic fallback model. The request is routed internally to Ultra's real Google Cloud Code PA (CCPA) backend.

4. **Pro Account Direct 1-to-1 Integrity**:
   - Antigravity Pro accounts run direct models 1-to-1 (e.g., `gemini-2.5-pro`, `gemini-2.5-flash`).
   - Pro accounts must never be silently rewritten or degraded to `tiered` or `gemini-3-flash`.

5. **Autonomous Healing Pin (`heal_model_stepdown_pin`)**:
   - The PDCA self-healing engine in `gateway/pdca.go` continuously monitors audit logs and upstream telemetry.
   - Any detected silent downgrade triggers the `heal_model_stepdown_pin` corrective action: the misbehaving route is pinned to its designated tier pool, the faulty account state is quarantined, and the routing resolver cache is refreshed immediately.

---

## 1. P — Plan (Continuous Observability & Metric Baselining)

The **Plan** phase monitors runtime operational invariants across all provider pools (`antigravity`, `claude-code`, `alibaba`) and establishes threshold baselines for automated intervention.

### Monitored Invariants

- **Model Fidelity**: Verify that `GatewayLogEntry.ResolvedModel` strictly matches the model executed upstream (`GatewayLogEntry.UpstreamModel`). For non-combo models, any divergence constitutes a critical model stepdown violation.
- **Account Health & Quota State**:
  - Antigravity Ultra accounts: Track CCPA 429/ResourceExhausted errors, token bucket depletion, and tier entitlement flags.
  - Antigravity Pro accounts: Track per-model rate limits (`IsAccountModelRateLimited`), Claude 3.7/Sonnet quota flags (`isAccountClaudeQuotaExhaustedFast`), and CCPA endpoint availability.
  - Claude Code & Alibaba accounts: Track API key balance, rate limits, and network latency anomalies.
- **Upstream CCPA Model Liveness**:
  - Maintain the synchronized live cache (`workingCCPAModelCache`) against `DefaultCCPAEndpoint`.
  - Continuously track model retirement banners via `IsUnsupportedClientBanner` and dead model markings (`isDeadCCPAModel`, `isAccountDeadCCPAModel`).

### Anomaly Threshold Matrix

| Anomaly Type | Condition | Severity | Trigger Window |
|---|---|---|---|
| `model_stepdown_detected` | `ResolvedModel != UpstreamModel` on non-combo request | CRITICAL | Instant (1 hit) |
| `ultra_pool_exhaustion` | Available Ultra accounts < 1 while Carbon/Argon traffic active | HIGH | 30 seconds |
| `ccpa_dead_model` | Upstream CCPA returns 404/400 model not supported | HIGH | 3 consecutive hits |
| `quota_exhaustion_spike` | Account returns 429 across 3 consecutive calls | MEDIUM | 60 seconds |
| `unsupported_client_banner` | Upstream returns HTTP 200 with deprecation text | CRITICAL | Instant (1 hit) |

---

## 2. D — Do (Autonomous Policy Enforcement & Traffic Shaping)

The **Do** phase implements deterministic traffic routing, preventing tier contamination and enforcing zero-downgrade routing rules in real time.

### Routing & Dispatch Rules

1. **Ultra Account Binding**:
   - Models matching `IsCarbonOrArgonHighOrMedium(modelID)` are mapped exclusively to Antigravity Ultra accounts having active, unexhausted CCPA quota.
   - If all Ultra accounts are temporarily rate-limited, the gateway holds the request up to the client context timeout or returns HTTP 429 with `Retry-After`. It **never** downgrades to Pro or Flash.

2. **Combo vs. Standalone Request Partitioning**:
   ```go
   if !resolved.IsCombo {
       // Standalone request: strictly enforce requested model execution.
       // Never iterate over resolved.FallbackChain.
       return executeDirectUpstream(ctx, targetAccount, resolved)
   }
   // Combo request: execute configured pipeline or failover across explicit targets.
   return executeComboPipeline(ctx, resolved)
   ```

3. **Account Isolation & Scope Normalization**:
   - Accounts are strictly isolated by credential scope using `normalizeAccScope(accScope)`.
   - Prevent cross-account state leakage by sanitizing model tokens with `sanitizePDCAModelToken(modelID)`.

---

## 3. C — Check (Real-Time Verification & Anomaly Detection)

The **Check** phase inspects gateway transactions, telemetry streams, and background health probe responses to detect deviations from the planned operational envelope.

### Verification Cycle

Every PDCA evaluation cycle (default: 10s intervals):

1. **Log Inspection for Tier Downgrade**:
   - Scan recent `GatewayLogEntry` records.
   - If any log exhibits `TokensSaved > 0` caused by unauthorized model truncation, or indicates a tier mismatch between requested and upstream model IDs, generate an anomaly of type `model_stepdown_detected` with severity `CRITICAL`.

2. **Account Quota Verification**:
   - Evaluate `lookupConnectedAntigravityAccountFast(email)`.
   - Audit accounts flagged by `isAccountClaudeQuotaExhaustedFast(email)` or `IsAccountModelRateLimited(email, model)`.
   - Verify dead model registries (`isDeadCCPAModel`, `isAccountDeadCCPAModel`).

3. **Banner & Deprecation Auditing**:
   - Pass upstream payloads through `IsUnsupportedClientBanner`.
   - Detect if Google CCPA returns HTTP 200 with deprecation strings for Antigravity IDE clients.

### Recorded PDCA Anomaly Structure

```json
{
  "id": "anom_01J8XK3B11...",
  "cycle_id": "cycle_01J8XK3A88...",
  "timestamp": "2025-02-15T04:22:10Z",
  "type": "model_stepdown_detected",
  "severity": "CRITICAL",
  "provider": "antigravity",
  "account": "ultra-service@project.iam.gserviceaccount.com",
  "model": "gemini-4-carbon",
  "detail": "Attempted stepdown to tiered/gemini-3-flash was blocked. Model locked to Ultra pool."
}
```

---

## 4. A — Act (Automated Self-Healing & Remediation)

The **Act** phase executes targeted remediation without requiring gateway restarts, repairing routing tables and isolating failing components.

### Self-Healing Actions

1. **`heal_model_stepdown_pin`**:
   - Triggered when: `model_stepdown_detected` anomaly is identified.
   - Action:
     1. Pin the model to its designated strict provider pool (e.g., lock Carbon/Argon strictly to Ultra accounts).
     2. Invalidate stale resolver cache entries via `ModelResolver.Resolve()`.
     3. Evict any intermediate rewrite rules that attempt to alias the model to `tiered` or `gemini-3-flash`.
     4. Log a `PDCAAction` record with type `heal_model_stepdown_pin`.

2. **`heal_quarantine_account`**:
   - Triggered when: Account exhibits repeated 429 quota exhaustion or returns client deprecation banners.
   - Action:
     1. Quarantine the affected account for a decaying cooldown window (default: 5 minutes).
     2. Shift live traffic to remaining healthy accounts in the pool using `StrategyP2C` or `StrategyLeastUsed`.
     3. Emit notification to administrator log.

3. **`heal_purge_dead_model_cache`**:
   - Triggered when: Upstream reports model retirement or CCPA endpoint changes.
   - Action:
     1. Remove the model from `workingCCPAModelCache`.
     2. Register the model in dead model registry (`isDeadCCPAModel`).
     3. Prevent further routing attempts toward that endpoint/model pair.

4. **`heal_refresh_oauth_tokens`**:
   - Triggered when: Antigravity returns HTTP 401 Unauthorized or OAuth token expiry is imminent (< 5 minutes).
   - Action:
     1. Trigger asynchronous background token refresh using the account's refresh token / credentials.
     2. Atomically update the account's in-memory and persisted OAuth token.

### Recorded PDCA Action Structure

```json
{
  "id": "act_01J8XK3C44...",
  "cycle_id": "cycle_01J8XK3A88...",
  "timestamp": "2025-02-15T04:22:11Z",
  "type": "heal_model_stepdown_pin",
  "provider": "antigravity",
  "account": "ultra-service@project.iam.gserviceaccount.com",
  "target": "gemini-4-carbon",
  "detail": "Restored 100% Ultra account pin for gemini-4-carbon; cleared unauthorized tier stepdown."
}
```

---

## Continuous Verification Checklist

- [ ] `X-Omni-Gateway-Tier-Stepdown` header does not appear in any HTTP response.
- [ ] Requests for `gemini-4-carbon` and `gemini-4-argon` only dispatch to accounts where `Tier == "ultra"`.
- [ ] Non-combo requests fail fast with 429/503 when primary tier is saturated, instead of stepping down to lower models.
- [ ] Combo requests (`auto/*`) execute failovers exclusively along explicit user-defined chains.
- [ ] PDCA loop logs zero unresolved `model_stepdown_detected` anomalies over every 24-hour window.
- [ ] `heal_model_stepdown_pin` fires automatically if configuration corruption or stale cache triggers a model mismatch.
