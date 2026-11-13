package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/source/httpsrc"
	"github.com/gettogo/togo/helper/internal/harness/spec"
	"github.com/gettogo/togo/helper/internal/version"
)

func init() { harness.RegisterRunner(Default) }

// Default is the registered rpc runner. The engine's rpc_reply builtin
// reaches its Answer method through harness.LookupRunner("rpc").
var Default = NewRunner()

// Backoff bounds for reconnecting a feed.
const (
	BackoffMin = 500 * time.Millisecond
	BackoffMax = 10 * time.Second
)

// Runner reads rpc sources and feeds. One runner serves every session; it
// keeps one fetch connection per endpoint and the server requests each
// feed connection has seen and not yet answered.
type Runner struct {
	// Dial opens a transport to an endpoint. Tests replace it.
	Dial func(ctx context.Context, endpoint string) (Transport, error)
	// Poll is how often a feed looks for newly loaded threads.
	Poll time.Duration

	mu      sync.Mutex
	pool    map[string]*Conn
	pending map[string]map[string]pendingReq // endpoint -> RequestKey -> request
}

type pendingReq struct {
	conn   *Conn
	id     json.RawMessage
	method string
	params json.RawMessage
}

// NewRunner returns a runner that dials unix sockets.
func NewRunner() *Runner {
	return &Runner{
		Dial: func(ctx context.Context, endpoint string) (Transport, error) {
			p, err := SocketPath(endpoint)
			if err != nil {
				return nil, err
			}
			return DialUnix(ctx, p)
		},
		Poll:    2 * time.Second,
		pool:    map[string]*Conn{},
		pending: map[string]map[string]pendingReq{},
	}
}

func (r *Runner) Kind() string { return spec.KindRPC }

func decodeSpec(s harness.SourceSpec) (spec.RPC, error) {
	var b spec.RPC
	if err := json.Unmarshal(s.Raw, &b); err != nil {
		return b, fmt.Errorf("rpc: source %s: %w", s.Name, err)
	}
	if b.Method == "" {
		return b, fmt.Errorf("rpc: source %s has no method", s.Name)
	}
	return b, nil
}

// params turns rendered template params into an object. Empty values are
// left out, so a template whose state is not known yet sends nothing.
func params(p map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range p {
		if v != "" {
			out[k] = v
		}
	}
	return out
}

// Initialize performs the handshake the Codex app-server requires once per
// connection. A server that does not know the method is accepted.
func Initialize(ctx context.Context, c *Conn) error {
	_, err := c.Call(ctx, "initialize", map[string]any{
		"clientInfo":   map[string]string{"name": "togo", "title": "Togo", "version": version.Version},
		"capabilities": map[string]any{"experimentalApi": true},
	})
	var rpcErr *Error
	if errors.As(err, &rpcErr) && rpcErr.Code == CodeMethodNotFound {
		return nil
	}
	if err != nil {
		return err
	}
	return c.Notify(ctx, "initialized", nil)
}

// Fetch calls the source's method once, on the session's endpoint or on a
// one-shot child, and returns the scrubbed result.
func (r *Runner) Fetch(ctx context.Context, s harness.SourceSpec, env harness.Env) (json.RawMessage, error) {
	b, err := decodeSpec(s)
	if err != nil {
		return nil, err
	}
	if len(b.Child) > 0 {
		res, err := runChild(ctx, b, env)
		if err != nil {
			return nil, err
		}
		return httpsrc.Scrub(res), nil
	}
	c, err := r.fetchConn(ctx, env.Endpoint)
	if err != nil {
		return nil, err
	}
	res, err := c.Call(ctx, b.Method, params(b.Params))
	if err != nil {
		return nil, err
	}
	return httpsrc.Scrub(res), nil
}

