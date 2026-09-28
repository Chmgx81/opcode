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

func stripANSI(s string) string {
	var b strings.Builder
	inEsc := false
	for _, r := range s {
		if r == 0x1b {
			inEsc = true
			continue
		}
		if inEsc {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEsc = false
			}
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
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

func TestShiftTabCyclesModes(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)

	m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	if m.opt.Mode != tools.ModeAutoAcceptSafe {
		t.Errorf("first cycle = %q, want auto-accept-safe-ops", m.opt.Mode)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	if m.opt.Mode != tools.ModeReadOnly {
		t.Errorf("after three cycles = %q, want read-only", m.opt.Mode)
	}
	if m.toast == "" {
		t.Error("mode toast not set")
	}
	// The gate follows.
	if decide := m.opt.Orch.Gate.Decide; decide == nil || decide(tools.WriteFile{}, `{}`) {
		t.Error("read-only gate must deny after cycling into it")
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
	if !strings.Contains(tr, "+1") || !strings.Contains(tr, "−0") {
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
	if !strings.Contains(view, "? help · / commands · ! shell · @ files") {
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
	const dimSGR = "38;2;91;107;96"     // #5B6B60 in truecolor
	const accentSGR = "38;2;22;219;101" // #16DB65 in truecolor
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
