package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

// pipe is an in-memory transport pair.
type pipe struct {
	in, out chan []byte
	done    chan struct{}
}

func pipes() (*pipe, *pipe) {
	a2b, b2a := make(chan []byte, 64), make(chan []byte, 64)
	done := make(chan struct{})
	return &pipe{in: b2a, out: a2b, done: done}, &pipe{in: a2b, out: b2a, done: done}
}

func (p *pipe) Read(ctx context.Context) ([]byte, error) {
	select {
	case b := <-p.in:
		return b, nil
	case <-p.done:
		return nil, io.EOF
	}
}

func (p *pipe) Write(_ context.Context, b []byte) error {
	select {
	case <-p.done:
		return io.EOF
	default:
	}
	p.out <- b
	return nil
}

func (p *pipe) Close() error {
	select {
	case <-p.done:
	default:
		close(p.done)
	}
	return nil
}

func readMsg(t *testing.T, p *pipe) Message {
	t.Helper()
	select {
	case b := <-p.in:
		var m Message
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatal(err)
		}
		return m
	case <-time.After(2 * time.Second):
		t.Fatal("no message")
	}
	return Message{}
}

func send(p *pipe, s string) { p.out <- []byte(s) }

func TestCallMatchesIDsOutOfOrder(t *testing.T) {
	client, server := pipes()
	c := NewConn(client, Handler{})
	defer c.Close()
	type res struct {
		v   string
		err error
	}
	r1, r2 := make(chan res, 1), make(chan res, 1)
	go func() { b, err := c.Call(context.Background(), "a", nil); r1 <- res{string(b), err} }()
	m1 := readMsg(t, server)
	go func() {
		b, err := c.Call(context.Background(), "b", map[string]string{"x": "1"})
		r2 <- res{string(b), err}
	}()
	m2 := readMsg(t, server)
	if m1.Method != "a" || m2.Method != "b" || string(m2.Params) != `{"x":"1"}` || m1.JSONRPC != "2.0" {
		t.Fatalf("requests: %+v %+v", m1, m2)
	}
	send(server, `{"id":`+string(m2.ID)+`,"result":"two"}`)
	send(server, `{"id":`+string(m1.ID)+`,"error":{"code":-32000,"message":"nope"}}`)
	if r := <-r2; r.err != nil || r.v != `"two"` {
		t.Fatalf("second: %+v", r)
	}
	var rpcErr *Error
	if r := <-r1; !errors.As(r.err, &rpcErr) || rpcErr.Code != -32000 {
		t.Fatalf("first: %+v", r)
	}
}

func TestNotificationsAndServerRequests(t *testing.T) {
	client, server := pipes()
	notes := make(chan Message, 4)
	reqs := make(chan Message, 4)
	c := NewConn(client, Handler{
		Notification: func(m Message) { notes <- m },
		Request:      func(m Message) { reqs <- m },
	})
	defer c.Close()
	send(server, `{"method":"turn/started","params":{"threadId":"t"}}`)
	send(server, `{"id":"req-1","method":"item/tool/requestUserInput","params":{"q":1}}`)
	send(server, `not json`)
	send(server, `{"id":99,"result":{}}`) // a response nobody waits for is dropped
	if m := <-notes; m.Method != "turn/started" {
		t.Fatalf("note %+v", m)
	}
	m := <-reqs
	if m.Method != "item/tool/requestUserInput" || RequestKey(m.ID) != "rq-req-1" {
		t.Fatalf("request %+v", m)
	}
	// The client does not answer a server request on its own.
	select {
	case b := <-server.in:
		t.Fatalf("unexpected auto reply %s", b)
	case <-time.After(100 * time.Millisecond):
	}
	if err := c.Reply(context.Background(), m.ID, json.RawMessage(`{"answers":{}}`), nil); err != nil {
		t.Fatal(err)
	}
	r := readMsg(t, server)
	if string(r.ID) != `"req-1"` || string(r.Result) != `{"answers":{}}` || r.Method != "" {
		t.Fatalf("reply %+v", r)
	}
}

func TestCallTimeoutAndClose(t *testing.T) {
	client, server := pipes()
	c := NewConn(client, Handler{})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := c.Call(ctx, "slow", nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout: %v", err)
	}
	readMsg(t, server)
	c.mu.Lock()
	n := len(c.pending)
	c.mu.Unlock()
	if n != 0 {
		t.Fatalf("pending left after timeout: %d", n)
	}
	errc := make(chan error, 1)
	go func() { _, err := c.Call(context.Background(), "hang", nil); errc <- err }()
	readMsg(t, server)
	server.Close()
	if err := <-errc; !errors.Is(err, ErrClosed) {
		t.Fatalf("call on dropped conn: %v", err)
	}
	<-c.Done()
	if _, err := c.Call(context.Background(), "after", nil); !errors.Is(err, ErrClosed) {
		t.Fatalf("call after close: %v", err)
	}
}

func TestLinesFraming(t *testing.T) {
	in := strings.NewReader("\n{\"method\":\"a\"}\n  \n{\"id\":1,\"result\":true}\n")
	var out bytes.Buffer
	tr := Lines(in, &out, nil)
	b, err := tr.Read(context.Background())
	if err != nil || string(b) != `{"method":"a"}` {
		t.Fatalf("read: %q %v", b, err)
	}
	b, _ = tr.Read(context.Background())
	if string(b) != `{"id":1,"result":true}` {
		t.Fatalf("second: %q", b)
	}
	if _, err := tr.Read(context.Background()); !errors.Is(err, io.EOF) {
		t.Fatalf("eof: %v", err)
	}
	tr.Write(context.Background(), []byte(`{"x":1}`))
	if out.String() != "{\"x\":1}\n" {
		t.Fatalf("write: %q", out.String())
	}
}

func TestRequestKeyAndSocketPath(t *testing.T) {
	if RequestKey(json.RawMessage(`7`)) != "rq-7" || RequestKey(json.RawMessage(`"a"`)) != "rq-a" {
		t.Fatal("request key")
	}
	if idKey(json.RawMessage(`5`)) == idKey(json.RawMessage(`"5"`)) {
		t.Fatal("number and string ids must differ")
	}
	for _, ok := range []string{"unix:///tmp/x.sock", "/tmp/x.sock"} {
		if _, err := SocketPath(ok); err != nil {
			t.Errorf("%s: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "unix://rel.sock", "/tmp/../etc/x", "ws://127.0.0.1:1"} {
		if _, err := SocketPath(bad); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}
}
