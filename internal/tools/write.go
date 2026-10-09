package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const writeDescription = `Create or overwrite a file with the given content. Parent directories are created as needed.`

var writeSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "path":    {"type": "string", "description": "File path, relative to the working directory."},
    "content": {"type": "string", "description": "Full file contents to write."}
  },
  "required": ["path", "content"]
}`)

// Write creates or overwrites a file.
type Write struct{}

// Spec returns the model-visible tool description.
func (Write) Spec() Spec {
	return Spec{Name: "write", Description: writeDescription, Schema: writeSchema}
}

type writeArgs struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// Run implements Tool.
func (Write) Run(_ context.Context, args json.RawMessage) (string, error) {
	var a writeArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	if a.Path == "" {
		return "", fmt.Errorf("path is required")
	}
	if dir := filepath.Dir(a.Path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", err
		}
	}
	if err := os.WriteFile(a.Path, []byte(a.Content), 0o644); err != nil {
		return "", err
	}
	return fmt.Sprintf("wrote %d bytes to %s", len(a.Content), a.Path), nil
}
