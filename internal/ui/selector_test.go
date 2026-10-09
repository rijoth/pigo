package ui

import "testing"

func TestCtrlLBoundToModelSelect(t *testing.T) {
	e := newLineEditor("")
	found := false
	for _, bind := range e.rl.Config.Binds["emacs"] {
		if bind.Action == "pigo-model-select" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("Ctrl+L is not bound to pigo-model-select")
	}
}
