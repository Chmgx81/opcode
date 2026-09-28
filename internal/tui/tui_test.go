package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"tilde/internal/llm"
	"tilde/internal/orchestrator"
	"tilde/internal/tools"
)

// scriptedProvider is an llm.Provider the TUI tests control: each
// StreamChat call can block until released, so mid-turn actions (steer,
// queue) happen at deterministic moments.
type scriptedProvider struct {
	mu          sync.Mutex
	rounds      [][]llm.ChatEvent
	blockFirst  chan struct{} // if non-nil, round 1 blocks until closed
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
	block := p.blockFirst
	if block != nil {
		p.blockFirst = nil
		p.mu.Unlock()
		select {
		case <-block:
		case <-ctx.Done():
		}
	} else {
		p.mu.Unlock()
	}
	ch := make(chan llm.ChatEvent, len(round))
	for _, ev := range round {
		ch <- ev
	}
	close(ch)
	return ch, nil
}

func (p *scriptedProvider) requestCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.gotRequests)
}

func newText(t *testing.T, dir string, rounds [][]llm.ChatEvent) (*Model, *scriptedProvider) {
	t.Helper()
	fp := &scriptedProvider{rounds: rounds}
	var reg tools.Registry
	reg.Register(tools.ReadFile{})
	reg.Register(tools.WriteFile{})
	orch := orchestrator.New(fp, "test-model", "sys", &reg, &tools.Gate{})
	m := New(Options{
		Orch:         orch,
		Model:        "test-model",
		Mode:         tools.ModeAskEveryTime,
		Cwd:          dir,
		TildeHome:    dir,
		ProviderName: "openrouter",
		BaseURL:      "http://example.test/v1",
		AuditPath:    filepath.Join(dir, "audit.jsonl"),
	})
	return m, fp
}

func keyMsg(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func enterKey() tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyEnter}
}

func altEnterKey() tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyEnter, Alt: true}
}

func escKey() tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyEsc}
}

func typeAndEnter(m *Model, text string) tea.Cmd {
	m.input.SetValue(text)
	model, cmd := m.Update(enterKey())
	m = model.(*Model)
	return cmd
}

func TestSubmitWhileIdleStartsTurn(t *testing.T) {
	dir := t.TempDir()
	m, fp := newText(t, dir, [][]llm.ChatEvent{
		{{Type: llm.TextEvent, Text: "hello"}},
	})

	cmd := typeAndEnter(m, "hi there")
	if cmd == nil {
		t.Fatal("submit returned no command")
	}
	if !m.working {
		t.Error("working = false after submit")
	}
	waitFor(t, func() bool { return fp.requestCount() == 1 })
	if got := fp.gotRequests[0].Messages[0].Content; got != "hi there" {
		t.Errorf("user message = %q", got)
	}
}

func TestEnterWhileWorkingSteers(t *testing.T) {
	dir := t.TempDir()
	block := make(chan struct{})
	fp := &scriptedProvider{
		blockFirst: block,
		rounds: [][]llm.ChatEvent{
			// Round 1 must call a tool, so the turn continues to
			// round 2 — the moment a steering message can fold in.
			{{Type: llm.ToolCallEvent, Call: llm.ToolCall{
				ID: "call-1", Name: "read_file", Arguments: `{"path": "missing-ok"}`}}},
			{{Type: llm.TextEvent, Text: "ok"}},
		},
	}
	var reg tools.Registry
	reg.Register(tools.ReadFile{})
	orch := orchestrator.New(fp, "m", "s", &reg, &tools.Gate{})
	m := New(Options{Orch: orch, TildeHome: dir, ProviderName: "openrouter",
		BaseURL: "http://example.test/v1", AuditPath: filepath.Join(dir, "audit.jsonl")})

	typeAndEnter(m, "first")
	waitFor(t, func() bool { return fp.requestCount() == 1 })

	// Enter mid-turn steers instead of queuing.
	typeAndEnter(m, "wait, do it differently")
	if len(m.queue) != 0 {
		t.Errorf("queue = %v, want empty (Enter steers, not queues)", m.queue)
	}
	close(block)
	waitFor(t, func() bool { return fp.requestCount() == 2 })

	msgs := fp.gotRequests[1].Messages
	var steer *llm.Message
	for i := range msgs {
		if msgs[i].Role == "user" && msgs[i].Content == "wait, do it differently" {
			steer = &msgs[i]
		}
	}
	if steer == nil {
		t.Errorf("steering message never reached the model: %+v", msgs)
	}
}

