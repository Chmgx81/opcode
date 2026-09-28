# Phase 5 Spec — Subagent Manager

## Goal

Let the model delegate a self-contained task to a subagent (Section
3.3): another Orchestrator instance with a narrower system prompt and
tool subset, running in its own goroutine, reporting progress through
typed events the TUI renders live — the user always sees what each
subagent is doing, not just the final result.

## Non-Goals

- Parallel subagents: multiple spawn calls in one model round execute
  sequentially. The TUI already stays live during a spawn (events
  stream while the parent waits), which is the non-blocking property
  the spec demands; true concurrent spawns change core dispatch and
  buy nothing yet.
- Nested subagents: the spawn tool is excluded from the subagent's own
  registry, so recursion is impossible by construction, not by a
  depth counter.
- Subagent panes/overlay UI: labeled transcript lines now ("collaps-
  ible log" spirit); panes are a visual upgrade for later.
- Selectable models per subagent: same model as the parent, config
  change later if ever needed.

## Approach

- `internal/subagent` — Runner: builds a sub-Orchestrator (same
  provider, model, and gate — same trust boundary, same audit log,
  same permission prompts) with its own system prompt ("complete the
  task, reply with the result only") and a registry subset that drops
  every tool whose name starts with `spawn_`.
- The parent reaches subagents through one tool: `spawn_subagent`
  {task, title?}. Action-Allowed tier — it is a paid API call. The
  tool blocks until the subagent's turn completes; the subagent's
  final assistant text is the tool result fed back to the parent.
- Events: title-labeled {text, tool, done, error, usage} flow through
  an injectable emitter wired by main to the TUI via program.Send —
  the same pattern as permission prompts, so the plumbing stays one
  shape.
- TUI: a `subagentMsg` renders dim labeled lines. Streaming text
  accumulates per title and flushes at natural boundaries (tool call,
  done, error) so the log stays readable instead of fragmenting into
  one line per delta. Subagent usage adds to the session totals.

## Edge Cases

- Subagent ends with no final text (tool loop exhausted, error): the
  parent gets an explicit "finished without a final answer" result,
  not silence.
- A subagent's permission prompt blocks the subagent's goroutine —
  exactly like the parent's; the gate is shared, so y/a/n behaves
  identically.
- Esc cancels the parent turn, which cancels the subagent's context
  too (ctx flows down); both stop.
- Missing `task` in the tool arguments is a clear tool error the model
  can correct.

## Test Plan

- Unit: runner returns the final text and emits the full event
  sequence; error propagation; the subagent's request carries the
  subagent system prompt and NO spawn tool (no recursion, asserted on
  the request the fake provider receives); the spawn tool's tier and
  argument validation; the same gate/audit receives the subagent's
  tool calls.
- TUI: subagentMsg renders labeled lines; text accumulation flushes
  at boundaries; usage adds to totals.
- Live: PTY with a scripted model calling spawn_subagent (the fake
  server distinguishes parent/subagent requests by system prompt),
  subagent executes a real tool, parent reports the subagent's result.
  Then a live OpenRouter run delegating a real task.
