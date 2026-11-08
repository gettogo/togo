package engine

import (
	"context"
	"path/filepath"
	"strings"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/spec"
)

// runRelaunch restarts the agent in its pane with a new argv (a model or
// mode flag, a resume id). The program must be the session's own agent:
// an adapter changes flags, never what runs. It waits for the agent to be
// idle so a turn is not cut off, and holds the session's typing lock so
// nothing is typed into the pane while it restarts.
func (e *Engine) runRelaunch(ctx context.Context, s *sess, r *spec.Relaunch, data map[string]any) (harness.ActionResult, error) {
	if e.cfg.Relaunch == nil || len(r.Argv) == 0 {
		return harness.ActionResult{Result: harness.ResultFallback}, nil
	}
	argv := make([]string, 0, len(r.Argv))
	for _, a := range r.Argv {
		v, err := Render(a, data)
		if err != nil || v == "" || len(v) > maxArgString || flagFromInput(a, v) {
			return harness.ActionResult{Result: harness.ResultFailed, Message: "The command could not be built"}, nil
		}
		argv = append(argv, v)
	}
group:
	for _, g := range r.Keep {
		vals := make([]string, 0, len(g))
		for _, a := range g {
			v, err := Render(a, data)
			if err != nil || v == "" || len(v) > maxArgString || (flagFromInput(a, v)) {
				continue group
			}
			vals = append(vals, v)
		}
		argv = append(argv, vals...)
	}
	if !sameProgram(argv[0], s.info.Command, s.info.Agent) {
		return harness.ActionResult{Result: harness.ResultFailed, Message: "Only the session's own agent can be restarted"}, nil
	}
	l := typingLock(s.id)
	l.Lock()
	defer l.Unlock()
	if err := e.waitIdle(ctx, s); err != nil {
		return harness.ActionResult{}, err
	}
	if err := e.cfg.Relaunch(ctx, s.id, argv); err != nil {
		e.cfg.Logf("harness: %s relaunch: %v", s.ad.Adapter.Adapter, err)
		return harness.ActionResult{Result: harness.ResultFailed, Message: "The agent could not be restarted"}, nil
	}
	s.mu.Lock()
	s.relaunchedAt = time.Now()
	s.mu.Unlock()
	return harness.ActionResult{Result: harness.ResultRelaunched}, nil
}

// sameProgram reports whether prog names the program the session runs.
func sameProgram(prog string, command []string, agent string) bool {
	base := filepath.Base(prog)
	if base != prog {
		return false // a path could point anywhere; adapters name the program
	}
	if len(command) > 0 {
		return filepath.Base(command[0]) == base
	}
	return agent != "" && base == agent
}

// modeIDs lists the adapter's mode choices, if it declares any.
func (a *adapter) modeIDs() map[string]bool {
	if a == nil || a.Modes == nil || len(a.Modes.Choices) == 0 {
		return nil
	}
	out := map[string]bool{}
	for _, c := range a.Modes.Choices {
		out[c.ID] = true
	}
	return out
}

// flagFromInput reports whether a templated argv element rendered to a
// flag its template did not start with: a value from the phone or state
// ("--dangerously-skip-permissions" as a resume id) must stay a value.
func flagFromInput(tmpl, rendered string) bool {
	return strings.Contains(tmpl, "{{") && strings.HasPrefix(rendered, "-") && !strings.HasPrefix(tmpl, "-")
}
