package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/rpc"
	"github.com/gettogo/togo/helper/internal/sockd"
)

const helloRequest = `{"jsonrpc":"2.0","id":1,"method":"hello","params":{"protocol":1,"client":{"name":"test","version":"0"},"capabilities":[]}}`

// shortHome returns a TOGO_HOME short enough for a unix socket path on
// macOS, where t.TempDir paths run past the 104-byte limit.
func shortHome(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "tg")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

// pingHandler answers "ping" with "pong" and, for "notify", sends one event
// notification before replying.
type pingHandler struct{}

func (pingHandler) Handle(ctx context.Context, method string, _ json.RawMessage) (any, *rpc.Error) {
	switch method {
	case "ping":
		return "pong", nil
	case "notify":
		if err := rpc.ConnFrom(ctx).Notify("event", map[string]any{"kind": "notify"}); err != nil {
			return nil, &rpc.Error{Code: -32603, Message: err.Error()}
		}
		return "sent", nil
	}
	return nil, &rpc.Error{Code: -32601, Message: "method not found"}
}

func buildPing(context.Context) (rpc.Handler, error) { return pingHandler{}, nil }

var testInfo = rpc.HelperInfo{Version: "test", Capabilities: []string{"session.v1"}}

// startDaemon runs a daemon on home until the test ends and waits until its
// socket accepts connections. done is closed when Run returns, after which
// *runErr holds its result.
func startDaemon(t *testing.T, home string) (cancel func(), done <-chan struct{}, runErr *error) {
	t.Helper()
	ctx, cancelFn := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	var result error
	go func() {
		defer close(stopped)
		result = Run(ctx, Config{Home: home, Build: buildPing, Info: testInfo, Logf: t.Logf})
	}()
	t.Cleanup(func() {
		cancelFn()
		select {
		case <-stopped:
		case <-time.After(5 * time.Second):
			t.Error("daemon did not stop")
		}
	})
	deadline := time.Now().Add(2 * time.Second)
	for {
		c, err := sockd.Dial(context.Background(), SocketPath(home))
		if err == nil {
			c.Close()
			return cancelFn, stopped, &result
		}
		if time.Now().After(deadline) {
			t.Fatalf("daemon socket never came up: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func call(t *testing.T, c net.Conn, req string) map[string]any {
	t.Helper()
	if err := rpc.WriteFrame(c, []byte(req)); err != nil {
		t.Fatal(err)
	}
	return readMessage(t, c)
}

func readMessage(t *testing.T, c net.Conn) map[string]any {
	t.Helper()
	c.SetReadDeadline(time.Now().Add(5 * time.Second))
	payload, err := rpc.ReadFrame(c)
	if err != nil {
		t.Fatalf("read frame: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(payload, &m); err != nil {
		t.Fatalf("frame %s: %v", payload, err)
	}
	return m
}

func TestDaemonAnswersHelloOverSocket(t *testing.T) {
	home := shortHome(t)
	startDaemon(t, home)
	c, err := sockd.Dial(context.Background(), SocketPath(home))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	reply := call(t, c, helloRequest)
	result, _ := reply["result"].(map[string]any)
	if result == nil || result["protocol"] != float64(1) {
		t.Fatalf("hello reply = %v, want protocol 1", reply)
	}
	if got := call(t, c, `{"jsonrpc":"2.0","id":2,"method":"ping"}`); got["result"] != "pong" {
		t.Errorf("ping reply = %v, want pong", got)
	}
}

func TestSecondDaemonExitsWithLockError(t *testing.T) {
	home := shortHome(t)
	startDaemon(t, home)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := Run(ctx, Config{Home: home, Build: buildPing, Info: testInfo})
	if !errors.Is(err, ErrRunning) {
		t.Fatalf("second Run = %v, want ErrRunning", err)
	}
	c, err := sockd.Dial(context.Background(), SocketPath(home))
	if err != nil {
		t.Fatalf("first daemon's socket gone after the second exited: %v", err)
	}
	c.Close()
}

func TestDaemonRemovesSocketOnShutdown(t *testing.T) {
	home := shortHome(t)
	cancel, done, runErr := startDaemon(t, home)
	open, err := sockd.Dial(context.Background(), SocketPath(home))
	if err != nil {
		t.Fatal(err)
	}
	defer open.Close()
	call(t, open, helloRequest)

	cancel()
	select {
	case <-done:
		if *runErr != nil {
			t.Fatalf("Run = %v, want nil after cancel", *runErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("daemon did not stop with a connection open")
	}
	if _, err := os.Lstat(SocketPath(home)); !os.IsNotExist(err) {
		t.Errorf("socket still present after shutdown (lstat: %v)", err)
	}
	open.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := rpc.ReadFrame(open); err == nil {
		t.Error("open connection still readable after shutdown")
	}
}

func TestDaemonAnswersHelloWhileHandlerBuilds(t *testing.T) {
	home := shortHome(t)
	release := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, Config{Home: home, Info: testInfo, Build: func(context.Context) (rpc.Handler, error) {
			<-release
			return pingHandler{}, nil
		}})
	}()
	defer func() {
		cancel()
		<-done
	}()
	var c net.Conn
	deadline := time.Now().Add(2 * time.Second)
	for {
		var err error
		if c, err = sockd.Dial(context.Background(), SocketPath(home)); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("socket not bound while the handler builds: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	defer c.Close()
	if reply := call(t, c, helloRequest); reply["result"] == nil {
		t.Fatalf("hello reply = %v, want a result before the handler is built", reply)
	}
	if err := rpc.WriteFrame(c, []byte(`{"jsonrpc":"2.0","id":2,"method":"ping"}`)); err != nil {
		t.Fatal(err)
	}
	close(release)
	if got := readMessage(t, c); got["result"] != "pong" {
		t.Errorf("ping reply = %v, want pong once built", got)
	}
}

func TestDaemonStopsWhenBuildFails(t *testing.T) {
	home := shortHome(t)
	boom := errors.New("no tmux")
	done := make(chan error, 1)
	go func() {
		done <- Run(context.Background(), Config{Home: home, Info: testInfo, Build: func(context.Context) (rpc.Handler, error) {
			return nil, boom
		}})
	}()
	select {
	case err := <-done:
		if !errors.Is(err, boom) {
			t.Fatalf("Run = %v, want the build error", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("daemon kept running after its build failed")
	}
	if _, err := os.Lstat(SocketPath(home)); !os.IsNotExist(err) {
		t.Errorf("socket left behind (lstat: %v)", err)
	}
}

func TestBuildFailureFailsWaitingCall(t *testing.T) {
	home := shortHome(t)
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- Run(context.Background(), Config{Home: home, Info: testInfo, Build: func(context.Context) (rpc.Handler, error) {
			<-release
			return nil, errors.New("no tmux")
		}})
	}()
	var c net.Conn
	deadline := time.Now().Add(2 * time.Second)
	for {
		var err error
		if c, err = sockd.Dial(context.Background(), SocketPath(home)); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("socket not bound while the handler builds: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	defer c.Close()
	call(t, c, helloRequest)
	if err := rpc.WriteFrame(c, []byte(`{"jsonrpc":"2.0","id":2,"method":"ping"}`)); err != nil {
		t.Fatal(err)
	}
	// Let the call reach the handler before the build fails.
	time.Sleep(50 * time.Millisecond)
	close(release)
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Run = nil, want the build error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("daemon hung on a call waiting for the failed build")
	}
}
