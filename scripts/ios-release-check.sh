#!/usr/bin/env bash
# Fails when a built app still carries the debug host screen: any
# DebugEntry/DebugRootView symbol, or any file from Togo/Debug/Resources.
# Usage: ios-release-check.sh <path to Togo.app>
# Run on a Debug build it must fail; that is how the gate is proven to look.
set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
debug_resources="$root/apps/ios/Togo/Debug/Resources"
app=${1:?usage: ios-release-check.sh <Togo.app>}
binary="$app/Togo"
test -f "$binary" || { echo "ios-release-check: $binary not found" >&2; exit 1; }

# Debug simulator builds keep the code in Togo.debug.dylib next to a stub
# executable, so both are scanned when present.
images=("$binary")
if [ -f "$app/Togo.debug.dylib" ]; then
  images+=("$app/Togo.debug.dylib")
fi

failed=0
symbols=0
for image in "${images[@]}"; do
  # Captured first so an nm failure stops the script instead of reading as 0.
  table=$(nm "$image")
  if [ -z "$table" ]; then
    echo "ios-release-check: nm printed no symbols for $image" >&2
    exit 1
  fi
  found=$(printf '%s\n' "$table" | grep -cE 'Debug(Entry|RootView)' || true)
  symbols=$((symbols + found))
done
echo "$symbols"
if [ "$symbols" != "0" ]; then
  echo "ios-release-check: $symbols debug symbol(s) present" >&2
  failed=1
fi

# Xcode copies resources flat into the bundle root, so a leaked
# Debug/Resources/x.json shows up as Togo.app/x.json. An empty source folder
# would make this check vacuous, hence the hard failure.
shopt -s nullglob
resources=("$debug_resources"/*)
if [ "${#resources[@]}" -eq 0 ]; then
  echo "ios-release-check: $debug_resources is empty; nothing to check against" >&2
  exit 1
fi
for resource in "${resources[@]}"; do
  name=$(basename "$resource")
  if [ -e "$app/$name" ]; then
    echo "ios-release-check: debug resource $name present in $app" >&2
    failed=1
  fi
done

exit "$failed"
