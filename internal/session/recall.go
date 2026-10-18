package session

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"pigo/internal/llm"
)

// recallArgs is the pigo_recall tool's argument shape.
type recallArgs struct {
	Query string `json:"query"`
	Ref   int    `json:"ref"`
	Mode  string `json:"mode"`
	Scope string `json:"scope"`
	Limit int    `json:"limit"`
	Page  int    `json:"page"`
}

// recallEntry is one message in the searchable, session-global "#N" space.
type recallEntry struct {
	index   int
	id      string
	msg     llm.Message
	lineage bool // on the active branch ending at the session leaf
}

var recallRefRe = regexp.MustCompile(`^#?(\d+)$`)

// Recall implements the pigo_recall tool: it searches every message in the
// session file (including anything dropped by compaction) and returns matches
// labelled with the same session-global #N refs used in compaction summaries.
func (s *Session) Recall(args json.RawMessage) (string, error) {
	var a recallArgs
	if len(args) > 0 {
		if err := json.Unmarshal(args, &a); err != nil {
			return "", fmt.Errorf("invalid arguments: %w", err)
		}
	}
	t, err := parse(s.path)
	if err != nil {
		return "", err
	}
	entries := t.recallEntries()
	if len(entries) == 0 {
		return "no prior messages in this session yet", nil
	}

	// A single #N expands one entry in full. `ref` or a bare "#12" query both work.
	if a.Ref > 0 || recallRefRe.MatchString(strings.TrimSpace(a.Query)) {
		ref := a.Ref
		if ref <= 0 {
			m := recallRefRe.FindStringSubmatch(strings.TrimSpace(a.Query))
			fmt.Sscanf(m[1], "%d", &ref)
		}
		for _, e := range entries {
			if e.index == ref {
				return formatRecallEntry(e), nil
			}
		}
		return "", fmt.Errorf("no entry #%d (valid range: #%d..#%d)", ref, entries[0].index, entries[len(entries)-1].index)
	}

	scoped := filterRecallScope(entries, a.Scope)
	if strings.EqualFold(strings.TrimSpace(a.Mode), "touched") {
		return formatRecallTouched(scoped), nil
	}
	if strings.TrimSpace(a.Query) == "" {
		return formatRecallRecent(scoped, a), nil
	}
	return formatRecallSearch(scoped, a), nil
}

// recallEntries numbers every message entry in file order, matching
// Session.GlobalIndex, and marks which are on the active branch.
func (t *Tree) recallEntries() []recallEntry {
	onBranch := map[string]bool{}
	for _, id := range t.ancestors(t.Leaf) {
		onBranch[id] = true
	}
	var out []recallEntry
	n := 0
	for _, id := range t.Order {
		node := t.Nodes[id]
		if node == nil || node.Type != "message" || node.Message == nil {
			continue
		}
		out = append(out, recallEntry{index: n, id: id, msg: *node.Message, lineage: onBranch[id]})
		n++
	}
	return out
}

func filterRecallScope(entries []recallEntry, scope string) []recallEntry {
	if strings.EqualFold(strings.TrimSpace(scope), "all") {
		return entries
	}
	var out []recallEntry
	for _, e := range entries {
		if e.lineage {
			out = append(out, e)
		}
	}
	if len(out) == 0 {
		return entries
	}
	return out
}

// recallHaystack is the searchable text of a message: its visible text plus
// tool calls and tool results.
func recallHaystack(m llm.Message) string {
	var b strings.Builder
	b.WriteString(m.Text)
	for _, c := range m.Calls {
		b.WriteString("\n")
		b.WriteString(c.Name)
		b.WriteString(" ")
		b.Write(c.Args)
	}
	for _, r := range m.Results {
		b.WriteString("\n")
		b.WriteString(r.Content)
	}
	return b.String()
}

func compileRecallTerms(query string) []*regexp.Regexp {
	var out []*regexp.Regexp
	for _, term := range strings.Fields(query) {
		re, err := regexp.Compile("(?i)" + term)
		if err != nil {
			re = regexp.MustCompile("(?i)" + regexp.QuoteMeta(term))
		}
		out = append(out, re)
	}
	return out
}

func formatRecallSearch(entries []recallEntry, a recallArgs) string {
	terms := compileRecallTerms(a.Query)
	type hit struct {
		entry   recallEntry
		score   int
		snippet string
	}
	var hits []hit
	for _, e := range entries {
		hay := recallHaystack(e.msg)
		ok, score := true, 0
		for _, re := range terms {
			loc := re.FindAllStringIndex(hay, -1)
			if len(loc) == 0 {
				ok = false
				break
			}
			score += len(loc)
		}
		if !ok {
			continue
		}
		hits = append(hits, hit{entry: e, score: score, snippet: recallSnippet(hay, terms[0])})
	}
	if len(hits) == 0 {
		return fmt.Sprintf("no entries matched %q", a.Query)
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].score != hits[j].score {
			return hits[i].score > hits[j].score
		}
		return hits[i].entry.index > hits[j].entry.index
	})
	lines := make([]string, 0, len(hits))
	for _, h := range hits {
		lines = append(lines, fmt.Sprintf("#%d %s: %s", h.entry.index, roleLabel(h.entry.msg), h.snippet))
	}
	return renderRecallPages(fmt.Sprintf("matched %q", a.Query), lines, a.Page, a.Limit)
}

