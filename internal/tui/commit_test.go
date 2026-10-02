package tui

import (
	"strings"
	"testing"

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
