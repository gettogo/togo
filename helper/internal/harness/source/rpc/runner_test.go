package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/spec"
)

// fakeServer is a small app-server: WebSocket over a unix socket, one
// JSON-RPC message per text frame.
type fakeServer struct {
	path string
	ln   net.Listener
	srv  *http.Server

	mu      sync.Mutex
	conns   []*websocket.Conn
	calls   []string
	replies []Message
	loaded  []string
	handle  func(method string, params json.RawMessage) (any, *Error)
}

func socketDir(t *testing.T) string {
	dir, err := os.MkdirTemp("", "rpc")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func startFake(t *testing.T, path string) *fakeServer {
	t.Helper()
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeServer{path: path, ln: ln}
	f.srv = &http.Server{Handler: http.HandlerFunc(f.serve)}
	go f.srv.Serve(ln)
	t.Cleanup(f.stop)
	return f
}

func (f *fakeServer) stop() {
	f.mu.Lock()
	for _, c := range f.conns {
		c.CloseNow()
	}
	f.conns = nil
	f.mu.Unlock()
	f.srv.Close()
	os.Remove(f.path)
}

func (f *fakeServer) serve(w http.ResponseWriter, r *http.Request) {
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	f.mu.Lock()
	f.conns = append(f.conns, c)
	f.mu.Unlock()
	ctx := context.Background()
	for {
		_, b, err := c.Read(ctx)
		if err != nil {
			return
		}
		var m Message
		json.Unmarshal(b, &m)
		f.mu.Lock()
		if m.IsResponse() {
			f.replies = append(f.replies, m)
			f.mu.Unlock()
			continue
		}
		f.calls = append(f.calls, m.Method)
		h := f.handle
		f.mu.Unlock()
		if !m.IsRequest() {
			continue
		}
		var res any = map[string]any{}
		var rerr *Error
		switch m.Method {
		case "initialize":
			res = map[string]string{"userAgent": "fake"}
		case "thread/loaded/list":
			f.mu.Lock()
			res = map[string]any{"data": append([]string{}, f.loaded...)}
			f.mu.Unlock()
		default:
			if h != nil {
				res, rerr = h(m.Method, m.Params)
			}
		}
		out := Message{JSONRPC: "2.0", ID: m.ID, Error: rerr}
		if rerr == nil {
			out.Result, _ = json.Marshal(res)
		}
		b, _ = json.Marshal(out)
		c.Write(ctx, websocket.MessageText, b)
	}
}

// broadcast sends a message to every connected client.
func (f *fakeServer) broadcast(s string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.conns {
		c.Write(context.Background(), websocket.MessageText, []byte(s))
	}
}

func (f *fakeServer) count(method string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if c == method {
			n++
		}
	}
	return n
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func feedSpec(t *testing.T, b spec.RPC) harness.SourceSpec {
	raw, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	return harness.SourceSpec{Name: "appserver", Kind: spec.KindRPC, Raw: raw}
}

type recorder struct {
	mu        sync.Mutex
	recs      []Message
	restoring int
}

func (r *recorder) emit(rec harness.Record) {
	var m Message
	json.Unmarshal(rec.Data, &m)
	r.mu.Lock()
	if rec.Replay != nil {
		r.restoring++
	}
	r.recs = append(r.recs, m)
	r.mu.Unlock()
}

func (r *recorder) methods() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for _, m := range r.recs {
		out = append(out, m.Method)
	}
	return out
}

func (r *recorder) has(method string) bool {
	for _, m := range r.methods() {
		if m == method {
			return true
		}
	}
	return false
}

func resumeHandler(method string, params json.RawMessage) (any, *Error) {
	if method == MethodResume {
		var p struct{ ThreadID string }
		json.Unmarshal(params, &p)
		return map[string]any{"thread": map[string]any{"id": p.ThreadID, "turns": []any{}}, "model": "m1"}, nil
	}
	return nil, &Error{Code: CodeMethodNotFound, Message: "unknown"}
}

