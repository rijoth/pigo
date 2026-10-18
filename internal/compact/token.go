package compact

import (
	"math"
	"strings"

	"pigo/internal/llm"
)

// EstimateMessageChars counts the token-bearing characters of a message,
// mirroring pi-vcc's estimateMessageContentChars (text, reasoning, tool name
// and args, and tool result content).
func EstimateMessageChars(m llm.Message) int {
	chars := len(m.Text)
	for _, r := range m.Reasoning {
		chars += len(r.Text)
	}
	for _, c := range m.Calls {
		chars += len(c.Name)
		if len(c.Args) > 0 {
			chars += len(c.Args)
		}
	}
	for _, r := range m.Results {
		chars += len(r.Content)
	}
	return chars
}

// EstimateTokensFromChars converts a character count to an estimated token
// count at the given chars-per-token ratio.
func EstimateTokensFromChars(chars int, charsPerToken float64) int {
	if charsPerToken <= 0 {
		charsPerToken = defaultCharsPerToken
	}
	return int(math.Ceil(float64(chars) / charsPerToken))
}

// EstimateTokens estimates the tokens in a message slice.
func EstimateTokens(msgs []llm.Message, charsPerToken float64) int {
	total := 0
	for _, m := range msgs {
		total += EstimateMessageChars(m)
	}
	return EstimateTokensFromChars(total, charsPerToken)
}

func joinStrings(parts []string, sep string) string { return strings.Join(parts, sep) }

func splitLines(text string) []string { return strings.Split(text, "\n") }

func joinLines(lines []string) string { return strings.Join(lines, "\n") }
