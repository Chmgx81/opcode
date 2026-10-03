# PROGRESS.md — current state

The running log of what opcode is, what is in flight, and what is
deliberately not done. The full chronological build log — every
phase, what was verified live, what was not — is
**[docs/progress-log.md](docs/progress-log.md)**. Read that for
history; read this for today.

Per [AGENTS.md](AGENTS.md) §4, this file is a working document: done,
in progress, next, blockers. Everything below was re-checked against
the code when this file was last restructured, not copied forward on
trust.

## Where things are

| | |
|---|---|
| Latest release | **v0.6.0** (2026-10-02; the UI redesign — Phases 45–49: the box composer, the violet identity, one list language, the mode picker, the first-run journey, the context readout, native scrollback) |
| Head | 15 Go packages (~16.9k lines non-test, 579 tests) |
| Go | the `go` line of `go.mod` (1.25.13); CI installs exactly that |
| Release platforms | linux/amd64, linux/arm64, darwin/amd64, darwin/arm64, windows/amd64 |
| Sandbox | Linux only (Landlock 5.13+; seccomp network block on x86_64) |

## Build and verify

```sh
go build ./...
go vet ./...
gofmt -l .                      # must print nothing
go test -race -count=1 ./...    # what CI runs
bash scripts/test-install.sh    # the installer, against a fake release
bash scripts/check-doc-links.sh # every relative link in every .md
```

CI also runs a five-target cross-build, the suite on macOS under
`-race` (so the non-Linux sandbox branches get exercised), govulncheck,
and shellcheck on `install.sh`. See
[docs/releasing.md](docs/releasing.md).

## Done

Everything the [spec index](docs/specs/README.md) lists as shipped is
in the tree: the orchestrator and tool layer, the Bubble Tea TUI, the
three permission modes, the Landlock+seccomp sandbox, skills, MCP,
subagents, sessions with compaction, the provider catalog with a
native Anthropic client, live model browsing, the display-boundary
sanitizer, `/doctor`, `/theme`, `/diff`, LaTeX-as-Unicode, precise
approval grants, the reasoning-effort dial, `opcode update` with
checksum verification, the CI/release pipeline, the Phase 45
UI restyle — the composer in the shared box language and a
one-line greeting lockup ([phase45](docs/specs/phase45-ui-restyle.md)) —
and the Phase 46 palette and list restyle: the violet identity
(teal kept as a named theme) and one selection language for every
list ([phase46](docs/specs/phase46-palette-and-lists.md)). Phase 47
made `/mode` a picker with per-mode color, `/model` the reachable
hub, and the first-run journey continuous — login → models → pick,
with the key pre-flight before the request
([phase47](docs/specs/phase47-modes-and-first-run.md)).

## In progress

**Website and docs redesign** (`site/`, uncommitted).
Spec: [docs/specs/site-redesign.md](docs/specs/site-redesign.md),
which maps each of the 30 "vibe-coded" tells to its treatment.

- Done: the editorial-paper direction is built and `npm run build`
  passes. Warm paper ground, Source Serif 4 + IBM Plex Mono, hairline
  rules, numbered sections, one ink band. Radius forced to 0, no
  shadows, blur or gradients anywhere. Lucide, motion,
  react-wrap-balancer, radix-slot and cva removed from the
  dependencies; Inter/Geist/Space Grotesk gone.
- Done: five real captures recorded from a `v0.6.0` build through a
  PTY against a scripted local provider, in `scripts/site-demo/`.
  The files in `site/src/content/` are byte-identical to
  `scripts/site-demo/out/` (md5-checked).
- Done: `/privacy` and `/terms` exist and are linked from the footer.
  The masthead release badge is a live GitHub fetch with a skeleton
  loading state, an offline fallback and an accessible label.
