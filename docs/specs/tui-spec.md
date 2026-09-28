# tilde TUI Spec

> The one spec for everything the user sees. Replaces tui-ux-spec.md
> and tui-design.md. What's here reflects **what is built today**
> (Phases 0–29); the short "Not yet built" list at the end is the
> honest remainder. Codex's approaches live in
> [docs/reference/](../reference/codex-adoption.md), not here.

**Rules of the spec:** match what is real; when code and spec
disagree, fix one of them in the same change; never describe a
feature that does not exist.

---

## 1. Principles

1. One accent, calm chrome, content leads.
2. Typing always works; state is always visible.
3. Dangerous things are visible before they happen.
4. Keyboard only; the terminal is respected (inline, no alt-screen,
   native scrollback, state restored on exit).
5. Degrade, don't break: truecolor → 256 → 16 → none; unicode → ASCII.
6. Colors and glyphs come from one place (`internal/tui/style.go`).
7. tilde's identity is the `~`. No emoji. Every glyph has a fixed
   meaning.

---

## 2. Tokens

**Colors** (dark / light, from `style.go`; body text uses the
terminal's default foreground):

| Token | Dark | Light | Used for |
|---|---|---|---|
| accent | `#2dd4bf` | `#0f766e` | brand, selection, prompt, spinner |
| muted | `#9aa0a6` | `#5f6368` | metadata, tool results |
| subtle | `#6b7280` | `#80868b` | hints, placeholder, chrome |
| success | `#4ade80` | `#15803d` | ok, added |
| warning | `#fbbf24` | `#b45309` | caution, shell mode, dialog borders |
| danger | `#f87171` | `#b91c1c` | errors, removed |
| info | `#60a5fa` | `#1d4ed8` | notices, links |
| border | `#3f4650` | `#c5cad1` | panel rules |
| surface.user | `#262a31` | `#eef0f3` | user-message block |
| surface.code | `#1c2026` | `#f5f6f8` | code blocks |

Dark/light adapts at startup (OSC 11 where available, else
`TILDE_THEME=light|dark`). Never color as the only signal — a glyph
or word rides along.

**Glyphs** (the whole vocabulary):

| Glyph | Meaning |
|---|---|
| `~` | the brand: header, composer prompt |
| `○ ⏸ › ⏵⏵` | modes: read-only, plan, ask, full-auto |
| `❯` | user message, picker selection |
| `!` | composer shell-mode prompt |
| `●` | tool action |
| `⎿` | result under its action |
| `✓` `✗` `⚠` | success, error, warning |
| `☐ ◐ ☑` | todo: pending, active, done |
| `⏵` | queued follow-up |
| `△` | reasoning block |
| `+` `−` | diff added / removed |
| `…` | truncation, always with a count |

---

## 3. The Screen

One frame, top to bottom: **scrollback** (committed turns) →
**live region** (the current turn's entries, working line, streaming
tail) → **composer or dialog** → **footer**. Finished turns commit
to native scrollback at the next turn boundary (`tea.Println`):
what's printed no longer redraws, and only the current turn can
trip the `… N earlier lines` marker — past turns are in the
terminal's own history, scrollable and searchable. Committed text is
frozen: ctrl+r expansion applies to the live region only. Nothing
exceeds the terminal; the dialog/composer tail is never trimmed.

### 3.1 Greeting (once, scrolls away)

```
   ▄▄▄▄▄▄▄      ~ tilde v0.2.1
▄▄█▀▀▀▀▀▀▀█▄     anthropic/claude-sonnet-4.5 · ask
▀▀         ▀███▄  /home/you/project
               ▀█▄▄▄▄▄▄▄█▀
                 ▀▀▀▀▀▀▀
sandbox: landlock v10 — reads anywhere, writes confined to this
directory, /tmp, and dev caches
```

Logo left, identity beside it, startup notes below (sandbox status,
MCP states, warnings).

### 3.2 A turn

```
❯ add rate limiting to the login route                    ← user block (surface.user fill)

● Read(src/routes/auth.ts)                                ← tool action
  ⎿ 84 lines                                              ← result
● Update(src/routes/auth.ts)
  ⎿ updated src/routes/auth.ts +1 −1                      ← write/edit renders as a diff
      + app.post('/login', limiter, handler)              ← syntax-highlighted, ctrl+r expands
△ thought for 2.1s · 214 chars (ctrl+r to expand)          ← reasoning, collapsed

I'll add a token-bucket limiter on the route.               ← assistant prose (markdown)

⠹ Editing… (12s • esc to interrupt • ↓ 1.8k tokens)      ← working line, live
```

### 3.3 Composer + footer

```
──────────────────────────────────────────────────────────  ← rule, border color (amber in shell mode)
~ ask tilde anything…                                        ← the input, brand ~ prompt
──────────────────────────────────────────────────────────  ← rule
› ask (tab to cycle)   ? for shortcuts · / commands
```

Full-width rules frame the input — the findable frame without a
box. The `~` sits in accent; shell mode flips it to `!` and the
rules to amber. The mode line carries each mode's own glyph —
`○` read-only, `⏸` plan, `›` ask, `⏵⏵` full-auto — so the
brand `~` belongs to the composer alone. The footer drops hint segments on narrow
terminals, never the mode.

### 3.4 Permission dialog (ask mode, Action-Allowed tools)

```
╭─ Bash command · Runs a command ───────────────────────────╮
│ npm init -y                                               │
│                                                           │
│ This command requires approval. Do you want to proceed?  │
│                                                           │
│ ❯ 1. Yes                                                  │
│   2. Yes, and don't ask again for: npm init:*             │
│   3. No                                                   │
│                                                           │
│ 1-3 or arrows to choose · enter selects · y/a/n · esc    │
╰───────────────────────────────────────────────────────────╯
```

Plain-words title and tier, the literal command verbatim, **No
preselected**. Option 2 grants a session-scoped prefix rule
(`npm init:*` covers `npm init --yes`, never `npm install`;
metacharacters fail closed). Toast confirms what was granted.

### 3.5 Pickers and palette

```
╭─ browse models — pick a provider ────────────────────────╮
│ ❯ anthropic    enter to browse models                    │
│   groq         no key — /login groq                      │
│   ollama       enter to browse models                    │
╰───────────────────────────────────────────────────────────╯
```

One component: type to filter, arrows move, enter selects, esc
closes, draft preserved. `/models` fetches the provider's real
model list live; `/login` (bare) picks which provider's key to
store.

### 3.6 Other dialogs

- **Trust prompt** — one line in a warning-bordered box: what
  would run, `y` trust / `n` decline.
- **Plan approval** — `y` implement, `a` implement with
  auto-accept, `n` keep planning; approval switches the session's
  mode live.
- **Login** — masked input in a box; Enter saves, Esc cancels and
  discards.

### 3.7 Todos (live panel at the transcript tail)

```
● tasks (2/5 done)
  ☑ Explore current project structure
  ◐ Draft the provider interface change
  ☐ Identify affected call sites
```

---

## 4. Keys

| Key | Action |
|---|---|
| Enter | send — mid-turn: steer at the next round boundary |
| ↑ / ↓ | recall a previous prompt (from the first/last line; inside a multiline draft the arrows move the cursor) |
| Ctrl+J | newline |
| Alt+Enter | queue a follow-up |
| Ctrl+R | expand / collapse results & thinking (live region only — committed text is frozen) |
| Ctrl+V | attach the clipboard image (png/jpeg/gif/webp, sniffed) |
| Ctrl+E | edit the draft in `$VISUAL`/`$EDITOR` |
| Tab / Shift+Tab | cycle permission mode |
| Esc | stop the turn, or close whatever is open |
| Ctrl+C | stop and quit |
| `?` | help overlay (empty composer) |
| `/` | command palette |
| `@` | file picker (live filter, `.gitignore`-aware) |
| `!` | shell mode — Enter runs it directly, no model round trip |

Slash commands: `/help /models /model /mode /skills /mcp
/sessions /login /logout /exit` (plus anything a project defines).

---

## 5. Behavior rules that matter

**Breathing space.** One blank line before every top-level block: a
user turn, an answer, a tool group, a reasoning receipt, a
compaction note, the todo panel. Zero inside a block — an action
and its `⎿` result stay tight, and consecutive tool calls in one
group stay tight. `collapseBlanks` guarantees the rhythm never
doubles, so entries can be appended freely.

- **Paste** collapses ≥ 4 lines / ~1000 chars to a `[paste N]`
  token; content re-expands on submit. A paste never submits.
- **Images** ride as `[Image #N]` tokens; sent as multimodal
  parts to OpenAI-compatible and Anthropic models.
- **Reasoning** streams into a dim `△` tail, then collapses to
  one expandable line. Never stored in history.
- **Prompt history** (↑ recall) persists to `history.jsonl` under
  tilde's home (global, 500 entries, consecutive duplicates
  collapse, 0600). Login keys never enter it. The stored form is
  what recall should put back: `[paste N]` tokens expand into
  their content (their map entry left with the submit), `@path`
  mentions stay raw and re-read fresh at submit; a paste whose
  expansion exceeds ~4 KB keeps the typed token, visibly dead on
  recall, rather than bloating the file.
- **Errors** always read as the provider's message, never a raw
  JSON dump; every error names the next step.
- **Sandbox** (Linux): shell writes confined to the project dir,
  `/tmp`, and dev caches; denials are kernel-enforced.
- **Draft is never lost** — it survives dialogs, errors, resize,
  and interrupts.
- **Headless** (`tilde -p`) imports no TUI package (CI-enforced),
  prompts never, fails closed.

---

## 6. Not yet built (the honest list)

1. **`safe/` sanitizer** — typed stripping of escape sequences
   from untrusted text.
2. **Transcript view** (`Ctrl+O` full-detail pager).
3. **Type-ahead protection** on dialogs (~400 ms input guard).
4. **ASCII glyph fallbacks** and a `--plain` screen-reader mode.
5. **Themes picker**, LaTeX conversion, `/doctor`, `/diff`.

Each lands as its own phase, verified live, logged in PROGRESS.md.
