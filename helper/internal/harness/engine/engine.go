// Package engine is the harness Engine: it matches each managed session to
// an adapter file, runs the adapter's feeds through Go mappers into the
// session's item log and mission snapshot, serves screens from the
// adapter's sources and applies its actions.
package engine

import (
	"context"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/gettogo/togo/helper/internal/gitx"
	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/redact"
	"github.com/gettogo/togo/helper/internal/harness/source"
	"github.com/gettogo/togo/helper/internal/ports"
	"github.com/gettogo/togo/helper/internal/session"
	"github.com/gettogo/togo/helper/internal/wire"
)

// Sessions looks sessions up.
type Sessions interface {
	Resolve(ctx context.Context, id string) (session.Session, error)
}

// Approvals is the approvals store the engine reads and answers.
type Approvals interface {
	Pending(sessionID string) []wire.Approval
	Answer(ctx context.Context, requestID, raw, by string) error
}

// ScreenReader is implemented by mappers that can read state (such as the
// permission mode) from the pane's visible text. The engine uses it to
// verify each key press of a mode cycle.
type ScreenReader interface {
	ReadScreen(text string) map[string]string
}

// Config holds the engine's collaborators. Nil fields turn the features
// that need them off.
type Config struct {
	Sessions  Sessions
	Typist    harness.Typist
	Marker    harness.Marker
	Redactor  harness.Redactor
	Log       *Log
	Approvals Approvals
	// Screen returns the pane's visible text.
	Screen func(ctx context.Context, id string) (string, error)
	// NeedsYou tells the phone a session needs the user at the terminal:
	// the typing guard found a dialog it will not type into.
	NeedsYou func(id, title, body string)
	// Done tells the phone a turn finished, with the reply's text, for
	// adapters with no hook to say so: without it the pane watcher's
	// "Done" is all the phone gets, with no body. Nil sends nothing.
	Done func(id, title, body string)
	// Status is the hub's status for a session (working, idle, ...).
	Status func(id string) string
	// Exit returns how the session's agent exited, nil when it runs or it
	// is not known. It names the exit status in the chat's ended notice.
	Exit func(id string) *wire.SessionExit
	// Relaunch restarts the agent with argv in its pane.
	Relaunch func(ctx context.Context, id string, argv []string) error
	Git      func(ctx context.Context, dir string) (gitx.Status, error)
	Ports    func(ctx context.Context, id string) ([]ports.Listener, error)
	// PanePIDs lists the pane processes of a session's tmux session; with
	// it a source can tell which files the session's own agent holds.
	PanePIDs func(ctx context.Context, id string) ([]int, error)
	// PaneTTY is the terminal device of a session's pane (/dev/ttys011);
	// adapters whose agent records its session per terminal read it as
	// the "tty" variable.
	PaneTTY func(ctx context.Context, id string) (string, error)
	// LoginPath is the PATH programs are looked up in.
	LoginPath func(ctx context.Context) string
	// LoginEnv is the login shell's environment; nil keeps the helper's.
	LoginEnv func(ctx context.Context) []string
	// Home is the user's home folder; AdapterDir holds user overrides;
	// TogoHome holds the sockets picked for harness endpoints.
	Home       string
	AdapterDir string
	TogoHome   string
	Now        func() time.Time
	NewID      func() string
	// OnSummary is told when a session's mission summary changed,
	// at most once a second per session.
	OnSummary func(id string)
	Logf      func(format string, args ...any)
	// Scale multiplies verify windows and idle waits; tests shrink it.
	Scale float64
}

// Engine implements harness.Engine.
type Engine struct {
	cfg      Config
	adapters map[string]*adapter
	gate     gate
	log      *Log
	red      harness.Redactor
	cache    cache
	eps      endpoints
	srv      servers

	mu       sync.Mutex
	sessions map[string]*sess
	// carried holds a restarted session's subscribers until it is served
	// again (carry), so open chats keep receiving across the restart.
	carried map[string][]harness.Subscriber
	idSeq   int64
	// act runs an action for a permission answered from a notification;
	// nil means Act. Tests replace it.
	act func(context.Context, harness.ActionRequest) (harness.ActionResult, error)
}

