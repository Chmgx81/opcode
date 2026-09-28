# Phase 1 Spec — Real Bubble Tea TUI

## Goal

Replace the bare Phase 0 terminal loop with a Bubble Tea TUI: streamed
tokens rendered as they arrive, a status bar, permission prompts for
Action-Allowed tools, steer vs. follow-up input handling, and `/login` /
`/logout`.

## Non-Goals

- The full permission-mode system (read-only / auto-accept-safe-ops /
  full-auto as selectable modes) — that is Phase 2. Phase 1 honors the
  two already-configured modes enough to be honest: `ask` prompts for
  Action-Allowed tools, `full-auto` allows and logs.
- Subagent panes, token budget management, compaction, session resume.
- Headless `-p` flag (Phase 6). The orchestrator stays UI-independent,
  so nothing here blocks it.

## Approach

- `internal/tui` owns a `tea.Model`. The orchestrator is driven exactly
  as in Phase 0: events pumped from the orchestrator channel to the tea
  program as `tea.Msg`; the TUI never calls into the orchestrator except
  `Send` (new turn) and `Steer` (mid-turn steering).
- **Steering** (spec 3.1): Enter while a turn is running folds the typed
  text into history at the next round boundary (after the current tool
  call finishes). **Follow-up** (Alt+Enter while running): queued in the
  TUI and sent when the turn completes. Enter while idle starts a turn.
- **Permission prompts**: the gate's `Decide` callback is implemented by
  the TUI — it sends a prompt msg to the program and blocks on a reply
  channel. `y` allows once, `a` allows Action-Allowed tools for the rest
  of the session, `n`/Esc denies. The orchestrator and tools packages
  stay UI-free; the blocking decision is just a slow `Decide`.
- **Token usage**: request `stream_options.include_usage` and surface
  prompt/completion totals in the status bar (cumulative per session).
  Servers that don't send usage just show nothing.
- `/login` prompts for a key with masked input and writes it via config
  helpers; `/logout` removes the stored credential and says it does not
  touch env vars or revoke anything at the provider.

## Edge Cases

- User types while a permission prompt is open: keys go to the prompt,
  not the input box.
- Esc while awaiting permission: deny (and stay in the turn); Esc while
  a turn is running with no prompt: cancel the turn.
- Both `/login` and `/logout` must keep auth.json at 0600 in the 0700
  dir; `/logout` with no stored key says so rather than erroring.
- Steering arrives between rounds: it is appended as a user message
  before the next request, never mid-round.
- Follow-up queue drains only after TurnComplete; a queued message is
  visible in the transcript as "(queued)".
- Ctrl+C during a turn cancels the turn; Ctrl+C at idle quits.

## Test Plan

- Unit: llm usage parsing from a usage-bearing SSE chunk; orchestrator
  steering injection (assert the steered text appears in the next
  request's messages); tier policy (ask prompts for action tier,
  full-auto allows, read-only never prompts); config write/remove auth.
- TUI state machine: drive `Update` with synthetic key and event msgs —
  submit while idle, steer while working, queue follow-up, permission
  y/a/n paths, /login and /logout flows — assert state and rendered
  output. No PTY needed.
- Live: run the real binary under a PTY (`script`) against the local
  scripted SSE server and against OpenRouter; verify streaming render,
  a permission prompt answered, and the audit log.
