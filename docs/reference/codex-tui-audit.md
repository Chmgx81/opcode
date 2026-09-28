# Codex CLI TUI (codex-rs/tui/src) — Audit Report

**Coverage method & honesty note.** The crate is a heavily extended fork: **1,037 `.rs` files (~93k lines in top-level files alone, 23 MB)**. I read the core architecture files substantially (`lib.rs`, `tui.rs`, `app.rs`, `app_event.rs`, `chatwidget.rs`, `bottom_pane/mod.rs`, `keymap.rs`, `transcript_view.rs`, `insert_history.rs`, `streaming/controller.rs`, `diff_render.rs`, `render/highlight.rs`, `terminal_probe.rs`, `cli.rs`, `local_settings.rs`, `style.rs`, and ~45 more), extracted the full `AppEvent` variant list, the complete default keymap, all module doc-comments (`//!`) for the major modules, and grepped every `env::var` in the tree. Items below marked **(doc-verified)** had their module header read but internals not fully read; items listed in the inventory from the file listing alone are the file's name/role as evidenced by module docs where present. Test files (`*_tests.rs`, `tests/`, `snapshots/`) are excluded from the inventory. All paths are relative to `codex-rs/tui/src/`.

---

## 1. INVENTORY

### Entry, startup, onboarding
- **CLI entry & arg0 dispatch** — `main.rs` (clap `TopCli`, exit-message rendering, `ExitReason::Fatal` → exit code 1)
- **Library entry / `run_main`** — `lib.rs` (config bootstrap, auth, state DB, `run_main` at line 1086; startup future is `Box::pin`ned to keep it off the stack)
- **CLI flags** — `cli.rs` (`--no-alt-screen`, `--no-daemon`, `-a/--ask-for-approval`, `--search`, `--strict-config`, positional `PROMPT`)
- **Startup orchestration** — `startup_orchestration.rs` (main event loop host; the `select!` loop lives in `app/startup.rs:1094-1180`)
- **Startup preflight / recovery / presentation** — `startup_preflight.rs`, `startup_recovery.rs`, `startup_presentation.rs`, `startup_hooks_review.rs`, `startup_error.rs`, `startup_draft.rs` (+ `startup_draft_layout.rs`, `startup_draft_input.rs`, `startup_draft_submission`)
- **Onboarding screens** — `onboarding/onboarding_screen.rs` (first-run sign-in), `onboarding/welcome.rs` (animated logo stage), `onboarding/auth.rs` + `onboarding/auth/headless_chatgpt_login.rs` (ChatGPT login flow), `onboarding/bedrock.rs` (AWS Bedrock onboarding), `onboarding/keys.rs`, `onboarding/directory_trust.rs` + `onboarding/trust_directory.rs` (cwd trust prompt)
- **OSS provider selection** — `oss_selection.rs`; **auth helpers** — `local_chatgpt_auth.rs`

### Core state & event model
- **`App` god-object** — `app.rs` (line 524; owns transcript cells, overlay, keymap, threads, reconnect, recap, feedback, worktrees, rate limits, pets)
- **`AppEvent` enum (~600 variants)** — `app_event.rs` (the internal wire between I/O workers and the UI loop)
- **Event sender handle** — `app_event_sender.rs`
- **Exhaustive event dispatcher** — `app/event_dispatch.rs` (`App::handle_event`, incl. offline-event quarantine list)
- **TUI event routing** — `app.rs` `handle_tui_event` (line 832) + `app/input.rs`, `app/event_dispatch.rs`
- **App commands (user intents)** — `app_command.rs`
- **`ChatWidget`** — `chatwidget.rs` (line 513; per-thread chat state machine; renders via the `Renderable` trait) with ~90 submodules under `chatwidget/`
- **App-server session adapter** — `app_server_session.rs` (4,255 lines; thread lifecycle, rollout history, thread list, models, realtime handoff) + `app_server_session/*`
- **Thread event buffering** — `app/thread_event_buffer.rs` (bounded replay), `app/thread_events.rs`, `app/realtime_delivery.rs`
- **Reconnect state machine** — `app/reconnect.rs` (out-of-process reconnect; offline quarantine)

### Rendering model & terminal integration
- **`Tui` wrapper over a custom ratatui Terminal** — `tui.rs` (alt-screen management, event stream, draw scheduling, ambient-pet image drawing)
- **Custom ratatui `Terminal` fork** — `custom_terminal.rs` (+ `custom_terminal/cursor.rs`; OSC 8 hyperlink-aware cell diffing, `draw_with_size`)
- **Scrollback insertion** — `insert_history.rs` (writes finalized history *above the viewport* via escape sequences)
- **Scrollback strategy detection** — `tui/scrollback.rs` (`Standard` / `Zellij` / `FullScreen`)
- **Alternate screen** — `tui/alternate_screen.rs`; **keyboard modes (kitty protocol, VSCode detection)** — `tui/keyboard_modes.rs`
- **Event broker & stream** — `tui/event_stream.rs` (drops/recreates the crossterm stream to fully release stdin)
- **Frame rate limiter** — `tui/frame_rate_limiter.rs` (120 FPS cap); **frame requester** — `tui/frame_requester.rs`
- **tmux resize monitor** — `tui/size_monitor.rs` (500 ms worker thread); **tmux helpers** — `tui/tmux.rs`; **Windows console** — `tui/windows_console.rs`
- **Job control / Ctrl+Z suspend** — `tui/job_control.rs`; **input boundary** — `tui/input_boundary.rs`; **link pointer** — `tui/link_pointer.rs`; **selection clipboard** — `tui/selection_clipboard.rs`; **history tail** — `tui/history_tail.rs`; **screen-size policy** — `tui/screen_size.rs`; **stderr guard** — `tui/terminal_stderr.rs`
- **Renderable abstraction** — `render/renderable.rs` (`trait Renderable { render, desired_height, render_scrolled, cursor_pos, cursor_style }`, `FlexRenderable`, insets)
- **Terminal probes** — `terminal_probe.rs` (+ `startup_replay.rs`, `terminal_identity.rs`, `windows.rs`): OSC 10/11 default colors, keyboard-enhancement flags, 250 ms budget
- **Terminal palette/color level** — `terminal_palette.rs` (TrueColor/Ansi256/Ansi16/Unknown via `supports_color`), `color.rs` (blend, lightness, perceptual distance)
- **Semantic hyperlinks** — `terminal_hyperlinks.rs` (+ `paragraph.rs`, `source.rs`): OSC 8 annotations applied at buffer-write time
- **Terminal title** — `terminal_title.rs` (sanitized OSC title, 240-char cap); **title setup wizard** — `bottom_pane/title_setup.rs`
- **Wrapping & measurement** — `wrapping.rs` (2,096 lines, `adaptive_wrap_line`), `line_truncation.rs`, `width.rs` (Unicode display width)
- **Resize reflow** — `app/resize_reflow.rs`, `resize_reflow_cap.rs` (terminal-specific row caps), `transcript_reflow.rs`

