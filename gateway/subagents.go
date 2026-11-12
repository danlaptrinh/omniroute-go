package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
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

	swarmHistoryMu          sync.RWMutex
	recentSwarmRuns         []SwarmRunSummary
	scheduledAutomations    []ScheduledAutomation
	vaultLoadedFromDisk     bool
	vaultLoadedAtomic       atomic.Bool
	enabledAutomationsCount atomic.Int32
	totalSubAgentsRun       int64
	activeSubAgentsNow      int64
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

// UpdateAccountActiveRole updates the active role/model label for an account currently in flight.
func UpdateAccountActiveRole(email string, role string) {
	key := strings.ToLower(strings.TrimSpace(email))
	if key == "" || role == "" {
		return
	}
	inFlightMu.Lock()
	if accountInFlight[key] > 0 {
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
	Index           int    `json:"index"`
	Role            string `json:"role"`
	Prompt          string `json:"prompt"`
	AccountEmail    string `json:"account_email"`
	FailoverFrom    string `json:"failover_from,omitempty"`
	WatchdogRetried bool   `json:"watchdog_retried,omitempty"`
	ResolvedModel   string `json:"resolved_model"`
	Status          string `json:"status"` // "ok" or "error"
	LatencyMs       int64  `json:"latency_ms"`
	Tokens          int    `json:"tokens"`
	Output          string `json:"output"`
	Error           string `json:"error,omitempty"`
}

// SwarmRunSummary records a completed parallel Sub-Agent Swarm execution.
type SwarmRunSummary struct {
	ID                string           `json:"id"`
	Timestamp         string           `json:"timestamp"`
	Mode              string           `json:"mode"` // "seo-project", "coding-project", or "custom-tasks"
	MasterGoal        string           `json:"master_goal,omitempty"`
	ManagerModel      string           `json:"manager_model,omitempty"`
	ManagerAccount    string           `json:"manager_account,omitempty"`
	ManagerPlan       string           `json:"manager_plan,omitempty"`
	SharedLinkMatrix  string           `json:"shared_link_matrix,omitempty"`
	ContentModel      string           `json:"content_model,omitempty"`
	CodingModel       string           `json:"coding_model,omitempty"`
	IsNightMode       bool             `json:"is_night_mode"`
	Model             string           `json:"model"`
	TotalSubAgents    int              `json:"total_sub_agents"`
	SuccessCount      int              `json:"success_count"`
	WatchdogFailovers int              `json:"watchdog_failovers"`
	DistinctAccounts  int              `json:"distinct_accounts"`
	TotalWallTimeMs   int64            `json:"total_wall_time_ms"`
	TotalTokens       int              `json:"total_tokens"`
	EstimatedValueUSD float64          `json:"estimated_value_usd"`
	Results           []SubAgentResult `json:"results"`
}

// ScheduledAutomation represents an Orca-style scheduled background Sub-Agent campaign (e.g., Night 40%-50% Off SEO & Code runs).
type ScheduledAutomation struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	MasterGoal     string `json:"master_goal"`
	Mode           string `json:"mode"`            // "seo-and-code", "content-only", "coding-only"
	AgentCount     int    `json:"agent_count"`     // 1..50
	Model          string `json:"model,omitempty"`
	ScheduleWindow string `json:"schedule_window"` // "nightly_21h", "night-22h", "interval_6h", "interval_12h"
	Enabled        bool   `json:"enabled"`
	NextRunUnix    int64  `json:"next_run_unix"`
	LastRunAt      string `json:"last_run_at,omitempty"`
	LastRunID      string `json:"last_run_id,omitempty"`
	LastStatus     string `json:"last_status,omitempty"`
	TotalRuns      int    `json:"total_runs"`
	CreatedAt      string `json:"created_at"`
}

type subAgentVaultDisk struct {
	Runs        []SwarmRunSummary     `json:"runs"`
	Automations []ScheduledAutomation `json:"automations"`
	UpdatedAt   string                `json:"updated_at"`
}

// SubAgentVaultFilePath returns the path to the persistent AI vault JSON file.
func SubAgentVaultFilePath() string {
	base := GatewayConfigFilePath()
	return filepath.Join(filepath.Dir(base), "omni_subagent_vault.json")
}

func defaultSeededAutomations() []ScheduledAutomation {
	loc := time.FixedZone("ICT", 7*3600)
	vnNow := time.Now().In(loc)
	next22 := time.Date(vnNow.Year(), vnNow.Month(), vnNow.Day(), 22, 0, 0, 0, loc)
	if !next22.After(vnNow) {
		next22 = next22.Add(24 * time.Hour)
	}
	return []ScheduledAutomation{
		{
			ID:             "auto-night-seo-default",
			Name:           "🌙 Chiến Dịch SEO & Code Ban Đêm (⚡ 40% Off Qwen 3.8 Max + Antigravity Pro)",
			MasterGoal:     "Triển khai bộ bài viết Pillar & Cluster chuẩn SEO kèm Schema JSON-LD tối ưu chuyển đổi cho dự án trọng điểm",
			Mode:           "seo-and-code",
			AgentCount:     6,
			Model:          "alibaba/qwen3.8-max",
			ScheduleWindow: "night-22h",
			Enabled:        false,
			NextRunUnix:    next22.Unix(),
			LastStatus:     "Sẵn sàng (Bấm Bật hoặc Chạy Ngay)",
			CreatedAt:      time.Now().Format("15:04:05 02/01/2006"),
		},
	}
}

