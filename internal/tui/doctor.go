package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/Chmgx81/tilde/internal/config"
	"github.com/Chmgx81/tilde/internal/safe"
	"github.com/Chmgx81/tilde/internal/sandbox"
	"github.com/Chmgx81/tilde/internal/trust"
	"github.com/Chmgx81/tilde/internal/update"
)

// doctor renders a diagnostic report into the transcript: one line per
// subsystem, each with a verdict glyph and, when something is wrong,
// the next step in plain language. Every check reads live state through
// the same loaders the startup path uses — doctor presents verdicts,
// it never repairs anything, and it never prints a secret.
func (m *Model) doctor() {
	var rows []string
	ok := func(text string) { rows = append(rows, okStyle.Render(GlyphOK)+" "+text) }
	warn := func(text string) { rows = append(rows, warnStyle.Render(GlyphWarn)+" "+text) }
	fail := func(text string) { rows = append(rows, dangerStyle.Render(GlyphError)+" "+text) }
	neutral := func(text string) { rows = append(rows, dimStyle.Render(GlyphInfo)+" "+text) }

	ok("tilde " + m.displayVersion())

	if m.opt.Model != "" {
		ok("model " + m.opt.Model + dimStyle.Render("  via "+m.opt.ProviderName+" — "+m.opt.BaseURL))
	} else {
		fail(`no model configured — set "model" in ~/.tilde/config.json`)
	}

	// The key check reports presence only; the resolved key never
	// reaches a row. KeyFor runs the same resolution chain /models
	// uses (auth.json, env, !command — cached process-lifetime).
	if m.opt.KeyFor == nil {
		neutral("api key: resolution not wired")
	} else if _, has := m.opt.KeyFor(m.opt.ProviderName); has {
		ok("api key resolved for " + m.opt.ProviderName)
	} else {
		warn("no api key for " + m.opt.ProviderName + " — /login " + m.opt.ProviderName)
	}

	if m.opt.TildeHome != "" {
		m.doctorConfig(ok, neutral, fail)
	} else {
		neutral("config: tilde home not wired")
	}

	m.doctorSandbox(ok, warn, neutral)
	m.doctorTrust(ok, warn, neutral, fail)
	m.doctorSkillsAndMcp(ok, neutral)
	m.doctorAudit(ok, neutral)
	m.doctorUpdate(ok, warn, neutral)
	m.doctorTerminal(neutral)

	m.add(entry{kind: entryDim, text: strings.Join(rows, "\n")})
}

func (m *Model) doctorConfig(ok, neutral, fail func(string)) {
	cfgPath := filepath.Join(m.opt.TildeHome, "config.json")
	cfg, err := config.LoadConfig(m.opt.TildeHome)
	switch {
	case err != nil:
		fail(err.Error())
	case os.IsNotExist(statErr(cfgPath)):
		neutral("no config.json — defaults in effect (mode " + cfg.PermissionMode + ")")
	default:
		ok("config.json — mode " + cfg.PermissionMode)
	}

	modelsPath := filepath.Join(m.opt.TildeHome, "models.json")
	_, err = config.LoadModels(m.opt.TildeHome)
	switch {
	case err != nil:
		fail(err.Error())
	case os.IsNotExist(statErr(modelsPath)):
		neutral("no models.json — /model switching needs one")
	default:
		ok("models.json")
	}
}

func (m *Model) doctorSandbox(ok, warn, neutral func(string)) {
	switch {
	case !sandbox.Supported():
		neutral("sandbox: unavailable on this platform — shell commands run unsandboxed")
	case sandbox.Active():
		abi, _ := sandbox.ProbeABI()
		ok(fmt.Sprintf("sandbox: landlock v%d — writes confined to this directory, temp, and dev caches", abi))
	default:
		warn(`sandbox: off — "sandbox": true in config.json confines shell writes`)
	}
}

func (m *Model) doctorTrust(ok, warn, neutral, fail func(string)) {
	if m.opt.TildeHome == "" || m.opt.Cwd == "" {
		neutral("project trust: not wired")
		return
	}
	store, err := trust.LoadStore(m.opt.TildeHome)
	if err != nil {
		fail("trust store: " + err.Error())
		return
	}
	st, files, err := store.Status(m.opt.Cwd)
	if err != nil {
		fail("project trust: " + err.Error())
		return
	}
	if st == trust.Changed {
		warn(fmt.Sprintf("project trust: changed since approval (%d runnable files) — re-asked at next launch", len(files)))
		return
	}
	ok("project trust: " + st.String())
}

func (m *Model) doctorSkillsAndMcp(ok, neutral func(string)) {
	if m.opt.Skills == nil || len(m.opt.Skills.Names()) == 0 {
		neutral("skills: none loaded — add one under ~/.tilde/skills/ (a folder with SKILL.md)")
	} else {
		ok(fmt.Sprintf("skills: %d loaded — /skills lists them", len(m.opt.Skills.Names())))
	}
	if m.opt.MCPNames == nil {
		neutral("mcp: manager not wired")
	} else if names := m.opt.MCPNames(); len(names) == 0 {
		neutral("mcp: no servers connected — add one to ~/.tilde/mcp.json and restart tilde")
	} else {
		ok("mcp: " + strings.Join(names, " · "))
	}
}

