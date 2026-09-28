#!/usr/bin/env python3
"""Minimal MCP server over stdio for tilde's tests: JSON-RPC 2.0,
newline-delimited. Implements initialize, tools/list (one echo tool),
and tools/call (echoes the arguments back as text). Modes via argv:

    echo            normal operation
    silent          never responds to initialize (timeout test)
    die-once MARK   exits on the first tools/call only, then works
                    (restart test): the marker file records the crash
"""
import json
import os
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
        send({"jsonrpc": "2.0", "id": req["id"], "result": {
            "content": [{"type": "text", "text": "echo: " + args.get("text", "")}]}})
    elif req.get("id") is not None:
        send({"jsonrpc": "2.0", "id": req["id"], "error": {
            "code": -32601, "message": "method not found: " + method}})
