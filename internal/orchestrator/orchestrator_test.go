package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Chmgx81/opcode/internal/llm"
	"github.com/Chmgx81/opcode/internal/tools"
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
	reg.Register(tools.Bash{})
	reg.Register(tools.PresentPlan{Approve: func(string) (bool, bool) { return false, false }})
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
	if len(round2.Tools) != 5 {
		t.Errorf("round 2 carries %d tools, want 5", len(round2.Tools))
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

func TestLoopEmptyResponseIsAnErrorAndStaysOutOfHistory(t *testing.T) {
	p := &fakeProvider{rounds: [][]llm.ChatEvent{
		{{Type: llm.UsageEvent, Usage: llm.Usage{PromptTokens: 5}}},
		{{Type: llm.TextEvent, Text: "recovered"}},
	}}
	orch, _ := newTestOrchestrator(p, nil, t.TempDir())

	events := drain(t, orch.Send(context.Background(), "go"))
	last := events[len(events)-1]
	if last.Kind != EventError || !errors.Is(last.Err, ErrEmptyResponse) {
		t.Fatalf("last event = %+v, want EventError(ErrEmptyResponse)", last)
	}
	for _, m := range orch.History() {
		if m.Role == "assistant" {
			t.Fatalf("empty reply was recorded in history: %+v", orch.History())
		}
	}

	events = drain(t, orch.Send(context.Background(), "again"))
	if last := events[len(events)-1]; last.Kind != EventTurnComplete {
		t.Fatalf("session did not recover after an empty reply: %+v", last)
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
	// delivered to the next request rather than lost or dropped
	// silently. It lands BEFORE the new message: the user typed it
	// against the conversation that was on screen, and the new
	// question came after.
	if len(p.gotRequests) != 1 {
		t.Fatalf("got %d requests, want 1", len(p.gotRequests))
	}
	msgs := p.gotRequests[0].Messages
	if len(msgs) != 2 {
		t.Fatalf("got %d messages, want pending steer + user", len(msgs))
	}
	if msgs[0].Content != "early" {
		t.Errorf("pending steer missing: %+v", msgs[0])
	}
	if msgs[1].Content != "go" {
		t.Errorf("the new message must come after the steer: %+v", msgs[1])
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
	cancel()
	events := orch.Send(ctx, "go")

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
	// mu guards rounds and gotRequests: StreamChat runs on the turn
	// goroutine while waitForRequest polls from the test goroutine.
	mu          sync.Mutex
	rounds      [][]llm.ChatEvent
	gotRequests []llm.ChatRequest
	steered     <-chan struct{}
}

func (f *steerableProvider) StreamChat(ctx context.Context, req llm.ChatRequest) (<-chan llm.ChatEvent, error) {
	f.mu.Lock()
	f.gotRequests = append(f.gotRequests, req)
	if len(f.rounds) == 0 {
		f.mu.Unlock()
		return nil, fmt.Errorf("no scripted round left")
	}
	round := f.rounds[0]
	f.rounds = f.rounds[1:]
	f.mu.Unlock()
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
		f.mu.Lock()
		n := len(f.gotRequests)
		f.mu.Unlock()
		if n > 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("provider never received a request")
}

func TestPlanModeOffersAllTools(t *testing.T) {
	dir := t.TempDir()
	p := &fakeProvider{rounds: [][]llm.ChatEvent{
		{{Type: llm.TextEvent, Text: "hi"}},
	}}
	orch, _ := newTestOrchestrator(p, nil, dir)
	orch.SetMode(tools.ModePlan)
	drain(t, orch.Send(context.Background(), "go"))

	req := p.gotRequests[0]
	// Phase 30: offering is not permission — every mode offers every
	// tier and the gate is the single enforcement point, so a
	// read-only session can still propose a write.
	names := map[string]bool{}
	for _, tt := range req.Tools {
		names[tt.Name] = true
	}
	for _, want := range []string{"read_file", "write_file", "edit_file", "bash", "present_plan"} {
		if !names[want] {
			t.Errorf("plan mode must still offer %s, got %v", want, names)
		}
	}
	// The system prompt carries the mode instruction.
	if !strings.Contains(req.System, "mode is plan") {
		t.Errorf("mode instruction missing from system prompt: %q", req.System)
	}
}

func TestOtherModesOfferAllToolsAndTheirPrompts(t *testing.T) {
	dir := t.TempDir()
	for _, mode := range []string{tools.ModeAsk, tools.ModeFullAuto} {
		p := &fakeProvider{rounds: [][]llm.ChatEvent{
			{{Type: llm.TextEvent, Text: "hi"}},
		}}
		orch, _ := newTestOrchestrator(p, nil, dir)
		orch.SetMode(mode)
		drain(t, orch.Send(context.Background(), "go"))
		req := p.gotRequests[0]
		if len(req.Tools) != 5 {
			t.Errorf("mode %s offered %d tools, want all 5", mode, len(req.Tools))
		}
		if !strings.Contains(req.System, mode) {
			t.Errorf("mode %s instruction missing from prompt: %q", mode, req.System)
		}
	}

	// The removed auto-accept-safe-ops mode survives as a legacy alias
	// that must present the build posture, never anything wider.
	p := &fakeProvider{rounds: [][]llm.ChatEvent{
		{{Type: llm.TextEvent, Text: "hi"}},
	}}
	orch, _ := newTestOrchestrator(p, nil, dir)
	orch.SetMode(tools.ModeAutoAcceptSafe)
	drain(t, orch.Send(context.Background(), "go"))
	if sys := p.gotRequests[0].System; !strings.Contains(sys, tools.ModeBuild) {
		t.Errorf("legacy auto-accept-safe-ops must map to build: %q", sys)
	}
}

func TestSkillsIndexComposedIntoSystemPrompt(t *testing.T) {
	dir := t.TempDir()
	p := &fakeProvider{rounds: [][]llm.ChatEvent{
		{{Type: llm.TextEvent, Text: "hi"}},
	}}
	orch, _ := newTestOrchestrator(p, nil, dir)
	orch.SetSkillsIndex("- deploy: Deploys the app")
	drain(t, orch.Send(context.Background(), "go"))

	sys := p.gotRequests[0].System
	for _, want := range []string{"test system prompt", "- deploy: Deploys the app", "mode is build"} {
		if !strings.Contains(sys, want) {
			t.Errorf("system prompt missing %q: %q", want, sys)
		}
	}

	// Updating the index mid-session reaches the next request.
	orch.SetSkillsIndex("- audit: Audits things")
	drain(t, orch.Send(context.Background(), "again"))
	if !strings.Contains(p.gotRequests[1].System, "- audit: Audits things") {
		t.Errorf("updated skills index not in second request: %q", p.gotRequests[1].System)
	}
	if strings.Contains(p.gotRequests[1].System, "- deploy") {
		t.Error("stale skills index still present")
	}
}

func TestCompactionTriggersOnThreshold(t *testing.T) {
	dir := t.TempDir()
	// Round 1 answers with usage that crosses 75% of a 1000-token
	// window; the scripted provider's next request must then start
	// with the recap instead of the original history.
	// Two reports: the first (400) anchors the baseline, the second
	// (900) grows past 75% of the remaining space — the growth the
	// baseline accounting reacts to. The 800-of-1000 single report
	// this test used before was the exact pathology Phase 41 fixed:
	// a prefilled window compacting on arrival.
	p := &fakeProvider{rounds: [][]llm.ChatEvent{
		{{Type: llm.ToolCallEvent, Call: llm.ToolCall{
			ID: "c1", Name: "read_file", Arguments: `{"path": "missing-ok"}`}},
			{Type: llm.UsageEvent, Usage: llm.Usage{PromptTokens: 400, CompletionTokens: 1}}},
		{{Type: llm.ToolCallEvent, Call: llm.ToolCall{
			ID: "c2", Name: "read_file", Arguments: `{"path": "missing-ok"}`}},
			{Type: llm.UsageEvent, Usage: llm.Usage{PromptTokens: 900, CompletionTokens: 1}}},
		{{Type: llm.TextEvent, Text: "the summary"}},
		{{Type: llm.TextEvent, Text: "answer two"}},
	}}
	orch, _ := newTestOrchestrator(p, nil, dir)
	orch.ContextWindow = 1000

	// Seed enough history that compaction has something to summarize.
	orch.Seed([]llm.Message{
		{Role: "user", Content: "q1"}, {Role: "assistant", Content: "a1"},
		{Role: "user", Content: "q2"}, {Role: "assistant", Content: "a2"},
		{Role: "user", Content: "q3"}, {Role: "assistant", Content: "a3"},
		{Role: "user", Content: "q4"},
	})

	events := drain(t, orch.Send(context.Background(), "go"))
	var compacted bool
	for _, ev := range events {
		if ev.Kind == EventCompaction {
			compacted = true
		}
	}
	if !compacted {
		t.Fatalf("no compaction event; events = %+v", events)
	}
	// Request order: [0] round 1, [1] round 2, [2] the summarizer,
	// [3] the post-compaction round. The compaction is MID-TURN
	// (round 2's check after the tool result), so the recap lands
	// LAST — the most recent item, where attention lives.
	if len(p.gotRequests) < 4 {
		t.Fatalf("got %d requests, want r1 + r2 + summarizer + r3", len(p.gotRequests))
	}
	if !strings.Contains(p.gotRequests[2].System, "Summarize") {
		t.Errorf("request 2 should be the summarizer round: %q", p.gotRequests[2].System)
	}
	req := p.gotRequests[3]
	last := req.Messages[len(req.Messages)-1]
	if !strings.Contains(last.Content, "Context recap") {
		t.Errorf("mid-tcompaction recap is not the last message: %+v", last)
	}
	if len(req.Messages) != keepRecent+1 {
		t.Errorf("post-compaction history = %d messages, want %d + recap", len(req.Messages), keepRecent)
	}
}

func TestCompactionUsesConfiguredModel(t *testing.T) {
	dir := t.TempDir()
	p := &fakeProvider{rounds: [][]llm.ChatEvent{
		{{Type: llm.ToolCallEvent, Call: llm.ToolCall{
			ID: "c1", Name: "read_file", Arguments: `{"path": "missing-ok"}`}},
			{Type: llm.UsageEvent, Usage: llm.Usage{PromptTokens: 300}}},
		{{Type: llm.ToolCallEvent, Call: llm.ToolCall{
			ID: "c2", Name: "read_file", Arguments: `{"path": "missing-ok"}`}},
			{Type: llm.UsageEvent, Usage: llm.Usage{PromptTokens: 900}}},
		{{Type: llm.TextEvent, Text: "the summary"}},
		{{Type: llm.TextEvent, Text: "final"}},
	}}
	orch, _ := newTestOrchestrator(p, nil, dir)
	orch.ContextWindow = 1000
	orch.CompactionModel = "cheap/summarizer"
	orch.Seed([]llm.Message{
		{Role: "user", Content: "q1"}, {Role: "assistant", Content: "a1"},
		{Role: "user", Content: "q2"}, {Role: "assistant", Content: "a2"},
		{Role: "user", Content: "q3"}, {Role: "assistant", Content: "a3"},
		{Role: "user", Content: "go"},
	})
	drain(t, orch.Send(context.Background(), "go"))

	// The summarizer round is request index 2 (after two tool
	// rounds) and must use the cheap model with the summarizer
	// system prompt.
	sumReq := p.gotRequests[2]
	if sumReq.Model != "cheap/summarizer" {
		t.Errorf("summarizer model = %q, want cheap/summarizer", sumReq.Model)
	}
	if !strings.Contains(sumReq.System, "Summarize") {
		t.Errorf("summarizer prompt missing: %q", sumReq.System)
	}
	// The recap carries the summarizer's output, as the LAST
	// message (mid-turn injection).
	post := p.gotRequests[3].Messages
	if !strings.Contains(post[len(post)-1].Content, "the summary") {
		t.Errorf("recap does not contain the summary: %q", post[len(post)-1].Content)
	}
}

func TestCompactionDisabledByDefault(t *testing.T) {
	dir := t.TempDir()
	p := &fakeProvider{rounds: [][]llm.ChatEvent{
		{{Type: llm.ToolCallEvent, Call: llm.ToolCall{
			ID: "c1", Name: "read_file", Arguments: `{"path": "missing-ok"}`}},
			{Type: llm.UsageEvent, Usage: llm.Usage{PromptTokens: 99999}}},
		{{Type: llm.TextEvent, Text: "done"}},
	}}
	orch, _ := newTestOrchestrator(p, nil, dir)
	orch.Seed([]llm.Message{
		{Role: "user", Content: "q1"}, {Role: "assistant", Content: "a1"},
		{Role: "user", Content: "q2"}, {Role: "assistant", Content: "a2"},
		{Role: "user", Content: "go"},
	})
	events := drain(t, orch.Send(context.Background(), "go"))
	for _, ev := range events {
		if ev.Kind == EventCompaction {
			t.Error("compaction ran with ContextWindow 0 — it must be opt-in")
		}
	}
	// 5 seeded + the user message + assistant + tool result = 8,
	// untouched.
	if len(p.gotRequests[1].Messages) != 8 {
		t.Errorf("history was modified without compaction enabled: %d messages", len(p.gotRequests[1].Messages))
	}
}

func TestCompactionFailureIsNotFatal(t *testing.T) {
	dir := t.TempDir()
	// The summarizer round errors; the turn must continue.
	p := &fakeProvider{rounds: [][]llm.ChatEvent{
		{{Type: llm.ToolCallEvent, Call: llm.ToolCall{
			ID: "c1", Name: "read_file", Arguments: `{"path": "missing-ok"}`}},
			{Type: llm.UsageEvent, Usage: llm.Usage{PromptTokens: 300}}},
		{{Type: llm.ToolCallEvent, Call: llm.ToolCall{
			ID: "c2", Name: "read_file", Arguments: `{"path": "missing-ok"}`}},
			{Type: llm.UsageEvent, Usage: llm.Usage{PromptTokens: 900}}},
		{{Type: llm.ErrorEvent, Err: fmt.Errorf("summarizer down")}},
		{{Type: llm.TextEvent, Text: "carried on"}},
	}}
	orch, _ := newTestOrchestrator(p, nil, dir)
	orch.ContextWindow = 1000
	orch.Seed([]llm.Message{
		{Role: "user", Content: "q1"}, {Role: "assistant", Content: "a1"},
		{Role: "user", Content: "q2"}, {Role: "assistant", Content: "a2"},
		{Role: "user", Content: "q3"}, {Role: "assistant", Content: "a3"},
		{Role: "user", Content: "go"},
	})
	events := drain(t, orch.Send(context.Background(), "go"))

	var skipped, completed bool
	for _, ev := range events {
		if ev.Kind == EventCompactionFailed {
			skipped = true
		}
		if ev.Kind == EventError {
			t.Errorf("compaction failure surfaced as a terminal EventError: %v", ev.Err)
		}
		if ev.Kind == EventTurnComplete {
			completed = true
		}
	}
	if !skipped {
		t.Error("compaction failure not reported")
	}
	if !completed {
		t.Error("the turn died on a compaction failure — it must continue uncompacted")
	}
}

func TestPlanModeAdvertisesResearchAndPlanTools(t *testing.T) {
	dir := t.TempDir()
	p := &fakeProvider{rounds: [][]llm.ChatEvent{
		{{Type: llm.TextEvent, Text: "hi"}},
	}}
	orch, _ := newTestOrchestrator(p, nil, dir)
	orch.SetMode(tools.ModePlan)
	drain(t, orch.Send(context.Background(), "plan this"))
	req := p.gotRequests[0]

	names := map[string]bool{}
	for _, tl := range req.Tools {
		names[tl.Name] = true
	}
	if !names["read_file"] || !names["present_plan"] {
		t.Errorf("plan mode must offer read_file and present_plan, got %v", names)
	}
	// Phase 30: action tools are offered too — proposing is not
	// running; the gate prompts for each call.
	for _, offered := range []string{"write_file", "edit_file", "bash"} {
		if !names[offered] {
			t.Errorf("plan mode must still offer %s (the gate prompts), got %v", offered, names)
		}
	}
	if !strings.Contains(req.System, "mode is plan") || !strings.Contains(req.System, "present_plan") {
		t.Errorf("plan instruction missing from prompt: %q", req.System)
	}
}

// TestToolFailureKeepsOutput: a failing tool's own output — the
// shell's stderr, a sandbox-denial note — is evidence of what
// actually failed; the model's result keeps it beside the error
// headline instead of discarding it (found live: a sandboxed write
// denial reached the model as "exit status 1" with no cause).
func TestToolFailureKeepsOutput(t *testing.T) {
	p := &fakeProvider{rounds: [][]llm.ChatEvent{
		{{Type: llm.ToolCallEvent, Call: llm.ToolCall{
			ID: "c1", Name: "bash",
			Arguments: `{"command": "echo boom >&2; exit 3"}`}}},
		{{Type: llm.TextEvent, Text: "ok"}},
	}}
	var reg tools.Registry
	reg.Register(tools.Bash{})
	orch := New(p, "m", "s", &reg, &tools.Gate{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var got string
	for ev := range orch.Send(ctx, "run it") {
		if ev.Kind == EventToolResult {
			got = ev.ToolResult
		}
	}
	if !strings.Contains(got, "error: exited with status 3") {
		t.Errorf("result missing the error headline: %q", got)
	}
	if !strings.Contains(got, "boom") {
		t.Errorf("result dropped the tool's own output: %q", got)
	}
}

// TestPrefilledWindowDoesNotCompactOnArrival: the baseline
// accounting's whole point — a resumed session at 80% of the window
// must not compact before the user adds anything. The first report
// anchors the baseline; only growth past 75% of the remaining space
// triggers.
func TestPrefilledWindowDoesNotCompactOnArrival(t *testing.T) {
	dir := t.TempDir()
	// Round 1 reports 800 of a 1000-token window — yesterday this
	// compacted immediately. Round 2 reports 810: growth of 10
	// against a remaining 200 — far under the 150 trigger.
	p := &fakeProvider{rounds: [][]llm.ChatEvent{
		{{Type: llm.ToolCallEvent, Call: llm.ToolCall{
			ID: "c1", Name: "read_file", Arguments: `{"path": "missing-ok"}`}},
			{Type: llm.UsageEvent, Usage: llm.Usage{PromptTokens: 800}}},
		{{Type: llm.ToolCallEvent, Call: llm.ToolCall{
			ID: "c2", Name: "read_file", Arguments: `{"path": "missing-ok"}`}},
			{Type: llm.UsageEvent, Usage: llm.Usage{PromptTokens: 810}}},
		{{Type: llm.TextEvent, Text: "done"}},
	}}
	orch, _ := newTestOrchestrator(p, nil, dir)
	orch.ContextWindow = 1000
	orch.Seed([]llm.Message{
		{Role: "user", Content: "q1"}, {Role: "assistant", Content: "a1"},
		{Role: "user", Content: "q2"}, {Role: "assistant", Content: "a2"},
		{Role: "user", Content: "q3"}, {Role: "assistant", Content: "a3"},
		{Role: "user", Content: "go"},
	})
	for _, ev := range drain(t, orch.Send(context.Background(), "go")) {
		if ev.Kind == EventCompaction {
			t.Fatal("the prefilled window compacted on arrival")
		}
	}
	// The history must be untouched: no summarizer round ran.
	if len(p.gotRequests) != 3 {
		t.Errorf("got %d requests, want 3 plain rounds", len(p.gotRequests))
	}
	if !strings.Contains(p.gotRequests[0].Messages[0].Content, "q1") {
		t.Error("history was rewritten without a compaction")
	}
}

// TestGrowthPastRemainingSpaceTriggers: the same 800 baseline, but
// growth to 960 — past 75% of the remaining 200 — compacts, and the
// baseline resets so the compacted size anchors fresh growth.
func TestGrowthPastRemainingSpaceTriggers(t *testing.T) {
	dir := t.TempDir()
	p := &fakeProvider{rounds: [][]llm.ChatEvent{
		{{Type: llm.ToolCallEvent, Call: llm.ToolCall{
			ID: "c1", Name: "read_file", Arguments: `{"path": "missing-ok"}`}},
			{Type: llm.UsageEvent, Usage: llm.Usage{PromptTokens: 800}}},
		{{Type: llm.ToolCallEvent, Call: llm.ToolCall{
			ID: "c2", Name: "read_file", Arguments: `{"path": "missing-ok"}`}},
			{Type: llm.UsageEvent, Usage: llm.Usage{PromptTokens: 960}}},
		{{Type: llm.TextEvent, Text: "the summary"}},
		{{Type: llm.TextEvent, Text: "done"}},
	}}
	orch, _ := newTestOrchestrator(p, nil, dir)
	orch.ContextWindow = 1000
	orch.Seed([]llm.Message{
		{Role: "user", Content: "q1"}, {Role: "assistant", Content: "a1"},
		{Role: "user", Content: "q2"}, {Role: "assistant", Content: "a2"},
		{Role: "user", Content: "q3"}, {Role: "assistant", Content: "a3"},
		{Role: "user", Content: "go"},
	})
	compacted := false
	for _, ev := range drain(t, orch.Send(context.Background(), "go")) {
		if ev.Kind == EventCompaction {
			compacted = true
		}
	}
	if !compacted {
		t.Fatal("growth past the remaining space did not compact")
	}
	// Mid-turn injection: the recap is the LAST message.
	post := p.gotRequests[3].Messages
	if !strings.Contains(post[len(post)-1].Content, "Context recap") {
		t.Errorf("mid-turn recap not last: %+v", post[len(post)-1])
	}
	// The baseline re-anchored: the next report measures fresh
	// growth instead of instantly re-triggering.
	if orch.baselineSet {
		t.Error("baseline not reset after compaction")
	}
}

// TestPreTurnCompactionKeepsRecapFirst: when the trigger lands on a
// turn's ROUND 0 — the previous turn's report crossed the line —
// the recap opens the history and the new user message stays near
// the end, where models are trained to find it.
func TestPreTurnCompactionKeepsRecapFirst(t *testing.T) {
	dir := t.TempDir()
	// Turn 1: growth to 960 compacts nothing yet (it reports at the
	// END of the turn). Turn 2's round 0 check sees 960 — pre-turn
	// position — and compacts with the recap FIRST.
	// Turn 1: round 1 anchors the baseline (500), round 2 stays under
	// the trigger (600), and the FINAL round reports 960 — the turn
	// ends before another compaction check can see it, so the line is
	// crossed pre-turn instead.
	p := &fakeProvider{rounds: [][]llm.ChatEvent{
		{{Type: llm.ToolCallEvent, Call: llm.ToolCall{
			ID: "c1", Name: "read_file", Arguments: `{"path": "missing-ok"}`}},
			{Type: llm.UsageEvent, Usage: llm.Usage{PromptTokens: 500}}},
		{{Type: llm.ToolCallEvent, Call: llm.ToolCall{
			ID: "c2", Name: "read_file", Arguments: `{"path": "missing-ok"}`}},
			{Type: llm.UsageEvent, Usage: llm.Usage{PromptTokens: 600}}},
		{{Type: llm.TextEvent, Text: "turn one done"},
			{Type: llm.UsageEvent, Usage: llm.Usage{PromptTokens: 960}}},
		{{Type: llm.TextEvent, Text: "the summary"}},
		{{Type: llm.TextEvent, Text: "turn two done"}},
	}}
	orch, _ := newTestOrchestrator(p, nil, dir)
	orch.ContextWindow = 1000
	orch.Seed([]llm.Message{
		{Role: "user", Content: "q1"}, {Role: "assistant", Content: "a1"},
		{Role: "user", Content: "q2"}, {Role: "assistant", Content: "a2"},
		{Role: "user", Content: "q3"}, {Role: "assistant", Content: "a3"},
	})
	drain(t, orch.Send(context.Background(), "first"))
	// The second turn starts with the compacted history: recap
	// first, the newest user message at the end.
	var post []llm.Message
	var compacted bool
	for _, ev := range drain(t, orch.Send(context.Background(), "second")) {
		if ev.Kind == EventCompaction {
			compacted = true
		}
	}
	// The last request of turn 2 carries the post-compaction shape.
	post = p.gotRequests[len(p.gotRequests)-1].Messages
	if !compacted {
		t.Fatal("pre-turn compaction did not run")
	}
	if !strings.Contains(post[0].Content, "Context recap") {
		t.Errorf("pre-turn recap not first: %+v", post[0])
	}
	if post[len(post)-1].Content != "second" {
		t.Errorf("the turn's user message is not last: %+v", post[len(post)-1])
	}
}

// funcProvider adapts a function to llm.Provider for tests that need
// per-request behavior.
type funcProvider func(ctx context.Context, req llm.ChatRequest) (<-chan llm.ChatEvent, error)

func (f funcProvider) StreamChat(ctx context.Context, req llm.ChatRequest) (<-chan llm.ChatEvent, error) {
	return f(ctx, req)
}

func eventsChan(evs ...llm.ChatEvent) <-chan llm.ChatEvent {
	ch := make(chan llm.ChatEvent, len(evs))
	for _, ev := range evs {
		ch <- ev
	}
	close(ch)
	return ch
}

// TestConcurrentSendHistorySteerSeedIsRaceFree hammers every entry
// point that touches conversation state at once. It asserts nothing
// about the final history — only that the race detector stays quiet,
// nothing deadlocks, and the turns drain. Overlapping Sends do not
// happen in the TUI, but History (exit-path session save after a
// WaitIdle timeout) and Seed (resume) can legitimately land mid-turn.
func TestConcurrentSendHistorySteerSeedIsRaceFree(t *testing.T) {
	p := funcProvider(func(ctx context.Context, req llm.ChatRequest) (<-chan llm.ChatEvent, error) {
		if strings.HasPrefix(req.System, "Summarize") {
			return eventsChan(llm.ChatEvent{Type: llm.TextEvent, Text: "recap"}), nil
		}
		usage := llm.ChatEvent{Type: llm.UsageEvent, Usage: llm.Usage{PromptTokens: 50 * len(req.Messages)}}
		if n := len(req.Messages); n > 0 && req.Messages[n-1].Role == "tool" {
			return eventsChan(usage, llm.ChatEvent{Type: llm.TextEvent, Text: "ok"}), nil
		}
		return eventsChan(usage, llm.ChatEvent{Type: llm.ToolCallEvent, Call: llm.ToolCall{
			ID: "c", Name: "read_file", Arguments: `{"path": "missing-ok"}`}}), nil
	})
	orch, _ := newTestOrchestrator(p, nil, t.TempDir())
	orch.ContextWindow = 1000
	seed := []llm.Message{
		{Role: "user", Content: "q1"}, {Role: "assistant", Content: "a1"},
		{Role: "user", Content: "q2"}, {Role: "assistant", Content: "a2"},
		{Role: "user", Content: "q3"}, {Role: "assistant", Content: "a3"},
		{Role: "user", Content: "q4"}, {Role: "assistant", Content: "a4"},
	}
	orch.Seed(seed)

	stop := make(chan struct{})
	var helpers sync.WaitGroup
	helper := func(f func()) {
		helpers.Add(1)
		go func() {
			defer helpers.Done()
			for {
				select {
				case <-stop:
					return
				default:
					f()
				}
			}
		}()
	}
	helper(func() { _ = orch.History() })
	helper(func() { orch.Steer("steer"); time.Sleep(time.Millisecond) })
	helper(func() { orch.Seed(seed); time.Sleep(time.Millisecond) })

	var senders sync.WaitGroup
	for i := 0; i < 4; i++ {
		senders.Add(1)
		go func() {
			defer senders.Done()
			for j := 0; j < 15; j++ {
				for range orch.Send(context.Background(), "go") {
				}
			}
		}()
	}
	senders.Wait()
	close(stop)
	helpers.Wait()
	if !orch.WaitIdle(5 * time.Second) {
		t.Fatal("turn goroutines still running after all Sends drained")
	}
}

// cancelingTool cancels the turn's context mid-dispatch, the way an
// interrupt landing during a tool call does. output simulates a tool
// that partially applied before the interrupt landed.
type cancelingTool struct {
	cancel context.CancelFunc
	name   string // empty = "cancel_now"
	output string
}

func (c cancelingTool) Name() string {
	if c.name != "" {
		return c.name
	}
	return "cancel_now"
}
func (cancelingTool) Description() string { return "test tool" }
func (cancelingTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object"}`)
}
func (cancelingTool) Tier() tools.Tier { return tools.TierReadOnly }
func (c cancelingTool) Execute(ctx context.Context, args string) (string, error) {
	c.cancel()
	return c.output, ctx.Err()
}

func TestInterruptedToolCallsStillGetResults(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	p := &fakeProvider{rounds: [][]llm.ChatEvent{
		{{Type: llm.ToolCallEvent, Call: llm.ToolCall{ID: "c1", Name: "cancel_now", Arguments: `{}`}},
			{Type: llm.ToolCallEvent, Call: llm.ToolCall{ID: "c2", Name: "read_file", Arguments: `{"path": "x"}`}}},
	}}
	orch, reg := newTestOrchestrator(p, nil, t.TempDir())
	reg.Register(cancelingTool{cancel: cancel})

	var got error
	for ev := range orch.Send(ctx, "go") {
		if ev.Kind == EventError {
			got = ev.Err
		}
	}
	if !errors.Is(got, ErrCancelled) {
		t.Fatalf("err = %v, want ErrCancelled", got)
	}
	// Providers reject an assistant tool_calls message whose calls are
	// not each followed by a tool result; the next turn would 400.
	h := orch.History()
	if len(h) != 4 || h[2].Role != "tool" || h[2].ToolCallID != "c1" || h[3].Role != "tool" || h[3].ToolCallID != "c2" {
		t.Fatalf("history after interrupt = %+v, want user, assistant, tool c1, tool c2", h)
	}
}

func TestCancelDuringSummaryIsAnInterruptNotACompactionFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var calls int
	p := funcProvider(func(c context.Context, req llm.ChatRequest) (<-chan llm.ChatEvent, error) {
		calls++
		switch calls {
		case 1:
			return eventsChan(
				llm.ChatEvent{Type: llm.ToolCallEvent, Call: llm.ToolCall{ID: "c1", Name: "read_file", Arguments: `{"path": "missing-ok"}`}},
				llm.ChatEvent{Type: llm.UsageEvent, Usage: llm.Usage{PromptTokens: 300}}), nil
		case 2:
			return eventsChan(
				llm.ChatEvent{Type: llm.ToolCallEvent, Call: llm.ToolCall{ID: "c2", Name: "read_file", Arguments: `{"path": "missing-ok"}`}},
				llm.ChatEvent{Type: llm.UsageEvent, Usage: llm.Usage{PromptTokens: 900}}), nil
		default:
			cancel() // the interrupt lands while the summarizer runs
			return nil, c.Err()
		}
	})
	orch, _ := newTestOrchestrator(p, nil, t.TempDir())
	orch.ContextWindow = 1000
	orch.Seed([]llm.Message{
		{Role: "user", Content: "q1"}, {Role: "assistant", Content: "a1"},
		{Role: "user", Content: "q2"}, {Role: "assistant", Content: "a2"},
		{Role: "user", Content: "q3"}, {Role: "assistant", Content: "a3"},
	})

	var got error
	for ev := range orch.Send(ctx, "go") {
		if ev.Kind == EventCompactionFailed {
			t.Errorf("interrupt reported as a compaction failure: %v", ev.Err)
		}
		if ev.Kind == EventError {
			got = ev.Err
		}
	}
	if !errors.Is(got, ErrCancelled) {
		t.Errorf("err = %v, want ErrCancelled", got)
	}
}

func TestSeedResetsCompactionSignal(t *testing.T) {
	orch, _ := newTestOrchestrator(&fakeProvider{}, nil, t.TempDir())
	orch.ContextWindow = 1000
	orch.lastPromptTokens, orch.baselineTokens, orch.baselineSet = 900, 300, true
	orch.Seed([]llm.Message{{Role: "user", Content: "fresh"}})
	if orch.lastPromptTokens != 0 || orch.baselineTokens != 0 || orch.baselineSet {
		t.Errorf("stale token signal survived Seed: last=%d baseline=%d set=%v",
			orch.lastPromptTokens, orch.baselineTokens, orch.baselineSet)
	}
}

func TestStaleCompactionIsDiscardedAfterSeed(t *testing.T) {
	orch, _ := newTestOrchestrator(&fakeProvider{}, nil, t.TempDir())
	orch.ContextWindow = 1000
	orch.Seed([]llm.Message{
		{Role: "user", Content: "q1"}, {Role: "assistant", Content: "a1"},
		{Role: "user", Content: "q2"}, {Role: "assistant", Content: "a2"},
		{Role: "user", Content: "q3"}, {Role: "assistant", Content: "a3"},
		{Role: "user", Content: "q4"},
	})
	orch.setPromptTokens(300)
	orch.compactionCandidate() // anchors the baseline
	orch.setPromptTokens(900)
	old, gen := orch.compactionCandidate()
	if old == nil {
		t.Fatal("expected compaction to be due")
	}

	// A resume lands while the summarizer round is in flight.
	orch.Seed([]llm.Message{{Role: "user", Content: "resumed"}})
	if orch.applyCompaction(gen, len(old), llm.Message{Role: "user", Content: "recap"}, InjectRecapFirst) {
		t.Fatal("stale compaction was applied over the resumed history")
	}
	if h := orch.History(); len(h) != 1 || h[0].Content != "resumed" {
		t.Errorf("history = %+v, want the resumed one untouched", h)
	}
}

// toolHeavyHistory is a long, tool-heavy conversation: a user message,
// an assistant turn that calls one tool, and that tool's result. Its
// length decides where the fixed-count cut lands, so a range of
// lengths sweeps every alignment against a tool round.
func toolHeavyHistory(n int) []llm.Message {
	var out []llm.Message
	for i := 0; len(out) < n; i++ {
		id := fmt.Sprintf("c%d", i)
		out = append(out,
			llm.Message{Role: "user", Content: fmt.Sprintf("q%d", i)},
			llm.Message{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: id, Name: "read_file", Arguments: `{}`}}},
			llm.Message{Role: "tool", ToolCallID: id, Content: "contents"},
		)
	}
	return out[:n]
}

// allToolResults builds a history that is nothing but tool results.
// Nothing can be done for it — the messages cannot be answered by
// anything in the list — so the only correct outcome is no compaction
// at all.
func allToolResults(n int) []llm.Message {
	out := make([]llm.Message, n)
	for i := range out {
		out[i] = llm.Message{Role: "tool", ToolCallID: fmt.Sprintf("c%d", i), Content: "contents"}
	}
	return out
}

// allUserMessages and allPlainAnswers cover the shapes that cannot break
// the cut, and are here so a future change to compactionCut is caught
// when it starts mangling them.
func allUserMessages(n int) []llm.Message {
	out := make([]llm.Message, n)
	for i := range out {
		out[i] = llm.Message{Role: "user", Content: fmt.Sprintf("q%d", i)}
	}
	return out
}

func allPlainAnswers(n int) []llm.Message {
	out := make([]llm.Message, n)
	for i := range out {
		out[i] = llm.Message{Role: "assistant", Content: fmt.Sprintf("a%d", i)}
	}
	return out
}

// multiCallHistory: rounds that call three tools at once, and assistant
// turns that speak before calling — both make the tool runs longer than
// one message, so a count-based cut has more ways to land mid-round.
func multiCallHistory(rounds int) []llm.Message {
	var out []llm.Message
	for i := 0; i < rounds; i++ {
		prefix := fmt.Sprintf("c%d-", i)
		calls := make([]llm.ToolCall, 3)
		for j := range calls {
			calls[j] = llm.ToolCall{ID: fmt.Sprintf("%s%d", prefix, j), Name: "read_file", Arguments: `{}`}
		}
		out = append(out, llm.Message{Role: "user", Content: fmt.Sprintf("q%d", i)},
			llm.Message{Role: "assistant", Content: "looking", ToolCalls: calls})
		for _, c := range calls {
			out = append(out, llm.Message{Role: "tool", ToolCallID: c.ID, Content: "contents"})
		}
	}
	return out
}

// TestCompactionLeavesAListTheProviderAccepts: the cut that keeps the
// newest keepRecent messages is a COUNT, so in a tool-heavy session it
// regularly lands in the middle of a tool round. The kept tail then
// opens with a tool result whose assistant tool_calls were summarized
// away, and every provider answers that with a hard 400. The cut has
// to move back to a boundary the wire accepts.
func TestCompactionLeavesAListTheProviderAccepts(t *testing.T) {
	type compactionCase struct {
		name    string
		history []llm.Message
		// unsummarizable marks a history that is malformed before
		// compaction ever sees it — nothing here can answer a tool
		// result, so the only correct outcome is no compaction.
		unsummarizable bool
	}
	cases := []compactionCase{
		{name: "empty", history: nil},
		{name: "one message", history: allUserMessages(1)},
		{name: "shorter than keepRecent", history: allUserMessages(3)},
		{name: "exactly keepRecent", history: allUserMessages(4)},
		{name: "just over the floor", history: allUserMessages(7)},
		{name: "all user", history: allUserMessages(20)},
		{name: "all plain answers", history: allPlainAnswers(20)},
		{name: "all tool results", history: allToolResults(20), unsummarizable: true},
		{name: "multi-call rounds", history: multiCallHistory(6)},
	}
	// Every alignment of the count-based cut against a tool round.
	for n := 6; n <= 20; n++ {
		cases = append(cases, compactionCase{
			name:    fmt.Sprintf("tool heavy, %d messages", n),
			history: toolHeavyHistory(n),
		})
	}
	// The exact shape the bug was found with: the recap stands in for
	// the first user message and its tool call, and the tail starts
	// with a tool result.
	cases = append(cases, compactionCase{
		name: "recap then a tool round the count cuts into",
		history: append(
			[]llm.Message{{Role: "user", Content: "Context recap: earlier work"}},
			toolHeavyHistory(20)...),
	})

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			var seen []llm.ChatRequest
			p := funcProvider(func(ctx context.Context, req llm.ChatRequest) (<-chan llm.ChatEvent, error) {
				mu.Lock()
				seen = append(seen, req)
				mu.Unlock()
				return eventsChan(llm.ChatEvent{Type: llm.TextEvent, Text: "recap"}), nil
			})
			orch, _ := newTestOrchestrator(p, nil, t.TempDir())
			orch.ContextWindow = 1000
			orch.Seed(tc.history)
			orch.setPromptTokens(300)
			orch.compactionCandidate() // anchors the baseline
			orch.setPromptTokens(900)  // growth past the trigger

			if _, err := orch.maybeCompact(context.Background(), InjectRecapFirst); err != nil {
				t.Fatalf("maybeCompact: %v", err)
			}

			// The summarizer round is a request too, carrying the
			// summarized prefix on its own: it must be a list a
			// provider accepts as well.
			for i, req := range seen {
				if !strings.HasPrefix(req.System, "Summarize") {
					continue
				}
				if err := Validate(req.Messages); err != nil {
					t.Errorf("summary request %d is malformed: %v", i, err)
				}
			}
			if tc.unsummarizable {
				// Compaction must decline rather than send a prefix
				// whose tool results answer nothing.
				if len(seen) != 0 {
					t.Errorf("summarizer ran on a history it cannot summarize")
				}
				if len(orch.History()) != len(tc.history) {
					t.Errorf("history was rewritten: %d messages, want the %d seeded",
						len(orch.History()), len(tc.history))
				}
				return
			}
			assertTailIsAnswerable(t, orch.History())
		})
	}
}

