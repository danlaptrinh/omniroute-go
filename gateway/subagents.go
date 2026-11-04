package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type pinnedAccountKeyType struct{}

var (
	inFlightMu        sync.RWMutex
	accountInFlight   = make(map[string]int)
	accountActiveRole = make(map[string]string)

	swarmHistoryMu     sync.RWMutex
	recentSwarmRuns    []SwarmRunSummary
	totalSubAgentsRun  int64
	activeSubAgentsNow int64
)

// WithPinnedAccount attaches a specific account email/ID to ctx so SelectProviderAccount binds 1-to-1 to that account.
func WithPinnedAccount(ctx context.Context, emailOrID string) context.Context {
	clean := strings.TrimSpace(emailOrID)
	if clean == "" {
		return ctx
	}
	return context.WithValue(ctx, pinnedAccountKeyType{}, clean)
}

// PinnedAccountFromContext retrieves the pinned account email/ID from ctx, if any.
func PinnedAccountFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(pinnedAccountKeyType{}).(string); ok {
		return v
	}
	return ""
}

// AcquireAccountInFlight increments the active concurrent request counter for an account.
func AcquireAccountInFlight(email string) {
	key := strings.ToLower(strings.TrimSpace(email))
	if key == "" {
		return
	}
	inFlightMu.Lock()
	accountInFlight[key]++
	inFlightMu.Unlock()
}

// AcquireAccountInFlightWithRole increments the active counter and records the current Sub-Agent role on the account.
func AcquireAccountInFlightWithRole(email string, role string) {
	key := strings.ToLower(strings.TrimSpace(email))
	if key == "" {
		return
	}
	inFlightMu.Lock()
	accountInFlight[key]++
	if role != "" {
		accountActiveRole[key] = role
	}
	inFlightMu.Unlock()
}

// ReleaseAccountInFlight decrements the active concurrent request counter for an account.
func ReleaseAccountInFlight(email string) {
	key := strings.ToLower(strings.TrimSpace(email))
	if key == "" {
		return
	}
	inFlightMu.Lock()
	if accountInFlight[key] > 0 {
		accountInFlight[key]--
	}
	if accountInFlight[key] == 0 {
		delete(accountActiveRole, key)
	}
	inFlightMu.Unlock()
}

// GetAccountInFlight returns the current number of concurrent requests running on an account.
func GetAccountInFlight(email string) int {
	key := strings.ToLower(strings.TrimSpace(email))
	if key == "" {
		return 0
	}
	inFlightMu.RLock()
	c := accountInFlight[key]
	inFlightMu.RUnlock()
	return c
}

// GetAccountActiveRole returns the active Sub-Agent role currently running on an account, if any.
func GetAccountActiveRole(email string) string {
	key := strings.ToLower(strings.TrimSpace(email))
	if key == "" {
		return ""
	}
	inFlightMu.RLock()
	r := accountActiveRole[key]
	inFlightMu.RUnlock()
	return r
}

// SubAgentTask defines a single Sub-Agent task inside a Parallel Swarm execution.
type SubAgentTask struct {
	Index         int    `json:"index"`
	Role          string `json:"role"`
	Prompt        string `json:"prompt"`
	Model         string `json:"model,omitempty"`
	PinnedAccount string `json:"pinned_account,omitempty"`
}

// SubAgentResult holds the execution result of a single Sub-Agent worker.
type SubAgentResult struct {
	Index         int    `json:"index"`
	Role          string `json:"role"`
	Prompt        string `json:"prompt"`
	AccountEmail  string `json:"account_email"`
	ResolvedModel string `json:"resolved_model"`
	Status        string `json:"status"` // "ok" or "error"
	LatencyMs     int64  `json:"latency_ms"`
	Tokens        int    `json:"tokens"`
	Output        string `json:"output"`
	Error         string `json:"error,omitempty"`
}

// SwarmRunSummary records a completed parallel Sub-Agent Swarm execution.
type SwarmRunSummary struct {
	ID               string           `json:"id"`
	Timestamp        string           `json:"timestamp"`
	Mode             string           `json:"mode"` // "auto-decompose" or "custom-tasks"
	MasterGoal       string           `json:"master_goal,omitempty"`
	Model            string           `json:"model"`
	TotalSubAgents   int              `json:"total_sub_agents"`
	SuccessCount     int              `json:"success_count"`
	DistinctAccounts int              `json:"distinct_accounts"`
	TotalWallTimeMs  int64            `json:"total_wall_time_ms"`
	TotalTokens      int              `json:"total_tokens"`
	Results          []SubAgentResult `json:"results"`
}

