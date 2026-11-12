package gateway

import (
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

// ComboModeStriped distributes concurrent requests across a combo's healthy models via atomic
// round-robin primary selection while keeping all remaining models in the combo as instant failover.
const ComboModeStriped ComboMode = "striped"

// BuiltInStripedCombos returns built-in multi-model striped parallel combo presets that spread
// concurrent requests across independent upstream model quota/capacity pools (Claude + Gemini).
func BuiltInStripedCombos() []ComboModel {
	return []ComboModel{
		{
			ID:          "auto/max-speed",
			Name:        "⚡ Max-Speed Striped Parallel (Gemini 3 Flash ⇄ Gemini 3.5 Flash Lite ⇄ Claude Sonnet 4.6)",
			Mode:        ComboModeStriped,
			Description: "Chia tải tuần tự tốc độ siêu thanh (Atomic Round-Robin) qua 3 bể hạn mức độc lập: Gemini 3 Flash, Gemini 3.5 Flash Lite và Claude Sonnet 4.6, kèm tự động failover tức thì.",
			Models: []string{
				"antigravity/gemini-3-flash",
				"antigravity/gemini-3.5-flash-lite",
				"antigravity/claude-sonnet-4-6",
			},
			Roles: []string{
				"Striped Pool 1 (Gemini 3 Flash)",
				"Striped Pool 2 (Gemini 3.5 Flash Lite)",
				"Striped Pool 3 (Claude Sonnet 4.6)",
			},
			BuiltIn: true,
		},
		{
			ID:          "auto/parallel-flagship",
			Name:        "🚀 Parallel Flagship Striped (Claude Opus 4.6 Thinking ⇄ Claude Sonnet 4.6 ⇄ Gemini 3 Flash)",
			Mode:        ComboModeStriped,
			Description: "Nhân 3x thông lượng coding thông minh song song trên toàn dàn tài khoản bằng cách rải đều request qua Claude Opus 4.6 Thinking, Claude Sonnet 4.6 và Gemini 3 Flash.",
			Models: []string{
				"antigravity/claude-opus-4-6-thinking",
				"antigravity/claude-sonnet-4-6",
				"antigravity/gemini-3-flash",
			},
			Roles: []string{
				"Flagship Pool 1 (Claude Opus 4.6 Thinking)",
				"Flagship Pool 2 (Claude Sonnet 4.6)",
				"Flagship Pool 3 (Gemini 3 Flash)",
			},
			BuiltIn: true,
		},
	}
}

// SaveComboModel creates or updates a custom ComboModel in gateway_config.json.
func SaveComboModel(combo ComboModel) ([]ComboModel, error) {
	id := strings.TrimSpace(combo.ID)
	if id == "" {
		return nil, fmt.Errorf("combo ID is required")
	}
	if !strings.HasPrefix(strings.ToLower(id), "auto/") {
		id = "auto/" + id
	}
	combo.ID = id
	if strings.TrimSpace(combo.Name) == "" {
		combo.Name = id
	}
	if combo.Mode == "" {
		combo.Mode = ComboModeFailover
	}

	cleanedModels := make([]string, 0, len(combo.Models))
	for _, m := range combo.Models {
		m = strings.TrimSpace(m)
		if m != "" {
			cleanedModels = append(cleanedModels, m)
		}
	}
	if len(cleanedModels) == 0 {
		return nil, fmt.Errorf("combo must contain at least 1 model")
	}
	combo.Models = cleanedModels

	configMu.Lock()
	cfg := loadSavedAuthConfigLocked()
	found := false
	for i, existing := range cfg.CustomCombos {
		if strings.EqualFold(existing.ID, combo.ID) {
			cfg.CustomCombos[i] = combo
			found = true
			break
		}
	}
	if !found {
		cfg.CustomCombos = append(cfg.CustomCombos, combo)
	}
	cfg.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	err := saveAuthConfigLocked(cfg)
	configMu.Unlock()
	if err != nil {
		return nil, err
	}
	return NewResolver().ListCombos(), nil
}

// DeleteComboModel removes a custom combo (or resets a customized built-in combo back to default).
func DeleteComboModel(id string) ([]ComboModel, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, fmt.Errorf("missing combo id")
	}
	if !strings.HasPrefix(strings.ToLower(id), "auto/") {
		id = "auto/" + id
	}

	configMu.Lock()
	cfg := loadSavedAuthConfigLocked()
	filtered := make([]ComboModel, 0, len(cfg.CustomCombos))
	for _, existing := range cfg.CustomCombos {
		if !strings.EqualFold(existing.ID, id) {
			filtered = append(filtered, existing)
		}
	}
	cfg.CustomCombos = filtered
	cfg.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	err := saveAuthConfigLocked(cfg)
	configMu.Unlock()
	if err != nil {
		return nil, err
	}
	return NewResolver().ListCombos(), nil
}

