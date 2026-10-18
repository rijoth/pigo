package compact

import (
	"encoding/json"

	"pigo/internal/llm"
)

// Normalize converts messages into blocks. src, when non-nil, supplies the
// session-global source index for each message; a missing entry yields -1.
func Normalize(msgs []llm.Message, src []int) []Block {
	names := callNames(msgs)
	var out []Block
	for i, m := range msgs {
		s := -1
		if src != nil && i < len(src) {
			s = src[i]
		}
		switch m.Role {
		case llm.RoleUser:
			out = append(out, Block{Kind: KindUser, Text: sanitize(m.Text), Src: s})
		case llm.RoleAssistant:
			if m.Text != "" {
				out = append(out, Block{Kind: KindAssistant, Text: sanitize(m.Text), Src: s})
			}
			for _, c := range m.Calls {
				if c.Name == "" {
					continue
				}
				out = append(out, Block{Kind: KindToolCall, Name: c.Name, Args: decodeArgs(c.Args), Src: s})
			}
		case llm.RoleTool:
			for _, r := range m.Results {
				out = append(out, Block{Kind: KindToolResult, Name: names[r.CallID], Text: sanitize(r.Content), Src: s})
			}
		}
	}
	return out
}

// callNames maps tool call ids to tool names so tool results can carry a name.
func callNames(msgs []llm.Message) map[string]string {
	names := map[string]string{}
	for _, m := range msgs {
		if m.Role != llm.RoleAssistant {
			continue
		}
		for _, c := range m.Calls {
			if c.ID != "" && c.Name != "" {
				names[c.ID] = c.Name
			}
		}
	}
	return names
}

// decodeArgs unmarshals tool arguments into a map, returning an empty map on
// malformed input so downstream extraction never panics.
func decodeArgs(raw json.RawMessage) map[string]any {
	args := map[string]any{}
	if len(raw) == 0 {
		return args
	}
	_ = json.Unmarshal(raw, &args)
	return args
}
