# Phase 33 — the /doctor command

## Goal

When something breaks, the user's first instinct should be a command,
not a restart. `/doctor` renders a live diagnostic report into the
transcript: one line per subsystem, each with a verdict glyph (✓ ok,
⚠ needs attention, ✗ broken) and — wherever something is wrong —
the next step in plain language. It is a presentation layer over
checks that already exist as startup paths; it invents no new logic.

## Non-Goals

- No network probing, no test API call, no "am I reachable" check —
  a real request costs money and time; the startup path already
  fails loudly on a bad endpoint.
- No writes: doctor never repairs, creates, or migrates anything.
- No new config parsing rules. It re-reads the same files through
  the same loaders the startup path uses (`config.LoadConfig`,
  `config.LoadModels`, `trust.LoadStore`) and reports their
  verdicts.
- No secrets: the API key check reports presence, never the key.

## Approach

A `/doctor` slash command (palette-listed like /skills and /mcp)
that builds rows from live state and adds one dim transcript entry.
Checks, in report order:

1. **Identity** — opcode's version string.
2. **Model** — provider, model, base URL from Options (what the
   session actually runs right now, not what config says).
3. **API key** — `Options.KeyFor(provider)`; present or missing
   with the `/login <provider>` next step. Nil KeyFor is an honest
   "key resolution not wired", not an error.
4. **config.json** — re-loaded from OpcodeHome; parse/permission-mode
   errors reported verbatim; the mode shown when healthy. A missing
   file is the normal first-run case and reads as such.
5. **models.json** — same loader verdict; missing file is dim, not
   an error (the picker falls back to the active provider anyway).
6. **Sandbox** — three honest postures: active (with the Landlock
   ABI version), available-but-off (with the config key that turns
   it on), unavailable on this platform (shell runs unsandboxed).
   `sandbox.Active()` is the truth the gate itself consults; doctor
   never claims enforcement that isn't real.
7. **Trust** — the project's live status (trusted / untrusted /
   changed) via `trust.LoadStore(OpcodeHome)`, with the changed-file
   count when the surface drifted.
8. **Skills and MCP** — counts and server names from the wired
   managers; "none" is a dim fact, not a failure.
9. **Audit log** — the path Options carries; exists (with size) or
   "created on first gated action" when absent.
10. **Terminal** — the color profile actually in effect, TERM,
    NO_COLOR, and the --plain posture if set.

## Edge Cases

- OpcodeHome unset in Options (headless wiring) — file checks report
  "not wired" dim instead of probing the filesystem blindly.
- A project directory that no longer exists — trust status reports
  the error verbatim rather than crashing the command.
- `!command` credentials — KeyFor may exec the user's command; that
  is the same resolution the /models picker already does, and the
  result is cached process-lifetime.
- Unusual glyphs — the plain/ASCII vocabulary (adaptGlyphs) already
  degrades every glyph; doctor uses the shared vocabulary.

## Test Plan

- Unit: a Model with healthy wiring (key present, real config dir,
  audit file present) renders all rows with the version, the
  provider/model line, and the ok glyph; the report never contains
  the key bytes.
- Unit: a missing key renders the warn glyph and the
  `/login <provider>` next step.
- Unit: a malformed config.json in OpcodeHome renders the error row,
  not a crash.
- Unit: sandbox-off posture names the config key that enables it.
- PTY live: /doctor through the palette in a real session renders
  the full report.
- Full suite green across all packages.
