package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/Chmgx81/opcode/internal/update"
)

func TestUnknownCommandHint(t *testing.T) {
	// Exact prefixes resolve to the first command carrying them.
	if got := unknownCommandHint("/mod"); got != "unknown command /mod — did you mean /mode?" {
		t.Errorf("hint = %q", got)
	}
	// Transpositions resolve by edit distance to a nearby command.
	if got := unknownCommandHint("/modle"); !strings.HasPrefix(got, "unknown command /modle — did you mean /mod") {
		t.Errorf("hint = %q, want a /mod… suggestion", got)
	}
	if got := unknownCommandHint("/sessions"); got != "" {
		t.Errorf("an exact command needs no hint, got %q", got)
	}
	if got := unknownCommandHint("/zzz-no-match"); got != "" {
		t.Errorf("nothing close means no hint, got %q", got)
	}
}

func TestUnknownSlashCommandStaysLocal(t *testing.T) {
	dir := t.TempDir()
	m, fp := newText(t, dir, nil)
	m.composer.SetValue("/modle")
	m.decideInput(false)
	if fp.requestCount() != 0 {
		t.Error("an unknown slash command must not start a model turn")
	}
	last := m.entries[len(m.entries)-1].text
	if !strings.Contains(last, "unknown command /modle") || !strings.Contains(last, "did you mean") {
		t.Errorf("last entry = %q, want the unknown-command error with a suggestion", last)
	}
}

// TestUpdateRedirectNamesTheShell: every update surface says "run
// `opcode update`", so typing it as a slash command gets the shell
// pointed at — not "unknown command", and not a real install.
func TestUpdateRedirectNamesTheShell(t *testing.T) {
	dir := t.TempDir()
	m, fp := newText(t, dir, nil)
	m.composer.SetValue("/update")
	m.decideInput(false)
	if fp.requestCount() != 0 {
		t.Error("/update must not start a model turn")
	}
	last := stripANSI(m.entries[len(m.entries)-1].text)
	if !strings.Contains(last, "`opcode update` runs in a shell") {
		t.Errorf("last entry = %q, want the shell redirect", last)
	}
	if strings.Contains(last, "unknown command") {
		t.Errorf("a documented command must not read as unknown: %q", last)
	}
}

// TestUpdateBadgeInFooter: the durable half of the signal. It shows
// only when a newer release is known, and it survives the reflow for as
// long as the terminal can hold it ALONGSIDE the mode — the mode is
// not droppable, because it is what every keystroke is scoped by, and a
// truncated badge with no mode on the line is a worse trade than a
// badge that waits for a wider terminal.
func TestUpdateBadgeInFooter(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)

	// Nothing known: the footer is exactly what it was before.
	if view := stripANSI(m.View()); strings.Contains(view, GlyphUpdate) {
		t.Errorf("no known update must not put a badge in the frame:\n%s", view)
	}
	if got := strings.TrimSpace(stripANSI(m.composerView()[len(m.composerView())-1])); !strings.HasSuffix(got, "commands") {
		t.Errorf("footer changed without an update: %q", got)
	}

	m.opt.UpdateTag = "v9.9.9"
	for _, w := range []int{100, 60, 40, 30, 24, 20} {
		m.width = w
		footer := stripANSI(m.composerView()[len(m.composerView())-1])
		if !strings.Contains(footer, GlyphUpdate+" v9.9.9") {
			t.Errorf("width %d: the update badge is missing from the footer: %q", w, footer)
		}
		// And the mode is on the same row at every one of these widths.
		if !strings.Contains(footer, m.opt.Mode) {
			t.Errorf("width %d: the mode line dropped the mode for the badge: %q", w, footer)
		}
	}
	// And at every width, badge or no badge, no footer row may be
	// wider than the terminal.
	for w := 10; w <= 120; w++ {
		m.width = w
		footer := m.composerView()[len(m.composerView())-1]
		if got := lipgloss.Width(footer); got > w {
			t.Errorf("width %d: the footer row is %d wide: %q", w, got, stripANSI(footer))
		}
	}
}

