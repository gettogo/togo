package engine

import (
	"context"
	"strings"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/termmenu"
)

// When a command or a choice leaves no list on the screen, it is done only
// if the harness is back at its input line. The line's text is not proof:
// the input line can stay visible behind a dialog. The cursor is: a harness
// keeps it where typing goes. So the input line is "back" when the cursor
// is again in a line that reads, up to the cursor, as the input line did
// before the command was typed, with nothing typed after it, and no list
// is on the screen.
//
// Anything else is something the phone cannot show. That is said plainly,
// nothing is replayed, and nothing is typed into the session until the
// input line is seen again.

// composerMark is the input line as it was with the cursor in it: the text
// before the cursor, and the cursor's column.
type composerMark struct {
	prefix string
	x      int
	ok     bool
}

// markComposer records the empty input line the cursor is in.
func (e *Engine) markComposer(ctx context.Context, s *sess) composerMark {
	if e.cfg.Cursor == nil || e.cfg.Screen == nil {
		return composerMark{}
	}
	x, y, _, err := e.cfg.Cursor(ctx, s.id)
	if err != nil {
		return composerMark{}
	}
	screen, err := e.cfg.Screen(ctx, s.id)
	if err != nil {
		return composerMark{}
	}
	lines := strings.Split(screen, "\n")
	if y < 0 || y >= len(lines) {
		return composerMark{}
	}
	line := []rune(lines[y])
	for len(line) < x {
		line = append(line, ' ')
	}
	prefix := string(line[:x])
	// A line with nothing before the cursor says nothing about where it
	// is: a blank line looks the same anywhere.
	if strings.TrimSpace(prefix) == "" {
		return composerMark{}
	}
	return composerMark{prefix: prefix, x: x, ok: true}
}

// composerBack reports that the cursor is in the input line again, with
// nothing typed in it, and that no list is on the screen.
func (e *Engine) composerBack(ctx context.Context, s *sess, mark composerMark) bool {
	if !mark.ok || e.cfg.Cursor == nil || e.cfg.Screen == nil {
		return false
	}
	x, y, _, err := e.cfg.Cursor(ctx, s.id)
	if err != nil || x != mark.x {
		return false
	}
	screen, err := e.cfg.Screen(ctx, s.id)
	if err != nil {
		return false
	}
	lines := strings.Split(screen, "\n")
	if y < 0 || y >= len(lines) {
		return false
	}
	line := []rune(lines[y])
	for len(line) < x {
		line = append(line, ' ')
	}
	if string(line[:x]) != mark.prefix {
		return false
	}
	if e.cfg.StyledScreen != nil {
		if styled, err := e.cfg.StyledScreen(ctx, s.id); err == nil {
			if menu, open := termmenu.Read(styled); open && menu.Title != "" {
				return false
			}
		}
	}
	return true
}

// awaitComposer waits for the input line after a key that left no list.
func (e *Engine) awaitComposer(ctx context.Context, s *sess, mark composerMark) bool {
	deadline := time.Now().Add(e.scaled(menuSettle))
	for {
		if e.composerBack(ctx, s, mark) {
			return true
		}
		if !time.Now().Before(deadline) {
			return false
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(e.scaled(menuQuiet)):
		}
	}
}

// notShown is the result when the harness shows something the phone cannot.
func (e *Engine) notShown(s *sess, mark composerMark) harness.ActionResult {
	e.menuMu.Lock()
	if e.awaiting == nil {
		e.awaiting = map[string]composerMark{}
	}
	e.awaiting[s.id] = mark
	e.menuMu.Unlock()
	return harness.ActionResult{Result: harness.ResultFallback,
		Message: s.ad.Name + " is showing something that cannot be shown here. Open the terminal."}
}

// awaitingComposer reports that the session's input line has not been seen
// since something the phone could not show opened. Once it is, the session
// is free again.
func (e *Engine) awaitingComposer(s *sess) bool {
	e.menuMu.Lock()
	mark, waiting := e.awaiting[s.id]
	e.menuMu.Unlock()
	if !waiting {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if mark.ok && !e.composerBack(ctx, s, mark) {
		return true
	}
	if !mark.ok {
		// Never known: only a new mark, taken when the phone next runs a
		// command, can free it; until then the usual dialog checks stand.
		e.menuMu.Lock()
		delete(e.awaiting, s.id)
		e.menuMu.Unlock()
		return false
	}
	e.menuMu.Lock()
	delete(e.awaiting, s.id)
	e.menuMu.Unlock()
	return false
}
