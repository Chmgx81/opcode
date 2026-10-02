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
<a href="https://github.com/Chmgx81/tilde/releases"><img src="https://img.shields.io/badge/go-1.25-2dd4bf.svg" alt="go"></a>&nbsp;
<a href="https://github.com/Chmgx81/tilde/actions"><img src="https://img.shields.io/github/actions/workflow/status/Chmgx81/tilde/ci.yml?label=ci" alt="ci"></a>&nbsp;
<a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-2dd4bf.svg" alt="MIT"></a>
</p>

---

## Quick start

```sh
mkdir -p ~/.tilde
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

## The three modes

| Mode | What the model gets |
|---|---|
| `plan` | read-tier tools (read_file, list_dir, grep, glob, current_time, load_skill) and the draft-only ones (`present_plan`, `todo_write`) run free; every write, command, or fetch it proposes asks you first. It researches, presents a plan, you approve |
| `build` | the sandbox is the safety: sandboxed commands and in-tree writes run without prompting; sandbox escapes and out-of-tree writes ask — the default |
| `full-auto` | runs without prompting, still logged |

Cycle with **Tab** (Shift+Tab goes back). Approving a plan (`y` implement, `a` implement with
auto-accept) switches the session into a working mode mid-turn — the
next request carries the action tools, no restart. `n` keeps planning.
Old configs naming `read-only`, `ask`, `ask-every-time`, or
`auto-accept-safe-ops` keep working — they normalize to the
restrictive side (`read-only` → `plan`, the rest → `build`).

Action-Allowed tools ask through a numbered dialog — the literal
command, then `1. Yes`, `2. Yes, and don't ask again for: <command
prefix>:*`, `3. No`. Option 2 grants a session-scoped prefix rule
(`npm init:*` covers `npm init --yes`, never `npm install`; any shell
metacharacter fails closed). Arrows, number keys, and `y`/`a`/`n` all
work; **No** is preselected.

## Sandbox

On Linux, shell commands run under a kernel Landlock ruleset:
reads and execution anywhere, **writes only to the project
directory, `$TMPDIR`, and the dev caches that exist on this
machine** (`$XDG_CACHE_HOME` or `~/.cache`, `~/go/pkg/mod`,
`~/.cargo/registry`, `~/.npm`, plus `$GOCACHE` / `$GOMODCACHE` when
set). A command that tries to write to `~/.ssh` or your home fails
with `Permission denied` — enforced by the kernel, not by tilde.
Landlock landed in Linux 5.13; tilde probes the kernel's ABI and
grants exactly the rights that version defines. On by default where
the kernel supports it. A model can pass `{"sandbox": false}` when
confinement breaks a command — that escape always goes through the
approval dialog in plan and build modes. PATH bin dirs (`~/go/bin`,
`~/.local/bin`) stay read-only, so a command can't drop an
executable where your shell will find it.

On x86_64 Linux a seccomp filter also denies `AF_INET`, `AF_INET6`
and `AF_PACKET` sockets to sandboxed commands — a confined command
that could read your files still could not phone home. The filter is
hand-written x86_64 BPF, so on other architectures the file
confinement applies and the network stays open; the startup status
line says which you got. See [SECURITY.md](SECURITY.md) for what
the sandbox is and is not.

In-process file writes (`write_file`, `edit_file`) can't be
Landlocked, so the gate bounds them by path instead: writes inside
the same roots run without prompting in `build` mode (symlinks are
resolved first, so an in-tree path pointing outside still asks);
anything else asks. Where Landlock is unavailable, build mode
prompts for every action — it never auto-runs a command it cannot
actually confine.

## In the TUI

| Key | |
|---|---|
| **Enter** | send — mid-turn: steer at the next round boundary |
| **↑ / ↓** | recall a previous prompt (persists across sessions) |
| **Ctrl+J / Shift+Enter** | newline |
| **Ctrl+V** | attach the clipboard image — the model sees it |
| **Ctrl+E** | edit the composer in `$VISUAL`/`$EDITOR` |
| **Alt+Enter** | queue a follow-up |
| **Esc** | stop — the turn, or whatever is open |
| **Ctrl+C / Ctrl+D** | press twice to exit — the first press interrupts a running turn |
| **Tab / Shift+Tab** | cycle permission mode forward / back |
| **Ctrl+R** | expand / collapse tool results & thinking |
| **Ctrl+O** | transcript — scroll the whole conversation, results expanded |
| **Alt+. / Alt+,** | reasoning effort — low / medium / high, or the provider default |
| **?** | help overlay — every key and command (empty, idle composer) |

`/model` switches models at runtime. `/models` browses every
provider's models — fetched live from the provider, never a cached
list — and switching provider + model applies without a restart.
`/sessions` resumes one. `/mode` shows or switches the permission
mode. `/skills` and `/mcp` list what is loaded. `/theme` picks the
palette — dark, light, or the original green — with a live preview;
esc restores. `/diff` shows the working tree's git changes, colored,
untracked files included. `/login` and `/logout` store and remove a
provider's key. `/doctor` diagnoses the whole setup — version, config, key, sandbox,
trust, MCP, update check, terminal — one line per subsystem with the next step
when something is wrong.
`/update` does not update in place — it points at `tilde update`, which
replaces the binary from a shell. `?` or `/help` lists every key and command;
/`/exit` (or `/quit`) leaves. Unknown `/commands` error in place with a
suggestion instead of billing a model turn.
Typing `@` opens a live file picker (type to filter, enter to attach);
`!` turns the composer amber — shell mode, Enter runs it directly, no
model round trip. Big pastes collapse to a token. LaTeX math in
answers renders as readable Unicode (`\alpha` → α); code, shell
variables, and currency are never touched.

Exits are graceful: the first Ctrl+C interrupts and hints, the second
quits, and tilde saves the session and says so on the way out —
`~ tilde — session saved · resume it with /sessions`.

The model's built-in tools: read_file, list_dir, grep (content
search), glob (pattern find), current_time, web_fetch, apply_patch
(V4A multi-file patches — the format Codex uses), write_file,
edit_file, bash, load_skill and run_skill_script, spawn_subagent,
todo_write, and present_plan — plus whatever your MCP servers
expose. Read-tier tools are free in every mode; apply_patch runs
without prompting in build mode while every file it touches stays
inside the sandbox's writable roots.

Accessibility: for 400 ms after a permission, plan, or trust dialog opens, keystrokes are
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
prompt). **MCP** — stdio servers from `~/.tilde/mcp.json`, and from
a project's `.tilde/mcp.json` once that project is trusted.
**Subagents** — the model can delegate; same gate, no recursion.
**Sessions** — tree-structured, resume anytime. **Headless** —

```sh
tilde -p "run the tests"        # one turn, plain text or --json
```

## Also

```sh
go install github.com/Chmgx81/tilde/cmd/tilde@latest   # via Go
tilde --help · tilde --version · tilde --continue     # resume latest
tilde --resume /path/to/session.json                  # resume one session
tilde --plain                                        # ASCII glyphs, no animation
tilde --trust                                        # pre-approve a CI checkout
tilde update [--check]    # update to the latest release (checksum-verified)
```

tilde checks for updates once a day at startup — one small HTTPS
request, cached, silent when offline — and every surface names the
same release:

| Where | What it shows |
|---|---|
| startup | one note in the transcript — `Update available: v1.0.0 → v1.1.0` — plus the command to run |
| the footer | a dim `↑ v1.1.0` beside the mode; it stays until you update, at any terminal width |
| `?` | what the badge means, and the command that acts on it |
| `/doctor` | the same check, plus whether the last one worked |
| `tilde --version` | appends `(update available: v1.1.0 — run tilde update)` — from the cache, never the network |
| the exit line | appends `· update available: v1.1.0 — run tilde update` to the session-saved line |

`tilde update` runs in a shell, not inside a session: it replaces the
binary the TUI is running from. Opt out of the check with
`"update_checks": false` in config.json or `TILDE_NO_UPDATE_CHECK=1`.
Source builds and `go install` builds are not release builds, so
nothing is ever offered for them. Only the latest release gets
security fixes.

| Env | What it does |
|---|---|
| `TILDE_HOME` | tilde home dir (default `~/.tilde`) |
| `TILDE_THEME=light\|dark` | force the palette posture |
| `TILDE_PLAIN=1` | ASCII glyphs, no animation |
| `TILDE_TRUST=1` | pre-approve the project's executable surface |
| `TILDE_NO_UPDATE_CHECK=1` | skip the startup update check |
| `TILDE_ALLOW_LOCAL_FETCH=1` | let web_fetch reach loopback/private addresses |
| `TILDE_INSTALL_DIR` | install.sh destination (default `~/.local/bin`) |
| `TILDE_VERSION` | install.sh pinned release tag (default: latest) |
| `TILDE_SKIP_CHECKSUM=1` | install.sh: skip sha256 verification (last resort) |
| `TILDE_RELEASE_BASE_URL` | install.sh: release root (mirrors, testing) |

Reduced motion: `{"animations": false}` in config.json — the spinner
and toast animations become static glyphs, the information stays.

Light terminals: tilde asks the terminal for its background and
re-skins (dark ink on light fills, deepened accents) — or force it
with `TILDE_THEME=light|dark`.

Screen readers: a detected reader (SCREEN_READER, atk-bridge) turns
animations off for the session and says so — the config key still
wins. The footer reflows on narrow terminals instead of wrapping.

Everything is recorded honestly in [PROGRESS.md](PROGRESS.md) — the
current state, what is deferred, and where to look.
[docs/progress-log.md](docs/progress-log.md) is the full
chronological build log: every phase, what was verified live and
what was not. The architecture and per-phase specs live in
[docs/specs/](docs/specs/) (start with its [index](docs/specs/README.md)).

MIT — see [LICENSE](LICENSE).