func formatRecallRecent(entries []recallEntry, a recallArgs) string {
	lines := make([]string, 0, len(entries))
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		lines = append(lines, fmt.Sprintf("#%d %s: %s", e.index, roleLabel(e.msg), recallPreview(e.msg)))
	}
	return renderRecallPages("recent entries", lines, a.Page, a.Limit)
}

func formatRecallTouched(entries []recallEntry) string {
	refs := map[string][]int{}
	for _, e := range entries {
		for _, c := range e.msg.Calls {
			if c.Name != "read" && c.Name != "write" && c.Name != "edit" {
				continue
			}
			var args struct {
				Path string `json:"path"`
			}
			if json.Unmarshal(c.Args, &args) != nil || args.Path == "" {
				continue
			}
			refs[args.Path] = append(refs[args.Path], e.index)
		}
	}
	if len(refs) == 0 {
		return "no files touched in this session yet"
	}
	paths := make([]string, 0, len(refs))
	for p := range refs {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	var b strings.Builder
	b.WriteString("files touched (newest refs first):\n")
	for _, p := range paths {
		seen := map[int]bool{}
		var uniq []int
		for i := len(refs[p]) - 1; i >= 0; i-- {
			if !seen[refs[p][i]] {
				seen[refs[p][i]] = true
				uniq = append(uniq, refs[p][i])
			}
		}
		if len(uniq) > 6 {
			uniq = uniq[:6]
		}
		if len(uniq) == 0 {
			continue
		}
		strs := make([]string, 0, len(uniq))
		for _, n := range uniq {
			strs = append(strs, fmt.Sprintf("#%d", n))
		}
		fmt.Fprintf(&b, "%s  (%s)\n", p, strings.Join(strs, ", "))
	}
	return strings.TrimRight(b.String(), "\n")
}

func formatRecallEntry(e recallEntry) string {
	var b strings.Builder
	fmt.Fprintf(&b, "#%d %s\n", e.index, roleLabel(e.msg))
	if e.msg.Text != "" {
		b.WriteString(e.msg.Text)
		b.WriteString("\n")
	}
	for _, c := range e.msg.Calls {
		fmt.Fprintf(&b, "[tool_call] %s %s\n", c.Name, string(c.Args))
	}
	for _, r := range e.msg.Results {
		fmt.Fprintf(&b, "[tool_result] %s\n", r.Content)
	}
	return strings.TrimRight(b.String(), "\n")
}

func renderRecallPages(label string, lines []string, page, limit int) string {
	total := len(lines)
	if total == 0 {
		return label + ": no results"
	}
	if limit <= 0 {
		limit = 5
	}
	if page < 1 {
		page = 1
	}
	pages := (total + limit - 1) / limit
	if page > pages {
		page = pages
	}
	start := (page - 1) * limit
	end := start + limit
	if end > total {
		end = total
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s: %d entries (page %d/%d)\n", label, total, page, pages)
	for _, l := range lines[start:end] {
		b.WriteString(l)
		b.WriteString("\n")
	}
	if page < pages {
		fmt.Fprintf(&b, "--- use page:%d for more ---\n", page+1)
	}
	return strings.TrimRight(b.String(), "\n")
}

func recallPreview(m llm.Message) string {
	text := strings.Join(strings.Fields(m.Text), " ")
	if text == "" {
		if len(m.Calls) > 0 {
			return m.Calls[0].Name
		}
		if len(m.Results) > 0 {
			return strings.Join(strings.Fields(m.Results[0].Content), " ")
		}
	}
	return recallClip(text, 100)
}

func recallSnippet(text string, re *regexp.Regexp) string {
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\n", " ")
	loc := re.FindStringIndex(text)
	if loc == nil {
		return recallClip(text, 160)
	}
	const radius = 80
	start := loc[0] - radius
	if start < 0 {
		start = 0
	}
	end := loc[1] + radius
	if end > len(text) {
		end = len(text)
	}
	prefix, suffix := "", ""
	if start > 0 {
		prefix = "..."
	}
	if end < len(text) {
		suffix = "..."
	}
	return prefix + strings.TrimSpace(text[start:end]) + suffix
}

func recallClip(text string, max int) string {
	r := []rune(text)
	if len(r) <= max {
		return text
	}
	return strings.TrimSpace(string(r[:max])) + "...(truncated)"
}

func roleLabel(m llm.Message) string {
	switch m.Role {
	case llm.RoleUser:
		if strings.HasPrefix(m.Text, "[Earlier conversation summary]") {
			return "summary"
		}
		return "user"
	case llm.RoleAssistant:
		return "assistant"
	case llm.RoleTool:
		return "tool"
	}
	return string(m.Role)
}
