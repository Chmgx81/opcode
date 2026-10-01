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

// maxCallsPerRound bounds the tool calls dispatched from ONE model
// round. MaxToolRounds bounds rounds, not calls: a single round can
// emit thousands of calls, and each one appends its arguments and
// result to history. Past the cap the extra calls are answered with an
// error result rather than dropped — a dropped call would leave its
// assistant tool_calls unanswered, which the next request rejects.
const maxCallsPerRound = 32

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
	// disclosure. Writers must go through SetSkillsIndex: a trust
	// grant updates it from the TUI event goroutine while a live
	// turn reads it under cfgMu every round. systemPrompt itself
	// must only be called with cfgMu held (the request builder does).
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
	//
	// histMu guards history, histGen and the three token fields
	// above. Turns are sequential in the TUI, but History (session
	// save on exit, possibly after a WaitIdle timeout), Seed (resume)
	// and tests reach these from other goroutines. It is never held
	// across a provider call, a tool dispatch or an event send.
	// histGen bumps whenever history is replaced wholesale (Seed,
	// compaction) so a compaction that summarized a now-stale
	// snapshot can tell and discard its result.
	histMu           sync.Mutex
	histGen          uint64
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
		Mode:     tools.ModeBuild,
	}
}

// SetEffort switches the reasoning effort; the next model request
// carries it.
func (o *Orchestrator) SetEffort(effort string) {
	o.cfgMu.Lock()
	defer o.cfgMu.Unlock()
	o.ReasoningEffort = effort
}

// Effort reports the live reasoning effort. Read it through this method,
// never off the field: SetEffort is called from the UI goroutine while
// the turn goroutine is streaming, so the field alone is a data race
// (the subagent runner reads it for every child it starts).
func (o *Orchestrator) Effort() string {
	o.cfgMu.Lock()
	defer o.cfgMu.Unlock()
	return o.ReasoningEffort
}

// SetMode switches the permission mode. The next model request picks
// up the new mode instruction; the gate's Decide callback is the
// caller's to rebuild.
func (o *Orchestrator) SetMode(mode string) {
	o.cfgMu.Lock()
	defer o.cfgMu.Unlock()
	o.Mode = tools.NormalizeMode(mode)
}

// SetSkillsIndex replaces the skills index. Like the other per-request
// knobs it is read under cfgMu when a request is built, so the write
// takes the same lock — a mid-turn trust grant must not race the turn.
func (o *Orchestrator) SetSkillsIndex(index string) {
	o.cfgMu.Lock()
	defer o.cfgMu.Unlock()
	o.SkillsIndex = index
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
	o.histMu.Lock()
	defer o.histMu.Unlock()
	return o.snapshotLocked()
}

// snapshotLocked copies the history; histMu must be held.
func (o *Orchestrator) snapshotLocked() []llm.Message {
	out := make([]llm.Message, len(o.history))
	copy(out, o.history)
	return out
}

// appendHistory adds messages to the conversation atomically.
func (o *Orchestrator) appendHistory(msgs ...llm.Message) {
	o.histMu.Lock()
	defer o.histMu.Unlock()
	o.history = append(o.history, msgs...)
}

// setPromptTokens records the provider-reported prompt size of the
// latest request — the compaction trigger signal.
func (o *Orchestrator) setPromptTokens(n int) {
	o.histMu.Lock()
	defer o.histMu.Unlock()
	o.lastPromptTokens = n
}

// Seed replaces the conversation with a loaded session's history, so
// a resumed session continues where it left off. The compaction
// signal resets with it: token counts measured against the previous
// conversation say nothing about this one, and a stale baseline would
// trigger a spurious compaction of the freshly loaded session.
func (o *Orchestrator) Seed(history []llm.Message) {
	o.histMu.Lock()
	defer o.histMu.Unlock()
	o.history = append([]llm.Message(nil), history...)
	o.histGen++
	o.lastPromptTokens = 0
	o.baselineTokens, o.baselineSet = 0, false
}

// Steer queues a steering message for the in-flight turn. It is appended
// to history as a user message before the next model request, so the
// model sees it as soon as the current tool call is done.
func (o *Orchestrator) Steer(text string) {
	o.steerMu.Lock()
	defer o.steerMu.Unlock()
	o.pendingSteer = append(o.pendingSteer, text)
}

