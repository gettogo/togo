package hub_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/gettogo/togo/helper/internal/hub"
	"github.com/gettogo/togo/helper/internal/rpc"
	"github.com/gettogo/togo/helper/internal/session"
	"github.com/gettogo/togo/helper/internal/wire"
)

func (e *env) handle(t *testing.T, method string, params any) (json.RawMessage, *rpc.Error) {
	t.Helper()
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	result, rpcErr := e.api.Handle(context.Background(), method, raw)
	if rpcErr != nil {
		return nil, rpcErr
	}
	out, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	return out, nil
}

func (e *env) list(t *testing.T) []session.Session {
	t.Helper()
	raw, rpcErr := e.handle(t, "session.list", map[string]any{})
	if rpcErr != nil {
		t.Fatalf("session.list: %+v", rpcErr)
	}
	var r struct{ Sessions []session.Session }
	if err := json.Unmarshal(raw, &r); err != nil {
		t.Fatal(err)
	}
	return r.Sessions
}

func TestSetMutePersistsInTmuxOption(t *testing.T) {
	e := newEnv(t)
	sess := e.createShell(t)
	raw, rpcErr := e.handle(t, wire.MethodSessionSetMute, map[string]any{"id": sess.ID, "muted": true})
	if rpcErr != nil {
		t.Fatalf("setMute: %+v", rpcErr)
	}
	var got session.Session
	if err := json.Unmarshal(raw, &got); err != nil || !got.Muted {
		t.Fatalf("setMute result %s, want muted", raw)
	}

	// A new hub knows nothing in memory, as after a daemon restart.
	fresh := hub.New(e.svc, nil)
	if !fresh.Muted(sess.ID) {
		t.Fatal("new hub reads the session as unmuted")
	}
	if _, rpcErr := e.handle(t, wire.MethodSessionSetMute, map[string]any{"id": sess.ID, "muted": false}); rpcErr != nil {
		t.Fatalf("unmute: %+v", rpcErr)
	}
	if hub.New(e.svc, nil).Muted(sess.ID) {
		t.Fatal("unmute did not reach tmux")
	}
}

func TestSetMutePublishesSession(t *testing.T) {
	e := newEnv(t)
	sess := e.createShell(t)
	c := e.dial(t)
	c.subscribe(true)
	c.call(wire.MethodSessionSetMute, map[string]any{"id": sess.ID, "muted": true})
	ev := c.event()
	var p struct {
		Session session.Session `json:"session"`
	}
	if err := json.Unmarshal(ev.Payload.(json.RawMessage), &p); err != nil {
		t.Fatal(err)
	}
	if ev.Kind != wire.EventSession || *ev.SessionID != sess.ID || !p.Session.Muted {
		t.Fatalf("event %+v session %+v, want a muted session event", ev, p.Session)
	}
}

func TestSetMuteUnmanagedIsNotManaged(t *testing.T) {
	e := newEnv(t)
	_, rpcErr := e.handle(t, wire.MethodSessionSetMute, map[string]any{"id": "u_776f726b", "muted": true})
	if rpcErr == nil || rpcErr.Code != rpc.CodeNotManaged {
		t.Fatalf("got %+v, want -32007", rpcErr)
	}
}

func TestSetMuteRejectsBadParams(t *testing.T) {
	e := newEnv(t)
	for _, params := range []map[string]any{
		{"id": "s_abcdefghij"},
		{"id": "nope", "muted": true},
		{"id": "s_abcdefghij", "muted": "yes"},
	} {
		if _, rpcErr := e.handle(t, wire.MethodSessionSetMute, params); rpcErr == nil || rpcErr.Code != rpc.CodeInvalidParams {
			t.Errorf("params %v: got %+v, want -32602", params, rpcErr)
		}
	}
}

func TestSetMuteUnknownSessionIsNotFound(t *testing.T) {
	e := newEnv(t)
	e.createShell(t)
	_, rpcErr := e.handle(t, wire.MethodSessionSetMute, map[string]any{"id": "s_zzzzzzzzzz", "muted": true})
	if rpcErr == nil || rpcErr.Code != rpc.CodeNotFound {
		t.Fatalf("got %+v, want -32001", rpcErr)
	}
}

