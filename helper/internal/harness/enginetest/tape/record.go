// Package tape exports what the real engine sends the app as a tape: the
// JSON-RPC requests the app makes, the results the engine gives, and the
// notifications it delivers, in order, one JSON object per line. The app's
// strict replay plays a tape back. A manifest next to each tape names the
// scenario, agent, session and the helper revision that made it.
//
// A Recorder wraps an enginetest rig and writes the tape as a scenario
// drives the phone side. Clean scrubs and normalises it, and Write puts
// the tape and its manifest on disk. Scenarios are the catalogue the
// togo-tape command runs.
package tape

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/gettogo/togo/helper/internal/api"
	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/enginetest"
	"github.com/gettogo/togo/helper/internal/rpc"
	"github.com/gettogo/togo/helper/internal/session"
)

// sub is the app's connection: it keeps what the engine notifies.
type sub struct {
	mu    sync.Mutex
	notes []harness.Notification
	done  chan struct{}
}

func (s *sub) Notify(n harness.Notification) error {
	s.mu.Lock()
	s.notes = append(s.notes, n)
	s.mu.Unlock()
	return nil
}

func (s *sub) Done() <-chan struct{} { return s.done }

func (s *sub) take(from int) []harness.Notification {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]harness.Notification(nil), s.notes[from:]...)
}

// Recorder records the phone side of one rig. Every request and its
// result, and every notification, is one line of the tape. It is for one
// goroutine at a time, as the app's serial requests are.
type Recorder struct {
	t   testing.TB
	r   *enginetest.Rig
	sub *sub
	// seen is how many of sub's notifications are on the tape.
	seen   int
	next   int
	lines  [][]byte
	cursor int64
	epoch  string
	// Notes counts the notifications on the tape, by kind.
	Notes map[string]int
	// Adapter is the adapter id harness.describe named.
	Adapter string
}

// NewRecorder starts a tape for the rig's session. The app has not opened
// the session yet.
func NewRecorder(t testing.TB, r *enginetest.Rig) *Recorder {
	return &Recorder{t: t, r: r, sub: &sub{done: make(chan struct{})}, next: 1, Notes: map[string]int{}}
}

// Lines is the tape so far, one JSON object per element.
func (c *Recorder) Lines() [][]byte { return c.lines }

// Cursor is the newest Seq the app has taken, from its last open and the
// notifications it has been sent since.
func (c *Recorder) Cursor() int64 { return c.cursor }

// Flush puts the notifications the engine has delivered since the last
// call on the tape, in the order they arrived.
func (c *Recorder) Flush() {
	c.t.Helper()
	for _, n := range c.sub.take(c.seen) {
		c.seen++
		c.Notes[n.Kind]++
		if n.Kind == harness.NoteItems && n.Epoch == c.epoch {
			for _, it := range n.Items {
				c.cursor = max(c.cursor, it.Seq)
			}
		}
		c.add(map[string]any{"jsonrpc": "2.0", "method": harness.NotificationMethod, "params": n})
	}
}

func (c *Recorder) add(frame map[string]any) {
	c.t.Helper()
	b, err := json.Marshal(frame)
	if err != nil {
		c.t.Fatalf("tape: %v", err)
	}
	c.lines = append(c.lines, b)
}

