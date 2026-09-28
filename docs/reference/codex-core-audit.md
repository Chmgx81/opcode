# Codex CLI Rust Core (`codex-rs/core` + `codex-rs/protocol`) — Architecture Audit

> Extracted by a subagent audit of the Codex source tree. Companion
> reports: [codex-tui-audit.md](codex-tui-audit.md),
> [codex-features-audit.md](codex-features-audit.md), and the
> tilde-focused synthesis in [codex-adoption.md](codex-adoption.md).

**Scope:** `codex-rs/core/src` + `codex-rs/protocol/src` (~275k lines incl. tests). The TOML config schema (`ConfigToml`) physically lives in the adjacent `codex-rs/config` crate but is loaded and enforced by `core/src/config`, so it is included and marked as such. This is a static audit: source and doc comments were read; nothing was executed or tested. Test files were only skimmed, so behavior claims come from implementation code and in-code documentation only. Where a mechanism lives in another crate (e.g. `codex-rollout`, `codex-login`, `codex-sandboxing`), the core-side file that wires it is cited and noted.

---

## 1. INVENTORY

### Protocol crate (`protocol/src/`)
- Submission operations (`Op`) — client→agent commands — `protocol/src/protocol.rs`
- Agent events (`EventMsg`, ~100 variants, serde `type`-tagged snake_case) — `protocol/src/protocol.rs`
- Event envelope `Event { id, msg }` correlating to a submission id — `protocol/src/protocol.rs`
- Prompt-injection XML tags (`<user_instructions>`, `<environment_context>`, `<skills_instructions>`, `<plugins_instructions>`, `<tools>`, `<collaboration_mode>`, `<context_window>`, …) — `protocol/src/protocol.rs:118-143`
- Turn-input routing model (`TurnInput`, `TurnInputRequest`, `TurnInputMode`, `TurnInputSubmission`, `NotSubmittedReason`, `TurnStartOptions`) — `protocol/src/turn_input.rs`
- Thread settings overrides (model, effort, summary, collaboration mode, personality, disabled plugins) — `protocol/src/protocol.rs:511-567` (`ThreadSettingsOverrides`)
- Approval request types (`ExecApprovalRequestEvent`, `ApplyPatchApprovalRequestEvent`, elicitation requests, `ReviewDecision`) — `protocol/src/approvals.rs`
- Guardian assessment protocol (risk level, outcome, status, decision source, user authorization) — `protocol/src/approvals.rs:89-260`
- Wire model items (`ResponseItem`: Message, AgentMessage, Reasoning, LocalShellCall, FunctionCall, …) — `protocol/src/models.rs:1012+`
- Persisted turn items (`TurnItem`: UserMessage, FunctionCallOutput, AgentMessage, Plan, Reasoning, CommandExecution, WebSearch, ImageView, McpToolCall, FileChange, ContextCompaction, EnteredReviewMode, …) — `protocol/src/items.rs`
- Permission model v2 (`PermissionProfile`, `FileSystemSandboxPolicy`, network policy, read-deny matchers, Windows globs) — `protocol/src/models.rs`, `protocol/src/permissions.rs`, `protocol/src/permissions/`
- Approval/sandbox policy enums (`AskForApproval`, `GranularApprovalConfig`, `NetworkAccess`, `SandboxPolicy{read_only, workspace_write, danger_full_access}`, `WritableRoot`) — `protocol/src/protocol.rs:986-1250`
- Auth mode taxonomy (`AuthMode`: ApiKey, Chatgpt, ChatgptAuthTokens, Headers, AgentIdentity, PersonalAccessToken, Bedrock*) — `protocol/src/auth.rs`
- Config value types (`WebSearchConfig`, `ServiceTier`, `ReasoningSummary`, `Verbosity`, `SandboxMode`, `ShellEnvironmentPolicy`, `Personality`, `MultiAgentMode`, `ToolExposureSurface`, `CollaborationMode`, `ApprovalsReviewer`, `TrustLevel`) — `protocol/src/config_types.rs`
- Review output schema + plain-text rendering (`ReviewFinding`, `format_review_findings_block`) — `protocol/src/review_format.rs`
- Hook event taxonomy (`HookEventName`: PreToolUse, PermissionRequest, PostToolUse, Pre/PostCompact, SessionStart/End, UserPromptSubmit, SubagentStart/Stop, Stop, Interrupt; handler types Command/McpTool/Prompt/Agent) — `protocol/src/protocol.rs:1579-1612`
- Agent status state machine (`AgentStatus`: PendingInit, Running, Interrupted, Completed, Errored, Shutdown, NotFound) — `protocol/src/protocol.rs:1823`
- Structured error taxonomy (`CodexErrorInfo`: ContextWindowExceeded, SessionBudgetExceeded, UsageLimitExceeded, RateLimitExceeded, FlexUnavailable, ServerOverloaded, …) — `protocol/src/protocol.rs:1854+`
- Realtime voice conversation protocol (audio frames, transcript deltas, SDP, voices list, handoff) — `protocol/src/protocol.rs:259-450`
- Inter-agent communication wire format incl. encrypted payloads — `protocol/src/protocol.rs:806-920`
- Turn/usage accounting (`TokenCountEvent`, `TokenUsageInfo`, `RateLimitSnapshot`) — `protocol/src/protocol.rs:2343+`, `protocol/src/response_usage.rs`
- Model catalog (`ModelInfo`, `ModelPreset`, effort presets, tool modes) — `protocol/src/openai_models.rs`
- Dynamic tools / request-permissions / request-user-input / plan tool schemas — `protocol/src/dynamic_tools.rs`, `request_permissions.rs`, `request_user_input.rs`, `plan_tool.rs`

