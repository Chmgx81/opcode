package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// restoreTheme puts the default palette back; theme tests mutate the
// package-level tokens, and leaking a theme into other tests would
// color their assertions.
func restoreTheme(t *testing.T) {
	t.Helper()
	t.Cleanup(func() { applyThemeName("dark") })
}

func TestApplyThemeNameSwapsTokens(t *testing.T) {
	restoreTheme(t)
	if !applyThemeName("green") {
		t.Fatal("green is a valid theme")
	}
	if HexAccent != "#16db65" || HexDeep2 != "#0d2818" {
		t.Errorf("green tokens not installed: accent %s deep2 %s", HexAccent, HexDeep2)
	}
	if curPalette != "green" {
		t.Errorf("curPalette = %q, want green", curPalette)
	}
	if !ValidTheme("light") || ValidTheme("neon") {
		t.Errorf("ValidTheme: light=%v neon=%v", ValidTheme("light"), ValidTheme("neon"))
	}
	before := HexAccent
	if applyThemeName("neon") {
		t.Error("unknown theme reported success")
	}
	if HexAccent != before {
		t.Error("failed apply changed the palette")
	}
}

// TestThemePickerPreviewAndRestore: opening sets the backup, moving
// the highlight re-skins live, and a close without selection puts the
// backup back — a cancelled preview never strands its palette.
func TestThemePickerPreviewAndRestore(t *testing.T) {
	restoreTheme(t)
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)
	m.curTheme = "dark"

	m.openThemePicker()
	if m.picker == nil || m.picker.kind != pickerThemes {
		t.Fatal("theme picker did not open")
	}
	if m.themeBackup != "dark" {
		t.Errorf("backup = %q, want dark", m.themeBackup)
	}

	// dark, light, green: two downs land on green, and the preview
	// applies it live.
	m.picker.down()
	m.picker.down()
	m.themePreview()
	if m.curTheme != "green" || HexAccent != "#16db65" {
		t.Errorf("preview did not apply green: cur=%s accent=%s", m.curTheme, HexAccent)
	}

	// Esc: the picker closes with nothing selected.
	m.picker = nil
	m.themePreview()
	if m.curTheme != "dark" || HexAccent != "#2dd4bf" {
		t.Errorf("cancel did not restore dark: cur=%s accent=%s", m.curTheme, HexAccent)
	}
	last := m.entries[len(m.entries)-1]
	if !strings.Contains(last.text, "theme restored") {
		t.Errorf("restore note = %q", last.text)
	}
	if m.themeBackup != "" {
		t.Error("backup not cleared after restore")
	}
}

// TestThemePickerSelectPersists: Enter applies and calls SetTheme with
// the chosen name; the note says it was saved.
func TestThemePickerSelectPersists(t *testing.T) {
	restoreTheme(t)
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)
	m.curTheme = "dark"
	saved := ""
	m.opt.SetTheme = func(name string) error { saved = name; return nil }

	m.pickerSelect(pickerItem{Label: "green", Theme: "green"})
	if HexAccent != "#16db65" || m.curTheme != "green" {
		t.Errorf("selection not applied: cur=%s accent=%s", m.curTheme, HexAccent)
	}
	if saved != "green" {
		t.Errorf("SetTheme = %q, want green", saved)
	}
	last := m.entries[len(m.entries)-1]
	if !strings.Contains(last.text, "saved to config.json") {
		t.Errorf("saved note = %q", last.text)
	}
}

// TestThemeSessionOnlyWithoutWriter: no SetTheme wired — the switch
// applies and the note says session-only instead of lying.
func TestThemeSessionOnlyWithoutWriter(t *testing.T) {
	restoreTheme(t)
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)
	m.curTheme = "dark"

	m.selectTheme("light")
	if m.curTheme != "light" {
		t.Fatalf("theme not applied: %s", m.curTheme)
	}
	last := m.entries[len(m.entries)-1]
	if !strings.Contains(last.text, "this session only") {
		t.Errorf("session-only note = %q", last.text)
	}
}

// TestThemePickerEmptyMatchRestores: Enter with zero matches selects
// nothing — the restore path must run, not strand the preview.
func TestThemePickerEmptyMatchRestores(t *testing.T) {
	restoreTheme(t)
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)
	m.curTheme = "dark"
	m.openThemePicker()

	// Simulate the real flow: preview applied, then Enter closes the
	// picker with no selection (no matches).
	m.applyTheme("green")
	m.picker = nil
	m.themePreview()
	if m.curTheme != "dark" {
		t.Errorf("empty-match close stranded the preview: %s", m.curTheme)
	}
}

// TestThemeDirectCommand: /theme <name> switches and persists; an
// unknown name is an error entry, not a silent no-op.
func TestThemeDirectCommand(t *testing.T) {
	restoreTheme(t)
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)
	m.curTheme = "dark"
	saved := ""
	m.opt.SetTheme = func(name string) error { saved = name; return nil }

	m.handleThemeCommand("green")
	if m.curTheme != "green" || saved != "green" {
		t.Errorf("direct switch failed: cur=%s saved=%s", m.curTheme, saved)
	}

	m.handleThemeCommand("neon")
	if m.curTheme != "green" {
		t.Errorf("unknown theme mutated the palette: %s", m.curTheme)
	}
	last := m.entries[len(m.entries)-1]
	if last.kind != entryErr || !strings.Contains(last.text, "unknown theme") {
		t.Errorf("unknown-theme entry = %+v", last)
	}
}

// TestApplyThemeClearsRenderCaches: markdown renders embed the old
// palette's ANSI codes; a switch must drop them so the next frame
// re-renders in the new colors.
func TestApplyThemeClearsRenderCaches(t *testing.T) {
	restoreTheme(t)
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)
	m.entries = append(m.entries, entry{kind: entryAssistant, text: "hi", rendered: []string{"x"}, renderedW: 80})
	m.streamRendered, m.streamRenderedLen, m.streamRenderedW = []string{"y"}, 1, 80

	m.applyTheme("light")
	if m.entries[0].rendered != nil || m.entries[0].renderedW != 0 {
		t.Error("entry render cache survived the switch")
	}
	if m.streamRendered != nil || m.streamRenderedLen != 0 {
		t.Error("stream render cache survived the switch")
	}
}

// TestComposerPromptFollowsTheme: bubbles' textarea keeps its active
// style as a pointer into the struct it was focused on — a copied
// model (New returns by value) renders prompt-style writes in the
// construction-time color until the pointer is re-seated. Found live
// in a PTY: a persisted green startup still showed a teal prompt.
func TestComposerPromptFollowsTheme(t *testing.T) {
	restoreTheme(t)
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(termenv.Ascii) })
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)

	if !m.applyTheme("green") {
		t.Fatal("green is a valid theme")
	}
	v := m.composer.View()
	if strings.Contains(v, "44;211;191") {
		t.Errorf("composer still renders the old accent after the switch: %q", v)
	}
	if !strings.Contains(v, "22;219;101") {
		t.Errorf("prompt not green after the switch: %q", v)
	}
}
