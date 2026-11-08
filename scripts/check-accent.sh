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
        -e '^apps/ios/Togo/Harness/Mission/MissionPill.swift:' -e '^apps/ios/Togo/Harness/Chat/WorkLine.swift:' -e '^apps/ios/Togo/Harness/Chat/BeaconFill.swift:' -e '^apps/ios/Togo/Harness/Chat/LiveLine.swift:' \
        -e '^apps/ios/Togo/Launch/' \
        -e '^apps/ios/TogoWidgets/LiveLockScreenView.swift:' -e '^apps/ios/TogoWidgets/IslandViews.swift:' \
        -e '^apps/ios/TogoWidgets/WaitingWidget.swift:' \
        -e '^apps/ios/TogoWatch/RequestDetailView.swift:' -e '^apps/ios/TogoWatch/RequestsView.swift:' \
        -e '^apps/ios/TogoWatchWidgets/WaitingComplication.swift:' || true)

status=0
if [ -n "$hits" ]; then
    echo "togoAccent is reserved for things that need the user. Found it in:" >&2
    echo "$hits" >&2
    status=1
fi

# Inside the files allowed above, every use must also say on its line, or within the six lines
# above it, what needs the user: needs-you, waiting, the beacon, lit, approve or a request; or
# carry an "accent-ok:" comment naming why. A new use in an allowed file is judged by its line,
# not waved through by its file.
unmarked=$(grep -rl --include='*.swift' 'togoAccent' \
        apps/ios/Togo apps/ios/TogoWidgets apps/ios/TogoWatch apps/ios/TogoWatchWidgets 2>/dev/null \
    | grep -v -e '/Design/Palette.swift$' | sort | while read -r f; do
        awk -v file="$f" '
            { line[NR] = $0 }
            /togoAccent/ {
                ok = 0
                for (i = NR; i >= NR - 6 && i > 0; i--)
                    if (line[i] ~ /[Nn]eeds|[Ww]aiting|[Ll]antern|(^|[^A-Za-z])lit([^A-Za-z]|$)|[Aa]pprove|[Rr]equest|accent-ok:/) ok = 1
                if (!ok) print file ":" NR ": " $0
            }
        ' "$f"
    done)
if [ -n "$unmarked" ]; then
    echo "togoAccent here does not say what needs the user (needs-you, waiting, beacon, lit, approve, request) or why (accent-ok:):" >&2
    echo "$unmarked" >&2
    status=1
fi

# System-drawn surfaces take the app's accent: without a global one they fall back to system
# blue, which reads as the beacon.
if ! grep -q 'ASSETCATALOG_COMPILER_GLOBAL_ACCENT_COLOR_NAME' apps/ios/project.yml; then
    echo "apps/ios/project.yml sets no ASSETCATALOG_COMPILER_GLOBAL_ACCENT_COLOR_NAME: system surfaces turn system blue." >&2
    status=1
fi

# No other blue stands in for it.
blue=$(grep -rnE --include='*.swift' 'Color\.accentColor|\.accentColor\(|Color\.blue\b|\.systemBlue|\.tint\(\.blue\)' \
    apps/ios/Togo apps/ios/TogoWidgets apps/ios/TogoWatch apps/ios/TogoWatchWidgets 2>/dev/null | grep -v '/Preview/' || true)
if [ -n "$blue" ]; then
    echo "A system blue reads as the beacon; use the palette:" >&2
    echo "$blue" >&2
    status=1
fi

# A UIKit text view that finds links draws them, and its handles, in its tint: set it.
for f in $(grep -rl --include='*.swift' 'dataDetectorTypes' apps/ios/Togo 2>/dev/null | xargs grep -l 'UITextView' 2>/dev/null); do
    if ! grep -q 'linkTextAttributes\|tintColor' "$f"; then
        echo "$f: finds links with dataDetectorTypes but sets no tintColor/linkTextAttributes (system blue)." >&2
        status=1
    fi
done
exit "$status"
