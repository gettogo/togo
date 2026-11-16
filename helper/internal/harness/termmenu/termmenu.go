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
	"unicode"
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
	// QueryKnown reports that the field's text was read (it may be empty).
	// A search field whose text could not be read leaves it false.
	QueryKnown bool
	Items      []Item
	// Position and Total are the list's own counter ("3/96"), when shown:
	// the list has Total rows and only some are on screen.
	Position, Total int
	// Ended reports that the rows visibly stop with room to spare inside
	// the list's own frame: a window with empty lines left shows the
	// whole list, so nothing is hidden above or below.
	Ended bool
}

// cell is one character of the screen with what the reader needs of its style.
type cell struct {
	r      rune
	bg, fg string
	rev    bool
	// ul: underlined, which lists use for a heading over their rows.
	ul bool
	// bold tells a name from the description after it on a row drawn in
	// one colour.
	bold bool
}

// ordinal is the number a list puts before a choice.
var ordinal = regexp.MustCompile(`^\d{1,3}[.)]$`)

// numbered is a choice written after its number: "3. Name".
var numbered = regexp.MustCompile(`^\d{1,3}[.)] (\S.*)$`)

var sgr = regexp.MustCompile(`\x1b\[([0-9;:]*)m`)
var otherEscape = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]|\x1b\][^\x07\x1b]*(\x07|\x1b\\)`)

// parse splits a screen into lines of cells.
func parse(screen string) [][]cell {
	var out [][]cell
	for _, line := range strings.Split(screen, "\n") {
		var cells []cell
		var bg, fg string
		rev, ul, bold := false, false, false
		rest := line
		for len(rest) > 0 {
			loc := sgr.FindStringSubmatchIndex(rest)
			text := rest
			if loc != nil {
				text = rest[:loc[0]]
			}
			for _, r := range otherEscape.ReplaceAllString(text, "") {
				cells = append(cells, cell{r, bg, fg, rev, ul, bold})
			}
			if loc == nil {
				break
			}
			applySGR(rest[loc[2]:loc[3]], &bg, &fg, &rev, &ul, &bold)
			rest = rest[loc[1]:]
		}
		out = append(out, cells)
	}
	return out
}

func applySGR(params string, bg, fg *string, rev, ul, bold *bool) {
	ps := strings.FieldsFunc(params, func(r rune) bool { return r == ';' || r == ':' })
	if len(ps) == 0 {
		ps = []string{"0"}
	}
	for i := 0; i < len(ps); i++ {
		p, _ := strconv.Atoi(ps[i])
		switch {
		case p == 0:
			*bg, *fg, *rev, *ul, *bold = "", "", false, false, false
		case p == 1:
			*bold = true
		case p == 22:
			*bold = false
		case p == 4:
			*ul = true
		case p == 24:
			*ul = false
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
	current   = regexp.MustCompile(`\s*←\s*current|\s*\(current\)|\s*[✓✔]`)
	scrollbar = regexp.MustCompile(`\s*[█▐▌▓▒░]+$`)
	// A search field with its text: "Search: open", "🔍 open", "🔍 > open".
	searchField = regexp.MustCompile(`^(?:Search:|🔍)\s*(?:>\s?)?(.*)$`)
)

// ReadOffered finds the short list a program offers under its input line
// while a command is typed. Unlike Read it accepts a single highlighted
// row with no title: the caller knows a list is due there and checks the
// row against what it typed.
func ReadOffered(screen string) (Menu, bool) {
	if m, ok := Read(screen); ok {
		return m, true
	}
	return readHighlightLoose(parse(screen))
}

// Read finds a list on the screen.
func Read(screen string) (Menu, bool) {
	m, ok := readAny(screen)
	if !ok {
		return m, false
	}
	// One row that only says there is nothing ("No plugins available") is
	// the list's message, not a choice.
	if len(m.Items) == 1 && nothingHere.MatchString(m.Items[0].Label) {
		return Menu{}, false
	}
	for i := range m.Items {
		// A description of dots alone ("..", "…") is where the program
		// cut one off: it says nothing.
		if !hasWord(m.Items[i].Detail) {
			m.Items[i].Detail = ""
		}
	}
	return m, true
}

// nothingHere is a list's own line for having no rows.
var nothingHere = regexp.MustCompile(`(?i)^no \S.*\b(available|found|yet|here|configured|installed)\b`)

func hasWord(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return true
		}
	}
	return false
}

func readAny(screen string) (Menu, bool) {
	lines := parse(screen)
	// A panel that only shows a link and a code has the frame of a list
	// but no rows to pick.
	if _, is := readPanel(lines); is {
		return Menu{}, false
	}
	if m, ok := readGlyph(lines); ok {
		return m, true
	}
	if m, ok := readHighlight(lines); ok {
		return m, true
	}
	return readTitledPanel(lines)
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

// lineColour is the text colour most of a line is written in.
func lineColour(cells []cell) string {
	count := map[string]int{}
	for _, c := range cells {
		if c.r != ' ' && !strings.ContainsRune(rules+borders+cursors, c.r) {
			count[c.fg]++
		}
	}
	best, n := "", 0
	for fg, v := range count {
		if v > n {
			best, n = fg, v
		}
	}
	return best
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
	// A list may draw each choice as a block: its name, then lines about it
	// in another colour, with an empty line between choices. The cursor's
	// block shows it: the cursor is on its first line and every other line
	// is in a colour the first is not. Each block is then one choice.
	part := func(i int) bool {
		if !shaped(i) {
			return false
		}
		t := strings.TrimSpace(pane(i))
		return !hintWords.MatchString(t) && !promptInput.MatchString(t) && !searchField.MatchString(t) && !counter.MatchString(t)
	}
	// Without a second block to compare, only a short block whose other
	// lines are sentences in one colour is taken for one: a short menu
	// that dims the choices the cursor is not on must stay a menu.
	blocks := false
	if row == top && bot > top && bot-top <= 3 {
		blocks = true
		wordy := false
		for i := top + 1; blocks && i <= bot; i++ {
			f := strings.Fields(pane(i))
			wordy = wordy || len(f) >= 4
			blocks = lineColour(cellLines[i]) != lineColour(cellLines[row]) && lineColour(cellLines[i]) == lineColour(cellLines[top+1]) &&
				len(f) > 0 && !ordinal.MatchString(f[0])
		}
		// Another block next to it, named in the cursor row's colour,
		// settles it either way.
		for _, at := range []int{bot + 2, top - 2} {
			if at == bot+2 && (shaped(bot+1) || !part(at)) || at == top-2 && (shaped(top-1) || !part(at)) {
				continue
			}
			first := at
			for at < top && part(first-1) {
				first--
			}
			wordy = true
			blocks = blocks && lineColour(cellLines[first]) == lineColour(cellLines[row]) && shaped(first+1) &&
				lineColour(cellLines[first+1]) == lineColour(cellLines[top+1])
		}
		blocks = blocks && wordy
	}
	if blocks {
		for part(top-2) && !shaped(top-1) {
			for top -= 2; part(top - 1); top-- {
			}
		}
		for part(bot+2) && !shaped(bot+1) {
			for bot += 2; part(bot + 1); bot++ {
			}
		}
	}
	var m Menu
	// Two empty lines inside the frame right under the last row (one is
	// only the gap between blocks).
	framed := func(i int) bool {
		if i < 0 || i >= len(lines) {
			return false
		}
		t := strings.TrimSpace(string(lines[i]))
		return len([]rune(t)) > 2 && strings.ContainsRune(rules, []rune(t)[0]) && strings.ContainsRune(rules, []rune(t)[len([]rune(t))-1]) &&
			strings.TrimSpace(strings.Trim(t, rules)) == ""
	}
	m.Ended = framed(bot+1) && framed(bot+2)
	// The colour a list writes its key hints in is the colour it writes
	// descriptions in: dimmer than the choices.
	hintColour := ""
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
			// The field says this only while it is empty.
			m.Search = true
			if !m.QueryKnown {
				m.Query, m.QueryKnown = "", true
			}
		}
		if g := searchField.FindStringSubmatch(t); g != nil {
			m.Search, m.Query, m.QueryKnown = true, strings.TrimSpace(gap.Split(g[1], 2)[0]), true
			return
		}
		if m.Hint == "" && hintWords.MatchString(t) && len([]rune(t)) < 160 {
			m.Hint = t
			hintColour = lineColour(cellLines[i])
		}
	}
	// To the bottom of the list's own box, however tall it is drawn.
	for i := bot + 1; i < len(lines) && i < bot+80; i++ {
		scan(i)
		if i >= bot+14 || promptBottom.MatchString(strings.TrimSpace(string(lines[i]))) {
			if m.Hint != "" || promptBottom.MatchString(strings.TrimSpace(string(lines[i]))) {
				break
			}
		}
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
	// lastColour is the colour of the last choice's own line: a list that
	// writes its choices in the hints' colour has no such descriptions.
	lastColour := ""
	// described is the count of choices when a description line was last
	// attached: the choice that has one may have more.
	described := 0
	blockStart := true
	numberedList := false
	// A list that starts its rows with a mark (✔, ⚠) writes its headings
	// without one.
	marked, plain := 0, 0
	for i := top; i <= bot; i++ {
		t := strings.TrimSpace(pane(i))
		if isCursor(t) {
			t = strings.TrimSpace(string([]rune(t)[1:]))
		}
		if f := strings.Fields(t); len(f) > 1 && icon(f[0]) {
			marked++
		} else if len(f) > 0 {
			plain++
		}
	}
	markedRows := marked >= 3 && marked >= 2*plain
	for i := top; i <= bot; i++ {
		p := pane(i)
		indent := lead(p)
		t := strings.TrimSpace(p)
		cur := isCursor(t)
		if cur {
			t = strings.TrimSpace(string([]rune(t)[1:]))
		}
		t = scrollbar.ReplaceAllString(t, "")
		// The top edge of a box drawn inside the list is not a row.
		if strings.ContainsAny(t, "╮╭") && strings.Contains(t, "──") {
			continue
		}
		// A scrolling list marks its last row in view with an arrow in
		// front of the number ("↓ 5.  Name").
		arrowed := false
		if f := strings.Fields(t); len(f) > 1 && (f[0] == "↓" || f[0] == "↑") && ordinal.MatchString(f[1]) {
			t = strings.TrimSpace(strings.TrimPrefix(t, f[0]))
			arrowed = true
		}
		// "… +1 model": how many more there are, not a choice.
		if strings.HasPrefix(t, "…") && !cur {
			continue
		}
		// In a list that numbers its choices, a line with no number is
		// the rest of the choice above it, wrapped.
		if isNum := numbered.MatchString(t) || ordinal.MatchString(strings.Fields(t + " x")[0]); isNum {
			numberedList = true
		} else if numberedList && !cur && indent <= labelCol+1 && len(m.Items) > 0 && t != "" && !hintWords.MatchString(t) {
			last := &m.Items[len(m.Items)-1]
			last.Label = strings.TrimSpace(last.Label + " " + t)
			continue
		}
		if f := strings.Fields(t); markedRows && !cur && len(f) > 0 && !icon(f[0]) && len(gap.Split(t, 2)) == 1 && !hintWords.MatchString(t) {
			group = t
			continue
		}
		// An underlined line over the rows is their heading, wherever it
		// is indented: the cursor never stops on it.
		if !cur && t != "" && underlined(cellLines[i]) {
			group = strings.Join(strings.Fields(t), " ")
			continue
		}
		if blocks {
			if t == "" {
				blockStart = true
				continue
			}
			if !blockStart && !cur && len(m.Items) > 0 {
				last := &m.Items[len(m.Items)-1]
				last.Detail = strings.TrimSpace(last.Detail + " " + strings.Join(strings.Fields(t), " "))
				continue
			}
			blockStart = false
		}
		if g := counter.FindStringSubmatch(t); g != nil {
			m.Position, _ = strconv.Atoi(g[1])
			m.Total, _ = strconv.Atoi(g[2])
			continue
		}
		if strings.HasPrefix(strings.ToLower(t), "type to search") {
			m.Search = true
			if !m.QueryKnown {
				m.Query, m.QueryKnown = "", true
			}
			continue
		}
		if g := searchField.FindStringSubmatch(t); g != nil && !cur {
			m.Search, m.Query, m.QueryKnown = true, strings.TrimSpace(gap.Split(g[1], 2)[0]), true
			continue
		}
		if !cur && hintWords.MatchString(t) {
			if m.Hint == "" {
				m.Hint = t
				hintColour = lineColour(cellLines[i])
			}
			continue
		}
		// A line in the hints' colour under a choice is that choice's
		// description, however many lines it wraps to.
		// (Prose only: a line in two columns, or a word or two, is a row
		// with its value or a heading, whatever its colour.)
		// (A second such line may be in columns: "5 hours ago  ·  13KB".)
		if !cur && hintColour != "" && len(m.Items) > 0 && lineColour(cellLines[i]) == hintColour && lastColour != hintColour &&
			len(strings.Fields(t)) >= 3 && (len(gap.Split(t, 2)) == 1 || described == len(m.Items)) {
			last := &m.Items[len(m.Items)-1]
			last.Detail = strings.TrimSpace(last.Detail + " " + strings.Join(strings.Fields(t), " "))
			described = len(m.Items)
			continue
		}
		here := indent
		if cur || arrowed {
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
		// A numbered row ("3.  Name   what it is"): the number is not the
		// choice's name.
		if ordinal.MatchString(label) && detail != "" {
			parts = gap.Split(detail, 2)
			label, detail = parts[0], ""
			if len(parts) > 1 {
				detail = strings.TrimSpace(parts[1])
			}
		}
		// "1. Name" with one space: the number is still not the name.
		if g := numbered.FindStringSubmatch(label); g != nil {
			label = g[1]
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
			// A heading is a word or a few; a sentence there is the
			// list's own description.
			if len(strings.Fields(t)) <= 4 && !strings.HasSuffix(t, ".") {
				group = label
			}
		default:
			m.Items = append(m.Items, Item{Label: label, Detail: detail, Group: group, Cursor: cur, Current: isCurrent})
			lastColour = lineColour(cellLines[i])
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
	// The list's own title, met again above its rows, is not a heading.
	if m.Title == "" && len(m.Items) > 0 && m.Items[0].Group != "" {
		same := true
		for _, it := range m.Items {
			same = same && it.Group == m.Items[0].Group
		}
		if same {
			m.Title = m.Items[0].Group
		}
	}
	for k := range m.Items {
		if m.Items[k].Group == m.Title {
			m.Items[k].Group = ""
		}
	}
	return m, len(m.Items) > 0
}

// DrawnCursor finds a cursor the harness draws itself: the one cell on the
// screen shown in reverse video with no reversed cell beside it. Harnesses
// that hide the terminal's cursor and park it in a corner (Pi, Droid) show
// where typing goes this way. More than one such cell, or none, says
// nothing.
func DrawnCursor(screen string) (x, y int, ok bool) {
	found := 0
	for row, line := range parse(screen) {
		for col, c := range line {
			if !c.rev || (col > 0 && line[col-1].rev) || (col+1 < len(line) && line[col+1].rev) {
				continue
			}
			found++
			x, y = col, row
		}
	}
	return x, y, found == 1
}

// underlined reports a line whose words are all underlined.
func underlined(cells []cell) bool {
	n := 0
	for _, c := range cells {
		if c.r == ' ' || strings.ContainsRune(rules+borders+cursors+"█▐▌▓▒░", c.r) {
			continue
		}
		if !c.ul {
			return false
		}
		n++
	}
	return n > 0
}
