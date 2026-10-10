package ui

import (
	"bytes"
	"strings"
	"testing"
)

// TestSpinnerStartStopIdempotent checks a second Start or Stop does not spawn
// another ticker or write a second cursor pair.
func TestSpinnerStartStopIdempotent(t *testing.T) {
	var b bytes.Buffer
	s := newSpinner(&b, true, newStyles(true), true)
	s.Start("Thinking…")
	s.Start("Thinking…")
	s.Stop()
	s.Stop()
	out := b.String()
	if n := strings.Count(out, hideCursor); n != 1 {
		t.Fatalf("hide cursor written %d times, want 1:\n%q", n, out)
	}
	if n := strings.Count(out, showCursor); n != 1 {
		t.Fatalf("show cursor written %d times, want 1:\n%q", n, out)
	}
	if !strings.Contains(out, "Thinking") {
		t.Fatalf("label missing:\n%q", out)
	}
}

// TestSpinnerNonTTYNoAnimation checks a non-TTY spinner writes one plain line
// and no frames, cursor control, or other ANSI escapes.
func TestSpinnerNonTTYNoAnimation(t *testing.T) {
	var b bytes.Buffer
	s := newSpinner(&b, false, newStyles(false), true)
	s.Start("Thinking…")
	s.Start("Thinking…")
	s.Stop()
	out := b.String()
	if strings.Contains(out, "\x1b") {
		t.Fatalf("non-TTY spinner wrote ANSI:\n%q", out)
	}
	if n := strings.Count(out, "Thinking"); n != 1 {
		t.Fatalf("static line written %d times, want 1:\n%q", n, out)
	}
}

// TestSpinnerDisabledQuiet checks a disabled spinner writes nothing and that
// disabling mid-run restores the cursor.
func TestSpinnerDisabledQuiet(t *testing.T) {
	var b bytes.Buffer
	u := &UI{spin: newSpinner(&b, true, newStyles(false), true)}
	u.StartSpinner("Thinking…")
	u.DisableSpinner()
	u.StartSpinner("Thinking…") // no-op after disable
	out := b.String()
	if n := strings.Count(out, hideCursor); n != 1 {
		t.Fatalf("hide cursor written %d times, want 1:\n%q", n, out)
	}
	if n := strings.Count(out, showCursor); n != 1 {
		t.Fatalf("cursor not restored on disable:\n%q", out)
	}
}