// assertTailIsAnswerable checks the invariants a compaction cut can
// break, written out rather than delegated to Validate: a test that
// calls the function under test proves nothing about it.
func assertTailIsAnswerable(t *testing.T, msgs []llm.Message) {
	t.Helper()
	if len(msgs) > 0 && msgs[0].Role == "tool" {
		t.Errorf("compacted history opens with a tool result: %+v", msgs[0])
	}
	asked := map[string]bool{}
	for i, m := range msgs {
		switch m.Role {
		case "assistant":
			asked = map[string]bool{}
			for _, tc := range m.ToolCalls {
				asked[tc.ID] = true
			}
		case "tool":
			if !asked[m.ToolCallID] {
				t.Errorf("message %d: tool result %q answers no surviving assistant tool call", i, m.ToolCallID)
			}
		}
	}
}

// TestCompactionStillSummarizesAKeptTail: the boundary must not cost
// the compaction its purpose. On a history whose tail is already at a
// safe boundary, exactly keepRecent messages survive verbatim.
func TestCompactionStillSummarizesAKeptTail(t *testing.T) {
	orch, _ := newTestOrchestrator(&fakeProvider{}, nil, t.TempDir())
	orch.ContextWindow = 1000
	orch.Seed([]llm.Message{
		{Role: "user", Content: "q1"}, {Role: "assistant", Content: "a1"},
		{Role: "user", Content: "q2"}, {Role: "assistant", Content: "a2"},
		{Role: "user", Content: "q3"}, {Role: "assistant", Content: "a3"},
		{Role: "user", Content: "q4"},
	})
	orch.setPromptTokens(300)
	orch.compactionCandidate()
	orch.setPromptTokens(900)
	old, gen := orch.compactionCandidate()
	if old == nil {
		t.Fatal("expected compaction to be due")
	}
	if len(old) != 7-keepRecent {
		t.Errorf("summarized %d messages, want %d", len(old), 7-keepRecent)
	}
	if !orch.applyCompaction(gen, len(old), llm.Message{Role: "user", Content: "recap"}, InjectRecapFirst) {
		t.Fatal("applyCompaction refused a live generation")
	}
	if got := len(orch.History()); got != keepRecent+1 {
		t.Errorf("compacted history = %d messages, want %d kept + recap", got, keepRecent)
	}
}

