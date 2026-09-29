# Phase 40 — syntax-highlight guardrails

## Goal

The adoption doc's small-gems list: Codex rejects pathological
highlight inputs (> 512 KB / 10k lines / 4 KiB lines) and falls
back to plain text so display never stalls (tui-audit §2.7).
tilde's `highlightLine` feeds whatever the model wrote to chroma
with no bound — and the lexer is chosen from a model-provided
file path, so a hostile or accidental monster line is an untrusted
input to a parser. Rendering must never be hostage to a highlight
input.

## Non-Goals

- No change to the lexer choice (chroma's `Match` is a registry
  lookup, already cheap) or the lexer cache.
- No cap on glamour's own code-block highlighting — that is
  glamour's seam; tilde's seam is the line-level highlighter.
- The other small gems, with their dispositions:
  - **Sanitized terminal title** — already adopted (Phase 24-era
    `sanitizeTitle`: controls and bidi stripped, 240-rune cap).
  - **Turn diff budget** — no-op for tilde: edit results render
    the tool's before/after strings directly, no diff computation
    to cap.
  - **One-shot screen-reader probe with a persisted marker** —
    skipped: tilde's detection is environment-variable reads with
    no terminal query; there is nothing expensive to remember.
  - **Session-log recording behind an env var** — deferred: a
    development harness, not a product surface; revisit when a
    rendering bug actually needs it.
  - **Session ids on events** — deferred: a field nothing
    consumes yet is padding; it becomes worth adding the day an
    async UI bug needs one.

## Approach

One check in `highlightLine`: a line longer than 4 KiB renders as
plain text, before chroma ever sees it. The cap sits at the
function every highlight path already funnels through (write_file
results, edit_file hunks, the diff body's source lines), so one
line of insurance covers every caller.

## Edge Cases

- Exactly 4 KiB highlights; one byte more does not. The boundary
  is arbitrary and documented — the point is the existence of a
  bound, not its exact value.
- The fallback returns the line unchanged, not truncated —
  truncation would be a second, silent failure mode.
- Plain posture: the cap is size, not glyphs; it applies identically
  whatever the theme or color profile.

## Test Plan

- Unit: a line under the cap renders with token styling; a line
  over it renders byte-identical to its input with no SGR codes;
  the boundary (exactly at the cap) highlights.
- Full suite green across all packages.
