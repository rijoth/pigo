package ui

import (
	"strings"
	"testing"

	"pigo/internal/config"
	"pigo/internal/llm"
)

func newTestUI() (*UI, *strings.Builder) {
	var b strings.Builder
	return &UI{out: &b, width: 60, styles: newStyles(false)}, &b
}

// TestToolOutputTruncation checks the UIToolLines limit and its footer.
func TestToolOutputTruncation(t *testing.T) {
	u, b := newTestUI()
	content := "exit code: 0\n" + strings.Repeat("line\n", config.UIToolLines+3)
	u.renderToolResult(nil, &llm.ToolResult{Content: content}, false)
	out := b.String()
	if got := strings.Count(out, "line\n"); got != config.UIToolLines {
		t.Fatalf("shown lines = %d, want %d:\n%s", got, config.UIToolLines, out)
	}
	if !strings.Contains(out, "… (+3 more lines)") {
		t.Fatalf("missing footer:\n%s", out)
	}
}

// TestExpandFullOutput checks that /expand bypasses the line limit.
func TestExpandFullOutput(t *testing.T) {
	u, b := newTestUI()
	content := "exit code: 0\n" + strings.Repeat("line\n", config.UIToolLines+3)
	u.renderToolResult(nil, &llm.ToolResult{Content: content}, false)
	b.Reset()
	u.Expand()
	out := b.String()
	if strings.Contains(out, "more lines") {
		t.Fatalf("expand truncated:\n%s", out)
	}
	if got := strings.Count(out, "line\n"); got != config.UIToolLines+3 {
		t.Fatalf("expanded lines = %d, want %d", got, config.UIToolLines+3)
	}
}

// TestBashErrorRedAndExitCode checks the non-zero exit path stays on one line
// with the code shown.
func TestBashErrorRedAndExitCode(t *testing.T) {
	u, b := newTestUI()
	u.renderToolResult(nil, &llm.ToolResult{IsError: true, Content: "exit status 3\nexit code: 3\nboom\n"}, false)
	out := b.String()
	if !strings.Contains(out, "boom") || !strings.Contains(out, "exit 3") {
		t.Fatalf("bad output:\n%s", out)
	}
	if strings.Contains(out, "exit code: 3") {
		t.Fatalf("exit-code prefix leaked:\n%s", out)
	}
}
