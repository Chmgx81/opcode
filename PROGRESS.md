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

---

# Phase 5 — Subagent Manager (status: complete, live-verified)

Spec: [docs/specs/phase5-subagents.md](docs/specs/phase5-subagents.md)

## Built

- `internal/subagent` — Runner (a sub-orchestrator: same provider,
  model, and gate — one trust boundary, one audit log — narrower
  system prompt, tool subset with every spawn tool excluded so
  recursion is impossible by construction), the `spawn_subagent` tool
  (Action-Allowed: a paid API call), and a settable Emitter so the
  TUI becomes the progress sink once it exists.
- `internal/tui` — labeled `[subagent title]` transcript lines; text
  accumulates per title and flushes at boundaries (tool, done, error)
  so the log stays readable; subagent usage adds to session totals.
- `internal/tools` — `Registry.All()` for the subset builder.

## Verified for real

- `go test -count=1 ./...` — all nine packages: runner returns the
  final answer and the exact event sequence; the subagent's request
  provably carries the subagent prompt and NO spawn tool (asserted on
  the request the fake provider receives); errors propagate; an
  empty answer is explicit, not silent; subagent tool calls land in
  the shared audit log; spawn tool tier and argument validation;
  TUI rendering and usage accumulation.
- **PTY**: parent called spawn_subagent; the TUI showed live labeled
  activity while the subagent really wrote a file through the shared
  gate; the result fed back; both levels audited.
- **Live OpenRouter**: the real model delegated a task; the subagent
  ran `echo delegate` for real, reported the output, and the parent
  relayed it — all visible in the transcript and audit log.

## Phase 5 assumptions

1. Sequential spawns: multiple spawn calls in one round execute one
   after another. The TUI stays live during a spawn (events stream
   while the parent waits), which is the non-blocking property the
   spec requires; parallel dispatch buys nothing yet and changes core
   dispatch.
2. No nesting: enforced by tool exclusion, not a depth counter.
3. Subagents share the parent's model and permission posture
   (including mid-session mode switches — the gate is shared).
4. Labeled transcript lines now; panes are a later visual upgrade.

## Next (Phase 6, only after user review)

- Polish: tree-structured session storage + resume, compaction,
  headless `-p`/`--json`, packaging/distribution. Plus the
  deliberately deferred items: AGENTS.md hierarchical loading (spec
  Section 5, still unowned), `/config` and `/reload`, prompt
  templates, MCP debug view, project config allow-list keys.

---

# Phase 6 — Polish (status: complete, live-verified)

Spec: [docs/specs/phase6-polish.md](docs/specs/phase6-polish.md)

## Built

- `internal/headless` — `tilde -p "prompt"` one-shot mode, plain text
  or `--json` (one event object per line). Permissions fail closed
  (nobody to ask); `--trust` keeps Section 7's headless posture.
- `internal/config` — hierarchical AGENTS.md context (Section 5 step 3,
  the item no phase owned): user file + root-to-cwd chain,
  AGENTS.override.md > AGENTS.md > CLAUDE.md per directory, 32KB cap
  per file, composed into the system prompt, loaded regardless of
  trust.
- `internal/session` — tree-structured session storage (Section 3.7's
  resolved decision): every message a node with a parent so rewind
  branches instead of overwriting, one JSON file per session under
  ~/.tilde/sessions/, 0600, credentials redacted on write; saved on
  exit in every mode; `--resume` / `--continue` seed the orchestrator.
- Compaction (Section 3.2) — opt-in via `context_window`
  (0 = disabled; model windows vary and a wrong default would silently
  rewrite history), 75% threshold on the provider-reported prompt
  size, summarizer round through the same Provider interface
  (optional cheaper `compaction_model`), failure skips compaction and
  the turn continues.
- Packaging — module path is now `github.com/Chmgx81/tilde` so
  `go install` works; README documents install, headless, sessions,
  compaction, and AGENTS.md behavior.

## Verified for real

