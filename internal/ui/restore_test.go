package ui

import (
	"encoding/json"
	"strings"
	"testing"

	"pigo/internal/llm"
)

// TestRestoreClearsAndReplays checks that restoring a session wipes the screen
// and prints user turns, assistant text, tool calls, and results in order.
func TestRestoreClearsAndReplays(t *testing.T) {
	u, b := newTestUI()
	u.editor = &lineEditor{} // non-nil makes Clear emit the escape
	u.raw = true
	history := []llm.Message{
		{Role: llm.RoleUser, Text: "hello"},
		{Role: llm.RoleAssistant, Text: "hi", Calls: []llm.ToolCall{{ID: "c1", Name: "bash", Args: json.RawMessage(`{"command":"ls"}`)}}},
		{Role: llm.RoleTool, Results: []llm.ToolResult{{CallID: "c1", Content: "file"}}},
	}
	u.Restore(history)
	out := b.String()
	if !strings.HasPrefix(out, "\x1b[2J\x1b[H") {
		t.Fatalf("did not clear the screen: %q", out)
	}
	for _, want := range []string{"user", "hello", "hi", "bash", "file"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in restored output:\n%s", want, out)
		}
	}
	if strings.Index(out, "hello") > strings.Index(out, "hi") {
		t.Fatalf("messages out of order:\n%s", out)
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
