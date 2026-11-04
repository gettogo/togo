package test

import (
	"context"
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/gettogo/togo/helper/internal/rpc"
)

// daemonClient speaks the protocol over the daemon's unix socket and sorts
// what arrives into replies, by id, and event notifications.
type daemonClient struct {
	t      *testing.T
	conn   net.Conn
	mu     sync.Mutex
	nextID int
	wait   map[int]chan daemonReply
	events chan event
	caps   []string
}

type event struct {
	Seq       int             `json:"seq"`
	Kind      string          `json:"kind"`
	SessionID *string         `json:"sessionId"`
	Approval  json.RawMessage `json:"approval"`
	Session   struct {
		Status string `json:"status"`
	} `json:"session"`
	RequestID  string `json:"requestId"`
	Resolution string `json:"resolution"`
	Level      string `json:"level"`
}

type approvalInfo struct {
	RequestID string `json:"requestId"`
	SessionID string `json:"sessionId"`
	Source    string `json:"source"`
	Options   []struct {
		N    int    `json:"n"`
		Role string `json:"role"`
	} `json:"options"`
}

type daemonReply struct {
	result json.RawMessage
	err    json.RawMessage
}

type replyError struct {
	Code int             `json:"code"`
	Data json.RawMessage `json:"data"`
}

