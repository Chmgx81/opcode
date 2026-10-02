# Phase 48 — the context readout

## Goal

The working line shows tokens flowing (`↓ 1.8k`) but not how full
the window is. Compaction is built (Phase 41) and announces itself
when it fires, but the approach to it is invisible — the user
cannot see the window filling until the recap lands.

The working line now carries the occupancy of the most recent
request: `context 62%`, riding the same parenthesized segment as
elapsed and token flow.

## Non-Goals

- No window-size catalog. Model windows vary and opcode does not
  guess: the readout appears only when `context_window` is set in
  config.json (the same value the compaction trigger uses). Unknown
  window, no readout — silence is honest.
- No compaction warning color. The trigger is growth past 75% of
  the space above the session's baseline (Phase 41's window
  accounting), which raw occupancy does not predict; a red number
  that fires at the wrong moment would be a lie in accent clothing.
  The number tells the story; the recap note tells the ending.
- No per-model window discovery from provider APIs.

## Approach

- `Options.ContextWindow`, wired from `cfg.ContextWindow` in
  cmd/opcode — the same source of truth the orchestrator's
  compaction trigger reads.
- The TUI tracks `lastPrompt`: the prompt-token count of the most
  recent round (`EventUsage`), which is the context in play right
  now. It persists across turns (context does not reset) and clears
  on session resume.
- The working line's full segment becomes
  `(12s • esc to interrupt • ↓ 1.8k tokens • context 62%)` when
  window and lastPrompt are both known. It is the first thing the
  reflow drops on a narrow terminal — elapsed survives, occupancy
  is a refinement.

## Edge Cases

- **Window unset (the default):** no readout, nothing invented.
- **First round in flight:** lastPrompt is still the previous
  turn's until the round reports — the readout lags one round,
  which is the honest precision of the data.
- **After compaction:** the next round's prompt tokens are the
  post-recap size; the readout drops on its own.
- **Over-full window** (occupancy > 100%): shown as-is — the number
  above 100 is the truest signal something is wrong.

## Test Plan

- `TestWorkingLineContextReadout`: with the window set and a usage
  event delivered, the working line carries `context N%`; without a
  window, it never appears; the narrow reflow drops it before the
  elapsed time.
- Full `go test -race -count=1 ./...`, `gofmt -l .`, `go vet ./...`.
