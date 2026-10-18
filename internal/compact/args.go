package compact

import (
	"regexp"
	"strings"
)

var (
	keepPrefixRe = regexp.MustCompile(`^keep:(\d+)(?:\s+|$)([\s\S]*)$`)
	keepTokenRe  = regexp.MustCompile(`^keep:(\d+)$`)
)

// ParseArgs parses a /compact argument. keep:N may appear leading or trailing;
// any remaining text is a follow-up prompt. keep defaults to 1.
func ParseArgs(arg string) (keep int, explicit bool, prompt string) {
	trimmed := strings.TrimSpace(arg)
	if trimmed == "" {
		return 1, false, ""
	}
	if m := keepPrefixRe.FindStringSubmatch(trimmed); m != nil {
		return parseKeep(m[1]), true, strings.TrimSpace(m[2])
	}
	parts := strings.Fields(trimmed)
	last := parts[len(parts)-1]
	if m := keepTokenRe.FindStringSubmatch(last); m != nil {
		return parseKeep(m[1]), true, strings.TrimSpace(strings.TrimSuffix(trimmed, last))
	}
	return 1, false, trimmed
}

func parseKeep(raw string) int {
	n := 0
	for _, c := range raw {
		if c < '0' || c > '9' {
			return 1
		}
		n = n*10 + int(c-'0')
		if n > 1<<30 {
			return 1 << 30
		}
	}
	return n
}
