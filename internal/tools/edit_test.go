package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runEdit(t *testing.T, path, old, new string) (string, error) {
	t.Helper()
	args, err := json.Marshal(editArgs{Path: path, Old: old, New: new})
	if err != nil {
		t.Fatal(err)
	}
	return Edit{}.Run(context.Background(), args)
}

func TestEditSingleMatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("hello world\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := runEdit(t, path, "world", "there"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "hello there\n" {
		t.Fatalf("content = %q", data)
	}
}

func TestEditZeroMatches(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	os.WriteFile(path, []byte("hello\n"), 0o644)
	_, err := runEdit(t, path, "missing", "x")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("err = %v", err)
	}
}

func TestEditMultipleMatches(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	os.WriteFile(path, []byte("a a a\n"), 0o644)
	_, err := runEdit(t, path, "a", "b")
	if err == nil || !strings.Contains(err.Error(), "3 times") {
		t.Fatalf("err = %v", err)
	}
}