// AccountSlotStatus represents one account slot in the 50-Account Sub-Agent Pool matrix.
type AccountSlotStatus struct {
	SlotIndex    int        `json:"slot_index"`
	Email        string     `json:"email"`
	Provider     ProviderID `json:"provider"`
	ProjectID    string     `json:"project_id,omitempty"`
	Enabled      bool       `json:"enabled"`
	InCooldown   bool       `json:"in_cooldown"`
	InFlight     int        `json:"in_flight"`
	ActiveRole   string     `json:"active_role,omitempty"`
	SuccessCount int64      `json:"success_count"`
	TotalTokens  int64      `json:"total_tokens"`
	AvgLatencyMs int64      `json:"avg_latency_ms"`
}

// listActiveAntigravityAccounts returns all enabled, non-cooldown Antigravity accounts first, followed by other active accounts.
func listActiveAccountPool() []ConnectedAccount {
	saved := LoadSavedAuthConfig()
	now := time.Now().Unix()

	var agyReady []ConnectedAccount
	var otherReady []ConnectedAccount
	var fallback []ConnectedAccount

	for _, a := range saved.Accounts {
		if a.Disabled {
			continue
		}
		prov := a.Provider
		if prov == "" {
			prov = ProviderAntigravity
		}
		if a.CooldownUntil <= now {
			if prov == ProviderAntigravity {
				agyReady = append(agyReady, a)
			} else {
				otherReady = append(otherReady, a)
			}
		} else {
			fallback = append(fallback, a)
		}
	}

	out := make([]ConnectedAccount, 0, len(agyReady)+len(otherReady)+len(fallback))
	out = append(out, agyReady...)
	out = append(out, otherReady...)
	if len(out) == 0 {
		out = append(out, fallback...)
	}
	return out
}

// buildDefaultSubAgentTasks decomposes a master goal into N specialized parallel Sub-Agent tasks.
func buildDefaultSubAgentTasks(goal string, count int, model string) []SubAgentTask {
	if count <= 0 {
		count = 4
	}
	if count > 60 {
		count = 60
	}

	specialties := []struct {
		role   string
		prefix string
	}{
		{"Sub-Agent #1 • Kiến Trúc Hệ Thống (System Architect)", "Thiết kế kiến trúc tổng thể, sơ đồ luồng dữ liệu, contracts/interfaces và cấu trúc thư mục cho yêu cầu sau: "},
		{"Sub-Agent #2 • Backend & API Core Engineer", "Viết mã nguồn xử lý Backend Core, REST/RPC API handlers và logic nghiệp vụ chính cho yêu cầu sau: "},
		{"Sub-Agent #3 • Database, PostgreSQL & Vector Schema", "Thiết kế schema cơ sở dữ liệu, truy vấn tối ưu, indexing và lớp lưu trữ bền vững cho yêu cầu sau: "},
		{"Sub-Agent #4 • Frontend & Dashboard UI Engineer", "Thiết kế và viết giao diện người dùng (UI/UX), trạng thái tương tác và kết nối API cho yêu cầu sau: "},
		{"Sub-Agent #5 • Bảo Mật, OAuth2 & Rate-Limit Guard", "Kiểm tra bảo mật, xác thực OAuth2/API Key, chống tấn công và tối ưu phân tải cho yêu cầu sau: "},
		{"Sub-Agent #6 • DevOps, Docker & Nginx VPS Deployment", "Xây dựng cấu hình Dockerfile, docker-compose, Nginx Reverse Proxy SSL và script tự động triển khai VPS cho yêu cầu sau: "},
		{"Sub-Agent #7 • Performance & Concurrency Optimizer", "Tối ưu hóa hiệu năng, Goroutines song song, bộ nhớ đệm (Cache) và giảm độ trễ cho yêu cầu sau: "},
		{"Sub-Agent #8 • Unit Tests & Edge-Case QA", "Viết bộ kiểm thử tự động (Unit Tests / Integration Tests) và xử lý các trường hợp biên (Edge Cases) cho yêu cầu sau: "},
	}

	tasks := make([]SubAgentTask, 0, count)
	for i := 0; i < count; i++ {
		var role, prompt string
		if i < len(specialties) {
			role = specialties[i].role
			prompt = specialties[i].prefix + goal
		} else {
			role = fmt.Sprintf("Sub-Agent #%d • Parallel Module Worker #%d", i+1, i+1)
			prompt = fmt.Sprintf("[Nhiệm vụ phân mảnh #%d/%d] Triển khai chi tiết module chuyên biệt số %d và giải pháp kỹ thuật cho bài toán: %s", i+1, count, i+1, goal)
		}
		tasks = append(tasks, SubAgentTask{
			Index:  i + 1,
			Role:   role,
			Prompt: prompt,
			Model:  model,
		})
	}
	return tasks
}

