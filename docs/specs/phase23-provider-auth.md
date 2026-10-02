# Phase 23 — Multi-provider authentication

## Goal

Bring opcode's credential story to the reference standard (Pi's
provider-auth doc): a key for ANY provider, resolved through a
documented chain, with `/login <provider>` and `/logout <provider>`
instead of always the active one, and a generic environment-variable
fallback that covers every OpenAI-compatible provider by rule
rather than by a hardcoded table.

## Non-Goals

- OAuth / browser / device flows — they need per-provider client IDs
  and callback servers; opcode's API-key story is complete without
  them. Revisit if a hosted provider ships no key auth.
- Cloud-provider extras (Azure resource names, Bedrock ambient
  credentials, Vertex ADC) — those are provider SDKs, not
  OpenAI-compatible endpoints.
- Loading auth from a project directory — still refused.

## Approach

**The resolution chain** (unchanged order, generalized fallback):

1. `auth.json` entry for the provider — literal, or `!command`
   shelled out to the user's secret manager (already built; the
   command runs once per process and its failure leaves the key
   unresolved, never silently degraded).
2. The provider's `api_key_env` from models.json, when set (the
   explicit override; empty means "no key needed" only for the
   local-server case — which instead falls through to rule 3).
3. The derived variable: `<PROVIDER>_API_KEY` — the provider name
   uppercased, dashes to underscores. `deepseek` →
   `DEEPSEEK_API_KEY`, `openai` → `OPENAI_API_KEY`, `openrouter` →
   `OPENROUTER_API_KEY`. One rule covers Pi's whole table for every
   OpenAI-compatible server, and matches the conventional names.

**`/login <provider>`** accepts a named provider; `/login` alone
keeps meaning the active one. The argument is not validated against
models.json: the TUI stays config-free (everything injected), and a
stored key for a provider not yet in models.json is pre-provisioning,
not an error. `/logout <provider>` mirrors it. The stored key takes
effect live only for the active provider (the rebuilt client uses
the session's base URL); the transcript entry says exactly what was
stored.

**Failure stays honest**: an unknown provider argument is used as
given; an empty key cancels as today; the OK/denial lines always
name the provider they acted on.

## Edge cases

- A provider whose `api_key_env` is deliberately empty (local
  server) — the derived `<NAME>_API_KEY` lookup simply finds nothing;
  no key, no error, exactly as before.
- Provider names with dots (none today) would produce invalid env
  names — dots are stripped in the derivation.
- `/login other-provider` while a turn is in flight: storing is
  fine; the live client is untouched (it stays on the active
  provider until restart or switch).

## Test plan

- Resolver: derived-variable fallback for custom providers, explicit
  `api_key_env` still wins over the derivation, auth.json still wins
  over both, dot-stripping.
- TUI: `/login <provider>` stores under that name and names it in
  the entry; `/logout <provider>` removes it; bare `/login` unchanged.
- Manual: docs updated (README auth section), existing permission
  and redaction tests untouched.
