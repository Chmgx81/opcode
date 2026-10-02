package tui

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"

	"github.com/Chmgx81/opcode/internal/safe"
	"github.com/Chmgx81/opcode/internal/tools"
)

// View implements tea.Model: identity block and timeline, a working
// status line while a turn runs, the composer with a mode line under
// it, and floating layers (palette, help) that fit the terminal.
func (m *Model) View() string {
	// The last line of defence, and the only one that cannot be
	// forgotten: whatever the layers above composed, no row of the
	// frame may be wider than the terminal. Every renderer clips or
	// wraps its own rows, and this catches the one that does not — a
	// row past the edge is what corrupts bubbletea's inline renderer,
	// and it corrupts the whole app, not one line of it.
	out := m.view()
	if m.width <= 0 {
		return out
	}
	rows := strings.Split(out, "\n")
	for i, l := range rows {
		rows[i] = clipCols(l, m.width)
	}
	return strings.Join(rows, "\n")
}

func (m *Model) view() string {
	tl := m.timelineView()
	cl := m.composerView()
	// The room the floating blocks get, decided before they are drawn so
	// each one can size itself.
	//
	// Two rows are held back: the blank between the transcript and the
	// block, and the trim marker fit prepends when the transcript does
	// not fit. Without the second one the protected tail was exactly the
	// terminal's height and the marker row pushed the frame one over.
	//
	// When the composer and the open modal cannot both fit, the composer
	// goes: it is the block the user cannot act on while a modal owns
	// the keyboard, and it is protected from the trim by design, so
	// leaving it there meant the frame outgrew the terminal by exactly
	// its height — which is the corruption. The draft is untouched in
	// the model and comes back when the dialog closes.
	room := m.height - len(cl) - 2
	if m.modalOpen() && room < m.modalMinRows() {
		cl, room = nil, m.height-2
	}
	// Published for the block renderers, which floatingView and the
	// tests both call directly. A field rather than a parameter on each
	// of them: the budget is a property of the frame, and threading it
	// through six renderers would be six chances to compute it
	// differently.
	m.overlayRows = room
	fl := m.floatingView()

	// The floating and composer layers are protected: the transcript
	// trims from the front, the dialog and composer never do.
	layers := append(append(tl, fl...), cl...)
	return strings.Join(m.fit(layers, len(fl)+len(cl)), "\n")
}

// modalOpen reports whether a floating block has the keyboard — a
// dialog, a picker, the palette, the pager, or the help sheet. Only
// these justify hiding the composer.
func (m *Model) modalOpen() bool {
	return m.transcriptOpen || m.picker != nil || m.helpOpen ||
		m.awaitingPerm != nil || m.awaitingPlan != nil ||
		m.awaitingTrust != nil || m.login != nil || m.paletteOpen()
}

// modalMinRows is what the open modal needs to be usable: its own
// content floor plus the blank line and the two borders in front of it.
// View asks before it draws, because whether the composer survives is
// the difference between a dialog that can be answered and one that
// cannot.
func (m *Model) modalMinRows() int {
	need := 0
	switch {
	case m.awaitingPerm != nil:
		need = permMinRows
	case m.helpOpen:
		need = 4
	case m.picker != nil, m.atQueryLive():
		need = 2
	default:
		// The single-line prompts, and the pager (one line is a page,
		// and the arrows move it): the minimum is the minimum.
		need = 1
	}
	return need + 3
}

// timelineView renders the live region: entries not yet committed
// to native scrollback. Every user query and every finished answer
// opens with a blank line — conversation blocks breathe — and
// collapseBlanks keeps that to a single gap no matter what glamour
// or the greeting emit around them.
func (m *Model) timelineView() []string {
	var out []string
	// Breathing space: one blank line before every top-level block —
	// a user turn, an answer, a tool group, a receipt — while an
	// action and its own result stay tight. collapseBlanks trims any
	// doubling, so entries may be added freely. The rhythm starts
	// from the last committed entry so it continues across the seam.
	prevKind := entryKind(-1)
	prevSub := ""
	if m.committed > 0 && m.committed <= len(m.entries) {
		prevKind = m.entries[m.committed-1].kind
		prevSub = m.entries[m.committed-1].subTitle
	}
	for i := range m.entries {
		if i < m.committed {
			continue
		}
		e := &m.entries[i]
		if blankBefore(e, prevKind, prevSub) {
			out = append(out, "")
		}
		out = append(out, m.renderEntry(e)...)
		prevKind, prevSub = e.kind, e.subTitle
	}
	// In-flight assistant text: rendered as markdown live, identical
	// to what the flushed entry will show.
	if s := strings.TrimSpace(m.stream.String()); s != "" {
		out = append(out, "")
		out = append(out, m.renderStream(s, m.termWidth())...)
	}
	// The live task list — state at the tail, not history.
	if len(m.todos) > 0 {
		out = append(out, "")
		out = append(out, m.todosView()...)
	}
	// The model's thinking, live: a dim tail so the user sees the
	// reasoning stream without it burying the transcript.
	if s := strings.TrimSpace(m.reasoning.String()); s != "" {
		out = append(out, "")
		out = append(out, m.reasoningLiveView()...)
	}
	return collapseBlanks(out)
}

// blankBefore decides whether a blank line separates this entry
// from the previous one. The rhythm: user turns, answers, plans,
// reasoning receipts, and compaction notes open fresh blocks; a
// tool group opens one when it follows anything but tool activity;
// subagent lines group by title. Results never add space above
// themselves — they belong to the action above them.
func blankBefore(e *entry, prev entryKind, prevSub string) bool {
	switch e.kind {
	case entryUser, entryAssistant, entryPlan, entryReasoning, entryCompaction:
		return true
	case entryTool:
		return prev != entryTool && prev != entryResult
	case entrySubagent:
		return prev != entrySubagent || prevSub != e.subTitle
	}
	return false
}

// reasoningLiveView shows the thinking stream as a dim italic tail —
// the last few lines only; thinking is atmosphere, not content.
func (m *Model) reasoningLiveView() []string {
	head := dimStyle.Render(GlyphThought + " thinking" + plainOr("…", "..."))
	lines := strings.Split(strings.TrimRight(m.reasoning.String(), "\n"), "\n")
	const keep = 3
	if len(lines) > keep {
		lines = lines[len(lines)-keep:]
	}
	w := m.termWidth()
	out := []string{clipCols(head, w)}
	for _, l := range lines {
		// Wrapped plain, styled after: wrapAll re-joins the words of a
		// styled line with spaces and counts the escape bytes as text,
		// so a long thought came out with its italic codes stranded.
		out = appendWrapped(out, "  "+l, w, dimStyle.Italic(true))
	}
	return out
}

