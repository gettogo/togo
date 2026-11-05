package mailbox

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/gettogo/togo/helper/internal/wire"
)

// fakeRelay mirrors the relay mailbox protocol: Bearer auth on the
// WebSocket upgrade, pending messages flushed on connect, live delivery,
// delete on ack, ping answered with pong, one socket per host.
type fakeRelay struct {
	t      *testing.T
	srv    *httptest.Server
	hostID string
	bearer string

	mu      sync.Mutex
	next    int
	pending []frame
	conn    *websocket.Conn
	dials   []time.Time
	reject  int // upgrades still to refuse with 503
	acked   chan string
	conned  chan struct{}
	unknown []string // ack ids that matched nothing
}

func newFakeRelay(t *testing.T, keys Keys) *fakeRelay {
	r := &fakeRelay{t: t, hostID: keys.HostID,
		bearer: "Bearer " + base64.RawURLEncoding.EncodeToString(keys.MailboxSecret),
		acked:  make(chan string, 64), conned: make(chan struct{}, 64)}
	r.srv = httptest.NewServer(http.HandlerFunc(r.serve))
	t.Cleanup(r.srv.Close)
	return r
}

func (r *fakeRelay) serve(w http.ResponseWriter, req *http.Request) {
	if req.URL.Path != "/v1/mailbox/"+r.hostID+"/ws" {
		http.NotFound(w, req)
		return
	}
	r.mu.Lock()
	r.dials = append(r.dials, time.Now())
	if r.reject > 0 {
		r.reject--
		r.mu.Unlock()
		http.Error(w, "busy", http.StatusServiceUnavailable)
		return
	}
	r.mu.Unlock()
	if req.Header.Get("Authorization") != r.bearer {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	c, err := websocket.Accept(w, req, nil)
	if err != nil {
		return
	}
	r.mu.Lock()
	if r.conn != nil {
		r.conn.Close(StatusReplaced, "replaced")
	}
	r.conn = c
	flush := append([]frame(nil), r.pending...)
	r.mu.Unlock()
	ctx := req.Context()
	for _, f := range flush {
		r.send(ctx, c, f)
	}
	r.conned <- struct{}{}
	for {
		_, data, err := c.Read(ctx)
		if err != nil {
			return
		}
		var f frame
		json.Unmarshal(data, &f)
		switch f.Type {
		case "ping":
			c.Write(ctx, websocket.MessageText, []byte(`{"type":"pong"}`))
		case "ack":
			r.ack(f.ID)
		}
	}
}

func (r *fakeRelay) send(ctx context.Context, c *websocket.Conn, f frame) {
	b, _ := json.Marshal(f)
	c.Write(ctx, websocket.MessageText, b)
}

func (r *fakeRelay) ack(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, f := range r.pending {
		if f.ID == id {
			r.pending = append(r.pending[:i], r.pending[i+1:]...)
			r.acked <- id
			return
		}
	}
	r.unknown = append(r.unknown, id)
}

// post queues a ciphertext and delivers it live when a socket is open.
func (r *fakeRelay) post(ct string) string {
	r.mu.Lock()
	r.next++
	f := frame{Type: "msg", ID: fmt.Sprintf("01JTEST%019d", r.next), Ciphertext: ct, ReceivedAt: time.Now().UnixMilli()}
	r.pending = append(r.pending, f)
	c := r.conn
	r.mu.Unlock()
	if c != nil {
		r.send(context.Background(), c, f)
	}
	return f.ID
}

func (r *fakeRelay) dialCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.dials)
}

type answered struct{ req, dec, by string }

// harness runs Run against a fake relay with fast timings.
type harness struct {
	f       *fixture
	relay   *fakeRelay
	answers chan answered
	logs    *logBuf
	devMu   sync.Mutex
	devs    []Device
	cancel  context.CancelFunc
	done    chan error
}

type logBuf struct {
	mu    sync.Mutex
	lines []string
}

func (l *logBuf) logf(format string, args ...any) {
	l.mu.Lock()
	l.lines = append(l.lines, fmt.Sprintf(format, args...))
	l.mu.Unlock()
}

func (l *logBuf) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.lines, "\n")
}

