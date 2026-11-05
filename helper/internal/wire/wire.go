// Package wire holds the protocol v1 shapes beyond sessions, agents and
// files (events, approvals, local hook and tap calls, pairing) so that the
// packages serving them agree on one Go form of protocol/v1/schema.json. It
// has no behaviour beyond encoding, id generation and decision parsing.
package wire

import (
	"crypto/rand"
	"encoding/base32"
	"encoding/json"
	"strings"
	"time"
)

// Hello capabilities. A process lists only the ones it serves.
const (
	CapEvents    = "events.v1"
	CapApprovals = "approvals.v1"
	CapMute      = "mute.v1"
	CapPush      = "push.v1"
	CapGit       = "git.v1"
	CapPorts     = "ports.v1"
)

// App-facing methods.
const (
	MethodEventsSubscribe = "events.subscribe"
	MethodEventsKeepalive = "events.keepalive"
	MethodApprovalList    = "approval.list"
	MethodApprovalAnswer  = "approval.answer"
	MethodSessionSetMute  = "session.setMute"
	MethodPushPair        = "push.pair"
	MethodPushUnpair      = "push.unpair"
	MethodGitStatus       = "git.status"
	MethodGitDiff         = "git.diff"
	MethodGitLog          = "git.log"
	MethodGitBranches     = "git.branches"
	MethodFsRead          = "fs.read"
	MethodFsUpload        = "fs.upload"
	MethodPortsList       = "ports.list"
)

// Local methods, served only on the daemon's unix socket and never listed
// in hello capabilities.
const (
	MethodLocalSignal = "local.signal"
	MethodLocalHook   = "local.hook"
	MethodLocalAsk    = "local.ask"
	MethodLocalNotify = "local.notify"
	MethodLocalJob    = "local.job"
)

// NotificationMethod is the JSON-RPC method of every event notification.
const NotificationMethod = "event"

// Event kinds.
const (
	EventSession        = "session"
	EventSessionRemoved = "session.removed"
	EventApprovalOpen   = "approval.open"
	EventApprovalClosed = "approval.closed"
	EventNotify         = "notify"
	EventJob            = "job"
)

// Approval sources, option roles and closing resolutions.
const (
	SourceHook   = "hook"
	SourceScreen = "screen"
	SourceAsk    = "ask"

	RoleApprove       = "approve"
	RoleApproveAlways = "approve_always"
	RoleDeny          = "deny"
	RoleOther         = "other"

	ResolutionPhone   = "phone"
	ResolutionDesk    = "desk"
	ResolutionExpired = "expired"
	ResolutionGone    = "gone"
)

// Conflict reasons and answerers carried in -32006 data.
const (
	ConflictAnswered   = "answered"
	ConflictPromptGone = "prompt_gone"
	ConflictExpired    = "expired"

	ByDesk  = "desk"
	ByPhone = "phone"
)

// Limits from the schema, enforced by the packages that build these shapes.
const (
	MaxTitle        = 200
	MaxDetail       = 256
	MaxBody         = 256
	MaxSnippetLines = 40
	MaxSnippetBytes = 4096
	MaxOptionLabel  = 80
	MaxAskOptions   = 9
	MaxDeviceName   = 64
)

// Publisher fans an event out to subscribers. sessionID is nil for events
// that belong to no session. payload is one of the *Event payload structs
// below; Publish assigns seq and ts.
type Publisher interface {
	Publish(kind string, sessionID *string, payload any)
}

// SessionMeta is what agent hooks report about a session. Every field is
// null until a hook says otherwise.
type SessionMeta struct {
	Model          *string  `json:"model"`
	ContextPercent *float64 `json:"contextPercent"`
	CostUSD        *float64 `json:"costUsd"`
}

// Option is one answer offered by an approval.
type Option struct {
	N     int    `json:"n"`
	Label string `json:"label"`
	Role  string `json:"role"`
}

// Approval is a pending question for the user. State is always "pending"
// on the wire: answered approvals are announced by approval.closed and
// never listed.
type Approval struct {
	RequestID string     `json:"requestId"`
	SessionID *string    `json:"sessionId"`
	Bib       *string    `json:"bib"`
	Agent     string     `json:"agent"`
	Source    string     `json:"source"`
	Title     string     `json:"title"`
	Detail    *string    `json:"detail"`
	Snippet   string     `json:"snippet"`
	Options   []Option   `json:"options"`
	AllowText bool       `json:"allowText"`
	CreatedAt time.Time  `json:"createdAt"`
	ExpiresAt *time.Time `json:"expiresAt"`
	State     string     `json:"state"`
}

// MarshalJSON writes nil Options as [], which the schema requires.
func (a Approval) MarshalJSON() ([]byte, error) {
	type plain Approval
	if a.Options == nil {
		a.Options = []Option{}
	}
	return json.Marshal(plain(a))
}

// StatePending is the only Approval.State on the wire.
const StatePending = "pending"

// Job statuses.
const (
	JobRunning = "running"
	JobOK      = "ok"
	JobFailed  = "failed"
)

// Job is a long-running task reported by `togo job`.
type Job struct {
	JobID     string     `json:"jobId"`
	Name      string     `json:"name"`
	Progress  *int       `json:"progress"`
	Message   *string    `json:"message"`
	Status    string     `json:"status"`
	StartedAt time.Time  `json:"startedAt"`
	EndedAt   *time.Time `json:"endedAt"`
}

// Notify levels.
const (
	LevelInfo     = "info"
	LevelDone     = "done"
	LevelNeedsYou = "needs_you"
)

