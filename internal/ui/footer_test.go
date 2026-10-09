package ui

import (
	"strings"
	"testing"

	"pigo/internal/event"
	"pigo/internal/llm"
)

// TestTurnEndHasNoModel checks the token footer carries usage only: the model
// label now lives at the prompt, so piped output stays free of padding.
func TestTurnEndHasNoModel(t *testing.T) {
	u, b := newTestUI()
	u.model = "openai/gpt-5"
	u.Render(event.Event{Kind: event.TurnEnd, Usage: &llm.Usage{InputTokens: 100, OutputTokens: 20}})
	got := b.String()
	if strings.Contains(got, "openai/gpt-5") {
		t.Fatalf("model leaked into footer: %q", got)
	}
	if !strings.Contains(got, "tokens:") {
		t.Fatalf("missing usage footer: %q", got)
	}
}