### Core crate — session & turn lifecycle (`core/src/session/`)
- `Session` runtime struct, config, services — `core/src/session/session.rs`
- Session spawn args / IO channels / fork persistence — `core/src/session/mod.rs:404-505`
- Submission queue + `Op` dispatch loop — `core/src/session/handlers.rs` (`submission_loop`)
- Turn start/steer/reject decision logic — `core/src/session/turn_input.rs`
- Submission metadata (trace, ancestry, residency guard) — `core/src/session/submission.rs`
- Sampling loop (`run_turn`) and streamed-item parsing — `core/src/session/turn.rs`
- Per-request `StepContext` (frozen tools, MCP binding, settings, AGENTS.md) — `core/src/session/step_context.rs`
- Per-turn `TurnContext` and `TurnEnvironment` — `core/src/session/turn_context.rs`
- Step settings resolution (model/effort overrides) — `core/src/session/step_settings.rs`, `step_activation.rs`
- Durable input queue with steered-input mailbox — `core/src/session/input_queue.rs`
- Task runners (regular turn, compact, review, user shell) — `core/src/tasks/{regular,compact,review,user_shell}.rs`, `lifecycle.rs`
- Managed startup with persistence guard — `core/src/session/startup.rs`
- Startup/shell-snapshot/MCP prewarm — `core/src/session/startup_prewarm.rs`, `mcp_prewarm.rs`
- Context-window token status math — `core/src/session/context_window.rs`
- Token-budget resolution and experimental-context gating — `core/src/session/token_budget.rs`
- Rollout budget reminders per window — `core/src/session/rollout_budget.rs`
- World-state assembly (all model-visible context blocks) — `core/src/session/world_state.rs`, `core/src/context/world_state/`
- Review-thread spawning (`/review`) — `core/src/session/review.rs`
- MCP runtime gating per turn — `core/src/session/mcp.rs`, `mcp_runtime.rs`, `mcp_refresh.rs`
- Guardian checkpoint persistence — `core/src/session/guardian_checkpoint.rs`
- Retained context / code-mode message tasks — `core/src/session/retained_context.rs`
- Daemon crash recovery of recorded turn input — `core/src/session/daemon_recovery.rs`
- Current-time reminder injection — `core/src/session/time_reminder.rs`, `core/src/current_time.rs`
- Turn suspension / extension interruption — `core/src/session/turn_suspension.rs`, `extension_interruption.rs`
- Reasoning-effort override recording — `core/src/session/reasoning_effort.rs`
- Thread settings application — `core/src/session/thread_settings.rs`

### Core crate — tools (`core/src/tools/`)
- Tool registry + exposure policy — `core/src/tools/registry.rs`
- Per-step tool plan assembly — `core/src/tools/spec_plan.rs` (`build_tool_router`, `add_core_tool_sources`)
- Router (finalized plan: advertised specs + executable runtimes) — `core/src/tools/router.rs` (`ToolRouter`)
- Tool-call runtime / parallel execution gate — `core/src/tools/parallel.rs` (`ToolCallRuntime`)
- Approval orchestration + Guardian routing — `core/src/tools/approvals.rs`
- Shared approval/sandbox traits + approval cache — `core/src/tools/sandboxing.rs`
- Unified-exec tool runtime (approval + sandbox orchestration) — `core/src/tools/runtimes/unified_exec.rs`
- Zsh-fork runtime (login-shell path) — `core/src/tools/runtimes/zsh_fork/`
- Apply-patch runtime — `core/src/tools/runtimes/apply_patch.rs`
- Hosted tool specs (web_search/view_image schema generation) — `core/src/tools/hosted_spec.rs`
- Tool search (BM25 over deferred tools) — `core/src/tools/handlers/tool_search.rs`
- Handlers: shell/exec, write_stdin, apply_patch, view_image, plan, sleep, current_time, request_user_input(+async), send_message_to_user_async, request_permissions, new_context_window, get_context_remaining, wait_for_environment, list/read MCP resources, request_plugin_install, test_sync, multi-agent v1/v2 — `core/src/tools/handlers/`
- Executed-tool-call recording for prompt caching — `core/src/tools/executed_tool_calls.rs`
- Network approval flow (deferred approvals, policy amendments) — `core/src/tools/network_approval.rs`
- Turn diff tracking for `TurnDiff` events — `core/src/turn_diff_tracker.rs`
- Code-mode tool surface — `core/src/tools/code_mode/`

### Core crate — execution & sandboxing
- Unified exec process manager (PTY lifecycle, output caps, reuse) — `core/src/unified_exec/{mod,process,process_manager,process_state}.rs`
- Head/tail output buffering — `core/src/unified_exec/head_tail_buffer.rs`
- stdin approval gating — `core/src/unified_exec/stdin_approval.rs`
- One-shot exec streaming — `core/src/exec.rs`
- Sandbox permission composition — `core/src/sandboxing/`
- Windows sandbox integration — `core/src/windows_sandbox.rs`, `windows_sandbox_read_grants.rs`
- Shell snapshot capture (cached per cwd/shell/login/sandbox) — `core/src/shell_snapshot.rs`, `shell_snapshot_sandbox.rs`
- Exec policy rules (`rules/rules` under CODEX_HOME, prefix rules, dangerous-command checks) — `core/src/exec_policy.rs` (+ `core/src/exec_policy/`)
- Command canonicalization for approval matching — `core/src/command_canonicalization.rs`
- Patch safety assessment — `core/src/safety.rs`
- Shell environment policy application to child processes — `core/src/exec_env.rs`
- User `!command` shell execution — `core/src/user_shell_command.rs`, `core/src/tasks/user_shell.rs`

### Core crate — context & compaction
- Local summarization compaction — `core/src/compact.rs`
- Server-side (remote v2) compaction — `core/src/compact_remote_v2.rs`, `compact_remote_v2_images.rs`, `compact_remote_history.rs`
- Token-budget compaction (fresh window, no summarization) — `core/src/compact_token_budget.rs`
- Compaction model-fallback logic — `core/src/compact_model_fallback.rs`
- History manager (token accounting, retained facts, truncation) — `core/src/context_manager/history.rs`
- Context block builders (environment, developer, guardian, plugins, realtime, memory, …) — `core/src/context/`
- Token-budget context notices — `core/src/context/token_budget_context.rs`

