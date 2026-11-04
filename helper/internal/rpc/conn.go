package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
)

// ErrClosed is returned by Notify once the stream has ended or a write failed.
var ErrClosed = errors.New("rpc: connection closed")

// ErrNotReady is returned by Notify before hello has succeeded, so a client
// never sees a notification ahead of the hello response.
var ErrNotReady = errors.New("rpc: hello not completed")

// Conn is the write side of one served stream. Responses and notifications
// share one mutex so frames from concurrent handlers never interleave.
type Conn struct {
	mu     sync.Mutex
	w      io.Writer
	ready  bool
	closed bool
	err    error

	done      chan struct{}
	closeOnce sync.Once
}

type connKey struct{}

func newConn(w io.Writer) *Conn {
	return &Conn{w: w, done: make(chan struct{})}
}

// ConnFrom returns the connection serving the request carried by ctx, or nil
// when ctx did not come from Serve.
func ConnFrom(ctx context.Context) *Conn {
	c, _ := ctx.Value(connKey{}).(*Conn)
	return c
}

func withConn(ctx context.Context, c *Conn) context.Context {
	return context.WithValue(ctx, connKey{}, c)
}

type notification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// Notify writes a JSON-RPC notification. It fails with ErrNotReady before
// hello and ErrClosed after the stream ends.
func (c *Conn) Notify(method string, params any) error {
	payload, err := json.Marshal(notification{JSONRPC: "2.0", Method: method, Params: params})
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return ErrClosed
	}
	if !c.ready {
		return ErrNotReady
	}
	return c.writeLocked(payload)
}

// Done is closed when the stream has ended.
func (c *Conn) Done() <-chan struct{} {
	return c.done
}

func (c *Conn) setReady() {
	c.mu.Lock()
	c.ready = true
	c.mu.Unlock()
}

func (c *Conn) close() {
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()
	c.closeOnce.Do(func() { close(c.done) })
}

// writeErr reports the first write failure, which ends the stream.
func (c *Conn) writeErr() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

func (c *Conn) send(resp response) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return ErrClosed
	}
	if c.err != nil {
		return c.err
	}
	// The result is encoded under c.mu, the lock Notify takes. The event hub
	// relies on this: its subscribe result opens the event stream from
	// MarshalJSON, so no event can precede the subscribe response.
	err := send(c.w, resp)
	if err != nil {
		c.err = err
	}
	return err
}

// writeLocked writes one frame. A failed write poisons the conn because the
// peer may have seen a partial frame; an oversized one wrote nothing.
func (c *Conn) writeLocked(payload []byte) error {
	if c.err != nil {
		return ErrClosed
	}
	err := WriteFrame(c.w, payload)
	if err != nil && !errors.Is(err, ErrFrameTooLarge) {
		c.err = err
	}
	return err
}
