# Phase 18 — Todos

## Goal

The model tracks its own multi-step work in a structured task list,
and the user sees it live in the TUI — visible progress instead of
implied progress, the highest-value every-session feature left.

## Non-Goals

- Persistent per-project task files (the list lives in the session).
- User-editable todos (this is the model's list; the user steers it
  through the conversation).
- Fancy dependency graphs — a flat ordered list, like every reference
  product.

## Approach

**The tool.** `todo_write`, Draft-Only tier (updating a plan changes
nothing — same rationale as present_plan). Full-replacement semantics:
the model sends the complete list every update (Claude Code's
TodoWrite contract) — no merges, no IDs, no conflict resolution.
Item: `{"content", "status"}` with status pending | in_progress | done.

**The state.** A shared `tools.TodoList` (mutex'd) owned by main, with
a settable OnChange callback that receives a copy. The TUI sets the
callback to a tea.Msg sender; headless leaves it nil (state updates
silently). The registry tool mutates the shared list and notifies.

**The render.** The current list renders as a live panel at the tail
of the transcript whenever it is non-empty (not history — state, like
the mode line): done ✓, in_progress ▸, pending ·, a count header, and
a row window so a 40-item list cannot eat the screen.

**Prompt guidance.** The tool's Description carries the contract
(use for 3+ step work, exactly one in_progress, send the full list) —
no system-prompt bloat.

## Edge Cases

- Unknown status or empty content: tool error, surfaced to the model.
- More than 50 items: rejected — a list that size is a planning
  failure, not a plan.
- Mid-turn updates: the panel re-renders from the msg (bubbletea only
  paints on messages, so the OnChange must Send).
- All done: the panel stays, showing the finished work.

## Test Plan

- Unit: parse/validate/replace/notify (with a copy, not the shared
  slice); TUI panel rendering (glyphs, counts, windowing); draft-tier
  advertisement.
- PTY: a fixture calls todo_write twice (two pending, then one done);
  the panel must be visible mid-turn with the right glyphs and the
  final state after the turn.
