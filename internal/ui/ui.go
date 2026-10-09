// Package ui owns all terminal input, output, and styling.
package ui

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"golang.org/x/term"

	"pigo/internal/config"
	"pigo/internal/event"
	"pigo/internal/llm"
	"pigo/internal/markdown"
)

// ErrInterrupt signals that the user pressed Ctrl+C at the prompt.
var ErrInterrupt = errors.New("interrupt")

// MainPrompt is the primary interactive prompt. main passes it to ReadLine so
// the UI can place the model or mode label before the marker.
const MainPrompt = "> "

// UI renders agent events and reads user input.
type UI struct {
	out    io.Writer
	errOut io.Writer

	raw    bool // RenderMode: "raw", or plain (non-TTY) output
	color  bool // ANSI styling enabled
	narrow bool // terminal below config.NarrowWidth
	width  int
	styles styles
	md     *markdown.Renderer

	editor  *lineEditor
	scanner *bufio.Scanner
	models  []string

	lastCall   *llm.ToolCall
	lastResult *llm.ToolResult

	contextWindow int
	model         string
	project       string
	failed        bool
	inTotal       int
	outTotal      int
}

// New builds a UI. historyFile may be empty to disable persistent history.
func New(out, errOut *os.File, historyFile string) *UI {
	interactive := isTerminal(os.Stdin) && isTerminal(out)
	// Non-TTY output is plain, and "raw" is the explicit escape hatch.
	raw := config.RenderMode == "raw" || !interactive
	color := interactive && !raw && os.Getenv("NO_COLOR") == ""
	width := termWidth(out)
	u := &UI{
		out:    out,
		errOut: errOut,
		raw:    raw,
		color:  color,
		narrow: width < config.NarrowWidth,
		width:  width,
		styles: newStyles(color),
	}
	if !raw {
		u.md = markdown.NewRenderer(width, color, u.narrow, func(s string) string { return u.styles.label.Render(s) }, u.write)
	}
	if interactive {
		u.editor = newLineEditor(historyFile)
	}
	if u.editor == nil {
		u.scanner = bufio.NewScanner(os.Stdin)
	}
	return u
}

// SetModels supplies the model refs used by the /model picker and completion.
func (u *UI) SetModels(refs []string) {
	u.models = refs
	if u.editor != nil {
		u.editor.models = refs
	}
}

// SetContextWindow supplies the current model's context size for the footer.
// Zero disables the context line.
func (u *UI) SetContextWindow(n int) { u.contextWindow = n }

// SetModel supplies the model label shown before the prompt marker. Empty
// leaves the marker bare.
func (u *UI) SetModel(label string) { u.model = label }

// SetProject supplies the project directory label shown at the start of the
// token footer. Empty omits it.
func (u *UI) SetProject(label string) { u.project = label }

// UsageTotals returns the accumulated input and output tokens for the session.
func (u *UI) UsageTotals() (int, int) { return u.inTotal, u.outTotal }

// Interactive reports whether the UI can prompt on a terminal.
func (u *UI) Interactive() bool { return u.editor != nil }

// Close releases the line editor.
func (u *UI) Close() {
	if u.editor != nil {
		u.editor.Close()
	}
}

// Interrupt flushes any buffered markdown and resets styles after Ctrl+C, so a
// cancelled stream never leaves a half-open ANSI sequence on screen.
func (u *UI) Interrupt() {
	u.flushMarkdown()
}

