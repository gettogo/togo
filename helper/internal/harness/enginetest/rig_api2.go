package enginetest

import (
	"crypto/rand"
	"encoding/hex"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// OpenCode 2.x in the rig. The session's agent is "opencode", its fake
// binary reports a 2.x version, so the engine serves it with
// opencode2.yaml. At launch the engine itself starts the agent's server
// (`opencode serve`): the fake binary runs the stand-in (standin.go), a
// process of its own that passes requests to the rig's APIServer. The
// terminal is the rig's pane, as for any typed agent: prompts the phone
// sends are typed there, and Term.OnSubmit is the agent taking them.

// variant is an adapter the rig serves for an agent at a version other
// than the agent's default one.
type variant struct{ agent, version string }

// variants are the adapter names a test may give as Options.Agent.
var variants = map[string]variant{"opencode2": {agent: "opencode", version: "2.2.5"}}

// IsVariant reports a name that is an adapter of an agent at a version,
// not an agent id.
func IsVariant(name string) bool { _, ok := variants[name]; return ok }

// serveCase is the fake binary's `serve`: the stand-in, in this test
// binary, given the rig folder it reads its upstream from.
func serveCase(dir string) string {
	exe, err := os.Executable()
	if err != nil {
		exe = os.Args[0]
	}
	return "serve) " + standInEnv + "='" + dir + "' exec '" + exe + "' \"$@\";;\n"
}

func secret() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// setupAPI2 makes the 2.x server: the APIServer the stand-ins pass to,
// listening on a free port with a password only they know.
func (r *Rig) setupAPI2() {
	s := newAPIServer("opencode", r.Opt.Home)
	s.v2 = newV2State()
	s.OnAbort = func() { r.Term.CloseDialog(); r.Term.interrupted("api") }
	port, err := freeLoopbackPort()
	if err != nil {
		r.T.Fatal(err)
	}
	pw := secret()
	if err := s.Start(port, pw, true); err != nil {
		r.T.Fatal(err)
	}
	if err := os.WriteFile(upstreamFile(r.bin), []byte("127.0.0.1:"+strconv.Itoa(port)+"\n"+pw+"\n"), 0o600); err != nil {
		r.T.Fatal(err)
	}
	r.api = s
	r.T.Cleanup(func() {
		_ = os.Remove(upstreamFile(r.bin))
		r.killStandIns()
		s.Stop()
	})
}

func freeLoopbackPort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// launchAPI2 is the agent's launch: the engine picks the address, starts
// the server (the stand-in) and names it in the terminal's arguments.
func (r *Rig) launchAPI2(eng interface {
	EndpointArgs(agentID, sessionID, dir string) []string
}) {
	r.T.Helper()
	args := eng.EndpointArgs(r.Opt.Agent, r.ID, r.Opt.Cwd)
	if len(args) != 2 || args[0] != "--server" || !strings.HasPrefix(args[1], "http://127.0.0.1:") {
		r.T.Fatalf("the engine started no 2.x server: %q (calls %q)", args, r.CLICalls())
	}
}

// ServerPort is the port of the agent's server the engine started, 0 when
// none runs.
func (r *Rig) ServerPort() int {
	pids := r.standIns()
	if len(pids) == 0 {
		return 0
	}
	b, _ := os.ReadFile(filepath.Join(r.bin, "standin-"+strconv.Itoa(pids[len(pids)-1])))
	p, _ := strconv.Atoi(string(b))
	return p
}

// standIns lists the stand-ins still running, oldest first.
func (r *Rig) standIns() []int {
	files, _ := filepath.Glob(filepath.Join(r.bin, "standin-*"))
	var out []int
	for _, f := range files {
		pid, err := strconv.Atoi(strings.TrimPrefix(filepath.Base(f), "standin-"))
		if err != nil {
			continue
		}
		if syscall.Kill(pid, 0) != nil {
			_ = os.Remove(f)
			continue
		}
		out = append(out, pid)
	}
	return out
}

// KillServer ends the agent's server process, as when it crashes: its
// streams end and nothing listens on its port.
func (r *Rig) KillServer() {
	r.T.Helper()
	r.killStandIns()
}

func (r *Rig) killStandIns() {
	for _, pid := range r.standIns() {
		_ = syscall.Kill(-pid, syscall.SIGKILL)
		_ = syscall.Kill(pid, syscall.SIGKILL)
		for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
			if syscall.Kill(pid, 0) != nil {
				break
			}
		}
		_ = os.Remove(filepath.Join(r.bin, "standin-"+strconv.Itoa(pid)))
	}
}

// StartServerAgain starts a server on the port the engine knows, with the
// password it gave, as the agent's own server coming back after a crash:
// a new process the engine did not start.
func (r *Rig) StartServerAgain(port int) {
	r.T.Helper()
	b, err := os.ReadFile(filepath.Join(r.TogoHome, "run", "harness", r.ID+".json"))
	if err != nil {
		r.T.Fatalf("no endpoint record: %v", err)
	}
	pw := between(string(b), `"password":"`, `"`)
	cmd := exec.Command(filepath.Join(r.bin, "opencode"), "serve", "--hostname", "127.0.0.1", "--port", strconv.Itoa(port))
	cmd.Env = append(os.Environ(), "OPENCODE_PASSWORD="+pw)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		r.T.Fatal(err)
	}
	go func() { _ = cmd.Wait() }()
	for deadline := time.Now().Add(30 * time.Second); r.ServerPort() != port; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			r.T.Fatalf("the server did not come back on port %d", port)
		}
	}
}

func between(s, a, b string) string {
	i := strings.Index(s, a)
	if i < 0 {
		return ""
	}
	s = s[i+len(a):]
	if j := strings.Index(s, b); j >= 0 {
		return s[:j]
	}
	return ""
}
