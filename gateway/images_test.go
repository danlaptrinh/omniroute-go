package gateway

import (
	"context"
	"strings"
	"testing"
)

func TestIsImageModel(t *testing.T) {
	tests := []struct {
		model    string
		expected bool
	}{
		{"gemini-3-pro-image", true},
		{"imagen-3", true},
		{"imagen-3.0-generate-002", true},
		{"antigravity/gemini-3-pro-image", true},
		{"wanx-v1", true},
		{"gemini-4-argon-high", false},
		{"claude-opus-4-6", false},
		{"qwen3-coder-plus", false},
	}

	for _, tt := range tests {
		got := IsImageModel(tt.model)
		if got != tt.expected {
			t.Errorf("IsImageModel(%q) = %v; want %v", tt.model, got, tt.expected)
		}
	}
}

func TestImageChatCompletion_MockGeneration(t *testing.T) {
	ctx := context.Background()
	resp, err := GenerateImageChatCompletion(ctx, "gemini-3-pro-image", "a futuristic neon cyberpunk cityscape")
	if err != nil {
		t.Fatalf("expected successful mock image completion, got err: %v", err)
	}

	if resp.Model != "gemini-3-pro-image" {
		t.Errorf("unexpected model: %s", resp.Model)
	}
	if len(resp.Choices) == 0 {
		t.Fatalf("empty choices in image response")
	}
	content := resp.Choices[0].Message.Content
	if !strings.Contains(content, "![Generated Image](data:image/") {
		t.Fatalf("expected markdown image format, got: %s", content)
	}
}
