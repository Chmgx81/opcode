package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Chmgx81/tilde/internal/llm"
)

func upKey() tea.KeyMsg   { return tea.KeyMsg{Type: tea.KeyUp} }
func downKey() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyDown} }
func runeKey(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

// TestHistoryRecall walks the prompt history: ↑ back, ↑ again older,
// ↑ at the oldest stays, ↓ forward, ↓ past the newest restores the
// live draft.
func TestHistoryRecall(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, [][]llm.ChatEvent{})

	typeAndEnter(m, "first prompt")
	typeAndEnter(m, "second prompt")
	if len(m.hist) != 2 {
		t.Fatalf("history = %v, want both prompts", m.hist)
	}

	m.Update(upKey())
	if v := m.composer.Value(); v != "second prompt" {
		t.Errorf("after first ↑ value = %q, want the newest prompt", v)
	}
	m.Update(upKey())
	if v := m.composer.Value(); v != "first prompt" {
		t.Errorf("after second ↑ value = %q, want the oldest prompt", v)
	}
	m.Update(upKey())
	if v := m.composer.Value(); v != "first prompt" {
		t.Errorf("↑ at the oldest changed the value to %q", v)
	}

	m.Update(downKey())
	if v := m.composer.Value(); v != "second prompt" {
		t.Errorf("after ↓ value = %q, want the newest prompt", v)
	}
	m.Update(downKey())
	if v := m.composer.Value(); v != "" {
		t.Errorf("↓ past the newest = %q, want the empty live draft back", v)
	}
}

// TestHistoryRecallSavesDraft: a draft in progress survives a
// round trip through the history.
func TestHistoryRecallSavesDraft(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, [][]llm.ChatEvent{})

	typeAndEnter(m, "earlier prompt")
	m.composer.SetValue("half-typed draft")
	m.Update(upKey())
	if v := m.composer.Value(); v != "earlier prompt" {
		t.Fatalf("↑ recalled %q, want the submitted prompt", v)
	}
	m.Update(downKey())
	if v := m.composer.Value(); v != "half-typed draft" {
		t.Errorf("draft not restored: %q", v)
	}
}

// TestHistoryTypingResetsRecall: typing while recalling puts the
// next ↑ back at the newest entry, not inside the recalled text.
func TestHistoryTypingResetsRecall(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, [][]llm.ChatEvent{})

	typeAndEnter(m, "one")
	typeAndEnter(m, "two")
	m.Update(upKey())
	m.Update(upKey())
	if v := m.composer.Value(); v != "one" {
		t.Fatalf("setup: value = %q, want %q", v, "one")
	}
	m.Update(runeKey("x"))
	m.Update(upKey())
	if v := m.composer.Value(); v != "two" {
		t.Errorf("after typing, ↑ recalled %q, want the newest prompt", v)
	}
}

// TestHistoryMultilineGuard: inside a multiline draft the arrows
// move the cursor, not the history — ↑ recalls only from the
// composer's first line.
func TestHistoryMultilineGuard(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, [][]llm.ChatEvent{})

	typeAndEnter(m, "a prompt")
	m.composer.SetValue("line one\nline two")
	// Cursor sits on the second line: the first ↑ moves within the
	// composer, not through history.
	m.Update(upKey())
	if v := m.composer.Value(); v != "line one\nline two" {
		t.Fatalf("↑ from the second line recalled history: %q", v)
	}
	// Now on the first line: the next ↑ recalls.
	m.Update(upKey())
	if v := m.composer.Value(); v != "a prompt" {
		t.Errorf("↑ from the first line = %q, want the prompt", v)
	}
}

// TestHistoryPersists: submits land in history.jsonl (one JSON line
// each) and a fresh model reloads them.
func TestHistoryPersists(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, [][]llm.ChatEvent{})

	typeAndEnter(m, "persisted prompt")

	data, err := os.ReadFile(filepath.Join(dir, "history.jsonl"))
	if err != nil {
		t.Fatalf("history.jsonl not written: %v", err)
	}
	if got, want := strings.TrimSpace(string(data)), `"persisted prompt"`; got != want {
		t.Errorf("history.jsonl = %s, want %s", got, want)
	}
	info, err := os.Stat(filepath.Join(dir, "history.jsonl"))
	if err == nil && info.Mode().Perm() != 0o600 {
		t.Errorf("history.jsonl mode = %v, want 0600", info.Mode().Perm())
	}

	m2, _ := newText(t, dir, [][]llm.ChatEvent{})
	if len(m2.hist) != 1 || m2.hist[0] != "persisted prompt" {
		t.Fatalf("reloaded history = %v", m2.hist)
	}
	m2.Update(upKey())
	if v := m2.composer.Value(); v != "persisted prompt" {
		t.Errorf("↑ after reload = %q", v)
	}
}

// TestHistoryCollapseAndCap: consecutive duplicates collapse and
// the list caps at 500.
func TestHistoryCollapseAndCap(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, [][]llm.ChatEvent{})

	typeAndEnter(m, "same")
	typeAndEnter(m, "same")
	if len(m.hist) != 1 {
		t.Errorf("consecutive duplicate recorded twice: %v", m.hist)
	}

	m.hist = nil
	for i := 0; i < 600; i++ {
		m.pushHistory(strings.Repeat("a", i%3+1))
	}
	if len(m.hist) != 500 {
		t.Errorf("history length = %d, want capped at 500", len(m.hist))
	}
}

// TestHistorySkipsEmpty: an empty submit records nothing.
func TestHistorySkipsEmpty(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, [][]llm.ChatEvent{})
	m.pushHistory("")
	if len(m.hist) != 0 {
		t.Errorf("empty prompt recorded: %v", m.hist)
	}
}

// TestHistoryExpandsPasteTokens: a submitted paste token stores its
// content — recall must give back usable text, not a dead token.
func TestHistoryExpandsPasteTokens(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, [][]llm.ChatEvent{})

	content := strings.Repeat("pasted content line\n", 60)
	m.handlePaste(content) // ≥4 lines, ≥1000 chars → collapsed token
	if v := m.composer.Value(); !strings.Contains(v, "[paste 1") {
		t.Fatalf("setup: paste not collapsed to a token: %q", v)
	}
	m.Update(enterKey())

	if len(m.hist) != 1 {
		t.Fatalf("history = %v", m.hist)
	}
	if strings.Contains(m.hist[0], "[paste 1") {
		t.Errorf("history kept the dead token: %q", m.hist[0])
	}
	if !strings.Contains(m.hist[0], "pasted content line") {
		t.Errorf("history lost the paste content: %q", m.hist[0])
	}
}

// TestHistoryKeepsHugePastesAsTyped: an expansion beyond the cap
// stores the typed form — a visible dead token on recall, not
// megabytes in history.jsonl.
func TestHistoryKeepsHugePastesAsTyped(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, [][]llm.ChatEvent{})

	content := strings.Repeat("x", 5000)
	m.handlePaste(content)
	if v := m.composer.Value(); !strings.Contains(v, "[paste 1") {
		t.Fatalf("setup: paste not collapsed to a token: %q", v)
	}
	m.Update(enterKey())

	if len(m.hist) != 1 {
		t.Fatalf("history = %v", m.hist)
	}
	if !strings.Contains(m.hist[0], "[paste 1") {
		t.Errorf("huge paste was expanded into history: %q", m.hist[0])
	}
}