func dialDaemon(t *testing.T, sock string) *daemonClient {
	t.Helper()
	var conn net.Conn
	deadline := time.Now().Add(10 * time.Second)
	for {
		var err error
		conn, err = net.Dial("unix", sock)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("dial daemon: %v", err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	c := &daemonClient{t: t, conn: conn, wait: map[int]chan daemonReply{}, events: make(chan event, 256)}
	t.Cleanup(func() { conn.Close() })
	go c.read()
	var hello struct {
		Capabilities []string `json:"capabilities"`
	}
	c.call("hello", map[string]any{
		"protocol": 1, "client": map[string]string{"name": "togo-e2e", "version": "0"}, "capabilities": []string{},
	}, &hello)
	c.caps = hello.Capabilities
	return c
}

func (c *daemonClient) read() {
	for {
		payload, err := rpc.ReadFrame(c.conn)
		if err != nil {
			close(c.events)
			return
		}
		var msg struct {
			ID     *int            `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
			Result json.RawMessage `json:"result"`
			Error  json.RawMessage `json:"error"`
		}
		if json.Unmarshal(payload, &msg) != nil {
			continue
		}
		if msg.ID == nil {
			var ev event
			if msg.Method == "event" && json.Unmarshal(msg.Params, &ev) == nil {
				c.events <- ev
			}
			continue
		}
		c.mu.Lock()
		ch := c.wait[*msg.ID]
		delete(c.wait, *msg.ID)
		c.mu.Unlock()
		if ch != nil {
			ch <- daemonReply{result: msg.Result, err: msg.Error}
		}
	}
}

// try sends one request and returns its result or its error.
func (c *daemonClient) try(method string, params any) (json.RawMessage, *replyError) {
	c.t.Helper()
	c.mu.Lock()
	c.nextID++
	id := c.nextID
	ch := make(chan daemonReply, 1)
	c.wait[id] = ch
	c.mu.Unlock()
	req, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	if err != nil {
		c.t.Fatal(err)
	}
	if err := rpc.WriteFrame(c.conn, req); err != nil {
		c.t.Fatalf("%s: write: %v", method, err)
	}
	select {
	case r := <-ch:
		if len(r.err) > 0 && string(r.err) != "null" {
			var e replyError
			json.Unmarshal(r.err, &e)
			return nil, &e
		}
		return r.result, nil
	case <-time.After(30 * time.Second):
		c.t.Fatalf("%s: no reply", method)
		return nil, nil
	}
}

func (c *daemonClient) call(method string, params, out any) {
	c.t.Helper()
	res, e := c.try(method, params)
	if e != nil {
		c.t.Fatalf("%s: error %d %s", method, e.Code, e.Data)
	}
	if out != nil {
		if err := json.Unmarshal(res, out); err != nil {
			c.t.Fatalf("%s: result %s: %v", method, res, err)
		}
	}
}

// waitEvent returns the first event that match accepts.
func (c *daemonClient) waitEvent(what string, d time.Duration, match func(event) bool) event {
	c.t.Helper()
	timeout := time.After(d)
	for {
		select {
		case ev, ok := <-c.events:
			if !ok {
				c.t.Fatalf("connection closed waiting for %s", what)
			}
			if match(ev) {
				return ev
			}
		case <-timeout:
			c.t.Fatalf("no %s within %s", what, d)
		}
	}
}

// startDaemon runs `togo daemon` with env until the test ends.
func startDaemon(t *testing.T, bin string, env []string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(bin, "daemon")
	cmd.Env = env
	logf, err := os.Create(filepath.Join(t.TempDir(), "daemon.log"))
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stdout, cmd.Stderr = logf, logf
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cmd.Process.Signal(os.Interrupt)
		done := make(chan struct{})
		go func() { cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			cmd.Process.Kill()
		}
		logf.Close()
		if t.Failed() {
			b, _ := os.ReadFile(logf.Name())
			t.Logf("daemon log:\n%s", b)
		}
	})
	return cmd
}

// phone is a paired device: its keys open pushes and sign answers.
type phone struct {
	id       string
	x25519   *ecdh.PrivateKey
	sign     ed25519.PrivateKey
	hostID   string
	hostKey  [32]byte
	relayURL string
}

func newPhone(t *testing.T, relayURL string) *phone {
	t.Helper()
	x, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	_, sk, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return &phone{id: "e2e-phone", x25519: x, sign: sk, relayURL: relayURL}
}

func (p *phone) pair(t *testing.T, c *daemonClient) {
	t.Helper()
	b64 := base64.RawURLEncoding
	var res struct {
		HostID  string `json:"hostId"`
		HostKey string `json:"hostKey"`
	}
	c.call("push.pair", map[string]any{
		"deviceId": p.id, "name": "E2E phone",
		"x25519":      b64.EncodeToString(p.x25519.PublicKey().Bytes()),
		"approvalKey": b64.EncodeToString(p.sign.Public().(ed25519.PublicKey)),
		"apnsToken":   strings.Repeat("ab", 32), "env": "sandbox", "relayUrl": p.relayURL,
	}, &res)
	key, err := b64.DecodeString(res.HostKey)
	if err != nil || len(key) != 32 {
		t.Fatalf("hostKey %q: %v", res.HostKey, err)
	}
	p.hostID = res.HostID
	copy(p.hostKey[:], key)
}

// fakeRelay records pushes and serves one mailbox WebSocket that sends
// whatever is queued on frames and reports acks.
type fakeRelay struct {
	srv    *httptest.Server
	pushes chan map[string]any
	frames chan string
	acks   chan string
}

func newFakeRelay(t *testing.T) *fakeRelay {
	t.Helper()
	r := &fakeRelay{pushes: make(chan map[string]any, 32), frames: make(chan string, 8), acks: make(chan string, 8)}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/push", func(w http.ResponseWriter, req *http.Request) {
		var body map[string]any
		json.NewDecoder(req.Body).Decode(&body)
		r.pushes <- body
		w.WriteHeader(http.StatusAccepted)
		w.Write([]byte(`{"apnsId":"e2e"}`))
	})
	mux.HandleFunc("GET /v1/mailbox/{host}/ws", func(w http.ResponseWriter, req *http.Request) {
		c, err := websocket.Accept(w, req, nil)
		if err != nil {
			return
		}
		ctx := req.Context()
		go func() {
			for {
				_, b, err := c.Read(ctx)
				if err != nil {
					return
				}
				var f struct{ Type, ID string }
				if json.Unmarshal(b, &f) == nil && f.Type == "ack" {
					r.acks <- f.ID
				}
				if f.Type == "ping" {
					c.Write(ctx, websocket.MessageText, []byte(`{"type":"pong"}`))
				}
			}
		}()
		for {
			select {
			case <-ctx.Done():
				return
			case f := <-r.frames:
				if err := c.Write(context.Background(), websocket.MessageText, []byte(f)); err != nil {
					return
				}
			}
		}
	})
	r.srv = httptest.NewServer(mux)
	t.Cleanup(r.srv.Close)
	return r
}

func base64URL(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }
