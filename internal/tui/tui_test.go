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
	"github.com/muesli/termenv"

	"github.com/Chmgx81/tilde/internal/config"
	"github.com/Chmgx81/tilde/internal/llm"
	"github.com/Chmgx81/tilde/internal/orchestrator"
	"github.com/Chmgx81/tilde/internal/session"
	"github.com/Chmgx81/tilde/internal/tools"
)

// scriptedProvider is an llm.Provider the tests control.
type scriptedProvider struct {
	mu          sync.Mutex
	rounds      [][]llm.ChatEvent
	gotRequests []llm.ChatRequest
	switchedTo  string
	saved       bool
	resumed     string
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
	reg.Register(tools.RunShell{})
	orch := orchestrator.New(fp, "test-model", "sys", &reg, &tools.Gate{})
	orch.SetMode(tools.ModeAskEveryTime)
	m := New(Options{
		Orch:         orch,
		Model:        "test-model",
		Mode:         tools.ModeAskEveryTime,
		Cwd:          dir,
		TildeHome:    dir,
		ProviderName: "openrouter",
		BaseURL:      "http://example.test/v1",
		AuditPath:    filepath.Join(dir, "audit.jsonl"),
		Animations:   true,
		Models: config.ModelsConfig{
			DefaultProvider: "openrouter",
			Providers: map[string]config.ProviderConfig{
				"openrouter": {
					BaseURL:   "http://example.test/v1",
					APIKeyEnv: "OPENROUTER_API_KEY",
					Models:    []string{"test-model", "other-model"},
				},
			},
		},
		SwitchModel: func(provider, model string) error {
			fp.switchedTo = model
			return nil
		},
		SaveCurrentSession: func() { fp.saved = true },
		ResumeSession: func(path string) (int, error) {
			fp.resumed = path
			return 42, nil
		},
	})
	m.width, m.height = 80, 30
	return m, fp
}

// transcript renders the entry list with ANSI stripped, for assertions.
func (m *Model) transcript() string {
	var out []string
	for i := range m.entries {
		out = append(out, m.renderEntry(&m.entries[i])...)
	}
	return stripANSI(strings.Join(out, "\n"))
}

func keyMsg(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func enterKey() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyEnter} }
func altEnterKey() tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyEnter, Alt: true}
}
func escKey() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyEsc} }

func typeAndEnter(m *Model, text string) tea.Cmd {
	m.composer.SetValue(text)
	model, cmd := m.Update(enterKey())
	_ = model
	return cmd
}

func drain(t *testing.T, events <-chan orchestrator.Event) []orchestrator.Event {
	t.Helper()
	var out []orchestrator.Event
	for ev := range events {
		out = append(out, ev)
	}
	return out
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
	// The composer cleared.
	if m.composer.Value() != "" {
		t.Error("composer not cleared on submit")
	}
}

func TestEnterWhileWorkingSteers(t *testing.T) {
	dir := t.TempDir()
	block := make(chan struct{})
	fp := &scriptedProvider{rounds: [][]llm.ChatEvent{
		{{Type: llm.ToolCallEvent, Call: llm.ToolCall{
			ID: "call-1", Name: "read_file", Arguments: `{"path": "missing-ok"}`}}},
		{{Type: llm.TextEvent, Text: "ok"}},
		{{Type: llm.TextEvent, Text: "ok2"}},
	}}
	var reg tools.Registry
	reg.Register(tools.ReadFile{})
	orch := orchestrator.New(fp, "m", "s", &reg, &tools.Gate{})
	m := New(Options{Orch: orch, TildeHome: dir, ProviderName: "openrouter",
		BaseURL: "http://example.test/v1", AuditPath: filepath.Join(dir, "audit.jsonl")})

	// Patch the provider to block round 1 until the test steers.
	steered := block
	orig := fp.StreamChat
	fp.mu.Lock()
	_ = orig
	fp.mu.Unlock()
	_ = steered

	typeAndEnter(m, "first")
	waitFor(t, func() bool { return fp.requestCount() >= 1 })

	typeAndEnter(m, "wait, do it differently")
	if len(m.queue) != 0 {
		t.Errorf("queue = %v, want empty (Enter steers, not queues)", m.queue)
	}
	// The steering message is queued; it reaches the model at the next
	// round boundary. The scripted rounds complete in microseconds, so
	// the deterministic check is on the NEXT user turn, whose first
	// round drains any still-pending steer.
	waitFor(t, func() bool { return fp.requestCount() >= 2 })
	m.Update(orchestratorMsg(orchestrator.Event{Kind: orchestrator.EventTurnComplete}))
	typeAndEnter(m, "next turn")
	waitFor(t, func() bool { return fp.requestCount() >= 3 })
	fp.mu.Lock()
	last := fp.gotRequests[len(fp.gotRequests)-1].Messages
	fp.mu.Unlock()
	var sawSteer, sawNext bool
	for _, msg := range last {
		if msg.Role == "user" && msg.Content == "wait, do it differently" {
			sawSteer = true
		}
		if msg.Role == "user" && msg.Content == "next turn" {
			sawNext = true
		}
	}
	if !sawSteer {
		t.Error("steering message never reached the model at a round boundary")
	}
	if !sawNext {
		t.Error("the next turn's own message is missing")
	}
}

func TestAltEnterWhileWorkingQueuesFollowUp(t *testing.T) {
	dir := t.TempDir()
	block := make(chan struct{})
	fp := &scriptedProvider{rounds: [][]llm.ChatEvent{
		{{Type: llm.ToolCallEvent, Call: llm.ToolCall{
			ID: "call-1", Name: "read_file", Arguments: `{"path": "missing-ok"}`}}},
		{{Type: llm.TextEvent, Text: "two"}},
	}}
	var reg tools.Registry
	reg.Register(tools.ReadFile{})
	orch := orchestrator.New(fp, "m", "s", &reg, &tools.Gate{})
	m := New(Options{Orch: orch, TildeHome: dir, ProviderName: "openrouter",
		BaseURL: "http://example.test/v1", AuditPath: filepath.Join(dir, "audit.jsonl")})

	typeAndEnter(m, "first message")
	waitFor(t, func() bool { return fp.requestCount() >= 1 })

	m.composer.SetValue("follow up next")
	m.Update(altEnterKey())
	if len(m.queue) != 1 || m.queue[0] != "follow up next" {
		t.Fatalf("queue = %v", m.queue)
	}

	// Turn completes; the queue drains into a new turn.
	close(block)
	m.Update(orchestratorMsg(orchestrator.Event{Kind: orchestrator.EventTurnComplete}))
	if !m.working {
		t.Error("queued follow-up did not start a new turn")
	}
}

