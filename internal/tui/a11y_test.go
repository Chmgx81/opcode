package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// TestTypeAheadGuard: keys inside the guard window after a dialog
// opens are swallowed — a fast typist's stray "y" must not answer an
// approval they never read.
func TestTypeAheadGuard(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)

	// The permission dialog just opened.
	req := &permRequest{
		tool: "run_shell", tier: "action-allowed", args: `{"command": "ls"}`,
		scope: "ls:*", sel: 2, openedAt: time.Now(),
		reply: make(chan bool, 1),
	}
	m.awaitingPerm = req
	m.Update(enterKey()) // an Enter typed inside the window
	select {
	case v := <-req.reply:
		t.Fatalf("the guarded key answered the dialog: %v", v)
	default:
	}
	if m.awaitingPerm == nil {
		t.Fatal("the dialog was consumed inside the guard window")
	}

	// Past the window, the same key answers.
	req.openedAt = time.Now().Add(-time.Second)
	m.Update(enterKey())
	select {
	case v := <-req.reply:
		if v {
			t.Error("enter on the preselected No must deny")
		}
	default:
		t.Error("the dialog never answered after the guard")
	}
}

// TestPlanTypeAheadGuard: the plan decision carries the same guard.
func TestPlanTypeAheadGuard(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)

	m.awaitingPlan = &planRequest{
		plan:     "do it",
		openedAt: time.Now(),
		reply:    make(chan planVerdict, 1),
	}
	m.Update(keyMsg("y"))
	select {
	case v := <-m.awaitingPlan.reply:
		t.Fatalf("guarded y answered the plan dialog: %+v", v)
	default:
	}
	if m.awaitingPlan == nil {
		t.Fatal("the plan dialog was consumed inside the guard window")
	}
}

// TestTranscriptPager: ctrl+o opens a pager over the whole
// conversation — committed entries included, results expanded —
// arrows scroll it, and esc closes.
func TestTranscriptPager(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)
	m.add(entry{kind: entryUser, text: "the question"})
	m.add(entry{kind: entryResult, tool: "read_file", summary: "the collapsed one-liner",
		full: "the full result text that ctrl+r would expand"})
	m.committed = 2 // both entries are in native scrollback already

	m.Update(tea.KeyMsg{Type: tea.KeyCtrlO})
	if !m.transcriptOpen {
		t.Fatal("ctrl+o did not open the transcript")
	}
	view := stripANSI(m.View())
	// The committed user block is readable again...
	if !strings.Contains(view, "the question") {
		t.Error("the transcript must show committed entries")
	}
	// ...and results are expanded without touching ctrl+r.
	if !strings.Contains(view, "the full result text") {
		t.Error("the transcript must expand results")
	}
	if strings.Contains(view, "ctrl+r to expand") {
		t.Error("the transcript left results collapsed")
	}
	if !strings.Contains(view, "transcript") {
		t.Error("the transcript header is missing")
	}

	// Scrolling moves the window.
	m.Update(tea.KeyMsg{Type: tea.KeyUp}) // no-op at the top: clamped
	if m.transcriptTop != 0 {
		t.Errorf("scrolling above the top moved to %d", m.transcriptTop)
	}
	m.transcriptTop = 5
	m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	if m.transcriptTop != 0 {
		t.Errorf("pgup from 5 went to %d, want 0 (clamped)", m.transcriptTop)
	}

	// Typing goes nowhere while it is open; esc closes it.
	m.composer.SetValue("typed into the void")
	m.Update(enterKey())
	if m.working {
		t.Error("enter submitted while the transcript owned the keyboard")
	}
	m.Update(escKey())
	if m.transcriptOpen {
		t.Error("esc did not close the transcript")
	}
}

// TestAdaptGlyphs: the plain posture swaps every glyph for ASCII and
// back — nothing disappears.
func TestAdaptGlyphs(t *testing.T) {
	adaptGlyphs(true)
	defer adaptGlyphs(false)
	for _, g := range []string{GlyphOK, GlyphError, GlyphWarn, GlyphBranch, GlyphTodoOn} {
		for _, r := range g {
			if r > 0x7f {
				t.Errorf("plain glyph %q still carries a non-ASCII rune", g)
			}
		}
	}
	if GlyphOK != "[ok]" || GlyphModeAsk != ">" {
		t.Errorf("plain glyphs = %q / %q, want [ok] / >", GlyphOK, GlyphModeAsk)
	}
}

// TestSanitizeTitle: control characters, bidi overrides, and
// oversized titles never reach the terminal's OSC surface.
func TestSanitizeTitle(t *testing.T) {
	if got := sanitizeTitle("tilde — a\x07b\u202ec d\u009b"); got != "tilde — abc d" {
		t.Errorf("sanitizeTitle = %q", got)
	}
	if got := sanitizeTitle(strings.Repeat("x", 300)); len([]rune(got)) != 240 {
		t.Errorf("cap = %d runes, want 240", len([]rune(got)))
	}
}
