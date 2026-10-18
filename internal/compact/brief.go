package compact

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	truncateUser                     = 256
	segmentClosingAssistantHeadWords = 120
	segmentClosingAssistantTailWords = 120
	assistantHeadWords               = 80
	assistantTailWords               = 120
	bashCap                          = 240
	heredocBodyCap                   = 80
	toolCallsPerTurn                 = 8
)

var (
	selfTalkPrefixRe = regexp.MustCompile(`(?i)^\s*(?:hmm|wait|actually|oh|okay|ok|well|so)[,.!\s-]+`)

	normalizeWhitespaceRe = regexp.MustCompile(`[^\S\n]+`)
	multiNewlineRe        = regexp.MustCompile(`\n{3,}`)

	// BASH command compression.
	pipeTailRe     = regexp.MustCompile(`\s*\|\s*(?:head|tail|sort|wc|column|tr|cut|awk|uniq|python3|node|bun)(?:\s[^|]*)?$`)
	trivialLineRe  = regexp.MustCompile(`^(?:set\s+[-+]|cd\s+\S+$|export\s+\w+=|(?:source|\.)\s+\S+|pwd$|true$|:$|#)`)
	heredocOpenRe  = regexp.MustCompile(`<<-?\s*["']?([A-Za-z_]\w*)["']?`)
	filewriterHDRe = regexp.MustCompile(`(?:^|[|&;]\s*)(?:cat|tee|dd)\b`)
	bodyNoiseRe    = regexp.MustCompile(`^(?:import\s|from\s+\S+\s+import|require\(|const\s+\w+\s*=\s*require|#|//|"""|'''|"use strict"|use\s+strict|<\?php)`)
	cdPrefixRe     = regexp.MustCompile(`^cd\s+\S+\s*&&\s*`)
)

// stopWords do not count toward brief word budgets.
var stopWords = map[string]bool{}

func init() {
	for _, w := range strings.Fields(`a an the is are was were be been being
have has had do does did will would could should may might shall can need must
to of in for on with at by from as into through during before after above below
between under over and but or nor not so yet both either neither each every all
any few more most other some such no that this these those it its i me my we our
you your he him his she her they them their who which what if then than when
where how just also`) {
		stopWords[w] = true
	}
}

// normalizeForTokenBudget collapses whitespace and blank runs.
func normalizeForTokenBudget(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = normalizeWhitespaceRe.ReplaceAllString(text, " ")
	text = multiNewlineRe.ReplaceAllString(text, "\n\n")
	return strings.TrimSpace(text)
}

type span struct{ start, end int }

// wordSpans returns the offsets of word-like runs (letters/digits, plus inner
// apostrophes and underscores) in s.
func wordSpans(s string) []span {
	var out []span
	i := 0
	for i < len(s) {
		r, size := utf8.DecodeRuneInString(s[i:])
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			i += size
			continue
		}
		start := i
		i += size
		for i < len(s) {
			r, size = utf8.DecodeRuneInString(s[i:])
			if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '\'' || r == '_' {
				i += size
				continue
			}
			break
		}
		out = append(out, span{start, i})
	}
	return out
}

func significantSpans(flat string) []span {
	var out []span
	for _, w := range wordSpans(flat) {
		if stopWords[strings.ToLower(flat[w.start:w.end])] {
			continue
		}
		out = append(out, w)
	}
	return out
}

// truncateTokens keeps the leading portion of text up to limit significant
// words, appending a truncation marker when it cuts.
func truncateTokens(text string, limit int) string {
	flat := normalizeForTokenBudget(text)
	count := 0
	lastEnd := 0
	for _, w := range wordSpans(flat) {
		if !stopWords[strings.ToLower(flat[w.start:w.end])] {
			count++
			if count > limit {
				return trimRightSpace(flat[:lastEnd]) + "...(truncated)"
			}
		}
		lastEnd = w.end
	}
	return flat
}

// truncateTokensHeadTail keeps the first headLimit and last tailLimit
// significant words, replacing the middle with a marker.
func truncateTokensHeadTail(text string, headLimit, tailLimit int) string {
	flat := normalizeForTokenBudget(text)
	if headLimit <= 0 || tailLimit <= 0 {
		return flat
	}
	words := significantSpans(flat)
	if len(words) <= headLimit+tailLimit {
		return flat
	}
	head := trimRightSpace(flat[:words[headLimit-1].end])
	tail := trimLeftSpace(flat[words[len(words)-tailLimit].start:])
	return head + "\n...(middle truncated)...\n" + tail
}

func nextRenderableBlock(blocks []Block, index int) (Block, bool) {
	for i := index + 1; i < len(blocks); i++ {
		if blocks[i].Kind != KindToolResult {
			return blocks[i], true
		}
	}
	return Block{}, false
}

func isSegmentClosingAssistant(blocks []Block, index int) bool {
	if index >= len(blocks) || blocks[index].Kind != KindAssistant {
		return false
	}
	next, ok := nextRenderableBlock(blocks, index)
	return !ok || next.Kind == KindUser
}

