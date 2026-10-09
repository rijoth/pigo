package ui

import (
	"strings"
	"testing"
)

// TestPromptLabel checks the primary prompt leads with the model and switches
// to the ! shell hint while the line starts with !. Other prompts pass through.
func TestPromptLabel(t *testing.T) {
	u, _ := newTestUI()
	u.model = "openai/gpt-5"

	if got := u.prompt(MainPrompt, ""); got != "openai/gpt-5 > " {
		t.Fatalf("normal = %q", got)
	}
	if got := u.prompt(MainPrompt, "!ls"); got != "! > " {
		t.Fatalf("shell = %q", got)
	}
	if got := u.prompt("... ", "!ls"); got != "... " {
		t.Fatalf("continuation = %q", got)
	}
}

// TestPromptMarkerColor checks the marker is accent-colored normally, yellow in
// shell mode, and red after a failed turn.
func TestPromptMarkerColor(t *testing.T) {
	u, _ := newTestUI()
	u.styles = newStyles(true)
	u.model = "m"

	if got := u.prompt(MainPrompt, ""); !strings.Contains(got, "38;5;39") {
		t.Fatalf("no accent: %q", got)
	}
	if got := u.prompt(MainPrompt, "!"); !strings.Contains(got, "38;5;220") {
		t.Fatalf("no shell yellow: %q", got)
	}
	u.failed = true
	if got := u.prompt(MainPrompt, ""); !strings.Contains(got, "38;5;203") {
		t.Fatalf("no error red: %q", got)
	}
}

// TestPromptWithoutModel checks an unset model leaves a bare marker.
func TestPromptWithoutModel(t *testing.T) {
	u, _ := newTestUI()
	if got := u.prompt(MainPrompt, ""); got != "> " {
		t.Fatalf("bare = %q", got)
	}
}
