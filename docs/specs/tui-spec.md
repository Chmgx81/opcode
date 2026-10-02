# opcode TUI Spec

> The one spec for everything the user sees. Replaces tui-ux-spec.md
> and tui-design.md. What's here reflects **what is built today**;
> the short "Not yet built" list at the end is the honest remainder.
> Codex's approaches live in
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
7. opcode's identity is the `◈` (`*` in the plain posture). No emoji. Every glyph has a fixed
   meaning.

---

## 2. Tokens

**Colors** (dark / light, from `style.go`; body text uses the
terminal's default foreground):

| Token | Dark | Light | Used for |
|---|---|---|---|
| accent | `#a78bfa` | `#7c3aed` | brand, selection band, prompt, spinner |
| on-accent | `#0d1117` | `#ffffff` | text on the selection band |
| muted | `#9aa0a6` | `#5f6368` | metadata, tool results |
| subtle | `#8b93a0` | `#666b70` | hints, placeholder, chrome |
| success | `#4ade80` | `#15803d` | ok, added |
| warning | `#fbbf24` | `#b45309` | caution, shell mode, dialog borders |
| danger | `#f87171` | `#b91c1c` | errors, removed |
| info | `#7dd3fc` | `#1d4ed8` | notices, links |
| border | `#6e7683` | `#7f858d` | panel rules |
| surface.user | `#2a2732` | `#ede9f5` | user-message block |
| surface.code | `#1f1c26` | `#f6f4fa` | code blocks |

Dark/light adapts at startup (OSC 11 where available, else
`OPCODE_THEME=light|dark`). Never color as the only signal — a glyph
or word rides along. The violet `dark`/`light` pair is the Phase 46
identity; `teal` is the Phase 34 default it replaced, and `green`
the original Phase 7 brand stack, both kept as choices.

**Contrast.** Every text token clears WCAG 4.5:1 against both
surfaces it sits on — the terminal floor and the code panel — the
border token clears the 3:1 a non-text boundary needs, and the
on-accent token clears 4.5:1 against the selection band it rides.
`TestThemeContrastIsLegible` is the table, and a palette edit that
drops below any bar fails there rather than in a bug report.

**Glyphs** (the whole vocabulary, every one with an ASCII form for
`--plain`):

| Glyph | Plain | Meaning |
|---|---|---|
| `◈` | `*` | the brand: header, composer prompt, login mask |
| `⏸ › ⏵⏵` | `= > >>` | modes: plan, build, full-auto |
| `❯` | `>` | user message, picker selection, palette caret |
| `!` | `!` | composer shell-mode prompt |
| `●` | `*` | tool action |
| `⎿` | `\-` | result under its action |
| `✓` `✗` `⚠` | `[ok] [x] [!]` | success, error, warning |
| `·` | `-` | neutral fact (doctor rows, line separators) |
| `☐ ◐ ☑` | `[ ] @ [x]` | todo: pending, active, done |
| `⏵` | `>` | queued follow-up |
| `△` | `^` | reasoning block |
| `↑` | `^` | footer: a newer release is available |
| `•` | `*` | one masked character of a hidden secret |
| `⏎` | `\|` | a line break inside a collapsed result |
| `+` `−` | `+` `-` | diff added / removed |
| `…` | (kept) | truncation, always with a count |

`TestEveryGlyphDegrades` walks the whole set by name, so a glyph added
later without an ASCII form fails there. `TestStateMarkersStayDistinct`
holds the task list, the verdict trio, and the mode glyphs apart from
one another — two states collapsing onto one ASCII string is a state a
reader cannot read back. The dialog boxes follow the posture too
(`dialogBorder`): lipgloss composes the frame, so the rounded corners
are the one mark the vocabulary does not own, and the ASCII box is
`+-|`. Arrows and em dashes in chrome are punctuation, and ask
`plainOr` rather than the vocabulary — the sheet spells `up/dn` and
uses `-` where the arrow was.

---

## 3. The Screen

At launch the frame moves to the terminal's top: the visible
screen is cleared and the cursor homed before the program starts,
so opcode always begins at row one regardless of where the shell
prompt left the cursor (scrollback above survives; only the
visible screen is erased). Maximizing the window itself is the
window manager's job — a terminal app cannot do it.

One frame, top to bottom: **scrollback** (committed turns) →
**live region** (the current turn's entries, working line, streaming
tail) → **composer or dialog** → **footer**. Finished turns commit
to native scrollback at the next turn boundary (`tea.Println`):
what's printed no longer redraws, and only the current turn can
trip the `… N earlier lines` marker — past turns are in the
terminal's own history, scrollable and searchable. Committed text is
frozen: ctrl+r expansion applies to the live region only.

**Nothing exceeds the terminal, ever** — no row wider than it, no
frame taller than it. That is a property of the whole frame, not of
each renderer, and it is enforced twice over. Every renderer clips or
wraps its own rows, and `View` clips every row of the composed frame
once more as the last line of defence, because a row past the edge is
what corrupts bubbletea's inline renderer and it corrupts the whole
app rather than one line of it. `TestFrameFitsEveryStateAtEveryWidth`
sweeps every state across every width from 8 to 120 columns and four
heights; `TestFrameFitsHostileTextAtEveryWidth` repeats it with
unsanitized text so a path that forgets to sanitize fails there too.

**Tall terminals and short ones.** The transcript trims from the
front and says how many rows went. The dialog and composer tail does
not trim — a dialog that lost its options is worse than one without a
border — so the floating blocks size themselves against a row budget
`View` publishes before they render (`floatingBlock`). The order the
budget is spent in is the order a reader needs it: the border and the
blank line go first, then the middle of a list, and a block whose
content cannot fit shows nothing at all rather than one clipped row
(the @-mention menu on an eight-row terminal). When a modal and the
composer cannot both have their minimum, **the composer goes**: it is
the block the user cannot act on while a modal owns the keyboard. The
draft is untouched in the model and returns when the dialog closes.
The permission dialog pins its title, the literal command, the three
options, and the key hints through every trim: a dialog the user
cannot answer is the one failure a permission dialog may not have.

### 3.1 Greeting (once, scrolls away)

```
 ▄▄▄▄    ▄▄▄▄▄    ◈ opcode v0.5.0
█▀  ▀█  █▀  ▀█    anthropic/claude-sonnet-4.5 · /home/you/project
█▄  ▄█  █▄▄▄▀
 ▀▄▄▀   █
sandbox: landlock v10 — reads anywhere, writes confined to this
directory, /tmp, and dev caches
```

Logo left, one identity line beside it — this model, in this
directory — and the startup notes below (sandbox status, MCP
states, warnings). The mode is deliberately absent from the
identity line: the greeting is a frozen launch snapshot (committed
scrollback cannot re-render), the mode is a live dial that changes
one keypress in, and its one true home is the footer — always
current. A snapshot showing mutable state is a contradiction a
reader cannot resolve.

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
╭──────────────────────────────────────────────────────────╮
│ ◈ ask opcode anything…                                    │
╰──────────────────────────────────────────────────────────╯
› build (tab to cycle)   ? for shortcuts · / commands
```

The input sits in the same rounded box every dialog draws — one box
language for every surface the user acts on, and the input's own
side edges for the same two rows the old full-width rules spent.
The border is the boundary token at rest; shell mode flips the
prompt to `!` and the border to amber. Below fourteen columns the
box is not drawn: border and padding would leave the input under
its floor, and a clipped border is worse chrome than none. The mode
line carries each mode's own glyph — `⏸` plan, `›` build, `⏵⏵`
full-auto — and its own color: plan in info blue, build in the brand
accent, full-auto in amber — the state every keystroke is scoped by,
colored by what it means. The glyph differs too, so the color is
emphasis, never the only signal. The brand `◈` belongs to the
composer alone.

The footer reflows from a candidate list, fullest to barest, first
one that fits whole. **The mode is never dropped for anything else**:
it is the state every keystroke is scoped by, so its bare form
outranks the update badge alone, and on a terminal narrower than the
bare mode the mode is clipped rather than replaced. (The badge used to
win: "⏵⏵ full-auto" is twelve columns, so a ten-column terminal showed
a truncated badge and no mode at all.) Hints drop first, then the
`(tab to cycle)` reminder, then the effort dial — which the toast that
set it and `/help` both name.

### 3.4 Permission dialog (build mode, Action-Allowed tools)

```
╭─ Bash command · Runs a command ───────────────────────────╮
│ npm init -y                                               │
│                                                           │
│ opcode needs your approval to run this. Do you want to proceed? │
│                                                           │
│ ❯ 1. Yes                                                  │
│   2. Yes, and don't ask again for: npm init:*             │
│   3. No                                                   │
│                                                           │
│ 1-3 or arrows to choose · enter selects · y/a/n · esc    │
╰───────────────────────────────────────────────────────────╯
```

Plain-words title and tier, the literal command verbatim, **No
preselected**. Option 2 grants a session-scoped prefix rule, and the
grant is never wider than what the dialog shows (Phase 37): a plain
program+subcommand covers its longer forms (`npm init:*` covers
`npm init --yes`, never `npm install`); when the second field is a
flag the grant carries the whole command verbatim (`git -C /tmp
push:*` covers exactly that and its longer forms, never `git -C
/etc reset --hard`). Both sides pass a tiny universal flag-synonym
table (`--yes`/`-y`, `--quiet`/`-q`, `--force`/`-f`, `--verbose`/
`-v`, `--recursive`/`-r`) so a grant matches its flags written either
way; pairs that differ across tools deliberately do not merge
(`--all`/`-a` — `grep -a` is `--text`). Toast confirms what was
granted.

**What the dialog shows is what happens.** A grant is only kept when
one can be formed, and a command that cannot be granted says so on the
option rather than promising a scope that would never match:

- **A metacharacter** (`;`, `|`, `&`, `$`, backtick, `<`, `>`, `\`).
  The allowlist matches by token prefix and fails closed on anything it
  cannot tokenize exactly, so a rule cut from `ls ; rm -rf /` could
  never fire. Option 2 reads "this one command only (no rule to
  remember)".
- **A line break.** Bash reads a newline as a command separator, so a
  multi-line "command" is several commands; and the allowlist's
  tokenizer splits on any whitespace, newline included, so a grant for
  `git status` used to match `git status\ncurl evil` — the second
  command riding in under the first one's prefix. Both sides refuse
  one now (`hasLineBreak`), in the rule and in the match.

Either way the call itself is still approved — only the *remembering*
is skipped — and option 2 never falls back to the per-tool grant.
That fall-through would have allowed **every** bash call for the
session while the dialog had promised one narrow prefix, which is the
one thing this dialog may never do.

### 3.5 Pickers and palette

```
╭─ browse models — pick a provider ────────────────────────╮
│ ❯ anthropic     enter to browse models                  │  ← the accent band fills the row
│   groq          no key — /login groq                    │
│   ollama        enter to browse models                  │
╰──────────────────────────────────────────────────────────╯
```

One component: type to filter, arrows move, enter selects, esc
closes, draft preserved. Every list surface — the pickers, the
command palette, the @-mention menu — draws its items through one
row composer (`menuRow`): the selection is an accent band with the
caret still on the row (the band is emphasis, never the only
signal), the label column is a fixed gutter so the details read as
a column, and a width with no room for the second column drops the
detail rather than wrapping the row — a wrapped row is a row the
frame's budget never counted. `/models` fetches the provider's real
model list live; `/login` (bare) picks which provider's key to
store. A filter matching nothing says so *and says how to widen it*,
because "no matches" alone reads as a broken picker.

`/mode` is the same component over the three permission postures:
each row says what runs without asking, the active one is marked,
and Enter goes through the same switch path tab uses — the grant
reset and the toast cannot drift between entry points.

`/model` is the model hub, ordered by what a user can reach: models
pinned in models.json first (the active one marked), then providers
with a resolvable key whose live list is one enter away, then
providers without one, each naming the `/login` that unlocks it —
enter starts exactly that. The browse picker (`/models`, and the
first-run list) holds the same rule: a keyless row starts the
login, never a fetch that can only fail — the row names
`/login <name>` and Enter does it. A key stored by `/login` is
visible to the session immediately (the resolvers' startup
snapshot never sees the write), and the login's success fetches
that provider's live model list itself, so the journey is
key → models → pick with no command to remember in between.
Sending with an active provider that has no resolvable key is
refused before the turn starts, and switching to an unkeyed
provider says so at the switch — both name `/login <provider>` as
the fix.

### 3.6 Other dialogs

- **Trust prompt** — what would run, `y` trust / `n` decline, on
  their own row. Folded into the sentence, the answer keys were the
  first thing the box's re-wrap pushed off the bottom, and a trust
  prompt whose keys are missing is a prompt the user has to guess at.
- **Plan approval** — `y` implement, `a` implement with
  auto-accept, `n` keep planning; approval switches the session's
  mode live.
- **Login** — masked input in a box; Enter saves, Esc cancels and
  discards. The mask is a vocabulary glyph, so `--plain` never paints
  a non-ASCII bullet over the secret it hides.
- **Help sheet** (`?`, or `/help`) — the whole binding list and the
  whole command list. It is longer than a normal terminal, so it
  **scrolls** (↑↓, pgup/pgdn, home/end) with the header, the update
  badge, and the two most important bindings pinned. It does not
  truncate: a silently shortened help sheet is a lie about what opcode
  can do. Any other key closes it.

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
| Ctrl+J / Shift+Enter | newline |
| Alt+Enter | queue a follow-up |
| Ctrl+R | expand / collapse results & thinking (live region only — committed text is frozen) |
| Ctrl+O | transcript pager — the whole conversation, results expanded, ↑↓/pgup/pgdn scroll, ctrl+o or esc closes |
| Ctrl+V | attach the clipboard image (png/jpeg/gif/webp, sniffed) |
| Ctrl+E | edit the draft in `$VISUAL`/`$EDITOR` |
| Tab / Shift+Tab | cycle permission mode forward / back |
| Alt+. / Alt+, | reasoning effort up / down (unset → low → medium → high → unset) |
| Esc | stop the turn, stop a running `!command`, or close exactly one open thing — and with nothing open, do nothing (it never eats a draft) |
| Ctrl+C / Ctrl+D | press twice to exit — the first press arms a short window and interrupts whatever is running (a turn, or a `!command`); `/exit` quits immediately |
| Exit line | on a clean exit opcode prints `◈ opcode — session saved · resume it with /sessions` |
| `?` | help sheet (empty, idle composer) — scrolls with ↑↓ / pgup / pgdn, any other key closes |
| `/` | command palette (type to filter, arrows or ctrl+n/p, enter selects) |
| `@` | file picker (live filter over the project tree, capped at 1000 files / 6 levels deep; `.git`, `node_modules`, `vendor`, `dist` and friends are skipped) |
| `!` | shell mode — Enter runs it directly, no model round trip |

`TestEscClosesExactlyOneThing` walks every layer esc closes — palette,
help, login, picker, @-mention, pager, permission, plan, trust — and
checks both that the one layer closed and that the draft survived.
`TestCommandsHelpAndSwitchAgree` checks the three copies of the
command list (the `commands` var, the help sheet, and the dispatch
switch) against each other, so a command handled but unlisted, or
listed but unhandled, fails there.

Slash commands: `/help /models /model /mode /skills /mcp
/sessions /login /logout /theme /diff /doctor /update /exit /quit`.
An unknown `/command` errors in place with the closest match —
it never becomes a model turn. `/update` does not update in place: it
points at `opcode update`, which runs in a shell, not in the TUI.

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
- **Type-ahead guard**: for 400 ms after a permission, plan, or
  trust dialog opens, keystrokes are swallowed — a fast typist's
  stray "y" must not answer an approval they never read (Codex
  blocks input the same way).
- **Plain posture** (`--plain`, `OPCODE_PLAIN`, or a detected screen
  reader): the glyph vocabulary degrades to ASCII (Section 2's table),
  animations off; no glyph disappears. The dialog boxes follow
  (`dialogBorder`: `+-|`), the composer's box follows (`+-|`), and the
  chrome's arrows and em dashes are spelled (`up/dn`, `-`). The
  spinner is static under plain even with an explicit
  `"animations": true`. Two known residuals: the `…` ellipsis stays as
  punctuation (screen readers read it), and the startup banner keeps
  its block-drawn logo. `TestFrameIsAsciiUnderPlain` asserts the whole
  plain frame over every state with only the ellipsis whitelisted.
  The window title is sanitized (control and bidi characters
  stripped, 240-rune cap) — OSC titles are an untrusted text surface.
- **Prompt history** (↑ recall) persists to `history.jsonl` under
  opcode's home (global, 500 entries, consecutive duplicates
  collapse, 0600, redacted against the session's secrets).
  Login keys never enter it. The stored form is
  what recall should put back: `[paste N]` tokens expand into
  their content (their map entry left with the submit), `@path`
  mentions stay raw and re-read fresh at submit; a paste whose
  expansion exceeds ~4 KB keeps the typed token, visibly dead on
  recall, rather than bloating the file.
- **Untrusted text is sanitized at the display boundary**
  (`internal/safe`, Phase 32): everything the model or a tool
  produced — streamed text, reasoning, tool arguments, results,
  subagent output, plan and approval dialog bodies, error text, a
  `!command`'s output and its wrapped failure, an `@mention`'s file, a
  saved session's preview in the picker, the `!` echo, and headless
  text-mode lines — passes `safe.Text` before it can reach
  the terminal. A typed parser strips CSI/OSC/DCS/SOS/PM/APC and the
  intermediate and two-rune escape forms, plus C0 (except `\n`,
  `\t`), DEL, and C1 runes; scanning is by rune so UTF-8 survives
  intact. Unterminated sequences never leak bytes. What the model
  sees in its own context is untouched — the boundary is the
  display, not the context. JSON headless output is exempt
  (`encoding/json` escapes control characters; downstream tools get
  the honest bytes).

  The boundary is a property of the *renderer*, not of one caller:
  the approval dialog's literal command is sanitized **after** the
  JSON unmarshal, because JSON escapes a control byte as the six ASCII
  characters `␛`, so sanitizing the JSON string finds nothing to
  strip and the byte comes back out of the unmarshal intact.
  `TestEveryUntrustedPathIsInert` drives one hostile payload through
  every one of those paths at once and asserts the rendered frame
  carries no control byte — without `stripANSI`, which would eat the
  very byte the payload arrives on.
- **Errors** always read as the provider's message, never a raw
  JSON dump; every error names the next step. The mapper
  (`errorWithNextStep`) recognizes the shapes that come back most
  often — a rejected key, a forbidden plan, a rate limit, no credit, an
  unreachable host, a bad certificate — and its "the provider does not
  serve that model" arm is scoped to phrases that actually name the
  model as the problem. The bare word "model" also appears in the
  provider's own "could not list models" and in a 404 for some other
  URL, and answering either with "that model is not served" is advice
  about a model the user never asked about. A message nobody
  recognizes passes through alone rather than being guessed at.
- **An `@mention`'s file is read with a bounded reader.** The cap is
  the *reader's*, not a length check afterwards: `os.ReadFile` of
  `@/dev/zero` (or any multi-gigabyte file) allocated the whole thing
  on the render goroutine before anything could stop it. The read is
  `mentionCap`+1 bytes so "was there more?" is a fact rather than a
  guess, and a truncated mention says so. A directory, a pipe, or a
  permission error each get a visible note — silence would send a
  half-read file to the model as though it were whole.
- **Sandbox** (Linux): shell writes confined to the project dir,
  `/tmp`, and dev caches; denials are kernel-enforced.
- **Draft is never lost** — it survives dialogs, errors, resize,
  and interrupts. It is also not *shown* behind a modal on a terminal
  too short to hold both, which is a hiding, not a loss: the value is
  untouched in the model and returns when the dialog closes.
- **The `!` escape is a real command, not a blocking call.** It runs
  as a `tea.Cmd` under a cancellable context, so the render loop keeps
  answering keys while it is out and `esc` (or the first ctrl+c) kills
  the whole process group through it. It is announced on the status
  line while it runs — a command that takes two minutes is not
  something to leave unannounced — and the interrupt note says the
  command was killed, not that it finished. It is recorded in the
  audit log *before* execution, so a hanging or crashing command is
  still on record. Inline it froze the whole TUI for the length of the
  command and had no way for esc to reach it. The exit path kills it
  too: ctrl+c's second press quits, and quitting mid-command used to
  leave the process group running with nobody left to reap it
  (`TestCtrlCExitDoesNotStrandTheCommand`).
- **Headless** (`opcode -p`) imports no TUI package (CI-enforced),
  prompts never, fails closed.
- **`/theme`** (Phase 34): the palette as a user choice — `dark`
  (teal default), `light`, `green` (the original Phase 7 brand
  stack). The shared picker previews live (moving the highlight
  re-skins the session), Esc restores the theme active when it
  opened, Enter persists to config.json's `theme` key (raw-object
  edit, unknown keys preserved). An explicit config theme wins over
  the background probe; empty keeps the probe with
  OPCODE_THEME=light|dark forcing the posture. Applying clears every
  render cache that embeds the old palette (glamour renderers,
  per-entry markdown, the live stream); committed scrollback keeps
  its original colors — the one stated residual.
- **LaTeX math converts to Unicode** (Phase 36, `internal/tui/latex.go`):
  assistant math renders as readable text — α, not `\alpha`. Applied
  at the renderMarkdown choke point, so finished entries, the
  in-flight stream, and plan bodies all convert. `\(...\)`,
  `\[...\]`, and `$$...$$` always convert; single `$...$` converts
  only when the content carries a math signal (`\`, `^`, `_`), so
  currency and shell variables survive byte for byte. Fenced code
  blocks and backtick spans are skipped; unclosed delimiters stay
  raw (a half-arrived stream region renders raw until its close
  lands). Fractions flatten with precedence-preserving parens,
  roots take the radical, super/subscripts map to Unicode when
  every character has a form (else the readable `^(...)` fallback),
  and unknown commands degrade to their bare name instead of
  vanishing. The model's context keeps the raw LaTeX — display
  only.
- **`/doctor`** is one transcript entry, one line per subsystem —
  version, model/provider, api-key presence (never the key),
  config.json and models.json loader verdicts, the live sandbox
  posture, project trust, skills and MCP counts, the audit log, the
  release-freshness check (cached — never a network call), and the
  terminal's color profile. Every verdict glyph carries the next step
  when something is wrong (✓ / ⚠ / ✗ / ·); the check re-reads the same
  loaders the startup path uses and never repairs or writes anything.
- **`/diff`** (Phase 35): the working tree's git changes in the
  transcript, colored with the verdict tokens (additions succeed,
  deletions danger, hunks info, headers chrome). `git diff
  --no-color HEAD` with a plain-diff fallback for repos with no
  commits; `git status --porcelain` lists untracked files, which a
  diff alone hides. Capped at 400 lines with an honest footer.
  User-invoked and read-only like the `!` shell escape — no model
  round trip, no permission prompt; output passes `safe.Text`, and
  git's own C-style quoting makes hostile filenames inert printable
  text. Every outcome is designed: not-a-repo and clean-tree notes,
  a missing git binary named. Diff lines are **clipped** to the
  terminal, not wrapped: a wrapped diff line stops reading as one, and
  a diff is mostly wider than a split pane.
- **The expanded diff views clip too** (ctrl+r). A source line is
  whatever the file holds — a minified bundle, a base64 blob, a long
  literal — and the hunk rows carry a `+`/`-` verdict at column
  zero, so clipping costs only the tail of the code. The plain
  expanded result wraps with its six-space indent counted *outside*
  the wrap budget, because `wrapAll`'s ten-column floor otherwise made
  the row overflow below sixteen columns.
- **The reasoning-effort knob** (Phase 39): reasoning is the most
  expensive dial, and it is now visible and turnable — alt+./alt+,
  cycle low / medium / high / provider-default with a toast naming
  the posture and a `◐ <effort>` segment on the mode line whenever
  one is set. Config seeds it (`reasoning_effort`, validated at
  load); the orchestrator carries it per request like the
  permission mode, so a mid-session cycle lands on the next round.
  OpenAI-compatible servers get `reasoning_effort` (omitted when
  unset); Anthropic gets a `thinking` budget (low 1024, medium
  8192, high 16384). Subagents inherit the parent's live posture
  at spawn time — deliberately no independent knob.
- **Update notice**: at startup opcode compares the running version
  against the cached latest release (refreshed at most once a day
  over HTTPS, silent when offline) and adds one startup note when a
  newer release exists — `Update available: vX → vY. Run
  \`opcode update\` to install it.` Only the latest release gets
  security fixes, so a stale binary is a finding, not trivia. Opt
  out with `"update_checks": false` or `OPCODE_NO_UPDATE_CHECK=1`;
  dev builds and platforms without prebuilt binaries never check. The
  footer's badge is explained in the help sheet, pinned at its top so
  a long sheet cannot scroll the news off the bottom of the screen.

---

## 6. The states every path is designed for

The list is the deliverable: a state not on it is a state nobody
looked at. `TestEmptyStatesNameAWayForward` holds the empty ones, and
`TestFrameFitsEveryStateAtEveryWidth` holds the shape of every one of
them from 8 to 120 columns.

| State | What it does |
|---|---|
| First run, no messages | the welcome names the three steps, the provider picker is already open, and the footer names `?` and `/` |
| No saved sessions | names where they land and what saves one |
| No skills / no MCP servers | names the file each comes from |
| No models configured | `/model` still offers the catalog — keyed providers browse their live lists, keyless ones name `/login` |
| A provider listing nothing | says so and points at `/models` for another provider |
| A filter matching nothing | names what was typed and how to widen it |
| An `@` matching nothing | says the path must be under the working directory |
| A mistyped `/command` | the closest real command, in place, never a model turn |
| A turn error | the provider's message, plus the next step |
| A permission dialog | title, tier in plain words, the literal command, three options, the keys — No preselected, every one pinned through any trim |
| A `!command` running | named on the status line, interruptible with esc |
| A `!command` failing | the output first, the cause on its own line |
| A stale `/models` fetch | noted dimly, never a surprise picker swap |
| A session file that will not parse | counted and reported, not skipped silently |
| Every terminal 8×8 to 200×50 | no row wider than it, no frame taller |
| `--plain` | ASCII, no glyph lost, no state readable only by color |

## 7. Not yet built (the honest list)

Nothing. Every deferred item in this spec has landed as its own
phase, verified live, and logged in [PROGRESS.md](../../PROGRESS.md).
Items deferred *out* of this spec (open designs, not UI gaps) are
named in the "Deferred (named, not hidden)" sections of the Phase
43 and Phase 44 entries there.
