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
| Latest release | **v0.4.0** (2026-10-02; v0.5.0 renames the project to opcode) |
| Head | 15 Go packages (~16.8k lines non-test, 562 tests) |
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
checksum verification, the CI/release pipeline, and the Phase 45
UI restyle — the composer in the shared box language and a
one-line greeting lockup ([phase45](docs/specs/phase45-ui-restyle.md)).

## In progress

Nothing is half-built. The working tree is clean between
milestones; if you are reading this mid-task, check
`git status` before trusting that claim.

## Next

Nothing is scheduled. The remaining work is the deferred list below,
and it is deliberately design work rather than a queue.

## Deferred (re-checked against the code, not copied from an old log)

Each item was open in the Phase 43 / Phase 44 audits
([specs/phase43-production-audit.md](docs/specs/phase43-production-audit.md),
[specs/phase44-release-hardening.md](docs/specs/phase44-release-hardening.md)).
Items already closed by those phases are listed afterwards so they
are not re-litigated.

Still open:

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
- **No `internal/config` → `internal/tools` import yet.** The
  direction is one-way today, so there is no cycle; it is worth
  doing if `tools` ever needs a config constant. A leaf package is
  the cleaner fix when that day comes.
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
