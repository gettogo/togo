package push

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/api"
	"github.com/gettogo/togo/helper/internal/rpc"
	"github.com/gettogo/togo/helper/internal/seal"
	"github.com/gettogo/togo/helper/internal/session"
	"github.com/gettogo/togo/helper/internal/wire"
)

// relayStub records every /v1/push body and answers with the status the
// test chooses.
type relayStub struct {
	*httptest.Server
	mu     sync.Mutex
	bodies [][]byte
	status atomic.Int32
	answer atomic.Value // string
}

func newRelay(t *testing.T) *relayStub {
	r := &relayStub{}
	r.status.Store(http.StatusAccepted)
	r.answer.Store(`{"apnsId":"x"}`)
	r.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/v1/push" || req.Method != http.MethodPost {
			http.NotFound(w, req)
			return
		}
		b, _ := io.ReadAll(req.Body)
		r.mu.Lock()
		r.bodies = append(r.bodies, b)
		r.mu.Unlock()
		w.WriteHeader(int(r.status.Load()))
		_, _ = io.WriteString(w, r.answer.Load().(string))
	}))
	t.Cleanup(r.Close)
	return r
}

func (r *relayStub) received() []relayPush {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []relayPush
	for _, b := range r.bodies {
		var p relayPush
		if err := json.Unmarshal(b, &p); err != nil {
			panic(err)
		}
		out = append(out, p)
	}
	return out
}

func (r *relayStub) raw() [][]byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([][]byte(nil), r.bodies...)
}

type fakeHub struct {
	fg  atomic.Int32
	mu  sync.Mutex
	fns []func(wire.Event)
}

func (h *fakeHub) OnEvent(fn func(wire.Event)) {
	h.mu.Lock()
	h.fns = append(h.fns, fn)
	h.mu.Unlock()
}

func (h *fakeHub) ForegroundSubscribers() int { return int(h.fg.Load()) }

func (h *fakeHub) publish(ev wire.Event) {
	h.mu.Lock()
	fns := h.fns
	h.mu.Unlock()
	for _, fn := range fns {
		fn(ev)
	}
}

type phone struct {
	id   string
	priv [32]byte
}

type env struct {
	svc    *Service
	sender *Sender
	hub    *fakeHub
	relay  *relayStub
	phones []phone
	muted  map[string]bool
	clock  time.Time
	logs   *bytes.Buffer
}

func newEnv(t *testing.T, devices int) *env {
	t.Helper()
	e := &env{hub: &fakeHub{}, relay: newRelay(t), muted: map[string]bool{}, logs: &bytes.Buffer{},
		clock: time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)}
	e.svc = &Service{Home: t.TempDir(), Getenv: func(k string) string {
		if k == InsecureRelayEnv {
			return "1"
		}
		return ""
	}}
	var logMu sync.Mutex
	e.sender = &Sender{
		Hub: e.hub, Keys: e.svc.HostKeys, Devices: e.svc.Devices(),
		Session: func(id string) (SessionInfo, bool) {
			bib, agent := "01", "claude"
			return SessionInfo{Bib: &bib, Agent: &agent, Muted: e.muted[id]}, true
		},
		Now:   func() time.Time { return e.clock },
		Sleep: func(context.Context, time.Duration) error { return nil },
		Logf: func(f string, a ...any) {
			logMu.Lock()
			defer logMu.Unlock()
			e.logs.WriteString(strings.TrimSpace(sprintf(f, a...)) + "\n")
		},
	}
	for i := range devices {
		e.pair(t, "dev_"+string(rune('a'+i)), "")
	}
	return e
}

func (e *env) pair(t *testing.T, id, liveToken string) {
	t.Helper()
	priv, pub, err := seal.GenerateX25519()
	if err != nil {
		t.Fatal(err)
	}
	p := pairParams(id, b64.EncodeToString(pub[:]), e.relay.URL)
	p.LiveActivityToken = liveToken
	if _, err := e.call(wire.MethodPushPair, p); err != nil {
		t.Fatalf("pair %s: %v", id, err)
	}
	e.phones = append(e.phones, phone{id: id, priv: priv})
}