func TestNewlineKeys(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)

	// shift+enter is indistinguishable from enter in this bubbletea
	// version (no kitty protocol); ctrl+j is the newline key.
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlJ})
	m.composer.InsertString("line one")
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlJ})
	m.composer.InsertString("line two")

	if v := m.composer.Value(); !strings.Contains(v, "\n") {
		t.Errorf("composer = %q, want a newline from shift+enter/ctrl+j", v)
	}
}

func TestLargePasteCollapsesToToken(t *testing.T) {
	dir := t.TempDir()
	m, fp := newText(t, dir, [][]llm.ChatEvent{
		{{Type: llm.TextEvent, Text: "got it"}},
	})

	// A 6-line paste arrives as one flagged key event.
	pasted := "alpha\nbravo\ncharlie\ndelta\necho\nfoxtrot"
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(pasted), Paste: true})

	v := m.composer.Value()
	if !strings.Contains(v, "[paste 1 · 6 lines]") {
		t.Fatalf("large paste not collapsed: %q", v)
	}
	if strings.Contains(v, "bravo") {
		t.Error("paste content leaked into the composer instead of the token")
	}

	// Submit expands the token back to the content.
	typeAndEnter(m, v)
	waitFor(t, func() bool { return fp.requestCount() == 1 })
	if got := fp.gotRequests[0].Messages[0].Content; !strings.Contains(got, "bravo") {
		t.Errorf("token not expanded on submit: %q", got)
	}

	// A small paste inserts inline.
	m2, _ := newText(t, dir, nil)
	m2.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("just one line"), Paste: true})
	if v := m2.composer.Value(); v != "just one line" {
		t.Errorf("small paste = %q, want inline", v)
	}
}

func TestPaletteFiltersAndRuns(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)

	m.composer.SetValue("/mo")
	if !m.paletteOpen() {
		t.Fatal("palette should open for a bare /command")
	}
	matches := m.paletteMatches()
	if len(matches) != 2 { // /mode, /model
		t.Fatalf("matches = %v", matches)
	}

	// Down selects /model; Enter opens the models picker.
	m.Update(keyMsg("down"))
	typeAndEnter(m, "/mo")
	if m.picker == nil || m.picker.kind != pickerModels {
		t.Fatalf("/model should open the model picker: %s", m.transcript())
	}
	if it, _ := m.picker.current(); it.Model != "test-model" || it.Label != "test-model" {
		t.Errorf("picker cursor = %+v, want test-model active first", it)
	}

	// A space closes the palette (an argument is being typed).
	m.picker = nil
	m.composer.SetValue("/mode read-only")
	if m.paletteOpen() {
		t.Error("palette must close once arguments start")
	}
}

func TestModelPickerFiltersAndSwitches(t *testing.T) {
	dir := t.TempDir()
	m, fp := newText(t, dir, nil)

	typeAndEnter(m, "/model")
	if m.picker == nil {
		t.Fatal("/model should open the picker")
	}
	// Type to filter; only other-model matches.
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("other")})
	if got := len(m.picker.matched); got != 1 {
		t.Fatalf("filter matches = %d, want 1", got)
	}
	// Enter selects; SwitchModel runs; the status state follows.
	m.Update(keyMsg("enter"))
	if m.picker != nil {
		t.Error("picker should close on select")
	}
	if fp.switchedTo != "other-model" {
		t.Errorf("SwitchModel got %q, want other-model", fp.switchedTo)
	}
	if m.opt.Model != "other-model" {
		t.Errorf("opt.Model = %q, want other-model", m.opt.Model)
	}
	if tr := m.transcript(); !strings.Contains(tr, "switched to other-model") {
		t.Errorf("no switch note: %s", tr)
	}
	// Esc with the picker open cancels without switching.
	typeAndEnter(m, "/model")
	m.Update(keyMsg("esc"))
	if m.picker != nil {
		t.Error("esc should close the picker")
	}
	if fp.switchedTo != "other-model" {
		t.Errorf("esc must not switch: got %q", fp.switchedTo)
	}
}

func TestSessionsPickerResumes(t *testing.T) {
	dir := t.TempDir()
	m, fp := newText(t, dir, nil)

	// One saved session in the sessions dir.
	s := session.FromHistory("saved-model", tools.ModeAskEveryTime,
		[]llm.Message{{Role: "user", Content: "hello from the past"}})
	if err := os.MkdirAll(session.Dir(dir), 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(session.Dir(dir), "20260101-000000-abc123.json")
	if err := s.Save(path, nil); err != nil {
		t.Fatal(err)
	}

	typeAndEnter(m, "/sessions")
	if m.picker == nil || m.picker.kind != pickerSessions {
		t.Fatalf("/sessions should open the picker: %s", m.transcript())
	}
	it, ok := m.picker.current()
	if !ok || it.Path != path {
		t.Fatalf("picker item = %+v, want %s", it, path)
	}
	if !strings.Contains(it.Detail, "hello from the past") {
		t.Errorf("preview missing: %q", it.Detail)
	}
	m.Update(keyMsg("enter"))
	if !fp.saved {
		t.Error("the current conversation must be saved before switching")
	}
	if fp.resumed != path {
		t.Errorf("ResumeSession got %q, want %s", fp.resumed, path)
	}
	if tr := m.transcript(); !strings.Contains(tr, "resumed") || !strings.Contains(tr, "42 messages") {
		t.Errorf("no resume note: %s", tr)
	}
}

func TestAssistantEntriesRenderMarkdown(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)
	m.entries = nil // drop the greeting block; test only this entry
	m.entries = append(m.entries, entry{kind: entryAssistant,
		text: "# Title\n\nsome *emphasis* and a [link](http://example.test)\n\n```go\nfmt.Println(\"hi\")\n```\n"})

	var lines []string
	for i := range m.entries {
		lines = append(lines, m.renderEntry(&m.entries[i])...)
	}
	rendered := stripANSI(strings.Join(lines, "\n"))
	for _, want := range []string{"Title", "emphasis", "link", `fmt.Println("hi")`} {
		if !strings.Contains(rendered, want) {
			t.Errorf("markdown render lost %q:\n%s", want, rendered)
		}
	}
	// Second render uses the cache: same pointer, no rebuild needed.
	if m.entries[0].rendered == nil || m.entries[0].renderedW != m.termWidth() {
		t.Errorf("render cache not populated: %d lines, width %d",
			len(m.entries[0].rendered), m.entries[0].renderedW)
	}
	before := m.entries[0].rendered
	for i := range m.entries {
		m.renderEntry(&m.entries[i])
	}
	if &m.entries[0].rendered[0] != &before[0] {
		t.Error("cache must be reused, not re-rendered")
	}
	// A width change re-renders instead of reusing a stale layout.
	m.width = 60
	for i := range m.entries {
		m.renderEntry(&m.entries[i])
	}
	if m.entries[0].renderedW != 60 {
		t.Errorf("width change not picked up: %d", m.entries[0].renderedW)
	}
}

