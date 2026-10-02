#!/usr/bin/env bash
# Exercises install.sh against a FAKE release served from a local
# http.server on 127.0.0.1 (OPCODE_RELEASE_BASE_URL), under both `sh` and
# `bash`. Never touches the real network. Needs: python3, curl, tar, and
# sha256sum or shasum.
#
#   scripts/test-install.sh
#
# Exits non-zero if any assertion fails.

set -eu

ROOT=$(cd "$(dirname "$0")/.." && pwd)
INSTALLER="$ROOT/install.sh"
TAG=v9.9.9

for tool in python3 curl tar; do
  command -v "$tool" >/dev/null 2>&1 || { echo "test-install: need $tool" >&2; exit 2; }
done

work=$(mktemp -d "${TMPDIR:-/tmp}/opcode-test-install.XXXXXX")
server_pid=
cleanup() {
  [ -z "$server_pid" ] || kill "$server_pid" 2>/dev/null || true
  rm -rf "$work"
}
trap cleanup EXIT
trap 'exit 130' INT TERM

sum_of() {
  if command -v sha256sum >/dev/null 2>&1; then sha256sum "$1" | cut -d' ' -f1
  else shasum -a 256 "$1" | cut -d' ' -f1; fi
}

# ---- fake releases ---------------------------------------------------
# One directory per scenario under $work/site. The installer's base URL
# is http://127.0.0.1:PORT/<scenario>.
mkdir -p "$work/tmp" "$work/home" "$work/fakebin"

make_archives() { # make_archives <dir> <body> : all four unix platforms
  local dir=$1 body=$2 os arch
  mkdir -p "$dir/download/$TAG" "$work/stage"
  for os in linux darwin; do
    for arch in amd64 arm64; do
      printf '#!/bin/sh\necho "%s"\n' "$body" >"$work/stage/opcode-$os-$arch"
      chmod 755 "$work/stage/opcode-$os-$arch"
      (cd "$work/stage" && tar czf "$dir/download/$TAG/opcode-$os-$arch.tar.gz" "opcode-$os-$arch")
    done
  done
}

