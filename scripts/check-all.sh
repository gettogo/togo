#!/bin/sh
# Every automatic check, in one run before an install: the static checks first (seconds), then
# the replays, the UI flows on one simulator, and the harness conformance suite where this
# branch has it. Each runs even when an earlier one failed; the summary names what failed.
#
# Usage: scripts/check-all.sh            everything
#        scripts/check-all.sh --static   the static checks only (motion, truncation, accent, layout)
# Builds go through the build lock ($TOGO_BUILD_LOCK) when it is set.
set -u
cd "$(dirname "$0")/.."

only_static=0
[ "${1:-}" = "--static" ] && only_static=1

failed=""
start=$(date +%s)

run() {
    name=$1; shift
    echo "== $name"
    t0=$(date +%s)
    if "$@"; then
        echo "   $name passed ($(( $(date +%s) - t0 ))s)"
    else
        echo "   $name FAILED ($(( $(date +%s) - t0 ))s)"
        failed="$failed $name"
    fi
}

run motion scripts/check-motion.sh
run truncation scripts/check-truncation.sh
run accent scripts/check-accent.sh
run layout scripts/check-layout.sh

if [ "$only_static" = 0 ]; then
    lock="${TOGO_BUILD_LOCK:-}"
    [ -x "$lock" ] && export TOGO_BUILD_LOCK="$lock"
    run replay scripts/check-replay.sh
    run ui scripts/check-ui.sh
    if [ -x scripts/check-conformance.sh ]; then
        run conformance scripts/check-conformance.sh
    else
        echo "== conformance: scripts/check-conformance.sh is not on this branch; skipped"
    fi
fi

total=$(( $(date +%s) - start ))
if [ -n "$failed" ]; then
    echo "check-all: FAILED:$failed (${total}s)"
    exit 1
fi
echo "check-all: passed (${total}s)"
