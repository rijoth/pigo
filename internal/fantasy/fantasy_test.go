package fantasy

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"charm.land/fantasy"
	"charm.land/fantasy/providers/anthropic"

	"pigo/internal/llm"
)

type fakeModel struct {
	call fantasy.Call
}

func (m *fakeModel) Generate(context.Context, fantasy.Call) (*fantasy.Response, error) {
	return nil, nil
}

func (m *fakeModel) Stream(_ context.Context, call fantasy.Call) (fantasy.StreamResponse, error) {
	m.call = call
	return func(yield func(fantasy.StreamPart) bool) {
		yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextDelta, Delta: "hi"})
		yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeToolCall, ID: "1", ToolCallName: "read", ToolCallInput: `{"path":"x"}`})
		yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeToolCall, ID: "2", ToolCallName: "bash"})
		yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeFinish, Usage: fantasy.Usage{InputTokens: 3, OutputTokens: 4}})
	}, nil
}

func (m *fakeModel) GenerateObject(context.Context, fantasy.ObjectCall) (*fantasy.ObjectResponse, error) {
	return nil, nil
}

func (m *fakeModel) StreamObject(context.Context, fantasy.ObjectCall) (fantasy.ObjectStreamResponse, error) {
	return nil, nil
}

func (m *fakeModel) Provider() string { return "fake" }
func (m *fakeModel) Model() string    { return "fake" }

type reasoningModel struct {
	fakeModel
	reasoningCall fantasy.Call
}

func (m *reasoningModel) Stream(_ context.Context, call fantasy.Call) (fantasy.StreamResponse, error) {
	m.reasoningCall = call
	return func(yield func(fantasy.StreamPart) bool) {
		yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeReasoningStart, ID: "0"})
		yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeReasoningDelta, ID: "0", Delta: "ponder"})
		yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeReasoningDelta, ID: "0", ProviderMetadata: fantasy.ProviderMetadata{
			anthropic.Name: &anthropic.ReasoningOptionMetadata{Signature: "sig"},
		}})
		yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeReasoningEnd, ID: "0"})
		yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeFinish, Usage: fantasy.Usage{}})
	}, nil
}

// TestFantasyReasoning checks that a reasoning block is emitted once complete,
// including its signature, and that stored metadata replays as provider options.
func TestFantasyReasoning(t *testing.T) {
	old := json.RawMessage(`{"anthropic":{"type":"anthropic.reasoning_metadata","data":{"signature":"old"}}}`)
	model := &reasoningModel{}
	p := &fantasyProvider{model: model}
	ch, err := p.Stream(context.Background(), llm.Request{Messages: []llm.Message{
		{Role: llm.RoleAssistant, Reasoning: []llm.ReasoningBlock{{Text: "prior", Metadata: old}}},
		{Role: llm.RoleUser, Text: "hi"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	var block *llm.ReasoningBlock
	for d := range ch {
		if d.Err != nil {
			t.Fatal(d.Err)
		}
		if d.Reasoning != nil {
			block = d.Reasoning
		}
	}
	if block == nil || block.Text != "ponder" {
		t.Fatalf("reasoning = %+v", block)
	}
	if !strings.Contains(string(block.Metadata), `"signature":"sig"`) {
		t.Fatalf("metadata = %s", block.Metadata)
	}
	if len(model.reasoningCall.Prompt) < 2 {
		t.Fatalf("prompt = %+v", model.reasoningCall.Prompt)
	}
	part, ok := fantasy.AsMessagePart[fantasy.ReasoningPart](model.reasoningCall.Prompt[0].Content[0])
	if !ok || part.Text != "prior" {
		t.Fatalf("replayed part = %+v (ok=%v)", part, ok)
	}
	meta := anthropic.GetReasoningMetadata(part.Options())
	if meta == nil || meta.Signature != "old" {
		t.Fatalf("replayed metadata = %+v", meta)
	}
}

func TestFantasyStreamMapping(t *testing.T) {
	model := &fakeModel{}
	p := &fantasyProvider{model: model}
	ch, err := p.Stream(context.Background(), llm.Request{
		System: "sys",
		Messages: []llm.Message{
			{Role: llm.RoleUser, Text: "hi"},
			{Role: llm.RoleAssistant, Calls: []llm.ToolCall{{ID: "0", Name: "read", Args: []byte(`{}`)}}},
			{Role: llm.RoleTool, Results: []llm.ToolResult{{CallID: "0", Content: "ok"}}},
		},
		Tools: []llm.ToolSpec{{Name: "read", Description: "d", Schema: []byte(`{"type":"object"}`)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var text string
	var calls []llm.ToolCall
	var usage *llm.Usage
	for d := range ch {
		if d.Err != nil {
			t.Fatal(d.Err)
		}
		text += d.Text
		if d.Call != nil {
			calls = append(calls, *d.Call)
		}
		if d.Usage != nil {
			usage = d.Usage
		}
	}
	if text != "hi" {
		t.Fatalf("text = %q", text)
	}
	if len(calls) != 2 || calls[0].Name != "read" || string(calls[1].Args) != "{}" {
		t.Fatalf("calls = %+v", calls)
	}
	if usage == nil || usage.InputTokens != 3 || usage.OutputTokens != 4 {
		t.Fatalf("usage = %+v", usage)
	}
	if len(model.call.Prompt) != 4 || model.call.Prompt[0].Role != fantasy.MessageRoleSystem {
		t.Fatalf("prompt = %+v", model.call.Prompt)
	}
	if len(model.call.Tools) != 1 {
		t.Fatalf("tools = %+v", model.call.Tools)
	}
}

func TestMapProviderErrorContextTooLarge(t *testing.T) {
	err := mapProviderError(&fantasy.ProviderError{Message: "too big", ContextTooLargeErr: true})
	if !errors.Is(err, llm.ErrContextTooLarge) {
		t.Fatalf("err = %v, want ErrContextTooLarge", err)
	}
}

func TestMapProviderErrorPassesOthersThrough(t *testing.T) {
	other := &fantasy.ProviderError{Message: "rate limited"}
	if got := mapProviderError(other); got != error(other) {
		t.Fatalf("got %v, want the original error", got)
	}
	plain := errors.New("boom")
	if got := mapProviderError(plain); got != plain {
		t.Fatalf("got %v, want the original error", got)
	}
	if mapProviderError(nil) != nil {
		t.Fatal("nil did not stay nil")
	}
}
