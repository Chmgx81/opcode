# Phase 46 — the palette and the lists

## Goal

Phase 45 restyled the chrome and left two things untouched that the
redesign should have owned: the colors and the list surfaces. This
phase does both.

1. **A new identity palette.** The teal default (`#2dd4bf` accent)
   was Tailwind's teal, not opcode's; nothing in the product said
   "teal". The default is now a violet accent (`#a78bfa` dark /
   `#7c3aed` light) with violet-tinted surfaces and a sky info —
   a palette the mark owns. The semantic tokens (success, danger,
   warning, muted, subtle, border) keep their values: they are
   contrast-locked meaning, and recoloring them is churn.
2. **One selection language for every list.** The pickers, the
   command palette, and the @-mention menu each drew their own rows
   — same caret, same dim, but ragged detail columns and no
   emphasis on the selection. All three now draw through one row
   composer: an accent band fills the selected row (the caret still
   marks it — the band is emphasis, never the only signal), and the
   label column is a fixed gutter so details align into a column.

## Non-Goals

- No user-defined themes. Curated, not user-extensible, by design.
- No change to list mechanics: filtering, arrows, enter/esc, and
  the empty-state copy are Phase 42/43 behavior that already tests
  well.
- The help sheet and dialogs are not lists in this sense; their
  row treatment is unchanged.

## Approach

### Palette

- `dark`: accent `#a78bfa`, info `#7dd3fc`, surfaces tinted violet
  (`#2a2732` user, `#1f1c26` code). `light`: accent `#7c3aed`,
  surfaces `#ede9f5` / `#f6f4fa`.
- New token `HexOnAccent`: the text color on the selection band,
  per theme (`#0d1117` dark, `#ffffff` light). The theme struct
  grows to eleven tokens.
- The old teal default becomes a named theme, `teal`, so nobody
  who chose it loses it — the same courtesy `green` got when it
  was replaced. Picker order: dark, light, teal, green.
- Every candidate value was checked against the contrast bars
  before landing: text tokens 4.5:1 against the floor and the code
  panel, border 3:1, and the new on-accent 4.5:1 against the band.
  `TestThemeContrastIsLegible` enforces all of it permanently.

### Lists

- `menuRow(selected, label, detail, w)` in `view.go` composes one
  item row; `pickerRows`, `paletteRows`, and `atMenuRows` all call
  it. Selected rows render through `selectedStyle` (on-accent fg
  on accent bg, bold) padded to the box's inner width so the band
  spans the row.
- The label column is a fixed 14-column gutter; below the width
  where marker + gutter fit, the row drops the detail rather than
  wrapping — a wrapped row is rows the frame's budget never
  counted, and the first implementation of this phase did exactly
  that and failed the 8-column frame sweep.
- Unselected rows stay dim; the count/empty rows stay dim.

## Edge Cases

- **Narrow terminals:** rows are built to the box's inner width
  before any styling, then clipped — no width from 8 to 120 makes
  a row wider than the box (`TestMenuRowSelectionBand` sweeps).
- **No color:** the caret still marks the selection; the band is
  never the only signal (the a11y posture's own rule).
- **Theme switch:** the band is built by `refreshTokens` like every
  other style, so a live `/theme` preview re-skins open lists.
- **Persisted themes:** `dark`/`light` names are unchanged, so a
  saved choice keeps working and just gets the new values.

## Test Plan

- `TestThemeContrastIsLegible` gains the teal floor and the
  on-accent bar; hardcoded SGR assertions in `tui_test.go` and
  `diff_test.go` move to the new hex values.
- `TestMenuRowSelectionBand` locks the band, the caret, the aligned
  column, and the width sweep.
- `go test -race -count=1 ./...`, `gofmt -l .`, `go vet ./...`.