### Composer & bottom pane
- **Bottom pane** — `bottom_pane/mod.rs` (`BottomPane`: composer + `view_stack: Vec<Box<dyn BottomPaneView>>`, status row, banners, questions)
- **Chat composer** — `bottom_pane/chat_composer.rs` (+ 20 submodules: `draft_state`, `attachment_state`, `completion_target`, `composer_layout`, `footer_state`, `inline_input`, `slash_input`, `paste_input`, `popup_state`, `mouse`, `reconnect`, `status_surface`, `warning_notice`, `sparkle.rs`/`sparkle_field.rs` (Astra sparkle), `vim_history`, `vim_search`, `history_search*`, `agents_navigation`)
- **Textarea engine** — `bottom_pane/textarea.rs` (+ `vim.rs`, `vim_commands.rs`, `vim_search.rs`, `wrapping.rs`, `hyperlinks.rs`, `mouse.rs`; single-entry kill buffer)
- **Slash command popup** — `bottom_pane/command_popup.rs`, `bottom_pane/slash_commands.rs`; **file search popup** — `bottom_pane/file_search_popup.rs` + `file_search.rs`; **skill popup** — `bottom_pane/skill_popup.rs`, `bottom_pane/skills_toggle_view.rs`
- **Generic pickers** — `bottom_pane/list_selection_view.rs`, `bottom_pane/multi_select_picker.rs`, `bottom_pane/picker_option.rs`, `picker_presets.rs`, `picker_rows.rs`, `picker_style.rs`, `selection_popup_common.rs`, `selection_row_layout.rs`, `selection_picker_layout.rs`, `selection_tabs.rs`, `scroll_state.rs`
- **Mentions** — `bottom_pane/mentions_v2/*` (candidate, filter, popup, search catalog/mode), `mention_codec.rs`, `app/connector_mentions.rs`, `app/plugin_mentions.rs`, `task_mentions.rs`
- **Status indicator row** — `status_indicator_widget.rs` (+ `timer.rs`, `summary_shimmer.rs`), `bottom_pane/effort_status_line.rs`, `bottom_pane/unified_exec_footer.rs`, `bottom_pane/hook_status.rs`
- **Effort ignition animations** — `bottom_pane/effort_ignition.rs` (+ styles): Wave/Aurora/Pulse when reasoning effort → Max/Ultra
- **Banners & warnings** — `bottom_pane/actionable_banner.rs`, `bottom_pane/action_required_title.rs`, `bottom_pane/warnings_view.rs`, `bottom_pane/warnings.rs`, `chatwidget/warnings.rs`, `history_cell/warnings.rs`, `app/startup_warnings.rs`
- **Footer & hints** — `bottom_pane/footer.rs`, `footer_hint.rs`, `key_hint.rs`, `bottom_pane/shortcut_overlay.rs` (3/2/1-column shortcut reference), `shortcut_help.rs`, `tooltips.rs` (keybinding-aware tooltip lines)
- **Async questions overlay** — `bottom_pane/questions.rs`, `bottom_pane/async_questions/*` (state/layout/render/input), `bottom_pane/request_user_input/*`
- **Approval modal** — `bottom_pane/approval_overlay.rs` (exec/apply-patch/MCP elicitation/permissions), `bottom_pane/apply_patch_header.rs`, `bottom_pane/pending_thread_approvals.rs`
- **MCP elicitation form** — `bottom_pane/mcp_server_elicitation.rs`; **hooks browser** — `bottom_pane/hooks_browser_view.rs` (+ render), `hooks_rpc.rs`, `chatwidget/hooks.rs`/`hook_lifecycle.rs`
- **Other bottom-pane views** — `bottom_pane/feedback_view.rs`, `feedback_note_view.rs`, `bottom_pane/memories_settings_view.rs` (memories editor), `bottom_pane/experimental_features_view.rs`, `bottom_pane/custom_prompt_view.rs` (review custom prompt + `picker.rs`), `bottom_pane/skills_toggle_view.rs`, `bottom_pane/user_verification.rs`, `bottom_pane/voice_strip.rs`, `bottom_pane/paste_burst.rs`, `bottom_pane/pending_input_preview.rs`, `bottom_pane/composer_gap.rs`, `bottom_pane/empty_state_policy.rs`, `bottom_pane/status_line_setup.rs` + `status_line_style.rs` (`/statusline` wizard), `bottom_pane/startup.rs`, `bottom_pane/prompts.rs`-adjacent `prompt_args.rs`