// drainSteerIntoHistory folds every pending steering message into the
// history as user messages. The turn calls it at each round boundary;
// a new message calls it first, so a steer that missed the last
// boundary still lands where the user meant it.
func (o *Orchestrator) drainSteerIntoHistory() {
	steers := o.drainSteer()
	if len(steers) == 0 {
		return
	}
	msgs := make([]llm.Message, len(steers))
	for i, steer := range steers {
		msgs[i] = llm.Message{Role: "user", Content: steer}
	}
	o.appendHistory(msgs...)
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
		// A steer typed after the previous turn's last round boundary
		// is still queued here. Land it BEFORE the new message, or it
		// rides into this turn after the question it was meant to
		// refine — the transcript shows it under the earlier turn, so
		// any other order makes the display and the model's context
		// disagree.
		o.drainSteerIntoHistory()
		o.appendHistory(llm.Message{Role: "user", Content: userText, Images: images})
		if err := o.runTurn(ctx, events); err != nil {
			if ctx.Err() != nil {
				err = ErrCancelled
			}
			events <- Event{Kind: EventError, Err: err}
		}
	}()
	return events
}

// ErrEmptyResponse ends a turn whose model reply carried neither text
// nor tool calls.
var ErrEmptyResponse = errors.New("the model returned an empty response — send your message again, or switch models with /model")

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
		o.drainSteerIntoHistory()
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
			// A cancel that landed mid-summary is the interrupt, not a
			// compaction problem: say only that.
			if ctx.Err() != nil {
				return ErrCancelled
			}
			events <- Event{Kind: EventCompactionFailed, Err: err}
		} else if note != "" {
			events <- Event{Kind: EventCompaction, Text: note}
		}
		// Copy: the request must be a snapshot. Handing out the live
		// slice lets any later append (the next turn, a subagent)
		// reach into a request that already went out. The knobs come
		// off the same snapshot, read under cfgMu so a mid-turn mode
		// or effort switch cannot tear the request.
		msgs := o.History()
		// Every provider 400s on a malformed list, and a 400 is the
		// worst possible report of our own bookkeeping bug: it says
		// nothing about which message is wrong. Check the wire
		// invariants here so the turn fails with the offending index.
		if err := Validate(msgs); err != nil {
			return fmt.Errorf("refusing to send a malformed history to the model: %w", err)
		}
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
				o.setPromptTokens(ev.Usage.PromptTokens)
				events <- Event{Kind: EventUsage, Usage: ev.Usage}
			case llm.ErrorEvent:
				return ev.Err
			}
		}

		// A call the provider emitted without an id has no tool result
		// it could be attached to, and one without a name has nothing
		// to run. Recording either as a call is a hard 400 on the next
		// request — an assistant tool_call the provider cannot match —
		// so they are kept out of the assistant message entirely and
		// reported as ordinary user notes afterwards.
		calls, unanswerable := partitionCalls(calls)
		if content == "" && len(calls) == 0 && len(unanswerable) == 0 {
			// Nothing came back: no text, no calls. Recording an
			// empty assistant message would poison the session (the
			// Anthropic wire rejects an empty text block on every
			// later request), and ending the turn quietly would
			// look like tilde hung. Say so; the user can resend.
			return ErrEmptyResponse
		}
		o.appendHistory(llm.Message{
			Role:      "assistant",
			Content:   content,
			ToolCalls: calls,
		})

		if len(calls) == 0 {
			o.reportMalformedCalls(events, unanswerable)
			events <- Event{Kind: EventTurnComplete}
			return nil
		}

		for i, call := range calls {
			events <- Event{Kind: EventToolStart, ToolCall: call}
			var result string
			if i >= maxCallsPerRound {
				// Over the cap. The call is still answered, so the
				// assistant's tool_calls each have their result; the
				// model can recover on the next round instead of
				// losing the whole round to a 400.
				result = fmt.Sprintf("error: too many tool calls in one round; only the first %d were run", maxCallsPerRound)
			} else {
				out, err := o.dispatch(ctx, call)
				if err != nil {
					if ctx.Err() != nil {
						o.answerCancelledCalls(calls[i:])
						return ErrCancelled
					}
					// Tool failures go back to the model as the tool
					// result; it can correct itself. The tool's own
					// output — the shell's stderr, a sandbox-denial
					// note — is the evidence of what actually failed,
					// so it stays beside the error headline; without
					// it the model could not see the failure's cause.
					// Only harness-level errors end the turn.
					result = "error: " + err.Error()
					if trimmed := strings.TrimSpace(out); trimmed != "" {
						result += "\n" + trimmed
					}
				} else {
					result = out
				}
			}
			o.appendHistory(llm.Message{
				Role:       "tool",
				ToolCallID: call.ID,
				Content:    truncate(result),
			})
			events <- Event{Kind: EventToolResult, ToolCall: call, ToolResult: result}
		}
		o.reportMalformedCalls(events, unanswerable)
	}
}

