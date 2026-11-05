#!/usr/bin/env bash
# Deploy the relay Worker. With --secrets, also set any missing APNs secrets
# from TOGO_APNS_KEY_FILE, TOGO_APNS_KEY_ID and TOGO_APNS_TEAM_ID. Secret
# values are never printed.
set -euo pipefail

with_secrets=0
for arg in "$@"; do
  case "$arg" in
    --secrets) with_secrets=1 ;;
    *) echo "usage: $0 [--secrets]" >&2; exit 2 ;;
  esac
done

root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"
if [ -n "$(git status --porcelain relay/)" ]; then
  echo "relay/ has uncommitted changes; commit or stash them first" >&2
  exit 1
fi

if [ "$with_secrets" = 1 ]; then
  : "${TOGO_APNS_KEY_FILE:?set TOGO_APNS_KEY_FILE}"
  : "${TOGO_APNS_KEY_ID:?set TOGO_APNS_KEY_ID}"
  : "${TOGO_APNS_TEAM_ID:?set TOGO_APNS_TEAM_ID}"
  [ -r "$TOGO_APNS_KEY_FILE" ] || { echo "TOGO_APNS_KEY_FILE is not readable" >&2; exit 1; }
fi

cd relay
npm ci
npm test
npx tsc --noEmit

out="$(npx wrangler deploy 2>&1)" || { printf '%s\n' "$out" >&2; exit 1; }
printf '%s\n' "$out"
url="$(printf '%s\n' "$out" | grep -Eo 'https://[A-Za-z0-9.-]+\.workers\.dev' | head -n 1)"
[ -n "$url" ] || { echo "could not find the deployed URL in wrangler output" >&2; exit 1; }
echo "deployed: $url"
if [ "$with_secrets" = 1 ]; then
  # The worker exists after the deploy above, so a failed list is an error.
  names="$(npx wrangler secret list --format json | node -e '
    let raw = "";
    process.stdin.on("data", (c) => (raw += c));
    process.stdin.on("end", () => {
      for (const s of JSON.parse(raw)) console.log(s.name);
    });
  ')"
  has_secret() { printf '%s\n' "$names" | grep -qx "$1"; }
  has_secret APNS_KEY || npx wrangler secret put APNS_KEY < "$TOGO_APNS_KEY_FILE" > /dev/null
  has_secret APNS_KEY_ID || printf %s "$TOGO_APNS_KEY_ID" | npx wrangler secret put APNS_KEY_ID > /dev/null
  has_secret APNS_TEAM_ID || printf %s "$TOGO_APNS_TEAM_ID" | npx wrangler secret put APNS_TEAM_ID > /dev/null
fi

curl -fsS "$url/v1/health"
echo
