package subagent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Chmgx81/opcode/internal/llm"
	"github.com/Chmgx81/opcode/internal/tools"
)

// scriptedProvider serves one round per StreamChat call and records
// every request, including whether it came from a subagent (detected
// via the system prompt).
type scriptedProvider struct {
	mu          sync.Mutex
	rounds      [][]llm.ChatEvent
	gotRequests []llm.ChatRequest
}

func (p *scriptedProvider) StreamChat(ctx context.Context, req llm.ChatRequest) (<-chan llm.ChatEvent, error) {
	p.mu.Lock()
	p.gotRequests = append(p.gotRequests, req)
	if len(p.rounds) == 0 {
		p.mu.Unlock()
		return nil, fmt.Errorf("no scripted round left")
	}
	round := p.rounds[0]
	p.rounds = p.rounds[1:]
	p.mu.Unlock()
	ch := make(chan llm.ChatEvent, len(round))
	for _, ev := range round {
		ch <- ev
	}
	close(ch)
	return ch, nil
}

func (p *scriptedProvider) requests() []llm.ChatRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]llm.ChatRequest(nil), p.gotRequests...)
}

func newRunner(t *testing.T, dir string, rounds [][]llm.ChatEvent) (*Runner, *scriptedProvider, *tools.Registry) {
	t.Helper()
	fp := &scriptedProvider{rounds: rounds}
	var reg tools.Registry
	reg.Register(tools.ReadFile{})
	reg.Register(tools.WriteFile{})
	gate := &tools.Gate{Audit: tools.NewAuditLog(filepath.Join(dir, "audit.jsonl"), nil)}
	return &Runner{Provider: fp, Model: "test-model", Registry: &reg, Gate: gate}, fp, &reg
}

func TestRunReturnsFinalAnswerAndEvents(t *testing.T) {
	dir := t.TempDir()
	runner, fp, _ := newRunner(t, dir, [][]llm.ChatEvent{
		// Round 1: a tool call, then round 2: the final answer.
		{{Type: llm.ToolCallEvent, Call: llm.ToolCall{
			ID: "c1", Name: "read_file", Arguments: `{"path": "missing-ok"}`}}},
		{{Type: llm.TextEvent, Text: "the audit found 3 files"},
			{Type: llm.UsageEvent, Usage: llm.Usage{PromptTokens: 10, CompletionTokens: 5}}},
	})

	var events []Event
	emit := func(ev Event) { events = append(events, ev) }

	answer, err := runner.Run(context.Background(), "audit the directory", "auditor", emit)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if answer != "the audit found 3 files" {
		t.Errorf("answer = %q", answer)
	}

	var kinds []string
	for _, ev := range events {
		kinds = append(kinds, ev.Kind)
	}
	// Tool, then text, usage, done — in order, all labeled.
	want := []string{EventTool, EventText, EventUsage, EventDone}
	if len(kinds) != len(want) {
		t.Fatalf("event kinds = %v, want %v", kinds, want)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Errorf("kinds[%d] = %s, want %s", i, kinds[i], want[i])
		}
	}
	for _, ev := range events {
		if ev.Title != "auditor" {
			t.Errorf("event not labeled: %+v", ev)
		}
	}

	// The subagent's request carries the subagent system prompt.
	reqs := fp.requests()
	if len(reqs) == 0 || !strings.Contains(reqs[0].System, "subagent") {
		t.Errorf("subagent system prompt missing: %+v", reqs)
	}
}

func TestRunNoRecursionAndNoSpawnTool(t *testing.T) {
	fp := &scriptedProvider{rounds: [][]llm.ChatEvent{
		{{Type: llm.TextEvent, Text: "done"}},
	}}
	var reg tools.Registry
	reg.Register(tools.ReadFile{})
	em := &Emitter{}
	reg.Register(SpawnTool{Runner: &Runner{Provider: fp, Model: "m", Registry: &reg, Gate: &tools.Gate{}}, Emitter: em})

	runner := &Runner{Provider: fp, Model: "m", Registry: &reg, Gate: &tools.Gate{}}
	_, err := runner.Run(context.Background(), "task", "t", func(Event) {})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// The subagent saw every non-spawn tool but NOT spawn_subagent —
	// recursion is impossible by construction.
	reqs := fp.requests()
	if len(reqs) != 1 {
		t.Fatalf("got %d requests", len(reqs))
	}
	for _, tool := range reqs[0].Tools {
		if tool.Name == "spawn_subagent" {
			t.Error("subagent was offered spawn_subagent — recursion must be excluded by construction")
		}
		if tool.Name != "read_file" {
			t.Errorf("unexpected tool offered: %s", tool.Name)
		}
	}
}

