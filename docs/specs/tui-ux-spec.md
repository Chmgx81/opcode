# tilde — TUI & UX Specification (v0.1)

> Source of truth for everything the user sees and touches. Read with `AGENTS.md` and `docs/specs/tilde-architecture.md`.
> **Precedence:** this doc wins on anything visual or interactive; the architecture doc wins on anything structural or security-related.
> **References** (Claude Code, Antigravity CLI, Pi) set the *feel and structure*. Do not clone their identity: no copied mascot, spinner verbs, or wording. tilde's identity is the `~`.

---

## 0. Principles

1. **Calm by default.** Dense but readable, one accent color. Chrome recedes; content leads.
2. **Never block the user.** Typing always works. First visible feedback within 100 ms of Enter.
3. **Always show state.** At a glance the user knows: idle, thinking, running X, or waiting on me.
4. **No surprises with power.** Anything that changes files, runs commands, or leaves the machine is visible *before* it happens.
5. **Keyboard first.** The mouse is never required and never captured by default.
6. **Degrade, don't break.** Truecolor → 256 → 16 → none. Unicode → ASCII. TTY → plain/pipe.
7. **Respect the terminal.** Inline scrollback, native selection, native scroll, and terminal state restored on every exit path.
8. **Untrusted text stays untrusted.** Model output, tool output, filenames, and MCP output are sanitized before they touch the screen (Section 3).
9. **Plain words.** Short, specific, kind. Say what happened and what to do next.
10. **One source of truth for looks.** Tokens (Section 2). No hardcoded colors, glyphs, or spacing inside components.

---

## 1. Architecture Rules for the UI

### 1.1 Events, not entanglement
The Orchestrator emits a **typed event stream**. The TUI and headless mode are two renderers of the same stream. No agent logic lives in the TUI.

| Event | Carries |
|---|---|
| `turn_start` / `turn_end` | turn id; end reason (done, interrupted, error, max-turns) |
| `text_delta`, `reasoning_delta` | streamed text |
| `tool_call_start` | id, name, args summary, permission tier |
| `tool_permission_request` | full call detail, risk flags, suggested rule scopes |
| `tool_output_delta`, `tool_call_end` | streamed output; status, duration, result summary |
| `question_request` | structured questions (Section 11) |
| `todo_update`, `plan_proposed`, `mode_changed` | state for those panels |
| `compaction_start` / `compaction_end` | tokens before/after |
| `retry`, `error` | attempt, delay, reason; error class, message, hint |
| `usage` | tokens, cost if reported, context % |
| `session_event` | saved, forked, rewound, renamed |

The schema is versioned (`"schema": 1`). Headless `stream-json` output *is* this stream.

### 1.2 Backend hooks this spec needs
Build the UI against interfaces and fixtures. Do not block on these:
- Built-in tools: `todo_write`, `ask_user`, `exit_plan` (present a plan for approval).
- Permission rule store (allow / ask / deny, with scopes) and a read API for the audit log.
- Model catalog metadata where the provider exposes it: context window, pricing, input modalities, reasoning support.
- Token accounting by bucket (system prompt, AGENTS.md, skills index, MCP schemas, messages) for `/context`.
- Session store with branches; pre-edit file snapshots (checkpoints) for rewind.
- File index service, clipboard/image helpers.

