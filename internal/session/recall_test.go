package session

import (
	"encoding/json"
	"strings"
	"testing"

	"pigo/internal/llm"
)

func TestRecallSearchAndExpand(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.Append(llm.Message{Role: llm.RoleUser, Text: "we should use redis for the cache"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Append(llm.Message{Role: llm.RoleAssistant, Text: "agreed, adding redis"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Append(llm.Message{Role: llm.RoleUser, Text: "unrelated chatter"}); err != nil {
		t.Fatal(err)
	}

	got, err := s.Recall(json.RawMessage(`{"query":"redis"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "#0 user") || !strings.Contains(got, "#1 assistant") {
		t.Fatalf("search misses entries: %q", got)
	}
	if strings.Contains(got, "#2") {
		t.Fatalf("search returned a non-match: %q", got)
	}

	full, err := s.Recall(json.RawMessage(`{"ref":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(full, "#1 assistant") || !strings.Contains(full, "adding redis") {
		t.Fatalf("expand: %q", full)
	}
}

func TestRecallTouched(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.Append(llm.Message{Role: llm.RoleUser, Text: "edit auth"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Append(llm.Message{Role: llm.RoleAssistant, Calls: []llm.ToolCall{
		{ID: "1", Name: "edit", Args: json.RawMessage(`{"path":"auth.go"}`)},
	}}); err != nil {
		t.Fatal(err)
	}

	got, err := s.Recall(json.RawMessage(`{"mode":"touched"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "auth.go") || !strings.Contains(got, "#1") {
		t.Fatalf("touched: %q", got)
	}
}

func TestRecallScope(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	id0, _ := s.Append(llm.Message{Role: llm.RoleUser, Text: "keep this"})
	if _, err := s.Append(llm.Message{Role: llm.RoleAssistant, Text: "ok"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Append(llm.Message{Role: llm.RoleUser, Text: "abandoned secret"}); err != nil {
		t.Fatal(err)
	}
	// Switch the active branch back so "abandoned secret" is off the lineage.
	if _, _, err := s.SetLeaf(id0); err != nil {
		t.Fatal(err)
	}

	lineage, err := s.Recall(json.RawMessage(`{"query":"secret"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(lineage, "no entries matched") {
		t.Fatalf("lineage should not see abandoned branch: %q", lineage)
	}
	all, err := s.Recall(json.RawMessage(`{"query":"secret","scope":"all"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(all, "abandoned secret") {
		t.Fatalf("scope all should see abandoned branch: %q", all)
	}
}

func TestRecallEmptySession(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, err := s.Recall(nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "no prior messages") {
		t.Fatalf("empty session: %q", got)
	}
}
