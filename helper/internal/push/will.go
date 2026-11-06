package push

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/gettogo/togo/helper/internal/session"
	"github.com/gettogo/togo/helper/internal/wire"
)

// Will frame limits, as the relay enforces them.
const (
	willAfterMs   = 300_000 // silence before the relay sends the will
	willMaxPushes = 8
	willMaxFrame  = 32_768

	willCollapseID = "offline"
)

// Will timings. A will is rebuilt at most once per debounce after a change,
// and refreshed while armed well inside the relay's 24 h limit.
const (
	willDebounce = 30 * time.Second
	willRefresh  = 6 * time.Hour
	willTick     = time.Second
)

// disarmFrame withdraws a stored will. It matches mailbox.DisarmFrame.
var disarmFrame = []byte(`{"type":"will","v":1,"pushes":[]}`)

// willFrame is the mailbox frame. Type comes first: the relay recognises a
// large will by its prefix before parsing it.
type willFrame struct {
	Type    string      `json:"type"`
	V       int         `json:"v"`
	AfterMs int         `json:"afterMs"`
	Pushes  []relayPush `json:"pushes"`
}

// willSession is what the keeper remembers about an active session.
type willSession struct {
	bib   *string
	agent string
	since time.Time
	needs bool
}

// WillKeeper seals, ahead of time, the "machine went silent" pushes for every
// paired device and keeps them ready for the mailbox, one frame per relay.
// The relay sends them only if this helper's connection goes quiet, so it
// stays armed only while at least one managed session is working or needs
// you, and is disarmed when nothing is active, the machine is muted or no
// device is paired. Pushes are sealed per device like every other push;
// the Live Activity part carries metadata only.
type WillKeeper struct {
	Hub     Hub
	Devices *Devices
	Keys    func() (HostKeys, error)
	// Muted reports whether this machine is muted; nil means never.
	Muted func() bool
	// Host is the machine name sealed into the push; empty means the short
	// form of os.Hostname.
	Host string
	Now  func() time.Time
	// Debounce is the wait between a change and the rebuild; zero means
	// 30 s.
	Debounce time.Duration
	// Logf receives build failures; nil discards.
	Logf func(format string, args ...any)
	// Sessions lists the sessions with the status the hub holds for them;
	// Run reads it once before the first build. Nil skips that.
	Sessions func(ctx context.Context) ([]session.Session, error)

	initOnce sync.Once
	subOnce  sync.Once

	mu       sync.Mutex
	active   map[string]willSession
	dirty    bool      // the active set changed since the last build
	dueAt    time.Time // pending rebuild; zero when none
	builtAt  time.Time
	built    bool
	armed    bool
	frames   map[string][]byte // relay URL -> frame
	gen      uint64            // Devices.Gen at the last build
	muted    bool              // Muted() at the last build
	changed  chan struct{}
	rebuilds int
}

func (w *WillKeeper) init() {
	w.initOnce.Do(func() {
		w.mu.Lock()
		defer w.mu.Unlock()
		w.active = map[string]willSession{}
		w.changed = make(chan struct{})
		if w.Debounce <= 0 {
			w.Debounce = willDebounce
		}
	})
}

func (w *WillKeeper) now() time.Time {
	if w.Now != nil {
		return w.Now()
	}
	return time.Now()
}

func (w *WillKeeper) logf(format string, args ...any) {
	if w.Logf != nil {
		w.Logf(format, args...)
	}
}

// Run follows the hub and rebuilds the will until ctx ends. The first build
// waits one debounce, so sessions already working are known before a will
// left by an earlier process is replaced.
func (w *WillKeeper) Run(ctx context.Context) {
	w.start(ctx)
	t := time.NewTicker(willTick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			w.tick()
		}
	}
}

// Subscribe starts following the hub. Call it when the hub is built, before
// anything publishes, so no status change is missed; Run calls it too and
// a second call does nothing.
func (w *WillKeeper) Subscribe() {
	w.init()
	w.subOnce.Do(func() { w.Hub.OnEvent(w.observe) })
}

