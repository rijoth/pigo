package ui

import (
	"testing"

	"github.com/reeflective/readline"
)

func TestParseBang(t *testing.T) {
	cases := []struct {
		line string
		cmd  string
		mode BangMode
	}{
		{"ls", "", BangNone},
		{"!ls", "ls", BangRunAndAdd},
		{"!!ls", "ls", BangRun},
		{"!", "", BangRunAndAdd},
		{"!!", "", BangRun},
		{"!  ls -la  ", "ls -la", BangRunAndAdd},
		{"!! echo hi", "echo hi", BangRun},
		{"  !ls", "", BangNone},
		{"!grep -r x .", "grep -r x .", BangRunAndAdd},
	}
	for _, tc := range cases {
		cmd, mode := ParseBang(tc.line)
		if cmd != tc.cmd || mode != tc.mode {
			t.Errorf("ParseBang(%q) = (%q, %v), want (%q, %v)", tc.line, cmd, mode, tc.cmd, tc.mode)
		}
	}
}

func TestCompleteModels(t *testing.T) {
	models := []string{"nanogpt/qwen", "opencode/glm-5.2"}
	comps := completeModels("op", models)
	var got []string
	comps.EachValue(func(c readline.Completion) readline.Completion {
		got = append(got, c.Value)
		return c
	})
	if len(got) != 1 || got[0] != "opencode/glm-5.2" {
		t.Fatalf("got %v", got)
	}
}

func TestCompleteCommands(t *testing.T) {
	cases := []struct {
		line string
		want []string
	}{
		{"/", []string{"/exit", "/new", "/resume", "/tree", "/session", "/compact", "/model", "/login", "/logout", "/expand"}},
		{"/m", []string{"/model"}},
		{"/x", nil},
		{"hello", nil},
		{"/model extra", nil},
	}
	for _, tc := range cases {
		comps := completeCommands([]rune(tc.line), len(tc.line))
		var got []string
		comps.EachValue(func(c readline.Completion) readline.Completion {
			got = append(got, c.Value)
			return c
		})
		if len(got) != len(tc.want) {
			t.Fatalf("%q: got %v, want %v", tc.line, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Fatalf("%q: got %v, want %v", tc.line, got, tc.want)
			}
		}
	}
}
