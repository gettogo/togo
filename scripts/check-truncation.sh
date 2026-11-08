#!/bin/sh
# Text the user needs is never cut where they cannot get it back. Every fixed line limit and
# every truncation mode on app, widget and Watch text must say why it is safe: a comment with
# "trunc-ok:" on the same line or within the three lines above, naming where the full text is
# (a tap, a sheet, a long press, .help, the accessibility label) or why the text cannot be long.
# Growing ranges for text fields (".lineLimit(1...6)") are not truncation and pass. A limit
# named by a constant (".lineLimit(BackgroundJob.tailLines)") is a fixed limit like a number.
#
# Usage: scripts/check-truncation.sh            fails on any unjustified site
#        scripts/check-truncation.sh --list     lists every site, justified or not
set -eu
cd "$(dirname "$0")/.."

list=0
[ "${1:-}" = "--list" ] && list=1

dirs="apps/ios/Togo apps/ios/TogoWidgets apps/ios/TogoWatch apps/ios/TogoWatchWidgets"
bad=0
out=$(find $dirs -name '*.swift' -not -path '*/DerivedData*' | sort | while read -r f; do
    awk -v file="$f" -v list="$list" '
        { line[NR] = $0 }
        /\.lineLimit\([0-9]+\)|\.lineLimit\([^)]*\? *[0-9]|\.lineLimit\([A-Za-z_][A-Za-z0-9_.]*\)|\.truncationMode\(|\.minimumScaleFactor\(/ && !/\.lineLimit\([^)]*[0-9]\.\.\./ && !/\.lineLimit\(nil\)/ {
            ok = 0
            for (i = NR; i >= NR - 3 && i > 0; i--) if (line[i] ~ /trunc-ok:/) ok = 1
            if (!ok) print "UNJUSTIFIED " file ":" NR ": " $0
            else if (list == 1) print "ok " file ":" NR ": " $0
        }
    ' "$f"
done)

if [ "$list" = 1 ]; then
    echo "$out"
    exit 0
fi
hits=$(echo "$out" | grep '^UNJUSTIFIED' || true)
if [ -n "$hits" ]; then
    echo "Text cut short without a way back. Let it wrap, or add a 'trunc-ok:' comment naming where the full text is:" >&2
    echo "$hits" | sed 's/^UNJUSTIFIED //' >&2
    exit 1
fi
