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

Slash commands: `/login` (store a key, masked input, takes effect
immediately), `/logout` (remove the stored key only — never touches env
vars or revokes at the provider), `/exit`.

Permission prompts (ask mode, Action-Allowed tools): `y` allow, `a` allow
action tools for this session, `n`/Esc deny. Read-Only tools never prompt.

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
