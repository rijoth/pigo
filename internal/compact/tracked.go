package compact

import (
	"regexp"
	"strings"
)

const (
	maxTrackedEntryChars = 80
	commandSeparator     = " | "
	commandsPerName      = 10
)

func unquotedSeparators(cmd string) []int {
	var seps []int
	var quote byte
	for i := 0; i < len(cmd); i++ {
		c := cmd[i]
		if quote != 0 {
			if c == '\\' && quote == '"' {
				i++
			} else if c == quote {
				quote = 0
			}
			continue
		}
		switch {
		case c == '\\':
			i++
		case c == '\'' || c == '"':
			quote = c
		case c == '#' && (i == 0 || cmd[i-1] == ' ' || cmd[i-1] == '\t' || cmd[i-1] == ';' || cmd[i-1] == '&' || cmd[i-1] == '|' || cmd[i-1] == '('):
			for i+1 < len(cmd) && cmd[i+1] != '\n' {
				i++
			}
		case c == '&' && (i > 0 && (cmd[i-1] == '>' || cmd[i-1] == '<') || i+1 < len(cmd) && cmd[i+1] == '>'):
		case c == ';' || c == '&' || c == '|' || c == '\n':
			seps = append(seps, i)
		}
	}
	return seps
}

func captureEntry(text string) string {
	seps := unquotedSeparators(text)
	if len(seps) > 0 {
		text = text[:seps[0]]
	}
	return strings.TrimSpace(text)
}

var wrapperValueFlags = map[string]*regexp.Regexp{
	"sudo": regexp.MustCompile(`^-[ugpCDRT]$`),
	"env":  regexp.MustCompile(`^-[uS]$`),
}

var wrapperSet = map[string]bool{"sudo": true, "env": true, "nohup": true, "time": true, "command": true}

func tokenAt(cmd string, pos int) (string, int) {
	for pos < len(cmd) && (cmd[pos] == ' ' || cmd[pos] == '\t') {
		pos++
	}
	end := pos
	for end < len(cmd) && cmd[end] != ' ' && cmd[end] != '\t' {
		end++
	}
	return cmd[pos:end], pos
}

var varPrefixRe = regexp.MustCompile(`^[A-Za-z_]\w*=`)

func skipToCommand(cmd string, pos int) int {
	for {
		tok, at := tokenAt(cmd, pos)
		if tok == "" {
			return at
		}
		if varPrefixRe.MatchString(tok) {
			pos = at + len(tok)
			continue
		}
		if !wrapperSet[tok] {
			return at
		}
		pos = at + len(tok)
		for {
			flag, fat := tokenAt(cmd, pos)
			if len(flag) == 0 || flag[0] != '-' {
				break
			}
			pos = fat + len(flag)
			if re := wrapperValueFlags[tok]; re != nil && re.MatchString(flag) {
				value, vat := tokenAt(cmd, pos)
				pos = vat + len(value)
			}
		}
	}
}

func findTopLevelInvocations(cmd, name string) []int {
	bounds := append([]int{0}, addOne(unquotedSeparators(cmd))...)
	var starts []int
	for _, bound := range bounds {
		pos := skipToCommand(cmd, bound)
		if !strings.HasPrefix(cmd[pos:], name) {
			continue
		}
		if pos+len(name) >= len(cmd) {
			starts = append(starts, bound)
			continue
		}
		after := cmd[pos+len(name)]
		if after == ' ' || after == '\t' || after == '\n' {
			starts = append(starts, bound)
		}
	}
	return starts
}

func addOne(nums []int) []int {
	out := make([]int, len(nums))
	for i, n := range nums {
		out[i] = n + 1
	}
	return out
}

var sshBoolFlagRe = regexp.MustCompile(`^-[46ACfGgKkMNTtVvXxYynaq]+$`)
var sshTokenRe = regexp.MustCompile(`\S+`)

func sshRemoteCommand(afterSsh string) string {
	locs := sshTokenRe.FindAllStringIndex(afterSsh, -1)
	i := 0
	for i < len(locs) && afterSsh[locs[i][0]] == '-' {
		if sshBoolFlagRe.MatchString(afterSsh[locs[i][0]:locs[i][1]]) {
			i++
		} else {
			i += 2
		}
	}
	if i >= len(locs) {
		return ""
	}
	rest := strings.TrimSpace(afterSsh[locs[i][1]:])
	return rest
}

// trackedActivity maps each tracked command name to the entries seen.
type trackedActivity struct {
	byCommand map[string]map[string]bool
	order     map[string][]string
}

func newTrackedActivity(names []string) trackedActivity {
	a := trackedActivity{byCommand: map[string]map[string]bool{}, order: map[string][]string{}}
	for _, n := range names {
		a.byCommand[n] = map[string]bool{}
	}
	return a
}

func (a *trackedActivity) add(name, item string) {
	if item == "" {
		return
	}
	set := a.byCommand[name]
	if set[item] {
		delete(set, item)
		var next []string
		for _, e := range a.order[name] {
			if e != item {
				next = append(next, e)
			}
		}
		a.order[name] = next
	}
	set[item] = true
	a.order[name] = append(a.order[name], item)
}

// ExtractTrackedCommands scans bash calls for configured command names and
// records a one-line snapshot per invocation.
func ExtractTrackedCommands(blocks []Block, trackCommands []string) trackedActivity {
	act := newTrackedActivity(trackCommands)
	if len(trackCommands) == 0 {
		return act
	}
	trackSsh := contains(trackCommands, "ssh")
	for _, b := range blocks {
		if b.Kind != KindToolCall || b.Name != "bash" {
			continue
		}
		cmd, _ := b.Args["command"].(string)
		if cmd == "" {
			continue
		}
		for _, name := range trackCommands {
			for _, startAt := range findTopLevelInvocations(cmd, name) {
				act.add(name, captureEntry(strings.TrimSpace(cmd[startAt:])))
			}
		}
		if trackSsh {
			for _, bound := range findTopLevelInvocations(cmd, "ssh") {
				seg := cmd[bound:]
				remote := sshRemoteCommand(seg[skipToCommand(seg, 0)+3:])
				if remote == "" {
					continue
				}
				unquoted := strings.NewReplacer(`'`, "", `"`, "").Replace(remote)
				for _, name := range trackCommands {
					if name == "ssh" {
						continue
					}
					for _, startAt2 := range findTopLevelInvocations(unquoted, name) {
						act.add(name, captureEntry(strings.TrimSpace(unquoted[startAt2:])))
					}
				}
			}
		}
	}
	return act
}

func contains(items []string, s string) bool {
	for _, item := range items {
		if item == s {
			return true
		}
	}
	return false
}

func renderTrackedEntry(entry string) string {
	line := strings.Join(strings.Fields(entry), " ")
	line = strings.ReplaceAll(line, commandSeparator, ` \| `)
	if r := []rune(line); len(r) > maxTrackedEntryChars {
		return string(r[:maxTrackedEntryChars]) + "\u2026"
	}
	return line
}

// FormatTrackedCommands renders tracked command activity, newest entries kept.
func FormatTrackedCommands(act trackedActivity) []string {
	var lines []string
	for name, set := range act.byCommand {
		if len(set) == 0 {
			continue
		}
		items := make([]string, 0, len(act.order[name]))
		for _, e := range act.order[name] {
			if set[e] {
				items = append(items, renderTrackedEntry(e))
			}
		}
		lines = append(lines, name+": "+capItems(items, commandsPerName, commandSeparator, "tail"))
	}
	return lines
}
