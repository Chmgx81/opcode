#!/usr/bin/env bash
# DIAGNOSTIC: find where the fake release server stalls on macOS.
# Temporary; removed once the cause is known.
set -u
cd "$(dirname "$0")/.."
work=$(mktemp -d "${TMPDIR:-/tmp}/probe.XXXXXX")
echo "work=$work"
echo "python3=$(command -v python3)"

# The script's exact server, but with a mark after every step so a stall
# names itself. If the marks stop, that line is the one that blocks.
cat >"$work/marked.py" <<'PY'
import sys, time
t0 = time.time()
def mark(m):
    sys.stderr.write("MARK %-30s %.3fs\n" % (m, time.time() - t0))
    sys.stderr.flush()

mark("begin")
import http.server, os
mark("imported")
root, tag, portfile = sys.argv[1], sys.argv[2], sys.argv[3]
class Handler(http.server.SimpleHTTPRequestHandler):
    def __init__(self, *a, **k):
        super().__init__(*a, directory=root, **k)
    def log_message(self, *a):
        pass
mark("class ready")
srv = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Handler)
mark("constructed port=%d" % srv.server_address[1])
mark("server_name=%r" % srv.server_name)
with open(portfile, "w") as f:
    f.write(str(srv.server_address[1]))
mark("port written")
srv.serve_forever()
mark("serving")
PY

mkdir -p "$work/site/ok/download"
python3 "$work/marked.py" "$work/site" v9.9.9 "$work/port" 2>"$work/err" &
pid=$!
sleep 4
echo "--- marks from the marked server:"
cat "$work/err"
if [ -s "$work/port" ]; then echo "--- port=$(cat "$work/port")"; else echo "--- no port"; fi
echo "--- process state:"
ps -o pid,stat,wchan,command -p "$pid" 2>/dev/null || echo "   (gone)"
echo "--- sample the stack if it is alive:"
if kill -0 "$pid" 2>/dev/null; then
  sample "$pid" 1 -mayDie 2>/dev/null | grep -A6 "Call graph" | head -20 || echo "   (sample unavailable)"
fi
kill "$pid" 2>/dev/null
rm -rf "$work"
