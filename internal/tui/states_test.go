package tui

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Chmgx81/tilde/internal/config"
	"github.com/Chmgx81/tilde/internal/tools"
)

// The states a user lands in and cannot get out of by reading the
// frame: a first run, an empty list, a dead provider, a mistyped
// command, the screen-reader posture. Each of these has to say what
// happened and what to do next, in the same words everywhere.

// TestCommandsHelpAndSwitchAgree: the palette list, the help overlay
// and the dispatch switch are three copies of one truth. A command
// handled but unlisted is invisible; a listed but unhandled command
// answers "unknown command" to a name tilde just used in its own copy.
func TestCommandsHelpAndSwitchAgree(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)
	// The help sheet windows itself around helpTop, so it is read
	// through a real frame: View publishes the row budget, and a tall
	// terminal holds the whole sheet on one screen. The short-terminal
	// scroll is covered by TestHelpOverlay.
	m.height = 200
	m.View()
	help := stripANSI(strings.Join(m.helpRows(200), "\n"))
	for _, c := range commands {
		if !strings.Contains(help, c.Name) {
			t.Errorf("the help overlay omits the listed command %s", c.Name)
		}
		// Every listed command answers with something other than
		// "unknown command". The picker commands report through
		// state rather than an entry, so only the error text is a
		// meaningful signal here.
		m.entries = nil
		m.runCommand(c.Name, "")
		for _, e := range m.entries {
			if strings.Contains(e.text, "unknown command") {
				t.Errorf("the listed command %s is not handled: %q", c.Name, e.text)
			}
		}
	}
	// A description a user can act on: the palette is the only place
	// these are shown outside /help.
	for _, c := range commands {
		if strings.TrimSpace(c.Desc) == "" {
			t.Errorf("%s has no description in the palette", c.Name)
		}
	}
}

// TestErrorCopyNamesTheNextStep: the raw cause stays (a user pasting
// it into an issue needs the real text), and the shapes that come
// back most often get the one thing the message does not say. An
// unrecognized message passes through alone rather than guessing.
func TestErrorCopyNamesTheNextStep(t *testing.T) {
	cases := []struct{ err, want string }{
		{"401 Unauthorized", "/login"},
		{"invalid api key", "/login"},
		{"429 rate limit exceeded", "wait a moment"},
		{"402 insufficient credits", "out of credit"},
		{"Post \"https://api.test/v1\": dial tcp 1.2.3.4:443: connect: connection refused", "could not reach"},
		{"x509: certificate signed by unknown authority", "certificate"},
		{"the model gpt-9 does not exist", "/model"},
	}
	for _, c := range cases {
		got := errorWithNextStep("openrouter", c.err)
		if !strings.HasPrefix(got, "error: "+c.err) {
			t.Errorf("the cause was not kept verbatim: %q", got)
		}
		if !strings.Contains(got, c.want) {
			t.Errorf("%q -> %q, want it to name %q", c.err, got, c.want)
		}
		if !strings.Contains(got, "\nnext: ") {
			t.Errorf("%q -> %q, want the next step on its own line", c.err, got)
		}
	}
	// Nothing is invented for a message nobody recognizes.
	if got := errorWithNextStep("openrouter", "something nobody has seen"); got !=
		"error: something nobody has seen" {
		t.Errorf("an unknown error gained a guess: %q", got)
	}
	// The provider name comes from the session, not the message, and a
	// build without one still reads.
	if got := errorWithNextStep("", "401 Unauthorized"); !strings.Contains(got, "/login the provider") {
		t.Errorf("no provider name wired: %q", got)
	}
}