func TestSessionListDecoratedWithNeedsYou(t *testing.T) {
	e := newEnv(t)
	sess := e.createShell(t)
	id := sess.ID

	working := "editing main.go"
	e.hub.SetStatus(id, "working", &working)
	model, pct := "opus", 41.0
	e.hub.SetMeta(id, wire.SessionMeta{Model: &model, ContextPercent: &pct})
	got := e.list(t)[0]
	if got.Status != "working" || got.Activity == nil || *got.Activity != working {
		t.Fatalf("status %q activity %v, want working", got.Status, got.Activity)
	}
	if got.Meta.Model == nil || *got.Meta.Model != model || *got.Meta.ContextPercent != pct || got.Meta.CostUSD != nil {
		t.Fatalf("meta %+v, want the reported model and context", got.Meta)
	}

	approval := wire.Approval{
		RequestID: wire.NewRequestID(), SessionID: &id, Agent: "shell", Source: wire.SourceHook,
		Title: "Run tests?", CreatedAt: fixedNow, State: wire.StatePending,
	}
	e.hub.Publish(wire.EventApprovalOpen, &id, wire.ApprovalOpenEvent{Approval: approval})
	if s := e.list(t)[0].Status; s != "needs_you" {
		t.Fatalf("status with a pending approval %q, want needs_you", s)
	}

	e.hub.Publish(wire.EventApprovalClosed, &id, wire.ApprovalClosedEvent{RequestID: approval.RequestID, Resolution: wire.ResolutionDesk})
	if s := e.list(t)[0].Status; s != "working" {
		t.Fatalf("status after the approval closed %q, want working", s)
	}
	e.hub.SetStatus(id, "", nil)
	if s := e.list(t)[0].Status; s != "idle" {
		t.Fatalf("status after clearing %q, want the tmux value idle", s)
	}
}

func TestSubscribeSnapshotListsPendingApprovals(t *testing.T) {
	e := newEnv(t)
	sess := e.createShell(t)
	id := sess.ID
	approval := wire.Approval{
		RequestID: "r_abcdefghij", SessionID: &id, Agent: "shell", Source: wire.SourceHook,
		Title: "Run tests?", CreatedAt: fixedNow, State: wire.StatePending,
	}
	e.hub.Publish(wire.EventApprovalOpen, &id, wire.ApprovalOpenEvent{Approval: approval})

	c := e.dial(t)
	resp := c.call(wire.MethodEventsSubscribe, map[string]any{})
	var r struct {
		Seq       int64
		Sessions  []session.Session
		Approvals []wire.Approval
	}
	if err := json.Unmarshal(resp["result"], &r); err != nil {
		t.Fatal(err)
	}
	if r.Seq != 1 || len(r.Approvals) != 1 || r.Approvals[0].RequestID != approval.RequestID {
		t.Fatalf("snapshot %+v, want seq 1 and the open approval", r)
	}
	if len(r.Sessions) != 1 || r.Sessions[0].Status != "needs_you" {
		t.Fatalf("snapshot sessions %+v, want one needing the user", r.Sessions)
	}
}

func TestOnEventSeesEveryEventInOrder(t *testing.T) {
	e := newEnv(t)
	var seqs []int64
	e.hub.OnEvent(func(ev wire.Event) { seqs = append(seqs, ev.Seq) })
	notify(e.hub, 3)
	if !reflect.DeepEqual(seqs, []int64{1, 2, 3}) {
		t.Fatalf("consumer saw %v, want [1 2 3]", seqs)
	}
}

func TestFixtures(t *testing.T) {
	for _, name := range []string{"events.subscribe", "events.keepalive", "session.setMute"} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			raw, err := os.ReadFile("../../../protocol/v1/fixtures/" + name + ".json")
			if err != nil {
				t.Fatal(err)
			}
			raw = bytes.ReplaceAll(raw, []byte("<home>"), []byte(e.home))
			var fx struct {
				Setup    []json.RawMessage `json:"setup"`
				Request  json.RawMessage   `json:"request"`
				Response json.RawMessage   `json:"response"`
			}
			if err := json.Unmarshal(raw, &fx); err != nil {
				t.Fatal(err)
			}
			c := e.dial(t)
			var last map[string]json.RawMessage
			for _, req := range append(fx.Setup[1:], fx.Request) {
				var r struct {
					Method string          `json:"method"`
					Params json.RawMessage `json:"params"`
				}
				if err := json.Unmarshal(req, &r); err != nil {
					t.Fatal(err)
				}
				last = c.call(r.Method, r.Params)
			}
			gotRaw, err := json.Marshal(last)
			if err != nil {
				t.Fatal(err)
			}
			var got, want any
			_ = json.Unmarshal(gotRaw, &got)
			_ = json.Unmarshal(fx.Response, &want)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("response %s\nwant %s", gotRaw, fx.Response)
			}
		})
	}
}
