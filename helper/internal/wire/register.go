package wire

import "strconv"

// Mailbox plaintext types. An absent type means PlaintextAnswer.
const (
	PlaintextAnswer   = "answer"
	PlaintextRegister = "register"
)

// RegisterPlaintext is what a paired device seals for the host (label
// answer, aad hostId) and posts to the relay mailbox when its APNs or Live
// Activity tokens or its command text choice change, so the host learns
// them without an SSH session. An empty token is left out and leaves the
// stored one as it is. LiveActivityCommand is required and replaces the
// stored choice. LiveActivityEnded is the update token of an activity the
// user dismissed or that otherwise ended on the device; the host forgets it
// if it is still the stored one, so pushes stop counting on that activity.
// TS is Unix milliseconds; Sig is base64url without padding, by the
// device's approvalKey over RegisterSignedBytes, or RegisterEndedSignedBytes
// when LiveActivityEnded is set.
type RegisterPlaintext struct {
	Type                   string `json:"type"`
	DeviceID               string `json:"deviceId"`
	APNsToken              string `json:"apnsToken,omitempty"`
	LiveActivityToken      string `json:"liveActivityToken,omitempty"`
	LiveActivityStartToken string `json:"liveActivityStartToken,omitempty"`
	LiveActivityEnded      string `json:"liveActivityEnded,omitempty"`
	LiveActivityCommand    *bool  `json:"liveActivityCommand"`
	TS                     int64  `json:"ts"`
	Sig                    string `json:"sig"`
}

// RegisterSignedBytes returns the exact bytes the device signs to register
// tokens and its command text choice. Absent tokens are empty lines; the
// choice is "true" or "false". The phone builds the same string, so any
// change here is a protocol change.
func RegisterSignedBytes(hostID, deviceID, apnsToken, laToken, laStartToken string, command bool, ts int64) []byte {
	return []byte("togo-register/v2\n" + hostID + "\n" + deviceID + "\n" + apnsToken + "\n" +
		laToken + "\n" + laStartToken + "\n" + strconv.FormatBool(command) + "\n" + strconv.FormatInt(ts, 10))
}

// RegisterEndedSignedBytes is RegisterSignedBytes for a registration that
// also reports an ended Live Activity: version v3, with the ended token on
// its own line after the start token. Registrations without one keep v2,
// so helpers that predate v3 still accept them.
func RegisterEndedSignedBytes(hostID, deviceID, apnsToken, laToken, laStartToken, laEnded string, command bool,
	ts int64) []byte {
	return []byte("togo-register/v3\n" + hostID + "\n" + deviceID + "\n" + apnsToken + "\n" +
		laToken + "\n" + laStartToken + "\n" + laEnded + "\n" + strconv.FormatBool(command) + "\n" +
		strconv.FormatInt(ts, 10))
}
