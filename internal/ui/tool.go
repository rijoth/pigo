package ui

import (
	"encoding/json"
	"fmt"
	"strings"

	"pigo/internal/config"
	"pigo/internal/diff"
	"pigo/internal/llm"
)

// indent returns the left padding used for tool output. Narrow terminals drop
// the padding to save columns.
func (u *UI) indent() string {
	if u.narrow {
		return ""
	}
	return "  "
}

// renderToolStart prints a compact one-line header for a tool call, followed by
// a preview of the change an edit or write would make.
func (u *UI) renderToolStart(call *llm.ToolCall) {
	if call == nil {
		return
	}
	u.flushMarkdown()
	u.write(u.styles.header.Render(u.toolHeader(call)) + "\n")
	if lines := u.preview(call); lines != nil {
		if u.diffs == nil {
			u.diffs = map[string][]string{}
		}
		u.diffs[call.ID] = lines
		for _, line := range limitLines(lines, config.UIDiffLines) {
			u.write(u.colorDiffLine(line) + "\n")
		}
	}
}

// preview returns the unified diff an edit or write call would produce, computed
// without running the tool. It returns nil for other tools or when no diff is
// available.
func (u *UI) preview(call *llm.ToolCall) []string {
	var pre, post []string
	var ok bool
	switch call.Name {
	case "edit":
		pre, post, ok = diff.EditPreview(argString(call.Args, "path"), argString(call.Args, "old"), argString(call.Args, "new"))
	case "write":
		pre, post, ok = diff.WritePreview(argString(call.Args, "path"), argString(call.Args, "content"))
	default:
		return nil
	}
	if !ok {
		return nil
	}
	return diff.Format(pre, post, 3)
}

// limitLines caps a preview at max lines and marks the hidden remainder.
func limitLines(lines []string, max int) []string {
	if len(lines) <= max {
		return lines
	}
	out := make([]string, 0, max+1)
	out = append(out, lines[:max]...)
	return append(out, fmt.Sprintf("… (+%d more lines)", len(lines)-max))
}

func (u *UI) toolHeader(call *llm.ToolCall) string {
	switch call.Name {
	case "bash":
		return "$ " + truncateLine(firstLine(argString(call.Args, "command")), u.width-2)
	case "read":
		path := argString(call.Args, "path")
		if u.narrow {
			return "read " + path
		}
		if span := readSpan(call.Args); span != "" {
			return "read " + path + " (" + span + ")"
		}
		return "read " + path
	case "write":
		path := argString(call.Args, "path")
		if u.narrow {
			return "write " + path
		}
		return fmt.Sprintf("write %s (%s)", path, humanSize(len(argString(call.Args, "content"))))
	case "edit":
		return "edit " + argString(call.Args, "path")
	default:
		return call.Name + " " + truncateLine(compactJSON(call.Args), u.width-len(call.Name)-1)
	}
}

// renderToolResult draws a tool result. full disables output truncation for
// /expand.
func (u *UI) renderToolResult(call *llm.ToolCall, res *llm.ToolResult, full bool) {
	if res == nil {
		return
	}
	if call != nil {
		c := *call
		u.lastCall = &c
	} else {
		u.lastCall = nil
	}
	r := *res
	u.lastResult = &r

	name := "bash"
	if call != nil {
		name = call.Name
	}
	switch name {
	case "read":
		u.renderRead(&r, full)
	case "edit":
		u.renderEdit(call, &r)
	case "write":
		u.renderWrite(&r)
	default:
		u.renderOutput(&r, full)
	}
}

// Expand prints the last tool result without truncation. A change preview is
// reprinted in full when one was captured for the last call.
func (u *UI) Expand() {
	if u.lastCall != nil {
		if lines := u.diffs[u.lastCall.ID]; len(lines) > 0 {
			for _, line := range lines {
				u.write(u.colorDiffLine(line) + "\n")
			}
			delete(u.diffs, u.lastCall.ID)
			return
		}
	}
	if u.lastResult == nil {
		u.write(u.styles.dim.Render("nothing to expand") + "\n")
		return
	}
	u.renderToolResult(u.lastCall, u.lastResult, true)
}

