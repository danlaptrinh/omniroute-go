package gateway

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	DefaultCCPAEndpoint      = "https://daily-cloudcode-pa.googleapis.com/v1internal:streamGenerateContent?alt=sse"
	DefaultGatewayConfigPath = `C:\Users\cloud\.gemini\gateway_config.json`
)

// SharedHTTPTransport and SharedHTTPClient reuse warm HTTP/2 connections across all requests,
// eliminating repeated TCP + TLS handshakes (~300-500ms saved per request).
var (
	SharedHTTPTransport = &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		MaxIdleConns:          256,
		MaxIdleConnsPerHost:   64,
		MaxConnsPerHost:       128,
		IdleConnTimeout:       120 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ForceAttemptHTTP2:     true,
	}
	SharedHTTPClient = &http.Client{
		Transport: SharedHTTPTransport,
		Timeout:   180 * time.Second,
	}

	// Fast-path caches for CCPA candidate resolution to avoid sequential HTTP 404 probing.
	workingCCPAModelCache sync.Map // key: resolved.CanonicalID -> value: string (winning CCPA model ID)
	deadCCPAModelCache    sync.Map // key: candidate CCPA model ID -> value: time.Time (when 404 occurred)
)

// GatewayUpstream implements UpstreamHandler with Standalone Direct Antigravity CCPA mode
// (daily-cloudcode-pa.googleapis.com/v1internal:streamGenerateContent?alt=sse) by default,
// and optional secondary upstream only if explicitly configured.
type GatewayUpstream struct {
	cfg    GatewayConfig
	client *http.Client
}

