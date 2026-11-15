package daemon

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	claudehook "github.com/gettogo/togo/helper/internal/adapters/claude"
	"github.com/gettogo/togo/helper/internal/agents"
	"github.com/gettogo/togo/helper/internal/gitx"
	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/adapters/shell"
	"github.com/gettogo/togo/helper/internal/harness/anchor"
	"github.com/gettogo/togo/helper/internal/harness/engine"
	"github.com/gettogo/togo/helper/internal/harness/source"
	"github.com/gettogo/togo/helper/internal/ports"
	"github.com/gettogo/togo/helper/internal/rpc"
	"github.com/gettogo/togo/helper/internal/session"
	"github.com/gettogo/togo/helper/internal/state"
	"github.com/gettogo/togo/helper/internal/surface"
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
		// Lists a harness draws in its terminal are read from this.
		StyledScreen: pane.StyledScreen,
		Cursor:       pane.Cursor,
		ClientGone:   rpc.ClientGone,
		ClientClosed: func(client any) bool {
			conn, _ := client.(*rpc.Conn)
			return conn.Gone()
		},
		// A list driven for the phone belongs to the connection that opened it.
		Client: func(ctx context.Context) any {
			if conn := rpc.ConnFrom(ctx); conn != nil {
				return conn
			}
			return nil
		},
		// The native surface service is built after the engine; both are
		// read when called, never at construction.
		SurfaceHeld:      func(id string) bool { return c.Surface != nil && c.Surface.Held(id) },
		// Screens come from the terminal reader, the same way for every
		// program. The program's own rendering protocol is offered to the
		// app only when asked for (TOGO_OMP_SURFACE=1); its companion still
		// loads, so added folders and the rest keep working.
		SurfaceAvailable: func(ctx context.Context, id string) bool {
			return os.Getenv("TOGO_OMP_SURFACE") == "1" && c.Surface != nil && c.Surface.Available(ctx, id)
		},
		// The running program's additional workspace folders, where its
		// companion reports them and the program is this session's.
		ExtraRoots: func(ctx context.Context, id string) ([]string, bool) {
			if c.Surface == nil {
				return nil, false
			}
			w := c.Surface.Roots(ctx, id)
			return w.Roots, w.Incomplete != ""
		},
		// The running program's own command list, where its companion publishes one.
		LiveCommands: func(id string) []harness.LiveCommand {
			list, ok := (&surface.Companions{Home: o.Home}).Commands(id)
			if !ok {
				return nil
			}
			out := make([]harness.LiveCommand, 0, len(list))
			for _, cmd := range list {
				out = append(out, harness.LiveCommand{Name: cmd.Name, Detail: cmd.Detail, State: cmd.State, Icon: cmd.Icon, Group: cmd.Group, Folder: cmd.Folder})
			}
			return out
		},
		Status: c.Hub.Status,
		Exit: func(id string) *wire.SessionExit {
			_, x := c.Hub.Exit(id)
			return x
		},
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
		PanePIDs:   svc.Tmux.PanePIDs,
		PaneTTY:    svc.Tmux.PaneTTY,
		LoginPath:  func(ctx context.Context) string { return loginPath(ctx, svc, home) },
		LoginEnv:   state.LoginEnv,
		Home:       home,
		AdapterDir: filepath.Join(o.Home, "adapters"),
		TogoHome:   o.Home,
		OnSummary:  c.Hub.MissionChanged,
		Logf:       c.logf,
	})
	c.Hub.SetMission(eng.Summary)
	c.Watch.SetTurnRunning(eng.TurnRunning)
	c.Watch.SetNeedsYou(eng.NeedsYou)
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
