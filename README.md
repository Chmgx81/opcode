# opcode

```text
 ▄▄▄▄    ▄▄▄▄▄
█▀  ▀█  █▀  ▀█
█▄  ▄█  █▄▄▄▀
 ▀▄▄▀   █
```

**A terminal coding agent, in one Go binary.**
Reads and writes files, runs commands, streams markdown — under your
permission system, not around it.

```sh
curl -fsSL https://raw.githubusercontent.com/Chmgx81/opcode/main/install.sh | bash
```

<p>
<a href="https://github.com/Chmgx81/opcode/releases"><img src="https://img.shields.io/github/v/release/Chmgx81/opcode?color=2dd4bf" alt="release"></a>&nbsp;
<a href="https://github.com/Chmgx81/opcode/actions"><img src="https://img.shields.io/github/actions/workflow/status/Chmgx81/opcode/ci.yml?label=ci" alt="ci"></a>&nbsp;
<a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-2dd4bf.svg" alt="MIT"></a>
</p>

---

## Why opcode

- **Safe by default** — shell commands run in a kernel sandbox; writes,
  commands, and fetches ask before they touch anything outside the project.
- **Any provider** — 15 built in, switched live at runtime; your key never
  leaves your machine.
- **Real work, visible** — streaming markdown, syntax-highlighted diffs,
  reasoning, todos, resumable sessions.

## Quick start

```sh
opcode        # in a project directory — that's the whole setup
```

First run opens the picker: **pick a provider → paste its key → pick a
model from the live list.** No files to edit. Keys are masked and stored
0600 in `auth.json`; the choice is remembered for next launch.

Prefer files, or scripting the setup?

```sh
mkdir -p ~/.opcode
echo '{"model": "anthropic/claude-sonnet-4.5"}' > ~/.opcode/config.json
```

## Providers

| Provider | Endpoint | Key |
|---|---|---|
| `openrouter` *(default)* | openrouter.ai/api/v1 | `OPENROUTER_API_KEY` |
| `anthropic` | Messages API, native client | `ANTHROPIC_API_KEY` |
| `openai` | api.openai.com/v1 | `OPENAI_API_KEY` |
| `mistral` | api.mistral.ai/v1 | `MISTRAL_API_KEY` |
| `google` | Gemini, OpenAI-compatible | `GEMINI_API_KEY` |
| `nvidia` | integrate.api.nvidia.com/v1 | `NVIDIA_API_KEY` |
| `groq` · `deepseek` · `together` · `cerebras` · `xai` · `moonshot` · `fireworks` · `qwen` | OpenAI-compatible | `<NAME>_API_KEY` |
| `ollama` | localhost:11434 | none |

- `/login` stores a key any time (`/login <provider>` skips the picker);
  `/models` fetches a provider's live model list and `/model` switches
  without a restart.
- Custom endpoints and proxies live in `models.json`'s `providers` block;
  an explicit entry always wins over the catalog.
- Keys resolve: `auth.json` (supports `!command` for secret managers),
  then the environment. Credentials never load from a project directory.

## Safety

Three modes, cycled with **Tab**:

| Mode | What happens without asking |
|---|---|
| `plan` | read-only tools; every write, command, or fetch proposes a plan you approve |
| `build` *(default)* | sandboxed commands and in-tree writes run free; anything else asks |
| `full-auto` | nothing — everything still lands in the audit log |

When something asks, the dialog shows the literal command and offers a
scoped "always allow" (prefix rules that fail closed on shell
metacharacters). **No is preselected.**

On Linux, commands run under a kernel **Landlock** ruleset: reads and
execution anywhere, writes confined to the project, `$TMPDIR`, and dev
caches — enforced by the kernel, not by opcode. On x86_64 a seccomp filter
also blocks network sockets. In-process writes are bounded by the same
roots, with symlinks resolved first. Where the kernel can't confine, opcode
asks instead of pretending. Details and the threat model:
[SECURITY.md](SECURITY.md).

## In the TUI

| Key | |
|---|---|
| **Enter** | send — mid-turn: steer at the next tool boundary |
| **↑ / ↓** | recall previous prompts |
| **Ctrl+J / Shift+Enter** | newline |
| **Ctrl+V** | attach the clipboard image |
| **Ctrl+E** | edit the draft in `$VISUAL`/`$EDITOR` |
| **Alt+Enter** | queue a follow-up |
| **Esc** | stop the turn, or close what's open |
| **Ctrl+C / Ctrl+D** | twice to exit — the first interrupts |
| **Tab / Shift+Tab** | cycle permission mode |
| **Ctrl+R** | expand / collapse results and thinking |
| **Ctrl+O** | transcript — the whole conversation, scrollable |
| **Alt+. / Alt+,** | reasoning effort up / down |
| **?** | help overlay |

Commands: `/model` `/models` `/mode` `/sessions` `/skills` `/mcp` `/theme`
`/diff` `/login` `/logout` `/doctor` `/update` `/help` `/exit`. Unknown
commands error in place with the closest match — never a billed model
turn. `@` opens a live file picker; `!` runs a shell command directly.

Built-in tools: `read_file` `list_dir` `grep` `glob` `current_time`
`web_fetch` `apply_patch` `write_file` `edit_file` `bash` `load_skill`
`run_skill_script` `spawn_subagent` `todo_write` `present_plan` — plus
your MCP servers'.

**Skills** — a `SKILL.md` folder in `~/.opcode/skills/`; the model loads
the body only when it matches. **MCP** — stdio servers in
`~/.opcode/mcp.json`. **Subagents** — the model delegates; same gate, no
recursion.

## Scripting and CI

```sh
opcode -p "run the tests"               # one turn, plain text
opcode -p "summarize the diff" --json   # one JSON event per line
opcode --continue                       # resume the latest session
opcode update                           # checksum-verified self-update
```

| Flag | |
|---|---|
| `--plain` | ASCII glyphs, no animation (screen-reader posture) |
| `--trust` | pre-approve the project's executable surface (CI checkouts) |
| `--resume PATH` | resume one session |

| Env | |
|---|---|
| `OPCODE_HOME` | opcode home (default `~/.opcode`) |
| `OPCODE_THEME=light\|dark` | force the palette posture |
| `OPCODE_NO_UPDATE_CHECK=1` | skip the daily update check |
| `OPCODE_ALLOW_LOCAL_FETCH=1` | let `web_fetch` reach loopback/private |
| `OPCODE_INSTALL_DIR` | install.sh destination |
| `OPCODE_VERSION` | install.sh: pin a release |

The update check runs once a day, cached and silent offline; `opcode
update` replaces the binary from a shell and verifies checksums. Source
builds are never offered updates.

Accessibility: a detected screen reader — or `--plain` — swaps every
glyph for ASCII and disables animation; dialogs swallow keystrokes for
400 ms so a fast typist can't accidentally approve; light terminals are
detected and re-skinned.

## Docs

- [SECURITY.md](SECURITY.md) — the sandbox, the gate, the threat model
- [PROGRESS.md](PROGRESS.md) — current state, deferred work
- [docs/specs/](docs/specs/) — architecture and design decisions
- [docs/releasing.md](docs/releasing.md) — cutting a release

MIT — see [LICENSE](LICENSE).