### Transcript, history cells, diffs, exec cells
- **Transcript viewport** — `transcript_view.rs` (+ 20 submodules: `layout`, `selection`, `search`, `text`, `bookmark`, `snapshot`, `follow_control`, `disclosure`, `footer`, `input`, `mutations`, `prompt_header`, `activity`, `composer_gap`, `turn_tip`)
- **Owned transcript composition** — `app/owned_transcript.rs` (+ tests)
- **History cells** — `history_cell/mod.rs` + ~25 cells: `base.rs`, `exec.rs`, `mcp.rs`/`mcp_preview.rs`/`mcp_result.rs`, `patches.rs`, `plans.rs`, `messages.rs`, `approvals.rs`, `computer_activity.rs`, `dynamic.rs`, `hook_cell.rs`, `request_user_input.rs`, `search.rs`, `separators.rs`, `session.rs`, `notices.rs`, `warnings.rs`, `startup_warnings.rs`, `spoken_artifacts.rs`, `activity_details.rs`/`activity_group.rs`/`activity_preview.rs`, `markdown_render_cache.rs`
- **Exec cells** — `exec_cell/mod.rs` (+ `model.rs`, `render.rs`, `compact.rs`, `live_output.rs`, `transcript.rs`); **command display helpers** — `exec_command.rs`, `tool_output.rs`, `live_wrap.rs`
- **Diff model & renderer** — `diff_model.rs` (`FileChange::{Add,Delete,Update}` with unified diff), `diff_render.rs` (2,745 lines; line numbers, gutter signs, syntax highlighting, theme-aware fills), `get_git_diff.rs`, `git_action_directives.rs`, `inline_visualization.rs` (+ `viewer.rs`)
- **Thread transcript loading** — `thread_transcript.rs` (+ `activity_pages.rs`, `computer_groups.rs`, `exploration_groups.rs`, `other_items.rs`, `tools.rs`), `app/history_pagination.rs`, `app/native_history.rs`, `app/history_ui.rs`, `app/history_completion.rs`
- **Transcript export** — `chatwidget/transcript_export.rs`, `app/transcript_export.rs`, `markdown_copy.rs` (+ `table.rs`)

### Streaming & markdown
- **Stream controllers** — `streaming/controller.rs` (`StreamController`, `PlanStreamController`, `StreamCore`), `streaming/render.rs`, `streaming/chunking.rs`, `streaming/code_fence.rs`, `streaming/commit_tick.rs`, `streaming/prose_preview.rs`, `streaming/table_holdback.rs`, `streaming/mod.rs` (`StreamState`)
- **Markdown event renderer** — `markdown_render.rs` (3,201 lines; pulldown-cmark → ratatui lines, table pipeline) + submodules: `math.rs` (+ `math/render.rs`, `structured.rs`), `mermaid.rs`, `task_lists.rs`, `local_links.rs`, `web_links.rs`, `file_citations.rs`, `source_tables.rs`, `table_key_value.rs`, `list_spacing.rs`, `preferences.rs`, `streaming.rs`
- **Markdown helpers** — `markdown.rs`, `markdown_stream.rs`, `markdown_text_merge.rs`, `table_detect.rs`
- **Syntax highlighting** — `render/highlight.rs` (syntect + two-face, ~250 languages), `render/highlight_streaming.rs`, `render/model_themes.rs` (six model-named bundled themes: ada, babbage, curie, cushan, dali, davinci), `render/line_utils.rs`
- **Inline images/visuals** — `empty_state_animation.rs` (blossom welcome animation; `geometry`, `greetings`, `lighting`, `paths`, `policy`, `renderer`, `sequence`), `shimmer.rs`, `summary_shimmer.rs`, `daybreak.rs` (Astra/cyber eligibility copy)

### Pickers, dialogs, screens, overlays
- **Overlay enum** — `pager_overlay.rs` (`Overlay::{Transcript, Static, Analytics}`), `pager_overlay/scrolling.rs`, `pager_overlay/transcript.rs`
- **Resume/fork session picker** — `resume_picker.rs` (7,325 lines) + `resume_picker/layout.rs`, `page_loading.rs`, `archive.rs`, `resume_picker_transcript_preview.rs`, `session_resume.rs`, `named_session_lookup.rs`, `unarchive_prompt.rs`, `session_archive_commands.rs`, `session_queue_commands.rs`
- **Model picker & popups** — `chatwidget/model_popups.rs`, `model_popup_state.rs`, `chatwidget/session_model_selection.rs`, `model_catalog.rs`, `model_migration.rs`, `oss_selection.rs`
- **Permissions UI** — `chatwidget/permissions_menu.rs`, `permission_popups.rs`, `permission_shortcuts.rs` (+ `app/permission_shortcuts.rs`), `permission_discovery.rs`, `chatwidget/permission_discovery.rs`, `approval_events.rs`, `app/file_change_approvals.rs`, `auto_review_denials.rs`
- **Review mode** — `chatwidget/review.rs`, `review_popups.rs`, `bottom_pane/custom_prompt_view.rs`
- **Agents dashboard (daemon-wide overview)** — `app/agents_overview.rs` (+ `agents_overview_{actions,details,errors,grouping,loading,new,threads,usage,view}.rs`), `app/agent_center/*` (hints/input/navigation/render/rows), `app/agent_picker.rs`, `agent_picker.rs`, `agent_navigation.rs` (`app/agent_navigation.rs`), `multi_agents.rs`, `app/agent_status_feed.rs`
- **Worktrees** — `chatwidget/worktree_picker.rs`, `worktree_browser.rs`, `app/managed_worktree_creation.rs`, `worktree_startup.rs`
- **TUI mode picker** — `app/tui_mode_picker.rs` + `chatwidget/tui_mode_picker.rs` (`/tui`)
- **Theme picker** — `theme_picker.rs` (live preview + cancel-restore)
- **Keymap editor** — `keymap_setup.rs` (2,067 lines) + `keymap_setup/{actions,capture,debug,picker}.rs`
- **Status & usage surfaces** — `status/mod.rs` (+ `account.rs`, `card.rs`, `format.rs`, `helpers.rs`, `rate_limits.rs`, `remote_connection.rs`, `thread_usage.rs`), `chatwidget/status_surfaces.rs`, `status_controls.rs`, `status_state.rs`, `token_usage.rs`, `chatwidget/usage.rs`, `usage_history.rs`, `usage_notice.rs`, `chatwidget/rate_limits.rs`, `app/rate_limit_refresh.rs`, `chatwidget/reset_credits.rs`
- **Recap** — `app/recap.rs`, `app/recap_history.rs`, `chatwidget/recap.rs`; **turn tips** — `app/turn_tips.rs`, `turn_tip.rs`, `chatwidget/user_messages.rs`
- **Feedback** — `bottom_pane/feedback_view.rs`, `feedback_note_view.rs`, `app/feedback_upload.rs`, `update_prompt.rs`/`updates.rs`/`updates_cache.rs`/`update_action.rs`/`update_versions.rs`/`npm_registry.rs` (update + feedback upload flows), `app/daemon_menu.rs` (daemon update menu), `daemon_startup.rs`, `daemon_recovery.rs`, `daemon_telemetry.rs`
- **Warnings screens** — `app/warnings*`, `startup_warnings.rs`, `app/backend_banner_fallback.rs`, `backend_banners.rs` (+ `actions.rs`, `render.rs`)
- **User verification / auth dialogs** — `app/user_verification.rs`, `user_verification_requests.rs`, `user_verification_errors.rs`, `bottom_pane/user_verification.rs`
- **External editor** — `external_editor.rs` (Ctrl+G handoff via `EDITOR`/`VISUAL`)
- **IDE integration** — `ide_context.rs` (+ `ipc.rs`, `prompt.rs`, `windows_pipe.rs`)
- **External agent config migration** — `external_agent_config_migration/*` (`/import` from Claude Code)
- **Windows sandbox prompts** — `windows_sandbox.rs`, `chatwidget/windows_sandbox_prompts.rs`, `app/` equivalents
- **cwd prompts** — `cwd_prompt.rs` (resume/fork cwd picker), `app/working_directory.rs` (`/cd`), `app/startup_prompts.rs`

