# Phase 39 — the reasoning-effort knob

## Goal

The adoption doc's #5. Reasoning is opcode's most expensive dial: the
same model with a big thinking budget can spend many times more
tokens on the same question. Codex exposes that dial per turn
(`model_reasoning_effort`, cycled with Alt+,/Alt+.). opcode gains the
same: a three-step effort (low / medium / high, plus unset = the
provider's default), a config key, and the footer segment showing
what is active.

## Non-Goals

- No per-turn persistence beyond the session: the knob is live state
  (like the permission mode), not written back to config.
- No effort presets per model in models.json (Codex catalogs them
  per model); opcode's three budgets are one honest mapping until a
  real need appears.
- No UI for Anthropic's exact token budgets: the names cycle; the
  budget mapping is opcode's choice.

## Approach

- `llm.ChatRequest.ReasoningEffort` ("" unset, or low/medium/high).
  OpenAI-compatible servers get `reasoning_effort` (omitempty);
  Anthropic gets `thinking: {type: enabled, budget_tokens: N}` with
  N = 1024 / 8192 / 16384 (Anthropic's documented minimum, a
  moderate default, a generous cap) — omitted when unset, so a
  model without extended thinking never sees the field.
- The orchestrator carries `ReasoningEffort` like it carries `Mode`
  (per-request, `SetEffort` switches it; mid-turn changes apply on
  the next round).
- Config: `reasoning_effort` in config.json, validated at load
  (unknown values fail loudly, like permission_mode), wired as the
  session's initial effort.
- TUI: **Alt+.** cycles unset → low → medium → high → unset; **Alt+,**
  cycles the other way. A toast names the new effort; the mode line
  gains the spec's `◐ low` segment (GlyphDoing, the effort glyph)
  whenever an effort is set, so the dial is visible, not silent.
  The next request carries the new value — same contract as tab's
  mode cycling.

## Edge Cases

- A model without a thinking feature: the OpenAI field is advisory
  (servers ignore unknown fields per the spec) and the Anthropic
  field is omitted unless set; unset is the default posture, so
  nothing changes for users who never touch the dial.
- Mid-turn cycling: takes effect on the next model request, like
  every per-request switch; the in-flight round is done.
- Subagents inherit the parent's effort (they share the
  orchestrator's posture); deliberately no independent knob.
- config validation and the TUI cycle agree on the name set; the
  error message names all three.

## Test Plan

- Unit (llm): the OpenAI wire carries `reasoning_effort` only when
  set; the Anthropic wire carries `thinking` with the right budget
  per name and omits it when unset.
- Unit (config): valid keys load, unknown values fail loudly.
- Unit (orchestrator): the request carries the effort, and SetEffort
  changes the next request's value.
- Unit (tui): alt+./alt+, cycle through the postures in both
  directions with a toast; the footer shows the ◐ segment when set
  and nothing when unset.
- PTY live: cycle the knob, confirm the footer segment and toast on
  screen, and confirm the next request's JSON body carries the
  chosen `reasoning_effort` (the scripted server echoes the request).
- Full suite green across all packages.