// TestCompactionCutStopsAtAProtocolBoundary: the cut itself, over the
// shapes that can break it. A cut of 0 means there is no boundary at
// all in the history, and compactionCandidate refuses to compact then.
func TestCompactionCutStopsAtAProtocolBoundary(t *testing.T) {
	tests := []struct {
		name    string
		history []llm.Message
		want    int
	}{
		{"empty", nil, -keepRecent},
		{"shorter than the kept tail", allUserMessages(3), -1},
		{"tail already at a boundary", allUserMessages(9), 5},
		// A tool round is user, assistant, tool: walking back from
		// the tool result stops on the assistant that owns it, which
		// is a boundary the wire accepts.
		{"tail would open on a tool result", toolHeavyHistory(12), 7},
		// A three-call round: the walk stops on its assistant.
		{"tail would open on a tool result of a long round", multiCallHistory(6), 26},
		{"nothing survives the walk back", allToolResults(20), 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := compactionCut(tc.history)
			if got != tc.want {
				t.Fatalf("cut = %d, want %d", got, tc.want)
			}
			if got <= 0 {
				return // nothing to keep; the candidate gate refuses
			}
			tail := tc.history[got:]
			if tail[0].Role == "tool" {
				t.Errorf("tail opens on a tool result: %+v", tail[0])
			}
			if prev := tc.history[got-1]; prev.Role == "assistant" && len(prev.ToolCalls) > 0 {
				t.Errorf("tail starts right after %+v", prev)
			}
		})
	}
}

