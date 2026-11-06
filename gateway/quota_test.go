package gateway

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMaskEmail(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		{"phongsaigon11@gmail.com", "pho***@gmail.com"},
		{"thuctapsinh316@gmail.com", "thu***@gmail.com"},
		{"a@b.com", "a***@b.com"},
		{"alibaba-coding", "ali***ng"},
	}

	for _, c := range cases {
		got := MaskEmail(c.input)
		if got != c.expected {
			t.Errorf("MaskEmail(%q) = %q; want %q", c.input, got, c.expected)
		}
	}
}

func TestFormatResetDuration(t *testing.T) {
	fallback := "fallback"
	if got := formatResetDuration("", fallback); got != fallback {
		t.Errorf("expected fallback, got %q", got)
	}

	future2h := time.Now().UTC().Add(2*time.Hour + 15*time.Minute).Format(time.RFC3339)
	res2h := formatResetDuration(future2h, fallback)
	if !strings.Contains(res2h, "Đặt lại sau 2h") {
		t.Errorf("expected 2h reset format, got %q", res2h)
	}

	future5d := time.Now().UTC().Add(5*24*time.Hour + 18*time.Hour).Format(time.RFC3339)
	res5d := formatResetDuration(future5d, fallback)
	if !strings.Contains(res5d, "Đặt lại sau 5d") {
		t.Errorf("expected 5d reset format, got %q", res5d)
	}

	past := time.Now().UTC().Add(-10 * time.Minute).Format(time.RFC3339)
	resPast := formatResetDuration(past, fallback)
	if resPast != "⏱️ Đặt lại ngay" {
		t.Errorf("expected reset immediately, got %q", resPast)
	}
}

func TestInspectAlibabaAccount(t *testing.T) {
	ctx := context.Background()
	acc := ConnectedAccount{
		ID:       "ali-test",
		Provider: ProviderAlibaba,
		Label:    "alibaba-coding",
	}
	detail := AccountQuotaDetail{
		ID:       acc.ID,
		Provider: acc.Provider,
	}

	inspectAlibabaAccount(ctx, &acc, &detail, "reset")

	if detail.Tier != "Token Plan" {
		t.Errorf("expected Token Plan tier, got %q", detail.Tier)
	}
	if len(detail.ModelQuotas) == 0 {
		t.Fatalf("expected model quotas, got 0")
	}

	// Default should be 72.24% used -> 28% left
	first := detail.ModelQuotas[0]
	if first.RemainingPercent != 28 {
		t.Errorf("expected 28%% remaining, got %d%%", first.RemainingPercent)
	}
	if !strings.Contains(first.DisplayName, "72.24%") {
		t.Errorf("expected display name to contain 72.24%%, got %q", first.DisplayName)
	}
}

func TestInspectClaudeCodeAccount(t *testing.T) {
	ctx := context.Background()
	acc := ConnectedAccount{
		ID:       "claude-test",
		Provider: ProviderClaudeCode,
		Email:    "claude@anthropic.com",
	}
	detail := AccountQuotaDetail{
		ID:       acc.ID,
		Provider: acc.Provider,
	}

	inspectClaudeCodeAccount(ctx, &acc, &detail, "daily-reset")

	if detail.Tier != "Pro/Max" {
		t.Errorf("expected Pro/Max tier, got %q", detail.Tier)
	}
	if len(detail.ModelQuotas) < 3 {
		t.Fatalf("expected at least 3 models, got %d", len(detail.ModelQuotas))
	}
	if detail.ModelQuotas[0].RemainingPercent != 100 {
		t.Errorf("expected 100%%, got %d", detail.ModelQuotas[0].RemainingPercent)
	}
}

