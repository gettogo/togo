package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/source"
	"github.com/gettogo/togo/helper/internal/harness/spec"
	"github.com/gettogo/togo/helper/internal/session"
)

// Only private runtime metadata crosses a helper-process replacement.
// Passwords never appear in errors, log messages or process arguments.
type endpointRecord struct {
	Version        int              `json:"version"`
	ID             string           `json:"id"`
	Generation     string           `json:"generation"`
	Adapter        string           `json:"adapter"`
	HarnessVersion string           `json:"harnessVersion,omitempty"`
	Dir            string           `json:"cwd"`
	LaunchedAt     time.Time        `json:"launchedAt"`
	CreatedAt      time.Time        `json:"createdAt,omitempty"`
	Kind           string           `json:"kind"`
	Port           int              `json:"port,omitempty"`
	Socket         string           `json:"socket,omitempty"`
	SocketKey      string           `json:"socketKey,omitempty"`
	EnvName        string           `json:"envName,omitempty"`
	Password       string           `json:"password,omitempty"`
	User           string           `json:"user,omitempty"`
	Server         *processIdentity `json:"server,omitempty"`
	Listener       *processIdentity `json:"listener,omitempty"`
	Argv           []string         `json:"argv,omitempty"`
}

func (e *Engine) recoveryPath(id string) string {
	if e.cfg.TogoHome == "" {
		return ""
	}
	if _, err := session.ValidateID(id); err != nil {
		return ""
	}
	return filepath.Join(e.cfg.TogoHome, "run", "harness", id+".json")
}

func (r endpointRecord) endpoint() endpoint {
	return endpoint{kind: r.Kind, port: r.Port, socket: r.Socket, envName: r.EnvName, password: r.Password,
		user: r.User, adapter: r.Adapter, version: r.HarnessVersion, generation: r.Generation, recovered: true}
}

func fileIdentity(path string) string {
	info, err := os.Lstat(path)
	if err != nil {
		return ""
	}
	key := func(info os.FileInfo) string {
		if stat, ok := info.Sys().(*syscall.Stat_t); ok {
			return fmt.Sprintf("%d:%d", stat.Dev, stat.Ino)
		}
		return ""
	}
	if info.Mode()&os.ModeSocket != 0 {
		return key(info)
	}
	// Codex exposes its socket through a symlink. Bind both the link
	// itself and its socket target, and only ever unlink our canonical
	// path: never remove the target outside Togo's runtime directory.
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Stat(path)
		if err == nil && target.Mode()&os.ModeSocket != 0 {
			return key(info) + ":" + key(target)
		}
	}
	return ""
}

func (e *Engine) writeEndpointRecord(record endpointRecord) error {
	path := e.recoveryPath(record.ID)
	if path == "" {
		return nil // Memory-only engines used without a configured Togo home.
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return err
	}
	b, err := json.Marshal(record)
	if err != nil {
		return err
	}
	if len(b) > 16<<10 {
		return errors.New("endpoint record is too large")
	}
	f, err := os.CreateTemp(dir, ".endpoint-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func (e *Engine) readEndpointRecord(id string) (endpointRecord, error) {
	path := e.recoveryPath(id)
	var record endpointRecord
	if path == "" {
		return record, os.ErrNotExist
	}
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return record, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return record, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 || info.Size() > 16<<10 {
		return record, errors.New("invalid endpoint record permissions or size")
	}
	if err := json.NewDecoder(io.LimitReader(f, 16<<10)).Decode(&record); err != nil {
		return record, errors.New("invalid endpoint record")
	}
	a := e.adapters[record.Adapter]
	if record.Version != 1 || record.ID != id || record.Generation == "" || a == nil || a.Endpoint == nil || a.Endpoint.Kind != record.Kind {
		return record, errors.New("endpoint record no longer matches its adapter")
	}
	if record.EnvName != a.Endpoint.PasswordEnv || record.User != a.Endpoint.Username {
		return record, errors.New("endpoint authentication no longer matches its adapter")
	}
	if record.Kind == spec.EndpointUnix && record.Socket != filepath.Join(filepath.Dir(path), id+".sock") {
		return record, errors.New("endpoint socket is outside its owned path")
	}
	if record.Kind == spec.EndpointHTTP && (record.Port < 1 || record.Port > 65535) {
		return record, errors.New("invalid endpoint port")
	}
	return record, nil
}

func (e *Engine) listenerIdentity(ctx context.Context, id string, port int) (*processIdentity, error) {
	if e.cfg.Ports == nil {
		return nil, errors.New("session listener ownership is unavailable")
	}
	listeners, err := e.cfg.Ports(ctx, id)
	if err != nil {
		return nil, err
	}
	for _, listener := range listeners {
		if listener.Port == port {
			owner, err := identifyProcess(listener.PID)
			return &owner, err
		}
	}
	return nil, errors.New("endpoint is not owned by the session")
}

// canonicalEndpointDir keeps aliases such as /tmp and /private/tmp from
// changing endpoint ownership when tmux reports the physical directory.
func canonicalEndpointDir(dir string) (string, error) {
	if dir == "" {
		return "", nil
	}
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", err
	}
	return filepath.Abs(resolved)
}

