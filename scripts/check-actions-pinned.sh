#!/usr/bin/env bash
# Fail when a workflow step uses an action that is not pinned to a full
# 40-character commit SHA. With --resolved, also fail on the all-zero
# placeholder SHA, which marks a pin that has not been looked up yet.
# usage: check-actions-pinned.sh [--resolved] [workflow-dir]
set -euo pipefail

resolved=0
if [ "${1:-}" = "--resolved" ]; then resolved=1; shift; fi
dir="${1:-.github/workflows}"

status=0
shopt -s nullglob
for f in "$dir"/*.yml "$dir"/*.yaml; do
  while IFS= read -r hit; do
    line="${hit%%:*}"
    text="${hit#*:}"
    ref="${text#*uses:}"
    ref="${ref#"${ref%%[![:space:]]*}"}"
    ref="${ref%%[[:space:]]*}"
    case "$ref" in ./*|docker://*) continue ;; esac
    sha="${ref##*@}"
    if [ "$sha" = "$ref" ] || ! [[ "$sha" =~ ^[0-9a-f]{40}$ ]]; then
      printf '%s:%s: %s is not pinned to a commit SHA\n' "$f" "$line" "$ref" >&2
      status=1
    elif [ "$resolved" = 1 ] && [ "$sha" = "0000000000000000000000000000000000000000" ]; then
      printf '%s:%s: %s still has the placeholder SHA\n' "$f" "$line" "$ref" >&2
      status=1
    elif ! [[ "$text" =~ \#[[:space:]]*v?[0-9] ]]; then
      printf '%s:%s: %s has no trailing version comment\n' "$f" "$line" "$ref" >&2
      status=1
    fi
  done < <(grep -nE '^[[:space:]-]*uses:' "$f" || true)
done
exit "$status"