// TestEmptyStatesNameAWayForward: an empty list that says only "none"
// leaves the user guessing. Each of these must name where the thing
// comes from or how to make it.
func TestEmptyStatesNameAWayForward(t *testing.T) {
	dir := t.TempDir()

	t.Run("sessions", func(t *testing.T) {
		m, _ := newText(t, dir, nil)
		m.entries = nil
		m.openSessionsPicker()
		got := m.entries[len(m.entries)-1].text
		if !strings.Contains(got, "tilde saves a session when a turn ends") {
			t.Errorf("empty /sessions says nothing about how to get one: %q", got)
		}
	})

	t.Run("skills", func(t *testing.T) {
		m, _ := newText(t, dir, nil)
		m.entries = nil
		m.listSkills()
		got := m.entries[len(m.entries)-1].text
		if !strings.Contains(got, ".tilde/skills/") {
			t.Errorf("empty /skills does not say where skills live: %q", got)
		}
		// There is no view to close: this is a transcript entry.
		if strings.Contains(got, "esc closes") {
			t.Errorf("the empty state promises a view that does not exist: %q", got)
		}
	})

	t.Run("mcp", func(t *testing.T) {
		m, _ := newText(t, dir, nil)
		m.opt.MCPNames = func() []string { return nil }
		m.entries = nil
		m.listMcp()
		got := m.entries[len(m.entries)-1].text
		if !strings.Contains(got, "mcp.json") {
			t.Errorf("empty /mcp does not say where servers come from: %q", got)
		}
		if strings.Contains(got, "esc closes") {
			t.Errorf("the empty state promises a view that does not exist: %q", got)
		}
	})

	t.Run("model", func(t *testing.T) {
		m, _ := newText(t, dir, nil)
		m.opt.Models.Providers = map[string]config.ProviderConfig{}
		m.opt.SwitchModel = func(string, string) error { return nil }
		m.entries = nil
		m.openModelPicker()
		got := m.entries[len(m.entries)-1].text
		if !strings.Contains(got, "models.json") {
			t.Errorf("an empty /model names no file to edit: %q", got)
		}
	})

	t.Run("palette no match", func(t *testing.T) {
		m, _ := newText(t, dir, nil)
		m.entries = nil
		m.composer.SetValue("/zzz")
		got := stripANSI(strings.Join(m.paletteRows(60), "\n"))
		if !strings.Contains(got, "/zzz") {
			t.Errorf("the empty palette does not echo what was typed: %q", got)
		}
		if !strings.Contains(got, "/help") {
			t.Errorf("the empty palette names no way to see the list: %q", got)
		}
	})

	t.Run("mention no match", func(t *testing.T) {
		m, _ := newText(t, dir, nil)
		m.entries = nil
		m.composer.SetValue("@zzznope")
		m.refreshAtMenu()
		if m.atMenuOpen() {
			t.Error("a menu with no rows must not take the keyboard")
		}
		view := stripANSI(m.View())
		if !strings.Contains(view, "no file matches") {
			t.Errorf("an unmatched @ says nothing:\n%s", view)
		}
	})
}

// TestTodoPanelPlainSafe: the three task markers are a status, not
// decoration — a reader has to be able to tell done from pending
// without color. The pending marker was a literal middle dot, which
// --plain does not degrade and which says nothing about state.
func TestTodoPanelPlainSafe(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)
	m.todos = []tools.Todo{
		{Content: "done one", Status: tools.TodoDone},
		{Content: "doing one", Status: tools.TodoInProgress},
		{Content: "pending one", Status: tools.TodoPending},
	}
	unicodePending, asciiPending := GlyphTodoOff, ""
	plain := stripANSI(strings.Join(m.todosView(), "\n"))
	adaptGlyphs(true)
	defer adaptGlyphs(false)
	asciiPending = GlyphTodoOff
	ascii := stripANSI(strings.Join(m.todosView(), "\n"))
	if !utf8.ValidString(ascii) {
		t.Fatalf("the plain task panel is not valid UTF-8: %q", ascii)
	}
	for _, r := range ascii {
		if r > 0x7f {
			t.Errorf("the plain task panel still carries %q: %q", r, ascii)
		}
	}
	for _, want := range []string{"[x] done one", "@ doing one", "[ ] pending one"} {
		if !strings.Contains(ascii, want) {
			t.Errorf("plain panel missing %q:\n%s", want, ascii)
		}
	}
	if !strings.Contains(plain, unicodePending) {
		t.Errorf("the unicode panel missing the pending marker %q:\n%s", unicodePending, plain)
	}
	if asciiPending == "" || asciiPending == unicodePending {
		t.Errorf("the pending marker has no distinct plain form: %q", asciiPending)
	}
}