// heredocOpenTerm returns the terminator of a heredoc opener in line, if any.
// The pi-vcc negative lookbehind is emulated by scanning candidate matches.
func heredocOpenTerm(line string) (string, bool) {
	for _, loc := range heredocOpenRe.FindAllStringSubmatchIndex(line, -1) {
		start := loc[0]
		if start > 0 {
			c := line[start-1]
			if c == ')' || isWordByte(c) {
				continue
			}
		}
		return line[loc[2]:loc[3]], true
	}
	return "", false
}

func isWordByte(c byte) bool {
	return c == '_' || (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// heredocCloseIndex returns the line index of the heredoc terminator opened at
// lines[i], or -1 when there is no opener or no matching terminator.
func heredocCloseIndex(lines []string, i int) int {
	term, ok := heredocOpenTerm(lines[i])
	if !ok {
		return -1
	}
	for j := i + 1; j < len(lines); j++ {
		if strings.TrimSpace(lines[j]) == term {
			return j
		}
	}
	return -1
}

func stripCdPrefix(line string) string {
	return strings.TrimSpace(cdPrefixRe.ReplaceAllString(line, ""))
}

func stripPipeTail(line string) string {
	c := line
	for i := 0; i < 3; i++ {
		stripped := pipeTailRe.ReplaceAllString(c, "")
		if stripped == c {
			break
		}
		c = stripped
	}
	return strings.TrimSpace(c)
}

// compressBash semantically compresses a possibly multi-line shell command:
// it drops heredoc bodies and trivial boilerplate, strips cd/pipe scaffolding,
// and caps the result.
func compressBash(raw string) string {
	rawLines := strings.Split(raw, "\n")
	var without []string
	for i := 0; i < len(rawLines); i++ {
		line := rawLines[i]
		without = append(without, line)
		close := heredocCloseIndex(rawLines, i)
		if close == -1 {
			continue
		}
		wantPreview := !filewriterHDRe.MatchString(line)
		preview := ""
		for j := i + 1; wantPreview && preview == "" && j < close; j++ {
			t := strings.TrimSpace(rawLines[j])
			if t != "" && !bodyNoiseRe.MatchString(t) {
				preview = t
			}
		}
		if preview != "" {
			r := []rune(preview)
			clipped := preview
			if len(r) > heredocBodyCap {
				clipped = string(r[:heredocBodyCap-1]) + "\u2026"
			}
			without[len(without)-1] = strings.TrimSpace(line) + " " + clipped
		}
		i = close
	}

	var lines []string
	for _, l := range without {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	if len(lines) == 0 {
		return strings.TrimSpace(raw)
	}

	var meaningful []string
	for _, l := range lines {
		if trivialLineRe.MatchString(l) {
			continue
		}
		if m := stripPipeTail(stripCdPrefix(l)); m != "" {
			meaningful = append(meaningful, m)
		}
	}
	chosen := meaningful
	if len(chosen) == 0 {
		if m := stripPipeTail(stripCdPrefix(lines[0])); m != "" {
			chosen = []string{m}
		}
	}
	cmd := strings.Join(chosen, "; ")
	if r := []rune(cmd); len(r) > bashCap {
		return string(r[:bashCap-3]) + "..."
	}
	return cmd
}

var toolSummaryFields = map[string]string{
	"Read": "file_path", "Edit": "file_path", "Write": "file_path",
	"read": "file_path", "edit": "file_path", "write": "file_path",
	"Glob": "pattern", "Grep": "pattern",
}

func toolOneLiner(name string, args map[string]any) string {
	if field, ok := toolSummaryFields[name]; ok {
		if v, ok := args[field].(string); ok {
			return `* ` + name + ` "` + v + `"`
		}
	}
	if p := extractPath(args); p != "" {
		return `* ` + name + ` "` + p + `"`
	}
	if name == "bash" || name == "Bash" {
		raw, _ := args["command"].(string)
		if raw == "" {
			raw, _ = args["description"].(string)
		}
		return `* ` + name + ` "` + compressBash(raw) + `"`
	}
	if q, ok := args["query"].(string); ok {
		return `* ` + name + ` "` + clip(q, 60) + `"`
	}
	return "* " + name
}

// briefLine is one header/body group in the brief transcript.
type briefLine struct {
	header string
	lines  []string
}

// BuildBriefSections converts normalized blocks into brief transcript groups.
func BuildBriefSections(blocks []Block) []briefLine {
	var sections []briefLine
	lastHeader := ""

	push := func(header, line string) {
		if header == lastHeader && len(sections) > 0 {
			sections[len(sections)-1].lines = append(sections[len(sections)-1].lines, line)
			return
		}
		sections = append(sections, briefLine{header: header, lines: []string{line}})
		lastHeader = header
	}
	pushText := func(header, text, ref string) {
		lines := strings.Split(text, "\n")
		if ref != "" && len(lines) > 0 {
			lines[len(lines)-1] += ref
		}
		for _, line := range lines {
			push(header, line)
		}
	}

	for i := 0; i < len(blocks); i++ {
		b := blocks[i]
		switch b.Kind {
		case KindUser:
			if strings.TrimSpace(b.Text) == "" {
				break
			}
			text := truncateTokens(collapseSkillText(b.Text), truncateUser)
			if text != "" {
				pushText("[user]", text, refSuffix(b.Src))
			}
			lastHeader = "[user]"
		case KindAssistant:
			raw := b.Text
			for j := 0; j < 2; j++ {
				stripped := selfTalkPrefixRe.ReplaceAllString(raw, "")
				if stripped == raw {
					break
				}
				raw = stripped
			}
			var text string
			if isSegmentClosingAssistant(blocks, i) {
				text = truncateTokensHeadTail(raw, segmentClosingAssistantHeadWords, segmentClosingAssistantTailWords)
			} else {
				text = truncateTokensHeadTail(raw, assistantHeadWords, assistantTailWords)
			}
			if text != "" {
				pushText("[assistant]", text, refSuffix(b.Src))
			}
		case KindToolCall:
			if strings.TrimSpace(b.Name) == "" {
				break
			}
			push("[assistant]", toolOneLiner(b.Name, b.Args)+refSuffix(b.Src))
		case KindToolResult:
			// Tool result bodies are intentionally omitted from the brief.
		}
	}

	collapseToolLines(sections)
	capToolCallsPerTurnSections(sections)
	return sections
}

func refSuffix(src int) string {
	if src < 0 {
		return ""
	}
	return " (#" + itoa(src) + ")"
}

var (
	toolLineRefRe  = regexp.MustCompile(`\(#(\d+)\)$`)
	collapseLineRe = regexp.MustCompile(`^(.*) \((#[\d, #]+)\) x(\d+)$`)
	singleRefRe    = regexp.MustCompile(`\(#\d+\)$`)
	refNumberRe    = regexp.MustCompile(`\s*\(#(\d+)\)$`)
)

func collapseToolLines(sections []briefLine) {
	for si := range sections {
		sec := &sections[si]
		if sec.header != "[assistant]" {
			continue
		}
		var out []string
		for _, line := range sec.lines {
			if !strings.HasPrefix(line, "* ") {
				out = append(out, line)
				continue
			}
			m := toolLineRefRe.FindStringSubmatch(line)
			if m == nil {
				out = append(out, line)
				continue
			}
			ref := m[1]
			base := strings.TrimRight(line[:len(line)-(len(ref)+4)], " ")
			last := ""
			if len(out) > 0 {
				last = out[len(out)-1]
			}
			if cm := collapseLineRe.FindStringSubmatch(last); cm != nil && cm[1] == base {
				n := atoi(cm[3]) + 1
				out[len(out)-1] = base + " (" + cm[2] + ", #" + ref + ") x" + itoa(n)
			} else if singleRefRe.MatchString(last) && refNumberRe.ReplaceAllString(last, "") == base {
				prev := refNumberRe.FindStringSubmatch(last)
				out[len(out)-1] = base + " (#" + prev[1] + ", #" + ref + ") x2"
			} else {
				out = append(out, line)
			}
		}
		sec.lines = out
	}
}

func capToolCallsPerTurnSections(sections []briefLine) {
	for si := range sections {
		sec := &sections[si]
		if sec.header != "[assistant]" {
			continue
		}
		var idxs []int
		for i, l := range sec.lines {
			if strings.HasPrefix(l, "* ") {
				idxs = append(idxs, i)
			}
		}
		if len(idxs) <= toolCallsPerTurn {
			continue
		}
		dropCount := len(idxs) - toolCallsPerTurn
		dropSet := map[int]bool{}
		for _, i := range idxs[:dropCount] {
			dropSet[i] = true
		}
		firstKept := idxs[dropCount]
		var next []string
		inserted := false
		for i, l := range sec.lines {
			if dropSet[i] {
				continue
			}
			if !inserted && i == firstKept {
				next = append(next, "* ("+itoa(dropCount)+" earlier tool-call entries omitted)")
				inserted = true
			}
			next = append(next, l)
		}
		sec.lines = next
	}
}

// StringifyBrief renders brief sections to text, suppressing blank lines
// between consecutive tool-summary groups.
func StringifyBrief(sections []briefLine) string {
	var out []string
	for i, sec := range sections {
		if i > 0 {
			prev := sections[i-1]
			prevTools := prev.header == "[assistant]" && allToolLines(prev.lines)
			curTools := sec.header == "[assistant]" && allToolLines(sec.lines)
			if !(prevTools && curTools) {
				out = append(out, "")
			}
		}
		out = append(out, sec.header)
		out = append(out, sec.lines...)
	}
	return joinLines(out)
}

func allToolLines(lines []string) bool {
	for _, l := range lines {
		if !strings.HasPrefix(l, "* ") {
			return false
		}
	}
	return true
}

func atoi(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
	}
	return n
}
