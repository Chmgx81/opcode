# Phase 3 Spec — Project Trust + Skill Loader

## Goal

Build the trust gate (Section 7) and the Skill Loader (Section 3.4) —
together, per the build order's constraint: skills are the first place a
cloned repo can make tilde run code, so trust must exist first.

## Non-Goals

- MCP (Phase 4) — but `.tilde/mcp.json` is already part of the trust
  fingerprint so its arrival doesn't need a trust redesign.
- Project `config.json` keys: the spec defines no allow-list entries yet,
  so Phase 3 allows **zero** project config keys. The file is still
  fingerprinted, so adding keys later is a trust-visible change, not a
  silent one. Loading project config can come when there are keys to
  load.
- Prompt templates (`/prompts/`) — explicitly low priority in the spec.
- AGENTS.md hierarchical loading — part of the startup sequence (§5),
  not the skill loader; still unowned by any phase and noted as such.
- Skill "trigger matching" as code: the model decides from the metadata
  index. Progressive disclosure is enforced mechanically — the body is
  only reachable through the `load_skill` tool — not by a heuristic
  matcher.

## Approach

**Trust (internal/trust):**
- Executable surface = `.tilde/config.json`, `.tilde/mcp.json`, and
  every file under `.tilde/skills/*/scripts/`. A project with none of
  these is trusted by default — there is nothing to run.
- Fingerprint: SHA-256 over each surface file's relative path, size, and
  content hash, in sorted order. Stored in `~/.tilde/trusted-projects.json`
  with the approved file list and timestamp.
- Status: trusted / untrusted (never seen) / changed (fingerprint
  mismatch → ask again, like direnv).
- The trust prompt shows the literal files that will be runnable, not a
  vague "trust this folder?".
- `--trust` flag pre-trusts without prompting (headless/CI posture per
  the spec; headless itself is Phase 6).

**Skills (internal/skills):**
- A skill is a folder: `SKILL.md` (required; flat YAML frontmatter with
  `name` and `description`, then the body) plus optional `scripts/`,
  `references/`, `assets/`.
- Discovery: `~/.tilde/skills/` always; `.tilde/skills/` only when
  trusted. Same name in both → project wins (most specific wins, the
  same rule as config precedence).
- Progressive disclosure, mechanically: the index (name + description
  per skill) is always in the system prompt; the body is only reachable
  via the `load_skill` tool; bundled resources only via the paths the
  body names.
- Skill tiers are structural, not configurable: loading a body is
  Read-Only; running a script is Action-Allowed (it spawns a process).
  No frontmatter can lower that.

**Tools (internal/tools):**
- `load_skill{name}` — Read-Only: returns the SKILL.md body.
- `run_skill_script{skill, script, input}` — Action-Allowed: runs
  `scripts/<script>` from that skill's directory as a subprocess with
  the fixed I/O contract (stdin: JSON, stdout: JSON). Script selection
  is by name against the discovered list — no path traversal. A
  project-scope script only exists in the manager when the project is
  trusted, so trust is enforced by construction.
- Non-executable script files run via `sh`; executable ones run
  directly (shebang languages work either way).

**Wiring:**
- Orchestrator gains `SkillsIndex`, appended to the system prompt before
  the mode instruction; updated in place after a trust grant.
- TUI: at startup, if the project has an executable surface and is
  untrusted or changed, a prompt box shows the runnable files and asks
  y/n. `y` persists trust and re-discovers skills into the same manager
  (the tools see them immediately); `n`/Esc continues with user-level
  skills only, and the transcript says so.

## Edge Cases

- Skill with malformed or missing frontmatter: the whole skill is
  skipped with a warning, not a startup failure — one bad folder must
  not kill the session.
- SKILL.md body target ~1,000-3,000 tokens (spec guidance); loader
  does not enforce, the index keeps cost at metadata-only.
- Script emits non-JSON stdout: contract violation reported back to the
  model as an error result; no silent fallback.
- Fingerprint collision between projects is irrelevant (keyed by path).
- Trust granted mid-session then a script file changes: already-loaded
  manager keeps running it — trust is a startup decision; next launch
  re-asks. Stated plainly.

## Test Plan

- Unit: fingerprint stability/invalidation, status transitions, store
  persistence; frontmatter parsing, bad-skill skipping, project-over-user
  precedence, untrusted-project exclusion; tool happy paths, traversal
  rejection, non-JSON output; skills index in the system prompt; TUI
  trust prompt y/n paths.
- Live: PTY session in a project with a skill script — prompt appears
  with the literal file list, `y` makes the skill callable, `n` leaves it
  out; then a live OpenRouter run where the model triggers a real
  user-level skill via `load_skill` and follows its instructions.
