package gateway

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	DefaultCCPAEndpoint        = "https://daily-cloudcode-pa.googleapis.com/v1internal:streamGenerateContent?alt=sse"
	DefaultCCPAMaxOutputTokens = 65536
	ccpaClientShards           = 16
)

type deadCCPAEntry struct {
	expiresAt  time.Time
	statusCode int
}

// SharedHTTPTransport and SharedHTTPClient reuse warm HTTP/2 connections across all requests,
// eliminating repeated TCP + TLS handshakes (~300-500ms saved per request).
var (
	SharedHTTPTransport = &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		MaxIdleConns:          256,
		MaxIdleConnsPerHost:   64,
		MaxConnsPerHost:       128,
		WriteBufferSize:       64 * 1024,
		ReadBufferSize:        64 * 1024,
		IdleConnTimeout:       180 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ForceAttemptHTTP2:     true,
	}
	SharedHTTPClient = &http.Client{
		Transport: SharedHTTPTransport,
		Timeout:   180 * time.Second,
	}

	ccpaShardedClients     [ccpaClientShards]*http.Client
	ccpaShardOffset        uint64
	stripedComboCounter    uint64
	lastCCPAWarmupUnixNano int64
	ccpaScopeInFlight      sync.Map // key: normalized account/token scope -> value: *atomic.Int64

	// Configurable CCPA watchdog timeouts (adjustable in unit tests).
	CCPAFirstChunkTimeout   = 20 * time.Second
	CCPAStreamIdleTimeout   = 25 * time.Second
	CCPACandidateMaxTimeout = 10 * time.Minute
	CCPAHedgeDelayFast      = 150 * time.Millisecond
	CCPAHedgeDelayPro       = 4500 * time.Millisecond
	CCPAHedgeDelayUltra     = 7000 * time.Millisecond

	// Fast-path caches for CCPA candidate resolution scoped by endpoint to avoid sequential probing.
	workingCCPAModelCache sync.Map // key: endpoint + "|" + resolved.CanonicalID -> value: string (winning CCPA model ID)
	deadCCPAModelCache    sync.Map // key: endpoint + "|" + candidate CCPA model ID -> value: deadCCPAEntry
)

func init() {
	for i := 0; i < ccpaClientShards; i++ {
		tr := &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			MaxIdleConns:          128,
			MaxIdleConnsPerHost:   64,
			MaxConnsPerHost:       128,
			WriteBufferSize:       64 * 1024,
			ReadBufferSize:        64 * 1024,
			IdleConnTimeout:       180 * time.Second,
			TLSHandshakeTimeout:   8 * time.Second,
			ResponseHeaderTimeout: 90 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
			ForceAttemptHTTP2:     true,
		}
		ccpaShardedClients[i] = &http.Client{
			Transport: tr,
		}
	}
}

func effectiveCCPAFirstChunkTimeout(req OpenAIChatRequest) time.Duration {
	base := CCPAFirstChunkTimeout
	if base >= 10*time.Second {
		promptTokens := EstimateTokens(req.Messages)
		if promptTokens > 16000 {
			extra := time.Duration((promptTokens-16000)/3000) * time.Second
			timeout := base + extra
			if timeout > 75*time.Second && base <= 75*time.Second {
				timeout = 75 * time.Second
			}
			return timeout
		}
	}
	return base
}

func accountCacheScope(w http.ResponseWriter, oauthToken string) string {
	if w != nil {
		if acc := strings.ToLower(strings.TrimSpace(w.Header().Get("X-Omni-Gateway-Account"))); acc != "" {
			return acc
		}
	}
	tok := strings.TrimSpace(oauthToken)
	if tok == "" {
		return "anon"
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte(tok))
	return fmt.Sprintf("%016x", h.Sum64())
}

func normalizeAccScope(accScope string) string {
	s := strings.ToLower(strings.TrimSpace(accScope))
	if s == "" {
		return "anon"
	}
	return s
}

func markDeadCCPAModel(endpoint, candidate string, ttl time.Duration) {
	candidate = strings.TrimSpace(candidate)
	if candidate == "" {
		return
	}
	if endpoint == "" {
		endpoint = DefaultCCPAEndpoint
	}
	deadCCPAModelCache.Store(endpoint+"|"+candidate, deadCCPAEntry{
		expiresAt: time.Now().Add(ttl),
	})
	prefix := endpoint + "|"
	workingCCPAModelCache.Range(func(k, v any) bool {
		ks, okK := k.(string)
		vs, okV := v.(string)
		if okK && okV && strings.HasPrefix(ks, prefix) && vs == candidate {
			workingCCPAModelCache.Delete(k)
		}
		return true
	})
}

func isDeadCCPAModel(endpoint, candidate string, now time.Time) bool {
	if endpoint == "" {
		endpoint = DefaultCCPAEndpoint
	}
	key := endpoint + "|" + strings.TrimSpace(candidate)
	val, ok := deadCCPAModelCache.Load(key)
	if !ok {
		return false
	}
	entry, ok := val.(deadCCPAEntry)
	if !ok {
		deadCCPAModelCache.Delete(key)
		return false
	}
	if now.Before(entry.expiresAt) {
		return true
	}
	deadCCPAModelCache.Delete(key)
	return false
}

func markAccountDeadCCPAModel(endpoint, accScope, candidate string, ttl time.Duration) {
	markAccountDeadCCPAModelWithStatus(endpoint, accScope, candidate, ttl, 0)
}

func markAccountDeadCCPAModelWithStatus(endpoint, accScope, candidate string, ttl time.Duration, statusCode int) {
	candidate = strings.TrimSpace(candidate)
	if candidate == "" {
		return
	}
	if endpoint == "" {
		endpoint = DefaultCCPAEndpoint
	}
	scope := normalizeAccScope(accScope)
	deadCCPAModelCache.Store(endpoint+"|acc:"+scope+"|"+candidate, deadCCPAEntry{
		expiresAt:  time.Now().Add(ttl),
		statusCode: statusCode,
	})
	prefix := endpoint + "|acc:" + scope + "|"
	workingCCPAModelCache.Range(func(k, v any) bool {
		ks, okK := k.(string)
		vs, okV := v.(string)
		if okK && okV && strings.HasPrefix(ks, prefix) && vs == candidate {
			workingCCPAModelCache.Delete(k)
		}
		return true
	})
}

func lookupAccountDeadCCPAEntry(endpoint, accScope, candidate string, now time.Time) (deadCCPAEntry, bool) {
	if endpoint == "" {
		endpoint = DefaultCCPAEndpoint
	}
	scope := normalizeAccScope(accScope)
	key := endpoint + "|acc:" + scope + "|" + strings.TrimSpace(candidate)
	val, ok := deadCCPAModelCache.Load(key)
	if !ok {
		return deadCCPAEntry{}, false
	}
	entry, ok := val.(deadCCPAEntry)
	if !ok {
		deadCCPAModelCache.Delete(key)
		return deadCCPAEntry{}, false
	}
	if now.Before(entry.expiresAt) {
		return entry, true
	}
	deadCCPAModelCache.Delete(key)
	return deadCCPAEntry{}, false
}

func isAccountDeadCCPAModel(endpoint, accScope, candidate string, now time.Time) bool {
	_, ok := lookupAccountDeadCCPAEntry(endpoint, accScope, candidate, now)
	return ok
}

func isAccountRateLimitedCCPAModel(endpoint, accScope, candidate string, now time.Time) bool {
	entry, ok := lookupAccountDeadCCPAEntry(endpoint, accScope, candidate, now)
	if !ok {
		return false
	}
	return entry.statusCode == 0 || entry.statusCode == http.StatusTooManyRequests
}

func isAccountOverloadedForHint(endpoint, accScope, modelHint string, now time.Time) bool {
	normModel := stripKnownModelPrefixes(modelHint)
	if normModel == "" {
		return false
	}
	probes := []string{normModel}
	switch {
	case strings.Contains(normModel, "carbon-high") || strings.Contains(normModel, "argon-high") || normModel == "gemini-4-carbon" || normModel == "gemini-4-argon":
		probes = append(probes, "gemini-4-carbon-high", "gemini-4-argon-high", "claude-opus-4-6-thinking")
	case strings.Contains(normModel, "carbon-medium") || strings.Contains(normModel, "argon-medium"):
		probes = append(probes, "gemini-4-carbon-medium", "gemini-4-argon-medium", "claude-sonnet-4-6")
	case normModel == "auto/fast":
		probes = append(probes, "gemini-3-flash")
	}
	scope := normalizeAccScope(accScope)
	for _, p := range probes {
		if entry, ok := lookupAccountDeadCCPAEntry(endpoint, scope, p, now); ok && entry.statusCode >= 500 {
			return true
		}
		suffix := "|acc:" + scope + "|" + strings.TrimSpace(p)
		overloaded := false
		deadCCPAModelCache.Range(func(k, v any) bool {
			ks, okK := k.(string)
			if !okK || !strings.HasSuffix(ks, suffix) {
				return true
			}
			if e, okE := v.(deadCCPAEntry); okE && now.Before(e.expiresAt) && e.statusCode >= 500 {
				overloaded = true
				return false
			}
			return true
		})
		if overloaded {
			return true
		}
	}
	return false
}

// ResetCCPAModelCaches clears both working and dead CCPA model caches and resets striped counters (useful for tests and diagnostics).
func ResetCCPAModelCaches() {
	workingCCPAModelCache.Range(func(k, _ any) bool {
		workingCCPAModelCache.Delete(k)
		return true
	})
	deadCCPAModelCache.Range(func(k, _ any) bool {
		deadCCPAModelCache.Delete(k)
		return true
	})
	atomic.StoreUint64(&stripedComboCounter, 0)
}

// ResetStripedComboCounter resets the atomic round-robin counter used by ComboModeStriped.
func ResetStripedComboCounter() {
	atomic.StoreUint64(&stripedComboCounter, 0)
}

func getOrCreateScopeCounter(key string) *atomic.Int64 {
	if existing, ok := ccpaScopeInFlight.Load(key); ok {
		return existing.(*atomic.Int64)
	}
	fresh := &atomic.Int64{}
	actual, _ := ccpaScopeInFlight.LoadOrStore(key, fresh)
	return actual.(*atomic.Int64)
}

func acquireCCPAScopeInFlight(accScope, oauthToken string) func() {
	sKey := normalizeAccScope(accScope)
	tKey := accountCacheScope(nil, oauthToken)
	var counters []*atomic.Int64
	if sKey != "" && sKey != "anon" {
		c := getOrCreateScopeCounter("acc:" + sKey)
		c.Add(1)
		counters = append(counters, c)
	}
	if tKey != "" && tKey != "anon" && tKey != sKey {
		c := getOrCreateScopeCounter("tok:" + tKey)
		c.Add(1)
		counters = append(counters, c)
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			for _, c := range counters {
				c.Add(-1)
			}
		})
	}
}

func currentCCPAScopeInFlight(accScope, oauthToken string) int {
	maxActive := 0
	sKey := normalizeAccScope(accScope)
	if sKey != "" && sKey != "anon" {
		if inf := GetAccountInFlight(sKey); inf > maxActive {
			maxActive = inf
		}
		if existing, ok := ccpaScopeInFlight.Load("acc:" + sKey); ok {
			if n := int(existing.(*atomic.Int64).Load()); n > maxActive {
				maxActive = n
			}
		}
	}
	tKey := accountCacheScope(nil, oauthToken)
	if tKey != "" && tKey != "anon" {
		if existing, ok := ccpaScopeInFlight.Load("tok:" + tKey); ok {
			if n := int(existing.(*atomic.Int64).Load()); n > maxActive {
				maxActive = n
			}
		}
		if existing, ok := ccpaScopeInFlight.Load("acc:" + tKey); ok {
			if n := int(existing.(*atomic.Int64).Load()); n > maxActive {
				maxActive = n
			}
		}
	}
	return maxActive
}

// WarmupCCPAConnectionsAsync pre-establishes warm HTTP/2 TLS connections to DefaultCCPAEndpoint
// across SharedHTTPClient and all 16 ccpaShardedClients in a non-blocking background goroutine
// so the very first live request avoids the 300-500ms TCP + TLS handshake delay.
func WarmupCCPAConnectionsAsync() {
	now := time.Now().UnixNano()
	prev := atomic.LoadInt64(&lastCCPAWarmupUnixNano)
	if prev > 0 && now-prev < int64(30*time.Second) {
		return
	}
	if !atomic.CompareAndSwapInt64(&lastCCPAWarmupUnixNano, prev, now) {
		return
	}
	WarmupCCPAEndpointAsync(DefaultCCPAEndpoint)
}

// WarmupCCPAEndpointAsync pre-establishes warm HTTP/2 / TLS connections to the origin of endpoint
// in a non-blocking background goroutine across SharedHTTPClient and all 16 ccpaShardedClients.
func WarmupCCPAEndpointAsync(endpoint string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = warmupCCPAEndpoint(ctx, endpoint)
	}()
}

func warmupCCPAEndpoint(ctx context.Context, endpoint string) int {
	raw := strings.TrimSpace(endpoint)
	if raw == "" {
		raw = DefaultCCPAEndpoint
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return 0
	}
	originURL := parsed.Scheme + "://" + parsed.Host + "/"

	clients := make([]*http.Client, 0, ccpaClientShards+1)
	if SharedHTTPClient != nil {
		clients = append(clients, SharedHTTPClient)
	}
	for i := 0; i < ccpaClientShards; i++ {
		if ccpaShardedClients[i] != nil {
			clients = append(clients, ccpaShardedClients[i])
		}
	}

	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	var warmed atomic.Int32
	ua := GetAgyUserAgent()

	for _, cl := range clients {
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		go func(c *http.Client) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return
			}
			reqCtx, cancelReq := context.WithTimeout(ctx, 3500*time.Millisecond)
			defer cancelReq()
			req, reqErr := http.NewRequestWithContext(reqCtx, http.MethodHead, originURL, nil)
			if reqErr != nil {
				return
			}
			req.Header.Set("User-Agent", ua)
			resp, doErr := c.Do(req)
			if doErr == nil && resp != nil {
				_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
				_ = resp.Body.Close()
				warmed.Add(1)
			}
		}(cl)
	}
	wg.Wait()
	return int(warmed.Load())
}

func isRunningUnderGoTest() bool {
	if len(os.Args) == 0 {
		return false
	}
	arg0 := strings.ToLower(os.Args[0])
	return strings.HasSuffix(arg0, ".test") || strings.HasSuffix(arg0, ".test.exe")
}

// GatewayUpstream implements UpstreamHandler with Standalone Direct Antigravity CCPA mode
// (daily-cloudcode-pa.googleapis.com/v1internal:streamGenerateContent?alt=sse) by default,
// and optional secondary upstream only if explicitly configured.
type GatewayUpstream struct {
	cfg    GatewayConfig
	client *http.Client
}

func (u *GatewayUpstream) ccpaHTTPClient(oauthToken string) *http.Client {
	return u.ccpaHTTPClientForScope("", oauthToken)
}

