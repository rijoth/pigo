package event

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	"pigo/internal/llm"
)

func TestEncode(t *testing.T) {
	cases := []struct {
		name string
		ev   Event
		want string
	}{
		{"text", Event{Kind: TextDelta, Text: "hi"}, `{"type":"text_delta","text":"hi"}`},
		{"tool_start", Event{Kind: ToolStart, Call: &llm.ToolCall{Name: "bash", Args: json.RawMessage(`{"command":"ls"}`)}}, `{"type":"tool_start","name":"bash","args":{"command":"ls"}}`},
		{"tool_result", Event{Kind: ToolResult, Result: &llm.ToolResult{Content: "out", IsError: true}}, `{"type":"tool_result","content":"out","is_error":true}`},
		{"turn_end", Event{Kind: TurnEnd, Usage: &llm.Usage{InputTokens: 3, OutputTokens: 4}}, `{"type":"turn_end","usage":{"input_tokens":3,"output_tokens":4}}`},
		{"notice", Event{Kind: Notice, Text: "n"}, `{"type":"notice","text":"n"}`},
		{"error", Event{Kind: Error, Err: errors.New("bad")}, `{"type":"error","error":"bad"}`},
	}
	for _, tc := range cases {
		var b bytes.Buffer
		if err := Encode(&b, tc.ev); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got := bytes.TrimRight(b.Bytes(), "\n"); string(got) != tc.want {
			t.Errorf("%s: got %s, want %s", tc.name, got, tc.want)
		}
	}
}
