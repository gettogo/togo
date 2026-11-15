package harness

import (
	"encoding/json"
	"time"
)

// Action roles.
const (
	RoleNormal      = "normal"
	RolePrimary     = "primary"
	RoleDestructive = "destructive"
)

// ActionRef offers an action on a screen, row or status block.
type ActionRef struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Role  string `json:"role"`
	// Confirm, when set, is asked before the action runs.
	Confirm string `json:"confirm,omitempty"`
	// Args are fixed args merged under the user's.
	Args map[string]json.RawMessage `json:"args,omitempty"`
}

// Built-in action ids every adapter may declare.
const (
	ActionAnswer    = "answer"    // answer a question item: args {"answers": {...}}
	ActionSetMode   = "set_mode"  // args {"id": mode id}
	ActionInterrupt = "interrupt" // same as harness.interrupt
	ActionStop      = "stop"      // stop an agent or background job: Target is its id
)

// ActionRequest is harness.action params.
type ActionRequest struct {
	Session string `json:"id"`
	Action  string `json:"action"`
	// Target is the item or row the action is about, when it has one.
	Target string                     `json:"target,omitempty"`
	Args   map[string]json.RawMessage `json:"args,omitempty"`
}

// Action outcomes.
const (
	// ResultApplied: the harness confirmed the change (an API answer, or the
	// verify source showed it).
	ResultApplied = "applied"
	// ResultTyped: a documented command was typed; the verify window passed
	// without confirmation, so the app says "Sent" rather than "Done".
	ResultTyped = "typed"
	// ResultRelaunched: the agent was restarted with new flags.
	ResultRelaunched = "relaunched"
	// ResultQueued: held until the agent is idle.
	ResultQueued = "queued"
	// ResultFailed: the route failed; Message says why.
	ResultFailed = "failed"
	// ResultFallback: no route works for this harness version; the app
	// opens the terminal.
	ResultFallback = "fallback"
)

// ActionResult is harness.action's result.
type ActionResult struct {
	Result  string `json:"result"`
	Message string `json:"message,omitempty"`
	// Screen is the screen read again after the action, when the action
	// came from one.
	Screen *Screen `json:"screen,omitempty"`
}

// Send modes.
const (
	SendNow   = "now"   // type now; refused with -32006 while the agent works
	SendQueue = "queue" // type now when idle, else hold until the turn ends
)

// SendRequest is harness.send params.
type SendRequest struct {
	Session string `json:"id"`
	Text    string `json:"text"`
	Mode    string `json:"mode"`
	// Images are absolute paths of pictures sent with the prompt, usually
	// files fs.upload wrote. They go in the same prompt, inserted the way
	// the adapter's images.insert says.
	Images []string `json:"images,omitempty"`
}

// SendResult is harness.send's result.
type SendResult struct {
	// Item is the user item the prompt became; Queued says whether it waits.
	Item Item `json:"item"`
}

// Completion kinds.
const (
	CompleteCommand = "command"
	CompleteFile    = "file"
)

// CompleteRequest is harness.complete params.
type CompleteRequest struct {
	Session string `json:"id"`
	Kind    string `json:"kind"`
	Query   string `json:"query"`
	Limit   int    `json:"limit,omitempty"`
}

// Completion is one suggestion for the / or @ menu.
type Completion struct {
	// Insert is the text the composer inserts, e.g. "/compact " or
	// "@Sources/App.swift ".
	Insert string `json:"insert"`
	Title  string `json:"title"`
	Detail string `json:"detail,omitempty"`
	// Hint is the argument hint, e.g. "[instructions]".
	Hint string `json:"hint,omitempty"`
	// Screen, when set, opens a native screen instead of inserting text
	// (the / menu's "model" opens the model picker).
	Screen string `json:"screen,omitempty"`
	// Kind is how the app runs it: choice, picker, args, run or terminal.
	Kind string `json:"kind,omitempty"`
	// Role is "destructive" when the app asks before running it.
	Role string `json:"role,omitempty"`
	// Action is an action id run instead of typing the command. For a
	// choice it is the action a picked value runs, with args {"id": v,
	// "value": v}.
	Action string `json:"action,omitempty"`
	// Opens names a native home shown instead of typing: status, changes
	// or a screen id.
	Opens string `json:"opens,omitempty"`
	// Choices are a choice command's values, the current one selected.
	Choices []CommandChoice `json:"choices,omitempty"`
	// Native describes the command as the running program has it. It is set
	// only when the list came from the program itself, which is then the
	// complete list; without it the list may leave commands out.
	Native *CommandNative `json:"native,omitempty"`
}

// What a command does when run, for CommandNative.Opens. Apps treat a value
// they do not know as OpensUnknown.
const (
	OpensPicker   = "picker"   // a list to choose from
	OpensSettings = "settings" // a settings editor
	OpensSignIn   = "signin"   // a provider sign-in
	OpensScreen   = "screen"   // a larger screen of its own (ScreenID names it)
	OpensInput    = "input"    // asks for text or a confirmation
	OpensText     = "text"     // shows a result
	OpensToggle   = "toggle"   // flips a setting
	OpensAction   = "action"   // acts at once
	OpensTurn     = "turn"     // sends a prompt to the model
	OpensExit     = "exit"     // ends or restarts the program
	OpensUnknown  = "unknown"  // not known ahead: what the program draws decides
)

