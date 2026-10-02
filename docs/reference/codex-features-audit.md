# Codex CLI (`codex-rs`) — Audit of Everything Outside `tui/` and `core/`

> Extracted by a subagent audit of the Codex source tree. Companion
> reports: [codex-tui-audit.md](codex-tui-audit.md),
> [codex-core-audit.md](codex-core-audit.md), and the opcode-focused
> synthesis in [codex-adoption.md](codex-adoption.md).

Scope: all crates in `codex-rs/` except `tui/` and `core/` (which were only skimmed for shared types they consume, e.g. `protocol/`). All file paths are relative to `codex-rs/`. Everything below was read directly from source; where a detail could not be verified, it says so explicitly.

---

## 1. INVENTORY

### Sandboxing & process isolation
- Linux sandbox helper binary (bubblewrap + seccomp/landlock): `linux-sandbox/src/main.rs`, `linux-sandbox/src/lib.rs`, `linux-sandbox/src/linux_run_main.rs`
- Bundled bubblewrap launcher: `linux-sandbox/src/bundled_bwrap.rs`, `linux-sandbox/src/bazel_bwrap.rs`
- bwrap arg construction, capability probing, `--ro-bind-fd` legacy translation: `linux-sandbox/src/launcher.rs`, `linux-sandbox/src/fd_mount.rs`
- In-process Landlock + seccomp (no_new_privs, network filters, VM-socket denial): `linux-sandbox/src/landlock.rs`
- PID namespace reuse for daemonized children: `linux-sandbox/src/daemon_mounts.rs`, `sandboxing/src/linux_pid_namespace.rs`
- WSLg / WSL1 detection and warnings: `linux-sandbox/src/wslg.rs`, `sandboxing/src/bwrap.rs` (WSL1_BWRAP_WARNING)
- macOS Seatbelt policy generation: `sandboxing/src/seatbelt.rs` (+ `seatbelt_base_policy.sbpl`, `seatbelt_network_policy.sbpl`, `seatbelt_preferences_policy.sbpl`, `seatbelt_read_only_platform_defaults.sbpl`)
- Seatbelt scratch-dir access rules: `sandboxing/src/seatbelt_scratch.rs`
- Seatbelt protected-socket policy (daemon socket path deny rules incl. ancestor unlink): `sandboxing/src/seatbelt_daemon.rs`
- Sandbox manager: platform selection, transform of commands into sandboxed spawns: `sandboxing/src/manager.rs`
- Permission-profile → policy transforms (normalization, merging, glob deny-read): `sandboxing/src/policy_transforms.rs`
- Sandbox violation detection/recovery (parse child stderr, reclassify): `sandboxing/src/denial.rs`, `sandboxing/src/violation.rs`
- Windows restricted-token sandbox (AppContainer/restricted token, ACLs, deny-read walker, DPAPI, hide-users, ConPTY): `windows-sandbox-rs/src/` (~77 files, e.g. `token.rs`, `acl.rs`, `deny_read_walker.rs`, `identity.rs`, `wfp.rs`)
- Windows MXC sandbox helper (policy conversion + native exec): `mxc-sandbox/src/lib.rs`, `mxc-sandbox/src/policy.rs`, `mxc-sandbox/src/windows.rs`
- Windows sandbox service (Windows service hosting the sandbox broker, named-pipe IPC, package identity authorization): `windows-sandbox-service/src/main.rs`, `windows-sandbox-service/src/lib.rs`, `windows-sandbox-service/src/ipc.rs`
- Network policy proxy (HTTP + SOCKS5, MITM, allowlists): `network-proxy/src/` (see §2)
- Unix shell escalation broker (patched shell exec interception): `shell-escalation/src/unix/escalate_server.rs`, `shell-escalation/src/unix/escalate_protocol.rs`, `shell-escalation/src/unix/escalate_client.rs`, `shell-escalation/src/unix/execve_wrapper.rs`
- Process hardening (pre-main: core dumps off, ptrace deny, LD_PRELOAD/DYLD scrub): `process-hardening/src/lib.rs`
- Arg0-based multi-tool dispatch (`apply_patch`, `codex-linux-sandbox`, `codex-fs-helper`, `codex-execve-wrapper`, windows sandbox, exec helper): `arg0/src/lib.rs`
- Argv[1] constants: `apply-patch/src/lib.rs` (`CODEX_CORE_APPLY_PATCH_ARG1`), `exec-server/src/arg0_exec_helper.rs`, `exec-server/src/fs_helper.rs`, `sandboxing/src/landlock.rs` (`CODEX_LINUX_SANDBOX_ARG0`)

### Policy engines
- Exec-policy (Starlark-like prefix rules with match/not_match self-tests and host executable pinning): `execpolicy/src/parser.rs`, `execpolicy/src/rule.rs`, `execpolicy/src/policy.rs`, `execpolicy/src/executable_name.rs`, `execpolicy/src/execpolicycheck.rs`
- Blocking policy amendment (append allow-prefix / network rules): `execpolicy/src/amend.rs`
- Prefix-rule migration from legacy config: `execpolicy/src/sandbox_migration.rs`
- Permissions TOML (profiles with `extends`, workspace roots, fs entries, network): `config/src/permissions_toml.rs`
- Runtime permission profile types (Managed/Disabled/External; fs sandbox policy): `protocol/src/models.rs`, `protocol/src/permissions.rs`
- Filesystem deny-read globs, special paths, target materialization: `protocol/src/permissions/{target.rs,deny_read_validator.rs,windows_glob.rs,local_aliases.rs}`

