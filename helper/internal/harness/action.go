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
}

// CompleteResult is harness.complete's result.
type CompleteResult struct {
	Items []Completion `json:"items"`
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
}

// Page is harness.history's result.
type Page struct {
	Items []Item `json:"items"`
	More  bool   `json:"more"`
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
