package tui

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// The frame is written one row at a time and bubbletea's inline
// renderer does not survive a row wider than the terminal. These
// assert the invariant across the widths a pane actually gets: a
// split terminal, a phone-sized ssh session, a normal 80, and a wide
// monitor.

// resize sends the WindowSizeMsg a real terminal would, so the
// composer is sized the way it is in production.
func resize(m *Model, w, h int) {
	m.Update(tea.WindowSizeMsg{Width: w, Height: h})
}

// widestRow is the visible width of the frame's longest row.
func widestRow(frame string) (int, string) {
	longest, row := 0, ""
	for _, l := range strings.Split(stripANSI(frame), "\n") {
		if n := lipgloss.Width(l); n > longest {
			longest, row = n, l
		}
	}
	return longest, row
}

// TestFrameFitsTerminalWidth: no row of a full transcript — a long
// tool call, a collapsed result, a file path in a header, an error —
// may run past the terminal. The tool line used to carry a fixed
// 70-column argument budget, which overflowed every terminal narrower
// than 80; the result line budgeted the width and forgot the indent
// and the hint.
func TestFrameFitsTerminalWidth(t *testing.T) {
	dir := t.TempDir()
	for _, w := range []int{14, 20, 30, 40, 60, 80, 200} {
		m, _ := newText(t, dir, nil)
		m.entries = nil
		m.add(entry{kind: entryUser, text: "a question long enough to want wrapping at every width tilde is run in"})
		m.add(entry{kind: entryTool, tool: "read_file",
			text: `{"path":"internal/tui/view.go","some":"argument padding to make this long"}`})
		m.add(entry{kind: entryResult, tool: "read_file",
			summary: strings.Repeat("result ", 40), full: strings.Repeat("full line of output\n", 30)})
		m.add(entry{kind: entryResult, tool: "edit_file",
			path: "internal/tui/a/very/deeply/nested/path/that/keeps/going/view.go",
			old:  "one\ntwo", new: "one\nthree"})
		m.add(entry{kind: entryResult, tool: "write_file",
			path: "internal/tui/另一个/深い/パス/file.go", full: "x\ny"})
		m.add(entry{kind: entryErr, text: errorWithNextStep("openrouter",
			`Post "https://api.example.test/v1/chat": dial tcp 1.2.3.4:443: connect: connection refused`)})
		m.add(entry{kind: entryAssistant, text: "## heading\n\ntext with **bold** and `code`\n\n- one\n- two\n"})
		// /diff renders its lines as composed, so a git line is as wide
		// as the file path in it — routinely past a split pane.
		m.add(entry{kind: entryDiff, text: wideDiff()})
		resize(m, w, 24)

		if n, row := widestRow(m.View()); n > w {
			t.Errorf("width %d: a row is %d wide: %q", w, n, row)
		}
	}
}

// TestRenderedEntriesFitOnTheirOwn: View clips every row as a last
// line of defence, which hides a per-entry overflow that is still an
// overflow — the clip is a backstop, not the design. Each entry is
// therefore also measured where it is built.
func TestRenderedEntriesFitOnTheirOwn(t *testing.T) {
	dir := t.TempDir()
	long := strings.Repeat("x", 200)
	m, _ := newText(t, dir, nil)
	m.entries = []entry{
		{kind: entryDiff, text: wideDiff()},
		{kind: entryResult, tool: "edit_file", path: "a.go", old: long, new: long + "y"},
		{kind: entryResult, tool: "write_file", path: "a.go", full: long + "\n" + long},
		{kind: entryResult, tool: "read_file", summary: "a summary", full: long + "\n" + long},
	}
	for _, w := range []int{14, 30, 60, 120} {
		m.width = w
		for i := range m.entries {
			for _, l := range m.renderEntry(&m.entries[i]) {
				if n := lipgloss.Width(l); n > w {
					t.Errorf("width %d, entry %d: a rendered row is %d wide", w, i, n)
				}
			}
		}
	}
}

// wideDiff is a git diff as wide as real ones get: a deep path, a
// function context after the hunk header, a long added line.
func wideDiff() string {
	return strings.Join(renderDiffBody(
		"diff --git a/internal/tui/view.go b/internal/tui/view.go\n"+
			"index 1234567..89abcde 100644\n--- a/internal/tui/view.go\n"+
			"+++ b/internal/tui/view.go\n"+
			"@@ -1,40 +1,42 @@ func (m *Model) View() string {\n"+
			"+\t// a very long added line that goes past forty columns\n"), "\n")
}

