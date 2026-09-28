package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"tilde/internal/llm"
	"tilde/internal/tools"
)

// fakeProvider replays scripted rounds of events and records every
// request it receives, so tests can assert what the model was told.
type fakeProvider struct {
	rounds      [][]llm.ChatEvent
	gotRequests []llm.ChatRequest
}

func (f *fakeProvider) StreamChat(ctx context.Context, req llm.ChatRequest) (<-chan llm.ChatEvent, error) {
	f.gotRequests = append(f.gotRequests, req)
	if len(f.rounds) == 0 {
		return nil, fmt.Errorf("no scripted round left")
	}
	round := f.rounds[0]
	f.rounds = f.rounds[1:]
	ch := make(chan llm.ChatEvent, len(round))
	for _, ev := range round {
		ch <- ev
	}
	close(ch)
	return ch, nil
}

func newTestOrchestrator(p llm.Provider, gate *tools.Gate, dir string) (*Orchestrator, *tools.Registry) {
	var reg tools.Registry
	reg.Register(tools.ReadFile{})
	reg.Register(tools.WriteFile{})
	reg.Register(tools.EditFile{})
	reg.Register(tools.RunShell{})
	if gate == nil {
		gate = &tools.Gate{}
	}
	return New(p, "test-model", "test system prompt", &reg, gate), &reg
}

func drain(t *testing.T, events <-chan Event) []Event {
	t.Helper()
	var out []Event
	for ev := range events {
		out = append(out, ev)
	}
	return out
}

func TestLoopExecutesToolAndFeedsResultBack(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "hello.txt")
	p := &fakeProvider{
		rounds: [][]llm.ChatEvent{
			{{Type: llm.ToolCallEvent, Call: llm.ToolCall{
				ID: "call-1", Name: "write_file",
				Arguments: fmt.Sprintf(`{"path": %q, "content": "hello from the loop"}`, target),
			}}},
			{{Type: llm.TextEvent, Text: "I wrote the file."}},
		},
	}
	orch, _ := newTestOrchestrator(p, nil, dir)

	events := drain(t, orch.Send(context.Background(), "write hello.txt"))

	// The tool really executed.
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("file was not written: %v", err)
	}
	if string(data) != "hello from the loop" {
		t.Errorf("content = %q", string(data))
	}

	// The turn streamed text and completed.
	var sawText, sawComplete, sawResult bool
	for _, ev := range events {
		switch ev.Kind {
		case EventText:
			sawText = true
		case EventToolResult:
			sawResult = true
			if ev.ToolCall.ID != "call-1" {
				t.Errorf("result event call ID = %q", ev.ToolCall.ID)
			}
			if !strings.Contains(ev.ToolResult, "hello.txt") {
				t.Errorf("tool result = %q", ev.ToolResult)
			}
		case EventTurnComplete:
			sawComplete = true
		}
	}
	if !sawText || !sawComplete || !sawResult {
		t.Errorf("missing events; text=%v complete=%v result=%v", sawText, sawComplete, sawResult)
	}

	// The model saw the real result: round 2's request carries the
	// assistant tool call and the matching tool message.
	if len(p.gotRequests) != 2 {
		t.Fatalf("got %d requests, want 2", len(p.gotRequests))
	}
	round2 := p.gotRequests[1]
	if len(round2.Messages) != 3 {
		t.Fatalf("round 2 has %d messages, want user+assistant+tool", len(round2.Messages))
	}
	asst, tool := round2.Messages[1], round2.Messages[2]
	if asst.Role != "assistant" || len(asst.ToolCalls) != 1 || asst.ToolCalls[0].ID != "call-1" {
		t.Errorf("assistant message = %+v", asst)
	}
	if tool.Role != "tool" || tool.ToolCallID != "call-1" {
		t.Errorf("tool message = %+v", tool)
	}
	if !strings.Contains(tool.Content, "hello.txt") {
		t.Errorf("tool result not fed back: %q", tool.Content)
	}
	// The request must carry the tool definitions.
	if len(round2.Tools) != 4 {
		t.Errorf("round 2 carries %d tools, want 4", len(round2.Tools))
	}
}

func TestLoopUnknownToolReportsToModel(t *testing.T) {
	dir := t.TempDir()
	p := &fakeProvider{
		rounds: [][]llm.ChatEvent{
			{{Type: llm.ToolCallEvent, Call: llm.ToolCall{
				ID: "call-1", Name: "delete_everything", Arguments: `{}`}}},
			{{Type: llm.TextEvent, Text: "ack"}},
		},
	}
	orch, _ := newTestOrchestrator(p, nil, dir)
	drain(t, orch.Send(context.Background(), "go"))

	tool := p.gotRequests[1].Messages[2]
	if !strings.Contains(tool.Content, "no tool named") {
		t.Errorf("unknown tool not reported to model: %q", tool.Content)
	}
}

