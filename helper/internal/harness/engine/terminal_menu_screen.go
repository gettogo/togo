package engine

import (
	"context"
	"encoding/json"
	"hash/fnv"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/spec"
	"github.com/gettogo/togo/helper/internal/harness/termmenu"
)

// menuKey is an extra key the list's own hint line offers.
type menuKey struct{ key, label string }

var hintChord = regexp.MustCompile(`(?i)([A-Za-z][A-Za-z ]*?)\s+ctrl\+([a-z])\b`)

// hintKeys reads "Connect an integration ctrl+a  Favorite ctrl+f".
func hintKeys(hint string) []menuKey {
	var out []menuKey
	for _, m := range hintChord.FindAllStringSubmatch(hint, 8) {
		key := "C-" + strings.ToLower(m[2])
		if label := strings.TrimSpace(m[1]); label != "" && spec.MenuChord(key) {
			out = append(out, menuKey{key, label})
		}
	}
	return out
}

func (e *Engine) menuScreen(m *terminalMenu, menu termmenu.Menu, complete bool) *harness.Screen {
	interaction, _ := json.Marshal(m.token)
	p := &harness.Picker{Items: []harness.PickerItem{}, OnSelect: terminalPrefix + "select", Searchable: menu.Search,
		OnSelectArgs: map[string]json.RawMessage{"interaction": interaction}}
	ids := rowIDs(m.rows)
	for i, it := range m.rows {
		id := m.token + "/" + ids[i]
		p.Items = append(p.Items, harness.PickerItem{ID: id, Title: it.Label, Subtitle: it.Detail, Group: it.Group})
		if it.Current {
			p.Selected = id
		}
	}
	title := menu.Title
	if title == "" {
		title = m.title
	}
	if menu.Search {
		p.SearchMode = "remote"
		if menu.QueryKnown {
			text := menu.Query
			p.Query = &text
		}
	}
	sc := &harness.Screen{ID: menuScreenPrefix + m.token, Interaction: m.token, Primitive: harness.PrimPicker, Title: title, FetchedAt: time.Now(), Picker: p}
	// A list read only in part is fresh, not stale: its rows are what the
	// harness shows now.
	// Whole only on evidence: the end was seen while moving through the
	// list, or the list says how many rows it has and all were read. A
	// list that merely fits what was read so far is not known to end there.
	complete = m.ended || (menu.Total > 0 && len(m.rows) >= menu.Total)
	if !complete {
		p.Presentation = &harness.PickerPresentation{State: "partial", Total: menu.Total}
		// More rows can be read by moving the harness's cursor, which the
		// phone asks for explicitly: in some lists it is not free (a theme
		// list previews each theme the cursor passes).
		if len(m.rows) < menuMaxRows {
			p.Presentation.MoreAction = &harness.ActionRef{ID: terminalPrefix + "more", Label: "Show more", Role: "normal",
				Args: map[string]json.RawMessage{"interaction": interaction}}
		} else {
			p.Presentation.Message = "More in the terminal."
		}
	} else if menu.Total > 0 {
		p.Presentation = &harness.PickerPresentation{Total: menu.Total}
	}
	sc.Actions = []harness.ActionRef{}
	for _, k := range m.keys {
		key, _ := json.Marshal(k.key)
		sc.Actions = append(sc.Actions, harness.ActionRef{ID: terminalPrefix + "key." + k.key, Label: k.label, Role: "normal",
			Args: map[string]json.RawMessage{"interaction": interaction, "key": key}})
	}
	return sc
}

// rowsEndOnScreen reports whether the list visibly ends on the screen. A
// list with no counter gives no way to know, so one whose window is full
// (many rows, none of them the end of a short list) is taken as cut off.
func rowsEndOnScreen(m termmenu.Menu) bool {
	return len(m.Items) < 6
}