// TestValidateRejectsWhatProvidersReject: the invariants themselves.
// Each case is a list that reached history through a different bug.
func TestValidateRejectsWhatProvidersReject(t *testing.T) {
	// ok means the list must pass. The three passing cases are there
	// to keep the check from becoming a blunt "reject anything odd":
	// a list the providers accept is not an error.
	tests := []struct {
		name string
		msgs []llm.Message
		ok   bool
	}{
		{"empty is fine", nil, true},
		{"a plain exchange is fine", []llm.Message{
			{Role: "user", Content: "hi"}, {Role: "assistant", Content: "hello"}}, true},
		{"images make an empty user message valid", []llm.Message{
			{Role: "user", Images: []llm.Image{{MimeType: "image/png", Data: []byte("x")}}},
			{Role: "assistant", Content: "a screenshot"}}, true},
		{"a tool result with nothing before it", []llm.Message{
			{Role: "tool", ToolCallID: "c1", Content: "result"}}, false},
		{"a tool result whose call was summarized away", []llm.Message{
			{Role: "user", Content: "recap"},
			{Role: "tool", ToolCallID: "c1", Content: "result"},
			{Role: "assistant", Content: "ok"}}, false},
		{"a tool result with an empty id", []llm.Message{
			{Role: "user", Content: "hi"},
			{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "", Name: "read_file"}}},
			{Role: "tool", ToolCallID: "", Content: "result"}}, false},
		{"a tool result answering a later round's call", []llm.Message{
			{Role: "user", Content: "hi"},
			{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "c1", Name: "read_file"}}},
			{Role: "tool", ToolCallID: "c2", Content: "result"}}, false},
		{"an empty user message", []llm.Message{
			{Role: "user"}, {Role: "assistant", Content: "hello"}}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := Validate(tc.msgs)
			if tc.ok && err != nil {
				t.Errorf("Validate = %v, want nil", err)
			}
			if !tc.ok && err == nil {
				t.Errorf("Validate accepted a list every provider rejects: %+v", tc.msgs)
			}
		})
	}
}

