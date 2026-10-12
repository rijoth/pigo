// Package agent runs the model/tool loop.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

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
}

// Run appends userText to history, runs the loop until the model stops calling
// tools, and returns the updated history. It emits events as it goes.
func (a *Agent) Run(ctx context.Context, history []llm.Message, userText string, emit func(event.Event)) ([]llm.Message, error) {
	history = append(history, llm.Message{Role: llm.RoleUser, Text: userText})
	steps := a.MaxSteps
	if steps <= 0 {
		steps = 50
	}
	specs := a.toolSpecs()

	for step := 0; step < steps; step++ {
		assistant, usage, err := a.streamWithRetry(ctx, llm.Request{System: a.System, Messages: history, Tools: specs, Thinking: a.Thinking}, emit)
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

// compactPrompt is the fixed shape of a compaction summary. Structured
// sections survive repeated compaction better than free prose.
const compactPrompt = `Summarize the conversation so far for a coding agent that will continue the work. Use exactly these sections:
## Goal
## Constraints & Preferences
## Progress
## Key Decisions
## Next Steps
## Critical Context
Preserve file paths, commands, decisions, and unfinished work. Be concise. Reply with the summary only.`

// Compact summarizes the older part of history, keeping the most recent
// messages. instructions, when non-empty, is appended to the summary request
// (for `/compact <instructions>`). It returns the new history, the summary,
// and the index of the first kept message. The caller persists the compaction.
func Compact(ctx context.Context, p llm.Provider, system string, history []llm.Message, keep int, instructions string) ([]llm.Message, string, int, error) {
	cut := len(history) - keep
	if cut < 1 {
		return history, "", 0, nil
	}
	for cut < len(history) && history[cut].Role != llm.RoleUser {
		cut++
	}
	if cut >= len(history) {
		return history, "", 0, nil
	}
	text := compactPrompt
	if instructions != "" {
		text += "\n\nAdditional instructions:\n" + instructions
	}
	files := touchedFiles(history)
	if len(files) > 0 {
		text += "\n\nFiles touched this session (reference only):\n- " + strings.Join(files, "\n- ")
	}
	old := append([]llm.Message(nil), history[:cut]...)
	old = append(old, llm.Message{Role: llm.RoleUser, Text: text})
	summary, err := summarize(ctx, p, system, old)
	if err != nil {
		return history, "", 0, err
	}
	if summary == "" {
		return history, "", 0, fmt.Errorf("compaction produced an empty summary")
	}
	// Persist the cumulative file list with the summary so it survives the
	// next compaction instead of depending on the model to repeat it.
	summary += formatFiles(files)
	summaryMsg := llm.Message{Role: llm.RoleUser, Text: "[Earlier conversation summary]\n" + summary}
	newHistory := append([]llm.Message{summaryMsg}, history[cut:]...)
	return newHistory, summary, cut, nil
}

// branchPrompt asks for a summary of the branch being left behind.
const branchPrompt = `Summarize the conversation above so another agent can continue from an earlier point without losing useful context. Cover the goal, what was tried, what worked or failed, and any files or commands that mattered. Reply with the summary only.`

// Summarize produces a summary of msgs for switching branches with /tree.
func Summarize(ctx context.Context, p llm.Provider, system string, msgs []llm.Message) (string, error) {
	req := append([]llm.Message(nil), msgs...)
	req = append(req, llm.Message{Role: llm.RoleUser, Text: branchPrompt})
	return summarize(ctx, p, system, req)
}

// summarize streams one summary response for a request.
func summarize(ctx context.Context, p llm.Provider, system string, msgs []llm.Message) (string, error) {
	stream, err := p.Stream(ctx, llm.Request{System: system, Messages: msgs})
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for d := range stream {
		if d.Err != nil {
			return "", d.Err
		}
		b.WriteString(d.Text)
	}
	return strings.TrimSpace(b.String()), nil
}

// filesMarker separates the cumulative touched-file list from a summary's
// prose. touchedFiles reads it back so file tracking survives compaction.
const filesMarker = "[Files touched]"

// formatFiles renders the touched-file list appended to a stored summary, or
// "" when there is nothing to record.
func formatFiles(files []string) string {
	if len(files) == 0 {
		return ""
	}
	return "\n\n" + filesMarker + "\n- " + strings.Join(files, "\n- ")
}

// parseFiles extracts a file list previously written by formatFiles.
func parseFiles(text string) []string {
	i := strings.LastIndex(text, filesMarker)
	if i < 0 {
		return nil
	}
	var out []string
	for _, line := range strings.Split(text[i+len(filesMarker):], "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "- ") {
			break
		}
		out = append(out, strings.TrimSpace(strings.TrimPrefix(line, "- ")))
	}
	return out
}

// touchedFiles lists the paths read, written, or edited and the commands run
// during the session, plus the files carried over from earlier compactions, so
// a summary preserves the full working set.
func touchedFiles(history []llm.Message) []string {
	seen := map[string]bool{}
	var out []string
	add := func(s string) {
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	for _, m := range history {
		for _, f := range parseFiles(m.Text) {
			add(f)
		}
		for _, c := range m.Calls {
			var a struct {
				Path    string `json:"path"`
				Command string `json:"command"`
			}
			if json.Unmarshal(c.Args, &a) != nil {
				continue
			}
			switch c.Name {
			case "read", "write", "edit":
				add(a.Path)
			case "bash":
				add("$ " + a.Command)
			}
		}
	}
	sort.Strings(out)
	return out
}
