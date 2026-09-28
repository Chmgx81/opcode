// Package orchestrator drives the agent loop (architecture doc Section 6):
// send history + tool list to the model, stream the response, dispatch
// completed tool calls through the permission gate, feed results back, and
// repeat until the model stops calling tools.
//
// The Orchestrator knows nothing about any UI: a TUI (Phase 1) and the
// headless CLI both consume the same event channel.
package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"tilde/internal/llm"
	"tilde/internal/tools"
)

// Event kinds emitted during a turn.
const (
	EventText         = "text"          // streamed text delta
	EventToolStart    = "tool_start"    // a tool call is being dispatched
	EventToolResult   = "tool_result"   // a tool call finished
	EventUsage        = "usage"         // token usage for one model round
	EventTurnComplete = "turn_complete" // the model stopped calling tools
	EventError        = "error"
)

// ErrCancelled is the turn error surfaced when the user cancels a run.
var ErrCancelled = errors.New("turn cancelled")

// Event is one thing that happened during a turn. UIs render these;
// nothing else leaks out.
type Event struct {
	Kind       string       // one of the Event* kinds
	Text       string       // EventText
	ToolCall   llm.ToolCall // EventToolStart, EventToolResult
	ToolResult string       // EventToolResult
	Usage      llm.Usage    // EventUsage
	Err        error        // EventError
}

// MaxToolRounds bounds one user turn so a model stuck in a tool-calling
// loop cannot spin forever. 25 is far beyond any legitimate use.
const MaxToolRounds = 25

// maxToolResultChars caps what a tool result contributes to the context.
// run_shell output in particular can be enormous; the model gets the head
// of it and a note that it was truncated.
const maxToolResultChars = 50_000

// Orchestrator owns the conversation state for one session.
type Orchestrator struct {
	Provider llm.Provider
	Model    string
	System   string
	Registry *tools.Registry
	Gate     *tools.Gate

	history []llm.Message

	steerMu sync.Mutex
	// pendingSteer holds steering messages typed mid-turn (spec 3.1):
	// folded into history at the next round boundary — after the current
	// tool call finishes — never mid-round.
	pendingSteer []string
}

func New(provider llm.Provider, model, system string, registry *tools.Registry, gate *tools.Gate) *Orchestrator {
	return &Orchestrator{
		Provider: provider,
		Model:    model,
		System:   system,
		Registry: registry,
		Gate:     gate,
	}
}

// Steer queues a steering message for the in-flight turn. It is appended
// to history as a user message before the next model request, so the
// model sees it as soon as the current tool call is done.
func (o *Orchestrator) Steer(text string) {
	o.steerMu.Lock()
	defer o.steerMu.Unlock()
	o.pendingSteer = append(o.pendingSteer, text)
}

// drainSteer returns and clears pending steering messages.
func (o *Orchestrator) drainSteer() []string {
	o.steerMu.Lock()
	defer o.steerMu.Unlock()
	steers := o.pendingSteer
	o.pendingSteer = nil
	return steers
}

// Send runs one full turn for a user message and streams events until the
// turn is complete, failed, or ctx is cancelled. The channel closes when
// the turn ends.
func (o *Orchestrator) Send(ctx context.Context, userText string) <-chan Event {
	events := make(chan Event, 32)
	go func() {
		defer close(events)
		o.history = append(o.history, llm.Message{Role: "user", Content: userText})
		if err := o.runTurn(ctx, events); err != nil {
			if ctx.Err() != nil {
				err = ErrCancelled
			}
			events <- Event{Kind: EventError, Err: err}
		}
	}()
	return events
}

func (o *Orchestrator) runTurn(ctx context.Context, events chan<- Event) error {
	for round := 0; ; round++ {
		if round >= MaxToolRounds {
			return fmt.Errorf("stopped after %d tool rounds without a final answer; the model may be stuck in a loop", MaxToolRounds)
		}
		if ctx.Err() != nil {
			return ErrCancelled
		}
		// Steering checkpoint: between rounds is the moment the spec
		// promises — the previous tool call has finished.
		for _, steer := range o.drainSteer() {
			o.history = append(o.history, llm.Message{Role: "user", Content: steer})
		}
		req := llm.ChatRequest{
			Model:    o.Model,
			System:   o.System,
			Messages: o.history,
			Tools:    o.toolDefs(),
		}
		stream, err := o.Provider.StreamChat(ctx, req)
		if err != nil {
			if ctx.Err() != nil {
				return ErrCancelled
			}
			return err
		}

		var content string
		var calls []llm.ToolCall
		for ev := range stream {
			switch ev.Type {
			case llm.TextEvent:
				content += ev.Text
				events <- Event{Kind: EventText, Text: ev.Text}
			case llm.ToolCallEvent:
				calls = append(calls, ev.Call)
			case llm.UsageEvent:
				events <- Event{Kind: EventUsage, Usage: ev.Usage}
			case llm.ErrorEvent:
				return ev.Err
			}
		}

		o.history = append(o.history, llm.Message{
			Role:      "assistant",
			Content:   content,
			ToolCalls: calls,
		})

		if len(calls) == 0 {
			events <- Event{Kind: EventTurnComplete}
			return nil
		}

		for _, call := range calls {
			events <- Event{Kind: EventToolStart, ToolCall: call}
			result, err := o.dispatch(ctx, call)
			if err != nil {
				if ctx.Err() != nil {
					return ErrCancelled
				}
				// Tool failures go back to the model as the tool
				// result; it can correct itself. Only harness-level
				// errors end the turn.
				result = "error: " + err.Error()
			}
			o.history = append(o.history, llm.Message{
				Role:       "tool",
				ToolCallID: call.ID,
				Content:    truncate(result),
			})
			events <- Event{Kind: EventToolResult, ToolCall: call, ToolResult: result}
		}
	}
}

// dispatch resolves the tool and runs it through the permission gate.
// An unknown tool is reported to the model rather than failing the turn.
func (o *Orchestrator) dispatch(ctx context.Context, call llm.ToolCall) (string, error) {
	tool, ok := o.Registry.Get(call.Name)
	if !ok {
		return fmt.Sprintf("error: no tool named %q", call.Name), nil
	}
	return o.Gate.Execute(ctx, tool, call.Arguments)
}

func (o *Orchestrator) toolDefs() []llm.Tool {
	defs := o.Registry.Defs()
	out := make([]llm.Tool, 0, len(defs))
	for _, d := range defs {
		out = append(out, llm.Tool{
			Name:        d.Name,
			Description: d.Description,
			Parameters:  d.Parameters,
		})
	}
	return out
}

func truncate(s string) string {
	if len(s) <= maxToolResultChars {
		return s
	}
	return s[:maxToolResultChars] +
		fmt.Sprintf("\n... truncated (%d more chars)", len(s)-maxToolResultChars)
}
