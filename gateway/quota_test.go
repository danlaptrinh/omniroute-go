package gateway

import (
	"context"
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
	if len(detail.ModelQuotas) != 3 {
		t.Fatalf("expected 3 models, got %d", len(detail.ModelQuotas))
	}
	if detail.ModelQuotas[0].RemainingPercent != 100 {
		t.Errorf("expected 100%%, got %d", detail.ModelQuotas[0].RemainingPercent)
	}
}