// TestEveryOverlayFitsTerminalHeight: the floating blocks are protected
// from the trim, so nothing downstream can pull one back into a short
// terminal. Every one of them is a protected layer that used to render
// at its natural height — the permission dialog and the help sheet both
// outgrew a 24-row pane, which is the corruption. Each block now sizes
// itself from the room View published.
func TestEveryOverlayFitsTerminalHeight(t *testing.T) {
	dir := t.TempDir()
	overlays := map[string]func(*Model){
		"permission": func(m *Model) { m.awaitingPerm = newPermReq("bash", `{"command":"git status"}`) },
		"trust": func(m *Model) {
			m.awaitingTrust = &TrustDecision{Approved: []string{"a.sh"}}
		},
		"plan":  func(m *Model) { m.awaitingPlan = &planRequest{plan: "p"} },
		"login": func(m *Model) { m.login = &loginFlow{provider: "openrouter"} },
		"picker": func(m *Model) {
			m.picker = newPicker(pickerModels, "switch model", []pickerItem{{Label: "a", Detail: "b"}})
		},
		"palette":    func(m *Model) { m.composer.SetValue("/m") },
		"@ mention":  func(m *Model) { m.composer.SetValue("@"); m.refreshAtMenu() },
		"help":       func(m *Model) { m.openHelp() },
		"transcript": func(m *Model) { m.transcriptOpen = true },
		// Nothing open: the composer is the whole protected tail.
		"bare": func(m *Model) {},
	}
	for name, open := range overlays {
		for _, h := range []int{8, 10, 14, 20, 24, 40} {
			m, _ := newText(t, dir, nil)
			m.entries = nil
			for i := 0; i < 40; i++ {
				m.add(entry{kind: entryResult, tool: "read_file", summary: "one line result"})
			}
			resize(m, 80, h)
			open(m)
			if rows := strings.Split(m.View(), "\n"); len(rows) > h {
				t.Errorf("%s at height %d: the frame is %d rows", name, h, len(rows))
			}
		}
	}
}

// TestPermissionDialogKeepsItsOptions: the dialog's options and key
// hints are pinned through the block's trim. A resize that cut them
// left a box the user could not answer, which is the one failure a
// permission dialog may not have — so at every height, all three
// options and the keys are on screen.
func TestPermissionDialogKeepsItsOptions(t *testing.T) {
	dir := t.TempDir()
	for _, h := range []int{8, 10, 14, 20, 24, 40} {
		m, _ := newText(t, dir, nil)
		m.entries = nil
		m.add(entry{kind: entryResult, tool: "read_file", summary: "one line result"})
		resize(m, 80, h)
		m.awaitingPerm = newPermReq("bash", `{"command":"npm init -y"}`)
		view := stripANSI(m.View())
		for _, want := range []string{"npm init -y", "1. Yes", "3. No", "esc cancels"} {
			if !strings.Contains(view, want) {
				t.Errorf("height %d: the dialog lost %q:\n%s", h, want, view)
			}
		}
	}
}

// TestExpandedResultsFitTerminalWidth: ctrl+r's expanded views render
// the file's own lines, and a source line is whatever the file holds —
// a minified bundle, a base64 blob, a long literal. The diff hunk rows
// are not wrapped (a wrapped diff line stops reading as one), so they
// are clipped instead; the plain expanded result wraps with its indent
// counted OUTSIDE the budget, because wrapAll's ten-column floor
// otherwise made the row overflow below sixteen columns.
func TestExpandedResultsFitTerminalWidth(t *testing.T) {
	dir := t.TempDir()
	long := strings.Repeat("x", 200)
	for _, w := range []int{14, 20, 30, 40, 60, 80, 200} {
		m, _ := newText(t, dir, nil)
		m.expandResults = true
		m.entries = []entry{
			{kind: entryResult, tool: "edit_file", path: "a.go",
				old: long, new: long + "y"},
			{kind: entryResult, tool: "write_file", path: "a.go",
				full: long + "\n" + long},
			{kind: entryResult, tool: "read_file", summary: "a summary",
				full: long + "\n" + long},
		}
		m.width = w
		for i := range m.entries {
			for _, l := range m.renderEntry(&m.entries[i]) {
				if n := lipgloss.Width(l); n > w {
					t.Errorf("width %d, %s: an expanded row is %d wide",
						w, m.entries[i].tool, n)
				}
			}
		}
		// The verdict markers survive the clip — they are at column
		// zero, which is the whole reason clipping is the right loss.
		hunk := strings.Join(m.renderEntry(&m.entries[0]), "\n")
		if !strings.Contains(hunk, GlyphAdded) || !strings.Contains(hunk, GlyphDeleted) {
			t.Errorf("width %d: the clip took a verdict marker:\n%s", w, hunk)
		}
	}
}

