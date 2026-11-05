// Package mailbox reads approval answers and token registrations that phones
// and watches post to the relay mailbox. It keeps one outbound WebSocket per
// relay, opens each sealed message, checks the device signature, freshness
// and single use, and hands only messages that pass every check on: answers
// to the approvals path, registrations to the pairing store. Text answers
// and "always allow" options are refused here because the relay path is
// meant for one-tap answers; they stay available over SSH.
package mailbox

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"sync"
	"time"

	"github.com/gettogo/togo/helper/internal/seal"
	"github.com/gettogo/togo/helper/internal/wire"
)

// FreshnessWindow bounds |now - ts| for an answer. It equals the relay's
// message TTL, so anything older has already been dropped by the relay.
const FreshnessWindow = 600 * time.Second

// Reasons an answer is dropped. They name the failed check and never carry
// message content.
var (
	ErrOpen          = errors.New("envelope does not open")
	ErrMalformed     = errors.New("malformed answer")
	ErrUnknownDevice = errors.New("unknown or revoked device")
	ErrSignature     = errors.New("bad signature")
	ErrStale         = errors.New("timestamp outside freshness window")
	ErrReplay        = errors.New("already received through the mailbox")
	ErrDecision      = errors.New("decision not allowed through the relay")
)

var (
	requestIDRe = regexp.MustCompile(`^r_[a-z2-7]{10}$`)
	deviceIDRe  = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
)

// Keys is the part of the host key file the reader needs.
type Keys struct {
	HostID        string
	Private       [32]byte // host X25519 private key
	MailboxSecret []byte   // 32 bytes; the Bearer credential for the mailbox
}

// Device is a paired device as the reader sees it. Dead covers both revoked
// devices and devices the relay reported as unregistered. RegisterTS is the
// ts of the last registration applied for it.
type Device struct {
	DeviceID    string
	ApprovalKey ed25519.PublicKey
	RelayURL    string
	Dead        bool
	RegisterTS  int64
}

// OptionsFunc returns the options of a pending approval, false when the
// request is unknown. The reader uses it to refuse approve_always options.
type OptionsFunc func(requestID string) ([]wire.Option, bool)

// Verifier checks sealed answers. It is safe for concurrent use.
type Verifier struct {
	keys    Keys
	devices func() []Device
	options OptionsFunc
	now     func() time.Time

	mu sync.Mutex
	// seen maps a replay key (a requestId, or deviceId|ts for a
	// registration) to when its entry can be pruned.
	seen map[string]time.Time
}

// Message is one verified mailbox message. Exactly one field is set.
type Message struct {
	Answer   *wire.AnswerPlaintext
	Register *wire.RegisterPlaintext
}

// replayKey is the key the message was reserved under.
func (m Message) replayKey() string {
	if m.Register != nil {
		return registerKey(*m.Register)
	}
	return m.Answer.RequestID
}

// NewVerifier returns a Verifier. now defaults to time.Now.
func NewVerifier(keys Keys, devices func() []Device, options OptionsFunc, now func() time.Time) *Verifier {
	if now == nil {
		now = time.Now
	}
	return &Verifier{keys: keys, devices: devices, options: options, now: now, seen: map[string]time.Time{}}
}

// Verify opens and checks one mailbox answer. On success the requestId is
// recorded as used, so the same answer is refused if it arrives again. A
// registration is ErrMalformed here; Open accepts both.
func (v *Verifier) Verify(ciphertext string) (requestID, decision string, err error) {
	m, err := v.Open(ciphertext)
	if err != nil {
		return "", "", err
	}
	if m.Answer == nil {
		v.release(m.replayKey())
		return "", "", ErrMalformed
	}
	return m.Answer.RequestID, m.Answer.Decision, nil
}

