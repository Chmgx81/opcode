package tui

import (
	"strings"
	"sync"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/ansi"
	"github.com/charmbracelet/lipgloss"
)

// Assistant markdown, rendered with glamour through the brand palette
// (Section 12's one-place reskin rule): blue headings, soft-blue links
// and inline code, a neutral dark code panel with syntax tokens tinted
// toward the brand.
//
// A finished assistant entry renders once per width and is cached on
// the entry (see renderEntry): View runs every frame and re-rendering
// markdown per frame would visibly cost. The in-flight stream keeps
// the cheap plain renderer until it flushes.

var (
	mdMu        sync.Mutex
	mdRenderers = map[int]*glamour.TermRenderer{}
)

// brandMarkdown is tilde's glamour style. Colors mirror style.go's
// tokens so markdown and the rest of the UI agree.
func brandMarkdown() ansi.StyleConfig {
	return ansi.StyleConfig{
		Document: ansi.StyleBlock{StylePrimitive: ansi.StylePrimitive{
			BlockPrefix: "\n", BlockSuffix: "\n", Color: strPtr(HexText),
		}},
		Paragraph: ansi.StyleBlock{},
		Heading:   ansi.StyleBlock{StylePrimitive: ansi.StylePrimitive{Bold: boolPtr(true)}},
		H1:        ansi.StyleBlock{StylePrimitive: ansi.StylePrimitive{Color: strPtr(HexAccent), Bold: boolPtr(true)}},
		H2:        ansi.StyleBlock{StylePrimitive: ansi.StylePrimitive{Color: strPtr(HexAccent)}},
		H3:        ansi.StyleBlock{StylePrimitive: ansi.StylePrimitive{Color: strPtr(HexInfo), Bold: boolPtr(true)}},
		H4:        ansi.StyleBlock{StylePrimitive: ansi.StylePrimitive{Color: strPtr(HexInfo)}},
		H5:        ansi.StyleBlock{StylePrimitive: ansi.StylePrimitive{Color: strPtr(HexInfo)}},
		H6:        ansi.StyleBlock{StylePrimitive: ansi.StylePrimitive{Color: strPtr(HexDim), Bold: boolPtr(true)}},
		Text:      ansi.StylePrimitive{},
		Strong:    ansi.StylePrimitive{Bold: boolPtr(true)},
		Emph:      ansi.StylePrimitive{Italic: boolPtr(true)},
		Strikethrough: ansi.StylePrimitive{
			CrossedOut: boolPtr(true), Color: strPtr(HexDim),
		},
		Link:     ansi.StylePrimitive{Color: strPtr(HexInfo), Underline: boolPtr(true)},
		LinkText: ansi.StylePrimitive{Color: strPtr(HexInfo)},
		Item: ansi.StylePrimitive{
			BlockPrefix: tableGlyph("• ", "* "), Color: strPtr(HexAccent),
		},
		Enumeration: ansi.StylePrimitive{
			BlockPrefix: ". ", Color: strPtr(HexAccent), Bold: boolPtr(true),
		},
		Task: ansi.StyleTask{
			Ticked:   GlyphOK,
			Unticked: GlyphTodoOff,
		},
		Code: ansi.StyleBlock{StylePrimitive: ansi.StylePrimitive{
			Color: strPtr(HexAccent),
		}},
		CodeBlock: ansi.StyleCodeBlock{
			StyleBlock: ansi.StyleBlock{
				Margin: uintPtr(2),
				StylePrimitive: ansi.StylePrimitive{
					Color: strPtr(HexText), BackgroundColor: strPtr(HexCode),
				},
			},
			// A custom chroma registry entry, not a theme name:
			// tokens tinted toward the brand instead of a stock
			// palette.
			Chroma: &ansi.Chroma{
				Text:              ansi.StylePrimitive{Color: strPtr(HexText)},
				Error:             ansi.StylePrimitive{Color: strPtr(HexDanger)},
				Comment:           ansi.StylePrimitive{Color: strPtr(HexDim), Italic: boolPtr(true)},
				CommentPreproc:    ansi.StylePrimitive{Color: strPtr(HexDim)},
				Keyword:           ansi.StylePrimitive{Color: strPtr(HexAccent)},
				KeywordReserved:   ansi.StylePrimitive{Color: strPtr(HexAccent)},
				KeywordType:       ansi.StylePrimitive{Color: strPtr(HexWarning)},
				Operator:          ansi.StylePrimitive{Color: strPtr(HexAccent)},
				Punctuation:       ansi.StylePrimitive{Color: strPtr(HexDim)},
				Name:              ansi.StylePrimitive{Color: strPtr(HexText)},
				NameBuiltin:       ansi.StylePrimitive{Color: strPtr(HexInfo)},
				NameTag:           ansi.StylePrimitive{Color: strPtr(HexAccent)},
				NameAttribute:     ansi.StylePrimitive{Color: strPtr(HexInfo)},
				NameClass:         ansi.StylePrimitive{Color: strPtr(HexWarning)},
				NameConstant:      ansi.StylePrimitive{Color: strPtr(HexWarning)},
				NameDecorator:     ansi.StylePrimitive{Color: strPtr(HexWarning)},
				NameFunction:      ansi.StylePrimitive{Color: strPtr(HexAccent)},
				NameException:     ansi.StylePrimitive{Color: strPtr(HexDanger)},
				Literal:           ansi.StylePrimitive{Color: strPtr(HexWarning)},
				LiteralNumber:     ansi.StylePrimitive{Color: strPtr(HexWarning)},
				LiteralString:     ansi.StylePrimitive{Color: strPtr(HexInfo)},
				GenericDeleted:    ansi.StylePrimitive{Color: strPtr(HexDanger)},
				GenericInserted:   ansi.StylePrimitive{Color: strPtr(HexAccent)},
				GenericSubheading: ansi.StylePrimitive{Color: strPtr(HexInfo)},
			},
		},
		Table: ansi.StyleTable{
			StyleBlock: ansi.StyleBlock{
				StylePrimitive: ansi.StylePrimitive{Color: strPtr(HexText)},
			},
			CenterSeparator: strPtr(tableGlyph("┼", "|")),
			ColumnSeparator: strPtr(tableGlyph("│", "|")),
			RowSeparator:    strPtr(tableGlyph("─", "-")),
		},
		BlockQuote: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Color: strPtr(HexDim), Italic: boolPtr(true),
			},
			IndentToken: strPtr(tableGlyph("│ ", "| ")),
		},
		HorizontalRule: ansi.StylePrimitive{Color: strPtr(HexDeep)},
	}
}

