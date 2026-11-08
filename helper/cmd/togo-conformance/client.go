package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"

	"github.com/gettogo/togo/helper/internal/rpc"
)

// rpcError is a JSON-RPC error answer.
type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *rpcError) Error() string {
	s := fmt.Sprintf("rpc %d %s", e.Code, e.Message)
	if len(e.Data) > 0 {
		s += " " + clip(string(e.Data), 160)
	}
	return s
}

type frame struct {
	ID     *int            `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error"`
}

// client speaks framed JSON-RPC to `togo serve --stdio`, which proxies to
// the daemon of the TOGO_HOME in env (starting it when none runs).
type client struct {
	cmd     *exec.Cmd
	w       io.WriteCloser
	mu      sync.Mutex
	next    int
	pending map[int]chan frame
	notify  func(method string, params json.RawMessage)
	done    chan struct{}
	before  func() // runs before every call (the suite's config ledger)
}

func dial(ctx context.Context, bin string, env []string, notify func(string, json.RawMessage)) (*client, error) {
	cmd := exec.Command(bin, "serve", "--stdio")
	cmd.Env = env
	cmd.Stderr = io.Discard
	w, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	r, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	c := &client{cmd: cmd, w: w, pending: map[int]chan frame{}, notify: notify, done: make(chan struct{})}
	go c.read(r)
	hello := map[string]any{
		"protocol":     1,
		"client":       map[string]string{"name": "togo-conformance", "version": "0"},
		"capabilities": []string{"harness.v1"},
	}
	if err := c.call(ctx, "hello", hello, nil); err != nil {
		c.close()
		return nil, fmt.Errorf("hello: %w", err)
	}
	return c, nil
}

func (c *client) read(r io.Reader) {
	defer close(c.done)
	for {
		payload, err := rpc.ReadFrame(r)
		if err != nil {
			c.mu.Lock()
			for id, ch := range c.pending {
				close(ch)
				delete(c.pending, id)
			}
			c.mu.Unlock()
			return
		}
		var f frame
		if json.Unmarshal(payload, &f) != nil {
			continue
		}
		if f.ID == nil {
			if c.notify != nil && f.Method != "" {
				c.notify(f.Method, f.Params)
			}
			continue
		}
		c.mu.Lock()
		ch := c.pending[*f.ID]
		delete(c.pending, *f.ID)
		c.mu.Unlock()
		if ch != nil {
			ch <- f
		}
	}
}

var errClosed = errors.New("helper connection closed")

// call sends method and decodes the result into out (when non-nil).
func (c *client) call(ctx context.Context, method string, params, out any) error {
	if c.before != nil {
		c.before()
	}
	c.mu.Lock()
	c.next++
	id := c.next
	ch := make(chan frame, 1)
	c.pending[id] = ch
	body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	if err == nil {
		err = rpc.WriteFrame(c.w, body)
	}
	c.mu.Unlock()
	if err != nil {
		return err
	}
	select {
	case f, ok := <-ch:
		if !ok {
			return errClosed
		}
		if f.Error != nil {
			return f.Error
		}
		if out != nil {
			return json.Unmarshal(f.Result, out)
		}
		return nil
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return fmt.Errorf("%s: %w", method, ctx.Err())
	}
}

func (c *client) close() {
	_ = c.w.Close()
	if c.cmd.Process != nil {
		_ = c.cmd.Process.Signal(os.Interrupt)
	}
	_ = c.cmd.Wait()
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
