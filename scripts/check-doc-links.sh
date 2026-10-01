#!/usr/bin/env bash
# Checks every relative link in every tracked markdown file.
#
#   scripts/check-doc-links.sh
#
# Exits non-zero and prints one line per broken link. Absolute URLs
# (http/https/mailto) and pure anchors (#foo) are skipped: this is a
# typo detector for paths, not a link rot monitor — a link that 404s
# on GitHub is a different check with different failure modes.
#
# Untracked markdown is included on purpose: a new doc is wrong in the
# same way as an old one, and the point is to catch it before the
# commit rather than after. The .git and local references/ trees (a
# scratch dir of third-party screenshots) are skipped.

set -eu

ROOT=$(cd "$(dirname "$0")/.." && pwd)
cd "$ROOT"

# Collect the files with find, not `git ls-files`, so this works in a
# tarball as well as a checkout.
files=$(find . -name '*.md' \
  -not -path './.git/*' \
  -not -path './references/*' | sort)

broken=0
checked=0

while IFS= read -r f; do
  [ -n "$f" ] || continue
  dir=$(dirname "$f")
  # Only markdown link targets: ](target), ignoring the image form
  # because a missing image is a missing image, not a broken link.
  while IFS= read -r target; do
    case "$target" in
      ''|'#'*|http://*|https://*|mailto:*) continue ;;
    esac
    # Drop any #anchor: the file's existence is what matters here.
    target=${target%%#*}
    [ -n "$target" ] || continue
    checked=$((checked + 1))
    if [ ! -e "$dir/$target" ]; then
      echo "broken link: $f -> $target"
      broken=$((broken + 1))
    fi
  done <<EOF
$(grep -oE '\]\([^)]+\)' "$f" | sed 's/^](//; s/)$//')
EOF
done <<EOF
$files
EOF

if [ "$broken" -gt 0 ]; then
  echo "check-doc-links: $broken broken link(s) of $checked checked"
  exit 1
fi
echo "check-doc-links: $checked relative links, all resolve"
