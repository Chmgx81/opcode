# Phase 21 — Landlock sandbox for shell commands

## Goal

Confine what a shell command can write: `run_shell` (and skill
scripts) execute under a Landlock ruleset that allows read+execute
everywhere but writes only beneath the working directory, the temp
dir, and known dev caches. A prompt-injected `rm -rf ~` or a write to
`~/.ssh/authorized_keys` then fails with EACCES instead of succeeding —
kernel-enforced, unprivileged, no dependencies.

## Non-Goals

- Network isolation (seccomp) — a separate, heavier phase; Landlock
  ABI v5 scopes abstract sockets but not TCP. Deferred.
- bwrap/namespaces (Codex's current backend) — needs a setuid helper
  or user namespaces; Landlock is the zero-dependency fit for one
  Go binary.
- Sandbox on macOS/Windows — `ErrUnsupported` there; the runner
  reports it and commands run as before.
- Restricting reads — tools need to read the whole filesystem
  (system headers, `/usr/lib`); Codex allows the same.

## Approach — mirrored from Codex's landlock.rs

- Ruleset handles all fs access rights up to the kernel's probed ABI
  (v2 adds REFER, v3 TRUNCATE, v4 IOCTL_DEV — each handled only if
  the kernel supports it).
- Rules: read-only (execute + read_file + read_dir) beneath `/`;
  full rw beneath each writable root; rw on `/dev/null`.
- `PR_SET_NO_NEW_PRIVS`, then `landlock_restrict_self`, then `execve`.

**The Go constraint:** Landlock applies to the calling *thread*, and
Go's runtime has several threads before `main`. Applying in-process
would leave the other threads unrestricted. So the sandbox runs the
command through a self re-exec: `tilde __sandbox <writable…> -- cmd`
starts a fresh, single-threaded child that applies the ruleset and
immediately `execve`s the command. The window between restrict and
exec runs only our own code; the exec'd process inherits the ruleset
process-wide.

**Writable roots:** cwd (recursive), `os.TempDir()`, plus dev caches
that exist on the machine — `~/.cache` (or `$XDG_CACHE_HOME`) which
covers `GOCACHE`'s default, `~/go/pkg/mod` (module cache),
`~/.cargo/registry`, `~/.npm`, and `$GOCACHE`/`$GOMODCACHE` when set.
`~/go/bin`, `~/.ssh`, `~/.config`, and everything else stay
read-only: dropping an executable on a PATH dir or editing ssh keys
is exactly the attack class this exists to stop.

**Default and config:** on when the kernel supports it (probe via
`landlock_create_ruleset(…, VERSION)`); `{"sandbox": false}` in
config.json disables. The startup notes and `--help` state the actual
posture — never silently unsandboxed. If Landlock is unavailable,
commands run as before with a startup note saying so.

## Edge cases

- Kernel without Landlock (< 5.13) or Landlock disabled: probe
  returns an error → runner degrades to unsandboxed, status says so.
- Old kernels (< v3): TRUNCATE not handled → truncating writes in the
  sandbox are denied; normal.
- A command that legitimately writes outside the roots (e.g.
  `go install` to `~/go/bin`) fails with EACCES — surfaced in the tool
  result so the model reports it; the user can add roots later or
  disable the sandbox per-project.
- The child dies with the parent (`Pdeathsig: SIGKILL`) so a
  sandboxed command can't outlive tilde.

## Test plan

- Unit: writable-roots computation (env overrides, nonexistent dirs
  dropped), status strings per platform state.
- Real enforcement (Linux only, gated on probe): a test-child process
  applies the ruleset and attempts writes inside and outside the
  roots — inside succeeds, outside is denied EACCES. The child is
  this same test binary via `TestMain` + env sentinel, so the test
  exercises the real syscalls, not a mock.
- PTY: a live session runs `touch` inside cwd and outside; the tool
  result shows the denial; startup note shows the sandbox status.
