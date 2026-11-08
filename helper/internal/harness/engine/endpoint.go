package engine

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/source"
	"github.com/gettogo/togo/helper/internal/harness/spec"
	"github.com/gettogo/togo/helper/internal/session"
)

// maxSocketPath keeps a unix socket path within sun_path (104 bytes on
// macOS, with room for the terminator).
const maxSocketPath = 100

// endpoints holds the address the helper picked for each session's live
// harness API: a loopback port or a unix socket under TOGO_HOME. Addresses
// never come from adapter files.
type endpoints struct {
	mu   sync.Mutex
	byID map[string]endpoint
}

type endpoint struct {
	kind   string
	port   int
	socket string
	// envName and password are the harness's server password, when its
	// adapter declares one; user is the Basic auth user name.
	envName, password, user string
}

// auth is the Authorization header the helper sends, or "".
func (ep endpoint) auth() string {
	if ep.password == "" {
		return ""
	}
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(ep.user+":"+ep.password))
}

// newPassword is a random server password, 32 hex characters.
func newPassword() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

func (ep endpoint) url() string {
	switch ep.kind {
	case spec.EndpointHTTP:
		return "http://127.0.0.1:" + strconv.Itoa(ep.port)
	case spec.EndpointUnix:
		return "unix://" + ep.socket
	}
	return ""
}

// EndpointArgs renders the launch args of the adapter serving agentID for
// one run of the session, picking its port or socket. It suits
// session.EndpointArgs; it returns nil when the adapter declares no
// endpoint or the address cannot be picked.
func (e *Engine) EndpointArgs(agentID, sessionID, dir string) []string {
	if _, err := session.ValidateID(sessionID); err != nil {
		return nil
	}
	a := e.adapterFor(agentID, nil, e.gateEnv())
	if a == nil || a.Endpoint == nil {
		return nil
	}
	if !e.versionFits(a) {
		// Launch flags belong to the versions the adapter knows: OpenCode
		// 2.x refuses 1.x's --port and exits (seen live after it updated
		// itself), so a harness outside the range starts plain.
		return nil
	}
	ep := endpoint{kind: a.Endpoint.Kind}
	switch ep.kind {
	case spec.EndpointHTTP:
		port, err := freePort()
		if err != nil {
			e.cfg.Logf("harness: no free port for %s: %v", a.Adapter.Adapter, err)
			return nil
		}
		ep.port = port
		if name := a.Endpoint.PasswordEnv; name != "" {
			pw, err := newPassword()
			if err != nil {
				return nil
			}
			ep.envName, ep.password, ep.user = name, pw, a.Endpoint.Username
		}
	case spec.EndpointUnix:
		if e.cfg.TogoHome == "" {
			return nil
		}
		dir := filepath.Join(e.cfg.TogoHome, "run", "harness")
		if err := os.MkdirAll(dir, 0o700); err != nil || os.Chmod(dir, 0o700) != nil {
			return nil
		}
		ep.socket = filepath.Join(dir, sessionID+".sock")
		if len(ep.socket) > maxSocketPath {
			e.cfg.Logf("harness: socket path too long for %s", a.Adapter.Adapter)
			return nil
		}
		_ = os.Remove(ep.socket)
	default:
		return nil
	}
	data := map[string]any{"port": strconv.Itoa(ep.port), "socket": ep.socket, "session": sessionID}
	out := make([]string, 0, len(a.Endpoint.LaunchArgs))
	for _, arg := range a.Endpoint.LaunchArgs {
		r, err := Render(arg, data)
		if err != nil {
			return nil
		}
		out = append(out, r)
	}
	if len(a.Endpoint.Server) > 0 {
		// The harness joins a server the helper starts first (Codex
		// app-server); without a healthy server it starts on its own.
		if err := e.startServer(a, sessionID, dir, ep, data); err != nil {
			e.cfg.Logf("harness: %s server for %s: %v", a.Adapter.Adapter, sessionID, err)
			return nil
		}
	}
	e.eps.mu.Lock()
	if e.eps.byID == nil {
		e.eps.byID = map[string]endpoint{}
	}
	e.eps.byID[sessionID] = ep
	e.eps.mu.Unlock()
	e.restart(sessionID)
	return out
}

// restart drops the session's adapter state after its harness got a new
// endpoint (a relaunch: OpenCode 2.x resumes by restarting, on a new port
// and password). The next request starts it again on the new address, its
// feeds and mapper reading the new run; open chats are closed and reopen.
func (e *Engine) restart(id string) {
	e.mu.Lock()
	s, ok := e.sessions[id]
	delete(e.sessions, id)
	e.mu.Unlock()
	if ok {
		s.stop()
	}
}

// endpointFor returns the address picked for a session, if any.
func (e *Engine) endpointFor(id string) (endpoint, bool) {
	e.eps.mu.Lock()
	defer e.eps.mu.Unlock()
	ep, ok := e.eps.byID[id]
	return ep, ok
}

// freePort asks the kernel for a free loopback port.
func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// EndpointEnv is the environment the harness needs for the endpoint
// EndpointArgs last picked for the session: its server password. It suits
// session.EndpointEnv.
func (e *Engine) EndpointEnv(sessionID string) map[string]string {
	ep, ok := e.endpointFor(sessionID)
	if !ok || ep.envName == "" {
		return nil
	}
	return map[string]string{ep.envName: ep.password}
}

// versionFits reports whether the installed harness is inside the
// adapter's version range. Only a version read and found outside it
// counts against it: a probe that could not run (a timeout, no login
// PATH yet) keeps the flags.
func (e *Engine) versionFits(a *adapter) bool {
	r := e.gate.check(context.Background(), a, e.gateEnv())
	return r.ok || !strings.HasPrefix(r.reason, "version ")
}

// gateEnv is the environment version commands run in.
func (e *Engine) gateEnv() harness.Env {
	env := harness.Env{Vars: map[string]string{}}
	if e.cfg.LoginPath != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		env.Vars[source.VarPath] = e.cfg.LoginPath(ctx)
		cancel()
	}
	if e.cfg.LoginEnv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		env.Environ = e.cfg.LoginEnv(ctx)
		cancel()
	}
	return env
}

// adapterFor picks the adapter serving a session. When several match (one
// per major version of a harness, as OpenCode 1.x and 2.x), the first whose
// version range holds the installed harness wins; with none, or no version
// read, the first by id.
func (e *Engine) adapterFor(agent string, argv []string, env harness.Env) *adapter {
	var found []*adapter
	for _, id := range sortedIDs(e.adapters) {
		if a := e.adapters[id]; a.matches(agent, argv) {
			found = append(found, a)
		}
	}
	if len(found) == 0 {
		return nil
	}
	if len(found) > 1 {
		for _, a := range found {
			if r := e.gate.check(context.Background(), a, env); r.ok {
				return a
			}
		}
	}
	return found[0]
}