// TestFrameFitsTerminalHeight: the transcript trims, the composer does
// not, and the trim marker says how many ROWS went — one dropped
// layer can be a twelve-row markdown block, so counting layers there
// read as a lie.
func TestFrameFitsTerminalHeight(t *testing.T) {
	dir := t.TempDir()
	for _, h := range []int{8, 10, 14, 20, 40} {
		m, _ := newText(t, dir, nil)
		m.entries = nil
		for i := 0; i < 40; i++ {
			m.add(entry{kind: entryResult, tool: "read_file", summary: "one line result"})
		}
		resize(m, 60, h)

		rows := strings.Split(m.View(), "\n")
		if len(rows) > h {
			t.Errorf("height %d: the frame is %d rows", h, len(rows))
		}
		if !strings.Contains(rows[0], "earlier lines") {
			continue
		}
		// The marker counts what it actually cut: 40 one-line results
		// in a 14-row window leaves 40 - (kept) rows above.
		cut := 0
		for _, l := range rows[1:] {
			if strings.Contains(l, "one line result") {
				cut++
			}
		}
		if want := 40 - cut; !strings.Contains(rows[0], itoa(want)+" earlier lines") {
			t.Errorf("height %d: marker says %q, want %d earlier lines", h, rows[0], want)
		}
	}
}

// TestFrameFillsTheTerminal: trimming used to reserve a fixed eight
// rows for a composer block that spends five, so a tall terminal threw
// away transcript it had room to show.
func TestFrameFillsTheTerminal(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)
	m.entries = nil
	for i := 0; i < 40; i++ {
		m.add(entry{kind: entryResult, tool: "read_file", summary: "one line result"})
	}
	resize(m, 60, 20)
	rows := strings.Split(m.View(), "\n")
	if len(rows) != 20 {
		t.Errorf("a 20-row terminal got a %d-row frame; the transcript is trimmed too early", len(rows))
	}
}

// TestTranscriptPagerFitsTerminal: the pager is a protected layer, so
// nothing downstream can trim it back into shape — it has to budget
// for the frame itself. Its rows are rendered at the terminal's width
// inside a box narrower than that, which lipgloss then re-wrapped,
// and the overlay came out nearly twice the window it claimed.
func TestTranscriptPagerFitsTerminal(t *testing.T) {
	dir := t.TempDir()
	for _, h := range []int{14, 20, 40} {
		for _, w := range []int{30, 60, 120} {
			m, _ := newText(t, dir, nil)
			m.entries = nil
			for i := 0; i < 40; i++ {
				m.add(entry{kind: entryResult, tool: "read_file", summary: "one line result"})
			}
			resize(m, w, h)
			m.transcriptOpen = true
			frame := m.View()
			if rows := len(strings.Split(frame, "\n")); rows > h {
				t.Errorf("pager at %dx%d: the frame is %d rows", w, h, rows)
			}
			if n, row := widestRow(frame); n > w {
				t.Errorf("pager at %dx%d: a row is %d wide: %q", w, h, n, row)
			}
		}
	}
}

// TestStatusAndFooterReflow: the working line and the mode line are
// the two fixed chrome rows. Both are built from candidates and both
// used to hand the terminal a row it could not show — the status line
// was 75 columns at every width, the mode line kept the effort dial
// past 16 columns.
func TestStatusAndFooterReflow(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)
	m.working = true
	m.workingSince = time.Now().Add(-3 * time.Minute)
	m.workingVerb = "Pondering…"
	m.usage.PromptTokens, m.usage.CompletionTokens = 12000, 3400
	m.effort = "high"
	m.opt.UpdateTag = "v9.9.9"
	// Twelve columns is the floor: the composer's own minimum width is
	// ten plus its prompt, and a terminal under that cannot hold a
	// prompt at all.
	for _, w := range []int{12, 14, 20, 30, 45, 60, 80} {
		resize(m, w, 24)
		rows := m.composerView()
		for i, l := range rows {
			if n, row := widestRow(l); n > w {
				t.Errorf("width %d: composer row %d is %d wide: %q", w, i, n, row)
			}
		}
	}
	// The elapsed time survives the reflow — it is the one number a
	// long turn needs.
	resize(m, 24, 24)
	if !strings.Contains(stripANSI(m.composerView()[1]), "3m0s") {
		t.Errorf("narrow status line lost the elapsed time: %q", stripANSI(m.composerView()[1]))
	}
}

