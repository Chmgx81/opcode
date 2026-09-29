#!/bin/sh
# tilde installer — downloads the prebuilt release binary for the
# current platform, verifies its sha256, and puts it on PATH, or points
# at go install when there is no prebuilt match. Safe to re-run.
#
#   curl -fsSL https://raw.githubusercontent.com/Chmgx81/tilde/main/install.sh | bash
#
# Environment overrides:
#   TILDE_INSTALL_DIR       where the binary lands (default ~/.local/bin)
#   TILDE_VERSION           release tag to install (default: latest)
#   TILDE_SKIP_CHECKSUM=1   do not verify the download's sha256
#                           (only if your system has no checksum tool)
#   TILDE_RELEASE_BASE_URL  releases root to download from, laid out like
#                           GitHub's: <base>/latest redirects to
#                           <base>/tag/<tag>, assets live at
#                           <base>/download/<tag>/<asset>. For mirrors
#                           and for testing (scripts/test-install.sh).
#                           Default: https://github.com/Chmgx81/tilde/releases

set -eu

REPO="Chmgx81/tilde"
INSTALL_DIR="${TILDE_INSTALL_DIR:-$HOME/.local/bin}"
WANT_VERSION="${TILDE_VERSION:-latest}"
RELEASES_URL="${TILDE_RELEASE_BASE_URL:-https://github.com/$REPO/releases}"
RELEASES_URL="${RELEASES_URL%/}"

say() { printf '%s\n' "$*"; }
die() {
  if [ -t 2 ]; then printf '\033[31m!\033[0m %s\n' "$*" >&2
  else printf '! %s\n' "$*" >&2; fi
  exit 1
}
have() { command -v "$1" >/dev/null 2>&1; }

# The installer takes no arguments: anything passed is almost
# certainly a paste mistake, and silently ignoring it would install
# something the user did not ask for.
if [ "$#" -gt 0 ]; then
  case "${1:-}" in
    -h|--help)
      echo "tilde installer — downloads the prebuilt release binary, verifies"
      echo "its sha256, and puts it on PATH (or points at go install)."
      echo ""
      echo "Usage: install.sh (no arguments; configure with environment)"
      echo ""
      echo "  TILDE_INSTALL_DIR       where the binary lands (default ~/.local/bin)"
      echo "  TILDE_VERSION           release tag to install (default: latest)"
      echo "  TILDE_SKIP_CHECKSUM=1   do not verify the download's sha256"
      exit 0
      ;;
    *)
      die "this installer takes no arguments (got \"$1\") — configure it with TILDE_INSTALL_DIR or TILDE_VERSION"
      ;;
  esac
fi

# Refuse a plaintext release base anywhere but loopback: the script
# follows redirects, and an http base would let a network attacker
# serve both the archive and its checksums.txt. Loopback http stays
# allowed so scripts/test-install.sh can serve a local fake release.
case "$RELEASES_URL" in
  https://*) ;;
  http://127.0.0.1*|http://localhost*|http://\[::1\]*) ;;
  http://*) die "refusing plaintext release base \"$RELEASES_URL\" — use https (loopback http is allowed for testing)" ;;
esac

# sha256_of <file>: the digest on stdout, or empty when no tool exists.
sha256_of() {
  if have sha256sum; then sha256sum "$1" | cut -d' ' -f1
  elif have shasum; then shasum -a 256 "$1" | cut -d' ' -f1
  else printf ''; fi
}

fetch() { # fetch <url> <dest>
  if have curl; then curl -fsSL --connect-timeout 15 "$1" -o "$2"
  elif have wget; then wget -q -T 30 "$1" -O "$2"
  else die "this installer needs curl or wget"; fi
}

# Everything temporary is removed however the script ends, including
# Ctrl-C: a half-downloaded file must never be left where PATH looks.
tmp=
stage=
cleanup() {
  [ -z "$tmp" ] || rm -rf "$tmp"
  [ -z "$stage" ] || rm -f "$stage"
}
trap cleanup EXIT
trap 'exit 1' INT TERM HUP

os=
case "$(uname -s)" in
  Linux)  os=linux ;;
  Darwin) os=darwin ;;
  MINGW*|MSYS*|CYGWIN*)
    die "this installer does not support Windows — download the .zip from https://github.com/$REPO/releases/latest, or: go install github.com/$REPO/cmd/tilde@latest" ;;
  *) die "no prebuilt tilde for $(uname -s) — install with: go install github.com/$REPO/cmd/tilde@latest" ;;
esac

arch=
case "$(uname -m)" in
  x86_64|amd64) arch=amd64 ;;
  aarch64|arm64) arch=arm64 ;;
  *) die "no prebuilt tilde for $(uname -m) — install with: go install github.com/$REPO/cmd/tilde@latest" ;;
esac

