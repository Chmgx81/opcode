<p align="center">
  <pre align="center">
   ▄▄▄▄▄▄▄
▄▄█▀▀▀▀▀▀▀█▄
▀▀         ▀███▄         ▄
               ▀█▄▄▄▄▄▄▄█▀
                 ▀▀▀▀▀▀▀</pre>
  <h1 align="center">tilde</h1>
  <p align="center">a terminal coding agent, in one Go binary</p>
</p>

<p align="center">
  <a href="https://pkg.go.dev/github.com/Chmgx81/tilde"><img src="https://img.shields.io/badge/go-1.24-16DB65.svg" alt="go 1.24"></a>
  <a href="https://github.com/Chmgx81/tilde/releases"><img src="https://img.shields.io/github/v/release/Chmgx81/tilde?color=16DB65&label=release" alt="release"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-16DB65.svg" alt="MIT"></a>
  <a href="https://github.com/Chmgx81/tilde/actions"><img src="https://img.shields.io/github/actions/workflow/status/Chmgx81/tilde/ci.yml?label=ci" alt="ci"></a>
</p>

---

tilde is a coding agent that lives in your terminal: it reads and writes
files, runs shell commands, and streams markdown answers as it works —
under your permission system, not around it. Any OpenAI-compatible
endpoint works (OpenRouter out of the box, Ollama/vLLM for local runs).

```sh
go install github.com/Chmgx81/tilde/cmd/tilde@latest
```

## Features

- **Streaming TUI** — markdown-rendered answers, a collapsible tool
  timeline (`ctrl+r`), per-entry diffs, and a one-line composer that
  grows with your text.
