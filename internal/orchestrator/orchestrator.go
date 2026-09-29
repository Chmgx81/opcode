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
	"time"

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
	EventError        = "error"         // the turn is OVER; Err carries why
	// EventCompactionFailed: compaction was attempted and failed —
	// the turn CONTINUES uncompacted. This is deliberately not
	// EventError: the TUI ends the turn on EventError, and a turn
	// wrongly marked dead while its goroutine still runs lets a
	// second Send race the first (audit C2).
	EventCompactionFailed = "compaction_failed"
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
// bash output in particular can be enormous; the model gets the head
// of it and a note that it was truncated.
const maxToolResultChars = 50_000

// Orchestrator owns the conversation state for one session.
type Orchestrator struct {
	Provider llm.Provider
	Model    string
	System   string
	Registry *tools.Registry
	Gate     *tools.Gate
	// Mode is the active permission mode. It shapes the system prompt
	// (ModeInstruction) and the gate's posture; every mode still
	// offers every tool — the gate decides, not the offer.
	Mode string
	// ReasoningEffort is the thinking knob: low, medium, high, or
	// empty for the provider's default. Per-request like Mode;
	// SetEffort switches it mid-session.
	ReasoningEffort string
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
	// baselineTokens/baselineSet anchor the prefill: the first
	// report of the session (or of the stretch since the last
	// compaction). Compaction reacts to GROWTH past the baseline,
	// never to the baseline itself — a resumed session at 80% of
	// the window must not compact on arrival.
	baselineTokens   int
	baselineSet      bool
	lastPromptTokens int

	// turnWg tracks the in-flight turn goroutine so the exit path can
	// wait for it (see WaitIdle) instead of racing it.
	turnWg sync.WaitGroup

	// cfgMu guards the per-request knobs (Provider, Model, Mode,
	// ReasoningEffort) against mid-turn writes from the UI — a mode
	// or effort switch, a re-login. The turn loop snapshots them
	// under the lock when it builds a request (audit C6).
	cfgMu sync.Mutex

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
// SetEffort switches the reasoning effort; the next model request
// carries it.
func (o *Orchestrator) SetEffort(effort string) {
	o.cfgMu.Lock()
	defer o.cfgMu.Unlock()
	o.ReasoningEffort = effort
}

// SetMode switches the permission mode. The next model request picks
// up the new mode instruction; the gate's Decide callback is the
// caller's to rebuild.
func (o *Orchestrator) SetMode(mode string) {
	o.cfgMu.Lock()
	defer o.cfgMu.Unlock()
	o.Mode = tools.NormalizeMode(mode)
}

// SetProvider swaps the LLM provider (a re-login or key change does
// this mid-session). The next model request uses it.
func (o *Orchestrator) SetProvider(p llm.Provider) {
	o.cfgMu.Lock()
	defer o.cfgMu.Unlock()
	o.Provider = p
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

// WaitIdle blocks until no turn goroutine is running, or the timeout
// passes. The exit path calls it after cancelling the turn: the session
// save must not race a live turn's appends to history (audit C3). A
// wedged provider cannot hold the exit hostage — the timeout keeps
// quitting bounded.
func (o *Orchestrator) WaitIdle(timeout time.Duration) bool {
	done := make(chan struct{})
	go func() { o.turnWg.Wait(); close(done) }()
	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
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
	o.turnWg.Add(1)
	go func() {
		defer close(events)
		defer o.turnWg.Done()
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
		pos := InjectRecapFirst
		if round > 0 {
			// Mid-turn: the history ends in tool results and the
			// recap belongs at the end, where attention lives.
			pos = InjectRecapLast
		}
		if note, err := o.maybeCompact(ctx, pos); err != nil {
			// Compaction failure must not kill the turn: the
			// conversation continues uncompacted and the user sees it
			// — as a non-terminal notice, never EventError.
			events <- Event{Kind: EventCompactionFailed, Err: err}
		} else if note != "" {
			events <- Event{Kind: EventCompaction, Text: note}
		}
		// Copy: the request must be a snapshot. Handing out the live
		// slice lets any later append (the next turn, a subagent)
		// reach into a request that already went out. The knobs come
		// off the same snapshot, read under cfgMu so a mid-turn mode
		// or effort switch cannot tear the request.
		msgs := make([]llm.Message, len(o.history))
		copy(msgs, o.history)
		o.cfgMu.Lock()
		provider, model, system, effort := o.Provider, o.Model, o.systemPrompt(), o.ReasoningEffort
		o.cfgMu.Unlock()
		req := llm.ChatRequest{
			Model:           model,
			System:          system,
			Messages:        msgs,
			Tools:           o.toolDefs(),
			ReasoningEffort: effort,
		}
		stream, err := provider.StreamChat(ctx, req)
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
				// result; it can correct itself. The tool's own
				// output — the shell's stderr, a sandbox-denial
				// note — is the evidence of what actually failed,
				// so it stays beside the error headline; without
				// it the model could not see the failure's cause.
				// Only harness-level errors end the turn.
				out := strings.TrimSpace(result)
				result = "error: " + err.Error()
				if out != "" {
					result += "\n" + out
				}
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

// toolDefs builds the tool list the model sees. Every mode offers
// every tier (Phase 30: hiding tools taught the model nothing and
// broke planning) — the posture bites at the gate, not the offer.
func (o *Orchestrator) toolDefs() []llm.Tool {
	var out []llm.Tool
	for _, d := range o.Registry.Defs() {
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

// InjectionPos says where a compaction recap lands in the history.
// Two values, because there are exactly two right answers: pre-turn
// compaction must not bury the user's message, and mid-turn
// compaction must not bury the recap. The enum makes the wrong
// placement unrepresentable at the call site.
type InjectionPos int

const (
	// InjectRecapFirst: pre-turn (round 0, history ending in the
	// user's new message). The recap opens the history; the turn's
	// exchange stays at the end, where models are trained to find
	// it.
	InjectRecapFirst InjectionPos = iota
	// InjectRecapLast: mid-turn (after tool rounds, history ending
	// in tool results). The recap is the most recent item — the
	// end of the window is where attention lives.
	InjectRecapLast
)

// maybeCompact summarizes the oldest history into a recap when the
// conversation has GROWN past the prefill baseline by
// CompactionFraction of the window's remaining space. A note is
// returned when compaction happened; an error means it was
// attempted and failed (the caller continues regardless).
func (o *Orchestrator) maybeCompact(ctx context.Context, pos InjectionPos) (string, error) {
	if o.ContextWindow <= 0 || o.lastPromptTokens == 0 {
		return "", nil
	}
	if !o.baselineSet {
		// The prefill anchor: the first report of the session, or
		// of the stretch since the last compaction. A heavy
		// prefill — a resumed session, a long system prompt — is
		// the starting point, not growth.
		o.baselineTokens = o.lastPromptTokens
		o.baselineSet = true
	}
	remaining := o.ContextWindow - o.baselineTokens
	if remaining < 0 {
		// The window is already over-full at the baseline; any
		// growth must trigger.
		remaining = 0
	}
	growth := o.lastPromptTokens - o.baselineTokens
	if growth < int(float64(remaining)*CompactionFraction) {
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
	switch pos {
	case InjectRecapLast:
		o.history = append(recent, recap)
	default:
		o.history = append([]llm.Message{recap}, recent...)
	}
	// Reset the signal AND the baseline: the compacted size is the
	// new anchor, so the next report measures fresh growth instead
	// of instantly re-triggering.
	o.lastPromptTokens = 0
	o.baselineTokens, o.baselineSet = 0, false
	return fmt.Sprintf("summarized %d older messages into a recap (%d kept verbatim)", len(old), keepRecent), nil
}

// summarize runs the summarizer round through the provider — the same
// interface, optionally a cheaper model (Section 3.2: keep the
// summarizer swappable).
func (o *Orchestrator) summarize(ctx context.Context, old []llm.Message) (string, error) {
	o.cfgMu.Lock()
	compactionModel := o.CompactionModel
	if compactionModel == "" {
		compactionModel = o.Model
	}
	provider := o.Provider
	o.cfgMu.Unlock()
	stream, err := provider.StreamChat(ctx, llm.ChatRequest{
		Model:    compactionModel,
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
