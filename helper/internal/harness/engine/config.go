package engine

import (
	"context"
	"time"

	"github.com/gettogo/togo/helper/internal/gitx"
	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/ports"
	"github.com/gettogo/togo/helper/internal/wire"
)

// Config holds the engine's collaborators. Nil fields turn the features
// that need them off.
type Config struct {
	Sessions  Sessions
	Typist    harness.Typist
	Marker    harness.Marker
	Redactor  harness.Redactor
	Log       *Log
	Approvals Approvals
	// Screen returns the pane's visible text.
	Screen func(ctx context.Context, id string) (string, error)
	// Client identifies the connection a call arrived on, so a list one
	// device is driving cannot be driven by another. Nil means lists are
	// not tied to a connection.
	Client func(ctx context.Context) any
	// ClientClosed reports that a connection (as Client returned it) has
	// ended. A list it was driving is let go at once, not when it expires.
	ClientClosed func(client any) bool
	// ClientGone reports that the client a call is running for has gone
	// away. A list is never driven further for a client that cannot see
	// it. Nil means never.
	ClientGone func(ctx context.Context) bool
	// Cursor reports where the pane's cursor is. A harness keeps it where
	// typing goes, so it tells the input line from a dialog over it. Nil
	// means it cannot be told.
	Cursor func(ctx context.Context, id string) (x, y int, visible bool, err error)
	// StyledScreen returns the pane's visible text with its colour
	// sequences. Lists drawn in the terminal are read from it; nil means
	// they are not read.
	StyledScreen func(ctx context.Context, id string) (string, error)
	// NeedsYou tells the phone a session needs the user at the terminal:
	// the typing guard found a dialog it will not type into.
	NeedsYou func(id, title, body string)
	// Done tells the phone a turn finished, with the reply's text, for
	// adapters with no hook to say so: without it the pane watcher's
	// "Done" is all the phone gets, with no body. Nil sends nothing.
	Done func(id, title, body string)
	// Status is the hub's status for a session (working, idle, ...).
	Status func(id string) string
	// Exit returns how the session's agent exited, nil when it runs or it
	// is not known. It names the exit status in the chat's ended notice.
	Exit func(id string) *wire.SessionExit
	// Relaunch restarts the agent with argv in its pane.
	Relaunch func(ctx context.Context, id string, argv []string) error
	Git      func(ctx context.Context, dir string) (gitx.Status, error)
	Ports    func(ctx context.Context, id string) ([]ports.Listener, error)
	// PanePIDs lists the pane processes of a session's tmux session; with
	// it a source can tell which files the session's own agent holds.
	PanePIDs func(ctx context.Context, id string) ([]int, error)
	// PaneTTY is the terminal device of a session's pane (/dev/ttys011);
	// adapters whose agent records its session per terminal read it as
	// the "tty" variable.
	PaneTTY func(ctx context.Context, id string) (string, error)
	// LoginPath is the PATH programs are looked up in.
	LoginPath func(ctx context.Context) string
	// LoginEnv is the login shell's environment; nil keeps the helper's.
	LoginEnv func(ctx context.Context) []string
	// SurfaceHeld reports whether a phone holds the session's terminal
	// natively right now (the agent draws for the phone, not the pane).
	// Keys typed then would be taken as someone at the desk and end the
	// native screen, so nothing is typed while it is true. Nil means never.
	SurfaceHeld func(session string) bool
	// LiveCommands is the session's command list as its running program
	// has it, or nil when the program does not publish one. It is the
	// complete list, where a harness's command API may leave commands out.
	LiveCommands func(session string) []harness.LiveCommand
	// ExtraRoots are the session's additional workspace folders (omp's
	// /add-dir), where the running program reports them. Files in them
	// are offered for completion by their absolute path.
	// incomplete says they are not all of them, or could not be confirmed.
	ExtraRoots func(ctx context.Context, session string) (roots []string, incomplete bool)
	// SurfaceAvailable reports, without taking anything, whether the
	// session's running agent can show its screens natively. Nil means no.
	SurfaceAvailable func(ctx context.Context, session string) bool
	// Home is the user's home folder; AdapterDir holds user overrides;
	// TogoHome holds the sockets picked for harness endpoints.
	Home       string
	AdapterDir string
	TogoHome   string
	Now        func() time.Time
	NewID      func() string
	// OnSummary is told when a session's mission summary changed,
	// at most once a second per session.
	OnSummary func(id string)
	Logf      func(format string, args ...any)
	// Scale multiplies verify windows and idle waits; tests shrink it.
	Scale float64
}
