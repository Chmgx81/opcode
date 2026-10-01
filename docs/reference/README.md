# Reference

Static audits of the [Codex CLI](https://github.com/openai/codex)
Rust source, and the synthesis that decided what tilde would adopt
from it.

These are not tilde documentation — they are the input to a decision,
kept so the reasoning survives. tilde's own behaviour is documented in
[../specs/](../specs/) and [../../README.md](../../README.md).

| Document | What it is |
|---|---|
| [codex-tui-audit.md](codex-tui-audit.md) | The `tui` crate: architecture, the full default keymap, styling, rendering |
| [codex-core-audit.md](codex-core-audit.md) | `core` + `protocol`: the agent loop, sandboxing, approval, tool dispatch |
| [codex-features-audit.md](codex-features-audit.md) | Everything else in `codex-rs`: auth, MCP, exec policy, telemetry, rollout |
| [codex-adoption.md](codex-adoption.md) | **Start here.** The three above, synthesized against tilde's state: what tilde has, what Codex does, and the honest scope of adopting each item |

All four are static source reads. Nothing in them was executed or
benchmarked, and each says so where it matters.

[codex-adoption.md](codex-adoption.md) was written against tilde at
Phase 28 and has not been re-synthesized since, so read it as the
menu it says it is — not as a live TODO list. Items from it have
since been adopted; each of those phases says which item it is
implementing in its own opening line:

| Adoption item | Phase |
|---|---|
| Approval cache keyed by canonicalized command | [Phase 37](../specs/phase37-grant-scopes.md) |
| Readable sandbox denials | [Phase 38](../specs/phase38-sandbox-denial.md) |
| Reasoning-effort knob | [Phase 39](../specs/phase39-effort-knob.md) |
| Syntax-highlight guardrails | [Phase 40](../specs/phase40-highlight-guardrails.md) |
| Compaction baseline + injection rule | [Phase 41](../specs/phase41-compaction-baseline.md) |
| The input decision as one typed place | [Phase 42](../specs/phase42-typed-input.md) |

The audits are also still worth reading for the items tilde
deliberately did **not** take — the reasoning there is as current as
the reasoning for what it did.
