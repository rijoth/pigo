package ui

import (
	"encoding/json"
	"strings"
	"testing"

	"pigo/internal/llm"
)

// TestRestoreClearsAndReplays checks that restoring a session wipes the screen,
// prints the banner header, and prints user turns, assistant text, tool calls,
// and results in order.
func TestRestoreClearsAndReplays(t *testing.T) {
	u, b := newTestUI()
	u.editor = &lineEditor{} // non-nil makes Clear emit the escape
	u.raw = true
	u.model = "test/model"
	history := []llm.Message{
		{Role: llm.RoleUser, Text: "hello"},
		{Role: llm.RoleAssistant, Text: "hi", Calls: []llm.ToolCall{{ID: "c1", Name: "bash", Args: json.RawMessage(`{"command":"ls"}`)}}},
		{Role: llm.RoleTool, Results: []llm.ToolResult{{CallID: "c1", Content: "file"}}},
	}
	u.Restore("PiGo test\n[Model]\nopenai/gpt-5", history)
	out := b.String()
	if !strings.HasPrefix(out, "\x1b[2J\x1b[H") {
		t.Fatalf("did not clear the screen: %q", out)
	}
	for _, want := range []string{"PiGo test", "openai/gpt-5", "test/model > hello", "hi", "bash", "file"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in restored output:\n%s", want, out)
		}
	}
	if strings.Contains(out, "user") {
		t.Fatalf("user label leaked into restored output:\n%s", out)
	}
	if strings.Index(out, "PiGo test") > strings.Index(out, "hello") {
		t.Fatalf("header is not before the transcript:\n%s", out)
	}
	if strings.Index(out, "hello") > strings.Index(out, "hi") {
		t.Fatalf("messages out of order:\n%s", out)
	}
}

// TestRestoreWithoutHeader checks an empty header clears without a banner.
func TestRestoreWithoutHeader(t *testing.T) {
	u, b := newTestUI()
	u.editor = &lineEditor{}
	u.raw = true
	u.Restore("", nil)
	if got := b.String(); got != "\x1b[2J\x1b[H" {
		t.Fatalf("got %q, want only the clear sequence", got)
	}
}

// TestClearNoopWhenNotInteractive checks Clear stays silent for piped output.
func TestClearNoopWhenNotInteractive(t *testing.T) {
	u, b := newTestUI()
	u.Clear()
	if b.String() != "" {
		t.Fatalf("Clear wrote %q", b.String())
	}
}

// TestBannerFramesWithRules checks the startup summary sits between two
// terminal-width horizontal rules.
func TestBannerFramesWithRules(t *testing.T) {
	u, b := newTestUI()
	u.Banner("PiGo test")
	rules := strings.Split(b.String(), "\n")
	if len(rules) < 6 || rules[0] != "" {
		t.Fatalf("expected a leading blank line:\n%q", b.String())
	}
	want := strings.Repeat("─", 60)
	if rules[1] != want || rules[5] != want {
		t.Fatalf("rules not framing banner:\n%q", b.String())
	}
	if rules[3] != "PiGo test" {
		t.Fatalf("banner text = %q", rules[3])
	}
}
