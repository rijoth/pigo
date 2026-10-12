package ui

import "testing"

// TestThinkingKeybinds checks Shift+Tab, Ctrl+T, and Ctrl+L are bound to
// pigo actions rather than their readline defaults.
func TestThinkingKeybinds(t *testing.T) {
	e := newLineEditor("")
	cases := map[string]string{
		"\x1b[Z": "pigo-thinking-cycle",
		"\x14":   "pigo-thinking-toggle",
		"\x0c":   "pigo-model-select",
	}
	for seq, want := range cases {
		if got := e.rl.Config.Binds["emacs"][seq].Action; got != want {
			t.Errorf("%q bound to %q, want %q", seq, got, want)
		}
	}
}
