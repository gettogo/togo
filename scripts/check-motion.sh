#!/usr/bin/env bash
# Fails when app code animates without a motion role.
# Usage: scripts/check-motion.sh [root]   (default apps/ios/Togo)
#
# A second pass checks choreography over the app, the Watch
# and the widgets: `.animation` without `value:`, a layout role (rowGrow, collapse, dropIn,
# paneFlow, sidebar, shrinkIntoTab, resize, reveal, conceal, present) animated implicitly with
# `.motion`/`.animation` instead of `withAnimation` at the state change, `withAnimation` around
# `await` or a Task, a moving `.transition` with no Reduce Motion path, and, in the Watch and
# widget targets that cannot reach Motion, a curve named outside a role.
# scripts/motion-choreography-baseline-*.txt list the offenders known when the pass was added,
# one file per fix area so parallel fixes never touch the same file; a new offender fails, and
# so does a baseline line that no longer matches (delete it). The baselines must end empty.
# The pass checks its own rules first against scripts/motion-lint/choreography-*.txt.
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

status=0
if [ -n "$found" ]; then
	echo "Animations must use a motion role (Motion.animation(_:reduce:), .motion(_:value:) or Motion.animator(_:reduce:animations:)):"
	echo "$found"
	status=1
fi

# Choreography. Prints "<file>\t<rule>\t<source>" per offence; each file is read whole so a
# call that spans lines is judged as one.
choreo='
function trim(t) { sub(/^[ \t]+/, "", t); sub(/[ \t]+$/, "", t); return t }
function depth(t,    i, c, d) {
	d = 0
	for (i = 1; i <= length(t); i++) { c = substr(t, i, 1); if (c == "(") d++; else if (c == ")") d-- }
	return d
}
function braces(t,    i, c, d) {
	d = 0
	for (i = 1; i <= length(t); i++) { c = substr(t, i, 1); if (c == "{") d++; else if (c == "}") d-- }
	return d
}
function report(i, rule) { printf "%s\t%s\t%s\n", path, rule, trim(src[i]) }
function judge(    i, j, line, rest, at, joined, d, b, body, hit) {
	for (i = 1; i <= n; i++) {
		line = src[i]
		if (trim(line) ~ /^\/\// || trim(line) ~ /^#/) continue
		# 1. .animation( without value: (the deprecated form animates every change below it).
		rest = line
		while ((at = index(rest, ".animation(")) > 0) {
			before = substr(rest, 1, at - 1)
			rest = substr(rest, at + 11)
			if (before ~ /Motion$/ || before ~ /TimelineView\($/) continue
			joined = rest; d = depth("(" rest); j = i
			while (d > 0 && j < i + 6 && j < n) { j++; joined = joined " " src[j]; d = depth("(" joined) }
			if (index(joined, "value:") == 0) { report(i, "animation-without-value"); break }
		}
		# 2. A layout role animated implicitly: its parents and the scroll position do not move
		#    with it, so the container and its content fall out of step.
		if (line ~ /\.motion\(\.(rowGrow|collapse|dropIn|paneFlow|sidebar|shrinkIntoTab|resize|reveal|conceal|present)[,)]/ ||
			line ~ /\.animation\(Motion\.animation\(\.(rowGrow|collapse|dropIn|paneFlow|sidebar|shrinkIntoTab|resize|reveal|conceal|present)[,)]/)
			report(i, "implicit-layout-role")
		# 3. withAnimation around network work: the animation ends long before the result lands.
		if (index(line, "withAnimation(")) {
			body = substr(line, index(line, "withAnimation("))
			b = braces(body); j = i
			while (b > 0 && j < i + 30 && j < n) { j++; body = body "\n" src[j]; b = braces(body) }
			if (body ~ /await[ \t]/ || body ~ /Task[ \t]*[\({]/) report(i, "animation-around-async")
		}
		# 4. A transition that moves, with no Reduce Motion path on the line.
		if (index(line, ".transition(") && line ~ /\.(move|offset|scale|slide|push)[\(( ,)]|\.slide\)|\.scale\)/ &&
			line !~ /[Rr]educe/)
			report(i, "moving-transition-without-reduce-motion")
		# 5. Targets without Motion (Watch, widgets) still use the roles curves, never their own.
		if (bare && line ~ /\.animation\(\.(default|snappy|smooth|bouncy|spring|easeIn|easeOut|easeInOut|linear|interactiveSpring)/)
			report(i, "curve-outside-role")
	}
}
FNR == 1 && NR > 1 { judge(); n = 0 }
FNR == 1 { path = FILENAME; sub(/^\.\//, "", path); bare = (path ~ /^(TogoWatch|TogoWidgets|TogoWatchWidgets)\//) }
{ src[++n] = $0 }
END { if (n > 0) judge() }'

cfix="$fixtures/choreography-flagged.txt"
cok="$fixtures/choreography-allowed.txt"
if [ -f "$cfix" ] && [ -f "$cok" ]; then
	expected="$(grep -c -e '^// expect' "$cfix")"
	caught="$(awk "$choreo" "$cfix" | wc -l | tr -d ' ')"
	if [ "$caught" != "$expected" ]; then
		echo "check-motion: the choreography rules flag $caught of $expected cases in $cfix:" >&2
		awk "$choreo" "$cfix" >&2
		exit 2
	fi
	wrong="$(awk "$choreo" "$cok")"
	if [ -n "$wrong" ]; then
		echo "check-motion: the choreography rules flag lines in $cok:" >&2
		echo "$wrong" >&2
		exit 2
	fi
fi

# The app and the Watch and widget targets beside it.
apps="$(dirname "$root")"
choreo_found="$(
	cd "$apps"
	dirs="$(basename "$root")"
	for extra in TogoWatch TogoWidgets TogoWatchWidgets; do [ -d "$extra" ] && dirs="$dirs $extra"; done
	# shellcheck disable=SC2086
	find $dirs -name '*.swift' ! -path '*/Design/Motion*.swift' -print0 | sort -z | xargs -0 awk "$choreo" | sort -u
)"
known="$(cat "$here"/motion-choreography-baseline-*.txt 2>/dev/null | grep -v -e '^#' -e '^[[:space:]]*$' | sort -u || true)"
new="$(comm -23 <(echo "$choreo_found" | sed '/^$/d') <(echo "$known" | sed '/^$/d'))"
stale="$(comm -13 <(echo "$choreo_found" | sed '/^$/d') <(echo "$known" | sed '/^$/d'))"
if [ -n "$new" ]; then
	echo "Motion choreography (see the motion spec): new offences; fix them, do not add them to a baseline:"
	echo "$new" | awk -F '\t' '{ printf "%s: %s: %s\n", $1, $2, $3 }'
	status=1
fi
if [ -n "$stale" ]; then
	echo "check-motion: these baseline lines (scripts/motion-choreography-baseline-*.txt) no longer match anything; delete them:"
	echo "$stale" | awk -F '\t' '{ printf "%s: %s: %s\n", $1, $2, $3 }'
	status=1
fi
exit "$status"
