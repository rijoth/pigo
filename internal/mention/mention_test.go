package mention

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pigo/internal/config"
)

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestExpandFile(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	write(t, dir, "a.txt", "hello\nworld\n")
	got := Expand("see @a.txt please")
	want := `see <file path="a.txt">` + "\nhello\nworld\n" + `</file>` + " please"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestExpandLeavesUnknownTokens(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	cases := []string{
		"mail me at me@example.com",
		"see @missing.go for details",
		"no mention here",
		"@",
	}
	for _, in := range cases {
		if got := Expand(in); got != in {
			t.Errorf("Expand(%q) = %q, want unchanged", in, got)
		}
	}
}

func TestExpandTrailingPunctuation(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	write(t, dir, "b.go", "package b")
	got := Expand("see @b.go.")
	if !strings.HasPrefix(got, `see <file path="b.go">`) || !strings.HasSuffix(got, "</file>.") {
		t.Fatalf("got %q", got)
	}
}

func TestExpandSkipsBinaryAndDirectory(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.WriteFile(filepath.Join(dir, "bin"), []byte("a\x00b"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, in := range []string{"@bin", "@sub"} {
		if got := Expand(in); got != in {
			t.Errorf("Expand(%q) = %q, want unchanged", in, got)
		}
	}
}

func TestExpandTruncates(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	write(t, dir, "big.txt", strings.Repeat("a", config.MaxReadBytes+1000))
	got := Expand("@big.txt")
	if !strings.Contains(got, "[truncated: showing first ") {
		t.Fatalf("missing truncation marker: %q", got[len(got)-120:])
	}
	if len(got) > config.MaxReadBytes+200 {
		t.Fatalf("block not truncated: %d bytes", len(got))
	}
}

func TestExpandTilde(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	write(t, home, "x.txt", "home file")
	got := Expand("@~/x.txt")
	if !strings.Contains(got, `<file path="~/x.txt">`) || !strings.Contains(got, "home file") {
		t.Fatalf("got %q", got)
	}
}

func TestCollapseRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	write(t, dir, "a.txt", "hello\nworld\n")
	for _, in := range []string{
		"see @a.txt please",
		"@a.txt",
		"see @a.txt.",
		"two @a.txt and @a.txt mentions",
	} {
		if got := Collapse(Expand(in)); got != in {
			t.Errorf("Collapse(Expand(%q)) = %q", in, got)
		}
	}
}

func TestCollapseLeavesPlainText(t *testing.T) {
	in := "no blocks here <file written> and < /file>"
	if got := Collapse(in); got != in {
		t.Fatalf("Collapse(%q) = %q", in, got)
	}
}
