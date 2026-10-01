package tui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// TestShellEscapeDoesNotBlockTheRenderLoop: the "!command" escape ran
// Bash.Execute on the tea goroutine, so `!sleep 300` froze the whole
// TUI for five minutes and esc had nothing to reach. It runs as a Cmd
// now, and the render loop keeps answering keys while it is out.
func TestShellEscapeDoesNotBlockTheRenderLoop(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)

	// A command that takes a moment. The submit must return before it
	// finishes, or the loop is blocked.
	started := time.Now()
	_, cmd := m.decideInputAfter(m, "! sleep 1")
	if cmd == nil {
		t.Fatal("the shell escape returned no command")
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Errorf("submit blocked for %s — the command ran on the render loop", elapsed)
	}
	// The frame renders, and the status line says what is running.
	if m.shell == nil {
		t.Error("no in-flight shell run while the command is out")
	}
	view := stripANSI(m.View())
	if !strings.Contains(view, "sleep 1") {
		t.Errorf("the running command is not on the status line:\n%s", view)
	}
	// esc reaches it: the context is cancelled, so the process group
	// dies, and the note says so rather than leaving a silent hang.
	m.Update(escKey())
	if m.shell != nil {
		t.Error("esc did not clear the in-flight shell run")
	}
	if tr := m.transcript(); !strings.Contains(tr, "interrupted") {
		t.Errorf("the interrupt was not recorded: %s", tr)
	}
	// The reply still lands when the command finally unwinds, and it
	// does not re-open a run that esc already closed.
	if msg, ok := cmd().(shellDoneMsg); ok {
		m.Update(msg)
	}
	if m.shell != nil {
		t.Error("a late reply resurrected the shell run")
	}
}

// decideInputAfter is decideInput with the composer already set — the
// shape the composer submit uses.
func (m *Model) decideInputAfter(_ *Model, text string) (inputDecision, tea.Cmd) {
	m.composer.SetValue(text)
	return m.decideInput(false)
}

// TestShellEscapeOutputSanitized: the escape's output reaches the
// transcript like any other untrusted display text. It did not: a
// command that printed an escape sequence drove the terminal.
func TestShellEscapeOutputSanitized(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)
	m.Update(shellDoneMsg{
		out: "before\x1b]0;pwned\x07after\x1b[2J",
		err: nil,
	})
	last := m.entries[len(m.entries)-1]
	if strings.ContainsAny(last.text, "\x1b\x07") {
		t.Fatalf("shell output carried control bytes into the transcript: %q", last.text)
	}
	if last.text != "beforeafter" {
		t.Errorf("sanitized shell output = %q, want %q", last.text, "beforeafter")
	}
}

// TestShellEscapeErrorNamesTheCommand: a failing command reports the
// output first and the cause on its own line — one glued-together line
// was unreadable — and the cause is sanitized like the output.
func TestShellEscapeErrorNamesTheCommand(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)
	m.Update(shellDoneMsg{out: "  no such file  \n", err: errWithEscape("bash: exit status 1")})
	last := m.entries[len(m.entries)-1]
	if last.kind != entryErr {
		t.Errorf("a failed command is not an error entry: %+v", last)
	}
	if !strings.Contains(last.text, "no such file") ||
		!strings.Contains(last.text, "\nbash: exit status 1") {
		t.Errorf("the failure does not carry both the output and the cause: %q", last.text)
	}
}

// TestMentionReadIsBounded: "@/dev/zero" (or any multi-gigabyte file)
// was read whole by os.ReadFile and only then capped, which allocated
// the entire file on the render goroutine before anything could stop
// it. The cap is now the reader's.
func TestMentionReadIsBounded(t *testing.T) {
	dir := t.TempDir()
	big := filepath.Join(dir, "big.bin")
	// Four times the cap, so an unbounded read would be four times too
	// long and a bounded one is exactly the cap.
	if err := os.WriteFile(big, []byte(strings.Repeat("A", 4*mentionCap)), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := readMention(big)
	if err != nil {
		t.Fatalf("readMention: %v", err)
	}
	if !strings.Contains(got, "truncated") {
		t.Error("a capped mention does not say it was truncated")
	}
	if n := len(got); n > mentionCap+32 {
		t.Errorf("readMention returned %d bytes for a %d-byte cap", n, mentionCap)
	}
}

// TestMentionReadStopsAtTheCap: the bound is the reader's, so a
// source that never ends is stopped too. "os.ReadFile then check the
// length" does not — it allocates the whole source first, which for a
// character device or an endless pipe is not a cost but a hang.
//
// A generator stands in for such a source: it yields endless bytes and
// counts how many it was asked for. The check is the count, not a
// timeout — an unbounded read would still be running when the test
// gave up, and this way nothing has to be interrupted.
func TestMentionReadStopsAtTheCap(t *testing.T) {
	got, err := readCapped(&endless{})
	if err != nil {
		t.Fatalf("readCapped: %v", err)
	}
	if !strings.Contains(got, "truncated") {
		t.Error("an endless source was not reported as truncated")
	}
	if n := len(got); n > mentionCap+64 {
		t.Errorf("readCapped returned %d bytes for a %d-byte cap", n, mentionCap)
	}
}

// endless is an io.Reader with no end. It stops producing after
// endlessLimit so a regression cannot spin here forever.
type endless struct{ asked int }

const endlessLimit = 4 * mentionCap

func (e *endless) Read(p []byte) (int, error) {
	if e.asked >= endlessLimit {
		return 0, errors.New("endless: asked for more than it promised")
	}
	e.asked += len(p)
	return len(p), nil
}

// TestMentionUnreadableIsVisible: a mention that cannot be read says
// so in place. Silence sent a half-read path to the model as though it
// were a whole file.
func TestMentionUnreadableIsVisible(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)
	// A directory: os.Open succeeds, the read fails.
	out := m.expandPastes("look at @" + dir)
	if !strings.Contains(out, "could not attach") {
		t.Errorf("an unreadable mention says nothing: %q", out)
	}
	if !strings.Contains(out, dir) {
		t.Errorf("the note does not name the path: %q", out)
	}
}

// TestMentionContentSanitized: an attached file's bytes are echoed
// straight into the transcript, so they are untrusted display text like
// any tool result.
func TestMentionContentSanitized(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)
	f := filepath.Join(dir, "hostile.txt")
	if err := os.WriteFile(f, []byte("pre\x1b]0;pwned\x07post"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := m.expandPastes("see @" + f)
	if strings.ContainsAny(out, "\x1b\x07") {
		t.Fatalf("an attached file carried control bytes into the frame: %q", out)
	}
	if !strings.Contains(out, "prepost") {
		t.Errorf("the file's content was lost: %q", out)
	}
}