// todosView renders the model's live task list: a count header, then
// the items — ☑ done, ◐ in progress, ☐ pending — windowed so a long
// list cannot eat the screen. Every marker comes from the vocabulary,
// so the plain posture degrades all three (a literal "·" did not).
func (m *Model) todosView() []string {
	done := 0
	for _, it := range m.todos {
		if it.Status == tools.TodoDone {
			done++
		}
	}
	head := accentStyle.Render(GlyphBullet+" tasks") +
		dimStyle.Render(fmt.Sprintf(" (%d/%d done)", done, len(m.todos)))
	out := []string{clipCols(head, maxInt(m.termWidth(), 1))}

	const rows = 8
	shown, extra := m.todos, 0
	if len(shown) > rows {
		// The tail of the list is where the live work is, so the
		// window keeps the last rows and the count says how many
		// it left above.
		cut := len(shown) - rows
		shown, extra = shown[cut:], cut
	}
	// The two-space indent, the marker, and its space are four columns
	// the wrap budget must not also spend. wrapAll has a ten-column
	// floor of its own, so below fourteen the row is clipped rather
	// than trusted: a row wider than the pane is the corruption.
	tw := m.termWidth()
	w := maxInt(tw-4, 1)
	for _, it := range shown {
		var marker, text string
		switch it.Status {
		case tools.TodoDone:
			marker, text = dimStyle.Render(GlyphTodoOn), dimStyle.Strikethrough(true).Render(it.Content)
		case tools.TodoInProgress:
			marker, text = accentStyle.Render(GlyphDoing), it.Content
		default:
			marker, text = dimStyle.Render(GlyphTodoOff), dimStyle.Render(it.Content)
		}
		for _, l := range wrapAll(text, w) {
			out = append(out, clipCols("  "+marker+" "+l, tw))
		}
	}
	if extra > 0 {
		out = append(out, clipCols(dimStyle.Render(
			fmt.Sprintf("  … %d earlier tasks", extra)), tw))
	}
	return out
}

// collapseBlanks trims runs of blank lines to a single blank and drops
// leading/trailing blanks. Glamour pads lines with styled spaces, so
// blankness is measured after stripping ANSI.
func collapseBlanks(lines []string) []string {
	out := make([]string, 0, len(lines))
	isBlank := func(s string) bool { return strings.TrimSpace(stripANSI(s)) == "" }
	for _, l := range lines {
		if isBlank(l) && (len(out) == 0 || isBlank(out[len(out)-1])) {
			continue
		}
		out = append(out, l)
	}
	for len(out) > 0 && isBlank(out[len(out)-1]) {
		out = out[:len(out)-1]
	}
	return out
}

// stripANSI removes SGR escape sequences; used for blank-line
// detection over styled renderer output.
func stripANSI(s string) string {
	var b strings.Builder
	inEsc := false
	for _, r := range s {
		if r == 0x1b {
			inEsc = true
			continue
		}
		if inEsc {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEsc = false
			}
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// sanitizeTitle makes a string safe for the terminal window title:
// control characters, C1 bytes, and bidi/isolating overrides are
// dropped, and the result is capped at 240 runes. OSC titles are an
// untrusted-text injection surface — a crafted directory name must
// not hijack the window title (codex-tui-audit notable 13).
func sanitizeTitle(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			continue
		}
		switch r {
		case 0x202a, 0x202b, 0x202c, 0x202d, 0x202e, // bidi overrides
			0x2066, 0x2067, 0x2068, 0x2069: // isolating marks
			continue
		}
		b.WriteRune(r)
	}
	out := strings.TrimSpace(b.String())
	if runes := []rune(out); len(runes) > 240 {
		return string(runes[:240])
	}
	return out
}

// renderEntry maps one structured entry to view lines. Assistant text
// renders as markdown, cached per width on the entry: View runs every
// frame and re-running glamour per frame would visibly cost.
// expandingResults reports whether tool results and thinking render
// expanded: the ctrl+r toggle, or the ctrl+O transcript pager, whose
// whole point is the full detail.
func (m *Model) expandingResults() bool {
	return m.expandResults || m.transcriptOpen
}

func (m *Model) renderEntry(e *entry) []string {
	w := m.termWidth()
	switch e.kind {
	case entryUser:
		// The echoed query sits in a subtle background panel with a
		// bold-dim › prefix — Codex's separation between what you
		// said and what the agent answered, without dimming the text.
		// Wrapped plain and styled after: the panel pads to the
		// terminal width, and a styled word count would mismeasure.
		lines := wrapAll(GlyphUser+" "+e.text, w)
		for i := range lines {
			lines[i] = dimStyle.Render(lines[i])
		}
		return []string{userPanel(lines, m.termWidth())}
	case entryAssistant:
		if e.rendered == nil || e.renderedW != w {
			e.rendered = renderMarkdown(e.text, w)
			e.renderedW = w
		}
		return e.rendered
	case entryTool:
		// The args share the row with the glyph and the tool name, so
		// the budget is what is left of the terminal. A fixed 70 here
		// overflowed every terminal narrower than 80, and one row too
		// wide is what corrupts bubbletea's inline renderer.
		glyph, name := GlyphBullet+" ", e.tool
		budget := w - lipgloss.Width(glyph) - len(name) - 1
		return []string{clipCols(accentStyle.Render(glyph)+
			toolNameStyle.Render(name)+dimStyle.Render(" "+truncate(e.text, budget)), w)}
	case entryResult:
		return m.renderResult(*e, w)
	case entryOK:
		return wrapAll(okStyle.Render(GlyphOK+" ")+e.text, w)
	case entryErr:
		return wrapAll(dangerStyle.Render(GlyphError+" ")+e.text, w)
	case entryDim:
		return wrapAll(e.text, w)
	case entrySteer:
		return wrapAll(steerStyle.Render("(steering) ")+e.text, w)
	case entryQueued:
		return wrapAll(queuedStyle.Render(GlyphQueued+" ")+e.text, w)
	case entryCompaction:
		// The marker rides the first row, not the whole block: styling
		// the note and then wrapping the note with the text left the
		// escape codes to be re-split across the rows.
		lines := wrapAll(e.text, w)
		for i := range lines {
			if i == 0 {
				lines[i] = infoStyle.Render("… ") + lines[i]
				continue
			}
			lines[i] = infoStyle.Render(lines[i])
		}
		return lines
	case entrySubagent:
		return wrapAll(dimStyle.Render(e.subTitle)+e.text, w)
	case entryReasoning:
		// Collapsed: one dim line — the thinking is atmosphere.
		// ctrl+r (the results toggle) expands the text, windowed.
		head := dimStyle.Render(GlyphThought) + dimStyle.Render(
			fmt.Sprintf(" thought for %s "+GlyphSep+" %d chars", e.dur, utf8.RuneCountInString(e.text)))
		if !m.expandingResults() {
			return []string{head + dimStyle.Render("  (ctrl+r to expand)")}
		}
		// Thinking is plain text: it is wrapped and THEN styled, per
		// row. Wrapping a styled string measures the escape bytes as
		// part of the first word and re-joins the pieces, which left
		// the SGR codes dangling at the start of the wrong row.
		out := []string{head}
		const rows = 12
		it := dimStyle.Italic(true)
		lines := strings.Split(strings.TrimRight(e.text, "\n"), "\n")
		if len(lines) > rows {
			extra := len(lines) - rows
			lines = lines[:rows]
			for _, l := range lines {
				out = appendWrapped(out, "  "+l, w, it)
			}
			return append(out, it.Render(fmt.Sprintf("  … %d more lines", extra)))
		}
		for _, l := range lines {
			out = appendWrapped(out, "  "+l, w, it)
		}
		return out
	case entryPlan:
		// The presented plan: a labeled markdown block in the
		// transcript; the decision line follows below it.
		out := []string{accentStyle.Render(GlyphBullet + " plan")}
		return append(out, renderMarkdown(e.text, w)...)
	case entryDiff:
		// /diff pre-colors git's unified diff line by line; the lines
		// render as composed (no re-wrap — a wrapped diff line stops
		// reading as a diff) but are clipped to the terminal, which
		// is not a choice: a row wider than the pane is what corrupts
		// bubbletea's renderer, and a diff is mostly wider than a
		// split pane.
		out := strings.Split(e.text, "\n")
		for i, l := range out {
			out[i] = clipCols(l, w)
		}
		return out
	}
	return nil
}

