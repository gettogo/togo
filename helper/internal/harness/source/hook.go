package source

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/redact"
	"github.com/gettogo/togo/helper/internal/harness/spec"
)

// Bus kinds.
const (
	BusHook       = "hook"
	BusStatusline = "statusline"
)

// backlogSize is how many payloads a session keeps for a feed that starts
// after they arrived (SessionStart reaches the daemon before the engine has
// matched the session).
const backlogSize = 64

// MaxPayload bounds a forwarded hook payload.
const MaxPayload = 256 << 10

// Payload is one forwarded hook or status line payload.
type Payload struct {
	Kind  string // BusHook or BusStatusline
	Event string // the agent's own hook event name; empty for the status line
	At    time.Time
	Data  json.RawMessage
}

// Bus carries hook payloads from local.hook to the hook and statusline
// feeds of the session they belong to.
type Bus struct {
	mu      sync.Mutex
	idle    *sync.Cond // signalled when a queued payload is done or a feed leaves
	backlog map[string][]Payload
	subs    map[string]map[*busSub]bool
	// OnDeliver, when set, is told the session of every delivery, so the
	// engine can start serving a session the moment its agent speaks.
	OnDeliver func(session string)
}

type busSub struct {
	ch chan Payload
	// queued counts the payloads sent to ch that the feed has not yet
	// finished with (under Bus.mu).
	queued int
}

// Hooks is the bus the daemon delivers to and the hook runners read.
var Hooks = NewBus()

// NewBus returns an empty bus.
func NewBus() *Bus {
	b := &Bus{backlog: map[string][]Payload{}, subs: map[string]map[*busSub]bool{}}
	b.idle = sync.NewCond(&b.mu)
	return b
}

// Deliver hands a payload to the session's feeds. Oversized or malformed
// payloads are dropped; the rest are redacted first.
func (b *Bus) Deliver(session string, p Payload) {
	if len(p.Data) == 0 || len(p.Data) > MaxPayload || !json.Valid(p.Data) {
		return
	}
	p.Data = redact.JSON(p.Data)
	if p.At.IsZero() {
		p.At = time.Now()
	}
	b.mu.Lock()
	bl := append(b.backlog[session], p)
	if len(bl) > backlogSize {
		bl = bl[len(bl)-backlogSize:]
	}
	b.backlog[session] = bl
	// Sent under the lock (it never blocks), so a feed that takes the
	// payload at once finds it counted when it reports it done.
	for s := range b.subs[session] {
		select {
		case s.ch <- p:
			s.queued++
		default: // a stuck feed loses payloads rather than blocking hooks
		}
	}
	on := b.OnDeliver
	b.mu.Unlock()
	if on != nil {
		on(session)
	}
}

// Forget drops a session's backlog.
func (b *Bus) Forget(session string) {
	b.mu.Lock()
	delete(b.backlog, session)
	b.mu.Unlock()
}

// subscribe returns the backlog and a channel for later payloads.
func (b *Bus) subscribe(session string) (*busSub, []Payload) {
	s := &busSub{ch: make(chan Payload, 256)}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.subs[session] == nil {
		b.subs[session] = map[*busSub]bool{}
	}
	b.subs[session][s] = true
	return s, slices.Clone(b.backlog[session])
}

func (b *Bus) unsubscribe(session string, s *busSub) {
	b.mu.Lock()
	delete(b.subs[session], s)
	if len(b.subs[session]) == 0 {
		delete(b.subs, session)
	}
	b.idle.Broadcast()
	b.mu.Unlock()
}

// done reports that the feed has finished with one payload from its
// queue: its record is mapped, stored and published.
func (b *Bus) done(s *busSub) {
	b.mu.Lock()
	s.queued--
	if s.queued <= 0 {
		b.idle.Broadcast()
	}
	b.mu.Unlock()
}

// subscribed reports whether a feed reads the session's payloads.
func (b *Bus) subscribed(session string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.subs[session]) > 0
}

// queuedLocked counts the payloads delivered to the session's feeds (all
// sessions' for "") that the feeds have not finished with.
func (b *Bus) queuedLocked(session string) int {
	n := 0
	for id, subs := range b.subs {
		if session != "" && id != session {
			continue
		}
		for s := range subs {
			n += s.queued
		}
	}
	return n
}

