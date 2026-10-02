package tui

import (
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

// Palette — the TUI & UX Specification's design tokens (Section 2.1,
// docs/specs/tui-ux-spec.md). Body text uses the terminal's default
// foreground (HexText empty = no fg set); one teal accent; muted and
// subtle grays; two surfaces (user block, code). Every rendered color
// (UI, markdown, diffs) pulls from these values so the look stays
// consistent and swappable here.
//
// These are the DARK defaults. adaptTheme swaps in the light-legible
// set when the terminal reports a light background (or
// OPCODE_THEME=light).
var (
	HexAccent = "#2dd4bf" // accent: brand, focus, selection, prompt
	HexInfo   = "#60a5fa" // info: neutral notices, links
	HexDeep   = "#6e7683" // border: rules, dialog frames
	HexDeep2  = "#262a31" // surface.user: user-message block bg
	HexCode   = "#1c2026" // surface.code: code block bg
	HexText   = ""        // fg: terminal default (empty = no fg style)

	// Semantic colors.
	HexSuccess = "#4ade80" // ok, added
	HexDanger  = "#f87171" // errors, removed
	HexWarning = "#fbbf24" // caution, pending, denied
	HexDim     = "#9aa0a6" // fg.muted: metadata, results, args
	HexSubtle  = "#8b93a0" // fg.subtle: hints, chrome, placeholders
	// Text colors are held to 4.5:1 against the two surfaces they sit
	// on (the terminal floor and the code panel); HexDeep is a boundary
	// and is held to the 3:1 a UI edge needs. The subtle gray and the
	// border were both under their bars — the placeholder and the
	// hints at 3.9:1, the composer's own rules at 2.0:1, which is a
	// frame the eye cannot find. theme_test.go's contrast table is why
	// these values are what they are.
)

// adaptTheme re-skins the palette for the terminal's actual
// background. Dark is the default posture (and what undetectable
// terminals fall back to); a light background gets the spec's light
// tokens — light text on a white terminal is invisible, and that is
// the failure this exists to prevent. A thin wrapper over the theme
// table so the background probe and the /theme picker share one
// mechanism.
func adaptTheme(dark bool) {
	if dark {
		applyThemeName("dark")
		return
	}
	applyThemeName("light")
}

// theme is one named palette: every Hex token plus the one-line
// description the picker shows. Curated, not user-extensible — a
// hand-typed hex that renders illegibly is a support ticket, not a
// feature.
type theme struct {
	name, desc string
	hex        [10]string // accent, info, deep, deep2, code, success, danger, warning, dim, subtle
}

// themes are ordered as the picker lists them. "dark" is the teal
// default; "green" is the original Phase 7 brand stack (electric
// green on near-black) kept alive as a choice.
var themes = []theme{
	{"dark", "teal accent on dark — the default", [10]string{
		"#2dd4bf", "#60a5fa", "#6e7683", "#262a31", "#1c2026",
		"#4ade80", "#f87171", "#fbbf24", "#9aa0a6", "#8b93a0"}},
	{"light", "for light terminal backgrounds", [10]string{
		"#0f766e", "#1d4ed8", "#7f858d", "#eef0f3", "#f5f6f8",
		"#15803d", "#b91c1c", "#b45309", "#5f6368", "#666b70"}},
	{"green", "the original brand stack — electric green on near-black", [10]string{
		"#16db65", "#60a5fa", "#3a7a53", "#0d2818", "#0a1f14",
		"#4ade80", "#f87171", "#fbbf24", "#8fa898", "#829486"}},
}

// ThemeNames lists the valid theme names, in picker order.
func ThemeNames() []string {
	names := make([]string, len(themes))
	for i, t := range themes {
		names[i] = t.name
	}
	return names
}

// ValidTheme reports whether name is a real theme.
func ValidTheme(name string) bool {
	for _, t := range themes {
		if t.name == name {
			return true
		}
	}
	return false
}

// ThemeDesc returns the picker's one-line description for a theme.
func ThemeDesc(name string) string {
	for _, t := range themes {
		if t.name == name {
			return t.desc
		}
	}
	return ""
}

// curPalette records which named palette is installed; Run seeds the
// model's curTheme from it when the theme is auto (probe-decided).
var curPalette = "dark"

// applyThemeName installs a named palette: every Hex token, every
// derived style, and a dropped glamour cache (renderers embed the
// palette at creation). Unknown names change nothing and report
// false — the caller falls back to its own posture.
func applyThemeName(name string) bool {
	var t theme
	found := false
	for _, cand := range themes {
		if cand.name == name {
			t, found = cand, true
			break
		}
	}
	if !found {
		return false
	}
	curPalette = t.name
	HexAccent = t.hex[0]
	HexInfo = t.hex[1]
	HexDeep = t.hex[2]
	HexDeep2 = t.hex[3]
	HexCode = t.hex[4]
	HexText = ""
	HexSuccess = t.hex[5]
	HexDanger = t.hex[6]
	HexWarning = t.hex[7]
	HexDim = t.hex[8]
	HexSubtle = t.hex[9]
	refreshTokens()
	// Glamour renderers embed the style config at creation; drop the
	// cache so post-switch renders pick up the swapped palette.
	mdMu.Lock()
	mdRenderers = map[int]*glamour.TermRenderer{}
	mdMu.Unlock()
	return true
}

// Design tokens derived from the palette; refreshTokens (re)builds
// them so adaptTheme's swap reaches every style. Only the surfaces a
// render reads directly are kept as colors — everything else is
// expressed as a style below, so no token is carried and never used.
var Deep2 lipgloss.Color

func init() { refreshTokens() }

// refreshTokens (re)derives every Style from the current Hex values —
// the one place the palette turns into rendered looks. The colors are
// built inline rather than kept as package vars: a token nothing
// renders through is a token that rots.
func refreshTokens() {
	// The user-message surface is read directly by the panel renderer,
	// which fills rows rather than styling text.
	Deep2 = lipgloss.Color(HexDeep2)

	accentStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(HexAccent))
	accent2Style = lipgloss.NewStyle().Foreground(lipgloss.Color(HexInfo))
	dimStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(HexDim))
	subtleStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(HexSubtle))
	infoStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(HexInfo))
	warnStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(HexWarning))
	dangerStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(HexDanger))
	okStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(HexSuccess))
	resultStyle = dimStyle
	// Prompt titles are bold default-fg (spec 2.3: bold for names and
	// labels); amber stays on the attention box border, not the words.
	promptStyle = lipgloss.NewStyle().Bold(true)
	steerStyle = warnStyle
	queuedStyle = dimStyle
	toolNameStyle = lipgloss.NewStyle().Bold(true)
	boldStyle = lipgloss.NewStyle().Bold(true)

	promptBoxStyle = lipgloss.NewStyle().
		Border(dialogBorder()).
		BorderForeground(lipgloss.Color(HexWarning)).
		Padding(0, 1)
	paletteStyle = lipgloss.NewStyle().
		Border(dialogBorder()).
		BorderForeground(lipgloss.Color(HexAccent)).
		Padding(0, 1)
	helpStyle = lipgloss.NewStyle().
		Border(dialogBorder()).
		BorderForeground(lipgloss.Color(HexDeep)).
		Padding(0, 1)
	// The composer draws the same box every overlay draws — one box
	// language for every surface the user acts on. The border is the
	// boundary token at rest; shell mode repaints it amber (the state
	// signal the composer's old full-width rules carried).
	composerStyle = lipgloss.NewStyle().
		Border(dialogBorder()).
		BorderForeground(lipgloss.Color(HexDeep)).
		Padding(0, 1)
}

