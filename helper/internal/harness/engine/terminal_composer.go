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
	// suffix is what stood after the cursor: nothing, or the placeholder
	// an empty line shows. A draft after the cursor reads differently.
	suffix string
	x      int
	ok     bool
	// visible: the harness showed its cursor there. A harness that never
	// shows one (it draws its own) is believed only if tracks: the cursor
	// was seen to move with text typed into the line, so its position is
	// live and not left over.
	visible, tracks bool
	// The running session and conversation the mark was taken in.
	sess *sess
	conv uint64
}

// markComposer records the empty input line the cursor is in.
func (e *Engine) markComposer(ctx context.Context, s *sess) composerMark {
	if e.cfg.Cursor == nil || e.cfg.Screen == nil {
		return composerMark{}
	}
	x, y, visible, err := e.cfg.Cursor(ctx, s.id)
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
	return composerMark{prefix: prefix, suffix: afterCursor(lines[y], x), x: x, ok: true, visible: visible, sess: s, conv: menuConv(ctx, s)}
}

// cursorAt reports the cursor's column and the text of its line.
func (e *Engine) cursorAt(ctx context.Context, s *sess) (x int, line string, visible, ok bool) {
	if e.cfg.Cursor == nil || e.cfg.Screen == nil {
		return 0, "", false, false
	}
	x, y, visible, err := e.cfg.Cursor(ctx, s.id)
	if err != nil {
		return 0, "", false, false
	}
	screen, err := e.cfg.Screen(ctx, s.id)
	if err != nil {
		return 0, "", false, false
	}
	lines := strings.Split(screen, "\n")
	if y < 0 || y >= len(lines) {
		return 0, "", false, false
	}
	return x, lines[y], visible, true
}

// composerBack reports that the cursor is in the input line again, with
// nothing typed in it, and that no list is on the screen.
func (e *Engine) composerBack(ctx context.Context, s *sess, mark composerMark) bool {
	if !mark.ok || e.cfg.Cursor == nil || e.cfg.Screen == nil {
		return false
	}
	if mark.sess != nil && (mark.sess != s || !e.stillServed(s, mark.conv)) {
		return false // taken in another run or conversation: proves nothing here
	}
	x, y, visible, err := e.cfg.Cursor(ctx, s.id)
	if err != nil || x != mark.x {
		return false
	}
	// A shown cursor is where typing goes. A hidden one counts only for a
	// harness whose hidden cursor was seen to follow typed text.
	if !visible && !(mark.tracks && !mark.visible) {
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
	if string(line[:x]) != mark.prefix || afterCursor(lines[y], x) != mark.suffix {
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
	if mark.sess == nil {
		mark.sess = s
		s.mu.Lock()
		mark.conv = s.convGen
		s.mu.Unlock()
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
		// How the input line looks was never known, so it cannot be seen
		// to be back: the session stays guarded. A command run for the
		// phone proves the input line again and frees it; a restart or a
		// new conversation does too.
		if mark.sess != nil && (mark.sess != s || !e.stillServed(s, mark.conv)) {
			e.menuMu.Lock()
			delete(e.awaiting, s.id)
			e.menuMu.Unlock()
			return false
		}
		return true
	}
	e.menuMu.Lock()
	delete(e.awaiting, s.id)
	e.menuMu.Unlock()
	return false
}

// afterCursor is what a line holds from the cursor's column on, without
// trailing spaces or a box's closing edge.
func afterCursor(line string, x int) string {
	rs := []rune(trimEdge(line))
	if x < 0 || x >= len(rs) {
		return ""
	}
	return strings.TrimRight(string(rs[x:]), " ")
}
