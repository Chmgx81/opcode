# Phase 13 — Plan Mode

## Goal

A Claude Code-style plan mode: the agent researches without touching
anything, presents a structured plan for approval, and only on
approval gains the ability to act — without a restart or a new session.

## Non-Goals

- Plan persistence to a plan.md file (the plan lives in the
  conversation and transcript).
- Per-step approval inside a plan; the existing per-action permission
  prompts cover that.
- A separate plan history / branching view.

## Approach

**Mode.** `plan` joins the cycle as a fourth mode:
read-only → plan → ask-every-time → full-auto (tab cycles). Like
read-only, action-tier tools are not even advertised to the model —
plus draft-tier tools ARE advertised, which is the whole point.

**The tool.** `present_plan` is the mechanical exit from planning:
Tier Draft-Only (always allowed — proposing changes nothing), args
`{"plan": "<markdown>"}`. Its Execute calls the injected Approve
callback, which blocks (same pattern as the permission gate) until
the user answers. The result text tells the model what happened:
approved + auto, approved (actions will prompt), or declined.

**Approval flow (TUI).** A plan prompt like the permission prompt:
`y` proceed — switch to ask-every-time unless already in a working
mode; `a` proceed with actions auto-accepted (full-auto); `n`/Esc keep
planning. Mode switching mid-turn is already supported (next model
request re-composes the tool list), so approval immediately widens
what the model can do — no restart.

**System prompt.** Plan mode instructs: research first, no changes,
then present exactly one plan via present_plan (goal, steps, risks)
and stop.

## Edge Cases

- Model calls present_plan outside plan mode: the prompt still shows;
  `y` keeps the current mode (no forced switch from a working mode).
- Approve callback nil (headless): fail closed — the tool returns
  "user cannot be reached", the model gets nothing actionable.
- Approval while a turn is running: allowed by design; the gate is
  rebuilt, the in-flight round finishes with its already-granted tools.
- Plan text missing/empty: tool error, surfaced to the model.

## Test Plan

- Unit: tool tier/args/verdict texts/fail-closed; policy matrix rows
  for plan mode; orchestrator advertisement (no write_file, yes
  present_plan, instruction composed); config accepts "plan"; TUI
  plan-prompt lifecycle (y/a/n/Esc, mode follow-through, transcript).
- PTY against the scripted server: plan-mode request → model calls
  present_plan → prompt answered with y → the NEXT request provably
  advertises write_file (fixture answers based on the offered tool
  set) → model streams the implementation. End to end.
