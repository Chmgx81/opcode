# opcode — Architecture & Implementation Plan (v0.1)

## 1. Recap: Goals & Constraints

- A terminal-based coding agent harness — "your own personal dev team" — covering the full SDLC: edit, write, run commands, end to end.
- Modular by design: skills, MCP, subagents, modes.
- Dead simple — both to use and to read the source of.
- Multi-audience: non-technical users, beginner-intermediate devs, security/systems architects, engineers, vibe coders.
- Fast, clean, responsive TUI — reference feel: Claude Code, Antigravity CLI, pi.
- **Base language: Go** — single static binary for distribution, goroutines for concurrency, Bubble Tea for the TUI.
- **LLM access: OpenRouter, OpenAI-compatible schema** — one client speaks one wire format and reaches many underlying models, behind a thin `Provider` interface so a direct/local provider can be added later without touching the Orchestrator (see 3.8).
- **Extension model** (decided):
  - Subagents → in-process (goroutines, same trust boundary as the harness itself)
  - Skills → in-process loader, but data-driven: a folder with `SKILL.md` + optional subprocess script, any language
  - MCP → out-of-process (this is inherent to the protocol, not a choice)
  - Modes → in-process config/system-prompt switches, no extension mechanism needed

  Two unrelated things both get called "modes" below — kept distinct on purpose: **Permission Mode** (plan / build / full-auto, Section 7) governs what the agent's allowed to *do*; **Invocation Mode** (interactive / headless, Section 3.9) governs *how opcode itself is launched and driven*.

---

## 2. High-Level Architecture

```
┌──────────────────────────────────────────────────────────────┐
│                          TUI Layer                             │
│               (Bubble Tea — Model / Update / View)              │
└──────────────────────────────┬─────────────────────────────────┘
                                │ tea.Msg (input, streamed tokens,
                                │          status, permission prompts)
┌───────────────────────────────▼────────────────────────────────┐
│                      Agent Orchestrator                         │
│   session state · message loop · mode switch · budget mgmt      │
└──────────┬───────────────────┬───────────────────┬──────────────┘
           │                   │                   │
   ┌───────▼──────┐   ┌────────▼────────┐   ┌──────▼───────┐
   │  Subagent    │   │  Skill Loader   │   │ MCP Manager  │
   │  Manager     │   │  (folder scan + │   │  (stdio only)│
   │  (goroutines)│   │  subprocess run)│   │  process pool)│
   └───────┬──────┘   └────────┬────────┘   └──────┬───────┘
           │                   │                    │
┌──────────▼───────────────────▼────────────────────▼─────────────┐
│                    Tool Execution Layer                          │
│   file read/write/edit · shell exec · git · search · grep        │
│   ── single permission gate lives here ──                        │
└──────────────────────────────┬───────────────────────────────────┘
                                │
┌───────────────────────────────▼───────────────────────────────┐
│                     LLM Provider Client                        │
│      (OpenRouter — OpenAI-compatible, streaming tool_calls)     │
└──────────────────────────────────────────────────────────────┘
```

**Why this shape:** everything that touches the outside world (files, shell, network) funnels through one Tool Execution Layer with one permission gate — regardless of whether the call came from a built-in tool, a skill, or an MCP server. That's the single decision that makes the security story simple enough to explain to your security-architect users and simple enough to actually implement correctly.

---

## 3. Core Components

### 3.1 TUI Layer
- Bubble Tea Model/Update/View. No alt-screen trickery beyond what Bubble Tea gives you for free.
- Panes: input box, message/output stream, status bar (model, mode, cwd, token usage, active subagents), permission-prompt overlay.
- Streaming: partial tokens arrive as `tea.Msg` and get appended incrementally — never buffer a full response before showing anything.
- Non-blocking: the TUI never blocks on a tool call. Long-running operations show a spinner/progress line and are cancelable with Esc/Ctrl+C.
- **Steer vs. follow up** — the user can send input while the agent is still working, and the two intents are handled differently: a **steering message** (Enter) interrupts after the current tool call finishes and gets folded into context immediately, redirecting the in-flight turn; a **follow-up** (Alt+Enter) queues silently and is only sent once the current turn completes. This matters in practice — without it, a user who spots a mistake mid-run either has to wait it out or kill the whole turn to correct it.

