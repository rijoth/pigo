package ui

import (
	"strings"
	"testing"

	"pigo/internal/event"
)

// TestThinkingRenderAndToggle checks that a reasoning block is shown when
// enabled and suppressed after a toggle.
func TestThinkingRenderAndToggle(t *testing.T) {
	u, b := newTestUI()
	u.showThinking = true
	u.Render(event.Event{Kind: event.ThinkingDelta, Text: "first\nsecond"})
	out := b.String()
	if !strings.Contains(out, "first") || !strings.Contains(out, "second") {
		t.Fatalf("thinking not rendered:\n%s", out)
	}
	if u.ToggleThinking() {
		t.Fatal("expected thinking to be off after toggle")
	}
	b.Reset()
	u.Render(event.Event{Kind: event.ThinkingDelta, Text: "hidden"})
	if strings.Contains(b.String(), "hidden") {
		t.Fatalf("hidden thinking leaked:\n%s", b.String())
	}
}
