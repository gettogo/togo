package claude

import (
	"context"
	"path/filepath"
	"strings"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

// A "startup" SessionStart of a session other than the one followed is a
// nested run (`claude -p` below the pane's agent) unless the followed
// session ended. A Claude Code that was killed, or exited while the helper
// was down, sends no SessionEnd, so a Claude Code the user then starts by
// hand in the pane looks nested too. It is the pane's own when the pane's
// process tree runs exactly one Claude Code: the one that sent it.
//
// Listing processes takes time, and Map runs under the session's lock, so
// the startup is held and the check runs off the lock (as source.Solo
// does). Later payloads of the same session are held behind it, in order.
// When the check ends the engine is woken (harness.Waker): Woken maps them
// all if the session is the pane's own, and drops them all if not. Unknown
// (no process view, a failed listing) is not the pane's own.

// startCheckWait bounds the process listing.
const startCheckWait = 3 * time.Second

// maxHeld bounds the payloads held behind one check.
const maxHeld = 500

type heldStart struct {
	id    string
	recs  []harness.Record
	done  bool
	own   bool
	pload []map[string]any
}

// agentCounter counts the session's own processes of a program
// (procs.Session.Within).
type agentCounter interface {
	Within(ctx context.Context, commands []string) (int, error)
}

// holdLocked holds p when it waits on a check, starting one for a
// startup of another session. It reports whether p was held.
func (h *hooks) holdLocked(p map[string]any, rec harness.Record) bool {
	id := str(p, "session_id")
	if hs := h.held; hs != nil && id == hs.id {
		if len(hs.recs) < maxHeld {
			hs.recs, hs.pload = append(hs.recs, rec), append(hs.pload, p)
		}
		return true
	}
	if h.held != nil || id == "" || id == h.accepted || h.ended || str(p, "hook_event_name") != "SessionStart" || str(p, "source") != "startup" {
		return false
	}
	cur := h.state["harness_session"]
	if cur == "" && h.state["transcript_path"] != "" {
		cur = strings.TrimSuffix(filepath.Base(h.state["transcript_path"]), ".jsonl")
	}
	c, ok := h.env.Procs.(agentCounter)
	if cur == "" || id == cur || !ok {
		return false
	}
	hs := &heldStart{id: id, recs: []harness.Record{rec}, pload: []map[string]any{p}}
	h.held = hs
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), startCheckWait)
		n, err := c.Within(ctx, []string{"claude"})
		cancel()
		h.mu.Lock()
		hs.done, hs.own = true, err == nil && n == 1
		wake := h.wake
		h.mu.Unlock()
		if wake != nil {
			wake()
		}
	}()
	return true
}

// Wake keeps the engine's wake for the check (harness.Waker).
func (h *hooks) Wake(f func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.wake = f
}

// Woken maps the held payloads in order when the check found the session
// the pane's own, and drops them when it did not.
func (h *hooks) Woken() ([]harness.Item, []harness.Op) {
	h.mu.Lock()
	defer h.mu.Unlock()
	hs := h.held
	if hs == nil || !hs.done {
		return nil, nil
	}
	h.held = nil
	if !hs.own {
		return nil, nil
	}
	h.accepted = hs.id
	var items []harness.Item
	var ops []harness.Op
	for i, p := range hs.pload {
		it, op := h.mapLocked(p, hs.recs[i])
		items, ops = append(items, it...), append(ops, op...)
	}
	return items, ops
}
