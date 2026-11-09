// Package wire holds the protocol v1 shapes beyond sessions, agents and
// files (events, approvals, local hook and tap calls, pairing) so that the
// packages serving them agree on one Go form of protocol/v1/schema.json. It
// has no behaviour beyond encoding, id generation and decision parsing.
package wire

import (
	"crypto/rand"
	"encoding/base32"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Hello capabilities. A process lists only the ones it serves.
const (
	CapEvents    = "events.v1"
	CapApprovals = "approvals.v1"
	CapMute      = "mute.v1"
	CapPush      = "push.v1"
	CapGit       = "git.v1"
	CapPorts     = "ports.v1"
	CapGitWrite  = "gitwrite.v1"
	CapLaunch    = "launch.v1"
	CapLifecycle = "lifecycle.v1"
	CapRestore   = "restore.v1"
	CapNotify    = "notify.v1"
	CapReply     = "reply.v1"
	CapAuthkeys  = "authkeys.v1"
	CapHostInfo  = "hostinfo.v1"
	// CapDoneSilent: push.pair takes doneSilent.
	CapDoneSilent = "donesilent.v1"
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
	MethodGitStage        = "git.stage"
	MethodGitUnstage      = "git.unstage"
	MethodGitDiscard      = "git.discard"
	MethodGitCommit       = "git.commit"
	MethodFsRead          = "fs.read"
	MethodFsUpload        = "fs.upload"
	MethodFsImage         = "fs.image"
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
	// MethodLocalShutdown asks the daemon to exit with status 75 so its
	// service manager, or the next serve, starts the installed binary.
	MethodLocalShutdown = "local.shutdown"
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
	ConflictRunning    = "running"
	ConflictShell      = "shell"
	ConflictEnded      = "ended"
	ConflictNeedsYou   = "needs_you"
	ConflictSymlink    = "symlink"
	ConflictMissing    = "missing"
	ConflictNotRegular = "not_regular"

	ByDesk  = "desk"
	ByPhone = "phone"
)

// Conflict reasons for refused git writes, carried in -32006 data. Nothing
// in the repository changed when one is returned, except for partial (a
// write failed after changing the paths listed in changed) and raced (the
// commit hash was made, but on a HEAD another process moved meanwhile).
const (
	ConflictStale         = "stale"
	ConflictLocked        = "locked"
	ConflictFiltered      = "filtered"
	ConflictUnmerged      = "unmerged"
	ConflictNothingStaged = "nothing_staged"
	ConflictIdentity      = "identity"
	ConflictUnbornAmend   = "unborn_amend"
	ConflictPartial       = "partial"
	ConflictRaced         = "raced"
	// ConflictCaseRename refuses undoing a rename that only changes letter
	// case: on a case-insensitive disk both names are one file.
	ConflictCaseRename = "case_rename"
)

// MaxBlobFingerprintBytes is the largest working-tree file fingerprinted by
// its blob hash; larger files and symlinks use size and mtime instead.
const MaxBlobFingerprintBytes = 8 << 20

// GitSeen is what the client saw for one path of a git write: the index and
// worktree status letters, and the entry's fingerprint when it had one.
type GitSeen struct {
	Status      string `json:"status"`
	Fingerprint string `json:"fingerprint,omitempty"`
}

// HashesForFingerprint reports whether a regular file of size bytes is
// fingerprinted by its blob hash.
func HashesForFingerprint(size int64) bool { return size <= MaxBlobFingerprintBytes }

// BlobFingerprint is the fingerprint of a file with this git blob hash
// (git hash-object --no-filters).
func BlobFingerprint(hash string) string { return "blob:" + hash }

// StatFingerprint is the fingerprint of a large file or symlink from its
// lstat size and modification time in nanoseconds since the epoch.
func StatFingerprint(size, mtimeNs int64) string {
	return "stat:" + strconv.FormatInt(size, 10) + ":" + strconv.FormatInt(mtimeNs, 10)
}

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
	MaxGitPaths     = 1000
	MaxCommitBytes  = 65536
	// MaxStatusFiles caps git.status files; beyond it the result is truncated.
	MaxStatusFiles = 2000
	MaxSubjectText = 1024
	MaxHostName    = 64
)