// handleCombos handles GET, POST, and DELETE /v1/combos for the Multi-Model Combo Builder UI.
func (s *Server) handleCombos(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		combos := s.resolver.ListCombos()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "ok",
			"combos": combos,
			"modes": []map[string]string{
				{
					"id":          string(ComboModePipeline),
					"name":        "🔗 Pipeline Nối Tiếp (Kiến Trúc Sư ➔ Lập Trình Viên)",
					"description": "Model 1 phân tích kiến trúc & lập kế hoạch ➔ chuyển kết quả cho Model 2 viết code/tổng hợp hoàn chỉnh",
				},
				{
					"id":          string(ComboModeConsensus),
					"name":        "🧠 Hội Đồng Đa Mô Hình Song Song (Mixture of Agents)",
					"description": "Chạy song song 2-3 model bằng Goroutines ➔ Model Tổng Hợp đúc kết câu trả lời xuất sắc nhất",
				},
				{
					"id":          string(ComboModeNightSaver),
					"name":        "⚡ Tối Ưu Giảm Giá Ban Đêm (Night 40%-50% Off)",
					"description": "Tự động ưu tiên các model giảm 40%-50% phí token ban đêm của Alibaba Cloud + dự phòng Antigravity & Claude",
				},
				{
					"id":          string(ComboModeStriped),
					"name":        "🚀 Striped Song Song Đa Mô Hình (3x-4x RPS Multi-Pool)",
					"description": "Chia tải tuần tự (Atomic Round-Robin) qua nhiều bể hạn mức model khác nhau ở mỗi request + tự động failover tức thì sang các model còn lại",
				},
				{
					"id":          string(ComboModeFailover),
					"name":        "🛡️ Failover Chuyển Tiếp Tức Thì (High Availability)",
					"description": "Ưu tiên chạy Model 1; nếu hết hạn mức hoặc lỗi sẽ tự động nhảy sang Model 2 ➔ Model 3",
				},
			},
		})

	case http.MethodPost:
		var req ComboModel
		if err := json.NewDecoder(io.LimitReader(r.Body, 64*1024)).Decode(&req); err != nil {
			writeOpenAIError(w, http.StatusBadRequest, "invalid JSON payload: "+err.Error(), "invalid_request_error")
			return
		}
		combos, err := SaveComboModel(req)
		if err != nil {
			writeOpenAIError(w, http.StatusBadRequest, err.Error(), "invalid_request_error")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "saved",
			"combos": combos,
		})

	case http.MethodDelete:
		id := strings.TrimSpace(r.URL.Query().Get("id"))
		combos, err := DeleteComboModel(id)
		if err != nil {
			writeOpenAIError(w, http.StatusBadRequest, err.Error(), "invalid_request_error")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "deleted",
			"combos": combos,
		})

	default:
		writeOpenAIError(w, http.StatusMethodNotAllowed, "method not allowed", "invalid_request_error")
	}
}

// extractAssistantTextFromRecorder parses a recorded response body (either JSON or SSE stream)
// and extracts the assistant content and reasoning text.
func extractAssistantTextFromRecorder(rec *responseRecorder, requestedModel string) (string, string, int) {
	if rec == nil || rec.body == nil {
		return "", "", 0
	}
	raw := bytes.TrimSpace(rec.body.Bytes())
	if len(raw) == 0 {
		return "", "", 0
	}
	if bytes.HasPrefix(raw, []byte("data:")) || bytes.Contains(raw, []byte("\ndata:")) {
		agg := aggregateSSEToResponse(raw, requestedModel)
		if len(agg.Choices) > 0 && agg.Choices[0].Message != nil {
			toks := 0
			if agg.Usage != nil {
				toks = agg.Usage.TotalTokens
			}
			return agg.Choices[0].Message.Content, agg.Choices[0].Message.ReasoningContent, toks
		}
	}
	var parsed OpenAIChatResponse
	if err := json.Unmarshal(raw, &parsed); err == nil && len(parsed.Choices) > 0 {
		msg := parsed.Choices[0].Message
		if msg == nil {
			msg = parsed.Choices[0].Delta
		}
		toks := 0
		if parsed.Usage != nil {
			toks = parsed.Usage.TotalTokens
		}
		if msg != nil {
			return msg.Content, msg.ReasoningContent, toks
		}
	}
	return "", "", 0
}

