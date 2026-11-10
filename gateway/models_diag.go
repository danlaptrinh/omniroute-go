package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ModelDiagnosticCard represents an item in the "Mô hình có sẵn" dashboard (matching Image 2).
type ModelDiagnosticCard struct {
	ID                string     `json:"id"`
	DisplayName       string     `json:"display_name"`
	Provider          ProviderID `json:"provider"`
	Badge             string     `json:"badge"` // e.g. "BUILT-IN Gemini 4 Argon (High)"
	IsActive          bool       `json:"is_active"`
	IsHidden          bool       `json:"is_hidden"`
	LastStatus        string     `json:"last_status"` // "ok", "error", "untested"
	LatencyMs         int64      `json:"latency_ms"`
	LastTestedAt      string     `json:"last_tested_at,omitempty"`
	ErrorMessage      string     `json:"error_message,omitempty"`
	SupportedAccounts []string   `json:"supported_accounts"` // List of account emails/labels
	IsImageModel      bool       `json:"is_image_model"`
}

var (
	modelDiagMu   sync.RWMutex
	diagCardState = make(map[string]*ModelDiagnosticCard)
	hiddenModels  = make(map[string]bool)

	diagResolver = NewResolver()
	diagUpstream = NewUpstream(GatewayConfig{CCPAEndpoint: DefaultCCPAEndpoint})
)

// curatedDashboardModels lists the clean, deduplicated, fast models displayed in the dashboard.
var curatedDashboardModels = []struct {
	ID       string
	Provider ProviderID
}{
	// --- Provider 1 (Flagship Gen 4 Carbon): Google Antigravity ---
	{"gemini-4-carbon-high", ProviderAntigravity},
	{"gemini-4-carbon-medium", ProviderAntigravity},
	{"gemini-4-carbon-low", ProviderAntigravity},

	// --- Combos (Smart Routing & Collaborative Multi-Model Pipelines) ---
	{"auto/seo-master", ProviderAutoCombo},
	{"auto/best-coding", ProviderAutoCombo},
	{"auto/team-coding", ProviderAutoCombo},
	{"auto/council-pro", ProviderAutoCombo},
	{"auto/night-coding", ProviderAutoCombo},
	{"auto/fast", ProviderAutoCombo},
	{"auto/reasoning", ProviderAutoCombo},

	// --- Provider 1: Google Antigravity (Gen 4, Gen 3, Claude & Image) ---
	{"gemini-4-argon-high", ProviderAntigravity},
	{"gemini-4-argon-medium", ProviderAntigravity},
	{"gemini-4-argon-low", ProviderAntigravity},
	{"gemini-3.1-pro-high", ProviderAntigravity},
	{"gemini-3.1-pro-medium", ProviderAntigravity},
	{"gemini-3.1-pro-low", ProviderAntigravity},
	{"gemini-3.8-flash-high", ProviderAntigravity},
	{"gemini-3.8-flash-medium", ProviderAntigravity},
	{"gemini-3.8-flash-low", ProviderAntigravity},
	{"antigravity/gemini-3.5-flash-lite", ProviderAntigravity},
	{"antigravity/gemini-3.1-flash-lite", ProviderAntigravity},
	{"antigravity/gemini-3-flash", ProviderAntigravity},
	{"antigravity/claude-opus-4-6-thinking", ProviderAntigravity},
	{"antigravity/claude-sonnet-4-6", ProviderAntigravity},
	{"gemini-3-pro-image", ProviderAntigravity},

	// --- Provider 2: Alibaba Token Plan (sk-sp-* MaaS & Coding Plan) ---
	{"alibaba/auto", ProviderAlibaba},
	{"alibaba/qwen3.8-max", ProviderAlibaba},
	{"alibaba/qwen3.8-flash", ProviderAlibaba},
	{"alibaba/qwen3.7-plus", ProviderAlibaba},
	{"alibaba/qwen3.7-max", ProviderAlibaba},
	{"alibaba/qwen3.6-flash", ProviderAlibaba},
	{"alibaba/deepseek-v4.1-flash-1017", ProviderAlibaba},
	{"alibaba/deepseek-v4.1-flash", ProviderAlibaba},
	{"alibaba/deepseek-v4-pro-0813", ProviderAlibaba},
	{"alibaba/deepseek-v4-pro", ProviderAlibaba},
	{"alibaba/deepseek-v4-flash-0731", ProviderAlibaba},
	{"alibaba/glm-5.3", ProviderAlibaba},
	{"alibaba/glm-5.2", ProviderAlibaba},
	{"alibaba/qwen-image-3.0-pro", ProviderAlibaba},
	{"alibaba/wan2.7-image", ProviderAlibaba},
	{"alibaba/wan2.7-image-pro", ProviderAlibaba},
	{"alibaba/wan3.0-video", ProviderAlibaba},
	{"alibaba/happyhorse-1.1-r2v", ProviderAlibaba},
	{"alibaba/happyhorse-1.1-t2v", ProviderAlibaba},
	{"alibaba/happyhorse-1.1-i2v", ProviderAlibaba},
	{"alibaba/qwen-audio-3.0-asr-flash", ProviderAlibaba},
	{"alibaba/qwen-audio-3.0-realtime-plus", ProviderAlibaba},
	{"alibaba/qwen-audio-3.0-tts-plus", ProviderAlibaba},
	{"alibaba/qwen3-coder-plus", ProviderAlibaba},

	// --- Provider 3: Anthropic Claude Code ---
	{"claude-code/claude-opus-5-5", ProviderClaudeCode},
	{"claude-code/claude-opus-5-5-thinking", ProviderClaudeCode},
	{"claude-code/claude-fable-5-1", ProviderClaudeCode},
	{"claude-code/claude-mythos-5-1", ProviderClaudeCode},
	{"claude-code/claude-sonnet-5-5", ProviderClaudeCode},
	{"claude-code/claude-sonnet-5-5-thinking", ProviderClaudeCode},
	{"claude-code/claude-haiku-5-5", ProviderClaudeCode},
	{"claude-code/claude-sonnet-5", ProviderClaudeCode},
	{"claude-code/claude-opus-5", ProviderClaudeCode},
	{"claude-code/claude-fable-5", ProviderClaudeCode},
	{"claude-code/claude-mythos-5", ProviderClaudeCode},
	{"claude-code/claude-opus-4-8", ProviderClaudeCode},
	{"claude-code/claude-opus-4-7", ProviderClaudeCode},
	{"claude-code/claude-opus-4-6-thinking", ProviderClaudeCode},
	{"claude-code/claude-opus-4-6", ProviderClaudeCode},
	{"claude-code/claude-sonnet-4-6", ProviderClaudeCode},
	{"claude-code/claude-haiku-4-5", ProviderClaudeCode},
}

