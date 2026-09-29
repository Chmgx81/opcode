package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

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
		m.composer.SetWidth(maxInt(msg.Width-2, 10))
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

	case toastTickMsg:
		if m.toastAnim > 0 {
			m.toastAnim--
			if m.toastAnim > 0 {
				return m, toastTick()
			}
		}
		return m, nil

	case permRequestMsg:
		m.awaitingPerm = msg.req
		return m, nil

	case planRequestMsg:
		m.awaitingPlan = msg.req
		// The plan lands in the transcript rendered as markdown; the
		// approval prompt below the composer carries the decision.
		m.add(entry{kind: entryPlan, text: msg.req.plan})
		return m, nil

	case editorDoneMsg:
		return m, m.editorFinished(msg)

	case todoMsg:
		m.todos = msg
		return m, nil

	case orchestratorMsg:
		return m.handleEvent(orchestrator.Event(msg))

	case modelsFetchedMsg:
		m.handleModelsFetched(msg)
		return m, nil

	case subagentMsg:
		m.handleSubagent(subagentEvent(msg))
		return m, nil

	case tea.KeyMsg:
		if msg.Paste {
			m.handlePaste(string(msg.Runes))
			m.resizeComposer()
			m.refreshAtMenu()
			m.syncComposerPrompt()
			return m, nil
		}
		model, cmd := m.handleKey(msg)
		m.resizeComposer()
		m.refreshAtMenu()
		m.syncComposerPrompt()
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
	// Then the permission dialog: arrows and number keys move, enter
	// selects, y/a/n are the fast paths, esc denies.
	if m.awaitingPerm != nil {
		req := m.awaitingPerm
		m.awaitingPerm = nil
		switch msg.String() {
		case "y", "Y", "1":
			req.reply <- true
		case "a", "A", "2":
			m.grantAlways(req)
			req.reply <- true
		case "n", "N", "esc", "3":
			req.reply <- false
		case "up":
			req.sel = (req.sel + 2) % 3
			m.awaitingPerm = req
		case "down":
			req.sel = (req.sel + 1) % 3
			m.awaitingPerm = req
		case "enter":
			switch req.sel {
			case 0:
				req.reply <- true
			case 1:
				m.grantAlways(req)
				req.reply <- true
			default:
				req.reply <- false
			}
		default:
			m.awaitingPerm = req
		}
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
				return m, m.pickerSelect(*sel)
			}
			return m, nil
		}
	}

	// The login prompt: Enter saves, Esc cancels and discards the
	// typed key. Tab/shift+tab are swallowed — cycling permission modes
	// while typing a secret is a non sequitur. Everything else falls
	// through to the composer (View renders it masked).
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
		case "tab", "shift+tab":
			return m, nil
		}
	}

	// The @-mention menu owns Enter, arrows, and Esc while it is open;
	// everything else keeps typing into the composer, which live-filters
	// the menu.
	if m.atMenuOpen() {
		switch msg.String() {
		case "enter":
			m.completeAt()
			return m, nil
		case "esc":
			m.dismissAt()
			return m, nil
		case "up":
			m.atIdx--
			if m.atIdx < 0 {
				m.atIdx = len(m.atMenu) - 1
			}
			return m, nil
		case "down":
			m.atIdx++
			m.atIdx %= len(m.atMenu)
			return m, nil
		}
	}

	// Then the plan decision: approve (optionally auto-accept), or
	// keep planning.
	if m.awaitingPlan != nil {
		var v planVerdict
		switch msg.String() {
		case "y", "Y":
			v.proceed = true
		case "a", "A":
			v.proceed, v.auto = true, true
		case "n", "N", "esc":
		default:
			return m, nil
		}
		req := m.awaitingPlan
		m.awaitingPlan = nil
		req.reply <- v
		m.notePlanVerdict(v)
		if v.proceed {
			// Approval graduates the session into a working mode so
			// the NEXT model request carries the action tools.
			mode := m.opt.Mode
			if v.auto {
				mode = tools.ModeFullAuto
			} else if mode == tools.ModeReadOnly || mode == tools.ModePlan {
				mode = tools.ModeAsk
			}
			if mode != m.opt.Mode {
				return m, m.setMode(mode)
			}
		}
		return m, nil
	}

	switch msg.String() {
	case "ctrl+c", "ctrl+d":
		// Codex's double-press exit: the first press arms a short
		// window — and interrupts a running turn — the toast says
		// so; only a second press inside the window exits. An
		// explicit /exit needs no arming.
		if time.Since(m.quitArmedAt) < quitWindow {
			m.cancelTurn()
			return m, tea.Quit
		}
		m.quitArmedAt = time.Now()
		if m.working {
			m.cancelTurn()
			m.showToast("interrupted — ctrl+c again to exit")
			return m, nil
		}
		m.showToast("ctrl+c again to exit")
		return m, nil
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
	case "tab", "shift+tab":
		dir := 1
		if msg.String() == "shift+tab" {
			dir = -1
		}
		return m, m.cycleMode(dir)
	case "ctrl+e":
		return m, m.openEditor()
	case "ctrl+v":
		// Image attach: the clipboard is read through the platform
		// tool (terminals can't deliver image bytes as text); the
		// login flow is masked input, not a place for images.
		if m.login == nil {
			m.attachClipboardImage()
		}
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
		// Prompt recall: ↑ walks back through submitted prompts,
		// ↓ forward again — only from the composer's first/last
		// line, so inside a multiline draft the arrows keep
		// moving the cursor. The login prompt never recalls.
		if m.login == nil && m.picker == nil {
			d := -1
			if msg.String() == "down" {
				d = 1
			}
			if m.recallHistory(d) {
				return m, nil
			}
		}
	}

	var cmd tea.Cmd
	// Typing exits history recall: the next ↑ starts from the live
	// draft again.
	if msg.Type == tea.KeyRunes || msg.Type == tea.KeySpace {
		m.histIdx = len(m.hist)
	}
	m.composer, cmd = m.composer.Update(msg)
	return m, cmd
}