// tableGlyph is a markdown table or quote mark. These were hardcoded
// Unicode, so --plain still drew box-drawing characters — the one
// place in the rendered frame the vocabulary did not reach.
func tableGlyph(unicode, ascii string) string { return plainOr(unicode, ascii) }

// mdRenderer returns the cached renderer for a wrap width, building it
// on first use. Renderers are process-lifetime: they are stateless
// pipelines and building per entry would be pure overhead.
func mdRenderer(width int) (*glamour.TermRenderer, error) {
	mdMu.Lock()
	defer mdMu.Unlock()
	if r, ok := mdRenderers[width]; ok {
		return r, nil
	}
	r, err := glamour.NewTermRenderer(
		glamour.WithStyles(brandMarkdown()),
		glamour.WithWordWrap(width),
		glamour.WithColorProfile(lipgloss.ColorProfile()),
	)
	if err != nil {
		return nil, err
	}
	mdRenderers[width] = r
	return r, nil
}

// renderMarkdown renders one finished assistant message. Any glamour
// failure falls back to the plain renderer — a styling problem must
// never cost content. LaTeX math converts to Unicode first (Phase
// 36): the reader sees α, not \alpha.
func renderMarkdown(text string, width int) []string {
	text = convertMath(text)
	// Glamour gets a floor of 20 columns because it misbehaves below
	// it; on a terminal narrower than that the rendered rows are
	// clipped back to the real width, so a 14-column pane gets 14
	// columns rather than 20 columns of overflow.
	wrapped := maxInt(width-4, 20)
	r, err := mdRenderer(wrapped)
	if err != nil {
		return wrapAll(text, width)
	}
	out, err := r.Render(text)
	if err != nil {
		return wrapAll(text, width)
	}
	lines := strings.Split(strings.Trim(out, "\n"), "\n")
	var body []string
	for _, l := range lines {
		if strings.TrimSpace(l) == "" && (len(body) == 0) {
			continue
		}
		body = append(body, clipCols(l, width))
	}
	// Drop glamour's trailing document margin too.
	for len(body) > 0 && strings.TrimSpace(body[len(body)-1]) == "" {
		body = body[:len(body)-1]
	}
	return body
}

func strPtr(s string) *string { return &s }
func boolPtr(b bool) *bool    { return &b }
func uintPtr(u uint) *uint    { return &u }
