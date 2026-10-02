# Phase 43 — the production-readiness audit

## Goal

opcode is about to have real users. Everything built so far was
verified phase by phase, but no one has audited the WHOLE with a
fresh eye against the readiness bar: security (AGENTS.md §9),
concurrency and failure handling (§5/6/10), UX states and copy
(§7), and slop/dead code/stale docs (§11). Four read-only audit
subagents sweep the codebase in parallel; this session triages
every finding and fixes the real ones.

## Non-Goals

- No new features. The audit's output is fixes and dispositions,
  not a roadmap.
- Auditors do not modify code: audit-only briefs keep evidence
  clean; fixes happen in this session with tests.
- No style crusades: findings must cite a concrete risk or
  concrete confusion, not taste.

## Approach

Four parallel subagent auditors, each grounded in a specific
AGENTS.md section, each required to cite file:line evidence and to
separate CONFIRMED findings from unconfirmed suspicions:

1. **Security** (§9): secrets, input validation, path traversal
   and symlinks, credential redaction (audit log AND session
   save), the sandbox escape path, file modes.
2. **Concurrency & failure** (§5/6/10): goroutine leaks, races,
   gate/plan deadlocks on UI death, swallowed errors, shutdown
   and reaping, timeouts.
3. **UX** (§7): first-run experience, error copy, every
   command's designed outcomes, missing states, key
   discoverability, README drift.
4. **Slop** (§11): dead code verified by grep, TODO
   classification, stale comments after 42 phases of drift,
   padding, stray files, spec-vs-code drift.

Triage discipline: each finding gets one of three dispositions —
FIX (this session, with a test where the fix is behavioral),
DEFER (recorded with a reason), or REJECT (false positive,
recorded why). No silent drops.

## Edge Cases

- Auditors may duplicate each other's findings: deduplicate at
  triage, not in the briefs.
- A "medium" pile of UX copy fixes is acceptable in one commit;
  behavior changes get individual tests.
- Anything the auditors cannot confirm is treated as
  unconfirmed, not as a finding.

## Test Plan

- Every behavioral fix carries a test that fails before it.
- Full suite green across all packages; vet and gofmt clean.
- CI green on the pushed result.
- PROGRESS.md records the audit, the dispositions, and anything
  deferred with its reason.