### Core crate — persistence, AGENTS.md, agents
- AGENTS.md discovery & concatenation — `core/src/agents_md.rs`
- Session instruction manager (refresh, inheritance, size validation) — `core/src/agents_md_manager.rs`
- Rollout/thread-store wiring + re-exports — `core/src/rollout.rs`
- Rollout truncation — `core/src/thread_rollout_truncation.rs`
- SQLite state DB init — `core/src/state_db_bridge.rs`
- Session-tree rollout budget accounting — `core/src/rollout_budget.rs`
- Thread manager (lifecycle, fork, internal sessions, unload) — `core/src/thread_manager.rs`
- `CodexThread` public API (submit ops, settings, approvals) — `core/src/codex_thread.rs`
- Multi-agent control plane (spawn, delivery, residency, budget, watch) — `core/src/agent/`, `core/src/agent/control/`
- Agent message board — `core/src/agent_message_board.rs`, `agent_communication.rs`
- Session state, active turn, mailbox phase — `core/src/state/` (`session.rs`, `turn.rs`, `turn_token_usage.rs`, `auto_compact_window.rs`, `additional_context.rs`)
- Multi-agent role specs — `core/src/agent/role.rs`

### Core crate — model client, auth, guardian, MCP, plugins
- Responses API / WebSocket client session — `core/src/client.rs`
- Prompt assembly + response event mapping — `core/src/client_common.rs`, `core/src/event_mapping.rs`, `core/src/stream_events_utils.rs`
- Stream retry/backoff + transport fallback — `core/src/responses_retry.rs`
- Harness metadata on requests — `core/src/responses_metadata.rs`, `responses_headers.rs`
- Model-request construction — `core/src/model_request.rs`
- Client-side tool metadata (prompt-caching tool ordering) — `core/src/client_tool_metadata.rs`
- Attestation header provider boundary — `core/src/attestation.rs`
- Guardian auto-review (approval decisions, reviewer sessions, budgets, prompts) — `core/src/guardian/`, `core/src/guardian_review.rs`
- MCP manager projection + Codex Apps server — `core/src/mcp.rs`
- MCP tool-call execution/approval — `core/src/mcp_tool_call.rs` (+ `mcp_tool_call/`)
- MCP tool exposure policy — `core/src/mcp_tool_exposure.rs`
- MCP elicitation coordination — `core/src/elicitation.rs`
- MCP approval templates / skill dependencies — `core/src/mcp_tool_approval_templates.rs`, `mcp_skill_dependencies.rs`
- Hook runtime (all hook lifecycle execution) — `core/src/hook_runtime.rs`, `hook_mcp_executor.rs`
- Skills loading & implicit invocation — `core/src/skills.rs`
- Plugins (discovery, mentions, injection, metrics) — `core/src/plugins/`
- Connectors/apps instructions — `core/src/connectors.rs`, `core/src/apps/`
- Environment (local/remote executor) selection — `core/src/environment_selection.rs`
- Image preparation/resize for model input — `core/src/image_preparation.rs`, `original_image_detail.rs`
- Web-search event detail formatting — `core/src/web_search.rs`
- Realtime conversation manager + history — `core/src/realtime_conversation.rs`, `realtime_history/`, `realtime_context.rs`, `realtime_prompt.rs`
- Turn timing/metrics — `core/src/turn_timing.rs`, `turn_metadata.rs`
- OTEL init — `core/src/otel_init.rs`
- Prompt debug (`build_prompt_input`) — `core/src/prompt_debug.rs`
- Config loading/validation/editing — `core/src/config/` (`mod.rs`, `edit.rs`)
- Legacy `codex_delegate` — `core/src/codex_delegate.rs`

---

## 2. HOW IT WORKS

### 2.1 Submission pipeline and the single-threaded session loop
Every client action is an `Op` (`protocol/src/protocol.rs:590`). `CodexThread` wraps it in a `Submission` carrying a submission id, optional W3C trace context, turn ancestry, and a residency read-guard that keeps a v2 sub-agent session loaded until the submission is handled (`core/src/session/submission.rs`). A single `submission_loop` per session consumes submissions in FIFO order (`core/src/session/handlers.rs:419+`); `Op::Shutdown` is the only exit. This gives one serialization point for turns, settings, approvals, compaction, and realtime ops. Reply-bearing ops (turn input) get a `oneshot` back, so submission acceptance is decoupled from execution.

### 2.2 Start / steer / reject decision
`core/src/session/turn_input.rs` is "the one place Core decides whether submitted input starts a turn, steers an active turn, or is rejected" (file doc comment). `TurnInputMode` (`protocol/src/turn_input.rs:133`) offers `StartOrSteer`, `StartIfIdle`, `ContinueIfIdle{expected_previous_turn_id}` (rejects if another task started since — guards against stale internal continuations), and `Steer{expected_turn_id}`. The reply is `Started/Steered/NotSubmitted{reason}` with a rich `NotSubmittedReason` enum (Superseded, ServerDraining, NotIdle, PendingTriggerTurn, PlanMode, NoActiveTurn, …). `NonSteerableTurnKind::{Review, Compact}` turns reject same-turn steering (`protocol/src/protocol.rs:1845`). Steered input cannot change the active turn's context; its thread-settings apply to subsequent turns.

### 2.3 The sampling loop (`run_turn`)
`core/src/session/turn.rs:163` implements the agent loop. Per its doc comment: each sampling request yields either function calls (executed, outputs fed into the next request) or an assistant message (recorded; turn completes). Sequence: pending Guardian input check → drain async hook results → **pre-sampling compaction** → collect required MCP servers/plugins from the input → capture a `StepContext` → record world state → build skills/plugin injections → session-start hooks → record inputs (persisted `PersistContext::TurnStart`) → then the loop: drain queued steered input, re-capture step context when input arrived, record world-state deltas and reasoning-effort overrides, build `sampling_request_input` from `sess.clone_history().for_prompt(input_modalities)`, run `run_sampling_request`, and afterwards check `needs_follow_up`, pending input, and context-window token status to decide compact/continue/complete. A turn-scoped `ModelClientSession` and a `TurnDiffTracker` are threaded through. Errors preserve the input on every early-exit path (so nothing is silently lost).

