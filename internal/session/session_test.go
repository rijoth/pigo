package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pigo/internal/llm"
)

func TestAppendAndReplay(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	msgs := []llm.Message{
		{Role: llm.RoleUser, Text: "hi"},
		{Role: llm.RoleAssistant, Text: "hello"},
	}
	for _, m := range msgs {
		if _, err := s.Append(m); err != nil {
			t.Fatal(err)
		}
	}
	path := s.Path()
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	_, history, ids, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 || history[1].Text != "hello" {
		t.Fatalf("history: %+v", history)
	}
	if len(ids) != 2 || ids[0] == "" {
		t.Fatalf("ids: %+v", ids)
	}
}

func TestCorruptTrailingLineSkipped(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	content := `{"type":"message","message":{"role":"user","text":"ok"}}` + "\n" + `{"type":"message","message":` + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	_, history, _, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 || history[0].Text != "ok" {
		t.Fatalf("history: %+v", history)
	}
}

func TestCompactionRecordReplacesHistory(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, m := range []llm.Message{
		{Role: llm.RoleUser, Text: "one"},
		{Role: llm.RoleAssistant, Text: "a"},
		{Role: llm.RoleUser, Text: "two"},
		{Role: llm.RoleAssistant, Text: "b"},
	} {
		id, err := s.Append(m)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	if err := s.AppendCompaction("the summary", ids[2]); err != nil {
		t.Fatal(err)
	}
	path := s.Path()
	s.Close()

	_, history, _, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 3 {
		t.Fatalf("history: %+v", history)
	}
	if history[0].Text != "[Earlier conversation summary]\nthe summary" {
		t.Fatalf("summary message: %+v", history[0])
	}
	if history[1].Text != "two" {
		t.Fatalf("kept message: %+v", history[1])
	}
}

func TestRepeatedCompaction(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	add := func(text string) {
		id, err := s.Append(llm.Message{Role: llm.RoleUser, Text: text})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	add("one")
	add("two")
	add("three")
	if err := s.AppendCompaction("summary one", ids[1]); err != nil {
		t.Fatal(err)
	}
	add("four")
	if err := s.AppendCompaction("summary two", ids[2]); err != nil {
		t.Fatal(err)
	}
	path := s.Path()
	s.Close()

	_, history, _, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 3 {
		t.Fatalf("history: %+v", history)
	}
	if history[0].Text != "[Earlier conversation summary]\nsummary two" {
		t.Fatalf("summary message: %+v", history[0])
	}
	if history[1].Text != "three" || history[2].Text != "four" {
		t.Fatalf("kept messages: %+v", history[1:])
	}
}

func TestCompactionUnknownBoundaryKeepsHistory(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []llm.Message{
		{Role: llm.RoleUser, Text: "one"},
		{Role: llm.RoleAssistant, Text: "a"},
	} {
		if _, err := s.Append(m); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.AppendCompaction("ignored", "does-not-exist"); err != nil {
		t.Fatal(err)
	}
	path := s.Path()
	s.Close()

	_, history, _, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 || history[0].Text != "one" {
		t.Fatalf("history: %+v", history)
	}
}

func TestLegacyReplacesStillReadable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	content := `{"type":"message","message":{"role":"user","text":"one"}}` + "\n" +
		`{"type":"message","message":{"role":"assistant","text":"a"}}` + "\n" +
		`{"type":"message","message":{"role":"user","text":"two"}}` + "\n" +
		`{"type":"compaction","summary":"old","replaces":2}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	_, history, _, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 || history[0].Text != "[Earlier conversation summary]\nold" || history[1].Text != "two" {
		t.Fatalf("history: %+v", history)
	}
}

func TestList(t *testing.T) {
	dir := t.TempDir()
	if _, err := List(dir); err != nil {
		t.Fatalf("empty dir: %v", err)
	}
	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Append(llm.Message{Role: llm.RoleUser, Text: "first question"}); err != nil {
		t.Fatal(err)
	}
	s.Close()
	infos, err := List(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(infos) != 1 || infos[0].Messages != 1 || infos[0].FirstUser != "first question" {
		t.Fatalf("infos: %+v", infos)
	}
}

func TestLatest(t *testing.T) {
	dir := t.TempDir()
	if _, err := Latest(dir); err == nil {
		t.Fatal("expected error for empty dir")
	}
	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, err := Latest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != s.Path() {
		t.Fatalf("Latest = %q, want %q", got, s.Path())
	}
}

func TestTreeBranchNavigation(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	appendMsg := func(role llm.Role, text string) string {
		id, err := s.Append(llm.Message{Role: role, Text: text})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	appendMsg(llm.RoleUser, "one")
	id1 := appendMsg(llm.RoleAssistant, "a")
	appendMsg(llm.RoleUser, "two")
	id3 := appendMsg(llm.RoleAssistant, "b")

	history, _, err := s.SetLeaf(id1)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 || history[1].Text != "a" {
		t.Fatalf("branch history: %+v", history)
	}
	appendMsg(llm.RoleUser, "alt")
	path := s.Path()
	s.Close()

	_, replayed, _, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(replayed) != 3 || replayed[2].Text != "alt" {
		t.Fatalf("replayed branch: %+v", replayed)
	}

	tr, err := parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(tr.Nodes) != 5 {
		t.Fatalf("nodes: %d", len(tr.Nodes))
	}
	abandoned := tr.Abandoned(id3, tr.Leaf)
	if len(abandoned) != 2 || abandoned[0].Text != "two" || abandoned[1].Text != "b" {
		t.Fatalf("abandoned: %+v", abandoned)
	}
	var inactive int
	for _, e := range tr.Entries() {
		if !e.Active {
			inactive++
		}
	}
	if inactive != 2 {
		t.Fatalf("inactive entries: %d", inactive)
	}
}

func TestBranchSummaryRecord(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	id0, _ := s.Append(llm.Message{Role: llm.RoleUser, Text: "one"})
	_, _ = s.Append(llm.Message{Role: llm.RoleAssistant, Text: "a"})
	_, _ = s.Append(llm.Message{Role: llm.RoleUser, Text: "two"})

	if _, _, err := s.SetLeaf(id0); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendBranchSummary(id0, "left branch explored two"); err != nil {
		t.Fatal(err)
	}
	path := s.Path()
	s.Close()

	_, history, _, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 {
		t.Fatalf("history: %+v", history)
	}
	if history[1].Text != "[Branch summary]\nleft branch explored two" {
		t.Fatalf("branch summary: %+v", history[1])
	}
}

func TestSetLeafSkipsDanglingToolCall(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	userID, _ := s.Append(llm.Message{Role: llm.RoleUser, Text: "do it"})
	asstID, _ := s.Append(llm.Message{Role: llm.RoleAssistant, Calls: []llm.ToolCall{{ID: "c1", Name: "bash"}}})
	_, _ = s.Append(llm.Message{Role: llm.RoleTool, Results: []llm.ToolResult{{CallID: "c1", Content: "ok"}}})

	history, _, err := s.SetLeaf(asstID)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 || history[0].Text != "do it" {
		t.Fatalf("history: %+v", history)
	}
	if s.Leaf() != userID {
		t.Fatalf("leaf = %q, want %q", s.Leaf(), userID)
	}
}

func TestOpenSkipsDanglingToolCallLeaf(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	content := `{"type":"message","id":"u1","message":{"role":"user","text":"do it"}}` + "\n" +
		`{"type":"message","id":"a1","parent_id":"u1","message":{"role":"assistant","calls":[{"id":"c1","name":"bash"}]}}` + "\n" +
		`{"type":"message","id":"t1","parent_id":"a1","message":{"role":"tool","results":[{"call_id":"c1","content":"ok"}]}}` + "\n" +
		`{"type":"leaf","leaf_id":"a1"}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	s, history, _, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if len(history) != 1 || history[0].Text != "do it" {
		t.Fatalf("history: %+v", history)
	}
	if s.Leaf() != "u1" {
		t.Fatalf("leaf = %q, want u1", s.Leaf())
	}
}

func TestProjectKeyDistinguishesPaths(t *testing.T) {
	if projectKey("/a/b-c") == projectKey("/a-b/c") {
		t.Fatal("distinct paths produced the same key")
	}
	if projectKey("/a/b") != projectKey("/a/b") {
		t.Fatal("key is not deterministic")
	}
	if !strings.HasPrefix(projectKey("/a/b"), "a-b-") {
		t.Fatalf("key is not readable: %q", projectKey("/a/b"))
	}
}

func TestProjectDirScopedByWorkingDirectory(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)

	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })

	one, two := t.TempDir(), t.TempDir()
	if err := os.Chdir(one); err != nil {
		t.Fatal(err)
	}
	dirOne := ProjectDir()
	if err := os.Chdir(two); err != nil {
		t.Fatal(err)
	}
	dirTwo := ProjectDir()
	if dirOne == dirTwo {
		t.Fatalf("project dirs collided: %q", dirOne)
	}
	if !strings.HasPrefix(dirOne, filepath.Join(state, "pigo", "sessions")) {
		t.Fatalf("dir %q is not under the state dir", dirOne)
	}

	s, err := New(dirOne)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Append(llm.Message{Role: llm.RoleUser, Text: "one"}); err != nil {
		t.Fatal(err)
	}
	s.Close()

	if _, err := Latest(dirTwo); err == nil {
		t.Fatal("sessions leaked into another project")
	}
	if latest, err := Latest(dirOne); err != nil || latest != s.Path() {
		t.Fatalf("Latest(one) = %q, %v", latest, err)
	}
}

