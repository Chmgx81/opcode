# What tilde should take from Codex

> Synthesis of the three Codex audits
> ([tui](codex-tui-audit.md), [core](codex-core-audit.md),
> [features](codex-features-audit.md)) against tilde's current
> state (Phases 0–28 in the build log). Ordered by value-to-effort;
> each item names what tilde has today, what Codex does, and the
> honest scope of adopting it. Nothing here is a commitment — it
> is the menu, with the reasoning.
>
> Method note: the audits are static reads of Codex source; nothing
> was executed. tilde's side is stated from its own code.
>
> **Status since writing:** this has not been re-synthesized past
> Phase 28, so "Adopt" below means *adopted in principle*, not
> *not yet done*. Six items did land, as Phases 37–42 — each of
> those specs opens by naming the item it implements. See
> [README.md](README.md) for the mapping. Treat the remaining
> entries as an unworked menu.

---

## Adopt — high value, clear fit

### 1. Approval cache keyed by canonicalized command
tilde has: session prefix grants (Phase 27) matched by raw
token-prefix. Codex does: `ApprovalStore` keys decisions by
canonicalized approval JSON plus `command_canonicalization.rs`, so
"always allow `cargo test`" matches `cargo test --quiet`
(codex-core-audit §4.8). tilde's grant matching normalizes nothing —
`npm init -y` vs `npm init --yes` are different commands to it.
Adopt: a canonicalization pass (flag normalization, whitespace,
trailing-arg merging) before prefix matching. Small, self-contained,
closes a real usability gap.

### 2. The steering decision as one typed place
tilde has: Enter steers, Alt+Enter queues — the logic is spread
across submitInput and turnEnded. Codex does: one
`turn_input.rs` that decides start/steer/reject with typed
`NotSubmittedReason`s, including an
`expected_previous_turn_id` optimistic-concurrency token
(codex-core-audit §2.2, §4.2). tilde's queue and steer paths work,
but the "why didn't my message submit" answers are implicit. Adopt
when the input model next changes: a single decide function with
typed outcomes. Also Codex's distinction that steered input cannot
alter the active turn's context (it applies next round) matches
tilde's behavior — worth making explicit in a comment or type.

### 3. Compaction: window-baseline accounting + the injection rule
tilde has: compaction with a ContextWindow threshold and a
CompactionModel (Phase 14-era). Codex does:
- `BodyAfterPrefix` scope: only tokens added since the window's
  prefill baseline count toward re-triggering (§2.9) — a heavily
  prefilled window doesn't instantly re-compact.
- `InitialContextInjection::{DoNotInject, BeforeLastUserMessage}`
  (§2.8): mid-turn compaction must place the summary as the last
  history item because models are trained that way; pre-turn must
  not. A two-value enum makes the wrong choice unrepresentable.
Both are cheap to adopt in tilde's compactor and fix real
model-behavior subtleties.

### 4. Sandbox-denial readability
tilde has: Landlock denials surface as raw `Permission denied`
from the shell. Codex does: `sandboxing/src/violation.rs` parses
child stderr and reclassifies so the model is told "write outside
workspace" (features-audit §2.1) — and its retry path reuses the
cached approval when policy allows an unsandboxed rerun
(core-audit §2.7). tilde already reclassifies *provider* errors
(Phase 23's readable errors); the same treatment for EACCES/EROFS
in run_shell results — "blocked by the sandbox: writes are
confined to this directory and /tmp" — is a small, high-clarity win.

### 5. Reasoning display: effort knob
tilde has: reasoning streamed and collapsed (Phase 19). Codex has:
`model_reasoning_effort` per turn plus `plan_mode_reasoning_effort`,
with Alt+,/Alt+. cycling in the TUI (tui-audit keymap) and effort
presets in the model catalog. tilde's ChatRequest has no effort
field. Adopt: a `ReasoningEffort` field on ChatRequest (wire:
`reasoning_effort` on OpenAI-compatible, `thinking` budget on
Anthropic), config key, and the effort line in the footer — the
spec's `◐ low/medium/high` footer segment.

### 6. Session-scoped shell environment policy
tilde has: child processes inherit everything. Codex does:
`shell_environment_policy` (inherit / include_only / exclude / set)
applied in `exec_env.rs`, with a shell-snapshot capture of the
user's real login shell (features-audit §2.1, notable). For tilde
the minimal honest version: an `env` config for run_shell children
(exclude by default: nothing; opt-in filtering) — mostly valuable
for redaction of secrets the user exports in their shell.

### 7. The `/doctor` command
tilde has: startup warnings only. Codex has a `doctor` CLI with
per-domain checks (security, sandbox, disk, network, git, updates,
thread inventory…). tilde's natural version: a `/doctor` slash
command checking key resolution, models.json validity, sandbox
status/availability, terminal capabilities, MCP server states, and
the audit log's existence — the first thing to suggest to a user
whose setup broke. All the ingredients already exist as startup
checks; this is a presentation layer over them.

### 8. History: the missing composer history
tilde has: none. Codex has: persisted, per-project + global
history with fuzzy search (`message-history`, TUI Ctrl+R/Ctrl+S
history search). tilde's spec already promises
it. Medium effort, big daily-use win. Note Codex's rule that
secret-input lines are never stored.

### 9. Frame pacing and the "protected input boundary"
tilde has: redraws on every tea.Msg; the type-ahead guard on
pickers was deferred with the spec. Codex does: input is *blocked*
while startup events are pending (`block_terminal_input_for_
pending_startup_events`, tui-audit §2.1) — keys typed before a
dialog rendered cannot answer it. When tilde implements the
spec's type-ahead protection (tui-spec §6), this is the
mechanism to copy.

---

## Adopt — structural, larger phases

### 10. Inline scrollback as the rendering model
The single biggest architectural divergence. Codex commits finished
rows into terminal-native scrollback (`insert_history.rs`,
per-terminal strategies, reflow caps) and only the live region
redraws; users keep native scroll/copy/search. tilde redraws a
windowed view every frame. The tui-spec already mandates this
(§6 item 1) and it's the top item on tilde's convergence roadmap. The
audit adds two practical warnings Codex paid to learn:
- resize reflow must replay committed rows, capped to what the
  terminal actually retained (`resize_reflow_cap.rs`);
- Zellij/Windows partial scroll regions are unreliable — detect
  and switch strategies (`tui/scrollback.rs`).
Bubble Tea's inline renderer already does most of this; tilde's
path is closer than it looks.

### 11. Streaming: block-commit + table holdback
Codex's `streaming/controller.rs` splits a stream into a stable
region (committed to scrollback) and a mutable tail, and holds
markdown tables back entirely until finalization because a new row
reshapes every column (`table_holdback.rs`). tilde currently
re-renders the whole live message per frame (cached, but the whole
message). With #10 this comes along; table holdback is adoptable
independently and cheap.

### 12. Pointer-keyed transcript anchors
When tilde gets its transcript view (spec §8.4), Codex's anchor
model — content addressed by `Arc` pointer, not index, so
prepending history pages never renumbers the reading position —
is the design to copy (tui-audit §2.4). Go's equivalent: key by the
entry's slice identity or a monotonically assigned id captured at
creation.

### 13. Tool exposure: the deferred-tools answer
Codex's lattice (`Direct/Deferred/CodeMode` + BM25 tool search
over the deferred set) is its answer to "too many tools in the
prompt" (core-audit §4.3). tilde will hit this when MCP servers
bring dozens of tools: advertise a tool *search* tool and defer the
rest. Adopt when MCP tool counts grow, not before.

