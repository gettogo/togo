package wire

import "strconv"

// CapLiveContext advertises signed request-data receipts through the mailbox.
const CapLiveContext = "livecontext.v1"

// PlaintextLiveContext confirms that one device saved a request for its Live Activity.
const PlaintextLiveContext = "live_context"

// LiveContextPlaintext is sealed for the host through the mailbox. It confirms
// display data, never an answer. RequestID is the exact saved request.
type LiveContextPlaintext struct {
	Type      string `json:"type"`
	DeviceID  string `json:"deviceId"`
	RequestID string `json:"requestId"`
	TokenHash string `json:"tokenHash"`
	TS        int64  `json:"ts"`
	Sig       string `json:"sig"`
}

func LiveContextSignedBytes(hostID, deviceID, requestID, tokenHash string, ts int64) []byte {
	return []byte("togo-live-context/v1\n" + hostID + "\n" + deviceID + "\n" + requestID + "\n" + tokenHash + "\n" + strconv.FormatInt(ts, 10))
}
