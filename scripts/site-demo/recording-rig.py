#!/usr/bin/env python3
"""A scripted OpenAI-compatible server used to record real opcode footage.

This is a recording rig, not a model. It replays a fixed script of
responses so an actual opcode session can be driven end to end and
captured. Nothing here ships with the product or the site; the site
only keeps the text opcode itself printed.
"""
import json
import sys
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

# Each request advances one step: list of (kind, payload) chunks.
SCRIPT = []


def text_chunks(text, n=6):
    words = text.split(" ")
    step = max(1, len(words) // n)
    for i in range(0, len(words), step):
        yield {"content": " ".join(words[i:i + step]) + " "}


def tool_chunk(index, call_id, name, arguments):
    return {
        "tool_calls": [{
            "index": index,
            "id": call_id,
            "type": "function",
            "function": {"name": name, "arguments": arguments},
        }]
    }


STEPS = {
    "approve": [
        # round 1: talk, then read the file
        [
            ("delta", {"reasoning": "The user wants a demo run. I will read the file first."}),
            * [("delta", c) for c in text_chunks(
                "I will read the file first, then make the edit and check it compiles.")],
            ("delta", tool_chunk(0, "call_read1", "read_file", '{"path":"main.go"}')),
            ("finish", "tool_calls"),
        ],
        # round 2: talk, then run a command that writes outside the project
        [
            * [("delta", c) for c in text_chunks(
                "The file reads cleanly. Next I need to refresh the generated "
                "manifest, which writes outside this directory.")] ,
            ("delta", tool_chunk(0, "call_bash1", "bash",
                                 '{"command":"echo regenerated > ~/opcode-site-demo.manifest","sandbox":false}')),
            ("finish", "tool_calls"),
        ],
        # round 3: final answer
        [
            * [("delta", c) for c in text_chunks(
                "Done. The manifest is regenerated and the file still builds. "
                "I stopped at the permission prompt so you could read the exact "
                "command before it ran.")],
            ("finish", "stop"),
        ],
    ],
    "diff": [
        [
            ("delta", {"reasoning": "The greeting needs no tools. I will edit the file."}),
            * [("delta", c) for c in text_chunks(
                "I will make the greeting configurable so it can be changed "
                "without editing main.")] ,
            ("delta", tool_chunk(0, "call_edit1", "edit_file",
                                 '{"path":"main.go","old":"func main() {\\n\\tfmt.Println(\\"hello\\")\\n}","new":"func main() {\\n\\tfmt.Println(greeting())\\n}\\n\\nfunc greeting() string {\\n\\treturn \\"hello\\"\\n}"}')),
            ("finish", "tool_calls"),
        ],
        [
            * [("delta", c) for c in text_chunks(
                "Done. The greeting now lives in its own function, so changing "
                "it is one line and the diff above is exactly what landed.")],
            ("finish", "stop"),
        ],
    ],
}

scenario = sys.argv[2] if len(sys.argv) > 2 else "approve"
SCRIPT = STEPS[scenario]
step_index = 0


class Handler(BaseHTTPRequestHandler):
    def log_message(self, *args):
        pass

    def do_POST(self):
        global step_index
        length = int(self.headers.get("Content-Length", 0))
        self.rfile.read(length)
        print("REQUEST step=%d path=%s" % (step_index, self.path), flush=True)

        if step_index >= len(SCRIPT):
            steps = SCRIPT[-1]
        else:
            steps = SCRIPT[step_index]
        step_index += 1

        self.send_response(200)
        self.send_header("Content-Type", "text/event-stream")
        self.end_headers()

        for kind, payload in steps:
            chunk = {"choices": [{"index": 0, "delta": {}, "finish_reason": None}]}
            if kind == "delta":
                chunk["choices"][0]["delta"] = payload
            else:
                chunk["choices"][0]["finish_reason"] = payload
            self.wfile.write(b"data: " + json.dumps(chunk).encode() + b"\n\n")
            self.wfile.flush()
            time.sleep(0.05)

        usage = {"choices": [], "usage": {"prompt_tokens": 812, "completion_tokens": 173}}
        self.wfile.write(b"data: " + json.dumps(usage).encode() + b"\n\n")
        self.wfile.write(b"data: [DONE]\n\n")
        self.wfile.flush()


if __name__ == "__main__":
    port = int(sys.argv[1]) if len(sys.argv) > 1 else 8787
    ThreadingHTTPServer(("127.0.0.1", port), Handler).serve_forever()
