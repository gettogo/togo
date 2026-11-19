package enginetest

import (
	"context"
	"crypto/rand"
	"fmt"
	"sync"
	"unicode/utf8"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/wire"
)

// The lifecycle events a session goes through, as rig operations. Each
// does what the daemon does when the event happens; the comments name
// where.

// lifeState is the rig's record of relaunches.
type lifeState struct {
	relaunches [][]string
	removed    bool
	// RelaunchErr, when set, is what the next relaunch fails with.
	RelaunchErr error
}

// NewID returns a fresh managed session id ("s_" and ten base32
// characters), for rigs that run at the same time.
func NewID() string {
	b := make([]byte, 10)
	_, _ = rand.Read(b)
	const alphabet = "abcdefghijklmnopqrstuvwxyz234567"
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return "s_" + string(b)
}

// relaunch is the engine's Config.Relaunch: the daemon kills the pane's
// program and starts argv in its place (session.Service.RelaunchWith).
// Here the pane's program runs again and the input line is empty.
func (r *Rig) relaunch(_ context.Context, _ string, argv []string) error {
	r.mu.Lock()
	err := r.RelaunchErr
	r.RelaunchErr = nil
	if err == nil {
		r.relaunches = append(r.relaunches, append([]string(nil), argv...))
	}
	r.mu.Unlock()
	if err != nil {
		return err
	}
	r.Term.SetGone(false)
	r.Term.CloseDialog()
	return nil
}

// Relaunches lists the argv of every relaunch so far.
func (r *Rig) Relaunches() [][]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([][]string(nil), r.relaunches...)
}

// Exit is the agent's process ending in its pane: nothing reads input any
// more, the hub reports the session ended (watch/watch.go reports the
// exit, the hub's session event carries it), and its prompt left the
// screen with it.
func (r *Rig) Exit() {
	r.Term.SetGone(true)
	r.Term.CloseDialog()
	r.SetEnded(true)
	r.Look()
}

// Relaunch is the user starting the agent again in the same pane (the
// app's relaunch, or by hand): the hub's session event says it runs.
func (r *Rig) Relaunch() {
	r.Term.SetGone(false)
	r.SetEnded(false)
}

// Remove is the session going away (the user closed it, or its tmux
// session ended): the session service no longer finds it, the hub
// publishes session.removed, which the engine
// takes (engine.go Observe) and the watcher follows by telling the
// approvals store the session is gone (watch/watch.go SessionGone).
func (r *Rig) Remove() {
	id := r.ID
	r.mu.Lock()
	r.removed = true
	r.mu.Unlock()
	r.Approvals.SessionGone(context.Background(), id)
	r.Engine.Observe(wire.Event{Kind: wire.EventSessionRemoved, SessionID: &id})
}

// Interrupt is the phone's harness.interrupt.
func (r *Rig) Interrupt() (harness.ActionResult, error) {
	var res harness.ActionResult
	raw, err := r.Call("harness.interrupt", map[string]string{"id": r.ID})
	if err != nil {
		return res, err
	}
	return res, jsonInto(raw, &res)
}

// cursor is where the pane's cursor is: at the end of the input line when
// the screen has one and no dialog is up.
func (t *Terminal) cursor() (int, int, bool, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.dialog != nil || t.base.Input < 0 {
		return 0, 0, false, nil
	}
	return utf8.RuneCountInString(t.base.Prefix + t.input), t.base.Input, true, nil
}

// Client is the phone's chat: it opens the session, takes every
// notification, can lose its connection and come back with the cursor
// it had, as the app does.
type Client struct {
	r      *Rig
	sub    *Sub
	mu     sync.Mutex
	items  map[string]harness.Item
	cursor int64
	epoch  string
	resets int
	seen   int // notifications applied from sub
}

// Phone opens the session as the app does.
func (r *Rig) Phone() *Client {
	r.T.Helper()
	c := &Client{r: r, items: map[string]harness.Item{}}
	c.open(0)
	return c
}

func (c *Client) open(after int64) {
	c.r.T.Helper()
	c.sub = newSub()
	res, err := c.r.Engine.Open(context.Background(), c.sub, c.r.ID, after, 1<<20)
	if err != nil {
		c.r.T.Fatalf("open: %v", err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if res.Epoch != c.epoch {
		c.items = map[string]harness.Item{}
		c.epoch = res.Epoch
	}
	for _, it := range res.Items {
		c.items[it.ID] = it
	}
	c.cursor = res.Cursor
	c.seen = 0
}

// Sync applies the notifications that arrived since the last call.
func (c *Client) Sync() {
	notes := c.sub.Notes()
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, n := range notes[c.seen:] {
		switch n.Kind {
		case harness.NoteReset:
			c.resets++
			c.items = map[string]harness.Item{}
		case harness.NoteItems:
			if n.Epoch != c.epoch {
				continue
			}
			for _, it := range n.Items {
				if it.Seq <= c.cursor {
					continue
				}
				c.items[it.ID] = it
				c.cursor = max(c.cursor, it.Seq)
			}
		}
	}
	c.seen = len(notes)
}

// Drop is the connection going away: the subscriber is done, and what
// happens meanwhile is not sent to it.
func (c *Client) Drop() {
	c.Sync()
	close(c.sub.done)
	_ = c.r.Engine.Close(context.Background(), c.sub, c.r.ID)
}

// Return is the app reconnecting: it opens again after its old cursor.
func (c *Client) Return() {
	c.open(c.cursor)
}

// Items is what the phone shows, by id.
func (c *Client) Items() map[string]harness.Item {
	c.Sync()
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[string]harness.Item, len(c.items))
	for k, v := range c.items {
		out[k] = v
	}
	return out
}

// Resynced: after a reconnect the phone holds every item the helper holds,
// each in its newest form, and nothing the helper dropped.
func Resynced(c *Client, st State) []Violation {
	have := c.Items()
	var out []Violation
	for id, it := range latest(st.Items) {
		got, ok := have[id]
		switch {
		case !ok:
			out = append(out, Violation{"resync-lost-item", id + " is not on the phone after it reconnected"})
		case got.Seq != it.Seq:
			out = append(out, Violation{"resync-stale-item", fmt.Sprintf("%s is at seq %d on the phone, %d in the helper", id, got.Seq, it.Seq)})
		}
	}
	return out
}
