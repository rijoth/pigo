package markdown

import (
	"strings"
	"testing"
)

// recorder collects markdownSplitter callbacks for assertions.
type recorder struct {
	blocks []string
	events []string
}

func newRecorder() *recorder {
	return &recorder{}
}

func (r *recorder) splitter() *markdownSplitter {
	return &markdownSplitter{
		onBlock:     func(s string) { r.blocks = append(r.blocks, s) },
		onCodeStart: func(lang string) { r.events = append(r.events, "start:"+lang) },
		onCodeLine:  func(line string) { r.events = append(r.events, "line:"+line) },
		onCodeEnd:   func() { r.events = append(r.events, "end") },
	}
}

// TestGoldenBlockSplitter is a golden test of grouping streamed text into
// markdown blocks. Colors are disabled, so the output is plain text.
func TestGoldenBlockSplitter(t *testing.T) {
	r := newRecorder()
	b := r.splitter()
	b.Write("hello ")
	b.Write("world\n\n")
	b.Write("# Head\n")
	b.Write("> quote\n\n")
	b.Write("| a | b |\n| - | - |\n")
	b.Flush()

	want := []string{"hello world\n", "# Head\n> quote\n", "| a | b |\n| - | - |\n"}
	if len(r.blocks) != len(want) {
		t.Fatalf("blocks = %q", r.blocks)
	}
	for i := range want {
		if r.blocks[i] != want[i] {
			t.Fatalf("block %d = %q, want %q", i, r.blocks[i], want[i])
		}
	}
}

// TestGoldenCodeFenceStreaming checks that fence lines are emitted as they
// arrive, before the closing fence, and that blank lines inside a fence do not
// split the block.
func TestGoldenCodeFenceStreaming(t *testing.T) {
	r := newRecorder()
	b := r.splitter()
	b.Write("text\n\n")
	b.Write("```go\n")
	b.Write("a := 1\n")
	b.Write("\n")
	b.Write("b := 2\n")

	// Before the closing fence the code has already streamed.
	wantEvents := []string{"start:go", "line:a := 1", "line:", "line:b := 2"}
	if strings.Join(r.events, "|") != strings.Join(wantEvents, "|") {
		t.Fatalf("streamed events = %q, want %q", r.events, wantEvents)
	}

	b.Write("```\n")
	b.Flush()
	wantEvents = append(wantEvents, "end")
	if strings.Join(r.events, "|") != strings.Join(wantEvents, "|") {
		t.Fatalf("final events = %q, want %q", r.events, wantEvents)
	}
	if len(r.blocks) != 1 || r.blocks[0] != "text\n" {
		t.Fatalf("blocks = %q", r.blocks)
	}
}

// TestGoldenFenceUnclosedFlush checks that an unterminated fence still closes
// on Flush so no renderer stays in code mode.
func TestGoldenFenceUnclosedFlush(t *testing.T) {
	r := newRecorder()
	b := r.splitter()
	b.Write("~~~\nraw\n")
	b.Flush()
	want := []string{"start:", "line:raw", "end"}
	if strings.Join(r.events, "|") != strings.Join(want, "|") {
		t.Fatalf("events = %q, want %q", r.events, want)
	}
}

func TestGoldenSplitAcrossWrites(t *testing.T) {
	r := newRecorder()
	b := r.splitter()
	b.Write("no newline yet")
	if len(r.events) != 0 || len(r.blocks) != 0 {
		t.Fatalf("emitted early: %q / %q", r.events, r.blocks)
	}
	b.Flush()
	if len(r.blocks) != 1 || r.blocks[0] != "no newline yet\n" {
		t.Fatalf("blocks = %q", r.blocks)
	}
}

// TestGoldenCodeFenceRenderNoColor checks the rendered fence with colors off:
// it must be plain text and must not wait for the closing fence.
func TestGoldenCodeFenceRenderNoColor(t *testing.T) {
	var out strings.Builder
	m := NewRenderer(40, false, false, func(s string) string { return s }, func(s string) { out.WriteString(s) })
	m.Write("```go\na := 1\n")
	if out.String() != "```go\na := 1\n" {
		t.Fatalf("streamed output = %q", out.String())
	}
	m.Write("```\n")
	m.Flush()
	if strings.Contains(out.String(), "\x1b[") {
		t.Fatalf("ANSI in plain output: %q", out.String())
	}
}