// TestLoginMaskIsPlainSafe: the mask hides a secret, and the plain
// posture is the screen reader. A bullet over the secret is both
// non-ASCII and read out loud character by character.
func TestLoginMaskIsPlainSafe(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)
	m.width = 80
	m.login = &loginFlow{provider: "openrouter"}
	m.composer.SetValue("sk-not-a-real-key")
	unicode := stripANSI(m.composerView()[2])
	if strings.Contains(unicode, "sk-not") {
		t.Error("the login flow echoed the key")
	}
	adaptGlyphs(true)
	defer adaptGlyphs(false)
	plain := stripANSI(m.composerView()[2])
	if strings.Contains(plain, "sk-not") {
		t.Errorf("the plain posture echoed the key: %q", plain)
	}
	if !strings.Contains(plain, strings.Repeat(GlyphMask, 15)) {
		t.Errorf("the plain mask is missing: %q", plain)
	}
	for _, r := range plain {
		if r > 0x7f {
			t.Errorf("the plain login row still carries %q: %q", r, plain)
		}
	}
}

// TestMarkdownMarksArePlainSafe: glamour composes a few marks of its
// own. A markdown task list used to draw the warning triangle on every
// unticked box, and the table rules and quote bars were hardcoded box
// drawing that --plain never reached.
func TestMarkdownMarksArePlainSafe(t *testing.T) {
	const src = "- [x] done\n- [ ] pending\n\n| a | b |\n|---|---|\n| 1 | 2 |\n\n> quoted\n"
	unicode := stripANSI(strings.Join(renderMarkdown(src, 74), "\n"))
	if strings.Contains(unicode, "⚠") {
		t.Errorf("an unticked box drew the warning glyph: %q", unicode)
	}
	if !strings.Contains(unicode, "☐") {
		t.Errorf("the unticked box has no marker: %q", unicode)
	}

	adaptGlyphs(true)
	defer adaptGlyphs(false)
	plain := stripANSI(strings.Join(renderMarkdown(src, 74), "\n"))
	if !utf8.ValidString(plain) {
		t.Fatalf("the plain markdown is not valid UTF-8: %q", plain)
	}
	for _, r := range plain {
		if r > 0x7f {
			t.Errorf("plain markdown still carries %q:\n%s", r, plain)
		}
	}
	if !strings.Contains(plain, "|") {
		t.Errorf("the plain table lost its rules:\n%s", plain)
	}
}

// TestHumanBytesSaysSomething: a size that rounds to "0 KiB" reads as
// a broken counter, and the clipboard toast is where a user looks to
// confirm the attach worked.
func TestHumanBytesSaysSomething(t *testing.T) {
	for _, c := range []struct {
		n    int
		want string
	}{
		{0, "0 B"}, {12, "12 B"}, {512, "512 B"},
		{1024, "1 KiB"}, {2048, "2 KiB"}, {8 << 20, "8.0 MiB"},
	} {
		if got := humanBytes(c.n); got != c.want {
			t.Errorf("humanBytes(%d) = %q, want %q", c.n, got, c.want)
		}
	}
}

// TestReasoningCountsCharacters: the receipt says "chars", and it
// counted bytes — a Japanese or emoji answer read as several times
// its own length.
func TestReasoningCountsCharacters(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)
	m.entries = nil
	m.add(entry{kind: entryReasoning, dur: "2s", text: strings.Repeat("é", 10)})
	got := stripANSI(strings.Join(m.renderEntry(&m.entries[0]), "\n"))
	if !strings.Contains(got, "10 chars") {
		t.Errorf("a 10-character answer reported as: %q", got)
	}
}
