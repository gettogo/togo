#!/usr/bin/env bash
# Copies a freshly built togo to a host over ssh and checks that
# "togo serve --stdio" answers hello and session.list there.
# Usage: scripts/smoke-ssh.sh [host]   (default: localhost)
# Not part of "make test": it needs working non-interactive ssh to the host.
set -euo pipefail

host="${1:-localhost}"
case "$host" in -*) echo "smoke-ssh: invalid host: $host" >&2; exit 1 ;; esac
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ssh_opts=(-o BatchMode=yes -o ConnectTimeout=5)
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

die() { echo "smoke-ssh: $*" >&2; exit 1; }

# Build for the host's platform, not this machine's.
target="$(ssh -n "${ssh_opts[@]}" -- "$host" 'uname -s; uname -m')" ||
  die "cannot ssh to $host non-interactively (check host key, keys and Remote Login)"
{ read -r os; read -r machine; } <<<"$target"
case "$os" in Darwin) goos=darwin ;; Linux) goos=linux ;; *) die "unsupported host OS: $os" ;; esac
case "$machine" in
  arm64 | aarch64) goarch=arm64 ;;
  x86_64 | amd64) goarch=amd64 ;;
  *) die "unsupported host architecture: $machine" ;;
esac

echo "building togo for $goos/$goarch"
(cd "$root/helper" && GOOS="$goos" GOARCH="$goarch" go build -o "$tmp/togo" ./cmd/togo) || die "build failed"
(cd "$root/helper" && go build -o "$tmp/framecat" ./test/framecat) || die "framecat build failed"

# umask keeps ~/.togo at 0700 when this creates it. The binary lands under a
# temporary name and is renamed, so a running helper is not overwritten
# in place (text file busy on Linux, stale code signature on macOS).
ssh -n "${ssh_opts[@]}" -- "$host" 'umask 077; mkdir -p ~/.togo/bin' || die "mkdir on $host failed"
scp "${ssh_opts[@]}" -q -- "$tmp/togo" "$host:.togo/bin/togo.new" || die "scp to $host failed"
ssh -n "${ssh_opts[@]}" -- "$host" 'mv ~/.togo/bin/togo.new ~/.togo/bin/togo' || die "install on $host failed"

requests='{"jsonrpc":"2.0","id":1,"method":"hello","params":{"protocol":1,"client":{"name":"smoke-ssh","version":"0"},"capabilities":[]}}
{"jsonrpc":"2.0","id":2,"method":"session.list","params":{}}'

# The remote helper stops when its stdin closes, which printf ends after the
# last request; pipefail turns a failure in any stage into a non-zero exit.
responses="$(printf '%s\n' "$requests" | "$tmp/framecat" |
  ssh "${ssh_opts[@]}" -- "$host" '~/.togo/bin/togo serve --stdio' |
  "$tmp/framecat" -decode)" || die "serve over ssh failed"

echo "$responses"
[ "$(printf '%s\n' "$responses" | grep -c '"result"')" -eq 2 ] || die "expected two results"
printf '%s\n' "$responses" | grep -q '"error"' && die "helper returned an error"
printf '%s\n' "$responses" | head -n 1 | grep -q '"protocol":1' || die "hello did not report protocol 1"
exit 0