VERSION="$WANT_VERSION"
if [ "$VERSION" = "latest" ]; then
  # <releases>/latest redirects to <releases>/tag/<tag>; that is the version.
  if have curl; then
    VERSION=$(curl -fsSI --connect-timeout 15 "$RELEASES_URL/latest" |
      sed -n 's/^[Ll]ocation:.*tag\/\([A-Za-z0-9._-]*\).*/\1/p' | tr -d '\r' | sed -n '1p')
  else
    VERSION=$(wget -S --spider -T 30 "$RELEASES_URL/latest" 2>&1 |
      sed -n 's/^ *[Ll]ocation:.*tag\/\([A-Za-z0-9._-]*\).*/\1/p' | tr -d '\r' | sed -n '1p')
  fi
  [ -n "$VERSION" ] ||
    die "could not resolve the latest release — set TILDE_VERSION=vX.Y.Z (see https://github.com/$REPO/releases)"
fi
# The tag ends up in a URL path: allow only what a release tag contains.
case "$VERSION" in
  v[0-9]*) ;;
  *) die "unexpected release tag \"$VERSION\" (want something like v1.2.3)" ;;
esac
case "$VERSION" in
  *[!A-Za-z0-9._-]*) die "unexpected characters in release tag \"$VERSION\"" ;;
esac

asset="tilde-$os-$arch.tar.gz"
base="$RELEASES_URL/download/$VERSION"

tmp=$(mktemp -d "${TMPDIR:-/tmp}/tilde-install.XXXXXX") || die "mktemp failed"

say ""
say "Setting up tilde $VERSION ($os/$arch)..."
[ "$RELEASES_URL" = "https://github.com/$REPO/releases" ] || say "  (release mirror: $RELEASES_URL)"
fetch "$base/$asset" "$tmp/$asset" ||
  die "download failed — check the assets at https://github.com/$REPO/releases"

# Verify the download against the release's published checksum before
# anything from it runs (a compromised CDN or a tampered proxy must not
# turn curl|bash into code execution). A missing checksum or a mismatch
# is fatal; the opt-out is explicit.
if [ "${TILDE_SKIP_CHECKSUM:-}" = "1" ]; then
  say "  warning: TILDE_SKIP_CHECKSUM=1 — the download is NOT being verified"
else
  fetch "$base/checksums.txt" "$tmp/checksums.txt" ||
    die "could not fetch checksums.txt for $VERSION — verify manually, or set TILDE_SKIP_CHECKSUM=1 to install without verification"
  # Exact name match (awk ==, not a regex), tolerating the "*name"
  # binary-mode marker and CRLF; both sha256sum and shasum print
  # "<hex>  <name>".
  want=$(awk -v a="$asset" '{
      sub(/\r$/, "")
      n = $2
      if (substr(n, 1, 1) == "*") n = substr(n, 2)
      if (n == a) print $1
    }' "$tmp/checksums.txt" | tr '[:upper:]' '[:lower:]' | sort -u)
  [ -n "$want" ] || die "no checksum entry for $asset in checksums.txt"
  case "$want" in
    *'
'*) die "conflicting checksum entries for $asset in checksums.txt" ;;
  esac
  case "$want" in
    *[!0-9a-f]*) die "malformed checksum for $asset in checksums.txt" ;;
  esac
  [ "${#want}" -eq 64 ] || die "malformed checksum for $asset in checksums.txt"
  got=$(sha256_of "$tmp/$asset")
  if [ -z "$got" ]; then
    die "no sha256 tool on this system (sha256sum or shasum) — install one, or set TILDE_SKIP_CHECKSUM=1 to install without verification"
  fi
  [ "$got" = "$want" ] ||
    die "checksum mismatch for $asset — expected $want, got $got. The download is corrupted or tampered with; refusing to install."
fi

# Extract only the binary, by name, into its own directory.
mkdir "$tmp/x"
bin=
for name in "tilde-$os-$arch" tilde; do
  if tar -xzf "$tmp/$asset" -C "$tmp/x" "$name" 2>/dev/null &&
    [ -f "$tmp/x/$name" ] && [ ! -L "$tmp/x/$name" ]; then
    bin="$tmp/x/$name"
    break
  fi
done
[ -n "$bin" ] || die "binary missing from the archive"

# Stage next to the destination, then rename: the rename is atomic (same
# filesystem), so PATH never sees a partial binary and a running tilde
# is replaced, not overwritten. Re-running just replaces it again.
mkdir -p "$INSTALL_DIR" || die "cannot create $INSTALL_DIR (set TILDE_INSTALL_DIR to a directory you can write)"
dest="$INSTALL_DIR/tilde"
[ ! -d "$dest" ] || die "$dest is a directory — move it away and re-run"
stage="$INSTALL_DIR/.tilde.install.$$"
cp "$bin" "$stage" || die "cannot write to $INSTALL_DIR (set TILDE_INSTALL_DIR to a directory you can write)"
chmod 755 "$stage"
mv -f "$stage" "$dest" || die "could not replace $dest"
stage=

say ""
say "✓ tilde successfully installed!"
say ""
say "  Version:  $VERSION"
say "  Location: $dest"
case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *)
    say ""
    say "  $INSTALL_DIR is not on your PATH — add it with:"
    say "    export PATH=\"$INSTALL_DIR:\$PATH\""
    ;;
esac
say ""
say "Next: Run tilde --help to get started"
say "Updates: tilde tells you when a newer release exists — install it with: tilde update"
