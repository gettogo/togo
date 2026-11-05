#!/bin/sh
# The accent marks only what needs the user: the "Needs you" status word, the approve button,
# the waiting count when it is not zero and the beacon in the launch mark. Any other use of it
# fails this check.
set -eu
cd "$(dirname "$0")/.."

hits=$(grep -rn --include='*.swift' 'togoAccent' \
        apps/ios/Togo apps/ios/TogoWidgets apps/ios/TogoWatch apps/ios/TogoWatchWidgets 2>/dev/null \
    | grep -v -e '^apps/ios/Togo/Design/Palette.swift:' \
        -e '/StatusWord.swift:' -e '/RequestCardView.swift:' -e '/ApproveButton.swift:' \
        -e '^apps/ios/Togo/Launch/' \
        -e '^apps/ios/TogoWidgets/LiveLockScreenView.swift:' -e '^apps/ios/TogoWidgets/IslandViews.swift:' \
        -e '^apps/ios/TogoWidgets/WaitingWidget.swift:' \
        -e '^apps/ios/TogoWatch/RequestDetailView.swift:' \
        -e '^apps/ios/TogoWatchWidgets/WaitingComplication.swift:' || true)

if [ -n "$hits" ]; then
    echo "togoAccent is reserved for things that need the user. Found it in:" >&2
    echo "$hits" >&2
    exit 1
fi