func TestResolve(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()

	if got, err := Resolve(dir, s.ID()); err != nil || got != s.Path() {
		t.Fatalf("exact id: got %q, %v", got, err)
	}
	if got, err := Resolve(dir, s.ID()[:4]); err != nil || got != s.Path() {
		t.Fatalf("id prefix: got %q, %v", got, err)
	}
	if _, err := Resolve(dir, "does-not-exist"); err == nil {
		t.Fatal("expected an error for a missing session")
	}

	other := filepath.Join(t.TempDir(), "other.jsonl")
	if err := os.WriteFile(other, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := Resolve(dir, other); err != nil || got != other {
		t.Fatalf("explicit path: got %q, %v", got, err)
	}
}

func TestModelRefPersist(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	if got := LoadModelRef(); got != "" {
		t.Fatalf("missing file: got %q, want empty", got)
	}
	SaveModelRef("openai/gpt-5")
	if got := LoadModelRef(); got != "openai/gpt-5" {
		t.Fatalf("got %q, want openai/gpt-5", got)
	}
	SaveModelRef("none")
	if got := LoadModelRef(); got != "openai/gpt-5" {
		t.Fatalf("empty ref overwrote: got %q", got)
	}
}

func TestThinkingLevelPersist(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	if got := LoadThinkingLevel(); got != "" {
		t.Fatalf("missing file: got %q, want empty", got)
	}
	SaveThinkingLevel("high")
	if got := LoadThinkingLevel(); got != "high" {
		t.Fatalf("got %q, want high", got)
	}
}