// GetModelDiagnosticsList returns the clean, deduplicated model diagnostic cards for connected providers.
func GetModelDiagnosticsList(ctx context.Context) []ModelDiagnosticCard {
	modelDiagMu.Lock()
	defer modelDiagMu.Unlock()

	auth := LoadSavedAuthConfig()
	now := time.Now().Unix()

	var result []ModelDiagnosticCard
	for _, m := range curatedDashboardModels {
		// Calculate accounts that support this model (preferring active non-cooldown accounts)
		var activeSupp []string
		var fallbackSupp []string
		for _, acc := range auth.Accounts {
			if acc.Disabled {
				continue
			}
			accProv := acc.Provider
			if accProv == "" {
				accProv = ProviderAntigravity
			}
			if accProv == m.Provider || m.Provider == ProviderAutoCombo {
				masked := MaskEmail(acc.Email)
				fallbackSupp = append(fallbackSupp, masked)
				if acc.CooldownUntil <= now {
					activeSupp = append(activeSupp, masked)
				}
			}
		}

		// Skip provider models that have zero connected accounts so unconnected providers don't clutter or error out
		if m.Provider != ProviderAutoCombo && len(fallbackSupp) == 0 && len(auth.Accounts) > 0 {
			continue
		}

		card, exists := diagCardState[m.ID]
		if !exists {
			badge := buildModelBadge(m.ID)
			isImg := IsImageModel(m.ID)
			card = &ModelDiagnosticCard{
				ID:           m.ID,
				DisplayName:  m.ID,
				Provider:     m.Provider,
				Badge:        badge,
				IsActive:     true,
				IsHidden:     hiddenModels[m.ID],
				LastStatus:   "untested",
				IsImageModel: isImg,
			}
			diagCardState[m.ID] = card
		}

		if len(activeSupp) > 0 {
			card.SupportedAccounts = activeSupp
		} else {
			card.SupportedAccounts = fallbackSupp
		}
		card.Badge = buildModelBadge(m.ID)
		card.IsHidden = hiddenModels[m.ID]
		result = append(result, *card)
	}

	return result
}

