# Phase 45 — UI restyle: one box language, a tighter greeting

> **Superseded in part (Phase 47, live findings):** the identity line
> described below included the mode; the greeting is a frozen launch
> snapshot and the mode is a live dial, so the mode left the line —
> see the log entry "the mode leaves the greeting's frozen snapshot".
> Everything else here holds.

## Goal

The audit asked "what would a redesign actually improve?" and the
answer was two things, both about chrome weight and coherence:

1. **The composer is the only interactive surface that is not a
   box.** Every dialog, picker, palette, and the help sheet draws a
   rounded box (`dialogBorder`). The composer — the surface used
   every second of every session — instead sits between two
   full-width `─` rules. Those two lines are the boldest horizontal
   structure on the screen, stronger than anything the content
   draws, and they buy no side edges: the input's left and right
   bounds are still undefined. The redesign puts the composer in the
   same rounded box, so the whole UI speaks one box language for
   "a thing you act on", and the input gains real edges for the
   same two rows of chrome it spends today.
2. **The greeting spends its identity on three lines.** The banner
   lockup reads `◈ opcode v0.5.0`, then model · mode, then the cwd —
   three rows beside the wave. Model, mode, and directory are one
   statement ("this, running here"); joined with `·` they fit one
   row on every realistic terminal and wrap when they do not. The
   launch frame gets a row back and the lockup reads as a sentence
   instead of a form.

## Non-Goals

- No palette changes. The accent/surfaces are WCAG-table-locked in
  `theme_test.go` and are not what the audit found weak.
- No changes to the timeline, user block, tool lines, todos, diffs,
  permission dialog, pickers, or the help sheet. The audit found
  these already at the bar; redesigning them would be churn, and
  churn on tested surfaces is risk without payoff.
- No new themes. Curated, not user-extensible, by design.
- The banner wave stays: it is the mark from the v0.5.0 rebrand.

## Approach

### 1. The composer box

`composerView` replaces its two `ruleLine` rows with one
`lipgloss` bordered block, the same `dialogBorder()` every overlay
uses:

- Border `HexDeep` at rest (the boundary token, 3:1), `HexWarning`
  in shell mode — the amber signal the rules already carried.
- Under the plain posture the box degrades to the `+-|` ASCII box
  for free, through the same `dialogBorder()` switch the dialogs
  use. `GlyphRule` leaves the vocabulary (nothing renders it), so
  `adaptGlyphs` and the a11y glyph table lose the entry.
- Width: content is the terminal less the two border columns and
  the box's 0,1 padding — `SetWidth(w-4)` on the textarea, and the
  box is drawn only at `w >= 14` (border + padding + a 10-column
  input floor). Below that the composer renders bare: at 8–13
  columns chrome is noise, and the `clipCols` backstop stays the
  final defence as always.
- Row budget: the box's two border rows are the same two rows the
  rules spent, so `room := m.height - len(cl) - 2` in `view()` is
  unchanged.
- The login flow's masked composer clips the mask to the box's
  inner width before render, the same budget it had before.

### 2. The greeting lockup

`New()` joins model, mode, and cwd onto one dim line,
`model · mode · cwd`, beside the banner. The first-run "no model
yet" placeholder and the startup notes below are unchanged.

## Edge Cases

- **Narrow terminals (w < 14):** no box, bare composer — never a
  clipped border. `TestFrameFitsEveryStateAtEveryWidth` sweeps
  8–120 and holds.
- **Plain posture / screen reader:** ASCII box via `dialogBorder`;
  `TestFrameIsAsciiUnderPlain` and the glyph table must both be
  updated in the same change (`GlyphRule` is gone).
- **Shell mode:** border turns amber with the `!` prompt — the
  state signal survives the restyle.
- **Multi-line drafts:** the textarea already grows
  (`resizeComposer`); the box grows with it, and `fit` still
  protects the composer tail as it does today.
- **Login masking:** the mask clips to the box's inner width; the
  secret never echoes.

## Test Plan

- `go test ./internal/tui/` — the composer-related tests
  (`states_test`, `composer_test`, `layout_test`, `narrow_test`,
  `tui_test` frame sweeps) must pass with the box; update any that
  indexed the old rule rows in the same change.
- The a11y glyph table drops the `rule` entry when `GlyphRule` is
  removed.
- `go test -race -count=1 ./...` — the full CI suite.
- `gofmt -l .` prints nothing; `go vet ./...` clean.
