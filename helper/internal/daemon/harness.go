package daemon

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	claudehook "github.com/gettogo/togo/helper/internal/adapters/claude"
	"github.com/gettogo/togo/helper/internal/agents"
	"github.com/gettogo/togo/helper/internal/gitx"
	"github.com/gettogo/togo/helper/internal/harness/adapters/shell"
	"github.com/gettogo/togo/helper/internal/harness/anchor"
	"github.com/gettogo/togo/helper/internal/harness/engine"
	"github.com/gettogo/togo/helper/internal/harness/source"
	"github.com/gettogo/togo/helper/internal/ports"
	"github.com/gettogo/togo/helper/internal/session"
	"github.com/gettogo/togo/helper/internal/state"
	"github.com/gettogo/togo/helper/internal/wire"
)

// newHarness builds the harness engine over the daemon's services and
// connects it: hub events start and stop sessions and carry approvals,
// local.hook delivers raw hook payloads, and the hub decorates sessions
// with their mission summary.
func newHarness(c *Components, svc *session.Service, git *gitx.Git, o ComposeOptions) *engine.Engine {
	home := svc.Home
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	pane := anchor.TmuxPane{Client: svc.Tmux}
	pl := &ports.Service{Sessions: svc, Panes: svc.Tmux}
	eng := engine.New(engine.Config{
		Sessions: svc,
		Typist:   engine.SessionTypist{Sessions: svc, Tmux: svc.Tmux, Status: c.Hub.Status},
		// Marked first, so the exit the killed run reports at once is not
		// taken for the session ending (it went "ended" and refused sends).
		Relaunch: func(ctx context.Context, id string, argv []string) error {
			c.Hub.Relaunched(id)
			return svc.RelaunchWith(ctx, id, argv)
		},
		Marker:    &anchor.Marker{Home: o.Home, Pane: pane},
		Approvals: c.Approvals,
		Screen:    pane.Screen,
		Status:    c.Hub.Status,
		NeedsYou: func(id, title, body string) {
			c.Hub.Publish(wire.EventNotify, &id, wire.NotifyEvent{Title: title, Body: body, Level: wire.LevelNeedsYou})
		},
		// Through local's publisher, which drops the pane watcher's empty
		// "Done" for the same turn (doneDedupe).
		Done: func(id, title, body string) {
			c.Local.Pub.Publish(wire.EventNotify, &id, wire.NotifyEvent{Title: title, Body: body, Level: wire.LevelDone})
		},
		Git:        git.Status,
		Ports:      pl.List,
		LoginPath:  func(ctx context.Context) string { return loginPath(ctx, svc, home) },
		LoginEnv:   state.LoginEnv,
		Home:       home,
		AdapterDir: filepath.Join(o.Home, "adapters"),
		TogoHome:   o.Home,
		OnSummary:  c.Hub.MissionChanged,
		Logf:       c.logf,
	})
	c.Hub.SetMission(eng.Summary)
	c.Hub.OnEvent(eng.Observe)
	source.Hooks.OnDeliver = eng.Delivered
	c.Local.Harness = deliverHook
	session.EndpointArgs = eng.EndpointArgs
	session.EndpointEnv = eng.EndpointEnv
	return eng
}

// loginPath is the PATH harness programs are found in: the session
// service's, then the agents' install folders.
func loginPath(ctx context.Context, svc *session.Service, home string) string {
	p := svc.Path
	if p == "" {
		p = state.LoginPath(ctx)
	}
	dirs := append([]string{p}, agents.FallbackDirs(home)...)
	return strings.Join(dirs, string(os.PathListSeparator))
}

// deliverHook hands a forwarded hook to the session's harness feeds. A
// Claude hook from an older togo hook command, sent without its payload,
// is rebuilt from the summary fields.
func deliverHook(id string, f wire.HookForward) {
	event, payload := f.Hook, f.Payload
	if len(payload) == 0 && f.Agent == "claude" {
		event, payload = claudehook.Synthesize(f)
	}
	if len(payload) == 0 {
		return
	}
	kind := source.BusHook
	if f.Hook == wire.HookStatusline || f.Hook == claudehook.Statusline {
		kind, event = source.BusStatusline, ""
	}
	source.Hooks.Deliver(id, source.Payload{Kind: kind, Event: event, Data: payload})
}

// deliverShell hands a shell command mark from the tap to the session's
// harness feeds (the shell adapter's hook feed).
func deliverShell(id string, body []byte) {
	source.Hooks.Deliver(id, source.Payload{Kind: source.BusHook, Event: shell.Event, Data: body})
}
