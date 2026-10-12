package ui

import (
	"strings"

	"github.com/reeflective/readline"
)

// Command is a slash command offered by the input completion menu.
type Command struct {
	Name string // includes the leading slash
	Help string // shown to the right of the name
}

// Commands is the list offered when the user starts a line with "/".
var Commands = []Command{
	{"/exit", "quit pigo"},
	{"/new", "start a fresh session"},
	{"/resume", "resume an earlier session"},
	{"/tree", "navigate the session tree"},
	{"/session", "show the current session"},
	{"/compact", "compact the context"},
	{"/model", "select or set the model"},
	{"/thinking", "set or show the reasoning level"},
	{"/login", "store an API key for a provider"},
	{"/logout", "remove a stored API key"},
	{"/expand", "show the last tool result in full"},
}

// BangMode describes what to do with a terminal line starting with ! or !!.
type BangMode int

const (
	BangNone      BangMode = iota
	BangRun                // !!: run and print only
	BangRunAndAdd          // !: run and add the output to the conversation
)

// ParseBang classifies a terminal line starting with ! (run and add to
// context) or !! (run locally). cmd is empty for a bare prefix.
func ParseBang(line string) (cmd string, mode BangMode) {
	if rest, ok := strings.CutPrefix(line, "!!"); ok {
		return strings.TrimSpace(rest), BangRun
	}
	if rest, ok := strings.CutPrefix(line, "!"); ok {
		return strings.TrimSpace(rest), BangRunAndAdd
	}
	return "", BangNone
}

// completeModels filters model refs for the argument of /model.
func completeModels(prefix string, models []string) readline.Completions {
	var values []string
	for _, m := range models {
		if strings.HasPrefix(m, prefix) {
			values = append(values, m)
		}
	}
	return readline.CompleteValues(values...)
}

// completeCommands filters the slash-command list against the current line.
// It returns nothing once the user moves on to arguments or normal text.
func completeCommands(line []rune, _ int) readline.Completions {
	text := string(line)
	if !strings.HasPrefix(text, "/") || strings.ContainsAny(text, " \t") {
		return readline.Completions{}
	}
	var pairs []string
	for _, c := range Commands {
		if strings.HasPrefix(c.Name, text) {
			pairs = append(pairs, c.Name, c.Help)
		}
	}
	return readline.CompleteValuesDescribed(pairs...)
}