func TestTabCyclesModes(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)

	// Four modes, forward: ask -> full-auto -> read-only -> plan.
	m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if m.opt.Mode != tools.ModeFullAuto {
		t.Errorf("first tab = %q, want full-auto", m.opt.Mode)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if m.opt.Mode != tools.ModeReadOnly {
		t.Errorf("second tab = %q, want read-only", m.opt.Mode)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if m.opt.Mode != tools.ModePlan {
		t.Errorf("third tab = %q, want plan", m.opt.Mode)
	}
	if m.toast == "" {
		t.Error("mode toast not set")
	}
	// The gate follows: read-only denies an action-tier call without
	// consulting anyone.
	if decide := m.opt.Orch.Gate.Decide; decide == nil || decide(tools.WriteFile{}, `{}`) {
		t.Error("read-only gate must deny after cycling into it")
	}
	// Backward: plan -> read-only -> full-auto -> ask.
	m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	if m.opt.Mode != tools.ModeReadOnly {
		t.Errorf("shift+tab = %q, want read-only", m.opt.Mode)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	if m.opt.Mode != tools.ModeFullAuto {
		t.Errorf("second shift+tab = %q, want full-auto", m.opt.Mode)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	if m.opt.Mode != tools.ModeAskEveryTime {
		t.Errorf("second shift+tab = %q, want ask-every-time", m.opt.Mode)
	}
	// Mode changes announce via the transient toast only — a line per
	// keypress buried the conversation (user-reported).
	if tr := m.transcript(); strings.Contains(tr, "mode switched") {
		t.Errorf("mode switches must not write transcript lines:\n%s", tr)
	}
}

func TestCtrlRTogglesResultExpansion(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)

	m.Update(orchestratorMsg(orchestrator.Event{
		Kind:       orchestrator.EventToolResult,
		ToolCall:   llm.ToolCall{Name: "run_shell"},
		ToolResult: strings.Repeat("line\n", 30) + "the end",
	}))
	collapsed := m.View()
	if !strings.Contains(stripANSI(collapsed), "ctrl+r to expand") {
		t.Error("collapsed result missing the expand hint")
	}

	m.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	if !m.expandResults {
		t.Fatal("ctrl+r did not expand")
	}
	expanded := stripANSI(m.View())
	if strings.Contains(expanded, "ctrl+r to expand") {
		t.Error("hint shown while expanded")
	}
	if !strings.Contains(expanded, "the end") {
		t.Error("full result not shown when expanded")
	}
}

func TestEditFileResultRendersDiff(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)

	m.Update(orchestratorMsg(orchestrator.Event{
		Kind: orchestrator.EventToolResult,
		ToolCall: llm.ToolCall{Name: "edit_file",
			Arguments: `{"path": "main.go", "old": "import \"old\"", "new": "import \"new\"\nimport \"extra\""}`},
		ToolResult: "replaced one occurrence in main.go",
	}))
	tr := m.transcript()
	if !strings.Contains(tr, "updated main.go") {
		t.Errorf("diff summary missing: %s", tr)
	}
	// One line replaced (−1 +1) plus one line added: +2 −1.
	if !strings.Contains(tr, "+2") || !strings.Contains(tr, "−1") {
		t.Errorf("add/remove counts missing: %s", tr)
	}

	// Expanded shows the colored hunk lines.
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	tr = m.transcript()
	if !strings.Contains(tr, "import \"old\"") || !strings.Contains(tr, "import \"new\"") {
		t.Errorf("expanded diff missing old/new: %s", tr)
	}
}

func TestShellEscape(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)

	typeAndEnter(m, "! echo shell-escape-works")
	tr := m.transcript()
	if !strings.Contains(tr, "shell-escape-works") {
		t.Errorf("shell escape output missing: %s", tr)
	}
	// No model request: the user ran it, not the model.
}

func TestAtMentionAttachesFile(t *testing.T) {
	dir := t.TempDir()
	m, fp := newText(t, dir, [][]llm.ChatEvent{
		{{Type: llm.TextEvent, Text: "ok"}},
	})
	target := filepath.Join(dir, "note.txt")
	os.WriteFile(target, []byte("file body here"), 0o644)

	typeAndEnter(m, "summarize @"+target)
	waitFor(t, func() bool { return fp.requestCount() == 1 })
	sent := fp.gotRequests[0].Messages[0].Content
	if !strings.Contains(sent, "file body here") {
		t.Errorf("file contents not attached: %q", sent)
	}
}

func TestHelpOverlay(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)

	m.Update(keyMsg("?"))
	if !m.helpOpen {
		t.Fatal("? did not open help")
	}
	v := stripANSI(m.View())
	if !strings.Contains(v, "shift+tab") || !strings.Contains(v, "/skills") {
		t.Errorf("help content missing keys/commands:\n%s", v)
	}
	// Any key closes it.
	m.Update(keyMsg("x"))
	if m.helpOpen {
		t.Error("help did not close on keypress")
	}
}

