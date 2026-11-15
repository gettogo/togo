// Package termmenu reads a list that a terminal program has drawn on its
// screen: a command menu, a chooser, a settings page. It knows no program
// and no command. It looks for what such lists have in common: a cursor
// (a glyph before one row, or one row on a different background), rows
// aligned under it, a title in the box's border or on the line above, a
// line of key hints, a search field, a position counter.
//
// The input is the screen as text with its colour sequences (tmux
// capture-pane -e). A screen that does not look like a list is reported as
// not one; nothing is guessed.
package termmenu

import (
	"regexp"
	"strconv"
	"strings"
)

// Item is one row of a list.
type Item struct {
	Label  string
	Detail string
	// Group is the heading the row sits under, when the list has headings.
	Group string
	// Cursor marks the row the program's cursor is on. Current marks the
	// row the program says is in use. They are different things.
	Cursor  bool
	Current bool
}

// Menu is a list read from a screen.
type Menu struct {
	Title string
	// Hint is the program's own line of keys ("↑↓ navigate · Enter select").
	Hint string
	// Search reports a search field in the list; Query is the text in it.
	Search bool
	Query  string
	Items  []Item
	// Position and Total are the list's own counter ("3/96"), when shown:
	// the list has Total rows and only some are on screen.
	Position, Total int
}

// cell is one character of the screen with what the reader needs of its style.
type cell struct {
	r      rune
	bg, fg string
	rev    bool
}