func (u *GatewayUpstream) ccpaHTTPClientForScope(accScope, oauthToken string) *http.Client {
	if u.client != nil && u.client != SharedHTTPClient {
		return u.client
	}
	cleanTok := strings.TrimSpace(oauthToken)
	scope := normalizeAccScope(accScope)
	if scope != "" && scope != "anon" {
		if pool := getOrCreateAccountIsolatedPool(scope, ""); pool != nil {
			if currentCCPAScopeInFlight(accScope, cleanTok) > 1 {
				idx := int(pool.shardCounter.Add(1) % accountTransportShards)
				return pool.ccpaClients[idx]
			}
			return pool.ccpaClients[0]
		}
	}
	if scope == "anon" && cleanTok != "" {
		scope = accountCacheScope(nil, cleanTok)
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(scope))
	_, _ = h.Write([]byte("|"))
	_, _ = h.Write([]byte(cleanTok))
	baseIdx := uint64(h.Sum32())

	if currentCCPAScopeInFlight(accScope, cleanTok) > 1 {
		offset := atomic.AddUint64(&ccpaShardOffset, 1)
		return ccpaShardedClients[int((baseIdx+offset)%uint64(ccpaClientShards))]
	}
	return ccpaShardedClients[int(baseIdx%uint64(ccpaClientShards))]
}

// NewUpstream creates a new GatewayUpstream instance sharing the warm HTTP/2 connection pool.
func NewUpstream(cfg GatewayConfig) *GatewayUpstream {
	explicitDefault := strings.EqualFold(strings.TrimSpace(cfg.CCPAEndpoint), DefaultCCPAEndpoint)
	if cfg.CCPAEndpoint == "" {
		cfg.CCPAEndpoint = DefaultCCPAEndpoint
	}
	if explicitDefault && !isRunningUnderGoTest() {
		if strings.TrimSpace(os.Getenv("OMNI_SKIP_CCPA_WARMUP")) == "" {
			WarmupCCPAConnectionsAsync()
		}
		WarmupAccountQuotasAndTiersAsync()
	}
	return &GatewayUpstream{
		cfg:    cfg,
		client: SharedHTTPClient,
	}
}

type translatorCtxKey string

const (
	cacheKeyReqCtxKey      translatorCtxKey = "omni_pre_compression_cache_req"
	ccpaPrimaryLeaseCtxKey translatorCtxKey = "omni_ccpa_primary_lease"
)

type ccpaPrimaryLease struct {
	accountEmail    string
	modelHint       string
	releaseInFlight func()
	winnerEmail     string
	winnerRelease   func()
	hedgedWon       bool
}

func withCCPAPrimaryLease(ctx context.Context, lease *ccpaPrimaryLease) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, ccpaPrimaryLeaseCtxKey, lease)
}

func ccpaPrimaryLeaseFromContext(ctx context.Context) *ccpaPrimaryLease {
	if ctx == nil {
		return nil
	}
	l, _ := ctx.Value(ccpaPrimaryLeaseCtxKey).(*ccpaPrimaryLease)
	return l
}

func cloneRequestForCacheKey(req OpenAIChatRequest) OpenAIChatRequest {
	cloned := req
	if req.Temperature != nil {
		t := *req.Temperature
		cloned.Temperature = &t
	}
	if req.ThinkingConfig != nil {
		tc := *req.ThinkingConfig
		cloned.ThinkingConfig = &tc
	}
	if len(req.Messages) > 0 {
		origMsgs := make([]OpenAIChatMessage, len(req.Messages))
		copy(origMsgs, req.Messages)
		for i := range origMsgs {
			if len(origMsgs[i].Content) > 0 {
				rawCopy := make(json.RawMessage, len(origMsgs[i].Content))
				copy(rawCopy, origMsgs[i].Content)
				origMsgs[i].Content = rawCopy
			}
		}
		cloned.Messages = origMsgs
	}
	return cloned
}

func withCacheKeyRequest(ctx context.Context, cacheReq *OpenAIChatRequest) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, cacheKeyReqCtxKey, cacheReq)
}

func cacheKeyRequestFromContext(ctx context.Context, fallback OpenAIChatRequest) OpenAIChatRequest {
	if ctx != nil {
		if orig, ok := ctx.Value(cacheKeyReqCtxKey).(*OpenAIChatRequest); ok && orig != nil {
			return *orig
		}
	}
	return fallback
}

// HandleChatCompletion routes and translates an OpenAIChatRequest across the 3 OmniRoute-Go providers:
//  1. antigravity (Google Antigravity Gen 3, Gen 4 & Claude)
//  2. claude-code (Anthropic Claude Code Pro/Max OAuth & API Key)
//  3. alibaba     (Alibaba Token Plan / Bailian Coding Plan)
func (u *GatewayUpstream) HandleChatCompletion(
	ctx context.Context,
	w http.ResponseWriter,
	req OpenAIChatRequest,
	resolved ResolvedModel,
	incomingAuth string,
) error {
	cleanAuth := strings.TrimSpace(incomingAuth)
	if strings.HasPrefix(strings.ToLower(cleanAuth), "bearer ") {
		cleanAuth = strings.TrimSpace(cleanAuth[7:])
	}

	// Snapshot exact pre-compression / pre-RAG request so ResponseCache.Put hashes the identical
	// request bytes that Server.handleChatCompletions passed to ResponseCache.Get.
	cacheKeyReq := cloneRequestForCacheKey(req)

	// Inject global Custom System Prompt if enabled in EndpointConfig
	req = ApplyCustomSystemPrompt(req)

	// Retrieve & inject Long-Term Vector Database RAG memories only when VectorDB and AutoInjectRAG are enabled
	if vc := GetVectorDBConfig(); vc.Enabled && vc.AutoInjectRAG && len(req.Messages) > 0 {
		if ragReq, ragCount := ApplyVectorMemoryRAG(req); ragCount > 0 {
			req = ragReq
			w.Header().Set("X-Omni-Gateway-Vector-Matches", fmt.Sprintf("%d", ragCount))
		}
	}

	// Apply OmniRoute Prompt and Token Compression pipeline
	compCfg := GetCompressionConfig()
	compressedReq, tokensBefore, tokensAfter := CompressRequest(req, compCfg)
	tokensSaved := tokensBefore - tokensAfter
	if tokensSaved > 0 {
		RecordCompressionSavings(tokensSaved)
		w.Header().Set("X-Omni-Gateway-Compression-Saved", fmt.Sprintf("%d", tokensSaved))
	}
	req = compressedReq

	// Build ordered list of ResolvedModel targets to try (primary + cross-provider combo fallbacks)
	var targets []ResolvedModel
	if resolved.IsCombo && len(resolved.FallbackChain) > 0 {
		resolver := NewResolver()
		for _, fbModelID := range resolved.FallbackChain {
			if strings.EqualFold(fbModelID, resolved.RequestedID) || strings.EqualFold(fbModelID, resolved.CanonicalID) {
				continue
			}
			fbResolved := resolver.Resolve(fbModelID, nil)
			fbResolved.RequestedID = resolved.RequestedID
			targets = append(targets, fbResolved)
		}
	}
	if len(targets) == 0 {
		targets = append(targets, resolved)
		if len(resolved.FallbackChain) > 0 {
			resolver := NewResolver()
			for _, fbModelID := range resolved.FallbackChain {
				if strings.EqualFold(fbModelID, resolved.RequestedID) || strings.EqualFold(fbModelID, resolved.CanonicalID) {
					continue
				}
				fbResolved := resolver.Resolve(fbModelID, nil)
				fbResolved.RequestedID = resolved.RequestedID
				targets = append(targets, fbResolved)
			}
		}
	}

	// Execute Multi-Model Collaborative Pipeline or Mixture-of-Agents Consensus if configured on Combo
	if resolved.IsCombo {
		switch resolved.ComboMode {
		case ComboModePipeline:
			if err := u.executeComboPipeline(ctx, w, req, resolved, targets, cleanAuth); err == nil {
				return nil
			}
		case ComboModeConsensus:
			if err := u.executeComboConsensus(ctx, w, req, resolved, targets, cleanAuth); err == nil {
				return nil
			}
		case ComboModeNightSaver:
			w.Header().Set("X-Omni-Gateway-Combo-Mode", string(ComboModeNightSaver))
		case ComboModeStriped:
			w.Header().Set("X-Omni-Gateway-Combo-Mode", string(ComboModeStriped))
			targets = u.rotateStripedComboTargets(resolved, targets)
		}
	}

	// Evening/Night coding support: when running at night (19:00 - 07:00 UTC+7) and Alibaba account is configured,
	// promote alibaba/qwen3.8-max in coding failover chains right after Carbon 4 / Argon 4 for high-speed, 40% discounted coding.
	if isVietnamNightHours() && u.isProviderConfigured(ProviderAlibaba, cleanAuth) {
		canonLower := strings.ToLower(strings.TrimSpace(resolved.CanonicalID))
		if canonLower == "auto/best-coding" || resolved.ComboMode == ComboModeNightSaver {
			var reordered []ResolvedModel
			var qwenTarget *ResolvedModel
			for _, t := range targets {
				if t.Provider == ProviderAlibaba && strings.Contains(strings.ToLower(t.CanonicalID), "qwen3.8-max") {
					qCopy := t
					qwenTarget = &qCopy
					break
				}
			}
			if qwenTarget == nil {
				resQwen := ResolveModel("alibaba/qwen3.8-max", nil)
				qwenTarget = &resQwen
			}
			// Keep Carbon/Argon flagships first
			for _, t := range targets {
				if strings.Contains(t.CanonicalID, "carbon") || strings.Contains(t.CanonicalID, "argon") {
					reordered = append(reordered, t)
				}
			}
			reordered = append(reordered, *qwenTarget)
			for _, t := range targets {
				if !strings.Contains(t.CanonicalID, "carbon") && !strings.Contains(t.CanonicalID, "argon") && !strings.Contains(t.CanonicalID, "qwen3.8-max") {
					reordered = append(reordered, t)
				}
			}
			targets = reordered
		}
	}

	standaloneMode := strings.TrimSpace(u.cfg.OmniRouteBaseURL) == ""
	var lastErr error
	var firstUpstreamErr error

	targetCtx := withCacheKeyRequest(ctx, &cacheKeyReq)

	hedgedEnabled, hedgeDelayMs := GetHedgedConfig()
	if hedgedEnabled && len(targets) > 1 && !req.Stream {
		hedgedTargets := targets
		if standaloneMode {
			filtered := make([]ResolvedModel, 0, len(targets))
			for idx, t := range targets {
				if idx == 0 || u.isProviderConfigured(t.Provider, cleanAuth) {
					filtered = append(filtered, t)
				}
			}
			hedgedTargets = filtered
		}
		if len(hedgedTargets) > 1 {
			if err := u.handleHedgedCompletion(targetCtx, w, req, hedgedTargets, cleanAuth, hedgeDelayMs); err == nil {
				return nil
			}
		}
	}

	for idx, target := range targets {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if idx > 0 && standaloneMode && !u.isProviderConfigured(target.Provider, cleanAuth) {
			continue
		}
		if err := u.tryTarget(targetCtx, w, req, target, cleanAuth); err == nil {
			return nil
		} else {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			lastErr = err
			if firstUpstreamErr == nil && !isMissingAccountErr(err) {
				firstUpstreamErr = err
			}
		}
	}

	if ctx.Err() != nil {
		return ctx.Err()
	}

	if strings.TrimSpace(u.cfg.OmniRouteBaseURL) != "" {
		return u.handleOmniRouteWithFallback(targetCtx, w, req, resolved, cleanAuth)
	}

	if firstUpstreamErr != nil {
		writeOpenAIError(w, http.StatusBadGateway, fmt.Sprintf("direct Antigravity CCPA error for model %q (%s): %v", resolved.RequestedID, resolved.CCPAModelID, firstUpstreamErr), "upstream_error")
		return firstUpstreamErr
	}
	_ = lastErr

	msg := fmt.Sprintf(
		"OmniRoute-Go v2.0: To call %q (provider=%s, model=%s), please connect an account (Antigravity OAuth, Claude Code, or Alibaba Token Plan) at http://127.0.0.1:8787/ui (config: %s)",
		resolved.RequestedID,
		resolved.Provider,
		resolved.UpstreamModelID,
		GatewayConfigFilePath(),
	)
	writeOpenAIError(w, http.StatusUnauthorized, msg, "authentication_required")
	return fmt.Errorf("%s", msg)
}

// isProviderConfigured checks in O(1) memory whether a fallback target's provider has active credentials
// configured via environment variables, gateway config, connected accounts, or request Authorization header.
func (u *GatewayUpstream) isProviderConfigured(provider ProviderID, incomingAuth string) bool {
	cleanAuth := strings.TrimSpace(incomingAuth)
	if strings.HasPrefix(strings.ToLower(cleanAuth), "bearer ") {
		cleanAuth = strings.TrimSpace(cleanAuth[7:])
	}
	lowerAuth := strings.ToLower(cleanAuth)

	switch provider {
	case "", ProviderAutoCombo, ProviderAntigravity:
		if strings.TrimSpace(u.cfg.AgyOAuthToken) != "" || strings.TrimSpace(os.Getenv("AGY_OAUTH_TOKEN")) != "" {
			return true
		}
		if cleanAuth != "" && !strings.HasPrefix(lowerAuth, "sk-omni-") && !strings.HasPrefix(lowerAuth, "sk-ant-") {
			return true
		}
		return hasSavedAntigravityTokenFast()

	case ProviderClaudeCode:
		if strings.TrimSpace(os.Getenv("CLAUDE_CODE_OAUTH_TOKEN")) != "" {
			return true
		}
		if strings.HasPrefix(lowerAuth, "sk-ant-") {
			return true
		}
		return HasConfiguredProviderAccount(ProviderClaudeCode)

	case ProviderAlibaba:
		if strings.TrimSpace(os.Getenv("ALIBABA_API_KEY")) != "" {
			return true
		}
		if strings.HasPrefix(lowerAuth, "sk-") && !strings.HasPrefix(lowerAuth, "sk-ant-") && !strings.HasPrefix(lowerAuth, "sk-omni-") {
			return true
		}
		return HasConfiguredProviderAccount(ProviderAlibaba)

	default:
		return false
	}
}

func isMissingAccountErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.HasPrefix(msg, "no valid ") ||
		strings.HasPrefix(msg, "no antigravity ") ||
		strings.HasPrefix(msg, "no claude ") ||
		strings.HasPrefix(msg, "no alibaba ") ||
		strings.Contains(msg, "oauth token configured") ||
		strings.Contains(msg, "api key configured")
}

