package enginetest

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sync"
	"time"

	"github.com/gettogo/togo/helper/internal/api"
	"github.com/gettogo/togo/helper/internal/approval"
	"github.com/gettogo/togo/helper/internal/detect"
	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/source"
	"github.com/gettogo/togo/helper/internal/local"
	"github.com/gettogo/togo/helper/internal/session"
	"github.com/gettogo/togo/helper/internal/wire"
)

// The rig's phone side is wired as the daemon wires it (daemon/compose.go
// Compose, daemon/harness.go newHarness): the real approvals store and
// local hook handlers, publishing to the engine's Observe, with a fake hub
// for session status and the rig's Terminal as the pane. What the phone
// sees is what reaches the publisher (Events) and the engine.

// Event is one event the helper published, as the phone would get it.
type Event struct {
	Kind    string
	Payload any
}

// publisher stands in for the hub: it records each event and hands it to
// the engine, as c.Hub.OnEvent(eng.Observe) does.
type publisher struct{ r *Rig }

func (p publisher) Publish(kind string, id *string, payload any) {
	p.r.note(Event{Kind: kind, Payload: payload})
	if e := p.r.engine(); e != nil {
		e.Observe(wire.Event{Kind: kind, SessionID: id, Payload: payload})
	}
}

// hub is the part of the hub the hook handlers drive (local.Hub).
type hub struct{ r *Rig }

func (h hub) SetStatus(_ string, status string, _ *string) { h.r.SetHubStatus(status) }
func (h hub) SetMeta(string, wire.SessionMeta)             {}
func (h hub) SetExit(_ string, status string, _ *wire.SessionExit) {
	h.r.SetHubStatus(status)
}
func (h hub) ClearExit(string)            {}
func (h hub) HookExitAllowed(string) bool { return true }

// SetHubStatus sets the session's status and tells the engine, as the hub's
// session event does.
func (r *Rig) SetHubStatus(status string) {
	r.SetStatus(status)
	id := r.ID
	if e := r.engine(); e != nil {
		e.Observe(wire.Event{Kind: wire.EventSession, SessionID: &id, Payload: wire.SessionEvent{
			Session: session.Session{ID: id, Agent: r.Opt.Agent, Managed: true, Status: status}}})
	}
}

func (r *Rig) note(ev Event) {
	r.mu.Lock()
	r.events = append(r.events, ev)
	r.mu.Unlock()
}

func (r *Rig) engine() interface{ Observe(wire.Event) } {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Engine == nil {
		return nil
	}
	return r.Engine
}

// Events lists what the helper published so far: approval.open and
// approval.closed, and the notifications (needs you, done).
func (r *Rig) Events() []Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Event(nil), r.events...)
}

// Closed lists the resolutions of the approvals closed so far, by request.
func (r *Rig) Closed() map[string]string {
	out := map[string]string{}
	for _, ev := range r.Events() {
		if c, ok := ev.Payload.(wire.ApprovalClosedEvent); ok {
			out[c.RequestID] = c.Resolution
		}
	}
	return out
}

// Notified lists the notifications the phone was sent at this level
// (wire.LevelNeedsYou, wire.LevelDone).
func (r *Rig) Notified(level string) []wire.NotifyEvent {
	var out []wire.NotifyEvent
	for _, ev := range r.Events() {
		if n, ok := ev.Payload.(wire.NotifyEvent); ok && n.Level == level {
			out = append(out, n)
		}
	}
	return out
}

