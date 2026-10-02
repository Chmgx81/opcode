> Superseded in part: the modes were later consolidated to three
> (plan / build / full-auto) — read-only folded into plan, ask became
> build; see PROGRESS.md. The gate mechanics here still hold.

# Phase 30 — The sandbox-aware gate

## Goal

Adopt Codex's core permission posture: **the sandbox is the safety,
approval is the exception.** Today ask mode prompts for every action
call, even ones the Landlock sandbox already confines — the prompt is
noise, and read-only mode hides the action tools entirely, so the
model can't even propose a write. Codex (verified in
`codex-rs/protocol`, `utils/approval-presets`) pairs a sandbox
(what can happen) with an approval policy (who is consulted), and
its default runs workspace-bounded actions without prompting,
reserving prompts for sandbox escapes.

## Non-Goals

- Per-call Landlock rulesets (a read-only ruleset for shell
  commands in read-only mode). The process-wide sandbox stays.
- Codex's granular approval switches, exec-policy rules, and
  reviewer routing — out of scope at opcode's scale.
- Network confinement (Landlock ABI here doesn't cover it; the
  spec never claimed it).

## Approach

**Modes.** `ask-every-time` is renamed to **`ask`** (its new
posture no longer asks every time; the name must not lie).
`NormalizeMode` maps the old spelling, `ask`, and the legacy
`auto-accept-safe-ops` to it, so every existing config keeps
working. Mode order is unchanged: read-only, plan, ask, full-auto.

**Offering is not permission.** Every mode offers every tool tier
(the gate is the single enforcement point). Read-only and plan
previously hid action-tier tools; now the model can propose a
write and the user approves it in place — no mode switch.

**The gate decides, bounded by what is actually enforced:**

| Mode | Read tier | Action tier |
|---|---|---|
| read-only | free | prompts (fail closed without a prompter) |
| plan | free | prompts; draft tier (present_plan) free |
| ask | free | auto-runs **only** what is bounded: sandboxed `run_shell` (Landlock active) and file writes inside the sandbox's writable roots. Escapes — `{"sandbox": false}` shell calls, writes outside the roots, or any call when Landlock is unavailable — prompt |
| full-auto | free | free |

**`run_shell` gains a real `{"sandbox": false}` opt-out** — the
README already claimed it; now it exists. The tool schema
documents it; the unsandboxed call is the approval-triggering
escape in ask mode.

**File writes bound by the same roots as the sandbox.**
`write_file`/`edit_file` run in-process, where Landlock can't
reach, so the gate bounds them by path against
`sandbox.WritableRoots()` — the identical promise a sandboxed
command gets. The parent directory is resolved through symlinks
first, so a lexical in-tree path that points outside does not
auto-run.

**The config `safe_commands` allowlist is removed.** Its only
job was auto-running specific commands when everything prompted;
with sandboxed commands auto-running in ask mode, it is dead
weight. Session "don't ask again" grants (the approval dialog)
are unchanged and still cover escapes.

## Edge Cases

- Landlock unavailable (non-Linux, old kernel, sandbox off in
  config): ask mode prompts for every action — it must never
  auto-run a command it cannot actually confine.
- Headless (`opcode -p`, no prompter): unchanged posture — bounded
  actions now run, every escape fails closed.
- Symlinked in-tree path resolving outside the roots → prompts.
- Unparsable args → not bounded → prompts (fail closed).
- `unknown` mode behaves as ask (a config typo must not widen).

## Test Plan

- Policy unit tests: the full decision matrix per mode, the
  sandbox-off fail-closed, `sandbox:false` escape, symlinked
  write path, out-of-root write, tmp-dir write.
- run_shell: the `sandbox` arg parses; default stays sandboxed.
- Orchestrator: read-only now receives action-tier defs.
- TUI: approval dialog copy names unsandboxed escapes.
- PTY live: ask mode auto-runs a sandboxed command with no
  dialog; a `sandbox:false` call opens the dialog; read-only
  proposes a write and the dialog appears (approve → it runs).
- Spec/README/PROGRESS updated in the same change.
