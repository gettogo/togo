package push

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gettogo/togo/helper/internal/agents"
	"github.com/gettogo/togo/helper/internal/detect"
	"github.com/gettogo/togo/helper/internal/wire"
)

const (
	requestTimeout  = 10 * time.Second
	sessionInterval = 5 * time.Second
	maxAttempts     = 3
	firstBackoff    = time.Second
	maxRetryAfter   = time.Hour
	maxCiphertext   = 3072
	queueSize       = 64
)

// Relay push kinds and targets, as the relay names them.
const (
	relayAlert        = "alert"
	relayBackground   = "background"
	relayLiveActivity = "liveactivity"
	targetWatch       = "watch"
)

// Hub is the part of the event hub the Sender needs.
type Hub interface {
	OnEvent(fn func(wire.Event))
	ForegroundSubscribers() int
}

// SessionInfo is what the Sender needs to know about a session.
type SessionInfo struct {
	Bib   *string
	Agent *string
	Muted bool
	// Name is the session's name, shown as the title of its pushes and
	// Live Activity; empty when unknown.
	Name string
}

// Message is one push before it is sealed for each device.
type Message struct {
	Plaintext wire.PushPlaintext
	// Kind is the relay kind: alert, or background for clear.
	Kind       string
	CollapseID string
}

// Sender turns hub events into sealed relay pushes for every live paired
// device. Pushes other than clear are skipped while any app is in the
// foreground (it gets the events directly) and for muted sessions. It also
// drives one Live Activity per machine on iOS devices (see LiveTracker);
// Watch devices get background pushes only.
type Sender struct {
	Hub     Hub
	Keys    func() (HostKeys, error)
	Devices *Devices
	// Session looks up a session by id; nil or a false result means unknown,
	// which is treated as unmuted with no bib or agent.
	Session func(id string) (SessionInfo, bool)
	// Client posts to the relay; nil means a client with a 10 s timeout that
	// does not follow redirects.
	Client *http.Client
	Now    func() time.Time
	// Sleep waits between retries; nil waits on a timer.
	Sleep func(ctx context.Context, d time.Duration) error
	// Logf receives one line per failed push, with ids hashed. Nil discards.
	Logf func(format string, args ...any)
	// Muted reports whether this whole machine is muted: only clear pushes
	// go out. Nil means never.
	Muted func() bool
	// Host is the machine name sealed into every push; empty means the
	// short form of os.Hostname.
	Host string
	// Live tracks this machine's Live Activity; nil means a new tracker.
	Live *LiveTracker
	// Tap sees every relay request, as hashes; nil for everything but
	// end-to-end tests (see TapEnv).
	Tap func(TapRecord)
	// Remind sends one reminder for a needs_you left unanswered; nil means
	// none. Its Deliver defaults to the Sender's own gated send.
	Remind *Reminders

	initOnce sync.Once
	queue    chan wire.Event
	host     *string

	mu          sync.Mutex
	lastPush    map[string]time.Time // session id -> last rate-limited push
	pausedUntil map[string]time.Time // device token -> relay 429 deadline
	relayDown   map[string]time.Time // relay URL -> when it last failed every attempt
	held        heldPushes
	exits       map[string]string    // session id -> the exit last announced
	requests    requestSeen          // needs_you pushed or closed, per request id

	liveMu  sync.Mutex
	liveDev map[string]*deviceLive // device id -> its activity
	liveGen uint64                 // Devices.Gen when the list was last read
}

func (s *Sender) init() {
	s.initOnce.Do(func() {
		s.queue = make(chan wire.Event, queueSize)
		s.lastPush = map[string]time.Time{}
		s.pausedUntil = map[string]time.Time{}
		s.relayDown = map[string]time.Time{}
		s.exits = map[string]string{}
		s.liveDev = map[string]*deviceLive{}
		if s.Live == nil {
			s.Live = &LiveTracker{}
		}
		if s.Remind != nil && s.Remind.Deliver == nil {
			s.Remind.Deliver = s.deliverReminder
		}
		h := s.Host
		if h == "" {
			h, _ = os.Hostname()
		}
		if h = shortHost(h); h != "" {
			s.host = &h
		}
		if s.Client == nil {
			s.Client = &http.Client{
				Timeout: requestTimeout,
				// A redirect could move the request off https; the relay
				// never redirects.
				CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
			}
		}
	})
}

// Run subscribes to the hub and sends pushes until ctx ends. Events are
// queued so the publisher never waits on the network; when the queue is
// full the event is dropped.
func (s *Sender) Run(ctx context.Context) {
	s.init()
	s.Hub.OnEvent(func(ev wire.Event) {
		select {
		case s.queue <- ev:
		default:
			s.logf("push: queue full, dropped %s event", ev.Kind)
		}
	})
	// Activities from before this process are unknown; a device counts as
	// having one again once it registers an update token.
	if err := s.Devices.ForgetLiveActivities(); err != nil {
		s.logf("push: %v", err)
	}
	t := time.NewTicker(liveTick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case ev := <-s.queue:
			s.process(ctx, ev)
		case <-t.C:
			s.pumpLive(ctx)
		}
	}
}