- `go test -count=1 ./...` — all eleven packages.
- **Headless live**: `-p` text mode streamed with tool lines and
  exit 0; `--json` produced one valid event object per line; the
  session file appeared (0600, tree nodes); `--continue` printed the
  resume note and the seeded history demonstrably reached the model
  (the scripted server answered from prior context immediately).
- **Live OpenRouter one-shot**: `-p` over the real API ran a real
  write_file + read_file round trip, and the saved session contains
  zero credential bytes (grep for the key: 0 matches).
- AGENTS.md composition is unit-tested end to end (hierarchy,
  precedence, fallback, caps); the live run had an AGENTS.md present
  and composed, though the free model's obedience to it is not
  asserted.

## Explicitly deferred (not forgotten)

- `/tree` rewind view in the TUI (storage supports it today).
- `/config` and `/reload` commands; prompt templates; MCP debug
  view; project config.json allow-list keys; parallel subagent
  dispatch; MCP over HTTP.

## Build order complete

Every phase of Section 8 is now built and live-verified: 0 loop,
1 TUI, 2 modes, 3 trust + skills, 4 MCP, 5 subagents, 6 polish.

---

# Phase 7 — UI/UX Overhaul + Branding (status: complete, live-verified)

## Built

- **Design tokens** (`internal/tui/style.go`): tilde's own identity —
  the green stack (16DB65 / 058C42 / 04471C / 0D2818 / 020202) plus
  danger/warning/info/dim semantics, a fixed glyph vocabulary (● └ ▹ ✓
  ! − +), and one place to reskin the whole product.
- **Composer v2**: multiline textarea (ctrl+j newline — shift+enter is
  indistinguishable from enter in this bubbletea version, no kitty
  protocol), bracketed paste with large pastes (≥4 lines or ≥1000
  chars) collapsed to `[paste N · L lines]` tokens that re-expand on
  submit, `!` shell escape (user-run, no model round trip), `@path`
  file mentions expanded inline (2KB cap).
- **Command palette**: typing `/` opens a filtered picker (arrows +
  enter); /help /model /skills /mcp /mode /login /logout /exit.
- **Tool timeline**: ● Tool(args) with └ collapsed result lines,
  ctrl+r expands/collapses; edit_file results render as colored diff
  hunks with +/− counts; assistant text boxes fenced code blocks and
  bolds headings.
- **Status & mode**: a working line (spinner, elapsed, tokens, esc/
  steer/queue hints), a persistent mode line under the composer, and
  shift+tab cycles permission modes with a toast; compaction renders
  as a status entry.
- **Help overlay** (`?`) with keys and commands.
- **Color robustness**: termenv's terminal query fails under script/
  CI (no answer → zero color); Run() now falls back to $TERM /
  COLORTERM so the brand palette survives those environments. Found
  live: the first PTY run rendered with no color at all.

## Verified for real

- All eleven packages pass; the TUI suite now covers paste collapse +
  re-expansion, palette filtering/execution (including the
  clear-before-consult bug the test caught), shift+tab cycling with
  gate follow-through, ctrl+r expansion, edit_file diff rendering,
  shell escape, @mention attachment, help overlay, layout fitting.
- **PTY**: timeline, collapsed/expanded results, mode cycle + toast,
  palette, /model — all live; 430 SGR sequences confirmed (the
  palette renders as ANSI256 under xterm-256color, exact hexes on
  truecolor terminals).
- **Live OpenRouter**: real write_file/read_file through the new UI,
  file created, clean exit.

## Found and fixed during verification

- Palette bug: submitInput cleared the composer before consulting the
  palette, so "/mo" + Enter became an unknown command instead of the
  highlighted /mode or /model.
- The color-profile fallback above.

## Explicitly deferred (from the request, not built this pass)

Syntax highlighting, LaTeX, image paste / vision, drag-and-drop,
models picker, session history sidebar/browser, plan mode, todos,
reasoning display, retry/handoff UI, multi-select questions,
@-mention fuzzy file picker (inline expansion only), MCP debug view,
streaming markdown beyond fence-boxing, kitty keyboard protocol
(would enable true shift+enter).

