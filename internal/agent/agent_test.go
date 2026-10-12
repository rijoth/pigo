package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"pigo/internal/event"
	"pigo/internal/llm"
	"pigo/internal/tools"
)

type fakeProvider struct {
	script [][]llm.Delta
	idx    int
}

func (f *fakeProvider) Stream(_ context.Context, _ llm.Request) (<-chan llm.Delta, error) {
	ch := make(chan llm.Delta)
	var deltas []llm.Delta
	if f.idx < len(f.script) {
		deltas = f.script[f.idx]
	}
	f.idx++
	go func() {
		defer close(ch)
		for _, d := range deltas {
			ch <- d
		}
	}()
	return ch, nil
}

type funcTool struct {
	spec tools.Spec
	fn   func(ctx context.Context, args json.RawMessage) (string, error)
}

func (t funcTool) Spec() tools.Spec { return t.spec }
func (t funcTool) Run(ctx context.Context, args json.RawMessage) (string, error) {
	return t.fn(ctx, args)
}

func toolCall(id, name string) llm.Delta {
	return llm.Delta{Call: &llm.ToolCall{ID: id, Name: name, Args: json.RawMessage(`{}`)}}
}

func collect(events *[]event.Event) func(event.Event) {
	return func(e event.Event) { *events = append(*events, e) }
}

func TestRunNoTools(t *testing.T) {
	p := &fakeProvider{script: [][]llm.Delta{{{Text: "hello"}}}}
	a := &Agent{Provider: p, Tools: map[string]tools.Tool{}, MaxSteps: 5}
	var events []event.Event
	history, err := a.Run(context.Background(), nil, "hi", collect(&events))
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 || history[1].Text != "hello" {
		t.Fatalf("unexpected history: %+v", history)
	}
	var text, end bool
	for _, e := range events {
		text = text || e.Kind == event.TextDelta
		end = end || e.Kind == event.TurnEnd
	}
	if !text || !end {
		t.Fatalf("missing events: %+v", events)
	}
}

func TestRunOneToolCall(t *testing.T) {
	p := &fakeProvider{script: [][]llm.Delta{
		{toolCall("1", "echo"), {Usage: &llm.Usage{InputTokens: 10}}},
		{{Text: "done"}},
	}}
	a := &Agent{Provider: p, MaxSteps: 5, Tools: map[string]tools.Tool{
		"echo": funcTool{spec: tools.Spec{Name: "echo"}, fn: func(context.Context, json.RawMessage) (string, error) {
			return "ok", nil
		}},
	}}
	var events []event.Event
	history, err := a.Run(context.Background(), nil, "hi", collect(&events))
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 4 {
		t.Fatalf("want 4 messages, got %d: %+v", len(history), history)
	}
	if got := history[2].Results[0].Content; got != "ok" {
		t.Fatalf("tool result = %q", got)
	}
	if history[3].Text != "done" {
		t.Fatalf("final = %q", history[3].Text)
	}
}

func TestRunToolErrorIsNotFatal(t *testing.T) {
	p := &fakeProvider{script: [][]llm.Delta{
		{toolCall("1", "boom")},
		{{Text: "recovered"}},
	}}
	a := &Agent{Provider: p, MaxSteps: 5, Tools: map[string]tools.Tool{
		"boom": funcTool{spec: tools.Spec{Name: "boom"}, fn: func(context.Context, json.RawMessage) (string, error) {
			return "", errors.New("kaboom")
		}},
	}}
	history, err := a.Run(context.Background(), nil, "hi", func(event.Event) {})
	if err != nil {
		t.Fatal(err)
	}
	if !history[2].Results[0].IsError {
		t.Fatalf("expected error result: %+v", history[2])
	}
}

func TestRunUnknownTool(t *testing.T) {
	p := &fakeProvider{script: [][]llm.Delta{
		{toolCall("1", "nope")},
		{{Text: "ok"}},
	}}
	a := &Agent{Provider: p, MaxSteps: 5, Tools: map[string]tools.Tool{}}
	history, err := a.Run(context.Background(), nil, "hi", func(event.Event) {})
	if err != nil {
		t.Fatal(err)
	}
	if !history[2].Results[0].IsError {
		t.Fatalf("expected error result: %+v", history[2])
	}
}