func newHarness(t *testing.T, mutate func(*harness)) *harness {
	t.Helper()
	f := newFixture(t, "")
	f.now = time.Now()
	h := &harness{f: f, answers: make(chan answered, 16), logs: &logBuf{}}
	h.relay = newFakeRelay(t, f.keys)
	f.devices[0].RelayURL = h.relay.srv.URL + "/"
	h.devs = append([]Device(nil), f.devices...)
	if mutate != nil {
		mutate(h)
	}
	cfg := Config{
		Keys:    f.keys,
		Devices: h.devices,
		Options: func(id string) ([]wire.Option, bool) { o, ok := f.opts[id]; return o, ok },
		Answer: func(_ context.Context, req, dec, by string) error {
			h.answers <- answered{req, dec, by}
			return nil
		},
		Logf:           h.logs.logf,
		MinBackoff:     20 * time.Millisecond,
		MaxBackoff:     200 * time.Millisecond,
		PingInterval:   50 * time.Millisecond,
		RescanInterval: 10 * time.Millisecond,
	}
	ctx, cancel := context.WithCancel(context.Background())
	h.cancel, h.done = cancel, make(chan error, 1)
	go func() { h.done <- Run(ctx, cfg) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-h.done:
		case <-time.After(5 * time.Second):
			t.Error("Run did not return after cancel")
		}
	})
	return h
}

func (h *harness) devices() []Device {
	h.devMu.Lock()
	defer h.devMu.Unlock()
	return append([]Device(nil), h.devs...)
}

// setDevices stores a copy, so the caller may keep changing d.
func (h *harness) setDevices(d []Device) {
	h.devMu.Lock()
	h.devs = append([]Device(nil), d...)
	h.devMu.Unlock()
}

// setDead marks the first device dead or alive under the lock.
func (h *harness) setDead(dead bool) {
	h.devMu.Lock()
	h.devs[0].Dead = dead
	h.devMu.Unlock()
}

