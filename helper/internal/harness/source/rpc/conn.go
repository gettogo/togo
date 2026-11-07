package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
)

// Transport moves whole JSON-RPC messages.
type Transport interface {
	Read(ctx context.Context) ([]byte, error)
	Write(ctx context.Context, msg []byte) error
	Close() error
}

// Handler receives what the other side sends on its own. Both functions
// run on the read loop and must not block.
type Handler struct {
	Notification func(Message)
	Request      func(Message)
}

// ErrClosed is returned for calls on a closed connection.
var ErrClosed = errors.New("rpc: connection closed")

// Conn is one JSON-RPC connection.
type Conn struct {
	t       Transport
	h       Handler
	next    atomic.Int64
	writeMu sync.Mutex

	mu      sync.Mutex
	pending map[string]chan *Message
	err     error
	done    chan struct{}
	once    sync.Once
}

// NewConn starts reading t. The connection ends when t fails or Close is
// called.
func NewConn(t Transport, h Handler) *Conn {
	c := &Conn{t: t, h: h, pending: map[string]chan *Message{}, done: make(chan struct{})}
	go c.readLoop()
	return c
}

// Done is closed when the connection ended.
func (c *Conn) Done() <-chan struct{} { return c.done }

// Err says why the connection ended.
func (c *Conn) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

// Close ends the connection; pending calls fail with ErrClosed.
func (c *Conn) Close() error {
	c.finish(ErrClosed)
	return nil
}

func (c *Conn) finish(err error) {
	c.once.Do(func() {
		c.mu.Lock()
		c.err = err
		pending := c.pending
		c.pending = map[string]chan *Message{}
		c.mu.Unlock()
		_ = c.t.Close()
		for _, ch := range pending {
			close(ch)
		}
		close(c.done)
	})
}

func (c *Conn) readLoop() {
	ctx := context.Background()
	for {
		b, err := c.t.Read(ctx)
		if err != nil {
			c.finish(err)
			return
		}
		var m Message
		if json.Unmarshal(b, &m) != nil {
			continue // a malformed message is dropped, not fatal
		}
		switch {
		case m.IsResponse():
			c.mu.Lock()
			ch, ok := c.pending[idKey(m.ID)]
			delete(c.pending, idKey(m.ID))
			c.mu.Unlock()
			if ok {
				ch <- &m
			}
		case m.IsRequest():
			if c.h.Request != nil {
				c.h.Request(m)
			}
		case m.IsNotification():
			if c.h.Notification != nil {
				c.h.Notification(m)
			}
		}
	}
}

func (c *Conn) write(ctx context.Context, m Message) error {
	m.JSONRPC = "2.0"
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	select {
	case <-c.done:
		return ErrClosed
	default:
	}
	return c.t.Write(ctx, b)
}

// Call sends a request and waits for its response or ctx.
func (c *Conn) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	raw, err := marshalParams(params)
	if err != nil {
		return nil, err
	}
	id := numID(c.next.Add(1))
	ch := make(chan *Message, 1)
	c.mu.Lock()
	if c.err != nil {
		c.mu.Unlock()
		return nil, ErrClosed
	}
	c.pending[idKey(id)] = ch
	c.mu.Unlock()
	drop := func() {
		c.mu.Lock()
		delete(c.pending, idKey(id))
		c.mu.Unlock()
	}
	if err := c.write(ctx, Message{ID: id, Method: method, Params: raw}); err != nil {
		drop()
		return nil, err
	}
	select {
	case m, ok := <-ch:
		if !ok {
			return nil, ErrClosed
		}
		if m.Error != nil {
			return nil, m.Error
		}
		if len(m.Result) == 0 {
			return json.RawMessage("null"), nil
		}
		return m.Result, nil
	case <-ctx.Done():
		drop()
		return nil, ctx.Err()
	}
}

// Notify sends a notification.
func (c *Conn) Notify(ctx context.Context, method string, params any) error {
	raw, err := marshalParams(params)
	if err != nil {
		return err
	}
	return c.write(ctx, Message{Method: method, Params: raw})
}

// Reply answers a server request with a result or an error.
func (c *Conn) Reply(ctx context.Context, id json.RawMessage, result json.RawMessage, rpcErr *Error) error {
	m := Message{ID: id}
	if rpcErr != nil {
		m.Error = rpcErr
	} else {
		if len(result) == 0 {
			result = json.RawMessage("null")
		}
		m.Result = result
	}
	return c.write(ctx, m)
}

func marshalParams(params any) (json.RawMessage, error) {
	switch p := params.(type) {
	case nil:
		return nil, nil
	case json.RawMessage:
		return p, nil
	}
	b, err := json.Marshal(params)
	return b, err
}