// isShortHealthPing returns true if the user prompt is a trivial diagnostic ping ("ping", "hi", "test")
// so that diagnostic health checks don't trigger multi-stage synthesis unnecessarily.
func isShortHealthPing(req OpenAIChatRequest) bool {
	if req.MaxTokens > 0 && req.MaxTokens <= 16 {
		return true
	}
	if len(req.Messages) == 1 {
		txt := strings.ToLower(strings.TrimSpace(req.Messages[0].ContentText()))
		if txt == "ping" || txt == "test" || txt == "hi" || txt == "hello" {
			return true
		}
	}
	return false
}

// executeComboPipeline runs Step 1 (Architect / Planner) -> Step 2 (Executor / Coder).
// If only 1 model succeeds, it gracefully returns Step 1's response.
func (u *GatewayUpstream) executeComboPipeline(
	ctx context.Context,
	w http.ResponseWriter,
	req OpenAIChatRequest,
	resolved ResolvedModel,
	targets []ResolvedModel,
	cleanAuth string,
) error {
	if len(targets) < 2 || isShortHealthPing(req) {
		return fmt.Errorf("skipping pipeline for single target or health ping")
	}

	// Step 1: Run Architect / Planner model (non-streaming internal buffer)
	step1Req := req
	step1Req.Stream = false
	var step1Rec *responseRecorder
	var step1Target ResolvedModel
	var step1Text string
	var step1Reasoning string
	var step1Tokens int
	step1Idx := -1

	for i, candidate := range targets {
		rec := newResponseRecorder()
		stepCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
		err := u.tryTarget(stepCtx, rec, step1Req, candidate, cleanAuth)
		cancel()
		if err == nil && rec.statusCode >= 200 && rec.statusCode < 300 {
			txt, reas, toks := extractAssistantTextFromRecorder(rec, resolved.RequestedID)
			if strings.TrimSpace(txt) != "" {
				step1Rec = rec
				step1Target = candidate
				step1Text = txt
				step1Reasoning = reas
				step1Tokens = toks
				step1Idx = i
				break
			}
		}
	}

	if step1Rec == nil {
		return fmt.Errorf("pipeline step 1 failed on all targets")
	}

	// Step 2: Find a second distinct model in the chain to act as the Executor / Coder
	handoff := strings.TrimSpace(resolved.ComboHandoff)
	if handoff == "" {
		handoff = "Dựa trên bản phân tích kỹ thuật và thiết kế từ Model Bước 1 ở trên, hãy hoàn thiện câu trả lời và viết code đầy đủ, chính xác nhất cho yêu cầu của người dùng."
	}

	handoffMsgText := fmt.Sprintf(
		"[BẢN PHÂN TÍCH & KIẾN TRÚC TỪ MODEL BƯỚC 1 (%s)]:\n%s\n\n[CHỈ DẪN PHỐI HỢP BƯỚC 2]: %s",
		step1Target.CanonicalID,
		step1Text,
		handoff,
	)
	rawHandoff, _ := json.Marshal(handoffMsgText)

	step2Messages := make([]OpenAIChatMessage, 0, len(req.Messages)+1)
	step2Messages = append(step2Messages, req.Messages...)
	step2Messages = append(step2Messages, OpenAIChatMessage{
		Role:    "system",
		Content: rawHandoff,
	})

	step2Req := req
	step2Req.Messages = step2Messages
	step2Req.Stream = false

	step2Candidates := targets[step1Idx+1:]
	if strings.EqualFold(resolved.CanonicalID, "auto/seo-master") {
		lastPrompt := ""
		if len(req.Messages) > 0 {
			lastPrompt = strings.ToLower(req.Messages[len(req.Messages)-1].ContentText())
		}
		isCodeTask := strings.Contains(lastPrompt, "code") ||
			strings.Contains(lastPrompt, "html") ||
			strings.Contains(lastPrompt, "schema") ||
			strings.Contains(lastPrompt, "script") ||
			strings.Contains(lastPrompt, "api") ||
			strings.Contains(lastPrompt, "css") ||
			strings.Contains(lastPrompt, "plugin")
		if isCodeTask {
			step2Candidates = []ResolvedModel{
				ResolveModel("antigravity/gemini-3.8-flash-high", nil),
				ResolveModel("antigravity/gemini-3.1-pro-high", nil),
			}
		} else {
			step2Candidates = []ResolvedModel{
				ResolveModel("antigravity/gemini-3.1-pro-high", nil),
				ResolveModel("antigravity/gemini-3.8-flash-high", nil),
			}
		}
	}

	for _, candidate := range step2Candidates {
		if strings.EqualFold(candidate.CanonicalID, step1Target.CanonicalID) {
			continue
		}
		rec2 := newResponseRecorder()
		step2Ctx, cancel2 := context.WithTimeout(ctx, 90*time.Second)
		err2 := u.tryTarget(step2Ctx, rec2, step2Req, candidate, cleanAuth)
		cancel2()
		if err2 == nil && rec2.statusCode >= 200 && rec2.statusCode < 300 {
			txt2, reas2, toks2 := extractAssistantTextFromRecorder(rec2, resolved.RequestedID)
			if strings.TrimSpace(txt2) != "" {
				chainHeader := fmt.Sprintf("%s -> %s", step1Target.CanonicalID, candidate.CanonicalID)
				w.Header().Set("X-Omni-Gateway-Combo-Mode", string(ComboModePipeline))
				w.Header().Set("X-Omni-Gateway-Combo-Chain", chainHeader)
				w.Header().Set("X-Omni-Gateway-Resolved-Model", chainHeader)
				totalToks := step1Tokens + toks2
				if totalToks > 0 {
					w.Header().Set("X-Omni-Gateway-Tokens", fmt.Sprintf("%d", totalToks))
				}

				combinedReasoning := fmt.Sprintf("[Bước 1 - %s]:\n%s", step1Target.CanonicalID, step1Text)
				if step1Reasoning != "" {
					combinedReasoning = fmt.Sprintf("[Suy luận Bước 1 - %s]:\n%s\n\n%s", step1Target.CanonicalID, step1Reasoning, combinedReasoning)
				}
				if reas2 != "" {
					combinedReasoning += fmt.Sprintf("\n\n[Suy luận Bước 2 - %s]:\n%s", candidate.CanonicalID, reas2)
				}

				return writeSynthesizedComboResponse(w, req, resolved.RequestedID, txt2, combinedReasoning, totalToks)
			}
		}
	}

	// Graceful fallback: if no second distinct model succeeded, return Step 1's response directly
	log.Printf("[omniroute-go] pipeline combo %q completed via Step 1 (%s) only", resolved.CanonicalID, step1Target.CanonicalID)
	w.Header().Set("X-Omni-Gateway-Combo-Mode", string(ComboModePipeline))
	w.Header().Set("X-Omni-Gateway-Combo-Chain", step1Target.CanonicalID)
	return writeSynthesizedComboResponse(w, req, resolved.RequestedID, step1Text, step1Reasoning, step1Tokens)
}

