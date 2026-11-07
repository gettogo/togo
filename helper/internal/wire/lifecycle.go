package wire

import (
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// App-facing methods for launching, ending, restoring and replying to
// sessions, and for the machine's notification, key and network settings.
const (
	MethodAgentsCheck       = "agents.check"
	MethodSessionRelaunch   = "session.relaunch"
	MethodSessionRestorable = "session.restorable"
	MethodSessionRestore    = "session.restore"
	MethodSessionDismiss    = "session.dismiss"
	MethodSessionSendText   = "session.sendText"
	MethodNotifyGet         = "notify.get"
	MethodNotifySet         = "notify.set"
	MethodAuthkeysRemove    = "authkeys.remove"
	MethodHostInfo          = "host.info"
)

// AgentCustom is the agent id of a program the user names in
// SessionCreateParams.Program.
const AgentCustom = "custom"

// Session statuses for an agent process that exited. Neither is ever
// reported as done.
const (
	StatusEnded  = "ended"
	StatusFailed = "failed"
)

// SessionExit reasons. An ended session carries ExitReasonExit with code 0
// or ExitReasonInterrupted; a failed one carries ExitReasonExit with a
// non-zero code, ExitReasonError or ExitReasonUsageLimit.
const (
	ExitReasonExit        = "exit"
	ExitReasonInterrupted = "interrupted"
	ExitReasonError       = "error"
	ExitReasonUsageLimit  = "usage_limit"
)

// -32602 data.reason words for the methods that name one.
const (
	ReasonArgs         = "args"
	ReasonProgram      = "program"
	ReasonNotInstalled = "not_installed"
	ReasonText         = "text"
	ReasonKey          = "key"
)

// Limits from the schema, enforced by the packages that serve these calls.
const (
	MaxArgs        = 64
	MaxArgBytes    = 4096
	MaxArgsBytes   = 16384
	MaxProgramPath = 1024
	MaxSendText    = 8000
	MaxExitDetail  = 200

	MinIdleSeconds       = 3
	MaxIdleSeconds       = 600
	DefaultIdleSeconds   = 6
	MinRemindMinutes     = 2
	MaxRemindMinutes     = 120
	DefaultRemindMinutes = 10
)

// Patterns from the schema. ProgramNamePattern is a bare program name looked
// up on the login-shell PATH; SSHPublicKeyPattern is a key with no options
// and no comment.
const (
	ProgramNamePattern  = `^[A-Za-z0-9][A-Za-z0-9._+-]{0,63}$`
	SSHPublicKeyPattern = `^(ssh-ed25519|ecdsa-sha2-nistp256|ssh-rsa) [A-Za-z0-9+/]+={0,2}$`
)

// SessionExit says how a session's agent process ended. Code is nil when
// the process reported no exit status. Detail is written by the helper,
// never copied raw from a terminal or an agent: it is cleaned like a push
// body (detect.CleanLine, which drops ANSI sequences, control, invisible
// and box drawing characters), cut to its first line and to MaxExitDetail
// runes, and nil when nothing is left. See Validate.
type SessionExit struct {
	Code   *int    `json:"code"`
	Reason string  `json:"reason"`
	Detail *string `json:"detail"`
}

// Validate checks e against the schema: a known reason, a code of 0-255 or
// nil, and a detail that is nil or 1-200 runes of UTF-8 on one line with no
// control (Unicode Cc), invisible (see IsInvisible), box drawing or block
// element (U+2500-U+259F) characters. Errors never quote the detail.
func (e SessionExit) Validate() error {
	switch e.Reason {
	case ExitReasonExit, ExitReasonInterrupted, ExitReasonError, ExitReasonUsageLimit:
	default:
		return errors.New("exit reason must be exit, interrupted, error or usage_limit")
	}
	if e.Code != nil && (*e.Code < 0 || *e.Code > 255) {
		return errors.New("exit code must be 0-255")
	}
	if e.Detail == nil {
		return nil
	}
	d := *e.Detail
	if d == "" || !utf8.ValidString(d) || utf8.RuneCountInString(d) > MaxExitDetail {
		return errors.New("exit detail must be 1-200 characters of UTF-8")
	}
	if strings.ContainsFunc(d, func(r rune) bool {
		return unicode.IsControl(r) || IsInvisible(r) || (r >= 0x2500 && r <= 0x259F)
	}) {
		return errors.New("exit detail contains a character a push may not carry")
	}
	return nil
}

// UnmarshalJSON rejects an exit that fails Validate, so a decoded
// SessionExit is always one the schema allows.
func (e *SessionExit) UnmarshalJSON(b []byte) error {
	type plain SessionExit
	var v plain
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	if err := SessionExit(v).Validate(); err != nil {
		return err
	}
	*e = SessionExit(v)
	return nil
}

// SessionCreateParams is the session.create params. Program is set only
// with AgentCustom; Args are argv entries for the program, never shell text.
type SessionCreateParams struct {
	Agent   string   `json:"agent,omitempty"`
	Cwd     string   `json:"cwd,omitempty"`
	Name    string   `json:"name,omitempty"`
	Cols    int      `json:"cols,omitempty"`
	Rows    int      `json:"rows,omitempty"`
	Program string   `json:"program,omitempty"`
	Args    []string `json:"args,omitempty"`
}

// AgentsCheckParams is the agents.check params.
type AgentsCheckParams struct {
	Program string `json:"program"`
}

// AgentsCheckResult answers agents.check. Path is nil when not installed.
type AgentsCheckResult struct {
	Installed bool    `json:"installed"`
	Path      *string `json:"path"`
}

// SessionRefParams names one session: session.relaunch, session.dismiss.
type SessionRefParams struct {
	ID string `json:"id"`
}

// RestorableSession is a managed session a host restart ended.
type RestorableSession struct {
	ID        string    `json:"id"`
	Bib       string    `json:"bib"`
	Name      string    `json:"name"`
	Agent     string    `json:"agent"`
	Command   []string  `json:"command"`
	Cwd       string    `json:"cwd"`
	CreatedAt time.Time `json:"createdAt"`
	EndedAt   time.Time `json:"endedAt"`
	Resumable bool      `json:"resumable"`
}

// RestorableResult answers session.restorable, newest EndedAt first.
type RestorableResult struct {
	Sessions []RestorableSession `json:"sessions"`
}

// MarshalJSON writes nil Sessions as [], which the schema requires.
func (r RestorableResult) MarshalJSON() ([]byte, error) {
	type plain RestorableResult
	if r.Sessions == nil {
		r.Sessions = []RestorableSession{}
	}
	return json.Marshal(plain(r))
}

// SessionRestoreParams is the session.restore params.
type SessionRestoreParams struct {
	ID     string `json:"id"`
	Resume bool   `json:"resume"`
}

// SessionSendTextParams is the session.sendText params. Submit presses
// Enter after Text.
type SessionSendTextParams struct {
	ID     string `json:"id"`
	Text   string `json:"text"`
	Submit bool   `json:"submit"`
}

// NotifySettings is the machine's push settings. RemindMinutes 0 is off.
type NotifySettings struct {
	IdleSeconds   int  `json:"idleSeconds"`
	Muted         bool `json:"muted"`
	RemindMinutes int  `json:"remindMinutes"`
}

// NotifySetParams is the notify.set params; nil fields keep their value.
type NotifySetParams struct {
	IdleSeconds   *int  `json:"idleSeconds,omitempty"`
	Muted         *bool `json:"muted,omitempty"`
	RemindMinutes *int  `json:"remindMinutes,omitempty"`
}

// AuthkeysRemoveParams is the authkeys.remove params.
type AuthkeysRemoveParams struct {
	PublicKey string `json:"publicKey"`
}

// AuthkeysRemoveResult answers authkeys.remove.
type AuthkeysRemoveResult struct {
	Removed int `json:"removed"`
}

// HostInfo answers host.info. WakeOnLAN is nil when unknown.
type HostInfo struct {
	Interfaces []HostInterface `json:"interfaces"`
	WakeOnLAN  *bool           `json:"wakeOnLan"`
	// Routes are the addresses a phone can try to reach this machine by.
	Routes []HostRoute `json:"routes"`
}

// Route kinds in HostInfo.Routes.
const (
	RouteMDNS      = "mdns"
	RouteLAN       = "lan"
	RouteTailscale = "tailscale"
)

// HostRoute is one address the machine answers on: a LAN address of an
// interface, its mDNS .local name, or its Tailscale MagicDNS name or address.
type HostRoute struct {
	Kind      string `json:"kind"`
	Host      string `json:"host"`
	Interface string `json:"interface,omitempty"`
}

// HostInterface is one network interface: lowercase MAC and IPv4 addresses.
type HostInterface struct {
	Name string   `json:"name"`
	MAC  string   `json:"mac"`
	IPv4 []string `json:"ipv4"`
}

// MarshalJSON writes nil Interfaces and Routes as [], which the schema requires.
func (h HostInfo) MarshalJSON() ([]byte, error) {
	type plain HostInfo
	if h.Interfaces == nil {
		h.Interfaces = []HostInterface{}
	}
	if h.Routes == nil {
		h.Routes = []HostRoute{}
	}
	return json.Marshal(plain(h))
}

// MarshalJSON writes nil IPv4 as [], which the schema requires.
func (i HostInterface) MarshalJSON() ([]byte, error) {
	type plain HostInterface
	if i.IPv4 == nil {
		i.IPv4 = []string{}
	}
	return json.Marshal(plain(i))
}
