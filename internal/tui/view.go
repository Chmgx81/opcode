package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	statusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	userStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("39"))
	steerStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	queuedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
	toolStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("206"))
	resultStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("247"))
	errorStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	okStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("114"))
	permStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("220")).Bold(true)
)

// View implements tea.Model: status bar, transcript, streaming text,
// permission prompt, and the input box.
func (m *Model) View() string {
	var b strings.Builder
	b.WriteString(m.statusBar())
	b.WriteString("\n\n")

	// Transcript: keep only what fits, so long sessions don't push the
	// input off screen.
	visible := m.visibleLines()
	for _, l := range visible {
		b.WriteString(l)
		b.WriteString("\n")
	}
	if s := m.stream.String(); s != "" {
		b.WriteString(s)
	}

	if m.login != nil {
		b.WriteString("\n")
		b.WriteString(permStyle.Render("API key for " + m.login.provider + " (input hidden, Enter to save, Esc to cancel)"))
		b.WriteString("\n")
	}

	if m.awaitingPerm != nil {
		b.WriteString("\n")
		b.WriteString(permStyle.Render(fmt.Sprintf(
			"allow %s (%s tier)? y = allow, a = allow all action tools this session, n/Esc = deny",
			m.awaitingPerm.tool, m.awaitingPerm.tier)))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(m.input.View())
	return b.String()
}

func (m *Model) statusBar() string {
	parts := []string{"tilde", m.opt.Model, m.opt.Mode}
	if m.opt.Cwd != "" {
		parts = append(parts, m.opt.Cwd)
	}
	if m.usage.PromptTokens > 0 || m.usage.CompletionTokens > 0 {
		parts = append(parts, fmt.Sprintf("tokens: %d in / %d out",
			m.usage.PromptTokens, m.usage.CompletionTokens))
	}
	if m.working {
		parts = append(parts, m.spinner.View()+" working (Esc cancels, Enter steers, Alt+Enter queues)")
	} else {
		parts = append(parts, "idle")
	}
	return statusStyle.Render(strings.Join(parts, " | "))
}

// visibleLines trims the transcript to the space left under the status
// bar, prompt lines, and input box.
func (m *Model) visibleLines() []string {
	reserved := 6 // status bar, blanks, prompt, input, stream headroom
	if m.height <= 0 {
		return m.lines
	}
	max := m.height - reserved
	if max < 1 {
		max = 1
	}
	if len(m.lines) <= max {
		return m.lines
	}
	dropped := len(m.lines) - max
	out := make([]string, 0, max)
	out = append(out, statusStyle.Render(fmt.Sprintf("... %d earlier lines not shown ...", dropped)))
	return append(out, m.lines[dropped:]...)
}
