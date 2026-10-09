package selector

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestFilterItems(t *testing.T) {
	items := []Item{
		{Value: "anthropic/claude-sonnet-4-5"},
		{Value: "openai/gpt-5"},
		{Value: "google/gemini-2.5-pro"},
		{Value: "local/llama", Label: "llama (local)"},
	}
	cases := []struct {
		query string
		want  []string
	}{
		{"", []string{"anthropic/claude-sonnet-4-5", "openai/gpt-5", "google/gemini-2.5-pro", "local/llama"}},
		{"gpt", []string{"openai/gpt-5"}},
		{"OPEN", []string{"openai/gpt-5"}},
		{"local)", []string{"local/llama"}},
		{"zzz", nil},
	}
	for _, tc := range cases {
		got := Filter(items, tc.query)
		var vals []string
		for _, it := range got {
			vals = append(vals, it.Value)
		}
		if strings.Join(vals, ",") != strings.Join(tc.want, ",") {
			t.Errorf("Filter(%q) = %v, want %v", tc.query, vals, tc.want)
		}
	}
	// The result must be a copy, not an alias of the input.
	all := Filter(items, "")
	all[0].Value = "changed"
	if items[0].Value != "anthropic/claude-sonnet-4-5" {
		t.Fatal("Filter aliased its input")
	}
}

func TestSelectorNavigation(t *testing.T) {
	refs := []Item{{Value: "a/one"}, {Value: "b/two"}, {Value: "c/three"}}
	s := New(refs, "b/two")
	if s.index != 1 {
		t.Fatalf("start index = %d, want 1", s.index)
	}
	s.Move(1)
	if got, _ := s.Selected(); got.Value != "c/three" {
		t.Fatalf("after down = %q", got.Value)
	}
	s.Move(1) // wraps to top
	if got, _ := s.Selected(); got.Value != "a/one" {
		t.Fatalf("wrap down = %q", got.Value)
	}
	s.Move(-1) // wraps to bottom
	if got, _ := s.Selected(); got.Value != "c/three" {
		t.Fatalf("wrap up = %q", got.Value)
	}

	s.Insert('t') // "two" and "three"
	if len(s.matches) != 2 || s.index != 0 {
		t.Fatalf("after filter matches=%v index=%d", s.matches, s.index)
	}
	if got, _ := s.Selected(); got.Value != "b/two" {
		t.Fatalf("first filtered = %q", got.Value)
	}
	s.extend("hree") // "three" only
	if got, _ := s.Selected(); got.Value != "c/three" {
		t.Fatalf("narrowed = %q", got.Value)
	}
	for len(s.query) > 1 { // back to the single-rune query "t"
		s.Backspace()
	}
	if len(s.matches) != 2 {
		t.Fatalf("after backspace matches=%v", s.matches)
	}
	for len(s.query) > 0 {
		s.Backspace()
	}
	if len(s.matches) != 3 {
		t.Fatalf("cleared matches=%v", s.matches)
	}
}

// extend is a test helper: Insert writes runes one at a time.
func (s *Selector) extend(extra string) {
	for _, r := range extra {
		s.Insert(r)
	}
}

func TestSelectorView(t *testing.T) {
	s := New([]Item{{Value: "a/one"}, {Value: "b/two"}}, "a/one")
	lines := s.View("model", "a/one", 10)
	if len(lines) != 3 {
		t.Fatalf("lines = %v", lines)
	}
	if lines[0] != "model> " {
		t.Fatalf("header = %q", lines[0])
	}
	if !strings.HasPrefix(lines[1], "→ ") || !strings.Contains(lines[1], "✓ a/one") {
		t.Fatalf("selected line = %q", lines[1])
	}
	if strings.Contains(lines[2], "→ ") {
		t.Fatalf("unselected line marked: %q", lines[2])
	}

	empty := New(nil, "")
	if got := empty.View("model", "", 10); len(got) != 2 || got[1] != "  (no matches)" {
		t.Fatalf("empty view = %v", got)
	}
}

func TestNumberedView(t *testing.T) {
	lines := NumberedView("provider", []Item{
		{Value: "anthropic", Label: "anthropic — logged in"},
		{Value: "openai"},
	}, "openai")
	if len(lines) != 3 {
		t.Fatalf("lines = %v", lines)
	}
	if lines[0] != "provider:" {
		t.Fatalf("header = %q", lines[0])
	}
	if lines[1] != "  1) anthropic — logged in" {
		t.Fatalf("row 1 = %q", lines[1])
	}
	if lines[2] != "  2) openai (current)" {
		t.Fatalf("row 2 = %q", lines[2])
	}
}

func TestTruncateLine(t *testing.T) {
	if got := Truncate("abcdef", 4); got != "abc…" {
		t.Fatalf("truncate = %q", got)
	}
	if got := Truncate("abc", 10); got != "abc" {
		t.Fatalf("short = %q", got)
	}
	if got := Truncate("日本語です", 5); ansi.StringWidth(got) > 5 {
		t.Fatalf("wide truncate width = %d (%q)", ansi.StringWidth(got), got)
	}
	if got := Truncate("anything", 1); got != "" {
		t.Fatalf("zero width = %q", got)
	}
}

// TestGoldenTruncateANSI checks ANSI-aware, wide-rune truncation.
func TestGoldenTruncateANSI(t *testing.T) {
	in := "\x1b[31mhello world\x1b[0m"
	got := Truncate(in, 8)
	if w := ansi.StringWidth(got); w > 8 {
		t.Fatalf("width = %d (%q)", w, got)
	}
	if !strings.Contains(got, "…") {
		t.Fatalf("missing ellipsis: %q", got)
	}
	if got := Truncate("日本語です", 5); ansi.StringWidth(got) > 5 {
		t.Fatalf("wide width = %d (%q)", ansi.StringWidth(got), got)
	}
}
