package gateway

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCompression_Lite(t *testing.T) {
	input := "Hello,   world!  \n\n\n\nThis is a    test.   \n"
	res := CompressTextLite(input, false)
	if strings.Contains(res, "\n\n\n") {
		t.Fatalf("expected redundant newlines to be collapsed, got: %q", res)
	}
	if strings.Contains(res, "    ") {
		t.Fatalf("expected redundant spaces to be collapsed, got: %q", res)
	}
}

func TestCompression_Caveman(t *testing.T) {
	input := "Hello, could you please write a quick function for me? Basically in my opinion it should be fast. Thank you in advance!"
	res := CompressTextCaveman(input, false)

	if strings.Contains(strings.ToLower(res), "could you please") {
		t.Fatalf("expected polite opener removed, got: %s", res)
	}
	if strings.Contains(strings.ToLower(res), "basically") {
		t.Fatalf("expected hedge phrase removed, got: %s", res)
	}
	if strings.Contains(strings.ToLower(res), "thank you in advance") {
		t.Fatalf("expected polite closer removed, got: %s", res)
	}
}

func TestCompression_PreserveCodeFences(t *testing.T) {
	input := "Please review this code:\n```go\nfunc main() {\n    // please don't touch indentation\n    var x = 1\n}\n```\nThank you so much!"
	res := CompressTextCaveman(input, false)

	// Code block must remain intact
	if !strings.Contains(res, "```go\nfunc main() {\n    // please don't touch indentation\n    var x = 1\n}\n```") {
		t.Fatalf("code inside fence was modified:\n%s", res)
	}
	// Outside text must have polite filler stripped
	if strings.Contains(strings.ToLower(res), "thank you so much") {
		t.Fatalf("polite closer outside code fence was not stripped: %s", res)
	}
}

func TestCompression_RTKLogTrimming(t *testing.T) {
	var lines []string
	lines = append(lines, "=== RUN   TestEverything", "=== STEP 1: initializing")
	for i := 0; i < 60; i++ {
		lines = append(lines, "PASS: subtest passed ok with 0 latency")
	}
	lines = append(lines, "FAIL: TestCriticalDatabase - connection refused", "exit status 1")

	input := strings.Join(lines, "\n")
	res := CompressTextRTK(input, false)

	if !strings.Contains(res, "omitted by RTK log trimmer") {
		t.Fatalf("expected RTK to omit repetitive passing lines, got length %d", len(res))
	}
	if !strings.Contains(res, "FAIL: TestCriticalDatabase") {
		t.Fatalf("expected critical failure line preserved, got:\n%s", res)
	}
}

func TestCompression_UltraHistory(t *testing.T) {
	var msgs []OpenAIChatMessage
	rawSys, _ := json.Marshal("You are an assistant.")
	msgs = append(msgs, OpenAIChatMessage{Role: "system", Content: rawSys})

	for i := 0; i < 15; i++ {
		rawU, _ := json.Marshal("User task step")
		rawA, _ := json.Marshal("Assistant response step")
		msgs = append(msgs, OpenAIChatMessage{Role: "user", Content: rawU})
		msgs = append(msgs, OpenAIChatMessage{Role: "assistant", Content: rawA})
	}

	compacted := compressUltraHistory(msgs, 6)
	if len(compacted) >= len(msgs) {
		t.Fatalf("expected messages to be reduced from %d, got %d", len(msgs), len(compacted))
	}
	// Verify summary placeholder exists
	foundSummary := false
	for _, m := range compacted {
		if strings.Contains(m.ContentText(), "Ultra-Compaction") {
			foundSummary = true
			break
		}
	}
	if !foundSummary {
		t.Fatalf("expected Ultra-Compaction summary placeholder in history")
	}
}

func TestCompression_PipelineSavings(t *testing.T) {
	rawUser, _ := json.Marshal("Hello there! Could you please optimize this prompt? Basically in my opinion it has too many words. Thank you very much!")
	req := OpenAIChatRequest{
		Model: "gemini-4-argon-high",
		Messages: []OpenAIChatMessage{
			{Role: "user", Content: rawUser},
		},
	}

	cfg := CompressionConfig{
		Mode:         CompressionCaveman,
		CompressCode: false,
	}

	compressed, before, after := CompressRequest(req, cfg)
	if after >= before {
		t.Fatalf("expected token reduction: before=%d, after=%d", before, after)
	}
	if len(compressed.Messages[0].ContentText()) >= len(req.Messages[0].ContentText()) {
		t.Fatalf("expected content length reduction")
	}
}

