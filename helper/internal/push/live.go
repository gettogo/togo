package push

import (
	"cmp"
	"github.com/gettogo/togo/helper/internal/harness/redact"
	"sync"
	"time"
	"unicode/utf16"

	"github.com/gettogo/togo/helper/internal/detect"
	"github.com/gettogo/togo/helper/internal/wire"
)

// Live Activity events and timings.
const (
	liveStart  = "start"
	liveUpdate = "update"
	liveEnd    = "end"

	// liveCoalesce is the shortest gap between two updates to one device,
	// except that changes into or out of needs_you go at once.
	liveCoalesce = 2 * time.Second
	// liveStepCoalesce is the shortest gap between two updates that only
	// change a working session's step, which can change every second.
	liveStepCoalesce = 10 * time.Second
	// liveQuiet is how long nothing may be waiting or working before the
	// activity ends.
	liveQuiet   = 10 * time.Minute
	liveDismiss = 900  // seconds an ended activity stays on the lock screen
	liveStale   = 3600 // seconds after each push before iOS marks it stale
	liveTick    = time.Second

	maxLiveCommand = 160

	// agentShell is the agent id for a session or request whose agent is
	// unknown; the relay requires a non-empty agent.
	agentShell = "shell"
)

// LiveState is the Live Activity content state, one per machine. Since is
// Unix seconds. RequestID is the focused request while one is waiting.
// Command is the focused request's subject, cleaned; it leaves the host
// only for devices that opted in, and is null for every other device.
// Reason is the exit reason word of an ended or failed session and is
// left out otherwise; it never carries exit detail. Ctx is the context
// (see LiveContext) sealed for the one device a push goes to; the tracker
// keeps it in plain form and the sender seals it per device.
type LiveState struct {
	Bib       *string `json:"bib"`
	Agent     string  `json:"agent"`
	Status    string  `json:"status"`
	Waiting   int     `json:"waiting"`
	Since     int64   `json:"since"`
	RequestID *string `json:"requestId"`
	Command   *string `json:"command"`
	Reason    string  `json:"reason,omitempty"`
	Ctx       *string `json:"ctx,omitempty"`

	context LiveContext
}

func (a LiveState) equal(b LiveState) bool {
	return eqPtr(a.Bib, b.Bib) && a.Agent == b.Agent && a.Status == b.Status && a.Waiting == b.Waiting &&
		a.Since == b.Since && eqPtr(a.RequestID, b.RequestID) && eqPtr(a.Command, b.Command) &&
		a.Reason == b.Reason && a.context.equal(b.context)
}

// onlyStep reports whether a and b differ in the step and nothing else.
func onlyStep(a, b LiveState) bool {
	if a.context.Step == b.context.Step {
		return false
	}
	a.context.Step, b.context.Step = "", ""
	a.Ctx, b.Ctx = nil, nil
	return a.equal(b)
}

