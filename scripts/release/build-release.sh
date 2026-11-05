#!/usr/bin/env bash
# Build, sign and checksum a helper release into dist/. Prints the whole
# command chain unless --run is given; nothing here publishes anything.
#
#   --skip-apple   skip Developer ID signing and notarization (machines
#                  without the identity); darwin binaries stay unsigned
#
# Environment: TOGO_SIGN_NAME (sign-darwin.sh), TOGO_NOTARY_PROFILE
# (notarize.sh), TOGO_MINISIGN_KEY and TOGO_MINISIGN_PUB (sign-sums.sh),
# TOGO_RELEASE_TEST=1 to allow a run while the test key is pinned.
#
# usage: build-release.sh <version> [--run] [--skip-apple]
set -euo pipefail

die() { printf 'build-release: %s\n' "$*" >&2; exit 1; }
usage() { printf 'usage: build-release.sh <version> [--run] [--skip-apple]\n' >&2; exit 2; }

mode="dry"
apple="yes"
version=""
for arg in "$@"; do
  case "$arg" in
    --run) mode="run" ;;
    --skip-apple) apple="no" ;;
    -*) usage ;;
    *) [ -z "$version" ] || usage; version="$arg" ;;
  esac
done
[ -n "$version" ] || usage
[[ "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]] || die "version must look like v1.2.3, got $version"

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo="$(cd "$here/../.." && pwd)"

# The pinned key is still the test key (ID 9AA09F4C20C5559E, stored as these
# bytes) until the owner's release key replaces it. Refuse to sign with it.
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
dist="$repo/dist"
plain="${version#v}"
runflag=()
[ "$mode" = "dry" ] || runflag=(--run)

# release runs togo-release from the helper module.
release() {
  if [ "$mode" = "run" ]; then
    (cd "$repo/helper" && go run ./cmd/togo-release "$@")
  else
    printf '(cd %q && go run ./cmd/togo-release ' "$repo/helper"; printf '%q ' "$@"; printf ')\n'
  fi
}

step() { printf '\n# %s\n' "$*"; }

if [ "$mode" = "run" ]; then
  check_pinned_key "$repo"
elif pinned_is_test_key "$repo"; then
  printf '# note: the pinned release key is the test key; --run refuses unless TOGO_RELEASE_TEST=1\n'
fi

step "build and archive every target"
release build --version "$version" --out "$dist"

if [ "$apple" = "yes" ]; then
  for arch in arm64 amd64; do
    archive="$dist/togo_${plain}_darwin_${arch}.tar.gz"
    step "sign and notarize darwin/$arch"
    if [ "$mode" = "run" ]; then
      [ -f "$archive" ] || die "missing $archive"
      work="$(mktemp -d)"
      tar -xzf "$archive" -C "$work" togo
    else
      work="<tmpdir-darwin-$arch>"
      printf '%q ' tar -xzf "$archive" -C "$work" togo; printf '\n'
    fi
    "$here/sign-darwin.sh" "$work/togo" ${runflag[@]+"${runflag[@]}"}
    "$here/notarize.sh" "$work/togo" ${runflag[@]+"${runflag[@]}"}
    release archive --version "$version" --target "darwin/$arch" --bin "$work/togo" --license "$repo/LICENSE" --out "$dist"
    if [ "$mode" = "run" ]; then
      find "$work" -mindepth 1 -delete
      rmdir "$work"
    fi
  done
else
  step "skipping Developer ID signing and notarization (--skip-apple)"
fi

# install.sh is a release asset too, so SHA256SUMS and its signature cover it.
# Its embedded key must be the pinned one or it would refuse this release.
pinned_key="$(sed -n 2p "$repo/helper/internal/release/minisign.pub" | tr -d '\r')"
grep -qx "RELEASE_KEY='$pinned_key'" "$repo/scripts/install.sh" ||
  die "scripts/install.sh RELEASE_KEY differs from helper/internal/release/minisign.pub"

step "add install.sh and checksum the release assets"
if [ "$mode" = "run" ]; then
  cp "$repo/scripts/install.sh" "$dist/install.sh"
else
  printf '%q ' cp "$repo/scripts/install.sh" "$dist/install.sh"; printf '\n'
fi
release sums "$dist"

step "sign and verify SHA256SUMS"
"$here/sign-sums.sh" "$dist" "$version" ${runflag[@]+"${runflag[@]}"}
