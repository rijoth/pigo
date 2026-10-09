package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

const editDescription = `Replace text in a file. The old string must appear exactly once. This is a literal match, not a regex.`

var editSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "path": {"type": "string", "description": "File path, relative to the working directory."},
    "old":  {"type": "string", "description": "Exact text to replace. Must occur exactly once."},
    "new":  {"type": "string", "description": "Replacement text."}
  },
  "required": ["path", "old", "new"]
}`)

// Edit performs an exact, unique string replacement in a file.
type Edit struct{}

// Spec returns the model-visible tool description.
func (Edit) Spec() Spec {
	return Spec{Name: "edit", Description: editDescription, Schema: editSchema}
}

type editArgs struct {
	Path string `json:"path"`
	Old  string `json:"old"`
	New  string `json:"new"`
}

// Run implements Tool.
func (Edit) Run(_ context.Context, args json.RawMessage) (string, error) {
	var a editArgs
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
	content := string(data)
	switch n := strings.Count(content, a.Old); {
	case n == 0:
		return "", fmt.Errorf("old text not found in %s", a.Path)
	case n > 1:
		return "", fmt.Errorf("old text occurs %d times in %s; it must be unique", n, a.Path)
	}
	updated := strings.Replace(content, a.Old, a.New, 1)
	if err := os.WriteFile(a.Path, []byte(updated), 0o644); err != nil {
		return "", err
	}
	return fmt.Sprintf("edited %s", a.Path), nil
}
