package gateway

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestCarbonArgonExclusivelyUltraAccounts(t *testing.T) {
	resetAuthCacheForTest(t)
	ClearAccountModelRateLimits("")

	cfg := SavedGatewayAuth{
		RoutingStrategy: StrategyRoundRobin,
		Accounts: []ConnectedAccount{
			{
				Email:       "pro1@gmail.com",
				Provider:    ProviderAntigravity,
				Tier:        "Pro",
				AccessToken: "ya29.pro1",
				ProjectID:   "proj-pro1",
				ExpiresAt:   time.Now().Unix() + 3600,
			},
			{
				Email:       "ultra1@gmail.com",
				Provider:    ProviderAntigravity,
				Tier:        "Ultra",
				AccessToken: "ya29.ultra1",
				ProjectID:   "proj-ultra1",
				ExpiresAt:   time.Now().Unix() + 3600,
			},
			{
				Email:       "pro2@gmail.com",
				Provider:    ProviderAntigravity,
				Tier:        "Pro",
				AccessToken: "ya29.pro2",
				ProjectID:   "proj-pro2",
				ExpiresAt:   time.Now().Unix() + 3600,
			},
			{
				Email:       "ultra2@gmail.com",
				Provider:    ProviderAntigravity,
				Tier:        "Ultra",
				AccessToken: "ya29.ultra2",
				ProjectID:   "proj-ultra2",
				ExpiresAt:   time.Now().Unix() + 3600,
			},
		},
	}
	if err := saveAuthConfigLocked(cfg); err != nil {
		t.Fatalf("saveAuthConfigLocked failed: %v", err)
	}

	ctx := context.Background()

	// 1. Carbon High must strictly pick Ultra accounts, never Pro accounts
	for i := 0; i < 6; i++ {
		acc, rel, err := AcquireProviderAccountForModel(ctx, ProviderAntigravity, "antigravity/gemini-4-carbon-high", "antigravity/gemini-4-carbon-high")
		if err != nil {
			t.Fatalf("AcquireProviderAccountForModel(carbon-high) failed: %v", err)
		}
		if rel != nil {
			rel()
		}
		if !strings.EqualFold(strings.TrimSpace(acc.Tier), "Ultra") {
			t.Fatalf("expected Carbon High to pick Ultra account, got email=%s tier=%s", acc.Email, acc.Tier)
		}
		if acc.Email != "ultra1@gmail.com" && acc.Email != "ultra2@gmail.com" {
			t.Fatalf("Carbon High picked non-Ultra email: %s", acc.Email)
		}
	}

	// 2. Argon High must also strictly pick Ultra accounts, never Pro accounts
	for i := 0; i < 6; i++ {
		acc, rel, err := AcquireProviderAccountForModel(ctx, ProviderAntigravity, "antigravity/gemini-4-argon-high", "antigravity/gemini-4-argon-high")
		if err != nil {
			t.Fatalf("AcquireProviderAccountForModel(argon-high) failed: %v", err)
		}
		if rel != nil {
			rel()
		}
		if !strings.EqualFold(strings.TrimSpace(acc.Tier), "Ultra") {
			t.Fatalf("expected Argon High to pick Ultra account, got email=%s tier=%s", acc.Email, acc.Tier)
		}
		if acc.Email != "ultra1@gmail.com" && acc.Email != "ultra2@gmail.com" {
			t.Fatalf("Argon High picked non-Ultra email: %s", acc.Email)
		}
	}
}

