# Phase 44 — release hardening: races, CI/CD, updates

## Goal

Close every known gap between "verified phase by phase" and "safe to
hand to real users": land and verify the uncommitted audit follow-ups
(checksum installer, LLM timeouts, MCP wedge, seccomp network block,
credential-file deny, `!` audit entry), fix the data races the race
detector finds, and give the project a release pipeline and update
path a maintainer can trust.

Audience: developers who run a terminal coding agent on Linux and
macOS (Windows best-effort), install with `curl | bash`, and keep
provider API keys on the same machine tilde runs shell commands on.
The threats that matter to them are key leakage, a hung agent, and a
tampered or broken update.

## Non-Goals

- No new user-facing features beyond `tilde update`.
- No seccomp on architectures other than x86_64 (documented, not
  faked); no Windows sandbox.
- No package-manager formulas (brew/apt) — a follow-up once releases
  are stable.

## Approach

Five parallel workstreams with disjoint write sets:

1. **llm** — fix the failing idle-watchdog test, review timeout.go.
2. **tools + sandbox** — fix the bash watchdog race (Start/Wait, not
   CombinedOutput), review seccomp/credential deny/redaction, make
   the denial test environment-robust.
3. **orchestrator + tui** — make conversation history race-free.
4. **CI/CD + install + update** — ci.yml (race, gofmt, cross-build,
   vulncheck), release.yml (test gate, checksums, pinned actions),
   dependabot, SECURITY.md, `tilde update` with checksum
   verification, installer tests.
5. **QA** — black-box end-to-end run of the built binary against a
   fake provider; MCP change review with `-race`.

## Edge Cases

- A test that fails only because of this host's environment must be
  made robust, not deleted.
- Anything that cannot be verified here (macOS, arm64, Windows) is
  reported as unverified, not assumed.
- Update must never replace the binary with unverified bytes, and
  must never leave a half-written binary on disk.

## Test Plan

- `go build`, `go vet`, `gofmt -l` clean on linux/darwin/windows.
- `go test -race -count=1 ./...` green.
- New behavior carries a test that fails without it.
- CI workflow YAML validated (actionlint if available, else parsed).
- Installer exercised against a local fake release (checksum ok,
  tampered, missing entry).
