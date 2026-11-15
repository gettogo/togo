package engine

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strings"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
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
//	terminal.more     read further rows of a list that scrolls (same screen, more rows)
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
	token  string
	title  string
	panel  string // the list's own title, to tell it is still this list
	client any    // the connection that is driving the list
	// sess and conv are the running session and the conversation the list
	// was opened in: a restart or a new conversation can show a list with
	// the same title and rows, and it is not this one.
	sess  *sess
	conv  uint64
	query string // the text last seen or typed in the list's search field
	// scrolled: the rows were collected by moving through a list longer
	// than the screen; readQuery is the search they were read under.
	scrolled  bool
	readQuery string
	// ended: the end of the list has been seen (the cursor stopped, or
	// came round to where it began); first is the row the cursor was on
	// when the list was first read.
	ended bool
	// whole: when read, the rows were the whole list, all of it on the
	// screen, by the list's own counter.
	whole  bool
	first  string
	search bool
	// prompt: this is a text prompt, not a list; panel is its title.
	prompt bool
	// info: this is a panel that only tells something; panel is its title.
	info    bool
	rows    []termmenu.Item
	keys    []menuKey
	expires time.Time
}

func (e *Engine) drivingMenu(session string) bool {
	e.menuMu.Lock()
	defer e.menuMu.Unlock()
	m := e.menus[session]
	if m == nil {
		return false
	}
	if m.sess != nil && !e.stillServedLocked(m.sess, m.conv) {
		delete(e.menus, session)
		delete(e.leftOpen, session)
		return false
	}
	// The connection that was driving it has ended: the list is let go
	// now, as one left open, so the phone can take it up again at once.
	if m.client != nil && e.cfg.ClientClosed != nil && e.cfg.ClientClosed(m.client) {
		e.markLeftLocked(session, m)
		delete(e.menus, session)
		return false
	}
	if time.Now().After(m.expires) {
		e.markLeftLocked(session, m)
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
		// A list with no title of its own is known by its rows: other
		// text on the screen (a command the program echoes above its
		// input line) can read as an untitled list too.
		if menu, is := termmenu.Read(screen); is && menu.Title == panel && (panel != "" || e.sharesLeftRow(s.id, menu)) {
			return panel, true
		}
		// A text prompt left open takes typed keys just as a list does.
		if p, is := termmenu.ReadPrompt(screen); is && p.Title == panel {
			return panel, true
		}
		// So does a panel that only tells something: its keys act at once.
		if p, is := termmenu.ReadPanel(screen); is && p.Title == panel {
			return panel, true
		}
	}
	e.menuMu.Lock()
	delete(e.leftOpen, s.id)
	delete(e.leftRows, s.id)
	e.menuMu.Unlock()
	return "", false
}

// markLeftLocked remembers a list as left open: its title and its rows.
func (e *Engine) markLeftLocked(session string, m *terminalMenu) {
	if e.leftOpen == nil {
		e.leftOpen = map[string]string{}
	}
	if e.leftRows == nil {
		e.leftRows = map[string][]string{}
	}
	e.leftOpen[session] = m.panel
	rows := make([]string, 0, len(m.rows))
	for _, it := range m.rows {
		rows = append(rows, it.Label)
	}
	e.leftRows[session] = rows
}

// sharesLeftRow reports whether the list on the screen has a row of the
// one remembered as left open. With no rows remembered it cannot tell and
// says yes.
func (e *Engine) sharesLeftRow(session string, now termmenu.Menu) bool {
	e.menuMu.Lock()
	rows := e.leftRows[session]
	e.menuMu.Unlock()
	if len(rows) == 0 {
		return true
	}
	for _, it := range now.Items {
		if slices.Contains(rows, it.Label) {
			return true
		}
	}
	return false
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
	if m.sess != nil && !e.stillServedLocked(m.sess, m.conv) {
		// The session restarted or moved to another conversation: the list
		// went with it, and what the screen shows now is something else.
		delete(e.menus, session)
		delete(e.leftOpen, session)
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
			e.markLeftLocked(session, old)
		}
		delete(e.menus, session)
		return
	}
	m.expires = time.Now().Add(menuLife)
	e.menus[session] = m
}

