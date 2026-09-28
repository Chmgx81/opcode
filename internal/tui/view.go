package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Palette matched against the references in tilde/references: Claude
// Code's dark slate terminal with its coral accent, and the blue/orange
// mix in the demo capture. tilde's own accent is the coral one.
var (
	accentStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#D97757")) // coral
	dimStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	steerStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	queuedStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
	toolNameStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	resultStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
	errorStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	okStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("114"))
	promptStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("220")).Bold(true)

	boxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("240")).
			Padding(0, 1)
	promptBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("220")).
			Padding(0, 1)
)

// View implements tea.Model. Layout follows the references: transcript
// with marker glyphs, then a rounded input box at the bottom with a dim
// meta row (status and token counts) under it — no top bar.
func (m *Model) View() string {
	var b strings.Builder

	visible := m.visibleLines()
	for _, l := range visible {
		b.WriteString(l)
		b.WriteString("\n")
	}
	if s := m.stream.String(); s != "" {
		for _, line := range wordWrap(s, m.termWidth()) {
			b.WriteString(line)
			b.WriteString("\n")
		}
	}

	if m.login != nil {
		b.WriteString("\n")
		b.WriteString(promptBoxStyle.Render(
			promptStyle.Render("API key for " + m.login.provider + " — input hidden, Enter to save, Esc to cancel")))
		b.WriteString("\n")
	}

	if m.awaitingPerm != nil {
		b.WriteString("\n")
		b.WriteString(promptBoxStyle.Width(m.termWidth() - 4).Render(fmt.Sprintf(
			"%s %s %s",
			promptStyle.Render("allow?"),
			toolNameStyle.Render(m.awaitingPerm.tool),
			dimStyle.Render("(tier "+string(m.awaitingPerm.tier)+") — y allow, a allow all action tools this session, n/Esc deny"))))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	box := boxStyle.Width(m.termWidth() - 4)
	b.WriteString(box.Render(m.input.View()))
	b.WriteString("\n")
	b.WriteString(m.metaRow())
	return b.String()
}

// metaRow is the dim line under the input box: what's happening on the
// left, model and tokens on the right.
// termWidth is the terminal width, defaulting to 80 before the first
// WindowSizeMsg.
func (m *Model) termWidth() int {
	if m.width > 0 {
		return m.width
	}
	return 80
}

func (m *Model) metaRow() string {
	left := "ctrl+c to exit"
	if m.working {
		left = m.spinner.View() + " esc to interrupt · Enter steers · Alt+Enter queues"
	}
	right := m.opt.Model
	if m.usage.PromptTokens > 0 || m.usage.CompletionTokens > 0 {
		right += fmt.Sprintf(" · %d in / %d out", m.usage.PromptTokens, m.usage.CompletionTokens)
	}
	gap := m.termWidth() - lipgloss.Width(left) - lipgloss.Width(right) - 2
	if gap < 1 {
		gap = 1
	}
	return dimStyle.Render(left) + strings.Repeat(" ", gap) + dimStyle.Render(right)
}

// visibleLines trims the transcript to the space left under the
// transcript, boxes, and meta row.
func (m *Model) visibleLines() []string {
	// The frame must fit the terminal exactly: bubbletea's inline
	// renderer corrupts the screen when a frame is taller than the
	// window. Budget for what the current layout actually renders
	// below the transcript: the input box (3), the meta row (1), the
	// prompt box (3) when a permission or login prompt is up, the
	// streaming line, and spacing.
	reserved := 8 // input box, meta row, spacing
	if m.awaitingPerm != nil || m.login != nil {
		reserved += 4
	}
	if m.stream.String() != "" {
		reserved += 1
	}
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
	out := make([]string, 0, max+1)
	out = append(out, dimStyle.Render(fmt.Sprintf("... %d earlier lines not shown ...", dropped)))
	return append(out, m.lines[dropped:]...)
}

// wordWrap wraps plain (unstyled) text at width, breaking on spaces.
// Transcript content that is styled gets wrapped before styling, where
// the plain text is still in hand — a wrapped ANSI string cannot be
// re-split safely afterwards.
func wordWrap(s string, width int) []string {
	if width < 10 {
		width = 10
	}
	var out []string
	for _, para := range strings.Split(s, "\n") {
		if len([]rune(para)) <= width {
			out = append(out, para)
			continue
		}
		line := ""
		for _, w := range strings.Fields(para) {
			if line == "" {
				line = w
			} else if len([]rune(line))+1+len([]rune(w)) <= width {
				line += " " + w
			} else {
				out = append(out, line)
				line = w
			}
		}
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}
