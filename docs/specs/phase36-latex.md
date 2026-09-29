# Phase 36 — LaTeX conversion

## Goal

Models answer math questions in LaTeX (`$\alpha + \beta$`,
`$$\frac{a+b}{c}$$`) because that is what their training data does.
In a terminal that markup is noise: the reader parses `\alpha` by
eye instead of seeing α. Assistant math now converts to Unicode at
the display boundary: greek letters, operators, super- and
subscripts, roots and fractions render as readable text.

## Non-Goals

- Not a typesetter: no layout, no alignment, no equation numbering.
  This is notation conversion — the shape stays linear text.
- Not complete LaTeX. The symbol table covers the common core
  (greek, relations, arrows, big operators, set membership); an
  unknown command degrades to its bare name (`\foo` → `foo`),
  which stays readable instead of vanishing.
- Model context is untouched, as with every display transform: what
  the model wrote is what it sees back; only the rendering converts.
- No conversion inside code — fenced blocks are skipped line by
  line, and `$...$` pairs whose content contains a backtick are left
  alone.

## Approach

`internal/tui/latex.go`, applied at the top of `renderMarkdown` —
the one choke point behind finished entries, the in-flight stream,
and plan bodies:

- Delimiters, in priority order: `\(...\)`, `\[...\]`, `$$...$$`
  always convert; single `$...$` converts only when the content
  looks like math (contains `\`, `^`, or `_`, no leading/trailing
  space, no backtick) — currency ("$5 and $10") and shell
  variables ("$HOME and $PATH") must survive untouched. Unclosed
  delimiters stay raw; the failure direction is fidelity, never
  corruption.
- `mathToText`: `\frac{a}{b}` → `a/b` (parenthesized when either
  side is compound), `\sqrt{x}` → `√x` / `√(x+1)`, `\text{...}` and
  friends → their content, `\mathbb{R}` → `ℝ`, `\left`/`\right`
  dropped, `~` → space, spacing commands → space, superscripts and
  subscripts via `^{...}`/`_{...}`/bare char mapped to Unicode
  super/subscripts when every character is representable (else the
  readable `^(...)` form stays), leftover group braces dropped, and
  the symbol table for everything else. Groups convert recursively.
- Known function names (`\sin`, `\log`, `\lim`, ...) lose the
  backslash and read as themselves.

## Edge Cases

- A `$` pair spanning a line break: conversion is line-scoped, so
  cross-line pairs stay raw — readable, never mangled.
- Partial math mid-stream: the stream re-renders as it grows; a
  half-arrived `$$...` renders raw until its close lands, then
  converts.
- `x^2y` reads as `x^{2}y` in LaTeX — the bare-char rule takes only
  the `2`. An exponent with no Unicode form (`x^{2y}`) keeps the
  readable `^(2y)` fallback; a silent loss would be worse.
- Plain posture and NO_COLOR: Unicode letters are text, not color —
  conversion works identically.

## Test Plan

- Unit, table-driven: every construct above, the anti-cases
  (currency, shell variables, backtick spans, fenced code, unclosed
  delimiters), and the fallbacks (unknown command, unrepresentable
  superscript).
- Unit: renderMarkdown integration — an entry containing math
  renders the converted glyphs.
- PTY live: a fixture model answers with LaTeX-marked math; the
  transcript shows α, √, ∑, superscripts — and raw `$HOME` in the
  same answer stays raw.
- Full suite green across all packages.
