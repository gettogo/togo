package local

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/api"
	"github.com/gettogo/togo/helper/internal/wire"
)

const sid = "s_aaaaaaaaaa"

type pubEvent struct {
	kind    string
	payload any
}

type fakePub struct {
	mu     sync.Mutex
	events []pubEvent
}

func (p *fakePub) Publish(kind string, _ *string, payload any) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, pubEvent{kind, payload})
}

type fakeHub struct {
	status   string
	activity *string
	meta     wire.SessionMeta
}

func (h *fakeHub) SetStatus(_ string, status string, activity *string) {
	h.status, h.activity = status, activity
}
func (h *fakeHub) SetMeta(_ string, m wire.SessionMeta) { h.meta = m }

type fakeApprovals struct {
	mu      sync.Mutex
	pending []wire.Approval
	opened  []wire.HookEvent
	closed  []string
	expired []string
	ask     chan string
}

func (f *fakeApprovals) Pending(string) []wire.Approval { return f.pending }
func (f *fakeApprovals) OpenFromHook(_ context.Context, _ string, ev wire.HookEvent) wire.Approval {
	f.opened = append(f.opened, ev)
	return wire.Approval{}
}
func (f *fakeApprovals) OpenAsk(context.Context, wire.AskParams) (wire.Approval, <-chan string) {
	return wire.Approval{RequestID: "r_aaaaaaaaaa"}, f.ask
}
func (f *fakeApprovals) Expire(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.expired = append(f.expired, id)
}
func (f *fakeApprovals) CloseDesk(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = append(f.closed, id)
}

func setup() (*Handlers, *fakeHub, *fakeApprovals, *fakePub) {
	hub, ap, pub := &fakeHub{}, &fakeApprovals{ask: make(chan string, 1)}, &fakePub{}
	h := &Handlers{Hub: hub, Approvals: ap, Pub: pub,
		Clock:    func() time.Time { return time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC) },
		NewJobID: func() string { return "j_aaaaaaaaaa" }}
	return h, hub, ap, pub
}

func call(t *testing.T, h *Handlers, method string, params any) (any, error) {
	t.Helper()
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	return h.Methods()[method](context.Background(), raw)
}

func hookEv(event string, data wire.HookData) wire.HookEvent {
	id := sid
	return wire.HookEvent{Agent: "claude", Event: event, SessionID: &id, Cwd: "/tmp", Data: data}
}

func TestHookEventsDriveHubAndApprovals(t *testing.T) {
	h, hub, ap, pub := setup()
	if _, err := call(t, h, wire.MethodLocalHook, hookEv(EventTool, wire.HookData{Tool: "Bash"})); err != nil {
		t.Fatal(err)
	}
	if hub.status != "working" || hub.activity == nil || *hub.activity != "Bash" {
		t.Fatalf("status %q activity %v", hub.status, hub.activity)
	}
	if _, err := call(t, h, wire.MethodLocalHook, hookEv(EventPermission, wire.HookData{Tool: "Bash"})); err != nil {
		t.Fatal(err)
	}
	if len(ap.opened) != 1 {
		t.Fatalf("opened %d approvals", len(ap.opened))
	}
	ap.pending = []wire.Approval{{RequestID: "r_bbbbbbbbbb", Source: wire.SourceHook}, {RequestID: "r_cccccccccc", Source: wire.SourceAsk}}
	if _, err := call(t, h, wire.MethodLocalHook, hookEv(EventToolDone, wire.HookData{})); err != nil {
		t.Fatal(err)
	}
	if len(ap.closed) != 1 || ap.closed[0] != "r_bbbbbbbbbb" {
		t.Fatalf("closed %v, want only the hook approval", ap.closed)
	}
	if _, err := call(t, h, wire.MethodLocalHook, hookEv(EventStop, wire.HookData{Message: "all\x1b[31m done"})); err != nil {
		t.Fatal(err)
	}
	last := pub.events[len(pub.events)-1].payload.(wire.NotifyEvent)
	if hub.status != "idle" || last.Level != wire.LevelDone || last.Body != "all[31m done" {
		t.Fatalf("status %q notify %+v", hub.status, last)
	}
}

func TestHookMetaMerges(t *testing.T) {
	h, hub, _, _ := setup()
	pct, cost := 42.5, 1.25
	if _, err := call(t, h, wire.MethodLocalHook, hookEv(EventStart, wire.HookData{Model: "claude-opus-5"})); err != nil {
		t.Fatal(err)
	}
	if _, err := call(t, h, wire.MethodLocalHook, hookEv(EventMeta, wire.HookData{ContextPercent: &pct, CostUSD: &cost})); err != nil {
		t.Fatal(err)
	}
	m := hub.meta
	if m.Model == nil || *m.Model != "claude-opus-5" || *m.ContextPercent != 42.5 || *m.CostUSD != 1.25 {
		t.Fatalf("meta %+v", m)
	}
}