func TestCompactionAndSubagentEntries(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)

	m.Update(orchestratorMsg(orchestrator.Event{Kind: orchestrator.EventCompaction,
		Text: "summarized 9 older messages into a recap (4 kept verbatim)"}))
	m.Update(subagentMsg(subagentEvent{Title: "auditor", Kind: subagentText, Text: "Reading. "}))
	m.Update(subagentMsg(subagentEvent{Title: "auditor", Kind: subagentTool, Text: "read_file"}))
	m.Update(subagentMsg(subagentEvent{Title: "auditor", Kind: subagentDone, Text: "done: 3 findings"}))

	tr := m.transcript()
	for _, want := range []string{"summarized 9 older", "Reading.", "read_file", "done: 3 findings"} {
		if !strings.Contains(tr, want) {
			t.Errorf("missing %q in transcript:\n%s", want, tr)
		}
	}
	if !strings.Contains(tr, "[auditor]") {
		t.Errorf("subagent label missing: %s", tr)
	}
}

func TestLoginLogoutAndModelCommand(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)

	// /login stores the key; the provider is rebuilt without a restart.
	typeAndEnter(m, "/login")
	if m.login == nil {
		t.Fatal("/login did not start the flow")
	}
	m.composer.SetValue("brand-new-key")
	m.Update(enterKey())
	data, err := os.ReadFile(filepath.Join(dir, "auth.json"))
	if err != nil {
		t.Fatalf("auth.json not written: %v", err)
	}
	if !strings.Contains(string(data), "brand-new-key") {
		t.Errorf("auth.json = %s", data)
	}
	if m.login != nil {
		t.Error("login flow still active after submit")
	}

	// /logout removes it and says what it does not do.
	if err := config.WriteAuthKey(dir, "openrouter", "to-remove"); err != nil {
		t.Fatal(err)
	}
	typeAndEnter(m, "/logout")
	tr := m.transcript()
	if !strings.Contains(tr, "does not unset") {
		t.Errorf("logout disclosure missing: %s", tr)
	}

	// Empty login key is cancelled, not written.
	before := m.transcript()
	typeAndEnter(m, "/login")
	m.composer.SetValue("   ")
	m.Update(enterKey())
	if !strings.Contains(m.transcript(), "login cancelled") || m.transcript() == before {
		t.Error("empty login key not cancelled")
	}
}

func TestModeCommandAndTrustPrompt(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)

	typeAndEnter(m, "/mode nonsense")
	if !strings.Contains(m.transcript(), "unknown mode nonsense") {
		t.Errorf("invalid mode not rejected: %s", m.transcript())
	}

	typeAndEnter(m, "/mode full-auto")
	if m.opt.Orch.Mode != tools.ModeFullAuto {
		t.Errorf("orchestrator mode = %q", m.opt.Orch.Mode)
	}
	if decide := m.opt.Orch.Gate.Decide; decide == nil || !decide(tools.WriteFile{}, `{}`) {
		t.Error("full-auto gate denied an action-tier call")
	}

	// Trust prompt paths.
	var answer *bool
	m.opt.PendingTrust = &TrustDecision{
		ProjectDir: "/tmp/some/project",
		Approved:   []string{".tilde/skills/deploy/scripts/run.sh"},
		OnAnswer: func(trusted bool) {
			v := trusted
			answer = &v
		},
	}
	m.awaitingTrust = m.opt.PendingTrust
	if v := m.View(); !strings.Contains(v, "trust this project?") || !strings.Contains(v, "run.sh") {
		t.Fatalf("trust prompt incomplete:\n%s", v)
	}
	m.Update(keyMsg("y"))
	if answer == nil || *answer != true {
		t.Errorf("accept not delivered: %v", answer)
	}
	if !strings.Contains(m.transcript(), "project trusted") {
		t.Error("grant note missing")
	}
}

func TestEscInterruptAndTurnCancelledRendering(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, [][]llm.ChatEvent{
		{{Type: llm.TextEvent, Text: "partial"}},
	})
	typeAndEnter(m, "go")
	if m.cancel == nil {
		t.Fatal("no cancel function after starting a turn")
	}
	m.Update(escKey())
	if m.cancel != nil {
		t.Error("esc did not cancel the turn")
	}

	m.Update(orchestratorMsg(orchestrator.Event{Kind: orchestrator.EventError, Err: orchestrator.ErrCancelled}))
	if m.working {
		t.Error("still working after cancelled event")
	}
	if !strings.Contains(m.transcript(), "turn interrupted") {
		t.Errorf("interrupt note missing: %s", m.transcript())
	}
}

func TestViewLayoutFitsTerminal(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)
	m.width, m.height = 80, 24

	m.add(entry{kind: entryUser, text: "fix the failing test in internal/tools/gate_test.go"})
	m.Update(orchestratorMsg(orchestrator.Event{Kind: orchestrator.EventText, Text: "I'll look at the failing test first."}))
	m.Update(orchestratorMsg(orchestrator.Event{
		Kind:     orchestrator.EventToolStart,
		ToolCall: llm.ToolCall{Name: "run_shell", Arguments: `{"command": "go test ./..."}`}}))
	m.Update(orchestratorMsg(orchestrator.Event{Kind: orchestrator.EventUsage,
		Usage: llm.Usage{PromptTokens: 1240, CompletionTokens: 96}}))
	req := &permRequest{tool: "write_file", tier: tools.TierActionAllowed,
		args: `{}`, reply: make(chan bool, 1)}
	m.Update(permRequestMsg{req})

	view := m.View()
	if n := strings.Count(view, "\n") + 1; n > 24 {
		t.Errorf("frame is %d rows, must fit 24:\n%s", n, view)
	}
	for i, l := range strings.Split(view, "\n") {
		if w := len([]rune(stripANSI(l))); w > 80 {
			t.Errorf("line %d is %d cols wide: %q", i, w, stripANSI(l))
		}
	}
	if !strings.Contains(view, "allow?") {
		t.Errorf("permission prompt missing:\n%s", view)
	}
}