// TestMalformedToolCallIsReportedAndNeverRecorded: a call the provider
// emitted without an id has no tool result it can be attached to, and
// one without a name has nothing to run. Either used to enter history
// as an unanswered tool_calls entry, which the next request rejects.
// The model must still be told, as a normal message.
func TestMalformedToolCallIsReportedAndNeverRecorded(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "readable.txt")
	if err := os.WriteFile(target, []byte("the file contents"), 0o600); err != nil {
		t.Fatal(err)
	}
	p := &fakeProvider{rounds: [][]llm.ChatEvent{
		{{Type: llm.ToolCallEvent, Call: llm.ToolCall{ID: "", Name: "read_file", Arguments: fmt.Sprintf(`{"path": %q}`, target)}},
			{Type: llm.ToolCallEvent, Call: llm.ToolCall{ID: "c2", Name: "", Arguments: `{}`}},
			{Type: llm.ToolCallEvent, Call: llm.ToolCall{ID: "c3", Name: "read_file", Arguments: fmt.Sprintf(`{"path": %q}`, target)}}},
		{{Type: llm.TextEvent, Text: "ok"}},
	}}
	orch, _ := newTestOrchestrator(p, nil, dir)
	drain(t, orch.Send(context.Background(), "go"))

	h := orch.History()
	if err := Validate(h); err != nil {
		t.Fatalf("history a provider would reject: %v\n%+v", err, h)
	}
	if err := Validate(p.gotRequests[1].Messages); err != nil {
		t.Fatalf("the model was sent a list it rejects: %v", err)
	}
	// The two malformed calls are reported; neither is recorded as a
	// call, so nothing is left waiting for a result that cannot exist.
	asst := h[1]
	if len(asst.ToolCalls) != 1 || asst.ToolCalls[0].ID != "c3" {
		t.Fatalf("assistant message = %+v, want only the well-formed call", asst)
	}
	if h[2].Role != "tool" || h[2].ToolCallID != "c3" {
		t.Fatalf("message 2 = %+v, want the result for c3", h[2])
	}
	// The well-formed call in the same round still ran.
	if !strings.Contains(h[2].Content, "the file contents") {
		t.Errorf("the valid call was not executed: %q", h[2].Content)
	}
	// The notes land after the round's results: a user message in
	// between would push a tool_result out of the turn it answers.
	for _, m := range h[3:5] {
		if m.Role != "user" || !strings.Contains(m.Content, "was not run") {
			t.Errorf("malformed call not reported to the model: %+v", m)
		}
	}
}

