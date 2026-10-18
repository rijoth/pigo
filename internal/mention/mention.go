// Package mention expands @path references in a prompt to file contents.
package mention

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"pigo/internal/config"
)

// openTag begins an expanded file block and closeTag ends it. Collapse reads
// both back, so keep their shape in sync with render.
const (
	openTag  = "<file path="
	closeTag = "</file>"
)

// Expand replaces each @path token in text with a file block containing the
// file's contents. A mention is an @ at the start of the text or after
// whitespace, followed by a non-empty run of non-space characters. A token that
// does not name a readable text file is left unchanged, so prose such as an
// email address is untouched.
func Expand(text string) string {
	if !strings.Contains(text, "@") {
		return text
	}
	var b strings.Builder
	i := 0
	for i < len(text) {
		if text[i] != '@' || (i > 0 && !isSpace(text[i-1])) {
			b.WriteByte(text[i])
			i++
			continue
		}
		j := i + 1
		for j < len(text) && !isSpace(text[j]) {
			j++
		}
		if j == i+1 {
			b.WriteByte(text[i])
			i++
			continue
		}
		block, suffix, ok := load(text[i+1 : j])
		if !ok {
			b.WriteByte(text[i])
			i++
			continue
		}
		b.WriteString(block)
		b.WriteString(suffix)
		i = j
	}
	return b.String()
}

// Collapse replaces each file block produced by Expand with its @path mention,
// so a restored session shows the user's original text instead of the expanded
// contents. Text outside blocks is unchanged.
func Collapse(text string) string {
	var b strings.Builder
	for {
		i := strings.Index(text, openTag)
		if i < 0 {
			b.WriteString(text)
			return b.String()
		}
		rest := text[i:]
		gt := strings.IndexByte(rest, '>')
		cl := strings.Index(rest, closeTag)
		if gt < 0 || cl < 0 || cl < gt {
			b.WriteString(text)
			return b.String()
		}
		b.WriteString(text[:i])
		path, err := strconv.Unquote(rest[len(openTag):gt])
		if err != nil {
			b.WriteString(rest[:cl+len(closeTag)])
		} else {
			b.WriteString("@" + path)
		}
		text = rest[cl+len(closeTag):]
	}
}

// load renders the file block for token. When token does not name a file, it
// retries without trailing prose punctuation, returning the stripped suffix so
// the caller can keep it in the text.
func load(token string) (block, suffix string, ok bool) {
	if block, ok := render(token); ok {
		return block, "", true
	}
	trimmed := strings.TrimRight(token, ".,;:!?)]}\"'")
	if trimmed == "" || trimmed == token {
		return "", "", false
	}
	if block, ok := render(trimmed); ok {
		return block, token[len(trimmed):], true
	}
	return "", "", false
}

// render reads one path and wraps its contents in a file block. It reports
// false for a missing path, a directory, or a binary file.
func render(path string) (string, bool) {
	info, err := os.Stat(expandHome(path))
	if err != nil || info.IsDir() {
		return "", false
	}
	data, err := os.ReadFile(expandHome(path))
	if err != nil || looksBinary(data) {
		return "", false
	}
	body := strings.TrimSuffix(string(data), "\n")
	var b strings.Builder
	fmt.Fprintf(&b, "%s%q>\n", openTag, path)
	if len(body) > config.MaxReadBytes {
		total := len(body)
		body = trimRune(body, config.MaxReadBytes)
		b.WriteString(body)
		fmt.Fprintf(&b, "\n[truncated: showing first %d of %d bytes]", len(body), total)
	} else {
		b.WriteString(body)
	}
	b.WriteString("\n" + closeTag)
	return b.String(), true
}

// expandHome resolves a leading ~ to the user's home directory.
func expandHome(path string) string {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, strings.TrimPrefix(path, "~"))
}

// trimRune cuts s to at most n bytes without splitting a UTF-8 rune.
func trimRune(s string, n int) string {
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

func looksBinary(data []byte) bool {
	if len(data) > 8000 {
		data = data[:8000]
	}
	return bytes.IndexByte(data, 0) >= 0
}

func isSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}
