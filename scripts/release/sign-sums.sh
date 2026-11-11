#!/usr/bin/env bash
# Sign <dist>/SHA256SUMS with minisign and verify the signature. Prints the
# commands unless --run is given.
#
#   TOGO_MINISIGN_KEY  path to the minisign secret key (normally
#                      ~/.togo-keys/minisign.key); required with --run
#   TOGO_MINISIGN_PUB  public key to verify with instead of the pinned
#                      release key; only for test keys
#   TOGO_RELEASE_TEST  set to 1 to allow signing while the test key is pinned
#
# usage: sign-sums.sh <dist> <version> [--run]
set -euo pipefail

die() { printf 'sign-sums: %s\n' "$*" >&2; exit 1; }
usage() { printf 'usage: sign-sums.sh <dist> <version> [--run]\n' >&2; exit 2; }

mode="dry"
args=()
for arg in "$@"; do
  case "$arg" in
    --run) mode="run" ;;
    -*) usage ;;
    *) args+=("$arg") ;;
  esac
done
[ "${#args[@]}" -eq 2 ] || usage
dist="${args[0]}"
version="${args[1]}"
[[ "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]] || die "version must look like v1.2.3, got $version"

repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

# A release must never be signed while the test key (ID 9AA09F4C20C5559E, stored
# as these bytes) is pinned in place of the release key. Refuse to sign with it.
TEST_KEY_BYTES="9e55c5204c9fa09a"
pinned_is_test_key() {
  local id
  id="$(sed -n 2p "$1/helper/internal/release/minisign.pub" | tr -d '\r' | base64 -d 2>/dev/null | od -An -tx1 -j2 -N8 | tr -d ' \n')"
  [ "$id" = "$TEST_KEY_BYTES" ]
}
check_pinned_key() {
  if pinned_is_test_key "$1" && [ "${TOGO_RELEASE_TEST:-}" != "1" ]; then
    die "the pinned release key is the test key; pin the owner's key in helper/internal/release/minisign.pub (TOGO_RELEASE_TEST=1 only for tests)"
  fi
}

run() {
  if [ "$mode" = "run" ]; then
    "$@"
  else
    printf '%q ' "$@"; printf '\n'
  fi
}

key="${TOGO_MINISIGN_KEY:-}"
pub="${TOGO_MINISIGN_PUB:-}"
if [ "$mode" = "run" ]; then
  check_pinned_key "$repo"
  [ -n "$key" ] || die "set TOGO_MINISIGN_KEY to the minisign secret key path (normally ~/.togo-keys/minisign.key)"
  [ -f "$key" ] || die "TOGO_MINISIGN_KEY does not name a file"
  [ -z "$pub" ] || [ -f "$pub" ] || die "TOGO_MINISIGN_PUB does not name a file"
  command -v minisign >/dev/null || die "minisign is not installed (brew install minisign)"
  [ -f "$dist/SHA256SUMS" ] || die "no SHA256SUMS in $dist"
  dist="$(cd "$dist" && pwd)"
else
  key="${key:-<TOGO_MINISIGN_KEY>}"
fi

verify=(go run ./cmd/togo-release verify --version "$version")
[ -z "$pub" ] || verify+=(--pub "$pub")
verify+=("$dist")

run minisign -S -s "$key" -m "$dist/SHA256SUMS" -t "togo $version SHA256SUMS"
if [ "$mode" = "run" ]; then
  (cd "$repo/helper" && "${verify[@]}")
else
  printf '(cd %q && ' "$repo/helper"; printf '%q ' "${verify[@]}"; printf ')\n'
fi
