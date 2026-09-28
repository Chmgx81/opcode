package tui

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Chmgx81/tilde/internal/tools"
)

func newPermReq(tool, args string) *permRequest {
	return &permRequest{
		tool: tool, tier: tools.TierActionAllowed, args: args,
		scope: alwaysScope(tool, args), sel: 2,
		reply: make(chan bool, 1),
	}
}

// TestPermDialogRender pins the dialog's anatomy: plain-words title
// and tier badge, the literal command, the three numbered options
// with No highlighted first.
func TestPermDialogRender(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)
	m.width = 80
	req := newPermReq("run_shell", `{"command": "npm init -y"}`)
	m.awaitingPerm = req

	view := m.View()
	for _, want := range []string{
		"Bash command", "Runs a command",
		"npm init -y",
		"Do you want to proceed?",
		"1. Yes",
		"2. Yes, and don't ask again for: npm init:*",
		"3. No",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("dialog missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "action-allowed") {
		t.Errorf("internal tier string leaked into the dialog:\n%s", view)
	}
}

// TestPermDialogKeys pins the interaction: arrows move, numbers and
// y/a/n select, esc denies, enter takes the highlighted option.
func TestPermDialogKeys(t *testing.T) {
	answer := func(m *Model, keys ...string) bool {
		t.Helper()
		req := newPermReq("run_shell", `{"command": "go test ./..."}`)
		m.awaitingPerm = req
		for _, k := range keys {
			m.Update(keyMsg(k))
		}
		select {
		case v := <-req.reply:
			return v
		default:
			t.Fatalf("no answer after keys %v (dialog still open: %v)", keys, m.awaitingPerm != nil)
			return false
		}
	}

	dir := t.TempDir()
	m, _ := newText(t, dir, nil)

	if !answer(m, "y") {
		t.Error("y must allow")
	}
	if !answer(m, "1") {
		t.Error("1 must allow")
	}
	if !answer(m, "2") {
		t.Error("2 must allow")
	}
	if m.sessionAllow == nil || !m.sessionAllow.Allows("go test ./...") {
		t.Errorf("option 2 did not grant the prefix: %v", m.sessionRules)
	}
	if answer(m, "n") {
		t.Error("n must deny")
	}
	if answer(m, "esc") {
		t.Error("esc must deny")
	}
	// Up from No (default) wraps to Yes; enter allows.
	if !answer(m, "up", "enter") {
		t.Error("up+enter must allow (No → Yes)")
	}
	// Down twice from No lands on Yes.
	if !answer(m, "down", "down", "enter") {
		t.Error("down+down+enter must allow")
	}
	// Down once from No wraps to Yes.
	if !answer(m, "down", "enter") {
		t.Error("down+enter must allow (No wraps to Yes)")
	}
}

// TestPermSessionGrantEndToEnd proves the "don't ask again" rule
// short-circuits the next matching call without a dialog, and that a
// NON-matching command still prompts.
func TestPermSessionGrantEndToEnd(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)
	m.program = nil // decide() must not need the program for granted calls

	// Grant "npm init" via option 2 on the first ask.
	req := newPermReq("run_shell", `{"command": "npm init -y"}`)
	m.awaitingPerm = req
	m.Update(keyMsg("2"))
	if v := <-req.reply; !v {
		t.Fatal("option 2 must allow")
	}

	// A matching command runs with no dialog at all.
	if !m.decide(runShellTool{}, `{"command": "npm init --yes"}`) {
		t.Error("granted prefix must auto-allow a matching command")
	}
	// A different command still asks.
	if m.decide(runShellTool{}, `{"command": "rm -rf /tmp/x"}`) {
		t.Error("non-matching command must not be auto-allowed")
	}
	// A metacharacter-bearing command never matches the grant.
	if m.decide(runShellTool{}, `{"command": "npm init ; rm -rf /"}`) {
		t.Error("metacharacters must fail closed against the grant")
	}

	// Non-shell tools get their own per-tool grant.
	req2 := newPermReq("write_file", `{"path": "/tmp/a.go"}`)
	m.awaitingPerm = req2
	m.Update(keyMsg("2"))
	if v := <-req2.reply; !v {
		t.Fatal("option 2 must allow write_file")
	}
	if !m.decide(fakeTool{name: "write_file"}, `{"path": "/tmp/b.go"}`) {
		t.Error("write_file grant must cover later write_file calls")
	}
}

type runShellTool struct{}

func (runShellTool) Name() string                { return "run_shell" }
func (runShellTool) Description() string         { return "" }
func (runShellTool) Parameters() json.RawMessage { return nil }
func (runShellTool) Tier() tools.Tier            { return tools.TierActionAllowed }
func (runShellTool) Execute(ctx context.Context, args string) (string, error) {
	return "", nil
}

type fakeTool struct{ name string }

func (f fakeTool) Name() string                { return f.name }
func (f fakeTool) Description() string         { return "" }
func (f fakeTool) Parameters() json.RawMessage { return nil }
func (f fakeTool) Tier() tools.Tier            { return tools.TierActionAllowed }
func (f fakeTool) Execute(ctx context.Context, args string) (string, error) {
	return "", nil
}
