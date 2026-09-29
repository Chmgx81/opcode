package tui

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/Chmgx81/tilde/internal/tools"
)

// View implements tea.Model: identity block and timeline, a working
// status line while a turn runs, the composer with a mode line under
// it, and floating layers (palette, help) that fit the terminal.
func (m *Model) View() string {
	tl := m.timelineView()
	fl := m.floatingView()
	cl := m.composerView()
	layers := append(append(tl, fl...), cl...)

	// The floating and composer layers are protected: the transcript
	// trims from the front, the dialog and composer never do.
	return strings.Join(m.fit(layers, len(fl)+len(cl)), "\n")
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
	head := dimStyle.Render(GlyphThought + " thinking…")
	lines := strings.Split(strings.TrimRight(m.reasoning.String(), "\n"), "\n")
	const keep = 3
	if len(lines) > keep {
		lines = lines[len(lines)-keep:]
	}
	out := []string{head}
	for _, l := range lines {
		out = append(out, dimStyle.Italic(true).Render("  "+l))
	}
	return out
}

// todosView renders the model's live task list: a count header, then
// the items — ✓ done, ▸ in progress, · pending — windowed so a long
// list cannot eat the screen.
func (m *Model) todosView() []string {
	done := 0
	for _, it := range m.todos {
		if it.Status == tools.TodoDone {
			done++
		}
	}
	head := accentStyle.Render(GlyphBullet+" tasks") +
		dimStyle.Render(fmt.Sprintf(" (%d/%d done)", done, len(m.todos)))
	out := []string{head}

	const rows = 8
	shown, extra := m.todos, 0
	if len(shown) > rows {
		// Prioritize the live work: everything up to the in-progress
		// item plus what follows, capped at the window.
		cut := len(shown) - rows
		shown, extra = shown[cut:], cut
	}
	for _, it := range shown {
		var marker, text string
		switch it.Status {
		case tools.TodoDone:
			marker, text = dimStyle.Render(GlyphTodoOn), dimStyle.Strikethrough(true).Render(it.Content)
		case tools.TodoInProgress:
			marker, text = accentStyle.Render(GlyphDoing), it.Content
		default:
			marker, text = dimStyle.Render("·"), dimStyle.Render(it.Content)
		}
		out = append(out, "  "+marker+" "+text)
	}
	if extra > 0 {
		out = append(out, dimStyle.Render(fmt.Sprintf("  … %d earlier tasks", extra)))
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
		return []string{userPanel(wrapAll(dimStyle.Render(GlyphUser+" ")+e.text, w), m.termWidth())}
	case entryAssistant:
		if e.rendered == nil || e.renderedW != w {
			e.rendered = renderMarkdown(e.text, w)
			e.renderedW = w
		}
		return e.rendered
	case entryTool:
		args := truncate(e.text, 70)
		return []string{accentStyle.Render(GlyphBullet+" ") +
			toolNameStyle.Render(e.tool) + dimStyle.Render(" "+args)}
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
		return wrapAll(infoStyle.Render("… ")+e.text, w)
	case entrySubagent:
		return wrapAll(dimStyle.Render(e.subTitle)+e.text, w)
	case entryReasoning:
		// Collapsed: one dim line — the thinking is atmosphere.
		// ctrl+r (the results toggle) expands the text, windowed.
		head := dimStyle.Render(GlyphThought) + dimStyle.Render(
			fmt.Sprintf(" thought for %s · %d chars", e.dur, len(e.text)))
		if !m.expandingResults() {
			return []string{head + dimStyle.Render("  (ctrl+r to expand)")}
		}
		out := []string{head}
		const rows = 12
		lines := strings.Split(strings.TrimRight(e.text, "\n"), "\n")
		if len(lines) > rows {
			extra := len(lines) - rows
			lines = lines[:rows]
			for _, l := range lines {
				out = append(out, dimStyle.Italic(true).Render("  "+l))
			}
			return append(out, dimStyle.Italic(true).Render(
				fmt.Sprintf("  … %d more lines", extra)))
		}
		for _, l := range lines {
			out = append(out, dimStyle.Italic(true).Render("  "+l))
		}
		return out
	case entryPlan:
		// The presented plan: a labeled markdown block in the
		// transcript; the decision line follows below it.
		out := []string{accentStyle.Render(GlyphBullet + " plan")}
		return append(out, renderMarkdown(e.text, w)...)
	case entryDiff:
		// /diff pre-colors git's unified diff line by line; the
		// lines render exactly as composed (no re-wrap — a wrapped
		// diff line stops reading as a diff).
		return strings.Split(e.text, "\n")
	}
	return nil
}

