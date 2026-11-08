#!/usr/bin/env bash
# Checks the public repository documents: relative links resolve, every
# `togo <command>` the README shows is a real helper command, and the copy
# keeps to the house rules (no internal words, no workers.dev hosts, no em
# dashes, no banned words).
# Usage: check-repo-docs.sh [root]   (root defaults to the repository)
set -uo pipefail

repo=$(cd "$(dirname "$0")/.." && pwd)
root=${1:-$repo}
root=$(cd "$root" && pwd) || exit 2

docs=()
for f in README.md CONTRIBUTING.md CODE_OF_CONDUCT.md SECURITY.md .github/PULL_REQUEST_TEMPLATE.md; do
  [ -f "$root/$f" ] && docs+=("$root/$f")
done
while IFS= read -r f; do docs+=("$f"); done < <(
  find "$root/docs" "$root/.github/ISSUE_TEMPLATE" "$root/.github/DISCUSSION_TEMPLATE" \
    -type f \( -name '*.md' -o -name '*.yml' \) 2>/dev/null | sort)

fail=0
problem() { echo "check-repo-docs: $*" >&2; fail=1; }

for f in README.md CONTRIBUTING.md CODE_OF_CONDUCT.md SECURITY.md docs/architecture.md docs/security.md; do
  [ -f "$root/$f" ] || problem "missing $f"
done

[ ${#docs[@]} -eq 0 ] && exit 1

# Relative links: [text](target) where target is not a URL or an anchor.
for f in "${docs[@]}"; do
  dir=$(dirname "$f")
  while IFS= read -r target; do
    path=${target%%#*}
    [ -z "$path" ] && continue
    case "$path" in http://*|https://*|mailto:*) continue ;; esac
    [ -e "$dir/$path" ] || problem "${f#"$root"/}: dead link $target"
  done < <(grep -oE '\]\([^) ]+\)' "$f" | sed -E 's/^\]\((.*)\)$/\1/')
done

# Commands the README shows must be registered by the helper.
if [ -f "$root/README.md" ]; then
  known=$(grep -ho 'register("[a-z-]*"' "$repo"/helper/cmd/togo/*.go | sed -E 's/register\("(.*)"/\1/' | sort -u)
  for cmd in $(grep -oE '(^|[^a-zA-Z/.-])togo [a-z][a-z-]*' "$root/README.md" | awk '{print $NF}' | sort -u); do
    echo "$known" | grep -qx "$cmd" || problem "README.md: togo $cmd is not a helper command"
  done
fi

# Internal words: the word list is kept outside this repository. Point
# TOGO_INTERNAL_WORDS at a file of extended regular expressions, one per
# line; without it the check is skipped.
if [ -n "${TOGO_INTERNAL_WORDS:-}" ] && [ -f "$TOGO_INTERNAL_WORDS" ]; then
  grep -inIE -f "$TOGO_INTERNAL_WORDS" "${docs[@]}" >&2 && problem "internal words found"
else
  echo "check-repo-docs: TOGO_INTERNAL_WORDS not set, internal word check skipped" >&2
fi

grep -nI 'workers\.dev' "${docs[@]}" >&2 && problem "workers.dev host named"
grep -nI $'\xe2\x80\x94' "${docs[@]}" >&2 && problem "em dash found"
grep -inIE '\b(robust|seamless(ly)?|leverag(e|es|ed|ing)|comprehensive|elegant(ly)?|delve|ensure that|blazing|game-?changer)\b' \
  "${docs[@]}" >&2 && problem "banned word found"

[ "$fail" -eq 0 ] && echo "repo docs ok"
exit "$fail"