### 14. Keymap: contexts + conflict validation
tilde has fixed keys. Codex has per-context binding resolution
with domain-rule validation ("printable keys are reserved for text
input", ctrl-z reserved) and a `/keymap` editor that reuses the
same resolver (tui-audit §9). If tilde ever makes keys
configurable (spec §13.2 says `~/.tilde/keybindings.json`), build it
as context → global → defaults with one validator — not a flat
map.

---

## Adopt — specific small gems

- **Turn diff budget**: Codex caps diff computation at 100 ms and
  falls back to a coarse diff so display never stalls completion
  (core-audit §4.11). tilde's write/edit diffs are cheap now, but
  the cap is one line of insurance.
- **Sanitized terminal title**: OSC titles are untrusted-text
  injection surfaces; Codex strips control/bidi chars and caps at
  240 (tui-audit notable 13). Adopt with the window-title feature
  (spec §17).
- **One-shot screen-reader probe with a persisted marker**: tilde
  probes per session; Codex persists "probe done" so it runs once
  ever (tui-audit §2.15). Trivial, kinder.
- **Session-log recording behind an env var**
  (`CODEX_TUI_RECORD_SESSION`): a JSONL recorder of exactly what
  the TUI rendered — tilde's PTY test harness would benefit from
  the same during development.
- **Syntax-highlight guardrails**: reject inputs > 512 KB / 10k
  lines / 4 KiB lines, fall back to plain text (tui-audit §2.7).
  tilde highlights model-provided paths; the cap is one check.
- **`agentic session ids` on events**: tilde's orchestrator events
  carry no turn id; every async UI bug gets easier when they do
  (core-audit §4.1). Cheap now, valuable as the TUI grows.

---

## Deliberately not adopting (with reasons)

- **The `App`/`AppEvent` ~600-variant bus**: tilde's tea.Msg set is
  small; a giant enum is Rust's answer to no-sum-types-in-traits.
  Keep tilde's typed messages.
- **Custom ratatui fork for OSC 8 hyperlinks**: valuable at Codex's
  scale; tilde's links are model output, low priority. Revisit
  with the safe/ sanitizer phase.
- **Voice/realtime, pets, analytics dashboard, remote control,
  cloud tasks, code mode (V8), plugins/marketplaces, external
  agent import, worktrees, Windows sandbox, network proxy with
  MITM**: real products, out of tilde's scope for the foreseeable
  roadmap. The Noise-relay crypto and the seatbelt ancestor-unlink
  rules are excellent engineering worth remembering when (and only
  when) the corresponding surface arrives.
- **Guardian (LLM auto-approver)**: interesting, but tilde's
  permission model is deliberately human-in-the-loop; an automated
  approver would invert the security posture the project was built
  on. Revisit only with explicit user demand.
- **154-flag feature registry**: tilde is one binary with one
  config; flags would be ceremony. The `[features]` idea becomes
  worth it only when tilde grows experimental surfaces that need
  gating (MCP OAuth, network sandbox).

---

## Sequencing suggestion

1. Small, now: #1 canonicalized approvals, #4 sandbox-denial
   copy, #5 effort knob, #7 /doctor, the small gems (#15 group).
2. Next structural: #8 composer history, #10 inline scrollback +
   #11 block-commit streaming (one phase, the spec's §1.4).
3. Then: #12 transcript anchors + transcript view, #3 compaction
   refinements, #2 typed input decisions.
4. On demand: #13 deferred tools, #14 keymap, everything in
   "not adopting" until a real user needs it.
