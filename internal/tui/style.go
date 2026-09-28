package tui

import (
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

// Palette — tilde's identity in one place. The brand is an ice-cyan
// stack: one electric cyan for action and attention, a grounded deep
// cyan for secondary accents, two dark teals for panels and depth, and
// the near-black terminal floor. Every rendered color (UI, markdown,
// diffs) pulls from these values so the look stays consistent and
// swappable here.
//
// These are the DARK defaults — the common terminal. adaptTheme swaps
// in a light-legible set when the terminal reports a light background
// (or TILDE_THEME=light): light text becomes dark ink, fills become
// light tints, and accents deepen for contrast on white (Codex's
// theme-adaptive approach at tilde's scale).
var (
	HexAccent  = "#22D3EE" // electric cyan: prompts, active markers
	HexAccent2 = "#0891B2" // grounded cyan: secondary emphasis
	HexDeep    = "#0B3A47" // dark teal: panel borders
	HexDeep2   = "#06222B" // darkest teal: subtle fills
	HexFloor   = "#020202" // terminal floor
	HexText    = "#E6F2F5" // near-white body text (cool-tinted)

	// Semantic colors.
	HexDanger  = "#F87171" // errors, denials, removals
	HexWarning = "#FBBF24" // caution, truncation, numbers
	HexInfo    = "#93C5FD" // neutral notices, links
	HexDim     = "#7A8B94" // muted text, hints (cool gray)
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
	HexAccent = "#0E7490" // cyan-700: legible on white
	HexAccent2 = "#155E75"
	HexDeep = "#A8C4CE"  // light panel borders
	HexDeep2 = "#E4EDF1" // light fills (the user panel, code blocks)
	HexText = "#1B2A32"  // dark ink
	HexDanger = "#B91C1C"
	HexWarning = "#A16207"
	HexInfo = "#1D4ED8"
	HexDim = "#5A6B74"
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
	Accent, Accent2, Deep, Deep2, Floor, Danger, Warning, Info, Dim lipgloss.Color
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
	okStyle = lipgloss.NewStyle().Foreground(Accent)
	errorStyle = dangerStyle
	resultStyle = dimStyle
	promptStyle = lipgloss.NewStyle().Foreground(Warning).Bold(true)
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
	GlyphBullet  = "●" // assistant / tool activity marker
	GlyphBranch  = "└" // tool result, indented under its call
	GlyphCaret   = "▹" // palette / list selection
	GlyphOK      = "✓" // success notes
	GlyphWarn    = "!" // warnings
	GlyphDeleted = "−" // diff: removed
	GlyphAdded   = "+" // diff: added
	GlyphDoing   = "▸" // todo: the item in progress
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