func newToken() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// PhoneDriving reports whether what the session's terminal shows now is a
// screen the phone asked for: a command, choice or entry of its own is
// under way, or it holds the list or prompt that came of one. Such a
// screen is not the program asking for someone: nobody is told it waits.
func (e *Engine) PhoneDriving(session string) bool {
	e.menuMu.Lock()
	busy := e.commanding[session] > 0
	e.menuMu.Unlock()
	return busy || e.drivingMenu(session)
}

func (e *Engine) terminalAct(ctx context.Context, s *sess, req harness.ActionRequest) (harness.ActionResult, error) {
	ctx = withMenuConv(ctx, s)
	e.menuMu.Lock()
	if e.commanding == nil {
		e.commanding = map[string]int{}
	}
	e.commanding[s.id]++
	e.menuMu.Unlock()
	defer func() {
		e.menuMu.Lock()
		if e.commanding[s.id]--; e.commanding[s.id] <= 0 {
			delete(e.commanding, s.id)
		}
		e.menuMu.Unlock()
	}()
	// A new command, choice or key replaces any watch for a late list. A
	// look for an open list (attach) or a release does not: another phone
	// looking must not end the watch of the one that ran the command.
	if verb := req.Action; verb == "terminal.command" || verb == "terminal.select" || verb == "terminal.input" || strings.HasPrefix(verb, "terminal.key") {
		e.stopLateWatch(s.id)
	}
	if raw, ok := req.Args["requestId"]; ok {
		var id string
		if json.Unmarshal(raw, &id) == nil && id != "" && len(id) <= 128 {
			ctx = context.WithValue(ctx, requestIDKey{}, id)
		}
	}
	failed := func(msg string) (harness.ActionResult, error) {
		return harness.ActionResult{Result: harness.ResultFailed, Message: msg}, nil
	}
	if e.cfg.StyledScreen == nil || e.cfg.Typist == nil {
		return harness.ActionResult{Result: harness.ResultFallback, Message: menuNotRecognised}, nil
	}
	if e.terminalBarred(s.id) {
		return harness.ActionResult{Result: harness.ResultFallback, Message: terminalBarredText}, nil
	}
	// A row id is "<interaction>/<n>"; cancel and release name the
	// interaction in their arguments or as the target.
	// (A prompt's target is its field; its interaction comes in the
	// arguments only.)
	named := req.Target
	if req.Action == terminalPrefix+"input" {
		named = ""
	}
	token, rowText, _ := strings.Cut(strings.TrimPrefix(named, menuScreenPrefix), "/")
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
		// One command on one line: a slash, a name, its arguments. A line
		// break would submit what follows it as something else.
		if clean := strings.TrimSpace(text); len(clean) > maxArgString || !strings.HasPrefix(clean, "/") || len(clean) < 2 ||
			strings.ContainsFunc(clean, func(r rune) bool { return r < 0x20 || r == 0x7f || r == 0x85 || r == 0x2028 || r == 0x2029 }) {
			return failed("That is not a single command.")
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
			// A text prompt left open is taken up the same way.
			if p, is := e.readPrompt(ctx, s); is && p.Empty {
				return e.openPrompt(ctx, s, p.Title, p), nil
			}
			if p, is := e.readPanel(ctx, s); is {
				return e.openPanel(ctx, s, p.Title, p), nil
			}
		}
		if !chooser {
			return harness.ActionResult{Result: harness.ResultFallback, Message: "No menu is open in the terminal."}, nil
		}
		rows, complete := e.wholeList(ctx, s, menu)
		m := &terminalMenu{client: e.client(ctx), sess: s, conv: menuConv(ctx, s), token: newToken(), title: menu.Title, panel: menu.Title, rows: rows, keys: hintKeys(menu.Hint)}
		m.whole = wholeOnScreen(menu) && len(rows) == len(menu.Items)
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
				return e.selectRow(ctx, s, m, i)
			}
		}
		return failed("That row is no longer in the list. Nothing was chosen.")
	case "input":
		m := e.menuOf(ctx, s.id, token)
		if m == nil || !m.prompt {
			return failed("This prompt has closed.")
		}
		if req.Target != promptField {
			return failed("That is not this prompt's field.")
		}
		var value string
		if err := json.Unmarshal(req.Args["value"], &value); err != nil {
			return failed("No entry was given.")
		}
		return e.answerPrompt(ctx, s, m, value)
	case "cancel":
		m := e.menuOf(ctx, s.id, token)
		if m == nil {
			return harness.ActionResult{Result: harness.ResultApplied}, nil
		}
		if m.info {
			closed, err := e.closePanel(ctx, s, m)
			e.setMenu(s.id, nil)
			if err != nil {
				return harness.ActionResult{}, err
			}
			if !closed {
				return failed("The panel did not close. Open the terminal to close it.")
			}
			return harness.ActionResult{Result: harness.ResultApplied}, nil
		}
		if m.prompt {
			// Escape goes only to the prompt this was: one still on the
			// screen under the same title.
			if p, is := e.readPrompt(ctx, s); is && p.Title == m.panel {
				if err := e.menuKeys(ctx, s, "Escape"); err != nil {
					return harness.ActionResult{}, err
				}
				closed := false
				for deadline := time.Now().Add(e.scaled(2 * time.Second)); time.Now().Before(deadline); {
					if after, still := e.readPrompt(ctx, s); !still || after.Title != m.panel {
						closed = true
						break
					}
					time.Sleep(e.scaled(80 * time.Millisecond))
				}
				if !closed {
					e.setMenu(s.id, nil)
					return failed("The prompt did not close. Open the terminal to close it.")
				}
			}
			e.setMenu(s.id, nil)
			return harness.ActionResult{Result: harness.ResultApplied}, nil
		}
		before, ok := e.readMenu(ctx, s)
		if ok && before.Title != m.panel {
			// Another list is on the screen now: it is not this one to close.
			e.setMenu(s.id, nil)
			return failed("The screen changed at the desk. Nothing was pressed.")
		}
		if ok {
			if err := e.menuKeys(ctx, s, "Escape"); err != nil {
				return harness.ActionResult{}, err
			}
			same := func() bool {
				after, still := e.readMenu(ctx, s)
				return still && after.Title == before.Title && len(after.Items) == len(before.Items)
			}
			// A list with a search field in it may give its first Escape
			// to that field. One more, only while the same list is still
			// there.
			if same() {
				if err := e.menuKeys(ctx, s, "Escape"); err != nil {
					return harness.ActionResult{}, err
				}
				if same() {
					e.setMenu(s.id, nil)
					return failed("The menu did not close. Open the terminal to close it.")
				}
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
	case "more":
		m := e.menuOf(ctx, s.id, token)
		if m == nil {
			return failed("This menu has closed.")
		}
		sc, err := e.pageMenu(ctx, s, m)
		if err != nil {
			return harness.ActionResult{}, err
		}
		return harness.ActionResult{Result: harness.ResultApplied, Screen: sc}, nil
	case "release":
		if e.menuOf(ctx, s.id, token) != nil {
			e.setMenu(s.id, nil)
		}
		return harness.ActionResult{Result: harness.ResultApplied}, nil
	}
	return harness.ActionResult{}, harness.ErrUnknownAction
}

type menuConvKey struct{}

// withMenuConv records the conversation a call began in, before it waits
// for anything: every key it presses later is pressed only if the session
// is still that running session in that conversation.
func withMenuConv(ctx context.Context, s *sess) context.Context {
	s.mu.Lock()
	conv := s.convGen
	s.mu.Unlock()
	return context.WithValue(ctx, menuConvKey{}, conv)
}

func menuConv(ctx context.Context, s *sess) uint64 {
	if conv, ok := ctx.Value(menuConvKey{}).(uint64); ok {
		return conv
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.convGen
}

// stillServedLocked is stillServed for callers that hold menuMu.
func (e *Engine) stillServedLocked(s *sess, conv uint64) bool {
	return e.stillServed(s, conv)
}