// renderResult shows a tool result: one collapsed line by default,
// full text when ctrl+r expanded. edit_file results render as a diff
// hunk with colored − / + lines either way.
func (m *Model) renderResult(e entry, w int) []string {
	prefix := dimStyle.Render("  " + GlyphBranch + " ")
	expandHint := dimStyle.Render("  (ctrl+r to expand)")
	if e.tool == "write_file" && e.path != "" && e.full != "" {
		lines := strings.Split(strings.TrimRight(e.full, "\n"), "\n")
		head := headWithPath(prefix, "wrote ", e.path,
			okStyle.Render(fmt.Sprintf(" %s%d", GlyphAdded, len(lines))), expandHint, w)
		if !m.expandingResults() {
			return []string{clipCols(head+expandHint, w)}
		}
		out := []string{head}
		const rows, indent = 10, 5
		l := lexerFor(e.path)
		shown := lines
		extra := 0
		if len(shown) > rows {
			extra = len(shown) - rows
			shown = shown[:rows]
		}
		// Clipped like the edit_file hunks below, and for the same
		// reason: a written file's line is as long as the file wants it
		// to be, and a diff row is not wrapped.
		for _, src := range shown {
			out = append(out, clipCols(
				okStyle.Render(strings.Repeat(" ", indent)+GlyphAdded+" ")+
					highlightLine(src, l), w))
		}
		if extra > 0 {
			out = append(out, clipCols(
				dimStyle.Render(fmt.Sprintf("      … %d more lines", extra)), w))
		}
		return out
	}
	if e.tool == "edit_file" && e.path != "" {
		adds, dels := diffCounts(e.old, e.new)
		head := headWithPath(prefix, "updated ", e.path,
			okStyle.Render(fmt.Sprintf(" %s%d", GlyphAdded, adds))+" "+
				dangerStyle.Render(fmt.Sprintf("%s%d", GlyphDeleted, dels)), expandHint, w)
		if !m.expandingResults() {
			return []string{clipCols(head+expandHint, w)}
		}
		out := []string{head}
		// Syntax-highlighted hunks: tokens carry language colors, the
		// − / + markers and indent carry the verdict.
		//
		// The line is highlighted, then CLIPPED. A source line is
		// whatever the file happens to hold — a minified bundle, a base64
		// blob, a long string literal — and a diff row is not wrapped
		// (a wrapped diff line stops reading as one), so without the
		// clip a 200-character line made a 207-column row and corrupted
		// the frame. The verdict marker is at column zero, so the clip
		// costs only the tail of the code.
		const indent = 5
		lexer := lexerFor(e.path)
		mark := func(style lipgloss.Style, glyph, src string) string {
			return clipCols(
				style.Render(strings.Repeat(" ", indent)+glyph+" ")+
					highlightLine(src, lexer), w)
		}
		for _, l2 := range strings.Split(e.old, "\n") {
			out = append(out, mark(dangerStyle, GlyphDeleted, l2))
		}
		for _, l3 := range strings.Split(e.new, "\n") {
			out = append(out, mark(okStyle, GlyphAdded, l3))
		}
		return out
	}
	if m.expandingResults() && e.full != "" {
		// The six-space indent is outside the wrap budget, so the
		// budget is the terminal less the indent and the row is clipped
		// as well: wrapAll has a ten-column floor of its own, and below
		// sixteen the floor alone made the row overflow.
		const indent = 6
		out := []string{clipCols(prefix+dimStyle.Render(truncate(e.summary, w-4)), w)}
		for _, l := range wrapAll(e.full, w-indent) {
			out = append(out, clipCols(dimStyle.Render(strings.Repeat(" ", indent)+l), w))
		}
		return out
	}
	hint := ""
	if e.summary != "" {
		hint = expandHint
	}
	// The summary shares the row with the indent and the hint, so the
	// budget is what is left of the terminal, not the whole of it.
	budget := w - lipgloss.Width(prefix) - lipgloss.Width(hint)
	return []string{clipCols(prefix+resultStyle.Render(truncate(e.summary, budget))+hint, w)}
}

// headWithPath assembles a one-line result header, shrinking the path
// to the room left after the indent, the verb, the verdict and the
// expand hint. A header wider than the terminal wraps into the next
// row and corrupts the frame, and the path is the only part of it
// that can give way without losing the verdict — so below that room
// the row is clipped rather than the verdict dropped.
func headWithPath(prefix, verb, path, verdict, hint string, w int) string {
	fixed := lipgloss.Width(prefix) + len(verb) +
		lipgloss.Width(verdict) + lipgloss.Width(hint)
	head := prefix + dimStyle.Render(verb+truncate(path, maxInt(w-fixed, 0))) + verdict
	return clipCols(head, w)
}

// renderStream renders the in-flight assistant text as markdown, the
// same renderer the flushed entry uses — the stream IS the finished
// look, so completing a message never reflows it (the reference
// products' newline-gated streaming, taken to its conclusion). The
// render caches on the Model and re-renders only when content or
// width changed, so View's per-frame pass costs nothing.
func (m *Model) renderStream(s string, w int) []string {
	if m.streamRenderedLen != len(s) || m.streamRenderedW != w {
		m.streamRendered = renderMarkdown(s, w)
		m.streamRenderedLen = len(s)
		m.streamRenderedW = w
	}
	return m.streamRendered
}

// userPanel paints the user's echoed query with the deep-fill
// background, padded to the terminal width so it reads as one panel.
//
// Clipped as well as padded: wrapAll has a ten-column floor of its own,
// so a panel narrower than that came out a few columns too wide, and a
// row past the terminal's edge is the corruption this package keeps
// preventing everywhere else.
func userPanel(lines []string, w int) string {
	panel := lipgloss.NewStyle().Background(Deep2)
	var out []string
	for _, l := range lines {
		l = clipCols(l, w)
		pad := maxInt(w-lipgloss.Width(l), 0)
		out = append(out, panel.Render(l+strings.Repeat(" ", pad)))
	}
	return strings.Join(out, "\n")
}

