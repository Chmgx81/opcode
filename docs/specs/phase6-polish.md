# Phase 6 Spec — Polish

## Goal

Close out the build order's Phase 6: headless invocation, the spec's
resolved session-storage decision, compaction, the startup sequence's
context loading, and packaging. Plus an explicit list of what stays
deferred so nothing hides.

## Scope

1. **Headless** (Section 3.9): `tilde -p "prompt"` runs one turn
   non-interactively; `--json` emits one JSON event object per line.
   The orchestrator is the same instance the TUI drives — this is the
   payoff of the layering. Permissions fail closed: with nobody to
   ask, an Action-Allowed call is denied and the denial is visible in
   the output; use full-auto for unattended automation. Project trust
   stays default-untrusted without `--trust` (already the case).
2. **AGENTS.md hierarchical context** (Section 5, step 3 — the item no
   phase owned): user `~/.tilde/AGENTS.md`, then each directory from
   root to cwd; per directory `AGENTS.override.md` beats `AGENTS.md`
   beats `CLAUDE.md`. Inert text, loaded regardless of trust, capped
   per file, composed into the system prompt.
3. **Tree-structured sessions** (Section 3.7, the resolved decision):
   one JSON file per session under `~/.tilde/sessions/`; every message
   is a node with a parent, so rewinding and continuing creates a
   branch — the format supports branches from day one even though the
   TUI's `/tree` rewind view is deferred. Saved on exit with
   credentials redacted; `--resume <id>` (or `--continue` for the
   latest) seeds the orchestrator's history.
4. **Compaction** (Section 3.2): when the previous round's reported
   prompt tokens reach a configurable fraction (default 75%) of a
   configured context window, the oldest messages are summarized into
   a recap via a provider call (separate compaction model optional,
   default the session model) and replaced in place. Disabled by
   default (`context_window` 0) because model windows vary and a
   wrong default silently rewrites history.
5. **Packaging**: module path becomes `github.com/Chmgx81/tilde` so
   `go install` works; README install/build docs.
6. Audit log: already built (Phase 0/1) — nothing to add.

## Non-Goals (explicitly deferred, not forgotten)

- `/tree` rewind view in the TUI (the spec itself says "later").
- `/config` and `/reload` TUI commands.
- Prompt templates (`/prompts/`), MCP debug view, project config.json
  allow-list keys.
- Context discipline for huge tool inventories (still small).

## Test Plan

- Unit: headless runner (text + JSON modes) against a scripted
  provider through the real orchestrator; AGENTS.md hierarchy,
  precedence, and fallback; session save/load/branch shapes and
  redaction; compaction trigger, recap replacement, and disabled
  default.
- Live: `tilde -p` against the local scripted server (text and JSON),
  then a live OpenRouter one-shot.