func TestIncrementVectorRAGHitsNoDeadlockAndFastQuotaSummary(t *testing.T) {
	tmpCfg := filepath.Join(t.TempDir(), "gateway_config.json")
	t.Setenv("GATEWAY_CONFIG_FILE", tmpCfg)

	// Ensure incrementVectorRAGHits never deadlocks even when VectorDBConfig was nil on disk
	done := make(chan struct{})
	go func() {
		incrementVectorRAGHits()
		vc := GetVectorDBConfig()
		if vc.TotalRAGHits < 1 {
			t.Errorf("expected TotalRAGHits >= 1, got %d", vc.TotalRAGHits)
		}
		summary := GetQuotaSummary(context.Background(), false)
		if summary == nil {
			t.Errorf("expected non-nil QuotaSummary")
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("incrementVectorRAGHits or GetQuotaSummary deadlocked!")
	}
}

func TestOrcaPricingAndMythosModels(t *testing.T) {
	mythosPrice := EstimateModelCostUSD("claude-code/claude-mythos-5-1", 2000)
	if mythosPrice <= 0 {
		t.Errorf("expected positive price for mythos, got %f", mythosPrice)
	}

	opusPrice := EstimateModelCostUSD("claude-code/claude-opus-5-5", 2000)
	if opusPrice <= 0 || mythosPrice <= opusPrice {
		t.Errorf("expected mythos price > opus price, got mythos=%f opus=%f", mythosPrice, opusPrice)
	}

	accValAgyUltra := EstimateAccountValueUSD(ConnectedAccount{
		Provider:    ProviderAntigravity,
		Email:       "ultra@gmail.com",
		TotalTokens: 50000,
	})
	if accValAgyUltra <= 0.4 {
		t.Errorf("expected Ultra tier value > 0.4, got %f", accValAgyUltra)
	}

	accValClaude := EstimateAccountValueUSD(ConnectedAccount{
		Provider:    ProviderClaudeCode,
		TotalTokens: 20000,
	})
	if accValClaude <= 0.15 {
		t.Errorf("expected Claude Code account value > 0.15, got %f", accValClaude)
	}
}

func TestFormatSwarmRunMarkdownAndVault(t *testing.T) {
	run := SwarmRunSummary{
		ID:                "swarm-test-123",
		Timestamp:         "15:30:00 07/11/2026",
		MasterGoal:        "Triển khai 50 bài viết Pillar & Cluster chuẩn SEO E-E-A-T",
		ManagerModel:      "antigravity/gemini-4-argon-high",
		ManagerAccount:    "lead@bucloud.vip",
		TotalSubAgents:    2,
		SuccessCount:      2,
		WatchdogFailovers: 1,
		DistinctAccounts:  2,
		TotalWallTimeMs:   3500,
		TotalTokens:       45000,
		EstimatedValueUSD: 0.158,
		SharedLinkMatrix:  "1. /vps-cloud -> /cloud-hosting\n2. /cloud-hosting -> /vps-cloud",
		Results: []SubAgentResult{
			{
				Index:           1,
				Role:            "Sub-Agent #1 (Pillar Writer)",
				ResolvedModel:   "antigravity/gemini-3.1-pro-high",
				AccountEmail:    "acc1@gmail.com",
				Status:          "ok",
				LatencyMs:       1800,
				Tokens:          22000,
				Output:          "# Bài viết Pillar VPS Cloud tốc độ cao",
				WatchdogRetried: true,
				FailoverFrom:    "slow_acc@gmail.com",
			},
			{
				Index:         2,
				Role:          "Sub-Agent #2 (Schema & Code)",
				ResolvedModel: "antigravity/gemini-3.8-flash-high",
				AccountEmail:  "acc2@gmail.com",
				Status:        "ok",
				LatencyMs:     1200,
				Tokens:        23000,
				Output:        "```json\n{\n  \"@context\": \"https://schema.org\"\n}\n```",
			},
		},
	}

	md := FormatSwarmRunMarkdown(run)
	if !strings.Contains(md, "Báo Cáo Chiến Dịch 50-Sub-Agent Swarm") {
		t.Errorf("expected title in markdown export")
	}
	if !strings.Contains(md, "Ma Trận Liên Kết Nội Bộ") {
		t.Errorf("expected link matrix section")
	}
	if !strings.Contains(md, "Watchdog Failover") {
		t.Errorf("expected watchdog failover badge")
	}

	vaultPath := SubAgentVaultFilePath()
	if !strings.Contains(vaultPath, "omni_subagent_vault.json") {
		t.Errorf("expected vault file name omni_subagent_vault.json, got %q", vaultPath)
	}
}


