#!/usr/bin/env bash
# Normalizes the Info.plist of every embedded framework App Store Connect would reject:
# Blink's prebuilt mosh.framework ships MinimumOSVersion "ios_version_min" and a four-part
# CFBundleShortVersionString "1.4.0.1845". The Togo target runs this after "Embed
# Frameworks" and before the app itself is signed; it re-signs each framework it touched.
# That binary carries no dSYM, so the upload's "Upload Symbols Failed" warning for
# mosh.framework is expected and harmless.
set -euo pipefail

frameworks="${TARGET_BUILD_DIR:?}/${FRAMEWORKS_FOLDER_PATH:?}"
min="${IPHONEOS_DEPLOYMENT_TARGET:-26.0}"
pb=/usr/libexec/PlistBuddy
test -d "$frameworks" || exit 0

for fw in "$frameworks"/*.framework; do
  plist="$fw/Info.plist"
  test -f "$plist" || continue
  changed=0
  current_min=$($pb -c 'Print :MinimumOSVersion' "$plist" 2>/dev/null || true)
  if ! [[ "$current_min" =~ ^[0-9]+(\.[0-9]+){0,2}$ ]]; then
    $pb -c "Delete :MinimumOSVersion" "$plist" 2>/dev/null || true
    $pb -c "Add :MinimumOSVersion string $min" "$plist"
    changed=1
  fi
  short=$($pb -c 'Print :CFBundleShortVersionString' "$plist" 2>/dev/null || true)
  if ! [[ "$short" =~ ^[0-9]+(\.[0-9]+){0,2}$ ]]; then
    fixed=$(echo "$short" | grep -oE '^[0-9]+(\.[0-9]+){0,2}' || echo "1.0.0")
    $pb -c "Set :CFBundleShortVersionString $fixed" "$plist"
    changed=1
  fi
  build=$($pb -c 'Print :CFBundleVersion' "$plist" 2>/dev/null || true)
  if ! [[ "$build" =~ ^[0-9]+(\.[0-9]+){0,2}$ ]]; then
    $pb -c "Set :CFBundleVersion 1" "$plist"
    changed=1
  fi
  if [ "$changed" = 1 ] && [ "${CODE_SIGNING_ALLOWED:-NO}" = "YES" ] && [ -n "${EXPANDED_CODE_SIGN_IDENTITY:-}" ]; then
    codesign --force --sign "$EXPANDED_CODE_SIGN_IDENTITY" --timestamp=none \
      ${OTHER_CODE_SIGN_FLAGS:-} "$fw"
  fi
done
