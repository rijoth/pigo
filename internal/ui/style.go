package ui

import "charm.land/lipgloss/v2"

// Colors are 256-color ANSI indices, never hex truecolor, so the UI works on
// Termux and any 8/16/256-color terminal.
const (
	colorDim    = "245"
	colorError  = "203"
	colorAdd    = "114"
	colorLabel  = "244"
	colorAccent = "39"  // primary prompt marker
	colorShell  = "220" // marker while typing a ! shell escape
)

// styles holds every lipgloss style the UI uses. When disabled, every field is
// the zero Style: Render returns its input unchanged and emits no ANSI.
type styles struct {
	dim    lipgloss.Style
	err    lipgloss.Style
	add    lipgloss.Style
	del    lipgloss.Style
	hunk   lipgloss.Style
	label  lipgloss.Style
	header lipgloss.Style
	prompt lipgloss.Style
	shell  lipgloss.Style
}

func newStyles(enabled bool) styles {
	var s styles
	if !enabled {
		return s
	}
	s.dim = lipgloss.NewStyle().Faint(true).Foreground(lipgloss.Color(colorDim))
	s.err = lipgloss.NewStyle().Foreground(lipgloss.Color(colorError))
	s.add = lipgloss.NewStyle().Foreground(lipgloss.Color(colorAdd))
	s.del = lipgloss.NewStyle().Foreground(lipgloss.Color(colorError))
	s.hunk = lipgloss.NewStyle().Foreground(lipgloss.Color(colorLabel))
	s.label = lipgloss.NewStyle().Faint(true).Foreground(lipgloss.Color(colorDim))
	s.header = lipgloss.NewStyle().Bold(true)
	s.prompt = lipgloss.NewStyle().Foreground(lipgloss.Color(colorAccent))
	s.shell = lipgloss.NewStyle().Foreground(lipgloss.Color(colorShell))
	return s
}
