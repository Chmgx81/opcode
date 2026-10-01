package tui

import (
	"strings"
	"unicode/utf8"
)

// LaTeX → Unicode conversion at the display boundary. Models write
// math in LaTeX because their training data does; a terminal reader
// should see α, not \alpha. Conversion is deliberately conservative:
// only regions that unambiguously look like math are touched, and
// everything else — currency, shell variables, code — must survive
// byte for byte.

// latexSymbols is the common core: greek letters, relations, arrows,
// big operators, set notation. Unknown commands degrade to their
// bare name, which stays readable instead of vanishing.
var latexSymbols = map[string]string{
	// Greek, lower then upper.
	"alpha": "α", "beta": "β", "gamma": "γ", "delta": "δ", "epsilon": "ε",
	"varepsilon": "ε", "zeta": "ζ", "eta": "η", "theta": "θ", "vartheta": "ϑ",
	"iota": "ι", "kappa": "κ", "lambda": "λ", "mu": "μ", "nu": "ν", "xi": "ξ",
	"pi": "π", "rho": "ρ", "sigma": "σ", "tau": "τ", "upsilon": "υ",
	"phi": "φ", "varphi": "φ", "chi": "χ", "psi": "ψ", "omega": "ω",
	"Gamma": "Γ", "Delta": "Δ", "Theta": "Θ", "Lambda": "Λ", "Xi": "Ξ",
	"Pi": "Π", "Sigma": "Σ", "Upsilon": "Υ", "Phi": "Φ", "Psi": "Ψ", "Omega": "Ω",

	// Relations and operators.
	"times": "×", "div": "÷", "pm": "±", "mp": "∓", "cdot": "·",
	"leq": "≤", "geq": "≥", "neq": "≠", "ne": "≠", "approx": "≈",
	"equiv": "≡", "sim": "∼", "propto": "∝", "ll": "≪", "gg": "≫",
	"in": "∈", "notin": "∉", "ni": "∋", "subset": "⊂", "supset": "⊃",
	"subseteq": "⊆", "cup": "∪", "cap": "∩", "setminus": "∖",
	"emptyset": "∅", "varnothing": "∅", "infty": "∞", "partial": "∂",
	"nabla": "∇", "forall": "∀", "exists": "∃", "neg": "¬", "land": "∧",
	"lor": "∨", "oplus": "⊕", "otimes": "⊗", "perp": "⊥", "parallel": "∥",
	"angle": "∠", "degree": "°", "circ": "∘", "bullet": "•",

	// Arrows.
	"to": "→", "rightarrow": "→", "leftarrow": "←", "leftrightarrow": "↔",
	"Rightarrow": "⇒", "Leftarrow": "⇐", "Leftrightarrow": "⇔",
	"mapsto": "↦", "uparrow": "↑", "downarrow": "↓",

	// Big operators.
	"sum": "∑", "prod": "∏", "coprod": "∐", "int": "∫", "iint": "∬",
	"oint": "∮", "bigcup": "⋃", "bigcap": "⋂",

	// Misc.
	"ldots": "…", "cdots": "⋯", "vdots": "⋮", "dots": "…",
	"prime": "′", "hbar": "ℏ", "ell": "ℓ", "Re": "ℜ", "Im": "ℑ",
	"aleph": "ℵ", "star": "⋆", "dagger": "†", "checkmark": "✓",
}

// supMap and subMap cover the characters with Unicode super/subscript
// forms; anything else keeps the readable caret/underscore form.
var supMap = map[rune]rune{
	'0': '⁰', '1': '¹', '2': '²', '3': '³', '4': '⁴', '5': '⁵',
	'6': '⁶', '7': '⁷', '8': '⁸', '9': '⁹', '+': '⁺', '-': '⁻',
	'=': '⁼', '(': '⁽', ')': '⁾', 'n': 'ⁿ', 'i': 'ⁱ',
}

