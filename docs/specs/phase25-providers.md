# Phase 25 — Built-in provider catalog + native Anthropic

## Goal

Providers work out of the box: `default_provider: "mistral"` in
models.json (no providers block) resolves a built-in base URL and
credential rule — openai, anthropic, mistral, google, nvidia, groq,
deepseek, together, cerebras, xai, moonshot, fireworks, qwen,
ollama, openrouter. Anthropic gets a native Messages-API client
(the only catalog entry that is not OpenAI-compatible), so Claude
works directly instead of only through OpenRouter.

## Non-Goals

- Model catalogs per provider (list models, pricing, vision flags) —
  the user names the model; a catalog UI is future work.
- OAuth flows (Phase 23 decision stands: API keys).
- Non-chat Anthropic features (batch, files).

## Approach

**The catalog** (`internal/config/providers.go`): name → base URL,
wire API (`openai` | `anthropic`), and an optional explicit env var
(empty means the Phase 23 derived rule applies). When
`default_provider` names a catalog entry that models.json's
`providers` map does not define, resolution fills it from the
catalog; explicit entries still win, and the OpenRouter default is
unchanged. `ProviderConfig` gains an `api` field so the wire type
flows to the client constructor.

**Anthropic Messages client** (`internal/llm/anthropic.go`):
a second `llm.Provider` implementation.
- POST `{base}/v1/messages` with `x-api-key` and
  `anthropic-version: 2023-06-01`; body: `model`, `max_tokens`
  (required — ChatRequest gains MaxTokens, default 8192),
  `system` as a top-level param, content blocks, `tools` with
  `input_schema`, `stream: true`.
- History mapping: user text → string content; images → `image`
  blocks with base64 sources (the Phase 22 feature carries over);
  assistant tool calls → `tool_use` blocks; tool results → user
  `tool_result` blocks keyed by `tool_use_id`.
- SSE mapping: `text_delta` → TextEvent, `thinking_delta` →
  ReasoningEvent (Phase 19 applies to Claude's extended thinking),
  `input_json_delta` accumulates and emits ToolCallEvent at
  `content_block_stop`, `message_start`/`message_delta` usage →
  UsageEvent, `error` → ErrorEvent. Errors reuse the readable-body
  parser (`{"error":{"message"}}` shape is shared).
- Client selection: `llm.New(api, baseURL, key)` switches on the
  wire type; the three construction sites (main, /model switch,
  /login re-key) go through it.

## Edge cases

- `max_tokens` is required by Anthropic and absent from the OpenAI
  path's needs; ChatRequest.MaxTokens=0 means "provider default"
  (Anthropic uses 8192).
- A tool_use block whose JSON never completes: flushed as-is at
  stop (the OpenAI client has the same policy).
- Anthropic's `ping` events are skipped like OpenRouter's comments.
- Explicit models.json entries keep full control (custom base URL,
  proxies); the catalog only fills unlisted names.

## Test plan

- config: catalog resolution (named built-in, explicit-wins, unknown
  name stays an error), env-var table for the explicit cases
  (GEMINI_API_KEY for google; derived for the rest).
- llm: Anthropic wire format (system param, max_tokens, tool_use
  and tool_result blocks, image sources), text/tool/thinking/usage
  event mapping, error body parsing — each against recorded SSE
  shapes, mirroring the OpenAI suite.
- PTY, live: a scripted Anthropic SSE fixture streams text, a
  tool call, and a final answer through the real TUI.
