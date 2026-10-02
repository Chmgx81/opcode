# Phase 49 — scrollback that scrolls

## Goal

Found live (the third report): a long first answer filled the
live region, the frame trimmed its start to "… 24 earlier lines",
and the terminal's own scrolling had nothing to show — because a
finished turn only committed to native scrollback when the NEXT
turn started. The reader's native way back through an answer is
the terminal scrollbar, and it was a full turn behind.

1. **A finished turn commits when it finishes.** `turnEnded`
   commits the transcript to native scrollback at the moment the
   turn completes, instead of holding it live for the next
   boundary. After every answer the scrollbar works; the live
   region goes back to holding only what is actually in flight.
2. **The trim marker names its escape.** "… 24 earlier lines"
   counted the loss and named no way to read it — the one dead end
   left in a UI whose rule is that every empty state says its way
   forward. It now reads "… 24 earlier lines — ctrl+o to read":
   the pager shows the whole conversation, committed and live
   alike, expanded.

## Non-Goals

- No change to the frozen-text contract: what's printed no longer
  redraws, ctrl+r expansion stays live-region-only, and committed
  markdown keeps its colors after a theme switch. The change moves
  the boundary, not the semantics — the last turn's ctrl+r
  expansion moves to the pager, which always expanded everything.
- No mid-turn streaming change: while a turn runs, the live region
  still trims with the marker — that is inherent to an inline
  renderer, and the marker now names the pager.

## Approach

- `turnEnded` with an empty follow-up queue returns
  `commitEntries()` instead of nil. The queued-follow-up path
  already committed there; the two paths now agree.
- The frame's trim marker (`fit`) appends the pager hint; the dash
  degrades under `--plain` like the rest of the chrome.

## Edge Cases

- **Interrupted turns** do not pass through `turnEnded`; they keep
  committing at the next boundary, as before.
- **No program wired** (tests, headless embeds): commit is a
  no-op, behavior unchanged.
- **The next submit's commit** finds nothing new and no-ops — the
  seam logic already handles the empty case.

## Test Plan

- `TestFinishedTurnCommitsAtTheEnd`: a working model with a program
  wired ends a turn with an empty queue — `committed` reaches the
  entry count, the print Cmd is returned, and View drops the
  finished answer from the live region.
- `TestTrimMarkerNamesTheEscape`: an over-tall transcript at a
  small height renders a marker that names ctrl+o.
- Full `go test -race -count=1 ./...`, `gofmt -l .`, `go vet ./...`.
