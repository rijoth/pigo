package ui

import (
	"strings"
	"testing"
	"unicode/utf8"

	"pigo/internal/llm"
)

// TestFooterAlignsModelRight checks the model label is pushed to the right edge
// on a terminal and appended without padding otherwise.
func TestFooterAlignsModelRight(t *testing.T) {
	usage := &llm.Usage{InputTokens: 100, OutputTokens: 20}

	u, _ := newTestUI()
	u.editor = &lineEditor{}
	u.model = "openai/gpt-5"
	got := u.footer(usage)
	if !strings.HasSuffix(got, "openai/gpt-5") {
		t.Fatalf("missing model: %q", got)
	}
	if n := utf8.RuneCountInString(got); n != u.width {
		t.Fatalf("width = %d, want %d: %q", n, u.width, got)
	}

	u.editor = nil
	u.model = ""
	plain := u.footer(usage)
	if strings.HasSuffix(plain, " ") {
		t.Fatalf("unexpected padding: %q", plain)
	}
}
