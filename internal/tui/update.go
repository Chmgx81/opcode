package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"tilde/internal/config"
	"tilde/internal/llm"
	"tilde/internal/orchestrator"
	"tilde/internal/tools"
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

	case permRequestMsg:
		m.awaitingPerm = msg.req
		return m, nil

	case orchestratorMsg:
		return m.handleEvent(orchestrator.Event(msg)), nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// A permission prompt owns the keyboard: nothing else may be typed,
	// steered, or queued while it is up.
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

	switch msg.String() {
	case "ctrl+c":
		m.cancelTurn()
		return m, tea.Quit
	case "esc":
		if m.working {
			m.cancelTurn()
		}
		return m, nil
	case "enter", "alt+enter":
		if m.login != nil {
			return m, m.submitLogin()
		}
		return m, m.submitInput(msg.String() == "alt+enter")
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// submitInput handles Enter / Alt+Enter. While a turn runs, Enter steers
// (folds in at the next round boundary) and Alt+Enter queues a follow-up;
// while idle, either starts a turn.
func (m *Model) submitInput(alt bool) tea.Cmd {
	text := strings.TrimSpace(m.input.Value())
	if text == "" {
		return nil
	}
	m.input.SetValue("")

	switch text {
	case "/exit", "/quit":
		m.cancelTurn()
		return tea.Quit
	case "/login":
		m.beginLogin()
		return nil
	case "/logout":
		m.logout()
		return nil
	}

	if m.working {
		if alt {
			m.queue = append(m.queue, text)
			m.lines = append(m.lines, queuedStyle.Render("(queued) "+text))
			return nil
		}
		m.opt.Orch.Steer(text)
		m.lines = append(m.lines, steerStyle.Render("(steering) "+text))
		return nil
	}

	m.appendWrapped(accentStyle, "~ ", text)
	m.startTurn(text)
	return tea.Cmd(func() tea.Msg { return m.spinner.Tick() })
}

// beginLogin switches the input to masked capture for the provider's key.
func (m *Model) beginLogin() {
	m.login = &loginFlow{provider: m.opt.ProviderName}
	m.input.SetValue("")
	m.input.EchoMode = textinput.EchoPassword
	m.input.Placeholder = "paste the API key for " + m.opt.ProviderName
}

func (m *Model) submitLogin() tea.Cmd {
	key := strings.TrimSpace(m.input.Value())
	provider := m.login.provider
	m.login = nil
	m.input.SetValue("")
	m.input.EchoMode = textinput.EchoNormal
	m.input.Placeholder = "type a message, /login, /logout, or /exit"

	if key == "" {
		m.appendWrapped(errorStyle, "", "login cancelled: no key entered")
		return nil
	}
	if err := config.WriteAuthKey(m.opt.TildeHome, provider, key); err != nil {
		m.appendWrapped(errorStyle, "", "login failed: "+err.Error())
		return nil
	}
	// Use the new key immediately: rebuild the provider and the audit
	// redactor instead of requiring a restart.
	m.opt.Orch.Provider = llm.NewOpenAICompat(m.opt.BaseURL, key)
	m.opt.Orch.Gate = &tools.Gate{
		Decide: tools.PolicyDecide(m.opt.Mode, func(tool tools.Tool, args string) bool {
			return m.decide(tool, args)
		}),
		Audit: tools.NewAuditLog(m.opt.AuditPath, tools.NewRedactor(key)),
	}
	m.appendWrapped(okStyle, "",
		"key for "+provider+" stored in auth.json (0600) — future requests use it")
	return nil
}

// logout removes tilde's stored credential and says exactly what that
// does and does not do (spec 3.10).
func (m *Model) logout() {
	removed, err := config.RemoveAuthKey(m.opt.TildeHome, m.opt.ProviderName)
	if err != nil {
		m.appendWrapped(errorStyle, "", "logout failed: "+err.Error())
		return
	}
	if !removed {
		m.appendWrapped(okStyle, "",
			"no stored key for "+m.opt.ProviderName+" — nothing to remove")
		return
	}
	m.opt.Orch.Provider = llm.NewOpenAICompat(m.opt.BaseURL, "")
	m.appendWrapped(okStyle, "",
		"removed the stored key for "+m.opt.ProviderName+
			". This does not unset environment variables or revoke the key at the provider.")
}

// handleEvent renders one orchestrator event into TUI state.
func (m *Model) handleEvent(ev orchestrator.Event) tea.Model {
	switch ev.Kind {
	case orchestrator.EventText:
		m.stream.WriteString(ev.Text)

	case orchestrator.EventToolStart:
		m.finishStream()
		args := ev.ToolCall.Arguments
		if len(args) > 60 {
			args = args[:60] + "..."
		}
		m.lines = append(m.lines, accentStyle.Render("⏺ ")+
			toolNameStyle.Render(ev.ToolCall.Name)+dimStyle.Render(" "+args))

	case orchestrator.EventToolResult:
		res := strings.ReplaceAll(strings.TrimSpace(ev.ToolResult), "\n", " ")
		if len(res) > 200 {
			res = res[:200] + "..."
		}
		m.appendWrapped(resultStyle, "  ⎿ ", res)

	case orchestrator.EventUsage:
		m.usage.PromptTokens += ev.Usage.PromptTokens
		m.usage.CompletionTokens += ev.Usage.CompletionTokens

	case orchestrator.EventTurnComplete:
		m.finishStream()
		m.turnEnded()

	case orchestrator.EventError:
		m.finishStream()
		if ev.Err == nil {
			m.turnEnded()
			return m
		}
		if err := ev.Err; err == orchestrator.ErrCancelled {
			m.lines = append(m.lines, errorStyle.Render("turn cancelled"))
		} else {
			m.lines = append(m.lines, errorStyle.Render("error: "+err.Error()))
		}
		m.turnEnded()
	}
	return m
}

// turnEnded resets turn state and drains one queued follow-up, if any.
func (m *Model) turnEnded() {
	m.working = false
	m.cancel = nil
	if len(m.queue) == 0 {
		return
	}
	next := m.queue[0]
	m.queue = m.queue[1:]
	m.lines = append(m.lines, accentStyle.Render("~ ")+dimStyle.Render("(follow-up) ")+next)
	m.startTurn(next)
}

// appendWrapped adds a transcript entry whose plain text is word-wrapped
// to the terminal width before styling, so long content never wraps a
// rendered frame line on its own.
func (m *Model) appendWrapped(style lipgloss.Style, prefix, text string) {
	width := m.termWidth() - lipgloss.Width(prefix)
	wrapped := wordWrap(text, width)
	for i, line := range wrapped {
		if i == 0 {
			m.lines = append(m.lines, style.Render(prefix+line))
		} else {
			// Continuation lines keep the indentation of the prefix.
			m.lines = append(m.lines, style.Render(
				strings.Repeat(" ", lipgloss.Width(prefix))+line))
		}
	}
}
