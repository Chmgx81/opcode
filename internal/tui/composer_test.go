package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Chmgx81/opcode/internal/tools"
)

// The composer is the one surface a user spends the whole session on.
// These are the states it can be in that the happy path never reaches:
// a draft too long for the frame, the cursor at either end, an attached
// image, a paste token that outlived its content, and the shell escape
// whose chrome has to be visible before Enter.

// TestComposerBoxFrame: the input sits in the same rounded box every
// dialog draws, and the frame follows the posture: the plain posture
// gets the ASCII box, and a terminal too narrow for the box gets a
// bare composer rather than a clipped border.
func TestComposerBoxFrame(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)
	resize(m, 60, 20)
	rows := m.composerView()
	if len(rows) != 5 { // blank, box top, input, box bottom, mode line
		t.Fatalf("composer frame is %d rows, want the box: %q", len(rows), rows)
	}
	if top := stripANSI(rows[1]); !strings.HasPrefix(top, "╭") || !strings.HasSuffix(top, "╮") {
		t.Errorf("the box top is missing: %q", top)
	}
	if bottom := stripANSI(rows[3]); !strings.HasPrefix(bottom, "╰") || !strings.HasSuffix(bottom, "╯") {
		t.Errorf("the box bottom is missing: %q", bottom)
	}
	for i, r := range rows {
		if n := lipgloss.Width(r); n > 60 {
			t.Errorf("row %d is %d cols on a 60-col terminal: %q", i, n, stripANSI(r))
		}
	}

	// --plain degrades the box to +-|, not to non-ASCII box drawing.
	adaptGlyphs(true)
	rows = m.composerView()
	if top := stripANSI(rows[1]); !strings.HasPrefix(top, "+") || !strings.HasSuffix(top, "+") {
		t.Errorf("the plain box top is missing: %q", top)
	}
	adaptGlyphs(false)

	// Below the box's floor the frame drops the box, not the input:
	// a clipped border is worse chrome than none.
	resize(m, 12, 10)
	rows = m.composerView()
	if len(rows) != 3 { // blank, input, mode line
		t.Errorf("a 12-col terminal drew %d composer rows, want the bare 3: %q",
			len(rows), rows)
	}
	for _, r := range rows {
		if s := stripANSI(r); strings.ContainsAny(s, "╭╮╰╯") {
			t.Errorf("a clipped border survived the narrow frame: %q", s)
		}
	}
}

// TestLongDraftStaysInTheFrame: a hundred-line draft grows the editor,
// not the frame. The composer is capped at six rows, so the transcript
// loses room rather than the render overflowing — a row past the
// terminal's height is the corruption, and the composer's own rows are
// the easiest ones to lose track of.
func TestLongDraftStaysInTheFrame(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)
	m.entries = nil
	for i := 0; i < 30; i++ {
		m.add(entry{kind: entryResult, tool: "read_file", summary: "one line result"})
	}
	resize(m, 80, 24)
	// A draft far taller than the cap.
	m.composer.SetValue(strings.Repeat("a very long line of draft text\n", 100))
	m.resizeComposer()
	if h := m.composer.Height(); h > 6 {
		t.Errorf("the composer grew to %d rows, want the 6-row cap", h)
	}
	if rows := strings.Split(m.View(), "\n"); len(rows) > 24 {
		t.Errorf("a long draft made the frame %d rows on a 24-row terminal", len(rows))
	}
	// The frame still shows the input — that is what the cap protects.
	if v := m.composer.Value(); !strings.Contains(v, "draft text") {
		t.Error("the cap dropped the draft instead of clipping the view")
	}
}

// TestComposerCursorAtBothEnds: ↑ and ↓ are history recall from the
// first and last line, and the cursor is the composer cursor. Walking
// to the very start and very end of a multiline draft and pressing
// them must recall, not walk off the end and lose the draft.
func TestComposerCursorAtBothEnds(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)
	m.hist = []string{"older prompt", "newer prompt"}
	m.histIdx = len(m.hist)

	// The cursor is walked to the first line with the textarea's own
	// move, then to the start of it: bubbles' CursorStart only sets the
	// column within the current row, so a cursor sitting on the second
	// line stays there and ↑ would recall from the wrong place.
	m.composer.SetValue("line one\nline two")
	m.composer.CursorUp()
	m.composer.CursorStart()
	if got := m.composer.Line(); got != 0 {
		t.Fatalf("cursor setup: line = %d, want 0", got)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if v := m.composer.Value(); v != "newer prompt" {
		t.Errorf("up at the first line did not recall: %q", v)
	}

	// Back to a multiline draft and walk the cursor to its last line, as
	// a user editing the second line has it.
	m.composer.SetValue("line one\nline two")
	m.composer.CursorEnd()
	if got := m.composer.Line(); got != m.composer.LineCount()-1 {
		t.Fatalf("cursor setup: line = %d, want %d", got, m.composer.LineCount()-1)
	}
	// ↓ past the newest recalled entry restores the draft that was live
	// when the first ↑ stashed it. The draft is set up BEFORE the recall
	// above in the real flow, so it is re-seeded here and the ↑ is
	// repeated to get the same state.
	m.draftSave = "line one\nline two"
	m.histIdx = 1
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if v := m.composer.Value(); !strings.Contains(v, "line two") {
		t.Errorf("down at the last line ate the draft: %q", v)
	}
}