func pairParams(id, x25519, relay string) wire.PairParams {
	return wire.PairParams{
		DeviceID: id, Name: "Test iPhone", X25519: x25519,
		ApprovalKey: "ERERERERERERERERERERERERERERERERERERERERERE",
		APNsToken:   strings.Repeat("ab", 32), Env: wire.EnvSandbox, RelayURL: relay,
	}
}

func (e *env) call(method string, params any) (any, error) {
	raw, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	return e.svc.Methods()[method](context.Background(), raw)
}

// handle runs one event through the sender synchronously.
func (e *env) handle(ev wire.Event) {
	e.sender.init()
	if m, ok := e.sender.message(ev); ok {
		e.sender.Deliver(context.Background(), m)
	}
}

func (e *env) open(t *testing.T, ct string, deviceID string) wire.PushPlaintext {
	t.Helper()
	keys, err := e.svc.HostKeys()
	if err != nil {
		t.Fatal(err)
	}
	for _, ph := range e.phones {
		if ph.id != deviceID {
			continue
		}
		raw, err := seal.Open(ph.priv, ct, seal.LabelPush, []byte(keys.HostID+"|"+deviceID))
		if err != nil {
			t.Fatalf("open for %s: %v", deviceID, err)
		}
		var p wire.PushPlaintext
		if err := json.Unmarshal(raw, &p); err != nil {
			t.Fatal(err)
		}
		return p
	}
	t.Fatalf("no phone %s", deviceID)
	return wire.PushPlaintext{}
}

func sid(s string) *string { return &s }

func approvalOpen(rid string) wire.Event {
	detail := "Bash: npm test"
	return wire.Event{Kind: wire.EventApprovalOpen, SessionID: sid("s_abcdefghij"), Payload: wire.ApprovalOpenEvent{Approval: wire.Approval{
		RequestID: rid, SessionID: sid("s_abcdefghij"), Bib: sid("01"), Agent: "claude", Source: wire.SourceHook,
		Title: "Run npm test?", Detail: &detail, Snippet: "x",
		Options: []wire.Option{
			{N: 1, Label: "Yes", Role: wire.RoleApprove},
			{N: 2, Label: "Yes, and don't ask again", Role: wire.RoleApproveAlways},
			{N: 3, Label: "No", Role: wire.RoleDeny},
		},
		CreatedAt: time.Date(2026, 11, 3, 11, 59, 0, 0, time.UTC), State: wire.StatePending,
	}}}
}

func notifyEv(session, level string) wire.Event {
	return wire.Event{Kind: wire.EventNotify, SessionID: sid(session), Payload: &wire.NotifyEvent{Title: "Build", Body: "finished", Level: level}}
}

