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
	"time"

	"github.com/gettogo/togo/helper/internal/rpc"
	"github.com/gettogo/togo/helper/internal/wire"
)

// helper is one `togo serve --stdio` connection, spoken to the way the app
// speaks to it: framed JSON-RPC, hello first.
type helper struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	mu     sync.Mutex
	nextID int
	calls  map[int]chan reply
	events chan event
	done   chan struct{}
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type reply struct {
	ID     *int            `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error"`
}

// event is the part of an `event` notification the test reads.
type event struct {
	Kind      string `json:"kind"`
	RequestID string `json:"requestId"`
	Approval  struct {
		RequestID string        `json:"requestId"`
		Title     string        `json:"title"`
		Options   []wire.Option `json:"options"`
	} `json:"approval"`
}

// dial starts `togo serve --stdio` with env and says hello.
func dial(ctx context.Context, togo string, env []string) (*helper, error) {
	cmd := exec.CommandContext(ctx, togo, "serve", "--stdio")
	cmd.Env = env
	cmd.Stderr = os.Stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	h := &helper{cmd: cmd, stdin: stdin, calls: map[int]chan reply{}, events: make(chan event, 64), done: make(chan struct{})}
	go h.read(stdout)
	var hello struct {
		Protocol int `json:"protocol"`
	}
	err = h.call("hello", map[string]any{
		"protocol":     1,
		"client":       map[string]string{"name": "togo-push-e2e", "version": "0"},
		"capabilities": []string{},
	}, &hello)
	if err != nil {
		h.close()
		return nil, fmt.Errorf("hello: %w", err)
	}
	return h, nil
}

func (h *helper) read(r io.Reader) {
	defer close(h.done)
	for {
		payload, err := rpc.ReadFrame(r)
		if err != nil {
			return
		}
		var m reply
		if json.Unmarshal(payload, &m) != nil {
			continue
		}
		if m.ID == nil {
			if m.Method == "event" {
				var ev event
				if json.Unmarshal(m.Params, &ev) == nil {
					select {
					case h.events <- ev:
					default:
					}
				}
			}
			continue
		}
		h.mu.Lock()
		ch := h.calls[*m.ID]
		delete(h.calls, *m.ID)
		h.mu.Unlock()
		if ch != nil {
			ch <- m
		}
	}
}

// call sends one request and waits up to 15 s for its reply.
func (h *helper) call(method string, params, result any) error {
	h.mu.Lock()
	h.nextID++
	id := h.nextID
	ch := make(chan reply, 1)
	h.calls[id] = ch
	h.mu.Unlock()
	req, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	if err != nil {
		return err
	}
	h.mu.Lock()
	err = rpc.WriteFrame(h.stdin, req)
	h.mu.Unlock()
	if err != nil {
		return fmt.Errorf("%s: %w", method, err)
	}
	select {
	case m := <-ch:
		if m.Error != nil {
			return fmt.Errorf("%s: error %d %s", method, m.Error.Code, m.Error.Message)
		}
		if result == nil {
			return nil
		}
		return json.Unmarshal(m.Result, result)
	case <-h.done:
		return fmt.Errorf("%s: helper closed the connection", method)
	case <-time.After(15 * time.Second):
		return fmt.Errorf("%s: no reply in 15 s", method)
	}
}

// subscribe asks for events as a background client, so the helper keeps
// pushing to phones while this connection is open.
func (h *helper) subscribe() error {
	return h.call(wire.MethodEventsSubscribe, map[string]any{"foreground": false}, nil)
}

// next returns the first event of kind within d.
func (h *helper) next(kind string, d time.Duration) (event, error) {
	deadline := time.After(d)
	for {
		select {
		case ev := <-h.events:
			if ev.Kind == kind {
				return ev, nil
			}
		case <-h.done:
			return event{}, errors.New("helper closed the connection")
		case <-deadline:
			return event{}, fmt.Errorf("no %s event in %s", kind, d)
		}
	}
}

func (h *helper) close() {
	h.stdin.Close()
	select {
	case <-h.done:
	case <-time.After(5 * time.Second):
		h.cmd.Process.Kill()
	}
	_ = h.cmd.Wait()
}
