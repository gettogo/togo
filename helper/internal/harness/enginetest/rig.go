// Package enginetest runs the real harness engine and adapters over
// session files on disk, for tests in other packages: a fake session
// list, pane, typist and agent binary, a temporary TOGO_HOME, and helpers
// to read every history page, collect notifications and restart.
package enginetest

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	_ "github.com/gettogo/togo/helper/internal/harness/adapters/amp"
	_ "github.com/gettogo/togo/helper/internal/harness/adapters/claude"
	_ "github.com/gettogo/togo/helper/internal/harness/adapters/copilot"
	_ "github.com/gettogo/togo/helper/internal/harness/adapters/cursor"
	_ "github.com/gettogo/togo/helper/internal/harness/adapters/droid"
	_ "github.com/gettogo/togo/helper/internal/harness/adapters/omp"
	"github.com/gettogo/togo/helper/internal/harness/engine"
	"github.com/gettogo/togo/helper/internal/harness/source"
	"github.com/gettogo/togo/helper/internal/session"
)

// SessionID is the Togo session every rig serves.
const SessionID = "s_enginetest"

// TTY is the pane's terminal, for agents (omp) that record their session
// file per terminal.
const TTY = "/dev/ttys042"

// versions are what the fake agent binaries print for the version gate.
var versions = map[string]string{"omp": "omp/18.21.0", "claude": "2.1.319 (Claude Code)",
	"copilot": "GitHub Copilot CLI 1.0.108.", "droid": "1.5.0", "cursor": "2026.11.05-86047df", "amp": "0.0.1794312042-g1a2b3c (released 2026-11-10T00:00:00.000Z)"}

// programs are the names the adapters' version commands run, where they
// differ from the agent's id.
var programs = map[string]string{"cursor": "cursor-agent"}

// Options picks the agent and folders a rig serves.
type Options struct {
	Agent string // "omp", "claude", "copilot", "droid", "cursor" or "amp"
	// Home is the user's home folder, where the session files are; Cwd
	// the session's folder under it.
	Home string
	Cwd  string
	// Created is when the Togo session started: the agent's files must
	// be from after it.
	Created time.Time
	// MaxItems is the engine's item log size; 0 keeps the default.
	MaxItems int
}

// Rig is a running engine over one session.
type Rig struct {
	T        testing.TB
	Opt      Options
	TogoHome string
	Engine   *engine.Engine
	Sub      *Sub

	bin    string
	mu     sync.Mutex
	status string
}

var pollOnce sync.Once

// New starts an engine for the session. The files may be written before
// or after.
func New(t testing.TB, opt Options) *Rig {
	t.Helper()
	// Set once, before any engine runs: the tails read it.
	pollOnce.Do(func() { source.PollEvery = 20 * time.Millisecond })
	r := &Rig{T: t, Opt: opt, TogoHome: t.TempDir(), bin: filepath.Join(t.TempDir(), "bin")}
	if err := os.MkdirAll(r.bin, 0o755); err != nil {
		t.Fatal(err)
	}
	// The gate runs `<agent> --version`; any other call fails at once.
	script := "#!/bin/sh\ncase \"$1\" in\n--version) echo '" + versions[opt.Agent] + "';;\n*) exit 1;;\nesac\n"
	prog := opt.Agent
	if p, ok := programs[prog]; ok {
		prog = p
	}
	if err := os.WriteFile(filepath.Join(r.bin, prog), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	r.start()
	return r
}

// SetStatus sets the hub's status for the session (working, idle,
// ended); "" means unknown.
func (r *Rig) SetStatus(s string) {
	r.mu.Lock()
	r.status = s
	r.mu.Unlock()
}

func (r *Rig) start() {
	path := r.bin + ":/usr/bin:/bin"
	log := &engine.Log{}
	if r.Opt.MaxItems > 0 {
		log.Max = r.Opt.MaxItems
	}
	r.Engine = engine.New(engine.Config{
		Sessions: sessions{r}, Typist: typist{}, Log: log, Home: r.Opt.Home, TogoHome: r.TogoHome,
		Screen:    func(context.Context, string) (string, error) { return "", nil },
		Cursor:    func(context.Context, string) (int, int, bool, error) { return 0, 0, false, nil },
		PaneTTY:   func(context.Context, string) (string, error) { return TTY, nil },
		LoginPath: func(context.Context) string { return path },
		LoginEnv:  func(context.Context) []string { return []string{"HOME=" + r.Opt.Home, "PATH=" + path} },
		Status: func(string) string {
			r.mu.Lock()
			defer r.mu.Unlock()
			return r.status
		},
		Scale: 0.05,
		// The engine's log goes to the test's, shown when a test fails.
		Logf: func(format string, args ...any) { r.T.Logf("engine: "+format, args...) },
	})
	r.T.Cleanup(func() { r.Engine.Forget(SessionID) })
	r.Sub = newSub()
	res, err := r.Engine.Open(context.Background(), r.Sub, SessionID, 0, 1)
	if err != nil {
		r.T.Fatalf("open: %v", err)
	}
	r.Engine.Release(r.Sub, SessionID)
	_ = res
}

// Restart stops this engine and starts another over the same TOGO_HOME
// and files, as `togo update` restarts the helper while the agent runs.
//
// That is a whole restart: the engine keeps nothing for a session outside
// TOGO_HOME/run (the followed file, run/follow-<id>.json, source/
// follow.go:24-68; an OpenCode conversation id and endpoint records,
// engine/history.go:333-370 and engine/recovery.go:23-52), and Forget
// removes only the conversation id and endpoint records
// (engine/engine.go:188-210, engine/history.go:374, engine/recovery.go:
// 315), which the omp and Claude Code adapters do not write.
func (r *Rig) Restart() {
	r.Engine.Forget(SessionID)
	r.start()
}

// Deliver sends a hook payload as the agent's hook command would.
func (r *Rig) Deliver(event string, payload []byte) {
	source.Hooks.Deliver(SessionID, source.Payload{Kind: source.BusHook, Event: event, Data: payload})
}

type sessions struct{ r *Rig }

func (s sessions) Resolve(_ context.Context, id string) (session.Session, error) {
	if id != SessionID {
		return session.Session{}, session.ErrNotFound
	}
	o := s.r.Opt
	return session.Session{ID: id, Name: "test", Agent: o.Agent, Cwd: o.Cwd, CreatedAt: o.Created, Status: "running",
		Managed: true, Command: []string{o.Agent}}, nil
}

type typist struct{}

func (typist) Type(context.Context, string, string, bool, bool) error { return nil }
func (typist) Keys(context.Context, string, ...string) error          { return nil }

// Sub collects the notifications the engine sends.
type Sub struct {
	mu    sync.Mutex
	notes []harness.Notification
	done  chan struct{}
}

func newSub() *Sub { return &Sub{done: make(chan struct{})} }

func (s *Sub) Notify(n harness.Notification) error {
	s.mu.Lock()
	s.notes = append(s.notes, n)
	s.mu.Unlock()
	return nil
}

func (s *Sub) Done() <-chan struct{} { return s.done }

// Notes returns the notifications so far.
func (s *Sub) Notes() []harness.Notification {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]harness.Notification(nil), s.notes...)
}