// quitWindow is how long the first ctrl+c keeps the exit armed —
// the toast hint shows for the same span, so the promise on screen
// never outlives the window.
const quitWindow = 4 * time.Second

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

// cycleMode moves through the permission modes: tab forward, shift+tab
// back. Both end in setMode, which drives the gate, the status bar, and
// the animated toast.
func (m *Model) cycleMode(dir int) tea.Cmd {
	for i, mode := range tools.Modes {
		if mode == m.opt.Mode {
			next := tools.Modes[(i+dir+len(tools.Modes))%len(tools.Modes)]
			return m.setMode(next)
		}
	}
	return m.setMode(tools.ModeAsk)
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
	m.pushHistory(raw)
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
		if arg == "" {
			m.openLoginPicker()
			return nil
		}
		m.beginLogin(arg)
		return nil
	case "/logout":
		m.logout(arg)
		return nil
	case "/mode":
		return m.setMode(arg)
	case "/model":
		m.handleModelCommand(arg)
		return nil
	case "/models":
		return m.openModelsPicker(arg)
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
			m.queue = append(m.queue, queued{text: text, images: m.pendingImages()})
			m.add(entry{kind: entryQueued, text: text})
			return nil
		}
		m.opt.Orch.Steer(text)
		m.add(entry{kind: entrySteer, text: text})
		return nil
	}

	// Commit everything before this turn's user entry to native
	// scrollback: past turns, the greeting, and command output
	// freeze above the live region and stop re-rendering per frame.
	commit := m.commitEntries()
	m.add(entry{kind: entryUser, text: text})
	m.startTurn(text, m.pendingImages())
	return tea.Batch(commit, m.spinner.Tick, statusTick())
}

