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
// stored choice. TS is Unix milliseconds; Sig is base64url without padding,
// by the device's approvalKey over RegisterSignedBytes.
type RegisterPlaintext struct {
	Type                   string `json:"type"`
	DeviceID               string `json:"deviceId"`
	APNsToken              string `json:"apnsToken,omitempty"`
	LiveActivityToken      string `json:"liveActivityToken,omitempty"`
	LiveActivityStartToken string `json:"liveActivityStartToken,omitempty"`
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
