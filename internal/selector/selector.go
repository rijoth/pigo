// Package selector implements the raw-terminal list picker used by /model,
// /login, and /resume.
package selector

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

// ErrCancel reports that the user dismissed the picker.
var ErrCancel = errors.New("cancelled")

const maxVisible = 10

// minWidth is the terminal width below which Run falls back to a numbered
// prompt instead of the arrow-key list.
const minWidth = 24

// Item is one row in a picker. Value is returned on selection; Label is shown,
// falling back to Value when empty.
type Item struct {
	Value string
	Label string
}

func (it Item) label() string {
	if it.Label != "" {
		return it.Label
	}
	return it.Value
}

// Host carries the terminal resources and callbacks Run needs.
type Host struct {
	Out         io.Writer
	Style       func(string) string
	ReadLine    func(prompt string) (string, error)
	Interrupted func(error) bool
}

// Run shows a picker over items and returns the selected Value. The query
// filters; Up/Down move; Enter selects; Esc cancels. A terminal narrower than
// minWidth uses a numbered prompt instead.
func Run(host Host, title string, items []Item, current string) (string, error) {
	if len(items) == 0 {
		return "", ErrCancel
	}
	fd := int(os.Stdin.Fd())
	width, height := 0, 0
	if w, h, err := term.GetSize(fd); err == nil && w > 0 {
		width, height = w, h
	}
	if width < minWidth {
		return numbered(host, title, items, current)
	}
	state, err := term.MakeRaw(fd)
	if err != nil {
		return "", err
	}
	defer term.Restore(fd, state)

	limit := maxVisible
	if height > 0 && height-3 < limit {
		limit = height - 3
	}
	if limit < 1 {
		limit = 1
	}

	s := New(items, current)
	drawn := 0
	draw := func() {
		if drawn > 0 {
			// Return to column 0 and move up over the previous block.
			fmt.Fprintf(host.Out, "\r\x1b[%dA", drawn)
		}
		fmt.Fprint(host.Out, "\x1b[J")
		lines := s.View(title, current, limit)
		for _, l := range lines {
			l = Truncate(l, width-1)
			if strings.HasPrefix(l, "→ ") && host.Style != nil {
				l = host.Style(l)
			}
			// Raw mode clears OPOST, so \n alone does not return the carriage.
			fmt.Fprint(host.Out, l, "\r\n")
		}
		drawn = len(lines)
	}
	clear := func() {
		if drawn > 0 {
			fmt.Fprintf(host.Out, "\r\x1b[%dA", drawn)
		}
		fmt.Fprint(host.Out, "\x1b[J")
	}

	draw()
	for {
		key, ch, err := ReadKey(fd)
		if err != nil {
			clear()
			return "", err
		}
		switch key {
		case KeyUp:
			s.Move(-1)
		case KeyDown:
			s.Move(1)
		case KeyEnter:
			it, ok := s.Selected()
			if !ok {
				continue
			}
			clear()
			return it.Value, nil
		case KeyCancel:
			clear()
			return "", ErrCancel
		case KeyBackspace:
			s.Backspace()
		case KeyChar:
			s.Insert(rune(ch))
		default:
			continue
		}
		draw()
	}
}

// numbered is the fallback for terminals too narrow for the raw picker. It
// prints a numbered list and reads a number through the host's line editor.
func numbered(host Host, title string, items []Item, current string) (string, error) {
	for _, l := range NumberedView(title, items, current) {
		fmt.Fprintln(host.Out, l)
	}
	for {
		line, err := host.ReadLine("select> ")
		if err != nil {
			if host.Interrupted != nil && host.Interrupted(err) {
				return "", ErrCancel
			}
			return "", err
		}
		n, err := strconv.Atoi(strings.TrimSpace(line))
		if err == nil && n >= 1 && n <= len(items) {
			return items[n-1].Value, nil
		}
		fmt.Fprintf(host.Out, "enter a number 1-%d\n", len(items))
	}
}

// NumberedView formats the narrow-terminal fallback list.
func NumberedView(title string, items []Item, current string) []string {
	lines := make([]string, 0, len(items)+1)
	lines = append(lines, title+":")
	for i, it := range items {
		mark := ""
		if it.Value == current {
			mark = " (current)"
		}
		lines = append(lines, fmt.Sprintf("  %d) %s%s", i+1, it.label(), mark))
	}
	return lines
}

// Selector is the pure state behind Run: the full item list, the typed query,
// the filtered matches, and the highlighted index.
type Selector struct {
	all     []Item
	query   []rune
	matches []Item
	index   int
}

// New builds a selector and places the highlight on current when present.
func New(items []Item, current string) *Selector {
	s := &Selector{all: items}
	s.refilter()
	for i, it := range s.matches {
		if it.Value == current {
			s.index = i
			break
		}
	}
	return s
}