var _ harness.Engine = (*Engine)(nil)

// New loads the adapter files and returns an engine.
func New(cfg Config) *Engine {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Logf == nil {
		cfg.Logf = func(string, ...any) {}
	}
	if cfg.Scale <= 0 {
		cfg.Scale = 1
	}
	e := &Engine{cfg: cfg, sessions: map[string]*sess{}, log: cfg.Log, red: cfg.Redactor}
	if e.log == nil {
		e.log = &Log{}
	}
	if e.red == nil {
		e.red = redact.Redactor{}
	}
	e.adapters = loadAdapters(cfg.AdapterDir, cfg.Logf)
	return e
}

func (e *Engine) scaled(d time.Duration) time.Duration {
	return time.Duration(float64(d) * e.cfg.Scale)
}

func (e *Engine) newID() string {
	if e.cfg.NewID != nil {
		return e.cfg.NewID()
	}
	e.mu.Lock()
	e.idSeq++
	n := e.idSeq
	e.mu.Unlock()
	return "p" + randomEpoch() + itoa(n)
}

// session returns the tracked session, starting it on first use. A
// session that does not exist is session.ErrNotFound; one no adapter
// serves is returned with a nil adapter.
func (e *Engine) session(ctx context.Context, id string) (*sess, error) {
	e.mu.Lock()
	s, ok := e.sessions[id]
	e.mu.Unlock()
	if !ok {
		if e.cfg.Sessions == nil {
			return nil, session.ErrNotFound
		}
		info, err := e.cfg.Sessions.Resolve(ctx, id)
		if err != nil {
			return nil, err
		}
		s = e.track(info)
	}
	s.begin()
	return s, nil
}

// track records a session seen in a listing or event. Its adapter starts
// (version check, feeds) on first use or on its agent's first hook.
func (e *Engine) track(info session.Session) *sess {
	e.mu.Lock()
	s, ok := e.sessions[info.ID]
	e.mu.Unlock()
	if ok {
		return s
	}
	// Built outside the lock: the login PATH lookup can take a while.
	fresh := newSess(e, info)
	e.mu.Lock()
	if s, ok := e.sessions[info.ID]; ok {
		e.mu.Unlock()
		fresh.cancel()
		return s
	}
	s = fresh
	e.sessions[info.ID] = s
	subs := e.carried[info.ID]
	delete(e.carried, info.ID)
	e.mu.Unlock()
	s.carry(subs)
	return s
}

// Forget stops serving a session and drops its state.
func (e *Engine) Forget(id string) {
	e.mu.Lock()
	s, ok := e.sessions[id]
	delete(e.sessions, id)
	e.mu.Unlock()
	if ok {
		s.stop()
	}
	e.log.Forget(id)
	e.cache.forget(id)
	e.dropRelaunch(id)
	source.Hooks.Forget(id)
	e.stopServer(id)
	e.eps.mu.Lock()
	ep, hadEP := e.eps.byID[id]
	delete(e.eps.byID, id)
	e.eps.mu.Unlock()
	if hadEP {
		if r, ok := harness.LookupRunner("rpc"); ok {
			if f, ok := r.(interface{ Forget(endpoint string) }); ok {
				f.Forget(ep.url())
			}
		}
		if ep.socket != "" {
			_ = os.Remove(ep.socket)
		}
	}
	typing.Delete(id)
	draining.Delete(id)
}

