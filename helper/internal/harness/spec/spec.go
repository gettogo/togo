// Package spec reads harness adapter files: one YAML document per harness
// that declares where its data comes from (sources and feeds), which native
// screens it fills (screens) and how changes are applied (actions). The
// files hold no code; mappers that turn feeds into conversation items are
// named here and written in Go.
package spec

import (
	"time"
)

// Revision is the adapter file format this helper reads.
const Revision = 1

// Adapter is one adapter file.
type Adapter struct {
	Adapter  string `yaml:"adapter"`
	Name     string `yaml:"name"`
	Revision int    `yaml:"revision"`
	Match    Match  `yaml:"match"`
	Harness  Gate   `yaml:"harness"`
	// Endpoint says how the helper reaches the live harness process, when
	// it can.
	Endpoint *Endpoint `yaml:"endpoint"`
	// Capabilities are the flags harness.describe advertises when every
	// source behind them works.
	Capabilities []string           `yaml:"capabilities"`
	Feeds        map[string]Feed    `yaml:"feeds"`
	Sources      map[string]Source  `yaml:"sources"`
	Screens      map[string]Screen  `yaml:"screens"`
	Actions      map[string]Action  `yaml:"actions"`
	Modes        *Modes             `yaml:"modes"`
	Commands     *Commands          `yaml:"commands"`
	Mission      map[string]Mission `yaml:"mission"`
	// Images says how the harness takes pictures in a prompt.
	Images *Images `yaml:"images"`
	// Status declares what the mission Status block offers.
	Status *Status `yaml:"status"`
	// Input says how to read the harness's input line off the screen, so
	// typed text lands in an empty line and its Enter is seen to submit.
	Input *Input `yaml:"input"`
}

// Input describes the input line of a harness's TUI.
type Input struct {
	// Line is a regexp whose first group is the text in the input line;
	// the last row on screen that matches is the input line.
	Line string `yaml:"line"`
	// Empty are regexps for what the line shows when nothing is typed (a
	// placeholder, a key hint).
	Empty []string `yaml:"empty"`
	// Clear are the keys that empty the line (C-u). When they leave text,
	// one BSpace per character is sent.
	Clear []string `yaml:"clear"`
	// Dismiss are the keys that close a completion popup that took Enter
	// without submitting; Enter follows them.
	Dismiss []string `yaml:"dismiss"`
}

// InputKeys are the keys an Input block may name.
var InputKeys = map[string]bool{"Escape": true, "C-u": true, "C-a": true, "C-k": true, "End": true, "BSpace": true, "Tab": true}

// Status declares the Status block's inline actions, such as compact on
// the context meter. They replace a confirm-only screen.
type Status struct {
	Actions []string `yaml:"actions"`
}

// Images is how a harness takes image paths typed into its input: insert
// per_path pastes each path on its own, together pastes them at once.
type Images struct {
	Insert string `yaml:"insert"`
}

// Match picks the sessions an adapter serves.
type Match struct {
	// Agents are Togo agent ids from agents.list.
	Agents []string `yaml:"agents"`
	// Commands are program base names, for custom launches.
	Commands []string `yaml:"commands"`
}

// Gate reads the harness version and turns the adapter off outside Range.
type Gate struct {
	Version []string `yaml:"version"` // argv that prints the version
	Pattern string   `yaml:"pattern"` // regexp with one group
	Range   string   `yaml:"range"`   // ">=2.1.250 <3"
	// Stderr "merge" reads the version from stderr too, for harnesses
	// that print it there when stdin is not a terminal (pi).
	Stderr string `yaml:"stderr"`
}

// StderrMerge reads a program's stderr together with its stdout.
const StderrMerge = "merge"

// Endpoint kinds.
const (
	EndpointHTTP = "http" // a TCP port on 127.0.0.1 the helper picks
	EndpointUnix = "unix" // a unix socket under TOGO_HOME the helper picks
)