### 2.4 Event protocol details
`EventMsg` is internally-tagged (`type`, snake_case) with deliberate wire compat: `TurnStarted` serializes as `task_started` but accepts `turn_started`; same for `task_complete` (`protocol/src/protocol.rs:1436-1460`). Events are correlated by submission id. The design distinguishes: lifecycle (`TurnStarted/Complete/Aborted` with timestamps, `root_turn_id`, `trace_id`), item-level (`ItemStarted`/`ItemCompleted` wrapping `TurnItem`s — the same items persisted to rollouts), deltas (`AgentMessageContentDelta`, `ReasoningContentDelta`, `ReasoningRawContentDelta`, `PlanDelta`), and approvals (`ExecApprovalRequest`, `ApplyPatchApprovalRequest`, `ElicitationRequest`, `RequestPermissions`, `RequestUserInput`, `DynamicToolCallRequest`). `TokenCountEvent` carries `TokenUsageInfo` plus a `RateLimitSnapshot` with quota aliases. Plan-mode text is streamed through `PlanModeStreamState`/`AssistantMessageStreamParsers` with `FuturesOrdered` ordering (`core/src/session/turn.rs:1885-1946`).

### 2.5 Tool plan assembly and the exposure lattice
Each sampling request builds a fresh `ToolRouter` (`core/src/tools/spec_plan.rs:123`): a `ToolRegistry` populated by `add_core_tool_sources` (shell, MCP resources, utilities, collaboration) → MCP tools appended by the MCP handler cache → extension executors → dynamic tools → hosted specs → `finalize_tool_router`. Every tool carries a `ToolExposure` computed from a lattice over `ToolExposureSurface::{Direct, Deferred, CodeMode}` (`protocol/src/config_types.rs:400`): `Direct`, `DirectModelOnly`, `Deferred`, `DeferredModelOnly`, `CodeModeOnly`, `Hidden` (`core/src/tools/spec_plan.rs:230-260`). `direct` and `deferred` are mutually exclusive; when tool-search is on, deferred tools are removed from the direct list. Server configs can omit tools from specific surfaces (`omit_tools_from`). Registration order is explicit (`registry.rs` has `prepend_trusted`), collisions are recorded and fail finalization. `Op`-level `ToolPolicy` can require managed sandbox or unified exec, silently emptying the registry otherwise.

### 2.6 Tool execution and approvals
`ToolRouter` maps model calls to `ToolCall`s (with optional encrypted function args, and a `DirectPlaintextMessage` source for collaboration messages that must never be logged in cleartext — `core/src/tools/router.rs:36-80`). `ToolCallRuntime` (`core/src/tools/parallel.rs`) executes calls under an `RwLock` parallel-execution gate (parallel only when the registry says the tool supports it and policy allows), with timing guards and abort propagation. Approvals flow through `core/src/tools/approvals.rs`: command canonicalization → exec-policy evaluation → permission-request hooks → Guardian reviewer decision → cached or emitted approval request. `ApprovalStore` (`core/src/tools/sandboxing.rs:48`) caches `ReviewDecision`s keyed by the serialized JSON of the approval key, so "always allow" persists across calls within the turn without re-prompting. Guardian assessments surface as `GuardianAssessment` events, and a denied action can be retried once via `Op::ApproveGuardianDeniedAction`.

### 2.7 Unified exec
`core/src/unified_exec/mod.rs` documents the design: the PTY/process layer is isolated from policy; a shared `ToolOrchestrator` handles approval → sandbox selection → run → retry. Constants: max 64 concurrent processes, 1 MiB / ~256Ki-token output caps, default 10k max output tokens, yield windows 250 ms–30 s (5 s floor for empty `write_stdin`), 5-minute default background-terminal timeout (`background_terminal_max_timeout` config). Sandbox denial is detected by the shared `is_likely_sandbox_denied` heuristic and retried unsandboxed only when policy allows, reusing the cached approval (no double prompt). When the `UnifiedExec` feature is off but policy demands resumable processes, tool registration refuses instead of downgrading (`spec_plan.rs:1103-1114`). `write_stdin` polls background terminals; `Op::CleanBackgroundTerminals` terminates them.

### 2.8 Compaction — three implementations, one lifecycle
- **Local** (`core/src/compact.rs`): prompts the model with `SUMMARIZATION_PROMPT`, truncates the compact user message to 20,000 tokens (`COMPACT_USER_MESSAGE_MAX_TOKENS`).
- **Remote v2** (`core/src/compact_remote_v2.rs`): server-side compaction over the Responses API, with model-fallback retry (`compact_model_fallback.rs`) and history item grouping/annotation (`compact_remote_history.rs`).
- **Token budget** (`core/src/compact_token_budget.rs`): skips summarization entirely and installs a *fresh context window* — but still modeled as a compaction (Pre/PostCompact hooks, `ContextCompaction` turn item) so all observers see one lifecycle.

`InitialContextInjection` (`core/src/compact.rs:53`) encodes a subtle model-training constraint: pre-turn/manual compaction uses `DoNotInject` (history replaced by summary; initial context fully reinjected by the next regular turn), while **mid-turn** compaction must use `BeforeLastUserMessage` because the model is trained to see the summary as the last history item, so initial context is injected just above the last real user message.

