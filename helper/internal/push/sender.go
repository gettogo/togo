package push

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gettogo/togo/helper/internal/seal"
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

// Relay push kinds and Live Activity events, as the relay names them.
const (
	relayAlert        = "alert"
	relayBackground   = "background"
	relayLiveActivity = "liveactivity"
	liveUpdate        = "update"
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
}

// Message is one push before it is sealed for each device.
type Message struct {
	Plaintext wire.PushPlaintext
	// Kind is the relay kind: alert, or background for clear.
	Kind       string
	CollapseID string
	// Live, when set, is also sent as a Live Activity update to devices that
	// registered a Live Activity token. It carries no ciphertext.
	Live *LiveState
}

// LiveState is the Live Activity content state. Since is Unix seconds.
type LiveState struct {
	Bib     *string `json:"bib"`
	Agent   string  `json:"agent"`
	Status  string  `json:"status"`
	Waiting int     `json:"waiting"`
	Since   int64   `json:"since"`
}

// Sender turns hub events into sealed relay pushes for every live paired
// device. Pushes other than clear are skipped while any app is in the
// foreground (it gets the events directly) and for muted sessions.
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

	initOnce sync.Once
	queue    chan wire.Event

	mu          sync.Mutex
	lastPush    map[string]time.Time            // session id -> last rate-limited push
	pausedUntil map[string]time.Time            // device token -> relay 429 deadline
	pending     map[string]map[string]time.Time // session id -> request id -> created
}

func (s *Sender) init() {
	s.initOnce.Do(func() {
		s.queue = make(chan wire.Event, queueSize)
		s.lastPush = map[string]time.Time{}
		s.pausedUntil = map[string]time.Time{}
		s.pending = map[string]map[string]time.Time{}
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
	for {
		select {
		case <-ctx.Done():
			return
		case ev := <-s.queue:
			if m, ok := s.message(ev); ok {
				s.Deliver(ctx, m)
			}
		}
	}
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
func (s *Sender) message(ev wire.Event) (Message, bool) {
	sid := ""
	if ev.SessionID != nil {
		sid = *ev.SessionID
	}
	var info SessionInfo
	if sid != "" && s.Session != nil {
		info, _ = s.Session(sid)
	}
	now := s.now()
	m := Message{Kind: relayAlert}
	p := &m.Plaintext
	*p = wire.PushPlaintext{V: 1, SessionID: ev.SessionID, Bib: info.Bib, Agent: info.Agent, TS: now.UnixMilli()}

	switch pl := deref(ev.Payload).(type) {
	case wire.ApprovalOpenEvent:
		a := pl.Approval
		p.Kind = wire.PushNeedsYou
		if a.Bib != nil {
			p.Bib = a.Bib
		}
		if a.Agent != "" {
			p.Agent = &a.Agent
		}
		p.Title = a.Title
		if a.Detail != nil {
			p.Body = *a.Detail
		}
		p.RequestID = a.RequestID
		p.Options = a.Options
		m.CollapseID = a.RequestID
		m.Live = s.track(sid, a.RequestID, a.CreatedAt, true, p.Bib, p.Agent)
	case wire.ApprovalClosedEvent:
		p.Kind = wire.PushClear
		p.RequestID = pl.RequestID
		m.Kind = relayBackground
		m.CollapseID = pl.RequestID
		m.Live = s.track(sid, pl.RequestID, time.Time{}, false, p.Bib, p.Agent)
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
	if p.Kind != wire.PushClear && (info.Muted || s.Hub.ForegroundSubscribers() > 0) {
		return Message{}, false
	}
	if p.Kind != wire.PushNeedsYou && p.Kind != wire.PushClear && !s.admit(sid, now) {
		return Message{}, false
	}
	p.Title = truncate(p.Title, wire.MaxTitle)
	p.Body = truncate(p.Body, wire.MaxBody)
	return m, true
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

// track records approvals opening and closing per session and returns the
// Live Activity state that results, or nil when there is no agent to show.
func (s *Sender) track(sid, rid string, created time.Time, open bool, bib, agent *string) *LiveState {
	s.mu.Lock()
	defer s.mu.Unlock()
	reqs := s.pending[sid]
	if open {
		if reqs == nil {
			reqs = map[string]time.Time{}
			s.pending[sid] = reqs
		}
		reqs[rid] = created
	} else {
		delete(reqs, rid)
		if len(reqs) == 0 {
			delete(s.pending, sid)
		}
	}
	if sid == "" || agent == nil || *agent == "" {
		return nil
	}
	st := &LiveState{Bib: bib, Agent: *agent, Status: "running", Waiting: len(reqs), Since: s.now().Unix()}
	if len(reqs) > 0 {
		st.Status = wire.LevelNeedsYou
		for _, t := range reqs {
			if t.Unix() < st.Since && !t.IsZero() {
				st.Since = t.Unix()
			}
		}
	}
	return st
}

// Result is the outcome of one push to one device.
type Result struct {
	DeviceID string
	Err      error
}

// Deliver seals m for every live device and posts it to the device's relay.
// Devices that registered a Live Activity token also get m.Live, unsealed.
func (s *Sender) Deliver(ctx context.Context, m Message) []Result {
	s.init()
	devices, err := s.Devices.List()
	if err != nil {
		s.logf("push: %v", err)
		return nil
	}
	var live []Device
	for _, d := range devices {
		if !d.Dead {
			live = append(live, d)
		}
	}
	if len(live) == 0 {
		return nil
	}
	keys, err := s.Keys()
	if err != nil {
		s.logf("push: %v", err)
		return nil
	}
	var out []Result
	for _, d := range live {
		err := s.deliverOne(ctx, keys.HostID, d, m)
		if err != nil {
			s.logf("push: device %s: %v", idHash(d.DeviceID), err)
		}
		out = append(out, Result{DeviceID: d.DeviceID, Err: err})
		if m.Live != nil && d.LiveActivityToken != "" {
			body := relayPush{Token: d.LiveActivityToken, Env: d.Env, Kind: relayLiveActivity,
				LiveActivity: &liveActivity{Event: liveUpdate, ContentState: *m.Live}}
			if err := s.post(ctx, d, body); err != nil {
				s.logf("push: device %s live activity: %v", idHash(d.DeviceID), err)
			}
		}
	}
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
	return s.post(ctx, d, relayPush{Token: d.APNsToken, Env: d.Env, Ciphertext: ct, CollapseID: m.CollapseID, Kind: m.Kind})
}

// sealFitting seals p, dropping options and then the body until the
// envelope fits the relay's ciphertext limit. The phone can still fetch the
// full approval over SSH.
func sealFitting(pub [32]byte, p wire.PushPlaintext, aad []byte) (string, error) {
	for {
		raw, err := json.Marshal(p)
		if err != nil {
			return "", err
		}
		ct, err := seal.Seal(pub, raw, seal.LabelPush, aad)
		if err != nil {
			return "", fmt.Errorf("seal: %w", err)
		}
		switch {
		case len(ct) <= maxCiphertext:
			return ct, nil
		case len(p.Options) > 0:
			p.Options = p.Options[:len(p.Options)-1]
		case p.Body != "":
			p.Body = ""
		case p.Title != "":
			p.Title = ""
		default:
			return "", errors.New("push too large")
		}
	}
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
