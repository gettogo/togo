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
	backlog map[string][]Payload
	subs    map[string]map[*busSub]bool
	// OnDeliver, when set, is told the session of every delivery, so the
	// engine can start serving a session the moment its agent speaks.
	OnDeliver func(session string)
}

type busSub struct {
	ch chan Payload
}

// Hooks is the bus the daemon delivers to and the hook runners read.
var Hooks = NewBus()

// NewBus returns an empty bus.
func NewBus() *Bus {
	return &Bus{backlog: map[string][]Payload{}, subs: map[string]map[*busSub]bool{}}
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
	var subs []*busSub
	for s := range b.subs[session] {
		subs = append(subs, s)
	}
	on := b.OnDeliver
	b.mu.Unlock()
	for _, s := range subs {
		select {
		case s.ch <- p:
		default: // a stuck feed loses payloads rather than blocking hooks
		}
	}
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
	b.mu.Unlock()
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