// Call makes one request as the app does, through the daemon's method
// table, and records the request, then its result or error with the same
// id. Notifications that arrived before the call come first; those that
// arrive during it follow the response.
func (c *Recorder) Call(method string, params any) (json.RawMessage, error) {
	c.t.Helper()
	c.Flush()
	id := c.next
	c.next++
	c.add(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	raw, err := c.r.Call(method, params)
	c.respond(id, raw, err)
	c.Flush()
	return raw, err
}

func (c *Recorder) respond(id int, raw json.RawMessage, err error) {
	c.t.Helper()
	if err != nil {
		c.add(map[string]any{"jsonrpc": "2.0", "id": id, "error": rpcError(err)})
		return
	}
	c.add(map[string]any{"jsonrpc": "2.0", "id": id, "result": raw})
}

// rpcError is the error object the daemon's connection sends for err
// (api.API.mapError).
func rpcError(err error) *rpc.Error {
	var ce *api.CodeError
	var invalid *session.InvalidParamsError
	switch {
	case errors.As(err, &ce):
		return rpc.NewError(ce.Code, ce.Data)
	case errors.Is(err, session.ErrNotFound):
		return rpc.NewError(rpc.CodeNotFound, nil)
	case errors.Is(err, session.ErrNotManaged):
		return rpc.NewError(rpc.CodeNotManaged, nil)
	case errors.As(err, &invalid):
		return rpc.InvalidParams(cmp.Or(invalid.Reason, invalid.Msg))
	}
	return rpc.NewError(rpc.CodeInternal, nil)
}

// Describe is harness.describe.
func (c *Recorder) Describe() harness.Descriptor {
	c.t.Helper()
	raw, err := c.Call(harness.MethodDescribe, map[string]any{"id": c.r.ID})
	if err != nil {
		c.t.Fatalf("describe: %v", err)
	}
	var d harness.Descriptor
	if err := json.Unmarshal(raw, &d); err != nil {
		c.t.Fatal(err)
	}
	if d.Adapter != nil {
		c.Adapter = d.Adapter.ID
	}
	return d
}

// Open is harness.open with the params the app sends: the session and the
// cursor, no limit. The connection is the recorder's, so the engine's
// notifications for the session reach the tape from here on; they are held
// back until the result is written, as the daemon holds them.
func (c *Recorder) Open(after int64) harness.OpenResult {
	c.t.Helper()
	c.Flush()
	id := c.next
	c.next++
	params := map[string]any{"id": c.r.ID, "after": after}
	c.add(map[string]any{"jsonrpc": "2.0", "id": id, "method": harness.MethodOpen, "params": params})
	res, err := c.r.Engine.Open(context.Background(), c.sub, c.r.ID, after, harness.DefaultLimit)
	if err != nil {
		c.respond(id, nil, err)
		c.t.Fatalf("open: %v", err)
	}
	raw, err := json.Marshal(res)
	if err != nil {
		c.t.Fatal(err)
	}
	c.respond(id, raw, nil)
	c.epoch, c.cursor = res.Epoch, res.Cursor
	c.r.Engine.Release(c.sub, c.r.ID)
	return res
}

// Send is harness.send; mode is harness.SendNow or harness.SendQueue.
func (c *Recorder) Send(text, mode string) (harness.SendResult, error) {
	c.t.Helper()
	var res harness.SendResult
	raw, err := c.Call(harness.MethodSend, map[string]any{"id": c.r.ID, "mode": mode, "text": text})
	if err != nil {
		return res, err
	}
	return res, json.Unmarshal(raw, &res)
}

// Interrupt is harness.interrupt.
func (c *Recorder) Interrupt() (harness.ActionResult, error) {
	c.t.Helper()
	var res harness.ActionResult
	raw, err := c.Call(harness.MethodInterrupt, map[string]any{"id": c.r.ID})
	if err != nil {
		return res, err
	}
	return res, json.Unmarshal(raw, &res)
}

// Restart is the helper restarting while the app is connected: the stream
// ends (the test-only replay.eof notification), the rig stops its engine
// and starts another over the same files, and the app's old connection is
// gone. The caller waits for the new engine to read the session, then
// calls Reopen.
func (c *Recorder) Restart() {
	c.t.Helper()
	c.Flush()
	c.add(map[string]any{"jsonrpc": "2.0", "method": "replay.eof", "params": map[string]any{"id": c.r.ID}})
	c.r.Restart()
	// What the old engine still sent is not on the tape: the app's
	// connection ended at the eof.
	close(c.sub.done)
	c.sub = &sub{done: make(chan struct{})}
	c.seen = 0
}

// Reopen is what the app does at the end of a stream: it opens with the
// cursor it had, sees the replacement epoch in the result, and opens
// again from the start. It returns both results.
func (c *Recorder) Reopen() (resumed, fresh harness.OpenResult) {
	c.t.Helper()
	resumed = c.Open(c.cursor)
	fresh = c.Open(0)
	return resumed, fresh
}