func TestRunCancellation(t *testing.T) {
	p := &fakeProvider{script: [][]llm.Delta{{toolCall("1", "slow")}}}
	a := &Agent{Provider: p, MaxSteps: 5, Tools: map[string]tools.Tool{
		"slow": funcTool{spec: tools.Spec{Name: "slow"}, fn: func(context.Context, json.RawMessage) (string, error) {
			return "should not run", nil
		}},
	}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	history, err := a.Run(ctx, nil, "hi", func(event.Event) {})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if len(history) != 3 {
		t.Fatalf("want 3 messages, got %+v", history)
	}
	if got := history[2].Results[0]; !got.IsError || got.Content != "cancelled by user" {
		t.Fatalf("result = %+v", got)
	}
}

func TestRunMaxSteps(t *testing.T) {
	p := &fakeProvider{script: [][]llm.Delta{
		{toolCall("1", "echo")},
		{toolCall("2", "echo")},
	}}
	a := &Agent{Provider: p, MaxSteps: 2, Tools: map[string]tools.Tool{
		"echo": funcTool{spec: tools.Spec{Name: "echo"}, fn: func(context.Context, json.RawMessage) (string, error) {
			return "ok", nil
		}},
	}}
	if _, err := a.Run(context.Background(), nil, "hi", func(event.Event) {}); err == nil {
		t.Fatal("expected max steps error")
	}
}

func TestCompact(t *testing.T) {
	p := &fakeProvider{script: [][]llm.Delta{{{Text: "summary"}}}}
	history := []llm.Message{
		{Role: llm.RoleUser, Text: "one"},
		{Role: llm.RoleAssistant, Text: "a"},
		{Role: llm.RoleUser, Text: "two"},
		{Role: llm.RoleAssistant, Text: "b"},
	}
	newHistory, summary, replaces, err := Compact(context.Background(), p, "sys", history, 2, "")
	if err != nil {
		t.Fatal(err)
	}
	if summary != "summary" || replaces != 2 {
		t.Fatalf("summary=%q replaces=%d", summary, replaces)
	}
	if len(newHistory) != 3 || newHistory[0].Text != "[Earlier conversation summary]\nsummary" {
		t.Fatalf("new history: %+v", newHistory)
	}
}

func TestCompactStoresTouchedFiles(t *testing.T) {
	p := &fakeProvider{script: [][]llm.Delta{{{Text: "summary"}}}}
	call := func(name string, path string) llm.ToolCall {
		b, _ := json.Marshal(map[string]string{"path": path})
		return llm.ToolCall{ID: "1", Name: name, Args: b}
	}
	history := []llm.Message{
		{Role: llm.RoleUser, Text: "one"},
		{Role: llm.RoleAssistant, Calls: []llm.ToolCall{call("edit", "a.go")}},
		{Role: llm.RoleTool, Results: []llm.ToolResult{{CallID: "1", Content: "x"}}},
		{Role: llm.RoleUser, Text: "two"},
		{Role: llm.RoleAssistant, Text: "b"},
	}
	newHistory, summary, _, err := Compact(context.Background(), p, "sys", history, 2, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(summary, filesMarker) || !strings.Contains(summary, "a.go") {
		t.Fatalf("stored summary missing files: %q", summary)
	}
	if !strings.Contains(newHistory[0].Text, "- a.go") {
		t.Fatalf("history summary missing files: %q", newHistory[0].Text)
	}
}

func TestTouchedFilesCarryAcrossCompactions(t *testing.T) {
	call := func(name, path string) llm.ToolCall {
		b, _ := json.Marshal(map[string]string{"path": path})
		return llm.ToolCall{ID: "1", Name: name, Args: b}
	}
	first := []llm.Message{
		{Role: llm.RoleUser, Text: "one"},
		{Role: llm.RoleAssistant, Calls: []llm.ToolCall{call("read", "a.go")}},
		{Role: llm.RoleTool, Results: []llm.ToolResult{{CallID: "1", Content: "x"}}},
	}
	if got := touchedFiles(first); len(got) != 1 || got[0] != "a.go" {
		t.Fatalf("first files: %v", got)
	}
	summaryMsg := llm.Message{Role: llm.RoleUser, Text: "[Earlier conversation summary]\nsummary" + formatFiles(touchedFiles(first))}
	second := append([]llm.Message{summaryMsg},
		llm.Message{Role: llm.RoleAssistant, Calls: []llm.ToolCall{call("write", "b.go")}},
		llm.Message{Role: llm.RoleTool, Results: []llm.ToolResult{{CallID: "1", Content: "y"}}},
	)
	got := touchedFiles(second)
	if len(got) != 2 || got[0] != "a.go" || got[1] != "b.go" {
		t.Fatalf("cumulative files: %v", got)
	}
}

// flakyProvider fails the first fails Stream calls and the next streamErrs
// streams before yielding script.
type flakyProvider struct {
	fails      int
	streamErrs int
	calls      int
	script     []llm.Delta
}

func (f *flakyProvider) Stream(_ context.Context, _ llm.Request) (<-chan llm.Delta, error) {
	f.calls++
	if f.calls <= f.fails {
		return nil, errors.New("transient")
	}
	ch := make(chan llm.Delta)
	failStream := f.calls <= f.fails+f.streamErrs
	script := f.script
	go func() {
		defer close(ch)
		if failStream {
			ch <- llm.Delta{Err: errors.New("transient stream")}
			return
		}
		for _, d := range script {
			ch <- d
		}
	}()
	return ch, nil
}

func retryAgent(p llm.Provider, maxRetries int) *Agent {
	return &Agent{
		Provider:   p,
		MaxSteps:   5,
		MaxRetries: maxRetries,
		RetryBase:  time.Millisecond,
		RetryMax:   2 * time.Millisecond,
	}
}

func TestRunRetriesTransientStreamError(t *testing.T) {
	p := &flakyProvider{fails: 2, script: []llm.Delta{{Text: "ok"}}}
	a := retryAgent(p, 3)
	history, err := a.Run(context.Background(), nil, "hi", func(event.Event) {})
	if err != nil {
		t.Fatal(err)
	}
	if p.calls != 3 {
		t.Fatalf("calls = %d, want 3", p.calls)
	}
	if history[1].Text != "ok" {
		t.Fatalf("history: %+v", history)
	}
}

func TestRunRetriesMidStreamBeforeOutput(t *testing.T) {
	p := &flakyProvider{streamErrs: 1, script: []llm.Delta{{Text: "ok"}}}
	a := retryAgent(p, 3)
	if _, err := a.Run(context.Background(), nil, "hi", func(event.Event) {}); err != nil {
		t.Fatal(err)
	}
	if p.calls != 2 {
		t.Fatalf("calls = %d, want 2", p.calls)
	}
}

func TestRunRetryGivesUp(t *testing.T) {
	p := &flakyProvider{fails: 10, script: []llm.Delta{{Text: "ok"}}}
	a := retryAgent(p, 2)
	if _, err := a.Run(context.Background(), nil, "hi", func(event.Event) {}); err == nil {
		t.Fatal("expected error")
	}
	if p.calls != 3 {
		t.Fatalf("calls = %d, want 3", p.calls)
	}
}

func TestRunNoRetryAfterOutput(t *testing.T) {
	p := &fakeProvider{script: [][]llm.Delta{{{Text: "partial"}, {Err: errors.New("boom")}}}}
	a := retryAgent(p, 3)
	if _, err := a.Run(context.Background(), nil, "hi", func(event.Event) {}); err == nil {
		t.Fatal("expected error")
	}
	if p.idx != 1 {
		t.Fatalf("retried after output: idx = %d", p.idx)
	}
}
