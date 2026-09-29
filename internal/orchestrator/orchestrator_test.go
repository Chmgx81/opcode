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
	"testing"
	"time"

	"github.com/Chmgx81/tilde/internal/llm"
	"github.com/Chmgx81/tilde/internal/tools"
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
// interrupt landing during a tool call does.
type cancelingTool struct{ cancel context.CancelFunc }

func (cancelingTool) Name() string        { return "cancel_now" }
func (cancelingTool) Description() string { return "test tool" }
func (cancelingTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object"}`)
}
func (cancelingTool) Tier() tools.Tier { return tools.TierReadOnly }
func (c cancelingTool) Execute(ctx context.Context, args string) (string, error) {
	c.cancel()
	return "", ctx.Err()
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