// start subscribes, counts the sessions already active and schedules the
// first build.
func (w *WillKeeper) start(ctx context.Context) {
	w.Subscribe()
	if w.Sessions != nil {
		lctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		list, err := w.Sessions(lctx)
		cancel()
		if err != nil {
			w.logf("push: will: sessions: %v", err)
		}
		for _, s := range list {
			id := s.ID
			w.observe(wire.Event{Kind: wire.EventSession, SessionID: &id, Payload: wire.SessionEvent{Session: s}})
		}
	}
	w.mu.Lock()
	w.dueAt = w.now().Add(w.Debounce)
	w.mu.Unlock()
}

// Will returns the frame for relayURL. ok is false until the first build;
// a relay with nothing to arm gets the disarm frame.
func (w *WillKeeper) Will(relayURL string) ([]byte, bool) {
	w.init()
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.built {
		return nil, false
	}
	if f, ok := w.frames[strings.TrimRight(relayURL, "/")]; ok {
		return f, true
	}
	return disarmFrame, true
}

// Changed returns a channel closed at the next rebuild. Take it before
// reading Will so no rebuild is missed.
func (w *WillKeeper) Changed() <-chan struct{} {
	w.init()
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.changed
}

// observe applies the active rule awake.Manager uses: a managed session
// that is working or needs you.
func (w *WillKeeper) observe(ev wire.Event) {
	if ev.SessionID == nil {
		return
	}
	id := *ev.SessionID
	var cur willSession
	active := false
	switch ev.Kind {
	case wire.EventSession:
		p, ok := deref(ev.Payload).(wire.SessionEvent)
		if !ok {
			return
		}
		s, ok := sessionOf(p.Session)
		if !ok {
			return
		}
		active = s.Managed && (s.Status == "working" || s.Status == wire.LevelNeedsYou)
		cur = willSession{bib: s.Bib, agent: s.Agent, needs: s.Status == wire.LevelNeedsYou}
	case wire.EventSessionRemoved:
	default:
		return
	}
	w.init()
	w.mu.Lock()
	defer w.mu.Unlock()
	prev, was := w.active[id]
	if !active {
		if was {
			delete(w.active, id)
			w.dirty = true
		}
		return
	}
	cur.since = w.now()
	if was {
		cur.since = prev.since
	}
	w.active[id] = cur
	// Only the count and the waiting count change what is sealed; a
	// session moving between working and needs you changes the latter.
	if !was || prev.needs != cur.needs {
		w.dirty = true
	}
}

// tick schedules a rebuild after a change and runs one when it is due.
func (w *WillKeeper) tick() {
	w.init()
	now := w.now()
	muted := w.Muted != nil && w.Muted()
	gen := w.Devices.Gen()
	w.mu.Lock()
	changed := w.dirty || (w.built && (gen != w.gen || muted != w.muted))
	w.dirty = false
	if changed && w.dueAt.IsZero() {
		w.dueAt = now.Add(w.Debounce)
	}
	due := !w.dueAt.IsZero() && !now.Before(w.dueAt)
	refresh := w.built && w.armed && now.Sub(w.builtAt) >= willRefresh
	w.mu.Unlock()
	if due || refresh {
		w.rebuild(now, muted, gen)
	}
}