func buildModelBadge(id string) string {
	s := strings.ToLower(id)
	switch {
	case strings.Contains(s, "carbon"):
		switch {
		case strings.Contains(s, "high"):
			return "BUILT-IN Gemini 4 Carbon (High)"
		case strings.Contains(s, "medium"):
			return "BUILT-IN Gemini 4 Carbon (Medium)"
		case strings.Contains(s, "low"):
			return "BUILT-IN Gemini 4 Carbon (Low)"
		default:
			return "BUILT-IN Gemini 4 Carbon"
		}
	case strings.Contains(s, "gemini-4-argon"):
		return "BUILT-IN Gemini 4 Argon"
	case strings.Contains(s, "qwen-image"):
		return "BUILT-IN Qwen Image 3.0 Pro"
	case strings.Contains(s, "wan2.7-image"):
		return "BUILT-IN Wan 2.7 Image"
	case strings.Contains(s, "wan3.0-video"):
		return "BUILT-IN Wan 3.0 Video (New)"
	case strings.Contains(s, "happyhorse"):
		return "BUILT-IN HappyHorse 1.1 Video"
	case strings.Contains(s, "qwen-audio"):
		return "BUILT-IN Qwen Audio 3.0"
	case strings.Contains(s, "image") || strings.Contains(s, "imagen"):
		return "BUILT-IN Gemini 3.1 Flash Image"
	case strings.Contains(s, "gemini-3.8-flash"):
		return "BUILT-IN Gemini 3.8 Flash"
	case strings.Contains(s, "gemini-3.7-flash"):
		return "BUILT-IN Gemini 3.7 Flash"
	case strings.Contains(s, "gemini-3.5-flash"):
		return "BUILT-IN Gemini 3.5 Flash Lite"
	case strings.Contains(s, "gemini-3.1-flash"):
		return "BUILT-IN Gemini 3.1 Flash Lite"
	case strings.Contains(s, "gemini-3-flash"):
		return "BUILT-IN Gemini 3 Flash"
	case strings.Contains(s, "gemini-3.1-pro"):
		return "BUILT-IN Gemini 3.1 Pro"
	case strings.Contains(s, "claude-opus-5-5"):
		return "BUILT-IN Claude Opus 5.5 (Default)"
	case strings.Contains(s, "claude-fable-5-1"):
		return "BUILT-IN Claude Fable 5.1"
	case strings.Contains(s, "claude-mythos-5-1"):
		return "BUILT-IN Claude Mythos 5.1 (New)"
	case strings.Contains(s, "claude-sonnet-5-5"):
		return "BUILT-IN Claude Sonnet 5.5"
	case strings.Contains(s, "claude-haiku-5-5"):
		return "BUILT-IN Claude Haiku 5.5"
	case strings.Contains(s, "claude-sonnet-5"):
		return "BUILT-IN Claude Sonnet 5"
	case strings.Contains(s, "claude-opus-5"):
		return "BUILT-IN Claude Opus 5"
	case strings.Contains(s, "claude-fable-5"):
		return "BUILT-IN Claude Fable 5"
	case strings.Contains(s, "claude-mythos-5"):
		return "BUILT-IN Claude Mythos 5 (New)"
	case strings.Contains(s, "claude-opus-4-8"):
		return "BUILT-IN Claude Opus 4.8"
	case strings.Contains(s, "claude-opus-4-7"):
		return "BUILT-IN Claude Opus 4.7"
	case strings.Contains(s, "claude-opus"):
		return "BUILT-IN Claude Opus 4.6 (Thinking)"
	case strings.Contains(s, "claude-sonnet"):
		return "BUILT-IN Claude Sonnet 4.6"
	case strings.Contains(s, "claude-haiku"):
		return "BUILT-IN Claude Haiku 4.5"
	case s == "alibaba/auto":
		return "BUILT-IN Qwen Auto (New)"
	case strings.Contains(s, "qwen3.8-max"):
		return "⚡ 40% Off Nights • Qwen 3.8 Max (New)"
	case strings.Contains(s, "qwen3.8-flash"):
		return "⚡ 40% Off Nights • Qwen 3.8 Flash"
	case strings.Contains(s, "qwen3.7-plus"):
		return "BUILT-IN Qwen 3.7 Plus (Token Plan)"
	case strings.Contains(s, "qwen3.7-max"):
		return "BUILT-IN Qwen 3.7 Max (Token Plan)"
	case strings.Contains(s, "qwen3.6-flash"):
		return "BUILT-IN Qwen 3.6 Flash (Token Plan)"
	case strings.Contains(s, "deepseek-v4.1-flash-1017"):
		return "⚡ 50% Off Nights • DeepSeek V4.1 Flash 1017 (New)"
	case strings.Contains(s, "deepseek-v4.1-flash"):
		return "⚡ 50% Off Nights • DeepSeek V4.1 Flash"
	case strings.Contains(s, "deepseek-v4-pro-0813"):
		return "⚡ Limited-time Night 50% Off • DeepSeek V4 Pro 0813"
	case strings.Contains(s, "deepseek-v4-flash-0731"):
		return "⚡ Limited-time Night 50% Off • DeepSeek V4 Flash 0731"
	case strings.Contains(s, "deepseek-v4-pro"):
		return "BUILT-IN DeepSeek V4 Pro (Token Plan)"
	case strings.Contains(s, "glm-5.3"):
		return "BUILT-IN Zhipu GLM 5.3 (Token Plan)"
	case strings.Contains(s, "glm-5.2"):
		return "BUILT-IN Zhipu GLM 5.2 (Token Plan)"
	case strings.Contains(s, "qwen3-coder"):
		return "BUILT-IN Qwen3 Coder Plus"
	case strings.Contains(s, "qwen3-max") || strings.Contains(s, "qwen-max"):
		return "BUILT-IN Qwen3 Max"
	case strings.Contains(s, "deepseek-r1"):
		return "BUILT-IN DeepSeek R1"
	case strings.Contains(s, "qwq"):
		return "BUILT-IN QwQ Plus"
	case s == "auto/team-coding":
		return "COMBO Pipeline (Opus 5.5 ➔ Argon 4 ➔ Qwen 3.8 Max)"
	case s == "auto/council-pro":
		return "COMBO Hội Đồng Đa Mô Hình (Mixture of Agents)"
	case s == "auto/night-coding":
		return "COMBO ⚡ Night 40-50% Off (Alibaba ➔ Argon 4)"
	case s == "auto/reasoning":
		return "COMBO Suy Luận Đa Tầng (Pipeline)"
	case strings.HasPrefix(s, "auto/"):
		return "COMBO OmniRoute Smart Routing"
	default:
		return "BUILT-IN Model"
	}
}

