# Phase 4 Spec — MCP Manager

## Goal

Connect MCP servers (Section 3.5) and expose their tools to the model as
ordinary tools — the model never knows MCP exists. Own the lifecycle:
parallel startup with timeouts so a dead server never blocks anything,
clean shutdown, bounded reconnection.

## Non-Goals

- HTTP/streamable-HTTP transport: stdio only this phase (the common
  case). An `http` entry in mcp.json is rejected with a clear message
  rather than half-supported.
- The raw JSON-RPC debug view — spec calls it a dev-time nicety; later.
- Context discipline for very large tool inventories: all discovered
  tools load; counts are small for now, the discipline is noted.
- A `/mcp` TUI command or server-add flow (with the third-party review
  reminder that belongs to *adding* a server, not connecting one).

## Approach

- Config: `mcpServers` map matching the Claude Code/Cursor convention —
  `{"mcpServers": {"name": {"command": ..., "args": [...], "env": {...}}}}`.
  User-level `~/.opcode/mcp.json` always; project `.opcode/mcp.json` only
  when trusted (its fingerprint already covers the file, so a changed
  server list re-prompts).
- Protocol: MCP over stdio — JSON-RPC 2.0, newline-delimited.
  Handshake: `initialize` (protocol 2024-11-05, clientInfo opcode) →
  `notifications/initialized` → `tools/list`. Calls: `tools/call` with
  `{name, arguments}`; result is the content array, text parts joined.
  `isError` results surface as tool errors.
- Naming: discovered tools register as `mcp__<server>__<tool>` so they
  can never collide with built-ins or skills.
- Tiers: every MCP tool is Action-Allowed. The protocol's
  `readOnlyHint` is self-reported by the server — a server the user
  didn't write claiming to be read-only is not a permission model, so
  hints never lower a tier.
- Lifecycle: servers connect in parallel at startup, each under a
  timeout; a failing or slow server is skipped with a visible warning
  and the session continues without it. On a call, a dead process gets
  one restart attempt (re-handshake included), then a clear error.
  Shutdown closes stdin and kills the process group.
- Collisions: a project server cannot shadow a user server with the
  same name — that would be a silent hijack of, say, the user's
  "github" server. The project entry is skipped with a warning.

## Edge Cases

- Server sends notifications while a response is pending: skipped; the
  client matches responses by id.
- Server emits extra whitespace/blank lines: tolerated.
- tools/list returning a tool with no inputSchema: an empty object
  schema is substituted.
- A server that exits mid-call: one restart + retry; if it dies again,
  the error names the server.
- Malformed mcp.json: load fails loudly at startup.

## Test Plan

- Unit against a real subprocess MCP server (a small Python fixture
  implementing the protocol): handshake, tool discovery, call
  round-trip with argument echo, isError propagation, restart-once
  after killing the process, and a server that never answers (connect
  must time out, not hang).
- Config: missing file, malformed file, http-transport rejection,
  collision skipping.
- Registry/gate: an MCP tool passes through the gate and audit log like
  any built-in.
- Live: PTY with a real stdio MCP server configured user-level; the
  scripted model calls the MCP tool and the result flows back. Then a
  live OpenRouter run where the model calls the MCP tool for real.