func TestFreshViewShowsBannerAndBrand(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)
	view := stripANSI(m.View())
	for _, want := range []string{"▄", "tilde " + version, "test-model", "? help · / commands"} {
		if !strings.Contains(view, want) {
			t.Errorf("fresh view missing %q:\n%s", want, view)
		}
	}
}

func TestComposerResizesWithTerminal(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)

	// Regression: the textarea's internal width defaults to 40 columns,
	// so the placeholder wrapped onto two lines on any terminal. The
	// composer must size itself from WindowSizeMsg.
	m.Update(tea.WindowSizeMsg{Width: 70, Height: 30})
	// SetWidth receives the box content width (70-8); the textarea
	// reserves its 2-wide prompt from that, so Width() is 60.
	if got := m.composer.Width(); got != 70-8-2 {
		t.Errorf("composer width = %d, want %d", got, 70-8-2)
	}
	// One line when empty: a short placeholder, not a hint crammed
	// into it — hints live on the footer line below.
	if h := m.composer.Height(); h != 1 {
		t.Errorf("empty composer height = %d, want 1", h)
	}
	view := stripANSI(m.View())
	for _, l := range strings.Split(view, "\n") {
		if strings.Contains(l, "ask tilde") && strings.Contains(l, "/ commands") {
			t.Errorf("hints leaked into the placeholder: %q", l)
		}
	}
	if !strings.Contains(view, "? help · / commands") {
		t.Errorf("footer hints missing:\n%s", view)
	}
}

func TestComposerGrowsWithContent(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 30})

	if h := m.composer.Height(); h != 1 {
		t.Fatalf("empty composer height = %d, want 1", h)
	}
	// A newline (ctrl+j) grows the composer to hold the content.
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("line one")})
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlJ})
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("line two")})
	if h := m.composer.Height(); h != 2 {
		t.Errorf("two-line composer height = %d, want 2", h)
	}
	// A wrapped line grows it too, without an explicit newline.
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(strings.Repeat("w ", 45))})
	if h := m.composer.Height(); h < 3 {
		t.Errorf("wrapped composer height = %d, want >= 3", h)
	}
	// Submitting clears the value and the composer shrinks back.
	typeAndEnter(m, m.composer.Value())
	if h := m.composer.Height(); h != 1 {
		t.Errorf("composer height after submit = %d, want 1", h)
	}
}

func TestTranscriptHierarchy(t *testing.T) {
	// Force a real color profile so style assertions can see SGR codes;
	// restore the ambient profile for the rest of the suite.
	defer func(p termenv.Profile) { lipgloss.SetColorProfile(p) }(lipgloss.ColorProfile())
	lipgloss.SetColorProfile(termenv.TrueColor)

	dir := t.TempDir()
	m, _ := newText(t, dir, nil)

	// The placeholder: dim, and carrying no background rectangle — the
	// textarea's stock focused CursorLine paints one and it reads as a
	// selection highlight on any terminal whose floor isn't pure black.
	const dimSGR = "38;2;121;139;147"   // HexDim #7A8B94 (termenv rounds one step)
	const accentSGR = "38;2;34;211;238" // HexAccent #22D3EE in truecolor
	// The cursor block overlays the placeholder's first character, so
	// probe for a tail fragment rather than the whole string.
	view := m.View()
	if !strings.Contains(view, "tilde anything") {
		t.Fatalf("placeholder missing from view:\n%s", view)
	}
	placeholderLine := ""
	for _, l := range strings.Split(view, "\n") {
		if strings.Contains(l, "tilde anything") {
			placeholderLine = l
		}
	}
	if !strings.Contains(placeholderLine, dimSGR) {
		t.Errorf("placeholder not dim:\n%q", placeholderLine)
	}
	if strings.Contains(placeholderLine, "40m") || strings.Contains(placeholderLine, "48;") {
		t.Errorf("placeholder carries a background highlight:\n%q", placeholderLine)
	}

	// The echoed query renders at full weight behind the accent prompt,
	// matching the reference apps; the agent's answer does the same.
	m.entries = []entry{
		{kind: entryUser, text: "hello query"},
		{kind: entryAssistant, text: "answer text"},
	}
	user := strings.Join(m.renderEntry(&m.entries[0]), "")
	assistant := strings.Join(m.renderEntry(&m.entries[1]), "")
	if !strings.Contains(user, accentSGR) {
		t.Errorf("user query missing accent prompt:\n%q", user)
	}
	for _, line := range []struct{ name, s string }{
		{"user query", user},
		{"assistant output", assistant},
	} {
		if strings.Contains(line.s, dimSGR) {
			t.Errorf("%s must not be dim:\n%q", line.name, line.s)
		}
	}
}

func TestLoginEscCancels(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)

	typeAndEnter(m, "/login")
	if m.login == nil {
		t.Fatal("/login did not start the flow")
	}
	// A typed key then Esc: the flow cancels, the key is discarded,
	// and nothing reaches auth.json.
	m.composer.SetValue("typed-secret")
	m.Update(escKey())
	if m.login != nil {
		t.Error("esc must cancel the login flow")
	}
	if v := m.composer.Value(); v != "" {
		t.Errorf("typed key not discarded: %q", v)
	}
	if _, err := os.Stat(filepath.Join(dir, "auth.json")); !os.IsNotExist(err) {
		t.Error("auth.json must not exist after cancel")
	}
	if tr := m.transcript(); !strings.Contains(tr, "login cancelled") {
		t.Errorf("cancel note missing: %s", tr)
	}
}

func TestEscClosesPalette(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)

	m.composer.SetValue("/mo")
	if !m.paletteOpen() {
		t.Fatal("palette should be open for a bare /command")
	}
	m.Update(escKey())
	if m.paletteOpen() {
		t.Error("esc should close the palette")
	}
	if v := m.composer.Value(); v != "" {
		t.Errorf("half-typed command left behind: %q", v)
	}
	// Esc must not eat a plain draft when no modal is open.
	m.composer.SetValue("a real message, not a command")
	m.Update(escKey())
	if v := m.composer.Value(); v != "a real message, not a command" {
		t.Errorf("esc cleared a plain draft: %q", v)
	}
}