// floatingView renders the trust prompt, permission prompt, login
// prompt, palette, toast, and help overlay. Every block draws itself
// through floatingBlock, so the frame's row budget is spent in one
// place and the blocks cannot disagree about it.
func (m *Model) floatingView() []string {
	var out []string
	w := m.termWidth()

	// minContent is the block's own floor: the rows it cannot be
	// trimmed below without losing the thing it exists to say. Below
	// that the block drops its border rather than its content.
	if m.transcriptOpen {
		out = append(out, m.floatingBlock(paletteStyle, m.transcriptRows(w), w, 0, 0, 1)...)
	}
	if m.awaitingTrust != nil {
		// The keys ride their own row: folded into the sentence they
		// were the first thing lipgloss's re-wrap pushed off the
		// bottom, and a trust prompt whose answer keys are missing is
		// a prompt the user has to guess at.
		files := strings.Join(m.awaitingTrust.Approved, ", ")
		out = append(out, m.floatingBlock(promptBoxStyle, append(
			wrapAll("trust this project? opcode would be able to run: "+files, w-8),
			dimStyle.Render("y trust "+GlyphSep+" n or esc decline")), w, 1, 0, 2)...)
	}
	if m.awaitingPerm != nil {
		rows, keepHead, keepTail := m.permDialogRows(m.awaitingPerm, w)
		out = append(out, m.floatingBlock(promptBoxStyle, rows, w,
			keepHead, keepTail, permMinRows)...)
	}
	if m.awaitingPlan != nil {
		out = append(out, m.floatingBlock(promptBoxStyle, []string{
			promptStyle.Render("proceed with this plan?") + " " +
				dimStyle.Render("y implement (actions will ask) "+GlyphSep+
					" a implement with auto-accept "+GlyphSep+" n/Esc keep planning"),
		}, w, 0, 0, 1)...)
	}
	if m.login != nil {
		out = append(out, m.floatingBlock(promptBoxStyle, []string{
			promptStyle.Render("API key for "+m.login.provider) +
				dimStyle.Render(" — input hidden, Enter to save, Esc to cancel"),
		}, w, 0, 0, 1)...)
	}
	if m.paletteOpen() {
		out = append(out, m.floatingBlock(paletteStyle, m.paletteRows(w), w, 0, 0, 1)...)
	}
	if m.picker != nil {
		out = append(out, m.floatingBlock(paletteStyle, m.pickerRows(w), w, 0, 0, 2)...)
	}
	if m.atQueryLive() {
		out = append(out, m.floatingBlock(paletteStyle, m.atMenuRows(w), w, 0, 0, 2)...)
	}
	if m.helpOpen {
		out = append(out, m.floatingBlock(helpStyle, m.helpRows(w), w, 0, 0, 4)...)
	}
	if m.toast != "" && time.Since(m.toastAt) < 4*time.Second {
		glyph, style := GlyphOK, okStyle
		if m.toastAnim > 0 {
			glyph = toastFrames[len(toastFrames)-m.toastAnim]
			style = accentStyle
		}
		out = append(out, "", style.Render(glyph+" "+m.toast))
	}
	return out
}

// transcriptRows is the ctrl+O pager's block: the whole conversation —
// committed and live entries alike, results and thinking expanded —
// windowed around transcriptTop. The block self-bounds through
// floatingBlock, so it can never push the composer off-screen.
func (m *Model) transcriptRows(w int) []string {
	lines := m.transcriptLines()
	// The window is the frame's own room for this block, less the blank
	// above it, the two borders, the header, and up to two markers. View
	// already decided whether the composer survives, so the room is the
	// one it published rather than a second guess here.
	visible := m.blockRoom() - 2 // the header and one marker
	if visible < 1 {
		// One readable line beats none.
		visible = 1
	}
	if maxTop := len(lines) - visible; m.transcriptTop > maxTop {
		m.transcriptTop = maxInt(maxTop, 0)
	}
	shown := []string{accentStyle.Render(GlyphBullet+" transcript") +
		dimStyle.Render("  "+plainOr("↑↓", "up/dn")+" scroll "+GlyphSep+
			" pgup/pgdn "+GlyphSep+" ctrl+o or esc close")}
	if m.transcriptTop > 0 {
		shown = append(shown, dimStyle.Render(
			fmt.Sprintf("  … %d lines above", m.transcriptTop)))
	}
	end := m.transcriptTop + visible
	if end > len(lines) {
		end = len(lines)
	}
	// The body is rendered at the terminal's width, the box at the
	// terminal's width less its border and padding. Left alone,
	// lipgloss re-wraps every one of those rows inside the box and the
	// overlay comes out nearly twice as tall as the window it claims.
	inner := w - 8
	for _, l := range lines[m.transcriptTop:end] {
		shown = append(shown, clipCols(l, inner))
	}
	if end < len(lines) {
		shown = append(shown, dimStyle.Render(
			fmt.Sprintf("  … %d lines below", len(lines)-end)))
	}
	return shown
}

// transcriptLines renders the entire conversation with the breathing
// rhythm — committed entries included, since the pager's whole point
// is reading what scrolled away. Expansion follows expandingResults,
// which is true while the pager is open.
func (m *Model) transcriptLines() []string {
	var out []string
	prevKind := entryKind(-1)
	prevSub := ""
	for i := range m.entries {
		e := &m.entries[i]
		if blankBefore(e, prevKind, prevSub) {
			out = append(out, "")
		}
		out = append(out, m.renderEntry(e)...)
		prevKind, prevSub = e.kind, e.subTitle
	}
	if s := strings.TrimSpace(m.stream.String()); s != "" {
		out = append(out, "")
		out = append(out, m.renderStream(s, m.termWidth())...)
	}
	return collapseBlanks(out)
}

// composerView renders the working status line, the composer box, and
// the mode line under it.
func (m *Model) composerView() []string {
	var out []string

	// Working line in the reference apps' shape: spinner, a gerund,
	// then how long and how much, with esc to stop. Enter steers and
	// alt+enter queue live in the help overlay, not here — the line
	// stays a feeling, not a legend.
	if m.working {
		elapsed := time.Since(m.workingSince).Round(time.Second)
		tokens := m.usage.PromptTokens + m.usage.CompletionTokens
		verb := m.workingVerb
		if verb == "" {
			verb = "Thinking…"
		}
		sp := m.spinner.View()
		// Reduced motion — and the plain posture, which implies it:
		// an explicit "animations": true under --plain still gets
		// the static glyph, because braille spinners are not ASCII.
		if !m.opt.Animations || m.opt.Plain {
			sp = GlyphBullet
		}
		// Codex's status shape: verb, then one parenthesized segment
		// with elapsed, interrupt, and token flow inside it. It
		// reflows the way the mode line below does: the full form
		// first, then the parts that must never be lost, then the
		// verb alone. A status line wider than the terminal corrupts
		// the frame and takes the composer down with it.
		seg := dimStyle.Render(fmt.Sprintf(" (%s %s esc to interrupt %s %s %s tokens)",
			elapsed, GlyphSep, GlyphSep, plainOr("↓", "down"), humanCount(tokens)))
		head := accentStyle.Render(verb)
		// A shell command reads by its own name, not a gerund: what is
		// running is the thing the user typed.
		if m.shell != nil {
			head = accentStyle.Render(GlyphShell + " " + truncate(m.shell.name, maxInt(m.termWidth()-4, 8)))
		}
		for _, c := range []string{head + seg, head + dimStyle.Render(" ("+elapsed.String()+")")} {
			if lipgloss.Width(sp+" "+c) <= m.termWidth() {
				head = c
				break
			}
		}
		sp = accentStyle.Render(sp)
		out = append(out, "", clipCols(sp+" "+head, m.termWidth()))
	} else {
		out = append(out, "")
	}

	composer := m.composer.View()
	if m.login != nil {
		// textarea has no echo mode: render the value masked here.
		// The mask is a vocabulary glyph, so the plain posture never
		// paints a non-ASCII bullet over the secret it hides.
		masked := strings.Repeat(GlyphMask, len(m.composer.Value()))
		composer = accentStyle.Render(GlyphBrand+" ") + masked
	}
	// The composer sits in the same rounded box every dialog draws —
	// one box language for every surface the user acts on, and the
	// input gains side edges for the same two rows the old full-width
	// rules spent. The border takes the mode's color: the boundary
	// token at rest, amber in shell mode.
	out = append(out, m.composerBox(composer)...)

	// Mode line in the reference shape: "mode (tab to cycle)" then
	// the minimal hints. The mode segment wears its own color — the
	// state every keystroke is scoped by, colored by what it means —
	// and Codex's footer fitting: candidates from fullest to bare
	// mode, first one that fits the terminal wins — a shortcut never
	// separates from its label on narrow screens.
	mode := modeStyle(m.opt.Mode).Render(modeGlyph(m.opt.Mode) + " " + m.opt.Mode)
	bare := mode
	// The effort segment rides the mode line whenever a posture is
	// set — a dial you cannot see is a dial you cannot trust.
	if m.effort != "" {
		mode += accent2Style.Render("   " + GlyphDoing + " " + m.effort)
	}
	var hint string
	if m.shellMode() {
		hint = dimStyle.Render("shell — enter runs it directly, no model round trip")
	} else {
		// Codex's hint shape: the key glyph in accent, the label in
		// secondary text.
		hint = accentStyle.Render("?") + dimStyle.Render(" for shortcuts") +
			dimStyle.Render(" "+GlyphSep+" ") +
			accentStyle.Render("/") + dimStyle.Render(" commands")
	}
	// The leading space matters when an effort is set: the mode line
	// then ends with the effort name, and without it the hint reads
	// "hightab to cycle".
	tab := dimStyle.Render(" (tab to cycle)")
	// The update badge rides the mode segment, not the hints: it is
	// the one piece of the update signal that outlives the startup
	// note, so it must survive the reflow.
	badge := ""
	if m.opt.UpdateTag != "" {
		badge = dimStyle.Render("   " + GlyphUpdate + " " + m.opt.UpdateTag)
	}
	// Fullest to barest, first one that fits whole wins. The mode's bare
	// form outranks the badge alone, and it has to: "⏵⏵ full-auto" is
	// twelve columns, so on a ten-column terminal the badge (ten columns)
	// was chosen whole and the mode vanished from its own status line —
	// the one piece of state every keystroke is scoped by, dropped for a
	// notice that also has a startup note, a toast, and /doctor.
	candidates := []string{
		mode + tab + badge + "   " + hint,
		mode + dimStyle.Render(" (tab)") + badge + "   " + hint,
		mode + badge + "   " + hint,
		mode + badge,
		bare,
	}
	if badge != "" {
		candidates = append(candidates, badge)
	}
	w := m.termWidth()
	line := ""
	for _, c := range candidates {
		if lipgloss.Width(c) <= w {
			line = c
			break
		}
	}
	if line == "" {
		// Nothing fits whole — a terminal narrower than the bare mode.
		// The mode wins anyway and is cut: a row past the terminal's
		// width is the corruption, and a named-as-far-as-it-fits mode
		// beats a truncated badge with no mode on it.
		line = clipCols(bare, w)
	}
	out = append(out, line)
	return out
}

