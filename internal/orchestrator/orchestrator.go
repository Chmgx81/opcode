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
	"strings"
	"sync"

	"github.com/Chmgx81/tilde/internal/llm"
	"github.com/Chmgx81/tilde/internal/tools"
)

// Event kinds emitted during a turn.
const (
	EventText         = "text"          // streamed text delta
	EventReasoning    = "reasoning"     // streamed thinking, shown not stored
	EventToolStart    = "tool_start"    // a tool call is being dispatched
	EventToolResult   = "tool_result"   // a tool call finished
	EventUsage        = "usage"         // token usage for one model round
	EventCompaction   = "compaction"    // history was auto-summarized
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
	// Mode is the active permission mode. It shapes which tools are
	// offered to the model (read-only mode hides action-tier tools) and
	// is composed into the system prompt via ModeInstruction.
	Mode string
	// SkillsIndex is the metadata-only skills index (name + description
	// per skill) appended to the system prompt — tier 1 of progressive
	// disclosure. Updated in place when a trust decision adds project
	// skills mid-session.
	SkillsIndex string

	// Compaction (Section 3.2): when the previous round's reported
	// prompt tokens reach CompactionFraction of ContextWindow, the
	// oldest messages are summarized into a recap and replaced in
	// place. ContextWindow 0 disables it — model windows vary and a
	// wrong default would silently rewrite history. CompactionModel
	// optionally names a cheaper model for the summarizer round.
	ContextWindow   int
	CompactionModel string

	// lastPromptTokens is the provider-reported prompt size of the
	// most recent request — the compaction trigger signal.
	lastPromptTokens int

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
		Mode:     tools.ModeAsk,
	}
}

// SetMode switches the permission mode. The next model request picks up
// the new tool set and mode instruction; the gate's Decide callback is
// the caller's to rebuild (it owns the prompt plumbing).
func (o *Orchestrator) SetMode(mode string) {
	o.Mode = tools.NormalizeMode(mode)
}

// systemPrompt composes the base prompt, the skills index, and the
// active mode's instruction — assembled fresh each round so mid-session
// changes (a trust grant adding project skills, a mode switch) are
// picked up by the next request.
func (o *Orchestrator) systemPrompt() string {
	parts := []string{o.System}
	if o.SkillsIndex != "" {
		parts = append(parts, o.SkillsIndex)
	}
	if instr := tools.ModeInstruction(o.Mode); instr != "" {
		parts = append(parts, instr)
	}
	return strings.Join(parts, "\n")
}

// History returns the conversation so far (a copy) — session storage
// snapshots this on exit.
func (o *Orchestrator) History() []llm.Message {
	out := make([]llm.Message, len(o.history))
	copy(out, o.history)
	return out
}

// Seed replaces the conversation with a loaded session's history, so
// a resumed session continues where it left off.
func (o *Orchestrator) Seed(history []llm.Message) {
	o.history = append([]llm.Message(nil), history...)
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
	return o.SendImages(ctx, userText, nil)
}

// SendImages is Send with clipboard-attached images (Phase 22). The
// images persist on the history message for the session — what the
// model saw once, it sees again on resume.
func (o *Orchestrator) SendImages(ctx context.Context, userText string, images []llm.Image) <-chan Event {
	events := make(chan Event, 32)
	go func() {
		defer close(events)
		o.history = append(o.history, llm.Message{Role: "user", Content: userText, Images: images})
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
		if note, err := o.maybeCompact(ctx); err != nil {
			// Compaction failure must not kill the turn: the
			// conversation continues uncompacted and the user sees it.
			events <- Event{Kind: EventError, Err: fmt.Errorf("compaction skipped: %w", err)}
		} else if note != "" {
			events <- Event{Kind: EventCompaction, Text: note}
		}
		// Copy: the request must be a snapshot. Handing out the live
		// slice lets any later append (the next turn, a subagent)
		// reach into a request that already went out.
		msgs := make([]llm.Message, len(o.history))
		copy(msgs, o.history)
		req := llm.ChatRequest{
			Model:    o.Model,
			System:   o.systemPrompt(),
			Messages: msgs,
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
			case llm.ReasoningEvent:
				// Thinking is for the UI; the answer is what the
				// conversation keeps.
				events <- Event{Kind: EventReasoning, Text: ev.Text}
			case llm.TextEvent:
				content += ev.Text
				events <- Event{Kind: EventText, Text: ev.Text}
			case llm.ToolCallEvent:
				calls = append(calls, ev.Call)
			case llm.UsageEvent:
				o.lastPromptTokens = ev.Usage.PromptTokens
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

// toolDefs builds the tool list the model sees, filtered by the active
// mode: read-only mode never offers state-changing tools.
func (o *Orchestrator) toolDefs() []llm.Tool {
	var out []llm.Tool
	for _, d := range o.Registry.Defs() {
		// The policy owns which tiers each mode offers (plan mode also
		// offers drafts — present_plan — which is how planning ends).
		if !tools.ModeAllowsTier(o.Mode, d.Tier) {
			continue
		}
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

// CompactionFraction is the share of the context window at which the
// oldest messages get summarized (spec: ~75%).
const CompactionFraction = 0.75

// keepRecent is how many of the newest messages survive a compaction
// verbatim; everything older becomes the recap.
const keepRecent = 4

// maybeCompact summarizes the oldest history into a recap when the
// previous round's prompt size crossed the configured threshold. A
// note is returned when compaction happened; an error means it was
// attempted and failed (the caller continues regardless).
func (o *Orchestrator) maybeCompact(ctx context.Context) (string, error) {
	if o.ContextWindow <= 0 || o.lastPromptTokens == 0 {
		return "", nil
	}
	if o.lastPromptTokens < int(float64(o.ContextWindow)*CompactionFraction) {
		return "", nil
	}
	if len(o.history) <= keepRecent+2 {
		return "", nil // not enough worth summarizing
	}
	old := o.history[:len(o.history)-keepRecent]
	recent := o.history[len(o.history)-keepRecent:]

	summary, err := o.summarize(ctx, old)
	if err != nil {
		return "", err
	}
	recap := llm.Message{Role: "user", Content: "Context recap (earlier conversation, auto-summarized):\n" + summary}
	o.history = append([]llm.Message{recap}, recent...)
	// Reset the signal so the next round reports fresh size rather than
	// immediately re-triggering.
	o.lastPromptTokens = 0
	return fmt.Sprintf("summarized %d older messages into a recap (%d kept verbatim)", len(old), keepRecent), nil
}

// summarize runs the summarizer round through the provider — the same
// interface, optionally a cheaper model (Section 3.2: keep the
// summarizer swappable).
func (o *Orchestrator) summarize(ctx context.Context, old []llm.Message) (string, error) {
	model := o.CompactionModel
	if model == "" {
		model = o.Model
	}
	stream, err := o.Provider.StreamChat(ctx, llm.ChatRequest{
		Model:    model,
		System:   "Summarize this conversation compactly: the tasks, decisions, results, and open threads. Reply with the summary only.",
		Messages: old,
	})
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for ev := range stream {
		switch ev.Type {
		case llm.TextEvent:
			b.WriteString(ev.Text)
		case llm.ErrorEvent:
			// Drain the rest so the provider goroutine exits.
			for range stream {
			}
			return "", ev.Err
		}
	}
	s := strings.TrimSpace(b.String())
	if s == "" {
		return "", fmt.Errorf("summarizer returned nothing")
	}
	return s, nil
}