### 1.3 Components
- One directory per component in `internal/tui/components/`. Each is `View(state, width, theme) string` plus `Update`. No I/O in `Update`; async work goes through commands.
- Shared packages: `theme/` (tokens), `glyphs/`, `layout/` (spacing), `safe/` (sanitizer), `term/` (capabilities), `copy/` (every user-facing string in one catalog).
- Own the composer model. `bubbles/textarea` is a fine start, but atomic paste tokens and `@` chips need a custom buffer.
- **Component gallery:** `go run ./cmd/tilde-gallery` renders every component in every state × widths {40, 80, 120} × color {truecolor, 256, 16, none} × {unicode, ascii}. Goldens come from the gallery.
- Verify library APIs against the pinned version before use (Bubble Tea's paste and keyboard-protocol APIs differ between major versions). Do not guess. Verify a dependency is maintained before adding it, and say why it's needed.

### 1.4 Rendering model: inline, not alt-screen
- **Inline.** Finished blocks are committed to the terminal's scrollback and never redrawn. Only the **live region** redraws.
- The live region holds: streaming tail, running tool blocks, spinner line, queued messages, todo panel, composer *or* dialog, footer. It must always fit in `rows − 1`. If it would not, commit the oldest live content first.
- **Streaming text:** commit each completed block (paragraph, list, code fence, table) as soon as it closes. Only the unfinished tail stays live. This is what keeps long answers from breaking the renderer.
- **Wrap at commit time** to the current width, with hanging indents. Committed text does not reflow on resize. The transcript view (`Ctrl+O`) re-renders from source at the new width.
- **Alt-screen only for transient full-screen views:** transcript, help, large pickers (sessions, history), `/diff` review. Always return to inline state cleanly.
- **No vertical borders** on prose, code, or diffs, because copying would include them. Use horizontal rules and whitespace gutters. Dialogs may use a single top rule.
- Wrap frames in synchronized-output mode (`CSI ? 2026 h/l`) where supported to prevent tearing.

### 1.5 Performance budgets
| Budget | Target |
|---|---|
| First paint after launch | ≤ 150 ms. Skills, MCP, and file index load asynchronously |
| Keypress → paint | ≤ 16 ms p95 idle, ≤ 50 ms while streaming |
| Render throttle | ≤ 30 fps; halve on SSH (`SSH_CONNECTION`) |
| Streaming markdown | re-render only the tail block, never the whole message |
| Syntax highlighting | cached by (language, content hash), off the UI goroutine |
| UI model memory | last N blocks only; the rest lives in the session store |

---

## 2. Design Tokens

### 2.1 Color tokens
Set colors **only** where listed. Body text uses the terminal's default foreground and background, so light and dark terminals both work.

| Token | Use | Dark | Light | 16-color |
|---|---|---|---|---|
| `fg` | body text | terminal default | terminal default | default |
| `fg.muted` | metadata, tool results, args | `#9aa0a6` | `#5f6368` | bright black |
| `fg.subtle` | hints, chrome, placeholders | `#6b7280` | `#80868b` | default + dim |
| `accent` | brand, focus, selection, prompt | `#2dd4bf` | `#0f766e` | cyan |
| `success` | ok, added | `#4ade80` | `#15803d` | green |
| `warning` | caution, pending, denied | `#fbbf24` | `#b45309` | yellow |
| `danger` | errors, removed, bypass | `#f87171` | `#b91c1c` | red |
| `info` | neutral notices | `#60a5fa` | `#1d4ed8` | blue |
| `mode.plan` | plan mode | `#60a5fa` | `#1d4ed8` | blue |
| `mode.accept` | accept-edits mode | `#c084fc` | `#7e22ce` | magenta |
| `mode.bypass` | bypass mode | = `danger` | = `danger` | red |
| `border` | rules, dialog frames | `#3f4650` | `#c5cad1` | bright black |
| `surface.user` | user-message block bg | `#262a31` | `#eef0f3` | none |
| `surface.code` | code/inline-code bg | `#1c2026` | `#f5f6f8` | none |
| `surface.selected` | picker selection bg | `#2a2f38` | `#e3e6ea` | reverse |
| `diff.add.bg` / `.strong` | added line / changed words | `#12351f` / `#1d5c34` | `#d4f5df` / `#a6e9bf` | none (green fg) |
| `diff.del.bg` / `.strong` | removed line / changed words | `#3d1618` / `#6b2226` | `#fbdcdc` / `#f5b3b3` | none (red fg) |
| `syntax.*` | keyword, string, number, comment, func, type | derived from the tokens above; comments use `fg.subtle` | same | ANSI 1–6 only |

**Rules**
- Background fills are allowed only for: user block, code block, diffs, selected row. Nothing else.
- Detect capability: `COLORTERM`, `TERM`, `NO_COLOR`, `CLICOLOR`, `FORCE_COLOR`. Apple Terminal has no truecolor, so it must get the 256 mapping.
- Detect dark/light with an OSC 11 query (100 ms timeout), then `COLORFGBG`, then default to dark.
- **Never use color as the only signal.** Every colored state also has a glyph or word.
- All text tokens must pass ≥ 4.5:1 contrast against `#1e1e1e` (dark) and `#ffffff` (light). `fg.subtle` may drop to ≥ 3:1 but is for non-essential chrome only. Enforce in a CI test.
- Syntax highlighting themes derive from these tokens. Do not ship a stock highlighter palette that ignores the theme.

### 2.2 Spacing and layout tokens
| Token | Value |
|---|---|
| `gutter` | 2 cols (marker + space) |
| `indent` | 2 cols per level; flatten after 3 levels |
| `gap.block` | 1 blank line between top-level blocks |
| `gap.tight` | 0 between a tool header and its results |
| `content.max` | 100 cols for prose. Code, diffs, tables may use full width |
| `composer.rows` | min 1, max `min(12, 40% of rows)`, then scrolls inside |
| `list.rows.max` | 8 (palette), 10 (pickers) |
| Breakpoints | narrow < 60, normal 60–119, wide ≥ 120 |
| Floor | below 40 cols or 8 rows: compact layout and a one-line notice. Never crash |

### 2.3 Visual hierarchy
| Level | What | Style |
|---|---|---|
| 1 | User message | `surface.user` block, `❯` in `fg.muted` |
| 2 | Assistant prose | default fg, `●` at the start of each message, hanging indent |
| 3 | Headings in prose | bold (h1 also `accent`). No `#` characters shown |
| 4 | Tool header | status-colored `●`, bold name, args in default fg |
| 5 | Tool result / detail | `fg.muted`, under `⎿`, indented |
| 6 | Chrome | `fg.subtle`: hints, footer, placeholders, tips |
| 7 | Alerts | `warning` / `danger` + glyph. Never full-width color banners |

**Emphasis budget:** one accent per line. Bold only for names and labels. Underline only for links. Never italic for essential information.

### 2.4 Glyphs
Never use emoji: their width and rendering are inconsistent. Verify every glyph's width in the target terminals. Every glyph has an ASCII fallback, used when the locale is not UTF-8, `TERM=linux`, legacy Windows console, or `TILDE_ASCII=1`.

| Role | Unicode | ASCII |
|---|---|---|
| Composer prompt | `❯` | `>` |
| Shell-mode prompt | `!` | `!` |
| Message / tool bullet | `●` | `*` |
| Result connector | `⎿` | `\` |
| Success / error / warning / info | `✓ ✗ ⚠ ℹ` | `OK x ! i` |
| Pending / running / skipped | `○ ● –` | `o * -` |
| Denied / interrupted | `⊘` | `-` |
| Plan mode / accept-edits mode | `⏸` / `⏵⏵` | `\|\|` / `>>` |
| Queued | `⏵` | `>` |
| Todo: pending / active / done | `☐ ◐ ☑` | `[ ] [~] [x]` |
| Expand / collapse | `▸ ▾` | `> v` |
| Tree | `├─ └─ │` | `+- +- \|` |
| Selection / current / favorite | `❯ ✓ ★` | `> * *` |
| Tokens in / out | `↑ ↓` | `^ v` |
| Ellipsis / wrap continuation | `… ↪` | `... >` |
| Spinner | braille dots `⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏`, 80 ms/frame | `\| / - \` |

### 2.5 Themes
Built-ins: `auto` (default), `dark`, `light`, `ansi` (16-color, inherits the terminal palette), `mono` (no color), `high-contrast`. Custom themes in `~/.tilde/themes/<name>.json` override any token. `/theme` opens a picker with live preview. Config key: `ui.theme`.

---

## 3. Terminal Safety and Hygiene

### 3.1 Sanitize all untrusted text
Untrusted means: model output, tool results, file contents in previews, filenames and paths, git output, MCP output, provider error bodies, pasted text, environment values.

- **Parse, don't regex.** Drop or neutralize all escape sequences (CSI, OSC, DCS, APC, PM, SOS), C0 controls except `\n` and `\t`, DEL, and C1 controls. Convert `\r` so it can never overwrite a line.
- **Show what was removed where it matters.** In approval dialogs, paths, commands, and diffs, render ESC as `^[` and invisible or bidi characters (U+200B–200F, U+202A–202E, U+2060, U+2066–2069, U+FEFF) as `‹U+202E›`. In prose, strip them silently.
- Use grapheme clusters for width (`uniseg`), not runes. Cap a single rendered line (~10k chars) and total rendered tool output (~1 MB) with a truncation notice. The full output goes to the session store, bounded.
- **Enforce with types.** `safe.Text(s, ctx)` returns a `Safe` string type (`ctx` ∈ Prose, Code, Command, Path). Components accept only `Safe`. The compiler then guarantees nothing unsanitized reaches the screen.

### 3.2 Paste safety
- Bracketed paste is on. A paste **never** submits, triggers a slash command, switches to shell mode, or opens a palette, even if it starts with `/` or `!` or contains newlines.
- Strip escape sequences from pasted text; normalize CRLF; expand tabs.
- NUL bytes or binary content: reject with "That looks like binary data. Attach it with `@file` instead."
- Over ~200 KB: offer to save it under `.tilde/pastes/` and reference it instead.
- **Secret guard:** scan pastes and `@` files for high-confidence secrets only (PEM private key headers, well-known provider key prefixes). Confirm once before sending. Configurable, default on.

### 3.3 Approval dialogs must tell the truth
- Show the **literal** command or path, fully wrapped, with control and invisible characters made visible. Never elide the middle silently. If it must be truncated, say "N more lines hidden (`v` to view all)".
- The model's own description ("Type-check the project") is shown *below* the literal command, muted and labeled as tilde's description. It is a claim, not a fact.
- Resolve symlinks and show the real target if it leaves the project directory.

### 3.4 Links and clipboard
- OSC 8 hyperlinks only for `http(s)://` and `file://` in sanitized text, and only where supported. If the visible text differs from the URL, also show the URL.
- **OSC 52 clipboard writes only on explicit user action** (`/copy`). Never from model or tool output. Never emit OSC 52 read queries.
- Do not auto-fetch remote images referenced in model output. Show `[image: alt]`.

### 3.5 Start and exit hygiene
- **On start**, enable: bracketed paste (`\e[?2004h`), focus reporting (`\e[?1004h`, for animation pause and notifications), keyboard-protocol enhancement if supported. Mouse capture stays **off**.
- **On every exit path** (normal, Ctrl+C, SIGTERM, SIGHUP, panic, fatal error) restore, in this order: disable bracketed paste (`\e[?2004l`), focus reporting off, pop keyboard flags, mouse off, show cursor, leave alt-screen, reset SGR, restore window title, print a trailing newline so the shell prompt is not glued to output.
- **Panic:** recover at the top level, restore the terminal *first*, save the session, then print a short message pointing to a crash log. Never dump a stack trace to the screen.
- `tilde --reset-terminal` emits all the resets, for the case where the process was killed with SIGKILL.
- Handle SIGWINCH (relayout), SIGTSTP (restore terminal, then suspend), SIGCONT (re-init and redraw).

---

## 4. Screen Anatomy

```
 ~ tilde v0.3 · openai/gpt-4.1 · ~/code/demo             header: printed once, scrolls away

 ❯ add rate limiting to the login route                  user block (surface.user)

 ● I'll read the route and its tests first.              assistant text

 ● Read(src/routes/auth.ts)                              tool call, status-colored bullet
   ⎿ 84 lines
 ● Update(src/routes/auth.ts)
   ⎿ Added 1 line, removed 1 line
      41 -  app.post('/login', handler)
      41 +  app.post('/login', limiter, handler)

 ⠹ Editing… (12s · ↓ 1.8k tokens · esc to interrupt)     live: spinner line

 ────────────────────────────────────────────────────    composer rule, mode-colored
 ❯ █                                                    composer
 ────────────────────────────────────────────────────
   ⏵⏵ accept edits on (shift+tab to cycle)   gpt-4.1 · ctx 23%    footer
```

Regions, top to bottom: **scrollback** (committed) → **live region** (streaming tail, running tools, spinner, queue, todos) → **composer or dialog** (never both; the draft is preserved while a dialog is open) → **footer** (1 line; left = mode and hints, right = model, effort, context %, cost).

Footer segments drop by priority when narrow: cost → effort → model → context % → mode label (never dropped).

---

## 5. Composer

### 5.1 Layout and editing
- Rule above and below, colored by mode (`border` in default mode). Prompt glyph in col 0, text at col 2, continuation lines hang-indented by 2.
- Grows from 1 line to `composer.rows` max, then scrolls internally. Cursor is always visible.
- **Newline:** `Ctrl+J` (works everywhere), `Shift+Enter` (where the terminal reports it, via the keyboard protocol), or `\` then Enter (fallback). `Alt+Enter` also inserts a newline when idle.
- **Submit:** Enter, when non-empty. Empty Enter does nothing (no blank turns). Trim outer whitespace on submit only.
- Editing keys: `Ctrl+A/E` line start/end, `Ctrl+K/U` kill to end/start, `Ctrl+W` or `Alt+Backspace` delete word, `Alt+B/F` or `Ctrl+←/→` word move, `Ctrl+_` undo. Home/End/Delete work as expected.
- `Ctrl+G` opens the draft in `$EDITOR` and returns the result.
- **Draft is never lost.** It survives dialogs, errors, resize, compaction, and interrupts.
- `Esc Esc` on a non-empty draft clears it (undoable with `Ctrl+_`). On an empty composer it opens rewind (Section 15).

### 5.2 History
- `↑`/`↓` navigate history **only** when the cursor is on the first/last line, so multi-line editing is never hijacked.
- `Ctrl+R` opens fuzzy history search (picker).
- Persisted per project plus a global list. Consecutive duplicates collapse. Paste tokens are stored as tokens (content in a capped side file). Secret-input lines are never stored. Headless never writes history.

### 5.3 Placeholder
Dim, vanishes on the first keystroke, returns when empty, never enters history.

| State | Placeholder |
|---|---|
| First run | `Ask tilde to build, fix, or explain something…` |
| Idle (returning) | rotates: `/ for commands · @ for files · ! for shell · ? for shortcuts` |
| Agent working | `Enter to steer · Tab to queue` |
| Plan mode | `Describe what to plan. tilde won't change anything yet` |
| Shell mode | `Run a shell command` |

### 5.4 Large paste (bracketed paste mode)
- Collapse when the paste is **≥ 4 lines or ≥ ~1000 chars** (both configurable: `paste.collapse_min_lines`, `paste.collapse_min_chars`).
- The composer shows an **atomic token**: `[Pasted text #1 · 42 lines]`. The cursor skips over it; Backspace deletes the whole token; it cannot be edited in place.
- Full content lives in a side table keyed by id and is inserted inline at that position on submit. `Alt+E` with the cursor after a token expands it into raw editable text (irreversible).
- Below the thresholds, paste inline as normal text.
- All safety rules from Section 3.2 apply.

### 5.5 Image paste and file drag-and-drop
- **Image paste:** `Ctrl+V` reads an image from the clipboard when one is present, otherwise falls through to text paste. Some terminals intercept `Ctrl+V` (Windows Terminal, GNOME Terminal), so also provide `/paste-image`, drag-and-drop, and `@image.png`. Clipboard access is best effort per OS; if no tool is available, say exactly what to install.
- Shown as a token: `[Image #1 · 1280×720 · 84 KB]`, removable as a unit.
- Before sending: downscale and compress to provider limits, **strip EXIF and GPS metadata**, and check the model accepts images. If it does not, offer to switch to a vision-capable model.
- **Drag-and-drop:** terminals paste a file path as text. Detect a paste that is one or more existing paths, handling quotes, shell-escaped spaces, `file://` URLs, and Windows paths. Images become image tokens; other files become `@path` references. Directories become `@dir/`.

### 5.6 `!` shell escape
- `!` as the **first character** of an empty composer (typed, never pasted) switches to shell mode: prompt becomes `!`, rule changes color, placeholder changes. Esc or Backspace on empty exits.
- Enter runs the command through the normal Tool Execution Layer. It is user-initiated, so no approval dialog, but it **is** written to the audit log as user-initiated.
- Output appears as a tool block and is added to context (capped, marked as user-run) so tilde can see it.
- Interactive programs (vim, less, ssh) are not supported in v1. Detect and say so.

---

## 6. Commands, Pickers, and `@` References

### 6.1 Slash palette
- Opens when `/` is the first character of the line (typed, never pasted). Renders **below** the composer in place of the footer, max 8 rows.
- Row: `/name` (accent when selected), args hint (subtle), description (muted, truncated with `…`). Non-built-in entries carry a badge: `(skill)`, `(user)`, `(project)`, `(mcp:server)`.
- Ranking: exact prefix > word prefix > fuzzy on name > description match. Ties: recency, then frequency. Matched characters are highlighted.
- Keys: `↑/↓` or `Ctrl+P/N` move (wrap); `Tab` completes the name and keeps the args hint; `Enter` runs (if required args are missing, it completes and shows the hint instead); `Esc` closes and keeps the typed text.
- **Unknown command:** do not send it to the model. Show `Unknown command /xyz. Did you mean /xyz2?`. To send literally, start with a space or `\/`. Text after `/` that contains another `/` (a path) is not treated as a command.
- **Core commands:** `/help /clear /new /resume /rename /model /effort /mode /compact /context /cost /copy /diff /rewind /tree /export /handoff /skills /agents /mcp /permissions /theme /config /login /logout /doctor /terminal-setup`. Skills, prompt templates, and MCP prompts add their own.

### 6.2 Pickers (one shared component)
- **Anatomy:** title (bold), optional subtitle, filter input (`❯` prompt, "Type to filter"), rows, footer hints.
- Selected row: `❯` in `accent`, `surface.selected`, bold name. Right-aligned metadata in `fg.muted`. `▲ n more` / `▼ n more` scroll indicators.
- **States:** loading (spinner + label, cancelable), empty ("No sessions yet"), filtered-empty ("No matches for 'xyz'"), error (message + `r` to retry).
- Height: `min(list.rows.max, available rows − 4)`. Closing returns focus and the draft.
- **Type-ahead protection** (same as dialogs, Section 9): ignore keys for ~300 ms after opening.
- Used for: model, effort, mode, theme, resume, history search, skills, agents, MCP servers, permission rules, `@` files, rewind points.

### 6.3 Model picker
Rows show `provider/model`, context window, price per million tokens, and glyphs for vision and reasoning support, when the provider's model list exposes them. `Ctrl+F` favorites (`★`), recent first, `✓` on current. Switching mid-session prints a divider: `Switched to <model> · context kept`. Warn when the new model's window is smaller than current usage, or lacks tool or vision support the session already uses. `/effort` appears only for models that support reasoning effort.

### 6.4 `@` file references
- Typing `@` opens a file picker. Index in the background at startup: `git ls-files -co --exclude-standard` inside a repo, otherwise a bounded walk that honors ignore files. Never block typing; show results as they arrive.
- Fuzzy match on path, directories shown with a trailing `/`. `Tab` accepts and continues into a directory. Paths with spaces are quoted. Supports `@file:10-40` line ranges.
- Inserted as `@path`, highlighted in `accent`. On submit the file is read and attached (size cap, truncation notice, binary and image handling).
- **Sensitive files** (`.env`, private keys, credentials files): confirm before attaching, since the content goes to a third-party model.
- `@agent-name` invokes a subagent; `@server:resource` references an MCP resource.

---

## 7. Streaming, Reasoning, Spinner, Microcopy

### 7.1 Streaming
- Coalesce tokens to word boundaries and release at ≤ 30 fps. Never re-render or re-wrap the whole message per token.
- Markdown streaming rules are in Section 16.5.
- **Stall handling:** no tokens for 10 s → spinner label becomes `Waiting for the model…` with elapsed time. At 45 s → show the retry hint (Section 14).
- After the first token, the spinner line shows `(12s · ↓ 1.8k tokens · esc to interrupt)`. Elapsed time appears after 2 s.

### 7.2 Reasoning
When the provider returns reasoning, show it collapsed: one dim line `Thinking… (Ctrl+O to expand)`. In the transcript it renders dim and italic-optional. Setting `ui.reasoning`: `hidden | collapsed | full`. Never present reasoning as a final answer.

### 7.3 Spinner and status line
- One line in the live region: spinner glyph (accent), label, `(elapsed · tokens · esc to interrupt)`.
- **Activity beats whimsy.** When tilde knows what it's doing, say it: `Reading`, `Searching`, `Editing`, `Running <cmd>`, `Fetching`, `Connecting to <server>`, `Compacting conversation`, `Waiting for you`.
- Only while the model is thinking may the label come from a small pool of tilde-specific playful verbs (`ui.playful`, default on, off in expert mode). Write tilde's own words. Do not reuse other tools' verbs.
- Animation rules: Section 7.5.

### 7.4 Dynamic microcopy
Voice: calm, direct, brief, sentence case. No exclamation marks, no emoji, no jargon in errors. All strings live in the `copy/` catalog, never inline. Error formula: **what happened → why (if known) → what to do next, with the key or command.**

| Situation | Copy |
|---|---|
| Bad API key | `OpenRouter didn't accept your API key. Run /login to add a new one.` |
| Out of credits | `Your OpenRouter credits ran out. Add credits, or pick a free model with /model.` |
| Rate limited | `Rate limited · retrying in 8s (2/5) · esc to cancel` |
| Provider hiccup | `The provider had a hiccup · retrying in 4s (1/5)` |
| Offline | `Can't reach the provider. Check your connection. Enter to retry.` |
| Interrupted | `Interrupted. Your draft is safe.` |
| Denied | `Denied by you. tilde will try another way.` |
| Empty composer Enter | (nothing) |
| Long wait tip | `Tip: Shift+Tab switches between ask, accept-edits, and plan modes` |
| Image on non-vision model | `<model> can't see images. Switch to a vision model with /model?` |
| Context nearly full | `Context 90% full · compacting now` |
| Session saved on exit | `Session saved. Resume with: tilde -c` |

Tips: at most one per turn, only after a wait > 5 s, never repeated in a session, off with `ui.tips=false`. Contextual tips beat random ones.

**Guidance level** (`ui.guidance`: `guided | standard | expert`) changes **copy density and defaults only, never capabilities or security.** Guided: plain-language explanations in permission dialogs by default, more tips, longer error help. Expert: terse, no tips, no playful verbs.

### 7.5 Animation and transitions
Animate only what carries meaning. Never more than two animated regions. Never animate while the user is typing in the composer.

| Element | Behavior |
|---|---|
| Spinner | 80 ms/frame; optional verb shimmer at most once per 1.5 s |
| Elapsed counter | updates once per second |
| Tool bullet | instant color change on state change (running → success/error). No fades |
| Toast (mode change, copy, saved) | occupies the footer line for 1.5–2 s, then settles to the persistent label. No layout shift |
| Dialog open/close | instant. Scroll position and draft preserved |
| Expand/collapse | instant, viewport anchored to the toggled block |
| Progress (MCP connect n/m, indexing) | `[███░░░]` style bar or `n/m` text |

- **Pause all animation when the terminal loses focus** (focus reporting).
- **Reduced motion** (`TILDE_REDUCE_MOTION=1` or `ui.reduce_motion`): static `…` plus a per-second elapsed counter, no shimmer, toasts stay 3 s.

---

## 8. Tool-Call Timeline

### 8.1 Status bullets
| State | Bullet | Detail line |
|---|---|---|
| Awaiting approval | `○` `warning` | `Waiting for you` |
| Running | `●` `fg.muted` | elapsed time appears after 2 s |
| Success | `●` `success` | result summary |
| Error | `●` `danger` | first line of the error |
| Denied | `⊘` `warning` | `Denied by you` |
| Interrupted | `●` `fg.subtle` | `Interrupted` |

### 8.2 Per-tool rendering
| Tool | Rendering |
|---|---|
| Read / search / list | **Grouped and collapsed** into one summary line: `Read 3 files (Ctrl+O to expand)`, `Searched for 2 patterns`. While running: `Reading 2 files…` |
| Edit | `Update(path)` + `Added N lines, removed M lines` + diff (8.3) |
| Write (new file) | `Write(path)` + `Wrote N lines` + first 10 lines dimmed |
| Shell | `Bash(cmd)` + live tail of the last 5 lines while running; after: first 3 and last 3 lines, `… +N lines (Ctrl+O)`. Exit code shown when non-zero, duration when > 2 s |
| Web fetch | URL + status + size |
| Skill | `Skill(name)` + `Loaded · 312 tokens` |
| MCP | `server › tool(args)` with the server name clearly attributed |
| Subagents | grouped tree (Section 12.3) |
| Todo | panel (Section 10.4) |

Truncation is always explicit: `… +40 lines (Ctrl+O to expand)`. Consecutive read-only calls merge into one group.

### 8.3 Diffs
- Line-number gutter (right-aligned to the widest number), then `+`/`-`/space, then code. Full-width `diff.add.bg` / `diff.del.bg`, with `.strong` on changed words. Keep syntax colors on top of the fills.
- 3 lines of context. Multiple hunks separated by a dim `⋯`. Over 40 lines: truncate with the expand hint. Binary, rename, delete, and mode changes: one-line summary.
- 16-color and mono: no backgrounds; use colored `+`/`-` markers only.
- `/diff` shows everything tilde changed this session, in a full-screen view.

### 8.4 Transcript view (`Ctrl+O`)
Full-screen, everything expanded: full tool output, reasoning, timestamps, durations, permission tier, and approval state (from the audit log). `j/k`, PgUp/PgDn, `/` to search, `q`/Esc/`Ctrl+O` to close. Re-renders from source at the current width.

---

## 9. Permissions

### 9.1 Dialog anatomy
Replaces the composer, separated by a rule. Top to bottom:
1. Title: `Bash command` / `Edit file` / `Fetch URL` / `MCP tool`.
2. **Tier badge in plain words:** `Read-only`, `Changes files`, `Runs a command`, `Uses network or an external service`.
3. The literal command, path, or diff (Section 3.3), then the muted model description.
4. **Risk flags** in `warning`: destructive patterns (`rm -rf`, force push, recursive chmod, `dd`, `mkfs`, pipe-to-shell), `sudo`, writes outside the project, secrets-looking paths, network access.
5. Options:
   - `1. Yes`
   - `2. Yes, and don't ask again for: <scope>`
   - `3. No` (Tab to add instructions for what to do differently)
6. Hints: `Esc to cancel · Tab to amend · Ctrl+E to explain`.

Edit dialogs offer `Yes, allow all edits this session (Shift+Tab)` as option 2 instead.

### 9.2 Interaction rules
- Keys: `↑/↓`, `1–3`, Enter, `y`/`n`. Esc denies.
- **Type-ahead protection.** The dialog ignores all input for ~400 ms after it appears and never accepts a key that arrived before it rendered. A user typing when a prompt pops up must not answer it by accident.
- **Safest default.** Preselect `Yes` for reads and edits. Preselect `No` when any destructive risk flag is set, and require explicit confirmation even in accept-edits mode.
- **`Ctrl+E` explain:** asks the model for a plain-language explanation of the command, shown inline. Marked as generated. This is the main help for non-technical users.
- `Tab` to amend: edit the command before it runs, or add feedback that is returned to the model with the denial.
- **One dialog at a time.** Requests from parallel calls or subagents queue: header shows `1 of 3 pending · from agent: test-writer`.
- No timeouts on dialogs.

### 9.3 "Don't ask again" rules
- Scope choices: this exact command, this command prefix (arg-boundary safe), or this session only. Default storage is the session. Saving to user level is explicit.
- **Never** offer a wildcard for command families that can run arbitrary code through arguments: `find -exec`, `xargs`, `sh -c`, `env`, `sudo`, `git -c`/aliases, `npm run`, `npx`/`uvx`/`pipx` of arbitrary packages, `curl` piped to a shell.
- Compound commands (`&&`, `;`, pipes, `$()`, backticks, redirects) are split and each part is evaluated. A rule for one part never approves the whole.
- **Project-level "always allow" rules load only from a trusted project** (architecture doc, Section 7). A repo cannot ship its own allow rules to an untrusted user.
- `/permissions` lists all rules with their source, and revokes them.

### 9.4 Headless
No dialogs ever. The policy comes from flags. Anything that would need approval fails closed: event `permission_denied`, exit code 3.

---

## 10. Modes, Plan, and Todos

### 10.1 Modes
`Shift+Tab` cycles **ask → accept edits → plan → ask**.

| Mode | Footer label | Rule color | Behavior |
|---|---|---|---|
| Ask (default) | `? for shortcuts` | `border` | prompts per tier |
| Accept edits | `⏵⏵ accept edits on (shift+tab to cycle)` | `mode.accept` | file edits auto-approved; commands still prompt |
| Plan | `⏸ plan mode on (shift+tab to cycle)` | `mode.plan` | read-only tools, plus writing the plan file |
| Bypass | `⚠ bypass permissions` (persistent) | `danger` | everything auto-approved |

- **Bypass is never reachable by `Shift+Tab`.** It requires an explicit flag or config plus a confirmation dialog listing the consequences. It does not persist across sessions by default and shows a persistent `danger` indicator.
- Mapping to the architecture doc: *plan* is the read-only posture plus an approval workflow; *accept edits* is auto-accept-safe-ops; *bypass* is full-auto.

### 10.2 Mode toast
On change, the footer shows a one-line toast for ~1.5 s with what changed: `Plan mode: read-only until you approve a plan`. Then it settles to the persistent label. No focus change, no layout shift.

### 10.3 Plan flow
1. In plan mode tilde explores with read-only tools and presents a plan through `exit_plan`.
2. The plan renders as a markdown block with a top rule in `mode.plan`, and is saved to `.tilde/plans/<slug>.md`.
3. Approval dialog:
   - `1. Yes, and auto-accept edits`
   - `2. Yes, and approve each edit`
   - `3. No, keep planning` (type feedback)
4. `Ctrl+G` edits the plan in `$EDITOR` before deciding. After approval the mode switches automatically, with a toast.

### 10.4 Todos
- Rendered as a panel in the live region when the agent updates its list: `☐` pending, `◐` active, `☑` done (dim, struck through). **Exactly one active item.**
- Compact when long: show the active item, the next two, and `+5 pending`. `Ctrl+T` toggles the full list.
- Session-scoped by default. `/todos export` writes `TODO.md`.

---

## 11. Questions and Structured Input

The `ask_user` tool lets the agent ask 1–4 questions at once. It replaces the composer like a dialog.

- Each question: a short header chip, the question, 2–4 options with one-line descriptions, plus an automatic **"Other (type your own)"**. Single-select or multi-select.
- **Keys:** `↑/↓` move, `Space` toggles (multi-select), Enter confirms and advances, `Tab` or `←/→` switch between questions, `1–9` jump to an option, Esc cancels.
- Multiple questions end with a **review screen** ("Review your answers": Submit / Edit).
- **Cancel** returns "the user declined to answer" to the model. The model must not treat it as consent.
- Optional option preview pane (code snippet or layout sketch) for design choices.
- Other input types: free text, confirm, and **secret input** (masked; never logged, stored in the session, written to history, or sent to the model; used by `/login`).
- Type-ahead protection applies.
- **Headless:** `ask_user` is unavailable. The tool returns an error stating there is no interactive user, so the model proceeds with stated assumptions or stops.

---

## 12. Skills, MCP, and Subagents

### 12.1 Skills
- Model-invoked: `Skill(name)` tool line, then `Loaded · N tokens`. User-invoked: `/skill-name` from the palette.
- `/skills` lists skills with **token cost**, source (user / project), and state. Project skills from an untrusted project show as `locked · trust this project to enable`.
- `/context` shows a bar and a table of what fills the window: system prompt, AGENTS.md, skills index (per skill), MCP tool schemas (per server), messages. This makes the cost of every skill and server visible.

### 12.2 MCP
- `/mcp` manager lists servers with status: `✓ connected`, `○ connecting`, `△ disconnected · Enter to log in`, `✗ failed: <reason>`, `– disabled`. Drill in for tools (each with its permission tier), resources, and prompts. Enable/disable per server and per tool.
- Auth flows for remote servers: show the URL and a paste-the-code fallback for remote/headless machines.
- Tool calls render as `server › tool(args)` with the server clearly attributed, and results labeled as external content.
- Connect asynchronously. The footer shows `MCP 2/3` while connecting. A failing server is reported per server, never as a global error.
- First connection to a project-declared server shows the trust dialog with the literal command line (architecture doc, Section 7).

### 12.3 Subagents
- Running: a grouped block, one line per agent showing its task and its latest action, with a spinner. Finished: `2 Explore agents finished (Ctrl+O to expand)`, then a tree:
  ```
  ● 2 agents finished (ctrl+o to expand)
    ├─ Explore project structure · 21 tool uses · 70.4k tokens
    │  ⎿ Done
    └─ Explore existing palette · 11 tool uses · 64.4k tokens
       ⎿ Done
  ```
- Footer shows `agents: 2 running`. `Ctrl+O` expands to per-agent transcripts. Esc interrupts all; in the expanded view `x` cancels one.
- A subagent's permission requests go to the main dialog queue, labeled with the agent name.
- Only the agent's summary is folded back into the parent context.
- `/agents` lists definitions with token cost.

---

## 13. Input While Working: Steer, Queue, Interrupt

- **Enter = steer.** Delivered after the current tool call finishes, then folded into context. **Tab or Alt+Enter = queue** a follow-up, sent when the turn ends. Tab is the reliable one, because Option/Alt often needs terminal configuration (macOS).
- Queued messages show above the composer: `⏵ queued (2): "run the tests too"`. `↑` on an empty composer pulls the last queued message back for editing.
- **Esc interrupts the turn and never sends anything new.** Queued text returns to the composer for review, so nothing is lost or sent unexpectedly. Steering messages already delivered cannot be recalled.
- Interrupted tool calls are marked `Interrupted`. A shell command gets SIGINT, then SIGKILL after a grace period.

### 13.1 Key behavior by state
| State | Enter | Tab / Alt+Enter | Esc | Ctrl+C |
|---|---|---|---|---|
| Idle, empty composer | nothing | nothing | `Esc Esc` opens rewind | press twice to exit |
| Idle, draft present | submit | (completion / nothing) | `Esc Esc` clears draft | clears draft |
| Thinking / streaming / tool running | steer | queue | interrupt | interrupt |
| Awaiting permission / question | select | (amend) | deny / cancel | interrupt |
| Compacting | queue | queue | cancel | cancel |
| Retrying | queue | queue | cancel retry | cancel retry |

### 13.2 Global keys
| Key | Action |
|---|---|
| `Ctrl+O` | transcript view / expand |
| `Ctrl+R` | history search |
| `Ctrl+V` | paste image if clipboard has one |
| `Ctrl+G` | edit draft or plan in `$EDITOR` |
| `Ctrl+T` | toggle full todo list |
| `Ctrl+L` | redraw screen (not clear) |
| `Ctrl+Z` | suspend |
| `Ctrl+D` | exit when composer is empty |
| `Shift+Tab` | cycle mode |
| `Alt+P` | model picker (also `/model`) |
| `?` | shortcuts overlay, on an empty composer |

Avoid `Ctrl+S`/`Ctrl+Q` (flow control) and chords the terminal cannot distinguish (`Ctrl+H/I/M/[`, `Ctrl+Shift+*`). Alt/Option combos are conveniences only; every essential action also has a command or a non-Alt key. All keys are rebindable in `~/.tilde/keybindings.json`.

---

## 14. Errors and Retry

### 14.1 Error block
`✗ <summary>` in `danger`, one dim detail line, then a hint with the key or command. Never raw JSON or a stack trace unless `--verbose` or `Ctrl+O`. Always redacted for secrets. Each error carries a short id the user can quote in a bug report.

### 14.2 Classes
| Class | Behavior |
|---|---|
| Auth (401/403) | no retry; `/login` hint |
| Credits (402) | no retry; `/model` hint |
| Rate limit (429) | auto-retry with exponential backoff and jitter, honoring `Retry-After`; show countdown; Esc cancels |
| Server / overloaded (5xx) | auto-retry, same policy |
| Network | retry a few times, then stop; footer shows `offline`; Enter or `/retry` resumes |
| Stalled stream | retry after the stall threshold |
| Context too long | compact automatically, then retry once |
| Content or policy filter | show the provider's message |
| Model unavailable / routing failure | suggest `/model` |
| Tool error | red bullet and first line. The model sees it and may recover. Three consecutive failures of the same tool: suggest steering or interrupting |
| Config parse error | file, line, and what was ignored; continue with defaults where safe |
| Internal panic | terminal restored, session saved, short message with crash-log path |

### 14.3 Retry rules
- LLM requests are safe to auto-retry **only** before any tool call in that model turn has executed.
- **Never auto-rerun a tool execution.**
- After a mid-stream failure, keep the partial output visible but dimmed and marked `discarded`. Retry resends from the last committed message.
- `/retry` resends the last user message. `↑` to edit and resend.
- `/doctor` checks connectivity, key validity, `models.json`, terminal capabilities, clipboard tools, and version. It is the first thing to suggest to a non-technical user.

---

## 15. Sessions, History, Handoff, Compaction

### 15.1 Sessions
- Auto-saved (tree-structured, architecture doc 3.7). Name from the first prompt, editable with `/rename`.
- `tilde -c` continues the latest session in this directory. `tilde -r` opens the resume picker. `/resume`, `/new`, `/clear` (starts a new session; the old one is kept).
- **Resume picker:** name, relative time, message count, branch, cwd, model, and a first/last prompt preview. Current project first. Search, delete, filter by directory or all.
- `/tree` navigates branches. `/fork` branches from here. `/export` writes HTML or Markdown, with secrets redacted.
- **Rewind** (`Esc Esc` on an empty composer, or `/rewind`): pick a point; restore the conversation, and optionally file state from pre-edit snapshots (checkpoints). Say clearly which of the two will be restored.

### 15.2 Handoff
I read "handoff" two ways; both are in scope:
- **Session handoff** (`/handoff`): writes a concise, portable summary (goal, current state, decisions, files touched, open tasks, next steps) to `.tilde/handoffs/<timestamp>.md` and copies it on request. `tilde --from-handoff <file>` starts a fresh session from it. It is compaction made explicit and shareable.
- **Human handoff:** when the agent is blocked on something only the user can do (log in, click a link, run a manual step), show a distinct `Needs you` block with exact instructions and `Enter when done`. Do not disguise it as a normal question.

### 15.3 Compaction
- Footer shows a context meter: `ctx 62%`, colored `success` under 60, `warning` 60–80, `danger` above 80, with the auto-compaction threshold marked in `/context`.
- While running: live line `Compacting conversation…` with a spinner. New input is queued.
- After: a divider block, `Conversation compacted · 84k → 9k tokens (Ctrl+O to view summary)`. The summary is inspectable.
- Warn before automatic compaction (`Context 90% full · compacting now`). `/compact [focus instructions]` runs it manually.

---

## 16. Rendering Content

### 16.1 Markdown
| Element | Rendering |
|---|---|
| Heading | h1 bold + `accent`; h2+ bold. No `#` shown |
| Paragraph | wrap at `min(width, 100)`, hanging indent |
| Emphasis | bold → SGR bold; italic → SGR italic (plain if unsupported); strikethrough → SGR 9 |
| Inline code | `surface.code` background (bold in 16-color) |
| Lists | `•` (`-`), nested `◦`; numbers preserved; task lists `☐ ☑`; hanging indents |
| Blockquote | `▎` bar in `fg.subtle` |
| Horizontal rule | full-width `─` in `border` |
| Table | no box; bold header, `─` separator, 2-space padding, fit to width, wrap cells; too narrow → stacked `key: value` rows |
| Links | text, plus URL in `fg.muted` when different; OSC 8 where supported (Section 3.4) |
| Raw HTML | escaped, never interpreted |
| Images | `[image: alt]` placeholder, never fetched |

Parser: `goldmark` (or equivalent) with a custom renderer built on the tokens. `glamour` is acceptable for a first pass only if it meets the streaming and token requirements.

### 16.2 Code blocks
No side borders; `surface.code` background across the content width; 1 col padding; language tag top-right in `fg.subtle`; no line numbers by default. Long lines soft-wrap with the `↪` continuation glyph so nothing is hidden. `diff` fences render as diffs. Unknown language: plain. `/copy` lets the user pick a code block or the last message.

### 16.3 Syntax highlighting
`chroma` (or equivalent), themed from the tokens (Section 2.1). Falls back to ANSI 1–6 in 16-color and to no color in mono.

### 16.4 Math (LaTeX)
- Terminals cannot typeset math. Convert common LaTeX to Unicode: Greek letters, super/subscripts, `∑ ∫ √ ≤ ≥ ≠ ∈ ∞ → ×`, simple fractions as `a/b`.
- If conversion is incomplete, show the **original LaTeX** in `surface.code`, labeled `math`. Never show a half-converted mess.
- **Avoid currency false positives:** the opening `$` needs a non-space character right after it, the closing `$` needs a non-space right before it and must not be followed by a digit (Pandoc's rule).
- Optional, later: render to an image through the terminal's graphics protocol where supported.
- Mermaid and Graphviz render as code blocks.

### 16.5 Streaming markdown
- Commit at block boundaries (Section 1.4).
- An unclosed code fence renders as a code block immediately.
- A table is held until its separator row arrives.
- A partial link (`[text](`) is held until it closes or the block ends.
- **Invariant (test it):** rendering a document in random chunks must end in exactly the same output as rendering it whole.

### 16.6 Images and vision
- Show inline only where the terminal supports a graphics protocol (kitty, iTerm2, sixel; detect, never assume). Otherwise a placeholder: `[Image #1 · screenshot.png · 1280×720 · 84 KB]`.
- Images from tools (for example a browser MCP server) show as placeholders. `/open <n>` opens one externally, user-initiated only.
- Sent images are downscaled and stripped of metadata (Section 5.5). Say which model will see them.

---

## 17. Help, Onboarding, Notifications, Title

- **`?`** on an empty composer opens a shortcuts overlay, grouped: editing, navigation, modes, commands. `/help` has tabs: general, commands, shortcuts.
- **First run**, at most three steps: theme (auto-detected, confirm), sign in (`/login`), trust this directory. Then a short welcome header with version, model, cwd, and mode. Skippable, never repeated.
- **Notifications** (opt-in, `ui.notify`): when a long turn finishes or approval is needed **and the terminal is unfocused**, ring the bell and/or send a desktop notification (OSC 9/777, sanitized). Nothing is sent while focused.
- **Window title:** `tilde · <session name> · ● working` / `✓ done` / `? needs you`. Restore the previous title on exit.
- **`/terminal-setup`** helps configure terminals so `Shift+Enter` and Alt/Option keys work. It explains before changing anything.

---

## 18. Degradation, Responsiveness, Accessibility

### 18.1 Capability detection
| Capability | Detect | Fallback |
|---|---|---|
| Color depth | `COLORTERM`, `TERM`, `NO_COLOR` | 256 → 16 → none |
| Dark/light | OSC 11 (100 ms), `COLORFGBG` | dark |
| Unicode | locale UTF-8, `TERM` | ASCII glyph set |
| Keyboard protocol | query | `Ctrl+J` and `\`+Enter for newline |
| OSC 8 links, graphics | env heuristics or query | plain URL / placeholder |
| Focus events | assume on TTYs, verify | no pause/notify |
| Synchronized output | query | plain frames |
| tmux / screen | `$TMUX`, `$STY` | adapt passthrough for OSC 52/8/graphics |
| Legacy Windows console | detect | ASCII, no OSC |
| SSH | `SSH_CONNECTION` | half frame rate |
| Not a TTY | `isatty` | plain mode (Section 19) |

### 18.2 Responsiveness
- Layout responds to SIGWINCH within one frame. Nothing may exceed the terminal width except intentionally unwrapped code with continuation markers.
- Narrow (< 60): shorter labels, footer drops segments by priority (Section 4), no right-aligned metadata. Very short (< 12 rows): hide footer hints.
- **Perceived speed:** spinner within 100 ms of Enter; composer usable immediately at launch; slow work (MCP connect, indexing, model list) never blocks input and reports progress in the footer.

### 18.3 Accessibility
- Color is never the only signal. Respect `NO_COLOR`. Contrast rules in Section 2.1.
- Keyboard only. No mouse capture by default. `--mouse` is opt-in for scrolling in transcript and pickers.
- `--plain` gives a linear, no-cursor-movement, no-animation mode (also used automatically for `TERM=dumb` and non-TTY). This is the mode screen readers should use.
- Reduced motion (Section 7.5). No flashing faster than the spinner.
- Copy is short, plain, and centralized in `copy/`, which also makes translation possible later.

---

## 19. Headless and Plain Mode

- `tilde -p "prompt"` prints only the final answer to **stdout**. Progress and logs go to **stderr**. Piped input is supported: `cat log.txt | tilde -p "summarize"`.
- `--output-format text | json | stream-json`. `stream-json` is the event stream from Section 1.1, one JSON object per line, versioned.
- **Exit codes:** 0 ok, 1 error, 2 usage or config error, 3 permission denied (needs approval), 4 interrupted or timed out, 130 SIGINT.
- No prompts ever; fail closed. Policy flags: `--permission-mode`, `--allowed-tools`, `--max-turns`, `--timeout`, `--max-budget`. Untrusted project by default (`--trust` opts in). `ask_user` is unavailable.
- Non-TTY stdout automatically disables ANSI, spinners, and OSC. Secrets are redacted from all output.

---

## 20. Testing and Acceptance

- **Goldens** from the gallery: plain-text goldens at widths 40/80/120, plus ANSI snapshots to check token usage, across truecolor/256/16/none × unicode/ascii.
- **Invariants and fuzzing:**
  - Streaming equivalence (16.5).
  - Sanitizer fuzz: no ESC byte, no control character, and no bidi override ever survives to the screen.
  - Paste handler fuzz, including ESC, NUL, huge input, CRLF.
  - No rendered line exceeds the terminal width.
  - Width tests with wide characters, emoji, combining marks, RTL text.
- **Scripted sessions** (`teatest` or equivalent: keys in, frames out): paste collapse, multi-line editing, history, palette, dialog type-ahead protection, interrupt restores the queue, terminal restored after panic.
- **Demo tapes** with `vhs` for the README and smoke checks.
- **Performance:** a harness with a fake provider streaming ~200 tokens/s verifies the budgets in Section 1.5.
- **Manual matrix:** Terminal.app, iTerm2, kitty, WezTerm, Ghostty, Alacritty, GNOME Terminal, Windows Terminal, VS Code terminal, tmux, over SSH, `TERM=dumb`, at 40×12 and 200×60.

**The TUI is done when:**
- [ ] Every component appears in the gallery in every state, with goldens.
- [ ] The terminal is restored on every exit path, including panic and SIGHUP.
- [ ] No untrusted string can reach the screen except through `safe`.
- [ ] Paste never submits or triggers a mode; large paste collapses to a token.
- [ ] A permission dialog cannot be answered by keys typed before it appeared.
- [ ] Esc interrupts and returns queued text to the composer.
- [ ] Streaming a long answer never breaks the live region or flickers.
- [ ] Everything works in 16-color, ASCII, and `--plain`.
- [ ] All user-facing strings are in `copy/`; all colors, glyphs, and spacing come from tokens.

---

## 21. Phase Mapping

Follows the Build Order in the architecture doc (Section 8). Later-phase components may be built early **in the gallery with fixtures**, but not wired to backends.

| Phase | UI scope |
|---|---|
| 0 / 1 | Capability detection, terminal hygiene, tokens, glyphs, themes, `safe`, event stream, gallery and goldens. Inline renderer. Composer basics (multiline, history, placeholder, paste collapse, paste safety, draft preservation). Basic `/` palette (built-ins: `/help /clear /login /logout /exit`). Streaming markdown and code. Spinner, status line, microcopy catalog. Tool timeline and diffs. Permission dialog (3 options, type-ahead protection). Provider errors and retry. Steer, queue, interrupt. Footer. Plain and non-TTY mode |
| 2 | Modes UI (`Shift+Tab`, toast, plan flow, bypass rules), full palette and pickers (model, effort, mode, theme), `@` references, `!` shell, `ask_user` dialogs, todos, `/permissions` |
| 3 | Skills UI, `/skills`, `/context` |
| 4 | MCP manager, MCP trust dialog, auth flows |
| 5 | Subagent groups, `/agents`, permission queue labeling |
| 6 | Sessions, resume picker, `/tree`, rewind and checkpoints, handoff, compaction UI, `/export`, `/copy`, `/diff`, `/doctor`, notifications, window title, image and vision display, LaTeX conversion, `/terminal-setup`, headless polish |

---

## 22. Out of Scope for v1

Mouse-driven UI; side-by-side diffs; inline graphics for math; voice input; theme marketplace; scriptable status line; split panes or multiple sessions in one window (use tmux); background shell jobs; web or remote UI; full i18n (but keep all copy centralized); IME preedit rendering.
