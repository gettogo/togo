package enginetest

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	_ "github.com/gettogo/togo/helper/internal/harness/adapters/codex"
	"github.com/gettogo/togo/helper/internal/harness/sessiongen"
)

// Codex is read through its app-server, which the engine starts for the
// session (engine/endpoint.go EndpointArgs, engine/server.go startServer)
// as `codex app-server --listen unix://SOCK`, found on the login PATH. The
// rig's fake codex program answers the version gate and, run as the
// app-server, notes its pid and socket in bin/app-server.log and sleeps
// (exec, so the engine's process group and identity checks see one
// process); the rig's CodexServer then listens on that socket. A helper
// restart finds the same process and socket again from the engine's
// record under TOGO_HOME/run (engine/recovery.go restoreEndpoint). The
// sleeping processes are killed when the test ends.
func init() {
	versions["codex"] = "codex-cli 0.167.0"
	subcommands["codex"] = `app-server) echo "$$ $3" >> "$(dirname "$0")/app-server.log"; exec sleep 600;;` + "\n"
}

// Codex is the session's app-server, nil for other agents.
func (r *Rig) Codex() *CodexServer {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.codex
}

func (r *Rig) setupCodex() {
	if r.Opt.Agent != "codex" || r.codex != nil {
		return
	}
	// The socket lives under TOGO_HOME/run/harness, and a socket path has
	// at most 104 bytes on macOS: a test's temporary folder is longer than
	// a user's ~/.togo. TOGO_HOME is reached through a short link to it,
	// in /tmp when the temporary folder's own name is long.
	base := os.TempDir()
	if len(base) > 40 {
		base = "/tmp"
	}
	link, err := os.MkdirTemp(base, "tg")
	if err != nil {
		r.T.Fatal(err)
	}
	if err := os.Remove(link); err != nil || os.Symlink(r.TogoHome, link) != nil {
		r.T.Fatalf("a short link to TOGO_HOME: %v", err)
	}
	r.T.Cleanup(func() { _ = os.Remove(link) })
	r.TogoHome = link
	s := newCodexServer(r.Opt.Home, r.Opt.Cwd)
	// An interrupt reaches the agent as an API agent's does: the turn
	// ends, and the approval it waited on leaves the terminal with it.
	s.OnInterrupt = func(string, string) { r.Term.CloseDialog(); r.Term.interrupted("rpc") }
	r.codex = s
	r.T.Cleanup(func() {
		s.Stop()
		r.killAppServers()
	})
}

// launchCodex is a launch: the engine picks the socket and starts the
// app-server; the server listens there once the fake program has run. It
// returns the terminal client's arguments, nil when the engine started
// none (a version outside the adapter's range).
func (r *Rig) launchCodex(eng interface {
	EndpointArgs(agentID, sessionID, dir string) []string
}) []string {
	r.T.Helper()
	log := filepath.Join(r.bin, "app-server.log")
	before := len(appServers(log))
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			if l := appServers(log); len(l) > before {
				if path, ok := strings.CutPrefix(l[len(l)-1][1], "unix://"); ok {
					if err := r.codex.Listen(path); err != nil {
						r.T.Errorf("the app-server cannot listen: %v", err)
					}
				}
				return
			}
			select {
			case <-stop:
				return
			case <-time.After(5 * time.Millisecond):
			}
		}
	}()
	args := eng.EndpointArgs(r.Opt.Agent, r.ID, r.Opt.Cwd)
	close(stop)
	<-done
	return args
}

// LaunchArgs are the terminal client's arguments from the last launch.
func (r *Rig) codexArgs(args []string) {
	r.mu.Lock()
	r.codexLaunch = args
	r.mu.Unlock()
}

// CodexLaunch is what the last launch gave the terminal client: --remote
// and the socket, or nothing when the app-server was not started.
func (r *Rig) CodexLaunch() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.codexLaunch...)
}

// exitCodex is the terminal client ending: its thread is no longer loaded
// (appserver-attach.jsonl:27). The app-server, the helper's, runs on.
func (r *Rig) exitCodex() {
	s := r.Codex()
	if s == nil {
		return
	}
	if th := s.Main(); th != "" {
		s.Append(sessiongen.CodexStatus(th, "notLoaded"))
		s.Flush()
	}
}

// relaunchCodex is the terminal client started again with argv: a new
// launch, so a new app-server on the same socket. `codex resume <id>`
// joins that thread; any other start makes a new thread.
func (r *Rig) relaunchCodex(argv []string) {
	s := r.Codex()
	if s == nil {
		return
	}
	thread := ""
	if len(argv) > 2 && argv[1] == "resume" {
		thread = argv[2]
	}
	s.Close()
	s.Relaunched(thread)
	r.codexArgs(r.launchCodex(r.Engine))
	if thread == "" {
		at := time.Now()
		s.Append(sessiongen.CodexThreadStarted(sessiongen.CodexThread(NewCodexThreadID(), "", "", "", r.Opt.Cwd,
			sessiongen.CodexModel, sessiongen.CodexEffort, at)))
		s.Flush()
	}
}

// NewCodexThreadID is a fresh thread id in Codex's shape.
func NewCodexThreadID() string {
	id := NewID()[2:]
	return "019b" + id[:4] + "-0000-7000-8000-00" + id
}

// appServers lists the app-servers the fake program ran: pid and socket.
func appServers(log string) [][2]string {
	b, err := os.ReadFile(log)
	if err != nil {
		return nil
	}
	var out [][2]string
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if f := strings.Fields(line); len(f) == 2 {
			out = append(out, [2]string{f[0], f[1]})
		}
	}
	return out
}

// AppServerPIDs are the app-server processes started so far.
func (r *Rig) AppServerPIDs() []int {
	var out []int
	for _, l := range appServers(filepath.Join(r.bin, "app-server.log")) {
		if pid, err := strconv.Atoi(l[0]); err == nil {
			out = append(out, pid)
		}
	}
	return out
}

// killAppServers ends every app-server the engine left, at the test's end.
func (r *Rig) killAppServers() {
	for _, pid := range r.AppServerPIDs() {
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}
}
