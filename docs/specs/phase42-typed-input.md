# Phase 42 — the input decision as one typed place

## Goal

The adoption doc's #2 (adopted now because this is the refactor's
moment: the input model has just grown the effort knob and the
queue, and the paths are spread across submitInput and turnEnded).
Enter steers, Alt+Enter queues, commands dispatch, `!` escapes —
all of it works, but WHAT a submit became is behavior to
reverse-engineer, not a value to read. The decision becomes one
typed function: `decideInput` returns one of six outcomes, so
"why didn't my message send" has an answer in the type.

## Non-Goals

- No behavior change: every path executes exactly as today; this
  is typing the decision, not re-deciding it. The existing
  steer/queue/start tests must pass untouched.
- No optimistic-concurrency token (Codex's
  `expected_previous_turn_id`): tilde's orchestrator is a
  single-channel loop with no concurrent turn writers to race;
  the token solves a problem tilde does not have.
- Command business rules (like /model's mid-turn refusal) stay
  in their commands — they are outcomes of a command, not of the
  input decision.

## Approach

`internal/tui/input.go`:

- `inputDecision`: `inputIgnored` (empty draft), `inputShell`
  (the `!` escape — user-run, no model), `inputCommand` (handled
  by the command system), `inputQueued` (a turn is running; the
  draft submits when it ends), `inputSteered` (a turn is running;
  the draft folds in at the next ROUND boundary), and
  `inputTurnStarted` (idle; a new turn runs).
- `decideInput(alt bool) (inputDecision, tea.Cmd)` is the one
  router — today's submitInput body, branch by branch, each
  branch returning its outcome. `submitInput` becomes a thin
  wrapper so no call site changes.
- The steer branch carries the explicit contract as a comment:
  a steered draft cannot alter the active round's in-flight
  request (it is already on the wire); it applies at the next
  round boundary — matching the orchestrator's `Steer`
  checkpoint.

## Edge Cases

- `/exit` returns `inputCommand` with tea.Quit — the decision is
  "a command handled it"; quitting is the command's effect.
- A queued draft with images keeps its attachments; queuing is
  unchanged.
- The palette resolution and history push happen before the
  decision branches, exactly as today.

## Test Plan

- Unit: each of the six outcomes, driven through decideInput —
  empty, `!`, `/help`, working+alt, working+plain, idle —
  asserting the typed outcome and the observable effect (queue
  length, transcript entry kind, working flag).
- The existing steer/queue/start behavior tests pass unchanged
  through the wrapper.
- Full suite green across all packages.