func TestWatchAttachesStreamsAndAnswers(t *testing.T) {
	sock := filepath.Join(socketDir(t), "s.sock")
	f := startFake(t, sock)
	f.handle = resumeHandler
	r := NewRunner()
	r.Poll = 20 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rec := &recorder{}
	env := harness.Env{SessionID: "s_x", Endpoint: "unix://" + sock}
	go r.Watch(ctx, feedSpec(t, spec.RPC{Method: MethodResume, Params: map[string]string{"threadId": ""},
		Notifications: []string{"turn/started", "serverRequest/resolved"}}), env, rec.emit)

	// No thread yet: the feed polls the loaded list, then joins.
	time.Sleep(60 * time.Millisecond)
	f.mu.Lock()
	f.loaded = []string{"thr-1"}
	f.mu.Unlock()
	waitFor(t, "resume record", func() bool { return rec.has(MethodResume) })
	if f.count(MethodResume) != 1 {
		t.Fatalf("resumed %d times", f.count(MethodResume))
	}
	rec.mu.Lock()
	restored := rec.restoring
	rec.mu.Unlock()
	if restored != 1 {
		t.Fatalf("resume was not delivered as a restoration: %d", restored)
	}

	f.broadcast(`{"method":"turn/started","params":{"threadId":"thr-1","turn":{"id":"u1"}}}`)
	f.broadcast(`{"method":"skills/changed","params":{}}`)
	f.broadcast(`{"id":0,"method":"item/tool/requestUserInput","params":{"threadId":"thr-1","questions":[]}}`)
	waitFor(t, "server request", func() bool { return rec.has("item/tool/requestUserInput") })
	if rec.has("skills/changed") {
		t.Fatal("unlisted notification passed")
	}
	if !rec.has("turn/started") {
		t.Fatal("listed notification missing")
	}

	// Answer through the registered shape.
	err := r.Answer(ctx, env, "rq-0", map[string]json.RawMessage{"answers": json.RawMessage(`{"q":["A"]}`)})
	if !errors.Is(err, ErrNoAnswerer) {
		t.Fatalf("answer without a shape: %v", err)
	}
	answers.Lock()
	answers.m["item/tool/requestUserInput"] = func(_ json.RawMessage, args map[string]json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`{"answers":` + string(args["answers"]) + `}`), nil
	}
	answers.Unlock()
	defer func() { answers.Lock(); delete(answers.m, "item/tool/requestUserInput"); answers.Unlock() }()
	if err := r.Answer(ctx, env, "rq-0", map[string]json.RawMessage{"answers": json.RawMessage(`{"q":["A"]}`)}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "reply", func() bool { f.mu.Lock(); defer f.mu.Unlock(); return len(f.replies) == 1 })
	f.mu.Lock()
	rep := f.replies[0]
	f.mu.Unlock()
	if string(rep.ID) != "0" || string(rep.Result) != `{"answers":{"q":["A"]}}` {
		t.Fatalf("reply %+v", rep)
	}
	if err := r.Answer(ctx, env, "rq-0", nil); !errors.Is(err, ErrNoRequest) {
		t.Fatalf("second answer: %v", err)
	}

	// A request resolved elsewhere (the terminal answered) is forgotten.
	f.broadcast(`{"id":1,"method":"item/tool/requestUserInput","params":{}}`)
	waitFor(t, "second request", func() bool { r.mu.Lock(); defer r.mu.Unlock(); return len(r.pending[env.Endpoint]) == 1 })
	f.broadcast(`{"method":"serverRequest/resolved","params":{"threadId":"thr-1","requestId":1}}`)
	waitFor(t, "resolved", func() bool { r.mu.Lock(); defer r.mu.Unlock(); return len(r.pending[env.Endpoint]) == 0 })
}

func TestWatchReconnectsAfterServerRestart(t *testing.T) {
	sock := filepath.Join(socketDir(t), "s.sock")
	f := startFake(t, sock)
	f.handle = resumeHandler
	f.loaded = []string{"thr-1"}
	r := NewRunner()
	r.Poll = 20 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rec := &recorder{}
	env := harness.Env{Endpoint: sock}
	go r.Watch(ctx, feedSpec(t, spec.RPC{Method: MethodResume, Params: map[string]string{"threadId": "thr-1"}}), env, rec.emit)
	waitFor(t, "first attach", func() bool { return f.count(MethodResume) == 1 })

	f.stop()
	f2 := startFake(t, sock)
	f2.handle = resumeHandler
	waitFor(t, "attach after restart", func() bool { return f2.count(MethodResume) == 1 })
	if f2.count("initialize") != 1 {
		t.Fatalf("initialize on reconnect: %d", f2.count("initialize"))
	}
	// The server counts the request before the runner emits its record:
	// wait for the record, not the count (it raced under -race).
	resumes := func() int {
		n := 0
		for _, m := range rec.methods() {
			if m == MethodResume {
				n++
			}
		}
		return n
	}
	waitFor(t, "second resume record", func() bool { return resumes() == 2 })
	time.Sleep(50 * time.Millisecond)
	if n := resumes(); n != 2 {
		t.Fatalf("resume records %d, want 2", n)
	}
}

func TestFetchPoolsAndRedials(t *testing.T) {
	sock := filepath.Join(socketDir(t), "s.sock")
	f := startFake(t, sock)
	f.handle = func(method string, _ json.RawMessage) (any, *Error) {
		return map[string]any{"data": []map[string]string{{"id": "m1", "apiKey": "sk-abcdefghijklmnopqrst"}}}, nil
	}
	r := NewRunner()
	env := harness.Env{Endpoint: "unix://" + sock}
	src := feedSpec(t, spec.RPC{Method: "model/list"})
	ctx := context.Background()
	for range 2 {
		out, err := r.Fetch(ctx, src, env)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(out), "sk-") {
			t.Fatalf("secret in result: %s", out)
		}
	}
	if f.count("initialize") != 1 {
		t.Fatalf("pooled connection initialized %d times", f.count("initialize"))
	}
	f.stop()
	f2 := startFake(t, sock)
	f2.handle = f.handle
	waitFor(t, "fetch after restart", func() bool {
		_, err := r.Fetch(ctx, src, env)
		return err == nil
	})
	r.Forget(env.Endpoint)
	if _, err := r.Fetch(ctx, src, harness.Env{}); !errors.Is(err, errNoEndpoint) {
		t.Fatalf("no endpoint: %v", err)
	}
}
