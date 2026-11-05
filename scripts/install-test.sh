#!/usr/bin/env bash
# End-to-end tests for scripts/install.sh against signed releases served from
# 127.0.0.1. Builds a fake togo for the host, packs it into release archives
# with togo-release, signs SHA256SUMS in minisign format with throwaway
# Ed25519 keys (via OpenSSL 3), then runs every case in scripts/install-test/
# cases.sh under each POSIX shell found: sh, dash, busybox sh, bash --posix.
#
# Needs go, python3 and OpenSSL 3. Optional: TOGO_INSTALL_TEST_MINISIGN, a
# minisign binary for the minisign case (else minisign from PATH, else skipped).
# TOGO_INSTALL_TEST_KEEP=1 keeps the work directory; TOGO_INSTALL_TEST_SHELLS
# limits the shells, e.g. "sh dash".
#
# usage: scripts/install-test.sh [case ...]
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo="$(cd "$here/.." && pwd)"
INSTALL_SH="${TOGO_INSTALL_TEST_SCRIPT:-$repo/scripts/install.sh}"

die() { printf 'install-test: %s\n' "$*" >&2; exit 1; }

command -v go >/dev/null || die "needs go"
PYTHON="$(command -v python3)" || die "needs python3"

openssl3() {
  local c
  for c in "$(command -v openssl || true)" /opt/homebrew/opt/openssl@3/bin/openssl /usr/local/opt/openssl@3/bin/openssl; do
    [ -n "$c" ] && [ -x "$c" ] || continue
    case "$("$c" version 2>/dev/null)" in "OpenSSL "[3-9].*) printf '%s\n' "$c"; return 0 ;; esac
  done
  return 1
}
OPENSSL="$(openssl3)" || die "needs OpenSSL 3 (macOS: brew install openssl@3)"

W="$(mktemp -d)"
W="$(cd "$W" && pwd -P)"
server_pid=""
cleanup() {
  if [ -n "$server_pid" ]; then { kill "$server_pid" && wait "$server_pid"; } 2>/dev/null || true; fi
  if [ "${TOGO_INSTALL_TEST_KEEP:-}" = 1 ]; then
    printf 'install-test: kept %s\n' "$W" >&2
  else
    chmod -R u+w "$W" 2>/dev/null || true
    rm -rf "$W"
  fi
}
trap cleanup EXIT

# Host platform as install.sh maps it.
case "$(uname -s)" in Darwin) HOST_OS=darwin ;; Linux) HOST_OS=linux ;; *) die "unsupported test host $(uname -s)" ;; esac
case "$(uname -m)" in x86_64 | amd64) HOST_ARCH=amd64 ;; arm64 | aarch64) HOST_ARCH=arm64 ;; *) die "unsupported test host $(uname -m)" ;; esac
if [ "$HOST_OS" = darwin ] && [ "$(sysctl -n sysctl.proc_translated 2>/dev/null || true)" = 1 ]; then HOST_ARCH=arm64; fi
TARGETS=(darwin/arm64 darwin/amd64 linux/amd64 linux/arm64)
CODESIGN=""
if [ "$HOST_OS" = darwin ] && command -v codesign >/dev/null; then CODESIGN="$(command -v codesign)"; fi

mkdir -p "$W/tools" "$W/fake" "$W/bins" "$W/keys" "$W/rel" "$W/www" "$W/cases"

# --- tools -------------------------------------------------------------------
(cd "$repo/helper" && go build -o "$W/tools/togo-release" ./cmd/togo-release)

# A stand-in for togo: answers version and service status, records argv.
cat >"$W/fake/go.mod" <<'EOF'
module faketogo

go 1.22
EOF
cat >"$W/fake/main.go" <<'EOF'
package main

import (
	"fmt"
	"io"
	"os"
	"strings"
)

var version, platform = "v0.0.0", "none"

func main() {
	args := strings.Join(os.Args[1:], " ")
	if os.Getenv("TOGO_FAKE_READ_STDIN") == "1" {
		io.Copy(io.Discard, os.Stdin)
	}
	if p := os.Getenv("TOGO_FAKE_LOG"); p != "" {
		f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err == nil {
			fmt.Fprintln(f, args)
			f.Close()
		}
	}
	switch args {
	case "version":
		fmt.Println("togo " + version)
	case "service status":
		if os.Getenv("TOGO_FAKE_SERVICE") == "installed" {
			fmt.Println("Installed and running from /fake/togo.plist")
		} else {
			fmt.Println("Not installed")
		}
	case "daemon restart --when-idle":
		fmt.Println("Restarting once idle")
	default:
		fmt.Fprintln(os.Stderr, "fake togo "+platform+": unexpected "+args)
		os.Exit(2)
	}
}
EOF
# fake_build VERSION OS/ARCH: a host binary whose bytes name the target.
fake_build() {
  local out="$W/bins/$1_${2%/*}_${2#*/}"
  (cd "$W/fake" && GOFLAGS= CGO_ENABLED=0 go build -trimpath -o "$out" \
    -ldflags "-X main.version=$1 -X main.platform=$2" .)
  if [ -n "$CODESIGN" ]; then
    "$CODESIGN" --force --sign - --identifier app.togo.helper "$out" 2>/dev/null
  fi
  printf '%s\n' "$out"
}

