package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeSkill(t *testing.T, dir, name, body string) {
	t.Helper()
	full := filepath.Join(dir, name, "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func skill(name, desc string) string {
	return "---\nname: " + name + "\ndescription: " + desc + "\n---\n\nInstructions.\n"
}

func TestDiscoverGlobalAndProject(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	cfg := filepath.Join(tmp, "config")
	proj := filepath.Join(home, "proj")
	writeSkill(t, filepath.Join(cfg, "pigo", "skills"), "shared", skill("shared", "global one"))
	writeSkill(t, filepath.Join(cfg, "pigo", "skills"), "only-global", skill("only-global", "global only"))
	writeSkill(t, filepath.Join(proj, ".pigo", "skills"), "shared", skill("shared", "project one"))
	writeSkill(t, filepath.Join(proj, ".pigo", "skills"), "only-project", skill("only-project", "project only"))
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", cfg)
	t.Chdir(proj)

	got := Discover()
	byName := map[string]string{}
	for _, s := range got {
		byName[s.Name] = s.Description
	}
	if byName["only-global"] != "global only" {
		t.Fatalf("global skill missing: %v", got)
	}
	if byName["only-project"] != "project only" {
		t.Fatalf("project skill missing: %v", got)
	}
	if byName["shared"] != "project one" {
		t.Fatalf("project skill did not override global: %q", byName["shared"])
	}
}

func TestDiscoverNearestProjectWins(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	outer := filepath.Join(home, "a")
	inner := filepath.Join(outer, "b")
	writeSkill(t, filepath.Join(outer, ".pigo", "skills"), "dup", skill("dup", "outer"))
	writeSkill(t, filepath.Join(inner, ".pigo", "skills"), "dup", skill("dup", "inner"))
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "config"))
	t.Chdir(inner)

	got := Discover()
	if len(got) != 1 || got[0].Description != "inner" {
		t.Fatalf("nearest skill should win: %v", got)
	}
}

func TestDiscoverRecursive(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	writeSkill(t, filepath.Join(home, ".pigo", "skills", "group", "nested"), "deep", skill("deep", "nested skill"))
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "config"))
	t.Chdir(home)

	got := Discover()
	if len(got) != 1 || got[0].Name != "deep" {
		t.Fatalf("nested skill not found: %v", got)
	}
}

func TestParseSkipsWithoutDescription(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "skills")
	writeSkill(t, dir, "nodesc", "---\nname: nodesc\n---\nbody\n")
	writeSkill(t, dir, "nofront", "no frontmatter here\n")
	if got := scan(dir); len(got) != 0 {
		t.Fatalf("malformed skills loaded: %v", got)
	}
}

func TestFrontmatterQuotes(t *testing.T) {
	fields := frontmatter("---\nname: foo\ndescription: \"A quoted: value\"\n---\n")
	if fields["name"] != "foo" || fields["description"] != "A quoted: value" {
		t.Fatalf("bad parse: %v", fields)
	}
}

func TestDiscoverGlobalFlag(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	cfg := filepath.Join(tmp, "config")
	proj := filepath.Join(home, "proj")
	writeSkill(t, filepath.Join(cfg, "pigo", "skills"), "g", skill("g", "global"))
	writeSkill(t, filepath.Join(proj, ".pigo", "skills"), "p", skill("p", "project"))
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", cfg)
	t.Chdir(proj)

	byName := map[string]bool{}
	for _, s := range Discover() {
		byName[s.Name] = s.Global
	}
	if !byName["g"] {
		t.Fatalf("global skill not marked global: %v", byName)
	}
	if byName["p"] {
		t.Fatalf("project skill marked global: %v", byName)
	}
}

func TestSection(t *testing.T) {
	if got := Section(nil); got != "" {
		t.Fatalf("empty skills should render nothing: %q", got)
	}
	list := []Skill{
		{Name: "shown", Description: "visible", Path: "/s/shown/SKILL.md"},
		{Name: "hidden", Description: "manual", Path: "/s/hidden/SKILL.md", DisableModel: true},
	}
	got := Section(list)
	if !strings.Contains(got, "shown") || !strings.Contains(got, "/s/shown/SKILL.md") {
		t.Fatalf("visible skill missing:\n%s", got)
	}
	if strings.Contains(got, "hidden") {
		t.Fatalf("disable-model-invocation skill should be hidden:\n%s", got)
	}
}