// composerBox frames the input in the shared dialog box, drawn at the
// terminal's own width. It spends the same chrome budget every
// floating block spends — four columns of border and padding — and
// the same two rows the old full-width rules did, so the frame's
// budget above is unchanged. Below fourteen columns the box is not
// drawn: border and padding would leave the input under its floor,
// and a clipped border is worse chrome than none — the bare prompt
// line carries the input on its own.
func (m *Model) composerBox(composer string) []string {
	w := m.termWidth()
	const chrome = 4
	if w < 14 {
		var bare []string
		for _, l := range strings.Split(composer, "\n") {
			bare = append(bare, clipCols(l, w))
		}
		return bare
	}
	st := composerStyle
	if m.shellMode() {
		st = st.BorderForeground(lipgloss.Color(HexWarning))
	}
	lines := strings.Split(composer, "\n")
	for i, l := range lines {
		lines[i] = clipCols(l, w-chrome)
	}
	return strings.Split(st.Width(w-2).Render(strings.Join(lines, "\n")), "\n")
}

// paletteOpen reports whether the command palette should show: the
// composer's first line starts with "/" and has no space yet.
func (m *Model) paletteOpen() bool {
	if m.login != nil || m.awaitingPerm != nil || m.awaitingTrust != nil {
		return false
	}
	v := m.composer.Value()
	first := strings.SplitN(v, "\n", 2)[0]
	return strings.HasPrefix(first, "/") && !strings.Contains(first, " ")
}

// paletteMatches returns the commands matching the typed prefix.
func (m *Model) paletteMatches() []command {
	prefix := strings.TrimSpace(strings.SplitN(m.composer.Value(), "\n", 2)[0])
	var out []command
	for _, c := range commands {
		if strings.HasPrefix(c.Name, prefix) {
			out = append(out, c)
		}
	}
	return out
}

// paletteRows is the command palette's block: the matches with a caret
// on the selection, or the "nothing matched" note that names the typed
// prefix and the way out.
func (m *Model) paletteRows(w int) []string {
	matches := m.paletteMatches()
	if len(matches) == 0 {
		// "nothing" with no way out reads as a broken palette. The
		// typed prefix is still in the composer, so name it and point
		// at the full list.
		typed := strings.TrimSpace(strings.SplitN(m.composer.Value(), "\n", 2)[0])
		return []string{dimStyle.Render("  no command matches " +
			truncate(typed, maxInt(w-30, 8)) +
			"\n  esc clears it " + GlyphSep + " /help lists every command")}
	}

	if m.paletteIdx >= len(matches) {
		m.paletteIdx = len(matches) - 1
	}
	var lines []string
	for i, c := range matches {
		lines = append(lines, menuRow(i == m.paletteIdx, c.Name, c.Desc, w))
	}
	return lines
}

// menuRow composes one item row for every list surface — the pickers,
// the command palette, and the @-mention menu — so selection is one
// language everywhere: the caret marks the row (the band is emphasis,
// never the only signal) and an accent band fills the row behind it.
// The label column is a fixed gutter, so the details read as a column
// instead of a ragged second word. Rows are built to the box's inner
// width before styling, so the band pads to the full width and never
// leaves a wrapped row the budget did not count.
func menuRow(selected bool, label, detail string, w int) string {
	const chrome = 4 // the box border and padding every block spends
	inner := maxInt(w-chrome, 2)
	marker := strings.Repeat(" ", lipgloss.Width(GlyphCaret)+1)
	if selected {
		marker = GlyphCaret + " "
	}
	row := marker
	const gutter = 14
	if detail == "" || lipgloss.Width(marker)+gutter >= inner {
		// No second column fits — a label-only row beats a wrapped
		// one: the label is the choice, the detail is the refinement.
		row += truncate(label, maxInt(inner-lipgloss.Width(marker), 1))
	} else {
		// The label keeps its own budget: a name a user recognizes
		// outranks the rest of the sentence.
		lab := truncate(label, gutter-2)
		row += lab + strings.Repeat(" ", gutter-lipgloss.Width(lab))
		row += truncate(detail, inner-lipgloss.Width(marker)-gutter)
	}
	if !selected {
		return clipCols(dimStyle.Render(row), inner)
	}
	return clipCols(selectedStyle.Bold(true).Width(inner).Render(row), inner)
}