// Open opens one mailbox ciphertext, dispatches on its type and checks it.
// On success its replay key is recorded as used.
func (v *Verifier) Open(ciphertext string) (Message, error) {
	pt, err := seal.Open(v.keys.Private, ciphertext, seal.LabelAnswer, []byte(v.keys.HostID))
	if err != nil {
		return Message{}, ErrOpen
	}
	var head struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(pt, &head) != nil {
		return Message{}, ErrMalformed
	}
	switch head.Type {
	case "", wire.PlaintextAnswer:
		a, err := v.verifyAnswer(pt)
		if err != nil {
			return Message{}, err
		}
		return Message{Answer: &a}, nil
	case wire.PlaintextRegister:
		r, err := v.verifyRegister(pt)
		if err != nil {
			return Message{}, err
		}
		return Message{Register: &r}, nil
	}
	return Message{}, ErrMalformed
}

func (v *Verifier) verifyAnswer(pt []byte) (wire.AnswerPlaintext, error) {
	var a wire.AnswerPlaintext
	dec := json.NewDecoder(bytes.NewReader(pt))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&a); err != nil || dec.More() {
		return a, ErrMalformed
	}
	if !requestIDRe.MatchString(a.RequestID) || !deviceIDRe.MatchString(a.DeviceID) {
		return a, ErrMalformed
	}
	dev, ok := v.device(a.DeviceID)
	if !ok {
		return a, ErrUnknownDevice
	}
	sig, err := base64.RawURLEncoding.Strict().DecodeString(a.Sig)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return a, ErrSignature
	}
	if !seal.Verify(dev.ApprovalKey, seal.SignMessage(v.keys.HostID, a.RequestID, a.Decision, a.TS), sig) {
		return a, ErrSignature
	}
	now := v.now()
	ts := time.UnixMilli(a.TS)
	if !fresh(ts, now) {
		return a, ErrStale
	}
	if err := v.checkDecision(a.RequestID, a.Decision); err != nil {
		return a, err
	}
	if !v.reserve(a.RequestID, ts, now) {
		return a, ErrReplay
	}
	return a, nil
}

func fresh(ts, now time.Time) bool {
	d := now.Sub(ts)
	return d <= FreshnessWindow && d >= -FreshnessWindow
}

func (v *Verifier) device(id string) (Device, bool) {
	if v.devices == nil {
		return Device{}, false
	}
	for _, d := range v.devices() {
		if d.DeviceID == id && !d.Dead && len(d.ApprovalKey) == ed25519.PublicKeySize {
			return d, true
		}
	}
	return Device{}, false
}

// checkDecision allows approve, deny and option:n whose role is not
// approve_always. An option on a request whose options are unknown is
// refused rather than guessed at.
func (v *Verifier) checkDecision(requestID, raw string) error {
	d, err := wire.ParseDecision(raw)
	if err != nil {
		return ErrMalformed
	}
	switch d.Kind {
	case wire.DecisionApprove, wire.DecisionDeny:
		return nil
	case wire.DecisionOption:
		if v.options == nil {
			return ErrDecision
		}
		opts, ok := v.options(requestID)
		if !ok {
			return ErrDecision
		}
		for _, o := range opts {
			if o.N == d.N {
				if o.Role == wire.RoleApproveAlways {
					return ErrDecision
				}
				return nil
			}
		}
		return ErrDecision
	}
	return ErrDecision
}

// reserve marks a replay key used. The entry lives until ts leaves the
// freshness window, after which a replay would fail the freshness check
// anyway.
func (v *Verifier) reserve(key string, ts, now time.Time) bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	for k, until := range v.seen {
		if now.After(until) {
			delete(v.seen, k)
		}
	}
	if _, dup := v.seen[key]; dup {
		return false
	}
	v.seen[key] = ts.Add(FreshnessWindow)
	return true
}

// release forgets a replay key so a redelivery can be handled. Used only
// when handing the message on failed for a reason that says nothing about
// the message itself, such as shutdown.
func (v *Verifier) release(key string) {
	v.mu.Lock()
	delete(v.seen, key)
	v.mu.Unlock()
}

// idHash is the 6-char identifier logged in place of message ids.
func idHash(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])[:6]
}
