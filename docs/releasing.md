# Releasing opcode

A release is a git tag. There is no version file to bump: the version is
injected at build time (`-ldflags "-X main.version=<tag>"` in
`.github/workflows/release.yml`; `var version` in `cmd/opcode/main.go`
defaults to `(devel)` for source builds).

## Cut a release

1. Make sure `main` is green in CI (`.github/workflows/ci.yml`: gofmt,
   `go mod tidy` drift, vet, `-race` tests, 5-target cross-build, macOS
   `-race` tests, installer tests, shellcheck on `install.sh`, a
   markdown link check, govulncheck).
2. Tag the commit and push the tag:

   ```sh
   git tag v1.2.3
   git push origin v1.2.3
   ```

   The tag must be `vMAJOR.MINOR.PATCH`, optionally with a prerelease
   suffix (`v1.2.3-rc.1`). Anything else fails the first step of the
   pipeline, before anything is built.
3. Watch the `release` workflow. When it finishes, the release page has
   five archives and `checksums.txt`, with generated notes.

A tag with a hyphen (`v1.3.0-rc.1`) is published as a **prerelease**.
GitHub never treats a prerelease as "latest", so `install.sh` and
`opcode update` ignore it. Use one to rehearse the pipeline (then delete
it, see below).

The workflow does not check that the tag points at a commit on `main`.
Tag from an up-to-date `main`.

## What the pipeline does

`.github/workflows/release.yml`, on a `v*` tag push:

| Job | Permissions | Does |
|---|---|---|
| `verify` | read | checks the tag is semver; `go vet`; `go test -race -count=1 ./...` |
| `build` | read | needs `verify`; builds linux/darwin (amd64, arm64) and windows/amd64 with `CGO_ENABLED=0 -trimpath`, version = tag; packs `opcode-<os>-<arch>.tar.gz` (`.zip` on windows); writes `checksums.txt` for all five and re-checks it; extracts the linux-amd64 archive and asserts `--version` contains the tag; uploads the files as a workflow artifact |
| `publish` | `contents: write` | needs `build`; creates the GitHub release (or, if it already exists, replaces its assets), marking prereleases |

Each archive contains one binary: `opcode-<os>-<arch>` (`.exe` on
windows). Publishing uses the `gh` CLI that ships on the runner, so the
only actions used are first-party `actions/*`.

Re-running a failed `publish` job is safe: an existing release gets its
assets replaced (`--clobber`). If a run died *while creating* the
release, check the Releases page for a leftover draft or half-populated
release and delete it before re-running.

## Roll back a bad release

Installers and `opcode update` both follow GitHub's `latest` release
(newest non-prerelease, non-draft).

- **Stop new installs of it quickly:** edit the release and tick "Set as
  a pre-release" (or `gh release edit v1.2.3 --prerelease`).
  `latest` falls back to the previous release. Nothing is deleted.
- **Remove it entirely:** delete the release and its tag
  (`gh release delete v1.2.3 --cleanup-tag`, or delete both in the UI).
- **Fix forward:** publish `v1.2.4`. Do not reuse a version number: a
  machine that already installed `v1.2.3` is not touched by any of the
  above, and `opcode update` only moves to a *higher* version, never
  down. A higher patch release is the only thing that reaches those
  users.

Users who need the old version back can pin it:
`curl -fsSL .../install.sh | OPCODE_VERSION=v1.2.2 bash`.

## `opcode update` and patch releases

`opcode update` asks the GitHub API for `releases/latest`, compares that
tag with the running binary's version by semver, and only proceeds when
the release is strictly newer. It then downloads the archive and
`checksums.txt` for the running platform, verifies the sha256 **before**
extracting, and atomically replaces the binary. It runs only when the
user types it; there is no background download.

How users learn an update exists: at startup opcode compares the running
version against a cached latest tag (one small HTTPS GET, at most once
a day, capped at three seconds because it blocks the first frame;
silent when offline, retried hourly after a failure) and every
surface names the same release:

- one startup note in the transcript, in full:
  `Update available: v1.0.0 → v1.1.0. Run \`opcode update\` to install it.`
