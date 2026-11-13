package mailbox

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"time"

	"github.com/gettogo/togo/helper/internal/seal"
	"github.com/gettogo/togo/helper/internal/wire"
)

// ContextReadyFunc records a verified request-data receipt independently of answers.
type ContextReadyFunc func(deviceID, requestID, tokenHash string, ts int64) error

func contextKey(r wire.LiveContextPlaintext) string {
	return "context|" + r.DeviceID + "|" + r.RequestID
}

func (v *Verifier) verifyContext(pt []byte) (wire.LiveContextPlaintext, error) {
	var r wire.LiveContextPlaintext
	dec := json.NewDecoder(bytes.NewReader(pt))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&r); err != nil || dec.More() || r.Type != wire.PlaintextLiveContext ||
		!deviceIDRe.MatchString(r.DeviceID) || !requestIDRe.MatchString(r.RequestID) || len(r.TokenHash) != 64 || !tokenRe.MatchString(r.TokenHash) {
		return r, ErrMalformed
	}
	dev, ok := v.device(r.DeviceID, false)
	if !ok {
		return r, ErrUnknownDevice
	}
	sig, err := base64.RawURLEncoding.Strict().DecodeString(r.Sig)
	if err != nil || len(sig) != ed25519.SignatureSize || !seal.Verify(dev.ApprovalKey,
		wire.LiveContextSignedBytes(v.keys.HostID, r.DeviceID, r.RequestID, r.TokenHash, r.TS), sig) {
		return r, ErrSignature
	}
	ts, now := time.UnixMilli(r.TS), v.now()
	if !fresh(ts, now) {
		return r, ErrStale
	}
	if v.options == nil {
		return r, ErrDecision
	}
	if _, pending := v.options(r.RequestID); !pending {
		return r, ErrDecision
	}
	if !v.reserve(contextKey(r), ts, now) {
		return r, ErrReplay
	}
	return r, nil
}