// Event payloads, one per kind. Session is the protocol Session object; it
// is typed any so this package does not depend on the session package.
type (
	SessionEvent struct {
		Session any `json:"session"`
	}
	SessionRemovedEvent struct{}
	ApprovalOpenEvent   struct {
		Approval Approval `json:"approval"`
	}
	ApprovalClosedEvent struct {
		RequestID  string `json:"requestId"`
		Resolution string `json:"resolution"`
	}
	NotifyEvent struct {
		Title string `json:"title"`
		Body  string `json:"body"`
		Level string `json:"level"`
	}
	JobEvent struct {
		Job Job `json:"job"`
	}
)

// SubscribeResult answers events.subscribe: the state as of Seq, after which
// every event with a larger seq is delivered.
type SubscribeResult struct {
	Seq       int64      `json:"seq"`
	Sessions  []any      `json:"sessions"`
	Approvals []Approval `json:"approvals"`
}

// MarshalJSON writes nil slices as [], which the schema requires.
func (r SubscribeResult) MarshalJSON() ([]byte, error) {
	type plain SubscribeResult
	if r.Sessions == nil {
		r.Sessions = []any{}
	}
	if r.Approvals == nil {
		r.Approvals = []Approval{}
	}
	return json.Marshal(plain(r))
}

// Signal kinds sent by `togo tap`.
const (
	SignalBell   = "bell"
	SignalOSC9   = "osc9"
	SignalOSC777 = "osc777"
	SignalOutput = "output"
)

// Signal is the local.signal params.
type Signal struct {
	SessionID string `json:"sessionId"`
	Kind      string `json:"kind"`
	Title     string `json:"title,omitempty"`
	Body      string `json:"body,omitempty"`
}

// HookEvent is the local.hook params: one agent hook invocation, reduced by
// the adapter to the fields the helper uses. The raw payload never leaves
// the adapter.
type HookEvent struct {
	Agent     string   `json:"agent"`
	Event     string   `json:"event"`
	SessionID *string  `json:"sessionId"`
	Cwd       string   `json:"cwd"`
	Data      HookData `json:"data"`
}

// HookData is the adapter's summary of a hook payload.
type HookData struct {
	Tool           string   `json:"tool,omitempty"`
	Command        string   `json:"command,omitempty"`
	Message        string   `json:"message,omitempty"`
	Model          string   `json:"model,omitempty"`
	ContextPercent *float64 `json:"contextPercent,omitempty"`
	CostUSD        *float64 `json:"costUsd,omitempty"`
	AgentSessionID string   `json:"agentSessionId,omitempty"`
}

// AskParams is the local.ask params. The call blocks until an answer or the
// timeout.
type AskParams struct {
	SessionID  *string  `json:"sessionId"`
	Title      string   `json:"title"`
	Options    []string `json:"options"`
	AllowText  bool     `json:"allowText"`
	TimeoutSec int      `json:"timeoutSec"`
}

// AskResult answers local.ask. Decision is nil on timeout.
type AskResult struct {
	Decision *string `json:"decision"`
	Reason   string  `json:"reason"`
}

// AskResult reasons.
const (
	AskAnswered = "answered"
	AskTimeout  = "timeout"
)

// NotifyParams is the local.notify params. Strict asks for delivered=false
// to be reported as a failure by the CLI.
type NotifyParams struct {
	SessionID *string `json:"sessionId"`
	Title     string  `json:"title"`
	Body      string  `json:"body"`
	Strict    bool    `json:"strict"`
}

// NotifyResult answers local.notify.
type NotifyResult struct {
	Delivered bool `json:"delivered"`
}

// Job operations for local.job.
const (
	JobStart  = "start"
	JobUpdate = "update"
	JobEnd    = "end"
)

// JobParams is the local.job params. JobID is required for update and end.
type JobParams struct {
	Op       string  `json:"op"`
	JobID    string  `json:"jobId,omitempty"`
	Name     string  `json:"name,omitempty"`
	Progress *int    `json:"progress,omitempty"`
	Message  *string `json:"message,omitempty"`
	Status   string  `json:"status,omitempty"`
}

// JobResult answers local.job.
type JobResult struct {
	JobID string `json:"jobId"`
}

// Push environments.
const (
	EnvSandbox    = "sandbox"
	EnvProduction = "production"
)

// PairParams is the push.pair params. Keys are base64url without padding.
type PairParams struct {
	DeviceID          string `json:"deviceId"`
	Name              string `json:"name"`
	X25519            string `json:"x25519"`
	ApprovalKey       string `json:"approvalKey"`
	APNsToken         string `json:"apnsToken"`
	Env               string `json:"env"`
	RelayURL          string `json:"relayUrl"`
	LiveActivityToken string `json:"liveActivityToken,omitempty"`
}

// PairResult answers push.pair. HostKey is the helper's X25519 public key.
type PairResult struct {
	HostID  string `json:"hostId"`
	HostKey string `json:"hostKey"`
}

// Device is a paired phone as the pairing store keeps it. It is never sent
// to the app and its tokens and keys are never logged.
type Device struct {
	PairParams
	PairedAt time.Time `json:"pairedAt"`
}

var idEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// NewRequestID returns an approval id: "r_" and 10 lowercase base32 characters.
func NewRequestID() string { return "r_" + randomID() }

// NewJobID returns a job id: "j_" and 10 lowercase base32 characters.
func NewJobID() string { return "j_" + randomID() }

func randomID() string {
	b := make([]byte, 7)
	_, _ = rand.Read(b) // crypto/rand.Read never fails since Go 1.24.
	return strings.ToLower(idEncoding.EncodeToString(b))[:10]
}

// Stamp returns t in the form the schema's Timestamp expects from the
// helper: UTC, whole seconds.
func Stamp(t time.Time) time.Time { return t.UTC().Truncate(time.Second) }