// pickerRows renders the /model and /sessions overlay: a filter line,
// the matched items with a caret on the selection, and a scroll hint
// when the list outgrows the frame.
func (m *Model) pickerRows(w int) []string {
	p := m.picker
	rows := []string{
		promptStyle.Render(p.title) +
			dimStyle.Render("  type to filter "+GlyphSep+" arrows to move "+
				GlyphSep+" enter to select "+GlyphSep+" esc to close"),
	}
	if q := p.query; q != "" {
		rows = append(rows, accentStyle.Render(GlyphPrompt+" ")+clipCols(q, maxInt(w-6, 8)))
	}
	if len(p.matched) == 0 {
		// "no matches" alone reads as a broken picker: say what to
		// type, since a filter too narrow to match anything is a
		// filter the user can widen.
		rows = append(rows, dimStyle.Render("  no matches — backspace widens the filter"))
	}
	const visible = 12
	lo, hi := 0, len(p.matched)
	if hi > visible {
		// Scroll a window around the selection.
		lo = maxInt(p.idx-visible/2, 0)
		if lo+visible > hi {
			lo = hi - visible
		}
		hi = lo + visible
	}
	for i := lo; i < hi; i++ {
		it := p.matched[i]
		rows = append(rows, menuRow(i == p.idx, it.Label, it.Detail, w))
	}
	if len(p.matched) > visible {
		rows = append(rows, dimStyle.Render(
			fmt.Sprintf("  … %d more "+plainOr("—", "-")+
				" keep typing to narrow", len(p.matched)-visible)))
	}
	return rows
}

// atMenuRows is the @-mention file picker: a compact live-filtered
// list above the composer. A live mention that matched nothing says
// so — the menu cannot open for an empty list (Enter and the arrows
// belong to the composer then), so a silent box is the only signal
// the user would get, and silence reads as a broken key.
func (m *Model) atMenuRows(w int) []string {
	rows := []string{
		promptStyle.Render("@ file") +
			dimStyle.Render("  type to filter "+GlyphSep+" enter to attach "+GlyphSep+" esc to close"),
	}
	if len(m.atMenu) == 0 {
		rows = append(rows, dimStyle.Render(
			"  no file matches "+plainOr("—", "-")+
				" @ needs a path under the working directory"))
		return rows
	}
	for i, p := range m.atMenu {
		rows = append(rows, menuRow(i == m.atIdx, p, "", w))
	}
	return rows
}

// helpRows is the "?" sheet: keys and commands at a glance, windowed
// around helpTop. The sheet is longer than a normal terminal, so it
// scrolls (up/down/pgup/pgdn, the transcript pager's own keys) instead
// of truncating: the whole point of the overlay is that every binding
// and every command is in it, and a silently shortened one is a lie
// about what opcode can do.
func (m *Model) helpRows(w int) []string {
	rows := []string{
		accentStyle.Render("keys and commands"),
		dimStyle.Render("  enter        send " + GlyphSep + " ctrl+j / shift+enter  newline"),
		dimStyle.Render("  " + plainOr("↑/↓", "up/dn") + "          recall a previous prompt"),
		dimStyle.Render("  ctrl+v       attach the clipboard image ([Image #N] rides along)"),
		dimStyle.Render("  alt+enter    queue a follow-up while working"),
		dimStyle.Render("  esc          interrupt the turn (idle: closes the palette)"),
		dimStyle.Render("  ctrl+c       press twice to exit " +
			plainOr("—", "-") + " the first press interrupts a turn"),
		dimStyle.Render("  ctrl+d       exit, the same double-press"),
		dimStyle.Render("  ctrl+e       open the prompt in $EDITOR"),
		dimStyle.Render("  tab          cycle permission mode (shift+tab back)"),
		dimStyle.Render("  alt+. / alt+, cycle reasoning effort (up / down)"),
		dimStyle.Render("  ctrl+r       expand / collapse results & thinking"),
		dimStyle.Render("  ctrl+o       transcript " + plainOr("—", "-") +
			" scroll with " + plainOr("↑↓", "up/dn") + "/pgup/pgdn, esc closes"),
		dimStyle.Render("  ! command    run a shell command directly (esc interrupts it)"),
		dimStyle.Render("  @path        attach a file's contents"),
		dimStyle.Render("  /            command palette"),
		dimStyle.Render("  ?            this overlay (empty, idle composer)"),
		dimStyle.Render("  " + plainOr("↑↓", "up/dn") + " / pgup/pgdn  scroll this sheet while it is open"),
		dimStyle.Render("  while working: type and press enter to steer the turn;"),
		dimStyle.Render("  alt+enter queues a follow-up for when it finishes"),
		accentStyle.Render("commands"),
	}
	for _, c := range commands {
		pad := 12 - len(c.Name)
		if pad < 1 {
			pad = 1
		}
		rows = append(rows, dimStyle.Render("  "+c.Name+strings.Repeat(" ", pad)+c.Desc))
	}
	// The window. The header, the two pinned bindings, and a marker
	// above plus one below are the frame's own rows; what is left is the
	// scrolling body. The keys ride pinned on top whatever the scroll:
	// they are the part a user cannot derive, and they are two rows. The
	// budget is the frame's own room, so a marker that gets trimmed
	// away — the one line saying the sheet is longer than the screen —
	// cannot happen.
	const pinned = 2
	// The header block: what the sheet is, plus — when there is one —
	// the badge the footer shows unexplained. The badge is chrome until
	// you know what it means, so it lives here, pinned at the top where
	// a long sheet cannot scroll the news off the bottom of the screen.
	head := []string{rows[0]}
	if m.opt.UpdateTag != "" {
		head = append(head, dimStyle.Render(
			GlyphUpdate+" "+m.opt.UpdateTag+"  a newer opcode is available "+
				plainOr("—", "-")+" run `opcode update` to install it"))
	}
	// The rows this block may spend: the whole frame's content budget,
	// less the header, the two pinned bindings, and the trailer. The
	// trailer is always one row — the "rows below" scroll hint while
	// there is more, the close hint once there is not — because a sheet
	// that silently ends is the state this replaced.
	body := rows[1:]
	var rest []string
	if len(body) > pinned {
		rest = body[pinned:]
	}
	body = body[:minInt(pinned, len(body))]
	avail := m.blockRoom() - len(head) - pinned - 1
	above := m.helpTop > 0
	if above {
		avail--
	}
	if avail < 1 {
		avail = 1
	}
	if maxTop := len(rest) - avail; m.helpTop > maxTop {
		m.helpTop = maxInt(maxTop, 0)
	}
	if m.helpTop < 0 {
		m.helpTop = 0
	}
	out := append(head, body...)
	if above {
		out = append(out, dimStyle.Render(fmt.Sprintf("  … %d rows above", m.helpTop)))
	}
	// The window never re-wraps: rows are clipped to the box's inner
	// width by floatingBlock, so the sheet is exactly avail rows tall
	// and a binding's description is cut rather than pushed onto a row
	// of its own.
	end := minInt(m.helpTop+avail, len(rest))
	for _, r := range rest[m.helpTop:end] {
		out = append(out, clipCols(r, w-4))
	}
	if end < len(rest) {
		out = append(out, dimStyle.Render(fmt.Sprintf("  … %d rows below "+
			plainOr("—", "-")+" "+plainOr("↑↓", "up/dn")+
			" or pgup/pgdn to scroll", len(rest)-end)))
	} else {
		out = append(out, dimStyle.Render("press any key to close"))
	}
	return out
}

// permMinRows is the permission dialog's own floor: the title, the
// literal command, three options, and the key hints. Below that a
// resize would leave a box the user cannot answer, so the dialog drops
// its border instead — the command and the choices are the dialog; the
// frame around them is not.
const permMinRows = 6

// minOverlay is the smallest total a floating block may take: the
// blank line above it, two borders, and three content rows — the least
// that can still say what the block is, what the choice is, and what
// the keys do. Three content rows is the floor every renderer clamps
// to; the +3 is the frame's own cost around them.
const minOverlay = 6

