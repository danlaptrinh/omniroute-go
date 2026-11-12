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
	expiresAt time.Time
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
	candidate = strings.TrimSpace(candidate)
	if candidate == "" {
		return
	}
	if endpoint == "" {
		endpoint = DefaultCCPAEndpoint
	}
	scope := normalizeAccScope(accScope)
	deadCCPAModelCache.Store(endpoint+"|acc:"+scope+"|"+candidate, deadCCPAEntry{
		expiresAt: time.Now().Add(ttl),
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

func isAccountDeadCCPAModel(endpoint, accScope, candidate string, now time.Time) bool {
	if endpoint == "" {
		endpoint = DefaultCCPAEndpoint
	}
	scope := normalizeAccScope(accScope)
	key := endpoint + "|acc:" + scope + "|" + strings.TrimSpace(candidate)
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
	if explicitDefault && strings.TrimSpace(os.Getenv("OMNI_SKIP_CCPA_WARMUP")) == "" && !isRunningUnderGoTest() {
		WarmupCCPAConnectionsAsync()
	}
	return &GatewayUpstream{
		cfg:    cfg,
		client: SharedHTTPClient,
	}
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

	// Inject global Custom System Prompt if enabled in EndpointConfig
	req = ApplyCustomSystemPrompt(req)

	// Retrieve & inject Long-Term Vector Database RAG memories if enabled
	if ragReq, ragCount := ApplyVectorMemoryRAG(req); ragCount > 0 {
		req = ragReq
		w.Header().Set("X-Omni-Gateway-Vector-Matches", fmt.Sprintf("%d", ragCount))
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

	var lastErr error
	var firstUpstreamErr error

	hedgedEnabled, hedgeDelayMs := GetHedgedConfig()
	if hedgedEnabled && len(targets) > 1 && !req.Stream {
		if err := u.handleHedgedCompletion(ctx, w, req, targets, cleanAuth, hedgeDelayMs); err == nil {
			return nil
		}
	}

	for _, target := range targets {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := u.tryTarget(ctx, w, req, target, cleanAuth); err == nil {
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
		return u.handleOmniRouteWithFallback(ctx, w, req, resolved, cleanAuth)
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
	} `json:"response,omitempty"`
	Candidates []struct {
		Content struct {
			Parts []ccpaPart `json:"parts"`
		} `json:"content"`
		FinishReason string `json:"finishReason,omitempty"`
	} `json:"candidates,omitempty"`
	UsageMetadata *ccpaUsageMetadata `json:"usageMetadata,omitempty"`
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
	releaseScopeInFlight := acquireCCPAScopeInFlight(accScope, oauthToken)
	defer releaseScopeInFlight()
	noThinking := req.ThinkingConfig != nil && !req.ThinkingConfig.IncludeThoughts && req.ThinkingConfig.ThinkingBudget == 0 && req.ThinkingConfig.ThinkingLevel == ThinkingLevelNone
	ccpaCandidates := buildCCPACandidatesWithCache(endpoint, accScope, resolved, noThinking)

	var (
		resp             *http.Response
		activeCCPAModel  string
		activeScanner    *bufio.Scanner
		activeIdleTimer  *time.Timer
		activeCandCtx    context.Context
		activeCancel     context.CancelFunc
		activeTimedOut   *atomic.Bool
		initialText      string
		initialThoughts  string
		lastUsage        *ccpaUsageMetadata
		lastFinishReason string
		lastCCPAErr      error
	)

	firstChunkTimeout := effectiveCCPAFirstChunkTimeout(req)

	for _, candidate := range ccpaCandidates {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		candInner := inner
		if strings.Contains(candidate, "lite") && candInner.GenerationConfig.MaxOutputTokens > 32768 {
			candInner.GenerationConfig.MaxOutputTokens = 32768
		}
		// Anthropic models, flash-lite/gemini-3-flash, and image models on CCPA do not accept thinkingConfig overrides
		if (strings.HasPrefix(candidate, "claude-") ||
			candidate == "gemini-3-flash" ||
			candidate == "gemini-3.5-flash-lite" ||
			candidate == "gemini-3.1-flash-lite" ||
			strings.Contains(candidate, "image")) && candInner.GenerationConfig.ThinkingConfig != nil {
			candInner.GenerationConfig.ThinkingConfig = nil
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

		candCtx, cancelCand := context.WithTimeout(ctx, CCPACandidateMaxTimeout)
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
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if timedOut.Load() || candErr != nil {
				markDeadCCPAModel(endpoint, candidate, 2*time.Minute)
			}
			lastCCPAErr = err
			log.Printf("[omni-gateway] CCPA candidate %q transport error: %v; trying next CCPA tier model", candidate, err)
			continue
		}

		if r.StatusCode < 200 || r.StatusCode >= 300 {
			idleTimer.Stop()
			errBody, _ := io.ReadAll(io.LimitReader(r.Body, 4096))
			_ = r.Body.Close()
			cancelCand()
			switch r.StatusCode {
			case http.StatusNotFound:
				markDeadCCPAModel(endpoint, candidate, 6*time.Hour)
			case http.StatusTooManyRequests:
				markAccountDeadCCPAModel(endpoint, accScope, candidate, 2*time.Minute)
			case http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
				markDeadCCPAModel(endpoint, candidate, 2*time.Minute)
			}
			lastCCPAErr = fmt.Errorf("CCPA HTTP %d (%s): %s", r.StatusCode, candidate, strings.TrimSpace(string(errBody)))
			if r.StatusCode == http.StatusUnauthorized || r.StatusCode == http.StatusForbidden {
				log.Printf("[omni-gateway] CCPA candidate %q returned HTTP %d; stopping tier fallback for account recovery/failover", candidate, r.StatusCode)
				break
			}
			log.Printf("[omni-gateway] CCPA candidate %q returned HTTP %d; trying next CCPA tier model", candidate, r.StatusCode)
			continue
		}

		// Verify first meaningful chunk inside candidate loop while idleTimer (firstChunkTimeout) is armed.
		scanner := bufio.NewScanner(r.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)

		var (
			candInitialText     strings.Builder
			candInitialThoughts strings.Builder
			candRawNonSSE       strings.Builder
			candLastUsage       *ccpaUsageMetadata
			candFinishReason    string
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
			textDelta, thoughtDelta, uMeta, rawFR := extractCCPAChunk(dataStr)
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
				_ = r.Body.Close()
				cancelCand()
				return fmt.Errorf("CCPA client version rejected by upstream (update OMNI_AGY_USER_AGENT): %s", TruncateUTF8(probe, 200))
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
			tDelta, thDelta, uMeta, rawFR := extractCCPAChunk(candRawNonSSE.String())
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
			_ = r.Body.Close()
			cancelCand()
			banner := candInitialText.String()
			if strings.TrimSpace(banner) == "" {
				banner = candRawNonSSE.String()
			}
			return fmt.Errorf("CCPA client version rejected by upstream (update OMNI_AGY_USER_AGENT): %s", TruncateUTF8(banner, 200))
		}

		if scanErr != nil || (candInitialText.Len() == 0 && candInitialThoughts.Len() == 0 && (timedOut.Load() || candErr != nil)) {
			idleTimer.Stop()
			_ = r.Body.Close()
			cancelCand()
			if ctx.Err() != nil {
				return ctx.Err()
			}
			markDeadCCPAModel(endpoint, candidate, 2*time.Minute)
			if candErr != nil {
				lastCCPAErr = fmt.Errorf("CCPA candidate %q timed out waiting for first token: %v", candidate, candErr)
			} else {
				lastCCPAErr = fmt.Errorf("CCPA candidate %q stream error before first token: %v", candidate, scanErr)
			}
			log.Printf("[omni-gateway] %v; trying next CCPA tier model", lastCCPAErr)
			continue
		}

		idleTimer.Reset(CCPAStreamIdleTimeout)
		resp = r
		activeCCPAModel = candidate
		activeScanner = scanner
		activeIdleTimer = idleTimer
		activeCandCtx = candCtx
		activeCancel = cancelCand
		activeTimedOut = &timedOut
		initialText = candInitialText.String()
		initialThoughts = candInitialThoughts.String()
		lastUsage = candLastUsage
		lastFinishReason = candFinishReason
		break
	}

	if resp == nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return lastCCPAErr
	}
	defer activeIdleTimer.Stop()
	defer activeCancel()
	defer resp.Body.Close()

	if !noThinking && resolved.CanonicalID != "" {
		workingCCPAModelCache.Store(endpoint+"|acc:"+normalizeAccScope(accScope)+"|"+resolved.CanonicalID, activeCCPAModel)
	}

	created := time.Now().Unix()
	respID := fmt.Sprintf("chatcmpl-ccpa-%d", time.Now().UnixNano())

	if req.Stream {
		var streamedText strings.Builder
		var streamedThoughts strings.Builder

		w.Header().Set("X-Omni-Gateway-Resolved-Model", activeCCPAModel)
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
			textDelta, thoughtDelta, uMeta, rawFR := extractCCPAChunk(dataStr)
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
			markDeadCCPAModel(endpoint, activeCCPAModel, 2*time.Minute)
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

		if streamScanErr == nil && stopReason != "length" && streamedText.Len() > 0 && !IsUnsupportedClientBanner(streamedText.String()) {
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
			GetGlobalCache().Put(req, cachedResp)
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
		tDelta, thDelta, uMeta, rawFR := extractCCPAChunk(dataStr)
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
			markDeadCCPAModel(endpoint, activeCCPAModel, 2*time.Minute)
		}
		return err
	}

	if IsUnsupportedClientBanner(fullText.String()) {
		return fmt.Errorf("CCPA client version rejected by upstream (update OMNI_AGY_USER_AGENT): %s", TruncateUTF8(fullText.String(), 200))
	}

	w.Header().Set("X-Omni-Gateway-Resolved-Model", activeCCPAModel)
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

	if stopReason != "length" {
		GetGlobalCache().Put(req, outResp)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	return json.NewEncoder(w).Encode(outResp)
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

	healthy := make([]ResolvedModel, 0, len(primaryPool))
	degraded := make([]ResolvedModel, 0, len(primaryPool))
	for _, t := range primaryPool {
		if t.Provider == ProviderAntigravity && t.CCPAModelID != "" && !isVirtualCCPAIDEAlias(t.CCPAModelID) && isDeadCCPAModel(endpoint, t.CCPAModelID, now) {
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

func isVirtualCCPAIDEAlias(candidate string) bool {
	m := strings.ToLower(strings.TrimSpace(candidate))
	return strings.HasPrefix(m, "gemini-4-carbon") ||
		strings.HasPrefix(m, "gemini-4-argon") ||
		m == "gemini-3.8-flash" ||
		strings.HasPrefix(m, "gemini-3.1-pro") ||
		strings.HasSuffix(m, "@default")
}

func buildCCPACandidatesWithCache(endpoint, accScope string, resolved ResolvedModel, noThinking bool) []string {
	if endpoint == "" {
		endpoint = DefaultCCPAEndpoint
	}
	if resolved.ComboMode == ComboModeStriped && strings.TrimSpace(resolved.CCPAModelID) != "" && !isVirtualCCPAIDEAlias(resolved.CCPAModelID) {
		return []string{strings.TrimSpace(resolved.CCPAModelID)}
	}
	isDefaultProdEndpoint := strings.EqualFold(strings.TrimSpace(endpoint), DefaultCCPAEndpoint)
	scope := normalizeAccScope(accScope)
	var raw []string
	var cachedWinner string
	if !noThinking && resolved.CanonicalID != "" {
		if cached, ok := workingCCPAModelCache.Load(endpoint + "|acc:" + scope + "|" + resolved.CanonicalID); ok {
			if s, ok := cached.(string); ok && s != "" {
				cachedWinner = strings.TrimSpace(s)
				raw = append(raw, cachedWinner)
			}
		}
	}

	switch {
	case resolved.IsCarbonFamily || strings.HasPrefix(resolved.CCPAModelID, "gemini-4-carbon"):
		switch resolved.ThinkingLevel {
		case ThinkingLevelLow:
			raw = append(raw,
				"gemini-4-carbon-low",
				"gemini-3-flash",
				"gemini-3.5-flash-lite",
				"claude-sonnet-4-6",
				"gemini-3.8-flash-tiered",
			)
		case ThinkingLevelMedium:
			raw = append(raw,
				"gemini-4-carbon-medium",
				"gemini-4-carbon",
				"claude-sonnet-4-6",
				"claude-opus-4-6-thinking",
				"gemini-3-flash",
				"gemini-3.8-flash-tiered",
			)
		default:
			raw = append(raw,
				"gemini-4-carbon-high",
				"gemini-4-carbon",
				"claude-opus-4-6-thinking",
				"claude-sonnet-4-6",
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
				"gemini-3.5-flash-lite",
				"claude-sonnet-4-6",
				"gemini-3.8-flash-tiered",
			)
		case ThinkingLevelMedium:
			raw = append(raw,
				"gemini-4-argon-medium",
				"claude-sonnet-4-6",
				"claude-opus-4-6-thinking",
				"gemini-3-flash",
				"gemini-3.8-flash-tiered",
			)
		default:
			raw = append(raw,
				"gemini-4-argon-high",
				"claude-opus-4-6-thinking",
				"claude-sonnet-4-6",
				"gemini-3-flash",
				"gemini-3.8-flash-tiered",
			)
		}

	case strings.HasPrefix(resolved.CCPAModelID, "gemini-3.1-pro") || strings.Contains(resolved.CanonicalID, "gemini-3.1-pro"):
		if resolved.CCPAModelID != "" {
			raw = append(raw, resolved.CCPAModelID)
		}
		raw = append(raw,
			"gemini-3.1-pro-preview",
			"claude-opus-4-6-thinking",
			"claude-sonnet-4-6",
			"gemini-3-flash",
			"gemini-3.8-flash-tiered",
		)

	case strings.HasPrefix(resolved.CCPAModelID, "gemini-3.8-flash"):
		if resolved.CCPAModelID != "" {
			raw = append(raw, resolved.CCPAModelID)
		}
		if resolved.ThinkingLevel == ThinkingLevelLow {
			raw = append(raw,
				"gemini-3-flash",
				"gemini-3.5-flash-lite",
				"claude-sonnet-4-6",
				"gemini-3.8-flash-tiered",
			)
		} else {
			raw = append(raw,
				"gemini-3-flash",
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
			"gemini-3-flash",
			"gemini-3.8-flash-tiered",
		)
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

	// On DefaultCCPAEndpoint ONLY, place real backend CCPA model IDs ahead of virtual Antigravity IDE aliases
	// so container cold-start requests never pay 700-1400ms probing HTTP 404 aliases.
	if isDefaultProdEndpoint && len(deduped) > 1 {
		realIDs := make([]string, 0, len(deduped))
		virtualAliases := make([]string, 0, len(deduped))
		for idx, m := range deduped {
			if idx == 0 && cachedWinner != "" && m == cachedWinner {
				realIDs = append(realIDs, m)
				continue
			}
			if isVirtualCCPAIDEAlias(m) {
				virtualAliases = append(virtualAliases, m)
			} else {
				realIDs = append(realIDs, m)
			}
		}
		if len(realIDs) > 0 && len(virtualAliases) > 0 {
			deduped = append(realIDs, virtualAliases...)
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
	var chunk ccpaStreamChunk
	if err := json.Unmarshal([]byte(dataStr), &chunk); err != nil {
		return "", "", nil, ""
	}
	candidates := chunk.Candidates
	usage = chunk.UsageMetadata
	if chunk.Response != nil {
		if len(chunk.Response.Candidates) > 0 {
			candidates = chunk.Response.Candidates
		}
		if chunk.Response.UsageMetadata != nil {
			usage = chunk.Response.UsageMetadata
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
	return tBuilder.String(), thBuilder.String(), usage, finishReason
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

		if req.Stream {
			return writeStreamingResponse(w, bodyBytes, resolved.RequestedID)
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
