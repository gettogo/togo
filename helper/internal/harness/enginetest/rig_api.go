package enginetest

import (
	"context"
	"os"
	"path/filepath"
	"strconv"

	_ "github.com/gettogo/togo/helper/internal/harness/adapters/kilo"
	_ "github.com/gettogo/togo/helper/internal/harness/adapters/opencode"
	"github.com/gettogo/togo/helper/internal/ports"
)

// The agents whose chat the helper reads from their own HTTP server
// (OpenCode 1.x, Kilo Code). Togo launches them with the port and password
// the engine picks (engine/endpoint.go EndpointArgs, called by the session
// launcher: session/launch.go EndpointArgs); after a helper restart the
// engine finds the server again from its record under TOGO_HOME/run and
// the session's listening ports (engine/recovery.go restoreEndpoint). The
// rig does the same: it asks the engine for the launch arguments, starts
// an APIServer on that port with that password, and lists it as the
// session's listener, owned by this process.

func init() {
	versions["opencode"] = "1.18.20"
	versions["kilo"] = "7.8.27"
	adapterFiles["opencode"] = "opencode.yaml"
}

// adapterFiles names the adapter file that serves the version the rig's
// fake binary reports, for an agent with one per major version.
var adapterFiles = map[string]string{}

// API is the agent's server, nil for an agent the rig serves without one.
func (r *Rig) API() *APIServer {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.api
}

// launchAPI starts the agent's server as a launch does: the engine picks
// the port and password, the agent listens there. fromStart says the
// helper is there from the agent's first event.
func (r *Rig) launchAPI(eng interface {
	EndpointArgs(agentID, sessionID, dir string) []string
	EndpointEnv(sessionID string) map[string]string
}, fromStart bool) {
	r.T.Helper()
	args := eng.EndpointArgs(r.Opt.Agent, r.ID, r.Opt.Cwd)
	port := 0
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "--port" {
			port, _ = strconv.Atoi(args[i+1])
		}
	}
	if port == 0 {
		r.T.Fatalf("the engine gave %s no port: %q", r.Opt.Agent, args)
	}
	password := ""
	for _, v := range eng.EndpointEnv(r.ID) {
		password = v
	}
	if password == "" {
		r.T.Fatalf("the engine gave %s no server password", r.Opt.Agent)
	}
	r.mu.Lock()
	s := r.api
	r.mu.Unlock()
	if err := s.Start(port, password, fromStart); err != nil {
		r.T.Fatalf("the agent's server: %v", err)
	}
}

// apiPorts is the engine's Config.Ports: the session's listening ports,
// which for an API agent is its server, held by this process.
func (r *Rig) apiPorts(context.Context, string) ([]ports.Listener, error) {
	s := r.API()
	if s == nil || s.Port() == 0 {
		return nil, nil
	}
	return []ports.Listener{{Port: s.Port(), Address: "127.0.0.1", PID: os.Getpid(), Command: r.Opt.Agent}}, nil
}

// setupAPI makes the server of an API agent, once per rig, and wires
// what the agent does with a prompt or an interrupt to the pane's hooks,
// so a test scripts an API agent as it scripts a typed one: Submitted
// lists the prompts the agent took, OnSubmit and OnInterrupt run for them.
func (r *Rig) setupAPI() {
	if !IsAPIAgent(r.Opt.Agent) || r.api != nil {
		return
	}
	s := newAPIServer(r.Opt.Agent, r.Opt.Home)
	s.OnPrompt = r.Term.take
	// An abort ends the turn and what it waits on: the permission
	// prompt leaves the screen with it.
	s.OnAbort = func() { r.Term.CloseDialog(); r.Term.interrupted("api") }
	r.api = s
	r.T.Cleanup(s.Stop)
}

// stopAPI is the agent's process ending: its server goes with it.
func (r *Rig) stopAPI() {
	if s := r.API(); s != nil {
		s.Stop()
	}
	r.exitCodex()
}

// relaunchAPI is the agent started again by the app: a new launch, so a
// new port and password, and a server whose stream starts from now.
func (r *Rig) relaunchAPI() {
	if s := r.API(); s != nil {
		s.Stop()
		r.launchAPI(r.Engine, false)
	}
}

// take is the agent taking a prompt through its API: it is what the agent
// was given, as a submitted line is.
func (t *Terminal) take(text string) {
	t.mu.Lock()
	t.subs = append(t.subs, text)
	submit := t.OnSubmit
	t.mu.Unlock()
	if submit != nil {
		submit(text)
	}
}

// interrupted is the agent taking an interrupt through its API.
func (t *Terminal) interrupted(via string) {
	t.mu.Lock()
	on := t.OnInterrupt
	t.mu.Unlock()
	if on != nil {
		on(via)
	}
}

// keepRunRecords reads the session's records under TOGO_HOME/run/harness
// (the endpoint and the conversation, engine/recovery.go and
// engine/history.go) and returns a function that writes them back: a
// helper that stops keeps them, and the next one finds the agent's server
// through them. Agents without a server keep none, and nothing is done.
func (r *Rig) keepRunRecords() func() {
	if r.api == nil {
		return func() {}
	}
	dir := filepath.Join(r.TogoHome, "run", "harness")
	saved := map[string][]byte{}
	for _, name := range []string{r.ID + ".json", r.ID + ".conversation"} {
		if b, err := os.ReadFile(filepath.Join(dir, name)); err == nil {
			saved[name] = b
		}
	}
	return func() {
		for name, b := range saved {
			_ = os.WriteFile(filepath.Join(dir, name), b, 0o600)
		}
	}
}
