#!/usr/bin/env bash
# Compare the app matrix backlog with a base commit that has the baseline.
# Pass the merge-base revision for PR checks.
set -euo pipefail
cd "$(dirname "$0")/.."
base=$(git rev-parse --verify --end-of-options "${1:?usage: check-app-matrix.sh BASE}^{commit}")
path=helper/internal/daemon/testdata/app_missing
scratch=$(mktemp -d)
trap 'rm -rf -- "$scratch"' EXIT
git archive "$base" "$path" | tar -xf - -C "$scratch"
cd helper
TOGO_APP_DEBT_BASE="$scratch/$path" go test ./internal/daemon -run '^TestAppMatrixDebt$' -count=1