// refilter recomputes matches and clamps the highlight. A non-empty query
// moves the highlight to the best (first) match, like Pi does.
func (s *Selector) refilter() {
	s.matches = Filter(s.all, string(s.query))
	switch {
	case len(s.matches) == 0:
		s.index = 0
	case len(s.query) > 0:
		s.index = 0
	case s.index >= len(s.matches):
		s.index = len(s.matches) - 1
	}
}

// Move shifts the highlight by delta, wrapping at either end.
func (s *Selector) Move(delta int) {
	n := len(s.matches)
	if n == 0 {
		return
	}
	s.index = (s.index + delta + n) % n
}

// Insert appends r to the query and refilters.
func (s *Selector) Insert(r rune) {
	s.query = append(s.query, r)
	s.refilter()
}

// Backspace removes the last query rune and refilters.
func (s *Selector) Backspace() {
	if len(s.query) == 0 {
		return
	}
	s.query = s.query[:len(s.query)-1]
	s.refilter()
}

// Selected returns the highlighted item.
func (s *Selector) Selected() (Item, bool) {
	if s.index < 0 || s.index >= len(s.matches) {
		return Item{}, false
	}
	return s.matches[s.index], true
}

// Filter returns the items whose value or label contains query,
// case-insensitively. An empty query returns every item.
func Filter(items []Item, query string) []Item {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return append([]Item(nil), items...)
	}
	out := make([]Item, 0, len(items))
	for _, it := range items {
		if strings.Contains(strings.ToLower(it.Value), q) || strings.Contains(strings.ToLower(it.label()), q) {
			out = append(out, it)
		}
	}
	return out
}

// View returns the picker's display lines. The selected row starts with "→ ";
// the current item is marked with "✓ ".
func (s *Selector) View(title, current string, maxVisible int) []string {
	lines := make([]string, 0, maxVisible+2)
	lines = append(lines, title+"> "+string(s.query))
	if len(s.matches) == 0 {
		return append(lines, "  (no matches)")
	}
	start := 0
	if len(s.matches) > maxVisible {
		start = s.index - maxVisible/2
		if start < 0 {
			start = 0
		}
		if start > len(s.matches)-maxVisible {
			start = len(s.matches) - maxVisible
		}
	}
	end := start + maxVisible
	if end > len(s.matches) {
		end = len(s.matches)
	}
	for i := start; i < end; i++ {
		cursor := "  "
		if i == s.index {
			cursor = "→ "
		}
		mark := "  "
		if s.matches[i].Value == current {
			mark = "✓ "
		}
		lines = append(lines, cursor+mark+s.matches[i].label())
	}
	if start > 0 || end < len(s.matches) {
		lines = append(lines, fmt.Sprintf("  (%d/%d)", s.index+1, len(s.matches)))
	}
	return lines
}

// Truncate clips s to width display columns, appending an ellipsis. It
// accounts for wide runes and ANSI escapes so a row never soft-wraps.
func Truncate(s string, width int) string {
	if width <= 1 {
		return ""
	}
	return ansi.Truncate(s, width, "…")
}

// Key is one decoded keypress in the picker.
type Key int

const (
	KeyNone Key = iota
	KeyUp
	KeyDown
	KeyEnter
	KeyCancel
	KeyBackspace
	KeyChar
)

// ReadKey decodes one key from fd. Escape sequences are disambiguated from a
// lone Esc with a short poll timeout.
func ReadKey(fd int) (Key, byte, error) {
	b, ok, err := PollByte(fd, -1)
	if err != nil {
		return KeyNone, 0, err
	}
	if !ok {
		return KeyCancel, 0, nil
	}
	switch b {
	case 0x1b:
		b2, ok, err := PollByte(fd, 50)
		if err != nil {
			return KeyNone, 0, err
		}
		if !ok {
			return KeyCancel, 0, nil
		}
		if b2 != '[' && b2 != 'O' {
			return KeyNone, 0, nil
		}
		b3, ok, err := PollByte(fd, 50)
		if err != nil {
			return KeyNone, 0, err
		}
		if !ok {
			return KeyNone, 0, nil
		}
		switch b3 {
		case 'A':
			return KeyUp, 0, nil
		case 'B':
			return KeyDown, 0, nil
		}
		return KeyNone, 0, nil
	case '\r', '\n':
		return KeyEnter, 0, nil
	case 0x7f, 0x08:
		return KeyBackspace, 0, nil
	case 0x03, 0x04:
		return KeyCancel, 0, nil
	default:
		if b >= 0x20 {
			return KeyChar, b, nil
		}
		return KeyNone, 0, nil
	}
}

// PollByte waits up to timeout milliseconds (negative blocks) and reads one
// byte. ok is false on timeout.
func PollByte(fd, timeout int) (byte, bool, error) {
	fds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
	for {
		n, err := unix.Poll(fds, timeout)
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			return 0, false, err
		}
		if n == 0 {
			return 0, false, nil
		}
		var buf [1]byte
		nr, err := unix.Read(fd, buf[:])
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			return 0, false, err
		}
		if nr == 0 {
			return 0, false, io.EOF
		}
		return buf[0], true, nil
	}
}
