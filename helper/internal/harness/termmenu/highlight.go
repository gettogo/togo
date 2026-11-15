package termmenu

import (
	"regexp"
	"strings"
)

// readHighlight reads a list whose cursor row is marked only by its
// background: the one row on a background its neighbours do not have.
func readHighlight(lines [][]cell) (Menu, bool) {
	rowsOf := map[string]map[int]bool{}
	weight := map[string]int{}
	for i, l := range lines {
		// The highlight is a bar across the row: its spaces count too, so
		// a short label ("low") is still a row.
		for _, c := range l {
			if c.bg != "" {
				if rowsOf[c.bg] == nil {
					rowsOf[c.bg] = map[int]bool{}
				}
				rowsOf[c.bg][i] = true
				weight[c.bg]++
			}
		}
	}
	// Each background used on exactly one row may be the cursor. A notice
	// or a status line can be one too, so each is tried and the one that
	// sits in a list is taken: it must be the only one, or the only one in
	// a titled panel.
	type candidate struct {
		menu   Menu
		weight int
	}
	var found []candidate
	for bg, rows := range rowsOf {
		if len(rows) != 1 || weight[bg] < 6 {
			continue
		}
		for r := range rows {
			if m, ok := readHighlightAt(lines, bg, r); ok && (len(m.Items) >= 2 || m.Title != "") {
				found = append(found, candidate{m, weight[bg]})
			}
		}
	}
	if len(found) == 1 {
		return found[0].menu, true
	}
	var titled []candidate
	for _, c := range found {
		if c.menu.Title != "" {
			titled = append(titled, c)
		}
	}
	if len(titled) == 1 {
		return titled[0].menu, true
	}
	// Still more than one: the cursor's bar runs the width of the list,
	// a stray patch of colour does not. The clearly widest is the cursor.
	pool := titled
	if len(pool) == 0 {
		pool = found
	}
	best, second := -1, 0
	for i, c := range pool {
		if best < 0 || c.weight > pool[best].weight {
			if best >= 0 {
				second = pool[best].weight
			}
			best = i
		} else if c.weight > second {
			second = c.weight
		}
	}
	if best >= 0 && pool[best].weight >= 3*second {
		return pool[best].menu, true
	}
	return Menu{}, false
}

