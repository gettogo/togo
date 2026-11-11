#!/usr/bin/env bash
# Checks that `relay-deploy.sh` refuses without owner sign-off
# before running npm or wrangler.
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
for tool in npm npx; do
  printf '#!/bin/sh\necho "$0 $*" >> "%s/ran"\n' "$tmp" > "$tmp/$tool"
  chmod +x "$tmp/$tool"
done
for signoff in unset empty; do
  rc=0
  if [ "$signoff" = unset ]; then
    env -u TOGO_OWNER_SIGNOFF PATH="$tmp:$PATH" "$root/scripts/relay-deploy.sh" >/dev/null 2>&1 || rc=$?
  else
    env TOGO_OWNER_SIGNOFF= PATH="$tmp:$PATH" "$root/scripts/relay-deploy.sh" >/dev/null 2>&1 || rc=$?
  fi
  [ "$rc" = 2 ] || { echo "FAIL ($signoff): expected exit 2, got $rc" >&2; exit 1; }
  [ ! -e "$tmp/ran" ] || { echo "FAIL ($signoff): ran: $(cat "$tmp/ran")" >&2; exit 1; }
done
echo "relay-deploy sign-off guard ok"