// Render is the single entry point for agent events.
func (u *UI) Render(ev event.Event) {
	switch ev.Kind {
	case event.TextDelta:
		if u.raw {
			fmt.Fprint(u.out, ev.Text)
		} else {
			u.md.Write(ev.Text)
		}
	case event.ToolStart:
		if u.raw {
			fmt.Fprintln(u.out, "> "+toolSummary(ev.Call.Name, ev.Call.Args, u.width))
			return
		}
		u.renderToolStart(ev.Call)
	case event.ToolResult:
		if u.raw {
			u.renderRawResult(ev.Result)
			return
		}
		u.renderToolResult(ev.Call, ev.Result, false)
	case event.TurnEnd:
		u.flushMarkdown()
		fmt.Fprintln(u.out)
		if ev.Usage != nil {
			u.inTotal += ev.Usage.InputTokens
			u.outTotal += ev.Usage.OutputTokens
			fmt.Fprintln(u.out, u.styles.dim.Render(u.usageLine(ev.Usage)))
			fmt.Fprintln(u.out, u.styles.dim.Render(u.rule()))
		}
	case event.Notice:
		fmt.Fprintln(u.out, u.styles.dim.Render("· "+ev.Text))
	case event.Error:
		u.flushMarkdown()
		u.failed = true
		fmt.Fprintln(u.errOut, u.styles.err.Render("error: "+ev.Err.Error()))
	}
}

func (u *UI) write(s string) { fmt.Fprint(u.out, s) }

// Banner prints the startup summary as a dim multi-line block framed by dim
// horizontal rules, so sections do not get the notice bullet prefix.
func (u *UI) Banner(text string) {
	fmt.Fprintln(u.out)
	fmt.Fprintln(u.out, u.styles.dim.Render(u.rule()))
	fmt.Fprintln(u.out)
	fmt.Fprintln(u.out, u.styles.dim.Render(text))
	fmt.Fprintln(u.out)
	fmt.Fprintln(u.out, u.styles.dim.Render(u.rule()))
	fmt.Fprintln(u.out)
}

// rule returns a line of box-drawing characters spanning the terminal width.
func (u *UI) rule() string {
	w := u.width
	if w < 1 {
		w = 80
	}
	return strings.Repeat("─", w)
}

// Clear erases the screen and moves the cursor home. It is a no-op when the
// UI is not interactive, so piped output stays clean.
func (u *UI) Clear() {
	if u.editor == nil {
		return
	}
	fmt.Fprint(u.out, "\x1b[2J\x1b[H")
}

// Restore clears the screen, prints the banner header, and replays a resumed
// conversation so the user sees the context the session continues from. The
// header is skipped when empty. User turns, assistant text, tool calls, and
// results are printed in order.
func (u *UI) Restore(header string, history []llm.Message) {
	u.Clear()
	if header != "" {
		u.Banner(header)
	}
	calls := map[string]*llm.ToolCall{}
	for _, m := range history {
		switch m.Role {
		case llm.RoleUser:
			u.write(u.prompt(MainPrompt, "") + m.Text + "\n")
		case llm.RoleAssistant:
			u.renderTextBlock(m.Text)
			for i := range m.Calls {
				call := &m.Calls[i]
				calls[call.ID] = call
				u.Render(event.Event{Kind: event.ToolStart, Call: call})
			}
		case llm.RoleTool:
			for i := range m.Results {
				res := m.Results[i]
				u.Render(event.Event{Kind: event.ToolResult, Call: calls[res.CallID], Result: &res})
			}
		}
	}
	u.flushMarkdown()
}

// renderTextBlock writes one message body, rendered as markdown when styling is
// on and as plain text otherwise.
func (u *UI) renderTextBlock(text string) {
	if strings.TrimSpace(text) == "" {
		return
	}
	if u.raw || u.md == nil {
		fmt.Fprintln(u.out, text)
		return
	}
	u.md.Write(text)
	u.md.Flush()
}

// prompt renders the primary prompt for the line currently being edited. The
// label is the model, or "!" while the line starts with a shell escape; the
// marker is accent-colored, yellow in shell mode, and red after a failed turn.
// Any other prompt is returned unchanged.
func (u *UI) prompt(base, current string) string {
	if base != MainPrompt {
		return base
	}
	label, marker := u.model, u.styles.prompt
	switch {
	case strings.HasPrefix(current, "!"):
		label, marker = "!", u.styles.shell
	case u.failed:
		marker = u.styles.err
	}
	if label == "" {
		return marker.Render(">") + " "
	}
	return label + " " + marker.Render(">") + " "
}

