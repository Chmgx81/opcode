# Phase 41 — compaction: baseline accounting + the injection rule

## Goal

The adoption doc's #3, two model-behavior subtleties in the
compactor:

1. **Window-baseline accounting.** Compaction triggers on
   ABSOLUTE prompt size today: a session resumed at 80% of the
   window compacts on its very next round, rewriting a
   conversation the user did not add anything to. What should
   count is GROWTH — only tokens added since the window's
   prefill baseline (the first report of the session, or of the
   stretch since the last compaction) re-trigger.
2. **The injection rule.** The recap's position must depend on
   when compaction happens. Pre-turn (round 0, history ending in
   the user's new message): the recap opens the history and the
   user message stays last, where models are trained to find it.
   Mid-turn (after tool rounds, history ending in tool results):
   the recap is the MOST RECENT history item — the end of the
   window is where attention lives, and the model continues from
   fresh context. Today the recap always goes first, which puts a
   mid-turn recap at position 0, the stalest possible position.

## Non-Goals

- No change to the trigger fraction (75% of the relevant space),
  keepRecent (4), the summarizer prompt, or the opt-in
  ContextWindow posture.
- No compaction of the system prompt or skills index — the
  baseline absorbs them, which is the point.
- No new config keys.

## Approach

- `baselineTokens`/`baselineSet` on the orchestrator: the first
  reported prompt size after session start (or after a
  compaction) is recorded as the prefill baseline. The trigger
  becomes: growth = last − baseline must reach
  `CompactionFraction × (window − baseline)` — growth measured
  against the REMAINING window, so a big prefill shrinks the
  space growth is measured against but never triggers on its
  own. A baseline at or beyond the window means any growth
  triggers (the window is already over-full).
- After a compaction the baseline resets with the token signal;
  the compacted size becomes the new baseline on the next
  report — the same conversation cannot instantly re-compact.
- `InjectionPos` (two-value enum, the wrong choice
  unrepresentable): `InjectRecapFirst` for pre-turn,
  `InjectRecapLast` for mid-turn. `runTurn` passes the position
  by round; `maybeCompact` places accordingly.

## Edge Cases

- A provider that never reports usage: no baseline, no trigger —
  unchanged.
- Baseline already ≥ window (resume overflow): remaining is
  clamped to 0, so any positive growth triggers.
- The mid-turn recap is a user-role message after tool results —
  a legal continuation shape on both wire formats.
- Growth within a single turn is small; the accounting
  accumulates across turns, which is when compaction is meant to
  act.

## Test Plan

- The three existing trigger tests move to the two-report growth
  pattern (a first report anchors the baseline; a second report
  past the remaining-space fraction triggers) — their single
  800-of-1000 report was the exact pathology this phase fixes.
- Unit: a resumed/prefilled window at 80% does NOT compact on its
  first rounds; growth past 75% of the remaining space does.
- Unit: post-compaction re-baseline — the compacted size anchors
  again; no instant re-compaction.
- Unit: mid-turn compaction puts the recap LAST; pre-turn
  compaction keeps it FIRST; the disabled and failure paths are
  unchanged.
- Full suite green across all packages.