// terminalMenuScreen is the terminal.menu screen: the list as the harness
// shows it now. Reading it keeps the phone's hold on the session. A list
// that has closed, or a screen that is no longer this list, is reported
// with the reason, and the hold ends without a key.
func (e *Engine) terminalMenuScreen(ctx context.Context, s *sess, fromName string, query map[string]string) (harness.Screen, error) {
	ctx = withMenuConv(ctx, s)
	queryArg := query
	token := query["interaction"]
	if token == "" {
		token = fromName
	}
	m := e.menuOf(ctx, s.id, token)
	if m == nil || e.cfg.StyledScreen == nil {
		return harness.Screen{}, &harness.FallbackError{Reason: "closed", Detail: "This menu has closed."}
	}
	menu, ok := e.readMenu(ctx, s)
	if !ok && m.search && m.panel != "" {
		// A search with no matching rows leaves a list with no cursor to
		// find. If its title is still on the screen it is this list, empty,
		// with the text last typed into its field.
		if plain, err := e.cfg.Screen(ctx, s.id); err == nil {
			if text, found := fieldUnderTitle(plain, m.panel); found {
				menu, ok = termmenu.Menu{Title: m.panel, Search: true, Query: text, QueryKnown: true}, true
			}
		}
	}
	if !ok {
		e.setMenu(s.id, nil)
		return harness.Screen{}, &harness.FallbackError{Reason: "closed", Detail: "This menu has closed."}
	}
	if menu.Title != m.panel {
		e.setMenu(s.id, nil)
		return harness.Screen{}, &harness.FallbackError{Reason: "moved", Detail: "The screen changed at the desk."}
	}
	// A search typed for the phone goes into the list's own search field:
	// what is there is removed one character at a time, the new text is
	// typed, and the list is read again. Nothing is typed when the field
	// already holds that text.
	if query["more"] == "down" {
		sc, err := e.pageMenu(ctx, s, m)
		if err != nil {
			return harness.Screen{}, err
		}
		return *sc, nil
	}
	if _, asked := query["query"]; asked && (!menu.Search || !menu.QueryKnown) {
		return harness.Screen{}, &harness.FallbackError{Reason: "source", Detail: "This list's search can only be used in the terminal."}
	}
	if query, asked := query["query"]; asked && query != menu.Query {
		if len(query) > 200 || strings.ContainsAny(query, "\n\r\x1b") {
			return harness.Screen{}, &harness.FallbackError{Reason: "source", Detail: "That search cannot be typed."}
		}
		// "before" is the text the phone last saw in the field. If the
		// field holds something else now, someone searched at the desk in
		// between: nothing is typed over it, and the hold is kept so the
		// phone can read the list as it is.
		// A search that changes the field must say what it saw there.
		before, given := queryArg["before"]
		if !given || before != menu.Query {
			return harness.Screen{}, &harness.FallbackError{Reason: "query", Detail: "The search changed at the desk. Nothing was typed."}
		}
		// The old text goes one character at a time, and before each
		// Backspace the field must still read as the last one left it: a
		// character typed at the desk meanwhile is not ours to remove.
		changed := &harness.FallbackError{Reason: "query", Detail: "The search changed at the desk. Nothing more was typed."}
		for left := []rune(menu.Query); len(left) > 0; {
			now, ok := e.fieldText(ctx, s, m)
			if !ok || now != string(left) {
				return harness.Screen{}, changed
			}
			if err := e.menuKeys(ctx, s, "BSpace"); err != nil {
				return harness.Screen{}, err
			}
			left = left[:len(left)-1]
			seen := false
			for deadline := time.Now().Add(e.scaled(700 * time.Millisecond)); time.Now().Before(deadline); {
				if now, ok := e.fieldText(ctx, s, m); ok && now == string(left) {
					seen = true
					break
				}
				time.Sleep(e.scaled(40 * time.Millisecond))
			}
			if !seen {
				return harness.Screen{}, changed
			}
		}
		if now, ok := e.fieldText(ctx, s, m); !ok || now != "" {
			return harness.Screen{}, changed
		}
		if query != "" {
			if err := e.menuType(ctx, s, query); err != nil {
				return harness.Screen{}, err
			}
		}
		after, ok := e.readMenu(ctx, s)
		for try := 0; !ok && try < 2; try++ {
			// The list is redrawing under the new text; a list that really
			// has no rows still reads as none after another look.
			after, ok = e.readMenu(ctx, s)
		}
		if !ok || after.Title != m.panel {
			// No rows match, or the list went away.
			if ok {
				e.setMenu(s.id, nil)
				return harness.Screen{}, &harness.FallbackError{Reason: "moved", Detail: "The screen changed at the desk."}
			}
			after = termmenu.Menu{Title: m.panel, Search: true, Query: query, QueryKnown: true}
		}
		e.menuMu.Lock()
		m.rows, m.query, m.search, m.scrolled, m.readQuery = after.Items, query, true, false, query
		// Another search is another list: what was known of the old one's
		// end says nothing about this one.
		m.ended, m.first = false, ""
		e.menuMu.Unlock()
		sc := e.menuScreen(m, after, after.Total > 0 || rowsEndOnScreen(after))
		return *sc, nil
	}
	// What the list holds now replaces what was read before: a search or a
	// change made at the desk must not leave rows that are no longer there.
	// Only a list that was collected by scrolling keeps its other rows,
	// refreshed by those on screen.
	e.menuMu.Lock()
	m.query, m.search = menu.Query, m.search || menu.Search
	if !m.scrolled || len(menu.Items) == 0 || menu.Query != m.readQuery {
		m.rows = append([]termmenu.Item(nil), menu.Items...)
		for i := range m.rows {
			m.rows[i].Cursor = false
		}
		if menu.Query != m.readQuery || len(menu.Items) == 0 {
			m.ended, m.first = false, ""
		}
		m.scrolled, m.readQuery = false, menu.Query
	}
	e.menuMu.Unlock()
	// Rows on screen refresh what is known of them (the row in use may
	// have changed); rows scrolled out of view keep what was read.
	for _, it := range menu.Items {
		for i := range m.rows {
			if sameRow(m.rows[i], it) {
				m.rows[i].Current, m.rows[i].Detail = it.Current, it.Detail
			}
		}
	}
	// A list with no counter that fills its window is read only in part,
	// on every reading and not just the first.
	return *e.menuScreen(m, menu, menu.Total > 0 || rowsEndOnScreen(menu) || m.scrolled), nil
}

