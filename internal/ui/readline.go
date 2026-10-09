package ui

import (
	"errors"
	"io"
	"strings"

	"github.com/reeflective/readline"
	"github.com/reeflective/readline/inputrc"
)

// ErrModelSelect signals that Ctrl+L asked to open the model picker.
var ErrModelSelect = errors.New("model select")

type lineEditor struct {
	rl          *readline.Shell
	models      []string
	selectModel bool
}

func newLineEditor(historyFile string) *lineEditor {
	rl := readline.NewShell()
	rl.Keymap.SetMain("emacs")
	rl.Config.Vars["autocomplete"] = true

	if historyFile != "" {
		if h, err := readline.NewHistoryFromFile(historyFile); err == nil {
			rl.History.Add("pigo", h)
		}
	}

	e := &lineEditor{rl: rl}
	rl.Completer = e.complete
	rl.Keymap.Register(map[string]func(){
		"pigo-menu-next":    func() { e.stepMenu(1) },
		"pigo-menu-prev":    func() { e.stepMenu(-1) },
		"pigo-model-select": e.requestModelSelect,
	})
	// Readline stores arrow keys metafied; match and replace them so plain
	// up/down cycle the command menu but stay bound to history otherwise.
	e.bindArrow("\x1b[B", "pigo-menu-next")
	e.bindArrow("\x1b[A", "pigo-menu-prev")
	// Ctrl+L opens the model picker instead of clearing the screen.
	e.bindArrow("\x0c", "pigo-model-select")
	return e
}

// requestModelSelect accepts the current line so the readline call returns,
// then readEditorLine reports ErrModelSelect to the caller.
func (e *lineEditor) requestModelSelect() {
	e.selectModel = true
	// Drop any partially typed line so it does not reach readline history.
	e.rl.Line().Set()
	e.rl.Cursor().Set(0)
	if cmds := e.rl.Keymap.Commands(); cmds["accept-line"] != nil {
		cmds["accept-line"]()
	}
}

func (e *lineEditor) Close() {}

// complete dispatches slash-command completion and /model argument completion.
func (e *lineEditor) complete(line []rune, pos int) readline.Completions {
	text := string(line)
	if rest, ok := strings.CutPrefix(text, "/model "); ok {
		return completeModels(strings.TrimSpace(rest), e.models)
	}
	return completeCommands(line, pos)
}

// stepMenu advances the completion menu when it is open, and falls back to
// history navigation otherwise. The fallback keeps normal arrow behavior intact.
func (e *lineEditor) stepMenu(delta int) {
	cmds := e.rl.Keymap.Commands()
	switch {
	case e.menuOpen():
		if delta > 0 {
			cmds["menu-complete"]()
		} else {
			cmds["menu-complete-backward"]()
		}
	case delta > 0:
		cmds["next-history"]()
	default:
		cmds["previous-history"]()
	}
}

func (e *lineEditor) menuOpen() bool {
	if e.rl.Keymap.Local() == "menu-select" {
		return true
	}
	return strings.HasPrefix(string(*e.rl.Line()), "/")
}

// bindArrow replaces the arrow bindings for every key sequence that decodes to
// target, since readline may store meta keys in more than one representation.
func (e *lineEditor) bindArrow(target, action string) {
	for seq := range e.rl.Config.Binds["emacs"] {
		if demeta(seq) == target {
			e.rl.Config.Binds["emacs"][seq] = inputrc.Bind{Action: action}
		}
	}
}

func demeta(s string) string {
	var b strings.Builder
	for _, r := range s {
		if inputrc.IsMeta(r) {
			b.WriteRune(inputrc.Esc)
			b.WriteRune(inputrc.Demeta(r))
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// readEditorLine reads one logical line; a trailing backslash continues it.
func (u *UI) readEditorLine(prompt string) (string, error) {
	e := u.editor
	e.rl.Prompt.Primary(func() string { return u.prompt(prompt, string(*e.rl.Line())) })
	defer func() { u.failed = false }()
	var lines []string
	for {
		line, err := e.rl.Readline()
		if e.selectModel {
			e.selectModel = false
			return "", ErrModelSelect
		}
		switch {
		case errors.Is(err, readline.ErrInterrupt):
			return "", ErrInterrupt
		case errors.Is(err, io.EOF):
			if len(lines) == 0 {
				return "", io.EOF
			}
			return strings.Join(lines, "\n"), nil
		case err != nil:
			return "", err
		}
		if strings.HasSuffix(line, "\\") {
			lines = append(lines, strings.TrimSuffix(line, "\\"))
			prompt = "... "
			continue
		}
		lines = append(lines, line)
		return strings.Join(lines, "\n"), nil
	}
}