func recordModelDiagResult(modelID string, provider ProviderID, latency int64, err error) {
	modelDiagMu.Lock()
	defer modelDiagMu.Unlock()

	card, ok := diagCardState[modelID]
	if !ok {
		card = &ModelDiagnosticCard{
			ID:           modelID,
			DisplayName:  modelID,
			Provider:     provider,
			Badge:        buildModelBadge(modelID),
			IsActive:     true,
			IsHidden:     hiddenModels[modelID],
			IsImageModel: IsImageModel(modelID),
		}
		diagCardState[modelID] = card
	}
	card.LastTestedAt = time.Now().Format("15:04:05")
	card.LatencyMs = latency
	if err != nil {
		card.LastStatus = "error"
		card.ErrorMessage = err.Error()
	} else {
		card.LastStatus = "ok"
		card.ErrorMessage = ""
	}
}

// TestModel performs an actual live connectivity test on a single model.
func TestModel(ctx context.Context, modelID string) (int64, error) {
	start := time.Now()

	resolved := diagResolver.Resolve(modelID, defaultFallbackModels)
	if !resolved.IsCombo {
		// Strictly test the target model/provider without cross-provider fallback masking
		resolved.FallbackChain = nil
	}

	promptText := `"ok"`
	if IsImageModel(modelID) {
		// Fast connectivity probe directly against CCPA gemini-3.1-flash-image (~1s instead of 17s image rendering)
		resolved.Provider = ProviderAntigravity
		resolved.CanonicalID = "antigravity/gemini-3.1-flash-image"
		resolved.UpstreamModelID = "gemini-3.1-flash-image"
		resolved.CCPAModelID = "gemini-3.1-flash-image"
		promptText = `"Reply strictly with the single word OK, do not generate an image."`
	}

	dummyW := &dummyResponseWriter{header: make(http.Header)}
	zeroTemp := 0.0
	req := OpenAIChatRequest{
		Model:       modelID,
		MaxTokens:   4,
		Stream:      true,
		Temperature: &zeroTemp,
		ThinkingConfig: &ThinkingConfig{
			IncludeThoughts: false,
			ThinkingLevel:   ThinkingLevelNone,
			ThinkingBudget:  0,
		},
		Messages: []OpenAIChatMessage{
			{Role: "user", Content: []byte(promptText)},
		},
	}

	err := diagUpstream.HandleChatCompletion(ctx, dummyW, req, resolved, "")
	latency := time.Since(start).Milliseconds()
	if err == nil && !dummyW.firstByteAt.IsZero() {
		ttfb := dummyW.firstByteAt.Sub(start).Milliseconds()
		if ttfb > 0 {
			latency = ttfb
		}
	}
	if err == nil && len(dummyW.buf) > 0 {
		rawPreview := string(dummyW.buf)
		agg := aggregateSSEToResponse(dummyW.buf, modelID)
		preview := rawPreview
		if len(agg.Choices) > 0 && agg.Choices[0].Message != nil && strings.TrimSpace(agg.Choices[0].Message.Content) != "" {
			preview = agg.Choices[0].Message.Content
		}
		if IsUnsupportedClientBanner(preview) || IsUnsupportedClientBanner(rawPreview) {
			err = fmt.Errorf("CCPA client version rejected by upstream (update OMNI_AGY_USER_AGENT): %s", TruncateUTF8(preview, 200))
		}
	}

	recordModelDiagResult(modelID, resolved.Provider, latency, err)
	return latency, err
}