// renderResult shows a tool result: one collapsed line by default,
// full text when ctrl+r expanded. edit_file results render as a diff
// hunk with colored − / + lines either way.
func (m *Model) renderResult(e entry, w int) []string {
	prefix := dimStyle.Render("  " + GlyphBranch + " ")
	if e.tool == "write_file" && e.path != "" && e.full != "" {
		lines := strings.Split(strings.TrimRight(e.full, "\n"), "\n")
		head := prefix + dimStyle.Render(fmt.Sprintf("wrote %s ", e.path)) +
			okStyle.Render(fmt.Sprintf("%s%d", GlyphAdded, len(lines)))
		if !m.expandingResults() {
			return []string{head + dimStyle.Render("  (ctrl+r to expand)")}
		}
		out := []string{head}
		const rows = 10
		l := lexerFor(e.path)
		shown := lines
		extra := 0
		if len(shown) > rows {
			extra = len(shown) - rows
			shown = shown[:rows]
		}
		for _, src := range shown {
			out = append(out, okStyle.Render(strings.Repeat(" ", 5)+GlyphAdded+" ")+highlightLine(src, l))
		}
		if extra > 0 {
			out = append(out, dimStyle.Render(fmt.Sprintf("      … %d more lines", extra)))
		}
		return out
	}
	if e.tool == "edit_file" && e.path != "" {
		adds, dels := diffCounts(e.old, e.new)
		head := prefix + dimStyle.Render(fmt.Sprintf("updated %s ", e.path)) +
			okStyle.Render(fmt.Sprintf("%s%d", GlyphAdded, adds)) + " " +
			dangerStyle.Render(fmt.Sprintf("%s%d", GlyphDeleted, dels))
		if !m.expandingResults() {
			return []string{head + dimStyle.Render("  (ctrl+r to expand)")}
		}
		out := []string{head}
		// Syntax-highlighted hunks: tokens carry language colors, the
		// − / + markers and indent carry the verdict.
		l := lexerFor(e.path)
		for _, l2 := range strings.Split(e.old, "\n") {
			out = append(out, dangerStyle.Render(strings.Repeat(" ", 5)+GlyphDeleted+" ")+highlightLine(l2, l))
		}
		for _, l3 := range strings.Split(e.new, "\n") {
			out = append(out, okStyle.Render(strings.Repeat(" ", 5)+GlyphAdded+" ")+highlightLine(l3, l))
		}
		return out
	}
	if m.expandingResults() && e.full != "" {
		out := []string{prefix + dimStyle.Render(e.summary)}
		for _, l := range wrapAll(e.full, w-4) {
			out = append(out, dimStyle.Render("      "+l))
		}
		return out
	}
	hint := ""
	if e.summary != "" {
		hint = dimStyle.Render("  (ctrl+r to expand)")
	}
	return []string{prefix + resultStyle.Render(truncate(e.summary, m.termWidth()-14)) + hint}
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
func userPanel(lines []string, w int) string {
	panel := lipgloss.NewStyle().Background(Deep2)
	var out []string
	for _, l := range lines {
		pad := maxInt(w-lipgloss.Width(l), 0)
		out = append(out, panel.Render(l+strings.Repeat(" ", pad)))
	}
	return strings.Join(out, "\n")
}

// floatingView renders the trust prompt, permission prompt, login
// prompt, palette, toast, and help overlay.
func (m *Model) floatingView() []string {
	var out []string
	w := m.termWidth()

	if m.transcriptOpen {
		out = append(out, "", m.transcriptView(w))
	}
	if m.awaitingTrust != nil {
		files := strings.Join(m.awaitingTrust.Approved, ", ")
		out = append(out, "",
			promptBoxStyle.Width(w-4).Render(
				promptStyle.Render("trust this project?")+" "+
					dimStyle.Render("tilde would be able to run: "+files+" — y trust, n/Esc decline")))
	}
	if m.awaitingPerm != nil {
		out = append(out, "", m.permDialogView(m.awaitingPerm, w))
	}
	if m.awaitingPlan != nil {
		out = append(out, "",
			promptBoxStyle.Width(w-4).Render(
				promptStyle.Render("proceed with this plan?")+" "+
					dimStyle.Render("y implement (actions will ask) · a implement with auto-accept · n/Esc keep planning")))
	}
	if m.login != nil {
		out = append(out, "",
			promptBoxStyle.Width(w-4).Render(
				promptStyle.Render("API key for "+m.login.provider+" — input hidden, Enter to save, Esc to cancel")))
	}
	if m.paletteOpen() {
		out = append(out, "", m.paletteView(w))
	}
	if m.picker != nil {
		out = append(out, "", m.pickerView(w))
	}
	if m.atMenuOpen() {
		out = append(out, "", m.atMenuView(w))
	}
	if m.helpOpen {
		out = append(out, "", m.helpView(w))
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

// transcriptView is the ctrl+O pager over the whole conversation —
// committed and live entries alike, results and thinking expanded,
// windowed around transcriptTop. The overlay self-bounds so it can
// never push the composer off-screen.
func (m *Model) transcriptView(w int) string {
	lines := m.transcriptLines()
	// Reserve the composer block (rules, input, mode line) and the
	// working line; the overlay never exceeds that.
	visible := m.height - m.composer.Height() - 8
	if visible < 3 {
		visible = 3
	}
	if maxTop := len(lines) - visible; m.transcriptTop > maxTop {
		m.transcriptTop = maxInt(maxTop, 0)
	}
	shown := []string{accentStyle.Render(GlyphBullet+" transcript") +
		dimStyle.Render("  ↑↓ scroll · pgup/pgdn · ctrl+o or esc close")}
	if m.transcriptTop > 0 {
		shown = append(shown, dimStyle.Render(
			fmt.Sprintf("  … %d lines above", m.transcriptTop)))
	}
	end := m.transcriptTop + visible
	if end > len(lines) {
		end = len(lines)
	}
	shown = append(shown, lines[m.transcriptTop:end]...)
	if end < len(lines) {
		shown = append(shown, dimStyle.Render(
			fmt.Sprintf("  … %d lines below", len(lines)-end)))
	}
	return paletteStyle.Width(w - 4).Render(strings.Join(shown, "\n"))
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
		if !m.opt.Animations {
			sp = GlyphBullet
		}
		// Codex's status shape: verb, then one parenthesized segment
		// with elapsed, interrupt, and token flow inside it.
		left := accentStyle.Render(sp) + " " +
			accentStyle.Render(verb) + dimStyle.Render(
			fmt.Sprintf(" (%s • esc to interrupt • ↓ %s tokens)",
				elapsed, humanCount(tokens)))
		out = append(out, "", left)
	} else {
		out = append(out, "")
	}

	composer := m.composer.View()
	if m.login != nil {
		// textarea has no echo mode: render the value masked here.
		masked := strings.Repeat("\u2022", len(m.composer.Value()))
		composer = accentStyle.Render(GlyphBrand+" ") + masked
	}
	// The composer sits between two full-width rules — the frame that
	// makes the input findable without a box. The rules take the
	// mode's color: border at rest, amber in shell mode.
	rule := ruleStyle
	if m.shellMode() {
		rule = warnStyle
	}
	ruleLine := rule.Render(strings.Repeat("─", m.termWidth()))
	out = append(out, ruleLine, composer, ruleLine)

	// Mode line in the reference shape: "~ mode (tab to cycle)" then
	// the minimal hints. Codex's footer fitting: candidates from
	// fullest to bare mode, first one that fits the terminal wins — a
	// shortcut never separates from its label on narrow screens.
	mode := accent2Style.Render(modeGlyph(m.opt.Mode) + " " + m.opt.Mode)
	var hint string
	if m.shellMode() {
		hint = dimStyle.Render("shell — enter runs it directly, no model round trip")
	} else {
		// Codex's hint shape: the key glyph in accent, the label in
		// secondary text.
		hint = accentStyle.Render("?") + dimStyle.Render(" for shortcuts") +
			dimStyle.Render(" · ") +
			accentStyle.Render("/") + dimStyle.Render(" commands")
	}
	tab := dimStyle.Render("(tab to cycle)")
	candidates := []string{
		mode + tab + "   " + hint,
		mode + dimStyle.Render(" (tab)") + "   " + hint,
		mode + "   " + hint,
		mode,
	}
	line := candidates[len(candidates)-1]
	for _, c := range candidates {
		if lipgloss.Width(c) <= m.termWidth() {
			line = c
			break
		}
	}
	out = append(out, line)
	return out
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

func (m *Model) paletteView(w int) string {
	matches := m.paletteMatches()
	if len(matches) == 0 {
		return dimStyle.Render("  no matching command")
	}

	if m.paletteIdx >= len(matches) {
		m.paletteIdx = len(matches) - 1
	}
	var lines []string
	for i, c := range matches {
		marker := "  "
		style := dimStyle
		if i == m.paletteIdx {
			marker = accentStyle.Render(GlyphCaret + " ")
			style = toolNameStyle
		}
		lines = append(lines, marker+style.Render(c.Name)+dimStyle.Render("  "+c.Desc))
	}
	return paletteStyle.Width(w - 4).Render(strings.Join(lines, "\n"))
}

// pickerView renders the /model and /sessions overlay: a filter line,
// the matched items with a caret on the selection, and a scroll hint
// when the list outgrows the frame.
func (m *Model) pickerView(w int) string {
	p := m.picker
	rows := []string{
		promptStyle.Render(p.title) +
			dimStyle.Render("  type to filter · arrows to move · enter to select · esc to close"),
	}
	if q := p.query; q != "" {
		rows = append(rows, accentStyle.Render(GlyphPrompt+" ")+q)
	}
	if len(p.matched) == 0 {
		rows = append(rows, dimStyle.Render("  no matches"))
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
		marker, style := "  ", dimStyle
		if i == p.idx {
			marker = accentStyle.Render(GlyphCaret + " ")
			style = toolNameStyle
		}
		line := marker + style.Render(truncate(it.Label, maxInt(w-16, 12)))
		if it.Detail != "" {
			line += dimStyle.Render("  " + truncate(it.Detail, 56))
		}
		rows = append(rows, line)
	}
	if len(p.matched) > visible {
		rows = append(rows, dimStyle.Render(
			fmt.Sprintf("  … %d more — keep typing to narrow", len(p.matched)-visible)))
	}
	return paletteStyle.Width(w - 4).Render(strings.Join(rows, "\n"))
}

// atMenuView is the @-mention file picker: a compact live-filtered
// list above the composer.
func (m *Model) atMenuView(w int) string {
	rows := []string{
		promptStyle.Render("@ file") +
			dimStyle.Render("  type to filter · enter to attach · esc to close"),
	}
	for i, p := range m.atMenu {
		marker, style := "  ", dimStyle
		if i == m.atIdx {
			marker = accentStyle.Render(GlyphCaret + " ")
			style = toolNameStyle
		}
		rows = append(rows, marker+style.Render(truncate(p, maxInt(w-16, 12))))
	}
	return paletteStyle.Width(w - 4).Render(strings.Join(rows, "\n"))
}

// helpView is the "?" overlay: keys and commands at a glance.
func (m *Model) helpView(w int) string {
	rows := []string{
		accentStyle.Render("keys"),
		dimStyle.Render("  enter        send · ctrl+j  newline"),
		dimStyle.Render("  ↑/↓          recall a previous prompt"),
		dimStyle.Render("  ctrl+v       attach the clipboard image ([Image #N] rides along)"),
		dimStyle.Render("  alt+enter    queue a follow-up while working"),
		dimStyle.Render("  esc          interrupt the turn"),
		dimStyle.Render("  ctrl+c       press twice to exit — the first press interrupts a turn"),
		dimStyle.Render("  ctrl+d       exit, the same double-press"),
		dimStyle.Render("  tab          cycle permission mode (shift+tab back)"),
		dimStyle.Render("  ctrl+r       expand / collapse results & thinking"),
		dimStyle.Render("  ctrl+o       transcript — scroll the whole conversation"),
		dimStyle.Render("  ! command    run a shell command directly"),
		dimStyle.Render("  @path        attach a file's contents"),
		dimStyle.Render("  /            command palette"),
		accentStyle.Render("commands"),
	}
	for _, c := range commands {
		rows = append(rows, dimStyle.Render("  "+c.Name+strings.Repeat(" ", 12-len(c.Name))+c.Desc))
	}
	rows = append(rows, dimStyle.Render("press any key to close"))
	return helpStyle.Width(w - 4).Render(strings.Join(rows, "\n"))
}

// fit trims rendered layers so the frame never exceeds the terminal
// (bubbletea's inline renderer corrupts when it does).
func (m *Model) fit(layers []string, protected int) []string {
	if m.height <= 0 {
		return layers
	}
	// Reserve rows for the composer (its live height + the two rules
	// that frame it + the footer mode line + the status/blank line
	// above it) so long transcripts trim, not the composer.
	reserved := m.composer.Height() + 8
	if m.working {
		reserved += 2
	}
	max := m.height - reserved
	if max < 1 {
		max = 1
	}
	// Count rendered ROWS, not layers: one layer may be a multi-row
	// block (the approval dialog). The transcript trims from the
	// front with a marker; the protected tail (open dialog, composer)
	// is never dropped.
	rows := 0
	for _, l := range layers {
		rows += strings.Count(l, "\n") + 1
	}
	if rows <= max {
		return layers
	}
	trimmable := len(layers) - protected
	dropped := 0
	for dropped < trimmable && rows > max {
		rows -= strings.Count(layers[dropped], "\n") + 1
		dropped++
	}
	if dropped == 0 {
		return layers
	}
	out := make([]string, 0, len(layers)-dropped+1)
	out = append(out, dimStyle.Render(fmt.Sprintf("… %d earlier lines", dropped)))
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

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
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
func permLiteral(tool, args string) string {
	var a struct {
		Command string `json:"command"`
		Path    string `json:"path"`
	}
	if json.Unmarshal([]byte(args), &a) == nil {
		if a.Command != "" {
			return a.Command
		}
		if a.Path != "" {
			return a.Path
		}
	}
	return args
}

// permDialogView renders the approval dialog (spec 9.1, the reference
// product's shape): title, tier badge in plain words, the literal
// command, the numbered options with the selection highlighted, and
// the key hints. No is highlighted first — the safest default.
func (m *Model) permDialogView(req *permRequest, w int) string {
	opts := []string{
		"Yes",
		"Yes, and don't ask again for: " + req.scope,
		"No",
	}
	var rows []string
	rows = append(rows, promptStyle.Render(permTitle(req.tool))+" "+
		dimStyle.Render("· "+permTierWords(req.tool, req.args)))
	rows = append(rows, "")
	for _, l := range wrapAll(permLiteral(req.tool, req.args), w-8) {
		rows = append(rows, l)
	}
	rows = append(rows, "", dimStyle.Render("tilde needs your approval to run this. Do you want to proceed?"), "")
	for i, o := range opts {
		marker := "  "
		style := dimStyle
		if i == req.sel {
			marker = accentStyle.Render(GlyphPrompt + " ")
			style = toolNameStyle
		}
		rows = append(rows, marker+style.Render(fmt.Sprintf("%d. %s", i+1, o)))
	}
	rows = append(rows, "",
		dimStyle.Render("1-3 or arrows to choose · enter selects · y/a/n work · esc cancels"))
	return promptBoxStyle.Width(w - 4).Render(strings.Join(rows, "\n"))
}

// modeGlyph maps a permission mode to its footer glyph. The brand ~
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