### 3.2 Agent Orchestrator
- Owns conversation state: message history, current mode, active subagents, token/budget tracking.
- Drives the core loop: send messages + full tool list to the LLM → receive a tool call → dispatch to the Tool Execution Layer → feed the tool result back → repeat until the model stops.
- Mode determines system prompt, allowed tool set, and default permission posture (ask vs auto-accept).
- **System prompt is composed, not static**: hierarchical AGENTS.md content (Section 5) + the current Permission Mode's instructions + the skills metadata index, assembled fresh each session rather than hand-maintained as one blob.
- **Compaction, not just vague trimming**: when the conversation approaches a configurable fraction of the model's context window (e.g. ~75%), the Orchestrator auto-summarizes the oldest messages into a compact recap and replaces them in place, rather than letting the window fill and degrade silently. Keep the summarization step swappable — a cheap/fast model can do this job even when a more capable one is doing the actual work, which OpenRouter makes trivial to wire up as a second `Provider` call. Beyond compaction: keep the skills index to metadata-only until a skill actually triggers, and don't let a finished subagent's full transcript linger in the parent's context once its result has been folded in.

### 3.3 Subagent Manager
- A subagent is just another Orchestrator instance, scoped to a narrower system prompt and tool subset, running in its own goroutine.
- Reports back to the parent via a channel of typed events (status update, partial output, done, error).
- Parent aggregates subagent activity into the TUI as labeled panes or a collapsible log — the user should always be able to see what each subagent is doing, not just the final result.

### 3.4 Skill Loader
- Scans `.opcode/skills/` (project-level) then `~/.opcode/skills/` (user-level) at startup.
- A skill is a folder, following the same open `SKILL.md` convention as Claude's own Skills and the `agentskills.io` standard:
  ```
  <skill-name>/
    SKILL.md        # required — YAML frontmatter (name, description/triggers) + instructions
    scripts/        # optional — deterministic code the skill can run (any language)
    references/     # optional — reference docs loaded only if the skill body needs them
    assets/         # optional — templates/schemas the skill produces or consumes
  ```
- **Three-tier progressive disclosure**, so an idle skill costs almost nothing in context:
  1. **Metadata (always loaded)** — just `name` + `description` from frontmatter, ~30–80 tokens per skill, part of the permanent skills index.
  2. **Body (loaded on trigger match)** — the full `SKILL.md` instructions, pulled into context only when the model's request matches a skill's description.
  3. **Bundled resources (loaded on demand)** — anything in `references/`/`assets/`, and `scripts/` output, only touched when the skill body actually calls for it.
- **Keep skill bodies lean** — target roughly 1,000–3,000 tokens; move large tables or domain data into `references/` rather than bloating the body. A skill whose main file balloons is a sign it should be split or should offload data to `references/`.
- Invocation of `scripts/` runs as a subprocess with a fixed I/O contract (stdin: JSON context, stdout: JSON result) — same subprocess boundary as MCP, just a far simpler protocol, which is what makes skills genuinely language-agnostic without designing or versioning a plugin API.
- Each skill is assigned a **permission tier** (see Section 7) rather than being implicitly trusted just because it loaded.
- **Prompt templates (optional, low-priority)** — a sibling, much simpler mechanism worth keeping distinct: a folder of plain markdown files (`.opcode/prompts/`) where each file becomes a `/name` slash command that expands verbatim into the input box. No model reasoning, no triggering logic, no execution — pure text substitution, explicitly invoked by the user. Cheap to add whenever there's spare time; doesn't need to be in the initial build order.