func TestAltEnterWhileWorkingQueuesFollowUp(t *testing.T) {
	dir := t.TempDir()
	block := make(chan struct{})
	fp := &scriptedProvider{
		blockFirst: block,
		rounds: [][]llm.ChatEvent{
			{{Type: llm.TextEvent, Text: "one"}},
			{{Type: llm.TextEvent, Text: "two"}},
		},
	}
	var reg tools.Registry
	reg.Register(tools.ReadFile{})
	orch := orchestrator.New(fp, "m", "s", &reg, &tools.Gate{})
	m := New(Options{Orch: orch, TildeHome: dir, ProviderName: "openrouter",
		BaseURL: "http://example.test/v1", AuditPath: filepath.Join(dir, "audit.jsonl")})

	typeAndEnter(m, "first message")
	waitFor(t, func() bool { return fp.requestCount() == 1 })

	m.input.SetValue("follow up next")
	m.Update(altEnterKey())
	if len(m.queue) != 1 || m.queue[0] != "follow up next" {
		t.Fatalf("queue = %v", m.queue)
	}

	// Turn completes; the queue must drain into a new turn. The turn
	// runs on its own goroutine, and this test injects the completion
	// event directly (the pump drops events without a tea program), so
	// give the goroutine a moment to append the assistant message
	// first — in production the drain fires from the turn's own event,
	// which is emitted only after that append, so the ordering there is
	// guaranteed rather than timed.
	close(block)
	time.Sleep(200 * time.Millisecond)
	m.Update(orchestratorMsg(orchestrator.Event{Kind: orchestrator.EventTurnComplete}))
	if !m.working {
		t.Error("queued follow-up did not start a new turn")
	}
	waitFor(t, func() bool { return fp.requestCount() == 2 })
	last := fp.gotRequests[1].Messages
	if last[len(last)-1].Content != "follow up next" {
		t.Errorf("follow-up not sent after turn complete: %+v", last)
	}
}

func TestPermissionPromptAnswers(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)

	req := &permRequest{tool: "write_file", tier: tools.TierActionAllowed,
		args: `{"path": "x"}`, reply: make(chan bool, 1)}
	m.Update(permRequestMsg{req})

	if m.awaitingPerm != req {
		t.Fatal("prompt not shown")
	}
	if v := m.View(); !strings.Contains(v, "write_file") || !strings.Contains(v, "allow all") {
		t.Errorf("view does not show the permission prompt: %q", v)
	}

	// Typing anything but y/a/n must not answer the prompt.
	m.Update(keyMsg("z"))
	select {
	case <-req.reply:
		t.Fatal("stray key answered the prompt")
	default:
	}

	m.Update(keyMsg("y"))
	if m.awaitingPerm != nil {
		t.Error("prompt still shown after answer")
	}
	if !<-req.reply {
		t.Error("y must allow")
	}

	// Deny path.
	req2 := &permRequest{tool: "run_shell", tier: tools.TierActionAllowed,
		args: `{}`, reply: make(chan bool, 1)}
	m.Update(permRequestMsg{req2})
	m.Update(escKey())
	if <-req2.reply {
		t.Error("esc must deny")
	}

	// "a" allows now and skips prompts for the session.
	req3 := &permRequest{tool: "write_file", tier: tools.TierActionAllowed,
		args: `{}`, reply: make(chan bool, 1)}
	m.Update(permRequestMsg{req3})
	m.Update(keyMsg("a"))
	if !<-req3.reply {
		t.Error("a must allow")
	}
	if !m.decide(tools.WriteFile{}, `{}`) {
		t.Error("after 'a', action-tier calls must skip the prompt")
	}
}

