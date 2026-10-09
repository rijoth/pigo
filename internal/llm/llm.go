// Package llm defines the provider seam and the shared conversation types.
package llm

import (
	"context"
	"encoding/json"
)

// Role identifies who produced a message.
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// Message is one conversation turn.
type Message struct {
	Role    Role         `json:"role"`
	Text    string       `json:"text,omitempty"`
	Calls   []ToolCall   `json:"calls,omitempty"`
	Results []ToolResult `json:"results,omitempty"`
}

// ToolCall is a tool invocation requested by the model.
type ToolCall struct {
	ID   string          `json:"id"`
	Name string          `json:"name"`
	Args json.RawMessage `json:"args,omitempty"`
}

// ToolResult is the outcome of running a tool.
type ToolResult struct {
	CallID  string `json:"call_id"`
	Content string `json:"content"`
	IsError bool   `json:"is_error,omitempty"`
}

// ToolSpec is a tool description visible to the model.
type ToolSpec struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Schema      json.RawMessage `json:"schema"`
}

// Request is one model call.
type Request struct {
	System   string
	Messages []Message
	Tools    []ToolSpec
}

// Usage reports token consumption for a call.
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// Delta is one item from a streaming response.
type Delta struct {
	Text  string
	Call  *ToolCall
	Usage *Usage
	Err   error
}

// Provider streams model responses.
type Provider interface {
	// Stream starts a request. The channel is closed when the response is
	// complete or ctx is cancelled. A terminal failure is sent as Delta.Err.
	Stream(ctx context.Context, req Request) (<-chan Delta, error)
}
