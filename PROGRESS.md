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

---

# Phase 2 — Permission Modes (status: complete, live-verified)

Spec: [docs/specs/phase2-modes.md](docs/specs/phase2-modes.md)

## Built

- `internal/tools` — the four modes (read-only / ask-every-time /
  auto-accept-safe-ops / full-auto), the full 4x3 decision matrix
  against the three Section 7 tiers (including Draft-Only, which no
  built-in tool uses yet — skills in Phase 3 will), mode-based tool
  advertisement, and per-mode system-prompt instructions.
- `internal/orchestrator` — Mode field; read-only mode filters
  action-tier tools out of the request entirely; mode instruction
  composed into the system prompt; `SetMode` for runtime switching.
- `internal/config` — `permission_mode` validated at load (unknown modes
  fail loudly); legacy `ask` normalized to `ask-every-time`.
- `internal/tui` — `/mode` to show or switch; switching rebuilds the
  gate policy and resets any session "allow all" grant.

## Verified for real

- `go test -count=1 ./...` — all packages, including the full decision
  matrix (each cell asserted with both a granting and a denying prompt,
  and prompt-consultation checked per cell), nil-prompt fail-closed,
  tool advertisement asserted on the request the fake provider
  receives, config normalization/rejection, and the /mode command paths.
- PTY sessions against the local scripted server, one per posture:
  read-only mode denied a write without prompting (the model was not
  even offered write tools); full-auto wrote with no prompt; ask
  prompted and the answer gated the call.
- Live OpenRouter run (full-auto): a real edit executed with no
  permission prompt and a clean audit entry.

## Phase 2 assumptions

1. Switching modes resets the session "allow all" grant — a mode change
   re-establishes the posture rather than inheriting a looser one.
2. Unknown modes behave as ask-every-time at the gate (fail closed) and
   are rejected at config load time; the two agree.
3. Draft-Only exists in the matrix but has no built-in tool yet; that is
   a stated gap, not a hidden one.
4. Mid-turn /mode takes effect at the next model request (tool list and
   prompt are per-request); in-flight dispatched calls are done.

## Next (Phase 3, only after user review)

- Skill Loader — with project trust built first per the build order's
  ordering constraint (Section 8): trust must exist before or together
  with Phase 3.

---

# Phase 3 — Project Trust + Skill Loader (status: complete, live-verified)

Spec: [docs/specs/phase3-skills-trust.md](docs/specs/phase3-skills-trust.md)

## Built

- `internal/trust` — the Section 7 trust gate: executable-surface
  fingerprint (skill scripts, `.tilde/config.json`, `.tilde/mcp.json`),
  `trusted-projects.json` persistence, trusted/untrusted/changed
  status. A project with nothing executable is trusted by default.
- `internal/skills` — SKILL.md discovery and parsing (flat frontmatter),
  user-always / project-if-trusted scopes, project-over-user precedence,
  metadata-only index, bad skills skipped with reasons.
- `internal/tools` — `load_skill` (Read-Only: returns the body) and
  `run_skill_script` (Action-Allowed: subprocess, JSON stdin/stdout
  contract, discovered-basename-only so no traversal, timeout).
- `internal/orchestrator` — SkillsIndex composed into the system prompt,
  re-composed per round so a mid-session trust grant reaches the next
  request.
- `internal/tui` — startup trust prompt showing the literal runnable
  files; y persists + re-discovers, n continues user-level only.
- `cmd/tilde` — `--trust` / `TILDE_TRUST=1` pre-approval (CI posture).

## Verified for real

- `go test -count=1 ./...` — all seven packages.
- **PTY, four scenarios against the local scripted server**: declined →
  project skill unreachable ("no skill named hello"); accepted → skill
  body loads and trust persists to trusted-projects.json; second run →
  no re-prompt, skill still available; script modified → re-prompt
  (direnv rule works live).
- **Live OpenRouter**: a real user-level `commit-message` skill — the
  model saw only the metadata index, decided the request matched, called
  `load_skill` itself, and wrote a commit message following the skill's
  rules (fix type, imperative, <50 chars). Progressive disclosure
  driven by a real model, not a mock.

## Found and fixed during verification

- The trust prompt never appeared in the first PTY run: main set
  `PendingTrust` on the options *after* `tui.New` had copied them into
  the model. The decision now must be part of Options before New —
  caught live, fixed, and re-verified.

## Phase 3 assumptions

1. Zero project `config.json` keys are allowed — the spec names no
   allow-list yet; the file is fingerprinted so adding keys later is
   trust-visible.
2. Skill tiers are structural (body load = Read-Only, script run =
   Action-Allowed); no frontmatter can lower them.
3. Project skill wins over user skill on the same name (most specific
   wins, like config precedence).
4. Trigger matching is the model's decision from the index; disclosure
   is enforced mechanically by reachability, not heuristics.
5. Trust is a startup decision: files changing mid-session take effect
   next launch.

## Next (Phase 4, only after user review)

- MCP Manager — project `.tilde/mcp.json` only after trust (the
  fingerprint already covers it).

---

# Phase 4 — MCP Manager (status: complete, live-verified)

Spec: [docs/specs/phase4-mcp.md](docs/specs/phase4-mcp.md)

## Built

- `internal/mcp` — stdio MCP client: JSON-RPC 2.0 newline-delimited,
  initialize -> initialized -> tools/list handshake, tools/call with
  isError propagation, one long-lived reader goroutine (notifications
  interleaving with responses are skipped by id-matching).
- Manager: parallel connects with a 5s per-server timeout, failure
  notes instead of startup blocks, clean shutdown (own process group,
  killed on exit), collision rule (a project server can never replace
  a user server's name), and mid-session project connects after a
  trust grant.
- Tool adapters: `mcp__<server>__<tool>`, always Action-Allowed
  (readOnlyHint is self-reported; hints are not a permission model).
- Wiring: user mcp.json always; project mcp.json only when trusted;
  startup notes in the TUI greeting.

## Verified for real

- `go test -count=1 ./...` — all eight packages, against a real
  subprocess MCP server (a Python fixture speaking the actual
  protocol): handshake, discovery, call round-trip, isError, restart-
  once after a genuine crash (marker-file fixture so the retry
  provably runs), silent-server timeout bounded by the manager.
- **PTY**: user-level mcp.json with the fixture — startup note
  "mcp: fixture (1 tools)", the scripted model called
  `mcp__fixture__echo`, the subprocess answered, the result fed back,
  audit logged with tier.
- **Live OpenRouter**: the model called the MCP tool over the real
  API and reported the real result verbatim.

## Found and fixed during verification

- Self-deadlock in the client: `start()` held the mutex while the
  handshake's roundtrip tried to lock it (mutexes are not reentrant).
  Spawn now happens under the lock; the handshake runs after it. Caught
  by the test binary's panic dump.
- Two test-fixture bugs of my own (a die-on-every-call mode that made
  the restart test unpassable, and a bare-number output that is
  incidentally valid JSON) — fixed by making the fixtures honest.

## Phase 4 assumptions

1. stdio transport only; HTTP entries are rejected with a clear
   message (stated gap, not a silent no-op).
2. All MCP tools are Action-Allowed regardless of server hints.
3. Trust grants mid-session connect project servers immediately
   (process spawn — the approved surface).
4. Context discipline for very large tool inventories is deferred;
   counts are small.

## Next (Phase 5, only after user review)

- Subagent Manager: in-process goroutines, typed event channels,
  same trust boundary — built last per the spec, once everything
  underneath is solid.