// readHighlightAt reads the list around the row that alone has the
// background hl.
func readHighlightAt(lines [][]cell, hl string, row int) (Menu, bool) {
	// The panel's background is the one its neighbouring rows use.
	near := map[string]int{}
	for _, i := range []int{row - 1, row + 1, row - 2, row + 2} {
		if i < 0 || i >= len(lines) {
			continue
		}
		for _, c := range lines[i] {
			if c.bg != "" && c.bg != hl {
				near[c.bg]++
			}
		}
	}
	panel, best := "", 0
	for bg, n := range near {
		if n > best {
			panel, best = bg, n
		}
	}
	// A neighbouring line whose colour runs exactly as far as the cursor's
	// row does is another row of the same list: its colour is the
	// panel's. Without a gap above the list, the lines of whatever stands
	// there (a conversation) would otherwise be counted in.
	if lo, hi, ok := run(lines[row], hl); ok {
		for _, i := range []int{row + 1, row - 1, row + 2, row - 2} {
			if i < 0 || i >= len(lines) || lo >= len(lines[i]) {
				continue
			}
			if bg := lines[i][lo].bg; bg != "" && bg != hl {
				if a, b, ok := run(lines[i], bg); ok && a == lo && b == hi {
					panel = bg
					break
				}
			}
		}
	}
	if panel == "" {
		return Menu{}, false
	}
	extent := func(i int) (int, int, bool) {
		lo, hi := -1, -1
		for k, c := range lines[i] {
			if c.bg == panel || c.bg == hl {
				if lo < 0 {
					lo = k
				}
				hi = k + 1
			}
		}
		return lo, hi, lo >= 0
	}
	lo, hi, _ := extent(row)
	inPanel := func(i int) bool {
		if i < 0 || i >= len(lines) {
			return false
		}
		a, b, ok := extent(i)
		return ok && a <= lo+2 && b >= lo+4
	}
	top, bot := row, row
	for inPanel(top - 1) {
		top--
	}
	for inPanel(bot + 1) {
		bot++
	}
	// The rows end at an empty line below the cursor: what follows in the
	// same panel (the input line, a status line) is not part of the list.
	blank := func(i int) bool {
		l := lines[i]
		a, b := lo, hi
		if a > len(l) {
			return true
		}
		if b > len(l) {
			b = len(l)
		}
		// A line that is only the panel's own border holds nothing.
		return strings.Trim(text(l[a:b]), rules+" ") == ""
	}
	listTop, listBot := row, row
	for listTop-1 >= top && !blank(listTop-1) {
		listTop--
	}
	for listBot+1 <= bot && !blank(listBot+1) {
		listBot++
	}
	// A gap inside the list separates groups: what follows a gap belongs
	// to the list when it is a block of lines (a heading and its rows). A
	// single line between gaps is the input line or a status line.
	for grew := true; grew; {
		grew = false
		k := listBot + 1
		for k <= bot && blank(k) {
			k++
		}
		if k+1 <= bot && !blank(k) && !blank(k+1) {
			listBot = k
			for listBot+1 <= bot && !blank(listBot+1) {
				listBot++
			}
			grew = true
		}
		k = listTop - 1
		for k >= top && blank(k) {
			k--
		}
		if k-1 >= top && !blank(k) && !blank(k-1) {
			listTop = k
			for listTop-1 >= top && !blank(listTop-1) {
				listTop--
			}
			grew = true
		}
	}
	type raw struct {
		Item
		fg string
	}
	var m Menu
	var rows []raw
	escTitle := regexp.MustCompile(`(?i)\s{2,}esc$`)
	keyHint := regexp.MustCompile(`(?i)\bctrl\+\w`)
	for i := top; i <= bot; i++ {
		l := lines[i]
		a, b := lo, hi
		if a > len(l) {
			continue
		}
		if b > len(l) {
			b = len(l)
		}
		// A panel as wide as the terminal draws its border inside the
		// stretch read as the panel: it is not part of a row.
		body := strings.TrimSpace(strings.Trim(strings.TrimSpace(text(l[a:b])), rules))
		if body == "" {
			continue
		}
		switch {
		case m.Title == "" && i < row && escTitle.MatchString(body):
			m.Title = strings.TrimSpace(escTitle.ReplaceAllString(body, ""))
			continue
		case i < row && strings.EqualFold(body, "search"):
			m.Search, m.Query, m.QueryKnown = true, "", true
			continue
		case i < listTop && m.Title != "" && !m.Search:
			// The one line between the title and the rows is the search
			// field with text in it (empty, it reads "Search").
			m.Search, m.QueryKnown = true, true
			m.Query = strings.TrimRight(body, "█▌▏│ ")
			continue
		case i > row && keyHint.MatchString(body):
			if m.Hint != "" {
				m.Hint += " · "
			}
			m.Hint += body
			continue
		}
		// Above and below the rows themselves the panel holds its title,
		// its search field, its key hints and, in a command menu, the
		// input line: none of them is a row.
		if i < listTop || i > listBot {
			continue
		}
		isCurrent := strings.HasPrefix(body, "●") || strings.HasPrefix(body, "✓")
		body = strings.TrimSpace(strings.TrimLeft(body, "●•✓"))
		parts := gap.Split(body, 2)
		detail := ""
		if len(parts) > 1 {
			detail = strings.TrimSpace(parts[1])
		}
		fgs := map[string]int{}
		for _, c := range l[a:b] {
			if c.r != ' ' {
				fgs[c.fg]++
			}
		}
		fg, n := "", 0
		for k, v := range fgs {
			if v > n {
				fg, n = k, v
			}
		}
		rows = append(rows, raw{Item{Label: parts[0], Detail: detail, Cursor: i == row, Current: isCurrent}, fg})
	}
	if len(rows) == 0 {
		return Menu{}, false
	}
	// A heading is a detail-less row in a text colour the choices do not use.
	count := map[string]int{}
	for _, r := range rows {
		count[r.fg]++
	}
	common, n := "", 0
	for k, v := range count {
		if v > n {
			common, n = k, v
		}
	}
	group := ""
	for _, r := range rows {
		if r.fg != common && r.Detail == "" && !r.Cursor && !r.Current {
			group = r.Label
			continue
		}
		r.Group = group
		m.Items = append(m.Items, r.Item)
	}
	return m, len(m.Items) > 0
}

