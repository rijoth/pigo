package compact

import (
	"regexp"
	"strings"
)

type commitInfo struct {
	hash    string
	message string
}

var (
	commitMsgRe        = regexp.MustCompile(`git\s+commit[^\n]*?-m\s+(?:"((?:[^"\\]|\\.)*)"|'((?:[^'\\]|\\.)*)'|\$?'((?:[^'\\]|\\.)*)')`)
	hashRe             = regexp.MustCompile(`\b([0-9a-f]{7,12})\b`)
	heredocCommitCmdRe = regexp.MustCompile(`^git\s+commit[^\n"'&;|]*?-m\s+"\$\(cat\s+<<-?\s*(?:'([^']+)'|"([^"]+)"|([A-Za-z_]\w*))(?:\r?\n|\\n)`)
	bracketHashRe      = regexp.MustCompile(`\[\S+\s+([0-9a-f]{7,12})\]`)
	rangeHashRe        = regexp.MustCompile(`\b([0-9a-f]{7,12})\.\.([0-9a-f]{7,12})\b`)
)

func cleanMessage(msg string) string {
	msg = strings.ReplaceAll(msg, `\"`, `"`)
	msg = strings.ReplaceAll(msg, `\'`, `'`)
	return strings.TrimSpace(msg)
}

func linesOf(text string) []string {
	text = strings.ReplaceAll(text, `\n`, "\n")
	return strings.Split(text, "\n")
}

func firstLineOf(text string) string {
	lines := linesOf(text)
	if len(lines) == 0 {
		return ""
	}
	return strings.TrimSpace(lines[0])
}

func firstHeredocBodyLine(rest, delim string) string {
	for _, line := range linesOf(rest) {
		trimmed := strings.TrimSpace(strings.TrimLeft(line, "\t"))
		if trimmed == delim {
			break
		}
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func subjectFromHeredocCmd(cmd string) string {
	m := heredocCommitCmdRe.FindStringSubmatchIndex(cmd)
	if m == nil {
		return ""
	}
	delim := ""
	for _, g := range []int{2, 4, 6} {
		if g < len(m) && m[g] >= 0 {
			delim = cmd[m[g]:m[g+1]]
			break
		}
	}
	if delim == "" {
		return ""
	}
	return cleanMessage(firstHeredocBodyLine(cmd[m[1]:], delim))
}

// ExtractCommits finds git commit commands and pairs them with a hash from the
// following tool result when present.
func ExtractCommits(blocks []Block) []commitInfo {
	var commits []commitInfo
	for i := 0; i < len(blocks); i++ {
		b := blocks[i]
		if b.Kind != KindToolCall || b.Name != "bash" {
			continue
		}
		cmd, _ := b.Args["command"].(string)
		if !regexp.MustCompile(`\bgit\s+commit\b`).MatchString(cmd) {
			continue
		}
		m := commitMsgRe.FindStringSubmatchIndex(cmd)
		if m == nil {
			continue
		}
		group := func(idx int) string {
			if idx < len(m) && m[idx] >= 0 {
				return cmd[m[idx]:m[idx+1]]
			}
			return ""
		}
		first := group(2)
		var heredoc string
		if first != "" && strings.HasPrefix(first, "$(cat <<") {
			heredoc = subjectFromHeredocCmd(cmd[m[0]:])
		}
		message := heredoc
		if message == "" {
			message = firstLineOf(cleanMessage(firstOr(group(4), group(6))))
		}
		if heredoc == "" && strings.HasPrefix(message, "$(cat <<") {
			continue
		}
		if message == "" {
			continue
		}

		hash := ""
		for j := i + 1; j < len(blocks) && j < i+3; j++ {
			r := blocks[j]
			if r.Kind != KindToolResult {
				continue
			}
			if mm := bracketHashRe.FindStringSubmatch(r.Text); mm != nil {
				hash = mm[1]
				break
			}
			if mm := rangeHashRe.FindStringSubmatch(r.Text); mm != nil {
				hash = mm[2]
				break
			}
			if mm := hashRe.FindStringSubmatch(r.Text); mm != nil {
				hash = mm[1]
				break
			}
		}

		key := hash + "::" + message
		dup := false
		for _, c := range commits {
			if c.hash+"::"+c.message == key {
				dup = true
				break
			}
		}
		if !dup {
			commits = append(commits, commitInfo{hash: hash, message: message})
		}
	}
	return commits
}

func firstOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// FormatCommits renders the most recent commits, capped at limit.
func FormatCommits(commits []commitInfo, limit int) []string {
	if limit <= 0 {
		limit = 8
	}
	items := commits
	if len(items) > limit {
		items = items[len(items)-limit:]
	}
	var lines []string
	for _, c := range items {
		prefix := ""
		if c.hash != "" {
			prefix = c.hash + ": "
		}
		lines = append(lines, prefix+c.message)
	}
	return lines
}
