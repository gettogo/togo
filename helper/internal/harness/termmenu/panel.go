package termmenu

import (
	"regexp"
	"strings"
)

// Panel is a panel that only tells something: a sign-in link and its code,
// with nothing to choose and nothing to type. Lines are its text as drawn.
// Nothing is worked out of them: a code stays inside the line that shows
// it.
type Panel struct {
	Title string
	Lines []string
	// Links are the lines that are one whole address. An address that
	// fills its line to the panel's edge may continue on the next one, so
	// it is kept as text only and is not offered as a link.
	Links []string
}

var (
	panelTitle = regexp.MustCompile(`(?i)^(.*\S)\s{2,}esc$`)
	wholeURL   = regexp.MustCompile(`^https?://\S+$`)
	// deskKey is one key that acts on the machine the harness runs
	// on ("open o  copy c").
	deskKey = regexp.MustCompile(`^[a-z]+ \S$`)
)

// ReadPanel reads an information panel: a panel on its own background
// whose first line is its title with "esc" at the right, holding at least
// one line that is a whole address. The same frame with rows to pick from
// is a list; Read takes those and refuses this.
func ReadPanel(screen string) (Panel, bool) { return readPanel(parse(screen)) }

func readPanel(lines [][]cell) (Panel, bool) {
	for top, l := range lines {
		for _, span := range bgSpans(l) {
			lo, hi, bg := span.lo, span.hi, span.bg
			g := panelTitle.FindStringSubmatch(strings.TrimSpace(text(l[lo:hi])))
			if g == nil {
				continue
			}
			pad := lead(text(l[lo:hi]))
			p := Panel{Title: strings.TrimSpace(g[1])}
			for i := top + 1; i < len(lines); i++ {
				row := lines[i]
				if lo >= len(row) {
					break
				}
				end := min(hi, len(row))
				on := 0
				for _, c := range row[lo:end] {
					if c.bg == bg {
						on++
					}
				}
				if on*2 < hi-lo {
					break // the panel has ended
				}
				body := strings.TrimRight(text(row[lo:end]), " ")
				line := strings.TrimSpace(body)
				if line == "" {
					continue
				}
				p.Lines = append(p.Lines, line)
				// The address is a link only with room left on its line.
				if wholeURL.MatchString(line) && len([]rune(body)) < hi-lo-pad {
					p.Links = append(p.Links, line)
				}
			}
			for len(p.Lines) > 0 && deskKeys(p.Lines[len(p.Lines)-1]) {
				p.Lines = p.Lines[:len(p.Lines)-1]
			}
			for _, line := range p.Lines {
				if wholeURL.MatchString(line) {
					return p, true
				}
			}
		}
	}
	return Panel{}, false
}

func deskKeys(line string) bool {
	for _, part := range gap.Split(line, -1) {
		if !deskKey.MatchString(strings.TrimSpace(part)) {
			return false
		}
	}
	return true
}

type bgSpan struct {
	bg     string
	lo, hi int
}

// bgSpans is, for each background colour on a line, the stretch from its
// first cell to its last, in the order the colours first appear.
func bgSpans(l []cell) []bgSpan {
	var out []bgSpan
	at := map[string]int{}
	for k, c := range l {
		if c.bg == "" {
			continue
		}
		i, seen := at[c.bg]
		if !seen {
			i = len(out)
			at[c.bg] = i
			out = append(out, bgSpan{bg: c.bg, lo: k})
		}
		out[i].hi = k + 1
	}
	return out
}