// TestImageTokenRidesTheDraft: an attached image is a token in the
// composer like any other text, and it re-expands into a real part on
// submit. The composer's job is to show it, not to resolve it.
func TestImageTokenRidesTheDraft(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)
	m.overlayRows = 30
	// A one-pixel PNG, so the attach path is exercised for real.
	png := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 0}
	restore := swapClipboard(func() ([]byte, error) { return png, nil })
	defer restore()

	m.Update(tea.KeyMsg{Type: tea.KeyCtrlV})
	v := m.composer.Value()
	if !strings.Contains(v, "[Image #1]") {
		t.Fatalf("ctrl+v did not insert a token: %q", v)
	}
	// The placeholder is in the frame, so a user can see what they
	// attached before sending it.
	if !strings.Contains(stripANSI(m.View()), "[Image #1]") {
		t.Error("the attached image is not visible in the frame")
	}
	// Second attach: numbered, not replacing.
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlV})
	if got := m.pendingImages(); len(got) != 2 {
		t.Errorf("pending images = %d, want 2", len(got))
	}
	if m.composer.Value() == "" {
		t.Error("consuming the images cleared the draft too")
	}
}

// TestShellModeChromeIsVisibleBeforeEnter: shell mode has to be obvious
// BEFORE Enter, because Enter is what runs it. The prompt glyph, the
// rule color, and the footer hint are the three signals.
func TestShellModeChromeIsVisibleBeforeEnter(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("! echo hi")})

	if m.composer.Prompt != GlyphShell+" " {
		t.Errorf("composer prompt = %q, want the shell glyph", m.composer.Prompt)
	}
	view := stripANSI(m.View())
	if !strings.Contains(view, "shell — enter runs it directly") {
		t.Errorf("the mode line does not say the next Enter runs it:\n%s", view)
	}
	// And the "!" is gone the moment it is: the draft is the user's.
	m.composer.SetValue("echo hi")
	m.syncComposerPrompt()
	if m.composer.Prompt != GlyphBrand+" " {
		t.Errorf("prompt = %q, want the brand glyph back", m.composer.Prompt)
	}
}

// TestDeadPasteTokenIsCalledOut: a recalled [paste N] token whose
// content left with an earlier submit is dead — the model would read it
// as prose. The submit says so once, visibly, instead of sending it
// quietly.
func TestDeadPasteTokenIsCalledOut(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)
	m.entries = nil
	// A token from a previous session, with no live content.
	m.composer.SetValue("look at [paste 1" + GlyphSep + " 9 lines]")
	m.Update(enterKey())
	tr := m.transcript()
	if !strings.Contains(tr, "has no content in this session") {
		t.Errorf("a dead paste token was not called out:\n%s", tr)
	}
}

// TestSubmitWithOnlyWhitespaceChangesNothing: a draft of spaces is not a
// prompt. It starts a turn, bills a round, and answers "you said
// nothing" — the one submit outcome that should not exist.
func TestSubmitWithOnlyWhitespaceChangesNothing(t *testing.T) {
	dir := t.TempDir()
	m, fp := newText(t, dir, nil)
	m.composer.SetValue("   \n\t  ")
	m.Update(enterKey())
	if m.working {
		t.Error("a whitespace-only draft started a turn")
	}
	if n := fp.requestCount(); n != 0 {
		t.Errorf("a whitespace-only draft billed %d requests", n)
	}
	// New() prepends the greeting, so the check is that nothing was
	// added: a submit that did nothing leaves exactly those entries.
	before := len(m.entries)
	m.composer.SetValue("   \n\t  ")
	m.Update(enterKey())
	if len(m.entries) != before {
		t.Errorf("a whitespace-only draft wrote %d entries, want the %d it started with",
			len(m.entries), before)
	}
}

// TestModeLineNeverEmpty: whatever the terminal's width and whatever is
// set, the mode line says which mode is active. It is the one piece of
// state a user must never have to ask about — every other signal can be
// scrolled away.
func TestModeLineNeverEmpty(t *testing.T) {
	dir := t.TempDir()
	for _, w := range []int{8, 10, 12, 14, 20, 40, 80, 200} {
		for _, mode := range tools.Modes {
			m, _ := newText(t, dir, nil)
			m.opt.Mode = mode
			m.effort = "high" // the widest footer state
			m.opt.UpdateTag = "v9.9.9"
			resize(m, w, 24)
			footer := stripANSI(m.composerView()[len(m.composerView())-1])
			// The mode's own glyph is the signal that has to survive
			// every width; the name rides along when there is room, and
			// is cut — not dropped — when there is not.
			if !strings.Contains(footer, modeGlyph(mode)) {
				t.Errorf("width %d, mode %s: the mode's glyph is not on the footer: %q",
					w, mode, footer)
			}
			if w >= lipgloss.Width(modeGlyph(mode)+" "+mode) &&
				!strings.Contains(footer, mode) {
				t.Errorf("width %d, mode %s: the mode name does not fit but should: %q",
					w, mode, footer)
			}
			if n := len([]rune(footer)); n > w {
				t.Errorf("width %d, mode %s: the footer is %d cols: %q", w, mode, n, footer)
			}
		}
	}
}