### 2.9 Auto-compaction triggers and token accounting
`core/src/session/context_window.rs` computes `ContextWindowTokenStatus`: active context tokens, scope tokens vs the configured limit, window prefill baseline, and three booleans (`token_limit_reached`, `full_context_window_limit_reached`, `turn_end_compaction_threshold_reached`). `model_auto_compact_token_limit_scope` selects `Total` (whole context vs the model's catalog limit) or `BodyAfterPrefix` (tokens after the compaction window's `prefill_input_tokens` baseline — i.e., only new work in this window counts). Turn-end compaction can additionally trigger on a percentage of the usable window (`model_post_turn_compact_threshold_percent`, 0–100). `run_auto_compact` / `run_pre_sampling_compact` are called from `run_turn`; compaction failure paths deliberately preserve un-recorded input (`turn.rs:175-220`). Compaction windows are numbered (`AutoCompactWindowIds`, `core/src/state/auto_compact_window.rs`) and compaction checkpoints are ordered against settings commits via a dedicated `thread_settings_persistence` semaphore so persisted events stay ordered.

### 2.10 Context assembly: world state + tagged blocks
`Session::build_world_state_for_step` (`core/src/session/world_state.rs`) assembles every model-visible block into a `WorldState` with per-block states (`AgentsMdState`, `PermissionsState`, `ToolsState`, `CollaborationModeState`, etc. in `core/src/context/world_state/`). Blocks are wrapped in the XML-ish tags from `protocol/src/protocol.rs:118-143` (`<user_instructions>`, `<environment_context>`, `<tools>`, …). Crucially, the *exact* `StepContext` (tools, MCP binding, settings, AGENTS.md) is captured once per sampling request and reused for context, advertised tools, and tool execution, then persisted as a `TurnContextItem` (`core/src/session/step_context.rs`) — so the durable record matches what the model actually saw. Only *changed* world state is re-injected per step (`record_step_world_state_if_changed`).

### 2.11 AGENTS.md handling
`core/src/agents_md.rs`: walk up from cwd to the project root (markers default `[".git"]`, configurable via `project_root_markers`; empty list disables traversal; no marker → cwd only), then concatenate every `AGENTS.md` from root down to cwd in that order, never past the root. `AGENTS.override.md` is the preferred local override; fallback filenames configurable (`project_doc_fallback_filenames`); total bytes capped at 32 KiB by default (`DEFAULT_PROJECT_DOC_MAX_BYTES`, `config/src/config_toml.rs:74`), enforced per-environment with a remaining-bytes budget. Untrusted projects get no project docs at all. `AgentsMdManager` (`core/src/agents_md_manager.rs`) serializes refreshes behind a semaphore, validates size, and serves the applied snapshot without blocking. Ancestor metadata probes are capped at 256 concurrent per environment (remote-friendly).

### 2.12 Persistence: rollouts, thread store, rollout budget
Live thread state is persisted through `codex-thread-store`/`codex-rollout` (external crates) but owned here: `core/src/rollout.rs` re-exports the recorder and adapts `Config` to `RolloutConfigView`; SQLite state DB init is bridged in `core/src/state_db_bridge.rs`; rollout truncation for size lives in `core/src/thread_rollout_truncation.rs`. `core/src/rollout_budget.rs` implements a *session-tree* token budget: weighted tokens used across the whole root-thread tree, per-thread reminder deliveries so every thread observes crossed thresholds, and `RolloutBudgetReminder{remaining_tokens, reminder_index}` acknowledged only after history insertion (`core/src/session/rollout_budget.rs`). Startup acquires a `LiveThreadInitGuard` that must be committed or discarded — never leaked — even if handoff is interrupted (`core/src/session/startup.rs`).

### 2.13 Model client, WebSocket prewarm, and retry
`core/src/client.rs` splits lifetimes explicitly (file doc): `ModelClient` is session-scoped (auth, provider, conversation id, transport fallback state); `ModelClientSession` is turn-scoped and caches a lazily-opened Responses **WebSocket** plus the `x-codex-turn-state` sticky-routing token; both are discarded when auth ownership changes. WebSocket prewarm is a v2 `response.create` with `generate=false` that completes so the next request reuses the connection and `previous_response_id`; a failed prewarm counts as the first connection attempt and normal retry logic recovers. Retry lives in `core/src/responses_retry.rs`: connection retries back off 5 s → 60 s; stream errors honor server retry advice and `Retry-After`; exhausted-retry advice is tagged with the turn id so a reused Guardian session can't apply stale advice. Turn execution runs prewarm best-effort before the first stream (`core/src/session/turn.rs` comment on `ModelClientSession` reuse).

### 2.14 Multi-agent (v2) architecture
Sub-agents are full sessions with their own threads. The model-facing surface is the `multi_agents_v2` tool namespace: `spawn_agent`, `send_message`, `followup_task`, `list_agents`, `interrupt_agent`, `wait_agent` (`core/src/tools/handlers/multi_agents_v2.rs`). Spawning goes through `core/src/agent/control/spawn.rs` (child config build, role application, permission-profile intersection, fork modes `SpawnAgentForkMode`, telemetry); delivery/residency/budget/watch/completion are separate control modules (`core/src/agent/control/`). Depth limits are enforced (`exceeds_thread_spawn_depth_limit`, `core/src/agent/registry.rs`). Inter-agent messages are `InterAgentCommunication` items (recordable as history and convertible to model input, with an `new_encrypted` constructor for encrypted payloads — `protocol/src/protocol.rs:844`). Cross-thread mail is folded into the parent turn by the `MailboxDeliveryPhase` state machine (`core/src/state/turn.rs`): `CurrentTurn` → `NextTurn` once visible final output exists, reopenable by a steer.