// blockRoom is how many CONTENT rows a bordered block may take: the
// frame's room less the blank line above it and its two borders. Every
// windowed block budgets against this, so a sheet that plans its own
// window and the block that draws it agree by construction.
func (m *Model) blockRoom() int {
	return maxInt(m.overlayRows, minOverlay) - 3
}

// floatingBlock renders one floating overlay — the leading blank line,
// the box, and the block's rows — into the lines the frame appends.
// `keep` pins the last rows of the block through a trim.
//
// The frame's room is the whole budget, so the block spends it in the
// order a reader needs: the border and the blank line go first, then
// the rows in the middle. What is left, the block's identity and its
// actionable rows, survives down to a single content row — a frame
// taller than the terminal is what corrupts bubbletea's inline
// renderer, and a dialog that cannot be answered is worse than one
// without a border.
func (m *Model) floatingBlock(st lipgloss.Style, rows []string, w, keepHead, keepTail, minContent int) []string {
	room := m.overlayRows
	// Zero (or less) is a real answer, not an unset budget: a
	// composer that filled the frame leaves nothing for a block, and
	// inventing room here is a frame taller than the terminal — the
	// corruption. The @-mention menu on an eight-row terminal is the
	// documented case: it shows nothing rather than a clipped row.
	if room < minContent {
		return nil
	}
	// The border is worth three rows only when the content behind it
	// gets the box. Below that it is decoration in front of the one
	// thing the block exists to say, so it goes: the permission dialog
	// on a ten-row pane keeps its command and its options instead of
	// its frame.
	if room < minOverlay || room-3 < minContent {
		avail := room
		if room >= minContent+1 {
			avail = room - 1
			rows = append([]string{""}, rows...)
		}
		return clipRows(rows, avail, w, keepHead, keepTail)
	}
	// Clipped to the box's INNER width. lipgloss re-wraps a row wider
	// than the block's own width rather than cutting it, and a wrapped
	// row is a row the budget never counted: at eight columns a
	// seventeen-column heading came back as five rows and the box was
	// five rows taller than the frame allowed. Border and padding are
	// four columns, so the inner width is four less than the block's.
	const chrome = 4
	rows = clipRows(rows, room-3, maxInt(w-chrome, 1), keepHead, keepTail)
	return []string{"", st.Width(w - chrome + 2).Render(strings.Join(rows, "\n"))}
}

// clipRows trims rows to fit a budget, pinning the first `keepHead` and
// the last `keepTail` of them and marking the cut. A block that quietly
// lost its options is worse than one that says it ran out of room.
func clipRows(rows []string, room, w, keepHead, keepTail int) []string {
	n := len(rows)
	if room < 1 {
		room = 1
	}
	if n <= room {
		return clipEach(rows, w)
	}
	// The pinned rows win over the marker, always. A block that says
	// what it is, shows the thing it is about, and names its keys is
	// useful at any size; a block that also says how much it is hiding
	// is better, but not at the price of the pins. So the marker is
	// taken out of whatever the pins do not claim.
	keepTail = minInt(keepTail, room)
	keepHead = minInt(keepHead, room-keepTail)
	if keepHead+keepTail >= room {
		// No row left for a marker. The cut is still real; the block
		// simply says less about it than it could.
		out := append([]string{}, rows[:keepHead]...)
		return clipEach(append(out, rows[n-keepTail:]...), w)
	}
	cut := n - keepHead - keepTail
	note := dimStyle.Render(fmt.Sprintf("  … %d more row%s "+
		plainOr("—", "-")+" widen the terminal", cut, plural(cut)))
	out := append([]string{}, rows[:keepHead]...)
	out = append(out, note)
	return clipEach(append(out, rows[n-keepTail:]...), w)
}

// clipEach clips every row to the terminal's width. A row wider than
// the pane is the corruption this whole budget exists to prevent, and
// the block's own wrapping was budgeted for a box that may not be
// there at all.
func clipEach(rows []string, w int) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = clipCols(r, w)
	}
	return out
}

// fit trims rendered layers so the frame never exceeds the terminal
// (bubbletea's inline renderer corrupts when it does).
func (m *Model) fit(layers []string, protected int) []string {
	if m.height <= 0 {
		return layers
	}
	// Count rendered ROWS, not layers: one layer may be a multi-row
	// block (the approval dialog). The transcript trims from the
	// front with a marker; the protected tail (open dialog, composer)
	// is never dropped. Measuring the rendered layers is the whole
	// budget: an estimate of the composer block's height was three
	// rows off, and a short terminal lost transcript it had room for.
	rows := 0
	for _, l := range layers {
		rows += strings.Count(l, "\n") + 1
	}
	if rows <= m.height {
		return layers
	}
	trimmable := len(layers) - protected
	// The trim marker is a row of its own, so trimming stops one row
	// short of the terminal — otherwise the frame lands exactly one
	// row over, which is the corruption this exists to prevent.
	target := m.height - 1
	dropped, cutRows := 0, 0
	for dropped < trimmable && rows > target {
		cutRows += strings.Count(layers[dropped], "\n") + 1
		rows -= strings.Count(layers[dropped], "\n") + 1
		dropped++
	}
	if dropped == 0 {
		return layers
	}
	out := make([]string, 0, len(layers)-dropped+1)
	// The marker counts rows, not layers: one dropped layer can be a
	// dozen-row markdown block, and "… 3 earlier lines" over a
	// forty-line cut reads as a lie.
	out = append(out, dimStyle.Render(clipCols(
		fmt.Sprintf("… %d earlier lines", cutRows), m.termWidth())))
	return append(out, layers[dropped:]...)
}

func (m *Model) termWidth() int {
	if m.width > 0 {
		return m.width
	}
	return 80
}

