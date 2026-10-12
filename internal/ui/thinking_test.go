package ui

import (
	"strings"
	"testing"

	"pigo/internal/event"
)

// TestThinkingRenderAndToggle checks that reasoning renders expanded when
// enabled and collapses to a one-line summary after a toggle.
func TestThinkingRenderAndToggle(t *testing.T) {
	u, b := newTestUI()
	u.showThinking = true
	u.Render(event.Event{Kind: event.ThinkingDelta, Text: "first\nsecond"})
	out := b.String()
	if !strings.Contains(out, "first") || !strings.Contains(out, "second") {
		t.Fatalf("expanded thinking not rendered:\n%s", out)
	}
	if u.ToggleThinking() {
		t.Fatal("expected thinking to be collapsed after toggle")
	}
	b.Reset()
	u.Render(event.Event{Kind: event.ThinkingDelta, Text: "first\nsecond\nthird"})
	out = b.String()
	if !strings.Contains(out, "first") {
		t.Fatalf("collapsed thinking lost its preview:\n%s", out)
	}
	if strings.Contains(out, "second") || strings.Contains(out, "third") {
		t.Fatalf("collapsed thinking leaked body lines:\n%s", out)
	}
	if !strings.Contains(out, "… (+2 more lines)") {
		t.Fatalf("collapsed thinking missing hidden-line count:\n%s", out)
	}
}

// TestThinkingEncrypted checks that a block with no visible text is still
// surfaced rather than silently dropped.
func TestThinkingEncrypted(t *testing.T) {
	u, b := newTestUI()
	u.showThinking = true
	u.Render(event.Event{Kind: event.ThinkingDelta, Text: ""})
	if out := b.String(); !strings.Contains(out, "thinking (encrypted)") {
		t.Fatalf("encrypted thinking not rendered:\n%q", out)
	}
}
