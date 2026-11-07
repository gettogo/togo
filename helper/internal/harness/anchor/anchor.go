// Package anchor records where in a pane's output a conversation item
// happened and finds that place again later, for "Show in terminal".
//
// A mark combines the tap's byte counter (the epoch names the tap run) with
// the pane's absolute grid line (history size plus cursor row) and a short
// needle of text from that line. Finding a mark recomputes the distance
// from the cursor and confirms it by the needle; when the line has scrolled
// out of history or the epoch changed, the needle is searched for instead.
package anchor

import (
	"context"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/tap"
)

// positionTTL is how long a pane position is reused between marks.
const positionTTL = 250 * time.Millisecond

// Position is where the pane's cursor is.
type Position struct {
	History int64 // lines scrolled into history
	CursorY int64 // cursor row in the visible screen
	Limit   int64 // history-limit; History == Limit means lines are being dropped
}

// Line is the cursor's absolute grid line.
func (p Position) Line() int64 { return p.History + p.CursorY }

// Pane reads a session's pane. Lines are addressed relative to the top of
// the visible screen, so history lines are negative.
type Pane interface {
	Position(ctx context.Context, session string) (Position, error)
	// Lines returns the text of lines from..to inclusive, relative to the
	// top of the visible screen, one string per line.
	Lines(ctx context.Context, session string, from, to int64) ([]string, error)
	// History returns every line from the top of history to the bottom of
	// the screen.
	History(ctx context.Context, session string) ([]string, error)
}

// Marker implements harness.Marker.
type Marker struct {
	// Home is TOGO_HOME, where the tap keeps its counter.
	Home string
	Pane Pane
	Now  func() time.Time

	mu    sync.Mutex
	cache map[string]cached
}

type cached struct {
	at  time.Time
	pos Position
}

var _ harness.Marker = (*Marker)(nil)

func (m *Marker) now() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}

// Mark records the pane's current output position.
func (m *Marker) Mark(ctx context.Context, session string) (harness.Anchor, error) {
	epoch, bytes, err := tap.ReadOffset(m.Home, session)
	if err != nil {
		// No tap counter yet: anchors still work by line and needle
		// within this helper run.
		epoch, bytes = "", 0
	}
	pos, err := m.position(ctx, session)
	if err != nil {
		return harness.Anchor{}, err
	}
	a := harness.Anchor{Epoch: epoch, Bytes: bytes, Line: pos.Line()}
	if lines, err := m.Pane.Lines(ctx, session, pos.CursorY-2, pos.CursorY); err == nil {
		a.Needle = needle(lines)
	}
	return a, nil
}

func (m *Marker) position(ctx context.Context, session string) (Position, error) {
	m.mu.Lock()
	if c, ok := m.cache[session]; ok && m.now().Sub(c.at) < positionTTL {
		m.mu.Unlock()
		return c.pos, nil
	}
	m.mu.Unlock()
	pos, err := m.Pane.Position(ctx, session)
	if err != nil {
		return Position{}, err
	}
	m.mu.Lock()
	if m.cache == nil {
		m.cache = map[string]cached{}
	}
	m.cache[session] = cached{at: m.now(), pos: pos}
	m.mu.Unlock()
	return pos, nil
}

// Find resolves an anchor against the pane as it is now.
func (m *Marker) Find(ctx context.Context, session string, a harness.Anchor) (harness.Location, error) {
	pos, err := m.Pane.Position(ctx, session)
	if err != nil {
		return harness.Location{}, err
	}
	epoch, _, _ := tap.ReadOffset(m.Home, session)
	sameEpoch := a.Epoch == epoch
	dropping := pos.Limit > 0 && pos.History >= pos.Limit
	if sameEpoch && !dropping && a.Line <= pos.Line() {
		rel := a.Line - pos.History
		ok := a.Needle == ""
		if !ok {
			if lines, err := m.Pane.Lines(ctx, session, rel-2, rel); err == nil {
				ok = containsNeedle(lines, a.Needle)
			}
		}
		if ok {
			return harness.Location{Found: true, LinesFromBottom: pos.Line() - a.Line, Needle: a.Needle}, nil
		}
	}
	if a.Needle == "" {
		return harness.Location{Found: false}, nil
	}
	hist, err := m.Pane.History(ctx, session)
	if err != nil {
		return harness.Location{}, err
	}
	at := LastMatch(hist, a.Needle)
	if at < 0 {
		return harness.Location{Found: false, Needle: a.Needle}, nil
	}
	// History starts at the top of history, so a line's index is its
	// absolute line now.
	return harness.Location{Found: true, LinesFromBottom: max(pos.Line()-int64(at), 0), Needle: a.Needle}, nil
}

// LastMatch is the index of the last line containing the needle, or -1.
func LastMatch(lines []string, needle string) int {
	n := strings.TrimSpace(needle)
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.Contains(lines[i], n) {
			return i
		}
	}
	return -1
}

func containsNeedle(lines []string, n string) bool {
	return LastMatch(lines, n) >= 0
}

// needle picks the last non-blank line of a few, trimmed to MaxNeedle runes.
func needle(lines []string) string {
	for i := len(lines) - 1; i >= 0; i-- {
		s := strings.TrimSpace(lines[i])
		if s == "" {
			continue
		}
		if utf8.RuneCountInString(s) > harness.MaxNeedle {
			r := []rune(s)
			s = strings.TrimSpace(string(r[:harness.MaxNeedle]))
		}
		return s
	}
	return ""
}