func (m *Model) doctorAudit(ok, neutral func(string)) {
	switch {
	case m.opt.AuditPath == "":
		neutral("audit log: not wired")
	default:
		if fi, err := os.Stat(m.opt.AuditPath); err == nil {
			ok(fmt.Sprintf("audit log: %s (%s)", m.opt.AuditPath, humanBytes(int(fi.Size()))))
		} else {
			neutral("audit log: " + m.opt.AuditPath + " — created on the first gated action")
		}
	}
}

// updateState is what the update row has to report. The decision is
// kept apart from the copy so the whole matrix is table-testable
// without a TUI, and every state has exactly one honest row.
type updateState int

const (
	updateUnwired     updateState = iota // no tilde home: nothing to read
	updateDisabled                       // the user opted out
	updateUnsupported                    // no prebuilt release for this platform
	updateDevBuild                       // a build with no release version
	updateNeverRun                       // no check has ever been recorded
	updateCheckFailed                    // the last check failed
	updatePending                        // a strictly newer release is known
	updateUpToDate                       // the last check found this version
)

// updateStateOf reads the cache and the two opt-out flags — /doctor
// never phones home, so "the check failed" here means the recorded
// last attempt, not a new one. Order matters: the reasons the check
// does not run (disabled, unsupported, not a release) come first,
// because a cached tag cannot make any of them untrue.
func updateStateOf(current string, have bool, c update.Cache, enabled, supported bool) updateState {
	switch {
	case !enabled:
		return updateDisabled
	case !supported:
		return updateUnsupported
	case !update.IsRelease(current):
		return updateDevBuild
	case !have:
		return updateNeverRun
	case c.Err != "":
		return updateCheckFailed
	case update.Pending(current, c.Tag) != "":
		return updatePending
	default:
		return updateUpToDate
	}
}

// updateRowText is the update row's copy, one line per state. Split
// from the decision so every row is table-testable, and from the
// glyph so a fact and a verdict can move independently.
func updateRowText(state updateState, current string, c update.Cache) string {
	switch state {
	case updateUnwired:
		return "update check: tilde home not wired"
	case updateDisabled:
		return `update check: off — set "update_checks": true, or unset TILDE_NO_UPDATE_CHECK`
	case updateUnsupported:
		return "update check: no prebuilt tilde for " + runtime.GOOS + "/" + runtime.GOARCH +
			" — `tilde update` says how to install one"
	case updateDevBuild:
		return "update check: " + current + " is a development build — update checks need a tagged release"
	case updateNeverRun:
		return "update check: never run — `tilde update --check` reports the latest release"
	case updateCheckFailed:
		// The text came out of a network library, so it is sanitized
		// like any other untrusted display string. A failed refresh
		// does not un-publish the release tilde last saw, so that tag
		// still rides along — the note, the badge, and this row would
		// otherwise contradict each other.
		row := fmt.Sprintf("update check failed %s: %s — `tilde update --check` retries now",
			c.CheckedAt.Local().Format("2006-01-02 15:04"), safe.Text(c.Err))
		if tag := update.Pending(current, c.Tag); tag != "" {
			row += fmt.Sprintf("; %s is still the newest release tilde has seen", tag)
		}
		return row
	case updatePending:
		return update.Notice(current, c.Tag)
	default: // updateUpToDate
		return "tilde " + current + " is the latest release tilde has seen"
	}
}

// doctorUpdate reports the running version against the latest release
// tilde has seen. It reads the update-check cache only — the cache is
// the whole point of the check, and a diagnostic must not turn into a
// phone-home.
func (m *Model) doctorUpdate(ok, warn, neutral func(string)) {
	if m.opt.TildeHome == "" {
		neutral(updateRowText(updateUnwired, m.displayVersion(), update.Cache{}))
		return
	}
	current := m.displayVersion()
	c, have := update.ReadCache(update.CachePath(m.opt.TildeHome))
	// The config's own opt-out, not just the environment's: a user
	// who set "update_checks": false must not be nagged by a
	// diagnostic. A malformed config.json is reported by its own row
	// above, so here it falls back to the default.
	cfg, _ := config.LoadConfig(m.opt.TildeHome)
	state := updateStateOf(current, have, c,
		update.ChecksEnabled(cfg.UpdateChecks), update.SupportedPlatform(runtime.GOOS, runtime.GOARCH))

	text := updateRowText(state, current, c)
	switch state {
	case updateCheckFailed, updatePending:
		warn(text)
	case updateUpToDate:
		ok(text)
	default:
		neutral(text)
	}
}

func (m *Model) doctorTerminal(neutral func(string)) {
	profile := "no color"
	switch lipgloss.ColorProfile() {
	case termenv.TrueColor:
		profile = "truecolor"
	case termenv.ANSI256:
		profile = "256-color"
	case termenv.ANSI:
		profile = "ansi"
	}
	line := "terminal: " + envOr("TERM", "unset") + " · color " + profile
	if os.Getenv("NO_COLOR") != "" {
		line += " · NO_COLOR"
	}
	if m.opt.Plain {
		line += " · plain glyphs"
	}
	neutral(line)
}

func statErr(path string) error {
	_, err := os.Stat(path)
	return err
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