func TestLoginStoresKeyAndSwapsProvider(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)

	typeAndEnter(m, "/login")
	if m.login == nil {
		t.Fatal("/login did not start the login flow")
	}
	if v := m.View(); !strings.Contains(v, "input hidden") {
		t.Errorf("view does not mention masked input: %q", v)
	}

	m.input.SetValue("brand-new-key")
	m.Update(enterKey())

	data, err := os.ReadFile(filepath.Join(dir, "auth.json"))
	if err != nil {
		t.Fatalf("auth.json not written: %v", err)
	}
	if !strings.Contains(string(data), "brand-new-key") {
		t.Errorf("auth.json = %s", data)
	}
	info, _ := os.Stat(filepath.Join(dir, "auth.json"))
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("auth.json mode = %v, want 0600", perm)
	}
	// The provider must use the new key without a restart.
	p, ok := m.opt.Orch.Provider.(*llm.OpenAICompat)
	if !ok || p.APIKey != "brand-new-key" {
		t.Errorf("provider not swapped to the new key: %+v", m.opt.Orch.Provider)
	}
	if m.login != nil {
		t.Error("login flow still active after submit")
	}
}

func TestLoginEmptyKeyCancels(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)
	typeAndEnter(m, "/login")
	m.input.SetValue("   ")
	m.Update(enterKey())
	if _, err := os.Stat(filepath.Join(dir, "auth.json")); !os.IsNotExist(err) {
		t.Error("empty key must not be written")
	}
	if m.login != nil {
		t.Error("login flow still active after empty submit")
	}
}

func TestLogoutRemovesStoredKey(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)
	if err := writeKeyForTest(dir, "openrouter", "to-remove"); err != nil {
		t.Fatal(err)
	}

	typeAndEnter(m, "/logout")
	data, err := os.ReadFile(filepath.Join(dir, "auth.json"))
	if err != nil {
		t.Fatalf("auth.json missing after logout: %v", err)
	}
	if strings.Contains(string(data), "to-remove") {
		t.Errorf("key still present after /logout: %s", data)
	}
	p, ok := m.opt.Orch.Provider.(*llm.OpenAICompat)
	if !ok || p.APIKey != "" {
		t.Errorf("provider still holds the old key after logout")
	}
	var said, saidWhat bool
	for _, l := range m.lines {
		if strings.Contains(l, "logout") || strings.Contains(l, "removed") {
			said = true
		}
		if strings.Contains(l, "does not unset") {
			saidWhat = true
		}
	}
	if !said || !saidWhat {
		t.Errorf("logout message incomplete: %v", m.lines)
	}

	// Second logout says there is nothing to remove rather than erroring.
	before := len(m.lines)
	typeAndEnter(m, "/logout")
	if len(m.lines) != before+1 {
		t.Error("second /logout should print a 'nothing to remove' line")
	}
}

func TestEscCancelsWorkingTurn(t *testing.T) {
	dir := t.TempDir()
	m, fp := newText(t, dir, [][]llm.ChatEvent{
		{{Type: llm.TextEvent, Text: "partial"}},
	})
	typeAndEnter(m, "go")
	waitFor(t, func() bool { return fp.requestCount() == 1 })
	if m.cancel == nil {
		t.Fatal("no cancel function after starting a turn")
	}
	m.Update(escKey())
	if m.cancel != nil {
		t.Error("esc did not cancel the turn")
	}

	// A cancelled turn renders as a plain note, not a scary error.
	m.Update(orchestratorMsg(orchestrator.Event{Kind: orchestrator.EventError, Err: orchestrator.ErrCancelled}))
	if m.working {
		t.Error("still working after cancelled event")
	}
	joined := strings.Join(m.lines, "\n")
	if !strings.Contains(joined, "turn cancelled") {
		t.Errorf("cancelled note missing: %q", joined)
	}
}