// TestUpdateHelpLine: the badge is unexplained chrome until /help says
// what it is, and the help line appears only when there is one.
func TestUpdateHelpLine(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)
	if help := stripANSI(strings.Join(m.helpRows(80), "\n")); strings.Contains(help, "a newer opcode is available") {
		t.Errorf("no update, no line in the help overlay:\n%s", help)
	}

	m.opt.UpdateTag = "v9.9.9"
	help := stripANSI(strings.Join(m.helpRows(100), "\n"))
	if !strings.Contains(help, GlyphUpdate+" v9.9.9") {
		t.Errorf("help must name the pending release:\n%s", help)
	}
	if !strings.Contains(help, "run `opcode update` to install it") {
		t.Errorf("help must name the way to act on it:\n%s", help)
	}
}

// TestUpdateBadgeIsPlainSafe: --plain is the screen-reader posture, so
// the badge must have an ASCII form like every other glyph.
func TestUpdateBadgeIsPlainSafe(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)
	m.opt.UpdateTag = "v9.9.9"
	adaptGlyphs(true)
	defer adaptGlyphs(false)

	if GlyphUpdate == "↑" {
		t.Error("the plain posture kept the Unicode badge")
	}
	for _, r := range GlyphUpdate {
		if r > 0x7f {
			t.Errorf("plain badge %q carries a non-ASCII rune", GlyphUpdate)
		}
	}
	m.width = 80
	footer := stripANSI(m.composerView()[len(m.composerView())-1])
	if !strings.Contains(footer, GlyphUpdate+" v9.9.9") {
		t.Errorf("footer = %q, want the ASCII badge", footer)
	}
	if help := stripANSI(strings.Join(m.helpRows(100), "\n")); !strings.Contains(help, GlyphUpdate+" v9.9.9") {
		t.Errorf("help must use the ASCII badge too:\n%s", help)
	}
}

func TestDoctorUpdateRow(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)
	m.opt.KeyFor = func(provider string) (string, bool) { return "k", true }

	// No cache yet: the row names the explicit check.
	if report := doctorText(t, m); !strings.Contains(report, "update check: never run — `opcode update --check`") {
		t.Errorf("no-cache row missing:\n%s", report)
	}

	// A cached newer release warns with the install step.
	update.WriteCache(update.CachePath(m.opt.OpcodeHome),
		update.Cache{Tag: "v9.9.9", CheckedAt: time.Now()})
	m.opt.Version = "v1.0.0"
	if report := doctorText(t, m); !strings.Contains(report, "Update available: v1.0.0 → v9.9.9. Run `opcode update`") {
		t.Errorf("stale row missing:\n%s", report)
	}

	// A cached equal release reads as up to date.
	update.WriteCache(update.CachePath(m.opt.OpcodeHome),
		update.Cache{Tag: "v1.0.0", CheckedAt: time.Now()})
	if report := doctorText(t, m); !strings.Contains(report, "is the latest release opcode has seen") {
		t.Errorf("current row missing:\n%s", report)
	}
}

// TestDoctorUpdateRowFailedCheck: a failed check is reported here and
// nowhere else — /doctor is the one surface allowed to say it.
func TestDoctorUpdateRowFailedCheck(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)
	m.opt.Version = "v1.0.0"
	update.WriteCache(update.CachePath(m.opt.OpcodeHome), update.Cache{
		Tag:       "v9.9.9",
		CheckedAt: time.Now(),
		Err:       "dial tcp 1.2.3.4:443: i/o timeout",
	})

	report := doctorText(t, m)
	if !strings.Contains(report, "update check failed") || !strings.Contains(report, "i/o timeout") {
		t.Errorf("the failure is not reported:\n%s", report)
	}
	// The release the last good check saw is still the truth.
	if !strings.Contains(report, "v9.9.9 is still the newest release") {
		t.Errorf("a failed refresh must not hide the known release:\n%s", report)
	}
	if !strings.Contains(report, "`opcode update --check` retries now") {
		t.Errorf("the failure names no way out:\n%s", report)
	}

	// A control character in a recorded error never reaches the frame.
	update.WriteCache(update.CachePath(m.opt.OpcodeHome), update.Cache{
		CheckedAt: time.Now(),
		Err:       "line one\x1b[2Jcleared",
	})
	report = doctorText(t, m)
	if strings.Contains(report, "\x1b") {
		t.Errorf("untrusted error text reached the frame unescaped: %q", report)
	}
}

