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
		if e.awaitComposer(ctx, s, mark) {
			return harness.ActionResult{Result: harness.ResultApplied}, nil
		}
		return e.notShown(s, mark), nil
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
	if err := e.heldForMenu(s); err != nil {
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
	// The input line is not cleared: what is in it is not ours. Whether
	// it is empty is found out by typing the command: a harness offers its
	// commands only at the start of an empty line.
	mark := e.markComposer(ctx, s)
	before, err := e.cfg.Screen(ctx, s.id)
	if err != nil {
		return "", err
	}
	if err := e.menuType(ctx, s, text); err != nil {
		return "", err
	}
	// Where the text landed is read from the screen: the one line that
	// changed by exactly that text. The harness redraws a moment after the
	// keys arrive, so it is looked for until it shows.
	located := false
	var typedLine, was string
	for deadline := time.Now().Add(e.scaled(1500 * time.Millisecond)); ; {
		if after, err := e.cfg.Screen(ctx, s.id); err == nil {
			if typedLine, was, located = ownTextLine(before, after, text); located {
				break
			}
		}
		if !time.Now().Before(deadline) || ctx.Err() != nil {
			break
		}
		time.Sleep(e.scaled(60 * time.Millisecond))
	}
	if !located {
		// What was typed cannot be found, so it cannot be proven ours to
		// remove, or to run.
		return s.ad.Name + " did not take the command where expected. Nothing was run; check its input line.", nil
	}
	if x, _, _, ok := e.cursorAt(ctx, s); ok && mark.ok && x == mark.x+len([]rune(text)) {
		mark.tracks = true // the cursor followed what was typed
	}
	// takeBack removes exactly what was typed: one Backspace at a time,
	// each seen to shorten that line by the character expected, until the
	// line reads as it did. A step that is not seen stops it.
	takeBack := func() {
		at := strings.LastIndex(typedLine, text)
		if at < 0 {
			return
		}
		head, tail := typedLine[:at], typedLine[at+len(text):]
		rest := []rune(text)
		for len(rest) > 0 {
			if e.menuKeys(ctx, s, "BSpace") != nil {
				return
			}
			rest = rest[:len(rest)-1]
			want := strings.TrimRight(head+string(rest)+tail, " ")
			seen := false
			for deadline := time.Now().Add(e.scaled(700 * time.Millisecond)); time.Now().Before(deadline); {
				// The list a harness offers grows and shrinks as the text
				// changes and can move the input line to another row: the
				// line is looked for by what it must read now.
				if screen, err := e.cfg.Screen(ctx, s.id); err == nil {
					for _, l := range strings.Split(screen, "\n") {
						if now := trimEdge(l); now == want || (len(rest) == 0 && now == was) {
							seen = true
							break
						}
					}
				}
				if seen {
					break
				}
				time.Sleep(e.scaled(40 * time.Millisecond))
			}
			if !seen {
				return
			}
		}
	}
	// Text right after what was typed is a draft the cursor sat in front
	// of: the command would run with it, or replace it.
	if !strings.HasSuffix(typedLine, text) {
		takeBack()
		return s.ad.Name + "'s input line holds a draft. Nothing was run.", nil
	}
	word := strings.TrimPrefix(strings.Fields(text)[0], "/")
	var menu termmenu.Menu
	offered := false
	if screen, ok := e.settled(ctx, s); ok {
		menu, offered = termmenu.ReadOffered(screen)
	}
	cur := -1
	if offered {
		cur = cursorOf(menu)
	}
	if !offered || cur < 0 {
		// No list of commands opened: the line was not empty (a draft at
		// the desk), or this harness offers none. Nothing is run.
		takeBack()
		return s.ad.Name + " did not offer its commands: its input line may hold a draft. Nothing was run.", nil
	}
	if on := strings.TrimPrefix(strings.Fields(menu.Items[cur].Label + " x")[0], "/"); on != word {
		takeBack()
		return s.ad.Name + " would run “/" + on + "” for “" + text + "”. Nothing was run.", nil
	}
	// The input line was empty and is this one: its look is the proof,
	// later, that the harness is back at it.
	e.menuMu.Lock()
	if e.composers == nil {
		e.composers = map[string]composerMark{}
	}
	e.composers[s.id] = mark
	delete(e.awaiting, s.id)
	e.menuMu.Unlock()
	if err := e.menuKeys(ctx, s, "Enter"); err != nil {
		return "", err
	}
	// Some harnesses take the first Enter to accept the offered command
	// into the line and the second to run it: if the line still ends with
	// the command and no list or dialog took its place, Enter once more.
	if _, ok := e.readMenu(ctx, s); !ok {
		if x3, line3, _, ok := e.cursorAt(ctx, s); ok {
			held := strings.TrimRight(beforeCursor(line3, x3), " ")
			if strings.HasSuffix(held, "/"+word) || strings.HasSuffix(held, text) {
				if err := e.menuKeys(ctx, s, "Enter"); err != nil {
					return "", err
				}
			}
		}
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

// menuType types text for a list under the same check.
func (e *Engine) menuType(ctx context.Context, s *sess, text string) error {
	if err := e.menuStillOurs(ctx, s); err != nil {
		return err
	}
	e.menuKeyAt.Store(s.id, e.cfg.Now())
	return e.typeAsKeys(ctx, s, text)
}

// typeAsKeys types text for a list as keystrokes where the typist can: a
// harness reacts to keys (it offers commands, filters a list), not always
// to a paste.
func (e *Engine) typeAsKeys(ctx context.Context, s *sess, text string) error {
	if k, ok := e.cfg.Typist.(interface {
		TypeKeys(ctx context.Context, id, text string) error
	}); ok {
		return k.TypeKeys(ctx, s.id, text)
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

// beforeCursor is the text of a line up to the cursor's column.
func beforeCursor(line string, x int) string {
	rs := []rune(line)
	for len(rs) < x {
		rs = append(rs, ' ')
	}
	if x < 0 {
		x = 0
	}
	return string(rs[:x])
}

// ownTextLine finds the line text was typed on, by what the lines say and
// not by where they are: a harness that draws in the scrolling terminal
// moves its input line when its list of commands opens. It is the one line
// that now holds the text and, with the text taken out, reads as a line
// that was on the screen before (or as far as such a line goes, where an
// empty field showed a placeholder the text replaced). It returns that
// line and the same line without the text.
func ownTextLine(before, after, text string) (line, without string, ok bool) {
	had := map[string]bool{}
	var old []string
	for _, l := range strings.Split(before, "\n") {
		t := trimEdge(l)
		had[t] = true
		old = append(old, t)
	}
	found := 0
	for _, l := range strings.Split(after, "\n") {
		is := trimEdge(l)
		at := strings.LastIndex(is, text)
		if at < 0 || had[is] {
			continue
		}
		rest := strings.TrimRight(is[:at]+is[at+len(text):], " ")
		match := had[rest]
		if !match && strings.HasSuffix(is, text) {
			// An empty field's placeholder went when the text was typed.
			stem := strings.TrimRight(is[:at], " ")
			for _, o := range old {
				if stem != "" && o != stem && strings.HasPrefix(o, stem) {
					match = true
					break
				}
			}
		}
		if !match {
			continue
		}
		found++
		line, without = is, rest
	}
	return line, without, found == 1
}
