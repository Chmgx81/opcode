package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/Chmgx81/tilde/internal/update"
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

func TestDoctorUpdateRow(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)
	m.opt.KeyFor = func(provider string) (string, bool) { return "k", true }

	// No cache yet: the row names the explicit check.
	if report := doctorText(t, m); !strings.Contains(report, "update check: never run — `tilde update --check`") {
		t.Errorf("no-cache row missing:\n%s", report)
	}

	// A cached newer release warns with the install step.
	update.WriteCache(update.CachePath(m.opt.TildeHome), "v9.9.9", time.Now())
	m.opt.Version = "v1.0.0"
	if report := doctorText(t, m); !strings.Contains(report, "Update available: v1.0.0 → v9.9.9. Run `tilde update`") {
		t.Errorf("stale row missing:\n%s", report)
	}

	// A cached equal release reads as up to date.
	update.WriteCache(update.CachePath(m.opt.TildeHome), "v1.0.0", time.Now())
	if report := doctorText(t, m); !strings.Contains(report, "is the latest release tilde has seen") {
		t.Errorf("current row missing:\n%s", report)
	}
}
