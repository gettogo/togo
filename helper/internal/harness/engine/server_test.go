package engine

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/gettogo/togo/helper/internal/harness"
)

func init() {
	harness.RegisterAdapterFile("fakesrv", []byte(`
adapter: fakesrv
name: Fake server
revision: 1
match: { agents: [fakesrv] }
endpoint:
  kind: unix
  server: [fakeappserver, '-test.run=^TestFakeAppServerProcess$', 'unix://{{.socket}}']
  launch_args: [--remote, 'unix://{{.socket}}']
  health: initialize
capabilities: [chat]
`))
}

// TestFakeAppServerProcess is the fake harness server the tests start: a
// WebSocket JSON-RPC server on the unix socket named by its last argument
// that answers initialize. It does nothing in a normal test run.
func TestFakeAppServerProcess(t *testing.T) {
	if os.Getenv("TOGO_FAKE_APPSERVER") == "" {
		return
	}
	path := strings.TrimPrefix(os.Args[len(os.Args)-1], "unix://")
	if mode := os.Getenv("TOGO_FAKE_APPSERVER"); mode == "dead" {
		os.Exit(3)
	}
	l, err := net.Listen("unix", path)
	if err != nil {
		os.Exit(2)
	}
	_ = http.Serve(l, http.HandlerFunc(serveInitialize))
}

func serveInitialize(w http.ResponseWriter, r *http.Request) {
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer c.CloseNow()
	for {
		_, b, err := c.Read(r.Context())
		if err != nil {
			return
		}
		var m struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if json.Unmarshal(b, &m) != nil || len(m.ID) == 0 {
			continue
		}
		out, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": map[string]string{"userAgent": "fake"}})
		_ = c.Write(r.Context(), websocket.MessageText, out)
	}
}

func fakeServerEngine(t *testing.T, mode string) *Engine {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	if err := os.Symlink(exe, filepath.Join(bin, "fakeappserver")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TOGO_FAKE_APPSERVER", mode)
	// Unix socket paths are short: keep TOGO_HOME under /tmp.
	home, err := os.MkdirTemp("/tmp", "tgs")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(home) })
	return New(Config{TogoHome: home, LoginPath: func(context.Context) string { return bin }})
}

// The helper starts the harness's server, waits until it answers
// initialize over WebSocket, then hands the TUI its socket; forgetting the
// session stops the server.
func TestEndpointServerStartsAndStops(t *testing.T) {
	e := fakeServerEngine(t, "serve")
	args := e.EndpointArgs("fakesrv", "s_abcdefghij")
	if len(args) != 2 || args[0] != "--remote" || !strings.HasPrefix(args[1], "unix://") {
		t.Fatalf("args = %v", args)
	}
	ep, ok := e.endpointFor("s_abcdefghij")
	if !ok || checkHealth(context.Background(), ep, "initialize") != nil {
		t.Fatalf("server not healthy: %+v", ep)
	}
	e.srv.mu.Lock()
	cmd := e.srv.byID["s_abcdefghij"]
	e.srv.mu.Unlock()
	if cmd == nil {
		t.Fatal("no server recorded")
	}
	e.Forget("s_abcdefghij")
	for deadline := time.Now().Add(5 * time.Second); syscall.Kill(cmd.Process.Pid, 0) == nil; {
		if time.Now().After(deadline) {
			t.Fatal("server still running after Forget")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, ok := e.endpointFor("s_abcdefghij"); ok {
		t.Fatal("endpoint kept after Forget")
	}
}

// A server that never becomes healthy leaves the harness to start on its
// own: no endpoint arguments.
func TestEndpointServerFailureFallsBack(t *testing.T) {
	prev := serverReady
	serverReady = time.Second
	t.Cleanup(func() { serverReady = prev })
	e := fakeServerEngine(t, "dead")
	if args := e.EndpointArgs("fakesrv", "s_abcdefghij"); args != nil {
		t.Fatalf("args for a dead server: %v", args)
	}
	if _, ok := e.endpointFor("s_abcdefghij"); ok {
		t.Fatal("endpoint recorded for a dead server")
	}
}

func TestHealthCheckHTTP(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ok" {
			w.WriteHeader(http.StatusNotFound)
		}
	})}
	go srv.Serve(l)
	t.Cleanup(func() { srv.Close() })
	ep := endpoint{kind: "http", port: l.Addr().(*net.TCPAddr).Port}
	if err := checkHealth(context.Background(), ep, "/ok"); err != nil {
		t.Fatal(err)
	}
	if err := checkHealth(context.Background(), ep, "/missing"); err == nil {
		t.Fatal("404 counted as healthy")
	}
	if err := checkHealth(context.Background(), endpoint{kind: "unix", socket: "/nonexistent.sock"}, "initialize"); err == nil || errors.Is(err, context.Canceled) {
		t.Fatalf("missing socket: %v", err)
	}
}

func init() {
	harness.RegisterAdapterFile("fakepw", []byte(`
adapter: fakepw
name: Fake password
revision: 1
match: { agents: [fakepw] }
endpoint: { kind: http, launch_args: [--port, '{{.port}}'], password_env: FAKE_SERVER_PASSWORD, username: fake }
capabilities: [chat]
`))
}

// A harness that takes a server password gets a fresh one in its
// environment on every start, and the helper's requests carry it.
func TestEndpointPassword(t *testing.T) {
	e := New(Config{TogoHome: t.TempDir()})
	if e.EndpointArgs("fakepw", "s_abcdefghij") == nil {
		t.Fatal("no args")
	}
	env := e.EndpointEnv("s_abcdefghij")
	pw := env["FAKE_SERVER_PASSWORD"]
	if len(pw) != 32 {
		t.Fatalf("env %v", env)
	}
	ep, _ := e.endpointFor("s_abcdefghij")
	if ep.auth() == "" || !strings.HasPrefix(ep.auth(), "Basic ") {
		t.Fatalf("auth %q", ep.auth())
	}
	e.EndpointArgs("fakepw", "s_abcdefghij")
	if again := e.EndpointEnv("s_abcdefghij")["FAKE_SERVER_PASSWORD"]; again == pw {
		t.Fatal("password reused across starts")
	}
	if e.EndpointEnv("s_none000000") != nil {
		t.Fatal("env for a session with no endpoint")
	}
}