// TestRoundCapsToolCallsAndAnswersTheRest: MaxToolRounds bounds rounds,
// not calls, so one round could append thousands of tool results to a
// single history. The excess is answered with an error rather than
// dropped — a dropped call would leave its tool_calls unanswered and
// the next request would 400.
func TestRoundCapsToolCallsAndAnswersTheRest(t *testing.T) {
	counter := &countingTool{}
	round := make([]llm.ChatEvent, 0, 100)
	for i := 0; i < 100; i++ {
		round = append(round, llm.ChatEvent{Type: llm.ToolCallEvent, Call: llm.ToolCall{
			ID: fmt.Sprintf("c%d", i), Name: "count", Arguments: `{}`}})
	}
	p := &fakeProvider{rounds: [][]llm.ChatEvent{
		round,
		{{Type: llm.TextEvent, Text: "ok"}},
	}}
	orch, reg := newTestOrchestrator(p, nil, t.TempDir())
	reg.Register(counter)
	drain(t, orch.Send(context.Background(), "go"))

	if got := counter.n.Load(); got != maxCallsPerRound {
		t.Errorf("executed %d calls, want the cap of %d", got, maxCallsPerRound)
	}
	h := orch.History()
	if err := Validate(h); err != nil {
		t.Fatalf("history a provider would reject: %v", err)
	}
	if len(h) != 2+100+1 {
		t.Fatalf("history = %d messages, want every one of the 100 calls answered", len(h))
	}
	if len(h[1].ToolCalls) != 100 {
		t.Errorf("assistant message carries %d calls, want all 100 recorded", len(h[1].ToolCalls))
	}
	for i, m := range h[2:102] {
		if m.Role != "tool" || m.ToolCallID != fmt.Sprintf("c%d", i) {
			t.Fatalf("message %d = %+v, want the result for c%d", i+2, m, i)
		}
		if over := i >= maxCallsPerRound; over && !strings.Contains(m.Content, "too many tool calls") {
			t.Errorf("message %d was not told why it did not run: %q", i+2, m.Content)
		} else if !over && strings.Contains(m.Content, "too many tool calls") {
			t.Errorf("message %d was refused, but it is inside the cap: %q", i+2, m.Content)
		}
	}
}

