package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"pigo/internal/compact"
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

func TestRunReasoning(t *testing.T) {
	block := &llm.ReasoningBlock{Text: "ponder", Metadata: json.RawMessage(`{"anthropic":{}}`)}
	p := &fakeProvider{script: [][]llm.Delta{{{Reasoning: block}, {Text: "hello"}}}}
	a := &Agent{Provider: p, Tools: map[string]tools.Tool{}, MaxSteps: 5}
	var events []event.Event
	history, err := a.Run(context.Background(), nil, "hi", collect(&events))
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 || len(history[1].Reasoning) != 1 || history[1].Reasoning[0].Text != "ponder" {
		t.Fatalf("reasoning not stored: %+v", history)
	}
	if string(history[1].Reasoning[0].Metadata) != `{"anthropic":{}}` {
		t.Fatalf("metadata not stored: %s", history[1].Reasoning[0].Metadata)
	}
	var thinking bool
	for _, e := range events {
		thinking = thinking || e.Kind == event.ThinkingDelta
	}
	if !thinking {
		t.Fatalf("missing thinking event: %+v", events)
	}
}

// TestRunReasoningRedacted checks that a signed reasoning block with no visible
// text (encrypted) still produces a ThinkingDelta, so the UI can show it.
func TestRunReasoningRedacted(t *testing.T) {
	block := &llm.ReasoningBlock{Metadata: json.RawMessage(`{"anthropic":{}}`)}
	p := &fakeProvider{script: [][]llm.Delta{{{Reasoning: block}, {Text: "hello"}}}}
	a := &Agent{Provider: p, Tools: map[string]tools.Tool{}, MaxSteps: 5}
	var events []event.Event
	history, err := a.Run(context.Background(), nil, "hi", collect(&events))
	if err != nil {
		t.Fatal(err)
	}
	if len(history[1].Reasoning) != 1 {
		t.Fatalf("reasoning not stored: %+v", history)
	}
	var thinking bool
	for _, e := range events {
		thinking = thinking || e.Kind == event.ThinkingDelta
	}
	if !thinking {
		t.Fatalf("missing thinking event for redacted block: %+v", events)
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

// overflowHistory is long enough that a user-role boundary exists before the
// message appended by Run.
func overflowHistory() []llm.Message {
	return []llm.Message{
		{Role: llm.RoleUser, Text: "one"},
		{Role: llm.RoleAssistant, Text: "a"},
		{Role: llm.RoleUser, Text: "two"},
		{Role: llm.RoleAssistant, Text: "b"},
	}
}

func TestRunRecoversFromContextOverflow(t *testing.T) {
	p := &fakeProvider{script: [][]llm.Delta{
		{{Err: fmt.Errorf("request too large: %w", llm.ErrContextTooLarge)}},
		{{Text: "ok"}},
	}}
	var compacted int
	var summary string
	a := &Agent{
		Provider:       p,
		Tools:          map[string]tools.Tool{},
		MaxSteps:       5,
		CompactOptions: compact.DefaultOptions(),
		OnCompact:      func(firstKept int, s string) { compacted, summary = firstKept, s },
	}
	var events []event.Event
	got, err := a.Run(context.Background(), overflowHistory(), "three", collect(&events))
	if err != nil {
		t.Fatal(err)
	}
	if p.idx != 2 {
		t.Fatalf("provider calls = %d, want 2 (deterministic, no summarization call)", p.idx)
	}
	if compacted != 2 {
		t.Fatalf("OnCompact firstKept = %d, want 2", compacted)
	}
	if summary == "" || !strings.Contains(summary, "[user]") {
		t.Fatalf("summary not deterministic sections: %q", summary)
	}
	if len(got) != 5 || !strings.HasPrefix(got[0].Text, "[Earlier conversation summary]") {
		t.Fatalf("recovered history: %+v", got)
	}
	if got[len(got)-1].Text != "ok" {
		t.Fatalf("last message: %+v", got[len(got)-1])
	}
	var notice bool
	for _, e := range events {
		notice = notice || (e.Kind == event.Notice && strings.Contains(e.Text, "compacted"))
	}
	if !notice {
		t.Fatalf("missing compaction notice: %+v", events)
	}
}

func TestRunRecoversOnlyOnce(t *testing.T) {
	p := &fakeProvider{script: [][]llm.Delta{
		{{Err: llm.ErrContextTooLarge}},
		{{Err: llm.ErrContextTooLarge}},
	}}
	a := &Agent{Provider: p, Tools: map[string]tools.Tool{}, MaxSteps: 5, CompactOptions: compact.DefaultOptions()}
	if _, err := a.Run(context.Background(), overflowHistory(), "three", func(event.Event) {}); !errors.Is(err, llm.ErrContextTooLarge) {
		t.Fatalf("err = %v, want ErrContextTooLarge", err)
	}
	if p.idx != 2 {
		t.Fatalf("provider calls = %d, want 2 (one recovery only)", p.idx)
	}
}

func TestRunContextOverflowWithoutBoundary(t *testing.T) {
	p := &fakeProvider{script: [][]llm.Delta{{{Err: llm.ErrContextTooLarge}}}}
	a := &Agent{Provider: p, Tools: map[string]tools.Tool{}, MaxSteps: 5, MaxRetries: 3, CompactOptions: compact.DefaultOptions()}
	if _, err := a.Run(context.Background(), nil, "hi", func(event.Event) {}); !errors.Is(err, llm.ErrContextTooLarge) {
		t.Fatalf("err = %v, want ErrContextTooLarge", err)
	}
	if p.idx != 1 {
		t.Fatalf("provider calls = %d, want 1 (no backoff, no summarization)", p.idx)
	}
}