func syncEnabledAutomationsCountLocked() {
	var cnt int32
	for i := range scheduledAutomations {
		if scheduledAutomations[i].Enabled {
			cnt++
		}
	}
	enabledAutomationsCount.Store(cnt)
	vaultLoadedAtomic.Store(true)
}

func ensureSubAgentVaultLoadedLocked() {
	if vaultLoadedFromDisk {
		return
	}
	vaultLoadedFromDisk = true
	path := SubAgentVaultFilePath()
	data, err := os.ReadFile(path)
	if err == nil {
		var disk subAgentVaultDisk
		if json.Unmarshal(data, &disk) == nil {
			if len(disk.Runs) > 0 {
				recentSwarmRuns = disk.Runs
			}
			if len(disk.Automations) > 0 {
				scheduledAutomations = disk.Automations
			}
		}
	}
	if len(scheduledAutomations) == 0 {
		scheduledAutomations = defaultSeededAutomations()
	}
	syncEnabledAutomationsCountLocked()
}

func saveSubAgentVaultAsyncLocked() {
	syncEnabledAutomationsCountLocked()
	runsCopy := make([]SwarmRunSummary, len(recentSwarmRuns))
	copy(runsCopy, recentSwarmRuns)
	autoCopy := make([]ScheduledAutomation, len(scheduledAutomations))
	copy(autoCopy, scheduledAutomations)
	targetPath := SubAgentVaultFilePath()

	go func() {
		disk := subAgentVaultDisk{
			Runs:        runsCopy,
			Automations: autoCopy,
			UpdatedAt:   time.Now().UTC().Format(time.RFC3339),
		}
		if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err == nil {
			if raw, err := json.MarshalIndent(disk, "", "  "); err == nil {
				_ = os.WriteFile(targetPath, raw, 0600)
			}
		}
	}()
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

// isVietnamNightHours checks if current time in Vietnam (UTC+7) is between 19:00 and 07:00 (Alibaba Night Discount window).
func isVietnamNightHours() bool {
	vnTime := time.Now().UTC().Add(7 * time.Hour)
	hr := vnTime.Hour()
	return hr >= 19 || hr < 7
}

// selectSEOProjectManagerChain returns the ordered Manager model chain:
// - Night hours (or forceNight): alibaba/qwen3.8-max (⚡40% Off) -> antigravity/gemini-4-carbon-high -> antigravity/gemini-4-argon-high -> claude-code/claude-opus-5-5
// - Day hours: antigravity/gemini-4-carbon-high -> antigravity/gemini-4-argon-high -> claude-code/claude-opus-5-5 -> alibaba/qwen3.8-max
func selectSEOProjectManagerChain(preferredManager string, forceNight bool) ([]string, bool) {
	night := forceNight || isVietnamNightHours()
	pref := strings.TrimSpace(preferredManager)
	var chain []string
	if pref != "" && pref != "auto-manager" {
		chain = append(chain, pref)
	}
	if night {
		chain = append(chain,
			"alibaba/qwen3.8-max",
			"antigravity/gemini-4-carbon-high",
			"antigravity/gemini-4-argon-high",
			"claude-code/claude-opus-5-5",
		)
	} else {
		chain = append(chain,
			"antigravity/gemini-4-carbon-high",
			"antigravity/gemini-4-argon-high",
			"claude-code/claude-opus-5-5",
			"alibaba/qwen3.8-max",
		)
	}
	return chain, night
}

// listActiveAccountPool returns all enabled, non-cooldown Antigravity Pro accounts first for Worker Sub-Agents.
func listActiveAccountPool() []ConnectedAccount {
	saved := LoadSavedAuthConfig()
	now := time.Now().Unix()

	var agyReady []ConnectedAccount
	var agyCooldown []ConnectedAccount
	var otherReady []ConnectedAccount

	for _, a := range saved.Accounts {
		if a.Disabled {
			continue
		}
		prov := a.Provider
		if prov == "" {
			prov = ProviderAntigravity
		}
		if prov == ProviderAntigravity {
			if a.CooldownUntil <= now {
				agyReady = append(agyReady, a)
			} else {
				agyCooldown = append(agyCooldown, a)
			}
		} else if a.CooldownUntil <= now {
			otherReady = append(otherReady, a)
		}
	}

	if len(agyReady) > 0 {
		return agyReady
	}
	if len(otherReady) > 0 {
		return otherReady
	}
	return agyCooldown
}

// buildDefaultSubAgentTasks decomposes a master goal into N specialized parallel Sub-Agent tasks:
// - SEO Article & Content Writers use "antigravity/gemini-3.1-pro-high" (Gemini 3.1 Pro High)
// - Coding & Technical SEO Workers use "antigravity/gemini-3.8-flash-high" (Gemini 3.8 Flash High)
func buildDefaultSubAgentTasks(goal string, count int, mode string, managerPlan string) []SubAgentTask {
	if count <= 0 {
		count = 5
	}
	if count > 60 {
		count = 60
	}

	const (
		seoWriterModel = "antigravity/gemini-3.1-pro-high"
		codeFlashModel = "antigravity/gemini-3.8-flash-high"
	)

	planContext := ""
	if strings.TrimSpace(managerPlan) != "" {
		planContext = "\n\n[ĐỊNH HƯỚNG TỪ QUẢN LÝ DỰ ÁN SEO (MANAGER)]:\n" + managerPlan
	}

	seoAndCodeSpecialties := []struct {
		role   string
		model  string
		prefix string
	}{
		{
			"Sub-Agent #1 • Viết Bài Pillar Chuẩn SEO (Gemini 3.1 Pro High)",
			seoWriterModel,
			"Viết bài Pillar Content chuẩn SEO chuyên sâu (tối ưu Title, Meta Description, Heading H1-H3, từ khóa LSI, E-E-A-T) cho chủ đề/dự án: ",
		},
		{
			"Sub-Agent #2 • Viết Bài Cluster & Landing Page SEO (Gemini 3.1 Pro High)",
			seoWriterModel,
			"Viết bài vệ tinh Cluster Content & nội dung chuyển đổi Landing Page chuẩn SEO hấp dẫn người đọc cho chủ đề/dự án: ",
		},
		{
			"Sub-Agent #3 • Code Giao Diện & Tối Ưu Core Web Vitals (Gemini 3.8 Flash High)",
			codeFlashModel,
			"Viết code HTML/CSS/JS hoặc Component tối ưu tốc độ tải trang (Core Web Vitals), cấu trúc Semantic HTML chuẩn SEO cho: ",
		},
		{
			"Sub-Agent #4 • Code Schema JSON-LD & Technical SEO (Gemini 3.8 Flash High)",
			codeFlashModel,
			"Viết mã nguồn Schema Markup (JSON-LD Article, FAQPage, Product, Organization), OpenGraph, Sitemap và cấu hình Technical SEO cho: ",
		},
		{
			"Sub-Agent #5 • Viết Bài FAQ, So Sánh & Review Chuẩn SEO (Gemini 3.1 Pro High)",
			seoWriterModel,
			"Viết bài phân tích chuyên sâu, câu hỏi thường gặp (FAQ Rich Snippets) và đánh giá chi tiết chuẩn SEO cho: ",
		},
		{
			"Sub-Agent #6 • Code Script Tự Động Hóa & Internal Link (Gemini 3.8 Flash High)",
			codeFlashModel,
			"Viết code backend/script tự động hóa liên kết nội bộ (Internal Linking), API hoặc công cụ hỗ trợ SEO cho: ",
		},
		{
			"Sub-Agent #7 • Viết Bài Case Study & Hướng Dẫn Chi Tiết (Gemini 3.1 Pro High)",
			seoWriterModel,
			"Viết bài hướng dẫn từng bước (How-to Guide) và Case Study thực tế chuẩn SEO giữ chân người dùng (Time on Site) cho: ",
		},
		{
			"Sub-Agent #8 • Code Audit, Kiểm Tra On-Page & Bảo Mật (Gemini 3.8 Flash High)",
			codeFlashModel,
			"Viết code kiểm tra On-Page SEO tự động, tối ưu thẻ Meta/Canonical/Robots và bảo mật hệ thống cho: ",
		},
	}

	tasks := make([]SubAgentTask, 0, count)
	for i := 0; i < count; i++ {
		var role, prompt, workerModel string
		if mode == "coding-only" {
			workerModel = codeFlashModel
			role = fmt.Sprintf("Sub-Agent #%d • Code Module Worker (Gemini 3.8 Flash High)", i+1)
			prompt = fmt.Sprintf("[Module Code #%d/%d] Viết code hoàn chỉnh, tối ưu hiệu năng cho yêu cầu: %s%s", i+1, count, goal, planContext)
		} else if mode == "content-only" {
			workerModel = seoWriterModel
			role = fmt.Sprintf("Sub-Agent #%d • Viết Bài Chuẩn SEO #%d (Gemini 3.1 Pro High)", i+1, i+1)
			prompt = fmt.Sprintf("[Bài viết chuẩn SEO #%d/%d] Viết bài chuẩn SEO chi tiết, độc đáo (Unique 100%%), đầy đủ H1-H3 cho chủ đề: %s%s", i+1, count, goal, planContext)
		} else if i < len(seoAndCodeSpecialties) {
			spec := seoAndCodeSpecialties[i]
			role = spec.role
			workerModel = spec.model
			prompt = spec.prefix + goal + planContext
		} else {
			// Alternate between Gemini 3.1 Pro High (Viết bài SEO) and Gemini 3.8 Flash High (Viết Code)
			if i%2 == 0 {
				workerModel = seoWriterModel
				role = fmt.Sprintf("Sub-Agent #%d • Viết Bài SEO Chuyên Sâu (Gemini 3.1 Pro High)", i+1)
				prompt = fmt.Sprintf("[Bài viết SEO mở rộng #%d/%d] Viết bài chuẩn SEO chuyên sâu triển khai khía cạnh số %d của dự án: %s%s", i+1, count, i+1, goal, planContext)
			} else {
				workerModel = codeFlashModel
				role = fmt.Sprintf("Sub-Agent #%d • Code & Kỹ Thuật SEO (Gemini 3.8 Flash High)", i+1)
				prompt = fmt.Sprintf("[Module Code & Kỹ thuật #%d/%d] Viết code & cấu hình kỹ thuật số %d cho dự án: %s%s", i+1, count, i+1, goal, planContext)
			}
		}
		tasks = append(tasks, SubAgentTask{
			Index:  i + 1,
			Role:   role,
			Prompt: prompt,
			Model:  workerModel,
		})
	}
	return tasks
}

// ExecuteSubAgentSwarm runs a 2-tier Hierarchical Swarm:
//   - Tier 1 (SEO & Project Manager): Uses Gemini 4 Argon High / Claude Opus 5.5 (Day) or Alibaba Qwen 3.8 Max ⚡40% Off (Night) to create the Master SEO & Technical Plan.
//   - Tier 2 (50 Antigravity Pro Workers): Runs N Sub-Agents 100% simultaneously in parallel Goroutines using Gemini 3.1 Pro High (SEO Article Writing) & Gemini 3.8 Flash High (Coding).
func (s *Server) ExecuteSubAgentSwarm(
	ctx context.Context,
	mode string,
	masterGoal string,
	agentCount int,
	defaultModel string,
	customTasks []SubAgentTask,
) SwarmRunSummary {
	startWall := time.Now()
	forceNight := strings.Contains(strings.ToLower(mode), "night") || strings.Contains(strings.ToLower(defaultModel), "qwen3.8-max")
	managerChain, isNight := selectSEOProjectManagerChain(defaultModel, forceNight)

	// STEP 0 (Tier 1): Lead SEO & Project Manager creates the Master Strategy & Shared Internal Link Matrix (Orca Inter-Agent Mailbox)
	var managerModelUsed string
	var managerAccountUsed string
	var managerPlanText string
	var sharedLinkMatrix string
	var managerTokens int

	if strings.TrimSpace(masterGoal) != "" && len(customTasks) == 0 {
		mgrSys := "Bạn là Quản Lý Dự Án SEO & Kiến Trúc Sư Trưởng (Lead SEO & Technical Project Manager). Hãy lập: (1) Dàn ý chiến lược SEO tổng thể (Từ khóa chính/LSI, Search Intent, định hướng cho đội Gemini 3.1 Pro High viết bài & đội Gemini 3.8 Flash High viết code), và (2) Sơ Đồ Liên Kết Nội Bộ Chéo (SHARED INTERNAL LINK MATRIX: danh sách 5-8 cặp Anchor Text ➔ URL Slug gợi ý để tất cả các bài viết của các Sub-Agents tự động chèn link chéo cho nhau không trùng lặp). Trình bày súc tích trong khoảng 180-280 từ."
		rawMgrSys, _ := json.Marshal(mgrSys)
		rawMgrUser, _ := json.Marshal(masterGoal)

		for _, candMgrModel := range managerChain {
			mgrResolved := s.resolver.Resolve(candMgrModel, s.cfg.FallbackModels)
			mgrReq := OpenAIChatRequest{
				Model: candMgrModel,
				Messages: []OpenAIChatMessage{
					{Role: "system", Content: rawMgrSys},
					{Role: "user", Content: rawMgrUser},
				},
				MaxTokens: 700,
				Stream:    false,
			}
			mgrCtx, cancelMgr := context.WithTimeout(ctx, 45*time.Second)
			recMgr := newResponseRecorder()
			errMgr := s.upstream.HandleChatCompletion(mgrCtx, recMgr, mgrReq, mgrResolved, "")
			cancelMgr()

			if errMgr == nil && recMgr.statusCode >= 200 && recMgr.statusCode < 300 {
				txt, reas, toks := extractAssistantTextFromRecorder(recMgr, candMgrModel)
				if strings.TrimSpace(txt) == "" {
					txt = reas
				}
				if strings.TrimSpace(txt) != "" {
					managerModelUsed = candMgrModel
					managerAccountUsed = recMgr.Header().Get("X-Omni-Gateway-Account")
					managerPlanText = txt
					sharedLinkMatrix = fmt.Sprintf("Hộp thư Liên Kết Nội Bộ Chung (Inter-Agent Mailbox • %d Workers): Sử dụng thống nhất hệ thống Slug & Anchor Text trong bản kế hoạch của Quản Lý Dự Án (%s) để liên kết chéo giữa bài Pillar, bài Cluster và Schema JSON-LD.", agentCount, candMgrModel)
					managerTokens = toks
					break
				}
			}
		}
	}
	if managerModelUsed == "" && len(managerChain) > 0 {
		managerModelUsed = managerChain[0]
	}

	// STEP 1 (Tier 2): Build N Worker Sub-Agent Tasks for the 50 Antigravity Pro Pool
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
			// Auto-assign Gemini 3.8 Flash High for code tasks and Gemini 3.1 Pro High for SEO writing tasks
			if strings.TrimSpace(t.Model) == "" || t.Model == "auto-worker" {
				lowerP := strings.ToLower(t.Prompt)
				if strings.Contains(lowerP, "code") || strings.Contains(lowerP, "html") || strings.Contains(lowerP, "schema") || strings.Contains(lowerP, "api") || strings.Contains(lowerP, "script") {
					t.Model = "antigravity/gemini-3.8-flash-high"
					t.Role += " • Code (Gemini 3.8 Flash High)"
				} else {
					t.Model = "antigravity/gemini-3.1-pro-high"
					t.Role += " • Viết Bài SEO (Gemini 3.1 Pro High)"
				}
			}
			tasks = append(tasks, t)
		}
		if mode == "" {
			mode = "custom-tasks"
		}
	} else {
		tasks = buildDefaultSubAgentTasks(masterGoal, agentCount, mode, managerPlanText)
		if mode == "" {
			mode = "seo-and-code"
		}
	}

	pool := listActiveAccountPool()
	results := make([]SubAgentResult, len(tasks))
	var wg sync.WaitGroup
	var watchdogFailoversCount int32

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

			tStart := time.Now()
			modelID := strings.TrimSpace(t.Model)
			if modelID == "" {
				modelID = defaultModel
			}
			resolved := s.resolver.Resolve(modelID, s.cfg.FallbackModels)

			sysText := fmt.Sprintf("Bạn là %s trong Đàn Sub-Agents Song Song của OmniRoute-Go. Hãy trả lời súc tích, chuyên sâu, đi thẳng vào nội dung bài viết chuẩn SEO (kèm chèn Internal Link theo Sơ đồ chung) hoặc giải pháp code trọng tâm.", t.Role)
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

			runAttempt := func(targetAccount string, timeout time.Duration) (*responseRecorder, error) {
				if targetAccount != "" {
					AcquireAccountInFlightWithRole(targetAccount, t.Role)
					defer ReleaseAccountInFlight(targetAccount)
				}
				attCtx, cancel := context.WithTimeout(ctx, timeout)
				defer cancel()
				if targetAccount != "" {
					attCtx = WithPinnedAccount(attCtx, targetAccount)
				}
				r := newResponseRecorder()
				err := s.upstream.HandleChatCompletion(attCtx, r, chatReq, resolved, "")
				return r, err
			}

			// Primary Attempt with Orca Hang Watchdog (55s stall fence)
			rec, err := runAttempt(assignedEmail, 55*time.Second)
			watchdogRetried := false
			failoverFrom := ""

			// If primary account stalled, timed out, or returned an error, Orca Watchdog fences it and fails over to another account in the pool!
			if (err != nil || rec.statusCode >= 400) && len(pool) > 1 && ctx.Err() == nil {
				altEmail := pool[(idx+1)%len(pool)].Email
				if !strings.EqualFold(altEmail, assignedEmail) {
					watchdogRetried = true
					failoverFrom = assignedEmail
					atomic.AddInt32(&watchdogFailoversCount, 1)
					rec2, err2 := runAttempt(altEmail, 65*time.Second)
					if err2 == nil && rec2.statusCode < 400 {
						rec = rec2
						err = nil
						assignedEmail = altEmail
					}
				}
			}

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
					Index:           t.Index,
					Role:            t.Role,
					Prompt:          t.Prompt,
					AccountEmail:    actualAcc,
					FailoverFrom:    failoverFrom,
					WatchdogRetried: watchdogRetried,
					ResolvedModel:   actualModel,
					Status:          "error",
					LatencyMs:       lat,
					Error:           errMsg,
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
				Index:           t.Index,
				Role:            t.Role,
				Prompt:          t.Prompt,
				AccountEmail:    actualAcc,
				FailoverFrom:    failoverFrom,
				WatchdogRetried: watchdogRetried,
				ResolvedModel:   actualModel,
				Status:          "ok",
				LatencyMs:       lat,
				Tokens:          toks,
				Output:          txt,
			}
		}(i, task, pinnedEmail)
	}

	wg.Wait()

	wallMs := time.Since(startWall).Milliseconds()
	succCount := 0
	totalToks := managerTokens
	estValueUSD := EstimateModelCostUSD(managerModelUsed, int64(managerTokens))
	uniqueAccs := make(map[string]bool)
	if managerAccountUsed != "" {
		uniqueAccs[strings.ToLower(managerAccountUsed)] = true
	}
	for _, r := range results {
		if r.Status == "ok" {
			succCount++
		}
		totalToks += r.Tokens
		estValueUSD += EstimateModelCostUSD(r.ResolvedModel, int64(r.Tokens))
		if r.AccountEmail != "" {
			uniqueAccs[strings.ToLower(r.AccountEmail)] = true
		}
	}

	summary := SwarmRunSummary{
		ID:                fmt.Sprintf("swarm-%d", time.Now().Unix()),
		Timestamp:         time.Now().Format("15:04:05 02/01/2006"),
		Mode:              mode,
		MasterGoal:        masterGoal,
		Model:             defaultModel,
		ManagerModel:      managerModelUsed,
		ManagerAccount:    managerAccountUsed,
		ManagerPlan:       managerPlanText,
		SharedLinkMatrix:  sharedLinkMatrix,
		ContentModel:      "antigravity/gemini-3.1-pro-high",
		CodingModel:       "antigravity/gemini-3.8-flash-high",
		IsNightMode:       isNight,
		TotalSubAgents:    len(results),
		SuccessCount:      succCount,
		WatchdogFailovers: int(atomic.LoadInt32(&watchdogFailoversCount)),
		DistinctAccounts:  len(uniqueAccs),
		TotalWallTimeMs:   wallMs,
		TotalTokens:       totalToks,
		EstimatedValueUSD: math.Round(estValueUSD*1000) / 1000,
		Results:           results,
	}

	swarmHistoryMu.Lock()
	ensureSubAgentVaultLoadedLocked()
	recentSwarmRuns = append([]SwarmRunSummary{summary}, recentSwarmRuns...)
	if len(recentSwarmRuns) > 25 {
		recentSwarmRuns = recentSwarmRuns[:25]
	}
	saveSubAgentVaultAsyncLocked()
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