// ExecuteSubAgentSwarm launches N Sub-Agents in 100% parallel Goroutines, assigning each Sub-Agent to a distinct account in the pool.
func (s *Server) ExecuteSubAgentSwarm(
	ctx context.Context,
	mode string,
	masterGoal string,
	agentCount int,
	defaultModel string,
	customTasks []SubAgentTask,
) SwarmRunSummary {
	startWall := time.Now()
	if strings.TrimSpace(defaultModel) == "" {
		defaultModel = "antigravity/gemini-4-argon-high"
	}

	var tasks []SubAgentTask
	if len(customTasks) > 0 {
		for i, t := range customTasks {
			if strings.TrimSpace(t.Prompt) == "" {
				continue
			}
			t.Index = i + 1
			if strings.TrimSpace(t.Role) == "" {
				t.Role = fmt.Sprintf("Sub-Agent #%d", i+1)
			}
			if strings.TrimSpace(t.Model) == "" {
				t.Model = defaultModel
			}
			tasks = append(tasks, t)
		}
		if mode == "" {
			mode = "custom-tasks"
		}
	} else {
		tasks = buildDefaultSubAgentTasks(masterGoal, agentCount, defaultModel)
		mode = "auto-decompose"
	}

	pool := listActiveAccountPool()
	results := make([]SubAgentResult, len(tasks))
	var wg sync.WaitGroup

	for i, task := range tasks {
		wg.Add(1)
		// Pin Sub-Agent #i strictly to Account #(i % len(pool)) so 50 Sub-Agents run on 50 distinct accounts simultaneously!
		pinnedEmail := task.PinnedAccount
		if pinnedEmail == "" && len(pool) > 0 {
			pinnedEmail = pool[i%len(pool)].Email
		}

		go func(idx int, t SubAgentTask, assignedEmail string) {
			defer wg.Done()
			atomic.AddInt64(&activeSubAgentsNow, 1)
			atomic.AddInt64(&totalSubAgentsRun, 1)
			defer atomic.AddInt64(&activeSubAgentsNow, -1)

			if assignedEmail != "" {
				AcquireAccountInFlightWithRole(assignedEmail, t.Role)
				defer ReleaseAccountInFlight(assignedEmail)
			}

			tStart := time.Now()
			modelID := strings.TrimSpace(t.Model)
			if modelID == "" {
				modelID = defaultModel
			}
			resolved := s.resolver.Resolve(modelID, s.cfg.FallbackModels)

			sysText := fmt.Sprintf("Bạn là %s trong Đàn Sub-Agents Song Song của OmniRoute-Go. Hãy trả lời ngắn gọn, súc tích, đi thẳng vào giải pháp kỹ thuật/code trọng tâm.", t.Role)
			rawSys, _ := json.Marshal(sysText)
			rawUser, _ := json.Marshal(t.Prompt)

			chatReq := OpenAIChatRequest{
				Model: modelID,
				Messages: []OpenAIChatMessage{
					{Role: "system", Content: rawSys},
					{Role: "user", Content: rawUser},
				},
				MaxTokens: 2048,
				Stream:    false,
			}

			subCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
			defer cancel()
			if assignedEmail != "" {
				subCtx = WithPinnedAccount(subCtx, assignedEmail)
			}

			rec := newResponseRecorder()
			err := s.upstream.HandleChatCompletion(subCtx, rec, chatReq, resolved, "")
			lat := time.Since(tStart).Milliseconds()

			actualAcc := rec.Header().Get("X-Omni-Gateway-Account")
			if actualAcc == "" {
				actualAcc = assignedEmail
			}
			actualModel := rec.Header().Get("X-Omni-Gateway-Resolved-Model")
			if actualModel == "" {
				actualModel = resolved.CanonicalID
			}

			if err != nil || rec.statusCode >= 400 {
				errMsg := ""
				if err != nil {
					errMsg = err.Error()
				} else {
					errMsg = fmt.Sprintf("HTTP %d: %s", rec.statusCode, strings.TrimSpace(rec.body.String()))
				}
				results[idx] = SubAgentResult{
					Index:         t.Index,
					Role:          t.Role,
					Prompt:        t.Prompt,
					AccountEmail:  actualAcc,
					ResolvedModel: actualModel,
					Status:        "error",
					LatencyMs:     lat,
					Error:         errMsg,
				}
				return
			}

			txt, reas, toks := extractAssistantTextFromRecorder(rec, modelID)
			if strings.TrimSpace(txt) == "" && reas != "" {
				txt = reas
			}
			if toks <= 0 {
				toks = EstimateTokens(chatReq.Messages) + (len(txt)+3)/4
			}

			results[idx] = SubAgentResult{
				Index:         t.Index,
				Role:          t.Role,
				Prompt:        t.Prompt,
				AccountEmail:  actualAcc,
				ResolvedModel: actualModel,
				Status:        "ok",
				LatencyMs:     lat,
				Tokens:        toks,
				Output:        txt,
			}
		}(i, task, pinnedEmail)
	}

	wg.Wait()

	wallMs := time.Since(startWall).Milliseconds()
	succCount := 0
	totalToks := 0
	uniqueAccs := make(map[string]bool)
	for _, r := range results {
		if r.Status == "ok" {
			succCount++
		}
		totalToks += r.Tokens
		if r.AccountEmail != "" {
			uniqueAccs[strings.ToLower(r.AccountEmail)] = true
		}
	}

	summary := SwarmRunSummary{
		ID:               fmt.Sprintf("swarm-%d", time.Now().UnixNano()),
		Timestamp:        time.Now().Format("15:04:05 02/01/2006"),
		Mode:             mode,
		MasterGoal:       masterGoal,
		Model:            defaultModel,
		TotalSubAgents:   len(results),
		SuccessCount:     succCount,
		DistinctAccounts: len(uniqueAccs),
		TotalWallTimeMs:  wallMs,
		TotalTokens:      totalToks,
		Results:          results,
	}

	swarmHistoryMu.Lock()
	recentSwarmRuns = append([]SwarmRunSummary{summary}, recentSwarmRuns...)
	if len(recentSwarmRuns) > 15 {
		recentSwarmRuns = recentSwarmRuns[:15]
	}
	swarmHistoryMu.Unlock()

	return summary
}