var sgr = regexp.MustCompile(`\x1b\[([0-9;:]*)m`)
var otherEscape = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]|\x1b\][^\x07\x1b]*(\x07|\x1b\\)`)

// parse splits a screen into lines of cells.
func parse(screen string) [][]cell {
	var out [][]cell
	for _, line := range strings.Split(screen, "\n") {
		var cells []cell
		var bg, fg string
		rev := false
		rest := line
		for len(rest) > 0 {
			loc := sgr.FindStringSubmatchIndex(rest)
			text := rest
			if loc != nil {
				text = rest[:loc[0]]
			}
			for _, r := range otherEscape.ReplaceAllString(text, "") {
				cells = append(cells, cell{r, bg, fg, rev})
			}
			if loc == nil {
				break
			}
			applySGR(rest[loc[2]:loc[3]], &bg, &fg, &rev)
			rest = rest[loc[1]:]
		}
		out = append(out, cells)
	}
	return out
}

func applySGR(params string, bg, fg *string, rev *bool) {
	ps := strings.FieldsFunc(params, func(r rune) bool { return r == ';' || r == ':' })
	if len(ps) == 0 {
		ps = []string{"0"}
	}
	for i := 0; i < len(ps); i++ {
		p, _ := strconv.Atoi(ps[i])
		switch {
		case p == 0:
			*bg, *fg, *rev = "", "", false
		case p == 7:
			*rev = true
		case p == 27:
			*rev = false
		case (p >= 30 && p <= 37) || (p >= 90 && p <= 97):
			*fg = ps[i]
		case p == 39:
			*fg = ""
		case (p >= 40 && p <= 47) || (p >= 100 && p <= 107):
			*bg = ps[i]
		case p == 49:
			*bg = ""
		case p == 38 || p == 48:
			target := fg
			if p == 48 {
				target = bg
			}
			if i+1 < len(ps) && ps[i+1] == "5" && i+2 < len(ps) {
				*target = "5;" + ps[i+2]
				i += 2
			} else if i+1 < len(ps) && ps[i+1] == "2" && i+4 < len(ps) {
				*target = "2;" + strings.Join(ps[i+2:i+5], ";")
				i += 4
			}
		}
	}
}

func text(cells []cell) string {
	var b strings.Builder
	for _, c := range cells {
		b.WriteRune(c.r)
	}
	return b.String()
}

const (
	cursors = "❯→➜▶›◆▸"
	rules   = "│┃"
	borders = "─━├┤┬┴┼╭╮╰╯┌┐└┘"
)

var (
	boxTop    = regexp.MustCompile(`^[╭┌]─+\s*(.*?)\s*─*[╮┐─]*$`)
	hintWords = regexp.MustCompile(`(?i)(navigate|↑↓|↑/↓|to change|to close|to select|to cancel|enter select|esc cancel|esc exit|⏎|⎋)`)
	counter   = regexp.MustCompile(`^\(?(\d+)\s*/\s*(\d+)\)?$`)
	gap       = regexp.MustCompile(`\s{2,}`)
	current   = regexp.MustCompile(`\s*←\s*current|\s*\(current\)|\s*✓`)
	scrollbar = regexp.MustCompile(`\s*[█▐▌▓▒░]+$`)
	// A search field with its text: "Search: open", "🔍 open", "🔍 > open".
	searchField = regexp.MustCompile(`^(?:Search:|🔍)\s*(?:>\s?)?(.*)$`)
)

// Read finds a list on the screen.
func Read(screen string) (Menu, bool) {
	lines := parse(screen)
	if m, ok := readGlyph(lines); ok {
		return m, true
	}
	return readHighlight(lines)
}

func lead(s string) int { return len([]rune(s)) - len([]rune(strings.TrimLeft(s, " "))) }

func onlyBorders(s string) bool {
	return strings.Trim(s, borders+rules+" ") == ""
}

// icon reports a short token of symbols before a label (omp's menu draws one).
func icon(tok string) bool {
	rs := []rune(tok)
	if len(rs) == 0 || len(rs) > 3 {
		return false
	}
	for _, r := range rs {
		if r < 0x80 || (r >= '0' && r <= '9') {
			return false
		}
		if strings.ContainsRune("/-_.[(<", r) {
			return false
		}
		// Letters of any script are labels, not icons.
		if (r >= 0x00C0 && r <= 0x024F) || (r >= 0x0370 && r <= 0x1FFF && r != 0x2025) {
			return false
		}
	}
	return true
}

// readGlyph reads a list whose cursor row starts with a cursor glyph.
func readGlyph(cellLines [][]cell) (Menu, bool) {
	lines := make([][]rune, len(cellLines))
	for i, c := range cellLines {
		lines[i] = []rune(strings.TrimRight(text(c), " "))
	}
	row, col := -1, -1
	for i, l := range lines {
		for k, r := range l {
			if !strings.ContainsRune(cursors, r) || k+1 >= len(l) || l[k+1] != ' ' {
				continue
			}
			// Before the cursor: only frame characters and spaces, or
			// another pane that ends at a vertical rule.
			before := strings.TrimRight(string(l[:k]), " ")
			if strings.Trim(before, rules+" ") == "" || strings.HasSuffix(before, "│") || strings.HasSuffix(before, "┃") {
				row, col = i, k
			}
			break
		}
		if row >= 0 {
			break
		}
	}
	if row < 0 {
		return Menu{}, false
	}
	// The pane is what lies between the vertical rules around the cursor.
	pane := func(i int) string {
		l := lines[i]
		left := -1
		for k := 0; k < col && k < len(l); k++ {
			if strings.ContainsRune(rules, l[k]) {
				left = k
			}
		}
		right := len(l)
		for k := col; k < len(l); k++ {
			if strings.ContainsRune(rules, l[k]) {
				right = k
				break
			}
		}
		if left+1 > right {
			return ""
		}
		return string(l[left+1 : right])
	}
	labelCol := lead(pane(row)) + 2
	isCursor := func(t string) bool {
		rs := []rune(t)
		return len(rs) > 1 && strings.ContainsRune(cursors, rs[0]) && rs[1] == ' '
	}
	shaped := func(i int) bool {
		if i < 0 || i >= len(lines) {
			return false
		}
		p := pane(i)
		t := strings.TrimSpace(p)
		if t == "" || onlyBorders(t) {
			return false
		}
		return lead(p) >= labelCol-2 || isCursor(t)
	}
	top, bot := row, row
	for shaped(top - 1) {
		top--
	}
	for shaped(bot + 1) {
		bot++
	}
	var m Menu
	for i := top - 1; i >= 0 && i >= top-8; i-- {
		whole := strings.TrimSpace(string(lines[i]))
		if g := boxTop.FindStringSubmatch(whole); g != nil && strings.Trim(g[1], borders+" ") != "" {
			m.Title = strings.TrimSpace(strings.Trim(g[1], borders+rules+" "))
			break
		}
		t := strings.Trim(whole, rules+" ")
		if i == top-1 && t != "" && !onlyBorders(t) && len(strings.Fields(t)) <= 6 && !hintWords.MatchString(t) && m.Title == "" {
			m.Title = t
		}
		if i == top-2 && m.Title != "" && hintWords.MatchString(m.Title) {
			m.Title = t
		}
	}
	scan := func(i int) {
		if i < 0 || i >= len(lines) {
			return
		}
		t := strings.Trim(strings.TrimSpace(string(lines[i])), rules+" ")
		if strings.Contains(strings.ToLower(t), "type to search") {
			m.Search = true
		}
		if g := searchField.FindStringSubmatch(t); g != nil {
			m.Search, m.Query = true, strings.TrimSpace(gap.Split(g[1], 2)[0])
			return
		}
		if m.Hint == "" && hintWords.MatchString(t) && len([]rune(t)) < 160 {
			m.Hint = t
		}
	}
	for i := bot + 1; i < bot+14; i++ {
		scan(i)
	}
	for i := top - 3; i < top; i++ {
		scan(i)
	}
	// A hint line just above the rows means the title is the line above it.
	for i := top - 1; i >= 0 && i >= top-3; i-- {
		t := strings.Trim(strings.TrimSpace(string(lines[i])), rules+" ")
		if t == "" {
			continue
		}
		if hintWords.MatchString(t) {
			for k := i - 1; k >= 0 && k >= i-2; k-- {
				if above := strings.Trim(strings.TrimSpace(string(lines[k])), rules+" "); above != "" && !onlyBorders(above) {
					m.Title = above
					break
				}
			}
		}
		break
	}
	group := ""
	for i := top; i <= bot; i++ {
		p := pane(i)
		indent := lead(p)
		t := strings.TrimSpace(p)
		cur := isCursor(t)
		if cur {
			t = strings.TrimSpace(string([]rune(t)[1:]))
		}
		t = scrollbar.ReplaceAllString(t, "")
		if g := counter.FindStringSubmatch(t); g != nil {
			m.Position, _ = strconv.Atoi(g[1])
			m.Total, _ = strconv.Atoi(g[2])
			continue
		}
		if strings.HasPrefix(strings.ToLower(t), "type to search") {
			m.Search = true
			continue
		}
		if g := searchField.FindStringSubmatch(t); g != nil && !cur {
			m.Search, m.Query = true, strings.TrimSpace(gap.Split(g[1], 2)[0])
			continue
		}
		if !cur && hintWords.MatchString(t) {
			if m.Hint == "" {
				m.Hint = t
			}
			continue
		}
		here := indent
		if cur {
			here += 2
		}
		if first, rest, ok := strings.Cut(t, " "); ok && icon(first) && strings.TrimSpace(rest) != "" {
			t = strings.TrimSpace(rest)
		}
		parts := gap.Split(t, 2)
		label := parts[0]
		detail := ""
		if len(parts) > 1 {
			detail = strings.TrimSpace(parts[1])
		}
		isCurrent := current.MatchString(t) || strings.HasPrefix(label, "●")
		label = strings.TrimSpace(strings.TrimPrefix(current.ReplaceAllString(label, ""), "●"))
		detail = strings.TrimSpace(current.ReplaceAllString(detail, ""))
		switch {
		case here > labelCol+1 && len(m.Items) > 0 && !cur:
			// Deeper than the labels: more of the row above.
			last := &m.Items[len(m.Items)-1]
			last.Detail = strings.TrimSpace(last.Detail + " " + t)
		case here < labelCol && !cur:
			group = label
		default:
			m.Items = append(m.Items, Item{Label: label, Detail: detail, Group: group, Cursor: cur, Current: isCurrent})
		}
	}
	// A label followed by a sentence on its own line at the same column
	// (a description under each choice).
	var merged []Item
	for _, it := range m.Items {
		if n := len(merged); n > 0 && !it.Cursor && !it.Current && merged[n-1].Detail == "" && it.Detail == "" &&
			len(strings.Fields(it.Label)) > 4 && strings.HasSuffix(it.Label, ".") {
			merged[n-1].Detail = it.Label
			continue
		}
		merged = append(merged, it)
	}
	m.Items = merged
	return m, len(m.Items) > 0
}