// TestTruncateIsSafe: truncate is handed a budget derived from the
// terminal width, so the budget can go negative on a split pane — and
// slicing bytes there both panicked and cut multi-byte runes in half,
// which the terminal renders as a replacement character.
func TestTruncateIsSafe(t *testing.T) {
	for _, n := range []int{0, -1, -20} {
		if got := truncate("a summary", n); got != "" {
			t.Errorf("truncate(n=%d) = %q, want empty", n, got)
		}
	}
	for _, c := range []string{"日本語のテキストです", "héllo wörld", "ascii only"} {
		got := truncate(c, 5)
		if !utf8.ValidString(got) {
			t.Errorf("truncate(%q, 5) = %q, which is not valid UTF-8", c, got)
		}
		if lipgloss.Width(got) > 5 {
			t.Errorf("truncate(%q, 5) = %q, %d columns wide", c, got, lipgloss.Width(got))
		}
	}
	if got := truncate("short", 40); got != "short" {
		t.Errorf("a budget wider than the text must not shorten it: %q", got)
	}
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)
	for _, w := range []int{8, 12, 14} {
		m.width = w
		m.entries = nil
		m.add(entry{kind: entryResult, tool: "read_file", summary: "a summary of the result"})
		// The result line budgeted width-14; at these widths that is
		// negative, and the slice panicked.
		if got := m.renderResult(m.entries[0], w); got == nil {
			t.Errorf("width %d: no rows rendered", w)
		}
	}
}

// TestWrapAllBreaksUnbreakableWords: a path, a URL, or a sha has no
// space to break at. Word wrapping left them whole, so one long token
// overflowed the frame at every width.
func TestWrapAllBreaksUnbreakableWords(t *testing.T) {
	long := "https://example.test/" + strings.Repeat("a", 200)
	for _, w := range []int{20, 40, 80} {
		for _, l := range wrapAll("see "+long+" for details", w) {
			if n, row := widestRow(l); n > w {
				t.Errorf("width %d: row is %d wide: %q", w, n, row)
			}
		}
	}
	lines := wrapAll(long, 20)
	if len(lines) < 10 {
		t.Errorf("a 200-column token at width 20 should split into many lines, got %d", len(lines))
	}
	if strings.Join(lines, "") != long {
		t.Error("splitting a long token lost or invented characters")
	}
	// Word wrapping still happens where there are spaces.
	if got := wrapAll("one two three four five six", 12); len(got) < 2 {
		t.Errorf("expected several lines, got %q", got)
	}
}

// TestClipColsKeepsEscapes: the styled rows are mostly SGR escapes, so
// the clipper that keeps them inside the terminal has to skip over
// them and count only the visible columns.
func TestClipColsKeepsEscapes(t *testing.T) {
	styled := okStyle.Render("✓ a styled line that is far too wide for the row")
	for _, w := range []int{4, 10, 20} {
		got := clipCols(styled, w)
		if n := lipgloss.Width(got); n > w {
			t.Errorf("width %d: clipped to %d columns", w, n)
		}
		if w > 0 && lipgloss.Width(stripANSI(got)) != w {
			t.Errorf("width %d: kept %d visible columns, want a full %d",
				w, lipgloss.Width(stripANSI(got)), w)
		}
	}
	if got := clipCols("plain", 10); got != "plain" {
		t.Errorf("a short line must pass through: %q", got)
	}
	if got := clipCols("plain", 0); got != "" {
		t.Errorf("width 0 must clip everything: %q", got)
	}
}

// itoa keeps the height assertion readable.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var d []byte
	for ; n > 0; n /= 10 {
		d = append([]byte{byte('0' + n%10)}, d...)
	}
	return string(d)
}
