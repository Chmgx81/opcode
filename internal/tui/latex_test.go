package tui

import (
	"strings"
	"testing"
)

// TestMathToText is table-driven over the constructs: symbols,
// fractions, roots, scripts, commands, and the degradations.
func TestMathToText(t *testing.T) {
	cases := []struct{ in, want string }{
		{`\alpha + \beta = \gamma`, `α + β = γ`},
		{`\Gamma \times \Omega`, `Γ × Ω`},
		{`x \leq y \neq z`, `x ≤ y ≠ z`},
		{`a \to b \Rightarrow c`, `a → b ⇒ c`},
		{`\frac{1}{2}`, `1/2`},
		{`\frac{a+b}{c-d}`, `(a+b)/(c-d)`},
		{`\frac{\alpha}{2}`, `α/2`},
		{`\frac{a_i}{b_i}`, `aᵢ/bᵢ`}, // a subscripted variable is one symbol, not compound
		{`\sqrt{2}`, `√2`},
		{`\sqrt{x+1}`, `√(x+1)`},
		{`x^2`, `x²`},
		{`x^{10}`, `x¹⁰`},
		{`e^{-i\pi}`, `e^(-iπ)`}, // π has no superscript: readable fallback
		{`x_i`, `xᵢ`},
		{`a_{n+1}`, `aₙ₊₁`},
		{`x^{2y}`, `x^(2y)`}, // no Unicode form: readable fallback
		{`x_{ab}`, `x_(ab)`}, // ditto
		{`\sum_{i=1}^{n} i`, `∑ᵢ₌₁ⁿ i`},
		{`\int_0^1 x\,dx`, `∫₀¹ x dx`},
		{`\lim_{x \to \infty}`, `lim_(x → ∞)`}, // spaces force the readable fallback
		{`\sin^2(x)`, `sin²(x)`},
		{`\log_2 n`, `log₂ n`},
		{`\mathbb{R}`, `ℝ`},
		{`\text{speed} = v`, `speed = v`},
		{`\left(x+y\right)`, `(x+y)`},
		{`E = mc^2`, `E = mc²`},
		{`\unknown x`, `unknown x`}, // unknown command: bare name, still readable
		{`\foo{bar}`, `foobar`},     // its group braces drop
		{`a ~ b`, `a   b`},          // ~ becomes a space (two literal spaces around it)
		{`\{\alpha\}`, `{α}`},       // literal braces
	}
	for _, c := range cases {
		if got := mathToText(c.in); got != c.want {
			t.Errorf("mathToText(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestConvertMathRegions: the delimiters and their gates.
func TestConvertMathRegions(t *testing.T) {
	cases := []struct{ in, want string }{
		// Unambiguous delimiters always convert.
		{`\(x^2\)`, `x²`},
		{`\[a_1\]`, `a₁`},
		{`$$\alpha\beta$$`, `αβ`},
		// Single dollars convert only with a math signal.
		{`E is $E = mc^2$ today`, `E is E = mc² today`},
		{`$x_1$ and $x_2$`, `x₁ and x₂`},
		// Currency, shell variables, prose: untouched.
		{`costs $5 and $10 total`, `costs $5 and $10 total`},
		{`run $HOME and $PATH first`, `run $HOME and $PATH first`},
		{`$5`, `$5`},
		// Unclosed delimiters stay raw.
		{`\(x`, `\(x`},
		{`$x^2`, `$x^2`},
		// Backtick content is code, not math.
		{"use `$x^2$` here", "use `$x^2$` here"},
		// Code fences are skipped entirely.
		{"```\n$x^2$ and \\alpha\n```\n$y^2$", "```\n$x^2$ and \\alpha\n```\ny²"},
		// Math beside text, multiple regions on one line.
		{`if \(a^2\) then \(b_3\) end`, `if a² then b₃ end`},
	}
	for _, c := range cases {
		if got := convertMath(c.in); got != c.want {
			t.Errorf("convertMath(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestConvertMathLeavesPlainText: a message with no math at all is
// byte-identical — the converter must be invisible when unused.
func TestConvertMathLeavesPlainText(t *testing.T) {
	src := "# Title\n\nSome prose with *emphasis* and `code`.\n" +
		"Costs $5. Shell var $HOME. 100% done.\n"
	if got := convertMath(src); got != src {
		t.Errorf("plain text changed:\n got %q\nwant %q", got, src)
	}
}

// TestRenderMarkdownConvertsMath: the pipeline choke point — a
// markdown message carrying math renders the converted glyphs.
func TestRenderMarkdownConvertsMath(t *testing.T) {
	out := renderMarkdown("The answer: $\\alpha + \\beta^2$ — done.\n", 80)
	joined := strings.Join(out, "\n")
	if !strings.Contains(joined, "α + β²") {
		t.Errorf("rendered markdown missing converted math:\n%s", joined)
	}
	if strings.Contains(joined, `\alpha`) {
		t.Errorf("raw LaTeX leaked into the render:\n%s", joined)
	}
}