// Endpoint is how the helper reaches the live harness.
type Endpoint struct {
	Kind string `yaml:"kind"`
	// LaunchArgs are added to the harness argv when Togo starts it; they
	// may use {{.port}} or {{.socket}}.
	LaunchArgs []string `yaml:"launch_args"`
	// Server, when set, is started before the harness (Codex app-server).
	Server []string `yaml:"server"`
	// Health is a path (http) or method (unix JSON-RPC) that answers when
	// the endpoint is ready.
	Health string `yaml:"health"`
	// PasswordEnv names the environment variable the harness reads its
	// server password from (OpenCode: OPENCODE_SERVER_PASSWORD). The helper
	// sets it to a fresh random value for each start and sends it with
	// HTTP Basic auth as Username, so no other local process can drive the
	// harness through the port. Only names ending in _PASSWORD are taken.
	PasswordEnv string `yaml:"password_env"`
	Username    string `yaml:"username"`
}

// Stability tags. Sources and actions must carry one.
const (
	StableDocumented        = "documented"
	StableExperimental      = "experimental"
	StableInternal          = "internal"
	StableDocumentedCommand = "documented-command"
	// StableUnverified marks a route written from the harness's help,
	// docs or binary but never exercised, usually because it needs a
	// login. It behaves like experimental.
	StableUnverified = "unverified"
)

// Feed is a streaming input for the conversation and mission control. One
// kind block is set.
type Feed struct {
	Kinds `yaml:",inline"`
	// Mapper names the Go mapper, e.g. claude.transcript.
	Mapper    string `yaml:"mapper"`
	Stability string `yaml:"stability"`
}

// Source is a fetch-on-demand input for screens. One kind block is set.
type Source struct {
	Kinds `yaml:",inline"`
	// Select is a jq expression applied to the raw result.
	Select    string        `yaml:"select"`
	Cache     time.Duration `yaml:"cache"`
	Timeout   time.Duration `yaml:"timeout"`
	Stability string        `yaml:"stability"`
	// Redact lists jq paths removed before anything else reads the data,
	// on top of the engine's own secret stripping.
	Redact   []string `yaml:"redact"`
	Fallback *Source  `yaml:"fallback"`
}

// Kinds holds exactly one source kind block.
type Kinds struct {
	CLI        *CLI        `yaml:"cli"`
	File       *File       `yaml:"file"`
	JSONL      *File       `yaml:"jsonl"`
	HTTP       *HTTP       `yaml:"http"`
	SSE        *HTTP       `yaml:"sse"`
	RPC        *RPC        `yaml:"rpc"`
	Hook       *Hook       `yaml:"hook"`
	Statusline *Statusline `yaml:"statusline"`
	// Static is data written in the file itself: fixed model aliases,
	// mode lists, built-in commands.
	Static any `yaml:"static"`
}

// Source kind names, as Runner.Kind reports them.
const (
	KindCLI        = "cli"
	KindFile       = "file"
	KindJSONL      = "jsonl"
	KindHTTP       = "http"
	KindSSE        = "sse"
	KindRPC        = "rpc"
	KindHook       = "hook"
	KindStatusline = "statusline"
	KindStatic     = "static"
)

// CLI runs a program with no shell. Argv elements are templates.
type CLI struct {
	Argv []string `yaml:"argv"`
	// Format is json (default), jsonl, toml or text.
	Format string `yaml:"format"`
	// Stderr "merge" appends stderr to the output; empty drops it.
	Stderr string `yaml:"stderr"`
}

// File reads a file (json, toml) or tails one (jsonl). Path is a template
// and must resolve under the user's home or the session cwd. With Glob, the
// result is an array of {path, name, mtime, head, tail} objects, newest
// first, where head and tail hold the first and last records of a jsonl
// file (or the whole document of a json file in head).
type File struct {
	Path   string `yaml:"path"`
	Glob   string `yaml:"glob"`
	Format string `yaml:"format"`
	Follow bool   `yaml:"follow"`
}

// HTTP calls the harness's endpoint. Path is a template; the base URL is
// always the helper-assigned endpoint, never one from the file.
type HTTP struct {
	Method string `yaml:"method"`
	Path   string `yaml:"path"`
	Body   string `yaml:"body"`
}

// RPC calls a JSON-RPC method on the endpoint (Codex app-server) or on a
// one-shot child (omp --mode rpc).
type RPC struct {
	Method string            `yaml:"method"`
	Params map[string]string `yaml:"params"`
	// Notifications, for feeds, lists the notification methods to stream.
	Notifications []string `yaml:"notifications"`
	// Child, when set, is a one-shot process speaking the RPC on stdio.
	Child []string `yaml:"child"`
	// Framing is how a child frames its messages: jsonrpc (JSON-RPC 2.0,
	// the default) or typed ({"type": method, "id": n} commands answered
	// by {"type": "response", "id": n, "success", "data"}, as omp's rpc
	// mode speaks).
	Framing string `yaml:"framing"`
}

