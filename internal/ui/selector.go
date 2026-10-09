package ui

import (
	"errors"

	"pigo/internal/selector"
)

// ErrCancel reports that the user dismissed an interactive picker.
var ErrCancel = selector.ErrCancel

// SelectItem is one row in a picker. Value is returned on selection; Label is
// shown, falling back to Value when empty.
type SelectItem = selector.Item

// SelectModel runs the picker over the configured model refs.
func (u *UI) SelectModel(current string) (string, error) {
	items := make([]SelectItem, 0, len(u.models))
	for _, m := range u.models {
		items = append(items, SelectItem{Value: m})
	}
	return u.Select("model", items, current)
}

// Select runs an interactive picker over items and returns the selected Value.
// The query filters; Up/Down move; Enter selects; Esc cancels. A narrow
// terminal uses a numbered prompt instead.
func (u *UI) Select(title string, items []SelectItem, current string) (string, error) {
	if u.editor == nil {
		return "", ErrCancel
	}
	return selector.Run(selector.Host{
		Out: u.out,
		Style: func(s string) string {
			if u.raw {
				return s
			}
			return u.styles.header.Render(s)
		},
		ReadLine:    u.ReadLine,
		Interrupted: func(err error) bool { return errors.Is(err, ErrInterrupt) },
	}, title, items, current)
}

// truncateLine clips s to width display columns, appending an ellipsis.
func truncateLine(s string, width int) string { return selector.Truncate(s, width) }