func eqPtr(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

type liveRequest struct {
	created time.Time
	bib     *string
	agent   string
	command *string
	session string // id of the session asking; "" when none
	context LiveContext
}

type liveSession struct {
	bib   *string
	agent string
	since time.Time // when it started working
}

// liveNames is what the tracker knows about a session beyond its status.
type liveNames struct {
	title string
	step  string
}

// LiveTracker owns the aggregate state of this machine's one Live Activity.
// The Sender feeds it approvals opening and closing and sessions changing
// status, then asks Next what, if anything, to push. It is safe for
// concurrent use.
//
// The focus is the oldest pending request's session, else the session
// that most recently started working, else the last focus, shown as done,
// or as ended or failed when that session's agent exited.
type LiveTracker struct {
	mu       sync.Mutex
	reqs     map[string]liveRequest
	working  map[string]liveSession
	exited   map[string]string // session id -> ended or failed
	names    map[string]liveNames
	reasons  map[string]string // session id -> exit reason word
	lastID   string            // session of the last working focus
	last     LiveState         // last computed state, the source of done
	hasLast  bool
	quietAt  time.Time // when nothing was left waiting or working
	rose     bool      // waiting went from 0 to more since the last Next
	pausedAt time.Time // when requests started waiting on the user; working clocks pause
	active   bool      // an update or start went out since the last end
	sent     LiveState
	hasSent  bool
}

func (t *LiveTracker) lazy() {
	if t.reqs == nil {
		t.reqs = map[string]liveRequest{}
		t.working = map[string]liveSession{}
		t.exited = map[string]string{}
		t.reasons = map[string]string{}
		t.names = map[string]liveNames{}
	}
}

// Open records a pending request. subject may be nil.
func (t *LiveTracker) Open(rid string, created time.Time, bib *string, agent string, subject *wire.Subject, now time.Time) {
	t.OpenRequest(rid, created, bib, agent, subject, "", LiveContext{}, now)
}

// OpenRequest records a pending request of a session ("" for none) with
// the context its Live Activity shows. subject may be nil.
func (t *LiveTracker) OpenRequest(rid string, created time.Time, bib *string, agent string, subject *wire.Subject,
	session string, c LiveContext, now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.lazy()
	if created.IsZero() {
		created = now
	}
	r := liveRequest{created: created, bib: nonEmpty(bib), agent: cmp.Or(agent, agentShell), session: session, context: c}
	if subject != nil && subject.Kind == wire.SubjectCommand {
		if c := truncateUTF16(detect.CleanLine(redact.String(subject.Text)), maxLiveCommand); c != "" {
			r.command = &c
		}
	}
	if len(t.reqs) == 0 {
		t.rose = true
		t.pausedAt = now
	}
	t.reqs[rid] = r
	t.settle(now)
}

// Close forgets a request.
func (t *LiveTracker) Close(rid string, now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.lazy()
	_, open := t.reqs[rid]
	delete(t.reqs, rid)
	if open && len(t.reqs) == 0 {
		t.resume(now)
	}
	t.settle(now)
}

// resume restarts the working clocks paused while requests waited on the
// user, so the time shown counts working time only, not the wait.
func (t *LiveTracker) resume(now time.Time) {
	if t.pausedAt.IsZero() {
		return
	}
	if wait := now.Sub(t.pausedAt); wait > 0 {
		for id, s := range t.working {
			if s.since.Before(t.pausedAt) {
				s.since = s.since.Add(wait)
				t.working[id] = s
			}
		}
	}
	t.pausedAt = time.Time{}
}

// Describe records a session's title and current step, shown with its
// requests and while it works.
func (t *LiveTracker) Describe(id, title, step string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.lazy()
	if id == "" {
		return
	}
	t.names[id] = liveNames{title: title, step: step}
}

// Session records a session's status. needs_you is ignored, since pending
// requests already say so; working starts the session's clock once; ended
// and failed are kept for the done slot along with the exit reason word;
// any other status means it stopped.
func (t *LiveTracker) Session(id, status string, bib *string, agent, reason string, now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.lazy()
	switch {
	case status == wire.LevelNeedsYou:
		return
	case status == wire.StatusEnded || status == wire.StatusFailed:
		delete(t.working, id)
		t.exited[id] = status
		t.reasons[id] = reason
		if t.lastID == "" {
			t.lastID = id
		}
	case status == wire.LiveWorking:
		delete(t.exited, id)
		delete(t.reasons, id)
		s, ok := t.working[id]
		if !ok {
			s.since = now
		}
		s.bib, s.agent = nonEmpty(bib), cmp.Or(agent, agentShell)
		t.working[id] = s
	default:
		delete(t.working, id)
		delete(t.exited, id)
		delete(t.reasons, id)
	}
	t.settle(now)
}

// Remove forgets a session that is gone or muted, with its pending
// requests, so it no longer holds or shows in the activity.
func (t *LiveTracker) Remove(id string, now time.Time) {
	t.mu.Lock()
	t.lazy()
	had := len(t.reqs) > 0
	for rid, r := range t.reqs {
		if r.session != "" && r.session == id {
			delete(t.reqs, rid)
		}
	}
	if had && len(t.reqs) == 0 {
		t.resume(now)
	}
	delete(t.names, id)
	t.mu.Unlock()
	t.Session(id, "", nil, "", "", now)
}

func (t *LiveTracker) settle(now time.Time) {
	switch {
	case len(t.reqs) > 0 || len(t.working) > 0:
		t.quietAt = time.Time{}
	case t.quietAt.IsZero():
		t.quietAt = now
	}
}

// Next reports what to push now: start when waiting went from none to
// some, end after liveQuiet with nothing waiting or working, update when
// the state changed since the last push. ok is false when nothing changed.
func (t *LiveTracker) Next(now time.Time) (state LiveState, event string, ok bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.lazy()
	rose := t.rose
	t.rose = false
	st, have := t.state()
	if !have {
		return LiveState{}, "", false
	}
	switch {
	case rose && st.Waiting > 0:
		event = liveStart
	case t.active && !t.quietAt.IsZero() && now.Sub(t.quietAt) >= liveQuiet:
		t.active = false
		return st, liveEnd, true
	case !t.hasSent || !st.equal(t.sent):
		event = liveUpdate
	default:
		return LiveState{}, "", false
	}
	t.active, t.sent, t.hasSent = true, st, true
	return st, event, true
}

// Shows reports whether the activity's state, as it stands, is the event
// a push of kind would announce, so an activity update can stand in for
// that push: an open request for needs_you, and the session sid as the
// done, ended or failed focus with nothing else waiting or working.
func (t *LiveTracker) Shows(kind, requestID, sid string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.lazy()
	switch kind {
	case wire.PushNeedsYou:
		_, open := t.reqs[requestID]
		return requestID != "" && open
	case wire.PushDone, wire.StatusEnded, wire.StatusFailed:
		if sid == "" || len(t.reqs) > 0 || len(t.working) > 0 || !t.hasLast || t.lastID != sid {
			return false
		}
		status := wire.PushDone
		if x, ok := t.exited[sid]; ok {
			status = x
		}
		return status == kind
	}
	return false
}

// Current returns the last pushed state while an activity is believed
// shown, for a device whose update token just arrived.
func (t *LiveTracker) Current() (LiveState, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.sent, t.active && t.hasSent
}

func (t *LiveTracker) state() (LiveState, bool) {
	var st LiveState
	switch {
	case len(t.reqs) > 0:
		var rid string
		var r liveRequest
		for id, x := range t.reqs {
			if rid == "" || cmp.Or(x.created.Compare(r.created), cmp.Compare(id, rid)) < 0 {
				rid, r = id, x
			}
		}
		st = LiveState{Bib: r.bib, Agent: r.agent, Status: wire.LevelNeedsYou, Waiting: len(t.reqs),
			Since: r.created.Unix(), RequestID: &rid, Command: r.command, context: r.context}
		if st.context.Title == "" {
			st.context.Title = t.names[r.session].title
		}
	case len(t.working) > 0:
		var sid string
		var s liveSession
		for id, x := range t.working {
			if sid == "" || cmp.Or(x.since.Compare(s.since), cmp.Compare(id, sid)) > 0 {
				sid, s = id, x
			}
		}
		st = LiveState{Bib: s.bib, Agent: s.agent, Status: wire.LiveWorking, Since: s.since.Unix()}
		st.context = LiveContext{Title: t.names[sid].title, Step: t.names[sid].step}
		t.lastID = sid
	case t.hasLast:
		status := wire.PushDone
		if x, ok := t.exited[t.lastID]; ok {
			status = x
		}
		st = LiveState{Bib: t.last.Bib, Agent: t.last.Agent, Status: status, Since: t.quietAt.Unix(),
			context: LiveContext{Title: t.last.context.Title}}
		if _, ended := t.exited[t.lastID]; ended {
			st.Reason = t.reasons[t.lastID]
		}
	default:
		return LiveState{}, false
	}
	t.last, t.hasLast = st, true
	return st, true
}

func nonEmpty(s *string) *string {
	if s == nil || *s == "" {
		return nil
	}
	return s
}

// truncateUTF16 cuts s to at most n UTF-16 code units, the unit the relay
// counts, without splitting a scalar.
func truncateUTF16(s string, n int) string {
	units := 0
	for i, r := range s {
		units += utf16.RuneLen(r)
		if units > n {
			return s[:i]
		}
	}
	return s
}