func (e *Engine) saveEndpoint(id, dir string, ep endpoint) error {
	canonical, err := canonicalEndpointDir(dir)
	if err != nil {
		return fmt.Errorf("endpoint working directory: %w", err)
	}
	dir = canonical
	record := endpointRecord{Version: 1, ID: id, Generation: ep.generation, Adapter: ep.adapter, HarnessVersion: ep.version,
		Dir: dir, LaunchedAt: e.cfg.Now(), Kind: ep.kind, Port: ep.port, Socket: ep.socket, SocketKey: fileIdentity(ep.socket),
		EnvName: ep.envName, Password: ep.password, User: ep.user}
	e.srv.mu.Lock()
	if owner, ok := e.srv.owners[id]; ok {
		record.Server = &owner
	}
	e.srv.mu.Unlock()
	e.eps.mu.Lock()
	if e.eps.relaunching[id] {
		record.Argv = append([]string(nil), e.eps.argv[id]...)
	}
	e.eps.mu.Unlock()
	return e.writeEndpointRecord(record)
}

func (e *Engine) restoreEndpoint(id string) (endpoint, bool) {
	record, err := e.readEndpointRecord(id)
	if err != nil {
		return endpoint{}, false
	}
	canonical, err := canonicalEndpointDir(record.Dir)
	if err != nil {
		return endpoint{}, false
	}
	record.Dir = canonical
	ctx, cancel := context.WithTimeout(context.Background(), serverReady)
	defer cancel()
	a := e.adapters[record.Adapter]
	if e.cfg.Sessions != nil {
		info, err := e.cfg.Sessions.Resolve(ctx, id)
		if err != nil {
			return endpoint{}, false
		}
		cwd, err := canonicalEndpointDir(source.Expand(info.Cwd, e.cfg.Home))
		if err != nil || !info.Managed || !a.matches(info.Agent, info.Command) ||
			cwd != record.Dir ||
			(!record.CreatedAt.IsZero() && !record.CreatedAt.Equal(info.CreatedAt)) || info.CreatedAt.After(record.LaunchedAt) {
			return endpoint{}, false
		}
		record.CreatedAt = info.CreatedAt
	} else if record.Server == nil {
		return endpoint{}, false
	}
	if record.Server != nil {
		if !record.Server.ownsGroup() {
			return endpoint{}, false
		}
	} else {
		owner, err := e.listenerIdentity(ctx, id, record.Port)
		if err != nil || record.Listener != nil && *record.Listener != *owner {
			return endpoint{}, false
		}
		record.Listener = owner
	}
	if record.Socket != "" && (record.SocketKey == "" || fileIdentity(record.Socket) != record.SocketKey) {
		return endpoint{}, false
	}
	ep := record.endpoint()
	if err := checkHealth(ctx, ep, a.Endpoint.Health); err != nil {
		return endpoint{}, false
	}
	current, err := e.readEndpointRecord(id)
	if err != nil || current.Generation != record.Generation {
		return endpoint{}, false
	}
	e.eps.mu.Lock()
	if existing, ok := e.eps.byID[id]; ok {
		e.eps.mu.Unlock()
		return existing, true
	}
	if e.eps.byID == nil {
		e.eps.byID = map[string]endpoint{}
	}
	e.eps.byID[id] = ep
	if len(record.Argv) > 0 {
		if e.eps.argv == nil {
			e.eps.argv = map[string][]string{}
		}
		e.eps.argv[id] = append([]string(nil), record.Argv...)
	}
	e.eps.mu.Unlock()
	if record.Server != nil {
		process, err := os.FindProcess(record.Server.PID)
		if err == nil {
			e.srv.mu.Lock()
			if e.srv.byID == nil {
				e.srv.byID = map[string]*exec.Cmd{}
			}
			if e.srv.owners == nil {
				e.srv.owners = map[string]processIdentity{}
			}
			if e.srv.adopted == nil {
				e.srv.adopted = map[string]bool{}
			}
			e.srv.byID[id] = &exec.Cmd{Process: process, Dir: record.Dir}
			e.srv.owners[id] = *record.Server
			e.srv.adopted[id] = true
			e.srv.mu.Unlock()
		}
	}
	if err := e.writeEndpointRecord(record); err != nil {
		e.cfg.Logf("harness: recovered endpoint metadata could not be refreshed")
	}
	return ep, true
}

func (e *Engine) forgetEndpointRecord(id, generation string) {
	if record, err := e.readEndpointRecord(id); err == nil {
		// An obsolete helper must not unlink a replacement instance's
		// socket or erase the credentials written by its successor.
		if generation != "" && record.Generation != generation {
			return
		}
		if record.SocketKey != "" && fileIdentity(record.Socket) == record.SocketKey {
			_ = os.Remove(record.Socket)
		}
	}
	if path := e.recoveryPath(id); path != "" {
		_ = os.Remove(path)
	}
}

func (e *Engine) cleanupFailedEndpoint(id string, ep endpoint) {
	key := fileIdentity(ep.socket)
	e.stopServer(id)
	if key != "" && fileIdentity(ep.socket) == key {
		_ = os.Remove(ep.socket)
	}
}

func (e *Engine) retireEndpoint(id string) {
	e.eps.mu.Lock()
	ep, had := e.eps.byID[id]
	e.eps.mu.Unlock()
	e.stopServer(id)
	e.forgetEndpointRecord(id, ep.generation)
	e.eps.mu.Lock()
	delete(e.eps.byID, id)
	e.eps.mu.Unlock()
	if had {
		if runner, ok := harness.LookupRunner("rpc"); ok {
			if forget, ok := runner.(interface{ Forget(string) }); ok {
				forget.Forget(ep.url())
			}
		}
	}
}