func TestRunErrorPropagates(t *testing.T) {
	dir := t.TempDir()
	runner, _, _ := newRunner(t, dir, [][]llm.ChatEvent{
		{{Type: llm.ErrorEvent, Err: fmt.Errorf("provider blew up")}},
	})
	var events []Event
	emit := func(ev Event) { events = append(events, ev) }

	if _, err := runner.Run(context.Background(), "task", "t", emit); err == nil {
		t.Fatal("expected error to propagate")
	}
	if len(events) == 0 || events[len(events)-1].Kind != EventError {
		t.Errorf("error event not emitted: %+v", events)
	}
}

func TestRunEmptyAnswerIsExplicit(t *testing.T) {
	dir := t.TempDir()
	// The subagent finishes a round with no text at all (usage only).
	runner, _, _ := newRunner(t, dir, [][]llm.ChatEvent{
		{{Type: llm.UsageEvent, Usage: llm.Usage{PromptTokens: 1}}},
	})
	answer, err := runner.Run(context.Background(), "task", "t", func(Event) {})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(answer, "without a final answer") {
		t.Errorf("answer = %q, want the explicit no-answer result", answer)
	}
}

func TestSpawnTool(t *testing.T) {
	dir := t.TempDir()
	// Each Execute consumes a scripted round, so each call gets a
	// fresh runner.
	freshRunner := func() *Runner {
		r, _, _ := newRunner(t, dir, [][]llm.ChatEvent{
			{{Type: llm.TextEvent, Text: "subagent result"}},
		})
		return r
	}
	em := &Emitter{}
	tool := SpawnTool{Runner: freshRunner(), Emitter: em}

	if tool.Tier() != tools.TierActionAllowed {
		t.Error("spawn_subagent must be Action-Allowed (paid API call)")
	}

	out, err := tool.Execute(context.Background(), `{"task": "do the thing", "title": "worker"}`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out != "subagent result" {
		t.Errorf("out = %q", out)
	}

	// Missing task is a correctable error; garbage JSON likewise.
	if _, err := (SpawnTool{Runner: freshRunner(), Emitter: em}).Execute(context.Background(), `{}`); err == nil {
		t.Error("missing task must error")
	}
	if _, err := (SpawnTool{Runner: freshRunner(), Emitter: em}).Execute(context.Background(), `not json`); err == nil {
		t.Error("malformed args must error")
	}

	// No title: the label falls back to the task's first words.
	var seen Event
	em.Set(func(ev Event) { seen = ev })
	if _, err := (SpawnTool{Runner: freshRunner(), Emitter: em}).Execute(
		context.Background(), `{"task": "count the files in the directory"}`); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.HasPrefix(seen.Title, "count the files") {
		t.Errorf("fallback title = %q", seen.Title)
	}
}

func TestEmitterSetAndDrop(t *testing.T) {
	em := &Emitter{}
	em.Emit(Event{Kind: "x"}) // no sink yet: dropped, not panicked

	var got []Event
	em.Set(func(ev Event) { got = append(got, ev) })
	em.Emit(Event{Kind: "y"})
	if len(got) != 1 || got[0].Kind != "y" {
		t.Errorf("got = %+v", got)
	}
}

func TestSubagentToolCallsShareTheGate(t *testing.T) {
	dir := t.TempDir()
	// The write path must be absolute and inside the temp dir: a
	// relative one resolves against the test binary's working
	// directory, which is the package source, and the test then
	// leaves x.txt in the repo (it was committed once).
	target, err := filepath.Abs(filepath.Join(dir, "x.txt"))
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	args, err := json.Marshal(map[string]string{"path": target, "content": "hi"})
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	// The subagent's tool call must land in the same audit log as the
	// parent's — one trust boundary.
	runner, fp, _ := newRunner(t, dir, [][]llm.ChatEvent{
		{{Type: llm.ToolCallEvent, Call: llm.ToolCall{
			ID: "c1", Name: "write_file", Arguments: string(args)}}},
		{{Type: llm.TextEvent, Text: "written"}},
	})
	_, err = runner.Run(context.Background(), "write x.txt", "writer", func(Event) {})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	_ = fp
	auditData, err := os.ReadFile(filepath.Join(dir, "audit.jsonl"))
	if err != nil {
		t.Fatalf("audit log: %v", err)
	}
	audit := string(auditData)
	if !strings.Contains(audit, "write_file") || !strings.Contains(audit, `"allowed":true`) {
		t.Errorf("subagent tool call missing from the shared audit log: %s", audit)
	}
}
