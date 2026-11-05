package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/rpc"
	"github.com/gettogo/togo/helper/internal/sockd"
	"github.com/gettogo/togo/helper/internal/wire"
)

// askHandler blocks local.ask until release is closed and answers "yes".
type askHandler struct{ release chan struct{} }

func (h askHandler) Handle(ctx context.Context, method string, _ json.RawMessage) (any, *rpc.Error) {
	switch method {
	case wire.MethodLocalAsk:
		select {
		case <-h.release:
			d := "yes"
			return wire.AskResult{Decision: &d, Reason: wire.AskAnswered}, nil
		case <-ctx.Done():
			return nil, rpc.NewError(rpc.CodeInternal, nil)
		}
	case "ping":
		return "pong", nil
	}
	return nil, &rpc.Error{Code: -32601, Message: "method not found"}
}

// runShutdownDaemon runs a daemon with d until the test ends and returns
// a channel that receives Run's result.
func runShutdownDaemon(t *testing.T, home string, d *Daemon, h rpc.Handler) <-chan error {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	info := testInfo
	info.Concurrent = []string{wire.MethodLocalAsk}
	go func() {
		result <- Run(ctx, Config{
			Home: home, Daemon: d, Info: info, Logf: t.Logf,
			Build: func(context.Context) (rpc.Handler, error) { return h, nil },
		})
	}()
	t.Cleanup(cancel)
	deadline := time.Now().Add(2 * time.Second)
	for {
		c, err := sockd.Dial(context.Background(), SocketPath(home))
		if err == nil {
			c.Close()
			return result
		}
		if time.Now().After(deadline) {
			t.Fatalf("daemon socket never came up: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func requestShutdown(t *testing.T, home string) wire.ShutdownResult {
	t.Helper()
	res, err := RequestShutdown(context.Background(), SocketPath(home), wire.ShutdownParams{Reason: wire.ShutdownUpgrade, When: wire.ShutdownIdle})
	if err != nil {
		t.Fatalf("local.shutdown: %v", err)
	}
	return res
}

func waitExit(t *testing.T, result <-chan error, within time.Duration) {
	t.Helper()
	select {
	case err := <-result:
		if !errors.Is(err, ErrShutdown) {
			t.Fatalf("Run = %v, want ErrShutdown", err)
		}
	case <-time.After(within):
		t.Fatalf("daemon still running %v after it became idle", within)
	}
}

func stillRunning(t *testing.T, result <-chan error, d time.Duration) {
	t.Helper()
	select {
	case err := <-result:
		t.Fatalf("daemon exited with %v while something was pending", err)
	case <-time.After(d):
	}
}

func TestShutdownIdleExitsNow(t *testing.T) {
	home := shortHome(t)
	d := New()
	result := runShutdownDaemon(t, home, d, pingHandler{})
	if res := requestShutdown(t, home); res.Deferred || res.Pending != 0 {
		t.Fatalf("result = %+v, want an immediate exit", res)
	}
	waitExit(t, result, 2*time.Second)
	if _, err := sockd.Dial(context.Background(), SocketPath(home)); err == nil {
		t.Error("socket still accepts after the daemon exited")
	}
}

func TestShutdownWaitsForPendingApproval(t *testing.T) {
	home := shortHome(t)
	d := New()
	var open atomic.Int32
	open.Store(1)
	d.SetPending(func() int { return int(open.Load()) })
	result := runShutdownDaemon(t, home, d, pingHandler{})

	if res := requestShutdown(t, home); !res.Deferred || res.Pending != 1 {
		t.Fatalf("result = %+v, want Deferred with 1 pending", res)
	}
	// Asking again changes nothing and reports the same wait.
	if res := requestShutdown(t, home); !res.Deferred || res.Pending != 1 {
		t.Fatalf("second result = %+v, want Deferred with 1 pending", res)
	}
	stillRunning(t, result, 4*settle)
	open.Store(0) // the approval is answered
	waitExit(t, result, 2*time.Second)
	if d.Requests() != 2 {
		t.Errorf("requests = %d, want 2", d.Requests())
	}
}

func TestShutdownWaitsForOpenAsk(t *testing.T) {
	home := shortHome(t)
	d := New()
	h := askHandler{release: make(chan struct{})}
	result := runShutdownDaemon(t, home, d, h)

	asker, err := sockd.Dial(context.Background(), SocketPath(home))
	if err != nil {
		t.Fatal(err)
	}
	defer asker.Close()
	call(t, asker, helloRequest)
	if err := rpc.WriteFrame(asker, []byte(`{"jsonrpc":"2.0","id":2,"method":"local.ask","params":{"title":"Deploy?","options":["yes","no"],"timeoutSec":60}}`)); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return d.Pending() == 1 })

	if res := requestShutdown(t, home); !res.Deferred || res.Pending != 1 {
		t.Fatalf("result = %+v, want Deferred with 1 pending", res)
	}
	stillRunning(t, result, 4*settle)
	close(h.release)
	// The asker gets its answer before the daemon goes.
	reply := readMessage(t, asker)
	if r, _ := reply["result"].(map[string]any); r == nil || r["decision"] != "yes" {
		t.Fatalf("ask reply = %v, want the answer", reply)
	}
	waitExit(t, result, 2*time.Second)
}

func TestShutdownRejectsOtherModes(t *testing.T) {
	home := shortHome(t)
	d := New()
	result := runShutdownDaemon(t, home, d, pingHandler{})
	_, err := RequestShutdown(context.Background(), SocketPath(home), wire.ShutdownParams{Reason: "test", When: "now"})
	if err == nil {
		t.Fatal("when=now accepted")
	}
	stillRunning(t, result, 2*settle)
	if d.Requests() != 0 {
		t.Errorf("requests = %d, want 0", d.Requests())
	}
}

func TestConnectVersionReportsDaemonVersion(t *testing.T) {
	home := shortHome(t)
	startDaemon(t, home)
	c, v, err := ConnectVersion(context.Background(), SocketPath(home), func() error { return nil }, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
	if v != testInfo.Version {
		t.Errorf("version = %q, want %q", v, testInfo.Version)
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met within 2s")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