- **Permission tiers** — read-only tools always run; writes, shell, and
  MCP tools prompt (or don't, if you say so). Four modes, one keypress
  to cycle.
- **Steering** — Enter mid-turn to steer at the next round boundary,
  Alt+Enter to queue a follow-up, Esc to stop everything (queue
  included).
- **Sessions** — tree-structured history in `~/.tilde/sessions/`,
  resume from `/sessions` in the TUI or `--continue` at launch.
- **Skills** — drop a `SKILL.md` folder in; the model sees the index
  and loads the body only when it matches. Project skills stay locked
  until you trust the project.
- **MCP** — stdio servers from `mcp.json`, parallel startup, crash
  restart, tools namespaced `mcp__<server>__<tool>`.
- **Subagents** — the model can delegate; same gate, same audit log,
  no recursion by construction.
- **Headless** — `tilde -p "..."` (text or `--json` events) for
  scripts and CI.

## Install

**Binaries** (Linux, macOS, Windows; amd64 + arm64) from the
[releases page](https://github.com/Chmgx81/tilde/releases) — every tag
is built by CI.

**Go**:

```sh
go install github.com/Chmgx81/tilde/cmd/tilde@latest
```

**From source**:

```sh
git clone https://github.com/Chmgx81/tilde
cd tilde && go build ./cmd/tilde
```

## Quick start

Everything lives user-level in `~/.tilde/` (override with `$TILDE_HOME`).
Three files, two optional:

```sh
# 1. pick a model — any OpenAI-compatible model name
echo '{"model": "anthropic/claude-sonnet-4.5", "permission_mode": "ask-every-time"}' > ~/.tilde/config.json

# 2. (optional) non-default provider: any OpenAI-compatible server
echo '{"default_provider": "local", "providers": {"local": {"base_url": "http://localhost:11434/v1", "models": ["llama3"]}}}' > ~/.tilde/models.json

# 3. your key — or skip this and set $OPENROUTER_API_KEY
echo '{"openrouter": "<key>"}' > ~/.tilde/auth.json && chmod 600 ~/.tilde/auth.json
```

Then run `tilde` in a project directory and type. `/login` does step 3
interactively (masked, no restart needed); `/model` switches models at
runtime.

Credentials resolve in order: `auth.json` entry (literal, or
`"!pass show openrouter"` to shell out to a secret manager), then the
provider's environment variable. tilde **never** reads credentials
from a project-level `.tilde/` directory and refuses them loudly.

## Keys

| Key | Effect |
|---|---|
| Enter | send · mid-turn: steer at the next round boundary |
| Ctrl+J | newline in the composer |
| Alt+Enter | queue a follow-up, sent when the turn completes |
| Esc | interrupt the turn — or close the palette, pickers, help, prompts |
| Shift+Tab | cycle permission mode |
| Ctrl+R | expand / collapse tool results |
| `?` | help overlay |
| Ctrl+C | quit |

Large pastes (4+ lines or 1000+ chars) collapse to `[paste N · L
lines]` and re-expand on send. `! <cmd>` runs a shell command directly
(no model round trip). `@<path>` attaches a file's contents.

## Commands

| Command | What it does |
|---|---|
| `/model` | pick a model from `models.json` (or `/model <name>` directly) — provider, key, and audit redactor follow |
| `/sessions` | browse saved sessions, resume in place (current one is saved first) |
| `/mode` | show or switch the permission mode |
| `/skills`, `/mcp` | what's loaded and connected |
| `/login`, `/logout` | store / remove a key (logout never touches env vars or the provider) |
| `/help`, `/exit` | |

## Permission modes

| Mode | Writes, shell, MCP tools |
|---|---|
| `read-only` | not even offered to the model |
| `ask-every-time` | prompted each time (default) |
| `auto-accept-safe-ops` | reads and drafts auto-run; actions still prompt |
| `full-auto` | allowed without prompting, still logged |

Prompts answer with `y`, `a` (this session), or `n`/Esc (deny). Headless
mode fails closed — use `full-auto` for unattended runs.

## Skills

A skill is a folder with a `SKILL.md` (frontmatter name + description,
body = instructions) under `~/.tilde/skills/` (always) or
`.tilde/skills/` (project, after trust). Optional `scripts/` run as
subprocesses with a JSON-in/JSON-out contract. Only the name and
description reach the model's context — the body loads on demand.

**Project trust**: the first run in a project whose `.tilde/` holds
anything executable (skill scripts, `mcp.json`) asks once, showing the
literal files, and fingerprints them. Change them (e.g. a `git pull`)
and tilde asks again. `--trust` / `TILDE_TRUST=1` pre-approves for
scripts.

## MCP & subagents

```json
{"mcpServers": {"fetch": {"command": "npx", "args": ["-y", "some-server"]}}}
```

MCP over stdio (JSON-RPC, newline-delimited; HTTP not yet). Parallel
connect at startup, 5s timeout each, one restart after a crash.
MCP tools are always action-tier — self-reported read-only hints never
lower a tier.

`spawn_subagent` (`{task, title?}`) delegates a self-contained task:
another orchestrator in its own goroutine, same permission gate and
audit log, no spawn tool of its own — recursion is impossible by
construction. Progress streams as labeled `[subagent]` lines.

## Headless & sessions

```sh
tilde -p "run the tests"          # one turn, plain text
tilde -p "..." --json             # one JSON event per line
tilde --continue                  # resume the latest session
tilde --resume ~/.tilde/sessions/<file>
```

Sessions save on exit (tree-structured, credentials redacted, 0600).
Compaction is opt-in: set `context_window` (and optionally a cheaper
`compaction_model`); at 75% of the window, old messages are summarized
into a recap.

Hierarchical `AGENTS.md` context loads automatically — user file, then
each directory from the filesystem root down to the working directory
(`AGENTS.override.md` > `AGENTS.md` > `CLAUDE.md` per directory).

## Layout

```
cmd/tilde              entry point: thin wiring, TUI or headless
internal/tui           Bubble Tea model: streaming, pickers, prompts
internal/orchestrator  the agent loop, UI-independent
internal/tools         built-ins, permission gate + tiers, audit log
internal/llm           Provider interface, OpenAI-compatible SSE client
internal/config        user-level config, credential resolution
internal/skills        SKILL.md discovery (user + trusted project)
internal/trust         project executable-surface fingerprinting
internal/mcp           stdio MCP client + manager
internal/subagent      scoped orchestrator instances, progress events
internal/session       tree-structured sessions, redaction on write
internal/headless      one-shot -p runner (text / --json)
```

## Status & docs

All phases are built and live-verified — honestly, with what was run
versus only compiled recorded per phase:

- [PROGRESS.md](PROGRESS.md) — per-phase build log, verification notes, deferred items
- [docs/specs/](docs/specs/) — the architecture spec and per-phase specs

```sh
go test ./...
```

## License

[MIT](LICENSE)