// RPC framings.
const (
	FramingJSONRPC = "jsonrpc"
	FramingTyped   = "typed"
)

// Hook streams the agent's hook events, which the togo hook command
// forwards to the daemon.
type Hook struct {
	Events []string `yaml:"events"`
}

// Statusline streams the status line payloads the togo hook command sees.
type Statusline struct {
	Fields []string `yaml:"fields"`
}

// Screen declares one native screen.
type Screen struct {
	Primitive  string `yaml:"primitive"`
	Title      string `yaml:"title"`
	Section    string `yaml:"section"`
	Capability string `yaml:"capability"`
	Source     string `yaml:"source"`
	// Map maps the selected source data onto the primitive. Keys depend
	// on the primitive (item.id, item.title, selected, field.value...);
	// values are jq expressions or templates.
	Map     map[string]string `yaml:"map"`
	Filters []Filter          `yaml:"filters"`
	Search  bool              `yaml:"search"`
	// OnSelect, Submit and Actions name entries in Actions.
	OnSelect string   `yaml:"on_select"`
	Submit   string   `yaml:"submit"`
	Actions  []string `yaml:"actions"`
	// Fields lists settings fields for a settings screen.
	Fields []SettingField `yaml:"fields"`
	// Setup lists, after a picker's items, what is not set up yet (a
	// provider not signed in), each with an action that sets it up.
	Setup *PickerSetup `yaml:"setup"`
}

// PickerSetup is a picker's "Not set up" section: Source's items mapped
// with Map (item.id, item.title, item.subtitle), offered with Action run
// with args {"id": item id}. An entry whose id names a group the picker
// already has is set up, and left out. Input "secret" asks for a value
// first (an API key), sent as args.value.
type PickerSetup struct {
	Source string            `yaml:"source"`
	Map    map[string]string `yaml:"map"`
	Action string            `yaml:"action"`
	Input  string            `yaml:"input"`
}

// Filter is a picker filter.
type Filter struct {
	ID      string `yaml:"id"`
	Label   string `yaml:"label"`
	Kind    string `yaml:"kind"`
	Field   string `yaml:"field"`
	Default string `yaml:"default"`
}

// SettingField declares one settings field.
type SettingField struct {
	ID      string   `yaml:"id"`
	Label   string   `yaml:"label"`
	Kind    string   `yaml:"kind"`
	Value   string   `yaml:"value"` // jq over the screen's source
	Options []string `yaml:"options"`
	Help    string   `yaml:"help"`
	Action  string   `yaml:"action"`
}

// Action declares how a change is applied. Exactly one route block is set,
// or Branch.
type Action struct {
	Label   string `yaml:"label"`
	Role    string `yaml:"role"`
	Confirm string `yaml:"confirm"`
	// Effect says what the action costs beyond its value: "relaunch"
	// restarts the harness. Derived from the routes when empty.
	Effect string `yaml:"effect"`
	// Note is said with a successful result, when the change has a catch
	// ("Applies to new threads").
	Note   string `yaml:"note"`
	Routes `yaml:",inline"`
	Branch []Branch `yaml:"branch"`
	Verify *Verify  `yaml:"verify"`
	// Stability is required.
	Stability string `yaml:"stability"`
}

// Routes holds exactly one way to apply an action.
type Routes struct {
	CLI      *CLI      `yaml:"cli"`
	HTTP     *HTTP     `yaml:"http"`
	RPC      *RPC      `yaml:"rpc"`
	Type     *Type     `yaml:"type"`
	Keys     *Keys     `yaml:"keys"`
	Relaunch *Relaunch `yaml:"relaunch"`
	Write    *Write    `yaml:"write"`
	Builtin  string    `yaml:"builtin"`
}

// Type types a documented command into the live session, e.g. "/model
// {{.id}}". Text must start with "/" unless Prompt is true (a plain prompt
// from the composer).
type Type struct {
	Text     string `yaml:"text"`
	Submit   bool   `yaml:"submit"`
	WhenIdle bool   `yaml:"when_idle"`
	Prompt   bool   `yaml:"prompt"`
}

