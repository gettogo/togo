#!/usr/bin/env bash
# Reject commits in a rev range that carry AI/LLM attribution trailers.
# Usage: check-trailers.sh <rev-range>
# Exits 1 if any commit message contains a forbidden trailer line, else 0.
set -euo pipefail

if [ "$#" -ne 1 ] || [ -z "$1" ]; then
	echo "usage: $(basename "$0") <rev-range>" >&2
	exit 1
fi

# grep exits 1 on no match; the if-condition keeps set -e from treating
# the clean case as a failure.
if matches=$(git log --format=%B "$1" | grep -inE '^(co-authored-by|claude-session):|generated with'); then
	printf '%s\n' "$matches"
	exit 1
fi
exit 0