func (u *GatewayUpstream) tryTarget(
	ctx context.Context,
	w http.ResponseWriter,
	req OpenAIChatRequest,
	target ResolvedModel,
	cleanAuth string,
) error {
	prov := target.Provider
	if prov == "" || prov == ProviderAutoCombo {
		prov = ProviderAntigravity
	}

	startMs := time.Now().UnixMilli()

	switch prov {
	case ProviderClaudeCode:
		return ExecuteClaudeWithReactiveRefresh(ctx, u, w, req, target, cleanAuth, startMs)

	case ProviderAlibaba:
		acc, err := SelectProviderAccount(ctx, ProviderAlibaba)
		lowerAuth := strings.ToLower(cleanAuth)
		if err != nil && strings.HasPrefix(lowerAuth, "sk-") && !strings.HasPrefix(lowerAuth, "sk-ant-") && !strings.HasPrefix(lowerAuth, "sk-omni-") {
			acc = &ConnectedAccount{Provider: ProviderAlibaba, Email: "header-key", APIKey: cleanAuth}
		} else if err != nil {
			if envKey := strings.TrimSpace(os.Getenv("ALIBABA_API_KEY")); envKey != "" {
				acc = &ConnectedAccount{Provider: ProviderAlibaba, Email: "env-key", APIKey: envKey}
			}
		}
		if acc == nil {
			return fmt.Errorf("no Alibaba Token Plan account configured")
		}
		AcquireAccountInFlightWithRole(acc.Email, target.CanonicalID)
		err = u.handleAlibabaProvider(ctx, w, req, target, *acc)
		ReleaseAccountInFlight(acc.Email)
		lat := time.Now().UnixMilli() - startMs
		if err == nil {
			usedTokens := parseRecordedTokens(w.Header().Get("X-Omni-Gateway-Tokens"), req)
			RecordAccountModelResult(acc.Email, ProviderAlibaba, target.CanonicalID, true, lat, usedTokens, http.StatusOK)
			entry := GatewayLogEntry{
				Provider:       ProviderAlibaba,
				Account:        acc.Email,
				RequestedModel: req.Model,
				ResolvedModel:  target.CanonicalID,
				StatusCode:     http.StatusOK,
				LatencyMs:      lat,
				TotalTokens:    usedTokens,
				IsError:        false,
				FallbackChain:  target.FallbackChain,
			}
			EnrichLogEntryFromContext(ctx, &entry, req)
			RecordLogEntry(entry)
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		RecordAccountModelResult(acc.Email, ProviderAlibaba, target.CanonicalID, false, lat, 0, http.StatusBadGateway)
		errEntry := GatewayLogEntry{
			Provider:       ProviderAlibaba,
			Account:        acc.Email,
			RequestedModel: req.Model,
			ResolvedModel:  target.CanonicalID,
			StatusCode:     http.StatusBadGateway,
			LatencyMs:      lat,
			IsError:        true,
			ErrorMessage:   err.Error(),
			FallbackChain:  target.FallbackChain,
		}
		EnrichLogEntryFromContext(ctx, &errEntry, req)
		RecordLogEntry(errEntry)
		log.Printf("[omniroute-go] alibaba target %q failed: %v; trying next provider in chain", target.CanonicalID, err)
		return err

	case ProviderAntigravity:
		return ExecuteAntigravityWithReactiveRefresh(ctx, u, w, req, target, cleanAuth, startMs)
	}
	return fmt.Errorf("unknown provider %s", prov)
}

func (u *GatewayUpstream) handleHedgedCompletion(
	ctx context.Context,
	w http.ResponseWriter,
	req OpenAIChatRequest,
	targets []ResolvedModel,
	cleanAuth string,
	hedgeDelayMs int,
) error {
	type hedgeResult struct {
		err  error
		buf  *bytes.Buffer
		hdrs http.Header
	}

	resChan := make(chan hedgeResult, 2)
	ctxParent, cancelAll := context.WithCancel(ctx)
	defer cancelAll()

	var wg sync.WaitGroup

	runTarget := func(t ResolvedModel) {
		defer wg.Done()
		rec := newResponseRecorder()
		err := u.tryTarget(ctxParent, rec, req, t, cleanAuth)
		if err == nil && rec.statusCode >= 200 && rec.statusCode < 300 {
			select {
			case resChan <- hedgeResult{err: nil, buf: rec.body, hdrs: rec.header}:
			default:
			}
		} else {
			select {
			case resChan <- hedgeResult{err: err}:
			default:
			}
		}
	}

	// 1. Launch Primary Target
	wg.Add(1)
	go runTarget(targets[0])

	// 2. Hedge Timer: If primary hasn't replied within hedgeDelayMs, launch backup target
	timer := time.NewTimer(time.Duration(hedgeDelayMs) * time.Millisecond)
	defer timer.Stop()

	var winner *hedgeResult

	select {
	case res := <-resChan:
		if res.err == nil && res.buf != nil {
			winner = &res
		}
	case <-timer.C:
		log.Printf("[omniroute-go] hedged execution triggered: %s exceeded %dms; racing secondary fallback %s",
			targets[0].CanonicalID, hedgeDelayMs, targets[1].CanonicalID)
		wg.Add(1)
		go runTarget(targets[1])

		select {
		case res := <-resChan:
			if res.err == nil && res.buf != nil {
				winner = &res
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	case <-ctx.Done():
		return ctx.Err()
	}

	if winner != nil {
		cancelAll()
		for k, v := range winner.hdrs {
			w.Header()[k] = v
		}
		w.Header().Set("X-Omni-Gateway-Hedged-Racing", "WIN")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(winner.buf.Bytes())
		return nil
	}

	return fmt.Errorf("hedged racing attempts failed; falling back to sequential execution")
}

type responseRecorder struct {
	header     http.Header
	body       *bytes.Buffer
	statusCode int
}

func newResponseRecorder() *responseRecorder {
	return &responseRecorder{
		header:     make(http.Header),
		body:       new(bytes.Buffer),
		statusCode: http.StatusOK,
	}
}

func (r *responseRecorder) Header() http.Header {
	return r.header
}

func (r *responseRecorder) Write(b []byte) (int, error) {
	return r.body.Write(b)
}

func (r *responseRecorder) WriteHeader(statusCode int) {
	r.statusCode = statusCode
}

// ccpaRequestPayload matches the internal Antigravity Cloud Code Private API schema.
type ccpaRequestPayload struct {
	Model   string       `json:"model"`
	Project string       `json:"project,omitempty"`
	Request ccpaInnerReq `json:"request"`
}

type ccpaInnerReq struct {
	Contents          []ccpaContent `json:"contents"`
	SystemInstruction *ccpaContent  `json:"systemInstruction,omitempty"`
	GenerationConfig  ccpaGenConfig `json:"generationConfig"`
}

type ccpaContent struct {
	Role  string     `json:"role,omitempty"`
	Parts []ccpaPart `json:"parts"`
}

type ccpaPart struct {
	Text    string `json:"text"`
	Thought bool   `json:"thought,omitempty"`
}

type ccpaThinkingConfig struct {
	IncludeThoughts bool          `json:"includeThoughts"`
	ThinkingLevel   ThinkingLevel `json:"thinkingLevel,omitempty"`
	ThinkingBudget  *int          `json:"thinkingBudget,omitempty"`
}

type ccpaGenConfig struct {
	Temperature     *float64            `json:"temperature,omitempty"`
	MaxOutputTokens int                 `json:"maxOutputTokens,omitempty"`
	ThinkingConfig  *ccpaThinkingConfig `json:"thinkingConfig,omitempty"`
}

type ccpaUsageMetadata struct {
	PromptTokenCount     int `json:"promptTokenCount"`
	CandidatesTokenCount int `json:"candidatesTokenCount"`
	ThoughtsTokenCount   int `json:"thoughtsTokenCount"`
	TotalTokenCount      int `json:"totalTokenCount"`
}

type ccpaStreamChunk struct {
	Response *struct {
		Candidates []struct {
			Content struct {
				Parts []ccpaPart `json:"parts"`
			} `json:"content"`
			FinishReason string `json:"finishReason,omitempty"`
		} `json:"candidates"`
		UsageMetadata *ccpaUsageMetadata `json:"usageMetadata,omitempty"`
		ModelVersion  string             `json:"modelVersion,omitempty"`
	} `json:"response,omitempty"`
	Candidates []struct {
		Content struct {
			Parts []ccpaPart `json:"parts"`
		} `json:"content"`
		FinishReason string `json:"finishReason,omitempty"`
	} `json:"candidates,omitempty"`
	UsageMetadata *ccpaUsageMetadata `json:"usageMetadata,omitempty"`
	ModelVersion  string             `json:"modelVersion,omitempty"`
}

func stringSliceContains(items []string, target string) bool {
	for _, s := range items {
		if strings.EqualFold(strings.TrimSpace(s), strings.TrimSpace(target)) {
			return true
		}
	}
	return false
}

func primaryFlagshipTierForModel(endpoint string, isConnected bool, resolved ResolvedModel) []string {
	canon := strings.ToLower(strings.TrimSpace(resolved.CanonicalID))
	ccpaID := strings.ToLower(strings.TrimSpace(resolved.CCPAModelID))
	if resolved.ThinkingLevel == ThinkingLevelLow ||
		strings.Contains(canon, "lite") ||
		strings.HasSuffix(canon, "-low") ||
		ccpaID == "gemini-3-flash" ||
		strings.Contains(ccpaID, "image") {
		return nil
	}

	isDefaultProd := strings.EqualFold(strings.TrimSpace(endpoint), DefaultCCPAEndpoint)
	if isDefaultProd && isConnected {
		switch {
		case resolved.IsCarbonFamily || strings.HasPrefix(ccpaID, "gemini-4-carbon"):
			if resolved.ThinkingLevel == ThinkingLevelMedium {
				return []string{"gemini-4-carbon-medium", "gemini-4-carbon", "gemini-4-argon-medium", "gemini-3.8-flash-medium", "gemini-3.8-flash-high"}
			}
			return []string{"gemini-4-carbon-high", "gemini-4-carbon", "gemini-4-argon-high", "gemini-3.8-flash-high"}
		case resolved.IsArgonFamily || strings.HasPrefix(ccpaID, "gemini-4-argon"):
			if resolved.ThinkingLevel == ThinkingLevelMedium {
				return []string{"gemini-4-argon-medium", "gemini-4-argon", "gemini-4-carbon-medium", "gemini-3.8-flash-medium", "gemini-3.8-flash-high"}
			}
			return []string{"gemini-4-argon-high", "gemini-4-argon", "gemini-4-carbon-high", "gemini-3.8-flash-high"}
		case strings.HasPrefix(ccpaID, "gemini-3.1-pro") || strings.Contains(canon, "gemini-3.1-pro"):
			return []string{"gemini-3.1-pro-high", "gemini-3.8-flash-high"}
		case strings.HasPrefix(ccpaID, "gemini-3.8-flash") || strings.Contains(canon, "gemini-3.8-flash"):
			if strings.Contains(canon, "tiered") {
				return []string{"gemini-3.8-flash-tiered", "gemini-3.8-flash-high"}
			}
			if resolved.ThinkingLevel == ThinkingLevelMedium {
				return []string{"gemini-3.8-flash-medium", "gemini-3.8-flash-high"}
			}
			return []string{"gemini-3.8-flash-high"}
		case strings.HasPrefix(ccpaID, "claude-opus"):
			return []string{"claude-opus-4-6-thinking"}
		case strings.HasPrefix(ccpaID, "claude-sonnet"):
			return []string{"claude-sonnet-4-6", "claude-opus-4-6-thinking"}
		}
		return nil
	}

	switch {
	case resolved.IsCarbonFamily || strings.HasPrefix(ccpaID, "gemini-4-carbon"):
		if resolved.ThinkingLevel == ThinkingLevelMedium {
			return []string{"gemini-4-carbon-medium", "gemini-4-carbon", "gemini-4-argon-medium"}
		}
		return []string{"gemini-4-carbon-high", "gemini-4-carbon", "gemini-4-argon-high", "gemini-4-argon", "gemini-3.1-pro-high"}
	case resolved.IsArgonFamily || strings.HasPrefix(ccpaID, "gemini-4-argon"):
		if resolved.ThinkingLevel == ThinkingLevelMedium {
			return []string{"gemini-4-argon-medium", "gemini-4-argon", "gemini-4-carbon-medium"}
		}
		return []string{"gemini-4-argon-high", "gemini-4-argon", "gemini-4-carbon-high", "gemini-4-carbon", "gemini-3.1-pro-high"}
	case strings.HasPrefix(ccpaID, "gemini-3.1-pro") || strings.Contains(canon, "gemini-3.1-pro"):
		return []string{"gemini-3.1-pro-high", "gemini-3.1-pro-preview"}
	case strings.HasPrefix(ccpaID, "claude-opus"):
		return []string{"claude-opus-4-6-thinking", "claude-sonnet-4-6"}
	case strings.HasPrefix(ccpaID, "claude-sonnet"):
		return []string{"claude-sonnet-4-6", "claude-opus-4-6-thinking"}
	}
	return nil
}

func (u *GatewayUpstream) handleDirectCCPA(
	ctx context.Context,
	w http.ResponseWriter,
	req OpenAIChatRequest,
	resolved ResolvedModel,
	oauthToken string,
	projectID string,
) error {
	var sysParts []ccpaPart
	var contents []ccpaContent

	for _, m := range req.Messages {
		text := m.ContentText()
		if text == "" {
			continue
		}
		switch strings.ToLower(m.Role) {
		case "system", "developer":
			sysParts = append(sysParts, ccpaPart{Text: text})
		case "assistant", "model":
			contents = append(contents, ccpaContent{
				Role:  "model",
				Parts: []ccpaPart{{Text: text}},
			})
		default:
			contents = append(contents, ccpaContent{
				Role:  "user",
				Parts: []ccpaPart{{Text: text}},
			})
		}
	}

	if len(contents) == 0 {
		contents = append(contents, ccpaContent{
			Role:  "user",
			Parts: []ccpaPart{{Text: "Hello"}},
		})
	}

	temp := 0.2
	if req.Temperature != nil {
		temp = *req.Temperature
	}

	maxOut := req.EffectiveMaxTokens(DefaultCCPAMaxOutputTokens)
	if maxOut > DefaultCCPAMaxOutputTokens {
		maxOut = DefaultCCPAMaxOutputTokens
	}

	genCfg := ccpaGenConfig{
		Temperature:     &temp,
		MaxOutputTokens: maxOut,
	}
	zeroBudget := 0
	if req.ThinkingConfig != nil {
		if !req.ThinkingConfig.IncludeThoughts && req.ThinkingConfig.ThinkingBudget == 0 && req.ThinkingConfig.ThinkingLevel == ThinkingLevelNone {
			genCfg.ThinkingConfig = &ccpaThinkingConfig{
				IncludeThoughts: false,
				ThinkingBudget:  &zeroBudget,
			}
		} else {
			var bPtr *int
			if req.ThinkingConfig.ThinkingBudget > 0 {
				b := req.ThinkingConfig.ThinkingBudget
				bPtr = &b
			}
			genCfg.ThinkingConfig = &ccpaThinkingConfig{
				IncludeThoughts: req.ThinkingConfig.IncludeThoughts,
				ThinkingLevel:   req.ThinkingConfig.ThinkingLevel,
				ThinkingBudget:  bPtr,
			}
		}
	} else if resolved.ThinkingLevel == ThinkingLevelLow {
		// On CCPA, thinkingBudget=0 is 3.4x faster than thinkingLevel="LOW"
		genCfg.ThinkingConfig = &ccpaThinkingConfig{
			IncludeThoughts: false,
			ThinkingBudget:  &zeroBudget,
		}
	} else if resolved.ThinkingLevel == ThinkingLevelMedium {
		medBudget := 1024
		genCfg.ThinkingConfig = &ccpaThinkingConfig{
			IncludeThoughts: resolved.IncludeThoughts,
			ThinkingBudget:  &medBudget,
		}
	} else if resolved.IncludeThoughts || resolved.ThinkingLevel != ThinkingLevelNone {
		genCfg.ThinkingConfig = &ccpaThinkingConfig{
			IncludeThoughts: resolved.IncludeThoughts,
			ThinkingLevel:   resolved.ThinkingLevel,
		}
	}

	inner := ccpaInnerReq{
		Contents:         contents,
		GenerationConfig: genCfg,
	}
	if len(sysParts) > 0 {
		inner.SystemInstruction = &ccpaContent{Parts: sysParts}
	}

	if ctx.Err() != nil {
		return ctx.Err()
	}

	endpoint := u.cfg.CCPAEndpoint
	if endpoint == "" {
		endpoint = DefaultCCPAEndpoint
	}
	accScope := accountCacheScope(w, oauthToken)
	noThinking := req.ThinkingConfig != nil && !req.ThinkingConfig.IncludeThoughts && req.ThinkingConfig.ThinkingBudget == 0 && req.ThinkingConfig.ThinkingLevel == ThinkingLevelNone
	isDefaultProdEndpoint := strings.EqualFold(strings.TrimSpace(endpoint), DefaultCCPAEndpoint)
	firstChunkTimeout := effectiveCCPAFirstChunkTimeout(req)

	hedgeModelID := strings.TrimSpace(resolved.CanonicalID)
	if hedgeModelID == "" {
		hedgeModelID = strings.TrimSpace(resolved.RequestedID)
	}
	if hedgeModelID == "" {
		hedgeModelID = strings.TrimSpace(req.Model)
	}

	lease := ccpaPrimaryLeaseFromContext(ctx)
	primaryEmail := ""
	if lease != nil && strings.Contains(lease.accountEmail, "@") {
		primaryEmail = strings.TrimSpace(lease.accountEmail)
	} else if strings.Contains(accScope, "@") {
		primaryEmail = strings.TrimSpace(accScope)
	}

	var opened *ccpaOpenedAttempt
	isDeepHedge := IsDeepReasoningGeminiModel(hedgeModelID)
	isFastHedge := !isDeepHedge && IsFastSpeedGeminiModel(hedgeModelID) && TotalAntigravityInFlight() < 22
	shouldHedgeTTFT := isDefaultProdEndpoint &&
		!IsPinnedAccount(ctx) &&
		(isDeepHedge || isFastHedge) &&
		primaryEmail != ""

	if !shouldHedgeTTFT {
		var err error
		opened, err = u.openCCPAStreamWithFirstChunk(ctx, nil, endpoint, accScope, oauthToken, projectID, req, resolved, inner, noThinking, firstChunkTimeout)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
		defer opened.closeAndRelease()
	} else {
		var hedgeDelay time.Duration
		if isFastHedge {
			hedgeDelay = CCPAHedgeDelayFast
		} else {
			primaryTier := "Pro"
			if acc, ok := lookupConnectedAntigravityAccountFast(primaryEmail); ok {
				if t := strings.TrimSpace(acc.Tier); t != "" {
					primaryTier = t
				} else {
					primaryTier = LookupCachedAccountTier(acc, nil)
				}
			}
			hedgeDelay = CCPAHedgeDelayPro
			if strings.EqualFold(strings.TrimSpace(primaryTier), "Ultra") {
				hedgeDelay = CCPAHedgeDelayUltra
			}
		}

		primCtx, cancelPrim := context.WithCancel(ctx)
		primCtrl := &ccpaRacerControl{cancel: cancelPrim}
		primCh := make(chan ccpaRaceResult, 1)
		go func() {
			att, err := u.openCCPAStreamWithFirstChunk(primCtx, primCtrl, endpoint, accScope, oauthToken, projectID, req, resolved, inner, noThinking, firstChunkTimeout)
			primCh <- ccpaRaceResult{opened: att, err: err}
		}()

		hedgeTimer := time.NewTimer(hedgeDelay)
		defer hedgeTimer.Stop()

		select {
		case res1 := <-primCh:
			if res1.err != nil || res1.opened == nil {
				primCtrl.abortNow()
				if ctx.Err() != nil {
					return ctx.Err()
				}
				return res1.err
			}
			opened = res1.opened
			defer func() {
				primCtrl.abortNow()
				opened.closeAndRelease()
			}()

		case <-ctx.Done():
			primCtrl.abortNow()
			go func() {
				if r1 := <-primCh; r1.opened != nil {
					r1.opened.closeAndRelease()
				}
			}()
			return ctx.Err()

		case <-hedgeTimer.C:
			hedgePickCtx := withHedgeAlternateAccountContext(ctx, primaryEmail)
			secAcc, rawSecRelease, secPickErr := AcquireProviderAccountForModel(
				hedgePickCtx,
				ProviderAntigravity,
				hedgeModelID,
				resolved.CanonicalID,
			)
			if secPickErr != nil || secAcc == nil || strings.TrimSpace(secAcc.AccessToken) == "" || strings.EqualFold(secAcc.Email, primaryEmail) {
				if rawSecRelease != nil {
					rawSecRelease()
				}
				select {
				case res1 := <-primCh:
					if res1.err != nil || res1.opened == nil {
						primCtrl.abortNow()
						if ctx.Err() != nil {
							return ctx.Err()
						}
						return res1.err
					}
					opened = res1.opened
					defer func() {
						primCtrl.abortNow()
						opened.closeAndRelease()
					}()
				case <-ctx.Done():
					primCtrl.abortNow()
					go func() {
						if r1 := <-primCh; r1.opened != nil {
							r1.opened.closeAndRelease()
						}
					}()
					return ctx.Err()
				}
			} else {
				var secRelOnce sync.Once
				secEmail := strings.TrimSpace(secAcc.Email)
				releaseSecOnce := func() {
					secRelOnce.Do(func() {
						if rawSecRelease != nil {
							rawSecRelease()
						} else if secEmail != "" {
							ReleaseAccountConcurrency(secEmail)
						}
					})
				}

				secToken := strings.TrimSpace(secAcc.AccessToken)
				secProj := strings.TrimSpace(secAcc.ProjectID)
				if secProj == "" {
					secProj = strings.TrimSpace(u.cfg.AgyProjectID)
				}
				secCtx, cancelSec := context.WithCancel(ctx)
				secCtrl := &ccpaRacerControl{cancel: cancelSec}
				secCh := make(chan ccpaRaceResult, 1)
				go func() {
					att, err := u.openCCPAStreamWithFirstChunk(secCtx, secCtrl, endpoint, secEmail, secToken, secProj, req, resolved, inner, noThinking, firstChunkTimeout)
					secCh <- ccpaRaceResult{opened: att, err: err}
				}()

				primWait := primCh
				secWait := secCh
				var lastRaceErr error

				for primWait != nil || secWait != nil {
					select {
					case <-ctx.Done():
						primCtrl.abortNow()
						secCtrl.abortNow()
						releaseSecOnce()
						if primWait != nil {
							go func(ch <-chan ccpaRaceResult) {
								if r1 := <-ch; r1.opened != nil {
									r1.opened.closeAndRelease()
								}
							}(primWait)
						}
						if secWait != nil {
							go func(ch <-chan ccpaRaceResult) {
								if r2 := <-ch; r2.opened != nil {
									r2.opened.closeAndRelease()
								}
							}(secWait)
						}
						return ctx.Err()

					case r1 := <-primWait:
						primWait = nil
						if r1.err == nil && r1.opened != nil {
							// Primary won: cancel, close body, and release concurrency on secondary loser BEFORE touching w.
							secCtrl.abortNow()
							releaseSecOnce()
							if secWait != nil {
								go func(ch <-chan ccpaRaceResult) {
									if r2 := <-ch; r2.opened != nil {
										r2.opened.closeAndRelease()
									}
								}(secWait)
								secWait = nil
							}
							opened = r1.opened
							defer func() {
								primCtrl.abortNow()
								opened.closeAndRelease()
							}()
							break
						}
						primCtrl.abortNow()
						if secWait != nil && lease != nil && lease.releaseInFlight != nil {
							lease.releaseInFlight()
						}
						if r1.err != nil {
							lastRaceErr = r1.err
						}

					case r2 := <-secWait:
						secWait = nil
						if r2.err == nil && r2.opened != nil {
							// Secondary hedge won: cancel, close body, and release concurrency on primary loser BEFORE touching w.
							primCtrl.abortNow()
							if lease != nil {
								if lease.releaseInFlight != nil {
									lease.releaseInFlight()
								}
								lease.winnerEmail = secEmail
								lease.winnerRelease = releaseSecOnce
								lease.hedgedWon = true
							} else {
								if primaryEmail != "" && GetAccountInFlight(primaryEmail) > 0 {
									ReleaseAccountConcurrency(primaryEmail)
								}
								defer releaseSecOnce()
							}
							if primWait != nil {
								go func(ch <-chan ccpaRaceResult) {
									if r1 := <-ch; r1.opened != nil {
										r1.opened.closeAndRelease()
									}
								}(primWait)
								primWait = nil
							}
							accScope = secEmail
							w.Header().Set("X-Omni-Gateway-Account", secEmail)
							w.Header().Set("X-Omni-Gateway-Hedged-Racing", "WIN")
							opened = r2.opened
							defer func() {
								secCtrl.abortNow()
								opened.closeAndRelease()
							}()
							break
						}
						secCtrl.abortNow()
						releaseSecOnce()
						if lastRaceErr == nil && r2.err != nil {
							lastRaceErr = r2.err
						}
					}
					if opened != nil {
						break
					}
				}

				if opened == nil {
					if ctx.Err() != nil {
						return ctx.Err()
					}
					return lastRaceErr
				}
			}
		}
	}

	activeCCPAModel := opened.activeCCPAModel
	activeModelVersion := opened.activeModelVersion
	activeScanner := opened.activeScanner
	activeIdleTimer := opened.activeIdleTimer
	activeCandCtx := opened.activeCandCtx
	activeTimedOut := opened.activeTimedOut
	initialText := opened.initialText
	initialThoughts := opened.initialThoughts
	lastUsage := opened.lastUsage
	lastFinishReason := opened.lastFinishReason
	cacheReq := cacheKeyRequestFromContext(ctx, req)

	isAutoFastReq := strings.EqualFold(strings.TrimSpace(resolved.RequestedID), "auto/fast") ||
		strings.EqualFold(strings.TrimSpace(resolved.CanonicalID), "auto/fast")
	isGeminiReq := strings.Contains(resolved.CanonicalID, "gemini") || resolved.IsCarbonFamily || resolved.IsArgonFamily
	isClaudeWinner := strings.HasPrefix(strings.ToLower(activeCCPAModel), "claude-")
	if !noThinking && !(isDefaultProdEndpoint && (isAutoFastReq || resolved.CCPAModelID == "gemini-3.8-flash-low")) && resolved.CanonicalID != "" && !(isGeminiReq && isClaudeWinner) {
		workingCCPAModelCache.Store(endpoint+"|acc:"+normalizeAccScope(accScope)+"|"+resolved.CanonicalID, activeCCPAModel)
	}

	if strings.Contains(accScope, "@") && activeCCPAModel != "" {
		liveBackend := activeCCPAModel
		if activeModelVersion != "" && !strings.EqualFold(activeModelVersion, activeCCPAModel) {
			liveBackend = fmt.Sprintf("%s [%s]", activeCCPAModel, activeModelVersion)
		}
		if !strings.EqualFold("antigravity/"+activeCCPAModel, resolved.CanonicalID) && !strings.EqualFold(activeCCPAModel, resolved.CanonicalID) {
			setAccountLiveRole(accScope, fmt.Sprintf("%s ➔ %s", resolved.CanonicalID, liveBackend))
		} else if activeModelVersion != "" && !strings.EqualFold(activeModelVersion, activeCCPAModel) {
			setAccountLiveRole(accScope, fmt.Sprintf("%s [%s]", resolved.CanonicalID, activeModelVersion))
		}
	}

	created := time.Now().Unix()
	respID := fmt.Sprintf("chatcmpl-ccpa-%d", time.Now().UnixNano())

	if req.Stream {
		var streamedText strings.Builder
		var streamedThoughts strings.Builder

		w.Header().Set("X-Omni-Gateway-Resolved-Model", activeCCPAModel)
		if activeModelVersion != "" {
			w.Header().Set("X-Omni-Gateway-Upstream-Model-Version", activeModelVersion)
		}
		w.Header().Set("X-Omni-Gateway-Route", "ccpa-direct")
		if resolved.ThinkingLevel != ThinkingLevelNone {
			w.Header().Set("X-Omni-Gateway-Thinking-Level", string(resolved.ThinkingLevel))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		if flusher != nil {
			flusher.Flush()
		}

		stopHeartbeat := make(chan struct{})
		defer close(stopHeartbeat)
		go func() {
			ticker := time.NewTicker(2500 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-stopHeartbeat:
					return
				case <-ctx.Done():
					return
				case <-ticker.C:
					_, _ = fmt.Fprintf(w, ": ping - thinking\n\n")
					if flusher != nil {
						flusher.Flush()
					}
				}
			}
		}()

		if len(initialText) > 0 || len(initialThoughts) > 0 {
			streamedText.WriteString(initialText)
			streamedThoughts.WriteString(initialThoughts)
			firstChunk := OpenAIChatResponse{
				ID:      respID,
				Object:  "chat.completion.chunk",
				Created: created,
				Model:   resolved.RequestedID,
				Choices: []OpenAIChatChoice{
					{
						Index: 0,
						Delta: &OpenAIOutputMessage{
							Role:             "assistant",
							Content:          initialText,
							ReasoningContent: initialThoughts,
						},
					},
				},
			}
			b, _ := json.Marshal(firstChunk)
			_, _ = fmt.Fprintf(w, "data: %s\n\n", b)
			if flusher != nil {
				flusher.Flush()
			}
		}

		for activeScanner.Scan() {
			activeIdleTimer.Reset(CCPAStreamIdleTimeout)
			line := strings.TrimSpace(activeScanner.Text())
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			dataStr := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if dataStr == "" || dataStr == "[DONE]" {
				continue
			}
			textDelta, thoughtDelta, uMeta, rawFR, mVer := extractCCPAChunkWithVersion(dataStr)
			if mVer != "" && activeModelVersion == "" {
				activeModelVersion = mVer
				w.Header().Set("X-Omni-Gateway-Upstream-Model-Version", activeModelVersion)
			}
			if uMeta != nil && (uMeta.TotalTokenCount > 0 || uMeta.PromptTokenCount > 0 || uMeta.CandidatesTokenCount > 0) {
				lastUsage = uMeta
			}
			if rawFR != "" {
				lastFinishReason = rawFR
			}
			if textDelta == "" && thoughtDelta == "" {
				continue
			}
			streamedText.WriteString(textDelta)
			streamedThoughts.WriteString(thoughtDelta)
			chunk := OpenAIChatResponse{
				ID:      respID,
				Object:  "chat.completion.chunk",
				Created: created,
				Model:   resolved.RequestedID,
				Choices: []OpenAIChatChoice{
					{
						Index: 0,
						Delta: &OpenAIOutputMessage{
							Role:             "assistant",
							Content:          textDelta,
							ReasoningContent: thoughtDelta,
						},
					},
				},
			}
			b, _ := json.Marshal(chunk)
			_, _ = fmt.Fprintf(w, "data: %s\n\n", b)
			if flusher != nil {
				flusher.Flush()
			}
		}

		streamScanErr := activeScanner.Err()
		if streamScanErr != nil && ctx.Err() == nil && (activeTimedOut.Load() || activeCandCtx.Err() != nil) {
			if accScope != "" && accScope != "anon" && strings.Contains(accScope, "@") {
				markAccountDeadCCPAModelWithStatus(endpoint, accScope, activeCCPAModel, 60*time.Second, http.StatusGatewayTimeout)
			} else {
				markDeadCCPAModel(endpoint, activeCCPAModel, 2*time.Minute)
			}
		}

		usage := computeCCPAUsage(req, streamedText.Len(), streamedThoughts.Len(), lastUsage)
		w.Header().Set("X-Omni-Gateway-Tokens", fmt.Sprintf("%d", usage.TotalTokens))
		SetAuditResponse(ctx, streamedText.String(), usage.PromptTokens, usage.CompletionTokens)

		stopReason := normalizeCCPAFinishReason(lastFinishReason)
		finalChunk := OpenAIChatResponse{
			ID:      respID,
			Object:  "chat.completion.chunk",
			Created: created,
			Model:   resolved.RequestedID,
			Choices: []OpenAIChatChoice{
				{
					Index:        0,
					Delta:        &OpenAIOutputMessage{},
					FinishReason: stopReason,
				},
			},
			Usage: &usage,
		}
		b, _ := json.Marshal(finalChunk)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", b)
		_, _ = fmt.Fprintf(w, "data: [DONE]\n\n")
		if flusher != nil {
			flusher.Flush()
		}

		cleanStreamFinish := lastFinishReason == "" || stopReason == "stop" || stopReason == "end_turn"
		if streamScanErr == nil && ctx.Err() == nil && cleanStreamFinish && streamedText.Len() > 0 && !IsUnsupportedClientBanner(streamedText.String()) {
			cachedResp := OpenAIChatResponse{
				ID:      respID,
				Object:  "chat.completion",
				Created: created,
				Model:   resolved.RequestedID,
				Choices: []OpenAIChatChoice{
					{
						Index: 0,
						Message: &OpenAIOutputMessage{
							Role:             "assistant",
							Content:          streamedText.String(),
							ReasoningContent: streamedThoughts.String(),
						},
						FinishReason: stopReason,
					},
				},
				Usage: &usage,
			}
			GetGlobalCache().Put(cacheReq, cachedResp)
		}
		return nil
	}

	var fullText strings.Builder
	var fullThoughts strings.Builder
	fullText.WriteString(initialText)
	fullThoughts.WriteString(initialThoughts)

	for activeScanner.Scan() {
		activeIdleTimer.Reset(CCPAStreamIdleTimeout)
		line := strings.TrimSpace(activeScanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		dataStr := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if dataStr == "" || dataStr == "[DONE]" {
			continue
		}
		tDelta, thDelta, uMeta, rawFR, mVer := extractCCPAChunkWithVersion(dataStr)
		if mVer != "" {
			activeModelVersion = mVer
		}
		if uMeta != nil && (uMeta.TotalTokenCount > 0 || uMeta.PromptTokenCount > 0 || uMeta.CandidatesTokenCount > 0) {
			lastUsage = uMeta
		}
		if rawFR != "" {
			lastFinishReason = rawFR
		}
		fullText.WriteString(tDelta)
		fullThoughts.WriteString(thDelta)
	}

	if err := activeScanner.Err(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if activeTimedOut.Load() || activeCandCtx.Err() != nil {
			if accScope != "" && accScope != "anon" && strings.Contains(accScope, "@") {
				markAccountDeadCCPAModelWithStatus(endpoint, accScope, activeCCPAModel, 60*time.Second, http.StatusGatewayTimeout)
			} else {
				markDeadCCPAModel(endpoint, activeCCPAModel, 2*time.Minute)
			}
		}
		return err
	}

	if IsUnsupportedClientBanner(fullText.String()) {
		return fmt.Errorf("CCPA client version rejected by upstream (update OMNI_AGY_USER_AGENT): %s", TruncateUTF8(fullText.String(), 200))
	}

	w.Header().Set("X-Omni-Gateway-Resolved-Model", activeCCPAModel)
	if activeModelVersion != "" {
		w.Header().Set("X-Omni-Gateway-Upstream-Model-Version", activeModelVersion)
	}
	w.Header().Set("X-Omni-Gateway-Route", "ccpa-direct")
	if resolved.ThinkingLevel != ThinkingLevelNone {
		w.Header().Set("X-Omni-Gateway-Thinking-Level", string(resolved.ThinkingLevel))
	}

	usage := computeCCPAUsage(req, fullText.Len(), fullThoughts.Len(), lastUsage)
	w.Header().Set("X-Omni-Gateway-Tokens", fmt.Sprintf("%d", usage.TotalTokens))
	SetAuditResponse(ctx, fullText.String(), usage.PromptTokens, usage.CompletionTokens)

	stopReason := normalizeCCPAFinishReason(lastFinishReason)
	outResp := OpenAIChatResponse{
		ID:      respID,
		Object:  "chat.completion",
		Created: created,
		Model:   resolved.RequestedID,
		Choices: []OpenAIChatChoice{
			{
				Index: 0,
				Message: &OpenAIOutputMessage{
					Role:             "assistant",
					Content:          fullText.String(),
					ReasoningContent: fullThoughts.String(),
				},
				FinishReason: stopReason,
			},
		},
		Usage: &usage,
	}

	if stopReason != "length" && fullText.Len() > 0 {
		GetGlobalCache().Put(cacheReq, outResp)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	return json.NewEncoder(w).Encode(outResp)
}

type ccpaRacerControl struct {
	cancel context.CancelFunc
	mu     sync.Mutex
	body   io.ReadCloser
	closed bool
}

func (rc *ccpaRacerControl) trackBody(b io.ReadCloser) bool {
	if rc == nil {
		return true
	}
	rc.mu.Lock()
	defer rc.mu.Unlock()
	if rc.closed {
		if b != nil {
			_ = b.Close()
		}
		return false
	}
	rc.body = b
	return true
}

func (rc *ccpaRacerControl) clearTrackedBody() {
	if rc == nil {
		return
	}
	rc.mu.Lock()
	rc.body = nil
	rc.mu.Unlock()
}

func (rc *ccpaRacerControl) abortNow() {
	if rc == nil {
		return
	}
	if rc.cancel != nil {
		rc.cancel()
	}
	rc.mu.Lock()
	rc.closed = true
	b := rc.body
	rc.body = nil
	rc.mu.Unlock()
	if b != nil {
		_ = b.Close()
	}
}

type ccpaOpenedAttempt struct {
	resp               *http.Response
	activeCCPAModel    string
	activeModelVersion string
	activeScanner      *bufio.Scanner
	activeIdleTimer    *time.Timer
	activeCandCtx      context.Context
	activeCancel       context.CancelFunc
	activeTimedOut     *atomic.Bool
	initialText        string
	initialThoughts    string
	lastUsage          *ccpaUsageMetadata
	lastFinishReason   string
	releaseScope       func()
	closeOnce          sync.Once
}

func (a *ccpaOpenedAttempt) closeAndRelease() {
	if a == nil {
		return
	}
	a.closeOnce.Do(func() {
		if a.activeIdleTimer != nil {
			a.activeIdleTimer.Stop()
		}
		if a.activeCancel != nil {
			a.activeCancel()
		}
		if a.resp != nil && a.resp.Body != nil {
			_ = a.resp.Body.Close()
		}
		if a.releaseScope != nil {
			a.releaseScope()
		}
	})
}

type ccpaRaceResult struct {
	opened *ccpaOpenedAttempt
	err    error
}

func (u *GatewayUpstream) openCCPAStreamWithFirstChunk(
	attemptCtx context.Context,
	ctrl *ccpaRacerControl,
	endpoint string,
	accScope string,
	oauthToken string,
	projectID string,
	req OpenAIChatRequest,
	resolved ResolvedModel,
	inner ccpaInnerReq,
	noThinking bool,
	firstChunkTimeout time.Duration,
) (*ccpaOpenedAttempt, error) {
	releaseScopeInFlight := acquireCCPAScopeInFlight(accScope, oauthToken)
	ccpaCandidates := buildCCPACandidatesWithCache(endpoint, accScope, resolved, noThinking)
	_, isConnectedScope := lookupConnectedAntigravityAccountFast(accScope)
	flagshipTier := primaryFlagshipTierForModel(endpoint, isConnectedScope, resolved)
	isDefaultProdEndpoint := strings.EqualFold(strings.TrimSpace(endpoint), DefaultCCPAEndpoint)

	var lastCCPAErr error
	saw5xxInAttempt := false

	for _, candidate := range ccpaCandidates {
		if attemptCtx.Err() != nil {
			releaseScopeInFlight()
			return nil, attemptCtx.Err()
		}

		// Multi-Account Flagship Protection: before downgrading to a lower model tier on the current account
		// after a 429/404, check if another healthy Antigravity account still has flagship quota available.
		// Never abort a local step-down after a transient per-account 5xx!
		if !saw5xxInAttempt &&
			PinnedAccountFromContext(attemptCtx) == "" &&
			len(flagshipTier) > 0 &&
			!stringSliceContains(flagshipTier, candidate) &&
			hasAlternateFlagshipAccount(endpoint, accScope, resolved.CanonicalID, flagshipTier) {
			if strings.Contains(accScope, "@") {
				RecordAccountModelRateLimit(accScope, resolved.CanonicalID, 2*time.Minute)
			}
			if lastCCPAErr != nil {
				lastCCPAErr = fmt.Errorf("[flagship-failover] %w", lastCCPAErr)
			} else {
				lastCCPAErr = fmt.Errorf("[flagship-failover] CCPA HTTP 429: flagship tier %v rate-limited on %s", flagshipTier, accScope)
			}
			break
		}

		candInner := inner
		if strings.Contains(candidate, "lite") && candInner.GenerationConfig.MaxOutputTokens > 32768 {
			candInner.GenerationConfig.MaxOutputTokens = 32768
		}
		callerWantsThoughts := req.ThinkingConfig != nil && req.ThinkingConfig.IncludeThoughts
		if (strings.HasPrefix(candidate, "claude-") ||
			candidate == "gemini-3.1-flash-lite" ||
			strings.Contains(candidate, "image")) && candInner.GenerationConfig.ThinkingConfig != nil {
			candInner.GenerationConfig.ThinkingConfig = nil
		} else if candidate == "gemini-3-flash" {
			if !callerWantsThoughts {
				candInner.GenerationConfig.ThinkingConfig = &ccpaThinkingConfig{
					IncludeThoughts: false,
					ThinkingLevel:   ThinkingLevelLow,
				}
			} else {
				lvl := req.ThinkingConfig.ThinkingLevel
				if lvl == ThinkingLevelNone {
					lvl = ThinkingLevelLow
				}
				candInner.GenerationConfig.ThinkingConfig = &ccpaThinkingConfig{
					IncludeThoughts: true,
					ThinkingLevel:   lvl,
				}
			}
			if candInner.GenerationConfig.MaxOutputTokens > 0 && candInner.GenerationConfig.MaxOutputTokens < 1024 {
				candInner.GenerationConfig.MaxOutputTokens += 1024
			}
		} else if candidate == "gemini-3.5-flash-lite" {
			if !callerWantsThoughts {
				zBudget := 0
				candInner.GenerationConfig.ThinkingConfig = &ccpaThinkingConfig{
					IncludeThoughts: false,
					ThinkingLevel:   "",
					ThinkingBudget:  &zBudget,
				}
			} else if req.ThinkingConfig.ThinkingBudget > 0 {
				b := req.ThinkingConfig.ThinkingBudget
				candInner.GenerationConfig.ThinkingConfig = &ccpaThinkingConfig{
					IncludeThoughts: true,
					ThinkingBudget:  &b,
				}
			} else {
				candInner.GenerationConfig.ThinkingConfig = nil
			}
			if candInner.GenerationConfig.MaxOutputTokens > 0 && candInner.GenerationConfig.MaxOutputTokens < 1024 {
				candInner.GenerationConfig.MaxOutputTokens += 1024
			}
		} else if candidate == "gemini-3.8-flash-high" || candidate == "gemini-3.8-flash-medium" || (isDefaultProdEndpoint && candidate == "gemini-3.8-flash-low") {
			if !noThinking {
				candInner.GenerationConfig.ThinkingConfig = &ccpaThinkingConfig{IncludeThoughts: true}
			} else {
				candInner.GenerationConfig.ThinkingConfig = nil
			}
			if candInner.GenerationConfig.MaxOutputTokens < 4096 {
				candInner.GenerationConfig.MaxOutputTokens += 4096
			}
		} else if candidate == "gemini-3.8-flash-tiered" && candInner.GenerationConfig.ThinkingConfig != nil {
			origTC := *candInner.GenerationConfig.ThinkingConfig
			cappedBudget := 1024
			if resolved.ThinkingLevel == ThinkingLevelLow || (origTC.ThinkingBudget != nil && *origTC.ThinkingBudget == 0) {
				cappedBudget = 0
			} else if origTC.ThinkingBudget != nil && *origTC.ThinkingBudget > 0 && *origTC.ThinkingBudget < cappedBudget {
				cappedBudget = *origTC.ThinkingBudget
			}
			candInner.GenerationConfig.ThinkingConfig = &ccpaThinkingConfig{
				IncludeThoughts: origTC.IncludeThoughts,
				ThinkingLevel:   "",
				ThinkingBudget:  &cappedBudget,
			}
			if cappedBudget > 0 && candInner.GenerationConfig.MaxOutputTokens < 2048 {
				candInner.GenerationConfig.MaxOutputTokens += 2048
			}
		}

		payload := ccpaRequestPayload{
			Model:   candidate,
			Project: projectID,
			Request: candInner,
		}

		bodyBytes, err := json.Marshal(payload)
		if err != nil {
			lastCCPAErr = err
			continue
		}

		candCtx, cancelCand := context.WithTimeout(attemptCtx, CCPACandidateMaxTimeout)
		var timedOut atomic.Bool
		idleTimer := time.AfterFunc(firstChunkTimeout, func() {
			timedOut.Store(true)
			cancelCand()
		})

		httpReq, err := http.NewRequestWithContext(candCtx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
		if err != nil {
			idleTimer.Stop()
			cancelCand()
			lastCCPAErr = err
			continue
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Accept", "text/event-stream")
		httpReq.Header.Set("User-Agent", GetAgyUserAgent())
		httpReq.Header.Set("Authorization", "Bearer "+oauthToken)

		r, err := u.ccpaHTTPClientForScope(accScope, oauthToken).Do(httpReq)
		if err != nil {
			idleTimer.Stop()
			candErr := candCtx.Err()
			cancelCand()
			if attemptCtx.Err() != nil {
				releaseScopeInFlight()
				return nil, attemptCtx.Err()
			}
			if timedOut.Load() || candErr != nil {
				if strings.Contains(accScope, "@") {
					markAccountDeadCCPAModelWithStatus(endpoint, accScope, candidate, 60*time.Second, http.StatusGatewayTimeout)
				} else {
					markDeadCCPAModel(endpoint, candidate, 2*time.Minute)
				}
			}
			lastCCPAErr = err
			log.Printf("[omni-gateway] CCPA candidate %q transport error: %v; trying next CCPA tier model", candidate, err)
			continue
		}

		if !ctrl.trackBody(r.Body) {
			idleTimer.Stop()
			cancelCand()
			releaseScopeInFlight()
			if attemptCtx.Err() != nil {
				return nil, attemptCtx.Err()
			}
			return nil, context.Canceled
		}

		if r.StatusCode < 200 || r.StatusCode >= 300 {
			idleTimer.Stop()
			errBody, _ := io.ReadAll(io.LimitReader(r.Body, 4096))
			ctrl.clearTrackedBody()
			_ = r.Body.Close()
			cancelCand()
			switch r.StatusCode {
			case http.StatusNotFound:
				if isDefaultProdEndpoint && strings.Contains(accScope, "@") {
					markAccountDeadCCPAModelWithStatus(endpoint, accScope, candidate, 15*time.Minute, http.StatusNotFound)
				} else {
					markDeadCCPAModel(endpoint, candidate, 6*time.Hour)
				}
			case http.StatusTooManyRequests:
				markAccountDeadCCPAModelWithStatus(endpoint, accScope, candidate, 2*time.Minute, http.StatusTooManyRequests)
				if strings.Contains(accScope, "@") {
					RecordAccountModelRateLimit(accScope, candidate, 2*time.Minute)
				}
				if isDefaultProdEndpoint && strings.HasPrefix(candidate, "claude-") {
					markAccountDeadCCPAModelWithStatus(endpoint, accScope, "claude-opus-4-6-thinking", 2*time.Minute, http.StatusTooManyRequests)
					markAccountDeadCCPAModelWithStatus(endpoint, accScope, "claude-sonnet-4-6", 2*time.Minute, http.StatusTooManyRequests)
					if strings.Contains(accScope, "@") {
						RecordAccountModelRateLimit(accScope, "claude-opus-4-6-thinking", 2*time.Minute)
						RecordAccountModelRateLimit(accScope, "claude-sonnet-4-6", 2*time.Minute)
					}
				}
			case http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
				saw5xxInAttempt = true
				if strings.Contains(accScope, "@") {
					markAccountDeadCCPAModelWithStatus(endpoint, accScope, candidate, 90*time.Second, r.StatusCode)
				} else {
					markDeadCCPAModel(endpoint, candidate, 2*time.Minute)
				}
			}
			lastCCPAErr = fmt.Errorf("CCPA HTTP %d (%s): %s", r.StatusCode, candidate, strings.TrimSpace(string(errBody)))
			if r.StatusCode == http.StatusUnauthorized || r.StatusCode == http.StatusForbidden {
				log.Printf("[omni-gateway] CCPA candidate %q returned HTTP %d; stopping tier fallback for account recovery/failover", candidate, r.StatusCode)
				break
			}
			log.Printf("[omni-gateway] CCPA candidate %q returned HTTP %d; trying next CCPA tier model", candidate, r.StatusCode)
			continue
		}

		scanner := bufio.NewScanner(r.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)

		var (
			candInitialText     strings.Builder
			candInitialThoughts strings.Builder
			candRawNonSSE       strings.Builder
			candLastUsage       *ccpaUsageMetadata
			candFinishReason    string
			candModelVersion    string
			foundSSE            bool
		)

		for scanner.Scan() {
			rawLine := scanner.Text()
			line := strings.TrimSpace(rawLine)
			if !strings.HasPrefix(line, "data:") {
				if line != "" && !strings.HasPrefix(line, ":") && !strings.HasPrefix(line, "event:") && !strings.HasPrefix(line, "id:") && !strings.HasPrefix(line, "retry:") {
					candRawNonSSE.WriteString(rawLine)
					candRawNonSSE.WriteByte('\n')
				}
				continue
			}
			foundSSE = true
			dataStr := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if dataStr == "" || dataStr == "[DONE]" {
				continue
			}
			textDelta, thoughtDelta, uMeta, rawFR, mVer := extractCCPAChunkWithVersion(dataStr)
			if mVer != "" {
				candModelVersion = mVer
			}
			if uMeta != nil && (uMeta.TotalTokenCount > 0 || uMeta.PromptTokenCount > 0 || uMeta.CandidatesTokenCount > 0) {
				candLastUsage = uMeta
			}
			if rawFR != "" {
				candFinishReason = rawFR
			}
			if textDelta == "" && thoughtDelta == "" {
				continue
			}
			idleTimer.Reset(CCPAStreamIdleTimeout)
			candInitialText.WriteString(textDelta)
			candInitialThoughts.WriteString(thoughtDelta)

			probe := candInitialText.String()
			if IsUnsupportedClientBanner(probe) {
				idleTimer.Stop()
				ctrl.clearTrackedBody()
				_ = r.Body.Close()
				cancelCand()
				releaseScopeInFlight()
				return nil, fmt.Errorf("CCPA client version rejected by upstream (update OMNI_AGY_USER_AGENT): %s", TruncateUTF8(probe, 200))
			}
			lowerProbe := strings.ToLower(strings.TrimSpace(probe))
			if candInitialThoughts.Len() == 0 && lowerProbe != "" && len(lowerProbe) < 60 &&
				strings.HasPrefix("this version of antigravity is no longer supported", lowerProbe) {
				continue
			}
			break
		}

		scanErr := scanner.Err()
		candErr := candCtx.Err()

		if !foundSSE && candRawNonSSE.Len() > 0 {
			tDelta, thDelta, uMeta, rawFR, mVer := extractCCPAChunkWithVersion(candRawNonSSE.String())
			if mVer != "" {
				candModelVersion = mVer
			}
			if uMeta != nil && (uMeta.TotalTokenCount > 0 || uMeta.PromptTokenCount > 0 || uMeta.CandidatesTokenCount > 0) {
				candLastUsage = uMeta
			}
			if rawFR != "" {
				candFinishReason = rawFR
			}
			candInitialText.WriteString(tDelta)
			candInitialThoughts.WriteString(thDelta)
		}

		if IsUnsupportedClientBanner(candInitialText.String()) || IsUnsupportedClientBanner(candRawNonSSE.String()) {
			idleTimer.Stop()
			ctrl.clearTrackedBody()
			_ = r.Body.Close()
			cancelCand()
			releaseScopeInFlight()
			banner := candInitialText.String()
			if strings.TrimSpace(banner) == "" {
				banner = candRawNonSSE.String()
			}
			return nil, fmt.Errorf("CCPA client version rejected by upstream (update OMNI_AGY_USER_AGENT): %s", TruncateUTF8(banner, 200))
		}

		if scanErr != nil || (candInitialText.Len() == 0 && candInitialThoughts.Len() == 0 && (timedOut.Load() || candErr != nil)) {
			idleTimer.Stop()
			ctrl.clearTrackedBody()
			_ = r.Body.Close()
			cancelCand()
			if attemptCtx.Err() != nil {
				releaseScopeInFlight()
				return nil, attemptCtx.Err()
			}
			if strings.Contains(accScope, "@") {
				markAccountDeadCCPAModelWithStatus(endpoint, accScope, candidate, 60*time.Second, http.StatusGatewayTimeout)
			} else {
				markDeadCCPAModel(endpoint, candidate, 2*time.Minute)
			}
			if candErr != nil {
				lastCCPAErr = fmt.Errorf("CCPA candidate %q timed out waiting for first token: %v", candidate, candErr)
			} else {
				lastCCPAErr = fmt.Errorf("CCPA candidate %q stream error before first token: %v", candidate, scanErr)
			}
			log.Printf("[omni-gateway] %v; trying next CCPA tier model", lastCCPAErr)
			continue
		}

		idleTimer.Reset(CCPAStreamIdleTimeout)
		return &ccpaOpenedAttempt{
			resp:               r,
			activeCCPAModel:    candidate,
			activeModelVersion: candModelVersion,
			activeScanner:      scanner,
			activeIdleTimer:    idleTimer,
			activeCandCtx:      candCtx,
			activeCancel:       cancelCand,
			activeTimedOut:     &timedOut,
			initialText:        candInitialText.String(),
			initialThoughts:    candInitialThoughts.String(),
			lastUsage:          candLastUsage,
			lastFinishReason:   candFinishReason,
			releaseScope:       releaseScopeInFlight,
		}, nil
	}

	releaseScopeInFlight()
	if attemptCtx.Err() != nil {
		return nil, attemptCtx.Err()
	}
	return nil, lastCCPAErr
}

func (u *GatewayUpstream) rotateStripedComboTargets(resolved ResolvedModel, targets []ResolvedModel) []ResolvedModel {
	if len(targets) == 0 {
		return targets
	}
	if len(targets) == 1 {
		out := make([]ResolvedModel, 1)
		out[0] = targets[0]
		out[0].ComboMode = ComboModeStriped
		return out
	}

	stripeCount := len(targets)
	for _, combo := range NewResolver().ListCombos() {
		if strings.EqualFold(combo.ID, resolved.CanonicalID) || strings.EqualFold(combo.ID, resolved.RequestedID) {
			seenModels := make(map[string]struct{}, len(combo.Models))
			validCount := 0
			for _, m := range combo.Models {
				m = strings.TrimSpace(m)
				if m == "" || strings.EqualFold(m, resolved.RequestedID) || strings.EqualFold(m, resolved.CanonicalID) {
					continue
				}
				if _, exists := seenModels[m]; !exists {
					seenModels[m] = struct{}{}
					validCount++
				}
			}
			if validCount > 0 && validCount < stripeCount {
				stripeCount = validCount
			}
			break
		}
	}

	primaryPool := make([]ResolvedModel, stripeCount)
	copy(primaryPool, targets[:stripeCount])
	for i := range primaryPool {
		primaryPool[i].ComboMode = ComboModeStriped
	}
	emergencyTail := targets[stripeCount:]

	endpoint := u.cfg.CCPAEndpoint
	if endpoint == "" {
		endpoint = DefaultCCPAEndpoint
	}
	now := time.Now()
	congestedMaxSpeed := strings.EqualFold(strings.TrimSpace(endpoint), DefaultCCPAEndpoint) &&
		(strings.EqualFold(resolved.CanonicalID, "auto/max-speed") || strings.EqualFold(resolved.RequestedID, "auto/max-speed")) &&
		TotalAntigravityInFlight() >= 6

	healthy := make([]ResolvedModel, 0, len(primaryPool))
	degraded := make([]ResolvedModel, 0, len(primaryPool))
	for _, t := range primaryPool {
		isClaudeCongested := congestedMaxSpeed && (strings.HasPrefix(strings.ToLower(t.CCPAModelID), "claude-") || strings.Contains(strings.ToLower(t.CanonicalID), "claude-"))
		if isClaudeCongested || (t.Provider == ProviderAntigravity && t.CCPAModelID != "" && !isVirtualCCPAIDEAlias(t.CCPAModelID) &&
			(isDeadCCPAModel(endpoint, t.CCPAModelID, now) || allAntigravityAccountsModelExhausted(endpoint, t.CCPAModelID, now))) {
			degraded = append(degraded, t)
		} else {
			healthy = append(healthy, t)
		}
	}

	seq := atomic.AddUint64(&stripedComboCounter, 1) - 1
	rotated := make([]ResolvedModel, 0, len(targets))
	if len(healthy) > 0 {
		start := int(seq % uint64(len(healthy)))
		rotated = append(rotated, healthy[start:]...)
		rotated = append(rotated, healthy[:start]...)
		rotated = append(rotated, degraded...)
	} else {
		start := int(seq % uint64(len(primaryPool)))
		rotated = append(rotated, primaryPool[start:]...)
		rotated = append(rotated, primaryPool[:start]...)
	}
	rotated = append(rotated, emergencyTail...)
	return rotated
}

func allAntigravityAccountsModelExhausted(endpoint, ccpaModelID string, now time.Time) bool {
	cand := strings.TrimSpace(ccpaModelID)
	if cand == "" {
		return false
	}
	accounts := readAuthAccountsSnapshotFast()
	enabledCount := 0
	exhaustedCount := 0
	nowUnix := now.Unix()
	isClaude := strings.HasPrefix(strings.ToLower(cand), "claude-")

	for _, a := range accounts {
		if a.Disabled {
			continue
		}
		p := a.Provider
		if p == "" {
			p = ProviderAntigravity
		}
		if p != ProviderAntigravity {
			continue
		}
		enabledCount++
		if a.CooldownUntil > nowUnix ||
			(isClaude && a.WeeklyClaudePercent != nil && *a.WeeklyClaudePercent >= 100) ||
			isAccountDeadCCPAModel(endpoint, a.Email, cand, now) ||
			IsAccountModelRateLimited(a.Email, cand) {
			exhaustedCount++
		}
	}
	return enabledCount > 0 && exhaustedCount == enabledCount
}

func isVirtualCCPAIDEAlias(candidate string) bool {
	m := strings.ToLower(strings.TrimSpace(candidate))
	return m == "gemini-4-carbon" ||
		m == "gemini-4-argon" ||
		m == "gemini-3.8-flash" ||
		strings.HasPrefix(m, "gemini-3.1-pro") ||
		m == "gemini-2.5-pro" ||
		strings.HasSuffix(m, "@default")
}

func buildCCPACandidatesWithCache(endpoint, accScope string, resolved ResolvedModel, noThinking bool) []string {
	if endpoint == "" {
		endpoint = DefaultCCPAEndpoint
	}
	isDefaultProdEndpoint := strings.EqualFold(strings.TrimSpace(endpoint), DefaultCCPAEndpoint)
	isAutoFastExplicit := strings.EqualFold(strings.TrimSpace(resolved.RequestedID), "auto/fast") ||
		strings.EqualFold(strings.TrimSpace(resolved.CanonicalID), "auto/fast")
	scope := normalizeAccScope(accScope)
	if resolved.ComboMode == ComboModeStriped && strings.TrimSpace(resolved.CCPAModelID) != "" && !isVirtualCCPAIDEAlias(resolved.CCPAModelID) {
		cand := strings.TrimSpace(resolved.CCPAModelID)
		deadOnAcc := strings.Contains(scope, "@") && isAccountDeadCCPAModel(endpoint, scope, cand, time.Now())
		rateMasked := isDefaultProdEndpoint && strings.Contains(scope, "@") && IsAccountModelRateLimited(scope, cand)
		if !deadOnAcc && !rateMasked {
			return []string{cand}
		}
	}
	_, isConnected := lookupConnectedAntigravityAccountFast(scope)
	claudeExhausted := isAccountClaudeQuotaExhaustedFast(scope)

	var raw []string
	var cachedWinner string
	bypassWorkingCache := isDefaultProdEndpoint && (isAutoFastExplicit || resolved.CCPAModelID == "gemini-3.8-flash-low")
	if !noThinking && !bypassWorkingCache && resolved.CanonicalID != "" {
		if cached, ok := workingCCPAModelCache.Load(endpoint + "|acc:" + scope + "|" + resolved.CanonicalID); ok {
			if s, ok := cached.(string); ok && s != "" {
				sTrim := strings.TrimSpace(s)
				sLower := strings.ToLower(sTrim)
				isGeminiReq := strings.Contains(resolved.CanonicalID, "gemini") || resolved.IsCarbonFamily || resolved.IsArgonFamily
				if isGeminiReq && strings.HasPrefix(sLower, "claude-") {
					workingCCPAModelCache.Delete(endpoint + "|acc:" + scope + "|" + resolved.CanonicalID)
				} else {
					rateMasked := isDefaultProdEndpoint && strings.Contains(scope, "@") && IsAccountModelRateLimited(scope, sTrim)
					if !rateMasked && !(claudeExhausted && strings.HasPrefix(sLower, "claude-")) {
						cachedWinner = sTrim
						raw = append(raw, cachedWinner)
					}
				}
			}
		}
	}

	if isDefaultProdEndpoint && isConnected {
		appendClaude := func(models ...string) {
			if claudeExhausted || isAccountClaudeQuotaExhaustedFast(scope) {
				return
			}
			for _, cm := range models {
				if strings.Contains(scope, "@") && IsAccountModelRateLimited(scope, cm) {
					continue
				}
				raw = append(raw, cm)
			}
		}
		canonLower := strings.ToLower(strings.TrimSpace(resolved.CanonicalID))
		reqLower := strings.ToLower(strings.TrimSpace(resolved.RequestedID))

		switch {
		case isAutoFastExplicit:
			raw = append(raw,
				"gemini-3-flash",
				"gemini-3.8-flash-low",
				"gemini-3.5-flash-lite",
			)
		case resolved.IsCarbonFamily || strings.HasPrefix(resolved.CCPAModelID, "gemini-4-carbon"):
			switch resolved.ThinkingLevel {
			case ThinkingLevelLow:
				if isAutoFastExplicit {
					raw = append(raw,
						"gemini-3-flash",
						"gemini-3.8-flash-low",
						"gemini-3.5-flash-lite",
					)
				} else {
					raw = append(raw,
						"gemini-4-carbon-low",
						"gemini-3-flash",
						"gemini-3.8-flash-low",
						"gemini-3.5-flash-lite",
					)
				}
			case ThinkingLevelMedium:
				raw = append(raw,
					"gemini-4-carbon-medium",
					"gemini-4-argon-medium",
					"gemini-4-carbon",
					"gemini-3.1-pro-high",
					"gemini-3.8-flash-medium",
					"gemini-3.8-flash-high",
					"gemini-3.8-flash-tiered",
					"gemini-3-flash",
				)
			default:
				raw = append(raw,
					"gemini-4-carbon-high",
					"gemini-4-argon-high",
					"gemini-4-carbon",
					"gemini-3.1-pro-high",
					"gemini-3.8-flash-high",
					"gemini-3.8-flash-tiered",
					"gemini-3-flash",
				)
			}

		case resolved.IsArgonFamily || strings.HasPrefix(resolved.CCPAModelID, "gemini-4-argon"):
			switch resolved.ThinkingLevel {
			case ThinkingLevelLow:
				if isAutoFastExplicit {
					raw = append(raw,
						"gemini-3-flash",
						"gemini-3.8-flash-low",
						"gemini-3.5-flash-lite",
					)
				} else {
					raw = append(raw,
						"gemini-4-argon-low",
						"gemini-3-flash",
						"gemini-3.8-flash-low",
						"gemini-3.5-flash-lite",
					)
				}
			case ThinkingLevelMedium:
				raw = append(raw,
					"gemini-4-argon-medium",
					"gemini-4-carbon-medium",
					"gemini-4-argon",
					"gemini-3.1-pro-high",
					"gemini-3.8-flash-medium",
					"gemini-3.8-flash-high",
					"gemini-3.8-flash-tiered",
					"gemini-3-flash",
				)
			default:
				raw = append(raw,
					"gemini-4-argon-high",
					"gemini-4-carbon-high",
					"gemini-4-argon",
					"gemini-3.1-pro-high",
					"gemini-3.8-flash-high",
					"gemini-3.8-flash-tiered",
					"gemini-3-flash",
				)
			}

		case strings.HasPrefix(resolved.CCPAModelID, "gemini-3.1-pro") || strings.Contains(canonLower, "gemini-3.1-pro"):
			if resolved.ThinkingLevel == ThinkingLevelLow {
				raw = append(raw,
					"gemini-3-flash",
					"gemini-3.8-flash-low",
					"gemini-3.8-flash-tiered",
				)
			} else {
				raw = append(raw,
					"gemini-3.1-pro-high",
					"gemini-3.8-flash-high",
					"gemini-3.8-flash-tiered",
					"gemini-3-flash",
				)
			}

		case strings.HasPrefix(resolved.CCPAModelID, "gemini-3.8-flash") || strings.Contains(canonLower, "gemini-3.8-flash"):
			if strings.Contains(reqLower, "tiered") || strings.Contains(canonLower, "tiered") {
				raw = append(raw,
					"gemini-3.8-flash-tiered",
					"gemini-3.8-flash-high",
					"gemini-3-flash",
				)
			} else if resolved.ThinkingLevel == ThinkingLevelLow {
				raw = append(raw,
					"gemini-3-flash",
					"gemini-3.8-flash-low",
					"gemini-3.5-flash-lite",
				)
			} else if resolved.ThinkingLevel == ThinkingLevelMedium {
				raw = append(raw,
					"gemini-3.8-flash-medium",
					"gemini-3.8-flash-high",
					"gemini-3.8-flash-tiered",
					"gemini-3-flash",
				)
			} else {
				raw = append(raw,
					"gemini-3.8-flash-high",
					"gemini-3.8-flash-tiered",
					"gemini-3-flash",
				)
			}

		case strings.Contains(canonLower, "lite") || resolved.CCPAModelID == "gemini-3-flash":
			if resolved.CCPAModelID != "" && !isVirtualCCPAIDEAlias(resolved.CCPAModelID) {
				raw = append(raw, resolved.CCPAModelID)
			}
			raw = append(raw,
				"gemini-3-flash",
				"gemini-3.5-flash-lite",
				"gemini-3.1-flash-lite",
			)

		case strings.Contains(resolved.CCPAModelID, "image"):
			raw = append(raw, "gemini-3.1-flash-image")

		case strings.HasPrefix(resolved.CCPAModelID, "claude-opus"):
			appendClaude("claude-opus-4-6-thinking", "claude-sonnet-4-6")
			raw = append(raw,
				"gemini-3.8-flash-high",
				"gemini-3.8-flash-tiered",
				"gemini-3-flash",
			)

		case strings.HasPrefix(resolved.CCPAModelID, "claude-sonnet"):
			appendClaude("claude-sonnet-4-6", "claude-opus-4-6-thinking")
			raw = append(raw,
				"gemini-3.8-flash-medium",
				"gemini-3.8-flash-high",
				"gemini-3.8-flash-tiered",
				"gemini-3-flash",
			)

		default:
			if resolved.CCPAModelID != "" && !isVirtualCCPAIDEAlias(resolved.CCPAModelID) {
				raw = append(raw, resolved.CCPAModelID)
			}
			raw = append(raw, "gemini-3.8-flash-high")
			appendClaude("claude-sonnet-4-6", "claude-opus-4-6-thinking")
			raw = append(raw,
				"gemini-3.8-flash-tiered",
				"gemini-3-flash",
			)
		}
	} else {
		switch {
		case isDefaultProdEndpoint && isAutoFastExplicit:
			raw = append(raw,
				"gemini-3-flash",
				"gemini-3.8-flash-low",
				"gemini-3.5-flash-lite",
			)
		case resolved.IsCarbonFamily || strings.HasPrefix(resolved.CCPAModelID, "gemini-4-carbon"):
			switch resolved.ThinkingLevel {
			case ThinkingLevelLow:
				if isDefaultProdEndpoint && isAutoFastExplicit {
					raw = append(raw,
						"gemini-3-flash",
						"gemini-3.8-flash-low",
						"gemini-3.5-flash-lite",
					)
				} else {
					raw = append(raw,
						"gemini-4-carbon-low",
						"gemini-3-flash",
						"gemini-3.8-flash-low",
						"gemini-3.5-flash-lite",
						"claude-sonnet-4-6",
						"gemini-3.8-flash-tiered",
					)
				}
			case ThinkingLevelMedium:
				raw = append(raw,
					"gemini-4-carbon-medium",
					"gemini-4-argon-medium",
					"gemini-4-carbon",
					"gemini-3.8-flash-medium",
					"gemini-3.8-flash-high",
					"gemini-3-flash",
					"gemini-3.8-flash-tiered",
				)
			default:
				raw = append(raw,
					"gemini-4-carbon-high",
					"gemini-4-argon-high",
					"gemini-4-carbon",
					"gemini-3.8-flash-high",
					"gemini-3-flash",
					"gemini-3.8-flash-tiered",
				)
			}

		case resolved.IsArgonFamily || strings.HasPrefix(resolved.CCPAModelID, "gemini-4-argon"):
			if resolved.CCPAModelID != "" {
				raw = append(raw, resolved.CCPAModelID)
			}
			switch resolved.ThinkingLevel {
			case ThinkingLevelLow:
				raw = append(raw,
					"gemini-4-argon-low",
					"gemini-3-flash",
					"gemini-3.8-flash-low",
					"gemini-3.5-flash-lite",
					"gemini-3.8-flash-tiered",
				)
			case ThinkingLevelMedium:
				raw = append(raw,
					"gemini-4-argon-medium",
					"gemini-4-carbon-medium",
					"gemini-4-argon",
					"gemini-3.8-flash-medium",
					"gemini-3.8-flash-high",
					"gemini-3-flash",
					"gemini-3.8-flash-tiered",
				)
			default:
				raw = append(raw,
					"gemini-4-argon-high",
					"gemini-4-carbon-high",
					"gemini-4-argon",
					"gemini-3.8-flash-high",
					"gemini-3-flash",
					"gemini-3.8-flash-tiered",
				)
			}

		case strings.HasPrefix(resolved.CCPAModelID, "gemini-3.1-pro") || strings.Contains(resolved.CanonicalID, "gemini-3.1-pro"):
			if resolved.CCPAModelID != "" {
				raw = append(raw, resolved.CCPAModelID)
			}
			raw = append(raw,
				"gemini-3.8-flash-high",
				"gemini-3-flash",
				"gemini-3.8-flash-tiered",
			)

		case strings.HasPrefix(resolved.CCPAModelID, "gemini-3.8-flash"):
			if resolved.CCPAModelID != "" && !(isDefaultProdEndpoint && resolved.ThinkingLevel == ThinkingLevelLow) {
				raw = append(raw, resolved.CCPAModelID)
			}
			if resolved.ThinkingLevel == ThinkingLevelLow {
				raw = append(raw,
					"gemini-3-flash",
					"gemini-3.8-flash-low",
					"gemini-3.5-flash-lite",
					"claude-sonnet-4-6",
					"gemini-3.8-flash-tiered",
				)
			} else {
				raw = append(raw,
					"gemini-3-flash",
					"gemini-3.8-flash-high",
					"claude-sonnet-4-6",
					"gemini-3.8-flash-tiered",
				)
			}

		case strings.Contains(resolved.CanonicalID, "lite") || resolved.CCPAModelID == "gemini-3-flash":
			if resolved.CCPAModelID != "" {
				raw = append(raw, resolved.CCPAModelID)
			}
			raw = append(raw,
				"gemini-3-flash",
				"gemini-3.5-flash-lite",
				"gemini-3.1-flash-lite",
			)

		case strings.HasPrefix(resolved.CCPAModelID, "claude-opus"):
			if resolved.CCPAModelID != "" {
				raw = append(raw, resolved.CCPAModelID)
			}
			raw = append(raw,
				"claude-opus-4-6-thinking",
				"claude-sonnet-4-6",
				"gemini-3.8-flash-high",
				"gemini-3-flash",
				"gemini-3.8-flash-tiered",
			)

		case strings.HasPrefix(resolved.CCPAModelID, "claude-sonnet"):
			if resolved.CCPAModelID != "" {
				raw = append(raw, resolved.CCPAModelID)
			}
			raw = append(raw,
				"claude-sonnet-4-6",
				"claude-opus-4-6-thinking",
				"gemini-3.8-flash-medium",
				"gemini-3-flash",
				"gemini-3.8-flash-tiered",
			)

		default:
			if resolved.CCPAModelID != "" {
				raw = append(raw, resolved.CCPAModelID)
			}
			raw = append(raw,
				"claude-sonnet-4-6",
				"claude-opus-4-6-thinking",
				"gemini-3.8-flash-high",
				"gemini-3-flash",
				"gemini-3.8-flash-tiered",
			)
		}
	}

	seen := make(map[string]bool, len(raw))
	deduped := make([]string, 0, len(raw))
	for _, m := range raw {
		m = strings.TrimSpace(m)
		if m != "" && !seen[m] {
			seen[m] = true
			deduped = append(deduped, m)
		}
	}

	// On DefaultCCPAEndpoint ONLY, strip virtual Antigravity IDE 404/503 aliases so requests
	// never pay 400-1400ms probing dead model IDs on cold start or quota exhaustion.
	if isDefaultProdEndpoint && len(deduped) > 0 {
		realIDs := make([]string, 0, len(deduped))
		for idx, m := range deduped {
			if idx == 0 && cachedWinner != "" && m == cachedWinner && !isVirtualCCPAIDEAlias(m) {
				realIDs = append(realIDs, m)
				continue
			}
			if !isVirtualCCPAIDEAlias(m) {
				realIDs = append(realIDs, m)
			}
		}
		if len(realIDs) > 0 {
			deduped = realIDs
		}
	}

	now := time.Now()
	alive := make([]string, 0, len(deduped))
	for _, m := range deduped {
		if isDeadCCPAModel(endpoint, m, now) || isAccountDeadCCPAModel(endpoint, scope, m, now) {
			continue
		}
		alive = append(alive, m)
	}
	if len(alive) > 0 {
		return alive
	}
	return deduped
}

func normalizeCCPAFinishReason(raw string) string {
	switch strings.ToUpper(strings.TrimSpace(raw)) {
	case "MAX_TOKENS", "MAX_OUTPUT_TOKENS", "LENGTH":
		return "length"
	default:
		return "stop"
	}
}

func extractCCPAChunk(dataStr string) (text string, thought string, usage *ccpaUsageMetadata, finishReason string) {
	t, th, u, fr, _ := extractCCPAChunkWithVersion(dataStr)
	return t, th, u, fr
}

func extractCCPAChunkWithVersion(dataStr string) (text string, thought string, usage *ccpaUsageMetadata, finishReason string, modelVersion string) {
	var chunk ccpaStreamChunk
	if err := json.Unmarshal([]byte(dataStr), &chunk); err != nil {
		return "", "", nil, "", ""
	}
	candidates := chunk.Candidates
	usage = chunk.UsageMetadata
	modelVersion = strings.TrimSpace(chunk.ModelVersion)
	if chunk.Response != nil {
		if len(chunk.Response.Candidates) > 0 {
			candidates = chunk.Response.Candidates
		}
		if chunk.Response.UsageMetadata != nil {
			usage = chunk.Response.UsageMetadata
		}
		if rv := strings.TrimSpace(chunk.Response.ModelVersion); rv != "" {
			modelVersion = rv
		}
	}
	var tBuilder, thBuilder strings.Builder
	for _, c := range candidates {
		if fr := strings.TrimSpace(c.FinishReason); fr != "" {
			finishReason = fr
		}
		for _, p := range c.Content.Parts {
			if p.Thought {
				thBuilder.WriteString(p.Text)
			} else {
				tBuilder.WriteString(p.Text)
			}
		}
	}
	return tBuilder.String(), thBuilder.String(), usage, finishReason, modelVersion
}

func extractCCPAPartsAndUsage(dataStr string) (text string, thought string, usage *ccpaUsageMetadata) {
	t, th, u, _ := extractCCPAChunk(dataStr)
	return t, th, u
}

func computeCCPAUsage(req OpenAIChatRequest, textLen int, thoughtLen int, uMeta *ccpaUsageMetadata) OpenAIUsage {
	promptToks := EstimateTokens(req.Messages)
	if promptToks <= 0 {
		promptToks = 1
	}
	compToks := (textLen + thoughtLen + 3) / 4
	if compToks <= 0 && (textLen > 0 || thoughtLen > 0) {
		compToks = 1
	}
	totalToks := promptToks + compToks

	if uMeta != nil {
		if uMeta.PromptTokenCount > 0 {
			promptToks = uMeta.PromptTokenCount
		}
		if uMeta.CandidatesTokenCount+uMeta.ThoughtsTokenCount > 0 {
			compToks = uMeta.CandidatesTokenCount + uMeta.ThoughtsTokenCount
		}
		if uMeta.TotalTokenCount > 0 {
			totalToks = uMeta.TotalTokenCount
		} else {
			totalToks = promptToks + compToks
		}
	}
	if totalToks <= 0 {
		totalToks = 1
	}
	return OpenAIUsage{
		PromptTokens:     promptToks,
		CompletionTokens: compToks,
		TotalTokens:      totalToks,
	}
}

func parseRecordedTokens(hdrVal string, req OpenAIChatRequest) int {
	if v := strings.TrimSpace(hdrVal); v != "" {
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil && n > 0 {
			return n
		}
	}
	est := EstimateTokens(req.Messages)
	if est <= 0 {
		est = 1
	}
	return est
}

func extractTokensFromOpenAIBody(rawBody []byte, req OpenAIChatRequest) int {
	trimmed := bytes.TrimSpace(rawBody)
	promptToks := EstimateTokens(req.Messages)
	if promptToks <= 0 {
		promptToks = 1
	}
	if bytes.HasPrefix(trimmed, []byte("data:")) || bytes.Contains(trimmed, []byte("\ndata:")) {
		agg := aggregateSSEToResponse(trimmed, req.Model)
		if agg.Usage != nil && agg.Usage.TotalTokens > 0 {
			return agg.Usage.TotalTokens
		}
		compToks := 0
		if len(agg.Choices) > 0 && agg.Choices[0].Message != nil {
			compToks = (len(agg.Choices[0].Message.Content) + len(agg.Choices[0].Message.ReasoningContent) + 3) / 4
		}
		return promptToks + compToks
	}
	var parsed OpenAIChatResponse
	if err := json.Unmarshal(trimmed, &parsed); err == nil {
		if parsed.Usage != nil && parsed.Usage.TotalTokens > 0 {
			return parsed.Usage.TotalTokens
		}
		compToks := 0
		if len(parsed.Choices) > 0 && parsed.Choices[0].Message != nil {
			compToks = (len(parsed.Choices[0].Message.Content) + len(parsed.Choices[0].Message.ReasoningContent) + 3) / 4
		}
		return promptToks + compToks
	}
	return promptToks + (len(trimmed)+3)/4
}

func (u *GatewayUpstream) handleOmniRouteWithFallback(
	ctx context.Context,
	w http.ResponseWriter,
	req OpenAIChatRequest,
	resolved ResolvedModel,
	cleanAuth string,
) error {
	baseURL := strings.TrimRight(strings.TrimSpace(u.cfg.OmniRouteBaseURL), "/")
	if baseURL == "" {
		writeOpenAIError(w, http.StatusBadRequest, "external upstream URL is disabled", "configuration_error")
		return fmt.Errorf("external upstream URL is disabled")
	}
	if !strings.HasSuffix(baseURL, "/v1") && !strings.HasSuffix(baseURL, "/chat/completions") {
		baseURL += "/v1"
	}
	endpoint := baseURL
	if !strings.HasSuffix(endpoint, "/chat/completions") {
		endpoint += "/chat/completions"
	}

	apiKey := strings.TrimSpace(u.cfg.OmniRouteAPIKey)
	if strings.HasPrefix(cleanAuth, "sk-") {
		apiKey = cleanAuth
	}

	var candidates []string
	seen := make(map[string]bool)
	addCandidate := func(m string) {
		m = strings.TrimSpace(m)
		if m != "" && !seen[m] {
			seen[m] = true
			candidates = append(candidates, m)
		}
	}
	addCandidate(resolved.OmniBaseModelID)
	for _, fb := range resolved.FallbackChain {
		addCandidate(fb)
	}
	for _, fb := range u.cfg.FallbackModels {
		addCandidate(fb)
	}
	if len(candidates) == 0 {
		addCandidate(req.Model)
	}

	var lastStatus int
	var lastErrBody string
	var lastErr error

	for idx, candidateModel := range candidates {
		outReq := req
		outReq.Model = candidateModel

		if outReq.ReasoningEffort == "" && resolved.ReasoningEffort != "" {
			outReq.ReasoningEffort = resolved.ReasoningEffort
		}
		if outReq.ThinkingConfig == nil && (resolved.IncludeThoughts || resolved.ThinkingLevel != ThinkingLevelNone) {
			outReq.ThinkingConfig = &ThinkingConfig{
				IncludeThoughts: resolved.IncludeThoughts,
				ThinkingLevel:   resolved.ThinkingLevel,
			}
		}

		payloadBytes, err := json.Marshal(outReq)
		if err != nil {
			lastErr = err
			continue
		}

		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payloadBytes))
		if err != nil {
			lastErr = err
			continue
		}
		httpReq.Header.Set("Content-Type", "application/json")
		if apiKey != "" {
			httpReq.Header.Set("Authorization", "Bearer "+apiKey)
		}

		resp, err := u.client.Do(httpReq)
		if err != nil {
			lastErr = err
			log.Printf("[omni-gateway] candidate %d/%d (%s) transport error: %v", idx+1, len(candidates), candidateModel, err)
			continue
		}

		if shouldRetryCandidate(resp.StatusCode, "") {
			errBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			_ = resp.Body.Close()
			lastStatus = resp.StatusCode
			lastErrBody = strings.TrimSpace(string(errBytes))
			log.Printf("[omni-gateway] candidate %d/%d (%s) returned HTTP %d (%s); trying next fallback",
				idx+1, len(candidates), candidateModel, resp.StatusCode, lastErrBody)
			continue
		}

		bodyBytes, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			lastErr = err
			continue
		}

		bodyStr := strings.TrimSpace(string(bodyBytes))
		if resp.StatusCode >= 400 || isRetryableErrorBody(bodyStr) {
			lastStatus = resp.StatusCode
			if lastStatus < 400 {
				lastStatus = http.StatusBadGateway
			}
			lastErrBody = bodyStr
			log.Printf("[omni-gateway] candidate %d/%d (%s) returned retryable body (%s); trying next fallback",
				idx+1, len(candidates), candidateModel, TruncateUTF8(bodyStr, 180))
			continue
		}

		w.Header().Set("X-Omni-Gateway-Resolved-Model", candidateModel)
		w.Header().Set("X-Omni-Gateway-Requested-Model", resolved.RequestedID)
		w.Header().Set("X-Omni-Gateway-Route", "omniroute")
		if resolved.ThinkingLevel != ThinkingLevelNone {
			w.Header().Set("X-Omni-Gateway-Thinking-Level", string(resolved.ThinkingLevel))
		}

		cacheReq := cacheKeyRequestFromContext(ctx, req)
		if req.Stream {
			if bytes.HasPrefix(bytes.TrimSpace(bodyBytes), []byte("data:")) || bytes.Contains(bodyBytes, []byte("\ndata:")) {
				agg := aggregateSSEToResponse(bodyBytes, resolved.RequestedID)
				if len(agg.Choices) > 0 && agg.Choices[0].Message != nil && agg.Choices[0].Message.Content != "" && agg.Choices[0].FinishReason != "length" {
					GetGlobalCache().Put(cacheReq, agg)
				}
			} else {
				var parsed OpenAIChatResponse
				if json.Unmarshal(bodyBytes, &parsed) == nil && len(parsed.Choices) > 0 && parsed.Choices[0].FinishReason != "length" {
					parsed.Model = resolved.RequestedID
					GetGlobalCache().Put(cacheReq, parsed)
				}
			}
			return writeStreamingResponse(w, bodyBytes, resolved.RequestedID)
		}
		var parsed OpenAIChatResponse
		if json.Unmarshal(bodyBytes, &parsed) == nil && len(parsed.Choices) > 0 && parsed.Choices[0].FinishReason != "length" {
			parsed.Model = resolved.RequestedID
			GetGlobalCache().Put(cacheReq, parsed)
		}
		return writeNonStreamingResponse(w, bodyBytes, resolved.RequestedID)
	}

	if lastStatus == 0 {
		lastStatus = http.StatusBadGateway
	}
	errMsg := fmt.Sprintf("all upstream candidates failed for model %q (tried %v)", resolved.RequestedID, candidates)
	if lastErrBody != "" {
		errMsg = fmt.Sprintf("%s: last upstream response: %s", errMsg, lastErrBody)
	} else if lastErr != nil {
		errMsg = fmt.Sprintf("%s: %v", errMsg, lastErr)
	}
	writeOpenAIError(w, lastStatus, errMsg, "model_unavailable")
	return fmt.Errorf("%s", errMsg)
}

func shouldRetryCandidate(statusCode int, body string) bool {
	switch statusCode {
	case http.StatusBadRequest,
		http.StatusNotFound,
		http.StatusTooManyRequests,
		http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout:
		return true
	}
	if body != "" && isRetryableErrorBody(body) {
		return true
	}
	return false
}

func isRetryableErrorBody(body string) bool {
	lower := strings.ToLower(strings.TrimSpace(body))
	if lower == "" {
		return true
	}
	if strings.Contains(lower, `"error"`) || strings.HasPrefix(lower, `{"error`) {
		for _, kw := range []string{
			"404",
			"not found",
			"no credentials",
			"cooldown",
			"unavailable",
			"unsupported model",
			"model_not_found",
			"rate limit",
			"degraded",
		} {
			if strings.Contains(lower, kw) {
				return true
			}
		}
	}
	return false
}

func writeStreamingResponse(w http.ResponseWriter, rawBody []byte, requestedModel string) error {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)

	trimmed := bytes.TrimSpace(rawBody)
	if bytes.HasPrefix(trimmed, []byte("data:")) || bytes.Contains(trimmed, []byte("\ndata:")) {
		scanner := bufio.NewScanner(bytes.NewReader(trimmed))
		scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			dataPayload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if dataPayload == "[DONE]" {
				continue
			}
			rewritten := rewriteModelInJSON([]byte(dataPayload), requestedModel)
			_, _ = fmt.Fprintf(w, "data: %s\n\n", rewritten)
			if flusher != nil {
				flusher.Flush()
			}
		}
		_, _ = fmt.Fprintf(w, "data: [DONE]\n\n")
		if flusher != nil {
			flusher.Flush()
		}
		return nil
	}

	var parsed OpenAIChatResponse
	if err := json.Unmarshal(trimmed, &parsed); err == nil && len(parsed.Choices) > 0 {
		msg := parsed.Choices[0].Message
		if msg == nil {
			msg = parsed.Choices[0].Delta
		}
		delta := &OpenAIOutputMessage{Role: "assistant"}
		if msg != nil {
			delta.Content = msg.Content
			delta.ReasoningContent = msg.ReasoningContent
		}
		chunk := OpenAIChatResponse{
			ID:      parsed.ID,
			Object:  "chat.completion.chunk",
			Created: parsed.Created,
			Model:   requestedModel,
			Choices: []OpenAIChatChoice{
				{
					Index:        0,
					Delta:        delta,
					FinishReason: "stop",
				},
			},
		}
		b, _ := json.Marshal(chunk)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", b)
		_, _ = fmt.Fprintf(w, "data: [DONE]\n\n")
		if flusher != nil {
			flusher.Flush()
		}
		return nil
	}

	rewritten := rewriteModelInJSON(trimmed, requestedModel)
	_, _ = fmt.Fprintf(w, "data: %s\n\n", rewritten)
	_, _ = fmt.Fprintf(w, "data: [DONE]\n\n")
	if flusher != nil {
		flusher.Flush()
	}
	return nil
}

func writeNonStreamingResponse(w http.ResponseWriter, rawBody []byte, requestedModel string) error {
	trimmed := bytes.TrimSpace(rawBody)

	if bytes.HasPrefix(trimmed, []byte("data:")) || bytes.Contains(trimmed, []byte("\ndata:")) {
		agg := aggregateSSEToResponse(trimmed, requestedModel)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		return json.NewEncoder(w).Encode(agg)
	}

	rewritten := rewriteModelInJSON(trimmed, requestedModel)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, err := w.Write(rewritten)
	return err
}

func aggregateSSEToResponse(sseBytes []byte, requestedModel string) OpenAIChatResponse {
	scanner := bufio.NewScanner(bytes.NewReader(sseBytes))
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	var (
		respID       string
		created      int64
		finishReason string
		contentBuf   strings.Builder
		reasoningBuf strings.Builder
		usage        *OpenAIUsage
	)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		dataPayload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if dataPayload == "" || dataPayload == "[DONE]" {
			continue
		}

		var chunk OpenAIChatResponse
		if err := json.Unmarshal([]byte(dataPayload), &chunk); err != nil {
			continue
		}
		if respID == "" && chunk.ID != "" {
			respID = chunk.ID
		}
		if created == 0 && chunk.Created != 0 {
			created = chunk.Created
		}
		if chunk.Usage != nil {
			usage = chunk.Usage
		}
		for _, c := range chunk.Choices {
			if c.Delta != nil {
				contentBuf.WriteString(c.Delta.Content)
				reasoningBuf.WriteString(c.Delta.ReasoningContent)
			} else if c.Message != nil {
				contentBuf.WriteString(c.Message.Content)
				reasoningBuf.WriteString(c.Message.ReasoningContent)
			}
			if c.FinishReason != "" {
				finishReason = c.FinishReason
			}
		}
	}

	if respID == "" {
		respID = fmt.Sprintf("chatcmpl-omni-%d", time.Now().UnixNano())
	}
	if created == 0 {
		created = time.Now().Unix()
	}
	if finishReason == "" {
		finishReason = "stop"
	}

	return OpenAIChatResponse{
		ID:      respID,
		Object:  "chat.completion",
		Created: created,
		Model:   requestedModel,
		Choices: []OpenAIChatChoice{
			{
				Index: 0,
				Message: &OpenAIOutputMessage{
					Role:             "assistant",
					Content:          contentBuf.String(),
					ReasoningContent: reasoningBuf.String(),
				},
				FinishReason: finishReason,
			},
		},
		Usage: usage,
	}
}

func rewriteModelInJSON(raw []byte, requestedModel string) []byte {
	if requestedModel == "" {
		return raw
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return raw
	}
	obj["model"] = requestedModel
	out, err := json.Marshal(obj)
	if err != nil {
		return raw
	}
	return out
}

func writeOpenAIError(w http.ResponseWriter, statusCode int, message string, errType string) {
	if statusCode < 400 {
		statusCode = http.StatusInternalServerError
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": OpenAIError{
			Message: message,
			Type:    errType,
			Code:    statusCode,
		},
	})
}

func truncateLog(s string, maxLen int) string {
	return TruncateUTF8(s, maxLen)
}