// usageLine formats the per-turn token footer. It leads with the project
// directory, then token counts, and includes context usage when the model's
// context window is known.
func (u *UI) usageLine(usage *llm.Usage) string {
	var parts []string
	if u.project != "" {
		parts = append(parts, u.project)
	}
	parts = append(parts, fmt.Sprintf("tokens: %s in / %s out", humanTokens(usage.InputTokens), humanTokens(usage.OutputTokens)))
	if u.contextWindow > 0 {
		parts = append(parts, fmt.Sprintf("context: %s / %s (%d%%)", humanTokens(usage.InputTokens), humanTokens(u.contextWindow), usage.InputTokens*100/u.contextWindow))
	}
	return strings.Join(parts, "  ")
}

// humanTokens renders a token count as "128k" when large enough to be noisy.
func humanTokens(n int) string {
	if n >= 1000 {
		return fmt.Sprintf("%dk", n/1000)
	}
	return strconv.Itoa(n)
}
func (u *UI) renderRawResult(r *llm.ToolResult) {
	if r == nil {
		return
	}
	fmt.Fprint(u.out, r.Content)
	if !strings.HasSuffix(r.Content, "\n") {
		fmt.Fprintln(u.out)
	}
}

func (u *UI) flushMarkdown() {
	if u.md != nil {
		u.md.Flush()
	}
	if u.color {
		fmt.Fprint(u.out, "\x1b[0m")
	}
}

// ReadLine reads one logical line. A trailing backslash continues input.
func (u *UI) ReadLine(prompt string) (string, error) {
	if u.editor != nil {
		return u.readEditorLine(prompt)
	}
	if u.scanner == nil || !u.scanner.Scan() {
		if u.scanner != nil {
			if err := u.scanner.Err(); err != nil {
				return "", err
			}
		}
		return "", io.EOF
	}
	return u.scanner.Text(), nil
}

// ReadSecret prompts for a secret and reads it without echoing whenever stdin
// is a terminal, even if stdout is redirected. The value is never written to
// history or logs.
func (u *UI) ReadSecret(prompt string) (string, error) {
	if isTerminal(os.Stdin) {
		fmt.Fprint(u.out, prompt)
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(u.out)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(b)), nil
	}
	return u.ReadLine(prompt)
}

// Confirm asks the user to approve a tool call.
func (u *UI) Confirm(name string, args json.RawMessage) bool {
	fmt.Fprintf(u.errOut, "run %s? [y/N] ", toolSummary(name, args, u.width))
	line, err := u.ReadLine("")
	if err != nil {
		return false
	}
	line = strings.ToLower(strings.TrimSpace(line))
	return line == "y" || line == "yes"
}

// Ask prints a yes/no question and reads the answer. It is not interactive
// when stdout is redirected, so it declines by default.
func (u *UI) Ask(question string) bool {
	if u.editor == nil {
		return false
	}
	fmt.Fprintf(u.out, "%s [y/N] ", question)
	line, err := u.ReadLine("")
	if err != nil {
		return false
	}
	line = strings.ToLower(strings.TrimSpace(line))
	return line == "y" || line == "yes"
}

func toolSummary(name string, args json.RawMessage, width int) string {
	summary := compactJSON(args)
	var m map[string]any
	if json.Unmarshal(args, &m) == nil {
		for _, k := range []string{"path", "command"} {
			if v, ok := m[k].(string); ok && v != "" {
				summary = v
				break
			}
		}
	}
	summary = firstLine(summary)
	label := name + ": " + summary
	return truncateLine(label, width)
}

func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func termWidth(f *os.File) int {
	if w, _, err := term.GetSize(int(f.Fd())); err == nil && w > 0 {
		return w
	}
	if v := os.Getenv("COLUMNS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 20 {
			return n
		}
	}
	return 80
}
