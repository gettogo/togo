#!/bin/sh
# The accent marks only what needs the user: the "Needs you" status word, the approve button,
# the waiting count when it is not zero, the chat's "Waiting for you" line, the beacon in the launch mark and the prompt bar's
# beacon, request light and Yes, and the mission pill while something needs the user. Any other
# use of it fails this check.
set -eu
cd "$(dirname "$0")/.."

hits=$(grep -rn --include='*.swift' 'togoAccent' \
        apps/ios/Togo apps/ios/TogoWidgets apps/ios/TogoWatch apps/ios/TogoWatchWidgets 2>/dev/null \
    | grep -v -e '^apps/ios/Togo/Design/Palette.swift:' \
        -e '/StatusWord.swift:' -e '/Roster/RosterStatus.swift:' -e '/Roster/RosterView.swift:' -e '/Prompt/PromptBeacon.swift:' -e '/ApproveButton.swift:' \
        -e '^apps/ios/Togo/Harness/Mission/MissionPill.swift:' -e '^apps/ios/Togo/Harness/Chat/WorkLine.swift:' \
        -e '^apps/ios/Togo/Launch/' \
        -e '^apps/ios/TogoWidgets/LiveLockScreenView.swift:' -e '^apps/ios/TogoWidgets/IslandViews.swift:' \
        -e '^apps/ios/TogoWidgets/WaitingWidget.swift:' \
        -e '^apps/ios/TogoWatch/RequestDetailView.swift:' -e '^apps/ios/TogoWatch/RequestsView.swift:' \
        -e '^apps/ios/TogoWatchWidgets/WaitingComplication.swift:' || true)

if [ -n "$hits" ]; then
    echo "togoAccent is reserved for things that need the user. Found it in:" >&2
    echo "$hits" >&2
    exit 1
fi
