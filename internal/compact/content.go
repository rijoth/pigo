package compact

import (
	"regexp"
	"strings"
)

// clip shortens text to at most max characters, preferring a word boundary.
func clip(text string, max int) string {
	r := []rune(text)
	if len(r) <= max {
		return text
	}
	cut := -1
	for i := max; i >= 0; i-- {
		if r[i] == ' ' {
			cut = i
			break
		}
	}
	end := max
	if cut > int(float64(max)*0.6) {
		end = cut
	}
	return string(r[:end])
}

// clipSentence clips to the last sentence boundary at or before max, falling
// back to a word boundary. Trailing whitespace is left in place, as in pi-vcc.
func clipSentence(text string, max int) string {
	r := []rune(text)
	if len(r) <= max {
		return text
	}
	last := -1
	for i := 0; i < max; i++ {
		if r[i] != '.' && r[i] != '!' && r[i] != '?' {
			continue
		}
		if i+1 == max || (i+1 < len(r) && isSpace(r[i+1])) {
			last = i
		}
	}
	if last >= 0 {
		end := last + 1
		if float64(end) >= float64(max)*0.5 {
			return string(r[:end])
		}
	}
	return clip(text, max)
}

func isSpace(r rune) bool {
	switch r {
	case ' ', '\t', '\n', '\r', '\v', '\f':
		return true
	}
	return false
}

// nonEmptyLines splits text into trimmed, non-empty lines.
func nonEmptyLines(text string) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, line)
		}
	}
	return out
}

// capItems joins items, capping at limit. "head" keeps the oldest and appends
// "(+N more)"; "tail" keeps the newest and prefixes "(+N earlier)".
func capItems(items []string, limit int, joinWith, keep string) string {
	if len(items) <= limit {
		return strings.Join(items, joinWith)
	}
	over := len(items) - limit
	if keep == "tail" {
		return "(+" + itoa(over) + " earlier) " + strings.Join(items[len(items)-limit:], joinWith)
	}
	return strings.Join(items[:limit], joinWith) + " (+" + itoa(over) + " more)"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

var (
	capEarlierPrefix = regexp.MustCompile(`^\s*\(\+\d+ earlier\)\s*`)
	capSuffix        = regexp.MustCompile(`\s*\(\+\d+ (?:more|earlier)\)\s*$`)
)

// stripCapMarker removes a cap annotation from a categorized line.
func stripCapMarker(text string) string {
	text = capEarlierPrefix.ReplaceAllString(text, "")
	return capSuffix.ReplaceAllString(text, "")
}