// FormatSwarmRunMarkdown converts a SwarmRunSummary into a clean Markdown document for 1-click export.
func FormatSwarmRunMarkdown(run SwarmRunSummary) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# 🐝 Báo Cáo Chiến Dịch 50-Sub-Agent Swarm (`%s`)\n\n", run.ID))
	sb.WriteString(fmt.Sprintf("- **Mục tiêu tổng thể**: %s\n", run.MasterGoal))
	sb.WriteString(fmt.Sprintf("- **Model Quản lý (Tier 1 Manager)**: `%s` (Tài khoản: `%s`)\n", run.ManagerModel, run.ManagerAccount))
	sb.WriteString(fmt.Sprintf("- **Thời điểm chạy**: `%s` (Tổng thời gian: **%d ms**)\n", run.Timestamp, run.TotalWallTimeMs))
	failedCount := run.TotalSubAgents - run.SuccessCount
	sb.WriteString(fmt.Sprintf("- **Tiến độ Sub-Agents**: **%d/%d thành công** (Thất bại: %d | Watchdog Failover: %d)\n", run.SuccessCount, run.TotalSubAgents, failedCount, run.WatchdogFailovers))
	sb.WriteString(fmt.Sprintf("- **Tài khoản tham gia**: **%d tài khoản**\n", run.DistinctAccounts))
	sb.WriteString(fmt.Sprintf("- **Tổng Tokens**: **%d tokens** | **Giá trị API ước tính**: **$%.4f USD**\n\n", run.TotalTokens, run.EstimatedValueUSD))

	if strings.TrimSpace(run.SharedLinkMatrix) != "" {
		sb.WriteString("## 🔗 Ma Trận Liên Kết Nội Bộ (Shared Inter-Agent Link Matrix)\n\n")
		sb.WriteString("```text\n" + run.SharedLinkMatrix + "\n```\n\n")
	}

	sb.WriteString("---\n\n## 📄 Chi Tiết Kết Quả Từng Sub-Agent\n\n")
	for _, res := range run.Results {
		statusBadge := "✅ THÀNH CÔNG"
		if res.Status != "ok" {
			statusBadge = "❌ LỖI: " + res.Error
		}
		sb.WriteString(fmt.Sprintf("### #%d. %s\n\n", res.Index, res.Role))
		sb.WriteString(fmt.Sprintf("- **Trạng thái**: %s\n", statusBadge))
		sb.WriteString(fmt.Sprintf("- **Model**: `%s` | **Tài khoản**: `%s`", res.ResolvedModel, res.AccountEmail))
		if res.WatchdogRetried && res.FailoverFrom != "" {
			sb.WriteString(fmt.Sprintf(" *(⚡ Watchdog Failover từ `%s`)*", res.FailoverFrom))
		}
		sb.WriteString("\n")
		sb.WriteString(fmt.Sprintf("- **Độ trễ**: `%d ms` | **Tokens**: `%d`\n\n", res.LatencyMs, res.Tokens))
		if strings.TrimSpace(res.Prompt) != "" {
			sb.WriteString("**Nhiệm vụ:**\n```text\n" + res.Prompt + "\n```\n\n")
		}
		content := res.Output
		if content == "" {
			content = res.Error
		}
		if strings.TrimSpace(content) != "" {
			sb.WriteString("**Kết quả:**\n\n" + content + "\n\n")
		}
		sb.WriteString("---\n\n")
	}
	return sb.String()
}

