# tilde (~) — TUI Design Specification v0.2

> **Status: historical reference, superseded.** This spec was written
> for the Python + Rich prototype lineage. The Go implementation is
> governed by [tui-ux-spec.md](tui-ux-spec.md) — that document wins
> on anything visual or interactive. Kept for its design reasoning
> (animation budget, glyph discipline, append-only transcript), which
> the Go spec absorbed.

**Changes from v0.1:** collapsed the animation model from five decorative primitives to two (spinner, tick — plus stream for text); cut the glyph vocabulary from 17 symbols to 9; removed the two-column gradient banner; flattened the plan-suggestion/approval modal pair into a plain manual mode; replaced the rotating-verb reasoning receipt with a fixed two-state spinner (`thinking` / `working`). Rationale for each cut is inline where it deviates from v0.1, and summarized in §5.

Scope: what the user sees when they run `tilde` in a terminal, and nothing else. If a rule is not needed to render a screen, it does not belong here.

---

## 0. Three rules that generate everything else

1. **Legible under stress.** The user may be reading this right before `rm -rf` runs. Never let decoration compete with a decision.
2. **Scannable, not read.** Skim glyphs and colors in the left gutter; know what happened without reading prose.
3. **One signal, one meaning.** A color, glyph, or indent means exactly one thing, everywhere. Tempted to reuse a color for something new? Make a new token. Tempted to animate something that isn't actually in progress? Don't — animation is reserved for "this is happening right now," full stop.

Corollary: **the transcript is append-only.** We never edit what has already been printed to scrollback. The only exception is a `rich.live.Live` region actively being updated in place (spinner, streaming text, an open modal). Once that region closes, its output is frozen forever, like everything else.

---

## 1. Architecture

### 1.1 What "Rich only" means