// pushHistory records a submitted prompt in the recall history and
// persists it. The stored form is what recall should put back:
// paste tokens expand into their content (their map entry left with
// the submit, so a recalled token would be dead), @mentions stay
// raw — they re-read the file fresh at submit. Consecutive
// duplicates collapse; the list caps at 500. Login keys never pass
// through here — secrets stay out of history by construction.
func (m *Model) pushHistory(text string) {
	if text == "" {
		return
	}
	text = m.expandHistoryPastes(text)
	if n := len(m.hist); n > 0 && m.hist[n-1] == text {
		m.histIdx = n
		return
	}
	m.hist = append(m.hist, text)
	const maxHist = 500
	if len(m.hist) > maxHist {
		m.hist = m.hist[len(m.hist)-maxHist:]
	}
	m.histIdx = len(m.hist)
	m.saveHistory()
}

// expandHistoryPastes replaces paste tokens with their content for
// history storage. A huge expansion keeps the typed form — recall
// then shows the dead token visibly instead of writing megabytes
// into history.jsonl.
func (m *Model) expandHistoryPastes(text string) string {
	const maxExpanded = 4096
	for token, content := range m.pasteAt {
		if !strings.Contains(text, token) {
			continue
		}
		candidate := strings.ReplaceAll(text, token, "\n"+content+"\n")
		if len(candidate) > maxExpanded {
			return text
		}
		text = candidate
	}
	return text
}

// saveHistory rewrites history.jsonl under TILDE_HOME (0600 —
// prompts can hold anything). Best-effort: a recall list that can't
// be written is a nuisance, not a failure.
func (m *Model) saveHistory() {
	if m.opt.TildeHome == "" {
		return
	}
	var b strings.Builder
	for _, h := range m.hist {
		line, err := json.Marshal(h)
		if err != nil {
			continue
		}
		b.Write(line)
		b.WriteByte('\n')
	}
	_ = os.WriteFile(filepath.Join(m.opt.TildeHome, "history.jsonl"), []byte(b.String()), 0o600)
}

// loadHistory reads the persisted recall list. A missing file or a
// corrupt line is skipped, never fatal — browsing must not break on
// one bad write.
func loadHistory(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var out []string
	for _, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		if line == "" {
			continue
		}
		var s string
		if json.Unmarshal([]byte(line), &s) == nil {
			out = append(out, s)
		}
	}
	return out
}

// recallHistory moves through the prompt history. ↑ walks back only
// from the composer's first line, ↓ forward only from its last —
// inside a multiline draft the arrows stay the cursor's. The live
// draft is saved on first recall and restored when ↓ walks past the
// newest entry.
func (m *Model) recallHistory(delta int) bool {
	if len(m.hist) == 0 {
		return false
	}
	if delta < 0 {
		if m.composer.Line() > 0 {
			return false
		}
		if m.histIdx == 0 {
			return false
		}
		if m.histIdx == len(m.hist) {
			m.draftSave = m.composer.Value()
		}
		m.histIdx--
	} else {
		if m.composer.Line() < m.composer.LineCount()-1 {
			return false
		}
		if m.histIdx >= len(m.hist) {
			return false
		}
		m.histIdx++
		if m.histIdx == len(m.hist) {
			m.composer.SetValue(m.draftSave)
			return true
		}
	}
	m.composer.SetValue(m.hist[m.histIdx])
	return true
}

// commitEntries prints the not-yet-committed transcript entries to
// native scrollback (tea.Println) and freezes them out of the live
// region. Without a running program there is nothing to print to —
// committing would silently drop the content — so it stays a no-op
// (tests, headless wiring). Returns the print command.
func (m *Model) commitEntries() tea.Cmd {
	if m.program == nil {
		return nil
	}
	lines := m.commitLines(m.committed)
	m.committed = len(m.entries)
	if len(lines) == 0 {
		return nil
	}
	// The trailing newline leaves one blank at the seam so the
	// committed block and the live region keep the transcript's
	// breathing rhythm.
	return tea.Println(strings.Join(lines, "\n") + "\n")
}

