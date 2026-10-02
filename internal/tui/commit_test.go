package tui

import (
	"fmt"
	"io"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Chmgx81/opcode/internal/llm"
)

// indexOfEntry finds the position of the first entry whose text
// contains want — the greeting New() prepends makes fixed indices
// brittle.
func indexOfEntry(m *Model, want string) int {
	for i := range m.entries {
		if strings.Contains(m.entries[i].text, want) {
			return i
		}
	}
	return -1
}

// TestCommitLinesCarriesBreathingSpace: what commits to scrollback
// keeps the transcript's rhythm — exactly one blank line between
// the user's block and the answer.
func TestCommitLinesCarriesBreathingSpace(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, [][]llm.ChatEvent{})
	m.add(entry{kind: entryUser, text: "the question"})
	m.add(entry{kind: entryAssistant, text: "the answer"})

	lines := m.commitLines(0)
	joined := stripANSI(strings.Join(lines, "\n"))
	if !strings.Contains(joined, "the question") || !strings.Contains(joined, "the answer") {
		t.Fatalf("commit lines missing content: %q", joined)
	}
	qi := -1
	for i, l := range lines {
		if strings.Contains(stripANSI(l), "the question") {
			qi = i
		}
	}
	ai := -1
	for i, l := range lines {
		if strings.Contains(stripANSI(l), "the answer") {
			ai = i
		}
	}
	if qi < 0 || ai < 0 || ai <= qi {
		t.Fatalf("question or answer not found in order: %q", joined)
	}
	between := lines[qi+1 : ai]
	if len(between) != 1 || strings.TrimSpace(stripANSI(between[0])) != "" {
		t.Errorf("blocks between question and answer = %q, want exactly one blank line", between)
	}
}

// TestCommitLinesFromBoundary: a commit that starts mid-transcript
// renders only the remaining entries.
func TestCommitLinesFromBoundary(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, [][]llm.ChatEvent{})
	m.add(entry{kind: entryUser, text: "old question"})
	m.add(entry{kind: entryAssistant, text: "old answer"})
	m.add(entry{kind: entryUser, text: "new question"})

	lines := m.commitLines(indexOfEntry(m, "new question"))
	joined := stripANSI(strings.Join(lines, "\n"))
	if strings.Contains(joined, "old answer") {
		t.Errorf("commit from the boundary re-rendered committed content: %q", joined)
	}
	if !strings.Contains(joined, "new question") {
		t.Errorf("commit from the boundary lost the live entry: %q", joined)
	}
}

// TestViewSkipsCommitted: entries already printed to scrollback
// leave the live region — the frame stops re-rendering them.
func TestViewSkipsCommitted(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, [][]llm.ChatEvent{})
	m.add(entry{kind: entryUser, text: "frozen question"})
	m.add(entry{kind: entryAssistant, text: "live answer"})

	m.committed = indexOfEntry(m, "live answer")
	view := stripANSI(m.View())
	if strings.Contains(view, "frozen question") {
		t.Error("View still renders the committed entry")
	}
	if !strings.Contains(view, "live answer") {
		t.Error("View dropped the uncommitted entry")
	}
}

// TestCommitEntriesWithoutProgram: no running tea program means no
// scrollback to print to — committing would silently drop content,
// so it stays a no-op and the frame keeps everything.
func TestCommitEntriesWithoutProgram(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, [][]llm.ChatEvent{})
	m.add(entry{kind: entryUser, text: "kept question"})

	if cmd := m.commitEntries(); cmd != nil {
		t.Error("commitEntries returned a command without a running program")
	}
	if m.committed != 0 {
		t.Errorf("committed = %d without a program, want 0", m.committed)
	}
	if !strings.Contains(stripANSI(m.View()), "kept question") {
		t.Error("View lost the entry despite the no-op commit")
	}
}