// TestUpdateStateMatrix: one row per state, and the reasons the check
// does not run outrank whatever the cache happens to say.
func TestUpdateStateMatrix(t *testing.T) {
	now := time.Now()
	fresh := update.Cache{Tag: "v9.9.9", CheckedAt: now}
	failed := update.Cache{Tag: "v9.9.9", CheckedAt: now, Err: "offline"}
	cases := []struct {
		name      string
		current   string
		have      bool
		c         update.Cache
		enabled   bool
		supported bool
		want      updateState
		contains  string
	}{
		{"opted out", "v1.0.0", true, fresh, false, true, updateDisabled, "off"},
		{"no prebuilt", "v1.0.0", true, fresh, true, false, updateUnsupported, "no prebuilt opcode for"},
		{"dev build", "(devel)", true, fresh, true, true, updateDevBuild, "development build"},
		{"never ran", "v1.0.0", false, update.Cache{}, true, true, updateNeverRun, "never run"},
		{"check failed", "v1.0.0", true, failed, true, true, updateCheckFailed, "update check failed"},
		{"newer known", "v1.0.0", true, fresh, true, true, updatePending, "Update available: v1.0.0 → v9.9.9"},
		{"up to date", "v9.9.9", true, fresh, true, true, updateUpToDate, "is the latest release"},
		// A cached tag cannot make any of the "does not run" reasons
		// untrue, and a dev build is never told it is current.
		{"opted out beats a pending tag", "v1.0.0", true, fresh, false, true, updateDisabled, "off"},
		{"dev build is never up to date", "(devel)", true, fresh, true, true, updateDevBuild, "development build"},
	}
	for _, c := range cases {
		state := updateStateOf(c.current, c.have, c.c, c.enabled, c.supported)
		if state != c.want {
			t.Errorf("%s: updateStateOf = %d, want %d", c.name, state, c.want)
			continue
		}
		text := updateRowText(state, c.current, c.c)
		if !strings.Contains(text, c.contains) {
			t.Errorf("%s: row = %q, want it to contain %q", c.name, text, c.contains)
		}
	}
	// Every state has a row: an empty one would be a blank line.
	for state := updateUnwired; state <= updateUpToDate; state++ {
		if text := updateRowText(state, "v1.0.0", fresh); strings.TrimSpace(text) == "" {
			t.Errorf("state %d has no row", state)
		}
	}
}

// TestDoctorUpdateRowOptOut: "update_checks": false in config.json is
// the user's decision, and a diagnostic must respect it too.
func TestDoctorUpdateRowOptOut(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)
	m.opt.Version = "v1.0.0"
	update.WriteCache(update.CachePath(m.opt.OpcodeHome),
		update.Cache{Tag: "v9.9.9", CheckedAt: time.Now()})
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"update_checks": false}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if report := doctorText(t, m); !strings.Contains(report, `update check: off`) {
		t.Errorf("opted-out row missing:\n%s", report)
	}
}

// TestDoctorUpdateRowDevBuild: the row must not call a source build
// the latest release — that was the one dishonest thing it said.
func TestDoctorUpdateRowDevBuild(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)
	m.opt.Version = "(devel)"
	update.WriteCache(update.CachePath(m.opt.OpcodeHome),
		update.Cache{Tag: "v9.9.9", CheckedAt: time.Now()})

	report := doctorText(t, m)
	if strings.Contains(report, "(devel) is the latest release") {
		t.Errorf("a development build was called the latest release:\n%s", report)
	}
	if !strings.Contains(report, "development build") {
		t.Errorf("dev-build row missing:\n%s", report)
	}
}