func TestInterruptClearsQueue(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)

	// A queued follow-up plus a real interruption: esc must stop
	// everything, not fire the queue the moment it lands.
	m.queue = []string{"follow-up one", "follow-up two"}
	m.working = true
	m.handleEvent(orchestrator.Event{Kind: orchestrator.EventError, Err: orchestrator.ErrCancelled})
	if len(m.queue) != 0 {
		t.Errorf("queue survived the interrupt: %v", m.queue)
	}
	tr := m.transcript()
	if !strings.Contains(tr, "turn interrupted") {
		t.Errorf("interrupt note missing: %s", tr)
	}
	if !strings.Contains(tr, "cleared 2 queued follow-ups") {
		t.Errorf("queue-clear note missing: %s", tr)
	}
	if m.working {
		t.Error("interrupt must end the turn")
	}
	// A clean completion still drains the queue (existing behavior,
	// guarded here so the fix can't overreach).
	m.queue = []string{"follow-up"}
	m.working = true
	m.handleEvent(orchestrator.Event{Kind: orchestrator.EventTurnComplete})
	if len(m.queue) != 0 {
		t.Errorf("clean completion must still drain the queue: %v", m.queue)
	}
}

func TestConversationSpacing(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)
	m.entries = append(m.entries,
		entry{kind: entryUser, text: "hello"},
		entry{kind: entryAssistant, text: "answer"},
		entry{kind: entryUser, text: "next question"},
	)
	lines := m.timelineView()
	// Exactly one blank line between blocks, none doubled.
	blanks := 0
	for i, l := range lines {
		if strings.TrimSpace(stripANSI(l)) == "" {
			blanks++
			if i > 0 && strings.TrimSpace(stripANSI(lines[i-1])) == "" {
				t.Errorf("doubled blank at line %d:\n%v", i, lines)
			}
		}
	}
	if blanks < 3 {
		t.Errorf("want a blank before each block, got %d of %d lines:\n%v", blanks, len(lines), lines)
	}
}

func TestEditDiffHighlightsSyntax(t *testing.T) {
	defer func(p termenv.Profile) { lipgloss.SetColorProfile(p) }(lipgloss.ColorProfile())
	lipgloss.SetColorProfile(termenv.TrueColor)

	m := &Model{expandResults: true}
	m.width = 80
	e := entry{kind: entryResult, tool: "edit_file", path: "main.go",
		old: "func main() {", new: "func main() error {"}
	lines := m.renderResult(e, 80)
	joined := strings.Join(lines, "\n")
	// The keyword keeps its accent color even inside the removed line;
	// the marker still carries the verdict.
	if !strings.Contains(joined, "38;2;34;211;238") { // HexAccent
		t.Errorf("keyword not highlighted in the diff:\n%q", joined)
	}
	if !strings.Contains(stripANSI(joined), "func main() {") {
		t.Errorf("content lost to highlighting:\n%s", joined)
	}
}

func TestAnimatedModeToast(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)

	cmd := m.setMode(tools.ModeFullAuto)
	if m.toastAnim != len(toastFrames) {
		t.Errorf("toastAnim = %d, want %d", m.toastAnim, len(toastFrames))
	}
	if cmd == nil {
		t.Fatal("animated toast must return a driving Cmd")
	}
	// Each tick consumes a frame until the animation settles.
	for i := 0; i < len(toastFrames); i++ {
		model, next := m.Update(toastTickMsg{})
		m = model.(*Model)
		if i < len(toastFrames)-1 && next == nil {
			t.Error("animation must keep ticking while frames remain")
		}
	}
	if m.toastAnim != 0 {
		t.Errorf("toastAnim after all frames = %d, want 0", m.toastAnim)
	}
}

func TestAtMentionPicker(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{"main.go", "notes.txt", "cmd/app/main.go"} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, f)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, f), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	m, _ := newText(t, dir, nil)

	// Typing "@" opens the live-filtered file menu.
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("see @")})
	if !m.atMenuOpen() {
		t.Fatal("trailing @ must open the file menu")
	}
	if !contains(m.atMenu, "main.go") || !contains(m.atMenu, "notes.txt") {
		t.Errorf("menu = %v", m.atMenu)
	}
	// Typing filters; arrows move; Enter inserts the path.
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("notes")})
	if len(m.atMenu) != 1 || m.atMenu[0] != "notes.txt" {
		t.Fatalf("filter = %v, want notes.txt only", m.atMenu)
	}
	m.Update(enterKey())
	if v := m.composer.Value(); !strings.HasSuffix(v, "@notes.txt ") {
		t.Errorf("completion = %q, want trailing @notes.txt", v)
	}
	if m.atMenuOpen() {
		t.Error("menu must close on completion")
	}

	// Esc dismisses until the mention changes.
	m.composer.SetValue("see @")
	m.refreshAtMenu()
	if !m.atMenuOpen() {
		t.Fatal("menu should reopen for a new mention")
	}
	m.Update(escKey())
	if m.atMenuOpen() {
		t.Error("esc must close the menu")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")}) // same mention, more text
	if m.atMenuOpen() {
		t.Error("a dismissed mention must stay closed while its query grows")
	}
	m.composer.SetValue("another @")
	m.refreshAtMenu()
	if !m.atMenuOpen() {
		t.Error("a fresh mention opens the menu again")
	}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func TestShellModeAmberIndication(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)

	// Typing "!" flips the composer into its shell-escape look before
	// Enter — the indication the user asked for, visible up front.
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("! echo hi")})
	if !m.shellMode() {
		t.Fatal("leading ! must be shell mode")
	}
	if m.composer.Prompt != GlyphWarn+" " {
		t.Errorf("prompt = %q, want the amber ! glyph", m.composer.Prompt)
	}
	view := m.View()
	if !strings.Contains(stripANSI(view), "shell — enter runs it directly") {
		t.Errorf("shell hint missing from the mode line:\n%s", view)
	}
	// Deleting the "!" returns the normal prompt.
	m.composer.SetValue("echo hi")
	m.syncComposerPrompt()
	if m.composer.Prompt != GlyphPrompt+" " {
		t.Errorf("prompt = %q, want ~ restored", m.composer.Prompt)
	}
}

