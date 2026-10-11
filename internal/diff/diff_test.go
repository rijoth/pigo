package diff

import (
	"os"
	"path/filepath"
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

// TestEditPreview checks the pre/post slices and the uniqueness guard.
func TestEditPreview(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("a\nb\nc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pre, post, ok := EditPreview(path, "b", "B")
	if !ok {
		t.Fatal("EditPreview rejected a unique match")
	}
	if got := strings.Join(Format(pre, post, 1), "\n"); !strings.Contains(got, "-    2 b") || !strings.Contains(got, "+    2 B") {
		t.Fatalf("edit preview:\n%s", got)
	}
	if _, _, ok := EditPreview(path, "missing", "x"); ok {
		t.Fatal("EditPreview accepted a missing match")
	}
	if _, _, ok := EditPreview(filepath.Join(dir, "nope.txt"), "a", "b"); ok {
		t.Fatal("EditPreview read a missing file")
	}
}

// TestEditPreviewAmbiguous checks that a non-unique old string is rejected.
func TestEditPreviewAmbiguous(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(path, []byte("a\na\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := EditPreview(path, "a", "b"); ok {
		t.Fatal("EditPreview accepted an ambiguous match")
	}
}

// TestWritePreview checks the existing and new-file cases.
func TestWritePreview(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f.txt")
	pre, post, ok := WritePreview(path, "x\ny\n")
	if !ok || len(pre) != 0 {
		t.Fatalf("new file preview = %v/%v ok=%v, want empty pre", pre, post, ok)
	}
	if got := strings.Join(Format(pre, post, 1), "\n"); !strings.Contains(got, "+    1 x") {
		t.Fatalf("new file diff:\n%s", got)
	}
	if err := os.WriteFile(path, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pre, post, ok = WritePreview(path, "x\ny\n")
	if !ok || len(pre) != 1 || len(post) != 2 {
		t.Fatalf("existing file preview = %v/%v ok=%v", pre, post, ok)
	}
}
