# Phase 38 — readable sandbox denials

## Goal

The adoption doc's #4. When the Landlock sandbox blocks a write,
the model sees the shell's raw complaint — `Permission denied` —
with no hint of why, and its most common next move is to flail
(retry the same command, try sudo, conclude the file is broken).
A sandboxed EACCES/EROFS now carries a plain-language
classification and the documented escape, so the model's next move
is either a correct in-scope write or a deliberate
`sandbox: false` retry that the user approves.

## Non-Goals

- No errno interception or seccomp parsing (Codex's violation.rs
  reads structured child errors). opcode classifies by the failure
  signature in the combined output, and the note says "probably"
  — a sandboxed EACCES is usually the boundary, but a genuine
  permission error inside the writable roots is possible, and the
  note must not lie about which one happened.
- No automatic unsandboxed retry (Codex reuses a cached approval
  when policy allows). opcode's escape stays explicit: the model
  re-issues the call with `sandbox: false`, which prompts in build
  mode — the human stays in the loop.
- No change to non-sandboxed commands or to the gate.

## Approach

In `tools.Bash.Execute`, when the command ran sandboxed and the
combined output carries a denial signature (`Permission denied`,
`Read-only file system`, `Operation not permitted`), prepend:

  note: this ran inside the sandbox, where writes are confined to
  the working directory, /tmp, and dev caches — the failure below
  is probably that boundary. If this write is legitimate, retry
  with {"sandbox": false} and the user will be asked to approve
  the unsandboxed run.

The original output follows unchanged — the classification adds
context, it never replaces evidence. The note applies on both the
exit-error and the swallowed-error paths (`|| true` can hide a
denial behind exit 0).

The tool's description already documents the escape; the note
meets the model at the point of failure, where it actually reads.

## Edge Cases

- A genuine permission error inside the writable roots: the note
  says "probably", the original error stays visible below it, and
  the retry escape is harmless (it prompts; the user can refuse).
- Non-sandboxed commands never get the note — the flag is tracked
  at the branch that chose the command, not inferred later.
- Platforms without Landlock: `sandbox.Active()` is false, the
  plain path runs, no note — an unsandboxed EACCES is a real
  permission error and must not be mislabeled.
- The timeout path returns first: a timed-out command's output is
  not classified.

## Found during design (and fixed as part of this phase)

The note is only useful if it reaches the model — and two
pre-existing bugs were silently eating ALL tool output on failure:

1. The gate zeroed the tool's output on error (`result = ""`)
   while the audit entry kept it — contradicting its own "execution
   result is reported to the caller regardless" comment.
2. The orchestrator overwrote a failing call's result with just
   `"error: " + err.Error()`, so a bash failure reached the model
   as "exit status 1" with no cause at all.

Both now forward the output: the failure result is the error
headline plus the tool's own bytes. This is the evidence channel
the classification rides on, and it was already the intent.

## Test Plan

- Unit: the signature matcher (each denial phrase, and the
  near-misses that must not match).
- Unit: Bash with the sandbox installed, writing outside the
  writable roots, carries the note and keeps the original error;
  skipped where Landlock is unsupported.
- Unit: an in-scope write carries no note; a non-sandboxed call
  carries no note even on EACCES.
- PTY live: a fixture asks for an out-of-scope write; the tool
  result line in the transcript shows the note, and the raw
  `Permission denied` below it.
- Full suite green across all packages.