make_sums() { # make_sums <dir> : checksums.txt in sha256sum format
  local dir=$1 f
  : >"$dir/download/$TAG/checksums.txt"
  for f in "$dir/download/$TAG"/*.tar.gz; do
    printf '%s  %s\n' "$(sum_of "$f")" "$(basename "$f")" >>"$dir/download/$TAG/checksums.txt"
  done
}

S="$work/site"
GOOD_OUT="opcode $TAG (fake)"

make_archives "$S/ok" "$GOOD_OUT"
make_sums "$S/ok"

# checksums describe the honest archives; the served archives are swapped
make_archives "$S/tampered" "$GOOD_OUT"
make_sums "$S/tampered"
make_archives "$S/tampered" "pwned"

make_archives "$S/nosums" "$GOOD_OUT"

make_archives "$S/noentry" "$GOOD_OUT"
printf '%s  %s\n' "$(sum_of "$S/noentry/download/$TAG/opcode-linux-amd64.tar.gz")" "other-file.tar.gz" \
  >"$S/noentry/download/$TAG/checksums.txt"

# an entry whose name merely contains the asset name must not match
make_archives "$S/substring" "$GOOD_OUT"
for os in linux darwin; do for arch in amd64 arm64; do
  printf '%s  %s\n' "$(sum_of "$S/substring/download/$TAG/opcode-$os-$arch.tar.gz")" "x-opcode-$os-$arch.tar.gz.sig" \
    >>"$S/substring/download/$TAG/checksums.txt"
done; done

make_archives "$S/conflict" "$GOOD_OUT"
make_sums "$S/conflict"
for os in linux darwin; do for arch in amd64 arm64; do
  printf '%s  %s\n' "$(printf '%064d' 0 | tr 0 a)" "opcode-$os-$arch.tar.gz" \
    >>"$S/conflict/download/$TAG/checksums.txt"
done; done

# correct checksum, but the archive has no binary in it
make_archives "$S/nobinary" "$GOOD_OUT"
for os in linux darwin; do for arch in amd64 arm64; do
  (cd "$work/stage" && echo hi >README && tar czf "$S/nobinary/download/$TAG/opcode-$os-$arch.tar.gz" README)
done; done
make_sums "$S/nobinary"

# ---- the server: static files + GitHub-style /latest redirect ----------
# A ThreadingTCPServer, not http.server.ThreadingHTTPServer. The latter's
# server_bind calls socket.getfqdn(host) to fill in server_name, and
# getfqdn is a REVERSE DNS lookup: on a machine with no resolver to answer
# it — which is what the macOS runner is — the constructor blocks on it for
# minutes, past any sane startup wait, before the port is ever written. The
# handler below serves the same static files either way; only the unused
# server_name is lost, and nothing in this test reads it.
cat >"$work/server.py" <<'PY'
import http.server, os, socketserver, sys

root, tag, portfile = sys.argv[1], sys.argv[2], sys.argv[3]

class Handler(http.server.SimpleHTTPRequestHandler):
    def __init__(self, *a, **k):
        super().__init__(*a, directory=root, **k)

    def _latest(self):
        # <scenario>/latest -> <scenario>/tag/<tag>, only if the scenario exists
        if self.path.endswith("/latest"):
            scen = self.path[: -len("latest")]
            if os.path.isdir(os.path.join(root, scen.strip("/"), "download")):
                self.send_response(302)
                self.send_header("Location", scen + "tag/" + tag)
                self.send_header("Content-Length", "0")
                self.end_headers()
                return True
        return False

    def do_GET(self):
        if not self._latest():
            super().do_GET()

    def do_HEAD(self):
        if not self._latest():
            super().do_HEAD()

    def log_message(self, *a):
        pass

class Server(socketserver.ThreadingTCPServer):
    allow_reuse_address = True
    daemon_threads = True

srv = Server(("127.0.0.1", 0), Handler)
with open(portfile, "w") as f:
    f.write(str(srv.server_address[1]))
srv.serve_forever()
PY

python3 "$work/server.py" "$S" "$TAG" "$work/port" 2>"$work/server.err" &
server_pid=$!
# A POSIX counter, not `seq`: seq is GNU coreutils and macOS ships the BSD
# userland, where the command does not exist. The shell prints "command not
# found" into a loop that then runs zero times, so the wait passes instantly
# and the check below reports "server did not start" with no hint at the real
# cause — the failure this script's own macOS job hit.
i=0
while [ "$i" -lt 50 ]; do
  [ -s "$work/port" ] && break
  i=$((i + 1))
  sleep 0.1
done
if [ ! -s "$work/port" ]; then
  echo "test-install: fake release server did not start (waited 5s for $work/port)" >&2
  # Python's traceback goes here, so the log says why rather than repeating
  # the symptom. Redirected to a file: a background process sharing this
  # script's stderr would interleave with the test output.
  sed 's/^/test-install: server said: /' "$work/server.err" >&2 || true
  kill "$server_pid" 2>/dev/null || true
  exit 2
fi
BASE="http://127.0.0.1:$(cat "$work/port")"

# ---- harness -------------------------------------------------------------
pass=0
fails=0
n=0
ok() { pass=$((pass + 1)); printf '  ok   %s\n' "$1"; }
bad() { fails=$((fails + 1)); printf '  FAIL %s\n' "$1"; printf '%s\n' "$OUT" | sed 's/^/       | /'; }

# run <shell> <scenario> [VAR=value ...]: sets DIR, RC, OUT
run() {
  local shell_bin=$1 scen=$2
  shift 2
  n=$((n + 1))
  DIR="$work/install-$n"
  set +e
  OUT=$(env HOME="$work/home" TMPDIR="$work/tmp" OPCODE_INSTALL_DIR="$DIR" \
    OPCODE_RELEASE_BASE_URL="$BASE/$scen" "$@" "$shell_bin" "$INSTALLER" 2>&1 </dev/null)
  RC=$?
  set -e
}

installed_clean() { # exactly one file, opcode, and it is executable
  [ -x "$DIR/opcode" ] && [ "$(ls -A "$DIR" | wc -l | tr -d ' ')" = 1 ]
}
nothing_installed() { [ ! -e "$DIR" ] || [ -z "$(ls -A "$DIR")" ]; }
tmp_clean() { [ -z "$(ls -A "$work/tmp")" ]; }

expect_installed() { # <name>
  if [ "$RC" = 0 ] && installed_clean && [ "$("$DIR/opcode")" = "$GOOD_OUT" ] && tmp_clean; then ok "$1"; else bad "$1 (rc=$RC)"; fi
}
expect_refused() { # <name> <message fragment>
  if [ "$RC" != 0 ] && nothing_installed && tmp_clean && printf '%s' "$OUT" | grep -qF -- "$2"; then
    ok "$1"
  else
    bad "$1 (rc=$RC, want failure mentioning: $2)"
  fi
}

suite() {
  local sh_bin=$1
  echo "== install.sh under $sh_bin"

  run "$sh_bin" ok OPCODE_VERSION=$TAG
  expect_installed "happy path, pinned version"

  run "$sh_bin" ok
  expect_installed "happy path, latest resolved via redirect"
  local first_dir=$DIR

  # idempotent re-run into the same directory
  n=$((n + 1))
  DIR=$first_dir
  set +e
  OUT=$(env HOME="$work/home" TMPDIR="$work/tmp" OPCODE_INSTALL_DIR="$DIR" \
    OPCODE_RELEASE_BASE_URL="$BASE/ok" "$sh_bin" "$INSTALLER" 2>&1 </dev/null)
  RC=$?
  set -e
  expect_installed "re-run over an existing install"

  # the documented one-liner feeds the script on stdin
  n=$((n + 1))
  DIR="$work/install-$n"
  set +e
  OUT=$(env HOME="$work/home" TMPDIR="$work/tmp" OPCODE_INSTALL_DIR="$DIR" \
    OPCODE_RELEASE_BASE_URL="$BASE/ok" "$sh_bin" <"$INSTALLER" 2>&1)
  RC=$?
  set -e
  expect_installed "script piped on stdin (curl | sh form)"

  run "$sh_bin" tampered OPCODE_VERSION=$TAG
  expect_refused "tampered archive is refused" "checksum mismatch"

  run "$sh_bin" nosums OPCODE_VERSION=$TAG
  expect_refused "missing checksums.txt is refused" "checksums.txt"

  run "$sh_bin" noentry OPCODE_VERSION=$TAG
  expect_refused "missing checksum entry is refused" "no checksum entry"

  run "$sh_bin" substring OPCODE_VERSION=$TAG
  expect_refused "substring-only checksum entry is refused" "no checksum entry"

  run "$sh_bin" conflict OPCODE_VERSION=$TAG
  expect_refused "conflicting checksum entries are refused" "conflicting checksum"

  run "$sh_bin" nobinary OPCODE_VERSION=$TAG
  expect_refused "archive without the binary is refused" "binary missing"

  run "$sh_bin" tampered OPCODE_VERSION=$TAG OPCODE_SKIP_CHECKSUM=1
  if [ "$RC" = 0 ] && [ -x "$DIR/opcode" ] && printf '%s' "$OUT" | grep -q "NOT being verified"; then
    ok "OPCODE_SKIP_CHECKSUM=1 bypasses verification (and says so)"
  else
    bad "OPCODE_SKIP_CHECKSUM=1 bypass (rc=$RC)"
  fi

  run "$sh_bin" nosums OPCODE_VERSION=$TAG OPCODE_SKIP_CHECKSUM=1
  if [ "$RC" = 0 ] && [ -x "$DIR/opcode" ]; then ok "skip flag installs without checksums.txt"; else bad "skip without checksums.txt (rc=$RC)"; fi

  # an existing binary survives a refused install
  n=$((n + 1))
  DIR="$work/install-$n"
  mkdir -p "$DIR"
  printf 'OLD' >"$DIR/opcode"
  set +e
  OUT=$(env HOME="$work/home" TMPDIR="$work/tmp" OPCODE_INSTALL_DIR="$DIR" OPCODE_VERSION=$TAG \
    OPCODE_RELEASE_BASE_URL="$BASE/tampered" "$sh_bin" "$INSTALLER" 2>&1 </dev/null)
  RC=$?
  set -e
  if [ "$RC" != 0 ] && [ "$(cat "$DIR/opcode")" = OLD ] && [ "$(ls -A "$DIR" | wc -l | tr -d ' ')" = 1 ]; then
    ok "refused install leaves the existing binary untouched"
  else
    bad "existing binary after refused install (rc=$RC)"
  fi

  run "$sh_bin" ok OPCODE_VERSION=v0.0.1
  expect_refused "unknown version: download failure, nothing installed" "download failed"

  run "$sh_bin" missing
  expect_refused "latest cannot be resolved: clear error" "could not resolve the latest release"

  run "$sh_bin" ok 'OPCODE_VERSION=v1.0.0/../evil'
  expect_refused "tag with path characters is rejected" "unexpected"

  run "$sh_bin" ok OPCODE_VERSION=latest-ish
  expect_refused "non-version tag is rejected" "unexpected release tag"

  run "$sh_bin" ok OPCODE_VERSION=$TAG "PATH=$work/fakebin:$PATH" FAKE_UNAME_S=Plan9
  expect_refused "unsupported OS message" "no prebuilt opcode for Plan9"

  run "$sh_bin" ok OPCODE_VERSION=$TAG "PATH=$work/fakebin:$PATH" FAKE_UNAME_M=riscv64
  expect_refused "unsupported architecture message" "no prebuilt opcode for riscv64"

  run "$sh_bin" ok OPCODE_VERSION=$TAG "PATH=$work/fakebin:$PATH" FAKE_UNAME_S=MINGW64_NT-10.0
  expect_refused "windows shell gets a pointer, not a broken install" "does not support Windows"

  # --help answers without touching the network and installs nothing.
  n=$((n + 1))
  DIR="$work/install-$n"
  set +e
  OUT=$(env HOME="$work/home" TMPDIR="$work/tmp" OPCODE_INSTALL_DIR="$DIR" \
    OPCODE_RELEASE_BASE_URL="$BASE/ok" "$sh_bin" "$INSTALLER" --help 2>&1 </dev/null)
  RC=$?
  set -e
  if [ "$RC" = 0 ] && nothing_installed && tmp_clean && printf '%s' "$OUT" | grep -q "Usage: install.sh"; then
    ok "explicit --help, no network, nothing installed"
  else
    bad "--help (rc=$RC)"
  fi

  # A stray argument is a paste mistake, not an install.
  n=$((n + 1))
  DIR="$work/install-$n"
  set +e
  OUT=$(env HOME="$work/home" TMPDIR="$work/tmp" OPCODE_INSTALL_DIR="$DIR" \
    OPCODE_RELEASE_BASE_URL="$BASE/ok" "$sh_bin" "$INSTALLER" --bogus 2>&1 </dev/null)
  RC=$?
  set -e
  if [ "$RC" != 0 ] && nothing_installed && tmp_clean && printf '%s' "$OUT" | grep -q "takes no arguments"; then
    ok "stray argument is refused, nothing installed"
  else
    bad "stray argument (rc=$RC)"
  fi

  # Plaintext release bases are refused anywhere but loopback: the
  # loopback $BASE the suite itself uses keeps working (every case
  # above proves it); a LAN mirror does not.
  n=$((n + 1))
  DIR="$work/install-$n"
  set +e
  OUT=$(env HOME="$work/home" TMPDIR="$work/tmp" OPCODE_INSTALL_DIR="$DIR" \
    OPCODE_RELEASE_BASE_URL="http://192.0.2.1/releases" "$sh_bin" "$INSTALLER" 2>&1 </dev/null)
  RC=$?
  set -e
  if [ "$RC" != 0 ] && nothing_installed && tmp_clean && printf '%s' "$OUT" | grep -q "refusing plaintext"; then
    ok "plaintext non-loopback base is refused"
  else
    bad "plaintext base (rc=$RC)"
  fi
}

# uname shim: only consulted when a run puts fakebin first on PATH.
real_uname=$(command -v uname)
cat >"$work/fakebin/uname" <<SH
#!/bin/sh
case "\$1" in
  -s) if [ -n "\${FAKE_UNAME_S:-}" ]; then echo "\$FAKE_UNAME_S"; else "$real_uname" -s; fi ;;
  -m) if [ -n "\${FAKE_UNAME_M:-}" ]; then echo "\$FAKE_UNAME_M"; else "$real_uname" -m; fi ;;
  *) "$real_uname" "\$@" ;;
esac
SH
chmod 755 "$work/fakebin/uname"

case "$("$real_uname" -s)/$("$real_uname" -m)" in
  Linux/x86_64|Linux/aarch64|Linux/amd64|Darwin/x86_64|Darwin/arm64) ;;
  *) echo "test-install: host platform has no prebuilt opcode; cannot run" >&2; exit 2 ;;
esac

echo "== syntax"
for sh_bin in sh bash; do
  command -v "$sh_bin" >/dev/null 2>&1 || continue
  if "$sh_bin" -n "$INSTALLER"; then ok "$sh_bin -n install.sh"; else OUT=; bad "$sh_bin -n install.sh"; fi
done
if command -v shellcheck >/dev/null 2>&1; then
  if shellcheck "$INSTALLER"; then ok "shellcheck install.sh"; else OUT=; bad "shellcheck install.sh"; fi
else
  echo "  skip shellcheck (not installed)"
fi

for sh_bin in sh bash; do
  command -v "$sh_bin" >/dev/null 2>&1 && suite "$sh_bin"
done

echo
echo "test-install: $pass passed, $fails failed"
[ "$fails" = 0 ]