### Voice / realtime
- **Realtime voice orchestration** — `chatwidget/realtime.rs` (WebRTC session owned locally, app-server-signaled) + `realtime/recording_controls.rs`, `realtime/transcript_replay.rs`, `chatwidget/realtime_split_flap.rs` (split-flap caption animation + amplitude history), `chatwidget/realtime_settings.rs`, `app/realtime_settings.rs`, `app/voice_owner.rs`

### Analytics dashboard
- **Account analytics** — `analytics.rs` (+ ~30 submodules: `dashboard.rs`, `plot.rs` (+ `annotations/layout/painting`), `activity_chart.rs`, `chart.rs`, `summary.rs`, `chats.rs`, `tokens.rs`, `tasks.rs`, `plan.rs`, `client.rs`, `data.rs`, `models.rs`, `normalize.rs`, `mouse.rs`, `controls.rs`, `hints.rs`, `sections.rs`, `chrome.rs`, `panels.rs`, `render.rs`, `styles.rs`); snapshot tests under `analytics/snapshots/` (insta-style)

### Pets, notifications, accessibility, misc
- **Ambient terminal pets** — `pets/mod.rs` (+ `ambient.rs`, `asset_pack.rs`, `catalog.rs`, `frames.rs`, `image_protocol.rs`, `model.rs`, `picker.rs`, `preview.rs`, `sixel.rs`), `app/pets.rs`, `chatwidget/pets.rs`
- **Desktop notifications** — `notifications/mod.rs`, `notifications/bel.rs` (BEL), `notifications/osc9.rs` (OSC 9)
- **Accessibility** — `screen_reader.rs` (+ `screen_reader_windows.rs`), `motion.rs` (reduced-motion primitives), `system_motion.rs`
- **Slash commands** — `slash_command.rs` (~55 commands, presentation-ordered enum), `chatwidget/slash_dispatch.rs`, `app_commands` wiring
- **Keymap core** — `keymap.rs` (4,083 lines) + `keymap/bindings.rs`, `keymap/chords.rs`, `keymap/vim_search.rs`
- **Clipboard** — `clipboard_copy.rs` (+ `tmux.rs` tmux passthrough, `worker.rs` async worker), `clipboard_html.rs` (HTML copy), `clipboard_paste.rs` (+ `text.rs`), `app/clipboard.rs`, `app/right_click_paste.rs`, `text_selection.rs`
- **Session logging** — `session_log.rs` (JSONL session recorder behind env vars)
- **Debug/diagnostic** — `debug_config.rs` (`/debug-config` config-layer report), `version.rs`, `app_info.rs`, `debug_config.rs`-adjacent `config_update.rs`
- **Dynamic tools (MCP-shaped)** — `dynamic_tools.rs` (1,473 lines), `dynamic_tools_mcp.rs`, `dynamic_tools_response.rs`, `temporary_structured_request.rs`, `app_backtrack.rs` (Esc backtrack), `assistant_directives.rs`, `async_question_reply.rs`, `workspace_command.rs`/`workspace_messages.rs`, `branch_summary.rs`, `goal_display.rs`/`goal_files.rs` (`/goal`), `clock_format.rs`, `thread_color.rs`, `text_formatting.rs`, `ui_consts.rs`, `test_backend.rs` (VT100 test backend), `public_widgets/` (public composer-input widget), `bin/md-events.rs` (markdown event dumper binary)
```

---

## 2. HOW IT WORKS

**1. Three-channel event loop (`app/startup.rs:1094-1180`).** The main loop is a single `tokio::select!` over (a) `app_event_rx: mpsc::UnboundedReceiver<AppEvent>` — every background worker (app-server protocol, clipboard, file search, timers) sends `AppEvent`s (~600 variants, `app_event.rs:279-1612`), (b) `active_thread_rx` — a per-thread channel of buffered protocol events that is only polled when `should_handle_active_thread_events(...)` passes and no app events are pending, and (c) `tui_events.next()` — crossterm input mapped by `TuiEventStream` (`tui/event_stream.rs`) into `TuiEvent::{Key, Mouse, Paste, Resize, Draw, FocusGained/Lost}`. Input is *blocked* while startup events are pending (`block_terminal_input_for_pending_startup_events`), which is how the "protected input boundary" keeps premature keystrokes from a modal that hasn't opened yet. Dispatch is exhaustive in `app/event_dispatch.rs` (`App::handle_event`), with a hard-coded offline whitelist of events allowed while `reconnect.offline`.

**2. Scrollback-as-transcript rendering model (`insert_history.rs`, `tui.rs`, `app.rs:1152-1205`).** The TUI does *not* keep a full-screen ratatui app on screen by default. `ChatWidget` implements `Renderable` (`render/renderable.rs:16-32`: `render(area, buf)`, `desired_height(width)`, optional `render_scrolled`, `cursor_pos/style`). `App::render_chat_widget_frame` asks the widget for `desired_height(width)`, then calls `tui.draw_with_resize_reflow(desired_height, ...)` so the terminal viewport grows only as tall as the live UI (composer + status + streaming tail). *Finalized* conversation rows are written **above the viewport directly into terminal-native scrollback** by `insert_history_lines` using cursor save/restore, `MoveTo`, `Clear` escape sequences — ratatui never re-renders them. `InsertHistoryMode::{Standard, FullScreen}` and `ScrollbackStrategy::{Standard, Zellij, FullScreen}` (`tui/scrollback.rs`) pick between partial scroll-region tricks and a full-screen repaint: Zellij and Windows Terminal (`WT_SESSION`) get different strategies because partial scroll regions are unreliable there. On terminal resize, `app/resize_reflow.rs` re-replays the retained cells into scrollback, capped per terminal by `resize_reflow_cap.rs` to avoid replaying more rows than the terminal retains.

**3. The `AppEvent` bus and draw pacing.** Widgets never sleep-poll; they request frames through `FrameRequester` (`tui/frame_requester.rs`), which is clamped by `FrameRateLimiter` to **120 FPS** (`MIN_FRAME_INTERVAL = 8.33 ms`, `tui/frame_rate_limiter.rs`). Time-based UI (the "press again to quit" hint, streaming commit ticks, status timers) is driven by `tokio::time::Interval`s owned by `App` (`commit_animation`, `app.rs`) that send events back into the loop — so idle animations cost nothing when stopped. `StatusTimer` (`status_indicator_widget/timer.rs`) similarly keeps elapsed time even when the streaming row drops.

**4. Anchored transcript viewport (`transcript_view.rs`).** `TranscriptView` addresses content by `EntryKey` — either `Cell(Arc::as_ptr(cell) as usize)` (stable across prepends because it's a pointer, not an index) or `Live` (the streaming tail). A reading position is an `Anchor { key, index, offset, row_bias }` where `row_bias` distinguishes synthetic separator rows (+) from disclosure-control rows (−) that share the source line's offset. `Position::{Latest, Reading(Anchor)}` means prepending older history pages never renumbers what you're looking at, and rewrapping under resize maps an anchor back to the right logical row. Layout is cached in `layout::LayoutCache`, with bounded `TextLayout` per entry; selection, search (`search.rs`), bookmarks, and follow-control (snap-to-latest vs. held reading) are separate state machines over the same visible-row list. The app owns the cells; the view owns only "reading/selection state and bounded layout caches" (module doc).

**5. Two-region streaming with table holdback (`streaming/controller.rs`).** Every agent/plan stream is partitioned into a *stable region* (queued for scrollback commit through `StreamState`) and a mutable *tail region* rendered in the active-cell slot. `StreamCore` maintains invariants `emitted_stable_len <= enqueued_stable_len <= render.lines.len()`; committed source is append-only until `reset()`. Because markdown tables are non-incremental (a new row reshapes every column), `table_holdback.rs` scans for the pipe-table header+delimiter pattern and keeps everything from the header onward in the tail until finalization. Mermaid fences swap the closing fence for a rendered diagram only while the block is last; on resize the diagram can fall back to source if it no longer fits. On width change `set_width` re-renders the whole stream and *rebuilds* the queue from the current emitted count instead of remapping bytes.

**6. Markdown pipeline with content-aware table layout (`markdown_render.rs`).** pulldown-cmark events are folded into styled ratatui lines. Tables go through: spillover-row heuristic filtering (undoing pulldown-cmark's lenient parsing), column-count normalization, then width allocation that classifies columns as **Narrative / TokenHeavy / Compact** — token-heavy columns (paths, URLs, hashes) yield width before prose, compact values are protected last, and when even 3-char columns can't fit, body rows transpose into key/value records. `tui.rendering` preferences can disable features (mermaid, math, tables), and the renderer then preserves the disabled feature **as source text**. `markdown_text_merge.rs` handles incremental text merges for streaming.

**7. Syntax highlighting engine (`render/highlight.rs`).** syntect + `two-face` (≈250 grammars, bundled themes) behind five process singletons: `SYNTAX_SET` (immutable grammars), `THEME` (`RwLock<Theme>`, swappable at runtime for `/theme` live preview), `THEME_REVISION` (`AtomicU64` — bumped on every theme swap so rendered-content caches like `history_cell/markdown_render_cache.rs` can invalidate), `THEME_OVERRIDE`, `CODEX_HOME`. Guardrails reject inputs > 512 KB, > 10,000 lines, or with any line > 4 KiB, returning `None` so callers fall back to plain text. Custom `.tmTheme` files are discovered under `{CODEX_HOME}/themes/` (`theme_picker.rs`). Six model-named themes (ada…davinci) are compiled in via `include_str!` in `render/model_themes.rs`.

**8. Diff rendering (`diff_render.rs`, `diff_model.rs`).** `FileChange::{Add, Delete, Update{unified_diff, move_path}}` is the wire model (serde snake_case-tagged). Each diff line gets a right-aligned line number, gutter sign, and content; **Update hunks are highlighted as one concatenated block** per hunk so syntect's parser state survives multi-line strings/comments across consecutive lines (cross-hunk state is deliberately dropped). `DiffTheme` picks muted tints (`#212922`/`#3C170F`) on dark terminals vs. GitHub pastels on light, with fixed palettes per color level so add/delete stay distinct even in ANSI-16; syntax-theme `markup.inserted/deleted` scope backgrounds override the hardcoded palette at rich color levels. Long lines hard-wrap with span splitting that preserves styles across the split.

