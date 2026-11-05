#!/bin/sh
# The accent marks only what needs the user: the "Needs you" status word, the approve button
# and the beacon in the launch mark. Any other use of it fails this check.
set -eu
cd "$(dirname "$0")/.."

hits=$(grep -rn --include='*.swift' 'togoAccent' apps/ios/Togo \
    | grep -v -e '^apps/ios/Togo/Design/Palette.swift:' \
        -e '/StatusWord.swift:' -e '/RequestCardView.swift:' -e '/ApproveButton.swift:' \
        -e '^apps/ios/Togo/Launch/' || true)

if [ -n "$hits" ]; then
    echo "togoAccent is reserved for things that need the user. Found it in:" >&2
    echo "$hits" >&2
    exit 1
fi
