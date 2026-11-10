package mailbox

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"regexp"
	"strconv"
	"time"

	"github.com/gettogo/togo/helper/internal/seal"
	"github.com/gettogo/togo/helper/internal/wire"
)

// RegisterFunc stores the tokens and command text choice a device
// registered at ts (Unix ms). An empty token leaves the stored one as it is.
// It must refuse a ts no newer than the last one it applied for the device.
type RegisterFunc func(deviceID, apnsToken, laToken, laStartToken string, command bool, ts int64) error

// EndLiveFunc forgets a device's Live Activity update token if it is still
// token, after the device reported that activity ended or was dismissed.
type EndLiveFunc func(deviceID, token string) error

var tokenRe = regexp.MustCompile(`^[0-9a-f]{64,200}$`)

// verifyRegister checks a registration with the same device, signature,
// freshness and single-use rules as an answer. Its replay key is
// deviceId|ts, so each signed registration applies once, and its ts must be
// newer than the device's last applied registration (Device.RegisterTS,
// kept with the pairing), so neither a replay after a restart nor an older
// registration arriving late can undo a newer one.
func (v *Verifier) verifyRegister(pt []byte) (wire.RegisterPlaintext, error) {
	var r wire.RegisterPlaintext
	dec := json.NewDecoder(bytes.NewReader(pt))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&r); err != nil || dec.More() || r.Type != wire.PlaintextRegister || r.LiveActivityCommand == nil {
		return r, ErrMalformed
	}
	if !deviceIDRe.MatchString(r.DeviceID) {
		return r, ErrMalformed
	}
	for _, tok := range []string{r.APNsToken, r.LiveActivityToken, r.LiveActivityStartToken, r.LiveActivityEnded} {
		if tok != "" && !tokenRe.MatchString(tok) {
			return r, ErrMalformed
		}
	}
	// A dead device may register: a new APNs token is what revives it.
	dev, ok := v.device(r.DeviceID, true)
	if !ok {
		return r, ErrUnknownDevice
	}
	sig, err := base64.RawURLEncoding.Strict().DecodeString(r.Sig)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return r, ErrSignature
	}
	msg := wire.RegisterSignedBytes(v.keys.HostID, r.DeviceID, r.APNsToken, r.LiveActivityToken, r.LiveActivityStartToken,
		*r.LiveActivityCommand, r.TS)
	if r.LiveActivityEnded != "" {
		msg = wire.RegisterEndedSignedBytes(v.keys.HostID, r.DeviceID, r.APNsToken, r.LiveActivityToken,
			r.LiveActivityStartToken, r.LiveActivityEnded, *r.LiveActivityCommand, r.TS)
	}
	if !seal.Verify(dev.ApprovalKey, msg, sig) {
		return r, ErrSignature
	}
	now := v.now()
	ts := time.UnixMilli(r.TS)
	if !fresh(ts, now) {
		return r, ErrStale
	}
	if r.TS <= dev.RegisterTS {
		return r, ErrReplay
	}
	if !v.reserve(registerKey(r), ts, now) {
		return r, ErrReplay
	}
	return r, nil
}

func registerKey(r wire.RegisterPlaintext) string {
	return r.DeviceID + "|" + strconv.FormatInt(r.TS, 10)
}
