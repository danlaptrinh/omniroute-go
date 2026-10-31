package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestModelAliases(t *testing.T) {
	resolver := NewResolver()

	// 1. Built-in alias: gpt-4o -> antigravity/claude-sonnet-4-6
	res := resolver.Resolve("gpt-4o", nil)
	if res.CanonicalID != "antigravity/claude-sonnet-4-6" {
		t.Errorf("expected gpt-4o to resolve to antigravity/claude-sonnet-4-6, got %s", res.CanonicalID)
	}

	// 2. Built-in alias: cursor-fast -> alibaba/qwen3.8-flash
	resFast := resolver.Resolve("cursor-fast", nil)
	if resFast.CanonicalID != "alibaba/qwen3.8-flash" {
		t.Errorf("expected cursor-fast to resolve to alibaba/qwen3.8-flash, got %s", resFast.CanonicalID)
	}

	// 3. Built-in alias: deepseek-r1 -> alibaba/deepseek-v4-pro
	resDeep := resolver.Resolve("deepseek-r1", nil)
	if resDeep.CanonicalID != "alibaba/deepseek-v4-pro" {
		t.Errorf("expected deepseek-r1 to resolve to alibaba/deepseek-v4-pro, got %s", resDeep.CanonicalID)
	}

	// 4. Custom alias
	err := SetModelAlias("my-test-model", "antigravity/gemini-4-argon-high")
	if err != nil {
		t.Fatalf("failed to set model alias: %v", err)
	}
	resCustom := resolver.Resolve("my-test-model", nil)
	if resCustom.CanonicalID != "antigravity/gemini-4-argon-high" {
		t.Errorf("expected my-test-model to resolve to antigravity/gemini-4-argon-high, got %s", resCustom.CanonicalID)
	}

	_ = DeleteModelAlias("my-test-model")
}

func TestHedgedConfig(t *testing.T) {
	err := SetHedgedConfig(true, 1200)
	if err != nil {
		t.Fatalf("SetHedgedConfig failed: %v", err)
	}

	enabled, delay := GetHedgedConfig()
	if !enabled {
		t.Errorf("expected enabled=true, got %v", enabled)
	}
	if delay != 1200 {
		t.Errorf("expected delay=1200, got %d", delay)
	}

	_ = SetHedgedConfig(false, 1500)
}

func TestResponseCache_StreamingHit(t *testing.T) {
	cache := GetGlobalCache()
	cache.Clear()

	zeroTemp := 0.0
	req := OpenAIChatRequest{
		Model: "gemini-4-argon-high",
		Messages: []OpenAIChatMessage{
			{Role: "user", Content: json.RawMessage(`"fast prompt"`),},
		},
		Temperature: &zeroTemp,
		Stream:      true,
	}

	resp := OpenAIChatResponse{
		ID:    "chatcmpl-test",
		Model: "gemini-4-argon-high",
		Choices: []OpenAIChatChoice{
			{
				Index:   0,
				Message: &OpenAIOutputMessage{Role: "assistant", Content: "Fast instant response!"},
			},
		},
		Usage: &OpenAIUsage{TotalTokens: 25},
	}

	cache.Put(req, resp)

	hit, ok := cache.Get(req)
	if !ok {
		t.Fatalf("expected cache hit")
	}
	if hit.Choices[0].Message.Content != "Fast instant response!" {
		t.Errorf("unexpected content: %s", hit.Choices[0].Message.Content)
	}
}

func TestServer_CacheAndAliasEndpoints(t *testing.T) {
	cfg := GatewayConfig{ListenAddr: "127.0.0.1:0"}
	server := NewServer(cfg, NewResolver(), NewUpstream(cfg))

	// Test GET /v1/aliases
	reqAliases := httptest.NewRequest(http.MethodGet, "/v1/aliases", nil)
	recAliases := httptest.NewRecorder()
	server.handleAliases(recAliases, reqAliases)
	if recAliases.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", recAliases.Code)
	}
	if !strings.Contains(recAliases.Body.String(), "gpt-4o") {
		t.Errorf("expected aliases list to contain gpt-4o")
	}

	// Test GET /v1/cache
	reqCache := httptest.NewRequest(http.MethodGet, "/v1/cache", nil)
	recCache := httptest.NewRecorder()
	server.handleCacheStats(recCache, reqCache)
	if recCache.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", recCache.Code)
	}
	if !strings.Contains(recCache.Body.String(), "hit_ratio") {
		t.Errorf("expected cache stats to contain hit_ratio")
	}

	// Test GET /v1/hedging
	reqHedge := httptest.NewRequest(http.MethodGet, "/v1/hedging", nil)
	recHedge := httptest.NewRecorder()
	server.handleHedgingConfig(recHedge, reqHedge)
	if recHedge.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", recHedge.Code)
	}
	if !strings.Contains(recHedge.Body.String(), "delay_ms") {
		t.Errorf("expected hedging config to contain delay_ms")
	}
}
