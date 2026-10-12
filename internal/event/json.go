package event

import (
	"encoding/json"
	"io"

	"pigo/internal/llm"
)

// jsonRecord is the stable machine-readable form of an Event. Its schema is
// versioned by convention: fields are only added, never repurposed.
type jsonRecord struct {
	Type    string          `json:"type"`
	Text    string          `json:"text,omitempty"`
	Name    string          `json:"name,omitempty"`
	Args    json.RawMessage `json:"args,omitempty"`
	Content string          `json:"content,omitempty"`
	IsError bool            `json:"is_error,omitempty"`
	Usage   *llm.Usage      `json:"usage,omitempty"`
	Error   string          `json:"error,omitempty"`
}

// Encode writes ev as one JSON object followed by a newline. Unknown kinds are
// written as an empty record so the stream stays well formed.
func Encode(w io.Writer, ev Event) error {
	r := jsonRecord{}
	switch ev.Kind {
	case TextDelta:
		r.Type, r.Text = "text_delta", ev.Text
	case ThinkingDelta:
		r.Type, r.Text = "thinking_delta", ev.Text
	case ToolStart:
		r.Type = "tool_start"
		if ev.Call != nil {
			r.Name, r.Args = ev.Call.Name, ev.Call.Args
		}
	case ToolResult:
		r.Type = "tool_result"
		if ev.Result != nil {
			r.Content, r.IsError = ev.Result.Content, ev.Result.IsError
		}
	case TurnEnd:
		r.Type, r.Usage = "turn_end", ev.Usage
	case Notice:
		r.Type, r.Text = "notice", ev.Text
	case Error:
		r.Type = "error"
		if ev.Err != nil {
			r.Error = ev.Err.Error()
		}
	}
	return json.NewEncoder(w).Encode(r)
}
