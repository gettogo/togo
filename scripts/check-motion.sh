#!/usr/bin/env bash
# Fails when app code animates without a motion role.
# Usage: scripts/check-motion.sh [root]   (default apps/ios/Togo)
#
# Every animation goes through Motion.animation(_:reduce:) or .motion(_:value:), so each
# change moves the way its role says and honours Reduce Motion. Only Design/Motion*.swift
# may name curves. scripts/motion-allowlist.txt lists older lines still to convert, one per
# line as <path relative to root><TAB><trimmed source line>; it only ever shrinks.
set -euo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
repo="$(dirname "$here")"
root="${1:-$repo/apps/ios/Togo}"
allowlist="$here/motion-allowlist.txt"

if [ ! -d "$root" ]; then
	echo "check-motion: no such directory: $root" >&2
	exit 2
fi
root="$(cd "$root" && pwd)"

found="$(
	cd "$root"
	find . -name '*.swift' ! -path './Design/Motion*.swift' -print0 | sort -z |
		xargs -0 awk -v allowlist="$allowlist" '
		BEGIN {
			while ((getline entry < allowlist) > 0) {
				if (entry ~ /^#/ || entry == "") continue
				allowed[entry] = 1
			}
		}
		{
			line = $0
			sub(/^[ \t]+/, "", line)
			sub(/[ \t]+$/, "", line)
			if (line ~ /^\/\//) next
			# Role calls are the sanctioned way to animate; drop them before looking.
			probe = line
			gsub(/withAnimation\(Motion\.animation\(/, "", probe)
			gsub(/\.animation\(Motion\.animation\(/, "", probe)
			gsub(/Motion\.animation\(/, "", probe)
			hit = (probe ~ /withAnimation\(/ || probe ~ /\.animation\(/ || index(probe, ".spring(") || \
				index(probe, ".easeIn") || index(probe, ".easeOut") || index(probe, ".linear(") || \
				index(probe, "UIView.animate("))
			if (!hit) next
			path = FILENAME
			sub(/^\.\//, "", path)
			if ((path "\t" line) in allowed) next
			printf "%s:%d: %s\n", path, FNR, line
		}'
)"

if [ -n "$found" ]; then
	echo "Animations must use a motion role (Motion.animation(_:reduce:) or .motion(_:value:)):"
	echo "$found"
	exit 1
fi
