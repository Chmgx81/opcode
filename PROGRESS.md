# PROGRESS.md — tilde build log

Phase 0 status: **complete and verified as far as possible without a real
OpenRouter key.** Stop point for user review.

## What Phase 0 built

| Package | What it does |
|---|---|
| `cmd/tilde` | Bare terminal input/output loop. Thin: wiring only, all logic lives below. No TUI framework. |
| `internal/orchestrator` | The agent loop (spec §6): send → stream → dispatch completed tool calls → feed results back → repeat until the model stops. Emits typed events on a channel; knows nothing about any UI. |
| `internal/tools` | 4 built-ins (`read_file`, `write_file`, `edit_file`, `run_shell`), the `Registry`, the single permission `Gate`, the JSONL audit log, and the `Redactor`. |
| `internal/llm` | `Provider` interface + `OpenAICompat` client: OpenAI-compatible `chat/completions`, SSE streaming, tool-call fragment reassembly keyed by index. |
| `internal/config` | User-level config only: `config.json`, `models.json`, `auth.json`, credential resolution (auth.json → `!command` → env var), project-credential refusal, permission warnings. |

## Verified for real (executed, not just compiled)

- `go test ./...` — all packages pass. Coverage:
  - Streamed tool-call reassembly: args split across chunks, interleaved
    calls, whole-call-in-one-chunk, missing `finish_reason`, provider
    error chunks, HTTP error status.
  - Credential resolution: auth.json beats env, env fallback, `!command`
    exec + process-lifetime caching, failed command is an error (no
    silent fallback), project-level key refused, loose-permission
    warnings.
  - Each built-in tool against temp dirs, including failure modes
    (missing file, ambiguous edit match, nonzero shell exit).
  - Gate: allow+log, deny-without-execute, credential redaction in the
    audit log.
  - Orchestrator: tool really executes, result really fed back (asserted
    on the second request's message list), unknown tool reported to the
    model, gate denial reaches the model, max-rounds bound.
  - Full stack except real network: real SSE client over `httptest`
    driving the real orchestrator + gate + tools.
- **End-to-end smoke test with the built binary** against a local
  scripted OpenAI-compatible SSE server (Python, throwaway, in /tmp):
  user message → streamed text → `write_file` with arguments split across
  two SSE chunks → tool really executed → result fed back → model
  confirmed it saw the real result → turn completed. Also verified the
  startup warning when a project-level `.tilde/auth.json` exists (and
  that its key was ignored).
- **Live OpenRouter run (2026-09-28, model `inclusionai/ling-3.0-flash-fin:free`)**:
  the same binary against the real `https://openrouter.ai/api/v1` with the
  user's key in `~/.tilde/auth.json`. Two turns exercised all four
  built-ins for real: `write_file` (file created with the requested
  content), `read_file` (contents fed back, model quoted them), `edit_file`
  (word replaced), `run_shell` (`cat report.txt`, output quoted by the
  model). Audit log has all 5 gate entries with tiers; the API key does
  not appear anywhere in the audit log or tool outputs.
- `go vet` clean, `gofmt` clean, `go build ./...` clean.

## Not verified

- **A real OpenRouter call.** No API key in this environment. The loop is
  proven end to end against a local OpenAI-compatible server; the only
  unproven step is OpenRouter's own endpoint behavior. To verify: set
  `model` in `~/.tilde/config.json`, put a key in `~/.tilde/auth.json` (or
  `OPENROUTER_API_KEY`), and run `tilde` in a test directory.

## Phase 0 assumptions (spec didn't pin these down)

1. `models.json` "optional key reference" = an `api_key_env` field naming
   the provider's credential environment variable (default
   `OPENROUTER_API_KEY` for the built-in openrouter entry). Empty means
   "needs no key" (local servers).
2. `auth.json` is a flat map: `{"openrouter": "<key-or-!command>"}`.
3. If `config.json` has no `model`, tilde refuses to start with
   instructions rather than guessing a default model (a wrong default
   costs real money).
