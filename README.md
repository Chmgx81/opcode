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
| `read-only` | read tools run free (read_file, list_dir, grep, glob, current_time); every write or command it proposes asks you first |
| `plan` | reads free, `present_plan` free; writes and commands ask. It researches, presents a plan, you approve |
| `ask` | the sandbox is the safety: sandboxed commands and in-tree writes run without prompting; sandbox escapes and out-of-tree writes ask — the default |
| `full-auto` | runs without prompting, still logged |

Cycle with **Tab**. Approving a plan (`y` implement, `a` implement with
auto-accept) switches the session into a working mode mid-turn — the
next request carries the action tools, no restart. `n` keeps planning.
Old configs with `ask-every-time` (or the Phase 1 `ask` spelling) keep
working — the mode was renamed because it no longer asks every time.

Action-Allowed tools ask through a numbered dialog — the literal
command, then `1. Yes`, `2. Yes, and don't ask again for: <command
prefix>:*`, `3. No`. Option 2 grants a session-scoped prefix rule
(`npm init:*` covers `npm init --yes`, never `npm install`; any shell
metacharacter fails closed). Arrows, number keys, and `y`/`a`/`n` all
work; **No** is preselected.

## Sandbox

On Linux, shell commands run under a kernel Landlock ruleset:
reads and execution anywhere, **writes only to the project
directory, `/tmp`, and dev caches** (`~/.cache`, `~/go/pkg/mod`,
`~/.cargo/registry`, `~/.npm`). A command that tries to write to
`~/.ssh` or your home fails with `Permission denied` — enforced by
the kernel, not by tilde. On by default where the kernel supports
it. A model can pass `{"sandbox": false}` when confinement breaks
a command — that escape always goes through the approval dialog in
ask mode. PATH bin dirs (`~/go/bin`,
`~/.local/bin`) stay read-only, so a command can't drop an
executable where your shell will find it.

In-process file writes (`write_file`, `edit_file`) can't be
Landlocked, so the gate bounds them by path instead: writes inside
the same roots run without prompting in `ask` mode (symlinks are
resolved first, so an in-tree path pointing outside still asks);
anything else asks. Where Landlock is unavailable, ask mode
prompts for every action — it never auto-runs a command it cannot
actually confine.

## In the TUI

| Key | |
|---|---|
| **Enter** | send — mid-turn: steer at the next round boundary |
| **↑ / ↓** | recall a previous prompt (persists across sessions) |
| **Ctrl+J** | newline |
| **Ctrl+V** | attach the clipboard image — the model sees it |
| **Ctrl+E** | edit the composer in `$VISUAL`/`$EDITOR` |
| **Alt+Enter** | queue a follow-up |
| **Esc** | stop — the turn, or whatever is open |
| **Ctrl+C / Ctrl+D** | press twice to exit — the first press interrupts a running turn |
| **Tab** | cycle permission mode |
| **Ctrl+R** | expand / collapse tool results & thinking |
| **Ctrl+O** | transcript — scroll the whole conversation, results expanded |
| **?** | everything else |

`/model` switches models at runtime. `/models` browses every
provider's models — fetched live from the provider, never a cached
list — and switching provider + model applies without a restart.
`/sessions` resumes one. `/doctor` diagnoses the whole setup —
config, key, sandbox, trust, MCP, terminal — one line per subsystem
with the next step when something is wrong.
Typing `@` opens a live file picker (type to filter, enter to attach);
`!` turns the composer amber — shell mode, Enter runs it directly, no
model round trip. Big pastes collapse to a token.

Exits are graceful: the first Ctrl+C interrupts and hints, the second
quits, and tilde saves the session and says so on the way out —
`~ tilde — session saved · resume it with /sessions`.

The model's built-in tools: read_file, list_dir, grep (content search), glob_files (pattern find), current_time, apply_patch
(V4A multi-file patches — the format Codex uses), write_file,
edit_file, bash, plus skills, MCP tools, subagents, todo
tracking, and present_plan. Read-tier tools are free in every mode;
apply_patch runs without prompting in ask mode while every file it
touches stays inside the sandbox's writable roots.

Accessibility: for 400 ms after any dialog opens, keystrokes are
swallowed (a fast typist cannot accidentally approve), and
`tilde --plain` — or a detected screen reader — swaps every glyph for
its ASCII form (`✓` → `[ok]`) with animation off. The window title is
sanitized against control and bidi-character injection.

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