### 3.5 MCP Manager
- Reads the server list from config (e.g. `.opcode/mcp.json`).
- Spawns each server over stdio, performs the handshake, discovers its tools. HTTP transport is not implemented; an `http://` server entry is rejected at config load with that reason rather than silently ignored.
- Exposes discovered tools to the Orchestrator as ordinary tool definitions — the model doesn't need to know MCP exists, it just sees more tools available.
- Owns lifecycle: reconnect on crash, clean shutdown on exit, timeout on a slow/dead server so it never blocks startup.
- **Consumption practices worth building in from the start:**
  - Before a user adds any third-party MCP server, surface a reminder to review its source if it's not an official/well-known one — it's about to get filesystem and credential access.
  - Default every server to the narrowest scope that works (project directory only, read-only DB flags where the server supports them) rather than trusting the server's own defaults.
  - Don't keep every connected server's full tool schema resident in context if the count grows large — load what's relevant to the session and drop the rest, the same context-discipline principle as the skill index.
  - During development, an MCP Inspector-style debug view (raw JSON-RPC in/out) pays for itself the first time a server misbehaves.

### 3.6 Tool Execution Layer
- Built-in tools (`internal/tools`, registered in `cmd/opcode/main.go`): `read_file`, `write_file`, `edit_file`, `apply_patch`, `bash`, `grep`, `glob`, `list_dir`, `web_fetch`, `current_time`, `load_skill`, `run_skill_script`, `spawn_subagent`, `todo_write`, `present_plan`, plus whatever the MCP servers expose. Git is reached through `bash`; the one git surface that is *not* the model's is `/diff`, which the user invokes.
- **Every** tool call — built-in, skill, or MCP — passes through one permission gate here. This is where mode (ask vs auto-accept) is enforced and where every action gets logged.
- Sandboxing hook lives here too (see Security, below).

### 3.7 Config & Session
Two scopes, one rule: **user-level is trusted, project-level is not until the user says so** (Section 7).

| User-level (`~/.opcode/`, relocatable with `OPCODE_HOME`) | Project-level (`./.opcode/`) |
|---|---|
| `config.json` — preferences (model, default permission mode, skill paths) | *(never loaded — fingerprinted for trust so adding one later re-asks)* |
| `models.json` — providers, endpoints, model lists | *(not allowed)* |
| `auth.json` — credentials (3.10) | *(never)* |
| `mcp.json` — MCP servers | `mcp.json` — only after project trust |
| `AGENTS.md` — instructions for every project | `AGENTS.md` lives at the project root and in subfolders instead |
| `skills/` | `skills/` — only after project trust |
| `trusted-projects.json` — trust decisions | *(not allowed)* |

There is no `prompts/` directory: prompt-template folders are a
deferred idea in 3.4, not a built feature.

- **Precedence:** user-level built-in defaults only. There is no project-level config to merge — providers, endpoints, credentials, and the permission mode are user-level by construction, so a cloned repo cannot point opcode's API traffic at a server it controls or ship a permissive default mode. A project's `.opcode/config.json` is fingerprinted for trust but never read (Section 7).
- **`models.json` is what makes local and self-hosted models a config change, not a code change.** Each entry is a `base_url`, an optional key reference, and a model list, so any OpenAI-compatible server, such as a local Ollama endpoint, is just another entry. Keep `base_url` configurable from Phase 0 instead of hardcoding OpenRouter's.
  ```json
  { "providers": { "local": { "base_url": "http://localhost:11434/v1", "models": ["<model-name>"] } } }
  ```