// countingTool records how often it ran, so the per-round cap can be
// counted rather than inferred.
type countingTool struct{ n atomic.Int64 }

func (*countingTool) Name() string        { return "count" }
func (*countingTool) Description() string { return "counts its executions" }
func (*countingTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object"}`)
}
func (*countingTool) Tier() tools.Tier { return tools.TierReadOnly }
func (c *countingTool) Execute(ctx context.Context, args string) (string, error) {
	c.n.Add(1)
	return "counted", nil
}

// TestSteerLandsWhereTheUserTypedIt covers both timings of a steer.
// Mid-round it folds in at the round boundary. Typed after the turn has
// already ended there is no boundary left: the text used to sit in the
// queue until the NEXT user message, which put it after the new
// question in the model's context while the transcript showed it under
// the turn the user was actually looking at.
func TestSteerLandsWhereTheUserTypedIt(t *testing.T) {
	t.Run("mid-round, at the boundary", func(t *testing.T) {
		steered := make(chan struct{})
		p := &steerableProvider{steered: steered, rounds: [][]llm.ChatEvent{
			{{Type: llm.ToolCallEvent, Call: llm.ToolCall{
				ID: "call-1", Name: "read_file", Arguments: `{"path": "x"}`}}},
			{{Type: llm.TextEvent, Text: "done"}},
		}}
		orch, _ := newTestOrchestrator(p, nil, t.TempDir())
		events := orch.Send(context.Background(), "go")
		p.waitForRequest(t)
		orch.Steer("use path y instead")
		close(steered)
		for ev := range events {
			if ev.Kind == EventError {
				t.Fatalf("unexpected error: %v", ev.Err)
			}
		}
		last := p.gotRequests[1].Messages
		if last[len(last)-1].Content != "use path y instead" {
			t.Errorf("steer not folded in after the tool result: %+v", last)
		}
	})

	t.Run("after the turn ended, before the next question", func(t *testing.T) {
		p := &fakeProvider{rounds: [][]llm.ChatEvent{
			{{Type: llm.TextEvent, Text: "turn one done"}},
			{{Type: llm.TextEvent, Text: "turn two done"}},
		}}
		orch, _ := newTestOrchestrator(p, nil, t.TempDir())
		drain(t, orch.Send(context.Background(), "first question"))

		// The turn is over and the user steers the conversation they
		// are looking at, then types a new question.
		orch.Steer("late steer")
		drain(t, orch.Send(context.Background(), "second question"))

		msgs := p.gotRequests[1].Messages
		if len(msgs) != 4 {
			t.Fatalf("second request has %d messages: %+v", len(msgs), msgs)
		}
		if msgs[2].Role != "user" || msgs[2].Content != "late steer" {
			t.Errorf("the steer is not where the user typed it: %+v", msgs[2])
		}
		if msgs[3].Content != "second question" {
			t.Errorf("the new question must follow the steer: %+v", msgs[3])
		}
	})
}

