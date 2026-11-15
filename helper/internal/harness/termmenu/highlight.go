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
	var found []Menu
	for bg, rows := range rowsOf {
		if len(rows) != 1 || weight[bg] < 6 {
			continue
		}
		for r := range rows {
			if m, ok := readHighlightAt(lines, bg, r); ok && (len(m.Items) >= 2 || m.Title != "") {
				found = append(found, m)
			}
		}
	}
	if len(found) == 1 {
		return found[0], true
	}
	var titled []Menu
	for _, m := range found {
		if m.Title != "" {
			titled = append(titled, m)
		}
	}
	if len(titled) == 1 {
		return titled[0], true
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
		return strings.TrimSpace(text(l[a:b])) == ""
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
		body := strings.TrimSpace(text(l[a:b]))
		if body == "" {
			continue
		}
		switch {
		case m.Title == "" && i < row && escTitle.MatchString(body):
			m.Title = strings.TrimSpace(escTitle.ReplaceAllString(body, ""))
			continue
		case i < row && strings.EqualFold(body, "search"):
			m.Search = true
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
