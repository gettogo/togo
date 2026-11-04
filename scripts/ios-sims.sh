#!/usr/bin/env bash
# Creates the simulators the iOS targets test on. Safe to run repeatedly:
# a device is only created when no device of that name exists on the runtime
# chosen for it.
set -euo pipefail

devices=(
  "iPhone 17|com.apple.CoreSimulator.SimDeviceType.iPhone-17"
  "iPhone Duo|com.apple.CoreSimulator.SimDeviceType.iPhone-Duo"
  "iPad Pro 11-inch (M5)|com.apple.CoreSimulator.SimDeviceType.iPad-Pro-11-inch-M5-12GB"
)

runtimes_json=$(xcrun simctl list runtimes -j)
devices_json=$(xcrun simctl list devices -j)

# Prints the iOS 27.x runtime id that supports the device type. Device-specific
# runtimes (the iPhone Duo ships as iOS-27-2-iPhone19-4) only list their own
# device types, so the general runtime wins whenever it supports the type.
runtime_for() {
  /usr/bin/python3 -I -c '
import json, re, sys
device_type = sys.argv[1]
runtimes = [
    r for r in json.loads(sys.argv[2])["runtimes"]
    if r.get("isAvailable") and r.get("platform") == "iOS" and r["version"].startswith("27.")
]
if device_type == "":
    sys.exit(0 if runtimes else 1)
matches = [
    r for r in runtimes
    if any(t["identifier"] == device_type for t in r.get("supportedDeviceTypes", []))
]
general = re.compile(r"\.iOS-\d+-\d+$")
matches.sort(key=lambda r: (general.search(r["identifier"]) is None,
                            [-int(p) for p in r["version"].split(".")]))
if not matches:
    sys.exit(1)
print(matches[0]["identifier"])
' "$1" "$runtimes_json"
}

device_exists() {
  /usr/bin/python3 -I -c '
import json, sys
name, runtime = sys.argv[1], sys.argv[2]
devices = json.loads(sys.argv[3])["devices"].get(runtime, [])
sys.exit(0 if any(d["name"] == name and d.get("isAvailable") for d in devices) else 1)
' "$1" "$2" "$devices_json"
}

if ! runtime_for "" >/dev/null; then
  echo "ios-sims: no available iOS 27.x simulator runtime" >&2
  echo "run: xcodebuild -downloadPlatform iOS" >&2
  exit 1
fi

created=0
for entry in "${devices[@]}"; do
  name=${entry%%|*}
  type=${entry#*|}
  if ! runtime=$(runtime_for "$type"); then
    echo "ios-sims: no iOS 27.x runtime supports $type" >&2
    echo "run: xcodebuild -downloadPlatform iOS" >&2
    exit 1
  fi
  if device_exists "$name" "$runtime"; then
    echo "exists:  $name on $runtime"
    continue
  fi
  udid=$(xcrun simctl create "$name" "$type" "$runtime")
  echo "created: $name ($udid) on $runtime"
  created=$((created + 1))
done
echo "ios-sims: created $created device(s)"