var (
	automationSchedulerOnce     sync.Once
	activeAutomationServer      atomic.Pointer[Server]
	automationSchedulerGoroRuns atomic.Int64
)

// EnabledAutomationsCountFast returns the lock-free count of enabled scheduled automations once loaded.
func EnabledAutomationsCountFast() int32 {
	if !vaultLoadedAtomic.Load() {
		swarmHistoryMu.Lock()
		ensureSubAgentVaultLoadedLocked()
		swarmHistoryMu.Unlock()
	}
	return enabledAutomationsCount.Load()
}

func runAutomationSchedulerTick(now time.Time) {
	// Fast-skip in < 2ns without locking swarmHistoryMu when 0 enabled automations exist
	if vaultLoadedAtomic.Load() && enabledAutomationsCount.Load() == 0 {
		return
	}

	srv := activeAutomationServer.Load()
	if srv == nil {
		return
	}

	loc := time.FixedZone("ICT", 7*3600)
	vnNow := now.In(loc)
	var dueList []ScheduledAutomation

	swarmHistoryMu.Lock()
	ensureSubAgentVaultLoadedLocked()
	if enabledAutomationsCount.Load() == 0 {
		swarmHistoryMu.Unlock()
		return
	}
	for i := range scheduledAutomations {
		auto := &scheduledAutomations[i]
		if !auto.Enabled {
			continue
		}
		isDue := false
		if auto.NextRunUnix > 0 && vnNow.Unix() >= auto.NextRunUnix {
			isDue = true
		} else if (auto.ScheduleWindow == "nightly_21h" || auto.ScheduleWindow == "night-22h") && vnNow.Hour() >= 21 {
			if auto.NextRunUnix == 0 {
				isDue = true
			}
		}
		if isDue {
			auto.LastRunAt = vnNow.Format(time.RFC3339)
			switch auto.ScheduleWindow {
			case "interval_6h":
				auto.NextRunUnix = vnNow.Add(6 * time.Hour).Unix()
			case "interval_12h":
				auto.NextRunUnix = vnNow.Add(12 * time.Hour).Unix()
			default: // nightly_21h / night-22h
				targetHour := 21
				if auto.ScheduleWindow == "night-22h" {
					targetHour = 22
				}
				next := time.Date(vnNow.Year(), vnNow.Month(), vnNow.Day(), targetHour, 0, 0, 0, loc)
				if !next.After(vnNow) {
					next = next.Add(24 * time.Hour)
				}
				auto.NextRunUnix = next.Unix()
			}
			dueList = append(dueList, *auto)
		}
	}
	if len(dueList) > 0 {
		saveSubAgentVaultAsyncLocked()
	}
	swarmHistoryMu.Unlock()

	for _, auto := range dueList {
		go func(a ScheduledAutomation) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			summary := srv.ExecuteSubAgentSwarm(ctx, "auto-decompose", a.MasterGoal, a.AgentCount, a.Model, nil)
			swarmHistoryMu.Lock()
			for i := range scheduledAutomations {
				if scheduledAutomations[i].ID == a.ID {
					scheduledAutomations[i].LastRunID = summary.ID
					scheduledAutomations[i].TotalRuns++
					scheduledAutomations[i].LastStatus = fmt.Sprintf("Thành công %d/%d agents", summary.SuccessCount, summary.TotalSubAgents)
					break
				}
			}
			saveSubAgentVaultAsyncLocked()
			swarmHistoryMu.Unlock()
		}(auto)
	}
}