### 2.15 Review mode and Guardian
Two distinct "review" systems:
- **`Op::Review`** (explicit `/review`): `core/src/session/handlers.rs:383` resolves the request (`core/src/review_prompts` via `codex-prompts`), spawns a *review child session* with `config.review_model` (falling back to the parent model), forcibly disabling web search and goals for the review session (`core/src/session/review.rs:17-50`). Results are structured `ReviewOutputEvent{findings, overall_correctness, …}` rendered by `protocol/src/review_format.rs`; lifecycle is `EnteredReviewMode`/`ExitedReviewMode` events + persisted items. Review and compact turns are `NonSteerableTurnKind`.
- **Guardian** (auto-approvals reviewer): an isolated synchronous reviewer extension (`core/src/guardian/`) that decides escalated approvals. Core owns permissions and *mandatory* review requirements; the extension owns policy and evidence (module doc, `core/src/guardian/mod.rs`). It has its own input budget (`input_budget.rs` — pending review evidence can itself force compaction, with a special `ExhaustedReviewBudget::Compacting` state handled in `turn.rs:300-330`), request budget (`request_budget.rs`), review-session manager with prewarm (`review_session.rs`), and persisted checkpoints (`core/src/session/guardian_checkpoint.rs`). `ApprovalsReviewer` config routes escalations; `auto_review` TOML adds policy text and a strict circuit-break mode.

### 2.16 Hooks, MCP, and skills
`core/src/hook_runtime.rs` executes the full hook taxonomy (Command / MCP-tool / Prompt / Agent handlers) at the `HookEventName` points, emits `HookStarted`/`HookCompleted` events with summaries, drains async hook results before user prompts, and lets `PreToolUse`/`PreCompact`/`Stop` hooks abort turns. `core/src/mcp.rs` builds `McpRuntimeProjection` (config + plugin availability + selected plugins in one view); per-turn required MCP servers are collected from the input (mentions, skills) in `run_turn` and gate the first sampling request (`required_mcp_servers_for_input`, `turn.rs:910`). MCP tool exposure and omit-lists are applied in `spec_plan.rs:170-260`. Elicitations pause tool-result delivery via a refcounted `ElicitationService` (`core/src/elicitation.rs`). Skills load from configured roots and can be invoked implicitly (`core/src/skills.rs`); plugins contribute tools, MCP servers, skills, and prompt fragments via the extension registry (`core/src/plugins/`, `core/src/session/plugin_selection.rs`).

---

## 3. CONFIG

### 3.1 Config system shape
- Layered TOML: CLI overrides → user `~/.codex/config.toml` → project `.codex/` folders; merged into `ConfigToml` with a retained `ConfigLayerStack` (`core/src/config/mod.rs:2079-2100`, `load_config_toml_with_layer_stack`). Values that managed requirements disallow fall back to the required value with a startup warning (`apply_requirement_constrained_value`, `core/src/config/mod.rs`).
- Feature flags: centralized `[features]` table (`FeaturesToml`) plus a managed-features overlay; flags referenced in core include `ShellTool`, `UnifiedExec`, `UnifiedExecTty`, `ExecPermissionApprovals`, `TokenBudget`, `ViewImage`, `SleepTool`, `CurrentTimeReminder`, `WebSearchRequest`, `WebSearchCached`, `StandaloneWebSearch`, `CodeMode`, `CodeModeOnly`, `DeferredExecutor`, `RequestPermissionsTool`, `SendMessageToUserAsync`, `CwdRelativeTurnDiffs`, `ContextManagement`, `Goals` (used via `codex_features::Feature` throughout `core/src/tools/spec_plan.rs`, `core/src/session/token_budget.rs`, `core/src/session/review.rs`; the flag registry itself is in the `codex-features` crate, outside scope).
- Thread-level settings overrides (per submission): model, reasoning effort/summary, web_search mode, sandbox/permissions, service tier, collaboration mode, personality, disabled plugins (`ThreadSettingsOverrides`, `protocol/src/protocol.rs:511`).

