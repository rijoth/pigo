package compact

import (
	"encoding/json"
	"strings"
	"testing"

	"pigo/internal/llm"
)

func TestParseArgs(t *testing.T) {
	cases := []struct {
		arg      string
		keep     int
		explicit bool
		prompt   string
	}{
		{"", 1, false, ""},
		{"do the thing", 1, false, "do the thing"},
		{"keep:2", 2, true, ""},
		{"keep:3 do the thing", 3, true, "do the thing"},
		{"do the thing keep:4", 4, true, "do the thing"},
	}
	for _, tc := range cases {
		keep, explicit, prompt := ParseArgs(tc.arg)
		if keep != tc.keep || explicit != tc.explicit || prompt != tc.prompt {
			t.Errorf("ParseArgs(%q) = (%d, %v, %q), want (%d, %v, %q)",
				tc.arg, keep, explicit, prompt, tc.keep, tc.explicit, tc.prompt)
		}
	}
}

func TestCompileSections(t *testing.T) {
	args, _ := json.Marshal(map[string]string{"path": "auth.go"})
	msgs := []llm.Message{
		{Role: llm.RoleUser, Text: "Fix the login bug in auth.go"},
		{Role: llm.RoleAssistant, Text: "Looking at auth.go now."},
		{Role: llm.RoleAssistant, Calls: []llm.ToolCall{{ID: "1", Name: "edit", Args: args}}},
		{Role: llm.RoleTool, Results: []llm.ToolResult{{CallID: "1", Content: "ok"}}},
		{Role: llm.RoleUser, Text: "Tests still fail"},
	}
	got := Compile(Input{History: msgs, Cut: len(msgs)})
	if got == "" {
		t.Fatal("empty summary")
	}
	if !strings.Contains(got, "[Session Goal]") {
		t.Fatalf("missing goal section: %q", got)
	}
	if !strings.Contains(got, "Modified: auth.go") {
		t.Fatalf("missing file activity: %q", got)
	}
	if !strings.Contains(got, "[user]") {
		t.Fatalf("missing brief transcript: %q", got)
	}
	if !strings.Contains(got, "vcc_recall") {
		t.Fatalf("missing recall note: %q", got)
	}
	if strings.Index(got, "[Session Goal]") > strings.Index(got, "[Files And Changes]") {
		t.Fatalf("section order wrong: %q", got)
	}
}

func TestMergePreviousModifiedWinsOverCreated(t *testing.T) {
	prev := "[Files And Changes]\n- Created: a.go\n\n---\n\n[user]\nold"
	fresh := "[Files And Changes]\n- Modified: a.go\n\n---\n\n[user]\nnew"
	merged := mergePrevious(prev, fresh, false)
	if strings.Contains(merged, "Created: a.go") {
		t.Fatalf("Created not dropped when Modified: %q", merged)
	}
	if !strings.Contains(merged, "Modified: a.go") {
		t.Fatalf("Modified missing: %q", merged)
	}
}

func TestStripRecallNote(t *testing.T) {
	stored := WrapLongLines("body text"+summarySeparator+recallNote, 0)
	got := stripRecallNote(stored)
	if strings.Contains(got, "vcc_recall") {
		t.Fatalf("recall note not stripped: %q", got)
	}
	if !strings.HasPrefix(got, "body text") {
		t.Fatalf("body damaged: %q", got)
	}
}

func TestResolveCutKeepZeroCompactsAll(t *testing.T) {
	live := []llm.Message{
		{Role: llm.RoleUser, Text: "one"},
		{Role: llm.RoleAssistant, Text: "a"},
		{Role: llm.RoleUser, Text: "two"},
		{Role: llm.RoleAssistant, Text: "b"},
	}
	cut, ok := ResolveCut(live, Options{Keep: 0, Explicit: true})
	if !ok || !cut.CompactAll {
		t.Fatalf("cut = %+v ok=%v, want compact-all", cut, ok)
	}
	if cut.FirstKept != len(live) {
		t.Fatalf("FirstKept = %d, want %d", cut.FirstKept, len(live))
	}
}

func TestResolveCutKeepTurns(t *testing.T) {
	live := []llm.Message{
		{Role: llm.RoleUser, Text: "one"},
		{Role: llm.RoleAssistant, Text: "a"},
		{Role: llm.RoleUser, Text: "two"},
		{Role: llm.RoleAssistant, Text: "b"},
		{Role: llm.RoleUser, Text: "three"},
		{Role: llm.RoleAssistant, Text: "c"},
	}
	cut, ok := ResolveCut(live, Options{Keep: 2, Explicit: true})
	if !ok || cut.CompactAll {
		t.Fatalf("cut = %+v ok=%v", cut, ok)
	}
	if cut.FirstKept != 2 {
		t.Fatalf("FirstKept = %d, want 2", cut.FirstKept)
	}
	if cut.KeptTurns != 2 || cut.TotalTurns != 3 {
		t.Fatalf("turns = %d/%d, want 2/3", cut.KeptTurns, cut.TotalTurns)
	}
}

func TestResolveCutKeepExceedsTurns(t *testing.T) {
	live := []llm.Message{
		{Role: llm.RoleUser, Text: "one"},
		{Role: llm.RoleAssistant, Text: "a"},
		{Role: llm.RoleUser, Text: "two"},
	}
	cut, ok := ResolveCut(live, Options{Keep: 9, Explicit: true})
	if !ok || !cut.CompactAll {
		t.Fatalf("cut = %+v ok=%v, want compact-all", cut, ok)
	}
}

func TestResolveCutBudgetCut(t *testing.T) {
	big := strings.Repeat("word ", 500)
	live := []llm.Message{
		{Role: llm.RoleUser, Text: "one"},
		{Role: llm.RoleAssistant, Text: big},
		{Role: llm.RoleUser, Text: "two"},
		{Role: llm.RoleAssistant, Text: big},
	}
	cut, ok := ResolveCut(live, Options{
		Keep:               1,
		Explicit:           false,
		SmartKeep:          false,
		MaxSmartTailTokens: 10,
		CharsPerToken:      4,
	})
	if !ok {
		t.Fatal("not ok")
	}
	if cut.CompactAll {
		t.Fatalf("expected budget cut, got compact-all: %+v", cut)
	}
	if cut.BudgetCut == "" {
		t.Fatalf("expected BudgetCut set: %+v", cut)
	}
}

func TestCompactCompactAllPersists(t *testing.T) {
	live := []llm.Message{
		{Role: llm.RoleUser, Text: "one"},
		{Role: llm.RoleAssistant, Text: "a"},
		{Role: llm.RoleUser, Text: "two"},
		{Role: llm.RoleAssistant, Text: "b"},
	}
	ids := []string{"1", "2", "3", "4"}
	newHistory, res, err := Compact(live, ids, 0, true, "", nil, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK || !res.CompactAll {
		t.Fatalf("res = %+v", res)
	}
	if len(newHistory) != 1 || !strings.HasPrefix(newHistory[0].Text, SummaryPrefix) {
		t.Fatalf("new history: %+v", newHistory)
	}
	if res.FirstKept != len(live) {
		t.Fatalf("FirstKept = %d, want %d", res.FirstKept, len(live))
	}
}
