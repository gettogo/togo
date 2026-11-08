package engine

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/gettogo/togo/helper/internal/agents"
	"github.com/gettogo/togo/helper/internal/harness/source/rpc"
	"github.com/gettogo/togo/helper/internal/harness/spec"
)

// serverReady bounds how long a session start waits for the harness's
// server (Codex app-server) to answer its health check.
var serverReady = 8 * time.Second

// servers are the harness server processes the helper started, one per
// session. Each runs in its own process group, so stopping it stops what
// it spawned.
type servers struct {
	mu   sync.Mutex
	byID map[string]*exec.Cmd
}

// startServer starts the adapter's endpoint server for one session and
// waits for its health check. It runs in the session folder dir (Codex
// 0.167's app-server starts the TUI's thread in its own working
// directory), else the home folder. On failure the process is stopped and
// the harness starts without the endpoint.
func (e *Engine) startServer(a *adapter, sessionID, dir string, ep endpoint, data map[string]any) error {
	e.stopServer(sessionID)
	argv := make([]string, 0, len(a.Endpoint.Server))
	for _, arg := range a.Endpoint.Server {
		r, err := Render(arg, data)
		if err != nil || r == "" {
			return errors.New("server command could not be built")
		}
		argv = append(argv, r)
	}
	ctx, cancel := context.WithTimeout(context.Background(), serverReady)
	defer cancel()
	path := ""
	if e.cfg.LoginPath != nil {
		path = e.cfg.LoginPath(ctx)
	}
	prog, _, ok := agents.Find(argv[0], path, nil)
	if !ok {
		return fmt.Errorf("%s is not installed", argv[0])
	}
	cmd := exec.Command(prog, argv[1:]...)
	if st, err := os.Stat(dir); dir != "" && err == nil && st.IsDir() {
		cmd.Dir = dir
	} else if home := e.cfg.Home; home != "" {
		cmd.Dir = home
	}
	cmd.Env = append(os.Environ(), "PATH="+path)
	if e.cfg.LoginEnv != nil {
		cmd.Env = append(append(os.Environ(), e.cfg.LoginEnv(ctx)...), "PATH="+path)
	}
	if ep.envName != "" {
		// The server checks the password the harness and the helper send.
		cmd.Env = append(cmd.Env, ep.envName+"="+ep.password)
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	e.srv.mu.Lock()
	if e.srv.byID == nil {
		e.srv.byID = map[string]*exec.Cmd{}
	}
	e.srv.byID[sessionID] = cmd
	e.srv.mu.Unlock()
	if err := waitHealthy(ctx, ep, a.Endpoint.Health); err != nil {
		e.stopServer(sessionID)
		return err
	}
	return nil
}

// stopServer stops the session's server process group, if the helper
// started one.
func (e *Engine) stopServer(sessionID string) {
	e.srv.mu.Lock()
	cmd := e.srv.byID[sessionID]
	delete(e.srv.byID, sessionID)
	e.srv.mu.Unlock()
	if cmd != nil && cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	}
}

// waitHealthy polls the endpoint until its health check passes or ctx
// ends.
func waitHealthy(ctx context.Context, ep endpoint, health string) error {
	for {
		err := checkHealth(ctx, ep, health)
		if err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("endpoint not ready: %w", err)
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// checkHealth runs one health check. A unix endpoint speaks JSON-RPC over
// WebSocket, as Codex app-server does, and must answer initialize; an
// http endpoint must answer GET on the health path with a 2xx status.
func checkHealth(ctx context.Context, ep endpoint, health string) error {
	switch ep.kind {
	case spec.EndpointUnix:
		cctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		t, err := rpc.DialUnix(cctx, ep.socket)
		if err != nil {
			return err
		}
		c := rpc.NewConn(t, rpc.Handler{})
		defer c.Close()
		if health == "" {
			return nil
		}
		if health == "initialize" {
			return rpc.Initialize(cctx, c)
		}
		_, err = c.Call(cctx, health, nil)
		return err
	case spec.EndpointHTTP:
		if health == "" || health[0] != '/' {
			health = "/"
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:"+strconv.Itoa(ep.port)+health, nil)
		if err != nil {
			return err
		}
		if a := ep.auth(); a != "" {
			req.Header.Set("Authorization", a)
		}
		res, err := (&http.Client{Timeout: 2 * time.Second, Transport: &http.Transport{Proxy: nil}}).Do(req)
		if err != nil {
			return err
		}
		res.Body.Close()
		if res.StatusCode/100 != 2 {
			return fmt.Errorf("health answered %d", res.StatusCode)
		}
		return nil
	}
	return errors.New("unknown endpoint kind")
}
