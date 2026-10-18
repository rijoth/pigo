package compact

import (
	"regexp"
	"strings"
)

var (
	ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)
	ctrlRe = regexp.MustCompile(`[\x00-\x08\x0b\x0c\x0e-\x1f]`)
)

// sanitize normalizes newlines and strips ANSI escapes and control characters.
func sanitize(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = ansiRe.ReplaceAllString(text, "")
	return ctrlRe.ReplaceAllString(text, "")
}
