package tui

import (
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

// Palette — Codex's design language (studied from codex-rs/tui):
// one ChatGPT-blue accent for keys and emphasis, neutral measured
// grays for secondary text and borders, a white-blend fill for user
// message blocks, muted amber for attention. Every rendered color
// (UI, markdown, diffs) pulls from these values so the look stays
// consistent and swappable here.
//
// These are the DARK defaults — the common terminal. adaptTheme swaps
// in a light-legible set when the terminal reports a light background
// (or TILDE_THEME=light): light text becomes dark ink, fills become
// light tints, and accents deepen for contrast on white (Codex's
// theme-adaptive approach at tilde's scale).
var (
	HexAccent  = "#63A8F8" // ChatGPT blue 200: keys, emphasis, selection
	HexAccent2 = "#3E82D6" // deeper blue: secondary emphasis, headings
	HexDeep    = "#404040" // neutral gray: panel borders
	HexDeep2   = "#292929" // white 16% blend: user panel, code blocks
	HexFloor   = "#020202" // terminal floor
	HexText    = "#E8E8E8" // near-white body text (neutral)

	// Semantic colors.
	HexSuccess = "#3FB950" // success notes, additions (terminal green)
	HexDanger  = "#F87171" // errors, denials, removals
	HexWarning = "#C4A767" // muted amber: caution, attention boxes
	HexInfo    = "#8FBFE8" // neutral notices, links
	HexDim     = "#999999" // secondary text (60% measured blend)
)

// adaptTheme re-skins the palette for the terminal's actual
// background. Dark is the default posture (and what undetectable
// terminals fall back to); a light background gets dark ink, light
// fills, and deepened accents — light text on a white terminal is
// invisible, and that is the failure this exists to prevent.
func adaptTheme(dark bool) {
	if dark {
		return
	}
	HexAccent = "#1C64C8" // Codex's light accent: legible on white
	HexAccent2 = "#27558F"
	HexDeep = "#B8B8B8"  // light panel borders
	HexDeep2 = "#F2F2F2" // light fills (4% black blend)
	HexText = "#1A1A1A"  // dark ink
	HexSuccess = "#1A7F37"
	HexDanger = "#B91C1C"
	HexWarning = "#8B6214"
	HexInfo = "#1D4ED8"
	HexDim = "#666666"
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
	Accent, Accent2, Deep, Deep2, Floor, Success, Danger, Warning, Info, Dim lipgloss.Color
)

func init() { refreshTokens() }

// refreshTokens (re)derives every Color and Style from the current
// Hex values — the one place the palette turns into rendered looks.
func refreshTokens() {
	Accent = lipgloss.Color(HexAccent)
	Accent2 = lipgloss.Color(HexAccent2)
	Deep = lipgloss.Color(HexDeep)
	Deep2 = lipgloss.Color(HexDeep2)
	Floor = lipgloss.Color(HexFloor)
	Success = lipgloss.Color(HexSuccess)
	Danger = lipgloss.Color(HexDanger)
	Warning = lipgloss.Color(HexWarning)
	Info = lipgloss.Color(HexInfo)
	Dim = lipgloss.Color(HexDim)

	accentStyle = lipgloss.NewStyle().Foreground(Accent)
	accent2Style = lipgloss.NewStyle().Foreground(Accent2)
	dimStyle = lipgloss.NewStyle().Foreground(Dim)
	infoStyle = lipgloss.NewStyle().Foreground(Info)
	warnStyle = lipgloss.NewStyle().Foreground(Warning)
	dangerStyle = lipgloss.NewStyle().Foreground(Danger)
	okStyle = lipgloss.NewStyle().Foreground(Success)
	errorStyle = dangerStyle
	resultStyle = dimStyle
	// Prompt titles are bold neutral (Codex's shape); amber stays on
	// the attention box border, not the words.
	promptStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(HexText)).Bold(true)
	steerStyle = warnStyle
	queuedStyle = dimStyle
	toolNameStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(HexText)).Bold(true)
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
		BorderForeground(Accent2).
		Padding(0, 1)
	helpStyle = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(Accent2).
		Padding(0, 1)
}

// Glyph vocabulary — a small, fixed set so the timeline reads as a
// system rather than an assortment.
const (
	GlyphPrompt  = "~" // the composer prompt: the product's own name
	GlyphUser    = "›" // transcript user message prefix (Codex's shape)
	GlyphBullet  = "●" // assistant / tool activity marker
	GlyphBranch  = "└" // tool result, indented under its call
	GlyphCaret   = "▹" // palette / list selection
	GlyphOK      = "✓" // success notes
	GlyphWarn    = "!" // warnings
	GlyphDeleted = "−" // diff: removed
	GlyphAdded   = "+" // diff: added
	GlyphDoing   = "▸" // todo: the item in progress
	GlyphThought = "△" // reasoning: the model's thinking block
)

// Spacing scale — the rhythm between blocks.
const (
	SpaceAfterGreeting = 1 // blank lines after the identity block
	SpaceBeforeStatus  = 1 // blank lines between transcript and status
)

// Styles derived from the tokens; populated by refreshTokens.
var (
	accentStyle, accent2Style, dimStyle, infoStyle, warnStyle,
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
