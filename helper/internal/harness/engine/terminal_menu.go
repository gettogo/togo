package engine

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"hash/fnv"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/spec"
	"github.com/gettogo/togo/helper/internal/harness/termmenu"
)

// A harness draws its own menus in its terminal: the list of commands, a
// chooser of models or modes, a confirmation. They already work there. The
// phone shows the same list natively and the helper presses the keys the
// harness already takes, reading the screen after every step: what it read
// is what the phone shows, and nothing is reported done that the screen
// does not show. No harness and no command is named here.
//
//	terminal.command  args {text}       type a command; a menu that opens comes back as a screen
//	terminal.select   target = row id   move the cursor to the row, check it, press Enter
//	terminal.cancel   args {interaction} or target: Esc, checked
//	terminal.release  the same: let go without pressing anything
//	terminal.attach   take up a list that is already on the screen
//
// A row id is "<interaction>/<row>", where <row> names the row by what it
// says, not by its place; the screen's id is "terminal.menu/<interaction>".

const (
	terminalPrefix    = "terminal."
	menuScreenPrefix  = "terminal.menu/"
	menuSettle        = 3 * time.Second
	menuQuiet         = 180 * time.Millisecond
	menuLife          = 20 * time.Second
	menuMaxRows       = 400
	menuMaxSteps      = 420
	menuNotRecognised = "This screen could not be read as a list. Open the terminal to use it."
)

// terminalMenu is the list a phone is driving in one session.
type terminalMenu struct {
	token   string
	title   string
	panel   string // the list's own title, to tell it is still this list
	client  any    // the connection that is driving the list
	query   string // the text last seen or typed in the list's search field
	search  bool
	rows    []termmenu.Item
	keys    []menuKey
	expires time.Time
}

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

func (e *Engine) drivingMenu(session string) bool {
	e.menuMu.Lock()
	defer e.menuMu.Unlock()
	m := e.menus[session]
	if m == nil {
		return false
	}
	if time.Now().After(m.expires) {
		if e.leftOpen == nil {
			e.leftOpen = map[string]string{}
		}
		e.leftOpen[session] = m.panel
		delete(e.menus, session)
		return false
	}
	return true
}

// menuLeftOpen reports a list the phone let go of that the harness still
// shows: released, expired, or a choice that stopped half way. It has no
// owner, and keys typed now would go into it. Once the screen no longer
// shows that list the session is free again.
func (e *Engine) menuLeftOpen(s *sess) (string, bool) {
	e.menuMu.Lock()
	panel, ok := e.leftOpen[s.id]
	e.menuMu.Unlock()
	if !ok || e.cfg.StyledScreen == nil {
		return "", false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	screen, err := e.cfg.StyledScreen(ctx, s.id)
	if err == nil {
		if menu, is := termmenu.Read(screen); is && menu.Title == panel {
			return panel, true
		}
	}
	e.menuMu.Lock()
	delete(e.leftOpen, s.id)
	e.menuMu.Unlock()
	return "", false
}

// client is the connection a call arrived on.
func (e *Engine) client(ctx context.Context) any {
	if e.cfg.Client == nil {
		return nil
	}
	return e.cfg.Client(ctx)
}

// menuOf is the list with that token, for the connection that is driving
// it. Another connection, even with the token, gets nothing.
func (e *Engine) menuOf(ctx context.Context, session, token string) *terminalMenu {
	e.menuMu.Lock()
	defer e.menuMu.Unlock()
	m := e.menus[session]
	if m == nil || m.token != token || time.Now().After(m.expires) {
		return nil
	}
	if m.client != nil && m.client != e.client(ctx) {
		return nil
	}
	m.expires = time.Now().Add(menuLife)
	return m
}

func (e *Engine) setMenu(session string, m *terminalMenu) {
	e.menuMu.Lock()
	defer e.menuMu.Unlock()
	if e.menus == nil {
		e.menus = map[string]*terminalMenu{}
	}
	if m == nil {
		// Whether the list is still on screen is not known here: it is
		// remembered, and checked before anything is typed.
		if old := e.menus[session]; old != nil {
			if e.leftOpen == nil {
				e.leftOpen = map[string]string{}
			}
			e.leftOpen[session] = old.panel
		}
		delete(e.menus, session)
		return
	}
	m.expires = time.Now().Add(menuLife)
	e.menus[session] = m
}

// settled reads the screen until two readings in a row are the same.
func (e *Engine) settled(ctx context.Context, s *sess) (string, bool) {
	deadline := time.Now().Add(e.scaled(menuSettle))
	last, err := e.cfg.StyledScreen(ctx, s.id)
	if err != nil {
		return "", false
	}
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return last, false
		case <-time.After(e.scaled(menuQuiet)):
		}
		next, err := e.cfg.StyledScreen(ctx, s.id)
		if err != nil {
			return "", false
		}
		if next == last {
			return last, true
		}
		last = next
	}
	return last, true
}

