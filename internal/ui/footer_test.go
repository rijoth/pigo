package ui

import (
	"strings"
	"testing"

	"pigo/internal/event"
	"pigo/internal/llm"
)

// TestTurnEndHasNoModel checks the token footer carries usage only: the model
// label now lives at the prompt, so piped output stays free of padding. The
// footer is led by the project label and closed by a dim rule.
func TestTurnEndHasNoModel(t *testing.T) {
	u, b := newTestUI()
	u.model = "openai/gpt-5"
	u.project = "~/proj/pigo"
	u.Render(event.Event{Kind: event.TurnEnd, Usage: &llm.Usage{InputTokens: 100, OutputTokens: 20}})
	got := b.String()
	if strings.Contains(got, "openai/gpt-5") {
		t.Fatalf("model leaked into footer: %q", got)
	}
	if !strings.HasPrefix(got, "\n~/proj/pigo  tokens:") {
		t.Fatalf("footer not led by project label: %q", got)
	}
	if !strings.HasSuffix(got, strings.Repeat("─", 60)+"\n") {
		t.Fatalf("footer not closed by a rule: %q", got)
	}
}

// TestCompactionShowsSummary checks that compaction prints the generated
// summary body, not just the one-line stats.
func TestCompactionShowsSummary(t *testing.T) {
	u, b := newTestUI()
	u.Compaction("[Session Goal]\n- fix the login bug", "kept 1/2 turns, ~5.2k tok")
	out := b.String()
	if !strings.Contains(out, "· compacted: kept 1/2 turns") {
		t.Fatalf("missing stats header: %q", out)
	}
	if !strings.Contains(out, "[Session Goal]") || !strings.Contains(out, "fix the login bug") {
		t.Fatalf("missing summary body: %q", out)
	}
}