// AllowedKeys are the only named keys an adapter may send. Menus are never
// driven with arrows; Escape interrupts, Enter submits, and BTab cycles a
// mode only when Keys.Cycle verifies each press.
var AllowedKeys = map[string]bool{"Escape": true, "Enter": true, "C-c": true, "BTab": true}

// StopKeys may be sent only as an until-idle interrupt's Then: chords that
// stop work Escape does not reach, such as Claude Code's background agents.
var StopKeys = map[string]bool{"C-x": true, "C-k": true}

// UntilIdle is the Until that presses an interrupt until the session is idle.
const UntilIdle = "idle"

// Keys sends named keys. Cycle repeats Keys until the verify source shows
// the wanted value, at most Max presses. Until "idle" repeats Keys, at most
// Max presses, until no turn runs and no approval waits; Then is sent once,
// after the first press.
type Keys struct {
	Keys  []string `yaml:"keys"`
	Cycle bool     `yaml:"cycle"`
	Max   int      `yaml:"max"`
	Until string   `yaml:"until"`
	Then  []string `yaml:"then"`
}

// Relaunch restarts the harness in place with a new argv (templates).
type Relaunch struct {
	Argv []string `yaml:"argv"`
}

// Write sets a key in a harness config file (json or toml), atomically.
type Write struct {
	Path   string `yaml:"path"`
	Format string `yaml:"format"`
	Key    string `yaml:"key"`
	Value  string `yaml:"value"`
}

// Branch is one conditional route; the first whose When holds runs. An
// empty When is the else branch.
type Branch struct {
	When   string `yaml:"when"`
	Routes `yaml:",inline"`
}

// Verify confirms an action took effect.
type Verify struct {
	Source string        `yaml:"source"`
	Select string        `yaml:"select"`
	Equals string        `yaml:"equals"`
	Within time.Duration `yaml:"within"`
	// Required makes a typed command that is not confirmed in the window
	// a failure that says what the harness shows instead, never "sent":
	// for changes the user relies on at once (the model of the next turn).
	Required bool `yaml:"required"`
}

// Modes declares the mode chip.
type Modes struct {
	Choices []ModeChoice `yaml:"choices"`
	Current string       `yaml:"current"` // a source name
	Select  string       `yaml:"select"`
	Set     string       `yaml:"set"` // an action name
	// Screen reads the mode off the pane for harnesses that report it
	// nowhere else: the last line that matches a rule names the mode;
	// with none, ScreenDefault (when set).
	Screen        []ScreenMode `yaml:"screen"`
	ScreenDefault string       `yaml:"screen_default"`
}

// ScreenMode is one screen rule: a line regexp and the mode it means.
type ScreenMode struct {
	Line string `yaml:"line"`
	ID   string `yaml:"id"`
}

// ModeChoice is one mode.
type ModeChoice struct {
	ID     string `yaml:"id"`
	Label  string `yaml:"label"`
	Detail string `yaml:"detail"`
}

// Commands declares the / menu.
type Commands struct {
	Source string          `yaml:"source"`
	Select string          `yaml:"select"`
	Static []StaticCommand `yaml:"static"`
}

// StaticCommand is a slash command the adapter lists itself.
type StaticCommand struct {
	Name   string `yaml:"name"`
	Hint   string `yaml:"hint"`
	Detail string `yaml:"detail"`
	Screen string `yaml:"screen"`
	// Kind is how the app runs the command: choice, picker, args, run or
	// terminal. Derived when empty (CommandKind).
	Kind string `yaml:"kind"`
	// Action runs this action instead of typing the command, so its
	// confirm and role apply.
	Action string `yaml:"action"`
	// Role is "destructive" for a command the app asks about first.
	Role string `yaml:"role"`
	// Opens names a native home shown instead of typing: status, changes
	// or a screen.
	Opens string `yaml:"opens"`
}

// Mission declares where a mission control section's rows come from when a
// feed mapper does not fill it.
type Mission struct {
	Source  string            `yaml:"source"`
	Map     map[string]string `yaml:"map"`
	Refresh time.Duration     `yaml:"refresh"`
}