func (e *Engine) readMenu(ctx context.Context, s *sess) (termmenu.Menu, bool) {
	screen, ok := e.settled(ctx, s)
	if !ok {
		return termmenu.Menu{}, false
	}
	return termmenu.Read(screen)
}

func cursorOf(m termmenu.Menu) int {
	for i, it := range m.Items {
		if it.Cursor {
			return i
		}
	}
	return -1
}

func sameRow(a, b termmenu.Item) bool {
	return a.Label == b.Label && a.Group == b.Group
}

// wholeList collects every row of a list that shows only a window of them,
// by moving the cursor down until the rows repeat or the counter ends, and
// then back to where it was.
func (e *Engine) wholeList(ctx context.Context, s *sess, first termmenu.Menu) ([]termmenu.Item, bool) {
	if first.Total <= len(first.Items) {
		return first.Items, true
	}
	var all []termmenu.Item
	seen := map[string]bool{}
	add := func(m termmenu.Menu) {
		for _, it := range m.Items {
			if key := it.Group + "\x00" + it.Label; !seen[key] {
				seen[key] = true
				it.Cursor = false
				all = append(all, it)
			}
		}
	}
	add(first)
	m, steps := first, 0
	for steps < menuMaxSteps && len(all) < menuMaxRows && len(all) < first.Total {
		if err := e.menuKeys(ctx, s, "Down"); err != nil {
			return all, false
		}
		steps++
		next, ok := e.readMenu(ctx, s)
		if !ok {
			return all, false
		}
		add(next)
		m = next
	}
	_ = m
	for ; steps > 0; steps-- {
		if err := e.menuKeys(ctx, s, "Up"); err != nil {
			return all, false
		}
	}
	return all, len(all) >= first.Total
}

