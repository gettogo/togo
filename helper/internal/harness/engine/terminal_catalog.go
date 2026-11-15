package engine

import (
	"context"
	"strings"
	"sync"
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
// It is read only when nothing is in the way: the session is idle, no list
// is open, nobody else is typing. Typing "/" after a draft does not open a
// command list, and the "/" is then removed, which leaves the draft as it
// was. The reading runs on its own; the command menu shows it once it is
// there.

const (
	catalogLife   = 10 * time.Minute
	catalogRetry  = 30 * time.Second
	catalogBudget = 12 * time.Second
	catalogStep   = 45 * time.Millisecond
)

type terminalCatalog struct {
	rows    []harness.Completion
	at      time.Time
	reading bool
	failed  time.Time
}

var catalogs sync.Map // session id -> *terminalCatalog, guarded by catalogMu
var catalogMu sync.Mutex

// terminalCommands returns the commands read from the harness's own list,
// and starts reading them when they are not known yet.
func (e *Engine) terminalCommands(s *sess) []harness.Completion {
	if e.cfg.StyledScreen == nil || e.cfg.Typist == nil {
		return nil
	}
	catalogMu.Lock()
	defer catalogMu.Unlock()
	v, _ := catalogs.LoadOrStore(s.id, &terminalCatalog{})
	c := v.(*terminalCatalog)
	fresh := len(c.rows) > 0 && time.Since(c.at) < catalogLife
	if !fresh && !c.reading && time.Since(c.failed) > catalogRetry {
		c.reading = true
		go e.readCatalog(s, c)
	}
	return c.rows
}

func (e *Engine) readCatalog(s *sess, c *terminalCatalog) {
	ctx, cancel := context.WithTimeout(context.Background(), catalogBudget)
	defer cancel()
	rows := e.walkCommandList(ctx, s)
	catalogMu.Lock()
	defer catalogMu.Unlock()
	c.reading = false
	if len(rows) == 0 {
		c.failed = time.Now()
		return
	}
	c.rows, c.at = rows, time.Now()
}

// walkCommandList types "/", reads the list that opens with the cursor on
// each row in turn, and removes the "/" again.
func (e *Engine) walkCommandList(ctx context.Context, s *sess) []harness.Completion {
	if !e.idleNow(s) || e.drivingMenu(s.id) {
		return nil
	}
	l := typingLock(s.id)
	if !l.TryLock() {
		return nil
	}
	defer l.Unlock()
	if e.heldNatively(s) != nil || e.screenClear(ctx, s) != nil || !e.idleNow(s) {
		return nil
	}
	if before, err := e.cfg.StyledScreen(ctx, s.id); err != nil {
		return nil
	} else if _, open := termmenu.Read(before); open {
		return nil // a list is already on the screen: not ours to type into
	}
	if err := e.cfg.Typist.Type(ctx, s.id, "/", false, false); err != nil {
		return nil
	}
	// Whatever happens next, the "/" is taken back.
	defer func() {
		back, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = e.cfg.Typist.Keys(back, s.id, "BSpace")
	}()
	first, ok := e.readMenu(ctx, s)
	if !ok || cursorOf(first) < 0 {
		return nil
	}
	var out []harness.Completion
	seen := map[string]bool{}
	add := func(m termmenu.Menu) int {
		n := 0
		for _, it := range m.Items {
			name := strings.TrimPrefix(strings.Fields(it.Label + " ")[0], "/")
			if name == "" || seen[name] || strings.ContainsAny(name, "()[]<>") {
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
		if err := e.cfg.Typist.Keys(ctx, s.id, "Down"); err != nil {
			break
		}
		// One quick look is enough here: a row missed on one step is seen
		// on the next, and the walk ends when nothing new turns up.
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
		if !got || cursorOf(m) < 0 {
			break
		}
		if add(m) == 0 {
			idle++
		} else {
			idle = 0
		}
		// Back at the row it started on, or nothing new for a full
		// window of steps: the list has been seen.
		if (step > 0 && m.Items[cursorOf(m)].Label == start) || idle > len(m.Items)+2 {
			break
		}
	}
	return out
}