func TestLoopGateDenialReachesModel(t *testing.T) {
	dir := t.TempDir()
	p := &fakeProvider{
		rounds: [][]llm.ChatEvent{
			{{Type: llm.ToolCallEvent, Call: llm.ToolCall{
				ID: "call-1", Name: "write_file", Arguments: `{"path": "x", "content": "y"}`}}},
			{{Type: llm.TextEvent, Text: "ack"}},
		},
	}
	gate := &tools.Gate{Decide: func(tool tools.Tool, args string) bool {
		return tool.Tier() == tools.TierReadOnly
	}}
	orch, _ := newTestOrchestrator(p, gate, dir)
	events := drain(t, orch.Send(context.Background(), "go"))

	tool := p.gotRequests[1].Messages[2]
	if !strings.Contains(tool.Content, "permission denied") {
		t.Errorf("denial not fed back to model: %q", tool.Content)
	}
	var deniedResult bool
	for _, ev := range events {
		if ev.Kind == EventToolResult && strings.Contains(ev.ToolResult, "permission denied") {
			deniedResult = true
		}
	}
	if !deniedResult {
		t.Error("no tool_result event carried the denial")
	}
}

func TestLoopProviderErrorEndsTurnWithError(t *testing.T) {
	dir := t.TempDir()
	p := &fakeProvider{
		rounds: [][]llm.ChatEvent{
			{{Type: llm.ErrorEvent, Err: fmt.Errorf("boom")}},
		},
	}
	orch, _ := newTestOrchestrator(p, nil, dir)
	events := drain(t, orch.Send(context.Background(), "go"))
	if len(events) != 1 || events[0].Kind != EventError || events[0].Err == nil {
		t.Fatalf("events = %+v, want a single error event", events)
	}
}

func TestLoopStopsAtMaxRounds(t *testing.T) {
	dir := t.TempDir()
	// Every scripted round calls the same tool again: the loop must
	// stop with an error instead of spinning forever.
	call := llm.ToolCall{ID: "c", Name: "read_file", Arguments: `{"path": "whatever"}`}
	round := []llm.ChatEvent{{Type: llm.ToolCallEvent, Call: call}}
	p := &fakeProvider{}
	for i := 0; i < MaxToolRounds+5; i++ {
		p.rounds = append(p.rounds, round)
	}
	orch, _ := newTestOrchestrator(p, nil, dir)
	events := drain(t, orch.Send(context.Background(), "go"))

	var last error
	for _, ev := range events {
		if ev.Kind == EventError {
			last = ev.Err
		}
	}
	if last == nil || !strings.Contains(last.Error(), "tool rounds") {
		t.Errorf("expected max-rounds error, got %v", last)
	}
}

// TestLoopThroughRealSSEClient runs the whole Phase 0 stack except the
// actual network: real OpenAI-compatible client against a scripted SSE
// server, real tools, real gate. This is the closest thing to the true
// end-to-end loop that doesn't need an API key.
func TestLoopThroughRealSSEClient(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "notes.txt")

	var requestCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.Header().Set("Content-Type", "text/event-stream")
		if requestCount == 1 {
			// Round 1: the model calls write_file, arguments split
			// across several SSE chunks to exercise reassembly.
			// strconv.Quote keeps the nested JSON-string escaping
			// honest whatever the temp path contains.
			chunks := []string{
				fmt.Sprintf(`{"choices": [{"delta": {"tool_calls": [{"index": 0, "id": "call-1", "function": {"name": "write_file", "arguments": %s}}]}}]}`,
					strconv.Quote(`{"path": "`+target+`",`)),
				fmt.Sprintf(`{"choices": [{"delta": {"tool_calls": [{"index": 0, "function": {"arguments": %s}}]}}]}`,
					strconv.Quote(`"content": "real bytes"}`)),
				`{"choices": [{"delta": {}, "finish_reason": "tool_calls"}]}`,
			}
			for _, c := range chunks {
				fmt.Fprintf(w, "data: %s\n\n", c)
			}
			return
		}
		// Round 2: the tool result came back; the model answers.
		fmt.Fprintf(w, "data: %s\n\n", `{"choices": [{"delta": {"content": "file written"}}]}`)
		fmt.Fprintf(w, "data: %s\n\n", `{"choices": [{"delta": {}, "finish_reason": "stop"}]}`)
	}))
	defer srv.Close()

	provider := llm.NewOpenAICompat(srv.URL, "test-key")
	orch, _ := newTestOrchestrator(provider, nil, dir)
	events := drain(t, orch.Send(context.Background(), "write notes.txt"))

	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("the loop never executed the tool for real: %v", err)
	}
	if string(data) != "real bytes" {
		t.Errorf("file content = %q", string(data))
	}
	if requestCount != 2 {
		t.Errorf("server saw %d requests, want 2", requestCount)
	}
	var text string
	for _, ev := range events {
		if ev.Kind == EventText {
			text += ev.Text
		}
	}
	if text != "file written" {
		t.Errorf("streamed text = %q", text)
	}
}