- Done: verified in headless Chromium across 10 routes x 4 viewports
  (1440/1024/768/360): no horizontal overflow, no forbidden font in
  any computed `font-family`, no non-zero `border-radius`, no
  `box-shadow`/`text-shadow`/`backdrop-filter`, no console or page
  errors. 40/40 clean. Two overflow bugs found and fixed this way
  (grid children defaulting to `min-width: auto`, and the install row
  refusing to shrink below the command's min-content).
- Done: contrast computed for every token pair; all clear WCAG AA.
  `ink-3` was darkened `#64696f` → `#5d6268` because it landed at
  4.44 on `paper-2`. Copy button, mobile menu, FAQ rows and the
  release badge's three states all exercised in the browser.
- Done: **dark theme and the toggle.** Ground and every step re-picked
  for charcoal (`#191b20`), accent lifted to `#e07a55`. The inverted
  plate carries its own six `band-*` tokens in both themes: in light it
  is the deep near-black, in dark it is *raised* above the ground, so
  the edge stays legible instead of either glaring or vanishing. That
  work also fixed a **pre-existing AA failure** — the footer and
  section 04 used `text-paper/45`, which measures 4.08 against the
  plate; now `text-band-dim` at 5.20. One bordered `menu`-style
  control names the mode it switches to, records the choice in
  `localStorage`, follows the system preference until told otherwise,
  and a pre-paint inline script in `index.html` sets `data-theme`
  before anything renders.
- Done: re-audited in **both** themes — 9 routes x 4 viewports x 2
  themes = 72 runs clean, with contrast now computed **per element**
  against its own alpha-composited background rather than per token
  pair. Fifteen interaction checks on the control pass, including
  keyboard operation, choice persistence against the opposite OS
  preference, blocked `localStorage`, and a no-flash check that blocks
  the JS bundle entirely so only the HTML script is left to prove the
  ground is painted right the first time.
- Done: visual review. The reviewer tool returns stale or mismatched
  files independent of path, so each capture carries a magenta banner
  with its **measured** state burned in, and was trusted only when the
  banner matched the request. Thirteen views confirmed by eye: `/` and
  `/docs/` at 1440 in both themes, the section 04 plate and footer in
  both themes, `/privacy` and `/terms` at 1440, and `/` at 768 and 360
  light plus 360 dark.
- Not covered: nobody has looked at these pages in a real browser on a
  real display, and the keyboard path through the whole page (beyond
  the theme control) has not been walked by hand. The automated checks
  judge whether the design obeys its own rules, not whether it is good.

## Next

- Commit the site redesign and push; the design work in the deferred
  list follows, which is deliberately a queue of judgement calls rather
  than a backlog.

## Deferred (re-checked against the code, not copied from an old log)

Each item was open in the Phase 43 / Phase 44 audits
([specs/phase43-production-audit.md](docs/specs/phase43-production-audit.md),
[specs/phase44-release-hardening.md](docs/specs/phase44-release-hardening.md)).
Items already closed by those phases are listed afterwards so they
are not re-litigated.

Still open:

- **No retry or backoff on transient provider errors.** A 429 or a
  5xx fails the turn and names the next step; the user resends. A
  single automatic retry with the provider's retry-after would take
  the rough edges off rate limits — design work, not a one-liner
  (billing and steering semantics need deciding first).
- **MCP tool calls are capped at 30s, for every server.** A
  legitimate slow tool (a build, a test suite) fails and a restart
  does not trigger on timeouts. A per-server `timeout` field in
  `mcp.json` is the shape; not built because no one has asked.
- **Compaction is off unless `context_window` is set.** The working
  line's occupancy readout is silent without it too. A "context is
  getting large" dim note at a generous fixed threshold (say 100k
  tokens) would warn without guessing a model's real window.
- **Skill-script trust is re-checked at launch, not mid-session.**
  Scripts run sandboxed either way; the gap is documentation-only
  and SECURITY.md states it.
- **Read-tier reads any path.** `read_file` is unconstrained apart
  from `auth.json`. This is a documented posture, not a bug — see
  [SECURITY.md](SECURITY.md) — but a path allowlist is the obvious
  next hardening step if it is ever wanted.
- **Network confinement is x86_64 only.** The seccomp filter is
  hand-written x86_64 BPF. On linux/arm64 the file confinement
  applies and the network stays open. Closing it needs a
  different mechanism (per-architecture BPF, or a network namespace).
- **No total timeout on a model turn.** There are a response-header
  timeout and a stream-idle timeout; a provider that sends a byte
  every few minutes forever will not be cut off. The user can still
  stop the turn with esc.
- **A chatty MCP server can wedge the reader.** Each line is capped
  at 8 MiB, but a server that never stops sending keeps the read
  loop busy. Bounded per line, unbounded in total.
- **Package boundaries.** `internal/tools` and `internal/tui` are
  both large; the natural splits (a permission leaf package,
  prompt/picker files) are a next-touch refactor, not urgent.
- **`internal/config` imports `internal/tools` (mode names); the
  reverse edge does not exist, so the graph stays acyclic.** The
  dependency is narrow and one-way; if it ever widens, a leaf
  package holding the mode constants is the cleaner cut.
- **`!` is shell-only.** The escape runs `bash -c`; there is no
  `!read_file`-style form to invoke a tool by hand.
- **Releases are unsigned.** No build attestation or cosign
  signature; integrity rests on GitHub account security. Action
  items are listed in [docs/releasing.md](docs/releasing.md).

Closed since the audits — do not reopen:

- Installer checksum verification (`install.sh`, `opcode update`).
- Clipboard tools resolved from an allowlist of system bin dirs, not
  `PATH`.
- Bash timeouts and interrupts kill the whole process group.
- The `!` escape writes an audit entry.
- LLM response-header and stream-idle timeouts.
- The Phase 24 spec's stale `tui-ux-spec.md` link.

## Blockers

None.

## Where to look

| Question | File |
|---|---|
| How do I use it? | [README.md](README.md) |
| How is it built? | [docs/specs/opcode-architecture.md](docs/specs/opcode-architecture.md) |
| What does the UI do? | [docs/specs/tui-spec.md](docs/specs/tui-spec.md) |
| Why does it work this way? | [docs/specs/README.md](docs/specs/README.md) |
| What is the threat model? | [SECURITY.md](SECURITY.md) |
| How do I cut a release? | [docs/releasing.md](docs/releasing.md) |
| What happened, in order? | [docs/progress-log.md](docs/progress-log.md) |
