# Phase 35 — the /diff command

## Goal

After a session of edits the user asks "what did you change?" — the
answer should be a command, not a shell round trip. `/diff` runs git
and renders the working tree's changes as a colored diff in the
transcript: added lines in the success color, removed lines in the
danger color, hunk headers dim, file headers bold — the same verdict
vocabulary the edit_file results already use.

## Non-Goals

- No diff input from the model, no new model tool: /diff is a
  user-invoked read-only git query, like the `!` shell escape —
  user-typed commands never round trip through the model or the
  permission gate.
- No staging, committing, reverting, or any state change: /diff
  presents, it never touches anything.
- No side-by-side or interline diff engine: git's own unified diff,
  recolored. The tool timeline's edit_file hunks keep their
  before/after renderer; /diff renders git's text.

## Approach

`internal/tui/diff.go`, dispatched like /skills and /mcp:

- `git diff --no-color HEAD` (staged + unstaged against the last
  commit); on a repo with no commits yet, fall back to plain
  `git diff --no-color`. Output is already untrusted text and passes
  `safe.Text` — a filename or file content cannot drive the terminal.
- `git status --porcelain` beside it, so untracked files — which
  `git diff` cannot show — are listed as dim `?` rows instead of
  silently missing from the review.
- Rendering: `diff --git` / `index` / `---` / `+++` header lines bold
  or dim, `@@` hunk lines dim info, `+` lines the success color,
  `-` lines the danger color, context lines dim. Verdict colors, not
  syntax tokens — git's diff already tells the reader what matters.
- Capped at 400 rendered lines with an honest "… N more lines"
  footer; a huge generated diff must not flood the transcript.
- Every outcome is designed, not just the happy path:
  - not a git repository — dim fact naming what /diff needs
  - git not installed — an error entry naming the missing binary
  - clean tree, no untracked — dim "working tree clean"
  - git error (shallow clone, broken index) — the error verbatim

Executed through `tools.Bash` — the same path the `!` shell escape
uses: bash -c, sandboxed by default, 5-minute bound, combined output.
Read-only commands, but no special casing: if the sandbox confines
something git needs, the honest error surfaces rather than a silent
escape.

## Edge Cases

- A diff produced while a turn is running: /diff answers from disk
  at the moment it runs; in-flight edits land after.
- CRLF and binary files: git prints its own notices; they render as
  ordinary lines.
- Very long lines are not wrapped — they render as git printed them;
  the transcript pager (ctrl+o) scrolls the full lines.

## Test Plan

- Unit: the renderer maps each line class to its verdict color and
  the cap leaves the honest footer.
- Unit: a repo with staged and unstaged changes renders both (the
  HEAD form); a repo with no commits falls back to the plain form.
- Unit: untracked files appear as `?` rows; a clean tree says so; a
  non-repo says so; a missing git binary surfaces as an error entry.
- PTY live: a real repo with a modification and an untracked file —
  /diff renders the colored hunks; a clean repo renders the clean
  note.
- Full suite green across all packages.
