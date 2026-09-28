package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Chmgx81/tilde/internal/config"
	"github.com/Chmgx81/tilde/internal/llm"
	"github.com/Chmgx81/tilde/internal/orchestrator"
	"github.com/Chmgx81/tilde/internal/session"
	"github.com/Chmgx81/tilde/internal/tools"
)

// Update implements tea.Model.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		// The composer wraps at its own width, not the terminal's —
		// keep it sized to the box: terminal minus border+padding.
		m.composer.SetWidth(maxInt(msg.Width-8, 10))
		m.resizeComposer()
		return m, nil

	case spinner.TickMsg:
		if m.working {
			sp, cmd := m.spinner.Update(msg)
			m.spinner = sp
			return m, cmd
		}
		return m, nil

	case statusTickMsg:
		if m.working {
			return m, statusTick()
		}
		return m, nil

	case permRequestMsg:
		m.awaitingPerm = msg.req
		return m, nil

	case orchestratorMsg:
		return m.handleEvent(orchestrator.Event(msg)), nil

	case subagentMsg:
		m.handleSubagent(subagentEvent(msg))
		return m, nil

	case tea.KeyMsg:
		if msg.Paste {
			m.handlePaste(string(msg.Runes))
			m.resizeComposer()
			return m, nil
		}
		model, cmd := m.handleKey(msg)
		m.resizeComposer()
		return model, cmd
	}
	return m, nil
}

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// The trust prompt owns the keyboard first.
	if m.awaitingTrust != nil {
		switch msg.String() {
		case "y", "Y":
			m.answerTrust(true)
		case "n", "N", "esc":
			m.answerTrust(false)
		}
		return m, nil
	}
	// Then the permission prompt.
	if m.awaitingPerm != nil {
		switch msg.String() {
		case "y", "Y":
			m.awaitingPerm.reply <- true
		case "a", "A":
			m.allowAll = true
			m.awaitingPerm.reply <- true
		case "n", "N", "esc":
			m.awaitingPerm.reply <- false
		default:
			return m, nil
		}
		m.awaitingPerm = nil
		return m, nil
	}

	// The help overlay closes on any key.
	if m.helpOpen {
		m.helpOpen = false
		return m, nil
	}

	// The overlay picker owns the keyboard while open; typing filters
	// instead of reaching the composer.
	if m.picker != nil {
		k := keyInput{name: msg.String()}
		if msg.Type == tea.KeyRunes || msg.Type == tea.KeySpace {
			k.printable = string(msg.Runes)
		}
		if sel, handled := m.handlePickerKey(k); handled {
			if sel != nil {
				m.pickerSelect(*sel)
			}
			return m, nil
		}
	}

	// The login prompt: Enter saves, Esc cancels and discards the
	// typed key. Everything else falls through to the composer (View
	// renders it masked).
	if m.login != nil {
		switch msg.String() {
		case "enter":
			return m, m.submitLogin()
		case "esc":
			m.login = nil
			m.composer.SetValue("")
			m.resizeComposer()
			m.add(entry{kind: entryDim, text: "login cancelled"})
			return m, nil
		}
	}

	switch msg.String() {
	case "ctrl+c":
		m.cancelTurn()
		return m, tea.Quit
	case "esc":
		if m.working {
			m.cancelTurn()
			return m, nil
		}
		// With no turn in flight, esc closes the command palette
		// instead of leaving a half-typed command in the composer.
		if m.paletteOpen() {
			m.composer.SetValue("")
			m.resizeComposer()
			m.showToast("palette closed")
		}
		return m, nil
	case "shift+tab":
		m.cycleMode()
		return m, nil
	case "ctrl+r":
		m.expandResults = !m.expandResults
		if m.expandResults {
			m.showToast("tool results expanded")
		} else {
			m.showToast("tool results collapsed")
		}
		return m, nil
	case "?":
		if !m.working && m.composer.Value() == "" {
			m.helpOpen = true
			return m, nil
		}
	case "enter":
		return m, m.submitInput(false)
	case "alt+enter":
		return m, m.submitInput(true)
	case "shift+enter", "ctrl+j":
		// Newline in the composer.
		m.composer.InsertString("\n")
		return m, nil
	case "up", "down":
		if m.paletteOpen() {
			matches := m.paletteMatches()
			if len(matches) > 0 {
				if msg.String() == "up" {
					m.paletteIdx--
				} else {
					m.paletteIdx++
				}
				m.paletteIdx = ((m.paletteIdx % len(matches)) + len(matches)) % len(matches)
			}
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.composer, cmd = m.composer.Update(msg)
	return m, cmd
}

// resizeComposer grows the input with its content, one visible line
// per typed or wrapped line, capped so a huge paste can't eat the
// transcript: typing never needs more than a screen's worth of editor.
func (m *Model) resizeComposer() {
	v := m.composer.Value()
	h := strings.Count(v, "\n") + 1
	if w := m.composer.Width(); w > 0 {
		for _, l := range strings.Split(v, "\n") {
			if n := lipgloss.Width(l); n > w {
				h += n / w
			}
		}
	}
	if h > 6 {
		h = 6
	}
	if h != m.composer.Height() {
		m.composer.SetHeight(h)
	}
}

// handlePaste stores a large paste and collapses it to a token in the
// composer; small pastes insert inline. Tokens re-expand on submit.
func (m *Model) handlePaste(text string) {
	const minLines, minChars = 4, 1000
	lines := strings.Count(text, "\n") + 1
	if lines < minLines && len(text) < minChars {
		m.composer.InsertString(text)
		return
	}
	m.pastes = append(m.pastes, text)
	token := fmt.Sprintf("[paste %d · %d lines]", len(m.pastes), lines)
	m.pasteAt[token] = text
	m.composer.InsertString(token)
	m.showToast(fmt.Sprintf("pasted %d lines collapsed to a token", lines))
}

// expandPastes replaces paste tokens with their stored content, and
// @path mentions with the file's contents (capped).
func (m *Model) expandPastes(text string) string {
	for token, content := range m.pasteAt {
		if strings.Contains(text, token) {
			text = strings.ReplaceAll(text, token, "\n"+content+"\n")
		}
	}
	// @file mentions: read the file, cap the contribution.
	fields := strings.Fields(text)
	for _, f := range fields {
		if !strings.HasPrefix(f, "@") || len(f) < 2 {
			continue
		}
		path := strings.TrimPrefix(f, "@")
		data, err := os.ReadFile(path)
		if err != nil {
			text = strings.Replace(text, f, f+" (unreadable: "+err.Error()+")", 1)
			continue
		}
		content := string(data)
		if len(content) > 2048 {
			content = content[:2048] + "\n… (truncated)"
		}
		text = strings.Replace(text, f,
			fmt.Sprintf("file %s contents:\n%s\n(end of %s)", path, content, path), 1)
	}
	return text
}

// cycleMode moves to the next permission mode (shift+tab).
func (m *Model) cycleMode() {
	for i, mode := range tools.Modes {
		if mode == m.opt.Mode {
			next := tools.Modes[(i+1)%len(tools.Modes)]
			m.setMode(next)
			return
		}
	}
	m.setMode(tools.ModeAskEveryTime)
}

// submitInput handles Enter / Alt+Enter. Large pastes and @ mentions
// expand here; "!" runs a shell command directly without the model.
func (m *Model) submitInput(alt bool) tea.Cmd {
	raw := strings.TrimSpace(m.composer.Value())
	if raw == "" {
		return nil
	}
	// Resolve the palette selection BEFORE clearing the composer: the
	// palette reads the composer's value, and clearing first would
	// turn "/mo" into an unknown command.
	if strings.HasPrefix(raw, "/") {
		fields := strings.Fields(raw)
		if len(fields) == 1 && m.paletteOpen() {
			matches := m.paletteMatches()
			if len(matches) > 0 {
				raw = matches[m.paletteIdx].Name
			}
		}
	}
	m.composer.SetValue("")
	defer func() { m.pastes, m.pasteAt = nil, map[string]string{} }()

	// Shell escape: the user typed it, not the model.
	if strings.HasPrefix(raw, "!") && len(raw) > 1 {
		cmd := strings.TrimSpace(strings.TrimPrefix(raw, "!"))
		m.add(entry{kind: entryUser, text: dimStyle.Render("! " + cmd)})
		out, err := (tools.RunShell{}).Execute(context.Background(), `{"command": `+mustJSON(cmd)+`}`)
		if err != nil {
			m.add(entry{kind: entryErr, text: out + " " + err.Error()})
		} else {
			m.add(entry{kind: entryDim, text: out})
		}
		return nil
	}

	text := m.expandPastes(raw)

	fields := strings.Fields(text)
	cmdName, arg := "", ""
	if len(fields) > 0 && strings.HasPrefix(fields[0], "/") {
		cmdName = fields[0]
		if len(fields) > 1 {
			arg = fields[1]
		}
	}
	switch cmdName {
	case "/exit", "/quit":
		m.cancelTurn()
		return tea.Quit
	case "/help":
		m.helpOpen = true
		return nil
	case "/login":
		m.beginLogin()
		return nil
	case "/logout":
		m.logout()
		return nil
	case "/mode":
		m.setMode(arg)
		return nil
	case "/model":
		m.handleModelCommand(arg)
		return nil
	case "/sessions":
		m.openSessionsPicker()
		return nil
	case "/skills":
		m.listSkills()
		return nil
	case "/mcp":
		m.listMcp()
		return nil
	}

	if m.working {
		if alt {
			m.queue = append(m.queue, text)
			m.add(entry{kind: entryQueued, text: text})
			return nil
		}
		m.opt.Orch.Steer(text)
		m.add(entry{kind: entrySteer, text: text})
		return nil
	}

	m.add(entry{kind: entryUser, text: text})
	m.startTurn(text)
	return tea.Batch(m.spinner.Tick, statusTick())
}

func mustJSON(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func (m *Model) listSkills() {
	if m.opt.Skills == nil {
		m.add(entry{kind: entryDim, text: "no skills loaded"})
		return
	}
	names := m.opt.Skills.Names()
	if len(names) == 0 {
		m.add(entry{kind: entryDim, text: "no skills loaded"})
		return
	}
	var rows []string
	for _, name := range names {
		s, _ := m.opt.Skills.Get(name)
		desc := ""
		if s != nil {
			desc = s.Description
		}
		rows = append(rows, accentStyle.Render(GlyphCaret+" ")+name+dimStyle.Render("  "+desc))
	}
	m.add(entry{kind: entryDim, text: strings.Join(rows, "\n")})
}

func (m *Model) listMcp() {
	if m.opt.MCPNames == nil {
		m.add(entry{kind: entryDim, text: "no MCP manager wired"})
		return
	}
	names := m.opt.MCPNames()
	if len(names) == 0 {
		m.add(entry{kind: entryDim, text: "no MCP servers connected"})
		return
	}
	m.add(entry{kind: entryDim, text: strings.Join(names, "\n")})
}

// applyNewKey rebuilds the provider and the audit redactor so /login
// and /logout take effect without a restart.
func (m *Model) applyNewKey(provider, key string) {
	m.opt.Orch.Provider = llm.NewOpenAICompat(m.opt.BaseURL, key)
	m.opt.Orch.Gate = &tools.Gate{
		Decide: tools.PolicyDecide(m.opt.Mode, m.Prompt()),
		Audit:  tools.NewAuditLog(m.opt.AuditPath, tools.NewRedactor(key)),
	}
	_ = provider
}

func (m *Model) beginLogin() {
	m.login = &loginFlow{provider: m.opt.ProviderName}
	m.composer.SetValue("")
}

func (m *Model) submitLogin() tea.Cmd {
	key := strings.TrimSpace(m.composer.Value())
	provider := m.login.provider
	m.login = nil
	m.composer.SetValue("")

	if key == "" {
		m.add(entry{kind: entryErr, text: "login cancelled: no key entered"})
		return nil
	}
	if err := config.WriteAuthKey(m.opt.TildeHome, provider, key); err != nil {
		m.add(entry{kind: entryErr, text: "login failed: " + err.Error()})
		return nil
	}
	m.applyNewKey(provider, key)
	m.add(entry{kind: entryOK, text: "key for " + provider + " stored in auth.json (0600) — future requests use it"})
	return nil
}

func (m *Model) logout() {
	removed, err := config.RemoveAuthKey(m.opt.TildeHome, m.opt.ProviderName)
	if err != nil {
		m.add(entry{kind: entryErr, text: "logout failed: " + err.Error()})
		return
	}
	if !removed {
		m.add(entry{kind: entryOK, text: "no stored key for " + m.opt.ProviderName + " — nothing to remove"})
		return
	}
	m.applyNewKey(m.opt.ProviderName, "")
	m.add(entry{kind: entryOK,
		text: "removed the stored key for " + m.opt.ProviderName +
			". This does not unset environment variables or revoke the key at the provider."})
}

// setMode implements /mode and shift+tab: no argument shows the mode;
// a valid name switches and resets any session allow-all grant.
func (m *Model) setMode(arg string) {
	if arg == "" {
		m.add(entry{kind: entryDim,
			text: "mode is " + m.opt.Mode + " — options: " + strings.Join(tools.Modes, ", ") + "; /mode <name> or shift+tab"})
		return
	}
	mode := tools.NormalizeMode(arg)
	if !tools.ValidMode(mode) {
		m.add(entry{kind: entryErr, text: "unknown mode " + arg + " — options: " + strings.Join(tools.Modes, ", ")})
		return
	}
	if mode == m.opt.Mode {
		m.showToast("mode is already " + mode)
		return
	}
	m.opt.Orch.SetMode(mode)
	m.rebuildGate(mode)
	m.opt.Mode = mode
	m.allowAll = false
	note := "mode switched to " + mode
	if m.working {
		note += " (takes effect for the next model request)"
	}
	m.showToast(note)
	m.add(entry{kind: entryOK, text: note})
}

func (m *Model) rebuildGate(mode string) {
	m.opt.Orch.Gate.Decide = tools.PolicyDecide(mode, m.Prompt())
}

func (m *Model) answerTrust(trusted bool) {
	d := m.awaitingTrust
	m.awaitingTrust = nil
	if d == nil || d.OnAnswer == nil {
		return
	}
	d.OnAnswer(trusted)
	if trusted {
		m.add(entry{kind: entryOK, text: "project trusted — project-level skills are available this session"})
	} else {
		m.add(entry{kind: entryErr, text: "project not trusted — running with user-level skills only"})
	}
}

// handleEvent maps orchestrator events to timeline entries.
func (m *Model) handleEvent(ev orchestrator.Event) tea.Model {
	switch ev.Kind {
	case orchestrator.EventText:
		m.stream.WriteString(ev.Text)

	case orchestrator.EventToolStart:
		m.finishStream()
		m.add(entry{kind: entryTool, tool: ev.ToolCall.Name, text: ev.ToolCall.Arguments})

	case orchestrator.EventToolResult:
		m.addResultEntry(ev)

	case orchestrator.EventUsage:
		m.usage.PromptTokens += ev.Usage.PromptTokens
		m.usage.CompletionTokens += ev.Usage.CompletionTokens

	case orchestrator.EventCompaction:
		m.finishStream()
		m.add(entry{kind: entryCompaction, text: ev.Text})

	case orchestrator.EventTurnComplete:
		m.finishStream()
		m.turnEnded()

	case orchestrator.EventError:
		m.finishStream()
		if ev.Err == nil {
			m.turnEnded()
			return m
		}
		if ev.Err == orchestrator.ErrCancelled {
			// Interrupt means stop: queued follow-ups must not fire
			// the moment the user pressed esc. Clear them with a note
			// instead of draining.
			if n := len(m.queue); n > 0 {
				m.queue = nil
				m.add(entry{kind: entryDim,
					text: fmt.Sprintf("cleared %d queued follow-up%s", n, plural(n))})
			}
			m.add(entry{kind: entryErr, text: "turn interrupted"})
		} else {
			m.add(entry{kind: entryErr, text: "error: " + ev.Err.Error()})
		}
		m.turnEnded()
	}
	return m
}

// addResultEntry stores a tool result with enough structure to render
// collapsed, expanded, or as an edit_file diff.
func (m *Model) addResultEntry(ev orchestrator.Event) {
	res := strings.ReplaceAll(strings.TrimSpace(ev.ToolResult), "\n", " ⏎ ")
	e := entry{kind: entryResult, tool: ev.ToolCall.Name, summary: res, full: ev.ToolResult}
	if ev.ToolCall.Name == "edit_file" {
		var a struct {
			Path string `json:"path"`
			Old  string `json:"old"`
			New  string `json:"new"`
		}
		if err := json.Unmarshal([]byte(ev.ToolCall.Arguments), &a); err == nil {
			e.path, e.old, e.new = a.Path, a.Old, a.New
		}
	}
	m.add(e)
}

func (m *Model) turnEnded() {
	m.working = false
	m.cancel = nil
	if len(m.queue) == 0 {
		return
	}
	next := m.queue[0]
	m.queue = m.queue[1:]
	m.add(entry{kind: entryUser, text: dimStyle.Render("(follow-up) ") + next})
	m.startTurn(next)
}

// handleSubagent renders subagent progress as labeled timeline entries.
func (m *Model) handleSubagent(ev subagentEvent) {
	label := "[" + ev.Title + "] "
	m.subMu.Lock()
	if m.subStreams == nil {
		m.subStreams = map[string]*strings.Builder{}
	}
	acc, ok := m.subStreams[ev.Title]
	if !ok {
		acc = &strings.Builder{}
		m.subStreams[ev.Title] = acc
	}
	flush := func() {
		if s := strings.TrimSpace(acc.String()); s != "" {
			m.add(entry{kind: entrySubagent, subTitle: label, text: s})
		}
		acc.Reset()
	}
	switch ev.Kind {
	case subagentText:
		acc.WriteString(ev.Text)
	case subagentTool:
		flush()
		m.add(entry{kind: entrySubagent, subTitle: label, text: ev.Text})
	case subagentUsage:
		m.usage.PromptTokens += ev.Usage.PromptTokens
		m.usage.CompletionTokens += ev.Usage.CompletionTokens
	case subagentDone:
		if ev.Text != "" {
			acc.Reset()
			acc.WriteString(ev.Text)
		}
		flush()
		m.add(entry{kind: entrySubagent, subTitle: label, text: dimStyle.Render("done")})
	case subagentError:
		flush()
		m.add(entry{kind: entrySubagent, subTitle: label, text: dangerStyle.Render("error: " + ev.Text)})
	}
	m.subMu.Unlock()
}

// handleModelCommand implements /model: no argument opens the picker
// over models.json; a name switches immediately if it is configured.
func (m *Model) handleModelCommand(arg string) {
	if arg == "" {
		m.openModelPicker()
		return
	}
	for provider, pc := range m.opt.Models.Providers {
		for _, model := range pc.Models {
			if model == arg {
				m.switchModel(provider, model)
				return
			}
		}
	}
	m.add(entry{kind: entryErr,
		text: "unknown model " + arg + " — /model to pick one, or add it to models.json"})
}

// openModelPicker lists every configured provider and model, marking
// the active one.
func (m *Model) openModelPicker() {
	if m.working {
		m.add(entry{kind: entryErr, text: "finish or interrupt the turn before switching models"})
		return
	}
	if m.opt.SwitchModel == nil {
		m.add(entry{kind: entryErr, text: "model switching is not wired in this build"})
		return
	}
	names := make([]string, 0, len(m.opt.Models.Providers))
	for name := range m.opt.Models.Providers {
		names = append(names, name)
	}
	sort.Strings(names)
	var items []pickerItem
	for _, name := range names {
		pc := m.opt.Models.Providers[name]
		if len(pc.Models) == 0 {
			items = append(items, pickerItem{
				Label:    name,
				Detail:   "no models listed — switches provider, keeps the current model",
				Provider: name,
			})
			continue
		}
		for _, model := range pc.Models {
			detail := name + " · " + pc.BaseURL
			if name == m.opt.ProviderName && model == m.opt.Model {
				detail += " · active"
			}
			items = append(items, pickerItem{Label: model, Detail: detail, Provider: name, Model: model})
		}
	}
	if len(items) == 0 {
		m.add(entry{kind: entryDim, text: "no providers or models configured in models.json"})
		return
	}
	m.picker = newPicker(pickerModels, "switch model", items)
}

// switchModel rebuilds the provider and model everywhere through the
// injected callback, then updates the status bar state it owns.
func (m *Model) switchModel(provider, model string) {
	if m.working {
		m.add(entry{kind: entryErr, text: "finish or interrupt the turn before switching models"})
		return
	}
	if m.opt.SwitchModel == nil {
		m.add(entry{kind: entryErr, text: "model switching is not wired in this build"})
		return
	}
	if err := m.opt.SwitchModel(provider, model); err != nil {
		m.add(entry{kind: entryErr, text: "could not switch: " + err.Error()})
		return
	}
	if model != "" {
		m.opt.Model = model
	}
	if provider != "" {
		m.opt.ProviderName = provider
	}
	if pc, ok := m.opt.Models.Providers[provider]; ok && pc.BaseURL != "" {
		m.opt.BaseURL = pc.BaseURL
	}
	note := "switched to " + m.opt.Model + " (provider " + m.opt.ProviderName + ") — the next request uses it"
	m.showToast("model switched to " + m.opt.Model)
	m.add(entry{kind: entryOK, text: note})
}

// openSessionsPicker lists saved sessions newest-first.
func (m *Model) openSessionsPicker() {
	if m.working {
		m.add(entry{kind: entryErr, text: "finish or interrupt the turn before resuming a session"})
		return
	}
	if m.opt.ResumeSession == nil {
		m.add(entry{kind: entryErr, text: "session resume is not wired in this build"})
		return
	}
	items, err := sessionItems(m.opt.TildeHome)
	if err != nil {
		m.add(entry{kind: entryErr, text: "could not read sessions: " + err.Error()})
		return
	}
	if len(items) == 0 {
		m.add(entry{kind: entryDim, text: "no saved sessions in " + session.Dir(m.opt.TildeHome)})
		return
	}
	m.picker = newPicker(pickerSessions, "resume session", items)
}

// sessionItems reads the sessions directory, newest first. A corrupt
// file is skipped, not fatal: history browsing must not break on one
// bad write.
func sessionItems(homeDir string) ([]pickerItem, error) {
	dir := session.Dir(homeDir)
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			names = append(names, e.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	items := make([]pickerItem, 0, len(names))
	for _, name := range names {
		path := filepath.Join(dir, name)
		s, err := session.Load(path)
		if err != nil {
			continue
		}
		detail := fmt.Sprintf("%d messages · %s", len(s.History()), s.Model)
		if preview := firstUserText(s); preview != "" {
			detail += " · " + preview
		}
		items = append(items, pickerItem{Label: name, Detail: detail, Path: path})
	}
	return items, nil
}

func firstUserText(s *session.Session) string {
	for _, msg := range s.History() {
		if msg.Role != "user" || strings.TrimSpace(msg.Content) == "" {
			continue
		}
		preview := strings.Join(strings.Fields(msg.Content), " ")
		return truncate(preview, 48)
	}
	return ""
}

// pickerSelect runs the command-specific action for a selection.
func (m *Model) pickerSelect(it pickerItem) {
	if it.Path != "" {
		m.resumeSession(it.Path, it.Label)
		return
	}
	m.switchModel(it.Provider, it.Model)
}

// resumeSession saves the current conversation, then re-seeds the
// orchestrator from the chosen session. The transcript resets: the
// conversation context moved wholesale into the orchestrator.
func (m *Model) resumeSession(path, label string) {
	if m.opt.SaveCurrentSession != nil {
		m.opt.SaveCurrentSession()
	}
	n, err := m.opt.ResumeSession(path)
	if err != nil {
		m.add(entry{kind: entryErr, text: "could not resume " + label + ": " + err.Error()})
		return
	}
	m.entries = nil
	m.stream.Reset()
	m.queue = nil
	m.usage = llm.Usage{}
	m.entries = append(m.entries, entry{kind: entryDim, text: boldStyle.Render("tilde " + version)})
	m.add(entry{kind: entryDim, text: dimStyle.Render(m.opt.Model + " · " + m.opt.Mode)})
	m.add(entry{kind: entryOK, text: fmt.Sprintf("resumed %s — %d messages in context", label, n)})
	m.showToast("resumed " + label)
}

// finishStream flushes the accumulated assistant text into the
// transcript.
func (m *Model) finishStream() {
	if s := strings.TrimRight(m.stream.String(), "\n"); strings.TrimSpace(s) != "" {
		m.add(entry{kind: entryAssistant, text: s})
	}
	m.stream.Reset()
}
