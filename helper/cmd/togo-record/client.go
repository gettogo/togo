package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"time"

	"github.com/gettogo/togo/helper/internal/rpc"
)

// client speaks the helper protocol to `togo serve --stdio`, the way the app
// does over SSH, and hands every frame to the recorder.
type client struct {
	cmd     *exec.Cmd
	in      io.WriteCloser
	mu      sync.Mutex
	next    int
	waiting map[int]chan frame
	methods map[int]string
	// onFrame sees every frame, out and in, in order.
	onFrame func(dir, method string, raw []byte)
	done    chan struct{}
}

type frame struct {
	raw    []byte
	Method string          `json:"method"`
	ID     *int            `json:"id"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	} `json:"error"`
}

func start(togo string, onFrame func(dir, method string, raw []byte)) (*client, error) {
	cmd := exec.Command(togo, "serve", "--stdio")
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	c := &client{cmd: cmd, in: in, waiting: map[int]chan frame{}, methods: map[int]string{},
		onFrame: onFrame, done: make(chan struct{})}
	go c.read(out)
	return c, nil
}

func (c *client) read(r io.Reader) {
	defer close(c.done)
	for {
		raw, err := rpc.ReadFrame(r)
		if err != nil {
			return
		}
		var f frame
		if json.Unmarshal(raw, &f) != nil {
			continue
		}
		f.raw = raw
		c.mu.Lock()
		method := f.Method
		var ch chan frame
		if f.ID != nil && f.Method == "" {
			method = c.methods[*f.ID]
			ch = c.waiting[*f.ID]
			delete(c.waiting, *f.ID)
			delete(c.methods, *f.ID)
		}
		c.onFrame("in", method, raw)
		c.mu.Unlock()
		if ch != nil {
			ch <- f
		}
	}
}

// call sends a request and waits for its answer, up to timeout.
func (c *client) call(method string, params any, timeout time.Duration) (frame, error) {
	c.mu.Lock()
	c.next++
	id := c.next
	raw, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	if err != nil {
		c.mu.Unlock()
		return frame{}, err
	}
	ch := make(chan frame, 1)
	c.waiting[id] = ch
	c.methods[id] = method
	c.onFrame("out", method, raw)
	err = rpc.WriteFrame(c.in, raw)
	c.mu.Unlock()
	if err != nil {
		return frame{}, err
	}
	select {
	case f := <-ch:
		if f.Error != nil {
			return f, fmt.Errorf("%s: %s %s", method, f.Error.Message, string(f.Error.Data))
		}
		return f, nil
	case <-time.After(timeout):
		return frame{}, fmt.Errorf("%s: no answer in %s", method, timeout)
	case <-c.done:
		return frame{}, fmt.Errorf("%s: helper closed", method)
	}
}

func (c *client) close() {
	_ = c.in.Close()
	select {
	case <-c.done:
	case <-time.After(3 * time.Second):
		_ = c.cmd.Process.Kill()
	}
	_ = c.cmd.Wait()
}
