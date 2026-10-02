# Phase 47 — the mode picker and the first-run flow

## Goal

Two surfaces the redesign had not reached: the mode UI and the
first-run journey from "no config" to "first prompt sent".

1. **`/mode` is a UI, not a sentence.** Bare `/mode` prints a dim
   line; tab cycles blind. The mode is the state every keystroke is
   scoped by, so it gets the same picker `/theme` has — three rows,
   each saying what runs without asking, the active one marked —
   and the mode line carries a per-mode color: plan blue (inquiry),
   build violet (the brand's working posture), full-auto amber
   (proceeds without asking). The glyph already differs; the color
   is emphasis, never the only signal.
2. **The first-run flow has a dead end.** `/model` reads models.json
   only, so the user who followed the welcome — picked a provider,
   stored a key via `/login` — runs `/model` and is told nothing is
   configured. The journey ends one step before the finish line.
   `/model` becomes the hub: providers with a stored key first
   (enter browses their live models), models.json choices next
   (marked active), and providers without a key last, each naming
   `/login <provider>` — enter starts exactly that.
3. **Continuity, not luck.** A successful `/login` immediately
   fetches that provider's live model list, so the chain is key →
   models → pick without the user having to know the next command.
4. **The error before the request, not after.** Sending with an
   active provider that has no resolvable key waits today for a
   doomed 401. A pre-flight check refuses the send with the fix
   named; switching to an unkeyed provider warns at the switch.

## Non-Goals

- No new modes; the three-mode model is Phase 30's decision.
- No key storage UX changes: masked input, 0600 auth.json, and the
  stored-key feedback are Phase 23/44 behavior that works.
- First run keeps auto-opening the provider picker — typing
  `/login` first, as the brief described it, would be an extra
  step; the picker on screen beats the instruction to open one.
- The permission/plan/trust dialogs, help sheet, and doctor are
  unchanged — audited again this phase and still at the bar.

## Approach

- `pickerModes` joins the picker kinds. Bare `/mode` and `/mode`
  with an unknown name open it; each row's detail is the README
  safety table's own sentence; the active mode is marked. Enter
  goes through `setMode` — the same switch path tab uses, so the
  grant reset and toast cannot drift.
- `modeStyle(mode)`: plan → info, build → accent, full-auto →
  warning. Applied to the mode segment and its bare form only; the
  effort dial and hints keep their tokens.
- `openModelPicker` becomes the hub: models.json rows (active
  marked) → keyed providers not covered by models.json (`Action:
  "fetch"`) → unkeyed providers (`Action: "login"`, detail
  "no key — /login <name>"). `pickerSelect` already routes both
  actions; the chain unkeyed-row → login → auto-fetch → pick is
  the brief's flow, minus the typing.
- `submitLogin` returns `fetchModelsCmd(provider)` on success. A
  fetch that fails is itself feedback (bad key), and the picker is
  esc-closable, so the offer is never a trap.
- Pre-flight on submit: the active provider needs a key, `KeyFor`
  resolves none → an error entry names `/login <provider>` and the
  turn does not start. `switchModel` to an unkeyed provider adds
  the same warning at the switch.

## Edge Cases

- **Local providers** (ollama, custom localhost endpoints) never
  need a key and are never pre-flighted.
- **KeyFor unwired** (tests, headless embeds): the check is a
  no-op; behavior matches today.
- **Provider set, model empty**: the existing no-model guard runs
  first and opens the picker; the key check sits behind it.
- **`/model <name>` with an argument** keeps resolving against
  models.json; unknown names keep their error.
- **A fetch racing an open picker** is already handled
  (`handleModelsFetched` notes it dimly rather than swapping).

## Test Plan

- `TestModePicker`: bare `/mode` opens the picker, the active mode
  is marked, Enter switches through `setMode` (mode + toast), and
  an unknown `/mode foo` opens the picker instead of only erroring.
- `TestModelPickerIsTheHub`: keyed providers offer fetch, models.json
  models keep their rows and active mark, unkeyed providers offer
  login with the `/login <name>` detail.
- `TestSendPreflightsTheKey`: submitting with an unkeyed active
  provider adds the error and starts no turn (zero requests billed).
- `TestLoginContinuesToModels`: a stored key fetches the provider's
  model list and opens the catalog picker (via the `fetchModels`
  seam).
- `TestSwitchToUnkeyedProviderWarns`: the switch lands and the
  warning names `/login`.
- Full `go test -race -count=1 ./...`, `gofmt -l .`, `go vet`.