// BulkImportAntigravityInputs imports multiple OAuth callback URLs, refresh tokens (1//...), or access tokens (ya29...) line by line.
func BulkImportAntigravityInputs(ctx context.Context, rawLines string) (int, []string, error) {
	lines := strings.Split(rawLines, "\n")
	imported := 0
	var errs []string

	for idx, line := range lines {
		clean := strings.TrimSpace(line)
		if clean == "" || strings.HasPrefix(clean, "#") {
			continue
		}
		// Check if line is "email|refresh_token" or "1//..." refresh token
		if strings.HasPrefix(clean, "1//") || strings.Contains(clean, "|1//") {
			labelEmail := fmt.Sprintf("agy-pool-acc-%d@gmail.com", time.Now().Unix()%10000+int64(idx))
			refToken := clean
			if strings.Contains(clean, "|") {
				parts := strings.SplitN(clean, "|", 2)
				if strings.TrimSpace(parts[0]) != "" {
					labelEmail = strings.TrimSpace(parts[0])
				}
				refToken = strings.TrimSpace(parts[1])
			}
			acc := ConnectedAccount{
				ID:           generateAccountID(ProviderAntigravity, labelEmail),
				Provider:     ProviderAntigravity,
				Email:        labelEmail,
				RefreshToken: refToken,
				ExpiresAt:    1,
				UpdatedAt:    time.Now().UTC().Format(time.RFC3339),
			}
			_ = RefreshAntigravityToken(ctx, &acc)
			if acc.AccessToken != "" {
				// Fetch real user email if possible
				infoReq, err := http.NewRequestWithContext(ctx, http.MethodGet, OAuthUserInfoEndpoint, nil)
				if err == nil {
					infoReq.Header.Set("Authorization", "Bearer "+acc.AccessToken)
					if infoResp, err := http.DefaultClient.Do(infoReq); err == nil {
						var uInfo struct {
							Email string `json:"email"`
						}
						_ = json.NewDecoder(infoResp.Body).Decode(&uInfo)
						_ = infoResp.Body.Close()
						if uInfo.Email != "" {
							acc.Email = uInfo.Email
							acc.ID = generateAccountID(ProviderAntigravity, uInfo.Email)
						}
					}
				}
			}
			if _, err := UpsertConnectedAccount(acc); err != nil {
				errs = append(errs, fmt.Sprintf("Dòng %d: %v", idx+1, err))
			} else {
				imported++
			}
			continue
		}

		// Standard OAuth callback URL, code, or ya29 token
		if _, err := ExchangeOAuthInput(ctx, clean, AgyOAuthDefaultRedirectURI); err != nil {
			errs = append(errs, fmt.Sprintf("Dòng %d: %v", idx+1, err))
		} else {
			imported++
		}
	}
	return imported, errs, nil
}

