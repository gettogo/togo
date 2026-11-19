// Package enginetest runs the real harness engine and adapters over
// session files on disk, for tests in other packages: a fake session
// list, pane, typist and agent binary, a temporary TOGO_HOME, and helpers
// to read every history page, collect notifications and restart.
package enginetest

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/approval"
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
	"github.com/gettogo/togo/helper/internal/wire"
)

// SessionID is the Togo session a rig serves unless Options.ID names another.
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

// subcommands are what a fake binary prints for a subcommand an adapter
// lists models with; `cursor-agent models` lists them as the adapter's
// jq reads (cursor/testdata, cursor_test.go).
var subcommands = map[string]string{"cursor": "models) printf 'Available models\\n\\ngpt-5 - GPT-5\\ncomposer-2.5 - Composer 2.5 (current)\\n';;\n"}

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
	// NoRedact passes records through unredacted. Redaction has its own
	// tests, and it is four fifths of the time a restore of a large file
	// takes (eight times that under the race detector).
	NoRedact bool
	// PanePIDs, when set, names the processes of the session's pane, so
	// the engine attributes files to the session by who holds them open
	// (the agents whose feed is exclusive: pi, kimi). Nil leaves the
	// engine without a process view.
	PanePIDs func() []int
	// ID is the Togo session's id; empty means SessionID. Rigs that run
	// at the same time need ids of their own: the hook bus and the typing
	// locks are keyed by it. NewID makes one.
	ID string
	// ApprovalTTL is how long an approval stays answerable; 0 keeps the
	// store's default.
	ApprovalTTL time.Duration
}

// asIs is a redactor that changes nothing.
type asIs struct{}

func (asIs) JSON(raw json.RawMessage) json.RawMessage { return raw }
func (asIs) String(s string) string                   { return s }

// Rig is a running engine over one session.
type Rig struct {
	T        testing.TB
	Opt      Options
	ID       string
	TogoHome string
	Engine   *engine.Engine
	Sub      *Sub
	// Term is the session's pane.
	Term *Terminal
	phoneState
	lifeState

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
	if opt.ID == "" {
		opt.ID = SessionID
	}
	r := &Rig{T: t, Opt: opt, ID: opt.ID, TogoHome: t.TempDir(), bin: filepath.Join(t.TempDir(), "bin"), Term: newTerminal(opt.Agent)}
	if err := os.MkdirAll(r.bin, 0o755); err != nil {
		t.Fatal(err)
	}
	// The gate runs `<agent> --version`; any other call fails at once.
	// Every call but the version check is recorded, one argv per line,
	// so a test sees what an action ran (CLICalls).
	script := "#!/bin/sh\ncase \"$1\" in\n--version|version) echo '" + versions[opt.Agent] + "';;\n" + subcommands[opt.Agent] +
		"*) echo \"$*\" >> '" + filepath.Join(r.bin, "calls.log") + "'; exit 1;;\nesac\n"
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
	var red harness.Redactor
	if r.Opt.NoRedact {
		red = asIs{}
	}
	var panePIDs func(context.Context, string) ([]int, error)
	if f := r.Opt.PanePIDs; f != nil {
		panePIDs = func(context.Context, string) ([]int, error) { return f(), nil }
	}
	eng := engine.New(engine.Config{
		Redactor: red,
		PanePIDs: panePIDs,
		Sessions: sessions{r}, Typist: r.Term, Log: log, Home: r.Opt.Home, TogoHome: r.TogoHome,
		Approvals:    approvals{r},
		Screen:       func(context.Context, string) (string, error) { return r.Term.Text(), nil },
		StyledScreen: func(context.Context, string) (string, error) { return r.Term.Styled(), nil },
		Cursor:       func(context.Context, string) (int, int, bool, error) { return r.Term.cursor() },
		NeedsYou: func(id, title, body string) {
			r.note(Event{Kind: wire.EventNotify, Payload: wire.NotifyEvent{Title: title, Body: body, Level: wire.LevelNeedsYou}})
		},
		Done: func(id, title, body string) {
			r.note(Event{Kind: wire.EventNotify, Payload: wire.NotifyEvent{Title: title, Body: body, Level: wire.LevelDone}})
		},
		Relaunch:  r.relaunch,
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
	r.mu.Lock()
	r.Engine = eng
	r.mu.Unlock()
	r.startPhone()
	r.T.Cleanup(func() { eng.Forget(r.ID) })
	r.Sub = newSub()
	res, err := r.Engine.Open(context.Background(), r.Sub, r.ID, 0, 1)
	if err != nil {
		r.T.Fatalf("open: %v", err)
	}
	r.Engine.Release(r.Sub, r.ID)
	_ = res
}

// approvals is the engine's view of the store; it follows the store a
// restart builds.
type approvals struct{ r *Rig }

func (a approvals) store() *approval.Store {
	a.r.mu.Lock()
	defer a.r.mu.Unlock()
	return a.r.Approvals
}

func (a approvals) Pending(id string) []wire.Approval {
	if s := a.store(); s != nil {
		return s.Pending(id)
	}
	return nil
}

func (a approvals) Answer(ctx context.Context, requestID, raw, by string) error {
	return a.store().Answer(ctx, requestID, raw, by)
}

func (a approvals) OpenAsk(ctx context.Context, p wire.AskParams) (wire.Approval, <-chan string) {
	return a.store().OpenAsk(ctx, p)
}

func (a approvals) CloseDesk(requestID string) { a.store().CloseDesk(requestID) }

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
	r.Stop()
	r.Start()
}

// Stop stops the engine, as the helper being down; Start builds a new
// one. Files written between the two are found by the new engine's
// restore, as after an update that took a while.
func (r *Rig) Stop() { r.Engine.Forget(r.ID) }

// Start builds a new engine over the same TOGO_HOME and files.
func (r *Rig) Start() { r.start() }

// Deliver sends a hook payload as the agent's hook command would.
func (r *Rig) Deliver(event string, payload []byte) {
	source.Hooks.Deliver(r.ID, source.Payload{Kind: source.BusHook, Event: event, Data: payload})
}

type sessions struct{ r *Rig }

func (s sessions) Resolve(_ context.Context, id string) (session.Session, error) {
	s.r.mu.Lock()
	removed := s.r.removed
	s.r.mu.Unlock()
	if id != s.r.ID || removed {
		return session.Session{}, session.ErrNotFound
	}
	o := s.r.Opt
	return session.Session{ID: id, Name: "test", Agent: o.Agent, Cwd: o.Cwd, CreatedAt: o.Created, Status: "running",
		Managed: true, Command: []string{o.Agent}}, nil
}

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
