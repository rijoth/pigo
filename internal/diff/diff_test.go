package diff

import (
	"strings"
	"testing"
)

// TestGoldenDiff is a golden test of the unified diff with context lines.
func TestGoldenDiff(t *testing.T) {
	old := SplitLines("a\nb\nc\nd\ne\nf\ng\n")
	new := SplitLines("a\nb\nc\nX\ne\nf\ng\n")
	got := strings.Join(Format(old, new, 1), "\n")
	want := strings.Join([]string{
		"@@ -3,3 +3,3 @@",
		"     3 c",
		"-    4 d",
		"+    4 X",
		"     5 e",
	}, "\n")
	if got != want {
		t.Fatalf("diff:\n%s\n\nwant:\n%s", got, want)
	}
}

// TestGoldenDiffInsert checks added lines and their numbers.
func TestGoldenDiffInsert(t *testing.T) {
	got := strings.Join(Format(SplitLines("a\nb\n"), SplitLines("a\nx\ny\nb\n"), 3), "\n")
	want := strings.Join([]string{
		"@@ -1,2 +1,4 @@",
		"     1 a",
		"+    2 x",
		"+    3 y",
		"     4 b",
	}, "\n")
	if got != want {
		t.Fatalf("diff:\n%s\n\nwant:\n%s", got, want)
	}
}
