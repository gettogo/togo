#!/bin/sh
# Layout is decided by the room a window has, never by the size class alone (presentation
# policy: regular means the regular size class and a measured width of at least 600 pt; a Duo
# Split View half, about 475 pt, reports regular and is compact). Fails on:
#   - a size-class comparison that does not also measure a width on its line, outside
#     Design/AdaptivePresentation.swift, unless "width-ok:" on its line or the three above says
#     why it is safe;
#   - a literal column width (".frame(maxWidth: 720)"), which drifts from what sits under it
#     (the chat and its composer share ChatLayout), unless "width-ok:" says why;
#   - device checks deciding layout ("userInterfaceIdiom", "UIScreen.main") outside copy that
#     names the device, unless "idiom-ok:" says why.
#
# Usage: scripts/check-layout.sh
set -eu
cd "$(dirname "$0")/.."

dirs="apps/ios/Togo apps/ios/TogoWidgets"
out=$(find $dirs -name '*.swift' -not -path '*/DerivedData*' -not -path '*/Preview/*' \
    -not -path '*/Design/AdaptivePresentation.swift' | sort | while read -r f; do
    awk -v file="$f" '
        { line[NR] = $0 }
        function justified(tag,    i) {
            for (i = NR; i >= NR - 3 && i > 0; i--) if (index(line[i], tag)) return 1
            return 0
        }
        /^[ \t]*\/\// { next }
        /([sS]izeClass) *[!=]= *\.(regular|compact)|\.(regular|compact) *[!=]= *[A-Za-z.]*[sS]izeClass/ {
            if ($0 !~ /[wW]idth/ && !justified("width-ok:")) print "size-class " file ":" NR ": " $0
        }
        /\.frame\(maxWidth: *[0-9][0-9][0-9]/ {
            if (!justified("width-ok:")) print "literal-width " file ":" NR ": " $0
        }
        /userInterfaceIdiom|UIScreen\.main/ {
            if (!justified("idiom-ok:")) print "device " file ":" NR ": " $0
        }
    ' "$f"
done)

if [ -n "$out" ]; then
    echo "Layout decided without measuring the room (policy: size class and >= 600 pt). Use AdaptivePresentation.width, a shared constant, or say why with width-ok:/idiom-ok::" >&2
    echo "$out" >&2
    exit 1
fi