- **No alt-screen, no layout tree, no `VerticalScroll`, no dock.** The terminal's native scrollback is the transcript. We append with `Console.print()` and never touch it again.
- **One blocking prompt loop** (`src/tilde/cli.py`). Not a `textual.App`, not a `Live`-driven event loop.
- **At most one `Live` region open at a time**, with a single documented exception (subagent batch, §3.19). A `Live` region is either the spinner (thinking/working/tool-running), streaming assistant text, or an open modal — never more than one of those simultaneously, because they represent mutually exclusive states (you can't be thinking and streaming prose at once).
- **Everything else is a plain `Console.print()`** that goes straight to scrollback.
- **No input focus, no widgets, no CSS.** `print`, ANSI cursor codes, and `input()` are the whole API surface.

Note on flicker: `Live.refresh()` repaints its whole region, not a diff. Because exactly one region is ever open, and its content is small (a spinner line, or a streaming paragraph), the repaint cost stays flat regardless of session length — the thing that made earlier drafts of this spec flicker-prone was running four independent timers (pulse ×3, rotate, tick) that each forced a repaint on their own clock. One region, one clock, per moment.

### 1.2 Composition (what the user sees, top to bottom)

```
  scrollback (grows forever)
  ├─ welcome block                    (printed once, at start, §3.1)
  ├─ user turn                        (composer freeze, §3.2)
  ├─ status line                      (spinner: thinking/working, §3.10 — transient, not frozen unless it resolves to prose)
  ├─ assistant prose                  (no glyph; streaming in a Live region)
  ├─ tool-call lines                  (glyph-prefixed, one per action)
  ├─ diff blocks                      (syntax-highlighted, green/red gutter)
  ├─ todo blocks                      (posted whenever the model chooses to plan, §3.9)
  ├─ subagent lines                   (batch, §3.19)
  ├─ inline images                    (Kitty / Sixel / half-block, §3.15)
  ├─ modal panels                     (Live, resolve to one ⎿ line)
  └─ errors / retries                 (dim, one line each)

  pre-composer block (transient)
  └─ queued steer line                (only when a steer is queued, §3.22)

  composer (2 lines while typing, 4 lines frozen)
  ├─ top rule                         (─ ─ ─ to terminal width)
  ├─ prompt line                      (❯ or ! + input, or placeholder)
  ├─ bottom rule                      (printed after input() returns)
  └─ hint line                        (printed after input() returns)
```

There is no fixed chrome beyond the composer. Everything above the top rule is history.

### 1.3 What this spec does NOT do

- No alt-screen. No `/vim` mode. No fixed status bar pinned to the bottom of the terminal.
- No overlay stack. All pickers, palettes, and modals are printed **above the composer's top rule**, not drawn as floating windows.
- No `textual` import anywhere, not even transitively.
- No animation that isn't tied to a real in-flight operation. If nothing is happening, nothing moves.

### 1.4 Headless mode

`--mode`, `--yes`, `--model`, `--skill`, `--export`, `--image`, `--vision-consent`, `--resume`. Parsed by Typer.

Headless **never** imports `rich`. It writes plain `sys.stdout.write` / `sys.stderr.write`, one event per line, no color, no glyphs.

### 1.5 Module layout for rendering

- **`src/tilde/theme.py`** — `THEME`, glyph tables (9 symbols, §2.2), ASCII fallback, `TildeDark` Pygments style, markdown element map, spinner frame table, `skill_suffixes`.
- **`src/tilde/render.py`** — `render_welcome()`, `render_markdown()`, `render_code_block()`, `render_diff()`, `render_new_file()`, `render_picker()`, `render_modal()`, `render_composer()`, `render_image()`, `render_subagent_batch()`, `render_question_prompt()`, `render_queued_steer()`, `render_status()`.
- **`src/tilde/stream.py`** — the paragraph- and language-aware streaming state machine.
- **`src/tilde/attachments.py`** — clipboard reads, drag-and-drop path unwrapping, image downscale/re-encode (`Pillow`), token management.
- **`src/tilde/vision.py`** — the vision side-call and consent flow.
- **`src/tilde/skills.py`** — skill loading, frontmatter parsing, shadow detection, inferred-suggestion matching.
- **`src/tilde/mcp.py`** — MCP client, tool-call gateway, output fencing.
- **`src/tilde/subagents.py`** — parallel task dispatch, lifecycle events.
- **`src/tilde/queue.py`** — steer queue, delivery to the model at safe boundaries, interrupt escalation.
- **`src/tilde/editor.py`** — `$EDITOR` escape via `Ctrl+E`.

Ten modules, each with one job. (`plan.py` from v0.1 is gone — see §3.18.)

### 1.6 Focus model

At most one interactive surface accepts keystrokes at a time.

| Surface | Focus behavior |
|---|---|
| Composer | Default. Every keystroke goes here. |
| Picker / palette (open above top rule) | Takes focus. Composer inert until dismissed. |
| Modal (confirm) | Takes focus. Composer inert until resolved. |
| `ask_question` panel (§3.21) | Takes focus. Composer inert until answered/skipped/cancelled. |
| Model is thinking / working / streaming | Composer active. User can type a steer, submit for queue (§3.22). |

**Keybinding routing while a non-composer surface has focus:**

- `Tab` / `Shift+Tab` navigate **within** the focused surface, except at the composer where they cycle mode.
- `↑` / `↓` navigate picker rows. In the composer, they recall history.
- `Enter` submits the focused surface. Never falls through to the composer.
- `Esc` dismisses the current surface and returns focus to the composer.
- `Ctrl+C` cancels the current action regardless of surface — global escape hatch. A second `Ctrl+C` within 2s exits.

---

## 2. Design tokens

### 2.1 Color

| Token | Hex | Where it's used | Meaning |
|---|---|---|---|
| `bg` | `#0E0F10` | terminal default, not painted | Near-black, warm-neutral |
| `bg-panel` | `#161719` | inline-code background only | Slightly lighter surface |
| `fg` | `#E4E2DC` | Assistant prose, code content | Primary text |
| `fg-muted` | `#8C8A82` | Tool targets, paths, descriptions | Secondary text |
| `fg-dim` | `#575650` | Hint line, sub-details, placeholder, footer, spinner label | Tertiary text |
| `fg-on-accent` | `#0E0F10` | Text on `accent` background | Dark text for picker selection |
| `border` | `#2A2B2C` | Panel borders, composer rules (idle) | Idle frame |
| `default` | `#C4826B` | Prompt `❯`/`!`, mode label, confirm border, active composer rules | Muted terracotta, "asking" |
| `accept` | `#B8A85C` | Prompt `❯`/`!`, mode label | Muted olive-gold, "proceeding" |
| `plan` | `#7A9BB8` | Prompt `❯`/`!`, mode label, plan-modal border | Muted steel-blue, "read-only" |
| `success` | `#8FBC72` | `✓`, diff `+`, todo `☑`, `[done]`, active-row check | Fresh green: done, added, passed |
| `success-muted` | `#6E9A5C` | String literals inside the syntax theme only | Dimmed green |
| `danger` | `#D96B6B` | `✗`, diff `-`, denied, handoff border | Muted coral: failed, removed, denied |
| `warning` | `#E0A85C` | `⚠`, retry markers, ctx ≥ 80% | Warm amber: needs attention |
| `accent` | `#C9A0DC` | Picker selection background, `@`-match highlight | Soft lavender: focus/selection |

Rules unchanged from v0.1: mode colors are for borders/labels/prompt only, never text content or glyphs; `success`/`danger` are the only colors in diffs; `bg-panel` only for inline code; `fg-on-accent` only as foreground on `accent`; `success-muted` only inside the syntax theme; `accent` background only for picker selection and `@`-match. Contrast checked at ≥4.5:1 against `bg`.

### 2.2 Glyphs (the left gutter is the API — nine symbols, full stop)

| Glyph | Meaning | State |
|---|---|---|
| `●` | Agent action, completed | Static, `fg` |
| spinner (`⋯`/`⋱`/`⋰`, 120 ms rotate) | Something is happening right now — a tool running, the model thinking, a subagent working | Live only. Never appears static. |
| `○` | Pending / not started | Static, `fg-dim` |
| `❯` | User input marker | Static, mode color, bold |
| `!` | Shell escape marker | Static, mode color, bold — same position as `❯` |
| `⎿` | Sub-detail, continuation, or suggestion tied to the line above | Static, `fg-dim`. Implies indentation by itself. |
| `✓` / `✗` | Success / failed | Static, `success` / `danger` |
| `□` / `▸` / `☑` | Todo: pending / active / done | Static. `▸` is bold mode-color, not animated — the model only ever has one active item, so the boldness alone is enough. |
| `⚠` | Needs approval / denied | Static, `warning` / `danger` |

That's the whole vocabulary. Where v0.1 had `◆` (reasoning), `⏵` (mode toast / queued steer), `↳` (inferred suggestion), and `›` (marketplace row), this version reuses the spinner, plain colored text, and `⎿` respectively — see the rationale in §5.

**ASCII fallback.**

| Unicode | ASCII |
|---|---|
| `●` | `*` |
| spinner | `...` / `..` / `.` |
| `○` | `o` |
| `❯` | `>` |
| `!` | `!` |
| `⎿` | `\_` |
| `✓` / `✗` | `[ok]` / `[x]` |
| `□` / `▸` / `☑` | `[ ]` / `[>]` / `[x]` |
| `⚠` | `[!]` |
| `│` (subagent done rule) | `\|` |
| `—` (em dash) | `--` |
| `╭ ╮ ╰ ╯ ─ │` | `+ + + + - \|` |

### 2.3 Spacing

- 1 blank line between turns; zero within a turn.
- No blank line between an action and its `⎿` line.
- No blank line between a diff's file-path header and its `─` rule.
- Blank line before and after a fenced code block in assistant prose.
- 2 columns per indent level. Left gutter is column 0. Nothing is centered.
- Bordered panels: `padding=(0, 1)` for one-line bodies, `padding=(1, 1)` for multi-line.
- The frozen composer contributes 4 lines per turn. Its live form is 2 lines (§3.2.1).
- The queued steer line (§3.22) contributes 1 line, plus 1 blank line separating it from the running turn's output above.

### 2.4 Hierarchy

1. **Color intensity:** `fg` > `fg-muted` > `fg-dim`.
2. **Background fill:** `accent` background is stronger than any foreground color; used only for selection.
3. **Weight:** bold only for active todo item, diff file paths, help key names, the active prompt marker, markdown headers, picker titles.
4. **Indentation:** deeper is more specific.
5. **Glyph:** `●` > `○` > no glyph.
6. **Plain prose outranks every glyph'd line.**

### 2.5 Animation model — three primitives, not five

**The hard constraint:** Rich's `Live` region repaints its whole region on every tick, not a diff. The fewer independent timers running, the less it flickers. v0.1 ran four (three flavors of pulse, plus rotate, plus tick) which could overlap. This version runs at most one timer at a time, because at most one `Live` region is ever open (§1.1).

| Primitive | Mechanism | Timer | Used by |
|---|---|---|---|
| **Spinner** | Cycle 3 frames of the same glyph | 120 ms | tool running, thinking, working, subagent running, stalled/retrying |
| **Tick** | Update a numeric field in place | 100 ms | elapsed seconds next to a running spinner |
| **Stream** | Append text inside the Live region | per chunk | assistant output, streaming code fence, streaming tool output |

Nothing pulses. No glyph or border breathes. A modal's border is static — it doesn't need to move to be noticed; it's the only bordered thing on screen.

**Spinner frame table:**
```
frame 0:  ⋯
frame 1:  ⋱
frame 2:  ⋰
frame 3:  ⋯     loop, 120 ms each
```
This is the *only* rotation table in the whole spec. It's reused verbatim for tool-running, thinking, working, retries, and subagents — one glyph family, one meaning ("in progress"), regardless of what's in progress. Where it appears more than once at a time (subagent batch), each instance runs the same table independently; see §3.19 for why that's still allowed under the one-`Live`-region rule.

**Transition rules.** When a live line resolves, the final frame freezes instantly — no cross-fade, no slide:

| From (live) | To (frozen) |
|---|---|
| `⋯ read_file config.go` | `● read_file config.go` |
| `⋯ run_shell go test ./...` | `✓ run_shell go test ./...` (or `✗` on failure) |
| `⋯ thinking 2.1s` | either nothing (if a tool call follows — see §3.10) or `thought for 2.1s`, `fg-dim`, no glyph |
| Modal, static border | `⎿ approved once` |
| `⋯ ` (subagent row) | `│ explore ... [done]` |

### 2.6 Theme invalidation

Unchanged from v0.1: the palette is fixed at session start. Terminal resize mid-session does not re-wrap frozen lines. `NO_COLOR` set mid-session has no effect until restart. `/theme` is not a v0.1 command.

---

## 3. Screens

### 3.1 Welcome (printed once, at start)

No banner. One identity line, cwd, a rule, a footer. That's the whole thing — it's seen once and then scrolls away; it doesn't deserve more real estate than a screen the user reads every turn.

```
tilde v0.1.0 · kimi-k2.7-code (default) · session_a1e2…

~/dev/tilde
─────────────────────────────────────────────────────────────
? for shortcuts · / commands · @ files · ! shell      kimi-k2.7 · default
```

| Region | Rows | Content |
|---|---|---|
| Identity | 1 | `tilde`, version, model, mode, session id — `fg-muted` except `tilde` bold `fg` and the mode word in mode color |
| cwd | 1 | Blank line above, none below. Left-aligned, `fg`. Home shortened to `~`. Long paths truncate from the **left** with `…/`. |
| rule | 1 | `─` to terminal width, `border` token |
| footer | 1 | Left: keybinding hints, `fg-dim`. Right: model + mode, mode word in mode color. |

Total: 4 rows plus one blank line before cwd. Freezes into scrollback on the first submit.

**Responsive behavior.** Below 60 columns, the footer drops the right-hand model/mode block. Below 40, the identity line drops the session id. There is no banner to degrade, so there's nothing else to specify.

**Safety notice.** Not shown here. Lives in `/help` (§3.12):
```
tilde can read, edit, and run shell commands you approve.
Use in trusted environments only.
```

- **Session always starts in `default` mode**, regardless of last session's end-state.
- Rendered by `render_welcome(ctx) -> Renderable`. No reactive state, no timers.

### 3.2 Composer

The composer has **two states**: live (2 lines, while typing) and frozen (4 lines, in scrollback). The difference is a constraint of `input()`, not a design choice.

#### 3.2.1 Two states

**Live (while typing):**
```
─────────────────────────────────────────────────────────────
❯ Give tilde a goal
```

**Frozen (after submit, in scrollback):**
```
─────────────────────────────────────────────────────────────
❯ fix the flaky test
─────────────────────────────────────────────────────────────
kimi-k2.7 · default · ~/dev/tilde · main +2 · ctx 3% (1.2k / 32k)
```

`input()` is blocking and terminal-native — Rich cannot redraw beneath the cursor while it runs. The bottom rule and hint line print **after** `input()` returns, and mark "the previous turn's input is done," not "typing is done."

#### 3.2.2 Layout

**Live form:** top rule (`─` to terminal width, `border` token) + prompt line (`❯ ` mode color bold, then input or placeholder).
**Frozen form adds:** bottom rule (same as top) + hint line (`fg-dim`, fields left to right, ctx % ≥ 80% in `warning`).

#### 3.2.3 Placeholder text

| Context | Placeholder |
|---|---|
| `default` mode | `Give tilde a goal` |
| `accept-edits` mode | `Build anything` |
| `plan` mode | `Explore, search, and plan` |
| Shell escape (`!`) | `Run a command — e.g., git status` |

Mechanism: print top rule, print `❯ ` + dim placeholder, reposition cursor to column 3 and clear to end of line, then `input()`. One-shot — doesn't reappear on backspace-to-empty. Skipped entirely under `console.legacy_windows`.

#### 3.2.4 Multi-line input

Three coexisting ways to insert a newline: `Shift+Enter` (fast but terminal-support is inconsistent — works in kitty/WezTerm/Ghostty/iTerm2-with-modifiers/Windows Terminal/VS Code, not macOS Terminal.app or most nested tmux), `Ctrl+J` (always works — it's a literal `\n`, distinct from Enter's `\r`), and a trailing `\` before Enter (bash-style continuation, consumed and not shown in frozen output). `?` help shows all three; none is "recommended" over the others because none works everywhere.

#### 3.2.5 Large paste

Bracketed paste mode enabled at session start. Paste ≥4 lines or ≥~1000 chars collapses to `[Pasted text #1 +8 lines]` (`fg-dim`). Backspace deletes the whole placeholder in one keystroke; numbers are reused. Real content is stored off-screen and substituted at submit — the frozen transcript line shows the placeholder, never the raw paste. Thresholds configurable in `~/.tilde/config.json`.

#### 3.2.6 Image paste

`Ctrl+V`/`Cmd+V` reads the OS clipboard (`xclip`/`wl-paste` on Linux, `PIL.ImageGrab.grabclipboard()` on macOS/Windows; silently a no-op over SSH or headless Wayland). Inserts `[Image #N]` (`fg-dim`) at the cursor. Formats: png/jpg/gif/webp/bmp/tiff. Downscaled to 1200px max dimension via `Pillow`, 32MB post-downscale ceiling with a one-line reason on drop. See §3.15 for rendering.

#### 3.2.7 File drag-and-drop

Dragging a file inserts its path at the cursor. Image files are read like a paste (`[Image #N]`); other files insert their path as plain text.

#### 3.2.8 States

**Empty, by mode (live form):**
```
❯ Give tilde a goal          (default)
❯ Build anything             (accept-edits)
❯ Explore, search, and plan  (plan)
```

**Shell escape (`!`) — rules brighten to mode color:**
```
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
! Run a command — e.g., git status
```

**Palette open** (list prints above the top rule):
```
  /model          Switch the active model
  /mode           Switch the active mode
  /skills         Browse and load a skill
  /plugins        Manage plugins
  /export         Write a portable markdown brief
  /diff           View the current session's full diff
  /undo [n]       Revert the last N edits
  /sandbox        Show sandbox policy status
  /config         Edit standing preferences
  /help           Show keybindings and commands
  /clear          Start a fresh session
  /history        Browse past sessions
  /quit           Exit tilde

─────────────────────────────────────────────────────────────
❯ /
```
(`/plan-suggest` from v0.1 is gone — plan mode is now a plain manual mode, §3.18.)

**Modal open:**
```
╭─ Confirm ────────────────────────────────────────────╮
│ run_shell: rm internal/provider/provider_old.go      │
│                                                      │
│ [y] approve once   [n] deny (Enter denies)           │
╰──────────────────────────────────────────────────────╯

─────────────────────────────────────────────────────────────
❯ 
```
Border is static — it does not pulse.

**Queued steer line** (§3.22, no glyph — plain dim text, since it's waiting, not happening):
```
queued · also check the loader tests
─────────────────────────────────────────────────────────────
❯ 
```

#### 3.2.9 Responsive behavior

Rules always stretch to terminal width. Hint line drops fields from the right, rightmost-first: raw token count → ctx % → dirty count → branch name → model → cwd. Never drop the mode word. Below 40 columns, drop the model name. Below 20, drop the rules too — just `❯ ` and input.

#### 3.2.10 What the composer does NOT do

No live bottom rule. No placeholder animation or reappearance. No animated cursor. No syntax highlighting inside the input. No image preview inside the composer — `[Image #N]` is a token, not a thumbnail. No `/vim` mode.

### 3.3 Streaming assistant output

Assistant prose is **unmarked**: no glyph, no indent. `rich.live.Live` region opens on first token, closes at end. Markdown renders live per §3.16.7. Streaming text is never truncated. On cancel, set a flag and let the stream drain, or raise `CancelledError` at the worker boundary — never break out of the stream early.

### 3.4 Tool-call timeline

```
● read_file       internal/config/config.go
● edit_file       internal/provider/provider.go
  ⎿ +12 -3
✓ run_shell       go test ./...
  ⎿ ok    tilde/internal/provider   0.412s
```

- **Verb column fixed-width**, `fg`, left-aligned: `read_file`, `write_file`, `edit_file`, `run_shell`, `mcp_call`, `vision`.
- **Target `fg-muted`.** `⎿` result only when there's something to report.

**Live behavior.** While a tool call is executing, its line shows the spinner glyph in place of the verdict marker:
```
⋯ run_shell       go test ./...
```
On completion it freezes to `✓` or `✗` in place — a swap, not a morph. No separate "Done" line needed; the frozen verdict glyph *is* the done signal.

**Streaming tools** (`run_shell`, `mcp_call`) stream stdout into a nested `⎿` block while running, 4-space prefix, line-buffered (a `\n` triggers a repaint, partial lines wait), capped at 40 visible lines with the oldest dropped from the top, ANSI stripped except `\r` progress-bar overwrites. `Ctrl+C` collapses the block to `⎿ cancelled by user`. On freeze, the block keeps its last 40 lines with `⋯ N earlier lines hidden ⋯` (`fg-dim`) at the top if it was longer.

**Stall detection.** A quiet stream (no bytes for a few seconds, connection open) shows `⋯ stalled — no response for 6s, retrying` — converts to the retry flow (§3.10) if the connection is dead, otherwise resumes in place.

**Grouping.** 2+ consecutive `read_file` calls collapse:
```
● read_file (2 files)    internal/config/config.go, internal/provider/provider.go
```
Any `edit_file`/`write_file`/`run_shell`/`mcp_call` breaks the group.

### 3.5 Diff and new-file rendering

**`edit_file` (diff):**
```
● edit_file       internal/provider/provider.go
  ⎿ +12 -3

 internal/provider/provider.go
 ───────────────────────────────
  42   type Provider interface {
  43  -    Complete(ctx context.Context, req Request) (Response, error)
  43  +    Complete(ctx context.Context, req Request) (Response, error)
  44  +    Stream(ctx context.Context, req Request) (<-chan Chunk, error)
  45   }
```

**`write_file` (new file):**
```
● write_file      internal/provider/stream.go
  ⎿ new file, 34 lines

 internal/provider/stream.go  (new file, 34 lines)
 ───────────────────────────────
  1  package provider
  2
  3  type Chunk struct { ... }
```

File path bold `fg` on its own line with a thin `─` rule beneath; 4-column right-aligned line-number gutter, `fg-dim`; code syntax-highlighted via `Syntax`; `+`/`-` marker unhighlighted, always `success`/`danger`; context lines dimmed. Diffs/new-files over 40 lines truncate in the middle with `⋯ N lines hidden ⋯`.

### 3.6 Modals (confirm)

A modal opens a `Live` region, updates in place when the user answers, closes. Border is **static** — the one bordered thing on screen doesn't need to move to be noticed.

```
╭─ Confirm ────────────────────────────────────────────╮
│ run_shell: rm internal/provider/provider_old.go      │
│                                                      │
│ [y] approve once   [n] deny (Enter denies)           │
│ [a] always allow this session                        │
╰──────────────────────────────────────────────────────╯
```

- Border `default`. Exact command shown verbatim. **Default focus is deny.**
- Resolves to `⎿ approved once` / `⎿ always allowed (this session)` / `⎿ denied`.
- **Permission check order is always `deny → ask → allow`.** Deny-tier rules fire first and can never be bypassed, not even in `accept-edits` mode. `ask` always prompts (fail-closed when non-interactive). `allow` only applies when neither deny nor ask matched.
- **Shell commands are matched per-segment.** A command containing `&&`, `||`, `;`, or `|` is split into segments first; deny/ask is evaluated on each segment, strictest verdict wins. The allow-scope (`[a] always allow`) matches the whole command string as-written.

**Deny tier:** plain inline line, not a bordered panel:
```
✗ denied by policy: reading .env is blocked (secrets)
```

### 3.7 Slash commands and pickers

**Shared shape, every picker:** title, one-line description, search hint, list, count/footer. **Selection is a full-row `accent` background** with `fg-on-accent` text — no leading glyph. The active/current item is marked with a right-aligned `✓` in `success` where relevant. Fuzzy matching via `rapidfuzz`. `esc` dismisses, `enter` selects, `j`/`k` work identically to `↑`/`↓` (every picker, no exceptions). Typing `/` moves focus into the filter field.

| Picker | Rows | Row content | Extra |
|---|---|---|---|
| `/` palette | one per command | name, description | — |
| `/model` | one per model, grouped by provider | model id, purpose blurb | provider header rows are non-selectable dividers; `shift+↑↓` jumps between them; active model marked `✓` |
| `/mode` | 3 (`default`, `accept-edits`, `plan`) | mode name, one-line rule | active mode marked `✓` |
| `/skills` | one per loaded skill | name, purpose (from frontmatter, never SKILL.md body), source tag (project/user) | shadowed skills keep a `[skill]` badge |
| `/plugins` | tabbed: Skills / MCP Servers | name, version, source, status (`install`/`installed`/`connected`/`disabled`) | `Tab` cycles the two tabs; MCP server add/edit/remove is external, in `~/.tilde/mcp.json` |
| `/config` | one per setting | setting name, current value, valid options | `enter` opens a small inline edit prompt below the row; writes to `~/.tilde/config.json` |
| `/history` | one per saved session | session id, relative time, cwd, first-message excerpt | `enter` resumes |

Example (`/mode`):
```
❯ /mode
  Select mode
  Set what tilde is allowed to do without asking. Applies to this session only.

  Mode             Rule
  default          Ask before every action
  accept-edits     Approve file edits, ask on shell
  plan             Read-only, no writes                              ✓

  Showing 3 of 3 · ↑↓ navigate · enter to select · esc to cancel
```

### 3.8 Mode toast

Explicit mode changes print a plain colored line, no glyph — the arrow already carries the meaning:
```
Mode: default → accept-edits
```
in the new mode's color. Auto-demotion (doom-loop protection) gets a reason:
```
Mode: accept-edits → default (3 failed edits to the same file, reassessing)
```

### 3.9 Todo lists

The model can post a todo block whenever it chooses to lay out steps before acting — this isn't gated by a special mode, it's just something the model does (most often right after entering `plan` mode, but not exclusively):

```
● Update Todos
  ☑ Explore current project structure
  ☑ Read config loader and its tests
  ▸ Draft the provider interface change
  □ Identify affected call sites
  □ ~~Add a compatibility shim~~ (cancelled, not needed)
```

Four states, never a percentage bar: `□` pending, `▸` active (bold mode-color, static, exactly one at a time), `☑` done, cancelled shown with strikethrough. Append-only, not bordered.

### 3.10 The reasoning receipt: thinking / working

One status line, live, spinner-led, two possible labels:

```
⋯ thinking     2.1s
```

while the model is generating hidden reasoning tokens, and

```
⋯ working      0.4s
```

the moment reasoning ends and a tool call is about to be dispatched — the bridge between "deciding what to do" and the actual `● verb target` line appearing. Same spinner, same tick, same `fg-dim` styling; only the word changes. No verb rotation, no rotating vocabulary, no `thinking_verbs` setting — the two words say exactly what's happening and nothing more.

**Resolution:**
- If a tool call follows: the `working` line is a bridge and is **not frozen** to scrollback — it's simply replaced in place by the tool's action line (§3.4) once that line is ready to print.
- If reasoning ends into prose instead (no tool call): the `thinking` line freezes to a plain dim record with no glyph, since it's now history rather than something happening: `thought for 2.1s`.
- **Floor duration: 500ms** — bursts shorter than that never render a line at all.
- Each reasoning burst gets its own line; nothing is retroactively edited.

**Headless:** plain `[thinking...]` / `[working...]` on stderr, no timer, no color.

### 3.11 Errors, retries, and handoff

**Provider retries** (spinner, same glyph family as everything else "in progress"):
```
⋯ retrying, model connection timed out, attempt 2/5, next in 4s
```
Error kind named in plain words. Exponential backoff + jitter, honors `Retry-After`. Unretryable errors fail fast:
```
✗ Authentication failed, check your OPENROUTER_API_KEY. No further retries will help.
```
Composer stays responsive — retries run on the async worker, never the main thread.

**Doom-loop handoff:**
```
╭─ Handoff to plan ─────────────────────────────────────╮
│ ✗ Same edit to config_test.go failed 3 times in a       │
│   row (compile error). Reverting to plan mode.          │
│   Nothing further will be changed. Partial diff and     │
│   full history are preserved above.                     │
╰─────────────────────────────────────────────────────────╯
```
Only panel using `danger` as a border. Static, not pulsing. Exactly three things: what got stuck, what the system did, reassurance nothing was lost.

**Startup/config errors** fail before any TUI frame renders — plain stderr, specific reason, classified exit code (§4). **Crash recovery:** session log is append-only and fsynced per entry; a crash loses at most the in-flight turn; next launch offers to resume.

### 3.12 Session history

```
  Recent sessions                                            3 sessions
❯ session_a1e2…   2h ago    ~/dev/tilde        "add streaming to..."
  session_9f3c…   1d ago    ~/dev/tilde        "fix flaky config test"
  session_41b8…   3d ago    ~/other-project    "why is CI red"
```

Selected row = `accent` background, `fg-on-accent` text, no glyph. `↑↓`/`j``k` navigate, `enter` resumes, `esc` cancels. **Resuming restores the mode the session was saved in** — if that was mid-explore in `plan` mode, it resumes in `plan` mode at the last clean turn boundary; there's no separate "detour state" to lose, because plan mode is just a mode (§3.18).

### 3.13 Help

```
  Tab                Cycle mode: default → accept-edits → plan → default
  Shift+Tab          Cycle mode backwards
  Ctrl+C             Cancel current action (press again to exit)
  Ctrl+D             Exit (at empty prompt)
  q                  Quit (at empty prompt, same as Ctrl+D)
  Ctrl+E             Open input in $EDITOR
  ↑ ↓                Command history / navigate pickers
  j k                Navigate pickers (same as ↑↓)
  /                  Filter in picker (focuses search field)
  Shift+↑↓           Jump between groups in a picker
  Shift+Enter        Insert newline (terminal-dependent; see below)
  Ctrl+J             Insert newline (always works)
  \                  at end of line continues to next line
  Ctrl+V / Cmd+V     Paste from clipboard (image supported)
  Esc                Dismiss palette or picker; cancel queued steer
  /  @  !            Commands · file reference · shell escape
  ?                  Show this help

  tilde can read, edit, and run shell commands you approve.
  Use in trusted environments only.
```

### 3.14 Compaction indicator

Ambient: hint line's `ctx` % turns `warning` past 80%, no popup. Event:
```
⋯ [compacted: 14 older turns — goals, findings & decisions kept · see session log]
```
Always `fg-dim`, one line, no animation. Keep the most recent ~20 turns verbatim; summarize older turns; never split a tool-call/result pair across the boundary; reserve ~16k tokens of headroom.

### 3.15 Shell escape (`!`)

Trigger: `!` at an empty prompt. Visual state change (no new tokens): prompt glyph `❯`→`!` in current mode color; top and bottom rule `border`→mode color.

**Submit behavior:**
1. Composer freezes as-is.
2. `plan` mode → inline `✗ denied: plan mode is read-only`, no modal.
3. Deny-tier match (per-segment, §3.6) → inline `✗ denied by policy: …`, no modal. Deny tier: `rm -rf /`/`rm -rf /*`, `sudo` (any form), piped network-fetch-to-shell, `dd if=/dev/`, `mkfs.*`, fork bombs, `chmod -R 777 /`, raw-device writes, forced push to `main`/`master`; reads of `.env`/`~/.ssh`/`~/.aws`/`~/.tilde/credentials.json` denied with a secrets-specific reason.
4. Otherwise, confirm modal opens (§3.6).
5. Approve → `⎿ approved once`, runs. Always-allow → `⎿ always allowed (this session)`, runs, remembered for the session. Deny → `⎿ denied`, doesn't run.

**Execution:**
```
⋯ run_shell       git status
```
freezing to
```
✓ run_shell       git status
  ⎿ On branch main
    Your branch is up to date with 'origin/main'.
```
Timeout 30s (`✗ Failed (timeout 30s)`). Non-zero exit appended to `⎿` in `danger`. Output truncated to last 40 lines. Shell cwd is the project cwd (jailed), not shown per-command.

### 3.16 `@` file reference

```
❯ Look at @conf
  internal/config/config.go
  internal/config/loader_test.go
```
Respects `.gitignore` (via `pathspec`, non-negotiable). Matched chars bold `fg` against unmatched `fg-muted`, scored via `rapidfuzz`. Selected row = `accent` background, no glyph. `Tab`/`Enter` inserts the path. `Esc` dismisses.

A path inserted by `@` is a *mention* (read into context as text). `[Image #N]` is an *attachment* (routed through the image pipeline, §3.15 of v0.1 → §3.15 image section below is folded into vision, see §3.15-vision).

### 3.16b Rendering: markdown, code, syntax, LaTeX

Unchanged from v0.1 — markdown element map, inline-code-vs-fence rules, Pygments `TildeDark` theme, table rendering, paragraph-level streaming deferral, and the Unicode-approximate LaTeX substitution table all carry over as-is. None of it depended on the animation or glyph changes above. (See v0.1 §3.17 for the full tables if needed verbatim; omitted here only to avoid duplicating unchanged content.)

### 3.17 Vision and image attachment

Protocol tiers (richest first): Kitty TGP → Sixel → iTerm2 inline → half-block Unicode → plain-text description, detected once at session start and cached.

```
❯ look at this @screenshot.png and [Image #1]

  ⎿ [Image #1] attached, 1440×900 → 1200×750 (png, 184 KB)

[image renders inline here via the selected protocol]
```

When the active model isn't vision-capable, first use asks once:
```
╭─ Vision side-call ───────────────────────────────────╮
│ The current model is not vision-capable.             │
│ Send [Image #1] to a small vision model for a        │
│ description? This leaves the project sandbox.        │
│                                                      │
│ [y] yes, remember for this session                   │
│ [a] yes, remember for all sessions                   │
│ [n] no (Enter)                                       │
╰──────────────────────────────────────────────────────╯
```
Answer persists to `~/.tilde/config.json` as `vision_consent`, also editable via `/config`. Every use after that is a normal timeline event:
```
✓ vision          describing [Image #1]
  ⎿ A stylized dark-mode terminal screenshot showing...
```
Gated by policy the same as `run_shell`/`mcp_call`. Description fenced as untrusted from that point on.

### 3.18 Plan mode (no detour, no modals)

v0.1 had a "plan detour": a suggestion modal, an auto-heuristic for when to fire it, an explore phase, and an approval modal, all layered on top of the underlying mode. That's gone. `plan` is now exactly as manual as `default` and `accept-edits`: the user enters it with `Tab`/`Shift+Tab` or `/mode`, same as the other two, and it prints the same plain mode-toast line (§3.8).

While in `plan` mode: no writes. `edit_file`, `write_file`, `run_shell` are all denied with `✗ denied: plan mode is read-only`. The model is free to explore (read files, grep, list dirs) and to post a todo block (§3.9) if it wants to lay out a plan before the user switches it back — that's just the model choosing to communicate its plan, not a UI state with its own modals. When the user is satisfied, they switch modes themselves; nothing auto-returns them, and nothing asks them to approve a plan before proceeding, because leaving `plan` mode *is* the approval.

This removes: the plan-suggest auto/always/never setting, the `/plan-suggest` command and picker, Modal 1 (suggestion) and Modal 2 (approval), and the "underlying mode" bookkeeping needed to remember what to return to. One fewer state machine, one fewer pair of modals to test, and it matches how the other two modes already work.

### 3.19 Skills

Three ways to bring a skill into a turn:

**Exact** — `/skill-name` at the start of the prompt. **Inline** — `/skill-name` dropped anywhere inside a longer prompt. **Inferred suggestion** — tilde scans the prompt against every currently-loaded skill's name/description; on a clear match, a hint appears under the composer's hint line, using `⎿` (not a separate glyph — it's a sub-detail of the composer, same as everywhere else `⎿` is used):

```
─────────────────────────────────────────────────────────────
❯ fix the flaky auth test and clean up the diff before I push
─────────────────────────────────────────────────────────────
kimi-k2.7 · default · ~/dev/tilde · main +2 · ctx 3% (1.2k / 32k)
  ⎿ Looks like a fit: /code-review — Tab to use it, Enter to send as typed
```

Purely additive, never blocking. At most one suggestion per turn. Suppressed in headless.

**Shadowing:** a skill whose name collides with a built-in command stays visible with a `[skill]` badge; `/skill:<name>` always resolves to the skill. Row content comes from frontmatter descriptions only.

### 3.20 MCP tools

```
✓ mcp_call        browser-review/navigate  localhost:3000/checkout
  ⎿ 200 OK, 1.2s

✓ mcp_call        browser-review/screenshot
  ⎿ [Image #2] rendered inline (Kitty TGP)
```

Verb is `mcp_call` for every MCP tool; server/tool name in the target column. Approval defaults to `ask`, same confirm modal as `run_shell`. Output fenced as untrusted. Browser use is just an MCP server (Playwright/Chrome DevTools) — no separate `Browser` verb, and its network egress gets the same `ask`-tier default as any network-reaching MCP tool. Streaming MCP tools render the same as `run_shell` (§3.4).

### 3.21 Structured multi-question prompt (`ask_question`)

```
  Waiting on answers for 2 questions                    turn: 7.1s · ctx 53.6k

  1/2  Which mode should I use?
  ○ agent      Full read/write access, runs the plan
  ○ plan       Read-only investigation, no edits
  z ○ Type your own answer here

  ←/→ switch question · ↑/↓ select · space toggle (multi-select) · enter submit
```
Up to 4 questions per call, 2–4 options each; `multiSelect` uses `☑`/`□`; free-text always available (`z`); answers can carry `[Image #N]`. Outcomes: answered, skipped (not an error), cancelled. Headless auto-selects the first option, disclosed in the tool result. Plain `fg`/`fg-muted` throughout — never a risk border, since this only gathers information.

### 3.22 Subagents / parallel view

```
⋯  explore   Explore checkout flow        explore · tilde        running
⋯  explore   Explore infra and CI         explore · tilde        running
│  explore   Explore shared Go libraries  explore · tilde        [done]
│  explore   Explore order services       explore · tilde        [done]
```

Each running row spins independently using the same spinner frame table as everywhere else (§2.5) — this is the sole exception to "one `Live` region at a time," because parallel subagents are genuinely simultaneous events, not decoration. On finish, a row's spinner is replaced by `│` and a right-aligned `[done]` in `success`. `Ctrl+C` cancels all running subagents cooperatively.

### 3.23 Session export

```
❯ /export
● Exported                session_a1e2… → tilde-brief-a1e2.md
  ⎿ 1 file, 3.1 KB — goal, decisions, files touched, next steps
```
Markdown + YAML frontmatter (`session_id`, `project_path`, `model`, `started`, `mode_at_export`, `files_touched`). Body order: Goal → Decisions and constraints → Files touched → Open/next steps. Never includes secrets. Pure I/O, no Rich dependency.

### 3.24 Additional slash commands

`/diff` (pager view of cumulative session diff), `/undo [n]` (revert last N edits via git-shadow checkpoints, prints `● undo   reverted 2 edits (…)`), `/sandbox` (prints jail/timeout/deny-tier/egress/vision policy), `/config` (§3.7 picker), `/sessions` (alias for `/history`).

### 3.25 Queue rendering

Composer stays live while a turn runs (§1.6). **First `Enter`** during a running turn queues a **steer** — delivered at the next safe boundary. **Second `Enter`** while one is already queued escalates to an **interrupt** (same cooperative-cancel path as `Ctrl+C`).

The queue line renders above the top rule, plain and dim, no glyph:
```
✓ run_shell       go test ./...
  ⎿ ok    tilde/internal/provider   0.412s

queued · also check the loader tests
─────────────────────────────────────────────────────────────
❯ 
```
Only one queued message at a time, truncated to one line with `…` if long. On delivery, the line is removed and the message is echoed as a normal `❯` user turn. `Esc` at the composer cancels a queued steer; `Ctrl+C` cancels the turn and any queued steer. Same behavior during a plan-mode explore phase. Headless: no queue, second stdin line ignored with a stderr warning.

### 3.26 Editor escape

`Ctrl+E` opens the composer's current input (tokens expanded back to real content, image tokens replaced with `<!-- image: path.png -->` placeholders) in `$EDITOR`/`$VISUAL`. Exit 0 + changed content replaces the input; exit 0 + unchanged leaves it; non-zero leaves it and prints `editor exited non-zero; input unchanged`. No `$EDITOR`/`$VISUAL` set → `no $EDITOR set · use Ctrl+J for newlines, or set EDITOR in your shell`. Not available while a modal or picker has focus.

---

## 4. Headless / non-interactive parity

No layout tree, no `Live` regions, no `rich` import. Flat stdout, one event per line, Typer for flags.

| Interactive element | Headless equivalent |
|---|---|
| Mode | `--mode default\|accept-edits\|plan` |
| Confirm prompt | `--yes` (confirm-tier only); non-zero exit on deny-tier |
| Compaction marker | Session log only |
| Handoff panel | Plain `ERROR:` line plus non-zero exit |
| `@` file reference | Path passed as an argument |
| Session history | `--resume <id>` |
| Grouping | Disabled; one line per action |
| Retry loop | `[retrying: <kind>, attempt N/5]` on stderr |
| Reasoning receipt | `[thinking...]` / `[working...]` on stderr, no timer |
| Composer / placeholder | Suppressed entirely |
| Image paste | `--image path.png` (repeatable) |
| Vision fallback | Fires the same; asks once via stderr prompt, or `--vision-consent yes\|no` |
| Skills | `--skill <name>` |
| Inferred skill suggestion | Suppressed |
| Session export | `--export <session-id> [--out path.md]` |
| `ask_question` | First option of every question auto-selected, disclosed in the tool result |
| Subagents | Works identically, no inline animation |
| Browser use | Works identically — MCP tools don't distinguish TTY from headless |
| Queued steer | Not applicable (one prompt per invocation) |
| Editor escape | Not applicable |
| Multi-line input | `\n` in the prompt string |
| Markdown / syntax highlighting | Off, plain text |

**Color and glyph styling drop entirely when stdout is not a TTY.**

**Exit codes:** `0` completed, `1` general, `2` config/startup failure, `3` provider error (retries exhausted), `4` doom-loop handoff, `5` denied by policy.

---

## 5. Rationale notes (what changed from v0.1, and why)

- **One `Live` region instead of "at most two."** Every additional simultaneous timer is a chance for two repaints to interleave and flicker. Thinking, working, tool-running, and streaming are mutually exclusive states in practice — there was never a real need for two at once outside subagents, which stay the one documented exception.
- **Three animation primitives instead of five.** Pulse (three separate uses: `●`, `▸`, modal borders) added motion that didn't track anything actually changing — a modal sitting open isn't "in progress," it's "waiting for you." Removing pulse removes an entire timer class for zero information loss: static color already told the user everything the pulse was adding.
- **One spinner table, reused everywhere "in progress" is true**, instead of four different rotation tables (`⋯`/`⋱`/`⋰` for tools, a second cycle for subagents, a separate diamond-pulse for thinking). One shape, one meaning, satisfies §0 rule 3 directly — v0.1 didn't.
- **`thinking` / `working` instead of a rotating verb list.** `Tracing… Correlating… Scoping…` is decoration animating for its own sake — it doesn't tell the user anything actually different is happening underneath, it just moves. Two fixed, literal words say exactly what phase the model is in and nothing more, which is what the receipt is for.
- **Glyph count 17 → 9.** A gutter the user is supposed to *skim, not read* under stress can't ask them to remember 17 shapes. `↳`, `›`, and `⏵` all did "this relates to the line above" or "this is informational" work that `⎿` and plain colored text already covered — merging them cost no information and halved the vocabulary.
- **No banner.** The welcome screen renders once per session and then is gone forever; the tool-call timeline and composer render every turn. Spec and code budget belongs with what's seen repeatedly, not what's seen once. A gradient two-column banner also risks reading as decoration competing with the content above it the moment scrollback fills — which §0 rule 1 rules out on principle, it just took a banner to notice.
- **Plan mode flattened, detour removed.** A suggest-modal → explore → approve-modal state machine duplicates work the mode toggle already does. If `plan` is a real mode the user can enter and leave at will (which it is, per §3.18), asking permission to enter it and then asking permission to leave it is two confirmations for a decision the user already made by pressing Tab. Removing it also removes an entire settings surface (`plan-suggest: auto/always/never`) and a class of "was I resumed into a detour or not" edge cases in session history.
- **Everything not touched by the above — diffs, tables, syntax theme, headless parity shape, permission ordering, LaTeX substitution, export format — carries over from v0.1 unchanged.** None of it was the problem; it's kept exactly as specified there.