// handleSubAgents handles GET and POST /v1/subagents for the 50-Account Parallel Sub-Agent Swarm Engine.
func (s *Server) handleSubAgents(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		saved := LoadSavedAuthConfig()
		now := time.Now().Unix()

		slots := make([]AccountSlotStatus, 0, len(saved.Accounts))
		agyCount := 0
		readyCount := 0

		for idx, a := range saved.Accounts {
			prov := a.Provider
			if prov == "" {
				prov = ProviderAntigravity
			}
			if prov == ProviderAntigravity {
				agyCount++
			}
			inCD := a.CooldownUntil > now
			if !a.Disabled && !inCD {
				readyCount++
			}
			slots = append(slots, AccountSlotStatus{
				SlotIndex:    idx + 1,
				Email:        a.Email,
				Provider:     prov,
				ProjectID:    a.ProjectID,
				Enabled:      !a.Disabled,
				InCooldown:   inCD,
				InFlight:     GetAccountInFlight(a.Email),
				ActiveRole:   GetAccountActiveRole(a.Email),
				SuccessCount: a.SuccessCount,
				TotalTokens:  a.TotalTokens,
				AvgLatencyMs: a.AvgLatencyMs,
			})
		}

		swarmHistoryMu.RLock()
		history := make([]SwarmRunSummary, len(recentSwarmRuns))
		copy(history, recentSwarmRuns)
		swarmHistoryMu.RUnlock()

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":               "ok",
			"total_accounts":       len(saved.Accounts),
			"antigravity_accounts": agyCount,
			"ready_accounts":       readyCount,
			"max_parallel_target":  50,
			"active_sub_agents":    atomic.LoadInt64(&activeSubAgentsNow),
			"total_sub_agents_run": atomic.LoadInt64(&totalSubAgentsRun),
			"account_slots":        slots,
			"recent_swarms":        history,
		})

	case http.MethodPost:
		var req struct {
			Action      string         `json:"action"` // "swarm" or "bulk_import"
			Mode        string         `json:"mode"`   // "auto-decompose" or "custom-tasks"
			MasterGoal  string         `json:"master_goal"`
			AgentCount  int            `json:"agent_count"`
			Model       string         `json:"model"`
			CustomTasks []SubAgentTask `json:"tasks"`
			BulkInput   string         `json:"bulk_input"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 512*1024)).Decode(&req); err != nil {
			writeOpenAIError(w, http.StatusBadRequest, "invalid JSON payload: "+err.Error(), "invalid_request_error")
			return
		}

		if req.Action == "bulk_import" {
			imported, errs, err := BulkImportAntigravityInputs(r.Context(), req.BulkInput)
			if err != nil {
				writeOpenAIError(w, http.StatusBadRequest, err.Error(), "bulk_import_error")
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status":   "ok",
				"imported": imported,
				"errors":   errs,
			})
			return
		}

		summary := s.ExecuteSubAgentSwarm(r.Context(), req.Mode, req.MasterGoal, req.AgentCount, req.Model, req.CustomTasks)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":  "ok",
			"summary": summary,
		})

	default:
		writeOpenAIError(w, http.StatusMethodNotAllowed, "method not allowed", "invalid_request_error")
	}
}