var subMap = map[rune]rune{
	'0': '₀', '1': '₁', '2': '₂', '3': '₃', '4': '₄', '5': '₅',
	'6': '₆', '7': '₇', '8': '₈', '9': '₉', '+': '₊', '-': '₋',
	'=': '₌', '(': '₍', ')': '₎', 'a': 'ₐ', 'e': 'ₑ', 'o': 'ₒ',
	'x': 'ₓ', 'h': 'ₕ', 'i': 'ᵢ', 'j': 'ⱼ', 'k': 'ₖ', 'l': 'ₗ',
	'm': 'ₘ', 'n': 'ₙ', 'p': 'ₚ', 'r': 'ᵣ', 's': 'ₛ', 't': 'ₜ',
	'u': 'ᵤ', 'v': 'ᵥ',
}

// convertMath converts LaTeX math regions to Unicode across a whole
// message. Fenced code blocks are skipped line by line; within a
// line, the delimiters convert in priority order and anything
// ambiguous — unclosed pairs, content that does not look like math —
// stays byte for byte.
func convertMath(src string) string {
	var out strings.Builder
	fence := false
	for _, line := range strings.SplitAfter(src, "\n") {
		body := strings.TrimSuffix(line, "\n")
		if strings.HasPrefix(strings.TrimSpace(body), "```") {
			fence = !fence
			out.WriteString(line)
			continue
		}
		if fence {
			out.WriteString(line)
			continue
		}
		out.WriteString(convertMathLine(body))
		if strings.HasSuffix(line, "\n") {
			out.WriteByte('\n')
		}
	}
	return out.String()
}

// looksLikeMath gates single-dollar conversion: the content must
// carry a math signal and must not be prose or code. Currency
// ("5 and 10") and shell variables ("HOME") carry none and stay
// untouched.
func looksLikeMath(content string) bool {
	if content == "" || len(content) > 100 {
		return false
	}
	if strings.Contains(content, "`") ||
		strings.HasPrefix(content, " ") || strings.HasSuffix(content, " ") {
		return false
	}
	return strings.ContainsAny(content, `\^_`)
}

// convertMathLine converts the math regions of one line, leaving
// everything else byte for byte.
func convertMathLine(line string) string {
	var b strings.Builder
	i := 0
	for i < len(line) {
		rest := line[i:]
		switch {
		case strings.HasPrefix(rest, `\(`):
			if end := strings.Index(rest[2:], `\)`); end >= 0 {
				b.WriteString(mathToText(rest[2 : 2+end]))
				i += 2 + end + 2
				continue
			}
		case strings.HasPrefix(rest, `\[`):
			if end := strings.Index(rest[2:], `\]`); end >= 0 {
				b.WriteString(mathToText(rest[2 : 2+end]))
				i += 2 + end + 2
				continue
			}
		case strings.HasPrefix(rest, "$$"):
			if end := strings.Index(rest[2:], "$$"); end >= 0 {
				b.WriteString(mathToText(rest[2 : 2+end]))
				i += 2 + end + 2
				continue
			}
		case rest[0] == '$':
			if end := strings.IndexByte(rest[1:], '$'); end >= 0 &&
				looksLikeMath(rest[1:1+end]) {
				b.WriteString(mathToText(rest[1 : 1+end]))
				i += 1 + end + 1
				continue
			}
		case rest[0] == '`':
			// Inline code is code: its dollars and backslashes are
			// literal, not math.
			if end := strings.IndexByte(rest[1:], '`'); end >= 0 {
				b.WriteString(rest[:1+end+1])
				i += 1 + end + 1
				continue
			}
		}
		b.WriteByte(line[i])
		i++
	}
	return b.String()
}