func (e *Engine) menuScreen(m *terminalMenu, menu termmenu.Menu, complete bool) *harness.Screen {
	interaction, _ := json.Marshal(m.token)
	p := &harness.Picker{Items: []harness.PickerItem{}, OnSelect: terminalPrefix + "select", Searchable: menu.Search, Query: menu.Query,
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

func newToken() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (e *Engine) terminalAct(ctx context.Context, s *sess, req harness.ActionRequest) (harness.ActionResult, error) {
	failed := func(msg string) (harness.ActionResult, error) {
		return harness.ActionResult{Result: harness.ResultFailed, Message: msg}, nil
	}
	if e.cfg.StyledScreen == nil || e.cfg.Typist == nil {
		return harness.ActionResult{Result: harness.ResultFallback, Message: menuNotRecognised}, nil
	}
	// A row id is "<interaction>/<n>"; cancel and release name the
	// interaction in their arguments or as the target.
	token, rowText, _ := strings.Cut(strings.TrimPrefix(req.Target, menuScreenPrefix), "/")
	if raw, ok := req.Args["interaction"]; ok {
		var named string
		if json.Unmarshal(raw, &named) == nil && named != "" {
			if token != "" && token != named {
				return failed("This menu has closed.")
			}
			token = named
		}
	}
	verb := strings.TrimPrefix(req.Action, terminalPrefix)
	if strings.HasPrefix(verb, "key.") {
		verb = "key"
	}
	switch verb {
	case "command":
		var text string
		if err := json.Unmarshal(req.Args["text"], &text); err != nil || strings.TrimSpace(text) == "" {
			return failed("No command was given")
		}
		if e.drivingMenu(s.id) {
			return failed("A menu is already open on the phone. Finish or close it first.")
		}
		text = strings.TrimSpace(text)
		if msg, err := e.typeMenuCommand(ctx, s, text); err != nil {
			return harness.ActionResult{}, err
		} else if msg != "" {
			return failed(msg)
		}
		return e.afterKey(ctx, s, text, nil)
	case "attach":
		// Take up a list that is already on the harness's screen: one the
		// phone let go of, or one opened at the desk.
		if e.drivingMenu(s.id) {
			return failed("A menu is already open on the phone. Finish or close it first.")
		}
		menu, ok := e.readMenu(ctx, s)
		// Attach is asked for without anyone having seen the screen, so it
		// must not mistake output for a list (a "❯ ls" line in a reply). It
		// takes the list this helper left open, or a titled chooser that
		// shows its own key hints or a search field.
		e.menuMu.Lock()
		left, wasLeft := e.leftOpen[s.id]
		e.menuMu.Unlock()
		chooser := ok && menu.Title != "" && ((wasLeft && menu.Title == left) || menu.Hint != "" || menu.Search)
		if !chooser {
			return harness.ActionResult{Result: harness.ResultFallback, Message: "No menu is open in the terminal."}, nil
		}
		rows, complete := e.wholeList(ctx, s, menu)
		m := &terminalMenu{client: e.client(ctx), token: newToken(), title: menu.Title, panel: menu.Title, rows: rows, keys: hintKeys(menu.Hint)}
		e.setMenu(s.id, m)
		return harness.ActionResult{Result: harness.ResultApplied, Screen: e.menuScreen(m, menu, complete && (menu.Total > 0 || rowsEndOnScreen(menu)))}, nil
	case "select":
		m := e.menuOf(ctx, s.id, token)
		if m == nil {
			return failed("This menu has closed.")
		}
		// The id names the row itself, not its place: after a search the
		// same place holds another row, and a tap made before the search
		// must not choose that one.
		for i, id := range rowIDs(m.rows) {
			if id == rowText {
				return e.selectRow(ctx, s, m, m.rows[i])
			}
		}
		return failed("That row is no longer in the list. Nothing was chosen.")
	case "cancel":
		m := e.menuOf(ctx, s.id, token)
		if m == nil {
			return harness.ActionResult{Result: harness.ResultApplied}, nil
		}
		before, ok := e.readMenu(ctx, s)
		if ok {
			if err := e.menuKeys(ctx, s, "Escape"); err != nil {
				return harness.ActionResult{}, err
			}
			if after, still := e.readMenu(ctx, s); still && after.Title == before.Title && len(after.Items) == len(before.Items) {
				e.setMenu(s.id, nil)
				return failed("The menu did not close. Open the terminal to close it.")
			}
		}
		e.setMenu(s.id, nil)
		return harness.ActionResult{Result: harness.ResultApplied}, nil
	case "key":
		m := e.menuOf(ctx, s.id, token)
		if m == nil {
			return failed("This menu has closed.")
		}
		var key string
		_ = json.Unmarshal(req.Args["key"], &key)
		offered := false
		for _, k := range m.keys {
			offered = offered || k.key == key
		}
		if !offered {
			return failed("This menu does not offer that key.")
		}
		if now, ok := e.readMenu(ctx, s); !ok || now.Title != m.panel {
			e.setMenu(s.id, nil)
			return failed("The screen changed at the desk. Nothing was pressed.")
		}
		if err := e.menuKeys(ctx, s, key); err != nil {
			return harness.ActionResult{}, err
		}
		return e.afterKey(ctx, s, "", m)
	case "release":
		if e.menuOf(ctx, s.id, token) != nil {
			e.setMenu(s.id, nil)
		}
		return harness.ActionResult{Result: harness.ResultApplied}, nil
	}
	return harness.ActionResult{}, harness.ErrUnknownAction
}

// afterKey reads what the harness shows after a command or Enter: another
// list (returned as a screen), or nothing more (the list is done).
func (e *Engine) afterKey(ctx context.Context, s *sess, title string, previous *terminalMenu) (harness.ActionResult, error) {
	menu, ok := e.readMenu(ctx, s)
	if !ok {
		e.setMenu(s.id, nil)
		return harness.ActionResult{Result: harness.ResultApplied}, nil
	}
	rows, complete := e.wholeList(ctx, s, menu)
	// A list that scrolls without saying how long it is has been read only
	// as far as the screen shows.
	if menu.Total == 0 && !rowsEndOnScreen(menu) {
		complete = false
	}
	m := &terminalMenu{client: e.client(ctx), token: newToken(), title: title, panel: menu.Title, rows: rows, keys: hintKeys(menu.Hint), search: menu.Search, query: menu.Query}
	if previous != nil && previous.title != "" {
		m.title = previous.title
	}
	e.setMenu(s.id, m)
	return harness.ActionResult{Result: harness.ResultApplied, Screen: e.menuScreen(m, menu, complete)}, nil
}

// selectRow moves the harness's cursor to the row, one key at a time,
// checks the cursor is on it, and presses Enter. A screen that is no longer
// this list ends it without another key.
func (e *Engine) selectRow(ctx context.Context, s *sess, m *terminalMenu, want termmenu.Item) (harness.ActionResult, error) {
	fail := func(msg string) (harness.ActionResult, error) {
		e.setMenu(s.id, nil)
		return harness.ActionResult{Result: harness.ResultFailed, Message: msg}, nil
	}
	target := -1
	for i, it := range m.rows {
		if sameRow(it, want) {
			target = i
			break
		}
	}
	for step := 0; step <= len(m.rows)+2; step++ {
		now, ok := e.readMenu(ctx, s)
		if !ok {
			return fail("The screen changed at the desk. Nothing was chosen.")
		}
		cur := cursorOf(now)
		if cur < 0 {
			return fail("The screen changed at the desk. Nothing was chosen.")
		}
		if sameRow(now.Items[cur], want) {
			if err := e.menuKeys(ctx, s, "Enter"); err != nil {
				return harness.ActionResult{}, err
			}
			return e.afterKey(ctx, s, "", m)
		}
		// Where the cursor is in the whole list tells which way to go.
		at := -1
		for i, it := range m.rows {
			if sameRow(it, now.Items[cur]) {
				at = i
				break
			}
		}
		if at < 0 || target < 0 {
			return fail("The screen changed at the desk. Nothing was chosen.")
		}
		key := "Down"
		if at > target {
			key = "Up"
		}
		if err := e.menuKeys(ctx, s, key); err != nil {
			return harness.ActionResult{}, err
		}
	}
	return fail("The row could not be reached. Nothing was chosen.")
}

// typeMenuCommand types a command into the harness's input line and presses
// Enter. Many harnesses open a list of matching commands while one is
// typed and run the row their cursor is on: Enter is pressed only when
// that row is the command that was typed, so a near match is never run in
// its place. A message is returned when the command was not run.
func (e *Engine) typeMenuCommand(ctx context.Context, s *sess, text string) (string, error) {
	l := typingLock(s.id)
	l.Lock()
	defer l.Unlock()
	if err := e.heldNatively(s); err != nil {
		return "", err
	}
	if err := e.screenClear(ctx, s); err != nil {
		return "", err
	}
	// A list already open in the terminal (left by the desk, or by a
	// choice that could not be completed) would take the keys.
	if open, ok := e.readMenu(ctx, s); ok && open.Title != "" {
		return s.ad.Name + " has “" + open.Title + "” open in its terminal. Close it there first.", nil
	}
	e.waitInputLine(ctx, s)
	if err := e.clearInput(ctx, s); err != nil {
		return "", err
	}
	if err := e.cfg.Typist.Type(ctx, s.id, text, false, false); err != nil {
		return "", err
	}
	word := strings.TrimPrefix(strings.Fields(text)[0], "/")
	if menu, ok := e.readMenu(ctx, s); ok {
		if cur := cursorOf(menu); cur >= 0 {
			if on := strings.TrimPrefix(strings.Fields(menu.Items[cur].Label + " x")[0], "/"); on != word {
				_ = e.clearInput(ctx, s)
				return s.ad.Name + " would run “/" + on + "” for “" + text + "”. Nothing was run.", nil
			}
		}
	}
	if err := e.menuKeys(ctx, s, "Enter"); err != nil {
		return "", err
	}
	return "", nil
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
				menu, ok = termmenu.Menu{Title: m.panel, Search: true, Query: text}, true
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
	if query, asked := query["query"]; asked && menu.Search && query != menu.Query {
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
		if ctx.Err() != nil || (e.cfg.ClientGone != nil && e.cfg.ClientGone(ctx)) {
			e.setMenu(s.id, nil)
			return harness.Screen{}, errMenuAbandoned
		}
		if query != "" {
			if err := e.cfg.Typist.Type(ctx, s.id, query, false, false); err != nil {
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
			after = termmenu.Menu{Title: m.panel, Search: true, Query: query}
		}
		e.menuMu.Lock()
		m.rows, m.query, m.search = after.Items, query, true
		e.menuMu.Unlock()
		sc := e.menuScreen(m, after, after.Total > 0 || rowsEndOnScreen(after))
		return *sc, nil
	}
	e.menuMu.Lock()
	m.query, m.search = menu.Query, m.search || menu.Search
	if len(menu.Items) == 0 {
		m.rows = nil
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
	return *e.menuScreen(m, menu, true), nil
}

// errMenuAbandoned ends a list whose phone has gone: no further key.
var errMenuAbandoned = errors.New("harness: the phone is no longer there; nothing more was pressed")

// menuKeys presses keys in a list for the phone, and only while the phone
// is still there: a closed connection or a cancelled call stops the list
// before the next key, Enter included. The list is then one the phone let
// go of, and nothing is typed until it is gone from the screen.
func (e *Engine) menuKeys(ctx context.Context, s *sess, keys ...string) error {
	if ctx.Err() != nil || (e.cfg.ClientGone != nil && e.cfg.ClientGone(ctx)) {
		e.setMenu(s.id, nil)
		return errMenuAbandoned
	}
	return e.cfg.Typist.Keys(ctx, s.id, keys...)
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
