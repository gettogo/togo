#!/usr/bin/env bash
# Submit a signed macOS binary to Apple's notary service and wait for the
# verdict. Fails on any status other than Accepted. Prints the commands
# unless --run is given.
#
#   TOGO_NOTARY_PROFILE  notarytool keychain profile (default togo-notary)
#
# usage: notarize.sh <binary> [--run]
set -euo pipefail

die() { printf 'notarize: %s\n' "$*" >&2; exit 1; }
usage() { printf 'usage: notarize.sh <binary> [--run]\n' >&2; exit 2; }

mode="dry"
bin=""
for arg in "$@"; do
  case "$arg" in
    --run) mode="run" ;;
    -*) usage ;;
    *) [ -z "$bin" ] || usage; bin="$arg" ;;
  esac
done
[ -n "$bin" ] || usage

profile="${TOGO_NOTARY_PROFILE:-togo-notary}"
case "$profile" in
  ''|-*|*[!A-Za-z0-9._-]*) die "TOGO_NOTARY_PROFILE must be a plain profile name" ;;
esac

if [ "$mode" = "dry" ]; then
  zip="<tmpdir>/$(basename "$bin").zip"
  printf '%q ' ditto -c -k --keepParent "$bin" "$zip"; printf '\n'
  printf '%q ' xcrun notarytool submit "$zip" --keychain-profile "$profile" --wait --output-format json; printf '\n'
  printf '# fails unless the submission status is Accepted\n'
  exit 0
fi

[ -f "$bin" ] || die "no such binary: $bin"
[ "$(uname -s)" = "Darwin" ] || die "notarytool needs macOS"

work="$(mktemp -d)"
trap 'find "$work" -mindepth 1 -delete; rmdir "$work"' EXIT
zip="$work/$(basename "$bin").zip"
ditto -c -k --keepParent "$bin" "$zip"

result="$work/result.json"
# notarytool exits nonzero for some failures; the status check below decides.
xcrun notarytool submit "$zip" --keychain-profile "$profile" --wait --output-format json >"$result" || true
status="$(plutil -extract status raw -o - "$result" 2>/dev/null || true)"
id="$(plutil -extract id raw -o - "$result" 2>/dev/null || true)"

if [ "$status" != "Accepted" ]; then
  printf 'notarize: status %s for %s\n' "${status:-unknown}" "$bin" >&2
  if [ -n "$id" ]; then
    xcrun notarytool log "$id" --keychain-profile "$profile" >&2 || true
  fi
  exit 1
fi
printf 'notarize: %s accepted (%s)\n' "$bin" "$id"
