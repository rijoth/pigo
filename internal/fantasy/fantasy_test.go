package fantasy

import (
	"context"
	"testing"

	"charm.land/fantasy"

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
