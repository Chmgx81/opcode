package headless

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Chmgx81/tilde/internal/llm"
	"github.com/Chmgx81/tilde/internal/orchestrator"
	"github.com/Chmgx81/tilde/internal/tools"
)

// scriptedProvider: round 1 calls write_file, round 2 answers.
type scriptedProvider struct {
	rounds      [][]llm.ChatEvent
	gotRequests []llm.ChatRequest
}

func (p *scriptedProvider) StreamChat(ctx context.Context, req llm.ChatRequest) (<-chan llm.ChatEvent, error) {
	p.gotRequests = append(p.gotRequests, req)
	if len(p.rounds) == 0 {
		return nil, fmt.Errorf("no scripted round left")
	}
	round := p.rounds[0]
	p.rounds = p.rounds[1:]
	ch := make(chan llm.ChatEvent, len(round))
	for _, ev := range round {
		ch <- ev
	}
	close(ch)
	return ch, nil
}

func TestRunTextMode(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "out.txt")
	p := &scriptedProvider{rounds: [][]llm.ChatEvent{
		{{Type: llm.ToolCallEvent, Call: llm.ToolCall{
			ID: "c1", Name: "write_file",
			Arguments: fmt.Sprintf(`{"path": %q, "content": "x"}`, target)}}},
		{{Type: llm.TextEvent, Text: "all done"}},
	}}
	var reg tools.Registry
	reg.Register(tools.ReadFile{})
	reg.Register(tools.WriteFile{})
	// nil Decide = the permissive default; the headless wiring passes
	// PolicyDecide(mode, nil) so ask mode fails closed — covered by the
	// denial test below.
	orch := orchestrator.New(p, "m", "s", &reg, &tools.Gate{})

	var buf bytes.Buffer
	if err := Run(context.Background(), orch, "write the file", Options{Out: &buf}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"~ write the file", "[tool] write_file", "[result]", "all done"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	if _, err := os.Stat(target); err != nil {
		t.Errorf("the tool never really executed: %v", err)
	}
}

func TestRunJSONMode(t *testing.T) {
	p := &scriptedProvider{rounds: [][]llm.ChatEvent{
		{{Type: llm.TextEvent, Text: "answer"}, {Type: llm.UsageEvent, Usage: llm.Usage{PromptTokens: 5, CompletionTokens: 2}}},
	}}
	orch := orchestrator.New(p, "m", "s", &tools.Registry{}, &tools.Gate{})

	var buf bytes.Buffer
	if err := Run(context.Background(), orch, "go", Options{Out: &buf, JSON: true}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Every line must be a valid JSON object with a kind.
	var kinds []string
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		var obj map[string]any
		if err := json.Unmarshal([]byte(line), &obj); err != nil {
			t.Fatalf("non-JSON line: %q", line)
		}
		kind, _ := obj["kind"].(string)
		kinds = append(kinds, kind)
	}
	want := []string{"start", "text", "usage", "done"}
	if len(kinds) != len(want) {
		t.Fatalf("kinds = %v, want %v", kinds, want)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Errorf("kinds[%d] = %s, want %s", i, kinds[i], want[i])
		}
	}
}

func TestRunProviderErrorSurfaces(t *testing.T) {
	p := &scriptedProvider{rounds: [][]llm.ChatEvent{
		{{Type: llm.ErrorEvent, Err: fmt.Errorf("provider down")}},
	}}
	orch := orchestrator.New(p, "m", "s", &tools.Registry{}, &tools.Gate{})

	var buf bytes.Buffer
	err := Run(context.Background(), orch, "go", Options{Out: &buf})
	if err == nil {
		t.Fatal("the turn error must surface as a non-zero result")
	}
	if !strings.Contains(buf.String(), "provider down") {
		t.Errorf("error not printed: %q", buf.String())
	}
}

func TestRunDenialIsVisible(t *testing.T) {
	// The headless wiring uses PolicyDecide(mode, nil) — with nobody
	// to ask, read-only denies action-tier calls and the denial is
	// printed as the tool result, not swallowed. (Ask mode would
	// auto-run this write: /tmp is inside the sandbox's writable
	// roots — see TestRunBoundedWriteRuns.)
	dir := t.TempDir()
	target := filepath.Join(dir, "out.txt")
	p := &scriptedProvider{rounds: [][]llm.ChatEvent{
		{{Type: llm.ToolCallEvent, Call: llm.ToolCall{
			ID: "c1", Name: "write_file",
			Arguments: fmt.Sprintf(`{"path": %q, "content": "x"}`, target)}}},
		{{Type: llm.TextEvent, Text: "could not write"}},
	}}
	var reg tools.Registry
	reg.Register(tools.WriteFile{})
	gate := &tools.Gate{Decide: tools.PolicyDecide(tools.ModeReadOnly, nil)}
	orch := orchestrator.New(p, "m", "s", &reg, gate)

	var buf bytes.Buffer
	if err := Run(context.Background(), orch, "write it", Options{Out: &buf}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(buf.String(), "permission denied") {
		t.Errorf("denial not visible in output: %q", buf.String())
	}
	if _, err := os.Stat(target); err == nil {
		t.Error("the denied tool executed anyway")
	}
}

// TestRunBoundedWriteRuns: ask mode's Phase 30 posture — a write
// inside the sandbox's writable roots runs headless (nobody to ask,
// none needed: the path is bounded) while the escape fails closed.
func TestRunBoundedWriteRuns(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "out.txt")
	p := &scriptedProvider{rounds: [][]llm.ChatEvent{
		{{Type: llm.ToolCallEvent, Call: llm.ToolCall{
			ID: "c1", Name: "write_file",
			Arguments: fmt.Sprintf(`{"path": %q, "content": "x"}`, target)}}},
		{{Type: llm.TextEvent, Text: "done"}},
	}}
	var reg tools.Registry
	reg.Register(tools.WriteFile{})
	gate := &tools.Gate{Decide: tools.PolicyDecide(tools.ModeAsk, nil)}
	orch := orchestrator.New(p, "m", "s", &reg, gate)

	var buf bytes.Buffer
	if err := Run(context.Background(), orch, "write it", Options{Out: &buf}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if data, err := os.ReadFile(target); err != nil || string(data) != "x" {
		t.Errorf("bounded write did not run: %v", err)
	}
	if strings.Contains(buf.String(), "permission denied") {
		t.Errorf("bounded write was denied: %q", buf.String())
	}
}