### 3.2 Config keys (from `ConfigToml`, `config/src/config_toml.rs:165-552` — schema consumed by `core/src/config`)
| Key | Purpose / default (where stated) |
|---|---|
| `model`, `review_model` | Model selection; review-model override for `/review` |
| `model_provider`, `model_providers` | Provider id; user-defined providers (built-ins not overridable) |
| `model_context_window` | Context window override (tokens) |
| `model_auto_compact_token_limit` | Auto-compaction trigger threshold |
| `model_auto_compact_token_limit_scope` | `Total` or `BodyAfterPrefix` (`protocol/src/config_types.rs:49`) |
| `model_post_turn_compact_threshold_percent` | 0–100; 0/omitted disables turn-end compaction |
| `approval_policy` | `AskForApproval` incl. `Granular{sandbox_approval, rules_approval, skill_approval, request_permissions, mcp_elicitations}` (`protocol/src/protocol.rs:986-1049`) |
| `approvals_reviewer` | Routes escalated approvals (`guardian` supported) |
| `auto_review` | Guardian policy: `policy`, `extra_policy`, `experimental_policy_template`, `circuit_break_action` (`default`/`strict`) |
| `browser_use`, `computer_use` | Browser/computer-use tool config |
| `shell_environment_policy` | Inherit/`include_only`/`exclude`/`set` env filtering (`protocol/src/config_types.rs:207-296`) |
| `allow_login_shell` | Default `true` |
| `sandbox_mode` | `SandboxMode` (read-only / workspace-write / danger-full-access) |
| `allow_symlinked_codex_home` | Default `false`; macOS writable-root symlink trust |
| `sandbox_workspace_write` | Writable roots, network toggles, excluded paths |
| `default_permissions`, `permissions` | Permission profiles (named, `:`-prefixed built-ins) |
| `notify` | External notify command |
| `instructions` | Base-instruction override |
| `developer_instructions`, `include_permissions_instructions`, `include_apps_instructions`, `include_collaboration_mode_instructions`, `include_environment_context` | Context-block injection toggles |
| `model_instructions_file` | Full instruction override (explicitly discouraged) |
| `compact_prompt`, `experimental_compact_prompt_file` | Compaction prompt override |
| `forced_chatgpt_workspace_id`, `forced_login_method` | Login restrictions |
| `cli_auth_credentials_store` | `file` (default) / `keyring` / `auto` |
| `mcp_servers` | MCP server definitions (raw shape) |
| `mcp_enterprise_managed_auth` | EMA IdP config |
| `mcp_oauth_credentials_store`, `mcp_oauth_callback_port`, `mcp_oauth_callback_url` | MCP OAuth |
| `mcp_optional_startup_grace_ms` | Default 1000; 0 disables shared grace |
| `project_doc_max_bytes` | Default 32 KiB (`DEFAULT_PROJECT_DOC_MAX_BYTES`) |
| `project_doc_fallback_filenames` | AGENTS.md fallback names |
| `project_root_markers` | Default `[".git"]` |
| `tool_output_token_limit` | Token budget for stored tool outputs |
| `background_terminal_max_timeout` | Default 300000 ms (5 min) |
| `thread_unload_delay_secs` | Default 60; 0 = immediate unload |
| `profile`, `profiles` | Named config profiles |
| `history` | `~/.codex/history.jsonl` persistence settings |
| `sqlite_home` | State DB dir (`$CODEX_SQLITE_HOME` else `$CODEX_HOME`) |
| `log_dir` | Default `$CODEX_HOME/log` |
| `file_opener` | URI-scheme hyperlinks for file citations |
| `tui` | TUI settings (incl. `disable_paste_burst` legacy top-level key) |
| `hide_agent_reasoning` (default false), `show_raw_agent_reasoning` | Reasoning display |
| `model_reasoning_effort`, `plan_mode_reasoning_effort`, `model_reasoning_summary`, `model_verbosity` | Reasoning controls |
| `model_catalog_json` | Startup-only JSON model catalog |
| `personality` | Deprecated (`friendly`/`pragmatic` no-op) |
| `service_tier` | Request id, e.g. `default`/`priority`/`flex` (legacy `fast`); default request value `"default"` (`SERVICE_TIER_DEFAULT_REQUEST_VALUE`, `protocol/src/config_types.rs:537`) |
| `chatgpt_base_url`, `openai_base_url` | Backend URL overrides |
| `apps_mcp_product_sku`, `apps` | Codex Apps settings |
| `responses_api_metadata` | Bounded k/v on every Responses request |
| `web_search` | `WebSearchMode`: Disabled / **Cached (default)** / Indexed / Live (`protocol/src/config_types.rs:376`) |
| `tools.web_search` | `context_size` (low/medium/high), `allowed_domains`, `location` |
| `tools.experimental_request_user_input.enabled` | Default `true` |
| `tools.update_plan.enabled` | Default `false` |
| `tool_suggest` | Discoverable-tool suggestion config |
| `agents`, `goals`, `memories`, `skills`, `hooks`, `plugins`, `marketplaces` | Subsystem tables |
| `features`, `suppress_unstable_features_warning` | Feature flags + warning suppression |
| `check_for_update_on_startup` | Default `true` |
| `analytics` (default true), `feedback` (default true) | Telemetry toggles |
| `windows` | Windows sandbox config |
| `otel`, `notice`, `desktop` | OTEL / notices / opaque desktop storage |
| `experimental_use_unified_exec_tool` | Unified-exec toggle |
| `oss_provider` | `"lmstudio"` / `"ollama"` |
| `experimental_realtime_*`, `realtime`, `audio` | Realtime voice overrides (transport webrtc/websocket, conversational/transcription, voice) |
| `experimental_thread_store` | `Local` / `InMemory` |
| `projects.<path>.trust_level` | Trusted/Untrusted per project (`TrustLevel`) |
| `model_verbosity`→`Verbosity`, `sleep_tool_mode` (AlwaysOn/ModelDriven), `current_time_reminder.sleep_tool` | Referenced from `core/src/tools/spec_plan.rs:1226-1250` |
| `js_repl_node_path`, `js_repl_node_module_dirs`, `experimental_thread_store_endpoint` | Deprecated/removed, retained to fail fast |

### 3.3 Environment variables observed in core/ + protocol
- `OPENAI_API_KEY`, `OPENAI_BASE_URL` — provider auth/base URL (referenced throughout `core/src/client.rs`).
- `CODEX_HOME`, `CODEX_SQLITE_HOME` — config/persistence roots (referenced in `core/src/config` and `core/src/rollout.rs` comments).
- `CODEX_THREAD_ID`, `CODEX_SESSION_ID`, `CODEX_VERSION`, `CODEX_PERMISSION_PROFILE` — injected into child processes by `core/src/exec_env.rs` (`CODEX_PERMISSION_PROFILE` is explicitly informational only, not proof of enforcement).
- `CODEX_SANDBOX`, `CODEX_SANDBOX_NETWORK_DISABLED` — sandbox interop (`core/src/sandboxing/`, `core/src/windows_sandbox.rs`).
- `CODEX_NETWORK_PROXY_ACTIVE`, `CODEX_NETWORK_PROXY_BROKERED_CREDENTIALS`, `CODEX_NETWORK_PROXY_SNAPSHOT_*` — network-proxy credential brokering (`core/src/shell_snapshot.rs`).
- `CODEX_CI` — CI detection.
- `OPENAI_WORKLOAD_IDENTITY_CONTEXT`, `OPENAI_IDENTITY_TOKEN_FILE`, `OPENAI_FEDERATION_RULE_ID` — workload identity auth.
- `CODEX_ESCALATE_SOCKET`, `CODEX_EXEC_SERVER_NOISE_AUTH_TOKEN` — escalation / exec-server auth.
- `CODEX_APPLY_PATCH_PRESERVE_LINE_ENDINGS` — re-exported at `core/src/exec_env.rs:1`.
- `CODEX_INTERNAL_ORIGINATOR_OVERRIDE_ENV_VAR` — originator override (re-exported from `codex-login`, `core/src/thread_manager.rs`).
- `CODEX_SHELL_ENVIRONMENT_SCRUBBER_TEST_MODE`, `CODEX_TEST_*`, `CODEX_PROTOCOL_TEST_*` — test-only.