func TestWorkingLineGerundAndUserPanel(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)

	// The working line reads like the demo: gerund + elapsed + tokens
	// with the down arrow, and the esc hint.
	m.working = true
	m.workingVerb = "Pondering…"
	m.workingSince = time.Now()
	var status string
	for _, l := range m.composerView() {
		if strings.Contains(l, "Pondering") {
			status = stripANSI(l)
		}
	}
	if status == "" {
		t.Fatal("working line missing")
	}
	for _, want := range []string{"Pondering…", "esc to interrupt", "↓ "} {
		if !strings.Contains(status, want) {
			t.Errorf("working line missing %q: %q", want, status)
		}
	}

	// The echoed query renders in a background panel.
	defer func(p termenv.Profile) { lipgloss.SetColorProfile(p) }(lipgloss.ColorProfile())
	lipgloss.SetColorProfile(termenv.TrueColor)
	panel := m.renderEntry(&entry{kind: entryUser, text: "hello"})
	joined := strings.Join(panel, "\n")
	if !strings.Contains(joined, "48;2;6;34;43") { // HexDeep2 #06222B
		t.Errorf("user entry lacks the background panel:\n%q", joined)
	}
	if !strings.Contains(stripANSI(joined), "hello") {
		t.Errorf("content lost in the panel:\n%s", joined)
	}
}

func TestPlanApprovalLifecycle(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)
	m.opt.Mode = tools.ModePlan
	m.opt.Orch.SetMode(tools.ModePlan)

	// The model presents a plan; the prompt owns the keyboard.
	reply := make(chan planVerdict, 1)
	m.Update(planRequestMsg{req: &planRequest{plan: "## Goal\nship it", reply: reply}})
	if m.awaitingPlan == nil {
		t.Fatal("plan prompt not shown")
	}
	if tr := m.transcript(); !strings.Contains(tr, "plan") || !strings.Contains(tr, "ship it") {
		t.Errorf("plan not rendered in the transcript:\n%s", tr)
	}
	if !strings.Contains(stripANSI(m.View()), "proceed with this plan?") {
		t.Errorf("approval prompt missing from the view")
	}

	// y: proceed — plan graduates to ask-every-time.
	m.Update(keyMsg("y"))
	v := <-reply
	if !v.proceed || v.auto {
		t.Errorf("y verdict = %+v, want proceed without auto", v)
	}
	if m.opt.Mode != tools.ModeAskEveryTime {
		t.Errorf("y must switch plan -> ask-every-time, got %q", m.opt.Mode)
	}
	if tr := m.transcript(); !strings.Contains(tr, "plan approved") {
		t.Errorf("approval note missing:\n%s", tr)
	}

	// a: proceed with auto-accept — full-auto.
	m.opt.Mode = tools.ModePlan
	m.opt.Orch.SetMode(tools.ModePlan)
	reply2 := make(chan planVerdict, 1)
	m.Update(planRequestMsg{req: &planRequest{plan: "again", reply: reply2}})
	m.Update(keyMsg("a"))
	v2 := <-reply2
	if !v2.proceed || !v2.auto {
		t.Errorf("a verdict = %+v, want proceed with auto", v2)
	}
	if m.opt.Mode != tools.ModeFullAuto {
		t.Errorf("a must switch to full-auto, got %q", m.opt.Mode)
	}

	// n: keep planning — mode unchanged, verdict declines.
	m.opt.Mode = tools.ModePlan
	m.opt.Orch.SetMode(tools.ModePlan)
	reply3 := make(chan planVerdict, 1)
	m.Update(planRequestMsg{req: &planRequest{plan: "third", reply: reply3}})
	m.Update(keyMsg("n"))
	if v3 := <-reply3; v3.proceed {
		t.Errorf("n verdict = %+v, want decline", v3)
	}
	if m.opt.Mode != tools.ModePlan {
		t.Errorf("n must keep plan mode, got %q", m.opt.Mode)
	}
	if tr := m.transcript(); !strings.Contains(tr, "plan declined") {
		t.Errorf("decline note missing:\n%s", tr)
	}
}

func TestStreamRendersAsMarkdownWithNoFlushPop(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)
	m.width = 80

	// Deltas accumulate; the in-flight view renders markdown (heading
	// styled, not plain).
	m.handleEvent(orchestrator.Event{Kind: orchestrator.EventText, Text: "## Done\n\n- one\n"})
	lines := m.timelineView()
	joined := stripANSI(strings.Join(lines, "\n"))
	if !strings.Contains(joined, "Done") || !strings.Contains(joined, "one") {
		t.Fatalf("stream not rendered:\n%s", joined)
	}
	streamed := strings.Join(m.renderStream(strings.TrimSpace(m.stream.String()), m.termWidth()), "\n")

	// Flushing must produce a byte-identical render — the guarantee
	// that a completed message never reflows.
	m.finishStream()
	if len(m.entries) == 0 {
		t.Fatal("flush produced no entry")
	}
	flushed := strings.Join(m.renderEntry(&m.entries[len(m.entries)-1]), "\n")
	if flushed != streamed {
		t.Errorf("flush reflowed the message:\nstream:\n%q\nflushed:\n%q", streamed, flushed)
	}

	// The cache invalidates on growth and width change.
	m.handleEvent(orchestrator.Event{Kind: orchestrator.EventText, Text: "more"})
	_ = m.timelineView() // the cache populates on render, not on the delta
	if m.streamRenderedLen == 0 {
		t.Error("stream cache not populated")
	}
	m.width = 60
	_ = m.timelineView()
	if m.streamRenderedW != 60 {
		t.Errorf("width change not picked up: %d", m.streamRenderedW)
	}
}