// TestEffortIsSafeToReadWhileItIsSwitched: the effort is written by the
// UI goroutine (SetEffort, mid-turn switch) and read for every request
// and every subagent. Reading the field directly is a data race; the
// accessor is what makes it safe, and this is the only thing that keeps
// it that way.
func TestEffortIsSafeToReadWhileItIsSwitched(t *testing.T) {
	orch, _ := newTestOrchestrator(&fakeProvider{}, nil, t.TempDir())
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for _, effort := range []string{"low", "medium", "high", ""} {
			for i := 0; i < 200; i++ {
				orch.SetEffort(effort)
			}
		}
		close(stop)
	}()
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					_ = orch.Effort()
				}
			}
		}()
	}
	wg.Wait()
	if got := orch.Effort(); got != "" {
		t.Errorf("Effort() = %q, want the last write", got)
	}
}

// TestInterruptedToolKeepsItsPartialOutput: a tool interrupted
// mid-run may have partially applied — a killed command's side
// effects are real — so its own output rides along with the
// interrupted note instead of being discarded. The model must not be
// told a write that happened did not.
func TestInterruptedToolKeepsItsPartialOutput(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	p := &fakeProvider{rounds: [][]llm.ChatEvent{
		{{Type: llm.ToolCallEvent, Call: llm.ToolCall{ID: "c1", Name: "cancel_partial", Arguments: `{}`}},
			{Type: llm.ToolCallEvent, Call: llm.ToolCall{ID: "c2", Name: "read_file", Arguments: `{"path": "missing-ok"}`}}},
	}}
	orch, reg := newTestOrchestrator(p, nil, t.TempDir())
	reg.Register(cancelingTool{
		cancel: cancel,
		name:   "cancel_partial",
		output: "created draft.txt, 12 lines",
	})

	for ev := range orch.Send(ctx, "go") {
		_ = ev
	}
	h := orch.History()
	if len(h) < 3 || h[2].Role != "tool" || h[2].ToolCallID != "c1" {
		t.Fatalf("history after interrupt = %+v", h)
	}
	if !strings.Contains(h[2].Content, "interrupted by the user") {
		t.Errorf("the interrupted note is missing: %q", h[2].Content)
	}
	if !strings.Contains(h[2].Content, "created draft.txt") {
		t.Errorf("the partially-applied evidence was discarded: %q", h[2].Content)
	}
	// The later call is answered as never run — its tool never started.
	if len(h) < 4 || !strings.Contains(h[3].Content, "before this tool ran") {
		t.Errorf("the never-started call was not marked: %+v", h)
	}
}
