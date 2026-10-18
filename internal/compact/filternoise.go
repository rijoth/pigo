package compact

import (
	"regexp"
	"strings"
)

var noiseTools = map[string]bool{
	"TodoWrite":     true,
	"TodoRead":      true,
	"ToolSearch":    true,
	"WebSearch":     true,
	"AskUser":       true,
	"ExitSpecMode":  true,
	"GenerateDroid": true,
}

var noiseStrings = []string{
	"Continue from where you left off.",
	"No response requested.",
	"IMPORTANT: TodoWrite was not called yet.",
	// pigo injects these synthetic user turns; never re-summarize them.
	"[Earlier conversation summary]",
	"[Branch summary]",
}

var xmlWrapperRes = []*regexp.Regexp{
	regexp.MustCompile(`(?s)<system-reminder[^>]*>.*?</system-reminder>`),
	regexp.MustCompile(`(?s)<ide_opened_file[^>]*>.*?</ide_opened_file>`),
	regexp.MustCompile(`(?s)<command-message[^>]*>.*?</command-message>`),
	regexp.MustCompile(`(?s)<context-window-usage[^>]*>.*?</context-window-usage>`),
}

func stripXMLWrappers(text string) string {
	for _, re := range xmlWrapperRes {
		text = re.ReplaceAllString(text, "")
	}
	return text
}

func isNoiseUserBlock(text string) bool {
	trimmed := strings.TrimSpace(text)
	for _, s := range noiseStrings {
		if strings.Contains(trimmed, s) {
			return true
		}
	}
	return strings.TrimSpace(stripXMLWrappers(trimmed)) == ""
}

func cleanUserText(text string) string {
	return strings.TrimSpace(stripXMLWrappers(text))
}

// FilterNoise drops noise tool calls, wrapper-only user turns, and noise
// strings, returning the blocks that carry durable context.
func FilterNoise(blocks []Block) []Block {
	out := make([]Block, 0, len(blocks))
	for _, b := range blocks {
		if b.Kind == KindToolCall && noiseTools[b.Name] {
			continue
		}
		if b.Kind == KindToolResult && noiseTools[b.Name] {
			continue
		}
		if b.Kind == KindUser {
			if isNoiseUserBlock(b.Text) {
				continue
			}
			cleaned := cleanUserText(b.Text)
			if cleaned == "" {
				continue
			}
			b.Text = cleaned
		}
		out = append(out, b)
	}
	return out
}
