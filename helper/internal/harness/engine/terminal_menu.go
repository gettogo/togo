package engine

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
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
//
// A row id is "<interaction>/<n>"; the screen's id is "terminal.menu/<interaction>".

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
		delete(e.menus, session)
		return false
	}
	return true
}

func (e *Engine) menuOf(session, token string) *terminalMenu {
	e.menuMu.Lock()
	defer e.menuMu.Unlock()
	m := e.menus[session]
	if m == nil || m.token != token || time.Now().After(m.expires) {
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
		if err := e.cfg.Typist.Keys(ctx, s.id, "Down"); err != nil {
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
		if err := e.cfg.Typist.Keys(ctx, s.id, "Up"); err != nil {
			return all, false
		}
	}
	return all, len(all) >= first.Total
}

func (e *Engine) menuScreen(m *terminalMenu, menu termmenu.Menu, complete bool) *harness.Screen {
	interaction, _ := json.Marshal(m.token)
	p := &harness.Picker{OnSelect: terminalPrefix + "select", Searchable: false,
		OnSelectArgs: map[string]json.RawMessage{"interaction": interaction}}
	for i, it := range m.rows {
		id := m.token + "/" + strconv.Itoa(i)
		p.Items = append(p.Items, harness.PickerItem{ID: id, Title: it.Label, Subtitle: it.Detail, Group: it.Group})
		if it.Current {
			p.Selected = id
		}
	}
	title := menu.Title
	if title == "" {
		title = m.title
	}
	sc := &harness.Screen{ID: menuScreenPrefix + m.token, Interaction: m.token, Primitive: harness.PrimPicker, Title: title, FetchedAt: time.Now(), Picker: p}
	sc.Stale = !complete
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
	case "select":
		m := e.menuOf(s.id, token)
		n, err := strconv.Atoi(rowText)
		if m == nil || err != nil || n < 0 || n >= len(m.rows) {
			return failed("This menu has closed.")
		}
		return e.selectRow(ctx, s, m, m.rows[n])
	case "cancel":
		m := e.menuOf(s.id, token)
		if m == nil {
			return harness.ActionResult{Result: harness.ResultApplied}, nil
		}
		before, ok := e.readMenu(ctx, s)
		if ok {
			if err := e.cfg.Typist.Keys(ctx, s.id, "Escape"); err != nil {
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
		m := e.menuOf(s.id, token)
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
		if err := e.cfg.Typist.Keys(ctx, s.id, key); err != nil {
			return harness.ActionResult{}, err
		}
		return e.afterKey(ctx, s, "", m)
	case "release":
		if e.menuOf(s.id, token) != nil {
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
	m := &terminalMenu{token: newToken(), title: title, panel: menu.Title, rows: rows, keys: hintKeys(menu.Hint)}
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
			if err := e.cfg.Typist.Keys(ctx, s.id, "Enter"); err != nil {
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
		if err := e.cfg.Typist.Keys(ctx, s.id, key); err != nil {
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
	if err := e.cfg.Typist.Keys(ctx, s.id, "Enter"); err != nil {
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
	token := query["interaction"]
	if token == "" {
		token = fromName
	}
	m := e.menuOf(s.id, token)
	if m == nil || e.cfg.StyledScreen == nil {
		return harness.Screen{}, &harness.FallbackError{Reason: "closed", Detail: "This menu has closed."}
	}
	menu, ok := e.readMenu(ctx, s)
	if !ok {
		e.setMenu(s.id, nil)
		return harness.Screen{}, &harness.FallbackError{Reason: "closed", Detail: "This menu has closed."}
	}
	if menu.Title != m.panel {
		e.setMenu(s.id, nil)
		return harness.Screen{}, &harness.FallbackError{Reason: "moved", Detail: "The screen changed at the desk."}
	}
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
