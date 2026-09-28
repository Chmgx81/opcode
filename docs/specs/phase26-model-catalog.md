# Phase 26 — Live model catalog, /login provider picker, /models

## Goal

The provider story closes the loop: `/models` browses what each
provider actually offers — fetched live from the provider, never a
hardcoded list — and `/login` without an argument shows every
provider available to configure. `default_provider` in models.json
remains the zero-config path; the pickers are discovery.

## Non-Goals

- Model metadata beyond id/display name (pricing, context window,
  vision flags) — providers expose it inconsistently on the models
  endpoint; add when a provider justifies it.
- Fetching every provider's models in parallel — that needs a key for
  each and fires 15 requests; the two-step picker (provider → models)
  is honest and fast.

## Approach

**The abstraction** (`internal/llm/models.go`):
`FetchModels(ctx, api, baseURL, key) ([]ModelInfo, error)` — one
function, both wire shapes: OpenAI-compatible `GET {base}/models`
(`{"data":[{"id":…}]}`, Bearer auth) and Anthropic
`GET {base}/v1/models` (x-api-key + version header, `display_name`).
Sorted by id; the caller owns caching policy (none: each browse is a
fresh, accurate list).

**`/models`** (new command): step 1 lists every provider — the
built-in catalog plus models.json customs, each row saying whether a
key resolves ("enter to browse live models" vs "no key — /login
<name>"). Selecting a provider spawns an async fetch; on arrival the
picker reopens as that provider's model list (loading is a dim
transcript note, not a frozen UI); selecting a model switches
provider + model through the existing SwitchModel rebuild. `/models
<provider>` skips step 1. A fetch result that lands after the user
moved on is discarded with a dim note, never a surprise picker.

**`/login`** (no argument): the same provider list as its picker;
selecting one starts the masked key input for that provider. `/login
<provider>` unchanged.

**Wiring**: the TUI Options gain `KeyFor func(provider) (key, ok)`
(main resolves through the existing credential chain — auth.json,
explicit env, derived env). switchModel in main falls back to the
catalog for providers not in models.json's providers map, and the
TUI updates its BaseURL/API state for catalog providers too, so a
later /login re-key keeps the right protocol.

## Edge cases

- A provider whose fetch fails (bad key, network): the error is
  shown in the transcript with the provider named; the picker stays
  closed.
- Local providers (ollama) list without a key.
- Unknown provider argument: named error, no silent fallback.

## Test plan

- llm: both wire shapes, sorting, error status with readable message.
- TUI: provider picker contents (catalog + customs, key hints), fetch
  arrival opens the model picker, stale arrival discarded with a
  note, model selection routes to switchModel, /login picker starts
  the masked flow for the chosen provider.
- PTY, live: a scripted /models session against the fixture —
  provider list, fetch, model list, switch.
