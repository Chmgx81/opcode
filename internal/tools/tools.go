// Package tools holds opcode's built-in tools and the single permission
// gate every tool call passes through (Section 3.6/7 of the architecture
// doc). The gate is the one place that decides "can this action happen"
// and the one place every executed action gets logged.
package tools

import (
	"context"
	"encoding/json"
	"sync"
)

// Tier is a tool's permission tier (Section 7). The gate checks the tier
// per action; permission modes (Phase 2) set the default posture across
// tiers. Tools declare their tier and never decide permission themselves,
// so the tiered model can be enforced without touching the tools.
type Tier string

const (
	// TierReadOnly can't change anything: always allowed, no prompt.
	TierReadOnly Tier = "read-only"
	// TierDraftOnly produces a proposal without applying it (a plan,
	// a todo list). Running it is always allowed; applying or
	// committing the result is a separate gated action.
	TierDraftOnly Tier = "draft-only"
	// TierActionAllowed mutates state (writes files, runs commands):
	// gated by mode — prompted by default, only skippable in full-auto.
	TierActionAllowed Tier = "action-allowed"
)

// Tool is one built-in (or later, skill/MCP) tool. Execute receives the
// model's arguments as a raw JSON string; the tool parses its own
// arguments so unknown fields fail loudly rather than being ignored.
type Tool interface {
	Name() string
	Description() string
	// Parameters is a JSON Schema object for the tool's arguments.
	Parameters() json.RawMessage
	Tier() Tier
	Execute(ctx context.Context, args string) (string, error)
}

// Registry holds the tools available to the model, in registration order.
//
// mu is not defensive: a mid-turn trust grant calls Register from the
// tea goroutine while the turn goroutine is inside Defs or Get building
// the next request. That is a real concurrent write to a real slice,
// and append's reallocation can hand the reader a torn header.
type Registry struct {
	mu    sync.RWMutex
	tools []Tool
}

func (r *Registry) Register(t Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools = append(r.tools, t)
}

// All returns every registered tool in registration order.
func (r *Registry) All() []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]Tool(nil), r.tools...)
}

func (r *Registry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, t := range r.tools {
		if t.Name() == name {
			return t, true
		}
	}
	return nil, false
}

// Def is the tool description handed to the LLM layer; the orchestrator
// maps these to llm.Tool values.
type Def struct {
	Name        string
	Description string
	Parameters  json.RawMessage
	Tier        Tier
}

func (r *Registry) Defs() []Def {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Def, 0, len(r.tools))
	for _, t := range r.tools {
		out = append(out, Def{
			Name:        t.Name(),
			Description: t.Description(),
			Parameters:  t.Parameters(),
			Tier:        t.Tier(),
		})
	}
	return out
}

// parseArgs decodes a tool-call arguments string into a struct, giving a
// clear error when the model sent malformed JSON.
func parseArgs(args string, v any) error {
	if args == "" {
		args = "{}"
	}
	if err := json.Unmarshal([]byte(args), v); err != nil {
		return &BadArgumentsError{Raw: args, Err: err}
	}
	return nil
}

type BadArgumentsError struct {
	Raw string
	Err error
}

func (e *BadArgumentsError) Error() string {
	return "bad arguments: " + e.Err.Error()
}