# --- minisign-format signing with OpenSSL ------------------------------------
# gen_key NAME: Ed25519 key, random key ID, minisign public key in NAME.pub.
gen_key() {
  local k="$W/keys/$1"
  "$OPENSSL" genpkey -algorithm ed25519 -out "$k.pem" 2>/dev/null
  "$OPENSSL" pkey -in "$k.pem" -pubout -outform DER | tail -c 32 >"$k.raw"
  "$OPENSSL" rand 8 >"$k.id"
  { printf 'Ed'; cat "$k.id" "$k.raw"; } | "$OPENSSL" base64 -A >"$k.b64"
  printf 'untrusted comment: minisign public key\n%s\n' "$(cat "$k.b64")" >"$k.pub"
}
# sign NAME FILE COMMENT: writes FILE.minisig (prehashed ED signature).
sign() {
  local k="$W/keys/$1" t
  t="$(mktemp -d "$W/sign.XXXXXX")"
  "$OPENSSL" dgst -blake2b512 -binary "$2" >"$t/digest"
  "$OPENSSL" pkeyutl -sign -inkey "$k.pem" -rawin -in "$t/digest" -out "$t/sig"
  { cat "$t/sig"; printf '%s' "$3"; } >"$t/global.msg"
  "$OPENSSL" pkeyutl -sign -inkey "$k.pem" -rawin -in "$t/global.msg" -out "$t/global"
  printf 'untrusted comment: signature from togo test key\n%s\ntrusted comment: %s\n%s\n' \
    "$({ printf 'ED'; cat "$k.id" "$t/sig"; } | "$OPENSSL" base64 -A)" "$3" \
    "$("$OPENSSL" base64 -A <"$t/global")" >"$2.minisig"
}

# make_release VERSION KEY: archives for every target, install.sh, signed sums.
make_release() {
  local d="$W/rel/$1" t bin
  mkdir -p "$d"
  for t in "${TARGETS[@]}"; do
    bin="$(fake_build "$1" "$t")"
    "$W/tools/togo-release" archive --version "$1" --target "$t" --binary "$bin" \
      --license "$repo/LICENSE" --out "$d" --module-dir "$repo/helper" >/dev/null
  done
  cp "$repo/scripts/install.sh" "$d/install.sh"
  "$W/tools/togo-release" sums "$d" >/dev/null
  sign "$2" "$d/SHA256SUMS" "togo $1 SHA256SUMS"
}

gen_key trusted
gen_key other
make_release v1.0.0 trusted
make_release v0.9.0 trusted
TEST_KEY="$(cat "$W/keys/trusted.b64")"

# The fixtures must verify with the Go verifier the release tools use.
TOGO_RELEASE_TEST=1 "$W/tools/togo-release" verify --version v1.0.0 --pub "$W/keys/trusted.pub" "$W/rel/v1.0.0" >/dev/null ||
  die "fixture release does not verify with togo-release verify"
MINISIGN="${TOGO_INSTALL_TEST_MINISIGN:-$(command -v minisign || true)}"
if [ -n "$MINISIGN" ]; then
  (cd "$W/rel/v1.0.0" && "$MINISIGN" -Vm SHA256SUMS -P "$TEST_KEY" >/dev/null) ||
    die "fixture release does not verify with $MINISIGN"
fi

# The script's pinned key must be the one the release tools pin.
pinned="$(sed -n 2p "$repo/helper/internal/release/minisign.pub" | tr -d '\r')"
grep -qx "RELEASE_KEY='$pinned'" "$repo/scripts/install.sh" ||
  die "install.sh RELEASE_KEY differs from helper/internal/release/minisign.pub"

# --- server ------------------------------------------------------------------
"$PYTHON" -I "$here/install-test/serve.py" "$W/www" "$W/port" &
server_pid=$!
for _ in $(seq 1 100); do [ -s "$W/port" ] && break; sleep 0.05; done
[ -s "$W/port" ] || die "test server did not start"
PORT="$(cat "$W/port")"

# --- shells ------------------------------------------------------------------
SHELLS=()
want_shells="${TOGO_INSTALL_TEST_SHELLS:-sh dash busybox bash}"
missing() { printf 'install-test: %s not found, skipped\n' "$1"; }
for s in $want_shells; do
  case "$s" in
    sh) SHELLS+=("/bin/sh") ;;
    dash) if command -v dash >/dev/null; then SHELLS+=("$(command -v dash)"); else missing dash; fi ;;
    bash) if command -v bash >/dev/null; then SHELLS+=("$(command -v bash) --posix"); else missing bash; fi ;;
    busybox) if command -v busybox >/dev/null; then SHELLS+=("$(command -v busybox) sh"); else missing busybox; fi ;;
    *) die "unknown shell $s" ;;
  esac
done

# shellcheck source=install-test/cases.sh
. "$here/install-test/cases.sh"

if [ $# -gt 0 ]; then CASES=("$@"); fi
pass=0
fail=0
skip=0
for sh_cmd in "${SHELLS[@]}"; do
  read -r -a SH <<<"$sh_cmd"
  SH_TAG="$(basename "${SH[0]}")"
  for c in "${CASES[@]}"; do
    FAILED=""
    SKIPPED=""
    setup_case "$c"
    "case_$c"
    if [ -n "$SKIPPED" ]; then
      skip=$((skip + 1))
      printf 'skip  %-6s %s (%s)\n' "$SH_TAG" "$c" "$SKIPPED"
    elif [ -n "$FAILED" ]; then
      fail=$((fail + 1))
      printf 'FAIL  %-6s %s\n%s' "$SH_TAG" "$c" "$FAILED"
      printf '      stdout: %s\n' "$(tr '\n' '|' <"$C/out")"
      printf '      stderr: %s\n' "$(tr '\n' '|' <"$C/err")"
    else
      pass=$((pass + 1))
      printf 'ok    %-6s %s\n' "$SH_TAG" "$c"
    fi
  done
done
printf 'install-test: %d passed, %d failed, %d skipped (%d shells)\n' "$pass" "$fail" "$skip" "${#SHELLS[@]}"
[ "$fail" = 0 ]
