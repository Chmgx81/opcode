# tilde

A terminal-based coding agent harness in Go. Phase 1: a Bubble Tea TUI
over the proven core loop — streaming responses, permission prompts for
actions, steering mid-turn, and `/login` / `/logout`.

Architecture and build order: [docs/specs/tilde-architecture.md](docs/specs/tilde-architecture.md).
Current status and honest verification notes: [PROGRESS.md](PROGRESS.md).

## Run

```
go build ./cmd/tilde   # binary: tilde
```

Configuration is user-level only for now (`~/.tilde/`, or `$TILDE_HOME`):

- `config.json` — `{"model": "anthropic/claude-sonnet-4.5", "permission_mode": "ask"}`
- `models.json` — providers; defaults to OpenRouter
  (`https://openrouter.ai/api/v1`). Any OpenAI-compatible server works:
  `{"default_provider": "local", "providers": {"local": {"base_url": "http://localhost:11434/v1", "models": ["llama3"]}}}`
- `auth.json` — `{"openrouter": "<key>"}`, or `{"openrouter": "!pass show openrouter"}`
  to fetch from a secret manager. Falls back to `$OPENROUTER_API_KEY`.
  Never read from a project-level `.tilde/` directory.

Then run `tilde` in your project directory.

Keys while a turn is running:

| Key | Effect |
|---|---|
| Enter | steer — folds into the conversation at the next round boundary |
| Alt+Enter | queue a follow-up — sent when the current turn completes |
| Esc | cancel the current turn |
| Ctrl+C | cancel and quit |

Slash commands: `/mode` (show or switch permission mode), `/login`
(store a key, masked input, takes effect immediately), `/logout`
(remove the stored key only — never touches env vars or revokes at the
provider), `/exit`.

Permission modes (`permission_mode` in config.json, or `/mode <name>` at
runtime):

| Mode | Action-Allowed tools (write/edit/shell) |
|---|---|
| `read-only` | not even offered to the model; a call is denied outright |
| `ask-every-time` | prompted each time (default) |
| `auto-accept-safe-ops` | prompted (read-only and draft tools auto-run) |
| `full-auto` | allowed without prompting, still logged |

Read-Only tools never prompt in any mode. Permission prompts (when they
appear): `y` allow, `a` allow action tools for this session, `n`/Esc
deny. The legacy config value `ask` still works and means
`ask-every-time`.

## Skills

A skill is a folder in `~/.tilde/skills/` (always loaded) or
`.tilde/skills/` (project, only after you trust the project) with a
`SKILL.md`:

```
my-skill/
  SKILL.md      # --- name: ... / description: ... --- then instructions
  scripts/      # optional: run via the run_skill_script tool (JSON in, JSON out)
```

Only the name and description sit in the context; the model loads the
body with the `load_skill` tool when a request matches. Scripts run as
subprocesses (Action-Allowed tier, so ask mode prompts).

**Project trust**: the first time tilde runs in a project whose
`.tilde/` contains anything executable (skill scripts, mcp.json), it
asks once and shows the literal files. The decision is stored in
`~/.tilde/trusted-projects.json` with a fingerprint of those files —
if a `git pull` changes them, tilde asks again. `--trust` (or
`TILDE_TRUST=1`) pre-approves for scripted use. Declining leaves the
project's skills unloaded; nothing from it runs.

## Test

```
go test ./...
```

## Layout

```
cmd/tilde              entry point (thin wiring, TUI launch)
internal/tui           Bubble Tea model: streaming, prompts, steer/follow-up
internal/orchestrator  agent loop, UI-independent
internal/tools         built-ins, permission gate + tier policy, audit log
internal/llm           Provider interface, OpenAI-compatible SSE client
internal/config        user-level config + credential resolution
```