// mathToText converts one math region's body. Unknown commands lose
// the backslash and stay readable; group braces drop; super- and
// subscripts map to Unicode when every character is representable.
func mathToText(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '\\':
			i = latexCommand(s, i, &b)
		case '^', '_':
			arg, next := readGroup(s, i+1)
			// Commands inside a script convert first, so
			// x^{\alpha} maps instead of hitting the fallback.
			b.WriteString(superSub(mathToText(arg), c == '^'))
			i = next - 1
		case '{', '}':
			// Leftover group braces: structure, not content.
		case '~':
			b.WriteByte(' ')
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// latexCommand handles the backslash command starting at s[i] and
// returns the index of its last consumed byte.
func latexCommand(s string, i int, b *strings.Builder) int {
	j := i + 1
	for j < len(s) && isLatexWord(s[j]) {
		j++
	}
	name := s[i+1 : j]
	if name == "" {
		// Punctuation-ish commands: spacing (\, \; \: \!) and the
		// literal braces (\{ \}).
		if j < len(s) {
			switch s[j] {
			case ',', ';', ':', '!', ' ':
				b.WriteByte(' ')
			case '{', '}':
				b.WriteByte(s[j])
			case '\\':
				b.WriteString("\n") // line break inside math
			default:
				b.WriteByte(s[j])
			}
			return j
		}
		return i
	}
	switch name {
	case "frac", "dfrac", "tfrac":
		num, after := readGroup(s, j)
		den, after2 := readGroup(s, after)
		b.WriteString(frac(mathToText(num), mathToText(den)))
		return after2 - 1
	case "sqrt":
		arg, after := readGroup(s, j)
		b.WriteString(sqrt(mathToText(arg)))
		return after - 1
	case "text", "mathrm", "mathit", "mathbf", "mathsf", "mathtt",
		"operatorname", "textbf", "textit", "mbox":
		arg, after := readGroup(s, j)
		b.WriteString(strings.TrimSpace(arg))
		return after - 1
	case "mathbb":
		arg, after := readGroup(s, j)
		b.WriteString(blackboard(arg))
		return after - 1
	case "left", "right", "displaystyle", "limits", "nolimits",
		"big", "Big", "bigg", "Bigg", "bigl", "bigr":
		return j - 1
	}
	if sym, ok := latexSymbols[name]; ok {
		b.WriteString(sym)
		return j - 1
	}
	// Known functions read as themselves; unknown commands degrade
	// to their bare name rather than vanishing.
	b.WriteString(name)
	return j - 1
}

// readGroup reads a `{...}` group (nested braces honored) or a bare
// single character at s[i], returning its content and the index
// after it.
func readGroup(s string, i int) (string, int) {
	if i >= len(s) {
		return "", i
	}
	if s[i] != '{' {
		if s[i] == '\\' {
			// A bare command argument, e.g. x^\alpha.
			j := i + 1
			for j < len(s) && isLatexWord(s[j]) {
				j++
			}
			return s[i:j], j
		}
		return s[i : i+1], i + 1
	}
	depth := 0
	for j := i; j < len(s); j++ {
		switch s[j] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[i+1 : j], j + 1
			}
		}
	}
	return s[i+1:], len(s)
}

// superSub converts an exponent or index to Unicode when every
// character has a form; otherwise it keeps the readable caret or
// underscore notation, because a silent loss would be worse.
func superSub(arg string, sup bool) string {
	m := supMap
	mark := "^"
	if !sup {
		m = subMap
		mark = "_"
	}
	var b strings.Builder
	for _, r := range arg {
		if f, ok := m[r]; ok {
			b.WriteRune(f)
			continue
		}
		return mark + "(" + arg + ")"
	}
	if b.Len() == 0 {
		return ""
	}
	return b.String()
}

// frac renders a fraction inline: 1/2 stays compact, compound sides
// get parentheses so precedence survives the flattening.
func frac(num, den string) string {
	// Parens carry precedence, so only a genuinely compound side
	// gets them: "aᵢ" is one symbol, "a+b" is not. Length alone
	// would parenthesize every subscripted variable.
	if mathCompound(num) {
		num = "(" + num + ")"
	}
	if mathCompound(den) {
		den = "(" + den + ")"
	}
	return num + "/" + den
}

// mathCompound reports whether a fraction side needs parentheses:
// it contains an operator, or it is long enough that flattening
// would read as a product.
func mathCompound(s string) bool {
	return strings.ContainsAny(s, "+-*/ ") || utf8.RuneCountInString(s) > 3
}

// sqrt renders a root: a single character hugs the radical,
// compound content gets parentheses.
func sqrt(arg string) string {
	if utf8.RuneCountInString(arg) == 1 {
		return "√" + arg
	}
	return "√(" + arg + ")"
}

// blackboard maps the common blackboard letters; the rest pass
// through unchanged.
func blackboard(s string) string {
	r := strings.NewReplacer("R", "ℝ", "Z", "ℤ", "N", "ℕ", "Q", "ℚ",
		"C", "ℂ", "H", "ℍ", "P", "ℙ", "1", "𝟙")
	return r.Replace(s)
}

func isLatexWord(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}