// process handles one hub event: the tracker sees it, the push it maps to
// goes out, then any Live Activity change.
func (s *Sender) process(ctx context.Context, ev wire.Event) {
	s.init()
	sid := ""
	if ev.SessionID != nil {
		sid = *ev.SessionID
	}
	var info SessionInfo
	if sid != "" && s.Session != nil {
		info, _ = s.Session(sid)
	}
	s.observe(ev, sid, info, s.now())
	if m, ok := s.message(ev, sid, info); ok {
		s.afterDeliver(m, s.Deliver(ctx, m), sid)
	}
	if s.Remind != nil && sid != "" {
		s.cancelReminders(ev, sid)
	}
	s.pumpLive(ctx)
}

// shortHost is the machine name as `hostname -s` prints it, cut to the
// schema's limit.
func shortHost(h string) string {
	h, _, _ = strings.Cut(h, ".")
	return truncate(h, wire.MaxHostName)
}

func (s *Sender) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Sender) logf(format string, args ...any) {
	if s.Logf != nil {
		s.Logf(format, args...)
	}
}

// message maps an event to a push, applying the foreground, mute and rate
// rules. ok is false when nothing should be sent.
func (s *Sender) message(ev wire.Event, sid string, info SessionInfo) (Message, bool) {
	now := s.now()
	m := Message{Kind: relayAlert}
	p := &m.Plaintext
	*p = wire.PushPlaintext{V: 1, SessionID: ev.SessionID, Bib: info.Bib, Agent: info.Agent, TS: now.UnixMilli(), Host: s.host}
	if name := sessionTitle(info.Name); name != "" {
		p.SessionName = &name
	}

	switch pl := deref(ev.Payload).(type) {
	case wire.ApprovalOpenEvent:
		a := pl.Approval
		if s.requests.blocks(a.RequestID) {
			return Message{}, false
		}
		p.Kind = wire.PushNeedsYou
		if a.Bib != nil {
			p.Bib = a.Bib
		}
		if agent := requestAgent(a.Agent, info); agent != "" {
			p.Agent = &agent
		}
		p.Title = a.Title
		// With no subject the app shows the body, which may be screen
		// text: clean it here too, before the length cap below.
		if a.Detail != nil {
			p.Body = detect.CleanLine(detect.DropKeyHints(*a.Detail))
		}
		p.RequestID = a.RequestID
		p.Options = a.Options
		p.Subject = a.Subject
		if s := a.Subject; s != nil && s.Kind == wire.SubjectFile && s.Validate() == nil {
			// A path reads from its end: home as ~, temporary folders by their last names.
			v := shortSubject(*s, maxPushPath)
			p.Subject = &v
		}
		// Carried into the reminder too, which copies this plaintext.
		p.AllowText = a.AllowText
		if a.Answerable != nil && !*a.Answerable {
			// Nothing sent from a notification can answer it yet (a
			// tabbed question, or a prompt not on screen yet): no buttons,
			// so the phone says to open the app.
			p.Options, p.AllowText = nil, false
		}
		m.CollapseID = a.RequestID
	case wire.ApprovalClosedEvent:
		s.requests.mark(pl.RequestID, seenClosed)
		p.Kind = wire.PushClear
		p.RequestID = pl.RequestID
		m.Kind = relayBackground
		m.CollapseID = pl.RequestID
	case wire.NotifyEvent:
		switch pl.Level {
		case wire.LevelDone:
			p.Kind = wire.PushDone
		case wire.LevelNeedsYou:
			p.Kind = wire.PushNotify
		default:
			return Message{}, false
		}
		p.Title, p.Body = pl.Title, pl.Body
	case wire.SessionEvent:
		sess, ok := sessionOf(pl.Session)
		if !ok || !s.exitChanged(sess) {
			return Message{}, false
		}
		p.Kind = sess.Status
		p.Bib = sess.Bib
		agent := agents.Resolve(sess.Agent, sess.Command)
		p.Agent = &agent
		p.Title = sess.Name
		if name := sessionTitle(sess.Name); name != "" {
			p.SessionName = &name
		}
		exit := *sess.Exit
		p.Exit = &exit
	case wire.SessionRemovedEvent:
		s.mu.Lock()
		delete(s.exits, sid)
		s.mu.Unlock()
		return Message{}, false
	case wire.JobEvent:
		j := pl.Job
		if j.Status == "running" {
			return Message{}, false
		}
		p.Kind = wire.PushJob
		p.Title = j.Name
		p.Body = j.Status
		if j.Message != nil && *j.Message != "" {
			p.Body = *j.Message
		}
	default:
		return Message{}, false
	}

	// A clear always goes out: skipping it could leave an earlier needs_you
	// on the lock screen or Watch after the approval closed.
	if p.Kind == wire.PushClear {
		s.held.cancel(p.RequestID)
	}
	if p.Kind != wire.PushClear && (info.Muted || (s.Muted != nil && s.Muted())) {
		return Message{}, false
	}
	if p.Kind != wire.PushClear && s.Hub.ForegroundSubscribers() > 0 {
		if p.Kind == wire.PushNeedsYou {
			held := m
			held.Plaintext.Title = truncate(p.Title, wire.MaxTitle)
			held.Plaintext.Body = truncate(p.Body, wire.MaxBody)
			s.held.hold(held, foregroundRecheck, s.releaseHeld)
		}
		return Message{}, false
	}
	if p.Kind != wire.PushNeedsYou && p.Kind != wire.PushClear && !s.admit(sid, now) {
		return Message{}, false
	}
	if p.Kind == wire.PushNeedsYou {
		s.requests.mark(p.RequestID, seenPushed)
	}
	p.Title = truncate(p.Title, wire.MaxTitle)
	p.Body = truncate(p.Body, wire.MaxBody)
	return m, true
}

