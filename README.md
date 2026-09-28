# tilde

```text
   ▄▄▄▄▄▄▄
▄▄█▀▀▀▀▀▀▀█▄
▀▀         ▀███▄         ▄
               ▀█▄▄▄▄▄▄▄█▀
                 ▀▀▀▀▀▀▀
```

**A terminal coding agent, in one Go binary.**
Reads and writes files, runs shell commands, streams markdown — under
your permission system, not around it.

```sh
curl -fsSL https://raw.githubusercontent.com/Chmgx81/tilde/main/install.sh | bash
```

<p>
<a href="https://github.com/Chmgx81/tilde/releases"><img src="https://img.shields.io/badge/go-1.24-2dd4bf.svg" alt="go"></a>&nbsp;
<a href="https://github.com/Chmgx81/tilde/actions"><img src="https://img.shields.io/github/actions/workflow/status/Chmgx81/tilde/ci.yml?label=ci" alt="ci"></a>&nbsp;
<a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-2dd4bf.svg" alt="MIT"></a>
</p>

---

## Quick start

```sh
echo '{"model": "anthropic/claude-sonnet-4.5"}' > ~/.tilde/config.json   # 1. any OpenAI-compatible model
tilde                                                                   # 2. run it in a project
```

No key yet? Start tilde and run `/login` — it lists every provider to
configure; `/login <provider>` skips the picker. Keys are masked,
stored 0600, and take effect immediately.

## Providers

Built in — name one in models.json and it works:

```json
{"default_provider": "anthropic"}
```

| Provider | Endpoint | Key |
|---|---|---|
| `openrouter` (default) | openrouter.ai/api/v1 | `OPENROUTER_API_KEY` |
| `anthropic` | Messages API, native client | `ANTHROPIC_API_KEY` |
| `openai` | api.openai.com/v1 | `OPENAI_API_KEY` |
| `mistral` | api.mistral.ai/v1 | `MISTRAL_API_KEY` |
| `google` | Gemini OpenAI-compat | `GEMINI_API_KEY` |
| `nvidia` | integrate.api.nvidia.com/v1 | `NVIDIA_API_KEY` |
| `groq` · `deepseek` · `together` · `cerebras` · `xai` · `moonshot` · `fireworks` · `qwen` | OpenAI-compatible | `<NAME>_API_KEY` |
| `ollama` | localhost:11434 | none |

Everything not OpenAI-compatible goes through a native client
(Anthropic today); the rest speak chat/completions. Custom endpoints
and proxies still belong in models.json's `providers` block — an
explicit entry always wins over the catalog, and an entry that names
only a base URL merges the rest from it.

Keys resolve in order: the `auth.json` entry (`{"anthropic": "<key>"}`,
or `{"anthropic": "!pass show anthropic"}` to fetch from a secret
manager at first use), then the environment — the provider's
`api_key_env` if models.json names one, else the conventional
`<PROVIDER>_API_KEY`. Credentials never load from a project-level
`.tilde/`.

## The four modes

| Mode | What the model gets |
|---|---|
| `read-only` | read tools only — nothing else is offered |
| `plan` | reads + `present_plan`: it researches, presents a plan, you approve |
| `ask-every-time` | asks you first — the default |
| `full-auto` | runs without prompting, still logged |

Cycle with **Tab**. Approving a plan (`y` implement, `a` implement with
auto-accept) switches the session into a working mode mid-turn — the
next request carries the action tools, no restart. `n` keeps planning.

Safe shell commands can skip the prompt entirely — a token-prefix
allowlist in `~/.tilde/config.json`:

```json
{"safe_commands": ["git status", "git diff", "ls", "go test", "echo"]}
```

`git status --short` matches; `git push` does not, and any shell
metacharacter (`;` `|` `&` `$` backtick, redirections) fails closed —
`git status ; rm -rf /` never auto-runs. The mode's posture still
dominates: read-only and plan deny shell outright.

## Sandbox

On Linux, shell commands run under a kernel Landlock ruleset:
reads and execution anywhere, **writes only to the project
directory, `/tmp`, and dev caches** (`~/.cache`, `~/go/pkg/mod`,
`~/.cargo/registry`, `~/.npm`). A command that tries to write to
`~/.ssh` or your home fails with `Permission denied` — enforced by
the kernel, not by tilde. On by default where the kernel supports
it; `{"sandbox": false}` opts out. PATH bin dirs (`~/go/bin`,
`~/.local/bin`) stay read-only, so a command can't drop an
executable where your shell will find it.

## In the TUI

| Key | |
|---|---|
| **Enter** | send — mid-turn: steer at the next round boundary |
| **Ctrl+J** | newline |
| **Ctrl+V** | attach the clipboard image — the model sees it |
| **Ctrl+E** | edit the composer in `$VISUAL`/`$EDITOR` |
| **Alt+Enter** | queue a follow-up |
| **Esc** | stop — the turn, or whatever is open |
| **Tab** | cycle permission mode |
| **Ctrl+R** | expand / collapse tool results & thinking |
| **?** | everything else |

`/model` switches models at runtime. `/models` browses every
provider's models — fetched live from the provider, never a cached
list — and switching provider + model applies without a restart.
`/sessions` resumes one.
Typing `@` opens a live file picker (type to filter, enter to attach);
`!` turns the composer amber — shell mode, Enter runs it directly, no
model round trip. Big pastes collapse to a token.

## Beyond the loop

**Thinking** — reasoning models' thoughts stream into a dim `△` tail
while it works, then collapse to one line (`ctrl+r` to expand). Writes
and edits render as syntax-highlighted diffs. **Todos** — the model tracks its own multi-step work with
`todo_write`; a live panel shows `☐ pending · ◐ in progress · ☑ done`
with counts, windowed so long lists stay compact.

**Skills** — drop a `SKILL.md` folder in `~/.tilde/skills/`; the model
sees the index, loads the body only when it matches. Project skills
stay locked until you trust the project (a fingerprinted, one-time
prompt). **MCP** — stdio servers from `~/.tilde/mcp.json`. **Subagents**
— the model can delegate; same gate, no recursion. **Sessions** —
tree-structured, resume anytime. **Headless** —

```sh
tilde -p "run the tests"        # one turn, plain text or --json
```

## Also

```sh
go install github.com/Chmgx81/tilde/cmd/tilde@latest   # via Go
tilde --help · tilde --version · tilde --continue     # resume latest
```

Reduced motion: `{"animations": false}` in config.json — the spinner
and toast animations become static glyphs, the information stays.

Light terminals: tilde asks the terminal for its background and
re-skins (dark ink on light fills, deepened accents) — or force it
with `TILDE_THEME=light|dark`.

Screen readers: a detected reader (SCREEN_READER, atk-bridge) turns
animations off for the session and says so — the config key still
wins. The footer reflows on narrow terminals instead of wrapping.

Everything is recorded honestly in [PROGRESS.md](PROGRESS.md) — what
was verified live, what wasn't, and what is deferred. The architecture
and per-phase specs live in [docs/specs/](docs/specs/).

MIT — see [LICENSE](LICENSE).
