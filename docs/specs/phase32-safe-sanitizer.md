# Phase 32 — the safe/ sanitizer

## Goal

Untrusted text must never drive the terminal. Tool results (file
contents, bash output, fetched pages), model output, and tool
arguments all reach the display; a sequence like `ESC]0;…BEL` in a
read file could retitle the window, `ESC[2J` could clear it, `ESC c`
could reset it. Strip every terminal control sequence and dangerous
control character from model- and tool-originated text before it is
rendered.

## Non-Goals

- Not a general "clean" pipeline: user-typed text and user file paths
  in opcode's own UI copy are trusted and untouched.
- Model-facing data is untouched: what the LLM sees in its context is
  exactly what the tool returned. Sanitizing there would silently
  corrupt file contents the model is reasoning about. The boundary is
  the display, not the context.
- JSON headless output is untouched — `encoding/json` escapes all
  control characters, so a JSON line can never carry a live sequence;
  downstream scripts get the honest bytes.
- No sanitization of session storage. Sessions are data files; the
  display boundary covers them.

## Approach

One new package, `internal/safe`, exposing `Text(s string) string`:

- Scans by rune, so UTF-8 text (including CJK, emoji, box-drawing)
  passes through untouched and continuation bytes are never mistaken
  for control bytes.
- Drops all ESC-initiated sequences with a typed parser: CSI
  (`ESC[…40–7E final`), OSC/DCS/SOS/PM/APC (`ESC]`, `ESCP`, `ESCX`,
  `ESC^`, `ESC_` … BEL or ST), nF intermediates (`ESC(B`), and bare
  Fe/Fs two-rune forms (`ESC c`).
- Drops C0 controls except `\n` and `\t`, drops DEL, drops C1 runes
  U+0080–U+009F (some terminals execute 8-bit C1, e.g. `0x9B` as CSI).
- Unterminated sequences are consumed, never passed on: the failure
  direction is display fidelity, never terminal control.

Wired at the display boundaries only:

- TUI `handleEvent`: `ev.Text`, `ev.ToolResult`, `ev.ToolCall.Arguments`
  sanitized once at the top — one choke point covers stream text,
  reasoning, compaction notes, tool lines, and result entries (the
  summary, the expanded view, the transcript pager, and committed
  scrollback all read these entries).
- TUI dialogs: the permission request stores a sanitized display copy
  of `args` (the raw args still drive grant matching and execution),
  and the plan dialog stores a sanitized copy of the plan.
- TUI subagent progress entries.
- Headless text mode: `[tool]`, `[result]`, and streamed text lines.

Known residual, accepted: a sequence split across two stream chunks
leaves its tail as inert printable text (`"[31m"`), never a live
sequence — the ESC half is always stripped.

## Edge Cases

- A file containing a literal unterminated OSC (e.g. `ESC]0;oops` with
  no BEL): the parser ends the sequence at the next BEL/ST or the next
  ESC that does not start ST, so the rest of the file renders.
- `CRLF` becomes `LF`; lone `\r` is dropped (carriage-return tricks
  only work on the terminal, and the display should not be one).
- Invalid UTF-8 bytes decode as U+FFFD (Go rune iteration); they are
  display text, never executed.
- An ESC as the last byte of a chunk: dropped alone; safe.

## Test Plan

- Unit: table-driven over CSI/OSC/DCS/nF/Fe forms, C0/C1/DEL,
  UTF-8 preservation (including a rune whose UTF-8 encoding contains
  0x80, proving rune-level scanning), unterminated sequences, and the
  keep-set (`\n`, `\t`).
- Unit: a TUI-level test feeding `handleEvent` an EventToolResult whose
  payload contains `ESC[2J` and `ESC]0;pwned BEL` asserts the stored
  entry carries neither.
- Unit: headless text mode renders a result with a title-injection
  payload without the escape bytes.
- Live: a PTY session where a fixture model returns a poisoned tool
  result; the window title and screen survive, and the poisoned bytes
  do not appear raw in the transcript.
- Full suite green across all packages.
