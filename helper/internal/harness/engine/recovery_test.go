package engine

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/gettogo/togo/helper/internal/ports"
	"github.com/gettogo/togo/helper/internal/session"
)

func TestNewEngineRecoversTheExistingServer(t *testing.T) {
	const id = "s_abcdefghij"
	e := fakeServerEngine(t, "serve")
	if e.EndpointArgs("fakesrv", id, "") == nil {
		t.Fatal("server did not start")
	}
	t.Cleanup(func() { e.Forget(id) })
	before, _ := e.endpointFor(id)
	next := New(e.cfg)
	restored, ok := next.endpointFor(id)
	if !ok {
		t.Fatal("new helper lost the running server's endpoint")
	}
	if restored.url() != before.url() || checkHealth(context.Background(), restored, "initialize") != nil {
		t.Fatal("new helper did not reconnect to the existing server")
	}
	e.srv.mu.Lock()
	pid := e.srv.byID[id].Process.Pid
	e.srv.mu.Unlock()
	next.Forget(id)
	waitFor(t, func() bool { return syscall.Kill(pid, 0) != nil })
	if _, err := os.Stat(before.socket); !os.IsNotExist(err) {
		t.Fatal("restored server socket was not cleaned up")
	}
}

func TestNewEngineKeepsTheExistingHTTPPassword(t *testing.T) {
	const id = "s_abcdefghij"
	home := t.TempDir()
	var port int
	cfg := Config{TogoHome: home, Sessions: fakeSessions{id: session.Session{ID: id, Agent: "fakepw", Managed: true}},
		Ports: func(context.Context, string) ([]ports.Listener, error) {
			return []ports.Listener{{Port: port, PID: os.Getpid()}}, nil
		}}
	e := New(cfg)
	if e.EndpointArgs("fakepw", id, "") == nil {
		t.Fatal("endpoint not allocated")
	}
	t.Cleanup(func() { e.Forget(id) })
	before, _ := e.endpointFor(id)
	port = before.port
	listener, err := net.Listen("tcp", before.url()[len("http://"):])
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != before.auth() {
			w.WriteHeader(http.StatusUnauthorized)
		}
	})}
	go server.Serve(listener)
	t.Cleanup(func() { server.Close() })
	next := New(cfg)
	restored, ok := next.endpointFor(id)
	if !ok || restored.url() != before.url() || restored.auth() != before.auth() {
		t.Fatal("new helper replaced or lost the running instance's credentials")
	}
	if err := checkHealth(context.Background(), restored, "/"); err != nil {
		t.Fatal("restored credentials did not reach the existing HTTP server")
	}
	files, _ := filepath.Glob(filepath.Join(home, "run", "harness", "*.json"))
	if len(files) != 1 {
		t.Fatal("missing private endpoint record")
	}
	info, err := os.Stat(files[0])
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatal("endpoint credentials were not stored privately")
	}
}
