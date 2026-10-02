// Package llm defines opcode's own conversation types and the Provider
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
	"encoding/base64"
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

// Image is one image attached to a user message. Data is raw bytes;
// the wire layer encodes it as a data URL. MimeType is what the
// clipboard reported (image/png from every supported platform tool).
type Image struct {
	MimeType string
	Data     []byte
}

// base64 returns the standard-encoding payload for wire formats.
func (i Image) base64() string {
	return base64.StdEncoding.EncodeToString(i.Data)
}

// Message is one node of the conversation. An assistant message may carry
// ToolCalls; a "tool" message carries a ToolCallID and the tool's result
// in Content. A user message may carry Images alongside Content.
type Message struct {
	Role       string     // "system", "user", "assistant", or "tool"
	Content    string     // "" for pure tool-call assistant messages is fine
	ToolCalls  []ToolCall // assistant messages only
	ToolCallID string     // tool result messages only
	Images     []Image    // user messages only
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
	// MaxTokens caps the completion; 0 means the provider's default
	// (Anthropic requires the field and uses 8192; OpenAI-compatible
	// servers omit it when 0).
	MaxTokens int
	// ReasoningEffort names the thinking budget: low, medium, high,
	// or empty for the provider's default. OpenAI-compatible servers
	// get reasoning_effort; Anthropic gets a thinking budget.
	ReasoningEffort string
}

// Chat event kinds.
const (
	// TextEvent carries a text delta, streamed as it arrives.
	TextEvent = "text"
	// ReasoningEvent carries a thinking delta — OpenRouter's
	// "reasoning" field, or the DeepSeek-compatible
	// "reasoning_content". For the UI; not conversation history.
	ReasoningEvent = "reasoning"
	// ToolCallEvent carries one completed tool call, emitted only after
	// its arguments are known complete.
	ToolCallEvent = "tool_call"
	// UsageEvent carries token usage for the finished round. Servers
	// that don't report usage simply never emit it.
	UsageEvent = "usage"
	// ErrorEvent carries a mid-stream failure; the channel closes after it.
	ErrorEvent = "error"
)

// Usage is the token accounting for one request.
type Usage struct {
	PromptTokens     int
	CompletionTokens int
}

// ChatEvent is one streamed event from the provider.
type ChatEvent struct {
	Type  string
	Text  string   // TextEvent
	Call  ToolCall // ToolCallEvent
	Usage Usage    // UsageEvent
	Err   error    // ErrorEvent
}