// executeComboConsensus runs up to 3 models in parallel (Mixture of Agents) using Goroutines,
// and synthesizes their outputs into a single best response.
func (u *GatewayUpstream) executeComboConsensus(
	ctx context.Context,
	w http.ResponseWriter,
	req OpenAIChatRequest,
	resolved ResolvedModel,
	targets []ResolvedModel,
	cleanAuth string,
) error {
	if len(targets) < 2 || isShortHealthPing(req) {
		return fmt.Errorf("skipping consensus for single target or health ping")
	}

	// Pick up to 3 distinct models from targets to run in parallel
	var panel []ResolvedModel
	seen := make(map[string]bool)
	for _, t := range targets {
		key := strings.ToLower(t.CanonicalID)
		if !seen[key] {
			seen[key] = true
			panel = append(panel, t)
			if len(panel) == 3 {
				break
			}
		}
	}

	type draftResult struct {
		model     string
		text      string
		reasoning string
		tokens    int
	}

	drafts := make([]draftResult, len(panel))
	var wg sync.WaitGroup
	subReq := req
	subReq.Stream = false

	for idx, member := range panel {
		wg.Add(1)
		go func(i int, target ResolvedModel) {
			defer wg.Done()
			rec := newResponseRecorder()
			mCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
			err := u.tryTarget(mCtx, rec, subReq, target, cleanAuth)
			cancel()
			if err == nil && rec.statusCode >= 200 && rec.statusCode < 300 {
				txt, reas, toks := extractAssistantTextFromRecorder(rec, resolved.RequestedID)
				if strings.TrimSpace(txt) != "" {
					drafts[i] = draftResult{
						model:     target.CanonicalID,
						text:      txt,
						reasoning: reas,
						tokens:    toks,
					}
				}
			}
		}(idx, member)
	}
	wg.Wait()

	var validDrafts []draftResult
	var participatingModels []string
	totalTokens := 0
	for _, d := range drafts {
		if d.text != "" {
			validDrafts = append(validDrafts, d)
			participatingModels = append(participatingModels, d.model)
			totalTokens += d.tokens
		}
	}

	if len(validDrafts) == 0 {
		return fmt.Errorf("consensus panel returned 0 valid responses")
	}
	if len(validDrafts) == 1 {
		d := validDrafts[0]
		w.Header().Set("X-Omni-Gateway-Combo-Mode", string(ComboModeConsensus))
		w.Header().Set("X-Omni-Gateway-Combo-Chain", d.model)
		return writeSynthesizedComboResponse(w, req, resolved.RequestedID, d.text, d.reasoning, d.tokens)
	}

	// Build synthesis prompt for the Judge/Synthesizer (using the first winning model)
	var draftBuilder strings.Builder
	for i, d := range validDrafts {
		draftBuilder.WriteString(fmt.Sprintf("--- GÓC NHÌN CHUYÊN GIA %d (%s) ---\n%s\n\n", i+1, d.model, d.text))
	}

	handoff := strings.TrimSpace(resolved.ComboHandoff)
	if handoff == "" {
		handoff = "Hãy tổng hợp các câu trả lời từ Hội đồng Đa Mô Hình ở trên thành một câu trả lời cuối cùng chính xác, đầy đủ và tối ưu nhất."
	}

	synthPrompt := fmt.Sprintf(
		"[HỘI ĐỒNG ĐA MÔ HÌNH OMNIROUTE (%s)]:\n%s\n[CHỈ DẪN TỔNG HỢP]: %s",
		strings.Join(participatingModels, " + "),
		draftBuilder.String(),
		handoff,
	)
	rawSynth, _ := json.Marshal(synthPrompt)

	synthMsgs := make([]OpenAIChatMessage, 0, len(req.Messages)+1)
	synthMsgs = append(synthMsgs, req.Messages...)
	synthMsgs = append(synthMsgs, OpenAIChatMessage{
		Role:    "system",
		Content: rawSynth,
	})

	synthReq := req
	synthReq.Messages = synthMsgs
	synthReq.Stream = false

	judgeTarget := NewResolver().Resolve(validDrafts[0].model, nil)
	judgeRec := newResponseRecorder()
	jCtx, cancelJ := context.WithTimeout(ctx, 75*time.Second)
	errJ := u.tryTarget(jCtx, judgeRec, synthReq, judgeTarget, cleanAuth)
	cancelJ()

	chainStr := strings.Join(participatingModels, " + ")
	w.Header().Set("X-Omni-Gateway-Combo-Mode", string(ComboModeConsensus))
	w.Header().Set("X-Omni-Gateway-Combo-Chain", chainStr)
	w.Header().Set("X-Omni-Gateway-Resolved-Model", chainStr)

	if errJ == nil && judgeRec.statusCode >= 200 && judgeRec.statusCode < 300 {
		finalTxt, finalReas, jToks := extractAssistantTextFromRecorder(judgeRec, resolved.RequestedID)
		if strings.TrimSpace(finalTxt) != "" {
			totalTokens += jToks
			w.Header().Set("X-Omni-Gateway-Tokens", fmt.Sprintf("%d", totalTokens))
			combinedReas := draftBuilder.String()
			if finalReas != "" {
				combinedReas += "\n[Tổng hợp]: " + finalReas
			}
			return writeSynthesizedComboResponse(w, req, resolved.RequestedID, finalTxt, combinedReas, totalTokens)
		}
	}

	// If synthesis step timed out, return the primary expert's answer with all council perspectives in reasoning_content
	return writeSynthesizedComboResponse(w, req, resolved.RequestedID, validDrafts[0].text, draftBuilder.String(), totalTokens)
}

func writeSynthesizedComboResponse(
	w http.ResponseWriter,
	req OpenAIChatRequest,
	requestedModel string,
	content string,
	reasoning string,
	totalTokens int,
) error {
	pToks := EstimateTokens(req.Messages)
	cToks := (len(content) + len(reasoning) + 3) / 4
	if totalTokens <= 0 {
		totalTokens = pToks + cToks
	}

	resp := OpenAIChatResponse{
		ID:      fmt.Sprintf("chatcmpl-combo-%d", time.Now().UnixNano()),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   requestedModel,
		Choices: []OpenAIChatChoice{
			{
				Index: 0,
				Message: &OpenAIOutputMessage{
					Role:             "assistant",
					Content:          content,
					ReasoningContent: reasoning,
				},
				FinishReason: "stop",
			},
		},
		Usage: &OpenAIUsage{
			PromptTokens:     pToks,
			CompletionTokens: cToks,
			TotalTokens:      totalTokens,
		},
	}

	rawJSON, err := json.Marshal(resp)
	if err != nil {
		return err
	}
	if req.Stream {
		return writeStreamingResponse(w, rawJSON, requestedModel)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, err = w.Write(rawJSON)
	return err
}