### Exec server (remote executor protocol)
- JSON-RPC dialect (no `"jsonrpc"` field), bounded deserialization: `exec-server-protocol/src/rpc.rs`
- Wire protocol (initialize, process/*, fs/*, environment/*): `exec-server-protocol/src/protocol.rs`
- Capability discovery v2 (plugins, skills, MCP servers on executor): `exec-server-protocol/src/capabilities.rs`, `exec-server/src/discoverV2/`
- Server implementation, environments, sandboxed fs ops: `exec-server/src/server.rs`, `exec-server/src/environment.rs`, `exec-server/src/sandboxed_file_system.rs`, `exec-server/src/fs_sandbox.rs`
- Local process/file implementations: `exec-server/src/local_process.rs`, `exec-server/src/local_file_system.rs`, `exec-server/src/regular_file.rs`
- Remote relay over WebSocket with hybrid Noise: `exec-server/src/relay.rs`, `exec-server/src/relay_proto.rs`, `exec-server/src/noise_relay/`
- Noise channel (Noise_hybridIK X25519+ML-KEM-768 AES-GCM): `exec-server/src/noise_channel.rs`
- Remote executor client + noise rendezvous connect bundles: `exec-server/src/client.rs`, `exec-server/src/client_api.rs`
- Remote/direct transports, registration retry: `exec-server/src/remote/`
- PTY spawn helper integration: `utils/pty/` (referenced from `app-server/src/bin/exec_server.rs`)
- WebSocket pong watchdog: `exec-server/src/websocket_pong_watchdog.rs`

### App server (IDE/daemon surface)
- Server binary with transports stdio/unix-socket/websocket/off: `app-server/src/main.rs`, `app-server-transport/src/transport/mod.rs`
- Core connection loop, routing, startup lock: `app-server/src/lib.rs`, `app-server/src/transport.rs`
- JSON-RPC request processors (40+ files): `app-server/src/request_processors/*.rs`
- Protocol types (v2, one module per domain: thread, turn, item, mcp, plugin, review, fs, process, remote_control, realtime, user_verification, …): `app-server-protocol/src/protocol/v2/*.rs`, method table in `app-server-protocol/src/protocol/common.rs`
- Experimental API gating via proc-macro `#[experimental("...")]`: `codex-experimental-api-macros/src/lib.rs`, `app-server-protocol/src/experimental_api.rs`
- Protocol docs/schema export, TS + JSON schema generation: `app-server-protocol/src/export.rs`, CLI subcommands `app-server generateTs/generateJsonSchema` (`cli/src/main.rs` `GenerateTs`, `GenerateJsonSchema`, `GenerateInternalJsonSchema`)
- In-process client for `codex exec`: `app-server-client/src/lib.rs`, used by `exec/src/lib.rs`
- Daemon (managed install, update loop, restart, remote control persistence): `app-server-daemon/src/{lib.rs,update_loop.rs,managed_install.rs,prepare_install.rs,settings.rs,launch.rs,migration.rs}`
- Remote control (enroll/pair/websocket bridge to phone clients): `app-server-transport/src/transport/remote_control/`, `cli/src/remote_control_cmd.rs`
- Turn admission (concurrency gate for turns): `app-server/src/turn_admission.rs`
- User-verification adapter + RPC surface: `app-server/src/user_verification*.rs`, protocol `app-server-protocol/src/protocol/v2/user_verification.rs`
- Local credential provider (macOS Secure Enclave-backed signing): `user-verification/src/{lib.rs,credential.rs,guard.rs,platform_macos.rs}`
- MCP processors (status list, tool call, resource read, event streams, elicitation): `app-server/src/request_processors/mcp_processor.rs`, `app-server/src/request_processors/mcp_event_stream.rs`, `app-server/src/mcp_refresh.rs`
- Skills watcher (fs-notify → `skills/changed` notifications): `app-server/src/skills_watcher.rs`
- Fs watch notifications (`fs/changed`): `app-server/src/fs_watch.rs`
- Fuzzy file search sessions: `app-server/src/fuzzy_file_search.rs`
- Account/gateway OAuth flows + account notifications: `app-server/src/{account_notifications.rs,external_auth.rs,gateway_oauth_notifications.rs}`
- Bedrock account setup wizard: `app-server-protocol/src/protocol/v2/bedrock.rs`, `app-server/src/request_processors/bedrock_auth.rs`
- Attestation generation: `app-server/src/attestation.rs`
- External agent migration (import from Claude Code / Cursor): `app-server/src/external_agent_migration/`, `external-agent-migration/src/`
- SQLite recovery of thread store: `app-server/src/daemon_thread_recovery.rs`, `cli/src/state_db_recovery.rs`

### MCP
- MCP client stack built on `rmcp` with multiple transports (stdio, streamable-http, executor-process, in-process, event-notification): `rmcp-client/src/{rmcp_client.rs,local_stdio_transport.rs,executor_process_transport.rs,in_process_transport.rs,event_notification_transport.rs,stdio_server_launcher.rs,streamable_http_retry.rs}`
- OAuth for MCP servers (authorization-code + PKCE, dynamic client registration, refresh coordination, EMA exchange): `rmcp-client/src/{oauth.rs,oauth_client_credentials.rs,oauth_client_registration.rs,perform_oauth_login.rs,oauth_callback.rs,ema_exchange.rs,ema_claims.rs}`
- Enterprise managed auth (EMA) policy: `rmcp-client/src/ema_auth_policy.rs`, `config/src/mcp_ema.rs`
- Codex-side MCP orchestration (catalog building, connection manager, tool filtering/naming, elicitation): `codex-mcp/src/{catalog.rs,connection_manager.rs,tools.rs,binding.rs,elicitation.rs,runtime.rs}`
- MCP server status snapshots & auth statuses: `codex-mcp/src/mcp/mod.rs` (exports `McpServerStatusSnapshot`, `compute_auth_statuses`)
- Codex Apps builtin MCP server (ChatGPT-hosted `/api/codex/ps/mcp`): `codex-mcp/src/mcp/mod.rs` (`codex_apps_mcp_server_config`)
- MCP config types + edits (add/remove servers from config.toml, `codex mcp` CLI): `config/src/mcp_types.rs`, `config/src/mcp_edit.rs`, `cli/src/mcp_cmd.rs`, `cli/src/mcp_login.rs`
- MCP policy requirements (enterprise constraints on which servers may run): `config/src/mcp_requirements.rs`, `protocol/src/mcp_policy.rs`
- MCP hooks (run an MCP tool as a lifecycle hook): `hooks/src/mcp.rs`, `hooks/src/engine/mcp_runner.rs`
- OpenAI Docs source attribution for app tools: `codex-mcp/src/openai_docs_source_attribution.rs`
- MCP extensions protocol types: `protocol/src/mcp.rs`, `protocol/src/mcp_approval_meta.rs`

### Code mode (JS REPL via embedded V8)
- Session protocol (cells, execute/wait/wait-to-pending outcomes): `code-mode-protocol/src/{session.rs,runtime.rs,response.rs,description.rs}`
- In-process runtime (V8 init with JIT modes, module loader, timers, audio callbacks): `code-mode-runtime/src/{service.rs,v8_init.rs,runtime/,cell_actor/,session_runtime/}`
- Standalone host process (framed client/host protocol with versioned handshake and capability negotiation): `code-mode-host/src/{lib.rs,transport.rs,peer.rs,delegate.rs,grpc/}`
- gRPC-backed session provider: `code-mode/src/grpc_session/`, `code-mode-host/src/grpc/`
- Process-owned session provider: `code-mode/src/remote_session.rs`
- V8 build info: `v8-poc/src/lib.rs`

### CLI surface
- Root command with subcommands: `cli/src/main.rs` — `agents`, `tcp-tunnel`, `exec`, `review`, `login`, `logout`, `mcp`, `plugin`, `app-server`, `remote-control`, `app`, `completion`, `doctor`, `sandbox` (debug), `debug` (models/app-server/prompt-input/trace-reduce), `execpolicy`, `apply`, `resume`, `queue`, `archive`, `delete`, `migrate-rollouts`, `unarchive`, `fork`, `cloud`, `responses-api-proxy`, `stdio-to-uds`, `exec-server`, `features`
- Non-interactive runner with human/JSONL output: `exec/src/{lib.rs,cli.rs,event_processor_with_human_output.rs,event_processor_with_jsonl_output.rs,exec_events.rs}`
- Review flow CLI: `exec/src/cli.rs` (`ReviewArgs`: `--uncommitted`, `--base`, `--commit`), protocol `app-server-protocol/src/protocol/v2/review.rs`, `prompts/src/review_request.rs`, `prompts/src/review_exit.rs`
- Cloud tasks CLI: `cloud-tasks/src/cli.rs` (exec/status/list/apply/diff), scrollable diff renderer `cloud-tasks/src/scrollable_diff.rs`
- Doctor diagnostics: `cli/src/doctor.rs` + `cli/src/doctor/{security.rs,sandbox.rs,disk.rs,network.rs,git.rs,updates.rs,thread_inventory.rs,windows_dev_drive.rs,desktop.rs,runtime.rs,system.rs}`
- Debug sandbox commands (seatbelt/bwrap arg emission for debugging): `cli/src/debug_sandbox.rs`, `cli/src/debug_sandbox/{seatbelt.rs,cloud_config.rs}`
- WSL path handling: `cli/src/wsl_paths.rs`
- Daemon install/update UX: `cli/src/daemon_install.rs`, `cli/src/daemon_telemetry.rs`
- MCP/plugin/marketplace subcommands: `cli/src/mcp_cmd.rs`, `cli/src/plugin_cmd.rs`, `cli/src/marketplace_cmd.rs`
- Queue command (queue a message onto a thread's queue): `cli/src/queue_cmd.rs`
- Apply (apply a cloud task diff locally): `chatgpt/src/apply_command.rs`
- Login flows: `cli/src/login.rs`, `login/src/{lib.rs,device_code_auth.rs,gateway_auth*.rs,pkce.rs,server.rs}`

### Plugins, skills, marketplaces
- Skill loading/parsing/mentions: `skills/src/{loading.rs,parser.rs,model.rs,mentions.rs,selection.rs,invocation.rs,name_counts.rs,interface.rs}`
- Embedded system skills installed to `CODEX_HOME/skills/.system` with fingerprint marker: `skills/src/lib.rs` (`install_system_skills`), assets `skills/src/assets/samples/` (imagegen, openai-docs, review-agent, skill-creator, skill-installer)
- Plugin manifest parsing (`.codex-plugin/plugin.json`): `plugin/src/manifest.rs`, `core-plugins/src/manifest.rs`
- Plugin/marketplace manager (install, uninstall, reconcile, search, share, remote metadata): `core-plugins/src/{manager.rs,marketplace.rs,marketplace_add.rs,marketplace_upgrade.rs,remote.rs,remote_metadata.rs,remote_mutations.rs,plugin_bundle_archive.rs,npm_source.rs,store.rs,loader.rs}`
- Curated marketplace policy: `core-plugins/src/marketplace_policy.rs`
- Plugin metrics sidecar: `core-plugins/src/{plugin_metrics.rs,plugin_metrics_sidecar.rs}`
- Skill/marketplace config editing: `config/src/{skills_config.rs,marketplace_edit.rs,plugin_edit.rs}`
- Connector runtime manager (Codex Apps tools caching/projection): `connectors/src/{connector_runtime/,snapshot.rs,runtime_projection.rs,metadata_store.rs}`
- Skills extension (host service, dynamic skill selector, aliases, cloud skills): `ext/skills/src/`

### Hooks
- Hook engine (discovery, dispatch, command runner, MCP runner, output parser): `hooks/src/engine/`
- Hook event definitions (12 events): `hooks/src/lib.rs`, `hooks/src/events/`
- Hook config schema (hooks.json + TOML, handler types command/mcp_tool/prompt/agent): `config/src/hook_config.rs`
- Hook trust hashing + state (`trusted_hash`, `enabled`): `config/src/hook_config.rs`, `hooks/src/engine/discovery.rs`
- additionalContext spill-to-disk with token threshold: `hooks/src/output_spill.rs`
- Legacy notify shim: `hooks/src/legacy_notify.rs`

### Multi-agent / collaboration
- Agent roles (TOML role files in `<config>/agents/`, roles table in config): `agent-roles/src/{loader.rs,discovery.rs,agent_role_config.rs}`
- Agent graph store (parent/child thread spawn topology): `agent-graph-store/src/{store.rs,types.rs,local.rs}`
- Agent message board (channels/threads/posts/subscribe tools, in-memory + local + remote backends): `ext/agent-message-board/src/{api.rs,tools.rs,host.rs,in_memory.rs,local/}`
- Message board client: `agent-message-board-client/src/{client.rs,protocol.rs}`
- Agent identity (ed25519 keys derived from seed, JWT/JWKS against ChatGPT backend, Curve25519): `agent-identity/src/lib.rs`
- Legacy subagent runner (fork parent thread): `ext/agent/src/lib.rs`
- Collaboration mode templates (Default/Plan): `collaboration-mode-templates/src/lib.rs` + `templates/{default.md,plan.md}`
- Multi-agent instructions prompts: `prompts/src/{multi_agent_instructions.rs,collaboration/}`
- Goals (goal set/get/clear tools, token budgets): `ext/goal/src/{api.rs,tool.rs,accounting.rs,steering.rs}`
- Queue service: `ext/queue/src/{lib.rs,service.rs}`

### Guardian (safety reviewer)
- Guardian policy loader & model policy: `config/src/guardian.rs`
- Guardian context construction (transcripts, budgets, enforcement, reviews, trusted tools/skills): `guardian-context/src/`
- Guardian reviewer (assessment schema, retry, circuit breaker, deadlines, routing of approval policy): `ext/guardian-reviewer/src/`
- Guardian v2 (async scorer + sync reviewer): `ext/guardian-v2/src/`
- Guardian instructions prompts: `prompts/src/guardian_instructions.rs`, `prompts/src/model_messages/guardian.rs`

### Extensions platform
- Extension API (contributor traits, registry builder, turn admission, session isolation, tool policy): `ext/extension-api/src/{registry.rs,contributors.rs,session_isolation.rs,tool_policy.rs,turn_admission.rs,capabilities.rs}`
- Built-in extensions: web search (`ext/web-search/`), image generation (`ext/image-generation/`), memories (`ext/memories/`), history notes (`ext/history-notes/`), goals, message board, skills (`ext/skills/`), items (`ext/items/`), git attribution (`ext/git-attribution/`), mcp extension (`ext/mcp/`), connectors (`ext/connectors/`)
- Core plugin glue (which extensions install into the registry): `core-plugins/src/provider.rs`, `plugin/src/provider.rs`
- Tools crate (tool specs, executor trait, JSON schema, tool search, dynamic tools): `tools/src/`

### Persistence & history
- Rollout recorder/files (JSONL sessions under `CODEX_HOME/sessions`), reverse scanner, compression: `rollout/src/{lib.rs,recorder.rs,search.rs,reverse_jsonl_scanner.rs,compression.rs}`
- Thread store (thread metadata, sections, attachments, search, fork, revert, archive; SQLite-backed via `codex-state`): `thread-store/src/`
- Thread store protocol for remote thread stores: `thread-store/src/types.rs`, `config/src/thread_config/`
- State DB (SQLite log db, migrations, audit): `state/src/{log_db.rs,migrations.rs,audit.rs}`
- History (shell-like command history persistence with `save-all`/`none` modes): `message-history/src/lib.rs`, `config/src/types.rs` (`History`)
- Rollout trace/replay tooling: `rollout-trace/src/` (reducer, thread, tool_dispatch, mcp, inference, bundle)
- Apply-patch parser/executor (Lark-grammar `*** Begin Patch` format): `apply-patch/src/{parser.rs,file_update.rs,invocation.rs,streaming_parser.rs,seek_sequence.rs}`

### Auth, telemetry, updates
- Login/auth manager (ChatGPT tokens, API key, device code, gateway auth, workload identity, outbound proxy support): `login/src/{auth/,manager.rs,device_code_auth.rs,gateway_auth.rs,pkce.rs}`
- Keyring-backed secret storage: `keyring-store/src/lib.rs`
- Secrets sanitization: `secrets/src/{lib.rs,sanitizer.rs}`
- Workload identity (federated token exchange): `workload-identity/src/{assertion.rs,exchange.rs}`
- AWS auth (Bedrock): `aws-auth/src/{discovery.rs,signing.rs,transport.rs}`
- OTEL provider (Statsig OTLP/HTTP metrics endpoint by default in release builds): `otel/src/{config.rs,provider.rs,targets.rs}`
- OTEL trace over websocket: `otel-trace-websocket/src/lib.rs`
- Analytics events client (capture-file support, reducer, facts): `analytics/src/`
- Feedback capture + report upload (redacted doctor report attachment): `feedback/src/{lib.rs,report_upload.rs,feedback_diagnostics.rs}`
- Daemon auto-update loop (installer script from `https://chatgpt.com/codex/install.sh|ps1`, pinned installs, updater re-exec): `app-server-daemon/src/update_loop.rs`
- Cloud config bundle (managed config from ChatGPT backend, caching, validation): `cloud-config/src/{bundle_loader.rs,cache.rs,service.rs,validation.rs}`
- Responses API proxy (local reverse proxy for debugging, dumps requests): `responses-api-proxy/src/{main.rs,lib.rs,dump.rs}`

### Voice / realtime
- Realtime WebRTC session (SDP exchange, audio frame protocol, ALSA on Linux): `realtime-webrtc/src/{lib.rs,protocol.rs,session.rs,client.rs,linux_alsa.rs}`
- Voice host (audio capture/playout devices, playout, capture worker): `voice-host/src/`
- Realtime thread RPC methods (start/appendAudio/appendSpeech/appendText/sdp/transcript deltas): `app-server-protocol/src/protocol/v2/realtime.rs`
- Browser computer-use / in-app browser requirements: `config/src/{browser_use.rs,computer_use.rs,in_app_browser_requirements.rs,browser_computer_use_requirements.rs}`

### Misc utility crates
- Managed git worktrees (Desktop contract, thread binding): `worktree/src/lib.rs`
- Feature flags registry (154 `FeatureSpec`s with lifecycle stages): `features/src/lib.rs`
- Mermaid diagram rendering: `mermaid/src/lib.rs`
- ANSI escape parsing: `ansi-escape/src/lib.rs`
- Stdio↔UDS bridge: `stdio-to-uds/src/lib.rs`
- TCP tunnel over HTTP/3 CONNECT (quinn): `tcp-tunnel/src/lib.rs`
- UDS helpers (daemon directory security, Windows peer validation): `uds/src/`
- Websocket auth policy: `websocket-auth/src/lib.rs`
- Ollama/LM Studio local provider clients: `ollama/src/`, `lmstudio/src/`
- Model provider info & catalog: `model-provider-info/src/lib.rs`, `models-manager/src/manager.rs`
- Build info / install context: `build-info/src/lib.rs`, `install-context/src/lib.rs`
- Shell command parsing (bash/powershell tokenization, dangerous-command detection): `shell-command/src/{parse_command.rs,bash.rs,powershell.rs,command_safety/,shell_detect.rs}`
- Shell snapshots (capture env/exports/credentials of user shell for model env): `shell-command/src/shell_snapshot*.rs`
- File search CLI (`codex-file-search` binary): `file-search/src/main.rs`
- Connection/attachment stores: `attachment-store/src/`, `context-fragments/src/`

---

## 2. HOW IT WORKS

### 2.1 Permission profiles → platform sandbox
The central abstraction is `PermissionProfile` (`protocol/src/models.rs:422`): `Managed { file_system, network }`, `Disabled`, or `External { network }` — with built-in ids `:read-only`, `:workspace`, `:danger-full-access` (`protocol/src/models.rs:410-416`). `SandboxManager::transform` (`sandboxing/src/manager.rs`) consumes a `SandboxCommand` (program/args/cwd/env/managed-network/additional permissions) plus a `SandboxTransformRequest` and produces a host-native `SandboxExecRequest`. `get_platform_sandbox` maps OS → `SandboxType` (`protocol/src/sandbox.rs:10`: None/MacosSeatbelt/LinuxSeccomp/WindowsRestrictedToken/WindowsMxc). `policy_transforms.rs` normalizes additional permissions (glob entries are only valid as deny-read; dedup; validate path conventions match the executor cwd) and merges base + per-call permission profiles. Sandbox violations from child stderr are parsed and re-classified (`sandboxing/src/violation.rs`, `denial.rs`) so the model can be told "write outside workspace" rather than seeing a raw OS error.

### 2.2 Linux sandbox: bubblewrap + seccomp helper
`codex-linux-sandbox` is a separate argv0 of the same binary (`arg0/src/lib.rs` symlink farm in a locked temp PATH entry). The parent builds args via `create_linux_sandbox_command_args_for_permission_profile` (`sandboxing/src/landlock.rs`): `--sandbox-policy-cwd`, `--command-cwd`, `--permission-profile <JSON>`, optional `--use-legacy-landlock`, optional `--managed-network <JSON>`, then `--` and the command. The helper (`linux-sandbox/src/linux_run_main.rs`) execs bwrap: system bwrap if present with capability probe (`--ro-bind-fd` support, argv0 support probed; `linux-sandbox/src/launcher.rs`), otherwise a bundled bwrap binary. Filesystem restrictions are bwrap bind mounts (`fd_mount.rs` passes mounts as FDs to avoid TOCTOU); network isolation is a seccomp filter installed on the child thread only (`apply_permission_profile_to_current_thread` in `linux-sandbox/src/landlock.rs`) — socket(2) is denied except in proxy-routed mode where only the proxy port is reachable; WSL2 interop VM sockets are additionally denied because they can launch host processes. `PR_SET_NO_NEW_PRIVS` is only set when seccomp/landlock is actually needed, because setuid bwrap deployments break otherwise. Managed networking forces an isolated netns with the proxy as the only route.

### 2.3 macOS Seatbelt
`create_seatbelt_command_args` (`sandboxing/src/seatbelt.rs:871`) assembles an `sbpl` program from four `include_str!` policy files plus generated rules: writable-root `(allow file-write* (subpath ...))` grants, read grants, protected ancestors (deny `file-write-unlink` on every ancestor of read-only carveouts so a parent rename can't move the protected subtree out from under the rules), scratch dir access, and loopback proxy port allowlists parsed from `HTTP_PROXY` env (`proxy_loopback_ports_from_env`). Only `/usr/bin/sandbox-exec` is accepted, explicitly to defeat PATH-injected trojans (`MACOS_PATH_TO_SEATBELT_EXECUTABLE`, seatbelt.rs:57). The base policy is closed-by-default `(deny default)` with an explicit sysctl/mach-lookup allowlist modeled on Chrome's sandbox policy (referenced in comments in `seatbelt_base_policy.sbpl`).

### 2.4 Network policy proxy
`network-proxy` runs an HTTP proxy (default `127.0.0.1:3128`) and a SOCKS5 proxy (default `127.0.0.1:8081`) — from `network-proxy/README.md`, which documents the whole config shape. Policy decisions are host-based with scoped wildcards (`*.openai.com`, `**.openai.com`; bare `*` rejected) with precedence `None < Allow < Deny` encoded in the enum ordering (`network-proxy/src/config.rs:25`). `mode = "limited"` (read-only network) automatically enables HTTPS MITM with a CA whose private key stays in proxy memory; spawned commands get `CUSTOM_CA_ENV_KEYS` pointing at immutable public bundles under `$CODEX_HOME/proxy/` so common clients trust it. MITM hooks (`mitm_hook.rs`) can match host/method/path-prefix and apply actions like stripping `authorization` headers — e.g. allowing GitHub PR reads but stripping credentials on writes. On Linux the sandboxed process's only network egress is to the proxy; attribution back to the spawning environment uses a magic-framed token (`\0CDXPXY1`, `attribution.rs`) or, on Windows, `GetExtendedTcpTable` PID lookup plus token SID inspection (`windows_tcp_attribution.rs`).

### 2.5 Exec server & Noise relay
The exec server is the remote-executor protocol used by Codex Cloud and the app-server. Wire is JSON-RPC without the `"jsonrpc"` field, with `RequestId` string-or-int, and a 256K-node deserialization budget to stop "billion laughs"-style expansion (`exec-server-protocol/src/rpc.rs:23`). Methods: `initialize`/`initialized`, `process/{start,read,write,signal,terminate,output,exited,closed}`, `fs/{readFile,open,readBlock,close,writeFile,createDirectory,getMetadata,canonicalize,copy,remove,readDirectory,walk}`, `environment/{info,status}`, and `capabilities/discoverV2` (`exec-server-protocol/src/protocol.rs:22-41`, `capabilities.rs`). Remote executors register with a registry and relay JSON-RPC over a WebSocket rendezvous, but the relay never sees plaintext: both sides run a hybrid Noise IK handshake — `Noise_hybridIK_X25519+MLKEM768_AESGCM_SHA256` via the `clatter` crate, prologue domain `codex-exec-server-relay-noise/v1` (`exec-server/src/noise_channel.rs`) — pinned to registry-provided static keys; AES-GCM records are sequenced and reordered before decryption (`noise_relay/ordered_ciphertext.rs`). This is post-quantum key agreement on a dev-tool control plane, which is unusually thorough.

### 2.6 App server protocol
The app server exposes ~200 JSON-RPC methods enumerated in `app-server-protocol/src/protocol/common.rs` (a macro table mapping `ThreadStart => "thread/start"` etc.). Transports: `stdio://` (default), `unix://[PATH]`, `ws://IP:PORT`, `off` (`app-server-transport/src/transport/mod.rs:81-86,120`). Experimental fields/methods are declared with `#[experimental("thread/start.permissions")]` and tracked by a derive macro (`codex-experimental-api-macros/src/lib.rs`) that produces the experimental-API registry used for gating and for the generated docs (`app-server-protocol/src/export.rs`). Threads are first-class: `thread/{start,resume,fork,revert,archive,delete,search,compact/start,...}`, turns are `turn/{start,steer,interrupt}`, and live model output arrives as `item/*` deltas (agentMessage, reasoning, fileChange patches, commandExecution output) plus `turn/diff/updated`. The `codex exec` binary drives this whole protocol through an in-process client (`app-server-client/src/lib.rs`), so headless and IDE surfaces share one server.

### 2.7 MCP integration
Servers are configured as `[mcp_servers.<name>]` with flattened transport (`Stdio { command, args, env, env_vars, cwd }` or `StreamableHttp { url, bearer_token_env_var, http_headers, ... }`) plus `auth = "oauth" | "chatgpt" | "ema_auth"`, `enabled`, `required`, `startup_readiness`, `supports_parallel_tool_calls`, `tool_input_schema_max_bytes` (default 5,000), `omit_tools_from` (`config/src/mcp_types.rs:233-273,624`). At runtime `EffectiveMcpServer` (`codex-mcp/src/server.rs`) wraps config with a credential policy: `HostFallbackAllowed` for host-configured servers vs `ExecutorOnly` for executor-discovered servers, so a guest environment can never promote its config into host credential lookup. Tool names are sanitized to Responses-API constraints and deduplicated with hash suffixes when two raw servers collide (`codex-mcp/src/tools.rs:105-199`). The builtin `codex_apps` server points at `<chatgpt_base>/api/codex/ps/mcp` with a `X-OpenAI-Product-Sku` header (`codex-mcp/src/mcp/mod.rs:623-659`). Auto-approval of MCP permission prompts is decided in `mcp_permission_prompt_is_auto_approved` (policy `Never` + full-disk-write profile → approve). OAuth flows live in `rmcp-client` (PKCE callback server on a fixed port, dynamic client registration, refresh-mode coordination, EMA = enterprise IdP refresh-token exchange with no fallback).

### 2.8 Hooks
Hooks fire on 12 events (`PreToolUse`, `PermissionRequest`, `PostToolUse`, `PreCompact`, `PostCompact`, `SessionStart`, `SessionEnd`, `UserPromptSubmit`, `SubagentStart`, `SubagentStop`, `Stop`, `Interrupt` — `hooks/src/lib.rs`). Sources are layered like config: each config layer can contribute `hooks.json` (Claude-style) or `[hooks]` TOML, plus plugin-declared hooks; discovery dedupes JSON/TOML dual definitions with a warning (`hooks/src/engine/discovery.rs:120-180`). Handlers are `command` (with `commandWindows`, timeout, `async`, `statusMessage`, `additionalContextLimit`), `mcp_tool` (server+tool+input), `prompt`, or `agent` (`config/src/hook_config.rs:154-199`). Trust is enforced by hashing handler definitions into `hooks.state.<name>.trusted_hash`; changed handlers need re-trust unless `--dangerously-bypass-hook-trust`. Handlers returning `additionalContext` are injected into the turn; large contexts spill to disk past the token threshold (default 2,500; `hooks/src/output_spill.rs`).

### 2.9 Skills
A skill is a directory with `SKILL.md` (YAML frontmatter: `name`, `description`, `short-description`; optional `metadata` and richer policy/interface fields parsed in `skills/src/model.rs`: `allow_implicit_invocation`, `products` restriction, icon/brand-color/default_prompt interface, and tool dependencies incl. MCP servers with `oauth_callback_port`). Selection supports explicit `@skill` mentions (`mentions.rs`, `selection.rs`) and implicit invocation detection gated by policy (`invocation.rs`). Bundled system skills (`skills/src/assets/samples/`: imagegen, openai-docs, review-agent, skill-creator, skill-installer) are embedded with `include_dir!` and installed into `CODEX_HOME/skills/.system`, skipped when a marker file fingerprint matches (`skills/src/lib.rs:74`). Skills catalog size is capped by `skills.max_context_tokens` (default 2% of context window, max 10,000 — `config/src/skills_config.rs:40-43`).

### 2.10 Code mode (JS REPL)
Code mode runs model-written JS in an embedded V8 with a cell-based protocol: `ExecuteRequest`/`StartedCell`/`WaitRequest`/`WaitOutcome`/`ExecuteToPendingOutcome` (`code-mode-protocol/src/runtime.rs`). A "cell" can yield control back (`YIELD_OBSERVATION_CAPABILITY` negotiated at host handshake, `code-mode-host/src/lib.rs`) so long-running loops don't block the turn; there are explicit cell execution limits (`max_heap_size_bytes` etc., `code-mode-protocol/src/session.rs:28`) and a grace period before forced yield (`code-mode-runtime/src/service.rs`). The runtime can live in-process or in a separate host process (`ProcessOwnedCodeModeSessionProvider`, `code-mode/src/remote_session.rs`) or behind gRPC; the host protocol is a framed binary protocol with versioned hello/capability negotiation (`code-mode-host/src/transport.rs`). V8 is initialized with configurable JIT mode (`code-mode-runtime/src/v8_init.rs`).

### 2.11 Guardian review
Guardian is an independent reviewer model over the main thread. `guardian-context/src/` builds the review input: action description, authorization state, token budgets, transcript slices (with truncation), retained instructions, and trusted-tool/skill exemptions. `ext/guardian-reviewer/src/` executes reviews with `MAX_REVIEW_ATTEMPTS = 3`, a rejection circuit breaker (`circuit_breaker.rs`), deadlines, and routing that maps the session's approval policy to whether guardian approval is required (`routing.rs`). Assessment output is a strict JSON schema (`assessment.rs` exposes `guardian_output_schema` + `guardian_output_contract_prompt`). Guardian v2 (`ext/guardian-v2/src/`) adds an async scorer that samples observations outside the critical path and a sync reviewer for approval-blocking decisions.

### 2.12 Feature flags & config layering
`features/src/lib.rs` is a registry of 154 `FeatureSpec { id, key, stage, default_enabled }` where stage is `UnderDevelopment | Experimental { name, menu_description, announcement } | Stable | Deprecated | Removed`. Config is layered (documented in `config/src/loader/README.md`) low→high: embedded packaged defaults (`config/defaults.toml`, `include_str!`-ed), System (`/etc/codex/config.toml` or Windows equivalent), EnterpriseManaged cloud bundle, User `config.toml`, user profile config, Project `.codex/config.toml`, session flags (CLI `-c key=value` dotted-path writes), legacy managed file, MDM managed config. The loader produces not just the merged TOML but per-key origins and per-layer fingerprints (`fingerprint.rs`) for optimistic concurrency in `config/value/write`. `strict_config` (`--strict-config`) fails on unknown keys.

### 2.13 Daemon & updates
`codex app-server daemon` manages a background app-server: pid files (`daemon.pid`, `daemon-updater.pid`), an operation lock, settings JSON with `remoteControlEnabled`, `featureOverrides`, updater settings `autoUpdateEnabled` (default true) / `updateIntervalMinutes` (default 60) / `shutdownGraceSeconds` (default 60, max 300) (`app-server-daemon/src/settings.rs`). The update loop (`update_loop.rs`) waits 5 minutes at startup, then periodically fetches `https://chatgpt.com/codex/install.sh` (or `install.ps1`), runs it against a package root, and re-execs itself as the new updater with the old binary's identity checked (`ExecutableIdentity`, `reexec_managed_updater`). A CLI-initiated daemon install pins the selected package and prints an explicit confirmation (`cli/src/daemon_install.rs`); `daemon update` returns to production updates. Thread state is saved on managed shutdown and recovered (`daemon_thread_recovery.rs`, `daemon_recovery_file_path`).

### 2.14 Remote control (phone/web control of a session)
The daemon optionally exposes the app-server to paired remote clients: it enrolls with a remote-control server (`EnrollRemoteServerRequest { name, os, arch, app_server_version, installation_id }` → server_id/environment_id/refresh token), then maintains an authenticated websocket (`app-server-transport/src/transport/remote_control/{server_api.rs,websocket.rs,controller.rs}`). Pairing produces a `pairing_code` and optional `manual_pairing_code` (`protocol.rs:46-53`); clients are listed/revoked via `remoteControl/client/{list,revoke}`. Client connections are tracked and segmented per stream (`segment.rs`, `client_tracker.rs`).

### 2.15 External agent migration
`external-agent-migration/src/` imports Claude Code (`.claude`, `source/cla.rs`) and Cursor (`.cursor`, `.cursorrules`, `source/cur.rs`) configuration: MCP servers, memory files (`memory_import.rs`), hooks (`hooks_cla.rs`), plugins, and session transcripts (`sessions/records_cla.rs` converts Claude session JSONL to Codex rollouts with an import ledger so re-imports are idempotent). It's surfaced over app-server as `externalAgentConfig/{detect,import,...}` with progress notifications (`app-server/src/external_agent_migration/protocol.rs`).

---

## 3. CONFIG

### config.toml keys (verified fields of `ConfigToml`, `config/src/config_toml.rs:167-520`)
`model`, `review_model`, `model_provider`, `model_context_window`, `model_auto_compact_token_limit`, `model_auto_compact_token_limit_scope`, `model_post_turn_compact_threshold_percent`, `approval_policy`, `approvals_reviewer`, `auto_review`, `browser_use`, `computer_use`, `shell_environment_policy`, `allow_login_shell`, `sandbox_mode` (`read-only`|`workspace-write`|`danger-full-access`, default read-only — `protocol/src/config_types.rs:104`), `allow_symlinked_codex_home`, `sandbox_workspace_write`, `default_permissions`, `permissions` (profile table), `notify`, `instructions`, `developer_instructions`, `include_permissions_instructions`, `include_apps_instructions`, `include_collaboration_mode_instructions`, `include_environment_context`, `model_instructions_file`, `compact_prompt`, `forced_chatgpt_workspace_id`, `forced_login_method`, `cli_auth_credentials_store`, `mcp_servers`, `mcp_enterprise_managed_auth`, `mcp_oauth_credentials_store` (`auto` default), `mcp_oauth_callback_port`, `mcp_optional_startup_grace_ms`, `model_providers`, `project_doc_max_bytes` (default 32768), `project_doc_fallback_filenames`, `tool_output_token_limit`, `background_terminal_max_timeout` (default 300000 ms), `thread_unload_delay_secs`, `js_repl_node_path`, `js_repl_node_module_dirs`, `profile`, `profiles`, `history` (`persistence = save-all` default, `max_bytes`), `sqlite_home`, `log_dir`, `file_opener` (default `vscode`), `tui`, `hide_agent_reasoning` (default false), `show_raw_agent_reasoning`, `model_reasoning_effort`, `plan_mode_reasoning_effort`, `model_reasoning_summary`, `model_verbosity`, `model_catalog_json`, `personality`, `service_tier`, `chatgpt_base_url` (default `https://chatgpt.com/backend-api/`), `apps_mcp_product_sku` (default `"codex"`), `responses_api_metadata`, `orchestrator`, `cloud`, `openai_base_url`, `audio`, `experimental_realtime_*`, `realtime`, `experimental_thread_store_endpoint`, `experimental_thread_store`, `projects`, `web_search`, `tools`, `tool_suggest`, `agents` (see below), `goals` (`max_goal_token_budget`), `memories`, `skills`, `hooks`, `plugins`, `marketplaces`, `features`, `suppress_unstable_features_warning`, `ghost_snapshot` (compat-only), `project_root_markers` (default `[".git"]`), `analytics.enabled`, `feedback.enabled`.

### Agents config (`config/src/config_toml.rs:723-752`)
`agents.enabled` (default true), `max_concurrent_threads_per_session` (alias `max_threads`), `max_depth`, `default_subagent_model`, `default_subagent_reasoning_effort`, `interrupt_message` (default true), `job_max_runtime_seconds` (removed no-op), and `[agents.<role>]` with `description`, `config_file`, `nickname_candidates`.

### Permissions profiles (`config/src/permissions_toml.rs`)
`default_permissions` selects a profile id (`:read-only`, `:workspace`, `:danger-full-access`, or user-defined). `[permissions.<id>]`: `description`, `extends` (cycle-checked, `Cycle` error lists the chain), `workspace_roots` (`"path" = true/false`), `filesystem` (flattened `"path" = "read"|"write"|"deny"` or scoped map, plus `glob_scan_max_depth`), `network` (`enabled`, `proxy_url`, `socks_url`, `enable_socks5`, `enable_socks5_udp`, `allow_upstream_proxy`, `dangerously_allow_non_loopback_proxy` (default false), `dangerously_allow_all_unix_sockets` (default false, macOS), `mode` = `full`|`limited`, `domains` map (`"host-pattern" = "allow"|"deny"|"none"`), `unix_sockets` map, `allow_local_binding` (default false except MXC; MXC rejects effective false), `mitm` + `mitm.hooks` + `mitm.actions`). Conflict precedence: `deny` > `write` > `read` (`protocol/src/permissions.rs:98-102`).

### Hooks (`config/src/hook_config.rs`)
`[hooks]` or per-layer `hooks.json`; `[hooks.state.<name>]` with `enabled`, `trusted_hash`. Handler fields: `command`, `commandWindows`, `timeout` (sec), `async`, `statusMessage`, `additionalContextLimit` (default 2500 tokens, 0 disables); `mcp_tool`: `server`, `tool`, `input`, `timeout`, `statusMessage`; plus `prompt` and `agent` types.

### MCP (`config/src/mcp_types.rs`)
`[mcp_servers.<name>]`: transport (`command`/`args`/`env`/`env_vars`/`cwd` or `url`/`bearer_token_env_var`/`http_headers`/env-sourced headers), `auth` (`oauth`|`chatgpt`|`ema_auth`), `environment_id`, `enabled` (default true), `required`, `startup_readiness` (`live`|`catalog`), `supports_parallel_tool_calls`, `tool_input_schema_max_bytes` (default 5000), `omit_tools_from`; OAuth sub-config `client_id`, `client_secret`, `callback_url`, `callback_port`, `authorization_server_issuer`.

### Feature flags
`[features]` table keyed by the 154 keys in `features/src/lib.rs` (e.g. `network_proxy`, `code_mode`, `guardian_v2`, `multi_agent_v2`, `shell_snapshot`, `plugins`, `codex_hooks`, `use_xaa`, `mcp_oauth_refresh_coordination`, `sleep_tool`, `steer`, `worktrees`, `windows_sandbox`...). Also `codex features enable|disable` CLI (`cli/src/main.rs:992-994`) and daemon `featureOverrides` (`app-server-daemon/src/settings.rs`).

### CLI flags (shared, `utils/cli/src/shared_options.rs`, `utils/cli/src/config_override.rs`, `exec/src/cli.rs`)
`-c/--config key=value` (TOML-parsed dotted-path overrides, global), `-m/--model`, `--oss`, `--local-provider`, `-p/--profile`, `-s/--sandbox`, `--auto-review`, `--dangerously-bypass-approvals-and-sandbox`, `--dangerously-bypass-hook-trust`, `--cwd`, `--worktree`, `--add-dir`, `--image/-i`, `--thread-source`, `--skip-git-repo-check`, `--ephemeral`, `--ignore-user-config`, `--ignore-rules`, `--output-schema`, `--color`, `--json`, `--last-message-file`, `--strict-config`. `codex exec` adds `resume|fork|review` subcommands; review: `--uncommitted`, `--base BRANCH`, `--commit SHA`. App-server binary: `--listen URL` (default `stdio://`), `--session-source` (default `vscode`), `--strict-config`, `--remote-control`, `--managed-daemon`, websocket-auth args. Daemon settings JSON: `remoteControlEnabled`, `featureOverrides`, `updater.autoUpdateEnabled` (default true), `updater.updateIntervalMinutes` (default 60), `shutdownGraceSeconds` (default 60).

### Environment variables (verified `CODEX_*` literals in scope)
`CODEX_HOME`, `CODEX_API_KEY` / `OPENAI_API_KEY` (`login/src/auth/manager.rs:953-954`), `CODEX_ACCESS_TOKEN`, `CODEX_BASE_URL`, `CODEX_BUILD_COMMIT`, `CODEX_SQLITE_HOME`, `CODEX_SANDBOX` (`seatbelt` value checked in `cli/src/doctor/network.rs:124`; set by spawn code as `CODEX_SANDBOX_ENV_VAR`), `CODEX_SANDBOX_NETWORK_DISABLED`, `CODEX_LINUX_SANDBOX_EXE` / `CODEX_TEST_LINUX_SANDBOX_EXE`, `CODEX_LINUX_SANDBOX_PID_NAMESPACE`, `CODEX_ESCALATE_SOCKET` (escalation socket FD env), `EXEC_WRAPPER`, `CODEX_ARG0_EXEC_HELPER_ARG1`, `CODEX_FS_HELPER_ARG1`, `CODEX_WINDOWS_MXC_ARG1`, `CODEX_WINDOWS_SANDBOX_PROXY_PORTS`, `CODEX_WINDOWS_SANDBOX_PACKAGE_FAMILY`, `CODEX_WINDOWS_REGISTERED_CORE`, `CODEX_APPLY_PATCH_PRESERVE_LINE_ENDINGS`, `CODEX_NETWORK_PROXY_ACTIVE`, `CODEX_NETWORK_PROXY_ATTRIBUTION`, `CODEX_NETWORK_PROXY_BROKERED_CREDENTIALS`, `CODEX_NETWORK_PROXY_CREDENTIAL_BROKER_ACTIVE`, `CODEX_NETWORK_ALLOW_LOCAL_BINDING`, `CODEX_NETWORK_POLICY_VIOLATION`, `CODEX_EXEC_SERVER_URL`, `CODEX_EXEC_SERVER_EXIT_ON_STDIN_CLOSE`, `CODEX_EXEC_SERVER_NOISE_REGISTRY_URL`, `CODEX_EXEC_SERVER_NOISE_AUTH_TOKEN`, `CODEX_EXEC_SERVER_NOISE_ENVIRONMENT_ID`, `CODEX_EXEC_SERVER_NOISE_CHATGPT_ACCOUNT_ID`, `CODEX_EXEC_SERVER_PROXY_PRIVATE_IPS_VIA_UPSTREAM`, `CODEX_REMOTE_TOKEN`, `CODEX_REMOTE_AUTH_TOKEN`, `CODEX_MANAGED_BY_NPM|PNPM|BUN|VITE_PLUS`, `CODEX_MANAGED_PACKAGE_ROOT`, `CODEX_MANAGED_CONFIG_SYSTEM_PATH`, `CODEX_APP_SERVER_MANAGED_CONFIG_PATH`, `CODEX_APP_SERVER_DISABLE_MANAGED_CONFIG`, `CODEX_APP_SERVER_LOGIN_CLIENT_ID`, `CODEX_APP_SERVER_LOGIN_ISSUER`, `CODEX_APP_SERVER_DEV_OPEN_APP_URL`, `CODEX_OPEN_APP_URL`, `CODEX_AGENT_IDENTITY_AUTHAPI_BASE_URL`, `CODEX_AGENT_IDENTITY_JWKS_BASE_URL`, `CODEX_AUTHAPI_BASE_URL`, `CODEX_REFRESH_TOKEN_URL_OVERRIDE`, `CODEX_REVOKE_TOKEN_URL_OVERRIDE`, `CODEX_INTERNAL_ORIGINATOR_OVERRIDE`, `CODEX_CONNECTORS_TOKEN`, `CODEX_APPS_MCP_SERVER_NAME`, `CODEX_APPS_META_KEY`, `CODEX_CLOUD_TASKS_BASE_URL` (must be trusted HTTPS :443), `CODEX_CLOUD_TASKS_MODE`, `CODEX_CLOUD_TASKS_FORCE_INTERNAL`, `CODEX_ANALYTICS_EVENTS_CAPTURE_FILE`, `CODEX_PLUGIN_METRICS_OUTPUT`, `CODEX_GITHUB_TOKEN`, `CODEX_CA_CERTIFICATE`, `CODEX_DOCTOR_DISABLED_MCP_TOKEN`, `CODEX_PROXY_GIT_SSH_COMMAND` (+marker/prefix/suffix), `CODEX_TUI_DISABLE_KEYBOARD_ENHANCEMENT`, `CODEX_TUI_ROUNDED`. Network proxy default ports: HTTP 3128 (Windows preferred range 3128-3159), SOCKS5 8081 (Windows 8081-8112) — `network-proxy/README.md`.

Not verified: defaults for `tool_output_token_limit`, `thread_unload_delay_secs`, or most `[tui]`/`[realtime]` subkeys; they live in `core/src/config/mod.rs` (out of scope).

---

## 4. NOTABLE

- **Post-quantum Noise on the executor relay** (`exec-server/src/noise_channel.rs`): hybrid IK with X25519 **and** ML-KEM-768, AES-GCM records with explicit sequence numbers and reorder-before-decrypt, a suite-tagged public-key struct to prevent cross-protocol key confusion, and a design where the relay routes frames but never sees plaintext or authenticates the executor. Worth adopting anywhere a control plane crosses untrusted networks.
- **Ancestor-protection rules for read-only carveouts** (`sandboxing/src/seatbelt.rs:936-960`, `seatbelt_daemon.rs`): every ancestor of a writable-root's read-only subpath gets a `deny file-write-unlink` rule, because renaming a parent directory would relocate the protected subtree outside all pathname rules. This is a subtle sandbox-escape class most implementations miss.
- **Exec-policy `match`/`not_match` self-tests** (`execpolicy/README.md`, `execpolicy/src/policy.rs`): every prefix rule can carry example invocations that must (not) match, validated at policy-load time — policy files carry their own unit tests, and `justification` is surfaced in approval prompts ("Use `jj` instead of `git`").
- **Load-time validation everywhere with typed diagnostics**: config errors carry `TextRange`/`TextPosition` and are rendered with source context (`config/src/diagnostics.rs`, `format_config_error_with_source`), and the exec-policy parser produces the same.
- **Experimental-API derive macro** (`codex-experimental-api-macros/src/lib.rs`): `#[experimental("thread/start.permissions")]` on protocol fields generates a registry of experimental surface, so gating, docs, and TS/JSON-schema generation can't drift from the type definitions.
- **Bounded JSON deserialization with a node budget** (`exec-server-protocol/src/rpc.rs:23`): `MAX_JSONRPC_VALUE_NODES = 256K` blocks compact-array expansion attacks on a protocol whose largest legitimate message is a 50,000-entry `fs/walk`.
- **System-skill fingerprint marker** (`skills/src/lib.rs:74-99`): embedded skills are written to disk once, with a marker containing a fingerprint of the embedded directory; identical fingerprints skip the reinstall entirely — cheap idempotent asset materialization.
- **bwrap capability probing instead of version sniffing** (`linux-sandbox/src/launcher.rs`): the launcher runs a 500 ms probe of system bwrap to learn whether `--ro-bind-fd` and `--aspid-1` are supported, and transparently translates legacy fd mounts for old bwrap. Feature-detect, don't version-gate.
- **Shell snapshot capture** (`shell-command/src/shell_snapshot*.rs`, ~4,600 lines incl. tests): instead of guessing the user's environment, Codex spawns their login shell, parses exports/credentials/literals out of it, and renders a sanitized snapshot for the model — including credential redaction (`shell_snapshot_credentials.rs`).
- **Per-thread seccomp application** (`linux-sandbox/src/landlock.rs:36`): sandbox primitives are applied to the child thread only, not the whole CLI process, so the parent keeps normal capabilities; and `PR_SET_NO_NEW_PRIVS` is deferred until actually needed because setuid bwrap breaks under it.
- **Config layering with per-key origins and per-layer fingerprints** (`config/src/loader/README.md`, `fingerprint.rs`): the loader returns the merged TOML *and* which layer won each key, plus stable layer fingerprints for optimistic-concurrency writes (`config/value/write`) — enabling the app-server to edit config safely under a UI.
- **Daemon updater identity checks** (`app-server-daemon/src/update_loop.rs`): the updater verifies its own `ExecutableIdentity` before and after re-exec, handles "installer mid-swap" races by retrying instead of crashing, and separates pinned CLI installs from production-channel updates.
- **Arg0 multi-tool dispatch with a locked PATH entry** (`arg0/src/lib.rs`): one binary serves as `codex`, `apply_patch`, `codex-linux-sandbox`, `codex-fs-helper`, `codex-execve-wrapper`, and the Windows sandbox helper via argv[0] symlinks in a per-session locked temp PATH directory — with an explicit rule that injected env vars prefixed `CODEX_` are rejected (line 302).
- **Pre-main process hardening** (`process-hardening/src/lib.rs`): invoked via `#[ctor::ctor]` before `main` — core dumps disabled, ptrace attach denied, `LD_PRELOAD`/`DYLD_*` stripped — so secrets-bearing CLI processes resist inspection even during early startup.
- **Windows TCP attribution** (`network-proxy/src/windows_tcp_attribution.rs`): on Windows, where processes can't be forced through an env-pointed proxy the way a sandboxed Linux child can, the proxy attributes connections by walking `GetExtendedTcpTable` to the owning PID and inspecting the restricted-SID groups of that process's token — policy enforcement survives clients that ignore proxy env vars.
- **`fs/walk` over a framed file protocol** (`exec-server/src/sandboxed_file_system.rs`, `exec-server-protocol/src/protocol.rs`): directory walks stream through the same sandboxed fs abstraction used for reads/writes, so a remote executor's filesystem view is identical whether the executor is local, remote, or sandboxed.