func (r *Runner) fetchConn(ctx context.Context, endpoint string) (*Conn, error) {
	if endpoint == "" {
		return nil, errNoEndpoint
	}
	r.mu.Lock()
	c := r.pool[endpoint]
	r.mu.Unlock()
	if c != nil {
		select {
		case <-c.Done():
		default:
			return c, nil
		}
	}
	t, err := r.Dial(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	c = NewConn(t, Handler{})
	if err := Initialize(ctx, c); err != nil {
		c.Close()
		return nil, err
	}
	r.mu.Lock()
	if old := r.pool[endpoint]; old != nil && old != c {
		old.Close()
	}
	r.pool[endpoint] = c
	r.mu.Unlock()
	return c, nil
}

// Forget closes the fetch connection of an endpoint and drops its pending
// requests, for a session that ended.
func (r *Runner) Forget(endpoint string) {
	r.mu.Lock()
	c := r.pool[endpoint]
	delete(r.pool, endpoint)
	delete(r.pending, endpoint)
	r.mu.Unlock()
	if c != nil {
		c.Close()
	}
}

// Watch streams the feed: it connects, runs the attach call, and emits
// every listed notification and every server request as a record, until
// ctx ends. A dropped connection is retried with backoff.
func (r *Runner) Watch(ctx context.Context, s harness.SourceSpec, env harness.Env, emit func(harness.Record)) error {
	b, err := decodeSpec(s)
	if err != nil {
		return err
	}
	if len(b.Child) > 0 {
		return harness.ErrNotStreaming
	}
	if env.Endpoint == "" {
		return errNoEndpoint
	}
	allow := map[string]bool{}
	for _, n := range b.Notifications {
		allow[n] = true
	}
	delay := BackoffMin
	for {
		started := time.Now()
		connected, err := r.watchOnce(ctx, s.Name, b, env, allow, emit)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		_ = err // the loop retries; the engine tracks feed health
		if connected {
			emit(harness.Record{Source: s.Name, At: time.Now(), Data: json.RawMessage(`{"method":"` + MethodClosed + `"}`)})
		}
		if time.Since(started) > BackoffMax {
			delay = BackoffMin
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
		delay = min(delay*2, BackoffMax)
	}
}

// MethodClosed is the record a feed emits when its connection ended after
// the handshake, so a mapper can tell its harness is no longer followed.
const MethodClosed = "rpc/closed"

// watchOnce follows one connection. connected says the handshake passed.
func (r *Runner) watchOnce(ctx context.Context, name string, b spec.RPC, env harness.Env, allow map[string]bool, emit func(harness.Record)) (connected bool, err error) {
	in := make(chan Message, 256)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	push := func(m Message) {
		select {
		case in <- m:
		case <-ctx.Done():
		}
	}
	t, err := r.Dial(ctx, env.Endpoint)
	if err != nil {
		return false, err
	}
	c := NewConn(t, Handler{Notification: push, Request: push})
	defer func() {
		c.Close()
		r.dropConn(env.Endpoint, c)
	}()
	if err := Initialize(ctx, c); err != nil {
		return false, err
	}
	go r.attach(ctx, c, b, push)
	for {
		select {
		case <-ctx.Done():
			return true, ctx.Err()
		case <-c.Done():
			return true, c.Err()
		case m := <-in:
			// The feed's own call result arrives as {"method": b.Method,
			// "params": {"result": ...}} and always passes.
			if m.IsNotification() && len(allow) > 0 && !allow[m.Method] && m.Method != b.Method {
				continue
			}
			if m.IsRequest() {
				r.remember(env.Endpoint, c, m)
			}
			if m.Method == "serverRequest/resolved" {
				r.resolved(env.Endpoint, m.Params)
			}
			data, err := json.Marshal(m)
			if err != nil {
				continue
			}
			rec := harness.Record{Source: name, At: time.Now(), Data: httpsrc.Scrub(data)}
			if m.Method == MethodResume && m.IsNotification() {
				rec.Replay = []json.RawMessage{rec.Data}
			}
			emit(rec)
		}
	}
}

func (r *Runner) remember(endpoint string, c *Conn, m Message) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.pending[endpoint] == nil {
		r.pending[endpoint] = map[string]pendingReq{}
	}
	r.pending[endpoint][RequestKey(m.ID)] = pendingReq{conn: c, id: m.ID, method: m.Method, params: m.Params}
}

func (r *Runner) resolved(endpoint string, params json.RawMessage) {
	var p struct {
		RequestID json.RawMessage `json:"requestId"`
	}
	if json.Unmarshal(params, &p) != nil || !hasID(p.RequestID) {
		return
	}
	r.mu.Lock()
	delete(r.pending[endpoint], RequestKey(p.RequestID))
	r.mu.Unlock()
}

func (r *Runner) dropConn(endpoint string, c *Conn) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for k, p := range r.pending[endpoint] {
		if p.conn == c {
			delete(r.pending[endpoint], k)
		}
	}
}

// AnswerFunc turns the user's answer args into the result a server request
// of one method expects.
type AnswerFunc func(params json.RawMessage, args map[string]json.RawMessage) (json.RawMessage, error)

var answers = struct {
	sync.Mutex
	m map[string]AnswerFunc
}{m: map[string]AnswerFunc{}}

// RegisterAnswer adds the answer shape for a server request method.
func RegisterAnswer(method string, f AnswerFunc) {
	answers.Lock()
	defer answers.Unlock()
	if _, dup := answers.m[method]; dup {
		panic("rpc: answer for " + method + " registered twice")
	}
	answers.m[method] = f
}

// Errors from Answer.
var (
	ErrNoRequest  = errors.New("rpc: no pending request with that id")
	ErrNoAnswerer = errors.New("rpc: this request cannot be answered here")
)

// Answer replies to the pending server request named by target (a
// RequestKey) with the result its method's AnswerFunc builds from args.
func (r *Runner) Answer(ctx context.Context, env harness.Env, target string, args map[string]json.RawMessage) error {
	r.mu.Lock()
	p, ok := r.pending[env.Endpoint][target]
	r.mu.Unlock()
	if !ok {
		return ErrNoRequest
	}
	answers.Lock()
	f := answers.m[p.method]
	answers.Unlock()
	if f == nil {
		return ErrNoAnswerer
	}
	res, err := f(p.params, args)
	if err != nil {
		return err
	}
	if err := p.conn.Reply(ctx, p.id, res, nil); err != nil {
		return err
	}
	r.mu.Lock()
	delete(r.pending[env.Endpoint], target)
	r.mu.Unlock()
	return nil
}
