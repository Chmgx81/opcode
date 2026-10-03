#!/usr/bin/env bash
# Record the site's figures from a real v0.6.0 build.
# See README.md in this directory for what is real and what is not.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SRC="${DEMO_SRC:-/tmp/opcode-demo-src}"
WORK="${DEMO_WORK:-/tmp/opcode-demo-work}"
BIN="$WORK/opcode"
PORT="${DEMO_PORT:-8787}"
export CAPTURE_COLS=100 CAPTURE_ROWS=42

# render.py needs a terminal emulator; keep it out of the system python.
if python3 -c 'import pyte' 2>/dev/null; then
  RENDER_PY=python3
else
  if [ ! -x "$HERE/.venv/bin/python" ]; then
    python3 -m venv "$HERE/.venv"
    "$HERE/.venv/bin/pip" install --quiet pyte
  fi
  RENDER_PY="$HERE/.venv/bin/python"
fi

if [ ! -d "$SRC" ]; then
  git -C "$(cd "$HERE/../.." && pwd)" worktree add "$SRC" v0.6.0
fi
if [ ! -x "$BIN" ]; then
  (cd "$SRC" && go build -trimpath -ldflags="-s -w -X main.version=v0.6.0" -o "$BIN" ./cmd/opcode)
fi

mkdir -p "$WORK/home" "$WORK/project"
cat > "$WORK/home/models.json" <<JSON
{
  "default_provider": "demo",
  "providers": {
    "demo": {
      "base_url": "http://127.0.0.1:$PORT/v1",
      "api": "openai",
      "models": ["demo-model"]
    }
  }
}
JSON
cat > "$WORK/home/config.json" <<JSON
{"model": "demo/demo-model", "mode": "build", "theme": "light", "update_checks": false}
JSON

reset_project() {
  cat > "$WORK/project/main.go" <<'GO'
package main

import "fmt"

func main() {
	fmt.Println("hello")
}
GO
}

rig() { # rig <scenario>
  python3 "$HERE/recording-rig.py" "$PORT" "$1" > "$WORK/rig.log" 2>&1 &
  RIG_PID=$!
  sleep 1
}
stop_rig() {
  [ -n "${RIG_PID:-}" ] && kill "$RIG_PID" 2>/dev/null || true
  wait "$RIG_PID" 2>/dev/null || true
  RIG_PID=""
}
trap stop_rig EXIT

record() { # record <outfile> <keys-json> <seconds> [home] [rows]
  local out="$1" keys="$2" secs="$3" home="${4:-$WORK/home}" rows="${5:-42}"
  (cd "$WORK/project" && CAPTURE_ROWS="$rows" OPCODE_HOME="$home" OPCODE_THEME=light \
    timeout 60 python3 "$HERE/pty-capture.py" "$BIN" "$keys" "$secs" > "$WORK/raw.tmp")
  CAPTURE_ROWS="$rows" "$RENDER_PY" "$HERE/render.py" "$WORK/raw.tmp" | awk '
    { line[NR] = $0 }
    END {
      first = 1; last = NR
      while (first <= last && line[first]  ~ /^[[:space:]]*$/) first++
      while (last   >= first && line[last] ~ /^[[:space:]]*$/) last--
      for (i = first; i <= last; i++) print line[i]
    }' > "$out"
}

echo "fig: first run"
rm -rf "$WORK/fresh" && mkdir -p "$WORK/fresh"
reset_project; rig approve
record "$HERE/out/first-run.txt" '' 6 "$WORK/fresh"

echo "fig: approval dialog"
stop_rig; reset_project; rig approve
record "$HERE/out/approval.txt" '[[1.5,"regenerate the manifest\r"]]' 18

echo "fig: expanded diff"
stop_rig; reset_project; rig diff
CAPTURE_ROWS=48
record "$HERE/out/diff.txt" '[[1.5,"make the greeting configurable\r"],[7,"\u0012"]]' 15 "$WORK/home" 48
CAPTURE_ROWS=42
# the pager sits over the view it came from; keep only the pager
awk '/^╭/{f=1} f{print} /^╰/{if(f)exit}' "$HERE/out/diff.txt" > "$WORK/diff.tmp"
mv "$WORK/diff.tmp" "$HERE/out/diff.txt"

echo "fig: headless event stream"
stop_rig; reset_project; rig approve
(cd "$WORK/project" && OPCODE_HOME="$WORK/home" timeout 30 "$BIN" -p "regenerate the manifest" --json) \
  > "$HERE/out/headless.jsonl"

echo "fig: help"
"$BIN" --help > "$HERE/out/help.txt" 2>&1

echo "recorded:"
wc -l "$HERE/out/"*
