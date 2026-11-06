#!/usr/bin/env bash
# Holds the median of a performance metric in a test result bundle to a budget. XCTest records
# the measurements of `measure(metrics:)` but does not let the test read them, so the budget is
# checked here, after the run.
# Usage: ios-perf-check.sh <bundle.xcresult> <metric identifier substring> <max median>
# A test skipped because the Mac was overloaded passes with "budget unverified", unless
# ENFORCE=1, when a skip fails too. A missing metric fails.
set -euo pipefail

bundle=${1:?usage: ios-perf-check.sh <bundle.xcresult> <metric> <max>}
metric=${2:?usage: ios-perf-check.sh <bundle.xcresult> <metric> <max>}
max=${3:?usage: ios-perf-check.sh <bundle.xcresult> <metric> <max>}

tests=$(xcrun xcresulttool get test-results tests --path "$bundle")
metrics=$(xcrun xcresulttool get test-results metrics --path "$bundle")
TESTS="$tests" METRICS="$metrics" python3 -I - "$metric" "$max" "${ENFORCE:-}" <<'PY'
import json, os, statistics, sys

metric, budget, enforce = sys.argv[1], float(sys.argv[2]), sys.argv[3] == "1"

def cases(node):
    if node.get("nodeType") == "Test Case":
        yield node
    for child in node.get("children", []):
        yield from cases(child)

def messages(node):
    for child in node.get("children", []):
        if child.get("nodeType") in ("Failure Message", "Skip Message"):
            yield child.get("name", "")
        yield from messages(child)

status = 0
for case in (c for root in json.loads(os.environ["TESTS"])["testNodes"] for c in cases(root)):
    name, result = case.get("name"), case.get("result")
    if result == "Skipped":
        why = " ".join(messages(case))
        print(f"{name}: skipped, budget unverified. {why}")
        if enforce:
            status = 1
    elif result != "Passed":
        print(f"{name}: {result}. {' '.join(messages(case))}")
        status = 1

found = False
for test in json.loads(os.environ["METRICS"]):
    for run in test.get("testRuns", []):
        for m in run.get("metrics", []):
            if metric not in m.get("identifier", ""):
                continue
            found = True
            values = m["measurements"]
            median = statistics.median(values)
            unit = m.get("unitOfMeasurement", "")
            line = f'{test["testIdentifier"]}: {m["displayName"]} median {median:.3f} {unit} of {", ".join(f"{v:.3f}" for v in values)}; budget {budget} {unit}'
            print(line)
            if median >= budget:
                print("over budget")
                status = 1

if not found and status == 0:
    skipped = any(c.get("result") == "Skipped" for root in json.loads(os.environ["TESTS"])["testNodes"] for c in cases(root))
    if not skipped:
        reported = sorted({m["identifier"].rsplit(".", 1)[-1] for t in json.loads(os.environ["METRICS"]) for r in t.get("testRuns", []) for m in r.get("metrics", [])})
        print(f"no measurement of {metric} in the result bundle (it has: {', '.join(reported) or 'nothing'}); "
              "the simulator reports scrolling duration but not hitches, so a hitch budget needs a device")
        status = 1
sys.exit(status)
PY
