package wire

import (
	"encoding/json"
	"strconv"
)

// Push kinds carried in PushPlaintext.Kind.
const (
	PushNeedsYou = "needs_you"
	PushDone     = "done"
	PushJob      = "job"
	PushNotify   = "notify"
	PushClear    = "clear"
	PushEnded    = "ended"
	PushFailed   = "failed"
	PushOffline  = "offline"
)

// LiveWorking is the Live Activity status while an agent works with nothing
// waiting; the other statuses are PushNeedsYou, PushDone, PushEnded,
// PushFailed and PushOffline.
const LiveWorking = "working"

// PushPlaintext is sealed for one device and sent through the relay.
// RequestID and Options are set for needs_you; clear sets RequestID only and
// leaves Title and Body empty; the other kinds set neither. Subject is set
// for needs_you when the approval has one. Host is the machine's short
// display name. TS is Unix milliseconds. Exit is set only for ended and
// failed, Active (sessions working) only for offline, and Reminder only on
// the one reminder for a needs_you still waiting. AllowText, needs_you only,
// is true when the approval takes a typed answer as well as its options.
type PushPlaintext struct {
	V         int      `json:"v"`
	Kind      string   `json:"kind"`
	SessionID *string  `json:"sessionId"`
	Bib       *string  `json:"bib"`
	Agent     *string  `json:"agent"`
	Title     string   `json:"title"`
	Body      string   `json:"body"`
	RequestID string   `json:"requestId,omitempty"`
	Options   []Option `json:"options,omitempty"`
	TS        int64    `json:"ts"`
	Subject   *Subject `json:"subject,omitempty"`
	Host      *string  `json:"host,omitempty"`

	Exit      *SessionExit `json:"exit,omitempty"`
	Active    *int         `json:"active,omitempty"`
	Reminder  bool         `json:"reminder,omitempty"`
	AllowText bool         `json:"allowText,omitempty"`
}

// MarshalJSON writes options as [] for needs_you even when there are none,
// because the schema requires the key for that kind and omits it otherwise.
func (p PushPlaintext) MarshalJSON() ([]byte, error) {
	type plain PushPlaintext
	if p.Kind != PushNeedsYou {
		return json.Marshal(plain(p))
	}
	opts := p.Options
	if opts == nil {
		opts = []Option{}
	}
	return json.Marshal(struct {
		plain
		Options []Option `json:"options"`
	}{plain(p), opts})
}

// AnswerPlaintext is what the phone seals for the host and posts to the
// relay mailbox. Decision is approve, deny or option:<n> only. TS is Unix
// milliseconds; Sig is base64url without padding. Senders omit type on
// answers, because helpers that predate it reject unknown fields; receivers
// accept it absent or PlaintextAnswer, and only RegisterPlaintext carries a
// type. MarshalJSON never writes it.
type AnswerPlaintext struct {
	Type      string `json:"type,omitempty"`
	RequestID string `json:"requestId"`
	Decision  string `json:"decision"`
	TS        int64  `json:"ts"`
	DeviceID  string `json:"deviceId"`
	Sig       string `json:"sig"`
}

// ApprovalSignedBytes returns the exact bytes the device signs with its
// approvalKey. The phone builds the same string, so any change here is a
// protocol change.
func ApprovalSignedBytes(hostID, requestID, decision string, ts int64) []byte {
	return []byte("togo-approval/v1\n" + hostID + "\n" + requestID + "\n" + decision + "\n" + strconv.FormatInt(ts, 10))
}

// MarshalJSON drops Type, so an answer reads the same to every helper.
func (a AnswerPlaintext) MarshalJSON() ([]byte, error) {
	type plain AnswerPlaintext
	a.Type = ""
	return json.Marshal(plain(a))
}