// CommandNative is what the running program and its source say about a
// command. Every field is optional.
type CommandNative struct {
	// Opens is what the command does with no arguments; OpensWithArgs what
	// it does with them, when that differs.
	Opens         string `json:"opens,omitempty"`
	OpensWithArgs string `json:"opensWithArgs,omitempty"`
	// Args is none, optional or required.
	Args string `json:"args,omitempty"`
	// State is the program's current value for the command (a model, on).
	State       string       `json:"state,omitempty"`
	Subcommands []CommandSub `json:"subcommands,omitempty"`
	Aliases     []string     `json:"aliases,omitempty"`
	// Group is the folder the program lists the command under ("skill:").
	Group string `json:"group,omitempty"`
	// Destructive is set from the program's source: data loss, a discarded
	// context, a git change or the end of the process.
	Destructive bool `json:"destructive,omitempty"`
	// Icon is the program's own name for the command's icon.
	Icon string `json:"icon,omitempty"`
	// Source is builtin, skill, file, prompt, extension or custom.
	Source string `json:"source,omitempty"`
	// ScreenID names what a command shows (a picker, settings, sign-in, a
	// screen, an input or a result): the command's canonical name unless the
	// adapter names it. Absent for commands that show nothing or are unknown.
	ScreenID string `json:"screenId,omitempty"`
}

// CommandSub is one subcommand of a command.
type CommandSub struct {
	Name   string `json:"name"`
	Detail string `json:"detail,omitempty"`
	Hint   string `json:"hint,omitempty"`
}

// MaxSubcommands bounds a command's subcommands.
const MaxSubcommands = 32

// LiveCommand is one command as the session's running program lists it.
type LiveCommand struct {
	Name, Detail, State, Icon, Group string
	// Folder marks an entry that only groups other commands.
	Folder bool
}

// MaxChoices bounds a completion's choices.
const MaxChoices = 50

// CommandChoice is one value of a choice command.
type CommandChoice struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Selected bool   `json:"selected,omitempty"`
}

// CompleteResult is harness.complete's result.
type CompleteResult struct {
	Items []Completion `json:"items"`
	// Pending is true while more commands are being read from the harness
	// itself: ask again shortly for the rest.
	Pending bool `json:"pending,omitempty"`
}

// OpenResult is harness.open's result: the items after the cursor and the
// mission snapshot. Notifications for the session follow it on the
// connection until harness.close or the connection ends.
type OpenResult struct {
	Epoch string `json:"epoch"`
	// Cursor is the Seq of the newest item the helper holds; the client
	// drops notified items with Seq <= Cursor.
	Cursor int64  `json:"cursor"`
	Items  []Item `json:"items"`
	// More is true when older items exist before Items[0] (harness.history).
	More    bool    `json:"more"`
	Mission Mission `json:"mission"`
	// CoveredAfter is the lower bound after which this response contains
	// every retained item through Cursor, including count/byte truncation.
	CoveredAfter *int64 `json:"coveredAfter,omitempty"`
	// DiscardedThrough advances only when ring capacity evicts an item.
	DiscardedThrough *int64 `json:"discardedThrough,omitempty"`
}

// Page is harness.history's result.
type Page struct {
	Items            []Item `json:"items"`
	More             bool   `json:"more"`
	Epoch            string `json:"epoch,omitempty"`
	DiscardedThrough *int64 `json:"discardedThrough,omitempty"`
}

// Location is harness.locate's result.
type Location struct {
	// Found is false when the anchor scrolled out of history and its needle
	// is not on screen.
	Found bool `json:"found"`
	// LinesFromBottom is how far above the last line of the pane the anchor
	// sits now.
	LinesFromBottom int64 `json:"linesFromBottom"`
	// Needle is the text to highlight on that line.
	Needle string `json:"needle,omitempty"`
}

// Notification kinds of the `harness` notification.
const (
	NoteItems   = "items"
	NoteMission = "mission"
	NoteReset   = "reset"
	// NoteCommands: the session's command list has more in it now (the
	// harness's own list was read). It carries no payload: ask again.
	NoteCommands = "commands"
)

// NotificationMethod is the JSON-RPC method of harness notifications.
const NotificationMethod = "harness"

// Notification is the params of a `harness` notification, sent only to a
// connection that opened the session with harness.open.
type Notification struct {
	SessionID string    `json:"sessionId"`
	Kind      string    `json:"kind"`
	TS        time.Time `json:"ts"`
	Epoch     string    `json:"epoch"`
	Items     []Item    `json:"items,omitempty"`
	Mission   *Delta    `json:"mission,omitempty"`
	// Reason says why a reset was sent: adapter, epoch or overflow.
	Reason string `json:"reason,omitempty"`
}
