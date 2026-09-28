# Phase 2 Spec — Permission Modes

## Goal

Implement Section 7's full permission-mode system: read-only,
ask-every-time, auto-accept-safe-ops, and full-auto — each defining the
default posture across the three tiers, switchable at runtime, with the
mode shaping which tools are even offered to the model and what the
system prompt says.

## Non-Goals

- Per-command shell classification (`ls` vs `rm`): run_shell stays a
  single Action-Allowed tool; the spec's tier model deliberately avoids
  content inspection.
- Sandbox/OS-level enforcement: modes are enforced at the gate, which is
  honest about what it is — a policy layer, not a sandbox.
- Draft-Only *tools*: no built-in produces proposals yet. The tier
  exists in the matrix because the mode semantics need it (skills,
  Phase 3, will assign it); no tool uses it in this phase and that is
  stated, not hidden.

## Approach

- Decision matrix (gate checks tier per action; mode sets the posture):

  | Tier \ Mode | read-only | ask-every-time | auto-accept-safe-ops | full-auto |
  |---|---|---|---|---|
  | Read-Only | allow | allow | allow | allow |
  | Draft-Only | allow | allow | allow | allow |
  | Action-Allowed | **deny, no prompt** | prompt | prompt | **allow, logged** |

  Read-only mode denies action-tier calls outright rather than
  prompting: "read-only mode can't touch your filesystem" must be a
  guarantee, not a mood. Unknown modes fail closed (behave as
  ask-every-time for prompting and exclude nothing else).

- Tool advertisement: in read-only mode the model is not even offered
  Action-Allowed tools (Section 3.2: mode determines the allowed tool
  set). Denial is the backstop for a model that hallucinates a call.
- Mode instructions are composed into the system prompt (Section 3.2),
  so the model knows the posture it is running under.
- `/mode` in the TUI: no argument prints current mode and options; with
  a valid name it switches the orchestrator's mode, rebuilds the gate
  policy, and notes the change in the transcript. Invalid names are
  rejected with the valid list.
- config: `permission_mode` accepts the four mode names; the old "ask"
  value (Phase 1 default) is normalized to ask-every-time so existing
  configs keep working.

## Edge Cases

- Switching modes mid-turn: the next round's request picks up the new
  tool set and prompt; the in-flight round's calls already dispatched
  are done.
- "a" (allow-all-this-session) from a prompt survives a mode switch —
  it is a session-level grant, orthogonal to mode. Switching INTO
  read-only mode is the exception a user may expect to clamp things
  down: mode switch resets the session grant so read-only stays honest.
- A mode switch to full-auto must still log every call — the gate's
  audit entry is unconditional and already is.

## Test Plan

- Unit: the full 4x3 decision matrix; nil-prompt fail-closed; unknown
  mode; tool advertisement per mode (asserted on the request the fake
  provider receives); config normalization; /mode command paths
  (switch, invalid, show).
- Live: PTY session per mode against the local scripted server —
  read-only denies a write without prompting; full-auto writes with no
  prompt; ask prompts. Then one live OpenRouter run in full-auto doing
  a real edit with no prompt.
