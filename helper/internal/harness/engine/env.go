package engine

import (
	"context"
	"path/filepath"
	"regexp"
	"strconv"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/procs"
	"github.com/gettogo/togo/helper/internal/harness/source"
	"github.com/gettogo/togo/helper/internal/session"
)

// envFor builds the template and runner environment of a session.
func (e *Engine) envFor(info session.Session) harness.Env {
	home := e.cfg.Home
	cwd := source.Expand(info.Cwd, home)
	env := harness.Env{SessionID: info.ID, Agent: info.Agent, Cwd: cwd, Argv: info.Command, Vars: map[string]string{
		source.VarHome: home, "cwd": cwd, "cwd_key": CwdKey(cwd), "session": info.ID,
		"created":          info.CreatedAt.UTC().Format(time.RFC3339),
		source.VarTogoHome: e.cfg.TogoHome,
	}}
	if ep, ok := e.endpointFor(info.ID); ok {
		env.Endpoint = ep.url()
		env.Auth = ep.auth()
		env.Vars["port"], env.Vars["socket"] = strconv.Itoa(ep.port), ep.socket
	}
	if argv, ok := e.relaunchedArgv(info.ID); ok {
		env.Argv = argv
	}
	if e.cfg.PanePIDs != nil {
		id := info.ID
		env.Procs = procs.Session{Panes: func(ctx context.Context) ([]int, error) { return e.cfg.PanePIDs(ctx, id) }}
	}
	// Read again after the agent restarts in its pane (pane_moved.go);
	// after a helper restart this reads the pane as it now runs.
	for k, v := range e.paneVars(context.Background(), info.ID) {
		if v != "" {
			env.Vars[k] = v
		}
	}
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

var nonKey = regexp.MustCompile(`[^A-Za-z0-9]`)

// CwdKey is a folder as Claude Code and omp name their per-project
// folders: every character other than a letter or digit becomes "-".
func CwdKey(dir string) string { return nonKey.ReplaceAllString(filepath.Clean(dir), "-") }