// StartAutomationScheduler ensures a single sync.Once background ticker checks ScheduledAutomation items
// every 60 seconds and fast-skips in O(1) when 0 enabled automations exist.
func (s *Server) StartAutomationScheduler() {
	if s != nil {
		activeAutomationServer.Store(s)
	}
	automationSchedulerOnce.Do(func() {
		automationSchedulerGoroRuns.Add(1)
		go func() {
			ticker := time.NewTicker(60 * time.Second)
			defer ticker.Stop()
			for now := range ticker.C {
				runAutomationSchedulerTick(now)
			}
		}()
	})
}

// handleSubAgents handles GET and POST /v1/subagents for the 50-Account Parallel Sub-Agent Swarm Engine.
func (s *Server) handleSubAgents(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		// Check if user requested 1-click export of a Swarm run from the AI Vault
		if exportID := strings.TrimSpace(r.URL.Query().Get("export")); exportID != "" {
			format := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format")))
			if format == "" {
				format = "md"
			}
			swarmHistoryMu.Lock()
			ensureSubAgentVaultLoadedLocked()
			var target *SwarmRunSummary
			for i := range recentSwarmRuns {
				if exportID == "latest" || recentSwarmRuns[i].ID == exportID {
					cp := recentSwarmRuns[i]
					target = &cp
					break
				}
			}
			swarmHistoryMu.Unlock()

			if target == nil {
				writeOpenAIError(w, http.StatusNotFound, "không tìm thấy chiến dịch Sub-Agent để xuất: "+exportID, "not_found")
				return
			}
			if format == "json" {
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s.json\"", target.ID))
				w.WriteHeader(http.StatusOK)
				enc := json.NewEncoder(w)
				enc.SetIndent("", "  ")
				_ = enc.Encode(target)
				return
			}
			md := FormatSwarmRunMarkdown(*target)
			w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
			w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s.md\"", target.ID))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(md))
			return
		}

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

		swarmHistoryMu.Lock()
		ensureSubAgentVaultLoadedLocked()
		history := make([]SwarmRunSummary, len(recentSwarmRuns))
		copy(history, recentSwarmRuns)
		automations := make([]ScheduledAutomation, len(scheduledAutomations))
		copy(automations, scheduledAutomations)
		swarmHistoryMu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":               "ok",
			"total_accounts":       len(saved.Accounts),
			"antigravity_accounts": agyCount,
			"ready_accounts":       readyCount,
			"max_parallel_target":  50,
			"is_night_mode":        isVietnamNightHours(),
			"day_manager_models":   []string{"antigravity/gemini-4-carbon-high", "antigravity/gemini-4-argon-high", "claude-code/claude-opus-5-5"},
			"night_manager_model":  "alibaba/qwen3.8-max",
			"seo_writer_model":     "antigravity/gemini-3.1-pro-high",
			"coding_worker_model":  "antigravity/gemini-3.8-flash-high",
			"active_sub_agents":    atomic.LoadInt64(&activeSubAgentsNow),
			"total_sub_agents_run": atomic.LoadInt64(&totalSubAgentsRun),
			"account_slots":        slots,
			"recent_swarms":        history,
			"automations":          automations,
			"vault_file":           SubAgentVaultFilePath(),
		})

	case http.MethodPost:
		var req struct {
			Action       string         `json:"action"` // "swarm", "bulk_import", "create_automation", "toggle_automation", "run_automation", "delete_automation"
			Mode         string         `json:"mode"`   // "auto-decompose" or "custom-tasks"
			MasterGoal   string         `json:"master_goal"`
			AgentCount   int            `json:"agent_count"`
			Model        string         `json:"model"`
			CustomTasks  []SubAgentTask `json:"tasks"`
			BulkInput    string         `json:"bulk_input"`
			AutomationID string         `json:"automation_id"`
			Name         string         `json:"name"`
			ScheduleType string         `json:"schedule_type"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 512*1024)).Decode(&req); err != nil {
			writeOpenAIError(w, http.StatusBadRequest, "invalid JSON payload: "+err.Error(), "invalid_request_error")
			return
		}

		switch req.Action {
		case "bulk_import":
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

		case "create_automation":
			goal := strings.TrimSpace(req.MasterGoal)
			if goal == "" {
				writeOpenAIError(w, http.StatusBadRequest, "master_goal không được để trống", "invalid_request_error")
				return
			}
			name := strings.TrimSpace(req.Name)
			if name == "" {
				name = "Chiến dịch SEO/Code Tự Động Ban Đêm (40%-50% Off)"
			}
			sched := strings.TrimSpace(req.ScheduleType)
			if sched == "" {
				sched = "nightly_21h"
			}
			cnt := req.AgentCount
			if cnt <= 0 {
				cnt = 15
			}
			loc := time.FixedZone("ICT", 7*3600)
			vnNow := time.Now().In(loc)
			nextRun := vnNow.Add(6 * time.Hour).Unix()
			if sched == "nightly_21h" || sched == "night-22h" {
				targetHour := 21
				if sched == "night-22h" {
					targetHour = 22
				}
				next := time.Date(vnNow.Year(), vnNow.Month(), vnNow.Day(), targetHour, 0, 0, 0, loc)
				if !next.After(vnNow) {
					next = next.Add(24 * time.Hour)
				}
				nextRun = next.Unix()
			} else if sched == "interval_12h" {
				nextRun = vnNow.Add(12 * time.Hour).Unix()
			}
			auto := ScheduledAutomation{
				ID:             fmt.Sprintf("auto-%d", time.Now().UnixMilli()),
				Name:           name,
				MasterGoal:     goal,
				Mode:           "seo-and-code",
				AgentCount:     cnt,
				Model:          strings.TrimSpace(req.Model),
				ScheduleWindow: sched,
				Enabled:        true,
				NextRunUnix:    nextRun,
				LastStatus:     "Mới tạo - chờ đến lịch chạy",
				CreatedAt:      vnNow.Format("15:04:05 02/01/2006"),
			}
			swarmHistoryMu.Lock()
			ensureSubAgentVaultLoadedLocked()
			scheduledAutomations = append([]ScheduledAutomation{auto}, scheduledAutomations...)
			saveSubAgentVaultAsyncLocked()
			swarmHistoryMu.Unlock()

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status":     "ok",
				"automation": auto,
			})
			return

		case "toggle_automation":
			swarmHistoryMu.Lock()
			ensureSubAgentVaultLoadedLocked()
			var updated *ScheduledAutomation
			for i := range scheduledAutomations {
				if scheduledAutomations[i].ID == req.AutomationID {
					scheduledAutomations[i].Enabled = !scheduledAutomations[i].Enabled
					cp := scheduledAutomations[i]
					updated = &cp
					break
				}
			}
			saveSubAgentVaultAsyncLocked()
			swarmHistoryMu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status":     "ok",
				"automation": updated,
			})
			return

		case "delete_automation":
			swarmHistoryMu.Lock()
			ensureSubAgentVaultLoadedLocked()
			filtered := make([]ScheduledAutomation, 0, len(scheduledAutomations))
			for _, a := range scheduledAutomations {
				if a.ID != req.AutomationID {
					filtered = append(filtered, a)
				}
			}
			scheduledAutomations = filtered
			saveSubAgentVaultAsyncLocked()
			swarmHistoryMu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "deleted",
				"id":     req.AutomationID,
			})
			return

		case "run_automation":
			swarmHistoryMu.Lock()
			ensureSubAgentVaultLoadedLocked()
			var target *ScheduledAutomation
			for i := range scheduledAutomations {
				if scheduledAutomations[i].ID == req.AutomationID {
					cp := scheduledAutomations[i]
					target = &cp
					break
				}
			}
			swarmHistoryMu.Unlock()
			if target == nil {
				writeOpenAIError(w, http.StatusNotFound, "không tìm thấy lịch tự động hóa: "+req.AutomationID, "not_found")
				return
			}
			summary := s.ExecuteSubAgentSwarm(r.Context(), "auto-decompose", target.MasterGoal, target.AgentCount, target.Model, nil)
			swarmHistoryMu.Lock()
			for i := range scheduledAutomations {
				if scheduledAutomations[i].ID == target.ID {
					scheduledAutomations[i].LastRunAt = time.Now().UTC().Format(time.RFC3339)
					scheduledAutomations[i].LastRunID = summary.ID
					scheduledAutomations[i].TotalRuns++
					scheduledAutomations[i].LastStatus = fmt.Sprintf("Thành công %d/%d agents", summary.SuccessCount, summary.TotalSubAgents)
					break
				}
			}
			saveSubAgentVaultAsyncLocked()
			swarmHistoryMu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status":  "ok",
				"summary": summary,
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

