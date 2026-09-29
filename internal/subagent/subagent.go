// Package subagent implements the Subagent Manager (Section 3.3): a
// subagent is just another Orchestrator instance, scoped to a narrower
// system prompt and a tool subset, running in its own goroutine —
// in-process, same trust boundary as the harness itself.
//
// The parent model reaches subagents through the spawn_subagent tool;
// progress flows back as title-labeled events so the TUI can show what
// each subagent is doing, not just the final result.
package subagent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/Chmgx81/tilde/internal/llm"
	"github.com/Chmgx81/tilde/internal/orchestrator"
	"github.com/Chmgx81/tilde/internal/tools"
)

// Event kinds a subagent emits.
const (
	EventText  = "text"  // streamed assistant text
	EventTool  = "tool"  // a tool call was dispatched
	EventDone  = "done"  // the subagent finished
	EventError = "error" // the subagent failed
	EventUsage = "usage" // token usage for one subagent round
)

// Event is one progress report from a running subagent.
type Event struct {
	Title string
	Kind  string
	Text  string
	Usage llm.Usage
}

// Emitter is a settable event sink. The spawn tool needs one at
// construction time, but the TUI (the real sink) only exists later —
// so main wires it after the fact and events before then are dropped.
type Emitter struct {
	mu   sync.Mutex
	emit func(Event)
}

func (e *Emitter) Set(fn func(Event)) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.emit = fn
}

func (e *Emitter) Emit(ev Event) {
	e.mu.Lock()
	fn := e.emit
	e.mu.Unlock()
	if fn != nil {
		fn(ev)
	}
}

// Runner builds sub-orchestrators. The provider, model, and gate are
// the parent's — same trust boundary, same audit log, same permission
// prompts — while the system prompt narrows and the tool subset drops
// spawn tools so subagents cannot recurse.
type Runner struct {
	Provider llm.Provider
	Model    string
	Registry *tools.Registry
	Gate     *tools.Gate
	// EffortOf returns the parent's live reasoning effort — the
	// subagent inherits the posture at spawn time; there is
	// deliberately no independent knob. Nil means unset.
	EffortOf func() string
}

// subagentSystemPrompt scopes the subagent: finish the task, answer
// with the result, no chatter.
const subagentSystemPrompt = "You are a subagent. Complete the assigned task with the available tools. " +
	"When done, reply with the final result only — no preamble, no questions."

// subset builds a registry with every spawn tool removed. Excluding
// the spawn tool by name prefix is the recursion guard: there is no
// depth counter because there is no second level to count.
func subset(reg *tools.Registry) *tools.Registry {
	var out tools.Registry
	for _, t := range reg.All() {
		if strings.HasPrefix(t.Name(), "spawn_") {
			continue
		}
		out.Register(t)
	}
	return &out
}

// Run drives one subagent turn for task and returns its final answer.
// Progress is reported through emit as it happens; blocking here is
// fine because the parent dispatches tool calls on its own goroutine
// and the TUI renders the events live.
func (r *Runner) Run(ctx context.Context, task, title string, emit func(Event)) (string, error) {
	orch := orchestrator.New(r.Provider, r.Model, subagentSystemPrompt, subset(r.Registry), r.Gate)
	if r.EffortOf != nil {
		orch.SetEffort(r.EffortOf())
	}
	ch := orch.Send(ctx, task)

	// The subagent's final answer is the text streamed after its last
	// tool call.
	var answer string
	var cur strings.Builder
	for ev := range ch {
		e := Event{Title: title}
		switch ev.Kind {
		case orchestrator.EventText:
			cur.WriteString(ev.Text)
			e.Kind, e.Text = EventText, ev.Text
		case orchestrator.EventToolStart:
			cur.Reset()
			e.Kind, e.Text = EventTool, ev.ToolCall.Name+" "+ev.ToolCall.Arguments
		case orchestrator.EventUsage:
			e.Kind, e.Usage = EventUsage, ev.Usage
		case orchestrator.EventTurnComplete:
			answer = cur.String()
			e.Kind, e.Text = EventDone, answer
		case orchestrator.EventError:
			e.Kind, e.Text = EventError, ev.Err.Error()
		default:
			continue
		}
		emit(e)
		if ev.Kind == orchestrator.EventError {
			return "", fmt.Errorf("subagent: %s", ev.Err)
		}
	}
	if strings.TrimSpace(answer) == "" {
		return "the subagent finished without a final answer", nil
	}
	return answer, nil
}

// SpawnTool is the parent-facing tool. Action-Allowed tier: it spends
// API budget.
type SpawnTool struct {
	Runner  *Runner
	Emitter *Emitter
}

func (SpawnTool) Name() string { return "spawn_subagent" }

func (SpawnTool) Description() string {
	return "Delegate a self-contained task to a focused subagent that works with the same tools and reports back a final result. Use it for work worth doing in isolation, like auditing a directory or drafting a document; the subagent cannot talk to the user."
}

func (SpawnTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"task": {"type": "string", "description": "Complete, self-contained instructions for the subagent"},
			"title": {"type": "string", "description": "Short label for the subagent shown to the user (optional)"}
		},
		"required": ["task"]
	}`)
}

func (SpawnTool) Tier() tools.Tier { return tools.TierActionAllowed }

func (t SpawnTool) Execute(ctx context.Context, args string) (string, error) {
	var a struct {
		Task  string `json:"task"`
		Title string `json:"title"`
	}
	if args == "" {
		args = "{}"
	}
	if err := json.Unmarshal([]byte(args), &a); err != nil {
		return "", fmt.Errorf("spawn_subagent: bad arguments: %w", err)
	}
	if strings.TrimSpace(a.Task) == "" {
		return "", fmt.Errorf("spawn_subagent: task is required")
	}
	title := strings.TrimSpace(a.Title)
	if title == "" {
		// A usable label from the task's first words.
		words := strings.Fields(a.Task)
		if len(words) > 5 {
			words = words[:5]
		}
		title = strings.Join(words, " ")
	}
	return t.Runner.Run(ctx, a.Task, title, t.Emitter.Emit)
}
