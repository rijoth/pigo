// Package event is the contract between the agent and the UI.
package event

import "pigo/internal/llm"

// Kind identifies an event type.
type Kind int

const (
	// TextDelta is streamed assistant text; Text is set.
	TextDelta Kind = iota
	// ToolStart means the model requested a tool; Call is set.
	ToolStart
	// ToolResult means a tool finished; Call and Result are set.
	ToolResult
	// TurnEnd means the assistant finished; Usage may be set.
	TurnEnd
	// Notice is informational; Text is set.
	Notice
	// Error carries a recoverable or fatal error; Err is set.
	Error
)

// Event is one thing that happened during a turn.
type Event struct {
	Kind   Kind
	Text   string
	Call   *llm.ToolCall
	Result *llm.ToolResult
	Usage  *llm.Usage
	Err    error
}