func TestPairCreatesHostKeysOnce0600(t *testing.T) {
	e := newEnv(t, 0)
	_, pub, _ := seal.GenerateX25519()
	first, err := e.call(wire.MethodPushPair, pairParams("dev_a", b64.EncodeToString(pub[:]), "https://relay.example.test"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := e.call(wire.MethodPushPair, pairParams("dev_b", b64.EncodeToString(pub[:]), "https://relay.example.test"))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("host identity changed between pairings: %v then %v", first, second)
	}
	res := first.(wire.PairResult)
	if !strings.HasPrefix(res.HostID, "h_") || len(res.HostID) != 28 || len(res.HostKey) != 43 {
		t.Fatalf("bad pair result %+v", res)
	}
	for _, name := range []string{hostKeysFile, devicesFile} {
		fi, err := os.Stat(filepath.Join(e.svc.Home, keysDir, name))
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm() != 0o600 {
			t.Fatalf("%s mode %v, want 0600", name, fi.Mode().Perm())
		}
	}
	fi, _ := os.Stat(filepath.Join(e.svc.Home, keysDir))
	if fi.Mode().Perm() != 0o700 {
		t.Fatalf("keys dir mode %v", fi.Mode().Perm())
	}
	// A fresh process over the same home reads the same keys.
	again, err := LoadOrCreateHostKeys(e.svc.Home)
	if err != nil || again.HostID != res.HostID || again.HostKey() != res.HostKey {
		t.Fatalf("reload gave %v, %v", again, err)
	}
	if strings.Contains(again.String()+sprintf("%v %#v", again, again), b64.EncodeToString(again.MailboxSecret[:])) {
		t.Fatal("HostKeys formatting leaks the mailbox secret")
	}
}

func TestPairRejectsHTTPRelay(t *testing.T) {
	_, pub, _ := seal.GenerateX25519()
	key := b64.EncodeToString(pub[:])
	strict := &Service{Home: t.TempDir()}
	insecure := &Service{Home: t.TempDir(), Getenv: func(string) string { return "1" }}
	cases := []struct {
		svc   *Service
		relay string
		ok    bool
	}{
		{strict, "http://relay.example.test", false},
		{strict, "http://127.0.0.1:8787", false},
		{insecure, "http://127.0.0.1:8787", true},
		{insecure, "http://localhost:8787", false},
		{insecure, "http://relay.example.test", false},
		{strict, "https://user:pw@relay.example.test", false},
		{strict, "https://relay.example.test?x=1", false},
		{strict, "ftp://relay.example.test", false},
		{strict, "https://relay.example.test/base", true},
	}
	for _, c := range cases {
		raw, _ := json.Marshal(pairParams("dev_a", key, c.relay))
		_, err := c.svc.Methods()[wire.MethodPushPair](context.Background(), raw)
		var inv *session.InvalidParamsError
		if c.ok != (err == nil) || (!c.ok && !errors.As(err, &inv)) {
			t.Errorf("relay %q: err %v, want ok=%v", c.relay, err, c.ok)
		}
	}
}

func TestPairValidatesFieldsWithoutEchoingSecrets(t *testing.T) {
	e := newEnv(t, 0)
	_, pub, _ := seal.GenerateX25519()
	good := pairParams("dev_a", b64.EncodeToString(pub[:]), "https://relay.example.test")
	bad := []func(p *wire.PairParams){
		func(p *wire.PairParams) { p.DeviceID = "bad id" },
		func(p *wire.PairParams) { p.Name = "" },
		func(p *wire.PairParams) { p.Name = strings.Repeat("n", 65) },
		func(p *wire.PairParams) { p.X25519 = "short" },
		func(p *wire.PairParams) { p.ApprovalKey = p.ApprovalKey + "=" },
		func(p *wire.PairParams) { p.APNsToken = strings.Repeat("AB", 32) },
		func(p *wire.PairParams) { p.APNsToken = "abcd" },
		func(p *wire.PairParams) { p.LiveActivityToken = "zz" },
		func(p *wire.PairParams) { p.Env = "prod" },
	}
	for i, mut := range bad {
		p := good
		mut(&p)
		_, err := e.call(wire.MethodPushPair, p)
		var inv *session.InvalidParamsError
		if !errors.As(err, &inv) {
			t.Fatalf("case %d: err %v, want invalid params", i, err)
		}
		if strings.Contains(inv.Msg, p.APNsToken) || strings.Contains(inv.Msg, p.X25519) {
			t.Fatalf("case %d: message echoes a secret: %q", i, inv.Msg)
		}
	}
}

func TestUnpairUnknownDeviceIsNotFound(t *testing.T) {
	e := newEnv(t, 1)
	if _, err := e.call(wire.MethodPushUnpair, map[string]string{"deviceId": "dev_a"}); err != nil {
		t.Fatal(err)
	}
	_, err := e.call(wire.MethodPushUnpair, map[string]string{"deviceId": "dev_a"})
	var ce *api.CodeError
	if !errors.As(err, &ce) || ce.Code != rpc.CodeNotFound {
		t.Fatalf("second unpair: %v, want -32001", err)
	}
	if list, _ := e.svc.Devices().List(); len(list) != 0 {
		t.Fatalf("devices after unpair: %d", len(list))
	}
}

func openRaw(ph phone, ct, hostID string) ([]byte, error) {
	return seal.Open(ph.priv, ct, seal.LabelPush, []byte(hostID+"|"+ph.id))
}