// TestSubmitRecordsHistoryWhileWorking: a submit during a running
// turn steers — but the prompt still lands in the recall history.
func TestSubmitRecordsHistoryWhileWorking(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, [][]llm.ChatEvent{})
	typeAndEnter(m, "first")
	// No scripted round: the turn is still "working", so the next
	// submit steers — but history still records it.
	typeAndEnter(m, "second")
	if len(m.hist) != 2 || m.hist[1] != "second" {
		t.Fatalf("history = %v, want both prompts", m.hist)
	}
	if len(m.queue) != 0 {
		t.Errorf("steer queued instead: %+v", m.queue)
	}
}

// TestFinishedTurnCommitsAtTheEnd: the turn commits to native
// scrollback the moment it finishes, not at the next boundary. The
// terminal's own scrolling is the reader's native way back through
// a long answer; holding the finished turn live left the whole
// session unscrollable and let the frame trim it to a marker.
func TestFinishedTurnCommitsAtTheEnd(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, [][]llm.ChatEvent{})
	// commit prints through the program; an unstarted one is inert
	// but non-nil, which is all the seam needs.
	m.program = tea.NewProgram(m, tea.WithInput(nil), tea.WithOutput(io.Discard))
	m.add(entry{kind: entryUser, text: "the question"})
	m.add(entry{kind: entryAssistant, text: "a long answer that deserves scrolling"})
	m.working = true

	cmd := m.turnEnded()
	if cmd == nil {
		t.Fatal("turnEnded returned no print command")
	}
	if m.committed != len(m.entries) {
		t.Errorf("committed = %d, want %d — the finished turn stayed live",
			m.committed, len(m.entries))
	}
	if v := stripANSI(m.View()); strings.Contains(v, "a long answer") {
		t.Errorf("the finished answer is still in the live region:\n%s", v)
	}
}

// TestTrimMarkerNamesTheEscape: the frame's trim marker counts the
// lines it cut and says how to read them — "… N earlier lines" alone
// is a dead end, and one that counts its loss is worse: it proves
// the lines exist somewhere.
func TestTrimMarkerNamesTheEscape(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, [][]llm.ChatEvent{})
	m.entries = nil
	for i := 0; i < 40; i++ {
		m.add(entry{kind: entryAssistant, text: fmt.Sprintf("line %d of a very long answer", i)})
	}
	resize(m, 80, 10)
	v := stripANSI(m.View())
	if !strings.Contains(v, "earlier lines") {
		t.Fatalf("no trim marker in the frame:\n%s", v)
	}
	if !strings.Contains(v, "ctrl+o") {
		t.Errorf("the trim marker names no way to read the trimmed lines:\n%s", v)
	}
}

// TestCtrlRKeepsTheFrozenPromises: committed text cannot re-render,
// so ctrl+r with an empty live region opens the transcript pager —
// the one view that expands everything — and toggles the live
// expansion only when there is something live to expand. The frozen
// "(ctrl+r to expand)" hints in scrollback stay true.
func TestCtrlRKeepsTheFrozenPromises(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, [][]llm.ChatEvent{})
	m.program = tea.NewProgram(m, tea.WithInput(nil), tea.WithOutput(io.Discard))
	m.add(entry{kind: entryUser, text: "the question"})
	m.add(entry{kind: entryAssistant, text: "the answer"})
	m.working = true
	m.turnEnded()
	if m.committed < len(m.entries) {
		t.Fatal("setup: the turn did not commit")
	}

	// Nothing live: the key opens the pager instead of flipping a
	// flag nothing on screen can act on.
	m.Update(keyMsg("ctrl+r"))
	if !m.transcriptOpen {
		t.Fatal("ctrl+r over committed text opened no pager")
	}
	if m.expandResults {
		t.Error("ctrl+r flipped the live toggle over committed text")
	}
	m.Update(keyMsg("ctrl+r"))
	if m.transcriptOpen {
		t.Error("ctrl+r did not close the pager it opened")
	}

	// Something live: the toggle, as always.
	m.add(entry{kind: entryResult, tool: "read_file", summary: "s", full: "full text"})
	m.Update(keyMsg("ctrl+r"))
	if m.transcriptOpen {
		t.Error("ctrl+r opened the pager while live entries could expand in place")
	}
	if !m.expandResults {
		t.Error("ctrl+r did not expand the live results")
	}
}
