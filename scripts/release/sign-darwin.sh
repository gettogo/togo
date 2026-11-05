#!/usr/bin/env bash
# Sign a macOS helper binary with the Developer ID Application identity and
# check the result. Prints the commands unless --run is given.
#
#   TOGO_SIGN_NAME   name in "Developer ID Application: <name> (WULW83YF8M)";
#                    required with --run. The identity lives in the login keychain.
#
# usage: sign-darwin.sh <binary> [--run]
set -euo pipefail

TEAM_ID="WULW83YF8M"
BUNDLE_ID="app.togo.helper"

die() { printf 'sign-darwin: %s\n' "$*" >&2; exit 1; }
usage() { printf 'usage: sign-darwin.sh <binary> [--run]\n' >&2; exit 2; }

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

run() {
  if [ "$mode" = "run" ]; then
    "$@"
  else
    printf '%q ' "$@"; printf '\n'
  fi
}

name="${TOGO_SIGN_NAME:-}"
if [ "$mode" = "run" ]; then
  [ -n "$name" ] || die "set TOGO_SIGN_NAME to the name on the Developer ID Application certificate"
  [ -f "$bin" ] || die "no such binary: $bin"
  [ "$(uname -s)" = "Darwin" ] || die "codesign needs macOS"
else
  name="${name:-<TOGO_SIGN_NAME>}"
fi
identity="Developer ID Application: $name ($TEAM_ID)"
# codesign reads -R as a file path unless the text starts with "=".
requirement="anchor apple generic and certificate leaf[subject.OU] = \"$TEAM_ID\""

run codesign --force --options runtime --timestamp --sign "$identity" -i "$BUNDLE_ID" "$bin"
run codesign --verify --strict --verbose=2 "$bin"
run codesign --verify --strict -R "=$requirement" "$bin"