// fieldUnderTitle reads the search field of a list that has no rows: the
// first line with text under the list's title, within the title line's
// columns. Empty, such a field reads "Search".
func fieldUnderTitle(screen, title string) (string, bool) {
	lines := strings.Split(screen, "\n")
	for i, line := range lines {
		rs := []rune(line)
		at := strings.Index(line, title)
		if at < 0 {
			continue
		}
		left := len([]rune(line[:at]))
		right := len([]rune(strings.TrimRight(line, " ")))
		for _, next := range lines[i+1:] {
			nr := []rune(next)
			if left >= len(nr) {
				continue
			}
			end := right
			if end > len(nr) {
				end = len(nr)
			}
			text := strings.TrimSpace(strings.TrimRight(string(nr[left:end]), "█▌▏│ "))
			if text == "" {
				continue
			}
			if strings.EqualFold(text, "search") {
				return "", true
			}
			return text, true
		}
		_ = rs
		return "", true
	}
	return "", false
}

// rowIDs names each row by its group and label, with a count for rows that
// say the same thing, so an id means the same row whatever else the list
// holds: a search that changes the rows cannot make an old id point at
// another row.
func rowIDs(rows []termmenu.Item) []string {
	seen := map[string]int{}
	out := make([]string, len(rows))
	for i, it := range rows {
		h := fnv.New64a()
		_, _ = h.Write([]byte(it.Group + "\x00" + it.Label))
		key := strconv.FormatUint(h.Sum64(), 36)
		out[i] = key + "." + strconv.Itoa(seen[key])
		seen[key]++
	}
	return out
}

// morePerRequest bounds the rows one request for more may walk.
const morePerRequest = 20