// readTitledPanel reads a list that has no cursor on any row yet: a panel
// on its own background whose first line is its title with "esc" at the
// right (a search just cleared leaves OpenCode's lists like this until a
// key moves the cursor in). No row is marked as the cursor.
func readTitledPanel(lines [][]cell) (Menu, bool) {
	escTitle := regexp.MustCompile(`(?i)^(.*\S)\s{2,}esc$`)
	for top, l := range lines {
		// The stretch of this line on one background that reads "Title  esc".
		byBG := map[string][2]int{}
		for k, c := range l {
			if c.bg == "" {
				continue
			}
			span, seen := byBG[c.bg]
			if !seen {
				span = [2]int{k, k + 1}
			}
			span[1] = k + 1
			byBG[c.bg] = span
		}
		for bg, span := range byBG {
			g := escTitle.FindStringSubmatch(strings.TrimSpace(text(l[span[0]:span[1]])))
			if g == nil {
				continue
			}
			m := Menu{Title: strings.TrimSpace(g[1])}
			type raw struct {
				Item
				fg string
			}
			var rows []raw
			blanks := 0
			for i := top + 1; i < len(lines) && blanks < 3; i++ {
				row := lines[i]
				lo, hi := span[0], span[1]
				if lo >= len(row) {
					break
				}
				if hi > len(row) {
					hi = len(row)
				}
				onPanel := 0
				for _, c := range row[lo:hi] {
					if c.bg == bg {
						onPanel++
					}
				}
				if onPanel*2 < hi-lo {
					break // the panel has ended
				}
				body := strings.TrimSpace(text(row[lo:hi]))
				if body == "" {
					blanks++
					continue
				}
				blanks = 0
				if strings.EqualFold(body, "search") && len(rows) == 0 {
					m.Search, m.Query, m.QueryKnown = true, "", true
					continue
				}
				// The first line under the title, set apart from the rows by
				// an empty line, is the search field with text in it.
				if len(rows) == 0 && !m.Search && i+1 < len(lines) && blankIn(lines[i+1], lo, hi) {
					m.Search, m.Query, m.QueryKnown = true, strings.TrimRight(body, "█▌▏│ "), true
					continue
				}
				// "No results found" is the list saying it is empty, not a row.
				if noResults.MatchString(body) {
					continue
				}
				if regexp.MustCompile(`(?i)\bctrl\+\w`).MatchString(body) {
					if m.Hint != "" {
						m.Hint += " · "
					}
					m.Hint += body
					continue
				}
				isCurrent := strings.HasPrefix(body, "●") || strings.HasPrefix(body, "✓")
				body = strings.TrimSpace(strings.TrimLeft(body, "●•✓"))
				parts := gap.Split(body, 2)
				detail := ""
				if len(parts) > 1 {
					detail = strings.TrimSpace(parts[1])
				}
				fgs := map[string]int{}
				for _, c := range row[lo:hi] {
					if c.r != ' ' {
						fgs[c.fg]++
					}
				}
				fg, n := "", 0
				for k, v := range fgs {
					if v > n {
						fg, n = k, v
					}
				}
				rows = append(rows, raw{Item{Label: parts[0], Detail: detail, Current: isCurrent}, fg})
			}
			if len(rows) == 0 {
				continue
			}
			count := map[string]int{}
			for _, r := range rows {
				count[r.fg]++
			}
			common, n := "", 0
			for k, v := range count {
				if v > n {
					common, n = k, v
				}
			}
			group := ""
			for _, r := range rows {
				if r.fg != common && r.Detail == "" && !r.Current {
					group = r.Label
					continue
				}
				r.Group = group
				m.Items = append(m.Items, r.Item)
			}
			if len(m.Items) > 0 {
				return m, true
			}
		}
	}
	return Menu{}, false
}

var noResults = regexp.MustCompile(`(?i)^no (results|matches|items)\b`)

func blankIn(row []cell, lo, hi int) bool {
	if lo >= len(row) {
		return true
	}
	if hi > len(row) {
		hi = len(row)
	}
	return strings.TrimSpace(text(row[lo:hi])) == ""
}

// readHighlightLoose is readHighlight for a list that may have one row:
// the widest one-row bar of colour, whatever it holds.
func readHighlightLoose(lines [][]cell) (Menu, bool) {
	rowsOf := map[string]map[int]bool{}
	weight := map[string]int{}
	for i, l := range lines {
		for _, c := range l {
			if c.bg != "" {
				if rowsOf[c.bg] == nil {
					rowsOf[c.bg] = map[int]bool{}
				}
				rowsOf[c.bg][i] = true
				weight[c.bg]++
			}
		}
	}
	var best Menu
	bestWeight, found := 0, false
	for bg, rows := range rowsOf {
		if len(rows) != 1 || weight[bg] < 6 {
			continue
		}
		for r := range rows {
			if m, ok := readHighlightAt(lines, bg, r); ok && weight[bg] > bestWeight {
				best, bestWeight, found = m, weight[bg], true
			}
		}
	}
	return best, found
}

// run is the single stretch of a line drawn on one background, if that
// background is on the line in one piece.
func run(l []cell, bg string) (lo, hi int, ok bool) {
	lo, hi = -1, -1
	for k, c := range l {
		if c.bg != bg {
			continue
		}
		if lo < 0 {
			lo = k
		} else if k != hi {
			return 0, 0, false
		}
		hi = k + 1
	}
	return lo, hi, lo >= 0
}