// NewUpstream creates a new GatewayUpstream instance sharing the warm HTTP/2 connection pool.
func NewUpstream(cfg GatewayConfig) *GatewayUpstream {
	if cfg.CCPAEndpoint == "" {
		cfg.CCPAEndpoint = DefaultCCPAEndpoint
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
		}
	}

	var lastErr error

	hedgedEnabled, hedgeDelayMs := GetHedgedConfig()
	if hedgedEnabled && len(targets) > 1 && !req.Stream {
		if err := u.handleHedgedCompletion(ctx, w, req, targets, cleanAuth, hedgeDelayMs); err == nil {
			return nil
		}
	}

	for _, target := range targets {
		if err := u.tryTarget(ctx, w, req, target, cleanAuth); err == nil {
			return nil
		} else {
			lastErr = err
		}
	}

	if strings.TrimSpace(u.cfg.OmniRouteBaseURL) != "" {
		return u.handleOmniRouteWithFallback(ctx, w, req, resolved, cleanAuth)
	}

	if lastErr != nil && !strings.Contains(lastErr.Error(), "no ") {
		writeOpenAIError(w, http.StatusBadGateway, fmt.Sprintf("direct Antigravity CCPA error for model %q (%s): %v", resolved.RequestedID, resolved.CCPAModelID, lastErr), "upstream_error")
		return lastErr
	}

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
		acc, err := SelectProviderAccount(ctx, ProviderClaudeCode)
		if err != nil && strings.HasPrefix(cleanAuth, "sk-ant-") {
			acc = &ConnectedAccount{Provider: ProviderClaudeCode, Email: "header-token", AccessToken: cleanAuth}
		}
		if acc == nil {
			return fmt.Errorf("no Claude Code account configured")
		}
		err = u.handleClaudeCodeProvider(ctx, w, req, target, *acc)
		lat := time.Now().UnixMilli() - startMs
		if err == nil {
			usedTokens := parseRecordedTokens(w.Header().Get("X-Omni-Gateway-Tokens"), req)
			RecordAccountResult(acc.Email, ProviderClaudeCode, true, lat, usedTokens, http.StatusOK)
			entry := GatewayLogEntry{
				Provider:       ProviderClaudeCode,
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
		RecordAccountResult(acc.Email, ProviderClaudeCode, false, lat, 0, http.StatusBadGateway)
		errEntry := GatewayLogEntry{
			Provider:       ProviderClaudeCode,
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
		log.Printf("[omniroute-go] claude-code target %q failed: %v; trying next provider in chain", target.CanonicalID, err)
		return err

	case ProviderAlibaba:
		acc, err := SelectProviderAccount(ctx, ProviderAlibaba)
		if err != nil && strings.HasPrefix(cleanAuth, "sk-") && !strings.HasPrefix(cleanAuth, "sk-ant-") {
			acc = &ConnectedAccount{Provider: ProviderAlibaba, Email: "header-key", APIKey: cleanAuth}
		}
		if acc == nil {
			return fmt.Errorf("no Alibaba Token Plan account configured")
		}
		err = u.handleAlibabaProvider(ctx, w, req, target, *acc)
		lat := time.Now().UnixMilli() - startMs
		if err == nil {
			usedTokens := parseRecordedTokens(w.Header().Get("X-Omni-Gateway-Tokens"), req)
			RecordAccountResult(acc.Email, ProviderAlibaba, true, lat, usedTokens, http.StatusOK)
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
		RecordAccountResult(acc.Email, ProviderAlibaba, false, lat, 0, http.StatusBadGateway)
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
		var lastErr error
		for attempt := 0; attempt < 2; attempt++ {
			agyToken := strings.TrimSpace(u.cfg.AgyOAuthToken)
			projectID := strings.TrimSpace(u.cfg.AgyProjectID)
			accountEmail := ""

			if tok, proj, email, err := GetValidAccountToken(ctx); err == nil && tok != "" {
				agyToken = tok
				if proj != "" {
					projectID = proj
				}
				accountEmail = email
			}
			if strings.HasPrefix(cleanAuth, "ya29.") {
				agyToken = cleanAuth
			}

			if agyToken == "" {
				return fmt.Errorf("no Antigravity OAuth token configured")
			}

			if accountEmail != "" {
				w.Header().Set("X-Omni-Gateway-Account", accountEmail)
			}
			w.Header().Set("X-Omni-Gateway-Provider", "antigravity")
			err := u.handleDirectCCPA(ctx, w, req, target, agyToken, projectID)
			lat := time.Now().UnixMilli() - startMs
			if err == nil {
				usedTokens := parseRecordedTokens(w.Header().Get("X-Omni-Gateway-Tokens"), req)
				if accountEmail != "" {
					RecordAccountResult(accountEmail, ProviderAntigravity, true, lat, usedTokens, http.StatusOK)
				}
				entry := GatewayLogEntry{
					Provider:       ProviderAntigravity,
					Account:        accountEmail,
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

			failCode := http.StatusBadGateway
			errMsg := err.Error()
			if strings.Contains(errMsg, "HTTP 401") || strings.Contains(errMsg, "HTTP 403") {
				failCode = http.StatusUnauthorized
			} else if strings.Contains(errMsg, "HTTP 429") {
				failCode = http.StatusTooManyRequests
			}
			if accountEmail != "" {
				RecordAccountResult(accountEmail, ProviderAntigravity, false, lat, 0, failCode)
			}
			errEntry := GatewayLogEntry{
				Provider:       ProviderAntigravity,
				Account:        accountEmail,
				RequestedModel: req.Model,
				ResolvedModel:  target.CanonicalID,
				StatusCode:     failCode,
				LatencyMs:      lat,
				IsError:        true,
				ErrorMessage:   errMsg,
				FallbackChain:  target.FallbackChain,
			}
			EnrichLogEntryFromContext(ctx, &errEntry, req)
			RecordLogEntry(errEntry)
			lastErr = err
			log.Printf("[omniroute-go] antigravity CCPA attempt %d for %q (%s, acc=%s) failed: %v", attempt+1, target.RequestedID, target.CCPAModelID, accountEmail, err)
			if failCode != http.StatusUnauthorized && failCode != http.StatusTooManyRequests {
				break
			}
		}
		return lastErr
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

	genCfg := ccpaGenConfig{
		Temperature:     &temp,
		MaxOutputTokens: req.MaxTokens,
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

	endpoint := u.cfg.CCPAEndpoint
	if endpoint == "" {
		endpoint = DefaultCCPAEndpoint
	}
	useFastCache := (endpoint == DefaultCCPAEndpoint)
	noThinking := req.ThinkingConfig != nil && !req.ThinkingConfig.IncludeThoughts && req.ThinkingConfig.ThinkingBudget == 0 && req.ThinkingConfig.ThinkingLevel == ThinkingLevelNone
	ccpaCandidates := buildCCPACandidatesWithCache(resolved, useFastCache, noThinking)

	var resp *http.Response
	var activeCCPAModel string
	var lastCCPAErr error

	for _, candidate := range ccpaCandidates {
		candInner := inner
		// Anthropic models, gemini-3-flash, and image models on CCPA do not accept thinkingConfig overrides
		if (strings.HasPrefix(candidate, "claude-") || candidate == "gemini-3-flash" || strings.Contains(candidate, "image")) && candInner.GenerationConfig.ThinkingConfig != nil {
			candInner.GenerationConfig.ThinkingConfig = nil
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

		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
		if err != nil {
			lastCCPAErr = err
			continue
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Accept", "text/event-stream")
		httpReq.Header.Set("User-Agent", AgyUserAgent)
		httpReq.Header.Set("Authorization", "Bearer "+oauthToken)

		r, err := u.client.Do(httpReq)
		if err != nil {
			lastCCPAErr = err
			continue
		}

		if r.StatusCode >= 200 && r.StatusCode < 300 {
			resp = r
			activeCCPAModel = candidate
			if useFastCache && !noThinking && resolved.CanonicalID != "" {
				workingCCPAModelCache.Store(resolved.CanonicalID, candidate)
			}
			break
		}

		if useFastCache && r.StatusCode == http.StatusNotFound {
			deadCCPAModelCache.Store(candidate, time.Now())
		}
		errBody, _ := io.ReadAll(io.LimitReader(r.Body, 4096))
		_ = r.Body.Close()
		lastCCPAErr = fmt.Errorf("CCPA HTTP %d (%s): %s", r.StatusCode, candidate, strings.TrimSpace(string(errBody)))
		log.Printf("[omni-gateway] CCPA candidate %q returned HTTP %d; trying next CCPA tier model", candidate, r.StatusCode)
	}

	if resp == nil {
		return lastCCPAErr
	}
	defer resp.Body.Close()

	w.Header().Set("X-Omni-Gateway-Resolved-Model", activeCCPAModel)
	w.Header().Set("X-Omni-Gateway-Route", "ccpa-direct")
	if resolved.ThinkingLevel != ThinkingLevelNone {
		w.Header().Set("X-Omni-Gateway-Thinking-Level", string(resolved.ThinkingLevel))
	}

	created := time.Now().Unix()
	respID := fmt.Sprintf("chatcmpl-ccpa-%d", time.Now().UnixNano())

	if req.Stream {
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

		var streamedText strings.Builder
		var streamedThoughts strings.Builder
		var lastUsage *ccpaUsageMetadata

		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			dataStr := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if dataStr == "" || dataStr == "[DONE]" {
				continue
			}
			textDelta, thoughtDelta, uMeta := extractCCPAPartsAndUsage(dataStr)
			if uMeta != nil && (uMeta.TotalTokenCount > 0 || uMeta.PromptTokenCount > 0 || uMeta.CandidatesTokenCount > 0) {
				lastUsage = uMeta
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

		usage := computeCCPAUsage(req, streamedText.Len(), streamedThoughts.Len(), lastUsage)
		w.Header().Set("X-Omni-Gateway-Tokens", fmt.Sprintf("%d", usage.TotalTokens))
		SetAuditResponse(ctx, streamedText.String(), usage.PromptTokens, usage.CompletionTokens)

		stopReason := "stop"
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

		if streamedText.Len() > 0 {
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
	var lastUsage *ccpaUsageMetadata

	rawBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	scanner := bufio.NewScanner(bytes.NewReader(rawBytes))
	scanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)
	foundSSE := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		foundSSE = true
		dataStr := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if dataStr == "" || dataStr == "[DONE]" {
			continue
		}
		tDelta, thDelta, uMeta := extractCCPAPartsAndUsage(dataStr)
		if uMeta != nil && (uMeta.TotalTokenCount > 0 || uMeta.PromptTokenCount > 0 || uMeta.CandidatesTokenCount > 0) {
			lastUsage = uMeta
		}
		fullText.WriteString(tDelta)
		fullThoughts.WriteString(thDelta)
	}

	if !foundSSE {
		tDelta, thDelta, uMeta := extractCCPAPartsAndUsage(string(rawBytes))
		if uMeta != nil && (uMeta.TotalTokenCount > 0 || uMeta.PromptTokenCount > 0 || uMeta.CandidatesTokenCount > 0) {
			lastUsage = uMeta
		}
		fullText.WriteString(tDelta)
		fullThoughts.WriteString(thDelta)
	}

	usage := computeCCPAUsage(req, fullText.Len(), fullThoughts.Len(), lastUsage)
	w.Header().Set("X-Omni-Gateway-Tokens", fmt.Sprintf("%d", usage.TotalTokens))
	SetAuditResponse(ctx, fullText.String(), usage.PromptTokens, usage.CompletionTokens)

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
				FinishReason: "stop",
			},
		},
		Usage: &usage,
	}

	GetGlobalCache().Put(req, outResp)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	return json.NewEncoder(w).Encode(outResp)
}

func buildCCPACandidates(resolved ResolvedModel) []string {
	return buildCCPACandidatesWithCache(resolved, false, false)
}

func buildCCPACandidatesWithCache(resolved ResolvedModel, useFastCache bool, noThinking bool) []string {
	var raw []string
	if useFastCache {
		if !noThinking && resolved.CanonicalID != "" {
			if cached, ok := workingCCPAModelCache.Load(resolved.CanonicalID); ok {
				if s, ok := cached.(string); ok && s != "" {
					raw = append(raw, s)
				}
			}
		}
		// Put known live CCPA identifiers before unmapped OmniRoute aliases to avoid cold-start 404s
		isFastOrLow := noThinking ||
			resolved.ThinkingLevel == ThinkingLevelLow ||
			strings.Contains(resolved.CanonicalID, "lite") ||
			strings.Contains(resolved.RequestedID, "fast")
		switch {
		case isFastOrLow && !strings.HasPrefix(resolved.CCPAModelID, "claude-"):
			raw = append(raw, "gemini-3-flash", "gemini-3.8-flash-tiered")
		case resolved.IsArgonFamily || strings.HasPrefix(resolved.CCPAModelID, "gemini-4-argon") || strings.HasPrefix(resolved.CCPAModelID, "gemini-3.8-flash"):
			raw = append(raw, "gemini-3.8-flash-tiered", "gemini-3-flash")
		}
	}
	if resolved.CCPAModelID != "" {
		raw = append(raw, resolved.CCPAModelID)
	}
	switch {
	case resolved.IsArgonFamily || resolved.CCPAModelID == "gemini-4-argon":
		raw = append(raw,
			"gemini-4-argon",
			"gemini-3.8-flash-tiered",
			"claude-sonnet-4-6",
			"claude-opus-4-6-thinking",
			"gemini-3-flash",
		)
	case strings.HasPrefix(resolved.CCPAModelID, "gemini-3.1-pro") || strings.Contains(resolved.CanonicalID, "gemini-3.1-pro"):
		raw = append(raw,
			"gemini-3.1-pro-preview",
			"gemini-3.1-pro",
			"gemini-3.8-flash-tiered",
			"claude-sonnet-4-6",
			"gemini-3-flash",
		)
	case strings.HasPrefix(resolved.CCPAModelID, "gemini-3.8-flash"):
		raw = append(raw,
			"gemini-3.8-flash-tiered",
			"claude-sonnet-4-6",
			"gemini-3-flash",
			"gemini-3.5-flash-lite",
		)
	case strings.HasPrefix(resolved.CCPAModelID, "claude-opus"):
		raw = append(raw,
			"claude-opus-4-6-thinking",
			"claude-sonnet-4-6",
			"gemini-3.8-flash-tiered",
		)
	case strings.HasPrefix(resolved.CCPAModelID, "claude-sonnet"):
		raw = append(raw,
			"claude-sonnet-4-6",
			"claude-opus-4-6-thinking",
			"gemini-3.8-flash-tiered",
		)
	default:
		raw = append(raw,
			"claude-sonnet-4-6",
			"gemini-3.8-flash-tiered",
			"claude-opus-4-6-thinking",
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

	if !useFastCache {
		return deduped
	}

	// Filter out candidates that recently returned HTTP 404 on live CCPA (TTL: 30 minutes)
	now := time.Now()
	alive := make([]string, 0, len(deduped))
	for _, m := range deduped {
		if deadVal, isDead := deadCCPAModelCache.Load(m); isDead {
			if deadAt, ok := deadVal.(time.Time); ok && now.Sub(deadAt) < 30*time.Minute {
				continue
			}
			deadCCPAModelCache.Delete(m)
		}
		alive = append(alive, m)
	}
	if len(alive) > 0 {
		return alive
	}
	return deduped
}

func extractCCPAParts(dataStr string) (text string, thought string) {
	t, th, _ := extractCCPAPartsAndUsage(dataStr)
	return t, th
}

func extractCCPAPartsAndUsage(dataStr string) (text string, thought string, usage *ccpaUsageMetadata) {
	var chunk ccpaStreamChunk
	if err := json.Unmarshal([]byte(dataStr), &chunk); err != nil {
		return "", "", nil
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
		for _, p := range c.Content.Parts {
			if p.Thought {
				thBuilder.WriteString(p.Text)
			} else {
				tBuilder.WriteString(p.Text)
			}
		}
	}
	return tBuilder.String(), thBuilder.String(), usage
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
				idx+1, len(candidates), candidateModel, truncateLog(bodyStr, 180))
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
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