**9. Runtime keymap (`keymap.rs`, `keymap/bindings.rs`, `keymap/chords.rs`).** Config deserializes into `TuiKeymap` then resolves to `RuntimeKeymap` with precedence **context binding → `tui.keymap.global` fallback → built-in defaults**, plus explicit unbinding and duplicate-key validation. Contexts: `global`, `chat`, `composer`, `editor`, `vim_normal`, `vim_operator`, `vim_search`, `vim_text_object`, `pager`, `list`, `agents`, `approval`. `validate_conflicts` rejects printable keys bound to actions that would steal text input ("printable keys are reserved for text input"), reserves `ctrl-z` for suspend, and rejects AltGr-modified bindings for certain actions. Two-stroke key chords are matched by `KeyChordMatcher` with a timeout (`keymap/chords.rs`, `KEY_CHORD_TIMEOUT` — value not verified). `/keymap` (`keymap_setup.rs`) is a guided three-step editor (action → replace/add/remove → capture key or chord) that writes `tui.keymap.<context>.<action>` and *reuses runtime resolution for validation*, so conflict rules have a single home.

**10. Bottom pane input layering (`bottom_pane/mod.rs`).** `BottomPane` owns the `ChatComposer` (retained even when a view is up, so drafts survive popups) plus a `Vec<Box<dyn BottomPaneView>>` stack of modal pickers. Ctrl+C is deliberately layered: active view consumes it (dismiss), then composer history-search (cancel), then `ChatWidget` decides interrupt-vs-quit with a double-press armed quit hint. Number shortcuts apply only to an empty, idle composer; paste bursts and dialogs keep normal routing (module doc, lines 1-18). The composer itself (`bottom_pane/chat_composer.rs`) is a state machine over `TextArea` handling paste-burst detection (unbracketed pastes, Windows-sensitive, Tokio-clock-based), slash-command atomics, popup key routing, and queueing (Tab).

