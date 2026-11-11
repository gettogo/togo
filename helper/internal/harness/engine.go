package harness

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// Engine serves the harness.* methods for every session. One Engine runs in
// the daemon; it matches each managed session to an adapter, runs that
// adapter's sources, keeps the session's item log and mission snapshot, and
// applies actions.
type Engine interface {
	// Describe reports the adapter and capabilities for a session. A session
	// with no adapter gets a Descriptor with a null Adapter and no error.
	Describe(ctx context.Context, session string) (Descriptor, error)
	// Open returns items after the cursor (at most limit, newest kept) and
	// the mission snapshot, and sends sub every later change for the
	// session until Close or sub is done.
	Open(ctx context.Context, sub Subscriber, session string, after int64, limit int) (OpenResult, error)
	Close(ctx context.Context, sub Subscriber, session string) error
	// History pages older items: up to limit items with Seq < before.
	History(ctx context.Context, session string, before int64, limit int) (Page, error)
	// Screen reads a screen's sources and fills its primitive. A source
	// failure with no cached copy is a *FallbackError.
	Screen(ctx context.Context, session, screen string, query map[string]string) (Screen, error)
	Act(ctx context.Context, req ActionRequest) (ActionResult, error)
	Send(ctx context.Context, req SendRequest) (SendResult, error)
	Interrupt(ctx context.Context, session string) (ActionResult, error)
	Complete(ctx context.Context, req CompleteRequest) (CompleteResult, error)
	Locate(ctx context.Context, session string, a Anchor) (Location, error)
	// Summary is the session's pill summary for Session.mission, or false
	// when the session has no adapter.
	Summary(session string) (Summary, bool)
}

// Subscriber receives harness notifications for the sessions it opened. The
// daemon's subscriber wraps an rpc.Conn.
type Subscriber interface {
	Notify(n Notification) error
	Done() <-chan struct{}
}

// Errors the engine returns; methods.go maps them to protocol errors.
var (
	// ErrNoAdapter: the session has no adapter (-32008, reason no_adapter).
	ErrNoAdapter = errors.New("harness: no adapter for session")
	// ErrUnknownScreen and ErrUnknownAction: the adapter declares no such id
	// (-32602).
	ErrUnknownScreen = errors.New("harness: unknown screen")
	ErrUnknownAction = errors.New("harness: unknown action")
	// ErrBusy: harness.send mode now while the agent works (-32006, reason
	// busy).
	ErrBusy = errors.New("harness: agent is working")
)

// FallbackError says a screen or source cannot be served and the app should
// show the terminal (-32008, reason fallback, with detail).
type FallbackError struct {
	// Reason is source, version or parse.
	Reason string
	Detail string
}

func (e *FallbackError) Error() string { return "harness fallback: " + e.Reason + ": " + e.Detail }

// Runtime pieces the engine composes. Each has one implementation per kind,
// registered by kind name.

// Env is what a source or action knows about the session it serves. Vars
// feed the adapter's templates ({{.cwd}}, {{.port}}).
type Env struct {
	SessionID string
	Agent     string
	Cwd       string
	Argv      []string
	// HarnessSession is the agent's own session id (from hooks), if known.
	HarnessSession string
	// Endpoint is the live harness's API address the helper arranged: an
	// OpenCode --port URL, a Codex app-server unix socket path.
	Endpoint string
	// Auth is the Authorization header value for an http endpoint that
	// takes a password, else "".
	Auth string
	Vars map[string]string
	// Environ is the user's login shell environment, which programs the
	// helper runs for the session get on top of its own: the agent reads
	// its API keys from there. Nil keeps the helper's environment.
	Environ []string
}

// SourceSpec is the part of an adapter's source declaration a runner
// reads. Raw holds the kind-specific block (argv, path, url, method...).
type SourceSpec struct {
	Name string
	Kind string
	Raw  json.RawMessage
}

// Runner reads one kind of source: cli, file, jsonl, http, sse, rpc, hook,
// statusline. Fetch answers a one-shot read (screens). Watch streams
// records until ctx ends (conversation and mission feeds); runners that
// cannot stream return ErrNotStreaming.
type Runner interface {
	Kind() string
	Fetch(ctx context.Context, s SourceSpec, env Env) (json.RawMessage, error)
	Watch(ctx context.Context, s SourceSpec, env Env, emit func(Record)) error
}

// ErrNotStreaming is returned by Runner.Watch for fetch-only kinds.
var ErrNotStreaming = errors.New("harness: source kind does not stream")

// Mapper turns a stream of records into items and mission ops. Mappers are
// Go code, one per harness feed (for example claude.transcript,
// codex.appserver), named from the adapter file. A mapper keeps its own
// state between records and must not block.
type Mapper interface {
	Name() string
	Map(rec Record) (items []Item, ops []Op)
	// State is what the mapper learned that screens and actions read as
	// $state in jq and .state in templates: model, mode, effort,
	// harness_session, transcript_path, working ("true" or "false").
	State() map[string]string
}

// IdleCloser is a mapper whose harness writes no turn end (Aider's chat
// history has none): when the session goes idle, by the pane or the other
// mappers, the engine asks it for the items that close the open turn.
type IdleCloser interface {
	CloseIdle(at time.Time) []Item
}

// Relauncher is a mapper that learns state from the flags an action
// restarted the agent with, for a harness that records none of them: Pi
// writes nothing for a --thinking level given on a resume. Its ops (a
// Status block) are published at once.
type Relauncher interface {
	Relaunched(argv []string, at time.Time) []Op
}

// MapperFactory builds a fresh mapper for one session.
type MapperFactory func(env Env) Mapper

// Typist types into a session's pane. Text goes in as a bracketed paste;
// Submit adds Enter. WhenIdle waits for the agent's idle signal first and
// fails with ErrBusy after the timeout. Keys sends named tmux keys (Escape,
// Enter) and is used only for Interrupt and Submit, never for menus.
type Typist interface {
	Type(ctx context.Context, session, text string, submit, whenIdle bool) error
	Keys(ctx context.Context, session string, keys ...string) error
}

// Marker records where the pane's output is now, for anchors.
type Marker interface {
	Mark(ctx context.Context, session string) (Anchor, error)
	// Find resolves an anchor against the pane as it is now.
	Find(ctx context.Context, session string, a Anchor) (Location, error)
}

// Redactor strips secrets from anything about to leave the helper:
// object keys that name secrets lose their values, and string values that
// look like keys or tokens are replaced. It must be applied to every
// screen, item and row before encoding.
type Redactor interface {
	JSON(raw json.RawMessage) json.RawMessage
	String(s string) string
}

// Log holds a session's items in a bounded ring and assigns Seq.
type Log interface {
	Epoch(session string) string
	// Append assigns Seq to each item (replacing an item with the same ID)
	// and returns them as stored.
	Append(session string, items []Item) []Item
	Since(session string, after int64, limit int) (items []Item, cursor int64, more bool)
	Before(session string, before int64, limit int) (items []Item, more bool)
	Forget(session string)
}
