package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"pigo/internal/config"
)

const readDescription = `Read a text file. Returns the contents with line numbers. Use offset and limit to read a slice of a large file.`

var readSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "path":   {"type": "string",  "description": "File path, relative to the working directory."},
    "offset": {"type": "integer", "description": "First line to read (1-based)."},
    "limit":  {"type": "integer", "description": "Maximum number of lines to read."}
  },
  "required": ["path"]
}`)

// Read returns file contents with line numbers.
type Read struct{}

// Spec returns the model-visible tool description.
func (Read) Spec() Spec {
	return Spec{Name: "read", Description: readDescription, Schema: readSchema}
}

type readArgs struct {
	Path   string `json:"path"`
	Offset int    `json:"offset"`
	Limit  int    `json:"limit"`
}

// Run implements Tool.
func (Read) Run(_ context.Context, args json.RawMessage) (string, error) {
	var a readArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	if a.Path == "" {
		return "", fmt.Errorf("path is required")
	}
	data, err := os.ReadFile(a.Path)
	if err != nil {
		return "", err
	}
	if isBinary(data) {
		return "", fmt.Errorf("refusing to read %s: binary file", a.Path)
	}
	text := strings.TrimSuffix(string(data), "\n")
	var lines []string
	if text != "" {
		lines = strings.Split(text, "\n")
	}
	if len(lines) == 0 {
		return "(empty file)", nil
	}
	start := a.Offset
	if start < 1 {
		start = 1
	}
	if start > len(lines) {
		return fmt.Sprintf("(file has %d lines; offset %d is past the end)", len(lines), start), nil
	}
	end := len(lines)
	if a.Limit > 0 && start-1+a.Limit < end {
		end = start - 1 + a.Limit
	}
	var b strings.Builder
	for i := start; i <= end; i++ {
		line := fmt.Sprintf("%6d\t%s\n", i, lines[i-1])
		if b.Len()+len(line) > config.MaxReadBytes {
			fmt.Fprintf(&b, "[output truncated: showing lines %d-%d of %d; continue with offset=%d]\n", start, i-1, len(lines), i)
			return b.String(), nil
		}
		b.WriteString(line)
	}
	return b.String(), nil
}