// plainBorder is the ASCII box: lipgloss composes the frame itself, so
// the rounded corners are a mark the glyph vocabulary does not own —
// the same gap tableGlyph closes for markdown's tables. The dialog
// bodies are single columns, so there are no mid-edges to draw.
var plainBorder = lipgloss.Border{
	Top: "-", Bottom: "-", Left: "|", Right: "|",
	TopLeft: "+", TopRight: "+", BottomLeft: "+", BottomRight: "+",
	MiddleLeft: "+", MiddleRight: "+",
}

// dialogBorder is the box every floating overlay draws. It follows the
// posture: refreshTokens is what builds the box styles, and it runs
// again on a /theme switch, which is after the glyph swap.
func dialogBorder() lipgloss.Border {
	if plainPosture {
		return plainBorder
	}
	return lipgloss.RoundedBorder()
}

// Glyph vocabulary — the spec's Section 2.4. A small, fixed set so
// the timeline reads as a system rather than an assortment. The
// defaults are the Unicode forms; adaptGlyphs swaps in ASCII for
// the plain posture (--plain, OPCODE_PLAIN, or a detected screen
// reader) so every glyph degrades, none disappears.
var (
	GlyphBrand   = "◈" // the product's mark: header, composer prompt, and toast frames
	GlyphShell   = "!" // the composer's shell-mode prompt
	GlyphPrompt  = "❯" // composer prompt, picker filter, selection
	GlyphUser    = "❯" // user-message block prefix (fg.muted per 2.3)
	GlyphBullet  = "●" // assistant / tool activity marker
	GlyphBranch  = "⎿" // tool result connector
	GlyphCaret   = "❯" // palette / list selection
	GlyphOK      = "✓" // success notes
	GlyphError   = "✗" // errors
	GlyphWarn    = "⚠" // warnings
	GlyphInfo    = "·" // neutral facts (doctor rows)
	GlyphDeleted = "−" // diff: removed
	GlyphAdded   = "+" // diff: added
	GlyphDoing   = "◐" // todo: the item in progress
	GlyphTodoOn  = "☑" // todo: done
	GlyphTodoOff = "☐" // todo: pending
	GlyphQueued  = "⏵" // queued follow-up
	GlyphUpdate  = "↑" // footer: a newer release is available
	GlyphMask    = "•" // one masked character of a hidden secret
	GlyphJoin    = "⏎" // a line break inside a collapsed result

	// Mode glyphs — the footer's mode line carries its own shape so
	// the brand ~ stays the composer's alone. Three modes since
	// Phase 30; there is no fourth glyph to keep in step.
	GlyphModePlan     = "⏸"  // plan: writes paused
	GlyphModeBuild    = "›"  // build: the ball is in your court
	GlyphModeFullAuto = "⏵⏵" // full-auto: everything proceeds
	GlyphThought      = "△"  // reasoning: the model's thinking block

	// Chrome marks — punctuation rather than vocabulary, but still
	// marks, so the plain posture degrades them with the rest: the
	// separator that joins the segments of a line.
	GlyphSep = "·"
)