func TestStreamingEventsRender(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)

	m.Update(orchestratorMsg(orchestrator.Event{Kind: orchestrator.EventText, Text: "Hello "}))
	m.Update(orchestratorMsg(orchestrator.Event{Kind: orchestrator.EventText, Text: "world"}))
	if v := m.View(); !strings.Contains(v, "Hello world") {
		t.Errorf("streamed text not rendered incrementally: %q", v)
	}

	m.Update(orchestratorMsg(orchestrator.Event{Kind: orchestrator.EventToolStart,
		ToolCall: llm.ToolCall{Name: "read_file", Arguments: `{"path": "x"}`}}))
	if v := m.View(); !strings.Contains(v, "read_file") {
		t.Errorf("tool call not rendered: %q", v)
	}

	m.Update(orchestratorMsg(orchestrator.Event{Kind: orchestrator.EventUsage,
		Usage: llm.Usage{PromptTokens: 7, CompletionTokens: 3}}))
	if v := m.View(); !strings.Contains(v, "7 in / 3 out") {
		t.Errorf("usage not in status bar: %q", v)
	}
}

func TestExitCommandQuits(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)
	cmd := typeAndEnter(m, "/exit")
	if cmd == nil {
		t.Fatal("/exit returned no command — it must quit")
	}
	// tea.Quit is a command; the only observable contract here is that
	// submitting /exit produces a command (nil means "keep going").
}

func writeKeyForTest(dir, provider, key string) error {
	f, err := os.Create(filepath.Join(dir, "auth.json"))
	if err != nil {
		return err
	}
	defer f.Close()
	fmt.Fprintf(f, `{"%s": "%s"}`, provider, key)
	return nil
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	for i := 0; i < 200; i++ {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition never became true")
}

func TestViewLayoutFitsTerminal(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)
	m.width, m.height = 80, 24

	// Build a realistic mid-turn state: transcript, stream, prompt.
	m.appendWrapped(accentStyle, "❯ ", "create the file")
	m.Update(orchestratorMsg(orchestrator.Event{Kind: orchestrator.EventText, Text: "Writing it now."}))
	m.Update(orchestratorMsg(orchestrator.Event{
		Kind:     orchestrator.EventToolStart,
		ToolCall: llm.ToolCall{Name: "write_file", Arguments: `{"path": "demo.txt", "content": "x"}`}}))
	req := &permRequest{tool: "write_file", tier: tools.TierActionAllowed,
		args: `{}`, reply: make(chan bool, 1)}
	m.Update(permRequestMsg{req})

	view := m.View()
	lines := strings.Split(view, "\n")

	// The whole frame must fit the terminal: bubbletea's inline
	// renderer corrupts the screen when a frame is taller than the
	// window (seen live in the PTY).
	if len(lines) > 24 {
		t.Errorf("frame is %d rows, must fit 24:\n%s", len(lines), view)
	}

	// Every rendered line must fit the width — a frame line wider than
	// the terminal wraps on its own and breaks the layout.
	for i, l := range lines {
		if w := lipgloss.Width(l); w > 80 {
			t.Errorf("line %d is %d cols wide (max 80): %q", i, w, l)
		}
	}

	// The input box must be a proper rounded rectangle, closed on both
	// sides, with its width tied to the terminal.
	var boxTop, boxBottom bool
	for _, l := range lines {
		v := lipgloss.Width(l)
		if strings.HasPrefix(l, "╭─") && v == 78 {
			boxTop = true
		}
		if strings.HasPrefix(l, "╰─") && v == 78 {
			boxBottom = true
		}
	}
	if !boxTop || !boxBottom {
		t.Errorf("input box borders missing or wrong width:\n%s", view)
	}

	// The permission prompt must render inside the frame, not push the
	// input box off-screen: with the prompt up, the transcript should
	// have been trimmed accordingly.
	if !strings.Contains(view, "allow?") {
		t.Errorf("permission prompt missing from view:\n%s", view)
	}
}