// Observe takes hub events: sessions start and stop being served, and
// approvals become needs rows and approval items. It never blocks.
func (e *Engine) Observe(ev wire.Event) {
	if ev.SessionID == nil {
		if ev.Kind == wire.EventApprovalClosed {
			go e.approvalClosed(ev)
		}
		return
	}
	id := *ev.SessionID
	switch ev.Kind {
	case wire.EventSession:
		if p, ok := ev.Payload.(wire.SessionEvent); ok {
			if info, ok := p.Session.(session.Session); ok && info.Managed {
				go e.track(info).statusChanged()
			}
		}
	case wire.EventSessionRemoved:
		go e.Forget(id)
	case wire.EventApprovalOpen:
		go e.approvalOpened(id, ev)
	case wire.EventApprovalClosed:
		go e.approvalClosed(ev)
	}
}

// Delivered is called when a hook payload for a session arrived, so the
// session's feeds run even before the app asks.
func (e *Engine) Delivered(id string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _ = e.session(ctx, id)
}

// Describe reports the session's adapter and capabilities.
func (e *Engine) Describe(ctx context.Context, id string) (harness.Descriptor, error) {
	s, err := e.session(ctx, id)
	if err != nil {
		return harness.Descriptor{}, err
	}
	if s.ad == nil {
		return harness.Nop{}.Describe(ctx, id)
	}
	s.waitReady(ctx)
	s.mu.Lock()
	defer s.mu.Unlock()
	a := s.ad
	d := harness.Descriptor{
		Adapter:      &harness.AdapterInfo{ID: a.Adapter.Adapter, Name: a.Name, HarnessVersion: s.version, Revision: a.Revision},
		Capabilities: slices.Clone(a.Capabilities),
		Screens:      []harness.ScreenRef{},
		Modes:        []harness.Choice{},
		Degraded:     s.degradedList(),
	}
	if d.Capabilities == nil {
		d.Capabilities = []string{}
	}
	if e.cfg.Approvals != nil && s.gateOK && !harness.Has(d.Capabilities, harness.CapNeedsApprovals) {
		// The screen detector serves every agent: the chat shows its
		// approvals whatever the adapter declares.
		d.Capabilities = append(d.Capabilities, harness.CapNeedsApprovals)
	}
	if im := a.Images; im != nil && im.Insert != "" {
		d.Images = &harness.ImagePolicy{Insert: im.Insert}
	}
	for _, name := range a.screenOrder {
		if !s.gateOK {
			break
		}
		sc := a.Screens[name]
		if sc.Capability != "" && (!harness.Has(a.Capabilities, sc.Capability) || s.degraded[sc.Capability]) {
			continue
		}
		if !s.screenListableLocked(sc) {
			continue
		}
		d.Screens = append(d.Screens, harness.ScreenRef{ID: name, Primitive: sc.Primitive, Title: sc.Title, Section: sc.Section})
	}
	if m := a.Modes; m != nil && !s.degraded[harness.CapMode] {
		for _, c := range m.Choices {
			d.Modes = append(d.Modes, harness.Choice{ID: c.ID, Label: c.Label, Detail: c.Detail})
		}
		d.Mode = s.currentModeLocked()
	}
	return d, nil
}

