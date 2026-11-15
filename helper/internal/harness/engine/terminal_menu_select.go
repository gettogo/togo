package engine

import (
	"context"
	"errors"
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

// firstSame is the first of rows that says what it says, or -1.
func firstSame(rows []termmenu.Item, it termmenu.Item) int {
	for i, r := range rows {
		if sameRow(r, it) {
			return i
		}
	}
	return -1
}

// knownRow finds a row read from the screen among the rows known, or -1.
// A list that scrolls takes its group headings off the screen, and a row
// then reads with no group. Such a row is the known row of that name when
// exactly one has it. When a grouped row and any other share the name, the
// row on the screen could be either, and it is not guessed at.
func knownRow(rows []termmenu.Item, it termmenu.Item) int {
	if it.Group != "" {
		return firstSame(rows, it)
	}
	at, n, grouped := -1, 0, false
	for i, r := range rows {
		if r.Label != it.Label {
			continue
		}
		if at < 0 {
			at = i
		}
		n++
		grouped = grouped || r.Group != ""
	}
	if grouped && n > 1 {
		return -1
	}
	return at
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
		// No list. Done only if the input line is seen again; otherwise
		// the harness shows something the phone cannot.
		e.menuMu.Lock()
		mark := e.composers[s.id]
		if previous != nil {
			// The list this followed is over either way, and is not left
			// behind as an open one.
			delete(e.menus, s.id)
		}
		e.menuMu.Unlock()
		// A harness may show its input line for a moment before the list
		// it is fetching opens (Kimi's model list comes a second or two
		// after Enter). So the input line must stay, with no list, for a
		// while before the command counts as done; a list that opens in
		// that time is the command's list.
		var backSince time.Time
		for deadline := time.Now().Add(e.scaled(menuSettle + lateList)); ; {
			if late, opened := e.readMenu(ctx, s); opened {
				menu, ok = late, true
				break
			}
			// A prompt for one line of text is shown as its own screen.
			// One already filled in (an editor opened with text) cannot be
			// answered from an empty field: it is left to the terminal.
			if p, is := e.readPrompt(ctx, s); is {
				if !p.Empty {
					return e.notShown(s, mark), nil
				}
				return e.openPrompt(ctx, s, title, p), nil
			}
			// A panel that only tells something is its own screen too.
			if p, is := e.readPanel(ctx, s); is {
				return e.openPanel(ctx, s, title, p), nil
			}
			if e.composerBack(ctx, s, mark) {
				if backSince.IsZero() {
					backSince = time.Now()
				} else if time.Since(backSince) >= e.scaled(lateList) {
					// Nothing was shown in the time waited. That is not
					// proof nothing will be: the screen is watched for a
					// while longer before anything is typed (lateWatch).
					e.quietDone.Store(s.id, e.cfg.Now())
					if previous != nil {
						// A list was open and closed into the input line:
						// that is an effect seen.
						return harness.ActionResult{Result: harness.ResultApplied}, nil
					}
					// Typed and submitted; nothing was seen to come of it.
					// Not "done": the harness may yet open its list, and
					// the screen is watched for it.
					e.watchLateList(ctx, s)
					return harness.ActionResult{Result: harness.ResultTyped}, nil
				}
			} else {
				backSince = time.Time{}
			}
			if !time.Now().Before(deadline) || ctx.Err() != nil {
				break
			}
			time.Sleep(e.scaled(menuQuiet))
		}
		if !ok {
			return e.notShown(s, mark), nil
		}
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
	m.whole = complete && len(rows) == len(menu.Items)
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
	nudged := false
	for step := 0; step <= len(m.rows)+3; step++ {
		now, ok := e.readMenu(ctx, s)
		if !ok {
			now, ok = e.readMenu(ctx, s) // a list that is redrawing: one more look
		}
		if !ok {
			return fail("The screen changed at the desk. Nothing was chosen.")
		}
		cur := cursorOf(now)
		if now.Title != m.panel {
			return fail("The screen changed at the desk. Nothing was chosen.")
		}
		if cur < 0 {
			// The list has no cursor on a row yet (a search just cleared):
			// one Down brings it in, once.
			if nudged {
				return fail("The screen changed at the desk. Nothing was chosen.")
			}
			nudged = true
			if err := e.menuKeys(ctx, s, "Down"); err != nil {
				return harness.ActionResult{}, err
			}
			continue
		}
		// With the whole list on the screen every heading is on it too:
		// a row is what it reads as, and its place tells it from a twin.
		// The same number of rows is not that: a list that scrolled shows
		// as many, without the heading that left.
		whole := m.wholeOn(now)
		onIt := sameRow(now.Items[cur], want)
		if !whole {
			onIt = knownRow(m.rows, now.Items[cur]) == firstSame(m.rows, want)
		}
		if onIt && rightOccurrence(m.rows, whole, cur, target) {
			if err := e.menuKeys(ctx, s, "Enter"); err != nil {
				return harness.ActionResult{}, err
			}
			return e.afterKey(ctx, s, "", m)
		}
		// Where the cursor is in the whole list tells which way to go.
		at := -1
		if whole {
			at = cur
		} else {
			if twins > 1 {
				return fail("Two rows in this list say the same and it does not fit the screen. Choose it in the terminal.")
			}
			at = knownRow(m.rows, now.Items[cur])
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

// lateWatch is how long after a command that showed nothing the screen is
// still checked for a list before anything is typed.
const lateWatch = 60 * time.Second

// lateList is how long the input line must stay, with no list, before a
// command that showed none counts as done.
const lateList = 2500 * time.Millisecond

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
	e.menuKeyAt.Store(s.id, e.cfg.Now())
	return e.cfg.Typist.Keys(ctx, s.id, keys...)
}

// menuActivity is how long after the last key in a list the pane's changes
// are still taken for the list's, not the agent's.
const menuActivity = 8 * time.Second

// menuKeysJustNow reports that a key was pressed in a list for the phone a
// moment ago, or that a list is open for it now.
func (e *Engine) menuKeysJustNow(session string, now time.Time) bool {
	if v, ok := e.menuKeyAt.Load(session); ok {
		if at, _ := v.(time.Time); now.Sub(at) < menuActivity {
			return true
		}
	}
	return false
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

// errMenuMoved ends a list whose session restarted or changed conversation.
var errMenuMoved = &harness.FallbackError{Reason: "moved", Detail: "The session moved on. Nothing more was pressed."}

// rightOccurrence reports whether the cursor is on the row that was meant
// and not on another that says the same. With the whole list on screen the
// place decides; a row that says something no other row says is its own.
func rightOccurrence(all []termmenu.Item, whole bool, cursor, target int) bool {
	twins := 0
	for _, it := range all {
		if sameRow(it, all[target]) {
			twins++
		}
	}
	if twins <= 1 {
		return true
	}
	return whole && cursor == target
}

// wholeOn reports whether the screen shows the whole list as it was read:
// the list was known to be complete, and every row now on the screen is
// the row known at that place.
func (m *terminalMenu) wholeOn(now termmenu.Menu) bool {
	if !m.whole || len(now.Items) != len(m.rows) {
		return false
	}
	for i, it := range now.Items {
		if !sameRow(it, m.rows[i]) {
			return false
		}
	}
	return true
}
