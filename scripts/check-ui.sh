#!/bin/sh
# The presentation smoke flows: about ten XCUITests over a replayed helper, asserting on
# elements and on the presentation coordinator's probe (never on pixels). One simulator, one
# build; portrait checks compact width and landscape regular width on the same iPhone.
#
# Usage: scripts/check-ui.sh                                   build, then run every flow
#        scripts/check-ui.sh testSendShowsWorkingThenClears    one flow
#        SIM="iPhone 18 Pro Max" scripts/check-ui.sh
# Every xcodebuild goes through the build lock when it exists, so parallel agents queue.
set -eu
cd "$(dirname "$0")/.."

SIM=${SIM:-"iPhone 18 Pro Max"}
DERIVED=apps/ios/DerivedData
PROJECT=apps/ios/Togo.xcodeproj
RESULT="$DERIVED/check-ui.xcresult"
LOCK="${TOGO_BUILD_LOCK:-}"
only="TogoUITests/PresentationFlowsUITests"
[ -n "${1:-}" ] && only="$only/$1"

locked() {
    if [ -x "$LOCK" ]; then "$LOCK" check-ui -- "$@"; else "$@"; fi
}

udid=$(xcrun simctl list devices available | grep -F "    $SIM (" | head -1 | sed -E 's/.*\(([0-9A-F-]{36})\).*/\1/')
if [ -z "$udid" ]; then
    echo "check-ui: no simulator named \"$SIM\" (xcrun simctl list devices)" >&2
    exit 2
fi

start=$(date +%s)
(cd apps/ios && xcodegen generate --quiet)
rm -rf "$RESULT"

# One lock for build and run; the simulator is shut down after, whatever happened.
status=0
locked env PROJECT="$PROJECT" DERIVED="$DERIVED" RESULT="$RESULT" udid="$udid" only="$only" bash -c '
    set -o pipefail
    xcrun simctl boot "$udid" 2>/dev/null || true
    xcodebuild -project "$PROJECT" -scheme TogoUITests -derivedDataPath "$DERIVED" \
        -skipPackagePluginValidation -skipMacroValidation -quiet -configuration Debug \
        -destination "id=$udid" -parallel-testing-enabled NO -resultBundlePath "$RESULT" \
        -only-testing:"$only" test 2>&1 \
        | grep -v -e IDELaunchParametersSnapshot -e IDETestOperationsObserverDebug -e "warning:" -e "note:" -e "^ *[0-9]* |" -e "^ *|"
    code=$?
    xcrun simctl shutdown "$udid" 2>/dev/null || true
    exit $code
' || status=$?

# Each flow with its time, and every failure with its message.
if [ -d "$RESULT" ]; then
    xcrun xcresulttool get test-results tests --path "$RESULT" > "$DERIVED/check-ui.json" 2>/dev/null || true
    /usr/bin/python3 -I - "$DERIVED/check-ui.json" <<'PY' || true
import json, sys
def walk(node):
    if node.get("nodeType") == "Test Case":
        print("  %-8s %7s  %s" % (node.get("result", "?"), node.get("duration", ""), node["name"]))
        for child in node.get("children", []):
            if child.get("nodeType") == "Failure Message":
                print("           " + child["name"])
    for child in node.get("children", []):
        walk(child)
try:
    nodes = json.load(open(sys.argv[1])).get("testNodes", [])
except ValueError:
    nodes = []
for node in nodes:
    walk(node)
PY
fi

if [ "$status" = 0 ]; then verdict=passed; else verdict=FAILED; fi
echo "check-ui: $verdict in $(( $(date +%s) - start ))s on $SIM"
exit "$status"
