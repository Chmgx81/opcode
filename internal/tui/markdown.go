package tui

import (
	"strings"
	"sync"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/ansi"
	"github.com/charmbracelet/lipgloss"
)

// Assistant markdown, rendered with glamour through the brand palette
// (Section 12's one-place reskin rule): green headings, muted-green
// links and inline code, a dark-green code panel with syntax tokens
// tinted toward the brand.
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
			BlockPrefix: "\n", BlockSuffix: "\n", Color: strPtr("#E7EFE9"),
		}},
		Paragraph: ansi.StyleBlock{},
		Heading:   ansi.StyleBlock{StylePrimitive: ansi.StylePrimitive{Bold: boolPtr(true)}},
		H1:        ansi.StyleBlock{StylePrimitive: ansi.StylePrimitive{Color: strPtr("#16DB65"), Bold: boolPtr(true)}},
		H2:        ansi.StyleBlock{StylePrimitive: ansi.StylePrimitive{Color: strPtr("#16DB65")}},
		H3:        ansi.StyleBlock{StylePrimitive: ansi.StylePrimitive{Color: strPtr("#058C42"), Bold: boolPtr(true)}},
		H4:        ansi.StyleBlock{StylePrimitive: ansi.StylePrimitive{Color: strPtr("#058C42")}},
		H5:        ansi.StyleBlock{StylePrimitive: ansi.StylePrimitive{Color: strPtr("#058C42")}},
		H6:        ansi.StyleBlock{StylePrimitive: ansi.StylePrimitive{Color: strPtr("#5B6B60"), Bold: boolPtr(true)}},
		Text:      ansi.StylePrimitive{},
		Strong:    ansi.StylePrimitive{Bold: boolPtr(true)},
		Emph:      ansi.StylePrimitive{Italic: boolPtr(true)},
		Strikethrough: ansi.StylePrimitive{
			CrossedOut: boolPtr(true), Color: strPtr("#5B6B60"),
		},
		Link:     ansi.StylePrimitive{Color: strPtr("#7DD3FC"), Underline: boolPtr(true)},
		LinkText: ansi.StylePrimitive{Color: strPtr("#058C42")},
		Item: ansi.StylePrimitive{
			BlockPrefix: "• ", Color: strPtr("#16DB65"),
		},
		Enumeration: ansi.StylePrimitive{
			BlockPrefix: ". ", Color: strPtr("#16DB65"), Bold: boolPtr(true),
		},
		Task: ansi.StyleTask{
			Ticked:   GlyphOK,
			Unticked: GlyphWarn,
		},
		Code: ansi.StyleBlock{StylePrimitive: ansi.StylePrimitive{
			Color: strPtr("#16DB65"),
		}},
		CodeBlock: ansi.StyleCodeBlock{
			StyleBlock: ansi.StyleBlock{
				Margin: uintPtr(2),
				StylePrimitive: ansi.StylePrimitive{
					Color: strPtr("#C4C4C4"), BackgroundColor: strPtr("#0D2818"),
				},
			},
			// A custom chroma registry entry, not a theme name:
			// tokens tinted toward the brand instead of a stock
			// palette.
			Chroma: &ansi.Chroma{
				Text:              ansi.StylePrimitive{Color: strPtr("#E7EFE9")},
				Error:             ansi.StylePrimitive{Color: strPtr("#F87171")},
				Comment:           ansi.StylePrimitive{Color: strPtr("#5B6B60"), Italic: boolPtr(true)},
				CommentPreproc:    ansi.StylePrimitive{Color: strPtr("#5B6B60")},
				Keyword:           ansi.StylePrimitive{Color: strPtr("#16DB65")},
				KeywordReserved:   ansi.StylePrimitive{Color: strPtr("#16DB65")},
				KeywordType:       ansi.StylePrimitive{Color: strPtr("#FBBF24")},
				Operator:          ansi.StylePrimitive{Color: strPtr("#16DB65")},
				Punctuation:       ansi.StylePrimitive{Color: strPtr("#9AA79D")},
				Name:              ansi.StylePrimitive{Color: strPtr("#E7EFE9")},
				NameBuiltin:       ansi.StylePrimitive{Color: strPtr("#7DD3FC")},
				NameTag:           ansi.StylePrimitive{Color: strPtr("#16DB65")},
				NameAttribute:     ansi.StylePrimitive{Color: strPtr("#7DD3FC")},
				NameClass:         ansi.StylePrimitive{Color: strPtr("#FBBF24")},
				NameConstant:      ansi.StylePrimitive{Color: strPtr("#FBBF24")},
				NameDecorator:     ansi.StylePrimitive{Color: strPtr("#FBBF24")},
				NameFunction:      ansi.StylePrimitive{Color: strPtr("#16DB65")},
				NameException:     ansi.StylePrimitive{Color: strPtr("#F87171")},
				Literal:           ansi.StylePrimitive{Color: strPtr("#FBBF24")},
				LiteralNumber:     ansi.StylePrimitive{Color: strPtr("#FBBF24")},
				LiteralString:     ansi.StylePrimitive{Color: strPtr("#7DD3FC")},
				GenericDeleted:    ansi.StylePrimitive{Color: strPtr("#F87171")},
				GenericInserted:   ansi.StylePrimitive{Color: strPtr("#16DB65")},
				GenericSubheading: ansi.StylePrimitive{Color: strPtr("#058C42")},
			},
		},
		Table: ansi.StyleTable{
			StyleBlock: ansi.StyleBlock{
				StylePrimitive: ansi.StylePrimitive{Color: strPtr("#E7EFE9")},
			},
			CenterSeparator: strPtr("┼"),
			ColumnSeparator: strPtr("│"),
			RowSeparator:    strPtr("─"),
		},
		BlockQuote: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Color: strPtr("#5B6B60"), Italic: boolPtr(true),
			},
			IndentToken: strPtr("│ "),
		},
		HorizontalRule: ansi.StylePrimitive{Color: strPtr("#04471C")},
	}
}

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
// never cost content.
func renderMarkdown(text string, width int) []string {
	r, err := mdRenderer(maxInt(width-4, 20))
	if err != nil {
		return renderAssistant(text, width)
	}
	out, err := r.Render(text)
	if err != nil {
		return renderAssistant(text, width)
	}
	lines := strings.Split(strings.Trim(out, "\n"), "\n")
	var body []string
	for _, l := range lines {
		if strings.TrimSpace(l) == "" && (len(body) == 0) {
			continue
		}
		body = append(body, l)
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
