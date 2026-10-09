// Package skills discovers Agent Skills: directories containing a SKILL.md
// file whose leading YAML-ish frontmatter declares a name and description.
// Only the name and description are advertised to the model up front; the full
// SKILL.md is loaded on demand with the read tool.
package skills

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"pigo/internal/config"
)

// Skill is one discovered SKILL.md.
type Skill struct {
	Name         string
	Description  string
	Path         string // absolute path to SKILL.md
	Global       bool   // from ConfigDir()/skills rather than a project dir
	DisableModel bool   // disable-model-invocation: true
}

// Discover returns the skills visible from the working directory: global
// skills in ConfigDir()/skills, then project skills in .pigo/skills from the
// working directory up to the home directory. A nearer project skill replaces
// a global or outer skill with the same name.
func Discover() []Skill {
	var out []Skill
	index := map[string]int{}
	add := func(dir string, global bool) {
		for _, s := range scan(dir) {
			s.Global = global
			if i, ok := index[s.Name]; ok {
				out[i] = s
				continue
			}
			index[s.Name] = len(out)
			out = append(out, s)
		}
	}
	if dir := config.ConfigDir(); dir != "" {
		add(filepath.Join(dir, "skills"), true)
	}
	for _, dir := range config.ProjectDirs() {
		add(filepath.Join(dir, ".pigo", "skills"), false)
	}
	return out
}

// Section renders the advertised skills for the system prompt, or "" when none
// are model-visible. Skills with disable-model-invocation are omitted.
func Section(list []Skill) string {
	var lines []string
	for _, s := range list {
		if s.DisableModel {
			continue
		}
		lines = append(lines, fmt.Sprintf("- %s: %s (read %s first)", s.Name, s.Description, s.Path))
	}
	if len(lines) == 0 {
		return ""
	}
	return "# Skills\n\nRead a skill's SKILL.md file before following it. Load a skill only when the task matches its description.\n\n" + strings.Join(lines, "\n")
}

// scan walks dir recursively and returns every parseable SKILL.md below it.
// Unreadable directories and malformed files are skipped.
func scan(dir string) []Skill {
	var out []Skill
	filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() && d.Name() == "SKILL.md" {
			if s, ok := parse(path); ok {
				out = append(out, s)
			}
		}
		return nil
	})
	return out
}

// parse reads name and description from a SKILL.md frontmatter block. A skill
// without both is not loaded.
func parse(path string) (Skill, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Skill{}, false
	}
	fields := frontmatter(string(b))
	name, desc := fields["name"], fields["description"]
	if name == "" || desc == "" {
		return Skill{}, false
	}
	return Skill{
		Name:         name,
		Description:  desc,
		Path:         path,
		DisableModel: fields["disable-model-invocation"] == "true",
	}, true
}

// frontmatter parses the simple "key: value" block between leading --- fences.
// It is deliberately not a YAML parser; unknown keys are ignored.
func frontmatter(text string) map[string]string {
	out := map[string]string{}
	lines := strings.Split(text, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return out
	}
	for _, line := range lines[1:] {
		if strings.TrimSpace(line) == "---" {
			break
		}
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		out[strings.TrimSpace(key)] = unquote(strings.TrimSpace(val))
	}
	return out
}

// unquote strips one pair of matching single or double quotes.
func unquote(s string) string {
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') && s[len(s)-1] == s[0] {
		return s[1 : len(s)-1]
	}
	return s
}
