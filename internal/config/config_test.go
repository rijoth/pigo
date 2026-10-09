package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestContextFilesPaths(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	cfg := filepath.Join(tmp, "config")
	proj := filepath.Join(home, "proj")
	write(t, filepath.Join(cfg, "pigo", "AGENTS.md"), "global")
	write(t, filepath.Join(proj, "AGENTS.md"), "project")
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", cfg)
	t.Chdir(proj)

	files := ContextFiles()
	if len(files) != 2 {
		t.Fatalf("got %d files, want 2: %v", len(files), files)
	}
	if !files[0].Global || files[0].Path != filepath.Join(cfg, "pigo", "AGENTS.md") {
		t.Fatalf("global file wrong: %+v", files[0])
	}
	if files[1].Global || files[1].Path != filepath.Join(proj, "AGENTS.md") {
		t.Fatalf("project file wrong: %+v", files[1])
	}
}

func TestPromptWithContextGlobal(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	cfg := filepath.Join(tmp, "config")
	work := filepath.Join(home, "proj")
	write(t, filepath.Join(cfg, "pigo", "AGENTS.md"), "global rules")
	mkdir(t, work)
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", cfg)
	t.Chdir(work)

	got := PromptWithContext()
	if !strings.Contains(got, "# Global context\nglobal rules") {
		t.Fatalf("global context missing:\n%s", got)
	}
	if strings.Contains(got, "# Project context") {
		t.Fatalf("unexpected project context:\n%s", got)
	}
}

func TestPromptWithContextNearestLast(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	outer := filepath.Join(home, "a")
	inner := filepath.Join(outer, "b")
	write(t, filepath.Join(home, "AGENTS.md"), "home rules")
	write(t, filepath.Join(outer, "AGENTS.md"), "outer rules")
	write(t, filepath.Join(inner, "CLAUDE.md"), "inner rules")
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "config"))
	t.Chdir(inner)

	got := PromptWithContext()
	homeIdx := strings.Index(got, "home rules")
	outerIdx := strings.Index(got, "outer rules")
	innerIdx := strings.Index(got, "inner rules")
	if homeIdx < 0 || outerIdx < 0 || innerIdx < 0 {
		t.Fatalf("missing a context file:\n%s", got)
	}
	if !(homeIdx < outerIdx && outerIdx < innerIdx) {
		t.Fatalf("wrong order (want home, outer, inner): %d %d %d", homeIdx, outerIdx, innerIdx)
	}
}

func TestPromptWithContextPrefersAgents(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	write(t, filepath.Join(home, "AGENTS.md"), "agents win")
	write(t, filepath.Join(home, "CLAUDE.md"), "claude loses")
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "config"))
	t.Chdir(home)

	got := PromptWithContext()
	if !strings.Contains(got, "agents win") || strings.Contains(got, "claude loses") {
		t.Fatalf("AGENTS.md should win over CLAUDE.md:\n%s", got)
	}
}

func TestPromptWithContextOverride(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	proj := filepath.Join(home, "proj")
	write(t, filepath.Join(home, "AGENTS.md"), "home rules")
	write(t, filepath.Join(proj, "AGENTS.md"), "proj rules")
	write(t, filepath.Join(proj, "CLAUDE.md"), "proj claude")
	write(t, filepath.Join(proj, "AGENTS.override.md"), "proj override")
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "config"))
	t.Chdir(proj)

	got := PromptWithContext()
	if !strings.Contains(got, "proj override") {
		t.Fatalf("override missing:\n%s", got)
	}
	if strings.Contains(got, "proj rules") || strings.Contains(got, "proj claude") {
		t.Fatalf("override did not replace same-dir files:\n%s", got)
	}
	if !strings.Contains(got, "home rules") {
		t.Fatalf("override suppressed parent dir:\n%s", got)
	}
}

func TestPromptWithContextOverrideWinsOverClaude(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	write(t, filepath.Join(home, "CLAUDE.md"), "claude loses")
	write(t, filepath.Join(home, "AGENTS.override.md"), "override wins")
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "config"))
	t.Chdir(home)

	got := PromptWithContext()
	if !strings.Contains(got, "override wins") || strings.Contains(got, "claude loses") {
		t.Fatalf("override should win over CLAUDE.md:\n%s", got)
	}
}

func TestPromptWithContextNone(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "config"))
	mkdir(t, home)
	t.Chdir(home)

	if got := PromptWithContext(); got != SystemPrompt {
		t.Fatalf("got:\n%s\nwant base prompt only", got)
	}
}

func TestPromptWithContextGlobalNotDuplicated(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	cfg := filepath.Join(tmp, "config")
	globalDir := filepath.Join(cfg, "pigo")
	write(t, filepath.Join(globalDir, "AGENTS.md"), "global rules")
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", cfg)
	t.Chdir(globalDir)

	got := PromptWithContext()
	if n := strings.Count(got, "global rules"); n != 1 {
		t.Fatalf("global rules repeated %d times:\n%s", n, got)
	}
}

func TestRelPath(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	work := filepath.Join(home, "proj")
	t.Setenv("HOME", home)
	mkdir(t, work)
	t.Chdir(work)

	cases := map[string]string{
		filepath.Join(work, "AGENTS.md"):                    "AGENTS.md",
		filepath.Join(work, "sub", "AGENTS.md"):             filepath.Join("sub", "AGENTS.md"),
		filepath.Join(home, ".config", "pigo", "AGENTS.md"): filepath.Join("~", ".config", "pigo", "AGENTS.md"),
		filepath.Join(tmp, "outside"):                       filepath.Join(tmp, "outside"),
	}
	for in, want := range cases {
		if got := RelPath(in); got != want {
			t.Errorf("RelPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRelHome(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	t.Setenv("HOME", home)
	cases := map[string]string{
		home:                          "~",
		filepath.Join(home, "proj"):   filepath.Join("~", "proj"),
		filepath.Join(tmp, "outside"): filepath.Join(tmp, "outside"),
	}
	for in, want := range cases {
		if got := RelHome(in); got != want {
			t.Errorf("RelHome(%q) = %q, want %q", in, got, want)
		}
	}
}
