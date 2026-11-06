#!/usr/bin/env bash
# Fails when app code animates without a motion role.
# Usage: scripts/check-motion.sh [root]   (default apps/ios/Togo)
#
# Every animation goes through Motion.animation(_:reduce:), .motion(_:value:) or, in UIKit,
# Motion.animator(_:reduce:animations:), so each change moves the way its role says and honours
# Reduce Motion. Only Design/Motion*.swift may name curves or make animators.
# scripts/motion-allowlist.txt must stay empty: any entry in it fails the check.
#
# The rules check themselves first against scripts/motion-lint/flagged.txt (every line must be
# flagged) and allowed.txt (none may be). TogoTests/MotionPassTests.swift runs the same rules
# against the same files on the simulator; change both together.
set -euo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
repo="$(dirname "$here")"
root="${1:-$repo/apps/ios/Togo}"
allowlist="$here/motion-allowlist.txt"
fixtures="$here/motion-lint"

if [ ! -d "$root" ]; then
	echo "check-motion: no such directory: $root" >&2
	exit 2
fi
root="$(cd "$root" && pwd)"

if [ -f "$allowlist" ] && grep -v -e '^#' -e '^[[:space:]]*$' "$allowlist" >/dev/null; then
	echo "check-motion: $allowlist must be empty; move these lines to a motion role:"
	grep -v -e '^#' -e '^[[:space:]]*$' "$allowlist"
	exit 1
fi

# Prints "<file>:<line>: <source>" for every line that animates outside a role.
rules='
function strip(text, word,    at) {
	while ((at = index(text, word)) > 0) {
		text = substr(text, 1, at - 1) substr(text, at + length(word))
	}
	return text
}
{
	line = $0
	sub(/^[ \t]+/, "", line)
	sub(/[ \t]+$/, "", line)
	if (line ~ /^\/\// || line ~ /^#/ || line == "") next
	hit = 0
	# A fallback after a role call brings back a curve, or an animation where the role has none.
	if (index(line, "Motion.animation(") && index(line, "??")) hit = 1
	# Assignments take nil, a role, or nil-or-role.
	if (match(line, /\.animation[ \t]*=[^=]/)) {
		rhs = substr(line, RSTART + RLENGTH - 1)
		sub(/^[ \t]+/, "", rhs)
		if (rhs !~ /^nil/ && rhs !~ /^Motion\.animation\(/ && rhs !~ /^[A-Za-z_.]+ \? nil : Motion\.animation\(/) hit = 1
	}
	probe = line
	probe = strip(probe, "withAnimation(Motion.animation(")
	probe = strip(probe, ".animation(Motion.animation(")
	probe = strip(probe, "Motion.animation(")
	probe = strip(probe, "Transaction(animation: nil)")
	probe = strip(probe, ".animation(nil")
	n = split("withAnimation|.animation(|Transaction(animation:|UIView.animate|UIView.transition(|" \
		"PropertyAnimator|phaseAnimator|keyframeAnimator|PhaseAnimator|KeyframeAnimator|" \
		"CABasicAnimation|CASpringAnimation|CAKeyframeAnimation|CATransition|CAAnimationGroup|" \
		".spring(|.easeIn|.easeOut|.linear(|.bouncy|.smooth|.snappy|.interactiveSpring|" \
		"UnitCurve.|Spring(|.speed(|repeatForever|.repeatCount(", banned, "|")
	for (i = 1; i <= n; i++) if (index(probe, banned[i])) hit = 1
	if (!hit) next
	path = FILENAME
	sub(/^\.\//, "", path)
	printf "%s:%d: %s\n", path, FNR, line
}'

# The rules first prove they still catch what they must and nothing they must not.
expected="$(grep -c -v -e '^#' -e '^[[:space:]]*$' "$fixtures/flagged.txt")"
caught="$(awk "$rules" "$fixtures/flagged.txt" | wc -l | tr -d ' ')"
if [ "$caught" != "$expected" ]; then
	echo "check-motion: the rules flag $caught of $expected lines in $fixtures/flagged.txt; missed:" >&2
	grep -n -v -e '^#' -e '^[[:space:]]*$' "$fixtures/flagged.txt" |
		awk -F: 'NR == FNR { seen[$2] = 1; next } !($1 in seen)' <(awk "$rules" "$fixtures/flagged.txt") - >&2
	exit 2
fi
wrong="$(awk "$rules" "$fixtures/allowed.txt")"
if [ -n "$wrong" ]; then
	echo "check-motion: the rules flag lines in $fixtures/allowed.txt:" >&2
	echo "$wrong" >&2
	exit 2
fi

found="$(
	cd "$root"
	find . -name '*.swift' ! -path './Design/Motion*.swift' -print0 | sort -z | xargs -0 awk "$rules"
)"

if [ -n "$found" ]; then
	echo "Animations must use a motion role (Motion.animation(_:reduce:), .motion(_:value:) or Motion.animator(_:reduce:animations:)):"
	echo "$found"
	exit 1
fi