// Drain waits until the feeds of session (every session for "") have
// finished with every payload delivered to them, or limit passes, and
// says whether they did. A hook's `togo hook` command has its answer once
// Deliver returned, before its feed read it, so a helper on its way out
// drains first: what it accepted is then stored for the next start
// (engine/hookstore.go). A feed that stops leaves its queue behind.
func (b *Bus) Drain(session string, limit time.Duration) bool {
	// The deadline is taken first: the timer fires no earlier, so a wait
	// that begins before it fires is woken by it.
	deadline := time.Now().Add(limit)
	timer := time.AfterFunc(limit, func() {
		b.mu.Lock()
		b.idle.Broadcast()
		b.mu.Unlock()
	})
	defer timer.Stop()
	b.mu.Lock()
	defer b.mu.Unlock()
	for b.queuedLocked(session) > 0 {
		if !time.Now().Before(deadline) {
			return false
		}
		b.idle.Wait()
	}
	return true
}

// Hook streams a session's hook payloads (kind hook) or status line
// payloads (kind statusline) from Hooks.
type Hook struct {
	kind string
	bus  *Bus
}

// NewHook returns a hook or statusline runner over bus, for tests.
func NewHook(kind string, bus *Bus) Hook { return Hook{kind: kind, bus: bus} }

func (h Hook) Kind() string { return h.kind }

func (h Hook) busOrDefault() *Bus {
	if h.bus != nil {
		return h.bus
	}
	return Hooks
}

// Fetch is not offered: hook payloads only stream.
func (h Hook) Fetch(context.Context, harness.SourceSpec, harness.Env) (json.RawMessage, error) {
	return nil, harness.ErrNotStreaming
}

// Watch emits the session's matching payloads, the backlog first, until
// ctx ends.
func (h Hook) Watch(ctx context.Context, s harness.SourceSpec, env harness.Env, emit func(harness.Record)) error {
	var events []string
	var fields []string
	switch h.kind {
	case spec.KindHook:
		var hs spec.Hook
		_ = json.Unmarshal(s.Raw, &hs)
		events = hs.Events
	case spec.KindStatusline:
		var ss spec.Statusline
		_ = json.Unmarshal(s.Raw, &ss)
		fields = ss.Fields
	}
	want := BusHook
	if h.kind == spec.KindStatusline {
		want = BusStatusline
	}
	bus := h.busOrDefault()
	sub, backlog := bus.subscribe(env.SessionID)
	defer bus.unsubscribe(env.SessionID, sub)
	send := func(p Payload) {
		if p.Kind != want {
			return
		}
		if want == BusHook && len(events) > 0 && p.Event != "" && !slices.Contains(events, p.Event) {
			return
		}
		data := p.Data
		if len(fields) > 0 {
			data = Project(data, fields)
		}
		emit(harness.Record{Source: s.Name, At: p.At, Data: data})
	}
	for _, p := range backlog {
		send(p)
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case p := <-sub.ch:
			send(p)
			bus.done(sub)
		}
	}
}

// Project keeps only the dotted fields of a JSON object (plus session_id,
// transcript_path and cwd, which identify the session).
func Project(raw json.RawMessage, fields []string) json.RawMessage {
	var obj map[string]any
	if json.Unmarshal(raw, &obj) != nil {
		return raw
	}
	out := map[string]any{}
	keep := append([]string{"session_id", "transcript_path", "cwd"}, fields...)
	for _, f := range keep {
		parts := strings.Split(f, ".")
		var cur any = obj
		ok := true
		for _, p := range parts {
			m, isMap := cur.(map[string]any)
			if !isMap {
				ok = false
				break
			}
			if cur, ok = m[p]; !ok {
				break
			}
		}
		if !ok {
			continue
		}
		dst := out
		for _, p := range parts[:len(parts)-1] {
			next, isMap := dst[p].(map[string]any)
			if !isMap {
				next = map[string]any{}
				dst[p] = next
			}
			dst = next
		}
		dst[parts[len(parts)-1]] = cur
	}
	b, err := json.Marshal(out)
	if err != nil {
		return raw
	}
	return b
}