// commitLines renders entries [from, len) with the same blank-line
// rhythm timelineView pins, so what prints above the live region is
// byte-identical to what the frame showed.
func (m *Model) commitLines(from int) []string {
	if from < 0 {
		from = 0
	}
	if from >= len(m.entries) {
		return nil
	}
	prevKind := entryKind(-1)
	prevSub := ""
	if from > 0 {
		prevKind = m.entries[from-1].kind
		prevSub = m.entries[from-1].subTitle
	}
	var out []string
	for i := from; i < len(m.entries); i++ {
		e := &m.entries[i]
		if blankBefore(e, prevKind, prevSub) {
			out = append(out, "")
		}
		out = append(out, m.renderEntry(e)...)
		prevKind, prevSub = e.kind, e.subTitle
	}
	return collapseBlanks(out)
}

// notePlanVerdict records the plan decision in the transcript so the
// conversation reads as plan → decision → execution.
func (m *Model) notePlanVerdict(v planVerdict) {
	switch {
	case v.proceed && v.auto:
		m.add(entry{kind: entryOK, text: "plan approved — full-auto, implementing"})
	case v.proceed:
		m.add(entry{kind: entryOK, text: "plan approved — implementing, actions will ask"})
	default:
		m.add(entry{kind: entryDim, text: "plan declined — staying in " + m.opt.Mode})
	}
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
	m.opt.Orch.Provider = llm.New(m.opt.API, m.opt.BaseURL, key)
	m.opt.Orch.Gate = &tools.Gate{
		Decide: tools.PolicyDecide(m.opt.Mode, m.Prompt()),
		Audit:  tools.NewAuditLog(m.opt.AuditPath, tools.NewRedactor(key)),
	}
	_ = provider
}

// beginLogin starts the masked key capture. An explicit provider names
// which entry auth.json gets (pre-provisioning another provider is
// fine); bare /login means the active one.
func (m *Model) beginLogin(provider string) {
	if provider == "" {
		provider = m.opt.ProviderName
	}
	m.login = &loginFlow{provider: provider}
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
	// The live client only changes when the stored key belongs to the
	// active provider — a different provider's key is stored for its
	// next session, not applied to this one's requests.
	if provider == m.opt.ProviderName {
		m.applyNewKey(provider, key)
		m.add(entry{kind: entryOK, text: "key for " + provider + " stored in auth.json (0600) — future requests use it"})
	} else {
		m.add(entry{kind: entryOK, text: "key for " + provider + " stored in auth.json (0600) — used when " + provider + " is the active provider"})
	}
	return nil
}

// logout removes the stored key for a provider: bare /logout means the
// active one, an argument names another. Only what tilde stored is
// touched — never environment variables, never the provider's side.
func (m *Model) logout(provider string) {
	if provider == "" {
		provider = m.opt.ProviderName
	}
	removed, err := config.RemoveAuthKey(m.opt.TildeHome, provider)
	if err != nil {
		m.add(entry{kind: entryErr, text: "logout failed: " + err.Error()})
		return
	}
	if !removed {
		m.add(entry{kind: entryOK, text: "no stored key for " + provider + " — nothing to remove"})
		return
	}
	if provider == m.opt.ProviderName {
		m.applyNewKey(provider, "")
	}
	m.add(entry{kind: entryOK,
		text: "removed the stored key for " + provider +
			". This does not unset environment variables or revoke the key at the provider."})
}

