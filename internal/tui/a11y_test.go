package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Chmgx81/tilde/internal/tools"
)

// TestTypeAheadGuard: keys inside the guard window after a dialog
// opens are swallowed — a fast typist's stray "y" must not answer an
// approval they never read.
func TestTypeAheadGuard(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)

	// The permission dialog just opened.
	req := &permRequest{
		tool: "bash", tier: "action-allowed", args: `{"command": "ls"}`,
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

// everyGlyph is the whole vocabulary, by name, so a new glyph cannot be
// added without deciding its ASCII form: the list is the check.
func everyGlyph() map[string]string {
	return map[string]string{
		"brand": GlyphBrand, "shell": GlyphShell, "prompt": GlyphPrompt,
		"user": GlyphUser, "bullet": GlyphBullet, "branch": GlyphBranch,
		"caret": GlyphCaret, "ok": GlyphOK, "error": GlyphError,
		"warn": GlyphWarn, "info": GlyphInfo, "deleted": GlyphDeleted,
		"added": GlyphAdded, "doing": GlyphDoing, "todoOn": GlyphTodoOn,
		"todoOff": GlyphTodoOff, "queued": GlyphQueued, "update": GlyphUpdate,
		"mask": GlyphMask, "modePlan": GlyphModePlan, "modeBuild": GlyphModeBuild,
		"modeFullAuto": GlyphModeFullAuto, "thought": GlyphThought,
		"rule": GlyphRule, "sep": GlyphSep, "join": GlyphJoin,
	}
}

// TestEveryGlyphDegrades: every mark in the vocabulary has an ASCII
// form. The check is by name over the whole set, so a glyph added
// later without one fails here rather than in a screen reader.
func TestEveryGlyphDegrades(t *testing.T) {
	unicode := everyGlyph()
	adaptGlyphs(true)
	plain := everyGlyph()
	adaptGlyphs(false)

	for name := range unicode {
		p, ok := plain[name]
		if !ok {
			t.Errorf("glyph %q has no plain counterpart", name)
			continue
		}
		for _, r := range p {
			if r > 0x7f {
				t.Errorf("glyph %q: plain form %q still carries %q", name, p, r)
			}
		}
		if p == "" {
			t.Errorf("glyph %q disappears under --plain", name)
		}
	}
	if plain["sep"] == "" || plain["rule"] == "" {
		t.Error("the chrome marks have no ASCII form")
	}
}

// TestStateMarkersStayDistinct: the marks that encode a STATE — the
// task list's three, the ok/error/warn trio — must stay distinguishable
// from one another under --plain. Two states collapsing onto one ASCII
// string is a state the reader cannot read back, which is the whole
// reason the vocabulary exists. (Collisions ACROSS contexts — the
// prompt's ">" and the mode line's, say — are fine: a reader never has
// both in one glance.)
func TestStateMarkersStayDistinct(t *testing.T) {
	adaptGlyphs(true)
	defer adaptGlyphs(false)
	for _, group := range [][]string{
		{GlyphTodoOn, GlyphTodoOff, GlyphDoing},            // the task list
		{GlyphOK, GlyphError, GlyphWarn, GlyphInfo},        // verdicts
		{GlyphModePlan, GlyphModeBuild, GlyphModeFullAuto}, // the footer's mode
	} {
		seen := map[string]bool{}
		for _, g := range group {
			if seen[g] {
				t.Errorf("two state markers share the plain form %q", g)
			}
			seen[g] = true
		}
	}
}

// TestFrameIsAsciiUnderPlain: the whole rendered frame — a full
// transcript, every dialog, the composer, the footer — must carry no
// rune above 0x7f under the plain posture. The box borders and the
// composer's rules were the two that did not: lipgloss composes them,
// so the vocabulary never reached them.
func TestFrameIsAsciiUnderPlain(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)
	m.opt.Plain = true
	adaptGlyphs(true)
	t.Cleanup(func() { adaptGlyphs(false) })
	m.opt.UpdateTag = "v9.9.9"
	m.working = true
	m.workingSince = time.Now()
	m.effort = "high"
	m.overlayRows = 30
	m.todos = []tools.Todo{
		{Content: "done one", Status: tools.TodoDone},
		{Content: "doing one", Status: tools.TodoInProgress},
		{Content: "pending one", Status: tools.TodoPending},
	}
	m.composer.Prompt = GlyphBrand + " "
	m.syncComposerPrompt()

	// The states that need an open view before they can render at all.
	m.picker = newPicker(pickerModels, "switch model",
		[]pickerItem{{Label: "a-model", Detail: "a provider"}})
	m.composer.SetValue("/m")
	defer func() { m.composer.SetValue("") }()
	permRows, _, _ := m.permDialogRows(newPermReq("bash", `{"command":"npm init -y"}`), 80)
	states := map[string]string{
		"composer":   strings.Join(m.composerView(), "\n"),
		"permission": strings.Join(permRows, "\n"),
		"help":       strings.Join(m.helpRows(80), "\n"),
		"picker":     strings.Join(m.pickerRows(80), "\n"),
		"palette":    strings.Join(m.paletteRows(80), "\n"),
		"@ mention":  strings.Join(m.atMenuRows(80), "\n"),
		"todos":      strings.Join(m.todosView(), "\n"),
		"markdown": strings.Join(renderMarkdown(
			"## head\n\n- [x] done\n\n- a bullet\n\n| a | b |\n|---|---|\n| 1 | 2 |\n\n> q\n", 74), "\n"),
	}
	// Residuals, named rather than waved through: the truncation
	// ellipsis, which a screen reader reads as the punctuation it is,
	// and the startup banner, which is not part of these states. The
	// spec's "plain posture" section lists both; anything else above
	// 0x7f in the plain frame is a bug.
	const residual = "…"
	for name, frame := range states {
		for _, r := range stripANSI(frame) {
			if r > 0x7f && !strings.ContainsRune(residual, r) {
				t.Errorf("%s: the plain frame still carries %q", name, r)
			}
		}
	}
}