func TestSteerFoldsIntoNextRound(t *testing.T) {
	dir := t.TempDir()
	// A provider whose scripted round blocks until the test steers, so
	// the steering race is deterministic: round 1 is consumed, then the
	// test steers while round 2 is pending.
	steered := make(chan struct{})
	p := &steerableProvider{steered: steered, rounds: [][]llm.ChatEvent{
		{{Type: llm.ToolCallEvent, Call: llm.ToolCall{
			ID: "call-1", Name: "read_file", Arguments: `{"path": "x"}`}}},
		{{Type: llm.TextEvent, Text: "done"}},
	}}
	orch, _ := newTestOrchestrator(p, nil, dir)

	events := orch.Send(context.Background(), "go")

	// Wait for round 1 to be requested, then steer mid-round.
	p.waitForRequest(t)
	orch.Steer("actually, use path y instead")
	close(steered)

	for ev := range events {
		if ev.Kind == EventError {
			t.Fatalf("unexpected error: %v", ev.Err)
		}
	}

	// Round 2's request must carry the steering message, after the tool
	// result — folded in at the round boundary, not mid-round.
	if len(p.gotRequests) != 2 {
		t.Fatalf("got %d requests, want 2", len(p.gotRequests))
	}
	round2 := p.gotRequests[1].Messages
	if len(round2) != 4 {
		t.Fatalf("round 2 has %d messages, want user+assistant+tool+steer", len(round2))
	}
	last := round2[len(round2)-1]
	if last.Role != "user" || last.Content != "actually, use path y instead" {
		t.Errorf("steering message not folded in: %+v", last)
	}
	if round2[2].Role != "tool" {
		t.Errorf("steering must come after the tool result, got %+v", round2[2])
	}
}

func TestSteerBeforeAnyTurnIsPending(t *testing.T) {
	dir := t.TempDir()
	p := &steerableProvider{rounds: [][]llm.ChatEvent{
		{{Type: llm.TextEvent, Text: "ok"}},
	}}
	orch, _ := newTestOrchestrator(p, nil, dir)
	orch.Steer("early")
	events := drain(t, orch.Send(context.Background(), "go"))

	// The steer was queued with no turn running; it must still be
	// delivered to the next round rather than lost or dropped silently.
	if len(p.gotRequests) != 1 {
		t.Fatalf("got %d requests, want 1", len(p.gotRequests))
	}
	msgs := p.gotRequests[0].Messages
	if len(msgs) != 2 {
		t.Fatalf("got %d messages, want user + pending steer", len(msgs))
	}
	if msgs[len(msgs)-1].Content != "early" {
		t.Errorf("pending steer missing: %+v", msgs[len(msgs)-1])
	}
	var complete bool
	for _, ev := range events {
		if ev.Kind == EventTurnComplete {
			complete = true
		}
	}
	if !complete {
		t.Error("turn did not complete")
	}
}

func TestUsageEventsForwarded(t *testing.T) {
	dir := t.TempDir()
	p := &fakeProvider{
		rounds: [][]llm.ChatEvent{
			{{Type: llm.UsageEvent, Usage: llm.Usage{PromptTokens: 10, CompletionTokens: 5}},
				{Type: llm.TextEvent, Text: "hi"}},
		},
	}
	orch, _ := newTestOrchestrator(p, nil, dir)
	events := drain(t, orch.Send(context.Background(), "go"))

	var usage []llm.Usage
	for _, ev := range events {
		if ev.Kind == EventUsage {
			usage = append(usage, ev.Usage)
		}
	}
	if len(usage) != 1 || usage[0].PromptTokens != 10 || usage[0].CompletionTokens != 5 {
		t.Errorf("usage events = %+v", usage)
	}
}