// renderOutput prints a bash or generic tool result: dim and indented, limited
// to UIToolLines, each line clipped to the terminal width. Non-zero exit codes
// and errors are red.
func (u *UI) renderOutput(res *llm.ToolResult, full bool) {
	out, code, hasCode := splitExitCode(res.Content)
	isErr := res.IsError || (hasCode && code != 0)
	style := u.styles.dim
	if isErr {
		style = u.styles.err
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		lines = nil
	}
	shown := lines
	if !full && len(shown) > config.UIToolLines {
		shown = shown[:config.UIToolLines]
	}
	indent := u.indent()
	width := u.width - len(indent)
	var b strings.Builder
	for _, l := range shown {
		b.WriteString(style.Render(indent+truncateLine(l, width)) + "\n")
	}
	if !full && len(lines) > len(shown) {
		b.WriteString(style.Render(fmt.Sprintf("%s… (+%d more lines)", indent, len(lines)-len(shown))) + "\n")
	}
	if hasCode && code != 0 {
		b.WriteString(style.Render(fmt.Sprintf("%sexit %d", indent, code)) + "\n")
	}
	u.write(b.String())
}

func (u *UI) renderRead(res *llm.ToolResult, full bool) {
	if res.IsError {
		u.write(u.styles.err.Render(u.indent()+firstLine(res.Content)) + "\n")
		return
	}
	if full {
		u.renderOutput(res, true)
		return
	}
	u.write(u.styles.dim.Render(fmt.Sprintf("%s%d lines", u.indent(), countContentLines(res.Content))) + "\n")
}

func (u *UI) renderWrite(res *llm.ToolResult) {
	style := u.styles.dim
	if res.IsError {
		style = u.styles.err
	}
	u.write(style.Render(u.indent()+firstLine(res.Content)) + "\n")
}

// renderEdit reports an edit result. The diff was already shown as a preview;
// a replay with no stored preview (a resumed session) reconstructs it from disk.
func (u *UI) renderEdit(call *llm.ToolCall, res *llm.ToolResult) {
	if res.IsError {
		u.write(u.styles.err.Render(u.indent()+firstLine(res.Content)) + "\n")
		return
	}
	if call != nil && len(u.diffs[call.ID]) > 0 {
		u.write(u.styles.dim.Render(u.indent()+firstLine(res.Content)) + "\n")
		return
	}
	path := ""
	var old, new string
	if call != nil {
		path = argString(call.Args, "path")
		old = argString(call.Args, "old")
		new = argString(call.Args, "new")
	}
	pre, post, ok := diff.Hunk(path, old, new)
	if !ok {
		pre, post = diff.SplitLines(old), diff.SplitLines(new)
	}
	for _, line := range diff.Format(pre, post, 3) {
		u.write(u.colorDiffLine(line) + "\n")
	}
}

// colorDiffLine applies the add/delete/hunk colors to a formatted diff line.
func (u *UI) colorDiffLine(line string) string {
	switch {
	case strings.HasPrefix(line, "@@"):
		return u.styles.hunk.Render(line)
	case strings.HasPrefix(line, "-"):
		return u.styles.del.Render(line)
	case strings.HasPrefix(line, "+"):
		return u.styles.add.Render(line)
	default:
		return u.styles.dim.Render(line)
	}
}

// splitExitCode splits the bash tool's "exit code: N" line from its output.
func splitExitCode(content string) (out string, code int, ok bool) {
	lines := strings.Split(content, "\n")
	for i, l := range lines {
		if n, err := fmt.Sscanf(strings.TrimSpace(l), "exit code: %d", &code); err == nil && n == 1 {
			return strings.Join(lines[i+1:], "\n"), code, true
		}
	}
	return content, 0, false
}

// countContentLines counts the lines in a read result.
func countContentLines(content string) int {
	if strings.HasPrefix(content, "(empty file)") {
		return 0
	}
	content = strings.TrimRight(content, "\n")
	if content == "" {
		return 0
	}
	return strings.Count(content, "\n") + 1
}

func argString(args json.RawMessage, key string) string {
	var m map[string]any
	if json.Unmarshal(args, &m) != nil {
		return ""
	}
	s, _ := m[key].(string)
	return s
}

// readSpan describes the offset/limit slice of a read call.
func readSpan(args json.RawMessage) string {
	var m map[string]any
	if json.Unmarshal(args, &m) != nil {
		return ""
	}
	offset, _ := m["offset"].(float64)
	limit, _ := m["limit"].(float64)
	switch {
	case offset > 0 && limit > 0:
		return fmt.Sprintf("lines %d-%d", int(offset), int(offset+limit-1))
	case offset > 0:
		return fmt.Sprintf("from line %d", int(offset))
	case limit > 0:
		return fmt.Sprintf("first %d lines", int(limit))
	}
	return ""
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func compactJSON(args json.RawMessage) string {
	s := strings.Join(strings.Fields(string(args)), " ")
	return s
}

func humanSize(n int) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%d B", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	default:
		return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
	}
}