// adaptGlyphs installs the vocabulary for the posture: Unicode by
// default, ASCII when plain. Called once from Run; both directions
// are explicit so the function is idempotent.
func adaptGlyphs(plain bool) {
	plainPosture = plain
	// Glamour renderers embed the vocabulary at creation (task
	// marks, table rules), so the cache has to go with the swap for
	// the same reason a theme switch drops it.
	mdMu.Lock()
	mdRenderers = map[int]*glamour.TermRenderer{}
	mdMu.Unlock()
	// The box borders follow the posture, and the box styles are built
	// by refreshTokens — so the swap has to run it, whichever order
	// the theme and the posture are applied in.
	defer refreshTokens()
	if !plain {
		GlyphBrand = "◈"
		GlyphPrompt, GlyphUser, GlyphCaret = "❯", "❯", "❯"
		GlyphBullet, GlyphBranch = "●", "⎿"
		GlyphOK, GlyphError, GlyphWarn, GlyphInfo = "✓", "✗", "⚠", "·"
		GlyphDeleted, GlyphAdded = "−", "+"
		GlyphDoing, GlyphTodoOn, GlyphTodoOff = "◐", "☑", "☐"
		GlyphQueued, GlyphThought = "⏵", "△"
		GlyphModePlan = "⏸"
		GlyphModeBuild, GlyphModeFullAuto = "›", "⏵⏵"
		GlyphUpdate, GlyphMask = "↑", "•"
		GlyphSep, GlyphJoin = "·", "⏎"
		return
	}
	GlyphBrand = "*"
	GlyphPrompt, GlyphUser, GlyphCaret = ">", ">", ">"
	GlyphBullet, GlyphBranch = "*", "\\-"
	GlyphOK, GlyphError, GlyphWarn, GlyphInfo = "[ok]", "[x]", "[!]", "-"
	GlyphDeleted, GlyphAdded = "-", "+"
	GlyphDoing, GlyphTodoOn, GlyphTodoOff = "@", "[x]", "[ ]"
	GlyphQueued, GlyphThought = ">", "^"
	GlyphModePlan = "="
	GlyphModeBuild, GlyphModeFullAuto = ">", ">>"
	GlyphSep = "-"
	// The update badge is a caret under the version it refers to;
	// GlyphThought's "^" only ever heads a collapsed thinking block,
	// so the two do not read as the same mark. The mask is a plain
	// asterisk: a screen reader should hear nothing per character
	// anyway, and a bullet is not ASCII.
	GlyphUpdate, GlyphMask = "^", "*"
	GlyphJoin = "|"
}

// plainPosture records the posture adaptGlyphs installed. The
// vocabulary covers the timeline's own marks; the few marks glamour
// composes (table rules, quote bars) ask this instead, so --plain
// never falls back to a hardcoded Unicode glyph.
var plainPosture bool

// plainOr picks between a Unicode mark and its ASCII stand-in by
// posture. For the marks that are punctuation rather than vocabulary —
// an arrow pair, an ellipsis — the vocabulary is the wrong home: they
// would double its size to hold one line of chrome.
func plainOr(unicode, ascii string) string {
	if plainPosture {
		return ascii
	}
	return unicode
}

// Styles derived from the tokens; populated by refreshTokens.
var (
	accentStyle, accent2Style, dimStyle, subtleStyle, infoStyle, warnStyle,
	dangerStyle, okStyle, resultStyle, promptStyle,
	steerStyle, queuedStyle, toolNameStyle, boldStyle,
	promptBoxStyle, paletteStyle, helpStyle, composerStyle lipgloss.Style
)
