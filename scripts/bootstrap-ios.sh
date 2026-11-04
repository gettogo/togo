#!/usr/bin/env bash
# Prepares a Mac to build the iOS app: checks XcodeGen, creates the
# simulators, writes the signing team and generates Togo.xcodeproj.
set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
required_xcodegen=2.46.0

if ! command -v xcodegen >/dev/null; then
  echo "bootstrap-ios: xcodegen not found; run: brew install xcodegen" >&2
  exit 1
fi
installed=$(xcodegen --version | awk '{print $NF}')
if [ "$installed" != "$required_xcodegen" ]; then
  echo "bootstrap-ios: xcodegen $installed found, $required_xcodegen is the tested version" >&2
fi

"$root/scripts/ios-sims.sh"

# The team id is the certificate's OU. The parenthesised id in the identity
# name is the personal certificate id, which Xcode does not accept as a team.
signing_local="$root/apps/ios/Config/Signing.local.xcconfig"
if [ ! -f "$signing_local" ]; then
  team=$(security find-certificate -c "Apple Development" -p 2>/dev/null \
    | openssl x509 -noout -subject 2>/dev/null \
    | sed -n 's/.*OU *= *\([A-Z0-9]*\).*/\1/p')
  if [ -n "$team" ]; then
    printf 'DEVELOPMENT_TEAM = %s\n' "$team" > "$signing_local"
    echo "bootstrap-ios: wrote DEVELOPMENT_TEAM = $team"
  else
    echo "bootstrap-ios: no Apple Development certificate; device builds need $signing_local" >&2
  fi
fi

cd "$root/apps/ios" && xcodegen generate --quiet
echo "bootstrap-ios: generated apps/ios/Togo.xcodeproj"