**11. Multi-thread architecture (`app.rs`).** One `ChatWidget` is active; the `App` holds `thread_event_channels: HashMap<ThreadId, ...>`, `side_threads`, `background_voice: Option<Box<ChatWidget>>` (a second full widget for voice), per-thread bounded replay buffers (`app/thread_event_buffer.rs` — merged streaming text is capped so replay eviction stays finite), and generation counters that invalidate stale async completions (e.g., `rate_limit_hard_stop_generation`, `pending_plugin_enabled_writes` serializing config writes "so stale completions cannot overwrite a newer toggle"). Sub-agent status feeds are bounded best-effort previews (`app/agent_status_feed.rs`); the daemon-wide agents dashboard (`app/agents_overview*.rs`) lists recent sessions and opens other owners' threads as frozen read-only snapshots.

**12. Approvals pipeline (`bottom_pane/approval_overlay.rs`, `approval_events.rs`, `app_server_approval_conversions.rs`).** exec / apply-patch / MCP-elicitation / permission requests are converted into a shared list-selection view with per-action shortcut sets; selection always emits an explicit decision event. MCP elicitation keeps `Esc` hard-mapped to Cancel even under custom keybindings so dismissal can't become "continue without info". Approval keymap defaults: `y` approve, `a` session, `p` prefix, `d` deny, `n`/`Esc` decline, `c` cancel, `Ctrl+A` fullscreen (`keymap.rs`).

**13. History pagination and resume (`app/history_pagination.rs`, `resume_picker.rs`, `app_server_session.rs`).** Initial load is bounded by `INITIAL_HISTORY_TURN_LIMIT`; scrolling to the top triggers `AppEvent::RequestOlderScrollbackHistory` which fetches bounded pages (`HISTORY_ITEM_PAGE_LIMIT`, `thread_items_page_params`) and prepends them into the transcript; since anchors are pointer-keyed, the prepend doesn't move the reading position. The resume picker (7,325 lines) lists threads (cwd-filtered or all, sorted, searchable, with transcript previews rendered through the same markdown pipeline), supports archive/unarchive, delete, and cwd-change-on-resume (`session_resume.rs`, `tui.resume_cwd`).

**14. Terminal probing and identity (`terminal_probe.rs`).** Crossterm's helpers block up to 2 s for terminal query responses; the TUI re-implements the probes with a **250 ms deadline** (`DEFAULT_TIMEOUT`), using duplicated stdio handles and falling back to the controlling-tty path. Probes read OSC 10/11 default fg/bg (feeding `terminal_palette.rs` contrast decisions and light/dark theme adaptation), kitty keyboard-enhancement flags, and terminal identity — and crucially `startup_replay.rs` **replays consumed terminal bytes back through crossterm's parser**, so user input that arrived interleaved with probe responses isn't lost.

**15. Accessibility & motion (`screen_reader.rs`, `motion.rs`, `local_settings.rs`).** At startup a bounded (450 ms) screen-reader probe runs once; its result is persisted as `tui.screen_reader_detection_done` in config.toml (presence of the marker — even `false` — skips future probes). A detected reader forces `MotionMode::Reduced` as the animation default unless the user explicitly configured `tui.animations`. `motion.rs` is the single place that chooses spinner/shimmer vs. static fallbacks, and `style.rs` downgrades yellow "attention" tones to plain on light or unknown backgrounds for contrast.

---

## 3. CONFIG

### CLI flags (`cli.rs`; verified)
| Flag | Default | Meaning |
|---|---|---|
| `PROMPT` (positional) | none | initial user message |
| `--strict-config` | `false` | error on unrecognized config.toml fields |
| `-a, --ask-for-approval <MODE>` | unset | approval policy override |
| `--search` | `false` | enable native `web_search` tool |
| `--no-alt-screen` | `false` | inline mode, preserve native scrollback |
| `--no-daemon` | `false` | skip the shared background server |
| internal (`clap(skip)`) | — | `resume_picker`, `resume_last`, `resume_session_id`, `resume_show_all`, `resume_include_non_interactive`, `agents_overview`, `fork_picker`, `fork_last`, `fork_session_id`, `fork_show_all` (set by `codex resume`/`codex fork` wrappers) |

