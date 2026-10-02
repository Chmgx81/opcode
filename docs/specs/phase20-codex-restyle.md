# Phase 20 — Codex-aligned restyle

## Goal

Restyle the TUI to closely match Codex's design language, studied
from its source (`codex-rs/tui`): a ChatGPT-blue accent, neutral
measured grays for secondary text and borders, background-shaded user
message blocks, muted-amber warnings, and Codex's status/hint
phrasing. opcode keeps its own glyphs and personality where Codex has
no equivalent (the `~` brand, the gerund pool).

## Non-Goals

- Cloning Codex pixel-for-pixel — ratatui and lipgloss render
  differently; this is a design-language adoption, not a port.
- Copying Codex keybindings (opcode's stay: tab cycles, ctrl+r expands).
- New features — colors, glyphs, and wording only. No behavior change.

## Approach — what Codex does (from source)

- Accent: `UI_ACCENT = #63A8F8` dark / `#1C64C8` light, bold —
  keys, emphasis, selection (style.rs).
- User prompts: a background-shaded block (white blended 16% over the
  terminal bg; 4% black on light) with a `› ` bold-dim prefix — not
  colored text (style.rs `history_prompt_style`, messages.rs).
- Secondary text: a measured 60% foreground blend, never ANSI dim.
- Warnings: muted amber `#C4A767` dark / `#8B6214` light.
- Status: terminal palette colors (green/red/yellow), bold.
- Working line: `Working (0s • esc to interrupt)` — verb, then a
  parenthesized elapsed/interrupt segment (status_indicator_widget.rs).
- Hints: accent-colored key glyph + plain label (`? for shortcuts`),
  secondary-text style (footer.rs).

## The mapping

| Token | Old (ice cyan) | New (Codex-aligned) |
|---|---|---|
| Accent | `#22D3EE` | `#63A8F8` |
| Accent2 | `#0891B2` | `#3E82D6` |
| Border (Deep) | `#0B3A47` | `#404040` neutral gray |
| Fill (Deep2) | `#06222B` | `#292929` (16% white blend) |
| Text | `#E6F2F5` cool | `#E8E8E8` neutral |
| Dim | `#7A8B94` cool | `#999999` (60% blend) |
| Warning | `#FBBF24` | `#C4A767` muted amber |
| Success | (accent) | `#3FB950` green token (new) |
| Info | `#93C5FD` | `#8FBFE8` soft blue |

Light theme mirrors Codex's light values: `#1C64C8` accent,
`#F2F2F2` fill, `#8B6214` amber, dark neutral inks.

Structural: user entries get the `› ` bold-dim prefix inside the
shaded panel (GlyphUser, new); prompt titles go bold-neutral instead
of amber (amber stays for the attention box border and warnings);
the working line becomes `Verb (Ns • esc to interrupt · ↓ Nk tokens)`;
the footer hint becomes `? for shortcuts · / commands` with the
key glyphs in accent.

## Edge cases

- Markdown, diffs, and the syntax highlighter all derive from the
  same Hex tokens, so one palette swap restyles everything — the
  glamour renderer cache is already dropped on theme adaptation.
- Amber box = attention (shell mode, prompts) reads correctly against
  the neutral palette; verified by contrast on both themes.

## Test plan

- Existing render tests updated to the new SGR codes (they pin exact
  colors — a restyle must move them, not break them).
- Theme-adaptation test updated for the new light values.
- PTY: dark and `OPCODE_THEME=light` frames checked by eye for the
  greeting, a user message, the working line, and the composer.
