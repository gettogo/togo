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
	if !complete || menu.Total > len(m.rows) {
		p.Presentation = &harness.PickerPresentation{State: "partial", Total: menu.Total}
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
		if before, given := queryArg["before"]; given && before != menu.Query {
			return harness.Screen{}, &harness.FallbackError{Reason: "query", Detail: "The search changed at the desk. Nothing was typed."}
		}
		for range []rune(menu.Query) {
			if err := e.menuKeys(ctx, s, "BSpace"); err != nil {
				return harness.Screen{}, err
			}
		}
		if query != "" {
			if err := e.menuType(ctx, s, query); err != nil {
				return harness.Screen{}, err
			}
		}
		after, ok := e.readMenu(ctx, s)
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
