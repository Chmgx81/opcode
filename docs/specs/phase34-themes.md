# Phase 34 — the themes picker

## Goal

The palette is already a set of swappable tokens (`Hex*` +
`refreshTokens`), but only two postures exist and the only switch is
the background probe. `/theme` makes the palette a user choice: a
picker with live preview (moving the highlight re-skins the whole
session in place), cancel-restore (Esc puts the previous theme
back), and persistence to `config.json` so the choice survives
restarts.

## Non-Goals

- No custom user-defined palettes (arbitrary hex in config). The
  theme list is curated; a hand-typed hex that renders illegibly is
  a support ticket, not a feature.
- No per-theme glyph or layout changes — themes are color only.
- NO_COLOR is untouched: it still removes color entirely, whatever
  the theme says.

## Approach

- A theme table in `internal/tui/style.go`: `dark` (the teal
  default), `light` (the existing light-legible set), and `green`
  (the original Phase 7 brand stack — electric green on near-black).
  `applyThemeName(name) bool` swaps the Hex tokens, refreshes every
  style, and drops the glamour renderer cache (renderers embed the
  palette at creation).
- `adaptTheme(dark)` becomes a thin wrapper over the table so the
  background probe keeps working unchanged.
- `Options.Theme` (from `config.json "theme"`) names the startup
  theme and wins over the probe; empty means today's auto behavior
  (probe, with `TILDE_THEME=light|dark` still forcing the posture).
  Unknown names fail loudly at startup in `cmd/tilde` via
  `tui.ValidTheme`, and `applyThemeName` fails closed (probe path)
  if it ever sees one anyway.
- `/theme` opens the shared picker; `/theme <name>` switches
  directly. While the picker is open, moving the highlight applies
  the highlighted theme live; Enter applies and persists via
  `Options.SetTheme` (writes `theme` into `config.json`,
  preserving unknown keys); Esc restores the theme active when the
  picker opened. A transcript note lands either way.
- Applying a theme clears the per-entry markdown render caches and
  the in-flight stream cache — they embed the old palette's ANSI
  codes. Committed native scrollback cannot be re-rendered; those
  lines keep their original colors (stated residual, same as every
  other post-hoc re-skin).

## Edge Cases

- Picker closed by Enter with zero matches: nothing selected — the
  preview must still restore, not strand the previewed palette.
- No `SetTheme` wired (tests, future headless): the switch applies
  for the session and the note says so honestly ("this session
  only").
- `config.json` absent when saving: `SaveTheme` creates it with
  just `theme`; existing unknown keys are preserved by editing the
  raw JSON map, not rewriting from the struct.
- A theme applied mid-turn: the in-flight stream re-renders on the
  next frame from the cleared cache; the model's context is
  untouched (display-only, like every palette decision).

## Test Plan

- Unit (config): SaveTheme creates the file, preserves an unknown
  sibling key, and overwrites a previous theme.
- Unit (tui): applyThemeName swaps the Hex tokens; unknown names
  return false and change nothing; the entry render caches clear.
- Unit (tui): the picker — opening sets the backup, moving applies
  the highlighted theme live, Esc restores it, Enter applies and
  calls SetTheme with the chosen name, Enter with no matches
  restores instead of stranding the preview.
- Unit (tui): `/theme green` switches directly and persists.
- PTY live: /theme → arrows preview the themes on screen → Esc
  restores; then /theme green → note + config.json carries
  `"theme": "green"`, and a second run starts green.
- Full suite green across all packages.
