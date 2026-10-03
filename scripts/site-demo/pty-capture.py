#!/usr/bin/env python3
"""Drive a TTY program under a pseudo-terminal and dump what it draws.

Answers the two probes a bubbletea app sends on startup (background
colour query, cursor-position report) so the program does not block,
optionally injects keystrokes, then prints the raw screen dump.
"""
import fcntl
import json
import os
import pty
import select
import signal
import struct
import sys
import termios
import time

cmd = sys.argv[1]
# argv[2] is a JSON list of [seconds, keystrokes] pairs, or a bare
# string of keystrokes sent after 1.5s.
raw_keys = sys.argv[2] if len(sys.argv) > 2 else ""
try:
    parsed = json.loads(raw_keys)
except ValueError:
    parsed = [[1.5, raw_keys]]
key_events = [(float(d), str(k).encode("utf-8")) for d, k in parsed]
duration = float(sys.argv[3]) if len(sys.argv) > 3 else 4.0
send_quit = len(sys.argv) > 4 and sys.argv[4] == "quit"

env = dict(os.environ)
env["TERM"] = "xterm-256color"
env["COLORTERM"] = "truecolor"

pid, fd = pty.fork()
if pid == 0:
    os.execvpe(cmd.split()[0], cmd.split(), env)

# pty.fork leaves the window size at 0x0; a terminal that reports no
# rows gives bubbletea nowhere to draw its overlays.
COLS = int(os.environ.get("CAPTURE_COLS", "100"))
ROWS = int(os.environ.get("CAPTURE_ROWS", "42"))
fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", ROWS, COLS, 0, 0))

os.set_blocking(fd, False)
buf = bytearray()
start = time.time()


def drain(timeout):
    end = time.time() + timeout
    while time.time() < end:
        r, _, _ = select.select([fd], [], [], 0.1)
        if not r:
            continue
        try:
            chunk = os.read(fd, 65536)
        except OSError:
            print("EOF-OSError at %.1fs" % (time.time() - start), file=sys.stderr)
            return False
        if not chunk:
            print("EOF-empty at %.1fs" % (time.time() - start), file=sys.stderr)
            return False
        buf.extend(chunk)
        if b"\x1b]11;?" in chunk:
            os.write(fd, b"\x1b]11;rgb:0000/0000/0000\x1b\\")
        if b"\x1b[6n" in chunk:
            os.write(fd, b"\x1b[1;1R")
    return True


pending = list(key_events)
while time.time() - start < duration:
    if not drain(0.25):
        break
    while pending and time.time() - start >= pending[0][0]:
        os.write(fd, pending.pop(0)[1])

drain(0.5)
if send_quit:
    try:
        os.write(fd, b"\x03")
    except OSError:
        pass
    drain(1.0)

try:
    os.kill(pid, signal.SIGKILL)
except OSError:
    pass

sys.stdout.buffer.write(bytes(buf))
