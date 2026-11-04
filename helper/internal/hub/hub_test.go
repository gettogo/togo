package hub_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/api"
	"github.com/gettogo/togo/helper/internal/hub"
	"github.com/gettogo/togo/helper/internal/rpc"
	"github.com/gettogo/togo/helper/internal/session"
	"github.com/gettogo/togo/helper/internal/state"
	"github.com/gettogo/togo/helper/internal/tmux"
	"github.com/gettogo/togo/helper/internal/tmux/tmuxtest"
	"github.com/gettogo/togo/helper/internal/wire"
)

var fixedNow = time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)

type env struct {
	svc  *session.Service
	hub  *hub.Hub
	api  *api.API
	home string
	// elapsed moves the hub's clock forward from fixedNow.
	elapsed *atomic.Int64
}

// newEnv returns a hub over a private tmux label and a temp HOME holding
// proj/, wired into an API the way the daemon serves it.
func newEnv(t *testing.T) *env {
	t.Helper()
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(home, "proj"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("SHELL", "/bin/sh")
	togoHome := t.TempDir()
	recent := &state.Recent{File: filepath.Join(togoHome, "recent.json")}
	svc := &session.Service{
		Tmux: tmuxtest.New(t), Recent: recent, Shell: "/bin/sh", Path: t.TempDir(),
		LockFile: filepath.Join(togoHome, "run", "bib.lock"),
		NewID:    func() string { return "s_abcdefghij" },
		Now:      func() time.Time { return fixedNow },
	}
	elapsed := new(atomic.Int64)
	h := hub.New(svc, func() time.Time { return fixedNow.Add(time.Duration(elapsed.Load())) })
	a := &api.API{Sessions: svc, Recent: recent, Home: home, Ext: h.Methods()}
	return &env{svc: svc, hub: h, api: a, home: home, elapsed: elapsed}
}

func (e *env) createShell(t *testing.T) session.Session {
	t.Helper()
	sess, err := e.svc.Create(context.Background(), session.CreateOpts{Agent: "shell", Cwd: "~/proj"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return sess
}

// client is one connection served by rpc.Serve over in-memory pipes. Frames
// arrive on its frames channel in wire order. Request ids count from 1,
// as in the fixtures.
type client struct {
	t      *testing.T
	in     *io.PipeWriter
	frames chan map[string]json.RawMessage
	nextID int
}

func (e *env) dial(t *testing.T) *client {
	t.Helper()
	return e.dialBuffered(t, 4096)
}

// dialBuffered sets how many frames may wait unread; with 0 a client that
// stops reading backs up into the server's writes.
func (e *env) dialBuffered(t *testing.T, buffer int) *client {
	t.Helper()
	reqR, reqW := io.Pipe()
	respR, respW := io.Pipe()
	go func() {
		_ = rpc.Serve(context.Background(), reqR, respW, e.hub.Renewing(e.api), rpc.HelperInfo{Version: "0.0.0-test"})
		_ = respW.Close()
	}()
	c := &client{t: t, in: reqW, frames: make(chan map[string]json.RawMessage, buffer), nextID: 0}
	go func() {
		defer close(c.frames)
		for {
			raw, err := rpc.ReadFrame(respR)
			if err != nil {
				return
			}
			var m map[string]json.RawMessage
			if err := json.Unmarshal(raw, &m); err != nil {
				return
			}
			c.frames <- m
		}
	}()
	t.Cleanup(c.close)
	c.call("hello", map[string]any{"protocol": 1, "client": map[string]string{"name": "t", "version": "0"}, "capabilities": []string{}})
	return c
}

func (c *client) close() { _ = c.in.Close() }

// send writes a request and returns its id without waiting.
func (c *client) send(method string, params any) int {
	c.t.Helper()
	c.nextID++
	raw, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": c.nextID, "method": method, "params": params})
	if err != nil {
		c.t.Fatal(err)
	}
	if err := rpc.WriteFrame(c.in, raw); err != nil {
		c.t.Fatalf("write %s: %v", method, err)
	}
	return c.nextID
}

// next returns the next frame or fails after a timeout.
func (c *client) next() map[string]json.RawMessage {
	c.t.Helper()
	select {
	case m, ok := <-c.frames:
		if !ok {
			c.t.Fatal("connection closed")
		}
		return m
	case <-time.After(5 * time.Second):
		c.t.Fatal("timed out waiting for a frame")
	}
	return nil
}

// call sends a request and returns its response, failing if a
// notification arrives first.
func (c *client) call(method string, params any) map[string]json.RawMessage {
	c.t.Helper()
	id := c.send(method, params)
	m := c.next()
	if string(m["id"]) != fmt.Sprint(id) {
		c.t.Fatalf("%s: got frame %v before its response", method, m)
	}
	return m
}

func (c *client) subscribe(foreground bool) int64 {
	c.t.Helper()
	resp := c.call(wire.MethodEventsSubscribe, map[string]any{"foreground": foreground})
	var r struct{ Seq int64 }
	if err := json.Unmarshal(resp["result"], &r); err != nil {
		c.t.Fatalf("subscribe: %v in %v", err, resp)
	}
	return r.Seq
}

func (c *client) event() wire.Event {
	c.t.Helper()
	m := c.next()
	if string(m["method"]) != `"event"` {
		c.t.Fatalf("got %v, want an event", m)
	}
	var ev wire.Event
	if err := json.Unmarshal(m["params"], &ev); err != nil {
		c.t.Fatal(err)
	}
	return ev
}

func notify(h *hub.Hub, n int) {
	for i := range n {
		h.Publish(wire.EventNotify, nil, wire.NotifyEvent{Title: fmt.Sprint(i), Body: "b", Level: wire.LevelInfo})
	}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestSubscribeSnapshotThenOrderedEvents(t *testing.T) {
	e := newEnv(t)
	notify(e.hub, 3)
	c := e.dial(t)

	// Publishing races the subscribe call, so the response must still be
	// the first frame and every later event must follow its seq.
	const racing = 100
	done := make(chan struct{})
	go func() {
		defer close(done)
		notify(e.hub, racing)
	}()
	snap := c.subscribe(true)
	<-done
	if snap < 3 {
		t.Fatalf("snapshot seq %d, want at least the 3 earlier events", snap)
	}
	last := snap
	for last < 3+racing {
		ev := c.event()
		if ev.Seq != last+1 {
			t.Fatalf("event seq %d after %d, want %d", ev.Seq, last, last+1)
		}
		last = ev.Seq
	}
}

func TestResubscribeGivesFreshSnapshotWithoutDuplicates(t *testing.T) {
	e := newEnv(t)
	c := e.dial(t)
	c.subscribe(true)
	notify(e.hub, 2)
	if got := c.event().Seq; got != 1 {
		t.Fatalf("first event seq %d, want 1", got)
	}
	// Event 2 may be written before or after the second response; either
	// way nothing after the new snapshot appears twice.
	id := c.send(wire.MethodEventsSubscribe, map[string]any{})
	var snap int64 = -1
	for snap < 0 {
		m := c.next()
		if string(m["id"]) == fmt.Sprint(id) {
			var r struct{ Seq int64 }
			_ = json.Unmarshal(m["result"], &r)
			snap = r.Seq
		}
	}
	if snap != 2 {
		t.Fatalf("resubscribe seq %d, want 2", snap)
	}
	notify(e.hub, 1)
	if got := c.event().Seq; got != 3 {
		t.Fatalf("event after resubscribe seq %d, want 3", got)
	}
	select {
	case m := <-c.frames:
		t.Fatalf("unexpected extra frame %v", m)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestForegroundCountTracksResubscribe(t *testing.T) {
	e := newEnv(t)
	a := e.dial(t)
	a.subscribe(true)
	if n := e.hub.ForegroundSubscribers(); n != 1 {
		t.Fatalf("after foreground subscribe: %d, want 1", n)
	}
	a.subscribe(false)
	if n := e.hub.ForegroundSubscribers(); n != 0 {
		t.Fatalf("after background resubscribe: %d, want 0", n)
	}
	a.call(wire.MethodEventsSubscribe, map[string]any{})
	if n := e.hub.ForegroundSubscribers(); n != 1 {
		t.Fatalf("foreground defaults to true: %d, want 1", n)
	}
	b := e.dial(t)
	b.subscribe(true)
	if n := e.hub.ForegroundSubscribers(); n != 2 {
		t.Fatalf("two foreground clients: %d, want 2", n)
	}
	a.close()
	waitFor(t, "closed client to unsubscribe", func() bool { return e.hub.ForegroundSubscribers() == 1 })
}

func TestSlowSubscriberLosesBacklogOthersUnaffected(t *testing.T) {
	e := newEnv(t)
	// Nothing reads the slow client's frames until the end, so its writer
	// blocks on the first event and its queue overflows.
	slow := e.dialBuffered(t, 0)
	slowSnap := slow.subscribe(true)
	fast := e.dial(t)
	fast.subscribe(true)

	// Batches stay under the queue bound, so only the slow client lags.
	const batches, batch = 10, 100
	want := int64(1)
	for range batches {
		published := make(chan struct{})
		go func() {
			defer close(published)
			notify(e.hub, batch)
		}()
		select {
		case <-published:
		case <-time.After(5 * time.Second):
			t.Fatal("Publish blocked on the slow subscriber")
		}
		for range batch {
			if got := fast.event().Seq; got != want {
				t.Fatalf("fast client got seq %d, want %d", got, want)
			}
			want++
		}
	}

	var seqs []int64
	for prev := slowSnap; ; {
		ev := slow.event()
		seqs = append(seqs, ev.Seq)
		if ev.Seq <= prev {
			t.Fatalf("slow client seq went from %d to %d", prev, ev.Seq)
		}
		if ev.Seq != prev+1 {
			break
		}
		prev = ev.Seq
	}
	if len(seqs) > 300 {
		t.Fatalf("slow client read %d contiguous events, want a gap within the queue bound", len(seqs))
	}
}

func TestLatestSessionEventCarriesLatestState(t *testing.T) {
	e := newEnv(t)
	sess := e.createShell(t)
	c := e.dial(t)
	c.subscribe(true)

	const writers = 20
	var wg sync.WaitGroup
	for i := range writers {
		wg.Go(func() {
			activity := fmt.Sprint("step ", i)
			e.hub.SetStatus(sess.ID, "working", &activity)
		})
		wg.Go(func() {
			_, _ = e.api.Handle(context.Background(), wire.MethodSessionSetMute,
				json.RawMessage(fmt.Sprintf(`{"id":%q,"muted":%t}`, sess.ID, i%2 == 0)))
		})
	}
	wg.Wait()

	var last session.Session
	for range 2 * writers {
		var p struct {
			Session session.Session `json:"session"`
		}
		if err := json.Unmarshal(c.event().Payload.(json.RawMessage), &p); err != nil {
			t.Fatal(err)
		}
		last = p.Session
	}
	now := e.hub.Decorate([]session.Session{sess})[0]
	if last.Activity == nil || *last.Activity != *now.Activity || last.Muted != now.Muted {
		t.Fatalf("last event activity %v muted %v, hub has %v muted %v", *last.Activity, last.Muted, *now.Activity, now.Muted)
	}
	if fresh := hub.New(e.svc, nil).Muted(sess.ID); fresh != now.Muted {
		t.Fatalf("tmux has muted %v, hub cache has %v", fresh, now.Muted)
	}
}

func TestSlowListDoesNotBlockPublish(t *testing.T) {
	dir := t.TempDir()
	release := filepath.Join(dir, "release")
	bin := filepath.Join(dir, "tmux")
	// list-sessions waits until the test lets it finish.
	script := "#!/bin/sh\ncase \"$*\" in *-V*) echo 'tmux 3.4'; exit 0;; esac\n" +
		"while [ ! -e " + release + " ]; do sleep 0.02; done\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	e := newEnv(t)
	e.svc.Tmux = &tmux.Client{Bin: bin}
	e.svc.User = nil
	c := e.dial(t)
	id := c.send(wire.MethodEventsSubscribe, map[string]any{})

	waitFor(t, "subscriber to register", func() bool { return e.hub.ForegroundSubscribers() == 1 })
	published := make(chan struct{})
	go func() {
		defer close(published)
		notify(e.hub, 1)
	}()
	select {
	case <-published:
	case <-time.After(2 * time.Second):
		t.Fatal("Publish waited on the subscribe's tmux list")
	}
	if err := os.WriteFile(release, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	m := c.next()
	if string(m["id"]) != fmt.Sprint(id) {
		t.Fatalf("got %v before the subscribe response", m)
	}
	var r struct{ Seq int64 }
	if err := json.Unmarshal(m["result"], &r); err != nil || m["error"] != nil {
		t.Fatalf("subscribe failed: %v", m)
	}
	if r.Seq != 0 {
		t.Fatalf("snapshot seq %d, want 0: the event came after registration", r.Seq)
	}
	if got := c.event().Seq; got != 1 {
		t.Fatalf("event seq %d, want 1", got)
	}
}
