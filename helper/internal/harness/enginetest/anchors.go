package enginetest

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/anchor"
	"github.com/gettogo/togo/helper/internal/ports"
)

// The pane's scroll-back, for the engine's anchors (Options.Anchors). The
// pane is the screen the test shows (a real capture) with the lines that
// scrolled off its top above it; the cursor stays on the input line, as
// it does while an agent prints above it.

// Output prints lines above the input line: they enter at the bottom of
// the visible rows and the same number of rows at the top scroll into
// the history (lines past the screen's height scroll off at once).
func (t *Terminal) Output(lines ...string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	all := append(append([]string(nil), t.base.Rows...), lines...)
	t.hist = append(t.hist, all[:len(lines)]...)
	t.base.Rows = all[len(lines):]
}

// termPane is the pane the anchor package reads: history lines first,
// then the visible rows.
type termPane struct{ t *Terminal }

func (p termPane) all() ([]string, int) {
	p.t.mu.Lock()
	defer p.t.mu.Unlock()
	rows := append(append([]string(nil), p.t.hist...), p.t.base.Rows...)
	for i := range rows {
		rows[i] = Plain(rows[i])
	}
	return rows, len(p.t.hist)
}

func (p termPane) Position(context.Context, string) (anchor.Position, error) {
	p.t.mu.Lock()
	defer p.t.mu.Unlock()
	if p.t.base.Input < 0 {
		return anchor.Position{}, errors.New("enginetest: the screen has no input line")
	}
	return anchor.Position{History: int64(len(p.t.hist)), CursorY: int64(p.t.base.Input)}, nil
}

func (p termPane) Lines(_ context.Context, _ string, from, to int64) ([]string, error) {
	rows, hist := p.all()
	var out []string
	for i := from; i <= to; i++ {
		if at := int64(hist) + i; at >= 0 && at < int64(len(rows)) {
			out = append(out, rows[at])
		}
	}
	return out, nil
}

func (p termPane) History(context.Context, string) ([]string, error) {
	rows, _ := p.all()
	return rows, nil
}

// SetListeners scripts the TCP ports the session's processes listen on,
// as the engine's next refresh of the Ports section reads them
// (Options.Ports).
func (r *Rig) SetListeners(l ...ports.Listener) {
	r.mu.Lock()
	r.listeners = append([]ports.Listener(nil), l...)
	r.mu.Unlock()
}

func (r *Rig) scriptedPorts(context.Context, string) ([]ports.Listener, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]ports.Listener(nil), r.listeners...), nil
}

// Locate is the phone's harness.locate: where an anchor is in the pane now.
func (r *Rig) Locate(a harness.Anchor) (harness.Location, error) {
	var res harness.Location
	raw, err := r.Call("harness.locate", map[string]any{"id": r.ID, "anchor": a})
	if err != nil {
		return res, err
	}
	return res, jsonInto(raw, &res)
}

// PlaceCursor puts the pane's cursor on the last row of s whose text
// (without colours) matches re: the input line of an agent whose adapter
// declares no input pattern. A capture does not record where the cursor
// was; the rows that hold the agent's prompt marker are where it sits.
// offset moves it from that row (Pi's input is the blank row two above
// its working directory line). It panics when no row matches.
func (t *Terminal) PlaceCursor(s Screen, re string, offset int) Screen {
	rx := regexp.MustCompile(re)
	for i := len(s.Rows) - 1; i >= 0; i-- {
		if rx.MatchString(strings.TrimRight(Plain(s.Rows[i]), " ")) {
			s.Input, s.Prefix, s.Text = i+offset, "", ""
			return s
		}
	}
	panic("enginetest: no row of " + s.Name + " matches " + re)
}