// Open returns the items after the cursor and the mission snapshot, and
// subscribes sub to later changes.
func (e *Engine) Open(ctx context.Context, sub harness.Subscriber, id string, after int64, limit int) (harness.OpenResult, error) {
	s, err := e.served(ctx, id)
	if err != nil {
		return harness.OpenResult{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	items, cursor, more := e.log.Since(id, after, limit)
	if fit, cut := newestFitting(items, pageBytes); cut {
		items, more = fit, true
	}
	res := harness.OpenResult{Epoch: e.log.Epoch(id), Cursor: cursor, Items: items, More: more, Mission: s.mission.snapshot()}
	if res.Items == nil {
		res.Items = []harness.Item{}
	}
	if old, ok := s.subs[sub]; ok {
		old.close()
	}
	s.subs[sub] = newSubQueue(sub, id, func() string { return e.log.Epoch(id) }, e.cfg.Now, func() { s.dropSub(sub) })
	return res, nil
}

// Release starts notifications for a subscriber once its open response
// is on its way.
func (e *Engine) Release(sub harness.Subscriber, id string) {
	e.mu.Lock()
	s, ok := e.sessions[id]
	e.mu.Unlock()
	if !ok {
		return
	}
	s.mu.Lock()
	q := s.subs[sub]
	s.mu.Unlock()
	if q != nil {
		q.release()
	}
}

// served returns a session an adapter serves and whose version gate
// passed.
func (e *Engine) served(ctx context.Context, id string) (*sess, error) {
	s, err := e.session(ctx, id)
	if err != nil {
		return nil, err
	}
	if s.ad == nil {
		return nil, harness.ErrNoAdapter
	}
	s.waitReady(ctx)
	if s.gateFailed() {
		return nil, &harness.FallbackError{Reason: "version", Detail: s.gateReason()}
	}
	return s, nil
}

// Close stops notifications for the session on sub.
func (e *Engine) Close(_ context.Context, sub harness.Subscriber, id string) error {
	e.mu.Lock()
	s, ok := e.sessions[id]
	e.mu.Unlock()
	if ok {
		s.dropSub(sub)
	}
	return nil
}

// History pages older items.
func (e *Engine) History(ctx context.Context, id string, before int64, limit int) (harness.Page, error) {
	if _, err := e.served(ctx, id); err != nil {
		return harness.Page{}, err
	}
	items, more := e.log.Before(id, before, limit)
	if fit, cut := newestFitting(items, pageBytes); cut {
		items, more = fit, true
	}
	if items == nil {
		items = []harness.Item{}
	}
	return harness.Page{Items: items, More: more}, nil
}

// Locate resolves an anchor against the pane.
func (e *Engine) Locate(ctx context.Context, id string, a harness.Anchor) (harness.Location, error) {
	if _, err := e.served(ctx, id); err != nil {
		return harness.Location{}, err
	}
	if e.cfg.Marker == nil {
		return harness.Location{Found: false}, nil
	}
	return e.cfg.Marker.Find(ctx, id, a)
}

// Summary is the session's pill summary, for Session.mission.
func (e *Engine) Summary(id string) (harness.Summary, bool) {
	e.mu.Lock()
	s, ok := e.sessions[id]
	e.mu.Unlock()
	if !ok || s.ad == nil {
		return harness.Summary{}, false
	}
	sum := s.sum.Load()
	if sum == nil {
		return harness.Summary{}, false
	}
	out := *sum
	if out.Plan != nil {
		p := *out.Plan
		out.Plan = &p
	}
	return out, true
}

// TurnRunning reports whether a session's feeds see its agent's turn still
// running, for a session fed by the agent's own server or stream, not by
// hooks. Unlike Summary it leaves out the hub's status, which follows the
// pane: the pane watcher asks before it calls a silent pane a finished
// turn. A hook-fed turn can miss its Stop, and the pane going idle is what
// ends it (checkStuckTurnLocked), so those are never vouched for. A feed
// can still be wrong for good (a dropped stream, a session file that never
// records a turn's end): the watcher takes this word for a bounded time
// only (watch.maxVouched), then the pane decides.
func (e *Engine) TurnRunning(id string) bool {
	e.mu.Lock()
	s, ok := e.sessions[id]
	e.mu.Unlock()
	if !ok || s.ad == nil || s.hookFed() {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failedHold || agentGone(s.hubStatus()) {
		return false
	}
	working, known := s.turnLocked()
	return known && working
}

// NeedsYou reports whether a question or an approval of the session waits
// for the user: its silence is no finished turn, whatever the feed says
// (OpenCode 2.2.5 reported its session idle while a permission waited, and
// the phone heard "done").
func (e *Engine) NeedsYou(id string) bool {
	e.mu.Lock()
	s, ok := e.sessions[id]
	e.mu.Unlock()
	if !ok || s.ad == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.needsOpenLocked()
}

func itoa(n int64) string {
	const digits = "0123456789"
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{digits[n%10]}, b...)
		n /= 10
	}
	return string(b)
}
