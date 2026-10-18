package compact

import "regexp"

// section renders an "- item" list under a bracketed title, or "" when empty.
func section(title string, items []string) string {
	if len(items) == 0 {
		return ""
	}
	body := ""
	for i, item := range items {
		if i > 0 {
			body += "\n"
		}
		body += "- " + item
	}
	return "[" + title + "]\n" + body
}

// briefMaxLines caps the brief transcript length.
const briefMaxLines = 120

// tuiSafeLineChars is the wrap width for rendered summary lines.
const tuiSafeLineChars = 120

var (
	lineIndentRe  = regexp.MustCompile(`^\s*(?:[-*]\s+|\d+\.\s+)?`)
	sectionHeadRe = regexp.MustCompile(`^\[.+\]`)
)

func wrapLine(line string, maxChars int) []string {
	if len([]rune(line)) <= maxChars {
		return []string{line}
	}
	indent := lineIndentRe.FindString(line)
	continuation := ""
	if indent != "" {
		n := len([]rune(indent))
		if n > 8 {
			n = 8
		}
		continuation = spaces(n)
	}
	var wrapped []string
	remaining := line
	prefix := ""
	for len([]rune(prefix))+len([]rune(remaining)) > maxChars {
		available := maxChars - len([]rune(prefix))
		if available < 20 {
			available = 20
		}
		r := []rune(remaining)
		splitAt := lastSpaceAtOrBefore(r, available)
		if splitAt < available/2 {
			splitAt = available
		}
		if splitAt > len(r) {
			splitAt = len(r)
		}
		wrapped = append(wrapped, prefix+trimRightSpace(string(r[:splitAt])))
		remaining = trimLeftSpace(string(r[splitAt:]))
		prefix = continuation
	}
	if remaining != "" {
		wrapped = append(wrapped, prefix+remaining)
	}
	if len(wrapped) == 0 {
		return []string{line}
	}
	return wrapped
}

func lastSpaceAtOrBefore(r []rune, max int) int {
	if max > len(r) {
		max = len(r)
	}
	for i := max - 1; i >= 0; i-- {
		if r[i] == ' ' {
			return i
		}
	}
	return -1
}

func spaces(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = ' '
	}
	return string(b)
}

func trimRightSpace(s string) string {
	i := len(s)
	for i > 0 && (s[i-1] == ' ' || s[i-1] == '\t') {
		i--
	}
	return s[:i]
}

func trimLeftSpace(s string) string {
	i := 0
	for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	return s[i:]
}

// WrapLongLines breaks lines longer than maxChars to keep the transcript
// renderable in a terminal.
func WrapLongLines(text string, maxChars int) string {
	if maxChars <= 0 {
		maxChars = tuiSafeLineChars
	}
	lines := splitLines(text)
	var out []string
	for _, line := range lines {
		out = append(out, wrapLine(line, maxChars)...)
	}
	return joinLines(out)
}

// capBrief keeps at most briefMaxLines of the brief transcript, preserving the
// most recent lines and the first full section header in the kept tail.
func capBrief(text string) string {
	lines := splitLines(text)
	if len(lines) <= briefMaxLines {
		return text
	}
	omitted := len(lines) - briefMaxLines
	kept := lines[len(lines)-briefMaxLines:]
	firstHeader := -1
	for i, l := range kept {
		if sectionHeadRe.MatchString(l) {
			firstHeader = i
			break
		}
	}
	clean := kept
	if firstHeader > 0 {
		clean = kept[firstHeader:]
	}
	return "...(" + itoa(omitted) + " earlier lines omitted)\n\n" + joinLines(clean)
}

// recallNote is appended once to every compiled summary.
const recallNote = "Use `vcc_recall` to search for prior work, decisions, and context from before this summary. Do not redo work already completed."

// sectionData is the extracted, structured content of a summary.
type sectionData struct {
	sessionGoal        []string
	outstandingContext []string
	filesAndChanges    []string
	commits            []string
	userPreferences    []string
	trackedCommands    []string
	briefTranscript    string
}

// formatSummary renders sectionData into the pi-vcc summary body.
func formatSummary(data sectionData, capBriefTranscript bool) string {
	var headerParts []string
	for _, s := range []string{
		section("Session Goal", data.sessionGoal),
		section("Files And Changes", data.filesAndChanges),
		section("Commits", data.commits),
		section("Tracked Commands", data.trackedCommands),
		section("Outstanding Context", data.outstandingContext),
		section("User Preferences", data.userPreferences),
	} {
		if s != "" {
			headerParts = append(headerParts, s)
		}
	}
	var parts []string
	if len(headerParts) > 0 {
		parts = append(parts, joinStrings(headerParts, "\n\n"))
	}
	if data.briefTranscript != "" {
		if capBriefTranscript {
			parts = append(parts, capBrief(data.briefTranscript))
		} else {
			parts = append(parts, data.briefTranscript)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return WrapLongLines(joinStrings(parts, "\n\n---\n\n"), tuiSafeLineChars)
}
