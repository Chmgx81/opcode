package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// View implements tea.Model: identity block and timeline, a working
// status line while a turn runs, the composer with a mode line under
// it, and floating layers (palette, help) that fit the terminal.
func (m *Model) View() string {
	var layers []string

	layers = append(layers, m.timelineView()...)
	layers = append(layers, m.floatingView()...)
	layers = append(layers, m.composerView()...)

	return strings.Join(m.fit(layers), "\n")
}

// timelineView renders the transcript entries. Every user query and
// every finished answer opens with a blank line — conversation blocks
// breathe — and collapseBlanks keeps that to a single gap no matter
// what glamour or the greeting emit around them.
func (m *Model) timelineView() []string {
	var out []string
	for i := range m.entries {
		e := &m.entries[i]
		if e.kind == entryUser || e.kind == entryAssistant {
			out = append(out, "")
		}
		out = append(out, m.renderEntry(e)...)
	}
	// In-flight assistant text.
	if s := strings.TrimSpace(m.stream.String()); s != "" {
		out = append(out, "")
		out = append(out, renderAssistant(s, m.termWidth())...)
	}
	return collapseBlanks(out)
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

// renderEntry maps one structured entry to view lines. Assistant text
// renders as markdown, cached per width on the entry: View runs every
// frame and re-running glamour per frame would visibly cost.
func (m *Model) renderEntry(e *entry) []string {
	w := m.termWidth()
	switch e.kind {
	case entryUser:
		// The echoed query stays bright with the accent prompt — the
		// reference apps render user messages at full weight, letting
		// the agent's response follow at the same level.
		return wrapAll(accentStyle.Render(GlyphPrompt+" ")+e.text, w)
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
		return wrapAll(dangerStyle.Render(GlyphWarn+" ")+e.text, w)
	case entryDim:
		return wrapAll(e.text, w)
	case entrySteer:
		return wrapAll(steerStyle.Render("(steering) ")+e.text, w)
	case entryQueued:
		return wrapAll(queuedStyle.Render("(queued) ")+e.text, w)
	case entryCompaction:
		return wrapAll(infoStyle.Render("… ")+e.text, w)
	case entrySubagent:
		return wrapAll(dimStyle.Render(e.subTitle)+e.text, w)
	}
	return nil
}

// renderResult shows a tool result: one collapsed line by default,
// full text when ctrl+r expanded. edit_file results render as a diff
// hunk with colored − / + lines either way.
func (m *Model) renderResult(e entry, w int) []string {
	prefix := dimStyle.Render("  " + GlyphBranch + " ")
	if e.tool == "edit_file" && e.path != "" {
		adds, dels := diffCounts(e.old, e.new)
		head := prefix + dimStyle.Render(fmt.Sprintf("updated %s ", e.path)) +
			okStyle.Render(fmt.Sprintf("%s%d", GlyphAdded, adds)) + " " +
			dangerStyle.Render(fmt.Sprintf("%s%d", GlyphDeleted, dels))
		if !m.expandResults {
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
	if m.expandResults && e.full != "" {
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

// renderAssistant renders assistant text with light structure: fenced
// code blocks become boxed monospace, everything else wraps plainly.
func renderAssistant(text string, w int) []string {
	var out []string
	var fence []string
	flushFence := func() {
		if len(fence) == 0 {
			return
		}
		box := fenceStyle.Width(maxInt(w-6, 20))
		for _, l := range strings.Split(box.Render(strings.Join(fence, "\n")), "\n") {
			out = append(out, l)
		}
		fence = nil
	}
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			if len(fence) > 0 {
				flushFence()
			} else {
				fence = fence[:0]
				_ = fence
			}
			continue
		}
		if fence != nil {
			fence = append(fence, line)
			continue
		}
		if trimmed == "" {
			out = append(out, "")
			continue
		}
		// Headings get weight; list markers stay as typed.
		if strings.HasPrefix(trimmed, "#") {
			out = append(out, wrapAll(boldStyle.Render(trimmed), w)...)
			continue
		}
		out = append(out, wrapAll(line, w)...)
	}
	flushFence()
	return out
}

// floatingView renders the trust prompt, permission prompt, login
// prompt, palette, toast, and help overlay.
func (m *Model) floatingView() []string {
	var out []string
	w := m.termWidth()

	if m.awaitingTrust != nil {
		files := strings.Join(m.awaitingTrust.Approved, ", ")
		out = append(out, "",
			promptBoxStyle.Width(w-4).Render(
				promptStyle.Render("trust this project?")+" "+
					dimStyle.Render("it would be able to run: "+files+" — y trust, n/Esc decline")))
	}
	if m.awaitingPerm != nil {
		out = append(out, "",
			promptBoxStyle.Width(w-4).Render(
				promptStyle.Render("allow?")+" "+toolNameStyle.Render(m.awaitingPerm.tool)+" "+
					dimStyle.Render("(tier "+string(m.awaitingPerm.tier)+
						") — y allow · a allow all action tools this session · n/Esc deny")))
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

// composerView renders the working status line, the composer box, and
// the mode line under it.
func (m *Model) composerView() []string {
	var out []string

	// Status line while a turn runs: spinner, elapsed, tokens, keys.
	if m.working {
		elapsed := time.Since(m.workingSince).Round(time.Second)
		tokens := m.usage.PromptTokens + m.usage.CompletionTokens
		left := accentStyle.Render(m.spinner.View()) + " " +
			infoStyle.Render("working") + dimStyle.Render(
			fmt.Sprintf(" · %s · %s tokens · esc to interrupt · enter steers · alt+enter queues",
				elapsed, humanCount(tokens)))
		out = append(out, "", left)
	} else {
		out = append(out, "")
	}

	composer := m.composer.View()
	if m.login != nil {
		// textarea has no echo mode: render the value masked here.
		masked := strings.Repeat("\u2022", len(m.composer.Value()))
		composer = accentStyle.Render(GlyphPrompt+" ") + masked
	}
	out = append(out, boxStyle.Width(m.termWidth()-4).Render(composer))

	// Mode line, Claude-Code-shaped footer: the permission posture
	// first, then the always-available prefixes as dim hints. The
	// placeholder stays a real placeholder ("ask tilde anything…"),
	// not a keymap.
	mode := accent2Style.Render(GlyphPrompt + " " + m.opt.Mode)
	hint := dimStyle.Render("? help · / commands · ! shell · @ files")
	out = append(out, mode+"   "+hint)
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

// helpView is the "?" overlay: keys and commands at a glance.
func (m *Model) helpView(w int) string {
	rows := []string{
		accentStyle.Render("keys"),
		dimStyle.Render("  enter        send · ctrl+j  newline"),
		dimStyle.Render("  alt+enter    queue a follow-up while working"),
		dimStyle.Render("  esc          interrupt the turn"),
		dimStyle.Render("  tab          cycle permission mode (shift+tab back)"),
		dimStyle.Render("  ctrl+r       expand / collapse tool results"),
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
func (m *Model) fit(layers []string) []string {
	if m.height <= 0 {
		return layers
	}
	// Reserve rows for the composer (its live height + borders + the
	// footer mode line + the status/blank line above it) so long
	// transcripts trim, not the composer.
	reserved := m.composer.Height() + 6
	if m.working {
		reserved += 2
	}
	max := m.height - reserved
	if max < 1 {
		max = 1
	}
	if len(layers) <= max {
		return layers
	}
	dropped := len(layers) - max
	out := make([]string, 0, max+1)
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