// rebuild seals a fresh will for every relay and wakes the mailbox.
func (w *WillKeeper) rebuild(now time.Time, muted bool, gen uint64) {
	w.mu.Lock()
	sessions := make([]willSession, 0, len(w.active))
	for _, s := range w.active {
		sessions = append(sessions, s)
	}
	w.mu.Unlock()

	frames, err := w.build(now, sessions, muted)
	if err != nil {
		// Keep the previous will and try again after another debounce.
		w.logf("push: will: %v", err)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.dueAt = time.Time{}
	if err != nil {
		w.dueAt = now.Add(w.Debounce)
		return
	}
	w.rebuilds++
	w.built, w.builtAt, w.gen, w.muted = true, now, gen, muted
	w.armed = len(frames) > 0
	w.frames = frames
	close(w.changed)
	w.changed = make(chan struct{})
}

// build returns the armed frame for each relay that has something to send.
// An empty map means disarm everywhere.
func (w *WillKeeper) build(now time.Time, sessions []willSession, muted bool) (map[string][]byte, error) {
	if len(sessions) == 0 || muted {
		return map[string][]byte{}, nil
	}
	devices, err := w.Devices.List()
	if err != nil {
		return nil, err
	}
	byRelay := map[string][]Device{}
	var order []string
	for _, d := range devices {
		if d.Dead || d.RelayURL == "" {
			continue
		}
		u := strings.TrimRight(d.RelayURL, "/")
		if _, ok := byRelay[u]; !ok {
			order = append(order, u)
		}
		byRelay[u] = append(byRelay[u], d)
	}
	if len(order) == 0 {
		return map[string][]byte{}, nil
	}
	keys, err := w.Keys()
	if err != nil {
		return nil, err
	}
	host := w.Host
	if host == "" {
		host, _ = os.Hostname()
	}
	host = shortHost(host)
	out := map[string][]byte{}
	for _, u := range order {
		pushes := w.pushes(now, keys.HostID, host, byRelay[u], sessions)
		frame, err := fitFrame(pushes)
		if err != nil {
			return nil, err
		}
		if frame != nil {
			out[u] = frame
		}
	}
	return out, nil
}

// pushes builds one sealed offline alert per device, then a Live Activity
// update for each iOS device with an activity, alerts first so they win
// when the relay's push limit cuts the list.
func (w *WillKeeper) pushes(now time.Time, hostID, host string, devices []Device, sessions []willSession) []relayPush {
	n := len(sessions)
	p := wire.PushPlaintext{V: 1, Kind: wire.PushOffline, TS: now.UnixMilli(), Active: &n}
	if host != "" {
		p.Host = &host
	}
	var alerts, live []relayPush
	for _, d := range devices {
		// The Watch shows the offline alert as the phone's notification,
		// mirrored by watchOS; a push of its own would spend its
		// background budget on a duplicate.
		if d.EffectivePlatform() == wire.PlatformWatch {
			continue
		}
		var pub [32]byte
		if !decode32(d.X25519, &pub) {
			w.logf("push: will: device %s key unreadable", idHash(d.DeviceID))
			continue
		}
		ct, err := sealFitting(pub, p, []byte(hostID+"|"+d.DeviceID))
		if err != nil {
			w.logf("push: will: device %s: %v", idHash(d.DeviceID), err)
			continue
		}
		alerts = append(alerts, relayPush{Token: d.APNsToken, Env: d.Env, Ciphertext: ct, CollapseID: willCollapseID, Kind: relayAlert})
	}
	st := offlineState(now, sessions)
	for _, d := range devices {
		if d.EffectivePlatform() != wire.PlatformIOS || d.LiveActivityToken == "" {
			continue
		}
		// No stale date: the will may be sent hours after it was sealed,
		// and a past one would show the activity as stale on arrival.
		live = append(live, relayPush{Token: d.LiveActivityToken, Env: d.Env, Kind: relayLiveActivity,
			LiveActivity: &liveActivity{Event: liveUpdate, ContentState: st}})
	}
	out := append(alerts, live...)
	if len(out) > willMaxPushes {
		out = out[:willMaxPushes]
	}
	return out
}

// offlineState is the Live Activity content for a silent machine: the
// focus is a session that needs you, else the longest working one. It is
// metadata only; the command is never included.
func offlineState(now time.Time, sessions []willSession) LiveState {
	waiting := 0
	for _, s := range sessions {
		if s.needs {
			waiting++
		}
	}
	focus := slices.MinFunc(sessions, func(a, b willSession) int {
		if a.needs != b.needs {
			if a.needs {
				return -1
			}
			return 1
		}
		return cmp.Compare(a.since.UnixNano(), b.since.UnixNano())
	})
	agent := focus.agent
	if agent == "" {
		agent = agentShell
	}
	return LiveState{Bib: focus.bib, Agent: agent, Status: wire.PushOffline, Waiting: waiting, Since: now.Unix()}
}

// fitFrame encodes pushes as a will frame, dropping pushes from the end
// until it fits the relay's frame limit. Nil means nothing to arm.
func fitFrame(pushes []relayPush) ([]byte, error) {
	for len(pushes) > 0 {
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(willFrame{Type: "will", V: 1, AfterMs: willAfterMs, Pushes: pushes}); err != nil {
			return nil, err
		}
		b := bytes.TrimRight(buf.Bytes(), "\n")
		if len(b) <= willMaxFrame {
			return b, nil
		}
		pushes = pushes[:len(pushes)-1]
	}
	return nil, nil
}
