# Progress log — the full build history

> **Renamed 2026-10-02:** the project was **tilde** through v0.4.0 and is
> now **opcode** from v0.5.0. Entries before the rename say "tilde" because that is
> what it was called then; only the name changed — the architecture,
> codebase, and history are the same. `~/.opcode` replaced `~/.tilde`,
> `OPCODE_*` replaced `TILDE_*`, and the repo moved from
> Chmgx81/tilde to Chmgx81/opcode (old links redirect).

# Build log (archived)

This is the full chronological build log, Phase 0 through Phase 49,
moved here verbatim from `PROGRESS.md`. It is the record of what each
phase set out to do, what was verified live, and what was left
unverified or deliberately deferred.

It is append-only history: where a section disagrees with the code,
the code is right and the section is history. Read
[../PROGRESS.md](../PROGRESS.md) for the current state, and
[specs/README.md](specs/README.md) for the spec index.

---

# PROGRESS.md — opcode build log

Phase 0 status: **complete and verified as far as possible without a real
OpenRouter key.** Stop point for user review.

## What Phase 0 built

| Package | What it does |
|---|---|
| `cmd/opcode` | Bare terminal input/output loop. Thin: wiring only, all logic lives below. No TUI framework. |
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
  startup warning when a project-level `.opcode/auth.json` exists (and
  that its key was ignored).
- **Live OpenRouter run (2026-09-28, model `inclusionai/ling-3.0-flash-fin:free`)**:
  the same binary against the real `https://openrouter.ai/api/v1` with the
  user's key in `~/.opcode/auth.json`. Two turns exercised all four
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
  `model` in `~/.opcode/config.json`, put a key in `~/.opcode/auth.json` (or
  `OPENROUTER_API_KEY`), and run `opcode` in a test directory.

## Phase 0 assumptions (spec didn't pin these down)

1. `models.json` "optional key reference" = an `api_key_env` field naming
   the provider's credential environment variable (default
   `OPENROUTER_API_KEY` for the built-in openrouter entry). Empty means
   "needs no key" (local servers).
2. `auth.json` is a flat map: `{"openrouter": "<key-or-!command>"}`.
3. If `config.json` has no `model`, opcode refuses to start with
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
- Audit log lives at `~/.opcode/audit.jsonl`; rotation can come later.
- No session persistence, compaction, project trust, skills, MCP,
  subagents, streaming cancel — all per the build order (spec §8).

## Next (Phase 1, only after user review)

- Real Bubble Tea TUI: streaming render, status bar, permission prompts,
  steer/follow-up, `/login` and `/logout`.

---

# Phase 1 — Real Bubble Tea TUI (status: complete, live-verified)

Spec: [docs/specs/phase1-tui.md](specs/phase1-tui.md)

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

Spec: [docs/specs/phase2-modes.md](specs/phase2-modes.md)

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

Spec: [docs/specs/phase3-skills-trust.md](specs/phase3-skills-trust.md)

## Built

- `internal/trust` — the Section 7 trust gate: executable-surface
  fingerprint (skill scripts, `.opcode/config.json`, `.opcode/mcp.json`),
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
- `cmd/opcode` — `--trust` / `OPCODE_TRUST=1` pre-approval (CI posture).

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

- MCP Manager — project `.opcode/mcp.json` only after trust (the
  fingerprint already covers it).

---

# Phase 4 — MCP Manager (status: complete, live-verified)

Spec: [docs/specs/phase4-mcp.md](specs/phase4-mcp.md)

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

Spec: [docs/specs/phase5-subagents.md](specs/phase5-subagents.md)

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

Spec: [docs/specs/phase6-polish.md](specs/phase6-polish.md)

## Built

- `internal/headless` — `opcode -p "prompt"` one-shot mode, plain text
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
  ~/.opcode/sessions/, 0600, credentials redacted on write; saved on
  exit in every mode; `--resume` / `--continue` seed the orchestrator.
- Compaction (Section 3.2) — opt-in via `context_window`
  (0 = disabled; model windows vary and a wrong default would silently
  rewrite history), 75% threshold on the provider-reported prompt
  size, summarizer round through the same Provider interface
  (optional cheaper `compaction_model`), failure skips compaction and
  the turn continues.
- Packaging — module path is now `github.com/Chmgx81/opcode` so
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

- **Design tokens** (`internal/tui/style.go`): opcode's own identity —
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
browser over ~/.opcode/sessions, kitty protocol for shift+enter.

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
  which (in cmd/opcode) resolves the new provider's key exactly as startup
  does, rebuilds the OpenAI-compatible client, re-points the
  orchestrator AND the subagent runner, and swaps the audit redactor for
  the new key. Refused mid-turn (finish or interrupt first).
- **/sessions browser**: lists ~/.opcode/sessions newest-first with
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

## Found and fixed during verification

- **Composer wrapped at 40 columns on any terminal** (user-reported
  from a live run): bubbles' textarea defaults its internal wrap width
  to 40 and never saw the terminal size, so the placeholder spilled
  onto two lines and long typed lines would have wrapped at 40 too.
  The composer now sizes itself from WindowSizeMsg (terminal minus box
  chrome minus prompt), and the placeholder was shortened to fit a
  60-column terminal. Regression test pins both: composer width after
  a resize event, and the placeholder unwrapped at width 70.
- **Composer restructured to the Claude Code reference shape**
  (user-reported, second pass): the placeholder had become a keymap
  ("/ commands · ! shell · @ files · ? help") crammed into the input,
  and the fixed 3-line textarea showed two empty prompt rows below it.
  Now: a one-line composer with a real placeholder ("ask opcode
  anything…") that grows with content (one row per typed or wrapped
  line, capped at 6) and shrinks on submit; the hints moved to the dim
  footer line under the box ("? help · / commands · ! shell · @
  files"), the shape of the reference apps' "? for shortcuts" footer.
  fit() now reserves the composer's live height instead of a constant.
- **Placeholder brightness + query/answer hierarchy** (user-reported,
  third pass, both from the Claude Code reference): the placeholder
  rendered at the textarea's default gray-bright, brighter than a hint
  should read; and user queries and assistant output rendered at the
  same weight, so a conversation read flat. The placeholder now uses
  the Dim token (FocusedStyle.Placeholder and BlurredStyle.Placeholder
  — the textarea keeps two style states), and the echoed user query
  renders dim behind the accent prompt while the agent's markdown stays
  bright: what the user said recedes, what the agent answered leads —
  the reference apps' exact hierarchy. The identity title moved off the
  user-entry kind (it is not a query and must not dim). Regression test
  asserts the placeholder line and the user line carry the dim SGR and
  the assistant line does not; PTY run confirmed the same codes live
  (59 for query text, 194/41 for the answer, 59 for the placeholder).
- **Fourth pass, corrected against the actual reference screenshots**:
  two reversals of the third pass. (1) The "highlight" on the
  placeholder was not its foreground at all — it was bubbles' default
  focused CursorLine painting a black background rectangle over the
  whole line, visible on any terminal whose floor isn't pure #000000,
  plus a bright-white stock prompt (color 7). Both focus states now use
  a clean CursorLine, the accent prompt, and the Dim placeholder — the
  rectangle is gone entirely. (2) The dim user query was wrong: the
  reference (Screenshot From 2026-09-28 08-11-31) renders the user
  message at full weight, no dim. Queries are bright again behind the
  accent prompt, matching the reference; agent output stays bright.
  TestTranscriptHierarchy pins all three: dim placeholder with no
  background SGR, accent+bright query, no dim on the answer.

- **Esc cancel audit** (user-reported: Esc did nothing during /login):
  went through every modal state. Trust prompt, permission prompt,
  help overlay, and the pickers already honored Esc; two did not. The
  /login input now cancels on Esc — the typed key is discarded,
  nothing is written to auth.json, and a "login cancelled" note lands
  in the transcript (the Enter handler moved up to its own early block
  in handleKey, since the main switch never saw Esc in that state).
  The command palette now also closes on Esc instead of leaving a
  half-typed command in the composer; a plain draft with no modal open
  is untouched. Both paths regression-tested and PTY-verified
  live: /login + typed key + Esc left no auth.json, /mo + Esc cleared
  the composer.
- **Full control sweep** (the "are all controls accurate" audit): a
  PTY session against a deliberately slow SSE fixture drove the whole
  key map through one live session — send, alt+enter queue while
  working, Esc interrupt, shift+tab mode cycle, ctrl+r expand, ?
  overlay open and close on any key, ! shell escape, /exit — all
  verified against the rendered frames. The sweep caught one real
  bug: interrupting a turn auto-fired the queued follow-ups the
  moment Esc landed, because turnEnded drained the queue
  unconditionally. Esc now clears the queue with a
  "cleared N queued follow-ups" note; a clean completion still drains
  it (both pinned by TestInterruptClearsQueue). Enter/steer, ctrl+j
  newline, palette arrows, picker navigation, and @ attachment were
  already covered by unit tests this session plus the earlier live
  runs.

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

---

# Phase 9 — Packaging, Distribution, Public (status: complete, live-verified)

## Built

- **README rewrite**: banner-led, badge row (go version, release,
  license, ci), feature list, install (binaries + go install + source),
  quick start, keys/commands/modes tables, skills/MCP/subagent/
  headless sections, layout tree, docs pointers. Written for a reader
  who has never seen the project.
- **LICENSE** — MIT (the user can swap it; a public repo ships with
  one).
- **CI** (`.github/workflows/ci.yml`) — go vet + go test -count=1 +
  build on every push/PR to main.
- **Release pipeline** (`.github/workflows/release.yml`) — on `v*`
  tags: cross-compiles linux/darwin (amd64+arm64) and windows/amd64,
  CGO off, trimpath, stripped; tar.gz per platform (zip for windows);
  publishes a GitHub Release with generated notes.
- **History purge before going public** (user-authorized force-push
  to main): filter-branch removed `references/` (17 MB of third-party
  product screenshots — a copyright and clone-weight problem in a
  public history) and the stray `internal/subagent/x.txt`; reflog
  expired and the repo garbage-collected. The local reference files
  stay on disk, ignored, for design work.

## Verified for real

- Full suite green after every change (11 packages).
- **Secrets scan of all history before publicizing**: only env-var
  NAMES in tests, no key material, across every commit.
- **History purge**: filter-branch + refs/original cleanup + gc took
  .git from 17 MB to 356 KB; the largest remaining blobs are
  PROGRESS.md revisions. Force-pushed main (authorized).
- **CI**: green on a clean GitHub runner — vet + the full test suite
  + build.
- **Release pipeline, end to end**: tagged v0.2.0; the first run
  FAILED on the Windows build (Setpgid/syscall.Kill are POSIX-only) —
  found live, fixed with build-tagged procsys_unix.go /
  procsys_windows.go, all five targets re-verified cross-compiling
  locally; tag re-pointed, second run green. The release carries all
  five binaries: darwin amd64/arm64, linux amd64/arm64, windows amd64.
- **go install from the public module**: `go install
  github.com/Chmgx81/opcode/cmd/opcode@latest` in a clean GOPATH
  resolved v0.2.0 through the Go proxy (checksummed), built, and the
  binary runs (verified: the honest "no model configured" startup
  error).

## Phase 9 assumptions

1. MIT is the license; the copyright line reads "Chmgx81". Swap it
   any time before wide distribution.
2. `references/` stays local-only (gitignored), not re-committed.
3. Version pinned at v0.2 (TUI version string) — tagged v0.2.0.

## Phase 9 addendum — install script + --version

- **install.sh** (Claude-Code-style `curl | bash`): detects os/arch,
  resolves the latest tag from the releases/latest redirect (no API
  dependency), downloads the platform archive, installs to
  ~/.local/bin (overridable), prints the version/location/next block,
  and warns when the install dir is not on PATH. Unknown platforms are
  pointed at go install instead of failing obscurely.
- **`opcode --version`**: linked version from the release pipeline
  (-X main.version), then the module version go install recorded,
  then (devel). The release workflow now injects the tag.
- **Verified live**: ran install.sh in a clean temp dir — resolved
  v0.2.0, downloaded the real release asset, extracted, installed,
  printed the success block, and the installed binary ran. The curl
  one-liner against raw.githubusercontent is verified in the trail
  after the commit that adds install.sh.

  Verified live, second pass with v0.2.1: the one-liner against
  raw.githubusercontent resolved v0.2.1, downloaded, installed, and
  the installed release binary printed "opcode v0.2.1" — the
  -X main.version injection works in the real pipeline. Release
  v0.2.1 carries all five binaries.

---

# Phase 10 — Conversation Rhythm, Tab Modes, Diff Highlighting (status: complete, live-verified)

## Built

- **Conversation spacing**: every user query and every finished answer
  opens with a blank line, and collapseBlanks (ANSI-aware, since
  glamour pads lines with styled spaces) keeps it to exactly one gap —
  blocks breathe; nothing doubles up against the greeting or glamour's
  own margins.
- **Three modes**: auto-accept-safe-ops is gone from the real set (it
  was indistinguishable from ask-every-time in the gate — drafts
  auto-run in every mode). Modes are read-only / ask-every-time /
  full-auto; the old name survives as a legacy config alias that
  normalizes to ask-every-time, always toward the restrictive
  direction, and the config error message names the three real modes.
- **Tab cycles modes** forward, shift+tab backward, across the three.
  Mode switches announce with an **animated toast**: a diamond glyph
  burst (◇ ◈ ◆ ◈ ◇, 90ms frames) driven by a toast tick, then settles
  to the normal toast.
- **Syntax-highlighted diffs**: edit_file hunks now token-color the
  source (chroma, lexer matched from the file path, memoized; token
  colors mirror the markdown registry) with − / + markers carrying
  the verdict — caught live that the old line-count heuristic reported
  +0 −0 for a same-line-count replacement; diffCounts now counts
  replaced lines as one − and one + plus the growth tail.
- **Accessibility / ease of use**: NO_COLOR is now honored (the
  termenv fallback used to resurrect color over it), opcode has a real
  --help (usage, flags, and a first-run pointer), and --version
  already existed from Phase 9.
- **README rewritten**: the banner was broken HTML (pre inside p —
  GitHub mangles it); now a plain code block. The whole README trimmed
  to ~100 lines — hero, one-liner install, quick start, the three
  modes, one keys table, a compact beyond-the-loop section.

## Verified for real

- All eleven packages; new tests: conversation spacing (blank before
  each block, never doubled), the three-mode cycle (tab forward,
  shift+tab back, gate follows into read-only denial), legacy alias
  normalization, animated toast frame consumption, diff highlighting
  (accent SGR on a keyword inside a removed line, content intact),
  and the corrected +/− counts.
- **PTY, live against a scripted server doing a real write_file +
  edit_file round**: blank lines between query/answer/tool blocks;
  the hunk rendered token-colored live (− red, func bold accent, main
  accent, braces dim); three tabs cycled full-auto → read-only →
  ask-every-time with all three toast glyph frames observed in the
  log; the edited file on disk really contained the change.
- --help output, --version, and NO_COLOR (zero SGR codes in a PTY
  run) all executed and checked.

## Phase 10 assumptions

1. Legacy configs naming auto-accept-safe-ops silently become
   ask-every-time — restrictive direction; the config error names the
   real modes for everything else.
2. Tab is free (the composer does not use it); it cycles modes. This
   matches the reference apps' muscle memory.
3. The diff highlighter degrades to plain lines for unknown languages
   and on tokenise failure — content is never at risk.

---

# Phase 11 — Toasts Only, Ice-Cyan Palette, Repo Identity (status: complete, live-verified)

## Built

- **Mode switches no longer write transcript lines** (user-reported:
  tab-mashing stacked a "✓ mode switched" line per keypress). The
  animated toast announces, the mode line under the composer persists,
  the transcript stays a conversation. The screenshot's phantom
  "duplicate" line was the toast rendering with the same ✓ glyph as
  transcript entries — visually indistinguishable; removing the
  entries fixed both the spam and the ambiguity.
- **Logic audit of the switch path found one real bug**: Tab during
  the /login prompt cycled permission modes while the user typed a
  secret. Tab/shift+tab are now swallowed in the login flow. The rest
  checked clean: pickers swallow Tab (filter keys), the allow-all
  session grant resets on every switch, rebuildGate re-wires the
  prompt closure, and an unknown current mode fails into ask.
- **Ice-cyan palette** (user asked to replace the greens): accent
  #22D3EE (electric cyan), secondary #0891B2, panels #0B3A47 /
  #06222B, floor unchanged, dim now a cool gray #7A8B94, notices
  moved to blue #93C5FD so they no longer collide with a cyan accent.
  The hexes are now named constants in style.go and markdown.go pulls
  from them — the whole product (UI, markdown, diffs) reskins from
  one place, which this change exercised end to end.
- **Repo identity**: About description and ten topics set via gh
  (coding-agent, terminal, go, llm, ai-agent, tui, bubbletea, cli,
  mcp, openai).

## Verified for real

- All eleven packages; new assertions: mode switches add nothing to
  the transcript, and the tab-during-login swallow.
- PTY: five rapid tabs produced exactly one transient toast (its
  animation frame and settled frame) and zero persistent lines —
  versus one permanent line per press before; the mode line landed on
  read-only and the toast expired.
- The cyan accent renders live (ANSI256 on xterm-256color); color
  assertions in the hierarchy/diff tests updated to the new tokens
  and pass under forced truecolor.

## Phase 11 assumptions

1. The ice-cyan direction is a recommendation, deliberately not the
   green it replaces and not Claude's coral; one file (style.go)
   reskins everything if the user wants another family.
2. Toast lifetime stays 4 seconds; mode switches are still visible in
   the working-status context because the mode line is always on.

---

# Phase 12 — Affordances and the Demo Feel (status: complete, live-verified)

## Built

- **Watched the demo.gif** (frames extracted with PIL, read through the
  vision model against the live OpenRouter key) and took the concrete
  details, not vibes: the gerund working line ("Grooving… (esc to
  interrupt · 8s · ↓ 555 tokens)"), the echoed query in a subtle
  background panel, the hairline-divided input strip, minimal hints.
- **@-mention file picker** (`internal/tui/mention.go`): typing "@" now
  OPENS a visible live-filtered list of project files (the affordance
  that was missing entirely — the mention only ever expanded on
  submit). Type to filter, arrows to move, Enter inserts "@path ",
  Esc dismisses that mention until a fresh "@" appears. The project
  walk caches once per session (1000 files, depth 6, .git and friends
  skipped, shallow-first order).
- **! amber indication**: a leading "!" turns the composer's border and
  prompt amber and swaps the mode line for "shell — enter runs it
  directly" BEFORE Enter, not after.
- **Working line, demo-shaped**: spinner + a per-turn gerund
  ("Thinking…", "Pondering…", "Marinating…", …) + "(esc to interrupt ·
  elapsed · ↓ N tokens)". The steer/queue hints moved to the help
  overlay; the line is a feeling, not a legend.
