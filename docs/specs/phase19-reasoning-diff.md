# Phase 19 — Reasoning display + write-as-diff

## Goal

Two render-layer gaps closed in one pass:

1. Reasoning models (DeepSeek R1-style) stream `reasoning` deltas that
   tilde silently dropped — the user saw nothing while the model
   "thought".
2. `write_file` results rendered as raw text, while `edit_file` already
   had a highlighted diff. Writes should read the same way.

## Non-Goals

- Persisting reasoning in conversation history — thinking is for the
  UI; the answer is what the conversation keeps. Replaying a resumed
  session shows answers, not thoughts.
- Sending reasoning back to the API on subsequent turns (the wire
  format for that is provider-specific and mostly undocumented).
- Diffing write_file against the previous file version — writes
  target new or fully-replaced content, so every line is an addition;
  a synthetic before-state adds noise, not information.

## Approach

**The wire.** Two field conventions in the wild: OpenRouter puts
thinking in `delta.reasoning`, DeepSeek in `delta.reasoning_content`.
The SSE delta struct carries both; whichever is non-empty emits a
`ChatEvent{Type: ReasoningEvent, Text}` before any content event. One
test pins both conventions against recorded delta JSON.

**The pipeline.** The orchestrator forwards reasoning events to the
UI but does not store them in history (Non-Goal 1). A new
`EventReasoning` kind carries them — same channel, same ordering.

**The render.** Reasoning accumulates in a builder, like streamed
content. Live view: a `△ thinking…` head with a dim italic tail
windowed to the last 3 lines — the user sees current thought, not a
wall. Boundaries (first text, tool call, completion, error) collapse
it into one entry: `△ thought for Ns · M chars (ctrl+r to expand)`.
The existing ctrl+r toggle expands it, windowed to 12 rows, alongside
tool results — one expand mechanism, not two.

**Write-as-diff.** `write_file` result entries parse the tool args
`{path, content}`; collapsed reads `└ wrote path +N`, expanded renders
`+ `-prefixed lines, syntax-highlighted by the file's existing
`lexerFor(path)` and windowed to 10 rows — identical chrome to
edit_file's diff.

## Edge cases

- Reasoning that never ends before an error: the error boundary
  collapses whatever arrived.
- Empty reasoning deltas are ignored; a turn with only content gets
  no thinking entry at all.
- `0s` durations are honest, not hidden — fast thoughts are fine.

## Test plan

- Unit: reasoning delta parsing (both conventions), render lifecycle
  (live tail-windowing across 5 steps, collapse, expand), write_file
  diff render (collapsed count, expanded `+` lines, window cap).
- PTY, live: a scripted SSE fixture streams reasoning, then content,
  a write_file call, and a final answer — assert the live `△ thinking…`
  frame, the collapsed entry, the expanded diff, and that the file
  really exists on disk.
