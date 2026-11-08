#!/bin/sh
# Harness conformance: standard scenarios on real, muted sessions of every installed coding
# agent (create, sign-in reaching Needs you, a first turn, working set and cleared, a model
# switch that the next turn really uses, mode and effort changes that hold, an approval
# answered by its request id, interrupt, a typed slash command, restore after the tmux server
# dies, close). Prints a harness x scenario table; exits 1 on any FAIL.
#
# The helper is built from this tree into a temporary folder; ~/.togo/bin/togo is not touched.
# Each agent gets its own TOGO_HOME and tmux server, so no session reaches the phone and the
# owner's sessions are never seen. Agent config files are snapshotted first; one is put back
# only when every change to it came during the suite's own run (current copy backed up first),
# otherwise it is left and reported.
#
# Usage: scripts/check-conformance.sh [--only claude,omp] [--scenario model_switch,mode]
#                                     [--jobs N] [--json results.json] [--keep]
set -eu
cd "$(dirname "$0")/.."
repo=$(pwd)

only="" scenario="" jobs="" json="" keep=0
while [ $# -gt 0 ]; do
    case "$1" in
        --only) only="$2"; shift 2 ;;
        --scenario) scenario="$2"; shift 2 ;;
        --jobs) jobs="$2"; shift 2 ;;
        --json) json="$2"; shift 2 ;;
        --keep) keep=1; shift ;;
        -h|--help) sed -n '2,15p' "$0"; exit 0 ;;
        *) echo "unknown argument: $1" >&2; exit 2 ;;
    esac
done

export PATH="/opt/homebrew/bin:/usr/local/bin:$PATH"
command -v tmux >/dev/null || { echo "tmux is required" >&2; exit 2; }

# Parallelism from free memory: 4 with plenty, 3 normally, 2 under pressure.
if [ -z "$jobs" ]; then
    free=$(memory_pressure 2>/dev/null | sed -n 's/.*free percentage: \([0-9]*\)%.*/\1/p')
    free=${free:-50}
    if [ "$free" -ge 45 ]; then jobs=4; elif [ "$free" -ge 20 ]; then jobs=3; else jobs=2; fi
fi

run_id="tcr$(od -An -N4 -tx1 /dev/urandom | tr -d ' \n')"
root="/private/tmp/togo-conformance-$run_id"
mkdir -p "$root/bin"
bin="$root/bin"

cleanup() {
    status=$?
    if [ -x "$bin/togo-conformance" ]; then
        "$bin/togo-conformance" -cleanup -root "$root" -run-id "$run_id" >/dev/null 2>&1 || true
    fi
    # Belt and braces: any tmux server of this run.
    for s in /private/tmp/tmux-"$(id -u)"/tc-"$run_id"-*; do
        [ -S "$s" ] && tmux -L "$(basename "$s")" kill-server 2>/dev/null || true
    done
    if [ "$keep" = 1 ]; then
        echo "kept $root" >&2
    else
        rm -rf "$root"
    fi
    exit $status
}
trap cleanup EXIT
trap 'exit 130' INT TERM

echo "building helper from $repo/helper" >&2
(cd helper && go build -o "$bin/togo" ./cmd/togo && go build -o "$bin/togo-conformance" ./cmd/togo-conformance)

set -- -bin "$bin/togo" -adapters "$repo/helper/internal/harness/adapters" -root "$root" -run-id "$run_id" -jobs "$jobs"
[ -n "$only" ] && set -- "$@" -only "$only"
[ -n "$scenario" ] && set -- "$@" -scenario "$scenario"
[ -n "$json" ] && set -- "$@" -json "$json"
"$bin/togo-conformance" "$@"
