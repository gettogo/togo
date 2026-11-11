#!/bin/bash
# Records the website's hero: the real app in the simulator playing the recorded OpenCode
# session on the status page (replay-status), driven by TogoUITests/HeroCaptureUITests, screen-recorded with
# `simctl io recordVideo`. The test plays the first turn off camera and writes `ready`; the
# recording starts, this writes `go`; the test writes `done` at the end.
#
#   scripts/capture-hero.sh [out.mp4]
#
# The Simulator drops frames whenever the Mac is busy, so the whole build and capture holds the
# shared queue (build-lock.sh) once, waits until no other build or test run is going, erases and
# boots the simulator fresh, and plays the story once off the record (WARMUPS=1) before the take.
# TAKES=n records n takes and keeps the smoothest (scripts/hero-smoothness.py scores them). Builds into
# apps/ios/DerivedData-hero (DERIVED overrides); the simulator is shut down at the end.
# STILLS=<folder> instead runs testHeroStills and testHeroStillsLock once, unrecorded, and leaves
# the app's and the Lock Screen's frames the hero rests on (hero-*.png) in that folder, for the
# website's fidelity tests.
set -euo pipefail
root=$(cd "$(dirname "$0")/.." && pwd)
ios="$root/apps/ios"
out=${1:-$root/.hero-capture/hero-raw.mp4}
case $out in /*) ;; *) out="$PWD/$out" ;; esac
SIM=${SIM:-iPhone 18 Pro}
WARMUPS=${WARMUPS:-1}
TAKES=${TAKES:-1}
LOCK=${LOCK:-${TOGO_BUILD_LOCK:-}}
dd=${DERIVED:-$ios/DerivedData-hero}
STILLS=${STILLS:-}
mkdir -p "$(dirname "$out")"

if [ -z "${HERO_LOCK_HELD:-}" ]; then
  (cd "$ios" && xcodegen generate --quiet)
  HERO_LOCK_HELD=1 STILLS="$STILLS" DERIVED="$dd" exec "$LOCK" hero-capture -- "$0" "$out"
fi

# Waits until nothing else heavy runs (another agent's build or test), three checks in a row.
idle() {
  local mine=$1 quiet=0 busy
  while [ $quiet -lt 3 ]; do
    busy=$(ps -Ao pid=,args= | grep -E "xcodebuild |next build|vitest|playwright test|swift-build|swift-frontend|ld64|clang " \
      | grep -vE "grep|test-server" | awk -v m="$mine" '$1!=m' || true)
    if [ -n "$busy" ]; then
      quiet=0; echo "capture-hero: waiting for: $(echo "$busy" | cut -c1-120 | head -3)" >&2; sleep 20
    else
      quiet=$((quiet+1)); sleep 3
    fi
  done
  echo "capture-hero: idle; $(memory_pressure | tail -1)" >&2
}

xcodebuild build-for-testing -project "$ios/Togo.xcodeproj" -scheme TogoUITests \
  -configuration Debug -derivedDataPath "$dd" -destination "platform=iOS Simulator,name=$SIM" \
  -skipPackagePluginValidation -skipMacroValidation -quiet

udid=$(xcrun simctl list devices available -j | node -e '
  const d=JSON.parse(require("fs").readFileSync(0,"utf8")).devices;
  const m=Object.values(d).flat().find(x=>x.name===process.argv[1]); if(!m) process.exit(1); console.log(m.udid)' "$SIM")
run=$(ls "$dd"/Build/Products/*.xctestrun | head -1)
hand=""; rec=""
cleanup() {
  [ -n "$rec" ] && kill -INT "$rec" 2>/dev/null || true
  [ -n "$hand" ] && { mkdir -p "$(dirname "$out")/hero-log" && cp "$hand"/*.txt "$hand"/*.log "$(dirname "$out")/hero-log/" 2>/dev/null || true; rm -rf "$hand"; }
  xcrun simctl shutdown "$udid" >/dev/null 2>&1 || true
}
trap cleanup EXIT

# A fresh device: erased, booted, left to settle.
xcrun simctl shutdown "$udid" >/dev/null 2>&1 || true
xcrun simctl erase "$udid"
xcrun simctl boot "$udid"
xcrun simctl bootstatus "$udid" -b >/dev/null
xcrun simctl ui "$udid" appearance dark
xcrun simctl status_bar "$udid" override --time 9:41 --dataNetwork wifi --wifiMode active --wifiBars 3 \
  --cellularMode active --cellularBars 4 --batteryState charged --batteryLevel 100
sleep 30

if [ -n "$STILLS" ]; then
  mkdir -p "$STILLS"
  TEST_RUNNER_TOGO_HERO_DIR="$STILLS" xcodebuild test-without-building -xctestrun "$run" -destination "id=$udid" \
    -only-testing:TogoUITests/HeroCaptureUITests/testHeroStills -only-testing:TogoUITests/HeroCaptureUITests/testHeroStillsLock \
    -parallel-testing-enabled NO >"$STILLS/test.log" 2>&1 \
    || { grep -E "error|failed|XCTAssert" "$STILLS/test.log" | tail -10; }
  grep -E "Test Case .*(passed|failed)" "$STILLS/test.log" || true
  ls "$STILLS"/*.png 2>/dev/null
  exit 0
fi

# One pass of the story; records to $1.
take() {
  local file=$1 test
  idle ""
  hand=$(mktemp -d)
  # A fresh install, so the notification question comes before the recording.
  xcrun simctl uninstall "$udid" app.togo.Togo >/dev/null 2>&1 || true
  TEST_RUNNER_TOGO_HERO_DIR="$hand" xcodebuild test-without-building -xctestrun "$run" -destination "id=$udid" \
    -only-testing:TogoUITests/HeroCaptureUITests/testHero -parallel-testing-enabled NO >"$hand/test.log" 2>&1 &
  test=$!
  until [ -f "$hand/ready" ]; do
    kill -0 "$test" 2>/dev/null || { tail -30 "$hand/test.log"; echo "capture-hero: the test ended before the recording" >&2; exit 1; }
    sleep 0.2
  done
  xcrun simctl io "$udid" recordVideo --codec=h264 --force "$file" 2>/dev/null &
  rec=$!
  sleep 1.5
  touch "$hand/go"
  until [ -f "$hand/done" ] || ! kill -0 "$test" 2>/dev/null; do sleep 0.2; done
  kill -INT "$rec"; wait "$rec" || true; rec=""
  wait "$test" || { grep -E "error|failed|XCTAssert" "$hand/test.log" | tail -10; }
  grep -E "Test Case .*(passed|failed)" "$hand/test.log" || true
}

for i in $(seq 1 "$WARMUPS"); do
  take "$(dirname "$out")/hero-warmup-$i.mp4"
  rm -rf "$hand"; hand=""
  echo "capture-hero: warm-up $i done" >&2
done
best=""; bestscore=-999
for i in $(seq 1 "$TAKES"); do
  file="$(dirname "$out")/hero-take-$i.mp4"
  take "$file"
  rm -rf "$hand"; hand=""
  score=$(python3 "$root/scripts/hero-smoothness.py" "$file" | tee /dev/stderr | awk '/^score/{print $2}')
  if [ -n "$score" ] && awk -v a="$score" -v b="$bestscore" 'BEGIN{exit !(a>b)}'; then best=$file; bestscore=$score; fi
done
cp "$best" "$out"
echo "capture-hero: wrote $out (from $(basename "$best"), score $bestscore)"