// pageMenu reads further rows of a list that scrolls, by moving the
// harness's cursor down one window. It is asked for explicitly. The rows
// read so far come back on the same screen; the list stops being partial
// only when its end was seen: the cursor did not move, or came round to
// the row it started on. The cursor is left where the walk ended.
func (e *Engine) pageMenu(ctx context.Context, s *sess, m *terminalMenu) (*harness.Screen, error) {
	menu, ok := e.readMenu(ctx, s)
	if !ok {
		menu, ok = e.readMenu(ctx, s)
	}
	if ok && menu.Title != m.panel {
		e.setMenu(s.id, nil)
		return nil, &harness.FallbackError{Reason: "moved", Detail: "The screen changed at the desk."}
	}
	if ok && cursorOf(menu) < 0 {
		// No cursor on a row yet: one Down brings it in.
		if err := e.menuKeys(ctx, s, "Down"); err != nil {
			return nil, err
		}
		menu, ok = e.readMenu(ctx, s)
		if ok && menu.Title != m.panel {
			e.setMenu(s.id, nil)
			return nil, &harness.FallbackError{Reason: "moved", Detail: "The screen changed at the desk."}
		}
	}
	if !ok || cursorOf(menu) < 0 {
		// The list cannot be made out just now: what was read stands, and
		// more can be asked for again. Only a different list ends it.
		return e.menuScreen(m, termmenu.Menu{Title: m.panel, Search: m.search, Query: m.query}, false), nil
	}
	cur := cursorOf(menu)
	e.menuMu.Lock()
	if m.first == "" {
		m.first = menu.Items[cur].Group + "\x00" + menu.Items[cur].Label
	}
	known := map[string]bool{}
	for _, it := range m.rows {
		known[it.Group+"\x00"+it.Label] = true
	}
	e.menuMu.Unlock()
	add := func(now termmenu.Menu) {
		e.menuMu.Lock()
		defer e.menuMu.Unlock()
		for _, it := range now.Items {
			if key := it.Group + "\x00" + it.Label; !known[key] && len(m.rows) < menuMaxRows {
				known[key] = true
				it.Cursor = false
				m.rows = append(m.rows, it)
			}
		}
		m.scrolled = true
	}
	add(menu)
	window := len(menu.Items)
	if window > morePerRequest {
		window = morePerRequest
	}
	ended := false
	for step := 0; step < window && !ended; step++ {
		before := menu.Items[cursorOf(menu)]
		if err := e.menuKeys(ctx, s, "Down"); err != nil {
			return nil, err
		}
		next, ok := e.readMenu(ctx, s)
		if !ok || cursorOf(next) < 0 {
			// The row under the cursor could not be made out (a list that
			// previews as the cursor moves redraws in other colours). One
			// more look; then the walk stops with what it has, and the
			// list stays the phone's.
			next, ok = e.readMenu(ctx, s)
		}
		if ok && next.Title != m.panel {
			e.setMenu(s.id, nil)
			return nil, &harness.FallbackError{Reason: "moved", Detail: "The screen changed at the desk."}
		}
		if !ok || cursorOf(next) < 0 {
			return e.menuScreen(m, menu, false), nil
		}
		at := next.Items[cursorOf(next)]
		add(next)
		menu = next
		switch {
		case sameRow(at, before):
			ended = true // the cursor did not move: the last row
		case at.Group+"\x00"+at.Label == m.first:
			ended = true // back where it began: the list has come round
		}
	}
	e.menuMu.Lock()
	m.ended = m.ended || ended
	e.menuMu.Unlock()
	return e.menuScreen(m, menu, ended), nil
}

// fieldText is the text in the list's search field now: read from the list
// when it can be made out, else from the line under its title.
func (e *Engine) fieldText(ctx context.Context, s *sess, m *terminalMenu) (string, bool) {
	if styled, err := e.cfg.StyledScreen(ctx, s.id); err == nil {
		if menu, ok := termmenu.Read(styled); ok && menu.Title == m.panel && menu.QueryKnown {
			return menu.Query, true
		}
	}
	if plain, err := e.cfg.Screen(ctx, s.id); err == nil && m.panel != "" {
		return fieldUnderTitle(plain, m.panel)
	}
	return "", false
}