func TestViewWideContentWrapsNotOverflows(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)
	m.width, m.height = 80, 40

	// A very long tool result must become multiple transcript lines,
	// each within the terminal width.
	m.Update(orchestratorMsg(orchestrator.Event{
		Kind:       orchestrator.EventToolResult,
		ToolResult: strings.Repeat("the quick brown fox jumps over the lazy dog ", 20),
	}))

	view := m.View()
	for i, l := range strings.Split(view, "\n") {
		if w := lipgloss.Width(l); w > 80 {
			t.Errorf("line %d is %d cols wide: %q", i, w, l)
		}
	}
	// Exactly one ⎿ marker per entry; the overflow shows up as extra
	// indented lines carrying the wrapped text.
	if n := strings.Count(view, "⎿"); n != 1 {
		t.Errorf("got %d ⎿ markers, want 1", n)
	}
	if n := strings.Count(view, "the quick brown fox"); n < 2 {
		t.Errorf("long result should wrap into several lines, got %d", n)
	}
}

func TestFreshViewShowsBanner(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)
	m.width, m.height = 80, 24

	view := m.View()
	// The logo glyphs must be present on a fresh session, in the accent
	// color, and every banner line must fit the terminal width.
	if !strings.Contains(view, "▄") || !strings.Contains(view, "▀") {
		t.Errorf("banner missing from fresh view:\n%s", view)
	}
	for i, l := range strings.Split(view, "\n") {
		if w := lipgloss.Width(l); w > 80 {
			t.Errorf("line %d is %d cols wide: %q", i, w, l)
		}
	}
	if len(strings.Split(view, "\n")) > 24 {
		t.Errorf("fresh frame is %d rows, must fit 24", len(strings.Split(view, "\n")))
	}
}

func TestModeCommand(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)

	// No argument: shows the current mode and the options.
	typeAndEnter(m, "/mode")
	joined := strings.Join(m.lines, "\n")
	if !strings.Contains(joined, "ask-every-time") || !strings.Contains(joined, "full-auto") {
		t.Errorf("/mode with no arg should list modes: %q", joined)
	}

	// Invalid mode: rejected with the valid list.
	typeAndEnter(m, "/mode nonsense")
	joined = strings.Join(m.lines, "\n")
	if !strings.Contains(joined, "unknown mode nonsense") {
		t.Errorf("invalid mode not rejected: %q", joined)
	}
	if m.opt.Mode != tools.ModeAskEveryTime {
		t.Errorf("mode changed on invalid input: %q", m.opt.Mode)
	}

	// Valid switch: orchestrator, gate policy, and display all move.
	typeAndEnter(m, "/mode full-auto")
	if m.opt.Orch.Mode != tools.ModeFullAuto {
		t.Errorf("orchestrator mode = %q", m.opt.Orch.Mode)
	}
	if m.opt.Mode != tools.ModeFullAuto {
		t.Errorf("displayed mode = %q", m.opt.Mode)
	}
	// The rebuilt gate must now allow action-tier calls without
	// prompting (and without a permission request being raised).
	decide := m.opt.Orch.Gate.Decide
	if decide == nil {
		t.Fatal("gate has no policy after mode switch")
	}
	if !decide(tools.WriteFile{}, `{}`) {
		t.Error("full-auto gate denied an action-tier call")
	}

	// A session "allow all" grant is reset by a mode switch.
	m.allowAll = true
	typeAndEnter(m, "/mode read-only")
	if m.allowAll {
		t.Error("mode switch must reset the allow-all grant")
	}
	if decide := m.opt.Orch.Gate.Decide; decide(tools.WriteFile{}, `{}`) {
		t.Error("read-only gate must deny action-tier calls outright")
	}
}
