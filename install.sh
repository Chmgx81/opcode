#!/bin/sh
# tilde installer — downloads the prebuilt release binary for the
# current platform and puts it on PATH, or points at go install when
# there is no prebuilt match. Safe to re-run.
#
#   curl -fsSL https://raw.githubusercontent.com/Chmgx81/tilde/main/install.sh | bash
#
# Environment overrides:
#   TILDE_INSTALL_DIR  where the binary lands (default ~/.local/bin)
#   TILDE_VERSION      release tag to install (default: latest)

set -eu

REPO="Chmgx81/tilde"
INSTALL_DIR="${TILDE_INSTALL_DIR:-$HOME/.local/bin}"
WANT_VERSION="${TILDE_VERSION:-latest}"

say() { printf '%s\n' "$*"; }
die() { printf '\033[31m!\033[0m %s\n' "$*" >&2; exit 1; }
have() { command -v "$1" >/dev/null 2>&1; }

fetch() { # fetch <url> <dest>
  if have curl; then curl -fsSL "$1" -o "$2"
  elif have wget; then wget -q "$1" -O "$2"
  else die "this installer needs curl or wget"; fi
}

os=
case "$(uname -s)" in
  Linux)  os=linux ;;
  Darwin) os=darwin ;;
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
  # The releases/latest page redirects to the tag; that is the version.
  if have curl; then
    VERSION=$(curl -fsSI "https://github.com/$REPO/releases/latest" |
      sed -n 's/^[Ll]ocation:.*tag\/\([A-Za-z0-9._-]*\).*/\1/p' | tr -d '\r')
  else
    VERSION=$(wget -S --spider "https://github.com/$REPO/releases/latest" 2>&1 |
      sed -n 's/^ *Location:.*tag\/\([A-Za-z0-9._-]*\).*/\1/p' | tr -d '\r')
  fi
  [ -n "$VERSION" ] || die "could not resolve the latest release"
fi

asset="tilde-$os-$arch.tar.gz"
url="https://github.com/$REPO/releases/download/$VERSION/$asset"

tmp=$(mktemp -d) || die "mktemp failed"
trap 'rm -rf "$tmp"' EXIT

say ""
say "Setting up tilde $VERSION ($os/$arch)..."
fetch "$url" "$tmp/$asset" ||
  die "download failed — check the assets at https://github.com/$REPO/releases"
tar -xzf "$tmp/$asset" -C "$tmp" || die "extraction failed"

bin="$tmp/tilde-$os-$arch"
[ -f "$bin" ] || bin="$tmp/tilde"
[ -f "$bin" ] || die "binary missing from the archive"

mkdir -p "$INSTALL_DIR"
dest="$INSTALL_DIR/tilde"
mv -f "$bin" "$dest"
chmod +x "$dest"

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
