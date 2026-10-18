// Package agent runs the model/tool loop.
package agent

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"pigo/internal/compact"
	"pigo/internal/event"
	"pigo/internal/llm"
	"pigo/internal/tools"
)

// Agent owns the conversation loop.
type Agent struct {
	Provider   llm.Provider
	Tools      map[string]tools.Tool
	System     string
	Thinking   string                  // reasoning level; empty or "off" disables
	Approve    func(llm.ToolCall) bool // nil means always approve
	MaxSteps   int
	MaxRetries int           // transient-failure retries per model call
	RetryBase  time.Duration // first backoff delay
	RetryMax   time.Duration // backoff cap
	// CompactOptions tunes deterministic compaction on context overflow. Zero
	// values fall back to the compact package defaults.
	CompactOptions compact.Options
	// OnCompact, when non-nil, is called after Run compacts history to recover
	// from a context-overflow error. firstKept is the index in the history passed
	// to Run of the first kept message; summary is the compaction text. Recovery
	// proceeds whether or not the caller persists it.
	OnCompact func(firstKept int, summary string)
}

// Run appends userText to history, runs the loop until the model stops calling
// tools, and returns the updated history. It emits events as it goes.
func (a *Agent) Run(ctx context.Context, history []llm.Message, userText string, emit func(event.Event)) ([]llm.Message, error) {
	base := len(history)
	history = append(history, llm.Message{Role: llm.RoleUser, Text: userText})
	steps := a.MaxSteps
	if steps <= 0 {
		steps = 50
	}
	specs := a.toolSpecs()
	recovered := false

	for step := 0; step < steps; step++ {
		assistant, usage, err := a.streamWithRetry(ctx, llm.Request{System: a.System, Messages: history, Tools: specs, Thinking: a.Thinking}, emit)
		// A context-overflow error arrives before any output, so the turn can be
		// compacted and retried once. Recovering after partial output would
		// duplicate it, so that case falls through to the normal error path.
		if err != nil && !recovered && errors.Is(err, llm.ErrContextTooLarge) &&
			assistant.Text == "" && len(assistant.Calls) == 0 && len(assistant.Reasoning) == 0 {
			if h, summary, cut, ok := a.recoverOverflow(history, base); ok {
				history, recovered = h, true
				if a.OnCompact != nil {
					a.OnCompact(cut, summary)
				}
				emit(event.Event{Kind: event.Notice, Text: "context too large; compacted and retrying"})
				continue
			}
		}
		if assistant.Text != "" || len(assistant.Calls) > 0 || len(assistant.Reasoning) > 0 {
			history = append(history, assistant)
		}

		if err != nil {
			if len(assistant.Calls) > 0 {
				history = append(history, cancelledResults(assistant.Calls))
			}
			if ctx.Err() != nil {
				return history, ctx.Err()
			}
			emit(event.Event{Kind: event.Error, Err: err})
			return history, err
		}
		if len(assistant.Calls) == 0 {
			emit(event.Event{Kind: event.TurnEnd, Usage: usage})
			return history, nil
		}

		results := make([]llm.ToolResult, 0, len(assistant.Calls))
		cancelled := false
		for _, call := range assistant.Calls {
			var res llm.ToolResult
			switch {
			case cancelled || ctx.Err() != nil:
				cancelled = true
				res = llm.ToolResult{CallID: call.ID, Content: "cancelled by user", IsError: true}
			case a.Approve != nil && !a.Approve(call):
				res = llm.ToolResult{CallID: call.ID, Content: "denied by user", IsError: true}
			default:
				res = a.run(ctx, call)
			}
			results = append(results, res)
			callCopy := call
			emit(event.Event{Kind: event.ToolResult, Call: &callCopy, Result: &results[len(results)-1]})
		}
		history = append(history, llm.Message{Role: llm.RoleTool, Results: results})
		if cancelled {
			return history, ctx.Err()
		}
	}

	err := fmt.Errorf("max steps (%d) reached", steps)
	emit(event.Event{Kind: event.Error, Err: err})
	return history, err
}