// setMode implements /mode, tab, and shift+tab: no argument shows the
// mode; a valid name switches, resets any session allow-all grant, and
// announces the change with an animated toast.
func (m *Model) setMode(arg string) tea.Cmd {
	if arg == "" {
		m.add(entry{kind: entryDim,
			text: "mode is " + m.opt.Mode + " — options: " + strings.Join(tools.Modes, ", ") + "; /mode <name> or tab"})
		return nil
	}
	mode := tools.NormalizeMode(arg)
	if !tools.ValidMode(mode) {
		m.add(entry{kind: entryErr, text: "unknown mode " + arg + " — options: " + strings.Join(tools.Modes, ", ")})
		return nil
	}
	if mode == m.opt.Mode {
		m.showToast("mode is already " + mode)
		return nil
	}
	m.opt.Orch.SetMode(mode)
	m.rebuildGate(mode)
	m.opt.Mode = mode
	m.allowAll = false
	note := "mode switched to " + mode
	if m.working {
		note += " (takes effect for the next model request)"
	}
	// No transcript entry: the animated toast announces the change and
	// the mode line under the composer persists it. A line per keypress
	// buried the conversation.
	return m.showAnimatedToast(note)
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

// handleEvent maps orchestrator events to timeline entries. The
// returned Cmd carries scrollback commits from turn boundaries.
func (m *Model) handleEvent(ev orchestrator.Event) (tea.Model, tea.Cmd) {
	switch ev.Kind {
	case orchestrator.EventReasoning:
		if m.reasoning.Len() == 0 {
			m.reasoningSince = time.Now()
		}
		m.reasoning.WriteString(ev.Text)

	case orchestrator.EventText:
		m.finishReasoning()
		m.stream.WriteString(ev.Text)

	case orchestrator.EventToolStart:
		m.finishReasoning()
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
		m.finishReasoning()
		m.finishStream()
		return m, m.turnEnded()

	case orchestrator.EventError:
		m.finishReasoning()
		m.finishStream()
		if ev.Err == nil {
			return m, m.turnEnded()
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
		return m, m.turnEnded()
	}
	return m, nil
}

// addResultEntry stores a tool result with enough structure to render
// collapsed, expanded, or as an edit_file diff.
func (m *Model) addResultEntry(ev orchestrator.Event) {
	res := strings.ReplaceAll(strings.TrimSpace(ev.ToolResult), "\n", " ⏎ ")
	e := entry{kind: entryResult, tool: ev.ToolCall.Name, summary: res, full: ev.ToolResult}
	switch ev.ToolCall.Name {
	case "edit_file":
		var a struct {
			Path string `json:"path"`
			Old  string `json:"old"`
			New  string `json:"new"`
		}
		if err := json.Unmarshal([]byte(ev.ToolCall.Arguments), &a); err == nil {
			e.path, e.old, e.new = a.Path, a.Old, a.New
		}
	case "write_file":
		// A write is a diff against the empty file: the content
		// renders as added lines so code edits read as code edits.
		var a struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		}
		if err := json.Unmarshal([]byte(ev.ToolCall.Arguments), &a); err == nil {
			e.path, e.full = a.Path, a.Content
		}
	}
	m.add(e)
}

func (m *Model) turnEnded() tea.Cmd {
	m.working = false
	m.cancel = nil
	if len(m.queue) == 0 {
		return nil
	}
	next := m.queue[0]
	m.queue = m.queue[1:]
	// The finished turn commits before the follow-up's entry joins
	// the live region — each turn's block is frozen whole.
	commit := m.commitEntries()
	m.add(entry{kind: entryUser, text: dimStyle.Render("(follow-up) ") + next.text})
	m.startTurn(next.text, next.images)
	return tea.Batch(commit, m.spinner.Tick, statusTick())
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
	if pc, ok := m.providerConfigFor(provider); ok && pc.BaseURL != "" {
		m.opt.BaseURL = pc.BaseURL
		m.opt.API = pc.API
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
// The item carries its own action (built when the picker opened — the
// picker itself is closed before this runs), and a fetch returns the
// Cmd so the async model list actually starts.
func (m *Model) pickerSelect(it pickerItem) tea.Cmd {
	if it.Path != "" {
		m.resumeSession(it.Path, it.Label)
		return nil
	}
	switch it.Action {
	case "login":
		m.beginLogin(it.Provider)
		return nil
	case "fetch":
		return m.fetchModelsCmd(it.Provider)
	default:
		m.switchModel(it.Provider, it.Model)
		return nil
	}
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
	m.committed = 0
	m.stream.Reset()
	m.queue = nil
	m.usage = llm.Usage{}
	m.entries = append(m.entries, entry{kind: entryDim, text: boldStyle.Render("tilde " + version)})
	m.add(entry{kind: entryDim, text: dimStyle.Render(m.opt.Model + " · " + m.opt.Mode)})
	m.add(entry{kind: entryOK, text: fmt.Sprintf("resumed %s — %d messages in context", label, n)})
	m.showToast("resumed " + label)
}

// finishReasoning closes the thinking block: a collapsed "thought
// for Ns" entry — the reasoning text stays available under ctrl+r.
func (m *Model) finishReasoning() {
	if m.reasoning.Len() == 0 {
		return
	}
	d := time.Since(m.reasoningSince).Round(time.Second)
	e := entry{
		kind: entryReasoning,
		text: m.reasoning.String(),
		dur:  d.String(),
	}
	m.reasoning.Reset()
	m.reasoningSince = time.Time{}
	m.add(e)
}

// finishStream flushes the accumulated assistant text into the
// transcript. The entry text is normalized exactly as the stream view
// was (TrimSpace), so the flushed entry renders byte-identical to the
// last streamed frame — no reflow "pop" at the round boundary.
func (m *Model) finishStream() {
	if s := strings.TrimSpace(m.stream.String()); s != "" {
		m.add(entry{kind: entryAssistant, text: s})
	}
	m.stream.Reset()
	m.streamRendered, m.streamRenderedLen, m.streamRenderedW = nil, 0, 0
}

// openEditor hands the composer to $VISUAL / $EDITOR (Codex's
// external-editor borrow): the text lands in a temp file, the editor
// runs with the TUI suspended, and editorFinished re-reads the result.
// Modal states own the keyboard, and a mid-turn suspension would be a
// surprise — ctrl+e only works in the plain composer.
func (m *Model) openEditor() tea.Cmd {
	if m.login != nil || m.picker != nil || m.helpOpen ||
		m.awaitingPerm != nil || m.awaitingPlan != nil || m.working {
		return nil
	}
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		m.showToast("no $VISUAL or $EDITOR set")
		return nil
	}
	f, err := os.CreateTemp("", "tilde-composer-*.md")
	if err != nil {
		m.showToast("could not create the editor file: " + err.Error())
		return nil
	}
	path := f.Name()
	if _, err := f.WriteString(m.composer.Value()); err != nil {
		f.Close()
		os.Remove(path)
		m.showToast("could not write the editor file: " + err.Error())
		return nil
	}
	f.Close()

	// $VISUAL/$EDITOR may carry arguments; fields is enough for the
	// conventional "code -w" style — quoted editor paths are rare and
	// fail loudly here.
	parts := strings.Fields(editor)
	c := exec.Command(parts[0], append(parts[1:], path)...)
	return tea.ExecProcess(c, func(err error) tea.Msg {
		return editorDoneMsg{path: path, err: err}
	})
}

// editorFinished reads the editor's file back into the composer. The
// edit wins even if the editor exited nonzero — half the editors in
// the wild do that — but a missing file keeps the composer untouched.
func (m *Model) editorFinished(msg editorDoneMsg) tea.Cmd {
	defer os.Remove(msg.path)
	data, err := os.ReadFile(msg.path)
	if err != nil {
		m.showToast("editor file unreadable: " + err.Error())
		return nil
	}
	m.composer.SetValue(string(data))
	m.resizeComposer()
	m.refreshAtMenu()
	m.syncComposerPrompt()
	m.showToast("composer loaded from the editor")
	return nil
}