- a dim `↑ v1.1.0` badge on the footer mode line, which stays on
  screen for the whole session and survives the narrow-terminal
  reflow, so the note scrolling away is not the end of the signal
- a line in the `?` overlay explaining that badge
- the `/doctor` row, which also says whether the last check *worked*
  (and that the check is off, that the build is not a release, or
  that no prebuilt binary exists for the platform) — the only place
  that reports a failure
- `opcode --version`, which reads the cache only and never phones
  home: it appends `(update available: v1.1.0 — run opcode update)`
- the exit line, which appends
  `· update available: v1.1.0 — run opcode update` to
  `~ opcode — session saved · resume it with /sessions`

`opcode update` is a shell command, not a slash command: it replaces
the binary the TUI is running from. Typing `/update` in a session says
so. `opcode update --check` reports immediately and refreshes the
cache. Opt out with `"update_checks": false` in config.json or
`OPCODE_NO_UPDATE_CHECK=1`.

- Source builds (`(devel)`) and untagged `go install` builds
  (pseudo-versions such as `v0.2.1-0.2025...-abc123def456`) are not
  release builds: `opcode update` says so and does nothing.
- A user on `v1.3.0-rc.1` is offered `v1.3.0` once it is published, not
  before (prereleases are never `latest`).
- The API is called unauthenticated: 60 requests/hour per IP. A rate
  limit shows up as an HTTP 403 with a hint.
- Windows cannot replace a running `.exe`; there the command fails
  cleanly, leaves the old binary, and points at the release page.

## Verify a download by hand

```sh
# in a directory with the archive and checksums.txt from the release page
grep " opcode-linux-amd64.tar.gz\$" checksums.txt | sha256sum -c -
# macOS: shasum -a 256 -c -
```

`OK` means the archive matches the release's `checksums.txt`. That
protects against corruption and tampering in transit or on a mirror. It
does not prove who published the release: the checksum file lives next
to the archive.

## Follow-ups not done yet

- **Pin actions by commit SHA.** Workflows use major tags
  (`actions/checkout@v4`, `actions/setup-go@v5`,
  `actions/upload-artifact@v4`, `actions/download-artifact@v4`). A moved
  tag would change what runs in the release job. Full-SHA pinning was
  deliberately not done here because the SHAs could not be looked up
  offline, and guessing one would be worse than a tag. To do it, resolve
  each tag (`git ls-remote https://github.com/actions/checkout 'refs/tags/v4*'`),
  write `uses: actions/checkout@<40-hex-sha> # v4.x.y`, and let
  Dependabot (`.github/dependabot.yml`, `github-actions` ecosystem) keep
  the SHAs current.
- **Provenance / signing.** No build attestation (for example
  `actions/attest-build-provenance`) and no cosign signature yet, so
  release integrity rests on GitHub account and repository security.
  Enable branch/tag protection and 2FA for maintainers meanwhile.
- **Go patch level.** `setup-go` installs exactly the version in the
  `go` line of `go.mod`, and release binaries embed that standard
  library. If `govulncheck` in CI reports standard-library
  vulnerabilities, raise that line to the latest patch release before
  tagging.
- **The tag is not checked against `main`.** `verify` confirms the tag
  is semver and that vet and the race suite pass, but nothing confirms
  the commit is reachable from `main` — a tag on a side branch
  publishes. `git merge-base --is-ancestor "$TAG" origin/main` in the
  `verify` job would close it, at the cost of blocking a deliberate
  release from a release branch. Left as a maintainer decision.
- **No dependency review on pull requests.** Dependabot proposes
  version bumps, but a PR that introduces a new transitive dependency
  is not flagged. `actions/dependency-review-action` on `pull_request`
  would do it; it is a third-party action, so it lands with the
  full-SHA pinning above rather than before.
- **No coverage gate.** The suite is large (552 test functions) and
  `go test -race -count=1 ./...` must be green, but nothing stops
  coverage from falling. A threshold needs a measured baseline first —
  pick a number from real data, not from a guess.