// reportMalformedCalls tells the model about the calls that could
// neither be run nor recorded. They arrive as user messages, after the
// round's tool results: a user message between an assistant's tool_calls
// and their results would push the results out of the turn they answer,
// which Anthropic rejects. Each call still gets a start and a result
// event, because the UI renders tool calls from the event stream and a
// call that never resolves would sit there forever.
func (o *Orchestrator) reportMalformedCalls(events chan<- Event, calls []llm.ToolCall) {
	for _, call := range calls {
		result := "error: " + malformedCallError(call)
		o.appendHistory(llm.Message{Role: "user", Content: result})
		events <- Event{Kind: EventToolStart, ToolCall: call}
		events <- Event{Kind: EventToolResult, ToolCall: call, ToolResult: result}
	}
}

// partitionCalls splits a round's calls into the ones a tool result can
// be attached to and the ones that cannot: a call with no id, or with
// no name, has no matching message on the wire. Order is kept within
// each group so the executed calls stay in the order the model emitted.
func partitionCalls(calls []llm.ToolCall) (answerable, unanswerable []llm.ToolCall) {
	for _, call := range calls {
		if call.ID == "" || call.Name == "" {
			unanswerable = append(unanswerable, call)
			continue
		}
		answerable = append(answerable, call)
	}
	return answerable, unanswerable
}

// malformedCallError says why a call cannot be run or recorded.
func malformedCallError(call llm.ToolCall) string {
	switch {
	case call.ID == "" && call.Name == "":
		return "the model emitted a tool call with no id and no name; it was not run"
	case call.ID == "":
		return fmt.Sprintf("the model emitted a call to %q with no id; it was not run", call.Name)
	default:
		return "the model emitted a tool call with no name; it was not run"
	}
}

// answerCancelledCalls gives every tool call an interrupted turn left
// unanswered a result message. Providers reject a history where an
// assistant message's tool calls are not each followed by their
// result, so without this the session would fail on the very next
// request after an interrupt (and on resume).
func (o *Orchestrator) answerCancelledCalls(calls []llm.ToolCall) {
	msgs := make([]llm.Message, len(calls))
	for i, call := range calls {
		msgs[i] = llm.Message{Role: "tool", ToolCallID: call.ID, Content: "error: interrupted by the user before this tool ran to completion"}
	}
	o.appendHistory(msgs...)
}

// dispatch resolves the tool and runs it through the permission gate.
// An unknown tool is reported to the model rather than failing the turn.
// A call that cannot even be named or identified is a harness-level
// error: there is nothing to look up and no result to attach it to, so
// it must not come back as a nil error with a nil tool.
func (o *Orchestrator) dispatch(ctx context.Context, call llm.ToolCall) (string, error) {
	if call.Name == "" {
		return "", errors.New("the model emitted a tool call with no name; there is nothing to run")
	}
	if call.ID == "" {
		return "", errors.New("the model emitted a tool call with no id; its result could not be matched to it")
	}
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
// verbatim; everything older becomes the recap. It is a count, so the
// cut it implies can land in the middle of a tool round — compactionCut
// moves the real cut back to a boundary the wire accepts.
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
	old, gen := o.compactionCandidate()
	if old == nil {
		return "", nil
	}
	// The summarizer round runs without histMu: it is a provider call.
	summary, err := o.summarize(ctx, old)
	if err != nil {
		return "", err
	}
	recap := llm.Message{Role: "user", Content: "Context recap (earlier conversation, auto-summarized):\n" + summary}
	if !o.applyCompaction(gen, len(old), recap, pos) {
		return "", nil // history was replaced meanwhile; the summary is stale
	}
	return fmt.Sprintf("summarized %d older messages into a recap (%d kept verbatim)", len(old), keepRecent), nil
}

