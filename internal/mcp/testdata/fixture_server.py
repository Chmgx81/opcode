#!/usr/bin/env python3
"""Minimal MCP server over stdio for opcode's tests: JSON-RPC 2.0,
newline-delimited. Implements initialize, tools/list (one echo tool),
and tools/call (echoes the arguments back as text). Modes via argv:

    echo            normal operation
    silent          never responds to initialize (timeout test)
    die-once MARK   exits on the first tools/call only, then works
                    (restart test): the marker file records the crash
    chatty          after each successful tools/call, floods 5000
                    notification lines (drop-count test)
    burst           BEFORE each tools/call response, floods 5000
                    notification lines (a response behind a burst)
    ping-first      before each tools/call response, sends a
                    server->client request (method "ping") that reuses
                    the client's request id
    env             tools/call returns the value of the env var named
                    by `text` ("NAME=value" or "NAME unset")
    escape PIDFILE  on initialize, spawns a detached `sleep` in its own
                    session that inherits stdout (so it survives a
                    process-group kill and keeps the pipe open) and
                    records its pid in PIDFILE
"""
import json
import os
import subprocess
import sys

MODE = sys.argv[1] if len(sys.argv) > 1 else "echo"


def send(obj):
    sys.stdout.write(json.dumps(obj) + "\n")
    sys.stdout.flush()


def fail(msg):
    print(msg, file=sys.stderr)
    sys.exit(1)


for line in sys.stdin:
    line = line.strip()
    if not line:
        continue
    try:
        req = json.loads(line)
    except json.JSONDecodeError:
        continue

    method = req.get("method", "")

    if method == "initialize":
        if MODE == "silent":
            continue  # never answer: the client must time out
        if MODE == "escape":
            child = subprocess.Popen(
                ["sleep", "60"], start_new_session=True,
                stdin=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            with open(sys.argv[2], "w") as f:
                f.write(str(child.pid))
        send({"jsonrpc": "2.0", "id": req["id"], "result": {
            "protocolVersion": "2024-11-05",
            "capabilities": {"tools": {}},
            "serverInfo": {"name": "fixture", "version": "1.0"}}})
    elif method == "notifications/initialized":
        pass
    elif method == "tools/list":
        send({"jsonrpc": "2.0", "id": req["id"], "result": {"tools": [
            {"name": "echo",
             "description": "Echoes its input back",
             "inputSchema": {"type": "object", "properties": {
                 "text": {"type": "string"}}, "required": ["text"]}}]}})
    elif method == "tools/call":
        if MODE == "die-once":
            marker = sys.argv[2]
            if not os.path.exists(marker):
                open(marker, "w").write("crashed once")
                sys.exit(1)
        name = req["params"]["name"]
        args = req["params"].get("arguments", {})
        if name != "echo":
            send({"jsonrpc": "2.0", "id": req["id"], "error": {
                "code": -32602, "message": "unknown tool " + name}})
            continue
        if args.get("text") == "fail-me":
            send({"jsonrpc": "2.0", "id": req["id"], "result": {
                "content": [{"type": "text", "text": "the tool refused"}],
                "isError": True}})
            continue
        if MODE == "env":
            var = args.get("text", "")
            val = os.environ.get(var)
            text = var + " unset" if val is None else var + "=" + val
            send({"jsonrpc": "2.0", "id": req["id"], "result": {
                "content": [{"type": "text", "text": text}]}})
            continue
        if MODE == "burst":
            chatter = json.dumps({"jsonrpc": "2.0",
                                  "method": "notifications/chatter"}) + "\n"
            sys.stdout.write(chatter * 5000)
            sys.stdout.flush()
        if MODE == "ping-first":
            send({"jsonrpc": "2.0", "id": req["id"], "method": "ping"})
        send({"jsonrpc": "2.0", "id": req["id"], "result": {
            "content": [{"type": "text", "text": "echo: " + args.get("text", "")}]}})
        if MODE == "chatty":
            # The client is idle between requests: these lines pile up
            # on a full channel. The reader must drop them (with a
            # count) rather than block, or this write would wedge the
            # server once the OS pipe fills.
            chatter = json.dumps({"jsonrpc": "2.0",
                                  "method": "notifications/chatter"}) + "\n"
            sys.stdout.write(chatter * 5000)
            sys.stdout.flush()
    elif req.get("id") is not None:
        send({"jsonrpc": "2.0", "id": req["id"], "error": {
            "code": -32601, "message": "method not found: " + method}})