// startPhone builds the approvals store and hook handlers. A helper
// restart builds new ones; the store opens again the approvals it kept
// under TOGO_HOME/run, as the daemon's does.
func (r *Rig) startPhone() {
	ttl := r.Opt.ApprovalTTL
	store := approval.New(approval.Config{Tmux: paneTmux{r}, Pub: publisher{r}, TTL: ttl,
		Path: filepath.Join(r.TogoHome, "run", "approvals.json")})
	r.mu.Lock()
	r.Approvals = store
	r.mu.Unlock()
	r.T.Cleanup(r.Approvals.Flush)
	r.Local = &local.Handlers{Hub: hub{r}, Approvals: r.Approvals, Pub: publisher{r},
		Harness: func(id string, f wire.HookForward) {
			if len(f.Payload) > 0 {
				source.Hooks.Deliver(id, source.Payload{Kind: source.BusHook, Event: f.Hook, Data: f.Payload})
			}
		}}
	r.methods = map[string]api.Method{}
	for _, m := range []map[string]api.Method{r.Approvals.Methods(), r.Local.Methods(), harness.Methods(r.Engine)} {
		for k, v := range m {
			r.methods[k] = v
		}
	}
	r.prompt = ""
}

// Call runs a protocol method as the app (or a hook process, for
// local.*) calls it, through the same method table the daemon serves,
// and returns its JSON result.
func (r *Rig) Call(method string, params any) (json.RawMessage, error) {
	r.mu.Lock()
	m, ok := r.methods[method]
	r.mu.Unlock()
	if !ok {
		r.T.Fatalf("no method %s", method)
	}
	raw, err := json.Marshal(params)
	if err != nil {
		r.T.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	res, err := m(ctx, raw)
	if err != nil {
		return nil, err
	}
	out, _ := json.Marshal(res)
	return out, nil
}

// Hook runs local.hook with what `togo hook <agent> <event>` sends: the
// summary the helper acts on and the payload for the harness feeds.
func (r *Rig) Hook(f wire.HookForward) error {
	id := r.ID
	f.SessionID = &id
	_, err := r.Call(wire.MethodLocalHook, f)
	return err
}

// Look does what the pane watcher does when it reads the screen
// (watch/watch.go inspect, promptSeen, clearPrompt): a prompt the
// detector recognises is reported once, and its leaving is reported. A
// prompt the phone itself opened is not, nor a step of a question dialog
// the chat already shows (daemon/compose.go ownScreens).
func (r *Rig) Look() {
	// One look at a time: two that read the old prompt would both report
	// what they saw.
	r.look.Lock()
	defer r.look.Unlock()
	ctx := context.Background()
	p, ok := detect.Detect(r.Term.Text())
	r.mu.Lock()
	prev := r.prompt
	r.mu.Unlock()
	switch {
	case ok && p.Fingerprint != prev:
		r.mu.Lock()
		r.prompt = p.Fingerprint
		r.mu.Unlock()
		switch {
		case r.Engine.PhoneDriving(r.ID):
		case r.Engine.QuestionDialog(r.ID, p):
			// A step of a question dialog whose question item the phone
			// already shows; a request opened for an earlier step goes.
			r.Approvals.CloseScreenPrompts(ctx, r.ID)
		default:
			r.Approvals.PromptSeen(ctx, r.ID, p)
		}
	case !ok && prev != "":
		r.mu.Lock()
		r.prompt = ""
		r.mu.Unlock()
		r.Approvals.PromptGone(ctx, r.ID)
	}
}

// Pending lists the session's open approvals, as approval.list gives them.
func (r *Rig) Pending() []wire.Approval { return r.Approvals.Pending(r.ID) }

// Answer answers an approval from the phone (approval.answer).
func (r *Rig) Answer(requestID, decision string) error {
	_, err := r.Call(wire.MethodApprovalAnswer, map[string]string{"requestId": requestID, "decision": decision})
	return err
}

// WaitPending waits until the session has n open approvals.
func (r *Rig) WaitPending(n int, limit time.Duration) []wire.Approval {
	r.T.Helper()
	limit = max(limit, MinSettleLimit)
	deadline := time.Now().Add(limit)
	for {
		p := r.Pending()
		if len(p) == n {
			return p
		}
		if time.Now().After(deadline) {
			r.T.Fatalf("%d open approvals, want %d", len(p), n)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// phoneState guards the rig's phone-side fields.
type phoneState struct {
	Approvals *approval.Store
	Local     *local.Handlers
	methods   map[string]api.Method
	events    []Event
	prompt    string
	look      sync.Mutex
}
