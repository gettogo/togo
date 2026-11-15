package engine

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/termmenu"
)

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
	// A long list is walked for a bounded time only; the rest can be asked
	// for (Show more), so a request never outlives its caller's patience.
	deadline := time.Now().Add(e.scaled(wholeListBudget))
	m, steps := first, 0
	for steps < menuMaxSteps && len(all) < menuMaxRows && len(all) < first.Total && time.Now().Before(deadline) {
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
	m := &terminalMenu{client: e.client(ctx), sess: s, conv: menuConv(ctx, s), token: newToken(), title: title, panel: menu.Title, rows: rows, keys: hintKeys(menu.Hint),
		search: menu.Search, query: menu.Query, readQuery: menu.Query, scrolled: len(rows) > len(menu.Items)}
	if previous != nil && previous.title != "" {
		m.title = previous.title
	}
	e.setMenu(s.id, m)
	return harness.ActionResult{Result: harness.ResultApplied, Screen: e.menuScreen(m, menu, complete)}, nil
}

// selectRow moves the harness's cursor to the row, one key at a time,
// checks the cursor is on it, and presses Enter. A screen that is no longer
// this list ends it without another key.
func (e *Engine) selectRow(ctx context.Context, s *sess, m *terminalMenu, target int) (harness.ActionResult, error) {
	want := m.rows[target]
	fail := func(msg string) (harness.ActionResult, error) {
		e.setMenu(s.id, nil)
		return harness.ActionResult{Result: harness.ResultFailed, Message: msg}, nil
	}
	// Two rows that say the same can be told apart only by their place,
	// and that is known only while the whole list is on the screen.
	twins := 0
	for _, it := range m.rows {
		if sameRow(it, want) {
			twins++
		}
	}
	for step := 0; step <= len(m.rows)+2; step++ {
		now, ok := e.readMenu(ctx, s)
		if !ok {
			return fail("The screen changed at the desk. Nothing was chosen.")
		}
		cur := cursorOf(now)
		if cur < 0 || now.Title != m.panel {
			return fail("The screen changed at the desk. Nothing was chosen.")
		}
		if sameRow(now.Items[cur], want) && rightOccurrence(m.rows, now.Items, cur, target) {
			if err := e.menuKeys(ctx, s, "Enter"); err != nil {
				return harness.ActionResult{}, err
			}
			return e.afterKey(ctx, s, "", m)
		}
		// Where the cursor is in the whole list tells which way to go.
		at := -1
		if len(now.Items) == len(m.rows) {
			at = cur
		} else {
			if twins > 1 {
				return fail("Two rows in this list say the same and it does not fit the screen. Choose it in the terminal.")
			}
			for i, it := range m.rows {
				if sameRow(it, now.Items[cur]) {
					at = i
					break
				}
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
	if err := e.menuType(ctx, s, text); err != nil {
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

// wholeListBudget bounds the walk of a list that says how long it is.
const wholeListBudget = 4 * time.Second

// errMenuAbandoned ends a list whose phone has gone: no further key.
var errMenuAbandoned = errors.New("harness: the phone is no longer there; nothing more was pressed")

// menuKeys presses keys in a list for the phone, and only while the phone
// is still there: a closed connection or a cancelled call stops the list
// before the next key, Enter included. The list is then one the phone let
// go of, and nothing is typed until it is gone from the screen.
func (e *Engine) menuKeys(ctx context.Context, s *sess, keys ...string) error {
	if err := e.menuStillOurs(ctx, s); err != nil {
		return err
	}
	return e.cfg.Typist.Keys(ctx, s.id, keys...)
}

// menuStillOurs is checked before every key and every text typed for a
// list: the phone is still there, and the session is still the running
// session, in the conversation the call began in.
func (e *Engine) menuStillOurs(ctx context.Context, s *sess) error {
	if ctx.Err() != nil || (e.cfg.ClientGone != nil && e.cfg.ClientGone(ctx)) {
		e.setMenu(s.id, nil)
		return errMenuAbandoned
	}
	if conv, ok := ctx.Value(menuConvKey{}).(uint64); ok && !e.stillServed(s, conv) {
		e.menuMu.Lock()
		delete(e.menus, s.id)
		delete(e.leftOpen, s.id)
		e.menuMu.Unlock()
		return errMenuMoved
	}
	return nil
}

// menuType types text for a list under the same check.
func (e *Engine) menuType(ctx context.Context, s *sess, text string) error {
	if err := e.menuStillOurs(ctx, s); err != nil {
		return err
	}
	return e.cfg.Typist.Type(ctx, s.id, text, false, false)
}

// errMenuMoved ends a list whose session restarted or changed conversation.
var errMenuMoved = &harness.FallbackError{Reason: "moved", Detail: "The session moved on. Nothing more was pressed."}

// rightOccurrence reports whether the cursor is on the row that was meant
// and not on another that says the same. With the whole list on screen the
// place decides; a row that says something no other row says is its own.
func rightOccurrence(all, visible []termmenu.Item, cursor, target int) bool {
	twins := 0
	for _, it := range all {
		if sameRow(it, all[target]) {
			twins++
		}
	}
	if twins <= 1 {
		return true
	}
	return len(visible) == len(all) && cursor == target
}
