# tilde

A terminal-based coding agent harness in Go. Phase 0: the core agent loop,
proven end to end — the model calls a tool, the tool really executes, the
model sees the real result and responds.

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

Then run `tilde` and type a message. The streamed response and tool calls
print to the terminal (bare loop; the real TUI is Phase 1).

## Test

```
go test ./...
```

## Layout

```
cmd/tilde          entry point (thin wiring, bare I/O loop)
internal/orchestrator  agent loop, UI-independent
internal/tools     built-ins, permission gate, audit log
internal/llm       Provider interface, OpenAI-compatible SSE client
internal/config    user-level config + credential resolution
```
