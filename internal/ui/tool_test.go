package ui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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

// TestEditPreviewRendered checks that a tool start shows the pending edit diff.
func TestEditPreviewRendered(t *testing.T) {
	u, b := newTestUI()
	path := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(path, []byte("a\nb\nc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	args, _ := json.Marshal(map[string]string{"path": path, "old": "b", "new": "B"})
	u.renderToolStart(&llm.ToolCall{ID: "1", Name: "edit", Args: args})
	out := b.String()
	if !strings.Contains(out, "edit "+path) || !strings.Contains(out, "-    2 b") || !strings.Contains(out, "+    2 B") {
		t.Fatalf("edit preview:\n%s", out)
	}
}

// TestWritePreviewRendered checks the all-additions preview for a new file.
func TestWritePreviewRendered(t *testing.T) {
	u, b := newTestUI()
	path := filepath.Join(t.TempDir(), "new.txt")
	args, _ := json.Marshal(map[string]string{"path": path, "content": "x\ny\n"})
	u.renderToolStart(&llm.ToolCall{ID: "2", Name: "write", Args: args})
	out := b.String()
	if !strings.Contains(out, "+    1 x") || !strings.Contains(out, "+    2 y") {
		t.Fatalf("write preview:\n%s", out)
	}
}

// TestExpandPreviewFull checks that /expand reprints the untruncated preview.
func TestExpandPreviewFull(t *testing.T) {
	u, b := newTestUI()
	path := filepath.Join(t.TempDir(), "new.txt")
	var content strings.Builder
	for i := 0; i < config.UIDiffLines+5; i++ {
		fmt.Fprintf(&content, "line%d\n", i)
	}
	args, _ := json.Marshal(map[string]string{"path": path, "content": content.String()})
	call := &llm.ToolCall{ID: "3", Name: "write", Args: args}
	u.renderToolStart(call)
	if !strings.Contains(b.String(), "more lines") {
		t.Fatalf("preview not truncated:\n%s", b.String())
	}
	u.lastCall = call
	b.Reset()
	u.Expand()
	if strings.Contains(b.String(), "more lines") {
		t.Fatalf("expand still truncated:\n%s", b.String())
	}
	if got := strings.Count(b.String(), "\n+"); got != config.UIDiffLines+5 {
		t.Fatalf("expanded + lines = %d, want %d", got, config.UIDiffLines+5)
	}
}