### Environment variables (verified by grepping every `env::var`/`var_os` in scope)
- **TUI-specific:** `CODEX_TUI_RECORD_SESSION` (`1/true/TRUE/yes/YES` enables JSONL session recording, `session_log.rs:85`), `CODEX_TUI_SESSION_LOG_PATH` (overrides log path, default `<log_dir>/session-<ts>.jsonl`).
- **External editor:** `EDITOR`, `VISUAL` (`external_editor.rs`).
- **Terminal detection:** `TERM`, `TERM_PROGRAM`, `WT_SESSION`, `TMUX`, `TMUX_PANE`, `STY` (screen), `SSH_TTY`, `SSH_CONNECTION`, `WSL_INTEROP`, `WSL_DISTRO_NAME`, `DISPLAY`, `WAYLAND_DISPLAY`, `FORCE_COLOR`, `HOME`.

### Config keys consumed by the TUI (from `local_settings.rs:50-92`; **defaults live in codex-config, out of audit scope — not verified here**)
`tui.notifications`, `tui.animations`, `tui.effects`, `tui.rendering`, `tui.show_tooltips`, `tui.show_server_version_notice`, `tui.auto_recap`, `tui.disable_paste_burst`, `tui.vim_mode_default`, `tui.question_esc_back`, `tui.raw_output_mode`, `tui.fullscreen_transcript`, `tui.copy_on_select`, `tui.right_click_paste`, `tui.alternate_screen` (`AltScreenMode`), `tui.status_line`, `tui.status_line_use_colors`, `tui.terminal_title`, `tui.theme`, `tui.pet`, `tui.pet_anchor`, `tui.session_picker_view`, `tui.resume_cwd`, `tui.keymap` (contexts: `global/chat/composer/editor/vim_normal/vim_operator/vim_search/vim_text_object/pager/list/agents/approval`), `tui.screen_reader_detection_done` (one-time probe marker), `model_availability_nux`, `terminal_resize_reflow.max_rows` (`Auto | Disabled | Limit(n)`), `history` (persistence), `notices`.

### Verified internal defaults & constants
| Constant | Value | Source |
|---|---|---|
| Frame cap | 120 FPS (`MIN_FRAME_INTERVAL` = 8.33 ms) | `tui/frame_rate_limiter.rs` |
| Terminal probe budget | 250 ms | `terminal_probe.rs` (`DEFAULT_TIMEOUT`) |
| Screen-reader probe timeout | 450 ms | `screen_reader.rs` |
| tmux size-monitor poll | 500 ms | `tui/size_monitor.rs` |
| Max popup rows | 8 | `bottom_pane/popup_consts.rs` |
| Exec tool-call max lines | 5 (user shell: 50) | `exec_cell/render.rs:43-44` |
| Terminal title cap | 240 chars | `terminal_title.rs` |
| Hyperlink destination cap | 8 KB (left as plain text beyond) | `terminal_hyperlinks.rs` |
| Status details max lines | 3 | `status_indicator_widget.rs` |
| Syntax-highlight guardrails | 512 KB / 10,000 lines / 4 KiB per line | `render/highlight.rs` |
| Selection accent | `#63A8F8` (ChatGPT Blue 200); light-bg fill `#A4CDFB` | `style.rs` |
| Default status-line hint | "Press Enter to confirm or Esc to go back" | `bottom_pane/popup_consts.rs` |

### Default keybindings (verified, `keymap.rs::built_in_defaults`, lines 1644-1950)
- **App/global:** `Ctrl+T` open transcript overlay; `F3` find in transcript; `F4` focus activity; `F2` warnings; `Ctrl+G` external editor; `Ctrl+O` copy last response; `Ctrl+L` clear; `Alt+R` raw-output mode; `Ctrl+/` toggle side conversation; `open_agents` **unbound by default**.
- **Chat:** `F8` toggle voice; `Ctrl+X` mute; `Esc` interrupt; `Alt+,`/`Shift+Down` and `Alt+.`/`Shift+Up` reasoning effort; `Shift+Left`/`Alt+Up` edit queued message; `Shift+Right`/`Alt+Down` prompt-stack back; `Ctrl+]` skip question.
- **Composer:** `Enter` submit; `Tab` queue; `?` shortcuts; `Ctrl+R`/`Ctrl+S` history search.
- **Editor (Emacs):** `Ctrl+J/M/Enter/Shift+Enter/Alt+Enter` newline; arrows + `Ctrl+B/F/P/N`; `Alt/Ctrl+Left|Right` word motion; `Ctrl+A/E`, Home/End; `Ctrl+H`/Backspace, `Ctrl+D`/Delete; `Ctrl+W`/`Alt+Backspace` kill word; `Ctrl+U` kill to line start; `Ctrl+K` kill to line end; `Ctrl+Y` yank.
- **Vim normal/operator/text-object:** full modal set (`i/a/A/I/o/O/R`, `hjkl`, `w/b/e/$/0`, `f/F/t/T`, `x/r/s/.`, `d/y/c` operators, `iw/aw` text objects, `u` undo, `Ctrl+R` redo).
- **Pager:** `Up/k`, `Down/j`, `PgUp/Ctrl+B/Shift+Space`, `PgDn/Space/Ctrl+F`, `Ctrl+U/D` half-page, Home/End, `q`/`Ctrl+C` close, `Ctrl+T` close transcript, `F3`/`/` find.
- **List:** `Up/Ctrl+P/Ctrl+K/k`, `Down/Ctrl+N/Ctrl+J/j`, Home/End, Enter accept, Esc cancel.
- **Agents dashboard:** `o` resume, `f` search, `n` new task, `w` new worktree, `r` rename, `x` stop, `a` archive, Backspace delete, `h` hide, `g` toggle grouping.
- **Approval:** listed in mechanism #12.
- **Reserved (rejected if bound):** `Ctrl+Z` (suspend), printable keys for chat text-competing actions (`keymap.rs::validate_conflicts`).