// streamWithRetry performs one model call, retrying transient failures that
// occur before any assistant text or tool call was observed. A turn that has
// already produced output is not idempotent, so it is never retried.
func (a *Agent) streamWithRetry(ctx context.Context, req llm.Request, emit func(event.Event)) (llm.Message, *llm.Usage, error) {
	assistant := llm.Message{Role: llm.RoleAssistant}
	var usage *llm.Usage
	for attempt := 0; ; attempt++ {
		stream, err := a.Provider.Stream(ctx, req)
		if err == nil {
			assistant = llm.Message{Role: llm.RoleAssistant}
			usage = nil
			observable := false
			for d := range stream {
				switch {
				case d.Err != nil:
					err = d.Err
				case d.Text != "":
					observable = true
					assistant.Text += d.Text
					emit(event.Event{Kind: event.TextDelta, Text: d.Text})
				case d.Reasoning != nil:
					observable = true
					assistant.Reasoning = append(assistant.Reasoning, *d.Reasoning)
					emit(event.Event{Kind: event.ThinkingDelta, Text: d.Reasoning.Text})
				case d.Call != nil:
					observable = true
					assistant.Calls = append(assistant.Calls, *d.Call)
					emit(event.Event{Kind: event.ToolStart, Call: d.Call})
				case d.Usage != nil:
					usage = d.Usage
				}
			}
			if err == nil || observable {
				return assistant, usage, err
			}
		}
		if errors.Is(err, llm.ErrContextTooLarge) {
			return assistant, usage, err // compaction, not backoff, recovers this
		}
		if attempt >= a.MaxRetries || ctx.Err() != nil {
			return assistant, usage, err
		}
		a.backoff(ctx, attempt, err, emit)
	}
}

// backoff waits before the next retry, doubling the delay each attempt up to
// RetryMax. It returns early when the context is cancelled.
func (a *Agent) backoff(ctx context.Context, attempt int, err error, emit func(event.Event)) {
	delay := a.RetryBase << attempt
	if delay <= 0 {
		delay = time.Millisecond
	}
	if a.RetryMax > 0 && delay > a.RetryMax {
		delay = a.RetryMax
	}
	emit(event.Event{Kind: event.Notice, Text: fmt.Sprintf("stream failed (%v); retrying in %s", err, delay)})
	t := time.NewTimer(delay)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}

func (a *Agent) run(ctx context.Context, call llm.ToolCall) llm.ToolResult {
	t, ok := a.Tools[call.Name]
	if !ok {
		return llm.ToolResult{CallID: call.ID, Content: fmt.Sprintf("unknown tool %q", call.Name), IsError: true}
	}
	out, err := t.Run(ctx, call.Args)
	if err != nil {
		return llm.ToolResult{CallID: call.ID, Content: err.Error(), IsError: true}
	}
	return llm.ToolResult{CallID: call.ID, Content: out}
}

func (a *Agent) toolSpecs() []llm.ToolSpec {
	specs := make([]llm.ToolSpec, 0, len(a.Tools))
	for _, t := range a.Tools {
		s := t.Spec()
		specs = append(specs, llm.ToolSpec{Name: s.Name, Description: s.Description, Schema: s.Schema})
	}
	sort.Slice(specs, func(i, j int) bool { return specs[i].Name < specs[j].Name })
	return specs
}

func cancelledResults(calls []llm.ToolCall) llm.Message {
	results := make([]llm.ToolResult, 0, len(calls))
	for _, c := range calls {
		results = append(results, llm.ToolResult{CallID: c.ID, Content: "cancelled by user", IsError: true})
	}
	return llm.Message{Role: llm.RoleTool, Results: results}
}

// recoverOverflow compacts history after the provider rejected a request as too
// large. The boundary stays within the persisted history (index at or below
// base). It reports false when no such boundary exists or compiling fails.
func (a *Agent) recoverOverflow(history []llm.Message, base int) ([]llm.Message, string, int, bool) {
	if base <= 0 {
		return history, "", 0, false
	}
	newHistory, res, err := compact.Compact(history, nil, a.CompactOptions.Keep, false, "", nil, a.CompactOptions)
	if err != nil || !res.OK || res.FirstKept <= 0 || res.FirstKept > base {
		return history, "", 0, false
	}
	return newHistory, res.Summary, res.FirstKept, true
}

// Summarize produces a deterministic summary of msgs for switching branches
// with /tree. It never calls a model.
func Summarize(msgs []llm.Message) string {
	return compact.CompileRanked(compact.Input{History: msgs, Cut: len(msgs)}, compact.DefaultOptions())
}