4. Tool calls are emitted to the orchestrator when the model's
   `finish_reason` arrives (or the stream ends) — the only reliable
   "arguments are complete" signal in the OpenAI streaming format.
5. `edit_file` requires exactly one match; zero or multiple is an error.
6. Tool results are capped at 50,000 characters fed back to the model.
7. One assistant message per model response, tool results in the order
   the model emitted them.

## Known gaps (deliberate, later phases)

- Permission gate always allows (Phase 0 requirement); `permission_mode`
  is loaded and validated but not yet enforced — that's Phase 2.
- Audit log lives at `~/.tilde/audit.jsonl`; rotation can come later.
- No session persistence, compaction, project trust, skills, MCP,
  subagents, streaming cancel — all per the build order (spec §8).

## Next (Phase 1, only after user review)

- Real Bubble Tea TUI: streaming render, status bar, permission prompts,
  steer/follow-up, `/login` and `/logout`.

---

# Phase 1 — Real Bubble Tea TUI (status: complete, live-verified)

Spec: [docs/specs/phase1-tui.md](docs/specs/phase1-tui.md)

## Built

- `internal/tui` — Bubble Tea front end: streaming render, status bar
  (model, mode, cwd, cumulative token usage, spinner), permission
  prompts, steer/follow-up input handling, `/login`, `/logout`, `/exit`.
- `internal/llm` — `stream_options.include_usage`; Usage events.
- `internal/orchestrator` — `Steer()` (folds in at round boundaries),
  `ErrCancelled` sentinel, usage forwarding.
- `internal/tools` — `PolicyDecide`: the Phase 1 subset of Section 7's
  tiered model (Read-Only always allowed; Action-Allowed prompted in ask
  mode, allowed+logged in full-auto, denied when no one can answer).
- `internal/config` — `WriteAuthKey` / `RemoveAuthKey` (0600/0700 kept),
  the backend for `/login` and `/logout`.

## Verified for real

- `go test -count=1 ./...` — all packages, including:
  - A full `tea.Program` session test (not just Update calls): real
    orchestrator, real gate in ask mode, real SSE server, permission
    prompt answered by a synthetic keystroke — tool executes, round 2
    runs, audit written.
  - TUI state machine: steer vs follow-up vs submit, queue drain after
    turn complete, permission y/a/n paths, cancel rendering, /login and
    /logout against temp dirs.
  - Orchestrator: steering folds in at the round boundary after the tool
    result; cancellation maps to `ErrCancelled`.
- **Live OpenRouter TUI run (2026-09-28, `inclusionai/ling-3.0-flash-fin:free`,
  ask mode)**: full session in a PTY — streamed response, `write_file`
  permission prompt answered with `y`, real file written, `read_file`
  ran without prompting (Read-Only tier), model confirmed the real
  contents, clean `/exit`. Audit log shows the allowed action-tier call
  and the unprompted read-tier call.
- **Live bug found and fixed during that run**: OpenRouter interleaves
  non-JSON `data:` lines in its SSE stream; the Phase 0 parser killed the
  turn on them. The parser now skips unparsable data lines (regression
  tested); errors inside valid JSON still fail loudly.

## Phase 1 assumptions

1. Permission prompt is inline in the transcript, not a modal overlay
   (spec Section 9 open question — inline is simpler and keeps context
   visible; can be revisited).
2. `y`/`a`/`n` single keys answer prompts; Esc denies.
3. Only `ask` and `full-auto` modes are honored; other values behave as
   ask (fail closed). The full mode set is Phase 2.
4. `/login` writes the key for the configured provider and rebuilds the
   provider + audit redactor immediately; no restart needed.
5. Token usage is requested via `include_usage` and shown cumulatively;
   servers that don't report it show nothing.

## Next (Phase 2, only after user review)

- Full permission mode system: read-only / ask-every-time /
  auto-accept-safe-ops / full-auto as a switchable mode set, mode-aware
  tool filtering and prompt policy.