// TestPlainBorderAndUnicodeBorder: the border swap is in both
// directions, and the unicode one is the rounded frame the reference
// look is built on.
func TestPlainBorderAndUnicodeBorder(t *testing.T) {
	defer adaptGlyphs(false)
	adaptGlyphs(true)
	if got := dialogBorder().TopLeft; got != "+" {
		t.Errorf("plain border corner = %q, want +", got)
	}
	if got := dialogBorder().Left; got != "|" {
		t.Errorf("plain border left = %q, want |", got)
	}
	adaptGlyphs(false)
	if got := dialogBorder().TopLeft; got != "╭" {
		t.Errorf("unicode border corner = %q, want the rounded frame", got)
	}
}

// TestPlainOrPicksByPosture: the punctuation helper the few marks
// outside the vocabulary ask — an arrow pair, an em dash.
func TestPlainOrPicksByPosture(t *testing.T) {
	defer adaptGlyphs(false)
	adaptGlyphs(true)
	if got := plainOr("↑↓", "up/dn"); got != "up/dn" {
		t.Errorf("plainOr = %q, want up/dn", got)
	}
	adaptGlyphs(false)
	if got := plainOr("↑↓", "up/dn"); got != "↑↓" {
		t.Errorf("plainOr = %q, want the arrows", got)
	}
}

// TestAdaptGlyphs: the plain posture swaps every glyph for ASCII and
// back — nothing disappears.
func TestAdaptGlyphs(t *testing.T) {
	adaptGlyphs(true)
	defer adaptGlyphs(false)
	for _, g := range []string{
		GlyphOK, GlyphError, GlyphWarn, GlyphBranch, GlyphTodoOn,
		GlyphTodoOff, GlyphMask, GlyphUpdate,
	} {
		for _, r := range g {
			if r > 0x7f {
				t.Errorf("plain glyph %q still carries a non-ASCII rune", g)
			}
		}
	}
	if GlyphOK != "[ok]" || GlyphModeBuild != ">" {
		t.Errorf("plain glyphs = %q / %q, want [ok] / >", GlyphOK, GlyphModeBuild)
	}
	// The update badge is the newest glyph; it needs its own ASCII
	// form or the screen-reader posture loses the signal entirely.
	if GlyphUpdate != "^" {
		t.Errorf("plain update glyph = %q, want ^", GlyphUpdate)
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