- Preferences change by editing `config.json` and restarting; there is no `/config` or `/reload` command today. The loaders are separate components (Section 5), so adding one later is cheap. What does not need a restart: `/theme` persists to `config.json` live, `/login` and `/logout` write a key and take effect on the next turn, and granting project trust re-discovers that project's skills and MCP servers mid-session.
- **Session storage: tree-structured, not a flat log.** Each message is a node; rewinding to any earlier point and continuing creates a new branch instead of overwriting history — all branches live in one session file. This resolves the earlier open question in favor of something more useful than a flat log, without the added operational weight of a database: it's still just a JSON file, just shaped as a tree instead of a list. Worth exposing directly in the TUI later (a `/tree` command) since "redo this differently from three steps back" is a real, common need, not a nice-to-have. Sessions get exported and shared, so credential values are redacted before anything is written (3.10).

### 3.8 LLM Provider Client
- Talks to LLM providers over the **OpenAI-compatible `chat/completions` schema**, via **OpenRouter** as the default provider — one API key, one wire format, access to many underlying models (Claude, GPT, Llama, and others) without writing a client per vendor.
- Built behind a thin interface from day one, precisely because OpenRouter's whole point is multiple models behind one shape — the abstraction is nearly free right now and expensive to retrofit later:
  ```go
  type Provider interface {
      StreamChat(ctx context.Context, req ChatRequest) (<-chan ChatEvent, error)
  }
  ```
  `ChatRequest`/`ChatEvent` are opcode's own internal types, not the wire format. There are two wire implementations behind that one interface: `OpenAICompat` (chat/completions, the default path for every OpenAI-compatible server) and `Anthropic` (the native Messages API, used when a provider's `api` is `anthropic`). Adding a third wire format is a new `Provider` implementation, not a rewrite of the Orchestrator.
- Streaming: consumes SSE `chat.completion.chunk` events. Tool calls arrive as **argument fragments across multiple chunks** — buffer each in-progress call by its index/id until the arguments JSON is complete before dispatching it to the Tool Execution Layer.
- Model selection is a config string, not a code change — OpenRouter model IDs are namespaced (`anthropic/claude-...`, `openai/gpt-...`, `meta-llama/...`), so trying a different model is a `config.json` edit, and adding a whole new endpoint (direct OpenAI, a local server) is a `models.json` entry (3.7). The built-in catalog in `internal/config/providers.go` covers fifteen providers; an explicit `providers` entry in `models.json` always wins over the catalog.

### 3.9 Invocation Modes
Distinct from Permission Mode (Section 7) — this is about how opcode itself is run, not what it's allowed to do:
- **Interactive** — the full TUI, the default and primary mode.
- **Headless / print** — `opcode -p "prompt"` runs one turn non-interactively and prints the result (plain text or `--json` for structured event output), for scripting and CI use by the engineer/security-architect end of the audience.
- This costs almost nothing extra *if* the layering in Section 2 is respected: the Orchestrator already doesn't know the TUI exists, so headless mode is just "skip Bubble Tea, drive the same Orchestrator from a CLI flag, print instead of render." Phase 0's bare input/output loop (Section 8) is effectively a first draft of this — a sign the architecture's decoupling is doing its job rather than a coincidence.

