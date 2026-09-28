// Package llm defines tilde's own conversation types and the Provider
// interface, plus the OpenAI-compatible streaming client (used for
// OpenRouter and any local server speaking the same schema).
//
// ChatRequest/ChatEvent are internal types, not the wire format: the
// OpenAI-compatible implementation in openai.go translates both ways, so a
// different provider is a new Provider implementation, not an Orchestrator
// rewrite.
package llm

import (
	"context"
	"encoding/json"
)

// Provider streams a chat completion. Implementations must close the
// returned channel exactly once; an error that occurs mid-stream is
// delivered as an ErrorEvent rather than a returned error.
type Provider interface {
	StreamChat(ctx context.Context, req ChatRequest) (<-chan ChatEvent, error)
}

// ToolCall is one completed call: Arguments is a JSON object as a string,
// reassembled from streamed fragments by the provider implementation.
type ToolCall struct {
	ID        string
	Name      string
	Arguments string
}

// Message is one node of the conversation. An assistant message may carry
// ToolCalls; a "tool" message carries a ToolCallID and the tool's result
// in Content.
type Message struct {
	Role       string     // "system", "user", "assistant", or "tool"
	Content    string     // "" for pure tool-call assistant messages is fine
	ToolCalls  []ToolCall // assistant messages only
	ToolCallID string     // tool result messages only
}

// Tool describes one tool to the model. Parameters is a JSON Schema
// object, passed through as-is.
type Tool struct {
	Name        string
	Description string
	Parameters  json.RawMessage
}

// ChatRequest is one streaming request to the model.
type ChatRequest struct {
	Model    string
	System   string // prepended as a system message
	Messages []Message
	Tools    []Tool
}

// Chat event kinds.
const (
	// TextEvent carries a text delta, streamed as it arrives.
	TextEvent = "text"
	// ToolCallEvent carries one completed tool call, emitted only after
	// its arguments are known complete.
	ToolCallEvent = "tool_call"
	// ErrorEvent carries a mid-stream failure; the channel closes after it.
	ErrorEvent = "error"
)

// ChatEvent is one streamed event from the provider.
type ChatEvent struct {
	Type string
	Text string   // TextEvent
	Call ToolCall // ToolCallEvent
	Err  error    // ErrorEvent
}
