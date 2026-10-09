// Package tools contains the four core capabilities exposed to the model.
// It depends on the standard library only.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Spec describes a tool to the model.
type Spec struct {
	Name        string
	Description string
	Schema      json.RawMessage
}

// Tool is one callable capability.
type Tool interface {
	Spec() Spec
	Run(ctx context.Context, args json.RawMessage) (string, error)
}

// Default returns the four core tools keyed by name.
func Default() map[string]Tool {
	return map[string]Tool{
		"read":  Read{},
		"write": Write{},
		"edit":  Edit{},
		"bash":  Bash{},
	}
}

// isBinary reports whether data looks like it contains binary content.
func isBinary(data []byte) bool {
	if len(data) > 8000 {
		data = data[:8000]
	}
	return strings.IndexByte(string(data), 0) >= 0
}

// truncateTail keeps the last max bytes, since command errors usually come last.
func truncateTail(s string, max int) string {
	if len(s) <= max {
		return s
	}
	start := len(s) - max
	// Do not split a UTF-8 rune or a line.
	for start < len(s) && !utf8.RuneStart(s[start]) {
		start++
	}
	if i := strings.IndexByte(s[start:], '\n'); i >= 0 && i < len(s[start:])-1 {
		start += i + 1
	}
	return fmt.Sprintf("[output truncated: showing last %d of %d bytes]\n%s", len(s)-start, len(s), s[start:])
}