### 3.10 Auth & Secrets
- `~/.opcode/auth.json` — **user-level only, never project-level.** Created with owner-only permissions (0600, in a 0700 directory), with a warning at startup if it's ever looser. If opcode finds a credential in a project-level `.opcode/` file, it refuses to load it and says so, since a project directory is exactly where a key gets committed by accident. Kept separate from `config.json` so config can be freely shared or committed without ever risking a leaked key.
- Each provider's credential can be a literal string, or a `!<command>` value that shells out to the user's own secret manager (`pass`, `1Password`'s `op`, macOS `security`, etc.). Run once, cache for the process lifetime; empty output, a timeout, or a nonzero exit leaves it unresolved rather than silently falling back to something less secure. **`!command` is honored only from the user-level `auth.json`** — never from anything a project supplies, because that would be arbitrary command execution from a cloned repo.
- Falls back to the provider's standard environment variable (`OPENROUTER_API_KEY` for the default provider) if `auth.json` has nothing set — the right default for CI and headless invocation (3.9), where you don't want a credential file on disk at all.
- **`/login` and `/logout` in the TUI**, so a non-technical user never has to hand-edit JSON: bare `/login` lists every provider to configure, `/login <provider>` skips the picker, and the key is masked as it is typed and written to `auth.json` (0600) taking effect on the next turn; `/logout <provider>` removes the stored credential. `/logout` only touches what opcode stored — it doesn't unset environment variables or revoke the key at the provider, and it says so.
- **Credentials never leak out through opcode's own records:** stored key values are redacted from the audit log, the session file, and any export or share.
- **No OAuth in v1, but the door is open.** v1 is API keys entered via `/login`. OpenRouter also offers a browser login (OAuth PKCE) that needs no client registration and hands back a user-controlled API key, so it's a natural later upgrade for non-technical users who'd rather click "authorize" than create a key in a dashboard. Build it as a `/login` option with a paste-the-code fallback for remote/headless machines, where the browser callback can't reach the local process.

---

## 4. Directory Layout (the opcode codebase itself)

```
opcode/
  cmd/opcode/main.go           # wiring only (+ `opcode update`)
  internal/
    tui/            # Bubble Tea front end (model, views, dialogs, pickers)
    orchestrator/   # agent loop, mode logic
    subagent/       # subagent manager
    skills/         # skill discovery + subprocess runner
    mcp/            # MCP client manager
    tools/          # built-in tool implementations + permission gate
    llm/            # Provider interface: OpenAI-compatible + Anthropic Messages clients,
                    #   streaming, live model listing
    config/         # config, models, provider catalog, auth, AGENTS.md context
    session/        # tree-structured session persistence
    trust/          # project trust fingerprints (skills/MCP gate)
    sandbox/        # Landlock confinement + seccomp network block for shell commands
    safe/           # terminal-escape sanitizer for untrusted display text
    update/         # `opcode update` + the cached startup update notice
    headless/       # -p one-turn mode, no TUI imports
  docs/
    specs/          # this file, tui-spec.md, per-phase specs (see specs/README.md)
    reference/      # Codex source audits + the opcode-focused adoption synthesis
    releasing.md    # how a release is cut, and what the pipeline does
    progress-log.md # the full chronological build log (Phase 0 → 49)
  scripts/          # test-install.sh — exercises install.sh against a fake release
  install.sh        # the one-liner installer
  AGENTS.md
```

`internal/` has no other packages; `go list ./internal/...` is the
check.

---

## 5. Startup Sequence

1. Resolve the user directory (`~/.opcode/`, or `OPCODE_HOME`) and load user-level `config.json`, `models.json`, and credentials (3.10).
2. **Check project trust** for the working directory (Section 7). Interactive: prompt if it's new or its fingerprint changed. Headless: untrusted unless explicitly opted in (`--trust` or an env var), because there's no one to ask. The project's `.opcode/config.json` is fingerprinted for trust but never loaded — there is no project-level config; user-level is the only config.
3. Load context files hierarchically — user-level (`~/.opcode/AGENTS.md`) → each parent directory → working directory. Per directory, `AGENTS.override.md` (a personal, gitignore-able replacement for that directory only) beats `AGENTS.md`, which beats `CLAUDE.md` as a fallback so repos already set up for other agents work unchanged. Most-specific wins on conflict. This step doesn't need trust: it's inert text (Section 7).
4. Discover skills (user-level always; project-level only if trusted), build the name+description index.
5. Connect MCP servers in parallel, with a timeout — a slow or dead server must never block startup (user-level always; project-level only if trusted).
6. Initialize the LLM client.
7. Start the TUI and enter the blocking prompt loop.

---

## 6. Request Lifecycle (one turn)

1. User types input → TUI sends `UserInputMsg` to the Orchestrator.
2. Orchestrator appends it to history, sends the full conversation + tool list (built-in + skills index + MCP tools) to the LLM.
3. LLM streams its response; text tokens stream straight to the TUI as they arrive.
4. On a tool call (buffered from streamed argument fragments, see 3.8): the Orchestrator resolves which layer owns it (built-in / skill / MCP) → Tool Execution Layer → permission gate → execute → result streamed back to the LLM.
5. Repeat steps 3–4 until the model returns a stop turn.
6. Orchestrator emits `TurnCompleteMsg`; TUI returns to an input-ready state.

---

## 7. Security & Permissions

- One permission gate (Section 3.6) — a single place to reason about "can this action happen," rather than scattered checks across every tool and skill.
- Default-deny for destructive operations outside the project directory.
- **Every tool, skill, and MCP-provided action carries a permission tier, not just a global mode:**
  | Tier | Meaning | Gate behavior |
  |---|---|---|
  | Read-Only | Can't change anything (read file, search, list, lint) | Always allowed, no prompt |
  | Draft-Only | Produces a proposal but doesn't apply it (a diff, a plan, a generated file in a scratch area) | Allowed to run; applying/committing the result still requires approval |
  | Action-Allowed | Actually mutates state (write file, run shell, git push, call a paid API) | Gated by mode — prompted by default; skippable without a prompt in full-auto, and in build mode only where the sandbox already bounds the call. Either way it is logged |

   Mode (plan / build / full-auto) sets the *default* posture across tiers, but the tier is what the gate actually checks per action — this is what lets you honestly tell a security-conscious user "plan mode asks before touching your filesystem" instead of just "trust the prompt."
- MCP server output is untrusted input, same as any external content — never let a tool result silently expand its own permissions or trigger an unreviewed action.
- Audit log of every executed tool call (what, when, which layer it came from, its tier, whether it was approved) at `~/.opcode/audit.jsonl` — this is a genuine differentiator for your security-architect users, and cheap to add if it's built into the gate from day one rather than bolted on later. Known key values are redacted before anything is written.
- The credentials file is refused above the mode matrix, in **every** mode including full-auto: no tool that takes a path may read or write `auth.json`.
- **Project Trust — a separate, earlier gate.** The per-action permission tier above governs what happens once opcode is already running; this one governs whether opcode should run anything from a project at all. Connecting to a *project-declared* MCP server means spawning a process, and that happens before any tool-call ever occurs — so a project's `.opcode/mcp.json` can't be allowed to execute on first `cd` into an unfamiliar repo. Before connecting a project-level MCP server or running a project-level skill's script for the first time, prompt to trust the project; persist the decision in a user-level file (`~/.opcode/trusted-projects.json`) so it's asked once, not every run. AGENTS.md text is the one exception — it loads regardless of trust because it's inert instructions, not executable code, and it's treated with the same caution as any other external content (advisory, not a command opcode blindly follows). Anything under your own `~/.opcode/` is implicitly trusted — you put it there yourself; this gate is only for what a project brings with it.
     - **Trust is tied to what was actually approved, not just the folder.** Record a fingerprint of the project's executable surface (`mcp.json`, `config.json`, skill scripts, `SKILL.md` bodies and their bundled references/assets) next to the path, the same way direnv's allow-list works. If a `git pull` changes any of it, ask again. Otherwise trust granted to a harmless repo silently carries over to a later malicious commit. The prompt should show exactly what will run (the literal MCP server command lines, the skill scripts), not just "trust this folder?".
   - **A trusted project still can't loosen the rules.** There is no
     project-level config to load: providers, endpoints, credentials,
     and the permission mode are user-level only, by construction.
     A cloned repo cannot point opcode's API traffic at a server it
     controls or ship a permissive default mode — the loader never
     reads project config at all (the file is fingerprinted for trust
     so adding one later is trust-visible).
  - **Headless has no one to ask,** so it defaults to untrusted and needs an explicit opt-in (`--trust` or an env var) — the safe default for CI running against a freshly checked-out, possibly external, branch.

---

## 8. Build Order (MVP → full)

| Phase | Scope |
|---|---|
| 0 | Orchestrator + built-in tools + minimal single-pane TUI (no streaming polish) — prove the loop works end to end; this doubles as a first draft of headless Invocation Mode. **User-level config and credentials only** (env var or `auth.json`, configurable `base_url`); no project-level config yet |
| 1 | Real Bubble Tea TUI: streaming, status bar, permission prompts, steer/follow-up input handling, `/login` and `/logout` |
| 2 | Modes (permission) |
| 3 | Skill Loader |
| 4 | MCP Manager |
| 5 | Subagent Manager |
| 6 | Polish: config, tree-structured session + resume, compaction, audit log, headless `-p`/`--json` flag, packaging/distribution |

Subagents are architecturally simple but compound bugs in every other layer — build them last, once the core loop, tools, and permission gate are solid.

**One ordering constraint that overrides the table:** project trust (Section 7) and the restricted project `config.json` must exist before, or together with, Phase 3. Skills are the first point where a cloned repo can make opcode run code, and MCP (Phase 4) repeats it. Until trust exists, load only user-level skills and servers.

---

## 9. Open Questions

These aren't blockers, but worth deciding early rather than mid-build:

- ~~LLM provider abstraction~~ — **resolved:** OpenRouter (OpenAI-compatible) behind a thin `Provider` interface from day one. See 3.8.
- ~~Permission prompt UX~~ — **resolved:** a modal dialog in the composer layer, with the transcript scrolled to it. See [tui-spec.md](tui-spec.md) §3.4.
- ~~Session storage format~~ — **resolved:** tree-structured session file (Section 3.7), not a flat log or a database.
- ~~Can skills define new modes?~~ — **resolved: no.** Mode is a closed enum of three (`tools.Modes` in `internal/tools/policy.go`); a skill is data, not a permission grant.
- ~~Telemetry stance~~ — **resolved: none.** There is no analytics of any kind in the codebase, and the only outbound calls are to the model provider, `web_fetch` on the model's behalf, and the once-a-day release check.

## 10. Explicitly Out of Scope (for now)

Worth naming so it's a decision, not an oversight:

- **A2A (Agent-to-Agent)** — a protocol for delegating to *external, network-hosted* specialist agents (e.g. a vendor's billing agent). Opcode's subagents are in-process and share a trust boundary with the harness, so this doesn't apply yet. Revisit only if opcode ever needs to call out to a third-party agent it doesn't run itself.
- **A2UI (Agent-to-UI)** — a declarative UI protocol for generative interfaces in web/mobile clients (buttons, cards, sliders rendered by a native app). Opcode is a TUI; Bubble Tea's own Model/View already plays this role locally. No separate protocol needed.
- **AP2 / UCP (agentic commerce)** — payment mandates and multi-vendor checkout. Not relevant to a coding harness.
- **Pi's "extensions instead of built-ins" philosophy** — Pi (a genuinely good reference harness) deliberately ships no built-in MCP, subagents, permission popups, or plan mode, arguing you build those yourself via extensions, tmux, or containers if you want them. That's a defensible design for a harness aimed at people who are comfortable doing that. It's the wrong call for opcode specifically: the explicit multi-audience goal includes non-technical users and security-conscious ones who will neither write an extension to get a permission flow nor hand-roll a container sandbox — they need the tiered permission gate (Section 7) and first-class MCP/subagent support working out of the box. Keep this in mind as a philosophy to *not* drift toward under "simplicity" pressure — dead-simple-to-use and minimal-core-that-does-nothing-by-default are different goals, and opcode is committed to the former.
- **A second `SYSTEM.md` file for prompt overrides** (as Pi has, alongside AGENTS.md) — declined. AGENTS.md already covers project conventions, and giving contributors two files with overlapping purpose (which one wins? which one do I edit?) works against the "dead simple" goal for no real gain here.