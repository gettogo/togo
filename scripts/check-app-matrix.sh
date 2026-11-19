#!/usr/bin/env bash
# Compare app coverage debt with a base commit that has the baseline.
# An optional JSON artifact supplies executed UI assertion results.
set -euo pipefail
if (( $# < 1 || $# > 2 )); then
    echo 'usage: check-app-matrix.sh BASE [PROOF.json]' >&2
    exit 2
fi
proof=
if (( $# == 2 )); then
    case ${2:?provide the path to the proof artifact} in
        /*) proof=$2 ;;
        *) proof=$PWD/$2 ;;
    esac
fi
cd "$(dirname "$0")/.."
base=$(git rev-parse --verify --end-of-options "$1^{commit}")
head=$(git rev-parse --verify HEAD)
path=helper/internal/daemon/testdata/app_missing
scratch=$(mktemp -d)
trap 'rm -rf -- "$scratch"' EXIT
git archive "$base" "$path" | tar -xf - -C "$scratch"
cd helper
TOGO_APP_DEBT_BASE="$scratch/$path" TOGO_APP_DEBT_PROOF="$proof" TOGO_APP_DEBT_REVISION="$head" \
    go test ./internal/daemon -run '^TestAppMatrixDebt$' -count=1