// compactionCandidate decides, under histMu, whether compaction is due.
// When it is, it returns a copy of the messages to summarize and the
// history generation they came from; otherwise a nil slice.
func (o *Orchestrator) compactionCandidate() ([]llm.Message, uint64) {
	o.histMu.Lock()
	defer o.histMu.Unlock()
	if o.ContextWindow <= 0 || o.lastPromptTokens == 0 {
		return nil, 0
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
		return nil, 0
	}
	cut := compactionCut(o.history)
	if cut <= 2 {
		// Nothing worth summarizing — either the history is shorter
		// than the kept tail, or the safe boundary sits so far back
		// that summarizing the rest would drop the conversation.
		return nil, 0
	}
	old := make([]llm.Message, cut)
	copy(old, o.history)
	// The summarizer round is a request of its own, and it carries
	// this prefix alone. A prefix that is malformed on its own — a
	// hand-seeded or resumed history that starts with a tool result —
	// would 400, so skip the compaction instead.
	if Validate(old) != nil {
		return nil, 0
	}
	return old, o.histGen
}

// compactionCut returns where the compaction cut may fall: the
// keepRecent-th-from-last boundary, walked back until the tail can
// stand on its own. A count alone is not a safe cut — a tool-heavy
// session puts a tool result at that index, and the tail then opens
// with a tool result whose assistant tool_calls were summarized away.
// OpenAI answers 400 ("tool message without preceding tool_calls") and
// Anthropic 400s on the unknown tool_use_id. So the tail must not
// start on a tool result, and must not start right after an assistant
// that is still carrying tool_calls.
func compactionCut(history []llm.Message) int {
	cut := len(history) - keepRecent
	for cut > 0 && (history[cut].Role == "tool" ||
		(history[cut-1].Role == "assistant" && len(history[cut-1].ToolCalls) > 0)) {
		cut--
	}
	return cut
}

// applyCompaction swaps the summarized prefix (the first nOld messages)
// for the recap. It reports false, changing nothing, when the history
// was replaced since the snapshot (generation mismatch). Anything
// appended after the snapshot survives in the kept tail.
func (o *Orchestrator) applyCompaction(gen uint64, nOld int, recap llm.Message, pos InjectionPos) bool {
	o.histMu.Lock()
	defer o.histMu.Unlock()
	if o.histGen != gen || len(o.history) < nOld {
		return false
	}
	tail := o.history[nOld:]
	next := make([]llm.Message, 0, len(tail)+1)
	switch pos {
	case InjectRecapLast:
		next = append(append(next, tail...), recap)
	default:
		next = append(append(next, recap), tail...)
	}
	o.history = next
	o.histGen++
	// Reset the signal AND the baseline: the compacted size is the
	// new anchor, so the next report measures fresh growth instead
	// of instantly re-triggering.
	o.lastPromptTokens = 0
	o.baselineTokens, o.baselineSet = 0, false
	return true
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

// Validate reports whether msgs is a message list the providers will
// accept. Every invariant here is a hard 400 in both wire formats, and
// each one has been produced by real bookkeeping mistakes: a compaction
// cut that split a tool round in two, a tool call the provider emitted
// without an id, a resumed session whose only content was an image.
// Checking here turns an opaque provider rejection into a message that
// names the offending index.
func Validate(msgs []llm.Message) error {
	// IDs of the tool calls each assistant message is still waiting
	// on. A tool result may only consume one of these, and only while
	// its assistant turn is the one being answered.
	pending := map[string]bool{}
	for i, m := range msgs {
		switch m.Role {
		case "tool":
			if len(pending) == 0 {
				return fmt.Errorf("message %d: tool result for %q has no assistant tool call before it", i, m.ToolCallID)
			}
			if !pending[m.ToolCallID] {
				return fmt.Errorf("message %d: tool result for %q does not match any tool call of the assistant message it follows", i, m.ToolCallID)
			}
			delete(pending, m.ToolCallID)
		case "assistant":
			// A new assistant turn retires the previous one's
			// unanswered calls: the round is over, and the providers
			// would reject the older results at this point anyway.
			pending = map[string]bool{}
			for _, tc := range m.ToolCalls {
				if tc.ID == "" {
					return fmt.Errorf("message %d: assistant calls %q with no id, so no result can match it", i, tc.Name)
				}
				pending[tc.ID] = true
			}
		case "user":
			if m.Content == "" && len(m.Images) == 0 {
				return fmt.Errorf("message %d: user message has no content and no image", i)
			}
		}
	}
	return nil
}