func (h *harness) waitAck(t *testing.T, id string) {
	t.Helper()
	select {
	case got := <-h.relay.acked:
		if got != id {
			t.Fatalf("acked %s, want %s", got, id)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no ack")
	}
}

func (h *harness) noAnswer(t *testing.T) {
	t.Helper()
	select {
	case a := <-h.answers:
		t.Fatalf("answer reached store: %+v", a)
	case <-time.After(50 * time.Millisecond):
	}
}

func expectDropped(t *testing.T, ct func(h *harness) string) {
	t.Helper()
	h := newHarness(t, nil)
	h.waitAck(t, h.relay.post(ct(h)))
	h.noAnswer(t)
}

func TestValidAnswerReachesStoreAndIsAcked(t *testing.T) {
	h := newHarness(t, nil)
	// Posted before the connection exists: delivered on connect.
	id := h.relay.post(h.f.good(t, "option:1"))
	h.waitAck(t, id)
	select {
	case a := <-h.answers:
		if a != (answered{testReq, "option:1", "phone"}) {
			t.Fatalf("answer %+v", a)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("answer never reached store")
	}
}

func TestForgedSignatureDropped(t *testing.T) {
	_, other, _ := ed25519.GenerateKey(rand.Reader)
	expectDropped(t, func(h *harness) string {
		return h.f.answer(t, testReq, "approve", h.f.now.UnixMilli(), other)
	})
}

func TestReplayDropped(t *testing.T) {
	h := newHarness(t, nil)
	ct := h.f.good(t, "approve")
	h.waitAck(t, h.relay.post(ct))
	<-h.answers
	h.waitAck(t, h.relay.post(ct))
	h.noAnswer(t)
}

func TestStaleTimestampDropped(t *testing.T) {
	expectDropped(t, func(h *harness) string {
		return h.f.answer(t, testReq, "approve", h.f.now.Add(-11*time.Minute).UnixMilli(), h.f.devPriv)
	})
}

func TestRevokedDeviceDropped(t *testing.T) {
	h := newHarness(t, func(h *harness) {
		// Two devices on the same relay; the signer is revoked, the other
		// keeps the connection alive.
		dead := h.devs[0]
		dead.Dead = true
		pub, _, _ := ed25519.GenerateKey(rand.Reader)
		h.devs = []Device{dead, {DeviceID: "watch-1", ApprovalKey: pub, RelayURL: dead.RelayURL}}
	})
	h.waitAck(t, h.relay.post(h.f.good(t, "approve")))
	h.noAnswer(t)
}

func TestTextDecisionViaRelayDropped(t *testing.T) {
	expectDropped(t, func(h *harness) string { return h.f.good(t, "text:rm -rf ~") })
}

func TestApproveAlwaysOptionViaRelayDropped(t *testing.T) {
	expectDropped(t, func(h *harness) string { return h.f.good(t, "option:2") })
}

func TestStoreRejectionIsAcked(t *testing.T) {
	// A definitive store error acks; a shutdown does not, and frees the
	// requestId so the redelivered answer can still be applied.
	f := newFixture(t, "")
	r := &runner{cfg: Config{Logf: func(string, ...any) {}}, v: f.verifier()}
	r.cfg.Answer = func(context.Context, string, string, string) error { return errors.New("answered") }
	if !r.handle(context.Background(), frame{ID: "m1", Ciphertext: f.good(t, "approve")}) {
		t.Fatal("definitive rejection was not acked")
	}
	f2 := newFixture(t, "")
	r = &runner{cfg: Config{Logf: func(string, ...any) {}}, v: f2.verifier()}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r.cfg.Answer = func(ctx context.Context, _, _, _ string) error { return ctx.Err() }
	ct := f2.good(t, "approve")
	if r.handle(ctx, frame{ID: "m2", Ciphertext: ct}) {
		t.Fatal("acked although shutdown interrupted handling")
	}
	if _, _, err := r.v.Verify(ct); err != nil {
		t.Fatalf("redelivery after shutdown refused: %v", err)
	}
}

func TestReconnectsWithBackoff(t *testing.T) {
	h := newHarness(t, func(h *harness) { h.relay.reject = 3 })
	select {
	case <-h.relay.conned:
	case <-time.After(5 * time.Second):
		t.Fatal("never connected")
	}
	h.relay.mu.Lock()
	dials := append([]time.Time(nil), h.relay.dials...)
	h.relay.mu.Unlock()
	if len(dials) != 4 {
		t.Fatalf("%d dials, want 4", len(dials))
	}
	// Delays are jittered within [base/2, base] with base 20, 40, 80 ms.
	for i, minGap := range []time.Duration{10, 20, 40} {
		if gap := dials[i+1].Sub(dials[i]); gap < minGap*time.Millisecond {
			t.Errorf("gap %d = %v, want >= %v ms", i, gap, minGap)
		}
	}

	// A 4000 close from a newer client is followed by a backed-off redial,
	// not an immediate one.
	h.relay.mu.Lock()
	h.relay.conn.Close(StatusReplaced, "replaced")
	h.relay.conn = nil
	closedAt := time.Now()
	h.relay.mu.Unlock()
	select {
	case <-h.relay.conned:
	case <-time.After(5 * time.Second):
		t.Fatal("no reconnect after replace")
	}
	h.relay.mu.Lock()
	last := h.relay.dials[len(h.relay.dials)-1]
	h.relay.mu.Unlock()
	if gap := last.Sub(closedAt); gap < 10*time.Millisecond {
		t.Errorf("redial %v after replace, want backoff", gap)
	}
	if !strings.Contains(h.logs.String(), "replaced") {
		t.Error("replace not logged")
	}
}

func TestNoConnectionWithoutDevices(t *testing.T) {
	h := newHarness(t, func(h *harness) {
		h.devs[0].Dead = true
	})
	time.Sleep(100 * time.Millisecond)
	if n := h.relay.dialCount(); n != 0 {
		t.Fatalf("%d dials with no live device", n)
	}
	h.setDead(false)
	select {
	case <-h.relay.conned:
	case <-time.After(5 * time.Second):
		t.Fatal("no connection after a live device appeared")
	}
	// Revoking the last device closes the connection and stops redialing.
	h.setDead(true)
	time.Sleep(100 * time.Millisecond)
	n := h.relay.dialCount()
	time.Sleep(150 * time.Millisecond)
	if m := h.relay.dialCount(); m != n {
		t.Fatalf("redialed (%d -> %d) after the last device was revoked", n, m)
	}
}

func TestPingKeepsConnection(t *testing.T) {
	h := newHarness(t, nil)
	<-h.relay.conned
	time.Sleep(400 * time.Millisecond) // 8 ping intervals
	if n := h.relay.dialCount(); n != 1 {
		t.Fatalf("%d dials; pongs should keep one connection", n)
	}
}

func TestLogsContainNoCiphertext(t *testing.T) {
	h := newHarness(t, nil)
	_, other, _ := ed25519.GenerateKey(rand.Reader)
	good := h.f.good(t, "option:1")
	bad := h.f.answer(t, testReq, "deny", h.f.now.UnixMilli(), other)
	idGood := h.relay.post(good)
	h.waitAck(t, idGood)
	idBad := h.relay.post(bad)
	h.waitAck(t, idBad)
	logs := h.logs.String()
	if logs == "" {
		t.Fatal("expected log lines")
	}
	secret := base64.RawURLEncoding.EncodeToString(h.f.keys.MailboxSecret)
	for _, s := range []string{good, bad, good[:24], bad[:24], secret, idGood, idBad, testReq, "option:1", "deny"} {
		if strings.Contains(logs, s) {
			t.Fatalf("log contains %q:\n%s", s, logs)
		}
	}
	if !strings.Contains(logs, idHash(idBad)) || !strings.Contains(logs, ErrSignature.Error()) {
		t.Fatalf("reject not logged with reason and id hash:\n%s", logs)
	}
}

func TestRunRejectsBadKeys(t *testing.T) {
	err := Run(context.Background(), Config{Keys: Keys{HostID: "h_x", MailboxSecret: []byte("short")},
		Devices: func() []Device { return nil }, Answer: func(context.Context, string, string, string) error { return nil }})
	if err == nil {
		t.Fatal("short secret accepted")
	}
}
