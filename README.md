# tilde

A terminal-based coding agent harness in Go: a Bubble Tea TUI over a
UI-independent agent loop — streaming markdown responses, a collapsible
tool timeline, permission modes, steering mid-turn, skills, MCP,
subagents, sessions, and a `/model` picker.

Architecture and build order: [docs/specs/tilde-architecture.md](docs/specs/tilde-architecture.md).
Current status and honest verification notes: [PROGRESS.md](PROGRESS.md).

## Install / Run

```
go install github.com/Chmgx81/tilde/cmd/tilde@latest   # from the repo
go build ./cmd/tilde                                    # from a checkout: binary "tilde"
```

One-shot (headless) mode runs a single turn without the TUI:

```
tilde -p "explain what this repo does"           # plain text
tilde -p "run the tests" --json                 # one JSON event per line
```

Permissions fail closed headless — with nobody to ask, Action-Allowed
tools are denied (visible in the output); use `full-auto` for
unattended automation. `--trust` / `TILDE_TRUST=1` pre-approves the
project's executable surface, per Section 7's headless posture.

Sessions are saved to `~/.tilde/sessions/` (tree-structured, with
credentials redacted). Resume with `tilde --continue` or
`tilde --resume ~/.tilde/sessions/<file>`.

Compaction (Section 3.2) is opt-in: set `context_window` in
config.json to the model's token window, and optionally
`compaction_model` for a cheaper summarizer; at 75% of the window the
oldest messages are auto-summarized into a recap.

Hierarchical `AGENTS.md` context loads automatically: the user-level
file, then each directory from the filesystem root to the working
directory (`AGENTS.override.md` > `AGENTS.md` > `CLAUDE.md` per
directory).

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

Slash commands: `/mode` (show or switch permission mode), `/model`
(pick a model, or `/model <name>` to switch directly — provider, key,
and audit redactor follow), `/sessions` (browse saved sessions
newest-first and resume in place; the current conversation is saved
first), `/skills`, `/mcp`, `/login` (store a key, masked input, takes
effect immediately), `/logout` (remove the stored key only — never
touches env vars or revokes at the provider), `/exit`.

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

## MCP

Servers are configured in `~/.tilde/mcp.json` (always) or
`.tilde/mcp.json` (only after project trust) using the usual
`mcpServers` shape:

```json
{"mcpServers": {"fetch": {"command": "npx", "args": ["-y", "some-server"]}}}
```

tilde speaks MCP over stdio (JSON-RPC, newline-delimited; HTTP
transport is not supported yet). Discovered tools appear to the model
as `mcp__<server>__<tool>` and are always Action-Allowed — the
protocol's read-only hints are self-reported and never lower a tier.
Servers connect in parallel at startup with a 5s timeout each; a dead
server is skipped with a visible note. A crashed server gets one
restart (with a full re-handshake) on its next call. A project server
can never replace a user server with the same name.

## Subagents

The model can delegate a self-contained task with the `spawn_subagent`
tool (`{task, title?}`). A subagent is another orchestrator instance in
its own goroutine: same provider, same permission gate and audit log
(same trust boundary), narrower system prompt, and no spawn tool of its
own — so subagents cannot recurse by construction. Progress renders in
the transcript as labeled `[subagent title]` lines while it works; the
final answer returns to the parent as the tool result.

## Test

```
go test ./...
```

## Layout

```
cmd/tilde              entry point (thin wiring, TUI or headless)
internal/headless      one-shot -p runner (text and --json event output)
internal/session       tree-structured session storage + resume, redaction on write
internal/subagent      spawn_subagent tool: scoped orchestrator instances, labeled progress events
internal/tui           Bubble Tea model: streaming, prompts, steer/follow-up
internal/orchestrator  agent loop, UI-independent
internal/tools         built-ins, permission gate + tier policy, audit log
internal/llm           Provider interface, OpenAI-compatible SSE client
internal/config        user-level config + credential resolution
```