func TestReducedMotion(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)
	m.opt.Animations = false

	// The toast appears but does not animate: no frames, no tick cmd.
	cmd := m.setMode(tools.ModeFullAuto)
	if cmd != nil {
		t.Error("reduced motion must not schedule animation ticks")
	}
	if m.toastAnim != 0 {
		t.Errorf("toastAnim = %d, want 0", m.toastAnim)
	}
	if m.toast == "" {
		t.Error("the toast itself must still show")
	}

	// The working line shows a static glyph, not spinner frames.
	m.working = true
	m.workingVerb = "Thinking…"
	m.workingSince = time.Now()
	var status string
	for _, l := range m.composerView() {
		if strings.Contains(stripANSI(l), "Thinking…") {
			status = l
		}
	}
	if status == "" {
		t.Fatal("working line missing")
	}
	for _, frame := range []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"} {
		if strings.Contains(status, frame) {
			t.Errorf("reduced motion rendered spinner frame %q", frame)
		}
	}
}

func TestLightThemeAdaptation(t *testing.T) {
	// The dark palette is the default; a light terminal re-skins it.
	// Restore afterward: these are package-wide tokens.
	defer func() {
		// The dark branch of adaptTheme is a no-op by design, so restore
		// the dark values explicitly.
		HexAccent, HexAccent2 = "#22D3EE", "#0891B2"
		HexDeep, HexDeep2 = "#0B3A47", "#06222B"
		HexText, HexDim = "#E6F2F5", "#7A8B94"
		HexDanger, HexWarning, HexInfo = "#F87171", "#FBBF24", "#93C5FD"
		refreshTokens()
	}()

	adaptTheme(false)

	if HexAccent != "#0E7490" || HexText != "#1B2A32" || HexDeep2 != "#E4EDF1" {
		t.Errorf("light palette not applied: accent=%s text=%s fill=%s",
			HexAccent, HexText, HexDeep2)
	}

	defer func(p termenv.Profile) { lipgloss.SetColorProfile(p) }(lipgloss.ColorProfile())
	lipgloss.SetColorProfile(termenv.TrueColor)

	// The user panel renders with the LIGHT fill, and the body text is
	// dark ink — the legibility failure class this exists to prevent.
	panel := strings.Join(m_renderUserPanel("hello"), "\n")
	if !strings.Contains(panel, "48;2;227;237;241") { // #E4EDF1 (termenv rounds one step)
		t.Errorf("user panel lacks the light fill:\n%q", panel)
	}
	// The composer's accent prompt uses the deepened light accent.
	prompt := accentStyle.Render("~ ")
	if !strings.Contains(prompt, "38;2;14;116;144") { // #0E7490
		t.Errorf("accent not deepened for light background: %q", prompt)
	}
}

func m_renderUserPanel(text string) []string {
	m := &Model{width: 80}
	return m.renderEntry(&entry{kind: entryUser, text: text})
}

func TestFooterFitsNarrowTerminals(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)

	// Wide terminal: the full mode line with both hints.
	m.width = 80
	view := m.View()
	for _, want := range []string{"(tab to cycle)", "? help · / commands"} {
		if !strings.Contains(stripANSI(view), want) {
			t.Errorf("wide footer missing %q:\n%s", want, view)
		}
	}

	// Narrow terminal: the footer degrades instead of wrapping —
	// the mode always survives, hints drop off first.
	m.width = 24
	footer := ""
	for _, l := range m.composerView() {
		p := stripANSI(l)
		if strings.Contains(p, m.opt.Mode) && !strings.Contains(p, "ask tilde") {
			footer = p
		}
	}
	if footer == "" {
		t.Fatal("mode line missing entirely on narrow width")
	}
	if w := len([]rune(footer)); w > 24 {
		t.Errorf("footer is %d cols on a 24-col terminal: %q", w, footer)
	}
	if !strings.Contains(footer, "ask-every-time") {
		t.Errorf("the mode must survive degradation: %q", footer)
	}
}

func TestExternalEditorFlow(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)

	// No editor configured: a toast, no process.
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "")
	if cmd := m.openEditor(); cmd != nil || m.toast == "" {
		t.Errorf("no $VISUAL/$EDITOR must toast, not exec: cmd=%v toast=%q", cmd, m.toast)
	}

	// The editor's result lands back in the composer; the temp file
	// is cleaned up.
	t.Setenv("EDITOR", "whatever")
	f, err := os.CreateTemp(t.TempDir(), "edit-*.md")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("edited in $EDITOR\nsecond line"); err != nil {
		t.Fatal(err)
	}
	f.Close()
	m.Update(editorDoneMsg{path: f.Name()})
	if v := m.composer.Value(); v != "edited in $EDITOR\nsecond line" {
		t.Errorf("composer = %q, want the editor's text", v)
	}
	if _, err := os.Stat(f.Name()); !os.IsNotExist(err) {
		t.Error("temp editor file must be removed after loading")
	}
	if h := m.composer.Height(); h != 2 {
		t.Errorf("composer height after load = %d, want 2", h)
	}

	// ctrl+e while a turn runs is a no-op — suspending mid-turn would
	// strand the orchestrator.
	m.working = true
	m.showToast("")
	if cmd := m.openEditor(); cmd != nil {
		t.Error("ctrl+e must not open an editor mid-turn")
	}
}

func TestTodosPanelRenders(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)

	m.Update(todoMsg([]tools.Todo{
		{Content: "read the config", Status: tools.TodoDone},
		{Content: "fix the auth bug", Status: tools.TodoInProgress},
		{Content: "write tests", Status: tools.TodoPending},
	}))
	view := stripANSI(m.View())
	for _, want := range []string{"tasks (1/3 done)", "✓ read the config", "▸ fix the auth bug", "· write tests"} {
		if !strings.Contains(view, want) {
			t.Errorf("panel missing %q:\n%s", want, view)
		}
	}

	// The window: a long list shows the live tail, not the first rows.
	var items []tools.Todo
	for i := 0; i < 12; i++ {
		items = append(items, tools.Todo{Content: "task", Status: tools.TodoDone})
	}
	items[11].Status = tools.TodoInProgress
	m.Update(todoMsg(items))
	view = stripANSI(m.View())
	if !strings.Contains(view, "4 earlier tasks") {
		t.Errorf("window note missing:\n%s", view)
	}
	if strings.Count(view, "✓ task") > 8 {
		t.Error("window must cap the rendered rows")
	}
}