func TestHookWithoutSessionIsNoOp(t *testing.T) {
	h, hub, ap, pub := setup()
	ev := hookEv(EventPermission, wire.HookData{Tool: "Bash"})
	ev.SessionID = nil
	if _, err := call(t, h, wire.MethodLocalHook, ev); err != nil {
		t.Fatal(err)
	}
	if hub.status != "" || len(ap.opened) != 0 || len(pub.events) != 0 {
		t.Fatal("hook without a session changed state")
	}
}

func TestHookRejectsBadInput(t *testing.T) {
	h, _, _, _ := setup()
	bad := []wire.HookEvent{hookEv("nope", wire.HookData{}), {Agent: "x", Event: EventStop}}
	unmanaged := "work"
	ev := hookEv(EventStop, wire.HookData{})
	ev.SessionID = &unmanaged
	bad = append(bad, ev)
	for _, b := range bad {
		if _, err := call(t, h, wire.MethodLocalHook, b); err == nil {
			t.Fatalf("accepted %+v", b)
		}
	}
}

func TestJobLifecyclePublishesEvents(t *testing.T) {
	h, _, _, pub := setup()
	res, err := call(t, h, wire.MethodLocalJob, wire.JobParams{Op: wire.JobStart, Name: "build"})
	if err != nil || res.(wire.JobResult).JobID != "j_aaaaaaaaaa" {
		t.Fatalf("start = %v, %v", res, err)
	}
	p := 50
	if _, err := call(t, h, wire.MethodLocalJob, wire.JobParams{Op: wire.JobUpdate, JobID: "j_aaaaaaaaaa", Progress: &p}); err != nil {
		t.Fatal(err)
	}
	if _, err := call(t, h, wire.MethodLocalJob, wire.JobParams{Op: wire.JobEnd, JobID: "j_aaaaaaaaaa", Status: wire.JobOK}); err != nil {
		t.Fatal(err)
	}
	if len(pub.events) != 3 {
		t.Fatalf("published %d events", len(pub.events))
	}
	end := pub.events[2].payload.(wire.JobEvent).Job
	if end.Status != wire.JobOK || end.EndedAt == nil || *end.Progress != 50 || pub.events[0].kind != wire.EventJob {
		t.Fatalf("end job %+v", end)
	}
	_, err = call(t, h, wire.MethodLocalJob, wire.JobParams{Op: wire.JobUpdate, JobID: "j_aaaaaaaaaa"})
	var ce *api.CodeError
	if !errors.As(err, &ce) || ce.Code != -32001 {
		t.Fatalf("update after end = %v, want -32001", err)
	}
	over := 101
	if _, err := call(t, h, wire.MethodLocalJob, wire.JobParams{Op: wire.JobStart, Name: "x", Progress: &over}); err == nil {
		t.Fatal("progress 101 accepted")
	}
}

func TestAskAnswerAndTimeout(t *testing.T) {
	h, _, ap, _ := setup()
	ap.ask <- "Yes"
	res, err := call(t, h, wire.MethodLocalAsk, wire.AskParams{Title: "Ship?", Options: []string{"Yes", "No"}, TimeoutSec: 5})
	if err != nil {
		t.Fatal(err)
	}
	if r := res.(wire.AskResult); r.Reason != wire.AskAnswered || *r.Decision != "Yes" {
		t.Fatalf("result %+v", r)
	}
	res, err = call(t, h, wire.MethodLocalAsk, wire.AskParams{Title: "Ship?", TimeoutSec: 1})
	if err != nil {
		t.Fatal(err)
	}
	if r := res.(wire.AskResult); r.Reason != wire.AskTimeout || r.Decision != nil || len(ap.expired) != 1 {
		t.Fatalf("result %+v expired %v", r, ap.expired)
	}
	if _, err := call(t, h, wire.MethodLocalAsk, wire.AskParams{Title: "x", TimeoutSec: 0}); err == nil {
		t.Fatal("timeout 0 accepted")
	}
}

func TestNotifyReach(t *testing.T) {
	h, _, _, pub := setup()
	h.Reach = func(*string) bool { return false }
	res, err := call(t, h, wire.MethodLocalNotify, wire.NotifyParams{Title: "hi", Body: "there"})
	if err != nil {
		t.Fatal(err)
	}
	if res.(wire.NotifyResult).Delivered || len(pub.events) != 1 {
		t.Fatalf("result %+v events %d", res, len(pub.events))
	}
}

func TestAskUnreachableTimesOutAtOnce(t *testing.T) {
	h, _, ap, _ := setup()
	h.Reach = func(*string) bool { return false }
	ap.ask <- "Yes"
	res, err := call(t, h, wire.MethodLocalAsk, wire.AskParams{Title: "Ship?", Options: []string{"Yes"}, TimeoutSec: 60})
	if err != nil {
		t.Fatal(err)
	}
	if r := res.(wire.AskResult); r.Reason != wire.AskTimeout || r.Decision != nil {
		t.Fatalf("result %+v, want an immediate timeout", r)
	}
}
