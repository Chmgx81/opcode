package tui

import "github.com/charmbracelet/lipgloss"

// Design tokens — tilde's own identity. The brand is a deep-forest
// green stack (not Claude's coral): one electric green for action and
// attention, a grounded green for secondary accents, two dark greens
// for panels and depth, near-black for the terminal floor. Everything
// the UI renders pulls from here so the look stays consistent and
// swappable in one place.
var (
	// Brand greens.
	Accent  = lipgloss.Color("#16DB65") // electric green: prompts, active markers
	Accent2 = lipgloss.Color("#058C42") // grounded green: secondary emphasis
	Deep    = lipgloss.Color("#04471C") // deep forest: panel borders
	Deep2   = lipgloss.Color("#0D2818") // darkest green: subtle fills
	Floor   = lipgloss.Color("#020202") // terminal floor

	// Semantic colors.
	Danger  = lipgloss.Color("#F87171") // errors, denials
	Warning = lipgloss.Color("#FBBF24") // caution, truncation
	Info    = lipgloss.Color("#7DD3FC") // neutral notices
	Dim     = lipgloss.Color("#5B6B60") // muted text, hints (green-tinted gray)
)

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
)

// Spacing scale — the rhythm between blocks.
const (
	SpaceAfterGreeting = 1 // blank lines after the identity block
	SpaceBeforeStatus  = 1 // blank lines between transcript and status
)

// Styles derived from the tokens.
var (
	accentStyle   = lipgloss.NewStyle().Foreground(Accent)
	accent2Style  = lipgloss.NewStyle().Foreground(Accent2)
	dimStyle      = lipgloss.NewStyle().Foreground(Dim)
	infoStyle     = lipgloss.NewStyle().Foreground(Info)
	warnStyle     = lipgloss.NewStyle().Foreground(Warning)
	dangerStyle   = lipgloss.NewStyle().Foreground(Danger)
	okStyle       = lipgloss.NewStyle().Foreground(Accent)
	errorStyle    = dangerStyle
	resultStyle   = dimStyle
	promptStyle   = lipgloss.NewStyle().Foreground(Warning).Bold(true)
	steerStyle    = warnStyle
	queuedStyle   = dimStyle
	toolNameStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#E7EFE9")).Bold(true)
	boldStyle     = lipgloss.NewStyle().Bold(true)
	codeStyle     = lipgloss.NewStyle().Foreground(Accent2)
	fenceStyle    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(Deep).Padding(0, 1)

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
)

// remapLegacyStyles keeps older references building during the token
// migration; new code must use the tokens above.
var (
	userStyle    = accentStyle
	toolStyle    = accentStyle
	resultStyle2 = resultStyle
)