// wrapAll hard-wraps plain (unstyled) text to width. Styled strings are
// measured by visible width via lipgloss.Width where needed.
func wrapAll(s string, w int) []string {
	if w < 10 {
		w = 10
	}
	var out []string
	for _, para := range strings.Split(s, "\n") {
		if lipgloss.Width(para) <= w {
			out = append(out, para)
			continue
		}
		line := ""
		for _, word := range strings.Fields(para) {
			// A word wider than the row on its own — a path, a URL, a
			// sha, a pasted stack frame — has no space to break at, so
			// it has to be cut or it runs off the terminal and
			// corrupts the frame.
			for lipgloss.Width(word) > w {
				head, tail := splitWidth(word, w)
				if line != "" {
					out = append(out, line)
					line = ""
				}
				out = append(out, head)
				word = tail
			}
			if line == "" {
				line = word
			} else if lipgloss.Width(line)+1+lipgloss.Width(word) <= w {
				line += " " + word
			} else {
				out = append(out, line)
				line = word
			}
		}
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

// appendWrapped wraps plain text to w and appends each row, styled.
// The styled-string variant (wrapAll on a rendered line) re-joins the
// words with spaces and cannot tell an escape sequence from a
// character, so the codes end up in the wrong row; styling after the
// wrap is the only order that survives.
func appendWrapped(out []string, s string, w int, st lipgloss.Style) []string {
	for _, l := range wrapAll(s, w) {
		out = append(out, st.Render(l))
	}
	return out
}

// splitWidth cuts s at the last rune that fits in w columns, returning
// the head and whatever is left. One rune wider than the row is
// emitted whole: half a rune renders as a replacement character, and
// losing content is worse than a single over-wide cell.
func splitWidth(s string, w int) (head, tail string) {
	width := 0
	for i, r := range s {
		rw := lipgloss.Width(string(r))
		if width+rw > w {
			if i == 0 {
				return s[:len(string(r))], s[len(string(r)):]
			}
			return s[:i], s[i:]
		}
		width += rw
	}
	return s, ""
}

// clipCols trims a styled line to w visible columns. truncate counts
// plain text, and a rendered line is mostly SGR escapes; this keeps
// the escapes and drops only the visible tail.
func clipCols(s string, w int) string {
	if lipgloss.Width(s) <= w {
		return s
	}
	if w <= 0 {
		return ""
	}
	var b strings.Builder
	width := 0
	for i := 0; i < len(s); {
		if seq := escapeLen(s[i:]); seq > 0 {
			b.WriteString(s[i : i+seq])
			i += seq
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if width+lipgloss.Width(string(r)) > w {
			break
		}
		b.WriteString(s[i : i+size])
		i += size
		width += lipgloss.Width(string(r))
	}
	return b.String()
}

// escapeLen is the byte length of the SGR escape starting s, or 0 when
// s does not start with one.
func escapeLen(s string) int {
	if len(s) < 2 || s[0] != 0x1b || s[1] != '[' {
		return 0
	}
	for i := 2; i < len(s); i++ {
		if s[i] >= 'a' && s[i] <= 'z' || s[i] >= 'A' && s[i] <= 'Z' {
			return i + 1
		}
	}
	return 0
}

// truncate shortens s to n visible columns with an ellipsis. Runes,
// not bytes: a byte slice through a multi-byte rune renders as a
// replacement character, and n can go negative at a narrow terminal
// (the result row once budgeted width-14 and panicked on the slice).
func truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= n {
		return s
	}
	runes := []rune(s)
	for len(runes) > 0 && lipgloss.Width(string(runes)) > n-1 {
		runes = runes[:len(runes)-1]
	}
	return string(runes) + "…"
}

// diffCounts counts changed lines: each replaced line is one removal
// and one addition, plus the tail of whichever side grew.
func diffCounts(old, new string) (adds, dels int) {
	if old == new {
		return 0, 0
	}
	o, n := strings.Split(old, "\n"), strings.Split(new, "\n")
	for i := 0; i < len(o) && i < len(n); i++ {
		if o[i] != n[i] {
			adds++
			dels++
		}
	}
	switch {
	case len(n) > len(o):
		adds += len(n) - len(o)
	case len(o) > len(n):
		dels += len(o) - len(n)
	}
	return adds, dels
}

func humanCount(n int) string {
	switch {
	case n >= 1000:
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// permTitle is the dialog's title in plain words (spec 9.1).
func permTitle(tool string) string {
	switch tool {
	case "bash":
		return "Bash command"
	case "write_file":
		return "Write file"
	case "edit_file":
		return "Edit file"
	case "apply_patch":
		return "Apply patch"
	case "web_fetch":
		return "Fetch web page"
	default:
		return tool
	}
}

// permTierWords is the tier badge in plain words, never the internal
// tier string. A shell escape ({"sandbox": false}) is named for what
// it is: the one class of command that runs without the kernel
// write-confinement.
func permTierWords(tool, args string) string {
	switch tool {
	case "bash":
		if tools.ShellEscaped(args) {
			return "Runs a command without the sandbox"
		}
		return "Runs a command (sandboxed)"
	case "write_file", "edit_file":
		return "Changes files"
	default:
		return "Uses an external service"
	}
}

// permLiteral is the literal command or path the approval is about —
// shown verbatim, never summarized (spec 3.3).
//
// Sanitized AFTER the unmarshal, not before. Sanitizing the JSON string
// looks equivalent and is not: JSON escapes a control byte as the six
// ASCII characters \u001b, so the scan finds nothing to strip and the
// byte comes back out of the unmarshal intact. The command name is
// model-authored text about to be rendered, so it gets the display
// boundary here — the one place it becomes a glyph.
func permLiteral(tool, args string) string {
	var a struct {
		Command string `json:"command"`
		Path    string `json:"path"`
	}
	if json.Unmarshal([]byte(args), &a) == nil {
		if a.Command != "" {
			return safe.Text(a.Command)
		}
		if a.Path != "" {
			return safe.Text(a.Path)
		}
	}
	return safe.Text(args)
}

// permDialogRows renders the approval dialog (spec 9.1, the reference
// product's shape): title, tier badge in plain words, the literal
// command, the numbered options with the selection highlighted, and
// the key hints. No is highlighted first — the safest default.
func (m *Model) permDialogRows(req *permRequest, w int) ([]string, int, int) {
	opts := []string{
		"Yes",
		"Yes, and don't ask again for: " + req.scope,
		"No",
	}
	// The essential rows, in the order a reader needs them: what is being
	// asked, the literal it is about, the three choices, the keys. The
	// blank lines and the reassurance sentence between them are spacing,
	// and spacing is the first thing a short terminal gives up — not the
	// command, never the options.
	head := promptStyle.Render(permTitle(req.tool)) + " " +
		dimStyle.Render(GlyphSep+" "+permTierWords(req.tool, req.args))
	literal := wrapAll(permLiteral(req.tool, req.args), w-8)
	var options []string
	for i, o := range opts {
		marker := "  "
		style := dimStyle
		if i == req.sel {
			marker = accentStyle.Render(GlyphPrompt + " ")
			style = toolNameStyle
		}
		// Option 2 carries the grant's literal scope, which can be
		// longer than the row; the dialog must show it whole, because
		// it is the whole of what option 2 promises.
		for _, l := range wrapAll(fmt.Sprintf("%d. %s", i+1, o), w-8) {
			options = append(options, marker+style.Render(l))
		}
	}
	keys := dimStyle.Render("1-3 or arrows to choose " + GlyphSep +
		" enter selects " + GlyphSep + " y/a/n work " + GlyphSep + " esc cancels")
	// Pinned head and tail: the title, the literal, the options, and the
	// keys are the dialog. A resize that cut any of them left a box the
	// user could not answer, which is the one failure a permission
	// dialog may not have — so the blank lines go first, and the
	// reassurance sentence with them.
	rows := []string{head}
	rows = append(rows, literal...)
	rows = append(rows, options...)
	rows = append(rows, keys)
	if m.blockRoom() >= len(rows)+4 {
		spaced := []string{head, ""}
		spaced = append(spaced, literal...)
		spaced = append(spaced, "", dimStyle.Render(
			"opcode needs your approval to run this. Do you want to proceed?"), "")
		spaced = append(spaced, options...)
		spaced = append(spaced, "", keys)
		return spaced, 2 + len(literal), len(options) + 2
	}
	return rows, 1 + len(literal), len(options) + 1
}

// modeGlyph maps a permission mode to its footer glyph. The brand mark
// belongs to the composer; each mode reads at a glance by shape.
func modeGlyph(mode string) string {
	switch tools.NormalizeMode(mode) {
	case tools.ModePlan:
		return GlyphModePlan
	case tools.ModeFullAuto:
		return GlyphModeFullAuto
	default: // build, unknown
		return GlyphModeBuild
	}
}

// modeStyle colors the mode segment by what the posture means: plan
// is inquiry (info), build is the working posture (the brand
// accent), full-auto proceeds without asking (amber — caution's own
// color). The glyph already differs; the color is emphasis, never the
// only signal.
func modeStyle(mode string) lipgloss.Style {
	switch tools.NormalizeMode(mode) {
	case tools.ModePlan:
		return infoStyle
	case tools.ModeFullAuto:
		return warnStyle
	default:
		return accentStyle
	}
}
