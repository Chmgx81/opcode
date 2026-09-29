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
// TILDE_THEME=light).
var (
	HexAccent = "#2dd4bf" // accent: brand, focus, selection, prompt
	HexInfo   = "#60a5fa" // info: neutral notices, links
	HexDeep   = "#3f4650" // border: rules, dialog frames
	HexDeep2  = "#262a31" // surface.user: user-message block bg
	HexCode   = "#1c2026" // surface.code: code block bg
	HexText   = ""        // fg: terminal default (empty = no fg style)

	// Semantic colors.
	HexSuccess = "#4ade80" // ok, added
	HexDanger  = "#f87171" // errors, removed
	HexWarning = "#fbbf24" // caution, pending, denied
	HexDim     = "#9aa0a6" // fg.muted: metadata, results, args
	HexSubtle  = "#6b7280" // fg.subtle: hints, chrome, placeholders
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
		"#2dd4bf", "#60a5fa", "#3f4650", "#262a31", "#1c2026",
		"#4ade80", "#f87171", "#fbbf24", "#9aa0a6", "#6b7280"}},
	{"light", "for light terminal backgrounds", [10]string{
		"#0f766e", "#1d4ed8", "#c5cad1", "#eef0f3", "#f5f6f8",
		"#15803d", "#b91c1c", "#b45309", "#5f6368", "#80868b"}},
	{"green", "the original brand stack — electric green on near-black", [10]string{
		"#16db65", "#60a5fa", "#1d4a30", "#0d2818", "#0a1f14",
		"#4ade80", "#f87171", "#fbbf24", "#8fa898", "#5b6b5d"}},
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
// them so adaptTheme's swap reaches every style.
var (
	Accent, Accent2, Deep, Deep2, Code, Success, Danger, Warning, Info, Dim, Subtle lipgloss.Color
)

func init() { refreshTokens() }

// refreshTokens (re)derives every Color and Style from the current
// Hex values — the one place the palette turns into rendered looks.
func refreshTokens() {
	Accent = lipgloss.Color(HexAccent)
	Accent2 = lipgloss.Color(HexInfo) // secondary emphasis = info per the token table
	Deep = lipgloss.Color(HexDeep)
	Deep2 = lipgloss.Color(HexDeep2)
	Code = lipgloss.Color(HexCode)
	Success = lipgloss.Color(HexSuccess)
	Danger = lipgloss.Color(HexDanger)
	Warning = lipgloss.Color(HexWarning)
	Info = lipgloss.Color(HexInfo)
	Dim = lipgloss.Color(HexDim)
	Subtle = lipgloss.Color(HexSubtle)

	accentStyle = lipgloss.NewStyle().Foreground(Accent)
	accent2Style = lipgloss.NewStyle().Foreground(Accent2)
	dimStyle = lipgloss.NewStyle().Foreground(Dim)
	subtleStyle = lipgloss.NewStyle().Foreground(Subtle)
	infoStyle = lipgloss.NewStyle().Foreground(Info)
	warnStyle = lipgloss.NewStyle().Foreground(Warning)
	dangerStyle = lipgloss.NewStyle().Foreground(Danger)
	okStyle = lipgloss.NewStyle().Foreground(Success)
	resultStyle = dimStyle
	// Prompt titles are bold default-fg (spec 2.3: bold for names and
	// labels); amber stays on the attention box border, not the words.
	promptStyle = lipgloss.NewStyle().Bold(true)
	steerStyle = warnStyle
	queuedStyle = dimStyle
	toolNameStyle = lipgloss.NewStyle().Bold(true)
	boldStyle = lipgloss.NewStyle().Bold(true)
	ruleStyle = lipgloss.NewStyle().Foreground(Deep)

	promptBoxStyle = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(Warning).
		Padding(0, 1)
	paletteStyle = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(Accent).
		Padding(0, 1)
	helpStyle = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(Deep).
		Padding(0, 1)
}

// Glyph vocabulary — the spec's Section 2.4. A small, fixed set so
// the timeline reads as a system rather than an assortment. The
// defaults are the Unicode forms; adaptGlyphs swaps in ASCII for
// the plain posture (--plain, TILDE_PLAIN, or a detected screen
// reader) so every glyph degrades, none disappears.
var (
	GlyphBrand   = "~" // the product's own name: header and composer prompt
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

	// Mode glyphs — the footer's mode line carries its own shape so
	// the brand ~ stays the composer's alone.
	GlyphModeReadOnly = "○"  // read-only: nothing will run
	GlyphModePlan     = "⏸"  // plan: writes paused
	GlyphModeBuild    = "›"  // build: the ball is in your court
	GlyphModeFullAuto = "⏵⏵" // full-auto: everything proceeds
	GlyphThought      = "△"  // reasoning: the model's thinking block
)

// adaptGlyphs installs the vocabulary for the posture: Unicode by
// default, ASCII when plain. Called once from Run; both directions
// are explicit so the function is idempotent.
func adaptGlyphs(plain bool) {
	if !plain {
		GlyphPrompt, GlyphUser, GlyphCaret = "❯", "❯", "❯"
		GlyphBullet, GlyphBranch = "●", "⎿"
		GlyphOK, GlyphError, GlyphWarn, GlyphInfo = "✓", "✗", "⚠", "·"
		GlyphDeleted, GlyphAdded = "−", "+"
		GlyphDoing, GlyphTodoOn, GlyphTodoOff = "◐", "☑", "☐"
		GlyphQueued, GlyphThought = "⏵", "△"
		GlyphModeReadOnly, GlyphModePlan = "○", "⏸"
		GlyphModeBuild, GlyphModeFullAuto = "›", "⏵⏵"
		return
	}
	GlyphPrompt, GlyphUser, GlyphCaret = ">", ">", ">"
	GlyphBullet, GlyphBranch = "*", "\\-"
	GlyphOK, GlyphError, GlyphWarn, GlyphInfo = "[ok]", "[x]", "[!]", "-"
	GlyphDeleted, GlyphAdded = "-", "+"
	GlyphDoing, GlyphTodoOn, GlyphTodoOff = "@", "[x]", "[ ]"
	GlyphQueued, GlyphThought = ">", "^"
	GlyphModeReadOnly, GlyphModePlan = "o", "="
	GlyphModeBuild, GlyphModeFullAuto = ">", ">>"
}

// Spacing scale — the rhythm between blocks.
const (
	SpaceAfterGreeting = 1 // blank lines after the identity block
	SpaceBeforeStatus  = 1 // blank lines between transcript and status
)

// Styles derived from the tokens; populated by refreshTokens.
var (
	accentStyle, accent2Style, dimStyle, subtleStyle, infoStyle, warnStyle,
	dangerStyle, okStyle, resultStyle, promptStyle,
	steerStyle, queuedStyle, toolNameStyle, boldStyle, ruleStyle,
	promptBoxStyle, paletteStyle, helpStyle lipgloss.Style
)
