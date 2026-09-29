package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func ctrlCKey() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyCtrlC} }
func ctrlDKey() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyCtrlD} }

// TestCtrlCDoublePressExits: Codex's exit posture — the first
// ctrl+c must not quit (it interrupts a running turn and hints);
// only a second press inside the window exits.
func TestCtrlCDoublePressExits(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)
	m.working = true

	// First press: interrupt + hint, no quit.
	_, cmd := m.Update(ctrlCKey())
	if cmd != nil {
		t.Error("first ctrl+c must not quit")
	}
	if m.toast != "interrupted — ctrl+c again to exit" {
		t.Errorf("first ctrl+c toast = %q", m.toast)
	}

	// Second press inside the window: quit.
	_, cmd = m.Update(ctrlCKey())
	if cmd == nil {
		t.Error("second ctrl+c inside the window must quit")
	}
}

// TestCtrlCHintWhenIdle: an idle first press just arms the exit.
func TestCtrlCHintWhenIdle(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)

	_, cmd := m.Update(ctrlCKey())
	if cmd != nil {
		t.Error("idle first ctrl+c must not quit")
	}
	if m.toast != "ctrl+c again to exit" {
		t.Errorf("idle first ctrl+c toast = %q", m.toast)
	}

	_, cmd = m.Update(ctrlCKey())
	if cmd == nil {
		t.Error("second ctrl+c must quit")
	}
}

// TestCtrlDIsTheSameDoublePress: ctrl+d arms and exits like
// ctrl+c — Codex parity.
func TestCtrlDIsTheSameDoublePress(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)

	if _, cmd := m.Update(ctrlDKey()); cmd != nil {
		t.Error("first ctrl+d must not quit")
	}
	if _, cmd := m.Update(ctrlDKey()); cmd == nil {
		t.Error("second ctrl+d must quit")
	}
}