// IsFileEntry reports whether a git write path can name a single file. A
// folder entry (trailing /) is invalid params; whether the path is a
// directory on disk is checked where the path is resolved.
func IsFileEntry(p string) bool { return p != "" && !strings.HasSuffix(p, "/") }

// MaxWritePathBytes is the longest path a git write accepts, in bytes.
const MaxWritePathBytes = 4096

// ValidWritePath reports whether p may be sent to a git write: a file
// entry of at most MaxWritePathBytes bytes with no NUL. Anything else is
// invalid params. Escapes and symlinks are checked where p is resolved.
func ValidWritePath(p string) bool {
	return IsFileEntry(p) && len(p) <= MaxWritePathBytes && !strings.ContainsRune(p, 0)
}

// ValidCommitHash reports whether h is a full SHA-1 or SHA-256 commit id
// in lowercase hex.
func ValidCommitHash(h string) bool {
	if len(h) != 40 && len(h) != 64 {
		return false
	}
	for _, c := range h {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

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

// Subject kinds: what an approval is about.
const (
	SubjectCommand = "command"
	SubjectFile    = "file"
	SubjectURL     = "url"
	SubjectText    = "text"
)

// Subject is the one thing an approval asks about, structured so that
// notifications, the Live Activity and the Watch can show it without any
// screen text.
type Subject struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
}

// Validate checks s against the schema: a known kind and 1-1024 runes
// (Unicode scalar values, not bytes) of UTF-8 with no control characters
// (Unicode Cc: U+0000-001F, U+007F-009F) other than newline and no
// invisible characters (see IsInvisible). Errors never quote the text.
func (s Subject) Validate() error {
	switch s.Kind {
	case SubjectCommand, SubjectFile, SubjectURL, SubjectText:
	default:
		return errors.New("subject kind must be command, file, url or text")
	}
	if s.Text == "" || !utf8.ValidString(s.Text) || utf8.RuneCountInString(s.Text) > MaxSubjectText {
		return errors.New("subject text must be 1-1024 characters of UTF-8")
	}
	if strings.ContainsFunc(s.Text, func(r rune) bool { return r != '\n' && unicode.IsControl(r) }) {
		return errors.New("subject text contains a control character")
	}
	if strings.ContainsFunc(s.Text, IsInvisible) {
		return errors.New("subject text contains an invisible character")
	}
	return nil
}

// IsInvisible reports bidirectional overrides, embeddings, isolates and
// marks (U+202A-202E, U+2066-2069, U+200E, U+200F, U+061C) and zero-width
// characters (U+200B-200D, U+2060, U+FEFF). They can make a command read
// differently from the bytes that run, so a subject may not contain them
// and displayed text drops them.
func IsInvisible(r rune) bool {
	switch {
	case r >= 0x202A && r <= 0x202E, r >= 0x2066 && r <= 0x2069, r >= 0x200B && r <= 0x200F:
		return true
	case r == 0x061C, r == 0x2060, r == 0xFEFF:
		return true
	}
	return false
}

// UnmarshalJSON rejects a subject that fails Validate, so a decoded Subject
// is always one the schema allows.
func (s *Subject) UnmarshalJSON(b []byte) error {
	type plain Subject
	var v plain
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	if err := Subject(v).Validate(); err != nil {
		return err
	}
	*s = Subject(v)
	return nil
}

// Approval is a pending question for the user. State is always "pending"
// on the wire: answered approvals are announced by approval.closed and
// never listed. When Subject is set, Detail is Subject.Text truncated to
// MaxDetail runes, for apps that predate Subject.
type Approval struct {
	RequestID string   `json:"requestId"`
	SessionID *string  `json:"sessionId"`
	Bib       *string  `json:"bib"`
	Agent     string   `json:"agent"`
	Source    string   `json:"source"`
	Title     string   `json:"title"`
	Detail    *string  `json:"detail"`
	Subject   *Subject `json:"subject"`
	Snippet   string   `json:"snippet"`
	Options   []Option `json:"options"`
	AllowText bool     `json:"allowText"`
	// SignIn is set when the prompt is a sign-in step (signin.v1).
	SignIn *SignIn `json:"signIn,omitempty"`
	// Answerable is false while no answer from the phone can be delivered
	// (no options and no fingerprint yet). Nil means true.
	Answerable *bool      `json:"answerable,omitempty"`
	CreatedAt  time.Time  `json:"createdAt"`
	ExpiresAt  *time.Time `json:"expiresAt"`
	State      string     `json:"state"`
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
	// SignalExit is sent by the launch wrapper when the agent exits.
	SignalExit = "exit"
)

// Signal is the local.signal params. Code is the agent's exit status, set
// for SignalExit only.
type Signal struct {
	SessionID string `json:"sessionId"`
	Kind      string `json:"kind"`
	Title     string `json:"title,omitempty"`
	Body      string `json:"body,omitempty"`
	Code      *int   `json:"code,omitempty"`
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
	// Subject is what a permission hook asks about, built by the adapter
	// from the tool and its input.
	Subject *Subject `json:"subject,omitempty"`
}

// AskParams is the local.ask params. The call blocks until an answer or the
// timeout.
type AskParams struct {
	SessionID  *string  `json:"sessionId"`
	Title      string   `json:"title"`
	Options    []string `json:"options"`
	AllowText  bool     `json:"allowText"`
	TimeoutSec int      `json:"timeoutSec"`
	// Approve and Deny name the option that answers approve or deny from
	// a notification. Empty means the role is inferred from the labels.
	Approve string `json:"approve,omitempty"`
	Deny    string `json:"deny,omitempty"`
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

// ShutdownParams is the local.shutdown params. When is ShutdownIdle: exit
// now when nothing is pending, else once the last pending approval or ask
// resolves. Reason is recorded in the daemon log.
type ShutdownParams struct {
	Reason string `json:"reason"`
	When   string `json:"when"`
}

// ShutdownParams values.
const (
	ShutdownIdle    = "idle"
	ShutdownUpgrade = "upgrade"
	ShutdownRestart = "restart"
)

// ShutdownResult answers local.shutdown. Deferred means the daemon waits
// for Pending approvals and asks before it exits.
type ShutdownResult struct {
	Deferred bool `json:"deferred"`
	Pending  int  `json:"pending"`
}

// Push environments.
const (
	EnvSandbox    = "sandbox"
	EnvProduction = "production"
)

// Device platforms in PairParams.Platform.
const (
	PlatformIOS   = "ios"
	PlatformWatch = "watchos"
)

// PairParams is the push.pair params. Keys are base64url without padding.
// An empty Platform means PlatformIOS (see EffectivePlatform). LiveActivityCommand
// is the user's opt-in to command text in Live Activity pushes.
type PairParams struct {
	DeviceID          string `json:"deviceId"`
	Name              string `json:"name"`
	X25519            string `json:"x25519"`
	ApprovalKey       string `json:"approvalKey"`
	APNsToken         string `json:"apnsToken"`
	Env               string `json:"env"`
	RelayURL          string `json:"relayUrl"`
	LiveActivityToken string `json:"liveActivityToken,omitempty"`

	Platform               string `json:"platform,omitempty"`
	LiveActivityStartToken string `json:"liveActivityStartToken,omitempty"`
	LiveActivityCommand    bool   `json:"liveActivityCommand,omitempty"`
	// DoneSilent is the user's choice to have done arrive without sound.
	// The device's notification service applies it to pushes; the host
	// applies it to the Live Activity update that stands in for one.
	DoneSilent bool `json:"doneSilent,omitempty"`
	// SSHKey is the public key the device signs in with over SSH; Device
	// stores it under the same name.
	SSHKey string `json:"sshKey,omitempty"`
}

// EffectivePlatform returns Platform, or PlatformIOS for devices that paired
// before the field existed. Readers use it instead of Platform. There is no
// UnmarshalJSON default because types embedding PairParams would inherit it
// and lose their own fields.
func (p PairParams) EffectivePlatform() string {
	if p.Platform == "" {
		return PlatformIOS
	}
	return p.Platform
}

// PairResult answers push.pair. HostKey is the helper's X25519 public key.
type PairResult struct {
	HostID  string `json:"hostId"`
	HostKey string `json:"hostKey"`
}

// Device is a paired phone as the pairing store keeps it. It is never sent
// to the app and its tokens and keys are never logged. Its SSH key is the
// embedded PairParams.SSHKey, which authkeys.remove takes off the machine.
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
