#!/usr/bin/env bash
# Replays the recorded helper event streams (protocol/v1/replays/*.jsonl)
# through the app's state (TogoKit: feed, timeline, fold, mission, monitor)
# and through the helper's push sender, and checks the invariants. Unit
# tests only: no simulator, no network. Run before every install.
#
#   scripts/check-replay.sh        # helper + TogoKit, about 15 s once built
#   scripts/check-replay.sh --app  # also HarnessSessionStore in TogoTests (an iOS
#                                  # test build, queued through $TOGO_BUILD_LOCK; minutes)
#
# Known product bugs are expected failures (ReplayKnownIssues.swift,
# replayKnown in helper/internal/push/replay_test.go): they keep the run
# green while they reproduce and turn it red once fixed.
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
start=$SECONDS

echo "== helper: push decisions and Live Activity (go test)"
(cd "$root/helper" && go test ./internal/push/ ./internal/replay/ -run 'TestReplay|TestScrub|TestLeaks' -count=1)

echo "== app state: TogoKit replays (swift test)"
cd "$root/packages/TogoKit"
swift build --build-tests -q 2>&1 | grep -E "error:" && exit 1
out="$(swift test --skip-build --filter 'ReplayTests|ReplayScenarioTests|ReplayEdgeTests' 2>&1)" || {
  echo "$out" | grep -E "✘|↳" | grep -v "known issue"
  echo "replay checks FAILED"
  exit 1
}
echo "$out" | grep -E "^(✔|━) Test run" || true
echo "$out" | grep -cE "recorded a known issue" | xargs -I{} echo "{} known product issues reproduced (see ReplayKnownIssues.swift)"
if [[ "${1:-}" == "--app" ]]; then
  echo "== app store: StoreReplayTests (xcodebuild)"
  # TOGO_BUILD_LOCK names a wrapper that queues heavy builds (`<lock> <label> -- <cmd>`).
  lock="${TOGO_BUILD_LOCK:-}"
  run=(xcodebuild -project "$root/apps/ios/Togo.xcodeproj" -scheme Togo -derivedDataPath "$root/apps/ios/DerivedData"
    -skipPackagePluginValidation -skipMacroValidation -quiet -configuration Debug
    -destination 'platform=iOS Simulator,name=iPhone 17' -only-testing:TogoTests/StoreReplayTests test)
  (cd "$root" && make ios-gen >/dev/null)
  if [[ -n "$lock" && -x "$lock" ]]; then "$lock" check-replay -- "${run[@]}"; else "${run[@]}"; fi
fi
echo "replay checks passed in $((SECONDS - start))s"