- **User entries render in a background panel** (the deep fill), the
  demo's separation of what you said from what the agent answered.
- **Mode line reshaped** to the reference: "~ mode (tab to cycle) ·
  ? help · / commands".

## Found and fixed during verification

- refreshAtMenu never returned early when there was no trailing
  mention — an empty query matched every file, so the menu tried to
  open on ANY typing, which cascaded into the login and full-session
  tests. The dismissal sentinel also collided with a bare "@"
  (empty query). Both fixed by anchoring state to the byte position of
  the "@" and early-returning when no mention is trailing.
- Two PTY "failures" that were script artifacts, not bugs: the ! test
  appended to a half-composed mention (so it submitted as a turn), and
  the probed "hello" was a steering entry (panels are for submitted
  queries by design). Re-run clean.

## Verified for real

- All eleven packages; new tests: the mention lifecycle (open on bare
  @, filter, complete, Esc-dismissal semantics per position), the
  amber shell indication (prompt glyph + mode-line hint + restore on
  backspace), the gerund working line, the user panel SGR.
- PTY live: "@" opened the picker listing real files, filtered to one,
  Enter completed it into the composer and the message really attached
  the file contents; "!" rendered the amber border and prompt with the
  shell hint while typing, and Enter ran echo for real; the working
  line showed "Thinking… (esc to interrupt · 0s · ↓ 0 tokens)"-shaped
  status; submitted queries rendered with the panel background
  (48;5;232 on xterm-256color).

---

# Phase 13 — Plan Mode (status: complete, live-verified)

Spec: [docs/specs/phase13-plan-mode.md](specs/phase13-plan-mode.md)

## Built

- **The mode**: `plan` joins the cycle — read-only → plan →
  ask-every-time → full-auto (Tab forward, shift+tab back). Like
  read-only, action-tier tools are not advertised; unlike read-only,
  Draft-Only tools ARE — which finally gives the long-empty Draft-Only
  tier its tool.
- **The tool**: `present_plan` (Draft-Only, always allowed — proposing
  changes nothing). Args: the plan as markdown (goal, steps, risks).
  Execute blocks on the injected Approve callback — the permission
  gate's pattern — and the result text tells the model the verdict:
  approved + full-auto, approved (actions will ask), or declined. Nil
  Approve (headless) fails closed: "user cannot be reached", nothing
  proceeds.
- **The approval flow**: a plan prompt like the permission prompt.
  `y` implement — switches plan/read-only into ask-every-time; `a`
  implement with auto-accept — full-auto; `n`/Esc keep planning. Mode
  switching mid-turn is the mechanism: the next model request
  re-composes the tool list, so approval immediately widens what the
  model can do — no restart, no new session.
- **Transcript**: the plan renders as a labeled markdown block
  (entryPlan, same renderer as answers); the decision line follows
  ("plan approved — implementing, actions will ask" / "plan declined").
- **System prompt**: plan mode instructs research-first, exactly one
  plan via present_plan, stop and wait.
- **Fix found while wiring**: the orchestrator had its own inline
  read-only tool filter instead of calling the policy — plan mode's
  advertisement would have been ignored. toolDefs now routes through
  ModeAllowsTier (the tier-level form of the policy).

## Verified for real

- All eleven packages, new tests: the tool (tier, verdict texts,
  fail-closed, arg validation), the policy matrix rows for plan mode,
  orchestrator advertisement (read_file + present_plan offered;
  write/edit/shell absent; instruction composed), the TUI plan
  lifecycle (y/a/n/Esc verdicts, mode follow-through, transcript
  entries), and the four-mode tab ring.
- **PTY, end-to-end with the decisive proof**: the fixture answers
  based on what the request ADVERTISES — no write_file → present_plan;
  write_file present → "implementing". Plan mode turn: the plan
  rendered, the prompt appeared, `y` was pressed, the mode line
  flipped to ask-every-time, and the NEXT request provably carried
  write_file (the implementation text streamed only because the tool
  was offered). Headless plan mode fails closed by construction.

## Phase 13 assumptions

1. `a` (auto-accept) maps to full-auto — opcode has no
  "auto-accept-edits-only" mode; the mapping is stated in the prompt
  text, not hidden.
2. The plan lives in the conversation and transcript; persisting a
  plan.md is deferred.
3. present_plan offered in every mode (drafts always are); outside
  plan mode the model is simply not instructed to use it.

---

# Phase 14 — Safe Shell Commands (execpolicy-lite) (status: complete, live-verified)

## Built

- **`safe_commands`** (config.json): shell command prefixes that run
  without prompting in ask mode — the biggest usability gap vs. both
  reference products ("read-only has no shell at all; ask prompts for
  `ls`"). Borrowed from Codex's execpolicy at opcode's scale: a flat
  token-prefix list instead of a Starlark rule engine.
- **Matching is token-wise and fails closed** (`tools.ShellAllowlist`):
  quote-aware tokenization ("git status" matches "git status --short"
  but not "git push", case-sensitive), any shell metacharacter in any
  token denies (`; | & $ \` < > ( )`) — found live in test review:
  "git status $(whoami)" would have auto-run with command substitution
  in it; the separator form "git status ; rm -rf /" matched the prefix
  and needed the metachar check to catch it. Unterminated quotes
  tokenize to nothing and deny. A whole-word quote is a DIFFERENT
  command and correctly does not match.
- **The mode's posture dominates**: ShellPolicyDecide checks
  read-only/plan FIRST — allowlisted commands still deny there; the
  allowlist widens nothing, it only narrows the prompt set in the
  working modes. Nil allowlist behaves exactly like PolicyDecide.
  Every gate rebuild (mode switch, /login, /logout) composes the same
  allowlist, so the policy survives mode cycling. The gate still logs
  allowlisted runs.

## Verified for real

- All eleven packages; new tests: the matching matrix (prefix, longer
  command, same-token-different-arg, case, quoting, metachar
  smuggling, substitution, pipe, redirection, whole-word quote),
  fail-closed cases (unterminated quote, nil allowlist, all-invalid
  input), and the composed policy (safe auto-allows without consulting
  the prompt — asserted via promptCalled; risky prompts and honors
  denial; read-only/plan deny even safe; nil = PolicyDecide).
- **PTY, ask mode with safe_commands ["echo"]**: the model issued two
  run_shell calls; `echo safe-ok` ran with zero permission prompts;
  `touch risky.txt` prompted, was denied with n, and the file was never
  created; the turn completed.

## Phase 14 assumptions

1. Prefix semantics over Codex's full rule language (no per-command
   rationale fields, no Starlark) — a flat list is auditable and
   matches opcode's config style.
2. Metacharacter conservatism: a quoted metachar also denies —
   untolerable ambiguity beats convenience.
3. The allowlist applies to subagents too (shared gate), which is
   intended: same trust boundary.

---

# Phase 15 — Streaming Markdown, Reduced Motion, Terminal Title (status: complete, live-verified)