func TestCancelledTurnReportsErrCancelled(t *testing.T) {
	dir := t.TempDir()
	p := &fakeProvider{
		rounds: [][]llm.ChatEvent{
			{{Type: llm.TextEvent, Text: "partial"}},
		},
	}
	orch, _ := newTestOrchestrator(p, nil, dir)
	ctx, cancel := context.WithCancel(context.Background())
	events := orch.Send(ctx, "go")
	cancel()

	var got error
	for ev := range events {
		if ev.Kind == EventError {
			got = ev.Err
		}
	}
	if !errors.Is(got, ErrCancelled) {
		t.Errorf("err = %v, want ErrCancelled", got)
	}
}

// steerableProvider is a fakeProvider that can block between rounds so
// tests can steer at a deterministic moment.
type steerableProvider struct {
	rounds      [][]llm.ChatEvent
	gotRequests []llm.ChatRequest
	steered     <-chan struct{}
}

func (f *steerableProvider) StreamChat(ctx context.Context, req llm.ChatRequest) (<-chan llm.ChatEvent, error) {
	f.gotRequests = append(f.gotRequests, req)
	if len(f.rounds) == 0 {
		return nil, fmt.Errorf("no scripted round left")
	}
	round := f.rounds[0]
	f.rounds = f.rounds[1:]
	if f.steered != nil {
		<-f.steered
	}
	ch := make(chan llm.ChatEvent, len(round))
	for _, ev := range round {
		ch <- ev
	}
	close(ch)
	return ch, nil
}

func (f *steerableProvider) waitForRequest(t *testing.T) {
	t.Helper()
	// Poll briefly: Send runs in a goroutine, so the request may not be
	// in gotRequests the instant Send returns.
	for i := 0; i < 100; i++ {
		if len(f.gotRequests) > 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("provider never received a request")
}

func TestReadOnlyModeHidesActionTools(t *testing.T) {
	dir := t.TempDir()
	p := &fakeProvider{rounds: [][]llm.ChatEvent{
		{{Type: llm.TextEvent, Text: "hi"}},
	}}
	orch, _ := newTestOrchestrator(p, nil, dir)
	orch.SetMode(tools.ModeReadOnly)
	drain(t, orch.Send(context.Background(), "go"))

	req := p.gotRequests[0]
	// Only read_file is offered; write/edit/shell are hidden entirely.
	if len(req.Tools) != 1 || req.Tools[0].Name != "read_file" {
		names := []string{}
		for _, tt := range req.Tools {
			names = append(names, tt.Name)
		}
		t.Errorf("read-only mode offered tools %v, want [read_file]", names)
	}
	// The system prompt carries the mode instruction.
	if !strings.Contains(req.System, "read-only") {
		t.Errorf("mode instruction missing from system prompt: %q", req.System)
	}
}

func TestOtherModesOfferAllToolsAndTheirPrompts(t *testing.T) {
	dir := t.TempDir()
	for _, mode := range []string{tools.ModeAskEveryTime, tools.ModeAutoAcceptSafe, tools.ModeFullAuto} {
		p := &fakeProvider{rounds: [][]llm.ChatEvent{
			{{Type: llm.TextEvent, Text: "hi"}},
		}}
		orch, _ := newTestOrchestrator(p, nil, dir)
		orch.SetMode(mode)
		drain(t, orch.Send(context.Background(), "go"))
		req := p.gotRequests[0]
		if len(req.Tools) != 4 {
			t.Errorf("mode %s offered %d tools, want all 4", mode, len(req.Tools))
		}
		if !strings.Contains(req.System, mode) {
			t.Errorf("mode %s instruction missing from prompt: %q", mode, req.System)
		}
	}
}

func TestSkillsIndexComposedIntoSystemPrompt(t *testing.T) {
	dir := t.TempDir()
	p := &fakeProvider{rounds: [][]llm.ChatEvent{
		{{Type: llm.TextEvent, Text: "hi"}},
	}}
	orch, _ := newTestOrchestrator(p, nil, dir)
	orch.SkillsIndex = "- deploy: Deploys the app"
	drain(t, orch.Send(context.Background(), "go"))

	sys := p.gotRequests[0].System
	for _, want := range []string{"test system prompt", "- deploy: Deploys the app", "ask-every-time"} {
		if !strings.Contains(sys, want) {
			t.Errorf("system prompt missing %q: %q", want, sys)
		}
	}

	// Updating the index mid-session reaches the next request.
	orch.SkillsIndex = "- audit: Audits things"
	drain(t, orch.Send(context.Background(), "again"))
	if !strings.Contains(p.gotRequests[1].System, "- audit: Audits things") {
		t.Errorf("updated skills index not in second request: %q", p.gotRequests[1].System)
	}
	if strings.Contains(p.gotRequests[1].System, "- deploy") {
		t.Error("stale skills index still present")
	}
}
