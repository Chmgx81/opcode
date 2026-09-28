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
	HexFloor  = "#020202" // terminal floor
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
// the failure this exists to prevent.
func adaptTheme(dark bool) {
	if dark {
		return
	}
	HexAccent = "#0f766e"
	HexInfo = "#1d4ed8"
	HexDeep = "#c5cad1"
	HexDeep2 = "#eef0f3"
	HexCode = "#f5f6f8"
	HexText = ""
	HexSuccess = "#15803d"
	HexDanger = "#b91c1c"
	HexWarning = "#b45309"
	HexDim = "#5f6368"
	HexSubtle = "#80868b"
	refreshTokens()
	// Glamour renderers embed the style config at creation; drop the
	// cache so post-adapt renders pick up the swapped palette.
	mdMu.Lock()
	mdRenderers = map[int]*glamour.TermRenderer{}
	mdMu.Unlock()
}

// Design tokens derived from the palette; refreshTokens (re)builds
// them so adaptTheme's swap reaches every style.
var (
	Accent, Accent2, Deep, Deep2, Code, Floor, Success, Danger, Warning, Info, Dim, Subtle lipgloss.Color
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
	Floor = lipgloss.Color(HexFloor)
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
	errorStyle = dangerStyle
	resultStyle = dimStyle
	// Prompt titles are bold default-fg (spec 2.3: bold for names and
	// labels); amber stays on the attention box border, not the words.
	promptStyle = lipgloss.NewStyle().Bold(true)
	steerStyle = warnStyle
	queuedStyle = dimStyle
	toolNameStyle = lipgloss.NewStyle().Bold(true)
	boldStyle = lipgloss.NewStyle().Bold(true)
	codeStyle = lipgloss.NewStyle().Foreground(Accent2)

	boxStyle = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(Deep).
		Padding(0, 1)
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
// the timeline reads as a system rather than an assortment. (The
// ASCII fallback set is a listed follow-up; today the Unicode forms
// are the product's minimum requirement.)
const (
	GlyphBrand   = "~" // the product's own name, in the header
	GlyphPrompt  = "❯" // composer prompt, picker filter, selection
	GlyphUser    = "❯" // user-message block prefix (fg.muted per 2.3)
	GlyphBullet  = "●" // assistant / tool activity marker
	GlyphBranch  = "⎿" // tool result connector
	GlyphCaret   = "❯" // palette / list selection
	GlyphOK      = "✓" // success notes
	GlyphError   = "✗" // errors
	GlyphWarn    = "⚠" // warnings
	GlyphDeleted = "−" // diff: removed
	GlyphAdded   = "+" // diff: added
	GlyphDoing   = "◐" // todo: the item in progress
	GlyphTodoOn  = "☑" // todo: done
	GlyphTodoOff = "☐" // todo: pending
	GlyphQueued  = "⏵" // queued follow-up
	GlyphThought = "△" // reasoning: the model's thinking block
)

// Spacing scale — the rhythm between blocks.
const (
	SpaceAfterGreeting = 1 // blank lines after the identity block
	SpaceBeforeStatus  = 1 // blank lines between transcript and status
)

// Styles derived from the tokens; populated by refreshTokens.
var (
	accentStyle, accent2Style, dimStyle, subtleStyle, infoStyle, warnStyle,
	dangerStyle, okStyle, errorStyle, resultStyle, promptStyle,
	steerStyle, queuedStyle, toolNameStyle, boldStyle, codeStyle,
	boxStyle, promptBoxStyle, paletteStyle, helpStyle lipgloss.Style
)

// remapLegacyStyles keeps older references building during the token
// migration; new code must use the tokens above.
var (
	userStyle    = accentStyle
	toolStyle    = accentStyle
	resultStyle2 = resultStyle
)