Borrowed from the Codex TUI audit (phase 14's comparison), in priority
order — items 1, 2, and half of 4 of the borrow list.

## Built

- **Streaming markdown (the flush "pop" is gone)**: the in-flight
  stream renders through the same glamour renderer as finished
  entries, cached on the Model and invalidated by content length or
  width — so View's per-frame pass costs nothing, and the flushed
  entry is byte-identical to the last streamed frame (finishStream now
  normalizes with the same TrimSpace the stream view uses, and resets
  the cache). Codex's newline-gated markdown_stream taken to its
  conclusion. renderAssistant and its fence-box renderer are deleted —
  the plain fallback for a glamour failure is now wrapAll.
- **Reduced motion** (config.json `"animations": false`): Codex's
  MotionMode at opcode's scale. The spinner becomes a static ●, the
  mode toast appears without the glyph burst and schedules no ticks.
  Information is preserved; only motion is removed. Their
  screen-reader probe that seeds and persists this default is noted as
  a future item.
- **Terminal title** (tea.SetWindowTitle "opcode — <cwd>"): OSC 2 in
  Init, like the reference apps' window titles.

## Verified for real

- All eleven packages; new tests: the no-pop guarantee (flushed render
  byte-identical to the streamed render, asserted with %q), the stream
  cache populating on render and invalidating on width change, and
  reduced motion (no tick cmd, toastAnim 0, toast still set, zero
  spinner frames on the working line).
- **PTY**: terminal title escape observed live (OSC 2 "opcode — /tmp/
  feelproj"); streamed text visible mid-turn against the slow fixture
  (rendered as it arrives); a second session with animations:false
  showed the static working line `● Thinking… (esc to interrupt · 0s
  · ↓ 0 tokens)` with no spinner frames and no toast burst while the
  toast still communicated the mode switch.

## Deferred (from the audit's borrow list)

3. Theme-adaptive fills (probe the terminal background, alpha-blend) —
   next pass. Then: footer-hint width fitting, external editor, screen
   reader detection seeding the animations default.

---

# Phase 16 — Theme-Adaptive Palette (status: complete, live-verified)

Borrow list item 3 from the Codex audit: their TUI probes the
terminal's actual background and adapts — light text on a white
terminal is invisible, and opcode's hardcoded dark palette had exactly
that failure class.

## Built

- **The palette became mutable**: style.go's Hex values are vars now,
  and refreshTokens() is the one place that re-derives every Color
  and Style from them (populated at init and after a swap). This is
  the reskin mechanism the "one place to change the look" comment
  always claimed, finally exercised.
- **adaptTheme(dark)**: dark is the default posture (and what
  undetectable terminals fall back to). Light swaps in: dark ink
  (#1B2A32) instead of near-white text, light fills (#E4EDF1) for the
  user panel and code blocks, light panel borders (#A8C4CE), and
  deepened accents/semantics for contrast on white (#0E7490 accent,
  darker danger/warning/info).
- **Detection in Run()**: termenv.HasDarkBackground() (the same OSC
  exchange as the profile probe), skipped under Ascii/NO_COLOR;
  OPCODE_THEME=light|dark overrides the probe. Glamour renderers embed
  their style config at creation, so adaptTheme drops the renderer
  cache — post-adapt renders pick up the swapped palette.

## Verified for real

- All eleven packages; new test: the light palette applied (hex
  assertions), the user panel rendering with the light fill under
  forced truecolor, and the deepened accent — with an explicit dark
  restore (adaptTheme's dark branch is a deliberate no-op).
- **PTY with OPCODE_THEME=light on xterm-256color**: the user panel
  rendered with the light fill (48;5;195 = #E4EDF1) and a dark accent
  prompt on it; the dark fill was provably absent; light borders on
  the boxes; the query echoed normally.

## Deferred (borrow list)

Footer-hint width fitting, external editor, screen-reader detection
seeding the animations default, OSC 8 hyperlinks.

---

# Phase 17 — Footer Fitting, External Editor, Screen-Reader Seeding (status: complete, live-verified)

Borrow list items 4 and the accessibility seed, from the Codex audit.

## Built

- **Footer fitting** (Codex's footer_hint.rs pattern): the mode line
  has candidates from fullest (mode + (tab to cycle) + hints) to bare
  mode; the first that fits the terminal wins. Hints degrade; the
  mode never does — a shortcut never separates from its label.
- **External editor** (ctrl+e): the composer's text lands in a temp
  file, $VISUAL/$EDITOR runs with the TUI suspended (tea.ExecProcess),
  and the result loads back (composer grows, mentions re-resolve,
  shell prompt re-syncs, temp file removed). No editor set → toast, no
  process. Deliberate no-ops: during a turn (suspending mid-turn
  strands the orchestrator) and in modal states (they own the
  keyboard). The editor's edit wins even on nonzero exit — half the
  editors in the wild exit nonzero — but an unreadable file leaves the
  composer untouched.
- **Screen-reader seeding** (Codex's probe, opcode-sized):
  config.ScreenReaderActive() checks the conventional signals
  (SCREEN_READER, atk-bridge in GTK_MODULES, ACCESSIBILITY_ENABLED) —
  deliberately conservative, because a false positive removes
  animation someone may want. When the user has not chosen
  explicitly, a detected reader turns animations off for the session
  with a visible startup note. Nothing is persisted silently (Codex
  writes the preference; opcode lets the config key win).

## Verified for real

- All eleven packages; new tests: the footer degradation ladder (full
  hints at 80 cols, ≤ terminal width with the mode intact at 24), the
  editor flow (no-editor toast, result lands in the composer, temp
  file removed, composer grows to fit, mid-turn no-op), and the
  screen-reader signal matrix.
- **PTY, live**: ctrl+e with a fake editor script — the draft went
  out, the editor appended a line, the composer loaded the edited
  text, and the toast confirmed; a 34-col terminal rendered the bare
  `~ ask-every-time` footer with the full hints provably absent; a
  SCREEN_READER=1 session showed the startup note and zero spinner
  frames.

## Deferred (borrow list)

OSC 8 hyperlinks (glamour emits no link anchors to post-process — a
fragile hack, not built on purpose). The borrow list is otherwise
exhausted.

---

# Phase 18 — Todos (status: complete, live-verified)

Spec: [docs/specs/phase18-todos.md](specs/phase18-todos.md)

## Built

- **`todo_write`** (Draft-Only — updating a plan changes nothing):
  full-replacement semantics; the model sends the complete list every
  update, so there is nothing to merge, no IDs, no stale state.
  Items: content + pending/in_progress/done. Validation: empty lists
  and empty contents error, unknown statuses error, and a 50-item
  bound rejects a list that stopped being a plan. The description
  carries the contract (3+ steps, one in_progress, full list) — no
  system-prompt bloat.
- **Shared state with an observer**: tools.TodoList (mutex'd) owned by
  main; the TUI sets OnChange to a tea.Msg sender (bubbletea only
  paints on messages, so the notify must Send); headless leaves it
  nil. OnChange receives a copy, never the shared slice — pinned by
  a test that mutates the notification and asserts state integrity.
- **The live panel**: rendered at the transcript tail whenever the
  list is non-empty — state, not history, like the mode line. Header
  `● tasks (1/2 done)`; items `✓ done`, `▸ in progress`, `· pending`
  (a new glyph joins the vocabulary); windowed to the live tail with
  an "… N earlier tasks" note. The tool result reports the summary so
  the model sees what the user sees.

## Verified for real

- All eleven packages; new tests: tool validation matrix and the
  copy-not-shared-slice guarantee; panel rendering (all three glyphs,
  counts, windowing cap).
- **PTY, live**: a scripted model called todo_write twice mid-turn —
  the panel showed `▸` on the live item after the first call and
  `✓/▸` with updated counts after the second, the tool calls sat in
  the timeline, and the turn completed.

## Phase 18 assumptions

1. The list is session state, not persisted — sessions store
   conversation, and a stale cross-session list would lie.
2. Full-replacement over incremental updates: the model is the only
   writer, and Claude Code's contract is proven at scale.
3. The tail window keeps the in-progress item visible; a 50-item list
   is rejected rather than rendered.

# Phase 19 — Reasoning display + write-as-diff (status: complete, live-verified)

Spec: [docs/specs/phase19-reasoning-diff.md](specs/phase19-reasoning-diff.md)

## Built

- **Reasoning on the wire**: the SSE delta struct carries both field
  conventions in the wild — OpenRouter's `reasoning` and DeepSeek's
  `reasoning_content` — and emits a `ReasoningEvent` before content.
  A test pins both conventions against recorded delta JSON.
- **Forwarded, not stored**: the orchestrator hands reasoning events
  to the UI but keeps them out of history — thinking is for the UI;
  the answer is what the conversation keeps. Resumed sessions show
  answers, not thoughts.
- **The render**: live, a `△ thinking…` head with a dim italic tail
  windowed to the last 3 lines; at boundaries (first text, tool call,
  completion, error) it collapses to one entry — `△ thought for Ns ·
  M chars (ctrl+r to expand)` — and the existing ctrl+r toggle
  expands it, windowed to 12 rows. One expand mechanism for results
  and thinking, not two.
- **write_file as a diff**: result entries parse the tool's
  `{path, content}` args; collapsed reads `└ wrote path +N`,
  expanded renders `+ `-prefixed lines with the same
  `lexerFor(path)` syntax highlighting edit_file already uses,
  windowed to 10 rows. Write and edit now read identically.

## Verified for real

- All eleven packages; new tests: both wire conventions, the render
  lifecycle (5-step tail-windowing, collapse, expand), and the
  write_file diff (collapsed count, `+` lines, window cap).
- **PTY, live**: a scripted SSE fixture streamed reasoning, content,
  a write_file call, and a final answer — the live frame showed
  `△ thinking…` with the tail, the turn collapsed to `△ thought for
  0s · 45 chars`, ctrl+r expanded the reasoning lines and the
  `+ func main() {}` write diff (and re-collapsed), and
  `/tmp/thinkproj/main.go` really existed on disk afterward.

## Phase 19 assumptions

1. Reasoning is UI-only state: no provider documents replaying it
   back, and a resumed session showing stale thoughts would lie.
2. write_file diffs against an empty before-state: writes are new or
   whole-file replacements, so every line is an addition.
3. `0s` durations render honestly — fast thoughts are normal.

# Phase 20 — Codex-aligned restyle (status: complete, live-verified)

Spec: [docs/specs/phase20-codex-restyle.md](specs/phase20-codex-restyle.md)

## Built

- **The palette**, studied from Codex's source (`codex-rs/tui/src/style.rs`)
  rather than screenshots: ChatGPT-blue accent `#63A8F8` (their
  `UI_ACCENT`), neutral measured grays for secondary text (`#999999`,
  their 60% foreground blend) and borders, a white-16%-blend fill
  `#292929` for user message blocks, muted amber `#C4A767` for
  warnings (their dark value), and a real success-green token
  (Codex status uses terminal green). Markdown, diffs, and syntax
  highlighting all derive from the same Hex tokens, so the one
  swap restyled everything.
- **Codex's shapes**: user entries get the `› ` bold-dim prefix inside
  the shaded block (their `history_prompt_style`); prompt titles went
  bold-neutral (amber stays on the attention box border); the working
  line is now `Verb (0s • esc to interrupt • ↓ Nk tokens)` (their
  status-indicator parenthesized segment); footer hints read
  `? for shortcuts · / commands` with the key glyphs in accent.
- **Light theme** mirrors Codex's light values: `#1C64C8` accent
  (their `LIGHT_BG_ACCENT_RGB`), `#F2F2F2` fill (their 4% black
  blend), `#8B6214` amber.
- **Kept opcode's own**: the `~` brand glyph, the gerund pool
  (Codex says plain "Working"), tab cycling, ctrl+r. A design-language
  adoption, not a clone — no behavior changed.

## Verified for real

- All eleven packages; the render tests that pin exact SGR codes were
  moved to the new values (moved, not deleted — the placeholder-dim,
  no-background-highlight, panel-fill, light-fill, and
  accent-deepening assertions all still assert).
- **PTY, live**: dark and `OPCODE_THEME=light` sessions against the
  scripted fixture — the user block renders shaded with `›`, the
  working line shows the parenthesized segment, hints show accent
  keys, the write diff shows green `+1`; light shows dark ink on the
  light fill with the deep accent.

## Phase 20 assumptions

1. Termenv quantizes truecolor one step in this environment
   (`#292929` renders as 40;40;40) — the pinned SGRs assert what
   actually renders.
2. ChatGPT blue as accent is "match Codex" done honestly; opcode's
   name and `~` glyph keep it a distinct product.

# Phase 21 — Landlock sandbox (status: complete, live-verified)

Spec: [docs/specs/phase21-landlock.md](specs/phase21-landlock.md)

## Built

- **The ruleset** (`internal/sandbox`, mirrors Codex's landlock.rs):
  handle every fs access right the kernel's ABI knows (probed at
  runtime; REFER ≥ v2, TRUNCATE ≥ v3, IOCTL_DEV ≥ v4), read+execute
  beneath `/`, full access beneath each writable root and the file
  subset on `/dev/null`, then `PR_SET_NO_NEW_PRIVS` and
  `landlock_restrict_self`.
- **The Go constraint, honestly solved**: Landlock confines the
  calling *thread* and Go's runtime has several threads before main —
  so commands run through a self re-exec, `opcode __sandbox
  <writable…> -- cmd`: a fresh single-threaded child applies the
  ruleset and immediately execs, which inherits it process-wide.
  Intercepted at the very top of `main`, before anything spawns.
- **Writable roots**: cwd, temp dir, and dev caches that exist
  (`~/.cache` via XDG, `~/go/pkg/mod`, `~/.cargo/registry`, `~/.npm`,
  `$GOCACHE`/`$GOMODCACHE` when set). PATH bin dirs stay read-only —
  the executable-drop class is exactly what this blocks.
- **Wiring**: `run_shell` and `run_skill_script` build commands
  through the installed runner (nil runner = plain commands, every
  call site unchanged). Default on where the kernel supports it;
  `{"sandbox": false}` opts out; the startup note always states the
  real posture — enforced, off, or unavailable. Children die with
  the parent (Pdeathsig).

## Verified for real

- All twelve packages; the enforcement test re-execs the test binary
  as a sandbox child that applies the actual ruleset and attempts
  writes — inside the root succeeds, outside is denied by the
  kernel (exit status + missing file both checked), no mocks.
- **Real binary, real kernel**: `opcode __sandbox` directly — an
  inside write succeeded, a home write failed `Permission denied`.
- **PTY, live**: a scripted model called run_shell writing both
  inside the project and to `$HOME`; the startup note showed
  `sandbox: landlock v10…`, the tool result carried the kernel's
  `Permission denied` for the home write, and only the project file
  exists on disk.

## Phase 21 assumptions

1. Reads stay unrestricted — tools need system headers and libs;
   Codex allows the same.
2. Commands that legitimately write outside the roots (e.g.
   `go install` to `~/go/bin`) now fail EACCES — surfaced in the
   tool result; disabling the sandbox is the documented escape.
3. Network isolation (seccomp) is a future phase; Landlock v5
   cannot scope TCP.

# Phase 22 — Image paste + vision (status: complete, live-verified)

Spec: [docs/specs/phase22-image-paste.md](specs/phase22-image-paste.md)

## Built

- **The wire**: `llm.Image` on `Message.Images`; a user message with
  images serializes as the OpenAI parts array — text part, then one
  `image_url` part per image as a data URL. Text-only and tool
  messages keep the plain string form. A test pins the exact
  serialized shape.
- **The clipboard**: ctrl+v reads the image through the platform
  tool — `wl-paste` (Wayland), `xclip` (X11), `pngpaste` (macOS),
  first that answers wins — because terminals cannot deliver image
  bytes through bracketed paste. A numbered `[Image #N]` placeholder
  lands at the cursor (the large-paste token pattern); an 8 MiB cap
  is enforced at the consumer, so the bound holds whatever produced
  the bytes.
- **The flow**: images attach to the message, ride with queued
  follow-ups until their turn runs, and persist on the history
  message for the session (what the model saw once, it sees again on
  resume).
- **Readable provider errors** (found live: an OpenRouter 404 dumped
  its raw JSON envelope into the transcript): non-2xx bodies are
  parsed for the OpenAI `error.message` (or top-level `message`) and
  shown as the message; unparseable bodies truncate to 300 chars.
  The user's live config was also migrated off the retired
  `:free` slug.

## Verified for real

- All twelve packages; new tests: the wire shape, the attach
  lifecycle (placeholder, numbering, cap, no-image toast), and
  queue-drain routing (the provider's recorded request carries the
  image).
- **PTY, live**: a fake `wl-paste` on PATH supplied a PNG; ctrl+v
  showed the toast and `[Image #1]`; the request-inspecting fixture
  answered "saw a png image" — the data-URL part was on the real
  wire.
- **Live provider**: the migrated config's model answered a real
  turn (`opcode -p`).

## Phase 22 assumptions

1. The image format is sniffed from the bytes' magic signature —
   png, jpeg, gif, webp — never assumed from the platform tool;
   unrecognized bytes are refused with a toast, not mislabeled.
2. Vision-less providers will 4xx the parts array; the error now
   reads as the provider's own message instead of a JSON dump.

# Phase 23 — Multi-provider authentication (status: complete)

Spec: [docs/specs/phase23-provider-auth.md](specs/phase23-provider-auth.md)

## Built

- **The derived env rule**: when a provider's models.json entry
  names no `api_key_env`, the conventional `<PROVIDER>_API_KEY`
  applies — uppercased, dashes and dots to underscores. One rule
  covers every OpenAI-compatible provider (deepseek →
  `DEEPSEEK_API_KEY`, groq → `GROQ_API_KEY`, …) where the reference
  products hardcode thirty-entry tables; an explicit `api_key_env`
  still wins over the derivation, and auth.json still wins over
  both.
- **`/login <provider>` and `/logout <provider>`**: bare forms keep
  meaning the active provider; a named form stores or removes that
  provider's key. The live client changes only when the named
  provider IS the active one — storing another provider's key is
  pre-provisioning, and the transcript entry says when that is the
  case. The provider argument is used as given (the TUI stays
  config-free), so a typo stores an entry the next `/logout` can
  remove.

## Verified for real

- All twelve packages; new tests: the derived fallback, the explicit
  override winning, the name-derivation table (dots, dashes, empty),
  and the named login/logout flow (stores under the name, removes
  exactly that entry, entries name the provider acted on).

## Phase 23 assumptions

1. OAuth/device flows stay out of scope: they need per-provider
   client IDs and callback servers, and opcode's API-key chain is
   complete for every OpenAI-compatible endpoint.
2. The conventional `<PROVIDER>_API_KEY` names match what users
   already export for other tools (the same names the reference
   table uses for the common providers).

# Phase 24 — TUI/UX spec adoption + design tokens (status: complete, live-verified)

Spec: [docs/specs/tui-ux-spec.md](specs/tui-spec.md) (merged into
tui-spec.md — see "Spec consolidation" below) — the
new source of truth for everything visual or interactive. By its own
precedence rule it overrides earlier direction where they conflict
(including the Phase 20 Codex restyle: the spec's accent is opcode's
own teal `#2dd4bf`, and it forbids cloning reference identities).

## Built — the first convergence step (Section 2)

- **Colors** to the token table: teal accent (`#2dd4bf` dark /
  `#0f766e` light), `fg.muted` `#9aa0a6`, `fg.subtle` `#6b7280`
  (placeholder and hints now subtle, metadata muted — two grays,
  not one), success/warning/danger/info per the table, `border`
  `#3f4650`, `surface.user` `#262a31`, and a separate
  `surface.code` `#1c2026` (code blocks no longer share the user
  block's fill). Body text now uses the **terminal's default
  foreground** (empty fg token), so light and dark terminals both
  read correctly at the body level.
- **Glyphs** to Section 2.4: `❯` composer/user/selection, `⎿`
  result connector, `✗` errors, `⚠` warnings, `☐ ◐ ☑` todos (done
  items dim + struck through), `⏵` queued, `~` kept as the header
  brand mark. Emoji: none, per the spec.
- **Tests**: every color/glyph pin moved to the spec values — moved,
  not deleted (placeholder-subtle, no-background-highlight,
  panel-fill, light-theme, accent-deepening, and todo-glyph
  assertions all still assert).

## Verified for real

- All twelve packages; PTY live (256-color): teal accent, subtle
  placeholder, muted metadata, ❯ prompts, user-block fill, and the
  header brand all render as the spec's screen anatomy describes.

## Convergence roadmap — what the spec demands that is NOT yet built

Honest list, in build order (each is its own phase):

1. **Modes + keys (Section 10, 13)**: ask / accept-edits / plan /
   bypass with Shift+Tab cycling, Tab queuing, mode-colored
   composer rules and footer labels. Today: read-only / plan /
   ask-every-time / full-auto on Tab. The spec's Section 10.1 maps
   them onto the existing postures — a rename plus the accept-edits
   split and bypass guardrails.
2. **`safe/` sanitizer (Section 3.1)**: typed sanitization of all
   untrusted text; today the redactor covers secrets, not escape
   sequences.
3. **Inline commit renderer (Section 1.4)**: block-commit streaming
   to scrollback; today the whole view repaints per frame.
4. **Composer rules + type-ahead protection + paste safety details
   (Sections 5, 9)**: horizontal rules around the composer,
   permission-dialog type-ahead guard, `/`-and-`!` paste guards.
5. **Component gallery + goldens (Section 1.3, 20)**.
6. **Copy catalog, ask_user, /context, /diff, transcript view,
   ASCII glyph fallback, themes picker** — the remaining Sections.

## Phase 24 assumptions

1. termenv rounds truecolor one step in this environment (the pins
   assert what renders, e.g. `#2dd4bf` → `44;211;191`).
2. The 16-color column degrades via termenv quantization from the
   hex values rather than a hand-mapped table; the spec's 16-color
   names (cyan accent, green success, …) match what quantization
   produces. Verified by eyeball in a 256-color PTY; a forced
   16-color golden comes with the gallery phase.

# Phase 25 — Built-in provider catalog + native Anthropic (status: complete, live-verified)

Spec: [docs/specs/phase25-providers.md](specs/phase25-providers.md)

## Built

- **The catalog** (`internal/config/providers.go`): openrouter
  (default), openai, anthropic, mistral, google, nvidia, groq,
  deepseek, together, cerebras, xai, moonshot, fireworks, qwen,
  ollama — name one in `default_provider` and the base URL and
  credential rule resolve with zero configuration. Explicit
  providers entries always win; entries that name only part of a
  catalog provider merge the rest. (The sole-provider selection bug
  this exposed — the forced OpenRouter insert ran before the
  sole-provider check — is fixed in the same pass.)
- **A native Anthropic Messages client** (`internal/llm/anthropic.go`)
  — the one catalog entry that is not OpenAI-compatible:
  `/v1/messages` with `x-api-key` + `anthropic-version`, `system`
  as a top-level param, required `max_tokens` (ChatRequest gained
  the field; default 8192), `tool_use`/`tool_result` content blocks,
  base64 `image` sources (Phase 22 carries over), and `input_schema`
  tools. SSE mapping: `text_delta` → text, `thinking_delta` →
  reasoning (Phase 19 works for Claude), `input_json_delta`
  accumulates and emits the call at `content_block_stop`,
  `message_start`/`message_delta` usage → UsageEvent, and errors
  reuse the readable-body parser.
- **Client selection**: `llm.New(api, baseURL, key)` switches on the
  wire type; the three construction sites (startup, `/model`, and
  `/login`'s live re-key) share it — the TUI's Options gained the
  API field so the re-keyed client keeps the right protocol.

## Verified for real

- All twelve packages; new tests: catalog resolution (named
  built-ins, explicit-wins, empty-entry merge, sole-provider
  selection), and the Anthropic suite mirroring the OpenAI one —
  wire format (system, max_tokens, tool_use/tool_result, image
  sources, input_schema), text/tool/thinking/usage/error mapping,
  and the readable HTTP error.
- **PTY, live**: a scripted Messages-API fixture streamed thinking,
  text, and a tool_use block through the real TUI — the thinking
  collapsed to `△ thought for 0s`, write_file really wrote
  `/tmp/anthproj/main.go`, and the second round completed.

## Phase 25 assumptions

1. The Messages wire details are from the documented API shape and
   verified against scripted SSE, not against Anthropic's live
   servers (no key here); the first live Claude session is the
   remaining honest check.
2. Bare model names (e.g. `claude-sonnet-4.5`) are the user's to
   set; per-provider model catalogs are future work.

# Phase 26 — Live model catalog, /login + /models pickers (status: complete, live-verified)

Spec: [docs/specs/phase26-model-catalog.md](specs/phase26-model-catalog.md)

## Built

- **The abstraction** (`internal/llm/models.go`):
  `FetchModels(ctx, api, baseURL, key)` — both wire shapes:
  OpenAI-compatible `GET {base}/models` (Bearer) and Anthropic's
  `GET {base}/v1/models` (x-api-key, `display_name` carried). Sorted
  by id; every browse is a fresh fetch, so the list is accurate, not
  a hardcoded snapshot.
- **`/models`**: step 1 lists every provider — the catalog plus
  models.json customs, each row honest about whether its models can
  be fetched right now ("enter to browse models" vs "no key —
  /login <name>"; local servers need no key). Selecting fetches
  off the UI goroutine; arrival opens the model picker (a stale
  arrival — the user moved on — is a dim note, never a surprise
  picker); selecting a model switches provider + model through the
  existing rebuild (main's switchModel now falls back to the catalog
  for providers models.json doesn't list; the TUI refreshes its
  BaseURL/API state so a later /login re-key keeps the protocol).
- **`/login`** (bare): the same provider list as its picker;
  selecting starts the masked key capture. `/login <provider>`
  unchanged.
- **Wiring**: Options gains `KeyFor` (the credential chain, injected
  from main); picker items carry their own action ("fetch" /
  "login") because the picker closes before selection dispatches —
  and pickerSelect returns a Cmd so the fetch actually starts.

## Verified for real

- All twelve packages; new tests: both fetch wire shapes, the
  provider picker's contents and key hints, arrival → model picker →
  switchModel routing, error and stale-arrival handling, the login
  picker starting the masked flow, and the fetch seam. Existing
  login tests moved to the explicit form (bare /login now opens the
  picker — the moved tests still assert the same flow).
- **PTY, live**: a fixture serving `/v1/models` + chat — `/models`
  opened the provider picker, filtered to the fixture, fetched its
  model list, switched to the fetched model, and the next turn
  answered on it ("round trip on the switched model" in the log).
  (An earlier all-empty result was a fixture bug — a NameError in
  its POST handler — not a product bug; found by probing the
  fixture directly.)

## Phase 26 assumptions

1. Model metadata stays id/display-name; pricing and context windows
   come when a provider's models endpoint justifies them.
2. Two-step browse (provider → models) over a parallel fetch-all:
   the latter needs a key for every provider and fires 15 requests.

# Housekeeping pass (post-Phase 26, status: complete)

A full-tree audit: every file read, every symbol grepped for usage.

## Removed

- `internal/subagent/x.txt` — a stray one-word scratch file.
- `remapLegacyStyles` (`userStyle`, `toolStyle`, `resultStyle2`) —
  zero usages; the token migration they bridged finished in Phase 24.
- The `HexFloor`/`Floor` token, `errorStyle`, and `codeStyle` —
  declared, never used.

## Fixed

- **Markdown code blocks used the wrong surface** — a Phase 24 miss:
  the code-block background still took `surface.user`'s fill after
  the surfaces were split. Now `surface.code` (`#1c2026`), as the
  spec's token table says.
- README: a missing blank line from a Phase 26 edit, badges still in
  the retired Codex blue (now the teal accent), and pre-Phase-24 todo
  glyphs in the feature text.
- `docs/specs/opcode-architecture.md` §4: the directory layout
  predated five packages (session, trust, sandbox, headless; and the
  llm/config descriptions were stale). Now matches the tree.
- `docs/specs/tui-design.md`: marked as a historical reference for
  the Python + Rich lineage — `tui-ux-spec.md` governs the Go
  implementation. Content untouched.

## Added

- A CI check enforcing the TUI/UX spec's import-graph rule: the
  headless package must never depend on the TUI stack (internal/tui,
  bubbletea, lipgloss). True today; now it stays true.

## Verified

- All twelve packages; gofmt, vet, and `go mod tidy` clean; no
  TODO/FIXME markers in non-test code; every PROGRESS spec link
  resolves to an existing file.

# Phase 27 — The approval dialog + install success screen (status: complete, live-verified)

The reference product's permission anatomy and install completion,
built to the tui-ux-spec's Section 9.

## Built

- **The approval dialog** replaces the one-line "allow?" box: a
  plain-words title and tier badge ("Bash command · Runs a command"),
  the literal command shown verbatim, the question, and three
  numbered options — `1. Yes`, `2. Yes, and don't ask again for:
  <prefix>:*`, `3. No` — with **No preselected** (the safest
  default, spec 9.2). Arrows move, number keys and `y`/`a`/`n` are
  the fast paths, enter takes the highlighted option, esc denies.
- **Session-scoped "don't ask again"** (option 2): shell commands
  grant a program+subcommand prefix rule (`npm init:*`), matched by
  the same fail-closed engine as the config allowlist —
  metacharacter-bearing commands never match; other tools grant
  per-tool. Session-only: nothing is written to disk, nothing is
  revoked at the provider, the toast says exactly what was granted.
- **A latent fit() bug fixed**: frame fitting counted *layers*, not
  rendered rows — fine while every layer was one line, broken the
  moment the multi-row dialog landed (a 31-row frame in a 24-row
  terminal). fit now counts rows and never trims the protected tail
  (open dialog, composer); the transcript trims from the front.
- **decide() fails closed** when no UI is running (nil program):
  nothing can be approved, so the action is denied — the same
  nil-guard posture startTurn already had.
- **install.sh**: "Next: Run opcode --help to get started", matching
  the reference's completion screen.

## Verified for real

- All twelve packages; new tests pin the dialog's anatomy (title,
  tier words, literal command, three options), every key path
  (arrows, numbers, y/a/n, esc, enter, wrap-around), and the grant
  end to end (matching command auto-allows, non-matching prompts,
  metacharacters fail closed, per-tool grants for non-shell tools).
- **PTY, live, ask mode**: a scripted model called `npm init -y` —
  the dialog rendered with the full anatomy, `2` granted
  `npm init:*` (toast confirmed), the command executed, and a
  second `npm init --yes` ran with **no second dialog**. A
  non-matching `npm install` correctly prompted again, and enter on
  the preselected No denied it. (Two earlier "nothing happened"
  runs were the fixture's recurring NameError — probed directly,
  not a product bug.)

## Phase 27 assumptions

1. `Tab` to amend and `Ctrl+E` to explain (spec 9.2) are not yet
  built — the dialog's hint line advertises only what works today.
2. The prefix grant covers program + subcommand, one level deep —
  the same scoping the reference product shows.

# Phase 28 — The Antigravity header + bare composer (status: complete, live-verified)

The reference screenshot's layout, applied literally.

## Built

- **The header**: the banner block on the left, the identity block
  beside it — `~ opcode v0.2.1`, model · mode, cwd — joined
  horizontally (lipgloss.JoinHorizontal, top-aligned), then startup
  notes below. Previously the logo and identity stacked vertically;
  the reference puts them side by side.
- **The composer, bare**: no rounded box — a plain `~ ` prompt line
  (the brand glyph, matching the reference's own `~` composer), the
  mode line under it unchanged. Shell mode keeps its up-front
  signal: the prompt glyph becomes `!` in warning amber — without a
  box, the glyph IS the chrome. boxStyle, now unused, is deleted.
- Width accounting follows: the bare line is terminal − 2, not
  terminal − 8 of box chrome.

## Verified for real

- All twelve packages; the pins moved to the new contract (bare
  composer width, `!`/`~` prompt glyphs). **PTY, live**: the frame
  shows the banner with model/mode/cwd beside it and a bare
  `~ ask opcode anything…` line — zero box borders in the whole
  frame.

## Phase 28 assumptions

1. The composer prompt is the brand `~` (the reference's literal
   look); the spec's `❯` stays on user-message blocks and picker
   selection, where it already lives.
2. The header's first line can fall to frame-fitting trim on very
   short terminals — everything scrolls anyway.

# Codex source audit (status: complete — reference docs)

Three parallel subagent audits of the Codex CLI source tree
(/home/chmgx81/Desktop/codex, ~100 crates, ~4,900 Rust files),
preserved as reference docs:

- docs/reference/codex-tui-audit.md — the TUI: rendering model
  (inline scrollback as persistence layer, two-region streaming
  with table holdback, pointer-keyed transcript anchors), the
  full default keymap, terminal probing, accessibility, and the
  bottom-pane/app architecture. Static audit; nothing executed.
- docs/reference/codex-core-audit.md — the session/turn
  lifecycle, the Op/event protocol, tool exposure lattice,
  approvals, compaction (three implementations, one lifecycle),
  world-state assembly, and the config schema.
- docs/reference/codex-features-audit.md — everything outside
  tui/core: the sandbox stack (bwrap+seccomp, Seatbelt, Windows
  restricted token, network policy proxy with MITM), MCP+OAuth,
  app-server protocol, hooks, skills/plugins, Guardian, the
  daemon, and the CLI surface.
- docs/reference/codex-adoption.md — the synthesis: what opcode
  adopts (prioritized, honest scoping), what it deliberately
  does not, and a sequencing suggestion. Top of the list:
  canonicalized approval matching, sandbox-denial readability,
  a reasoning-effort knob, /doctor, composer history, and the
  inline scrollback + block-commit streaming phase the TUI/UX
  spec already mandates.

# Spec consolidation (status: complete)

docs/specs/tui-design.md (the historical Python+Rich reference)
and docs/specs/tui-ux-spec.md (the v0.1 Go spec) are merged into
one dead-simple spec: docs/specs/tui-spec.md. It describes what
opcode actually is as of Phase 28 — tokens, glyphs, every screen
with a mockup, keys, behavior rules — plus the short honest
"not yet built" list. Earlier log entries above still cite the
old filenames; both point there now. Codex's handling of every
one of these sections lives in docs/reference/codex-*.md.

# Breathing space (status: complete, live-verified)

The transcript gets the reference products' spacing rhythm, in
blankBefore: one blank line before every top-level block — user
turns, answers, plans, reasoning receipts, compaction notes, tool
groups (when they follow anything but tool activity), and subagent
groups (by title) — while an action and its own result stay tight,
as do consecutive calls inside one tool group. collapseBlanks
keeps the rhythm from doubling. Pinned by
TestBlockBreathingSpace (block opens get air, action → result is
tight); PTY shows the blank lines between assistant text, the
tool group, and the dialog. The spec's Behavior rules now state
the rhythm.

# Composer rules (status: complete, live-verified)

The full-width horizontal rules from the spec's screen anatomy —
never actually implemented (the composer had a rounded box until
Phase 28 removed it for the bare look, which left the input with
no frame at all). Now the composer sits between two full-width `─`
rules: border-colored at rest, amber in shell mode, so the input
is findable without a box. fit()'s composer reservation grows by
the two rule rows so short terminals still trim the transcript,
never the composer. The spec's §3.3 mockup shows the frame.

# Mode glyphs (status: complete, live-verified)

The mode line no longer borrows the brand ~: each permission mode
carries its own glyph — ○ read-only (nothing will run), ⏸ plan
(writes paused), › ask (the ball is in your court), ⏵⏵ full-auto
(everything proceeds, the reference product's own shape). The ~
now means exactly one thing: the composer. Verified live by
cycling all four modes with Tab in a PTY.

# Scrollback commit + prompt history (status: complete, live-verified)

The two complaints from the PTY session: past turns vanished from
native scrollback (the frame repainted every tick and fit() trimmed
the transcript with a "… N earlier lines" marker), and the composer
had no ↑ recall.

Scrollback: the transcript now commits per turn. A `committed`
counter on the Model tracks entries already printed above the live
region via tea.Println; submitInput commits everything before the
new user entry, and turnEnded commits before a queued follow-up
starts. commitLines() renders the not-yet-committed entries with the
same blankBefore rhythm the live frame pins (one trailing newline
keeps the seam breathing), timelineView skips committed entries, and
fit()'s "… earlier lines" marker can now only ever refer to the
current turn's own content. Committed text is frozen: ctrl+r
expansion applies to the live region only. Without a running tea
program (tests, headless) commitEntries is a no-op — there is no
scrollback to print to, so committing would silently drop content.

History: ↑/↓ recall submitted prompts. ↑ recalls only from the
composer's first line and ↓ forward from its last (composer.Line /
LineCount — bubbles v1.0.0 has no CursorOn* helpers), so inside a
multiline draft the arrows still move the cursor. The live draft is
saved on first recall and restored walking past the newest; typing
resets the recall position. Prompts persist to history.jsonl under
opcode's home — one JSON line each, 0600, capped at 500, consecutive
duplicates collapse. Login keys bypass submitInput entirely, so
secrets never enter history. Known edge: a recalled [paste N] token
from an earlier session no longer expands — the typed form is what
history keeps, visibly so.

Verified live in a PTY against a text-only fixture (two 15-line
turns): the full first turn sits in native scrollback after the
second submit (no marker anywhere in the log), and ↑ recalled the
previous prompt into the composer. history.jsonl holds both prompts
at mode 600. Unit tests: history recall/draft/typing-reset/
multiline-guard/persistence/cap, commit boundary rhythm, View
skipping committed entries, the no-program no-op. Spec §3/§4/§5/§6
updated in the same change; the not-yet list drops inline commit
and composer history.

# History paste-token caveat fixed (status: complete, unit-verified)

The known edge from the scrollback/history phase: history stored the
typed form, so a recalled [paste N] token from an earlier session was
a dead token. Fixed in pushHistory: at submit time the paste map is
still populated, so paste tokens expand into their content for the
stored form — recall now gives back usable text. @path mentions
stay raw (they re-read the file fresh at submit, which is what you
want). An expansion beyond ~4 KB keeps the typed form on purpose:
a visible dead token on recall beats megabytes in history.jsonl.
Pinned by TestHistoryExpandsPasteTokens and
TestHistoryKeepsHugePastesAsTyped. The recall key path itself was
already PTY-verified last phase; this change only touches what
pushHistory stores, so a fresh PTY run was not needed.

# list_dir — read-only and plan modes can explore (status: complete, live-verified)

From a live session: in read-only mode the model could read files but
had no way to list a directory — it guessed paths and apologized for
the missing tool. list_dir closes that gap: Read-Only tier, so the
tier-based mode policy admits it in every mode (read-only and plan
included) with no prompt, no mode-logic changes. It lists one
directory's entries one per line, directories marked with a trailing
slash, sorted, capped at 500 entries with a count note so a huge
directory can't dump its whole index into context. Registered in
cmd/opcode's registry (subagents inherit it through subset()); the
gate, audit log, and headless wiring needed no changes.

Verified live in a PTY: read-only mode, a fixture that calls
list_dir on the session's working directory — the transcript shows
the call, the real entries (main.go), and the follow-up answer, with
no permission dialog. Pinned by TestListDir (entries, dir markers,
empty dir, file-as-path and missing-path errors),
TestListDirCapsHugeDirectories, and list_dir rows in TestToolTiers
and TestModeAllowsTool.

# The sandbox-aware gate (status: complete, live-verified)

Phase 30 adopts Codex's core permission posture, verified in the
codex-rs sources: the sandbox is the safety, approval is the
exception. Until now ask mode prompted for every action call — even
ones the Landlock sandbox already confined — and read-only/plan hid
the action tools entirely, so the model couldn't even propose a
write.

The changes: ask-every-time is renamed to ask (its posture no longer
asks every time; the name must not lie) with NormalizeMode mapping
the old spellings so every config keeps working. Offering is not
permission: every mode now offers every tier, and the gate is the
single enforcement point. run_shell gained a real {"sandbox": false}
opt-out — the README claimed it since Phase 21; it exists now, and
it is the approval-triggering escape. In ask mode, bounded actions
run without prompting: Landlock-confined shell commands (credited
only when sandbox.Active() — where Landlock is unavailable every
action prompts, never auto-runs a command it cannot confine) and
write_file/edit_file targets inside the same writable roots, with
the parent's symlinks resolved first so a lexical in-tree path
pointing outside still asks. read-only and plan prompt for every
action-tier call: the model can propose a write and the user
approves it in place, no mode switch. The config safe_commands
allowlist is removed — sandboxed commands auto-run in ask mode, so
a per-command allowlist had nothing left to do; the session "don't
ask again" grants survive (they cover escapes). The approval dialog
names an escape for what it is ("Runs a command without the
sandbox").

Honest scope notes: headless in ask mode now runs bounded actions
where it used to fail closed on everything — escapes still fail
closed; spec'd in phase30-sandbox-gate.md. read-only shell commands
prompt rather than running under a per-call empty ruleset (Codex's
read-only sandbox) — that refinement is not built; noted in the
phase spec's non-goals.

Verified live in PTYs against scripted fixtures: ask mode ran a
sandboxed echo>file with no dialog, then a {"sandbox": false} call
opened exactly one dialog (named as an escape), approved, and ran;
read-only mode proposed a write_file, the dialog appeared, approval
wrote the file — no mode switch. Unit tests: the new decision
matrix, bounded/escaping shell and write paths, the symlink
resolution, the no-sandbox fail-closed, mode offering, legacy
aliases, plus a headless bounded-write test. Full suite green
across all 12 packages.

# Move to top + the brand says its name (status: complete, live-verified)

Two asks from a live session: opcode should start at the top of the
terminal, and — like Codex, which names itself in everything it
shows — opcode's copy should say opcode.

Move to top: Run clears the visible screen and homes the cursor
(ESC[H ESC[2J) before the program takes over, so the frame always
starts at row one instead of wherever the shell prompt left the
cursor. Scrollback above survives — only the visible screen is
erased — so the per-turn scrollback commits from the earlier phase
still land in the terminal's own history. Maximize-the-window
itself is the window manager's job; no terminal app can do it
(that's ptyxis' maximize button).

Brand: the approval dialog now reads "opcode needs your approval to
run this" and the trust dialog "opcode would be able to run: ..." —
the two surfaces where Codex says "Codex". The greeting, window
title, and composer placeholder already carried the name; the ~ in
the footer stays reserved to the composer per the earlier glyph
decision.

Verified live in a PTY: junk lines printed first, then the log shows
clear+home immediately followed by the banner at the top row. Full
suite green.

# Graceful exits (status: complete, live-verified)

Codex's exit posture, adopted: ctrl+c (and ctrl+d, Codex parity) no
longer kill opcode on first press. The first press arms a 4-second
window — interrupting a running turn, with the toast saying
"interrupted — ctrl+c again to exit" (or just "ctrl+c again to
exit" when idle) — and only a second press inside the window exits.
The window matches the toast's lifetime, so the on-screen promise
never outlives the arm. /exit and /quit remain immediate; esc stays
the plain interrupt. The help overlay and the spec/README key
tables carry the new rows.

Feedback on the way out: after the program stops, main saves the
session and prints one line — "~ opcode — session saved · resume it
with /sessions" — the goodbye that says what happened and the way
back in.

Verified live in a PTY: first ctrl+c shows the hint and keeps
running, the second exits cleanly, and the farewell line appears
after the frame. Pinned by TestCtrlCDoublePressExits,
TestCtrlCHintWhenIdle, and TestCtrlDIsTheSameDoublePress. Full
suite green.

# Accessibility & navigation (status: complete, live-verified)

A pass over the Codex audits for the issues that bite real users,
four items:

Type-ahead guard: for 400 ms after the permission, plan, or trust
dialog opens, keystrokes are swallowed — a fast typist's stray "y"
landing on a just-rendered approval can no longer answer it (Codex's
block_terminal_input_for_pending_startup_events, adoption item 9;
spec §6 item 3 retired). The plan tests that answer instantly now
fast-forward past the guard, which is the honest fix: the guard is
the feature.

Ctrl+O transcript pager: an overlay over the whole conversation —
committed entries included (the pager's point is reading what
scrolled into native scrollback), results and thinking expanded,
↑↓/pgup/pgdn scrolling with count markers, esc closes. While open
it owns the keyboard: typing goes nowhere until it closes (spec §6
item 4 retired).

--plain / OPCODE_PLAIN / detected screen reader: the glyph vocabulary
becomes vars, adaptGlyphs swaps every one for ASCII — ✓→[ok], ⎿→\-,
○⏸›⏵⏵→o=>>> — no glyph disappears (spec §6 item 6 retired). The
composer prompt re-reads the glyph after the swap (the textarea
captured it at construction). Animation follows the existing
animations:false key.

Sanitized window title: control characters, C1 bytes, and bidi
overrides are stripped and the title capped at 240 runes before it
reaches the OSC surface — a crafted directory name can no longer
hijack the terminal window title (tui-audit notable 13).

Verified live in a PTY under OPCODE_PLAIN: zero Unicode glyphs remain
in the log (mode line "> ask", composer ">", pager header rendered
after ctrl+O), clean double-press exit. Pinned by TestTypeAheadGuard,
TestPlanTypeAheadGuard, TestTranscriptPager, TestAdaptGlyphs,
TestSanitizeTitle. The spec's not-yet list is down to two items.

# More tools, more capability (status: complete, live-verified)

Against Codex's tool inventory (shell, apply_patch, view_image,
update_plan, current_time, get_context_remaining, request_*, MCP)
and the agent-definition tool lists (Bash, Glob, Grep, Read,
WebFetch, TodoWrite), opcode had three real gaps. Filled:

- search_files (Grep): regex content search over a tree —
  path:line:text matches, binary/.git skipped, 50-match cap with a
  count note. Read-Only tier, so read-only and plan modes can now
  FIND things, not just read what they guessed at.
- glob_files (Glob): pattern-based file finding; ** crosses
  directory separators, single * does not, 200-match cap. Read-Only
  tier.
- current_time: RFC 1123 + UTC/local + weekday — models have no
  clock. Read-Only tier.
- apply_patch (Codex's signature tool): V4A patches — Update File
  with @@ context hunks (exact match first, then a
  leading-whitespace-normalized pass, as Codex's seek_sequence
  does), Add File, Delete File, in one call; the optional
  <<'EOF' heredoc wrapper is accepted. A hunk whose context is
  absent fails loudly instead of guessing, and the error says how
  many earlier files were already changed. Action tier, and the
  gate credits it as bounded only when EVERY touched path sits
  inside the sandbox's writable roots — a partial bound is no
  bound; anything touching outside prompts.

Verified live: read-only mode searched a work directory through
search_files (haystack.txt:1: the needle is here) and answered,
zero dialogs. apply_patch end-to-end, bad-context rejection, and
boundedness are unit-pinned (7 new tests; the boundedness test
proved its worth by catching a test that chdir'd the package and
poisoned the writable-roots check for the tests after it).
Deliberately not built: WebFetch/WebSearch (network egress needs
its own policy conversation), view_image (opcode attaches images on
input), request_user_input (opcode asks through the plan and
permission surfaces). Agent definitions with named tool subsets —
the codex screenshot's pattern — are a possible later phase on top
of subagents.

# grep and glob, by name (status: complete, unit-verified)

search_files and glob_files became grep and glob. Not taste —
training data: the reference agents' Grep/Glob are the names models
emit fluently, and a tool the model reaches for without prompting
is worth more than a naming convention. The rest of the vocabulary
stays verb_noun snake_case (read_file, write_file, apply_patch,
run_shell, todo_write); these two are the deliberate exceptions,
the way Codex's own shell tool is "shell" and Claude Code's is
"Bash" rather than run_shell_command. No compat shim needed: tool
names are not persisted in sessions, and no external user ever saw
the old names.

# Three modes: plan / build / full-auto + web_fetch (status: complete, live-verified)

The user's instinct matched both references: Codex ships three
presets (Read Only / Default / Full Access), Claude Code's working
triad is plan/default/bypass. opcode's four collapsed into three —
and the collapse was nearly free, because read-only and plan were
already the same posture in the gate (every action prompts); only
the instruction differed. read-only folded into plan; ask became
build. NormalizeMode maps every legacy spelling (read-only ->
plan; ask, ask-every-time, auto-accept-safe-ops -> build), so no
config breaks. Tab cycles three; plan approval graduates into
build; the mode glyph set is now ⏸ › ⏵⏵ (read-only's ○ is retired
with the mode).

web_fetch: the WebFetch shape — fetch a URL, strip HTML to
readable text (script/style dropped, entities decoded, case
preserved; the naive version lowercased the whole document and a
block-tag loop never advanced past the tag it just found — both
caught by the tests), 5-redirect cap, 256 KiB cap, non-text types
report instead of dumping. Action-Allowed tier: network egress is a
trust boundary the sandbox does not cover (Codex's sandbox denies
network by default), so it asks in plan and build and runs free
only in full-auto. Deliberately not built: web_search (needs a
provider key and its own policy talk) and browser automation (MCP
territory — point opcode's MCP config at a Playwright server).

Verified live: a PTY session cycled build -> full-auto -> plan and
back, all three glyphs on the mode line. Full suite green across
all 12 packages; mode tests rewritten for the triad (the
decision-matrix rows, the legacy-alias expectations, the tab-cycle
sequence, the footer degradation).

# run_shell became bash (status: complete, live-verified)

The last naming wart. An audit of the whole vocabulary against the
references: grep/glob/apply_patch/current_time match their
Claude/Codex counterparts exactly (the names models emit fluently
from training data), todo_write matches Claude's TodoWrite, and
read_file/write_file/edit_file/list_dir/present_plan are opcode's
own self-describing verb_noun school — clearer than Claude's bare
Read/Write/Edit. The one name diverging from every reference was
run_shell. Renamed to bash — Claude Code's name, and the name the
approval dialog already used as its title ("Bash command") — and
the tool now literally runs bash -c instead of sh -c so the name is
true rather than aspirational. Claude Code made the same choice;
the cost is bash as a dependency, which every dev workstation
carries.

Verified live in a PTY: the composer's ! shell escape ran
`echo bash-runs-$(printf x)y` through the tool and printed
bash-runs-xy; full suite green across all 12 packages.

# The safe/ sanitizer (status: complete, live-verified)

The first item off the honest list. Untrusted text — file contents,
shell output, fetched pages, model output, tool arguments — flowed
into the display raw. A file containing `ESC]0;pwned BEL` could
retitle the window; `ESC[2J` could clear it; `ESC[?1h` could remap
the keyboard. Tool results were data, but the terminal executes what
it is handed.

Built `internal/safe`: `safe.Text` scans by rune (UTF-8 survives
untouched — an em-dash encodes 0x80 as a continuation byte, and
byte-level C1 stripping would corrupt it) and strips every
ESC-initiated sequence with a typed parser: CSI up to its 0x40-0x7E
final byte, OSC/DCS/SOS/PM/APC to BEL or ST, the nF intermediates
(`ESC (B`), and the two-rune Fe/Fs forms — plus C0 minus newline and
tab, DEL, and the C1 runes some terminals execute as 8-bit controls.
An unterminated sequence is consumed, never passed on: the failure
direction is display fidelity, never terminal control. An OSC that
never sees its BEL ends at the next newline or bare ESC instead of
swallowing the rest of the file.

The boundary is the display, not the context. The orchestrator's
copy — what the model reasons over — keeps the honest bytes;
sanitizing there would corrupt file contents the model is reading.
Wired at the TUI event choke point (stream text, reasoning, tool
arguments, results, subagent output, error text), the permission
dialog's display copy of args (grant matching and execution keep
raw), the plan dialog, and headless text mode. JSON headless output
is exempt: encoding/json escapes every control character, so
downstream tools get honest data and a JSON line can never carry a
live sequence.

Verified live in a PTY: a fixture model read a poisoned file
(title grab, screen clear, keyboard remap, lone trailing ESC) and
the rendered result line showed the readable text "prepostend" —
the 29 escape bytes in the capture were all opcode's own UI styling
and cursor addressing, "pwned" never reached the terminal, and the
window title survived. Full suite green across all 13 packages;
unit tests cover every sequence form, the UTF-8 continuation-byte
trap, unterminated sequences, and integration tests feed poisoned
payloads through handleEvent, the tool line, the stream, and
headless text mode.

# Phase 33 — /doctor (status: complete, live-verified)

Spec: [docs/specs/phase33-doctor.md](specs/phase33-doctor.md)
(The adoption doc's #7: "the first thing to suggest to a user whose
setup broke.")

## Built

- `internal/tui/doctor.go` — `/doctor` renders one transcript entry,
  one row per subsystem, each with a verdict glyph (✓ ok, ⚠ attention,
  ✗ broken, · neutral fact) and the next step when something is
  wrong: version, model + provider + base URL, api-key presence
  (never the key itself — the test asserts the key bytes never reach
  the report), config.json and models.json through the same loaders
  the startup path uses, the live sandbox posture (active with the
  Landlock ABI, off with the config key that turns it on, or
  unavailable on this platform), project trust (trusted / untrusted /
  changed with the changed-file count), skills and MCP counts, the
  audit log's path and size (or the honest "created on the first
  gated action"), and the terminal's color profile with TERM,
  NO_COLOR, and the plain posture.
- `GlyphInfo` ("·" / "-") added to the shared glyph vocabulary so
  neutral rows degrade in the plain posture like every other glyph.
- Palette entry and command dispatch; no new Options fields — the
  checks read what was already wired (KeyFor, OpcodeHome, AuditPath,
  Skills, MCPNames, Cwd).

## Design choices

- Doctor presents, never repairs: no writes, no migrations, no
  network probe (a real API call costs money; a broken endpoint
  already fails loudly at startup).
- A nil KeyFor (headless wiring) reports "resolution not wired" as a
  dim fact, not an error.
- Missing config.json and models.json are the normal first-run case
  and read as neutral facts, not warnings.

## Verified for real

- `go test -count=1 ./...` — all 13 packages; doctor tests cover the
  healthy report (every row present, key bytes absent), the missing
  key with its /login next step, the unwired-key posture, a malformed
  config.json (verbatim error, the report survives), the empty-model
  failure, and the not-yet-created audit log.
- **PTY live**: /doctor through the palette in a real session — the
  report rendered with live values: "model fixture/mini via fixture",
  key resolved, "config.json — mode build", "sandbox: landlock v10",
  "project trust: trusted", and the audit log honestly reading
  "created on the first gated action" (no gated action ran). Clean
  /exit, session saved.


# Phase 34 — the themes picker (status: complete, live-verified)

Spec: [docs/specs/phase34-themes.md](specs/phase34-themes.md)
(The tui audit's theme_picker: live preview + cancel-restore.)

## Built

- **The theme table** (`internal/tui/style.go`): three curated
  palettes — `dark` (the teal default), `light` (the existing
  light-legible set), and `green` (the original Phase 7 brand stack,
  electric green on near-black, kept alive as a choice).
  `applyThemeName` swaps every Hex token, refreshes every derived
  style, and drops the glamour renderer cache; `adaptTheme` is now a
  thin wrapper over the table, so the background probe and the
  picker share one mechanism. Unknown names change nothing and
  report false.
- **`/theme`** (`internal/tui/theme.go`): the shared picker with
  the active theme marked. Moving the highlight applies the
  highlighted theme live — the whole session re-skins in place;
  Esc restores the theme active when the picker opened (a cancelled
  preview never strands its palette, including Enter-with-zero-
  matches); Enter applies and persists via `Options.SetTheme`.
  `/theme <name>` switches directly. Without a SetTheme writer the
  note says "this session only" instead of lying.
- **Persistence**: config.json's `theme` key. `config.SaveTheme`
  edits the file as a raw JSON object, so unknown sibling keys
  survive; a missing file is created (0600). An explicit config
  theme wins over the background probe at startup; empty keeps the
  probe (OPCODE_THEME=light|dark still forces the posture). Unknown
  names fail loudly at startup in cmd/opcode via `tui.ValidTheme`.
- **Render-cache discipline**: applying a theme clears the per-entry
  markdown caches and the in-flight stream cache — both embed the
  old palette's ANSI codes. Committed native scrollback keeps its
  original colors (stated residual).

## Found and fixed during verification

- **The composer's prompt survived every theme switch in the old
  palette** — found live in the PTY: a persisted green startup
  rendered green rules but a teal "❯". Root cause is a bubbles
  footgun: textarea.Model holds its active style as a *pointer into
  the struct it was focused on*, and New returns the model by value,
  so the copy's pointer still aims at the original — every later
  FocusedStyle write renders in the construction-time color.
  `syncComposerPrompt` now re-seats the pointer (Focus/Blur) after
  writing styles, and Run does the same for the startup path. The
  shell-mode amber prompt worked only by luck of the Update cycle
  re-seating for us; that luck is now a guarantee.
  TestComposerPromptFollowsTheme pins it at the byte level.

## Verified for real

- `go test -count=1 ./...` — all 13 packages; theme tests cover the
  token swap (and that a failed apply changes nothing), the picker
  (backup on open, live preview on move, restore on cancel,
  persist on select, restore on empty-match close), the direct
  command (including the unknown-name error entry), the session-only
  note without a writer, the render-cache clearing, and the composer
  prompt regression. config's SaveTheme is tested for creation,
  overwrite, unknown-key preservation, and refusal over a
  non-object file.
- **PTY live, truecolor forced so accents are exact RGB**: /theme
  opened with the teal accent live; two downs rendered the session
  green with zero teal bytes; Esc restored teal with zero green
  bytes plus the "theme restored" note; /theme green saved (note +
  config.json carrying theme while preserving model and
  permission_mode); a second run started fully green — zero teal
  bytes in the entire capture, the prompt regression fix confirmed
  on screen.

# Phase 35 — the /diff command (status: complete, live-verified)

Spec: [docs/specs/phase35-diff.md](specs/phase35-diff.md)

## Built

- `internal/tui/diff.go` — /diff answers "what changed?" with the
  working tree's git changes rendered into the transcript with the
  verdict tokens: additions in the success color, deletions danger,
  hunk headers info, file headers bold chrome, context dim — the
  same vocabulary the edit_file hunks already use.
- The full posture: `git diff --no-color HEAD` (staged and
  unstaged; plain-diff fallback for a repo with no commits yet),
  plus `git status --porcelain` so untracked files — which a diff
  alone can never show — are listed as dim `?` rows instead of
  silently missing from the review.
- Every outcome is a designed state, not just the happy path:
  not-a-repo and clean-tree dim notes, a missing git binary named
  as an error, git errors verbatim, and a 400-line cap with an
  honest "… N more lines" footer so a monster diff cannot flood
  the transcript.
- User-invoked and read-only like the `!` shell escape: no model
  round trip, no permission prompt, no state change. Runs through
  tools.Bash (sandboxed, bounded) with `git -C <project>` so it
  does not depend on the process cwd; every byte of output passes
  safe.Text before rendering.

## Found during verification

- git itself C-style-quotes control characters in porcelain
  output, so a hostile filename arrives as inert printable text
  (`"\033]0;..."` with literal backslashes) — visible and named,
  incapable of driving the terminal, on top of opcode's own
  safe.Text pass. The test pins that no live OSC sequence can
  reach the entry.
- lipgloss gamut-shifts hex colors slightly when rendering
  (#4ade80 emits 73;222;128, not 74;222;128) — the tests assert
  the real emitted bytes.

## Verified for real

- `go test -count=1 ./...` — all 13 packages. /diff tests run the
  real git against real repositories: modified + untracked, staged
  changes visible in the HEAD diff, clean tree, not-a-repo,
  no-commits fallback, the renderer's per-line-class colors, the
  cap footer, and the sanitized-payload entry.
- **PTY live**: a repo with a modified go file and an untracked
  file — /diff rendered the `diff --git` header (bold), the removed
  line in danger, the added line in success, the `@@` hunk in info,
  and "1 untracked: fresh.txt" as a dim row. The only BEL in the
  capture is opcode's own window-title OSC. Clean /exit.

# Phase 36 — LaTeX conversion (status: complete, live-verified)

Spec: [docs/specs/phase36-latex.md](specs/phase36-latex.md)
(The honest list's last item.)

## Built

- `internal/tui/latex.go` — assistant math converts to Unicode at
  the display boundary. Applied at the top of `renderMarkdown`, the
  one choke point behind finished entries, the in-flight stream,
  and plan bodies, so every markdown surface converts and the
  model's context keeps the raw LaTeX.
- Delimiters: `\(...\)` and `\[...\]` and `$$...$$` always convert.
  Single `$...$` converts only when the content carries a math
  signal (`\`, `^`, or `_`), has no leading/trailing space, and no
  backtick — so "costs $5 and $10" and "run $HOME and $PATH" are
  byte-identical after conversion. Unclosed delimiters stay raw:
  a half-streamed region renders raw until its close lands, and
  the failure direction is fidelity, never corruption.
- `mathToText`: greek letters, relations, arrows, big operators,
  set notation from a curated symbol table; `\frac` flattens with
  precedence-preserving parens (only a genuinely compound side —
  an operator, or long enough to read as a product — gets them;
  `aᵢ` is one symbol); `\sqrt` takes the radical; super- and
  subscripts map to Unicode when every character has a form, else
  keep the readable `^(...)`/`_(...)` fallback; script arguments
  convert their commands first (`x^{\alpha}` maps); `\text`-family
  and `\mathbb` handled; unknown commands degrade to their bare
  name instead of vanishing; group braces drop; `~` becomes a
  space.
- Fenced code blocks are skipped line by line, and backtick spans
  are skipped like code — dollars and backslashes inside code are
  literal, not math.

## Found and fixed during verification

- Byte-vs-rune traps caught by the tests: the super/subscript
  glyphs are multi-byte (the maps are rune-keyed), and `frac`'s
  compound test was byte-length — `aᵢ` (2 runes after conversion)
  got parenthesized as if compound. Now rune-counted and
  operator-aware.
- Mid-stream frames legitimately show raw markup until a region's
  closing delimiter arrives; the frozen streaming scrollback keeps
  those frames (same as every streaming artifact), while the
  final frame converts everything.

## Verified for real

- `go test -count=1 ./...` — all 13 packages. Table-driven tests
  cover every construct, the degradations, and the anti-cases
  (currency, shell variables, backtick spans, fenced code,
  unclosed delimiters); a byte-identical plain-text test proves
  the converter is invisible when unused; an integration test
  proves renderMarkdown carries the conversion.
- **PTY live**: a fixture answer carrying `$\alpha + \beta^2$`,
  `$$\sum_{i=1}^{n} \frac{a_i}{b_i} \to \infty$$`, and
  `$\sqrt{2} \approx 1.41$` rendered as "α + β²", "∑ᵢ₌₁ⁿ aᵢ/bᵢ → ∞",
  and "√2 ≈ 1.41" in the final frame — while the same answer's
  "$HOME and $PATH" and "$5 to $10" stayed raw. No LaTeX markup in
  the final frame.

## The honest list is empty

Every item the tui-spec ever deferred has now landed as its own
phase, verified live: safe sanitization, /doctor, the themes
picker, /diff, and LaTeX conversion. The spec's "Not yet built"
section says so.

# Phase 37 — precise approval scopes (status: complete, live-verified)

Spec: [docs/specs/phase37-grant-scopes.md](specs/phase37-grant-scopes.md)
(The adoption doc's #1, canonicalized approvals.)

## Built

- **The over-broad grant, closed.** alwaysScope built the
  "don't ask again" rule from the command's first two whitespace
  fields — so a flag in second position produced a grant that
  approved every other value of that flag: approving
  `git -C /tmp push` granted `git -C:*`, which auto-ran
  `git -C /etc reset --hard`. Now the grant is never wider than
  the dialog shows: a plain program+subcommand stays a two-token
  prefix (`cargo build:*` covers `cargo build --release`); a flag
  in second position carries the whole command verbatim
  (`git -C /tmp push:*` matches exactly that and its longer
  forms).
- **Flag-synonym canonicalization** (`tools.ShellAllowlist`): a
  curated table of genuinely universal long/short pairs
  (--yes/-y, --quiet/-q, --force/-f, --verbose/-v, --recursive/-r)
  runs on both stored grants and checked commands, so a grant
  matches its flags written either way. Deliberately tiny: pairs
  that differ across tools do not merge (--all/-a is excluded —
  grep -a is --text, not --all); a synonym that merged two
  distinct flags would widen a grant past what the user read.
- Fail-closed matching unchanged: metacharacters deny,
  unterminated quotes deny, untokenizable grants are dropped.

## Verified for real

- `go test -count=1 ./...` — all 13 packages. New tests pin the
  security property directly: a grant from `git -C /tmp push`
  does not match `git -C /etc reset --hard` (the old code's hole)
  or `git -C /tmp status`, while still covering the approved
  command and its longer forms; synonyms match both directions;
  non-synonyms (--all/-a) do not merge; fail-closed unchanged.
- **PTY live**: a fixture asked for `git -C <proj> status` three
  times — approved with "always", the dialog showing the precise
  `git -C <proj> status:*` scope and the toast naming it; the
  exact same command re-ran with NO second prompt (the grant
  covered it); `git -C /etc status` prompted again (the grant
  did not widen). The sandboxed command auto-ran without any
  prompt in build mode, as designed — the dialogs are the
  unsandboxed escape, which is what the fixture exercised.

# Phase 38 — readable sandbox denials (status: complete, live-verified)

Spec: [docs/specs/phase38-sandbox-denial.md](specs/phase38-sandbox-denial.md)
(The adoption doc's #4.)

## Built

- `tools.Bash.Execute` classifies a sandboxed EACCES/EROFS: when the
  command ran sandboxed, the sandbox is active, and the combined
  output carries a denial signature (Permission denied / Read-only
  file system / Operation not permitted), a plain-language note is
  prepended — the write boundary named, "probably" honest about the
  ambiguity, the `{"sandbox": false}` escape spelled out, and the
  shell's original complaint kept below it. Non-sandboxed commands
  never get the note: their EACCES is a real permission error.
- The note applies on both the exit-error and swallowed-error paths
  ("|| true" can hide a denial behind exit 0); the timeout path
  returns unclassified.

## Found during verification — two real pre-existing bugs

The note is only useful if it reaches the model, and live PTY
verification showed a failing bash call reached the model as
"error: exit status: exit status 1" with NO cause at all:

1. **The gate zeroed tool output on error** — `result = ""` right
   after the audit entry had recorded it, contradicting the comment
   above it ("execution result is reported to the caller
   regardless"). Fixed: the output stays; the audit entry is
   unchanged.
2. **The orchestrator discarded the output of failing calls** —
   `result = "error: " + err.Error()`. Fixed: the failure result is
   the error headline plus the tool's own bytes, the evidence
   channel every tool failure needs, not just sandbox denials.

Both fixes have regression tests
(TestToolFailureKeepsOutput; the tools-package tests assert the
note and the kept original error end to end through the real
Landlock sandbox — the tools test binary gained the same
`__sandbox` re-exec TestMain the sandbox package's own tests use).

## Verified for real

- `go test -count=1 ./...` — all 13 packages. Tests cover the
  signature matcher and its near-misses; a sandboxed out-of-scope
  write carrying the note, the escape, and the original error
  through the real sandbox (skipped where Landlock is unsupported);
  an in-scope write with no note; an unsandboxed denial
  (chmod-built, host-independent) mislabeled by nothing; and the
  orchestrator's failure result keeping both headline and output.
- **PTY live**: a fixture asked for `echo probe > /etc/...`; the
  expanded tool result in the transcript shows the note, the
  named escape, and the raw `Permission denied` — and the file did
  not land.

# Phase 39 — the reasoning-effort knob (status: complete, live-verified)

Spec: [docs/specs/phase39-effort-knob.md](specs/phase39-effort-knob.md)
(The adoption doc's #5.)

## Built

- **The wire**: `ChatRequest.ReasoningEffort` ("" = provider
  default). OpenAI-compatible requests carry `reasoning_effort`,
  omitted when unset; Anthropic requests carry
  `thinking: {type: enabled, budget_tokens: N}` with opcode's
  mapping (low 1024 — the documented minimum, medium 8192, high
  16384), also omitted when unset — a model without extended
  thinking never sees the field.
- **The orchestrator** carries the effort per request like the
  permission mode (`SetEffort` switches it; mid-turn changes land
  on the next round). Subagents inherit the parent's live posture
  at spawn through `Runner.EffortOf` — deliberately no independent
  knob.
- **The config** seeds it: `reasoning_effort` in config.json,
  validated at load (unknown values fail loudly, like
  permission_mode), wired as the session's initial posture.
- **The TUI**: alt+. climbs the cycle (unset → low → medium →
  high → unset), alt+, descends; a toast names the posture, and
  the mode line gains the `◐ <effort>` segment (GlyphDoing, the
  effort glyph) whenever one is set — a dial you cannot see is a
  dial you cannot trust.

## Verified for real

- `go test -count=1 ./...` — all 13 packages. Tests cover both
  wires (the OpenAI field present only when set; the Anthropic
  budget per name, and the key absent from the wire when unset),
  config validation, the cycle in both directions with the footer
  segment appearing and disappearing, and the request provably
  carrying the posture (asserted on the request the fake provider
  receives).
- **PTY live, three turns around the cycle**: turn 1 sent no
  effort (the fixture echoes the wire's `reasoning_effort`:
  "none"); alt+. twice showed the "low" then "medium" toasts and
  the `◐ medium` footer segment, and turn 2's wire carried
  "medium"; alt+, moved the footer to `◐ low` and turn 3's wire
  carried "low". The toast, the footer, and the wire agree.

# Phase 40 — syntax-highlight guardrails (status: complete, verified)

Spec: [docs/specs/phase40-highlight-guardrails.md](specs/phase40-highlight-guardrails.md)
(The adoption doc's small-gems list, closed out.)

## Built

- `highlightLine` now refuses lines over 4 KiB before chroma ever
  sees them — the lexer is chosen from a model-provided file path,
  so the highlight input is untrusted, and rendering must never be
  hostage to a parser. One check at the function every highlight
  path funnels through (write_file results, edit_file hunks, diff
  source lines); the fallback returns the line unchanged, not
  truncated.

## The rest of the small-gems list, with dispositions

- **Sanitized terminal title** — already adopted (sanitizeTitle
  strips control and bidi characters and caps at 240 runes).
- **Turn diff budget** — no-op for opcode: edit results render the
  tool's before/after strings directly; there is no diff
  computation to cap.
- **One-shot screen-reader probe with a persisted marker** —
  skipped: opcode's detection reads environment variables, no
  terminal query; nothing expensive to remember.
- **Session-log recording behind an env var** — deferred: a
  development harness, not a product surface.
- **Session ids on events** — deferred: a field nothing consumes
  yet is padding; it becomes worth adding the day an async UI bug
  needs one.

## Verified for real

- `go test -count=1 ./...` — all 13 packages. The cap test pins:
  a small line highlights (SGR present under a color profile), a
  line one byte over 4 KiB renders byte-identical with no SGR
  anywhere, and the boundary is the cap itself.

## Note for future sessions: pushing from the sandbox

This agent shell runs inside the VSCodium flatpak, where the git
credential helper points at /usr/bin/gh — which exists on the
host, not in the sandbox. The push therefore fails from the
sandbox itself ("No such file or directory"). The fix, found
2026-09-29: run it on the host through the flatpak portal:

    flatpak-spawn --host bash -c "cd /home/chmgx81/Desktop/opcode && git push origin main"

The host's gh is authenticated; the same call also works for
`gh run list` to watch CI.

# Phase 41 — compaction: baseline accounting + the injection rule (status: complete, verified)

Spec: [docs/specs/phase41-compaction-baseline.md](specs/phase41-compaction-baseline.md)
(The adoption doc's #3, both subtleties.)

## Built

- **Window-baseline accounting.** Compaction triggered on absolute
  prompt size: a session resumed at 80% of the window compacted on
  its very next round, rewriting a conversation the user had not
  added anything to. The first reported prompt size of the session
  (or of the stretch since the last compaction) now anchors a
  prefill baseline; the trigger is GROWTH past the baseline
  reaching 75% of the window's REMAINING space. A heavy prefill
  shrinks the space growth is measured against but never triggers
  on its own; a baseline at or beyond the window means any growth
  triggers. After a compaction the baseline resets with the token
  signal — the compacted size anchors fresh growth, so nothing
  re-compacts instantly.
- **The injection rule, as an enum.** `InjectionPos`
  (`InjectRecapFirst` / `InjectRecapLast`): pre-turn compaction
  (round 0, history ending in the user's message) keeps the recap
  first and the user message last, where models are trained to
  find it; mid-turn compaction (after tool rounds, history ending
  in tool results) places the recap as the MOST RECENT history
  item — the end of the window is where attention lives. The old
  code always put the recap first, which put a mid-turn recap at
  position 0, the stalest possible position.

## Found while updating the tests

The three existing trigger tests used a single 800-of-1000 report
— exactly the pathology this phase fixes (a prefilled window
compacting on arrival), which is why they had to move to the
two-report growth pattern rather than just pass. And designing the
pre-turn test surfaced the real rule: a pre-turn trigger can only
come from a turn's FINAL report crossing the line, because a
mid-round crossing compacts mid-turn first — the test's first
draft assumed otherwise and failed honestly.

## Verified for real

- `go test -count=1 ./...` — all 13 packages. Seven compaction
  tests now: the prefilled window does NOT compact on arrival
  (800 baseline, 810 growth — nothing happens, history untouched);
  growth past 75% of the remaining space compacts with the recap
  LAST and the baseline re-anchored; a pre-turn compaction (the
  final report crossing the line) keeps the recap FIRST and the
  new user message last; the configured-model, disabled, and
  failure paths are unchanged.

# Phase 42 — the input decision as one typed place (status: complete, verified)

Spec: [docs/specs/phase42-typed-input.md](specs/phase42-typed-input.md)
(The adoption doc's #2, adopted now — the input model just grew
the effort knob and the queue, and the paths were spread across
submitInput and turnEnded.)

## Built

- `inputDecision` (internal/tui/input.go): six typed outcomes —
  inputIgnored (empty draft), inputShell (the user's "!cmd"
  escape), inputCommand (handled by the command system),
  inputQueued (held for turn end, Alt+Enter), inputSteered (a
  turn is running; folds in at the next round boundary), and
  inputTurnStarted (idle; a new turn runs).
- `decideInput` is the one router: today's submitInput body,
  branch by branch, each returning its outcome. `submitInput`
  became a thin wrapper, so no call site changed and the
  existing steer/queue/start tests pass untouched — the
  refactor's own acceptance criterion.
- The steer branch now carries the explicit contract as a
  comment: a steered draft cannot alter the active round (its
  request is on the wire); it applies at the orchestrator's
  round checkpoint. Deliberately NOT adopted: Codex's
  `expected_previous_turn_id` concurrency token — opcode's
  orchestrator has a single event loop with no concurrent turn
  writers to race.
- Documented in the spec: an unknown "/xyz" falls through to the
  model as ordinary text — existing behavior, preserved as-is.

## Verified for real

- `go test -count=1 ./...` — all 13 packages. The new test drives
  all six outcomes through decideInput (empty, !, /help,
  idle+text, working+plain, working+alt) asserting the typed
  value AND the observable effect; the full pre-existing TUI
  suite (steer, queue, start, palette, shell escape) passed
  unchanged through the wrapper on the first run.

## v0.3.0 released (2026-09-29)

Everything since v0.2.1, shipped: the display-boundary sanitizer,
/doctor, /theme, /diff, LaTeX math as Unicode, precise approval
scopes, readable sandbox denials (plus the failing-tool output
fixes), the reasoning-effort knob, highlight guardrails,
growth-based compaction with the injection rule, and the typed
input decision. CI green on the bump commit; the release pipeline
built all five platform binaries (1m14s); the raw.githubusercontent
install one-liner resolved v0.3.0 in a clean temp dir and the
installed release binary prints "opcode v0.3.0" — the -X
main.version injection verified in the real pipeline.

## Phase 43: production audit (2026-09-29)

Four read-only audit passes (security, concurrency, UX, slop) over
the whole codebase; findings triaged in
docs/specs/phase43-production-audit.md, then fixed in three
commits: the slop sweep, the security/concurrency batch, and the UX
batch. All deferred and rejected calls are named below — nothing
was silently dropped.

### Shipped — slop (commit: "the slop sweep")
Dead code out (pendingPick, ModeAllowsTool/ModeAllowsTier and their
constant-tests, Session.BranchIDs, unreachable plainMode branch,
pointless MCPNames copy), stale comments told the truth, /help
deduped, /quit added to the palette, --plain added to --help,
x.txt untracked, three version constants collapsed into the one
buildVersion feeds.

### Shipped — security + concurrency
- S3/S9: web_fetch refuses loopback/link-local targets and
  revalidates every redirect hop (OPCODE_ALLOW_LOCAL_FETCH opts out);
  config.IsLocalBaseURL parses the URL host instead of
  substring-matching "localhost".
- S4/S5/X21/X22: one shared mutex-guarded Redactor holds every
  auth.json value plus each live key; the gate is never forked, so
  subagents, audit log, and session saves all redact the same set.
- S6: write-bound checks resolve the full path including symlinks
  (Lstat on the base component too).
- S7: MCP children get an allowlist environment, not the parent's.
- C1: anthropic resp.Body closed on every path (closingReader).
- C2: compaction failure is a non-terminal EventCompactionFailed —
  a skip must not end the turn as EventError.
- C3: WaitIdle(2s) on both exit paths before saveSession, so the
  save cannot race a live turn (and a wedged provider cannot hang
  the exit).
- C4/C5/C6: grant state under grantMu; cancelTurn answers pending
  permission/plan prompts (quit can no longer wedge the dispatch
  goroutine); orchestrator per-request knobs under cfgMu with
  SetMode/SetEffort/SetProvider, and Gate.SetDecide for the policy
  swap — all three mid-turn writers are now synchronized.
- C9/C11/C12: denied-audit write errors surface in the returned
  error; headless returns the first JSON encode error; /models
  fails fast with "no key — /login <provider>" instead of a bare
  401 round-trip.

### Shipped — UX
U1 mkdir ~/.opcode before any error references it; U2/U3/U4/U5/U6
startup, key, config, resume, and provider errors now name the
next step (key note rides in StartupNotes instead of stderr);
U7 "exited with status N" instead of the doubled "exit status:
exit status N"; U9 /sessions reports skipped unreadable files;
U10 /skills and /mcp empty states say where skills and servers
come from; U13 help overlay documents ctrl+e, alt+./alt+,, and
steering; U15/X24 palette /quit; U17 README mkdir -p; U18 README
names the real tool (glob).

### Deferred (named, not hidden)
- S1 read-tier arbitrary paths, S2 bash network egress: design
  work beyond this phase (path allowlist design; seccomp/netns).
  Blunted by S4/S21: credentials cannot leak through logs or saves.
- S8 installer checksum, S11 clipboard PATH lookup, C7 process-
  group kill for bash grandchildren, C8 chatty-MCP buffer wedge,
  C10 LLM client hard timeout, C13 !-escape audit entry: each a
  self-contained follow-up; none is a data-loss or auth risk.
- U14 REJECTED: the ? guard is correct — users must be able to
  type "?" mid-composer.

### Verified for real
`go build ./...`, `go vet ./...`, `gofmt -l .` clean, and
`go test -count=1 ./...` green across all packages after each
batch, with the two exit-status wording assertions updated to the
new message.

## C7 shipped: bash timeouts kill the whole process group (2026-09-29)

The first deferred audit item, closed. The original finding was
"timeout doesn't kill grandchildren"; the regression test found the
deeper form: CombinedOutput cannot even RETURN while a backgrounded
grandchild holds the stdout pipe open, so a hung command with
background work wedged the agent loop indefinitely — the group kill
was unreachable code if it waited for the return.

- Children spawned through the sandbox now lead their own process
  group (deathAttr sets Setpgid) and WaitDelay bounds the pipe wait
  for anything that escapes the group.
- shell.go runs a watchdog: the moment the context dies (timeout
  or esc interrupt), sandbox.KillGroup SIGKILLs the entire group —
  bash and every process it spawned.
- Regression test TestBashTimeoutKillsGrandchildren: a command that
  backgrounds `sleep 300` then hangs must leave the grandchild dead
  within seconds of the timeout. Before the fix this test hangs
  forever; now it passes in 0.4s.

Full suite, vet, and gofmt green.

# Phase 44 shipped + full production review (2026-09-29)

Phase 44 (release hardening: races, CI/CD, updates) is committed in
full: the uncommitted work — `opcode update` with checksum
verification, the installer hardening, LLM timeouts, the MCP wedge
fix, seccomp network block, credential-file deny, `!` audit entries,
atomic config/session/trust writes, and the C7 process-group kill —
is in, with SECURITY.md, docs/releasing.md, dependabot, and the
installer test script.

On top of it, five parallel audit passes (UI/UX, logic, architecture,
security, docs/convenience) swept the whole tree; every finding got a
disposition — fixed below, or named as deferred. No silent drops.

## Fixed in this pass

- **Update availability (the known gap).** A stale binary had no
  in-product way to learn a release exists. Now: at most one cached,
  throttled (24h), silent-on-failure HTTPS check per day at startup;
  one startup note when a newer release is out (`Update available:
  vX → vY. Run `opcode update` to install it.`); the same cached
  check as a `/doctor` row; `opcode update [--check]` refreshes the
  cache; opt out with `"update_checks": false` or
  `OPCODE_NO_UPDATE_CHECK=1`; dev builds and platforms without
  prebuilt binaries never check. Tests: cache roundtrip/stale/
  silent-failure/dev-skip, doctor rows.
- **Trust-prompt type-ahead guard was dead** (OpenedAt never set):
  set at construction.
- **Two version truths**: the TUI rendered its own const while
  --version/update used buildVersion. Options.Version injects the
  real one; the const is the tests-only fallback.
- **SkillsIndex data race** (trust grant vs live turn): SetSkillsIndex
  under cfgMu; systemPrompt documented as call-under-lock.
- **Unknown /commands billed a model turn**: in-place error with
  prefix/substring/edit-distance suggestion, turn never starts.
- **Headless --json leaked secrets** (no redaction): Redact wired
  from the session redactor. **history.jsonl unredacted**: redacted
  on save.
- **Write/edit/apply_patch reached the credentials file with a nil
  Decide**: Execute-level deny + guarded reads; apply_patch also
  refuses traversal and absolute-outside-roots paths (absolute
  in-roots paths still work — the end-to-end test proves it).
- **web_fetch SSRF**: private LAN ranges blocked; DNS-TOCTOU stated
  as the approval gate's job; OPCODE_ALLOW_LOCAL_FETCH documented in
  SECURITY.md.
- **Trust fingerprint missed the instruction surface**: SKILL.md,
  references/, assets/ are trust-visible now (test updated).
- **LLM edges**: index-less tool fragments join a solo call;
  Anthropic tolerates `data:` without space and flushes open
  tool_use at EOF (tests for all three).
- **Bash timeout misattribution**: parent-context deadlines no
  longer report as the 5-minute timeout.
- **install.sh**: --help/no-args guard; plaintext bases refused
  except loopback (test mirror keeps working); success screen names
  `opcode update`.
- **UX copy**: /doctor skills+MCP rows name the next step; empty
  login/logout are dim notes, not errors/successes; bare `!`
  explains itself; dead paste tokens warn visibly; help overlay
  covers shift+enter/pager keys/`?` precondition; picker details
  fit narrow terminals; plain posture forces the static spinner.
- **Docs**: README modes table is plan/build/full-auto with legacy
  mapping, tool list gains web_fetch, Also block gains
  `opcode update` + env table; architecture §4 lists safe/update,
  modes fixed, project-config promise corrected (fingerprinted,
  never loaded); tui-spec keys/slashes/behavior current, v0.3.0
  figure; releasing.md documents the notice; .gitignore covers
  dist/archives; release.yml comment honest about CI vs gate;
  DefaultPermissionMode is canonically "build"; stale test comment
  fixed.

## Deferred (named, not hidden)

- S1/S2 full designs (path allowlist; seccomp/netns beyond
  x86_64): blunted by credential deny + redaction + approval gate.
- config→tools import (mode constants): one-directional today, no
  cycle; a leaf package is the fix when tools next needs config.
- tools/ + tui/ package splits (perm leaf; prompt/picker files):
  correct direction, next-touch refactor, not this pass.
- run() helper extraction (triplicated key resolution): works,
  tested; refactor with a compiler present, not blind.
- MCP chatty-server wedge buffer, LLM hard total-timeout, `!`
  non-shell forms: unchanged from the phase-43 dispositions.
- PROGRESS Phase 24 entry still links the merged tui-ux-spec.md
  filename: history, left as the consolidation section documents
  the rename.

## Not verified in this environment

No Go toolchain is installed in this sandbox (`go`, `gofmt`
absent; none on the host portal either), so `go build/vet/test`
could not run here. Every change was reviewed diff-by-diff for
compile safety (imports, signatures, test helpers), and every
behavioral fix carries a test — but CI (`go vet`, `gofmt`,
`go test -race -count=1 ./...`, cross-builds, installer tests,
govulncheck) is the honest gate before tagging. Do not cut a
release on red CI.

## Pre-release review pass (2026-10-01)

Full pass against a built binary, not just the suite: a throwaway
fake OpenAI-compatible server and a pty driver exercised first run,
the approval dialog, `/doctor`, narrow terminals, `--plain`, headless
text and `--json`, fail-closed permissions, `--continue`, and a
provider that is down, 500s, sends HTML, or cuts the stream.

Found and fixed:

- `opcode -p ""` fell through and opened the interactive UI. It is now
  a usage error.
- `opcode` with stdin or stdout not a terminal painted escape codes into
  the pipe and died on `/dev/tty`. It now says the UI needs a terminal
  and points at `-p`. (`checkLaunchMode`, tested.)
- A model reply with no text and no tool calls ended the turn silently
  and, on the Anthropic wire, recorded an empty assistant message that
  poisons every later request. It is now `ErrEmptyResponse`, kept out
  of history, and the next message works. A subagent in that state
  still returns its explicit no-answer result.
- `--plain` still drew `•` for markdown bullets; the a11y test now
  covers lists, and was confirmed to fail without the fix.
- Dead code from staticcheck (unused `latexFuncs`, two test helpers, a
  dead assignment, two struct-literal conversions).
- `TestCancelledTurnReportsErrCancelled` raced `cancel()` against a
  finishing turn and failed intermittently on the untouched baseline;
  it now cancels before sending.
- `install.sh` and the scripts had lost their executable bit in the
  working tree.

Verified: gofmt, vet, `go test -race -count=1 ./...`, staticcheck,
govulncheck (0 reachable), installer tests, doc-link check, and
cross-builds for linux/arm64, darwin/{amd64,arm64}, windows/amd64.
Not verified: macOS and Windows runtime behavior (CI covers macOS
tests; Windows is build-only), and a real provider (the fake server
speaks the OpenAI wire only).

## CI red on the first push: three portability and honesty bugs (2026-10-01)

The push above went red. All three failures were pre-existing, and
none of them was visible on this Linux machine — they only surface on
the macOS runner or under a resolver that is not glibc's.

- **`internal/tools`: a new file through a symlinked directory was
  refused.** `resolveLikeKernel` counted a link target's outstanding
  components only when it Lstat-ed them, so a target ending in `..`
  (or one whose ancestor is itself a symlink) left the count above
  zero and every later missing component read as a dangling link. On
  macOS `/var` is a symlink, so the project's own temp dir hit it.
  The count now drains as each target component is consumed, `..`
  included. Covered by two new tests, and the existing dangling-link
  bypass tests still refuse what they must.
- **`internal/sandbox`: a requested sandbox on an unsupported
  platform said "off".** `New` folds the config's request together
  with the platform's capability, so `Status()` could not tell "you
  asked for off" from "you asked for on and this machine cannot".
  The runner now keeps both, and the startup line says `unavailable`
  for the second — which is what a macOS user with `"sandbox": true`
  was actually seeing. New test covers it on any platform.
- **`internal/tools`: the web_fetch opt-in test needed the machine's
  resolver.** It fetched `http://127.1:<port>/`, a valid IPv4 form
  that glibc resolves and the macOS runner does not, so the test
  failed there for a reason that had nothing to do with opcode. The
  address and `localhost` still cover the opt-in; a new test checks
  that the flag opens both the literal-host and the dial-time check.
- **`TestWritableRoots` compared a path to its real path.** macOS
  spells TMPDIR `/var/folders/...` and the real path is
  `/private/var/...`. The test now compares through the same symlink
  resolution the gate uses.

Two documentation bugs, both found by an audit pass:

- The architecture spec claimed MCP spoke `stdio or HTTP`; the code
  rejects an `http://` server at config load. The living doc now says
  stdio only and says what happens to an http entry.
- `/update` was implemented, dispatched, and listed in the palette and
  help sheet, but absent from the spec's command list and the README.
  Both now list it, and a new test (`TestSpecCommandListMatchesCode`)
  compares the spec's enumerated list against the `commands` var in
  both directions, so the prose cannot drift from the code again. It
  was confirmed to fail when a name is removed from the list or added
  to it.

## The Go floor is a security floor (2026-10-01)

`go.mod` now says `go 1.25.13` instead of `go 1.25.0`. All three
govulncheck findings that made CI red were standard-library bugs fixed
in 1.25.13 — GO-2026-6218 (quadratic `net/url` resolvePath), GO-2026-6090
(post-handshake TLS messages) and GO-2026-6088 (encoding/xml decode
depth) — and opcode reaches all three: every provider request, the update
check, and markdown rendering through glamour's XML lexer. There is no
code change that fixes them; the patch release is the fix.

This is a build-floor change with a real cost: anyone building opcode from
source now needs Go 1.25.13 or newer. That is the intended trade — a
vulnerable dependency floor is not a floor. CI reads the version from
`go.mod`, so every job, including the cross-builds, moves with it.

## The macOS installer job: seq, of all things (2026-10-02)

The macOS job failed on "fake release server did not start" with no
Python error at all. The server was fine; the wait around it was not.

Line 139 was `for _ in $(seq 1 50)`. `seq` is GNU coreutils, and macOS
ships the BSD userland, where the command does not exist. Under `set -e`
a command substitution that fails inside a `for` list prints "command not
found" and then runs the loop body zero times, so the script checked for
the port file with no wait, found nothing, and reported the server as
broken. The message pointed at the wrong thing entirely.

Replaced with a POSIX `while` counter, so the script waits on every
platform. The failure branch now also prints the port file's path and
whatever the server wrote to stderr, because "did not start" is not an
actionable message and that is what made this slow to find.

Verified by reproducing the macOS condition on Linux: with a PATH that
omits `seq`, the old script fails with exactly the message CI reported
and the new one passes 48/48. `install.sh` itself was checked for the
same class of bug and is clean. Both scripts pass shellcheck.

Also in this pass: history cleanup. Seven `diag:` commits and a
`scripts/diag-server.sh` had been pushed to main while chasing this
(several of those diagnostic attempts were themselves broken — bad YAML
quoting, and a `timeout` that does not exist on macOS). They were
dropped from history and the temporary script and CI step deleted, so
main carries only the three real fix commits. The code was re-verified
after the rewrite: gofmt, vet, the full race suite, the installer test,
and the doc-link check all pass.

## The real cause: socket.getfqdn, a reverse DNS lookup (2026-10-02)

The macOS job's fake release server never wrote its port file. The
earlier entry blamed `seq` and that was a real bug, but not this one —
fixing it made the wait honest and the server still never appeared.

Finding it took a step-marked copy of the server pushed to the runner,
because the symptom was maddeningly quiet: the process stayed **alive**,
wrote nothing to stdout or stderr, and the port file never appeared. The
marks narrowed it precisely — `class ready` printed, `constructed` never
did, so the stall was inside the `ThreadingHTTPServer(...)` constructor.

That constructor calls `HTTPServer.server_bind`, whose last line is
`self.server_name = socket.getfqdn(host)`. `getfqdn` is a **reverse DNS
lookup**. On a machine with no resolver to answer it — which is what the
macOS runner is — the constructor blocks for minutes, long past any
startup wait, holding the port unwritten. Nothing raises, which is why
the log was empty every time.

Fixed by serving from a plain `ThreadingTCPServer`, which does the same
socket setup without the reverse lookup. The only thing lost is
`server_name`, which nothing in this test reads.

Verified on Linux by making every reverse lookup hang, which is the
macOS condition: the old server stalls with no port, the new one writes
its port immediately and answers HTTP 200, and the full installer test
passes 48/48 under that shim while the old code fails with exactly the
message CI reported. The temporary probe script and its CI step are
removed.

## Real providers, real keys (2026-10-02)

Every test until now ran against a fake OpenAI-compatible server. This
pass ran the built binary against three real providers using the
maintainer's own keys, headless and in the TUI, to close the last gap
before a release:

- **OpenRouter** (the default provider): plain turn, tool loop, `--json`
  events, and a reasoning model (`nemotron` free) in the TUI — reasoning
  streamed and collapsed to "thought for 7s · 727 chars", the tool ran,
  and when the free pool 502'd mid-turn the transcript carried Nvidia's
  own words ("ResourceExhausted ... request limit reached") instead of a
  bare status.
- **Mistral** (catalog provider, key from the env): plain and tool turns,
  1-2s each.
- **Nvidia** (catalog provider): plain and tool turns on
  `meta/llama-3.2-90b-vision-instruct`. Several other model ids 404 with
  a per-account catalog error; opcode surfaced it honestly.
- **Secrets**: after real turns, the raw key appears in **zero** files
  under `~/.opcode` — session saves and the audit log are redacted with
  real credentials, not just in tests.
- **Anthropic native client**: still no key in this file for it, so it
  remains covered by unit tests only, not a live call. Stated plainly.

Two error-copy gaps the real bodies exposed, both fixed with tests from
the actual responses:

- OpenRouter answers rate limits with a generic top-level message and
  the useful text in `{"metadata":{"raw":...}}`. The transcript now
  shows "temporarily rate-limited upstream, retry shortly" instead of
  the content-free "Provider returned error".
- Nvidia-style bodies `{"status","title","detail"}` have no message
  field; the transcript now shows the `detail` line instead of the whole
  envelope.

Live check after the fix: the same gemma request that printed
"Provider returned error" now prints the full upstream message naming
the remedy. `gofmt`, `vet`, the full race suite, and staticcheck pass.

## The UI is now the whole setup (2026-10-02)

The UX question that prompted this pass: can someone who will never
edit a JSON file configure opcode? Two answers were no, and both are
fixed.

- **First run used to be a dead end.** With no config.json, main
  exited with "set \"model\" in ~/.opcode/config.json" — the one user
  who most needs the UI never reached it. The interactive UI now
  starts: a three-step welcome (provider, key, model) with the
  provider picker already open — every catalog provider listed, each
  row saying whether a key is needed. Headless keeps the error; a
  script has nobody to answer a picker.
- **A model picked in the UI used to evaporate on restart.** /model
  and /models switched the session's provider and model in memory
  only, while /login persisted its key — so a first-run user who set
  everything up through the UI found the model silently reverted.
  switchModel now writes config.json ("model") and models.json
  ("default_provider") through the same raw-edit path as /theme:
  unknown keys survive, symlinks and permissions are preserved, and a
  missing models.json is created. A save failure never undoes the
  switch; the note says the choice is for this session only.
- Sending with no model no longer 400s at the provider: the send is
  refused with the reason and the picker opens. The header shows
  "no model yet" instead of an empty slot, and /doctor's row points at
  the picker instead of the file.

Verified live, end to end, with a real provider: first run shows the
welcome and the picker; esc + a prompt hits the guard and reopens the
picker; /models → openrouter → live list → pick writes both files;
relaunch shows the model in the header with no welcome. Two existing
tests had been building the TUI with no Model while the orchestrator
had one — the guard caught the inconsistency, and the fixtures now
agree.

The scroll mechanism was examined and left alone by design: finished
turns commit to the terminal's native scrollback (tea.Println, no
mouse capture, no alt-screen), so wheel and shift+pgup are the
terminal's own scrolling; ctrl+o opens the in-app transcript with
↑↓/pgup/pgdn; overlays (help, sheets) each own their scroll keys. That
is the right shape and changing it would be churn.

# Phase 45 — the UI restyle: one box language (status: complete, verified)

Phase 45 ([specs/phase45-ui-restyle.md](specs/phase45-ui-restyle.md)):
the composer joins the box language every dialog already spoke, and
the greeting's identity becomes one line.

- **The composer is a box.** Every floating surface — dialogs,
  pickers, the palette, the help sheet — drew the rounded
  `dialogBorder` box; the composer alone sat between two full-width
  `─` rules, the boldest horizontal structure on the screen, with no
  side edges on the input. It now draws the same box: the boundary
  token at rest, amber in shell mode (the signal the rules carried),
  ASCII `+-|` under --plain through the same `dialogBorder()` switch,
  and nothing below fourteen columns — a clipped border is worse
  chrome than none. Same two rows of chrome, real edges. `GlyphRule`
  left the vocabulary (nothing renders it), so `adaptGlyphs` and the
  a11y glyph table lost the entry.
- **The row accounting became honest, and that found a bug.** The
  old composer appended the multi-line textarea as one element, so
  the frame's room budget undercounted it by a row; the box splits
  into real rows, the budget is exact, and the honest count exposed
  `floatingBlock`'s "room == 0 means unset" fallback granting
  overlays six rows a full composer had already spent — a frame
  taller than the terminal, the exact corruption the package exists
  to prevent. Zero room is now a real answer: the block renders
  nothing, which is the @-mention menu's documented behavior on an
  eight-row terminal.
- **The greeting says who, what, where on one line.** Model · mode ·
  cwd joined beside the mark — the lockup reads as a sentence
  instead of a form.
- **tui-spec.md caught up with the rebrand** in the same change, per
  its own rule: the brand glyph rows said `~` where the code has
  drawn `◈` since v0.5.0 (header, composer prompt, exit line), and
  the composer and greeting sections now match what renders.

Verified: `go build`, `go vet`, and the full
`go test -race -count=1 ./...` suite pass; `TestComposerBoxFrame`
locks the box, its plain posture, and the narrow bare fallback;
`TestFrameFitsEveryStateAtEveryWidth` holds the 8–120 column sweep
with the box in every state, including the two-line-draft case that
the honest accounting fixed.

# Phase 46 — the palette and the lists (status: complete, verified)

Phase 46 ([specs/phase46-palette-and-lists.md](specs/phase46-palette-and-lists.md)):
the redesign's missing halves — the colors and the list surfaces.

- **A violet identity.** The teal default was Tailwind's, not
  opcode's. `dark` is now `#a78bfa` on violet-tinted surfaces
  (`#2a2732` / `#1f1c26`) with a sky info (`#7dd3fc`); `light` is
  `#7c3aed` on `#ede9f5` / `#f6f4fa`. The semantic tokens (success,
  danger, warning, muted, subtle, border) are unchanged — they are
  contrast-locked meaning. The old teal default became a named
  theme, `teal` (the courtesy `green` got), so no choice is lost.
- **One selection language for every list.** The pickers, the
  command palette, and the @-mention menu draw their rows through
  one composer, `menuRow`: the selected row is an accent band
  (the caret still marks it — the band is emphasis, never the only
  signal) and the label column is a fixed gutter, so the details
  align into a column instead of a ragged second word. A new
  `HexOnAccent` token holds the band's text, per theme.
- **Both bars held.** Every new value was checked before landing
  and is held permanently: text tokens 4.5:1 against the floor and
  the code panel, border 3:1, on-accent 4.5:1 against the band.
  `TestThemeContrastIsLegible` grew the teal floor and the band
  bar; `TestMenuRowSelectionBand` locks the band, the caret, the
  aligned column, and an 8–120 column width sweep — the sweep that
  caught the first version's wrapped rows (a fixed gutter at eight
  columns made one item three rows and broke the frame budget; the
  row now drops the detail below the width where the gutter fits).

Verified: `go build`, `go vet`, `gofmt -l .` empty, and the full
`go test -race -count=1 ./...` suite pass; hardcoded SGR assertions
in `tui_test.go` and `diff_test.go` moved to the new hex values;
README badges carry the new accent.

# Phase 47 — the mode picker and the first-run flow (status: complete, verified)

Phase 47 ([specs/phase47-modes-and-first-run.md](specs/phase47-modes-and-first-run.md)):
the mode UI and the journey from "no config" to "first prompt sent".

- **`/mode` is a picker, not a sentence.** Bare `/mode` printed a dim
  line; it now opens the same component `/theme` uses — one row per
  posture, each carrying the README safety table's own sentence,
  the active one marked, Enter switching through the exact path tab
  uses. An unknown `/mode <name>` opens the picker on top of its
  error: the list IS the answer to "what is valid". The mode line
  wears its own color — plan in info blue, build in the brand
  accent, full-auto in amber — with the glyph still differing, so
  color is never the only signal.
- **`/model` is the hub.** It read models.json only, so the first-run
  user who had just stored a key was told nothing was configured —
  a dead end one step from the finish. It now lists what a user can
  reach: models.json choices first (active marked), providers with
  a resolvable key whose live list is one enter away, then keyless
  providers, each naming `/login <name>` — enter starts exactly
  that. The journey the brief described, minus the typing.
- **Continuity, not luck.** `/login`'s success now fetches that
  provider's live model list itself, so the chain is key → models →
  pick with no command to remember in between. This required fixing a
  real gap: the resolvers hold auth.json's startup snapshot and
  never see a later write, so a key stored this session was
  invisible to every picker and fetch until a restart. The session
  keeps its own copy (`storedKeys`), `/logout` forgets it, and the
  pickers, fetches, and pre-flight resolve through it.
- **The error before the request.** Sending with an active provider
  that has no resolvable key used to wait for a doomed 401. The
  pre-flight refuses the turn and names `/login <provider>`;
  switching to an unkeyed provider says so at the switch, with the
  same fix.
- **The welcome reads like a product.** The three-step first-run
  copy names what each step does and which commands work later.

Verified: `go build`, `go vet`, `gofmt -l .` empty, and the full
`go test -race -count=1 ./...` suite pass. New tests: the mode
picker's rows, marks, and select path (`TestModePicker`); the hub's
ordering and both row actions (`TestModelPickerIsTheHub`); the
login → fetch → catalog chain with the session key
(`TestLoginContinuesToModels`); the switch warning
(`TestSwitchToUnkeyedProviderWarns`); and the pre-flight refusing
the turn with zero requests billed (`TestSendPreflightsTheKey`).

Found live on a first run (the report that closed the loop): picking
a keyless provider in the auto-opened browse picker fired the fetch
anyway and failed with "no api key for mistral — /login mistral…",
stranding the user at an empty composer one step into the journey.
The row named the command; Enter now does it — a keyless row in any
provider list (`/models`, the first-run picker, the `/model` hub)
starts that provider's login, and the login's success carries the
journey to the model list on its own.
`TestKeylessProviderPickStartsLogin` locks the path, and the
all-rows-fetch assertion in `TestModelsPickerListsAllProviders`
became the per-row contract: keyed and local fetch, keyless logs in.

Found live (the second report): the greeting's identity line said
`build` while the footer said `⏸ plan`, and the reader cannot know
which is true. Nothing flips modes at launch — the `✓ mode switched`
line in the report is the toast working — the flaw is that the
greeting is a frozen launch snapshot (committed scrollback cannot
re-render) carrying the mode, a live dial that changes one keypress
in. The mode left the identity line: the footer is its one true
home, always current. `model · directory` is what the snapshot can
honestly say. The empty-model-slot check in
`TestFirstRunOpensOnboardingPicker` had been passing vacuously
(probing `View()` for a mode segment the frame may have trimmed
away with the whole greeting to fit the open picker); it now reads
the header entry and asserts the placeholder directly.

# Phase 48 — the context readout (status: complete, verified)

Phase 48 ([specs/phase48-context-readout.md](specs/phase48-context-readout.md)):
the working line now shows how full the window is.

- **The readout.** The parenthesized segment carries
  `context 62%` — the most recent round's prompt tokens over the
  configured window — so the approach to a compaction is visible
  before the recap lands. The TUI tracks the last round's prompt
  count (`lastPrompt`, persisted across turns, cleared on resume)
  and reads `Options.ContextWindow`, wired from the same
  `context_window` config the compaction trigger uses.
- **Silent rather than guessed.** No window configured — the
  default — no readout. Model windows vary and a number invented
  for one would be a lie for another. The readout is also the
  segment's refinement: a narrow terminal drops it before the
  elapsed time.
- **No compaction warning color, deliberately.** The trigger is
  growth past 75% of the space above the session's baseline (Phase
  41's window accounting), which raw occupancy does not predict; a
  red number firing at the wrong moment would be a lie in accent
  clothing. The number tells the story, the recap note tells the
  ending.

Verified: `go build`, `go vet`, `gofmt -l .` empty, and the full
`go test -race -count=1 ./...` suite pass.
`TestWorkingLineContextReadout` holds all three behaviors: no
readout without a window, the percentage with one, and the reflow
dropping occupancy before elapsed time.

# Phase 49 — scrollback that scrolls (status: complete, verified)

Phase 49 ([specs/phase49-scrollback.md](specs/phase49-scrollback.md)),
found live (the third report): a long first answer filled the live
region, the frame trimmed its start to "… 24 earlier lines", and
the terminal's own scrolling had nothing to show — a finished turn
only committed to native scrollback when the NEXT turn started.

- **A finished turn commits when it finishes.** `turnEnded`
   commits through `tea.Println` the moment the turn completes, so
   after every answer the terminal's own scrollbar works — the
   reader's native way back, never a turn behind. The queued
   follow-up path already committed there; the two paths agree
   now. The frozen-text contract is unchanged: what's printed no
   longer redraws, and the last turn's ctrl+r expansion moves to
   the pager, which always expanded everything.
- **The trim marker names its escape.** "… 24 earlier lines"
   counted the loss and named no way to read it — the one dead
   end left in a UI whose rule is that every empty state says its
   way forward. It reads "… 24 earlier lines — ctrl+o to read"
   now, and the dash degrades under --plain like the rest of the
   chrome. The marker now appears only while a turn is in flight.

Verified: `go build`, `go vet`, `gofmt -l .` empty, and the full
`go test -race -count=1 ./...` suite pass — the commit tests held
the mechanics (rhythm, boundary renders, View skipping committed
entries) and needed no timing changes.
`TestFinishedTurnCommitsAtTheEnd` holds the new boundary;
`TestTrimMarkerNamesTheEscape` holds the named escape.

The ctrl+r half of Phase 49's tradeoff is addressed: committed text
cannot re-render, so ctrl+r with an empty live region opens the
transcript pager — the one view that expands everything — and
closes it again (the pager's close keys gained ctrl+r; the key is a
toggle everywhere it lands). The frozen "(ctrl+r to expand)" hints
already sitting in scrollback stay true: the key still shows the
expanded content, in the overlay instead of the frozen bytes. The
theme-repaint half is physics and stays documented: printed bytes
belong to the terminal, and the pager is where old turns wear a
new palette. `TestCtrlRKeepsTheFrozenPromises` holds both paths.

# v0.6.0 tagged (2026-10-02)

Phases 45–49 and the five live-found fixes, released as one story:
the composer joins the box language every dialog speaks; the violet
identity (teal kept as a theme); one selection language across
every list; `/mode` as a picker with per-mode color; `/model` as the
hub and the first-run journey continuous — key → models → pick; the
context readout on the working line; and scrollback that scrolls —
a finished turn commits the moment it ends, the trim marker names
the pager, and ctrl+r keeps the frozen hints' promise.

Local pre-flight for the tag: `go mod tidy` clean, `gofmt` empty,
`go vet` clean, the full `-race` suite (15 packages, 573 tests),
`scripts/test-install.sh` (48 checks), and the doc link check.

# The full-stack review (2026-10-02, post-v0.6.0)

Four parallel review passes — security (against the
secure-code-review skill methodology), end-user experience,
reliability and scalability, and maintainability per AGENTS.md.
Every finding was verified against the code before anything was
changed; the maintainability pass found the codebase honest (docs
earn their "re-checked against the code" claims — one exception,
fixed) and the security pass confirmed more claims than it broke.

**Fixed, in this order of severity:**

- **The session autosave was a lie.** The UI said "opcode saves a
  session when a turn ends"; nothing saved between launch and exit,
  and a crash lost the whole conversation. The session file is now
  held for the process's life and rewritten atomically at every
  turn boundary; resuming switches the pointer so continuing a
  session grows that session's own file; and an empty conversation
  writes nothing — so an accidental empty launch can no longer
  shadow the real latest for `--continue`. `--continue` also walks
  past a corrupt newest session instead of dying on it, naming
  every file it skipped. (`TestTurnEndSavesTheSession`,
  `TestLatestReadableWalksPastCorruption`)
- **The write path lacked the open-time bound the read path has.**
  `openReadable` checks the opened descriptor by inode because a
  sandboxed command can swap a symlink under a gate-time check;
  writes had no equivalent — `os.WriteFile`, following whatever the
  filesystem said. Every in-process write now goes through
  `writeGuarded`: `O_NOFOLLOW` on the final component, a refusal
  for files with more than one hard link (the Landlock ABI 1 move),
  and an exec-time revalidation of the writable roots.
  (`TestWriteGuardedRefusesSwappedTargets`)
- **An interrupt discarded a completed tool's evidence.** A tool
  interrupted mid-run may have partially applied; its partial
  output was dropped and the history recorded a generic
  "interrupted" note. The output rides along now — the model is not
  told a write that happened did not.
  (`TestInterruptedToolKeepsItsPartialOutput`)
- **ctrl+c was swallowed in every modal.** The picker's own comment
  claimed it quits; the code returned handled for every key. The
  quit keys now route through one `quitKey` before any layer can
  eat them — arm, announce, second press exits — verified in the
  picker, the pager, help, and the permission dialog.
  (`TestQuitKeysWorkInEveryLayer`)
- **Smaller truths:** "no image on the clipboard" no longer lies on
  a Mac without pngpaste (the error names the install); the
  empty-model-list advice no longer points at a `/model <name>`
  that refuses the name; `--json` without `-p` errors instead of
  silently opening the TUI; `switchModel` goes through the
  cfgMu-guarded setters; `/quit` joined the README's command list.
- **Docs:** PROGRESS's "no config→tools import" claim (false — the
  import exists, the claim now describes the real one-way edge);
  the architecture log's stale "Phase 0 → 44"; a superseded-note
  on the phase45 spec; stale version constants; `.commandcode/`
  ignored.

**Deliberately deferred (named, in PROGRESS.md "Still open"):**
provider retry/backoff, a per-server MCP tool-call timeout, a
"context is getting large" warning without a configured window,
mid-session skill-script re-fingerprinting, transcript memory
pruning, and the `run()` split — each is design work with real
trade-offs, not a gap to paper over.

Verified: `go build`, `go vet`, `gofmt -l .` empty, and the full
`go test -race -count=1 ./...` suite pass — 579 tests.

# The website (2026-10-02)

`site/` — a zero-dependency static site: plain HTML and one
stylesheet, no generator, no JavaScript, no cookies, no tracking.
Deployed to GitHub Pages by `.github/workflows/site.yml` on every
push to main that touches it; Pages serves the `site/` directory
as-is.

The landing page leads with the one-line install, the honest
differentiators (kernel sandbox, fifteen providers with your key,
one binary and no account), and the feature blocks this category's
products headline — plan mode, subagents — shown as real terminal
mockups of opcode's own output, not invented screenshots. The docs
section mirrors the living documents, and every page carries an
"edit on GitHub" line pointing at its source, so the site cannot
quietly drift from the code. Deliberately absent: pricing tables
(there is nothing to price), testimonials and logo walls (there is
no social proof to show, and fabricating it would break the no-slop
rules), and any claim the code does not meet — the headless page's
first draft invented a `--mode` flag; it was caught and replaced
with the real config-based mechanism before commit.

Verified: every internal link resolves (a Python HTMLParser pass
over all seven pages), the stylesheet is site-root-relative so
GitHub Pages serves it under the docs subpaths, and the deploy
workflow uses only first-party actions.

Found live (the fourth report): the site rendered unstyled — every
link was site-root-absolute (`/assets/style.css`), and a GitHub
Pages project site serves under the `/opcode/` subpath, so the
browser fetched `chmgx81.github.io/assets/style.css` and got a 404.
The link check that passed had verified filesystem paths, not served
URLs — the wrong check, so a false green. All links are relative
now, per-file, and the check resolves every href at its served URL
under the base path, which is the check that actually models the
deployment.

# The site rebuilt on a real stack (2026-10-02)

The hand-rolled site answered "does it work" but not "does it look
like a product". Rebuilt as an **Astro + Tailwind CSS v4** project
in `site/` — the current standard for dev-tool sites: component
model (`Terminal`, layouts), Tailwind's token system for the visual
language, self-hosted Inter and JetBrains Mono variable fonts via
fontsource (no Google Fonts request — the no-tracking claim stays
true), zero client JS except a 20-line reduced-motion-aware scroll
reveal whose hidden state exists only when JS runs.

Design: the violet identity carries over (#a78bfa accent on a
deeper #0b0d12), gradient hero text, radial glow and grid backdrop,
terminal-window components with traffic-light chrome rendering
opcode's own output, cards with hover lift, native details/summary
FAQ accordions, docs on the typography plugin with a sticky sidebar.

Two build lessons recorded: npm writes dependencies to package.json
only after a full install resolves, so the first attempt's 300s kill
left an empty lockfile that every later "up to date in 189ms"
faithfully installed as nothing; and Astro treats a literal `{` in
the template as expression syntax, so JSON samples live in
frontmatter strings. Verified: `astro build` produces 7
directory-style pages with every internal link carrying the
/opcode/ base, the served-URL link checker passes over `dist/`, and
no page requests a third-party font or CDN. The deploy workflow
does `npm ci && npm run build` on Node 22 and publishes `dist/`.
