package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/daemon"
	"github.com/gettogo/togo/helper/internal/rpc"
	"github.com/gettogo/togo/helper/internal/sockd"
	"github.com/gettogo/togo/helper/internal/wire"
)

// A fast service handoff need not leave an unlocked sample for the CLI.
// Hold the lock across two socket connections: shutdown belongs to the first,
// and subsequent connections reach the replacement.
func TestRestartAcceptsReplacementWithoutUnlockedSample(t *testing.T) {
	home := shortHome(t)
	t.Setenv("HOME", t.TempDir())
	release, err := sockd.Lock(daemon.LockPath(home))
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ln, err := sockd.Listen(daemon.SocketPath(home))
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var connections atomic.Int32
	var shutdowns atomic.Int32
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			first := connections.Add(1) == 1
			go func() {
				defer c.Close()
				var output io.Writer = c
				if first {
					output = &closeShutdownReply{Conn: c}
				}
				_ = rpc.Serve(ctx, c, output, handoffHandler{shutdowns: &shutdowns}, rpc.HelperInfo{Version: "v1.0.4"})
			}()
		}
	}()
	old := restartExitWait
	restartExitWait = 200 * time.Millisecond
	t.Cleanup(func() { restartExitWait = old })
	var out bytes.Buffer
	if err := restartDaemon(ctx, home, wire.ShutdownRestart, false, &out); err != nil {
		t.Fatalf("restart reported failure after replacement: %v", err)
	}
	if out.String() != "Restarted\n" || shutdowns.Load() != 1 || !daemonRunning(home) {
		t.Fatalf("stdout=%q shutdowns=%d replacement running=%t", out.String(), shutdowns.Load(), daemonRunning(home))
	}
}

type handoffHandler struct{ shutdowns *atomic.Int32 }

func (h handoffHandler) Handle(_ context.Context, method string, _ json.RawMessage) (any, *rpc.Error) {
	if method == wire.MethodLocalShutdown {
		h.shutdowns.Add(1)
		return wire.ShutdownResult{}, nil
	}
	return "pong", nil
}

// Close only after the complete shutdown reply has been written, as Run does.
type closeShutdownReply struct{ net.Conn }

func (w *closeShutdownReply) Write(p []byte) (int, error) {
	n, err := w.Conn.Write(p)
	var reply struct {
		ID     int                  `json:"id"`
		Result *wire.ShutdownResult `json:"result"`
	}
	if err == nil && len(p) > 4 && json.Unmarshal(p[4:], &reply) == nil && reply.ID == 2 && reply.Result != nil {
		_ = w.Conn.Close()
	}
	return n, err
}
