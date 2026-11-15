package engine

import (
	"context"
	"strings"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/spec"
	"github.com/gettogo/togo/helper/internal/harness/termmenu"
)

// A harness lists its own commands when "/" is typed in its input line:
// every command it has for this session, with what each does. Where no
// better source says so, that list is read once and kept: "/" is typed, the
// list is walked with the cursor, and the "/" is removed again.
//
// Nobody asked for these keys on this screen, so each one is pressed only
// while the screen is provably still as this reading left it: the line the
// "/" was typed on must read exactly as it did right after. A key at the
// desk changes that line, and then nothing more is pressed, the "/"
// included: what is on the line is no longer only ours to remove. The "/"
// is taken back only from a line that still reads as we left it.
//
// The list belongs to one running session and one conversation; a restart
// or a new conversation reads it again.

const (
	catalogLife   = 10 * time.Minute
	catalogRetry  = 30 * time.Second
	catalogBudget = 12 * time.Second
	catalogStep   = 45 * time.Millisecond
)

type terminalCatalog struct {
	rows    []harness.Completion
	conv    uint64
	at      time.Time
	reading bool
	failed  time.Time
}

// terminalCommands returns the commands read from the harness's own list
// and whether they are still being read; it starts the reading when they
// are not known yet.
func (e *Engine) terminalCommands(s *sess) (rows []harness.Completion, pending bool) {
	if e.cfg.StyledScreen == nil || e.cfg.Screen == nil || e.cfg.Typist == nil {
		return nil, false
	}
	s.mu.Lock()
	conv := s.convGen
	s.mu.Unlock()
	e.menuMu.Lock()
	defer e.menuMu.Unlock()
	if e.catalogs == nil {
		e.catalogs = map[*sess]*terminalCatalog{}
	}
	c := e.catalogs[s]
	if c == nil || c.conv != conv {
		c = &terminalCatalog{conv: conv}
		e.catalogs[s] = c
	}
	fresh := len(c.rows) > 0 && time.Since(c.at) < catalogLife
	if !fresh && !c.reading && time.Since(c.failed) > catalogRetry {
		c.reading = true
		go e.readCatalog(s, c)
	}
	return c.rows, c.reading
}

func (e *Engine) readCatalog(s *sess, c *terminalCatalog) {
	ctx, cancel := context.WithTimeout(context.Background(), catalogBudget)
	defer cancel()
	rows, whole := e.walkCommandList(ctx, s, c.conv)
	e.menuMu.Lock()
	defer e.menuMu.Unlock()
	c.reading = false
	if len(rows) == 0 {
		c.failed = time.Now()
		return
	}
	c.rows, c.at = rows, time.Now()
	if !whole {
		// Interrupted (someone typed at the desk): what was read is kept
		// and the rest is read on a later try.
		c.at = time.Now().Add(catalogRetry - catalogLife)
	}
	// Tell the open chats the list has grown; they ask for it again.
	go func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.notifyLocked(harness.Notification{Kind: harness.NoteCommands})
	}()
}