// requestAgent is the agent an approval names, unless it names none or
// only "custom", where the session's own agent (resolved from its
// program) says more.
func requestAgent(agent string, info SessionInfo) string {
	if (agent == "" || agent == wire.AgentCustom) && info.Agent != nil && *info.Agent != "" {
		return *info.Agent
	}
	return agent
}

// deref accepts payloads published as values or pointers.
func deref(v any) any {
	switch p := v.(type) {
	case *wire.ApprovalOpenEvent:
		return *p
	case *wire.ApprovalClosedEvent:
		return *p
	case *wire.NotifyEvent:
		return *p
	case *wire.JobEvent:
		return *p
	case *wire.SessionEvent:
		return *p
	case *wire.SessionRemovedEvent:
		return *p
	}
	return v
}

// admit applies the one-push-per-5-s limit for non-approval pushes in one
// session. Events with no session share one bucket.
func (s *Sender) admit(sid string, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if last, ok := s.lastPush[sid]; ok && now.Sub(last) < sessionInterval {
		return false
	}
	s.lastPush[sid] = now
	return true
}

// Result is the outcome of one push to one device.
type Result struct {
	DeviceID string
	Err      error
}

// Deliver seals m for every live device and posts it to the device's relay,
// addressed to the Watch app, as a background push, for watchOS devices.
func (s *Sender) Deliver(ctx context.Context, m Message) []Result {
	s.init()
	devices, err := s.Devices.List()
	if err != nil {
		s.logf("push: %v", err)
		return nil
	}
	var live []Device
	for _, d := range devices {
		if d.Dead || !watchWants(d, m.Plaintext.Kind) {
			continue
		}
		live = append(live, d)
	}
	if len(live) == 0 {
		return nil
	}
	keys, err := s.Keys()
	if err != nil {
		s.logf("push: %v", err)
		return nil
	}
	// Devices are sent to side by side: one slow or failing device (or
	// relay) does not hold up the others.
	out := make([]Result, len(live))
	var wg sync.WaitGroup
	for i, d := range live {
		wg.Go(func() {
			err := s.deliverOne(ctx, keys.HostID, d, m)
			if err != nil {
				s.logf("push: device %s: %v", idHash(d.DeviceID), err)
			}
			out[i] = Result{DeviceID: d.DeviceID, Err: err}
		})
	}
	wg.Wait()
	return out
}

func (s *Sender) deliverOne(ctx context.Context, hostID string, d Device, m Message) error {
	var pub [32]byte
	if !decode32(d.X25519, &pub) {
		return errors.New("stored device key unreadable")
	}
	ct, err := sealFitting(pub, m.Plaintext, []byte(hostID+"|"+d.DeviceID))
	if err != nil {
		return err
	}
	body := relayPush{Token: d.APNsToken, Env: d.Env, Ciphertext: ct, CollapseID: m.CollapseID, Kind: m.Kind}
	if d.EffectivePlatform() == wire.PlatformWatch {
		// The phone's alert is mirrored to the Watch with its actions, so the Watch app gets
		// silent pushes that only update its list and complication.
		body.Target = targetWatch
		body.Kind = relayBackground
	}
	return s.post(ctx, d, body)
}

// watchWants reports whether d should get a push of kind. The Watch list and
// complication use only needs_you and clear, and watchOS rations background
// pushes, so the rest would spend that budget for nothing.
func watchWants(d Device, kind string) bool {
	if d.EffectivePlatform() != wire.PlatformWatch {
		return true
	}
	return kind == wire.PushNeedsYou || kind == wire.PushClear
}

// idHash is the 6-character sha256 prefix logged in place of an id.
func idHash(id string) string {
	sum := sha256.Sum256([]byte(id))
	return hex.EncodeToString(sum[:])[:6]
}

// truncate cuts s to n code points, the unit the schema's maxLength counts.
func truncate(s string, n int) string {
	i := 0
	for j := range s {
		if i == n {
			return s[:j]
		}
		i++
	}
	return s
}
