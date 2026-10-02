# Phase 37 — precise approval scopes

## Goal

The adoption doc's #1 (Codex's approval cache keyed by canonicalized
command). opcode's "always allow" grant is built from the command's
first two whitespace fields — program and subcommand. When the second
field is a flag (`git -C /tmp push`, `sudo -u root systemctl …`), the
grant becomes `<program> <flag>:*` — which auto-approves every other
value of that flag: approving `git -C /tmp push` would silently
approve `git -C /etc reset --hard`. The grant must never be broader
than what the user read on the dialog.

## Non-Goals

- No full command canonicalization engine (Codex's
  `command_canonicalization.rs` is a parser). opcode's grants are
  token-prefix rules; this phase fixes their precision and adds a
  small synonym pass, nothing more.
- No persistent approval store — grants stay session-scoped (the
  Phase 27 decision).
- No change to fail-closed matching: metacharacters still deny,
  untokenizable commands still deny.

## Approach

1. **Flag-aware scope** (`tui.alwaysScope`): when the command's
   second field starts with `-`, the grant carries the ENTIRE
   command verbatim — the user approved exactly that, so exactly
   that (and its longer forms) auto-runs. When the second field is
   a subcommand, the grant stays `program subcommand:*` as today
   (`cargo build` covers `cargo build --release`).
2. **Flag-synonym canonicalization** (`tools`): with flags now
   inside grants, `npm install --save-dev` and its synonym forms
   should agree. A small curated table of genuinely universal
   long/short pairs (`--yes`/`-y`, `--quiet`/`-q`, `--force`/`-f`,
   `--verbose`/`-v`, `--recursive`/`-r`) normalizes both the stored
   grant tokens and the checked command tokens at match time.
   Deliberately NOT included: pairs that differ across tools
   (`--all`/`-a` — `grep -a` is `--text`, not `--all`); a synonym
   that merges two distinct flags would widen a grant past what the
   user read.
3. The dialog's scope line shows the literal grant (`git -C /tmp
   push:*`), so the user can read exactly what "always" means.

## Edge Cases

- A flag-command containing shell metacharacters: the 2-field path
  was already rejected by `prefixRule`; the full-command grant is
  rejected the same way — metacharacter commands cannot get a
  prefix rule at all (fail closed to a per-tool grant, unchanged).
- Quote-aware storage vs display: the rule is stored via
  `shellWords` (quote-aware), the scope displayed via `Fields` —
  a quoted second field renders with its quotes and still
  tokenizes identically on both sides.
- Case: `-C` vs `-c` stay distinct — no case folding, ever.

## Test Plan

- Unit: alwaysScope — subcommand form unchanged, flag form carries
  the whole command, empty command falls back to the tool name.
- Unit: the security property — a grant from `git -C /tmp push`
  does not match `git -C /etc reset --hard` (the old code's hole),
  and does match `git -C /tmp push --quiet`.
- Unit: synonyms — `cargo test --quiet` matches a `-q` grant and
  vice versa; non-synonym flags (`--all` vs `-a`) do NOT merge.
- Unit: fail-closed unchanged — metacharacters and unterminated
  quotes still deny.
- PTY live: approve `git -C <proj> status` with "always"; the same
  command re-runs without a prompt, and a different `git -C`
  target prompts again.
- Full suite green across all packages.