// stillServed reports that s is the running session for its id and is in
// the conversation the reading began in.
func (e *Engine) stillServed(s *sess, conv uint64) bool {
	e.mu.Lock()
	current := e.sessions[s.id]
	e.mu.Unlock()
	if current != s {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.convGen == conv
}

// ownLine finds the line a "/" was typed on: the one line of the screen
// that now ends with "/" and, without it, is the line as it was (or as far
// as it goes, where an empty field showed a placeholder that the "/"
// replaced). A row of the list that opened holds a "/" too, but does not
// end with one.
func ownLine(before, after string) (index int, text string, ok bool) {
	a, b := strings.Split(before, "\n"), strings.Split(after, "\n")
	index = -1
	for i := range b {
		if i >= len(a) {
			break
		}
		was, is := trimEdge(a[i]), trimEdge(b[i])
		if was == is || !strings.HasSuffix(is, "/") {
			continue
		}
		stem := strings.TrimRight(strings.TrimSuffix(is, "/"), " ")
		if stem != was && !(stem != "" && strings.HasPrefix(was, stem)) {
			continue
		}
		if index >= 0 {
			return -1, "", false // two candidates: not sure which is ours
		}
		index, text = i, is
	}
	return index, text, index >= 0
}

// walkCommandList types "/", reads the list that opens with the cursor on
// each row in turn, and removes the "/" again.
func (e *Engine) walkCommandList(ctx context.Context, s *sess, conv uint64) (rows []harness.Completion, whole bool) {
	if !e.idleNow(s) || e.drivingMenu(s.id) || !e.stillServed(s, conv) {
		return nil, false
	}
	l := typingLock(s.id)
	if !l.TryLock() {
		return nil, false
	}
	defer l.Unlock()
	if e.heldNatively(s) != nil || e.screenClear(ctx, s) != nil || !e.idleNow(s) {
		return nil, false
	}
	styled, err := e.cfg.StyledScreen(ctx, s.id)
	if err != nil {
		return nil, false
	}
	if _, open := termmenu.Read(styled); open {
		return nil, false // a list is already on the screen: not ours to type into
	}
	before, err := e.cfg.Screen(ctx, s.id)
	if err != nil {
		return nil, false
	}
	e.menuKeyAt.Store(s.id, e.cfg.Now())
	if err := e.typeAsKeys(ctx, s, "/"); err != nil {
		return nil, false
	}
	first, opened := e.readMenu(ctx, s)
	after, err := e.cfg.Screen(ctx, s.id)
	if err != nil {
		return nil, false
	}
	// Found by what the line says, wherever it now is: a harness that
	// draws in the scrolling terminal moves its input line when the list
	// of commands opens.
	line, _, located := ownTextLine(before, after, "/")
	// ours reports that the line the "/" went on still reads as we left it.
	ours := func() bool {
		if !e.stillServed(s, conv) {
			return false
		}
		now, err := e.cfg.Screen(ctx, s.id)
		if err != nil {
			return false
		}
		if !located {
			// The line could not be picked out, so nothing is walked; the
			// "/" is still ours to take back only if the whole screen is
			// exactly as it was right after it was typed.
			return now == after
		}
		for _, l := range strings.Split(now, "\n") {
			if trimEdge(l) == line {
				return true
			}
		}
		return false
	}
	// The "/" is taken back only from a line that is still as we left it.
	defer func() {
		back, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		ctx = back
		if ours() {
			_ = e.cfg.Typist.Keys(back, s.id, "BSpace")
		}
	}()
	if !opened || cursorOf(first) < 0 || !located {
		return nil, false
	}
	var out []harness.Completion
	seen := map[string]bool{}
	add := func(m termmenu.Menu) int {
		n := 0
		for _, it := range m.Items {
			name, ok := commandName(it.Label)
			if !ok || seen[name] {
				continue
			}
			seen[name] = true
			n++
			out = append(out, harness.Completion{Insert: "/" + name + " ", Title: "/" + name, Detail: it.Detail,
				Kind: spec.CommandRun, Native: &harness.CommandNative{Opens: harness.OpensUnknown, Group: it.Group}})
		}
		return n
	}
	add(first)
	start := first.Items[cursorOf(first)].Label
	idle := 0
	for step := 0; step < menuMaxSteps && len(out) < menuMaxRows && ctx.Err() == nil; step++ {
		if first.Total > 0 && len(out) >= first.Total {
			break
		}
		// Each key only while the line is still ours.
		if !ours() {
			return out, false
		}
		e.menuKeyAt.Store(s.id, e.cfg.Now())
		if err := e.cfg.Typist.Keys(ctx, s.id, "Down"); err != nil {
			return out, false
		}
		var m termmenu.Menu
		got := false
		for try := 0; try < 4 && !got; try++ {
			select {
			case <-ctx.Done():
			case <-time.After(e.scaled(catalogStep)):
			}
			if screen, err := e.cfg.StyledScreen(ctx, s.id); err == nil {
				m, got = termmenu.Read(screen)
			}
		}
		if !got || cursorOf(m) < 0 || m.Title != first.Title {
			return out, false
		}
		if add(m) == 0 {
			idle++
		} else {
			idle = 0
		}
		if (step > 0 && m.Items[cursorOf(m)].Label == start) || idle > len(m.Items)+2 {
			break
		}
	}
	return out, ctx.Err() == nil
}

// trimEdge drops trailing spaces and, from a line drawn in a box (an edge
// at both ends), the closing edge.
func trimEdge(line string) string {
	t := strings.TrimRight(line, " ")
	rs := []rune(strings.TrimLeft(t, " "))
	if len(rs) > 1 && strings.ContainsRune("│┃|", rs[0]) && strings.ContainsRune("│┃|", rs[len(rs)-1]) {
		t = strings.TrimRight(strings.TrimSuffix(t, string(rs[len(rs)-1])), " ")
	}
	return t
}

// commandName is the command a row of a harness's command list names: its
// first word without the "/", made of letters, digits and - _ : . only and
// starting with a letter or digit. A row that is part of the list's frame
// (a border, a counter) names none.
func commandName(label string) (string, bool) {
	words := strings.Fields(label)
	if len(words) == 0 {
		return "", false
	}
	name := strings.TrimPrefix(words[0], "/")
	if name == "" {
		return "", false
	}
	for i, r := range name {
		letter := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
		if !letter && (i == 0 || !strings.ContainsRune("-_:.", r)) {
			return "", false
		}
	}
	return name, true
}
