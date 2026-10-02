# Specs

Every phase of opcode wrote its spec before the code. This index is
the map: what each file is, and whether it still describes the
software or has been overtaken by a later phase.

**The two living docs.** [opcode-architecture.md](opcode-architecture.md)
and [tui-spec.md](tui-spec.md) are kept current against the code —
when one of them and the code disagree, the fix lands in the same
change. Read those two first; they are what a new reader wants.

**The phase specs are a record.** They describe intent at a point in
time. A phase that was later revised keeps its original text (with a
note at the top when the revision matters) so the reasoning survives
the change that replaced it. Where a spec and the code now disagree,
the code is right and the spec is history — unless the spec is one of
the two living docs above.

What actually happened phase by phase, including what was verified
live and what was not, is in [../../PROGRESS.md](../../PROGRESS.md)
and its full chronological log,
[../progress-log.md](../progress-log.md).

## Index

| Phase | Spec | One line | Status |
|---|---|---|---|
| 1 | [phase1-tui.md](phase1-tui.md) | Replace the bare loop with a Bubble Tea TUI: streaming, status line, permission prompts, steer vs. follow-up | shipped |
| 2 | [phase2-modes.md](phase2-modes.md) | Permission modes as a user-facing choice | **superseded** by [phase30](phase30-sandbox-gate.md) — its decision matrix no longer holds |
| 3 | [phase3-skills-trust.md](phase3-skills-trust.md) | Project trust gate + the `SKILL.md` loader | shipped |
| 4 | [phase4-mcp.md](phase4-mcp.md) | MCP server manager (stdio, tool discovery) | shipped |
| 5 | [phase5-subagents.md](phase5-subagents.md) | Subagent manager — a second orchestrator, narrower scope | shipped |
| 6 | [phase6-polish.md](phase6-polish.md) | Sessions + resume, compaction, audit log, headless `-p`/`--json`, packaging | shipped |
| 13 | [phase13-plan-mode.md](phase13-plan-mode.md) | Plan mode: research, present a plan, switch to acting on approval | shipped |
| 18 | [phase18-todos.md](phase18-todos.md) | `todo_write` plus a live todo panel | shipped |
| 19 | [phase19-reasoning-diff.md](phase19-reasoning-diff.md) | Show reasoning as it streams; render writes as diffs | shipped |
| 20 | [phase20-codex-restyle.md](phase20-codex-restyle.md) | Restyle toward Codex's design language | shipped |
| 21 | [phase21-landlock.md](phase21-landlock.md) | Landlock confinement for shell commands | shipped |
| 22 | [phase22-image-paste.md](phase22-image-paste.md) | Ctrl+V attaches a clipboard image; the model sees it | shipped |
| 23 | [phase23-provider-auth.md](phase23-provider-auth.md) | One credential chain for any provider; `/login <provider>`, `/logout` | shipped |
| 25 | [phase25-providers.md](phase25-providers.md) | Built-in provider catalog + a native Anthropic client | shipped |
| 26 | [phase26-model-catalog.md](phase26-model-catalog.md) | Live per-provider model lists; `/models` and the `/login` picker | shipped |
| 30 | [phase30-sandbox-gate.md](phase30-sandbox-gate.md) | Make the gate sandbox-aware; consolidate to three modes | shipped (mode naming since revised) |
| 32 | [phase32-safe-sanitizer.md](phase32-safe-sanitizer.md) | `internal/safe` — strip terminal escapes at the display boundary | shipped |
| 33 | [phase33-doctor.md](phase33-doctor.md) | `/doctor`: one line per subsystem, each naming the next step | shipped |
| 34 | [phase34-themes.md](phase34-themes.md) | `/theme` as a user choice, with live preview | shipped |
| 35 | [phase35-diff.md](phase35-diff.md) | `/diff`: the working tree, colored, untracked files included | shipped |
| 36 | [phase36-latex.md](phase36-latex.md) | LaTeX math renders as Unicode, code and currency untouched | shipped |
| 37 | [phase37-grant-scopes.md](phase37-grant-scopes.md) | "Don't ask again" grants keyed to a canonicalized command, never wider than shown | shipped |
| 38 | [phase38-sandbox-denial.md](phase38-sandbox-denial.md) | Readable sandbox denials, and keep a failing tool's output | shipped |
| 39 | [phase39-effort-knob.md](phase39-effort-knob.md) | The reasoning-effort dial, cycled with alt+./alt+, | shipped |
| 40 | [phase40-highlight-guardrails.md](phase40-highlight-guardrails.md) | Bound syntax highlighting so a pathological input can't stall the display | shipped |
| 41 | [phase41-compaction-baseline.md](phase41-compaction-baseline.md) | Compaction: window-baseline accounting and the recap injection rule | shipped |
| 42 | [phase42-typed-input.md](phase42-typed-input.md) | One typed decision for composer input (send / steer / queue / command / shell) | shipped |
| 43 | [phase43-production-audit.md](phase43-production-audit.md) | Whole-codebase audit: security, concurrency, UX, slop — findings triaged | shipped |
| 44 | [phase44-release-hardening.md](phase44-release-hardening.md) | Release hardening: races, CI/CD, `opcode update`, installer checksums | shipped |
| 45 | [phase45-ui-restyle.md](phase45-ui-restyle.md) | UI restyle: the composer joins the box language; a one-line greeting lockup | shipped |

Phases 7–12, 14–17, 24, 27–29 and 31 have no spec file of their own;
their work is recorded in the [build log](../progress-log.md) and
folded into [tui-spec.md](tui-spec.md).

## Status vocabulary

- **shipped** — the code is in the tree and the phase's verification
  notes are in the build log.
- **superseded** — the text is kept as the record of a decision, but a
  later phase changed the behaviour. The note at the top of the file
  says what replaced it.

## Related

- [../releasing.md](../releasing.md) — cutting a release, and what the
  pipeline does and does not verify.
- [../reference/codex-adoption.md](../reference/codex-adoption.md) —
  the menu opcode picked from, and the reasoning, for features adopted
  from the Codex CLI.