---

## 4. NOTABLE — engineering worth adopting

1. **Terminal-native scrollback as the persistence layer** (`insert_history.rs`, `tui/scrollback.rs`, `app/resize_reflow.rs`). Instead of owning a virtual transcript inside alt-screen, finalized rows are committed into the host terminal's scrollback via raw cursor/scroll-region sequences with per-terminal strategies (Zellij/Windows detected), and the live UI only claims `desired_height` rows. Users get their terminal's native scroll, search, and copy for free. The trade-off is visible in the code: reflow on resize must re-replay rows, with conservative caps (`resize_reflow_cap.rs`) so it doesn't replay more than the terminal retained.

2. **A forked ratatui `Terminal` that understands OSC 8 hyperlinks** (`custom_terminal.rs`, `terminal_hyperlinks.rs`). Hyperlink destinations are carried *beside* the ratatui text (`HyperlinkLine`), never inside it, so wrapping/measuring stay geometry-pure; OSC 8 escapes are emitted only at buffer-flush time, remapped across wrapped lines, with an 8 KB destination cap so huge URLs degrade to plain text.

3. **Drop-and-recreate event stream to truly release stdin** (`tui/event_stream.rs`). Crossterm's `EventStream` keeps a reader thread alive even when merely not polled, which steals input from spawned editors and eats terminal query responses. The `EventBroker` owns the stream and drops/recreates it around external-editor and probe handoffs, plus `terminal_probe/startup_replay.rs` re-injects bytes consumed during probes. A subtle, well-documented fix for a class of "mysterious input after running vim" bugs.

4. **The tmux resize monitor** (`tui/size_monitor.rs`) — tmux drops resize notifications; a dedicated worker thread polls geometry every 500 ms *without holding the shared lock*, and generation counters invalidate both in-flight and queued samples on pause, with drop-as-wakeup instead of joining a possibly blocked OS call. Model for doing background I/O against a UI loop safely.

5. **Table holdback during streaming** (`streaming/table_holdback.rs`) — recognizing that markdown tables are non-incremental and deferring the whole table to the mutable tail until finalization, instead of flickering column realignments. Cheap heuristic (header+delimiter scan outside code fences), large UX win.

6. **Content-aware markdown table layout** (`markdown_render.rs`) — classifying columns Narrative/TokenHeavy/Compact and sacrificing width in that order, then *transposing* to key/value records as a graceful degradation when nothing fits, is a genuinely good algorithm for terminal tables.

7. **Pointer-keyed anchors for infinite-scroll transcripts** (`transcript_view.rs`) — `EntryKey::Cell(Arc pointer)` + within-entry `(index, offset, row_bias)` means prepending older pages and rewrapping cannot renumber the reading position. Cleanest solution I've seen to the "pagination breaks my scroll position" problem.

8. **`THEME_REVISION` atomic cache invalidation** (`render/highlight.rs`) — a global monotonic counter bumped on theme swap so every rendered-content cache can cheaply detect staliness without holding references to the theme. Combined with hard guardrails (512 KB / 10k lines / 4 KiB) that make the highlighter fail-safe to plain text.

9. **Keymap resolution with centralized conflict validation** (`keymap.rs`) — precedence (context → global → defaults) plus a validator that encodes *domain rules* ("printable keys are reserved for text input", "ctrl-z is reserved for suspend") in one place, and a `/keymap` UI that validates new captures by re-running the same resolution. Config UIs that can't produce a broken config are worth copying.

10. **One-time screen-reader probe with a completion marker** (`screen_reader.rs`) — detection result persisted as `tui.screen_reader_detection_done` where *presence* (even false) skips re-probing, explicit `tui.animations` config always wins over the probe, and an unwriteable config still yields a session-local default. Accessible defaults without nagging.

11. **Bounded, generation-guarded async completion** — throughout `app.rs`: `rate_limit_hard_stop_generation`, `pending_plugin_enabled_writes`/`pending_hook_enabled_writes` (per-key serialized writes "so stale completions cannot overwrite a newer toggle"), `thread_event_buffer.rs` capping merged streaming text so replay eviction stays finite. A consistent pattern for taming out-of-order async results in a UI.

12. **Layered Ctrl+C semantics** (`bottom_pane/mod.rs` module doc) — a documented ownership ladder (active view → composer search → widget interrupt/double-press-quit) rather than one global handler; prevents the classic TUI bug where Esc/Ctrl+C does different things depending on hidden state.

13. **Sanitized OSC terminal title** (`terminal_title.rs`) — treating title content as untrusted (model output, thread names, paths) and stripping control chars, bidi/invisible codepoints (Trojan Source family), and whitespace before it enters an OSC sequence, with a 240-char cap and a distinct "sanitized to nothing" outcome. Rare example of escape-injection hygiene applied to a "cosmetic" feature.

14. **Insta-style snapshot testing of rendered terminals** — `analytics/snapshots/*.snap` (28 committed snapshots covering narrow/wide, dark/light, partial-cell, twelve-hour-clock cases) plus `test_backend.rs` (a VT100 backend for tests) and per-module `*_tests.rs` colocated next to sources. The snapshot names alone (`oversized_axes_are_hidden_instead_of_truncated`) read as a regression spec.

**Explicitly not verified:** runtime behavior (nothing was executed — no `cargo test`/`cargo run` was performed in this environment); the internals of `resume_picker.rs` (7,325 lines), `app_server_session.rs` (4,255 lines), and `analytics/` beyond module docs and snapshot names; the value of `KEY_CHORD_TIMEOUT` (`keymap/chords.rs`); the full status-line item catalog (`tui.status_line` values are parsed in codex-config, out of scope); pets image-protocol internals (`pets/sixel.rs`, `pets/image_protocol.rs`); the exact WebRTC message flow in `chatwidget/realtime.rs`; and every `*_tests.rs` file (excluded by design).