### 3.4 Protocol-level settings applied per turn
- `TurnSettingsUpdate` (effort, summary, verbosity, web search, permissions, tier, collaboration mode, personality, disabled plugins) and `GranularApprovalConfig` gate which approval kinds may be requested (`protocol/src/protocol.rs:480-1049`).
- `SandboxPolicy::new_read_only_policy()` / `new_workspace_write_policy()` define the built-in permission presets (`protocol/src/protocol.rs:1204-1213`).

---

## 4. NOTABLE — engineering worth adopting

1. **Submission id correlation everywhere.** Every event carries the originating submission id (`Event.id`, `protocol/src/protocol.rs:1340`), and every reply-bearing op returns a oneshot. This makes any front-end trivially able to correlate async work, and it is the backbone of the whole protocol. Cheap, uniform, and rare in practice.

2. **Start/steer/reject as a single typed decision.** `core/src/session/turn_input.rs` centralizes the entire concurrency decision (steer vs start vs reject) with exhaustive `NotSubmittedReason`s including `ContinueIfIdle{expected_previous_turn_id}` — an optimistic-concurrency token that prevents stale internal continuations from firing after a user interrupted (`protocol/src/turn_input.rs:133-149`). This pattern generalizes to any agent with queued user input.

3. **The tool-exposure lattice.** Instead of boolean "tool on/off", every tool gets exposure computed over `Direct × Deferred × CodeMode` surfaces with a mutually-exclusive direct/deferred rule and per-server omit lists (`core/src/tools/spec_plan.rs:170-260`). Combined with BM25 `tool_search` over deferred tools (`core/src/tools/handlers/tool_search.rs`), this is a clean answer to "too many tools in the prompt" — worth adopting wholesale.

4. **`StepContext` as an immutable, persistable snapshot.** One capture per sampling request is shared by prompt assembly, tool advertisement, and tool execution, and is itself serializable into a `TurnContextItem` (`core/src/session/step_context.rs`). The durable rollout therefore proves what the model actually saw — invaluable for replay and debugging.

5. **`MailboxDeliveryPhase` (CurrentTurn → NextTurn → reopen).** `core/src/state/turn.rs:36-52` solves the "child agent finished after the user already saw the final answer" problem with a tiny explicit state machine that reopens when a steer or post-answer tool call occurs. Better than timestamp heuristics.

6. **`InitialContextInjection` encoding a model-training constraint in the type system.** Mid-turn compaction must place the summary last because models are trained that way; pre-turn must not. `core/src/compact.rs:53` makes this a two-variant enum so the wrong choice cannot be made silently.

7. **Compaction-window baseline accounting.** `BodyAfterPrefix` scope charges only tokens added since the window's `prefill_input_tokens` baseline, not the whole context (`core/src/session/context_window.rs:52-80`) — so a heavily prefilled window doesn't instantly re-trigger compaction.

8. **Approval cache keyed by canonicalized-approval JSON.** `ApprovalStore` (`core/src/tools/sandboxing.rs:48`) keys `ReviewDecision`s by `serde_json::to_string(key)`, plus explicit command canonicalization (`core/src/command_canonicalization.rs`) so "always allow `cargo test`" actually matches `cargo test --quiet`.

9. **WebSocket prewarm with `generate=false`.** `core/src/client.rs:1-27` — a completed no-generate request warms the connection and `previous_response_id` chain so the real request rides an established socket; prewarm failure is deliberately counted as the first retry attempt so the retry budget isn't doubled. Subtle and correct.

10. **Residency guards on submissions.** A `Submission` carries an `OwnedRwLockReadGuard` that keeps a v2 sub-agent session resident until the submission is handled or dropped (`core/src/session/submission.rs`) — resource lifetime expressed in the type system, zero bookkeeping code.

11. **Turn diff with a 100 ms budget.** `TurnDiffTracker` produces git-style diffs per turn but falls back to a coarse, content-exact diff if the fine-grained one exceeds 100 ms, so display never stalls tool completion (`core/src/turn_diff_tracker.rs:16-18`).

12. **`#![deny(clippy::print_stdout, print_stderr)]` in a library** (`core/src/lib.rs:4`) — a one-line guarantee that core never bypasses the event/telemetry abstractions with stray prints.

13. **Secrets-safe tool logging by construction.** Collaboration `send_message`/`spawn_agent` calls with empty encrypted args are classified `DirectPlaintextMessage`, and `tool_log_payload` returns `"[plaintext arguments]"` for them (`core/src/tools/router.rs:70-80`) — privacy enforced at the logging boundary rather than by convention.

14. **Three implementations, one lifecycle for compaction.** Token-budget compaction skips summarization but still emits Pre/PostCompact hooks and a `ContextCompaction` item (`core/src/compact_token_budget.rs`) so hooks, UIs, and rollouts need no special cases.

15. **`Op` is intentionally not `Clone`** (`core/src/thread_manager.rs:127-130`), forcing test code to construct an explicit small snapshot of ops rather than cheaply duplicating submission state — a discipline choice worth copying in message-passing cores.

---

### Explicit non-claims
- Runtime behavior was not verified; everything above is from reading source and in-code docs. Nothing was built, run, or tested.
- Files over ~10k lines (mostly tests: `core/src/config/config_tests.rs`, `core/src/session/tests.rs`, `core/src/agent/control_tests.rs`) were not read in full; inventory lines for those areas come from non-test sources.
- Components whose implementation lives outside `core/`/`protocol/` (the actual rollout recorder in `codex-rollout`, sandbox enforcement in `codex-sandboxing`/`linux-sandbox`, auth storage in `codex-login`/`keyring-store`, execpolicy rule engine in `codex-execpolicy`, apply-patch parser in `codex-apply-patch`, features registry in `codex-features`) are described only through their core-side wiring, which was verified; their internals were not audited.
- The exact set of `[features]` flag names and their defaults lives in the `codex-features` crate (outside scope); the flags listed are those referenced from core code that was read.
