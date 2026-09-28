package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Chmgx81/tilde/internal/config"
	"github.com/Chmgx81/tilde/internal/llm"
	"github.com/Chmgx81/tilde/internal/orchestrator"
	"github.com/Chmgx81/tilde/internal/tools"
)

// Update implements tea.Model.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
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
			return m, nil
		}
		return m.handleKey(msg)
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

	switch msg.String() {
	case "ctrl+c":
		m.cancelTurn()
		return m, tea.Quit
	case "esc":
		if m.working {
			m.cancelTurn()
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
		if m.login != nil {
			return m, m.submitLogin()
		}
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
		m.add(entry{kind: entryDim, text: fmt.Sprintf("model %s · provider %s · base %s",
			m.opt.Model, m.opt.ProviderName, m.opt.BaseURL)})
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

// finishStream flushes the accumulated assistant text into the
// transcript.
func (m *Model) finishStream() {
	if s := strings.TrimRight(m.stream.String(), "\n"); strings.TrimSpace(s) != "" {
		m.add(entry{kind: entryAssistant, text: s})
	}
	m.stream.Reset()
}