type dummyResponseWriter struct {
	header      http.Header
	buf         []byte
	code        int
	firstByteAt time.Time
}

func (d *dummyResponseWriter) Header() http.Header { return d.header }
func (d *dummyResponseWriter) Write(b []byte) (int, error) {
	if d.firstByteAt.IsZero() && len(b) > 0 {
		d.firstByteAt = time.Now()
	}
	d.buf = append(d.buf, b...)
	return len(b), nil
}
func (d *dummyResponseWriter) WriteHeader(statusCode int) {
	d.code = statusCode
	if statusCode == http.StatusOK && d.firstByteAt.IsZero() {
		d.firstByteAt = time.Now()
	}
}
func (d *dummyResponseWriter) Flush() {}

// --- HTTP API Handlers for Model Diagnostics ---

func (s *Server) handleModelDiagnostics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeOpenAIError(w, http.StatusMethodNotAllowed, "only GET is supported", "invalid_request_error")
		return
	}

	list := GetModelDiagnosticsList(r.Context())
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"object": "list",
		"data":   list,
		"total":  len(list),
	})
}

func (s *Server) handleModelTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeOpenAIError(w, http.StatusMethodNotAllowed, "only POST is supported", "invalid_request_error")
		return
	}

	var req struct {
		Model string `json:"model"`
		All   bool   `json:"all"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 16*1024)).Decode(&req); err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "invalid JSON: "+err.Error(), "invalid_request_error")
		return
	}

	if req.All {
		// Run batch test on active curated models: warm up cache with first model, then run concurrent worker pool
		go func() {
			cards := GetModelDiagnosticsList(context.Background())
			if len(cards) == 0 {
				return
			}
			_, _ = TestModel(context.Background(), cards[0].ID)

			sem := make(chan struct{}, 4)
			var wg sync.WaitGroup
			for _, c := range cards[1:] {
				wg.Add(1)
				sem <- struct{}{}
				go func(id string) {
					defer wg.Done()
					defer func() { <-sem }()
					_, _ = TestModel(context.Background(), id)
				}(c.ID)
			}
			wg.Wait()
		}()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "testing_all_started"})
		return
	}

	lat, err := TestModel(r.Context(), req.Model)
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":     "error",
			"model":      req.Model,
			"latency_ms": lat,
			"error":      err.Error(),
		})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":     "ok",
		"model":      req.Model,
		"latency_ms": lat,
	})
}

func (s *Server) handleModelToggleVisibility(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeOpenAIError(w, http.StatusMethodNotAllowed, "only POST is supported", "invalid_request_error")
		return
	}

	var req struct {
		Model  string `json:"model"`
		Hidden bool   `json:"hidden"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 16*1024)).Decode(&req); err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "invalid JSON: "+err.Error(), "invalid_request_error")
		return
	}

	modelDiagMu.Lock()
	hiddenModels[req.Model] = req.Hidden
	if card, ok := diagCardState[req.Model]; ok {
		card.IsHidden = req.Hidden
	}
	modelDiagMu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status": "ok",
		"model":  req.Model,
		"hidden": req.Hidden,
	})
}