## Next candidates

Markdown streaming polish (glamour or hand-rolled), syntax
highlighting (chroma), models picker over models.json, session
browser over ~/.tilde/sessions, kitty protocol for shift+enter.

---

# Phase 8 — Markdown, Models Picker, Session Browser (status: complete, live-verified)

## Built

- **Assistant markdown** (`internal/tui/markdown.go`): glamour v1 with
  a brand-themed StyleConfig — green headings, electric-green bullets,
  accent-colored links and inline code, and a custom chroma registry
  entry (not a stock theme) so syntax tokens carry the palette: keywords
  electric green, strings sky, numbers amber, comments the dim
  green-gray. Renderers are cached per wrap width; the rendered lines
  cache on the transcript entry itself, so View's per-frame pass never
  re-runs glamour. The in-flight stream keeps the cheap plain renderer;
  the flushed entry upgrades in place. Any glamour failure falls back to
  the plain renderer — a styling problem never costs content.
- **Overlay picker** (`internal/tui/picker.go`): the shared interaction
  behind /model and /sessions — type to filter (case-insensitive over
  label + detail), arrows or ctrl+n/p to move, Enter to select, Esc to
  close, list windowed to 12 rows around the selection.
- **/model picker**: lists every provider and model from models.json
  (the active one marked), or `/model <name>` switches directly when the
  name is configured. Selecting runs the injected SwitchModel callback,
  which (in cmd/tilde) resolves the new provider's key exactly as startup
  does, rebuilds the OpenAI-compatible client, re-points the
  orchestrator AND the subagent runner, and swaps the audit redactor for
  the new key. Refused mid-turn (finish or interrupt first).
- **/sessions browser**: lists ~/.tilde/sessions newest-first with
  message count, model, and a preview of the first user message;
  corrupt files are skipped, not fatal. Selecting saves the current
  conversation, re-seeds the orchestrator from the chosen session, and
  resets the transcript (usage counter included) with a resume note.
- **Push repair**: the earlier "pushed" commits had silently failed —
  git's credential helper pointed at /usr/bin/gh, absent in this
  sandbox. Re-pushed the six pending commits with a gh-based helper.

## Verified for real

- `go test -count=1 ./...` — all eleven packages, including new tests:
  picker open/filter/select/cancel (select provably calls SwitchModel
  with the right pair; Esc provably does not), /sessions against a real
  saved session file (current conversation saved first, resume callback
  receives the right path, preview text present), markdown render
  content (heading, emphasis, link, code fence survive render), and the
  per-entry render cache (same slice reused; a width change re-renders).
- **PTY against the local scripted server**: markdown turn rendered
  with ANSI256 brand colors (H1 green, bullets, code panel); the /model
  picker filtered "second" to one match, Enter switched, and the NEXT
  request's body provably carried second-model (the server answers with
  the request's model name); /sessions listed two real saved sessions
  newest-first and resuming reset the transcript with the
  "resumed … 2 messages in context" note.
- **Live OpenRouter** (`inclusionai/ling-3.0-flash-fin:free`): asked
  the real model for a bullet list plus a tiny go code block; the
  response rendered as styled markdown — bullet, code panel, no raw
  fence markers left.

## Phase 8 assumptions

1. Model switching is refused mid-turn rather than racing an in-flight
   request; it takes effect on the next request.
2. A provider with no `models` list appears in the picker as a
   provider-only entry that switches the provider and keeps the current
   model (there is nothing better to do with no list).
3. Resuming saves the current conversation first, but as its own new
   session file (tree storage has no delete; a "move into" semantic is
   out of scope).
4. The subagent runner follows the parent's provider/model switch —
   there is deliberately no independent subagent model picker yet.

## Still deferred (updated list)

LaTeX, image paste / vision, drag-and-drop, plan mode, todos, reasoning
display, retry/handoff UI, multi-select questions, @-mention fuzzy file
picker, MCP debug view, kitty keyboard protocol (true shift+enter),
persistent left-column session sidebar.
