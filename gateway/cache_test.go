package gateway

import (
	"encoding/json"
	"testing"
	"time"
)

func TestResponseCache_PutGet(t *testing.T) {
	cache := NewResponseCache(10, 1*time.Minute)

	req := OpenAIChatRequest{
		Model: "gemini-4-argon-high",
		Messages: []OpenAIChatMessage{
			{Role: "user", Content: json.RawMessage(`"hello world"`)},
		},
	}

	resp := OpenAIChatResponse{
		ID:    "test-id",
		Model: "gemini-4-argon-high",
		Choices: []OpenAIChatChoice{
			{
				Index:   0,
				Message: &OpenAIOutputMessage{Role: "assistant", Content: "Hello from cache!"},
			},
		},
		Usage: &OpenAIUsage{TotalTokens: 42},
	}

	// Initially miss
	if _, ok := cache.Get(req); ok {
		t.Fatalf("expected cache miss, got hit")
	}

	// Put
	cache.Put(req, resp)

	// Hit
	hitResp, ok := cache.Get(req)
	if !ok {
		t.Fatalf("expected cache hit, got miss")
	}
	if hitResp.Choices[0].Message.Content != "Hello from cache!" {
		t.Errorf("unexpected content: %s", hitResp.Choices[0].Message.Content)
	}

	stats := cache.Stats()
	if stats["hits"].(int64) != 1 {
		t.Errorf("expected 1 hit, got %v", stats["hits"])
	}
}
