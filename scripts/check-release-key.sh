#!/usr/bin/env bash
# Fails while the helper's pinned update key is still the test key, so no Release build of
# the app ships beside a helper whose updates nobody can sign. The Togo target runs this
# for the Release configuration; TOGO_ALLOW_TEST_RELEASE_KEY=YES skips it for checks that
# only inspect the binary (make ios-release-check).
#
# usage: check-release-key.sh [path to minisign.pub]
set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
pub=${1:-"$root/helper/internal/release/minisign.pub"}
# Key ID 9AA09F4C20C5559E (helper/internal/release TestKeyID), as minisign stores it.
test_key_bytes="9e55c5204c9fa09a"

test -f "$pub" || { echo "error: check-release-key: $pub not found" >&2; exit 1; }
id=$(sed -n 2p "$pub" | tr -d '\r' | base64 -d 2>/dev/null | od -An -tx1 -j2 -N8 | tr -d ' \n')
if [ -z "$id" ]; then
  echo "error: check-release-key: $pub is not a minisign public key" >&2
  exit 1
fi
if [ "$id" = "$test_key_bytes" ]; then
  echo "error: the helper's pinned update key is the test key (9AA09F4C20C5559E). Pin the release key in helper/internal/release/minisign.pub before building Release." >&2
  exit 1
fi
echo "check-release-key: pinned key is not the test key"
