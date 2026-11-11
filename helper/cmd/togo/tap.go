package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"path/filepath"
	"sync"
	"time"

	"github.com/gettogo/togo/helper/internal/rpc"
	"github.com/gettogo/togo/helper/internal/session"
	"github.com/gettogo/togo/helper/internal/state"
	"github.com/gettogo/togo/helper/internal/tap"
	"github.com/gettogo/togo/helper/internal/wire"
)

const (
	tapOutputEvery = time.Second
	tapRedialEvery = 5 * time.Second
	tapIOTimeout   = 2 * time.Second
	tapFlushWait   = 2 * time.Second
	tapQueue       = 64
)

func init() {
	register("tap", func(ctx context.Context, args []string, env map[string]string, stdin io.Reader, _, stderr io.Writer) int {
		return runTap(ctx, args, env, stdin, stderr)
	})
}

// runTap is the process tmux's pipe-pane starts for a session. It always
// reads stdin to the end: a pipe nobody reads would stall the pane. Reporting
// to the daemon is best effort and runs on its own goroutine, so a missing or
// slow daemon costs signals, never output.
func runTap(ctx context.Context, args []string, env map[string]string, stdin io.Reader, stderr io.Writer) int {
	if len(args) != 1 {
		return usageError(stderr, errors.New("tap takes a session id"))
	}
	id := args[0]
	if _, err := session.ValidateID(id); err != nil {
		return usageError(stderr, err)
	}
	sock, err := tapSocketPath(env)
	if err != nil {
		return fail(stderr, err)
	}

	sendCtx, stop := context.WithCancel(ctx)
	defer stop()
	queue := make(chan wire.Signal, tapQueue)
	done := make(chan struct{})
	go func() {
		defer close(done)
		tapSend(sendCtx, sock, queue)
	}()

	// The byte counter lets anchors say where in the pane's output an item
	// happened; without it they still work by line and text.
	counter, err := tap.NewCounter(filepath.Dir(filepath.Dir(sock)), id)
	if err != nil {
		counter = nil
	} else {
		defer counter.Close()
	}

	var sc tap.Scanner
	enqueue := func(s wire.Signal) {
		s.SessionID = id
		select {
		case queue <- s:
		default:
		}
	}
	output := &outputSignals{every: tapOutputEvery, emit: func() { enqueue(wire.Signal{Kind: wire.SignalOutput}) }}
	buf := make([]byte, 32*1024)
	for {
		n, err := stdin.Read(buf)
		if n > 0 {
			if counter != nil {
				counter.Add(n)
			}
			for _, s := range sc.Feed(buf[:n]) {
				enqueue(s)
			}
			output.note(time.Now())
		}
		if err != nil {
			break
		}
	}
	output.stop()
	close(queue)
	select {
	case <-done:
	case <-time.After(tapFlushWait):
	}
	return exitOK
}

// outputSignals reports pane output at most once per interval, and once
// more when an interval in which output came after the report ends: the
// last change of a burst (a dialog closing, the reply drawn as the agent
// goes idle) is the screen the watcher must read, and nothing may follow
// it.
type outputSignals struct {
	every time.Duration
	emit  func()

	mu      sync.Mutex
	last    time.Time
	timer   *time.Timer
	stopped bool
}

func (o *outputSignals) note(now time.Time) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.stopped {
		return
	}
	if since := now.Sub(o.last); since < o.every {
		if o.timer == nil {
			o.timer = time.AfterFunc(o.every-since, o.trail)
		}
		return
	}
	o.last = now
	o.emit()
}

func (o *outputSignals) trail() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.timer = nil
	if o.stopped {
		return
	}
	o.last = time.Now()
	o.emit()
}

// stop drops a trailing report still due; emit is not called afterwards.
func (o *outputSignals) stop() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.stopped = true
	if o.timer != nil {
		o.timer.Stop()
		o.timer = nil
	}
}

func tapSocketPath(env map[string]string) (string, error) {
	home := env["TOGO_HOME"]
	if home == "" {
		var err error
		if home, err = state.Home(); err != nil {
			return "", err
		}
	}
	return filepath.Join(home, "run", "togo.sock"), nil
}

// tapSend delivers queued signals to the daemon until the queue closes. While
// the daemon is unreachable it discards what arrives: a bell minutes old is
// not worth replaying, and the queue must keep draining.
func tapSend(ctx context.Context, sock string, queue <-chan wire.Signal) {
	for {
		conn, err := tapConnect(ctx, sock)
		if err == nil {
			if tapDeliver(conn, queue) {
				return
			}
		}
		wait := time.NewTimer(tapRedialEvery)
	idle:
		for {
			select {
			case _, ok := <-queue:
				if !ok {
					wait.Stop()
					return
				}
			case <-wait.C:
				break idle
			case <-ctx.Done():
				wait.Stop()
				return
			}
		}
	}
}

// tapConnect dials the daemon and completes the hello exchange.
func tapConnect(ctx context.Context, sock string) (net.Conn, error) {
	d := net.Dialer{Timeout: tapIOTimeout}
	conn, err := d.DialContext(ctx, "unix", sock)
	if err != nil {
		return nil, err
	}
	_ = conn.SetDeadline(time.Now().Add(tapIOTimeout))
	hello := `{"jsonrpc":"2.0","id":1,"method":"hello","params":{"protocol":1}}`
	if err := rpc.WriteFrame(conn, []byte(hello)); err != nil {
		conn.Close()
		return nil, err
	}
	if _, err := rpc.ReadFrame(conn); err != nil {
		conn.Close()
		return nil, err
	}
	_ = conn.SetDeadline(time.Time{})
	return conn, nil
}

// tapDeliver writes queued signals to conn. It reports true when the queue
// closed and everything was sent, false when the connection failed.
func tapDeliver(conn net.Conn, queue <-chan wire.Signal) (finished bool) {
	defer conn.Close()
	// Responses carry nothing the tap needs; reading them keeps the daemon's
	// writes from backing up and notices a daemon that went away.
	gone := make(chan struct{})
	go func() {
		defer close(gone)
		for {
			if _, err := rpc.ReadFrame(conn); err != nil {
				return
			}
		}
	}()
	for id := 2; ; id++ {
		select {
		case s, ok := <-queue:
			if !ok {
				return true
			}
			req, err := json.Marshal(map[string]any{
				"jsonrpc": "2.0", "id": id, "method": wire.MethodLocalSignal, "params": s,
			})
			if err != nil {
				continue
			}
			_ = conn.SetWriteDeadline(time.Now().Add(tapIOTimeout))
			if err := rpc.WriteFrame(conn, req); err != nil {
				return false
			}
		case <-gone:
			return false
		}
	}
}