func TestCarbonArgonExhaustionStepdownToProAccounts(t *testing.T) {
	resetAuthCacheForTest(t)
	ClearAccountModelRateLimits("")

	cfg := SavedGatewayAuth{
		RoutingStrategy: StrategyRoundRobin,
		Accounts: []ConnectedAccount{
			{
				Email:       "pro1@gmail.com",
				Provider:    ProviderAntigravity,
				Tier:        "Pro",
				AccessToken: "ya29.pro1",
				ProjectID:   "proj-pro1",
				ExpiresAt:   time.Now().Unix() + 3600,
			},
			{
				Email:       "ultra1@gmail.com",
				Provider:    ProviderAntigravity,
				Tier:        "Ultra",
				AccessToken: "ya29.ultra1",
				ProjectID:   "proj-ultra1",
				ExpiresAt:   time.Now().Unix() + 3600,
			},
			{
				Email:       "ultra2@gmail.com",
				Provider:    ProviderAntigravity,
				Tier:        "Ultra",
				AccessToken: "ya29.ultra2",
				ProjectID:   "proj-ultra2",
				ExpiresAt:   time.Now().Unix() + 3600,
			},
		},
	}
	if err := saveAuthConfigLocked(cfg); err != nil {
		t.Fatalf("saveAuthConfigLocked failed: %v", err)
	}

	ctx := context.Background()

	// Rate-limit all Ultra accounts for Carbon High
	RecordAccountModelRateLimit("ultra1@gmail.com", "antigravity/gemini-4-carbon-high", 2*time.Minute)
	RecordAccountModelRateLimit("ultra2@gmail.com", "antigravity/gemini-4-carbon-high", 2*time.Minute)

	// Since all Ultra accounts are exhausted, scheduler must safely fall back to Pro account
	acc, rel, err := AcquireProviderAccountForModel(ctx, ProviderAntigravity, "antigravity/gemini-4-carbon-high", "antigravity/gemini-4-carbon-high")
	if err != nil {
		t.Fatalf("expected fallback to Pro account on Ultra exhaustion, got err: %v", err)
	}
	if rel != nil {
		rel()
	}
	if acc.Email != "pro1@gmail.com" || strings.EqualFold(acc.Tier, "Ultra") {
		t.Fatalf("expected Pro account pro1@gmail.com fallback, got %s (tier=%s)", acc.Email, acc.Tier)
	}

	// Verify IsCarbonOrArgonFamilyModel helper correctly detects Carbon & Argon
	if !IsCarbonOrArgonFamilyModel("antigravity/gemini-4-carbon-high") {
		t.Fatalf("expected IsCarbonOrArgonFamilyModel(gemini-4-carbon-high) = true")
	}
	if !IsCarbonOrArgonFamilyModel("antigravity/gemini-4-argon-high") {
		t.Fatalf("expected IsCarbonOrArgonFamilyModel(gemini-4-argon-high) = true")
	}
	if IsCarbonOrArgonFamilyModel("antigravity/gemini-3.1-pro-high") {
		t.Fatalf("expected IsCarbonOrArgonFamilyModel(gemini-3.1-pro-high) = false")
	}
}

func TestGemini31ProPrefersProAccounts(t *testing.T) {
	resetAuthCacheForTest(t)
	ClearAccountModelRateLimits("")

	cfg := SavedGatewayAuth{
		RoutingStrategy: StrategyRoundRobin,
		Accounts: []ConnectedAccount{
			{
				Email:       "ultra1@gmail.com",
				Provider:    ProviderAntigravity,
				Tier:        "Ultra",
				AccessToken: "ya29.ultra1",
				ProjectID:   "proj-ultra1",
				ExpiresAt:   time.Now().Unix() + 3600,
			},
			{
				Email:       "pro1@gmail.com",
				Provider:    ProviderAntigravity,
				Tier:        "Pro",
				AccessToken: "ya29.pro1",
				ProjectID:   "proj-pro1",
				ExpiresAt:   time.Now().Unix() + 3600,
			},
		},
	}
	if err := saveAuthConfigLocked(cfg); err != nil {
		t.Fatalf("saveAuthConfigLocked failed: %v", err)
	}

	ctx := context.Background()

	// Gemini 3.1 Pro High should prefer Pro accounts to save Ultra tokens for Carbon/Argon
	acc, rel, err := AcquireProviderAccountForModel(ctx, ProviderAntigravity, "antigravity/gemini-3.1-pro-high", "antigravity/gemini-3.1-pro-high")
	if err != nil {
		t.Fatalf("AcquireProviderAccountForModel(gemini-3.1-pro-high) failed: %v", err)
	}
	if rel != nil {
		rel()
	}
	if acc.Email != "pro1@gmail.com" {
		t.Fatalf("expected gemini-3.1-pro-high to prefer Pro account pro1@gmail.com, got %s", acc.Email)
	}
}
