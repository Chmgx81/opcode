package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/Chmgx81/tilde/internal/llm"
	"github.com/Chmgx81/tilde/internal/orchestrator"
	"github.com/Chmgx81/tilde/internal/safe"
	"github.com/Chmgx81/tilde/internal/session"
	"github.com/Chmgx81/tilde/internal/tools"
)

// Every path where text the TUI did not author reaches the frame. The
// list is the point: a new untrusted source has to be added here, and
// the frame is asserted rune by rune rather than by eyeballing a
// substring.

// hostile is the payload every path below carries: a window-title grab,
// a screen clear, and a lone trailing ESC. One payload, not one per
// path, so a path that handles one of them handles the shape.
const hostile = "pre\x1b]0;pwned\x07post\x1b[2Jend\x1b"

// withoutSGR removes the colour sequences the styles themselves emit,
// and NOTHING else. stripANSI is the wrong tool here: it eats any
// ESC-initiated sequence, which is exactly the byte a hostile payload
// arrives on — it would have hidden the bug it was written to find.
func withoutSGR(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if n := escapeLen(s[i:]); n > 0 {
			i += n
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		b.WriteRune(r)
		i += size
	}
	return b.String()
}

// assertInert fails when a rendered frame carries a live control byte.
// ESC and BEL are the two that drive a terminal; the rest of C0 is
// covered so a stray CR or a form feed cannot slip in either.
func assertInert(t *testing.T, where, frame string) {
	t.Helper()
	plain := withoutSGR(frame)
	for _, r := range plain {
		if r == 0x1b || r == '\a' || (r < 0x20 && r != '\n' && r != '\t') {
			t.Fatalf("%s: the frame carries control byte %q in %q", where, r, plain)
		}
	}
}

// TestEveryUntrustedPathIsInert: one frame per untrusted source, each
// carrying the same hostile payload, each asserted rune by rune. The
// sources the TUI renders raw are the ones this catches: a path that
// was added and never sanitized shows up as a live escape byte here
// rather than as a hijacked window title in a bug report.
func TestEveryUntrustedPathIsInert(t *testing.T) {
	dir := t.TempDir()

	// One model, wired for every round these cases drive.
	m, _ := newText(t, dir, nil)
	m.entries = nil

	// Streamed prose, and the tool call line, and the tool result.
	_, _ = m.handleEvent(orchestrator.Event{Kind: orchestrator.EventText, Text: hostile})
	_, _ = m.handleEvent(orchestrator.Event{
		Kind:     orchestrator.EventToolStart,
		ToolCall: llm.ToolCall{Name: "bash", Arguments: hostile}})
	_, _ = m.handleEvent(orchestrator.Event{
		Kind:       orchestrator.EventToolResult,
		ToolCall:   llm.ToolCall{Name: "bash", Arguments: hostile},
		ToolResult: hostile})
	// Compaction note and the turn's error.
	_, _ = m.handleEvent(orchestrator.Event{Kind: orchestrator.EventCompaction, Text: hostile})
	_, _ = m.handleEvent(orchestrator.Event{
		Kind: orchestrator.EventError, Err: errWithEscape(hostile)})
	// A subagent's progress and its failure.
	m.handleSubagent(subagentEvent{Title: hostile, Kind: subagentText, Text: hostile})
	m.handleSubagent(subagentEvent{Title: "t", Kind: subagentError, Text: hostile})
	// The live task list.
	m.todos = []tools.Todo{{Content: hostile, Status: tools.TodoInProgress}}

	// A session's own user text, replayed into the /sessions picker.
	// This is the one path that reads a file off disk into a frame.
	sess := session.FromHistory("m", tools.ModeBuild,
		[]llm.Message{{Role: "user", Content: hostile}})
	if err := os.MkdirAll(session.Dir(dir), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := sess.Save(filepath.Join(session.Dir(dir), "hostile.json"), nil); err != nil {
		t.Fatal(err)
	}

	// The plan body and the approval dialog body.
	planReply := make(chan planVerdict, 1)
	m.Update(planRequestMsg{req: &planRequest{plan: hostile, reply: planReply}})
	permReply := make(chan bool, 1)
	m.Update(permRequestMsg{req: &permRequest{
		tool: "bash", tier: tools.TierActionAllowed,
		args: safeArgs(hostile), scope: "ls:*", reply: permReply,
		// Past the type-ahead guard, or the dialog would not be drawn
		// as open for this frame.
		openedAt: time.Now().Add(-time.Second)}})

	// The transcript pager, which re-renders every one of the above.
	m.transcriptOpen = true
	// An expanded result, which is the other render of the same text.
	m.expandResults = true

	assertInert(t, "transcript pager", m.View())

	// The shell escape's output and its wrapped failure.
	m2, _ := newText(t, dir, nil)
	m2.Update(shellDoneMsg{out: hostile})
	m2.Update(shellDoneMsg{out: hostile, err: errWithEscape(hostile)})
	assertInert(t, "shell escape", m2.transcript())

	// An @mention's file, read into the transcript.
	m3, _ := newText(t, dir, nil)
	if err := os.WriteFile(filepath.Join(dir, "hostile.txt"),
		[]byte(hostile), 0o644); err != nil {
		t.Fatal(err)
	}
	got := m3.expandPastes("see @" + dir + "/hostile.txt")
	if strings.ContainsAny(got, "\x1b\a") {
		t.Fatalf("an @mention's file carried control bytes: %q", got)
	}

	// /diff: git's own output through the same bash path.
	m4, _ := newText(t, dir, nil)
	m4.showDiff()
	for i := range m4.entries {
		assertInert(t, "/diff", strings.Join(m4.renderEntry(&m4.entries[i]), "\n"))
	}

	// A saved session's preview in the picker, and a bad file's error.
	m5, _ := newText(t, dir, nil)
	m5.openSessionsPicker()
	if m5.picker != nil {
		assertInert(t, "/sessions picker", strings.Join(m5.pickerRows(80), "\n"))
	}

	// The window title: the cwd is user-controlled text and OSC is an
	// injection surface there too. The ESC byte is what drives the
	// terminal; what is left after it is gone is inert text that happens
	// to read like a sequence, so the byte check is the whole claim.
	assertInert(t, "window title", sanitizeTitle("tilde — "+hostile))
}

// errWithEscape is a wrapped error carrying a control byte — the shape
// a hostile command name produces.
type errWithEscape string

func (e errWithEscape) Error() string { return string(e) }

// safeArgs is the gate's sanitized copy of a hostile tool call: the
// request the dialog is built from, exactly as decide() builds it.
func safeArgs(hostile string) string {
	return `{"command": ` + mustJSON(safe.Text(hostile)) + `}`
}
