package tui

import (
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/charmbracelet/lipgloss"
)

// Syntax highlighting for the edit_file diff view (chroma, the same
// engine glamour uses for code blocks). Token colors mirror
// markdown.go's chroma registry entry so inline diffs and code blocks
// agree; unknown token types stay unstyled rather than guessed.

// diffLexer resolves a lexer for a file path, memoized: paths repeat
// across a session's edits and lexers.Match is a linear scan.
var diffLexers = map[string]chroma.Lexer{}

func lexerFor(path string) chroma.Lexer {
	if l, ok := diffLexers[path]; ok {
		return l
	}
	l := lexers.Match(path)
	if l != nil {
		l = chroma.Coalesce(l)
	}
	diffLexers[path] = l
	return l
}

// highlightLine colorizes one source line. No lexer or a tokenise
// failure returns the line untouched — highlighting must never cost
// content.
func highlightLine(src string, l chroma.Lexer) string {
	if l == nil {
		return src
	}
	it, err := l.Tokenise(nil, src)
	if err != nil {
		return src
	}
	var b strings.Builder
	for _, tok := range it.Tokens() {
		b.WriteString(tokenStyle(tok.Type).Render(tok.Value))
	}
	return b.String()
}

// tokenStyle maps chroma token categories onto the brand palette —
// the same mapping markdown.go registers with glamour.
func tokenStyle(t chroma.TokenType) lipgloss.Style {
	switch {
	case t.InCategory(chroma.Comment):
		return dimStyle.Italic(true)
	case t.InCategory(chroma.Keyword):
		return accentStyle.Bold(true)
	case t.InCategory(chroma.Operator), t == chroma.Punctuation:
		return dimStyle
	case t.InCategory(chroma.LiteralString):
		return infoStyle
	case t == chroma.LiteralNumber:
		return warnStyle
	case t.InCategory(chroma.NameFunction), t.InCategory(chroma.NameDecorator):
		return accentStyle
	case t.InCategory(chroma.NameClass), t == chroma.NameConstant:
		return warnStyle
	case t.InCategory(chroma.NameBuiltin), t.InCategory(chroma.NameTag):
		return accent2Style
	default:
		return lipgloss.NewStyle()
	}
}
