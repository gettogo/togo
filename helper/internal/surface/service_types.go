package surface

import (
	"encoding/base64"
	"encoding/json"
)

// Lease belongs to one RPC connection and one original runtime.
type Lease struct {
	ID         string `json:"id"`
	Token      string `json:"token"`
	Generation string `json:"generation"`
	// Features names what the program's companion adds to the protocol's
	// own events: "submit" (the togo.submit action on a field that only
	// Return submits) and "secret" (input nodes carry secret true or false).
	// An app must not use, or rely on, one that is not listed.
	Features []string `json:"features,omitempty"`
}

// LeaseRef identifies an already opened renderer.
type LeaseRef struct {
	ID    string `json:"id"`
	Lease string `json:"lease"`
}

// ReadParams selects a durable revision or a chunk of an immutable snapshot.
type ReadParams struct {
	ID       string `json:"id"`
	Lease    string `json:"lease"`
	Revision uint64 `json:"revision"`
	Snapshot string `json:"snapshot,omitempty"`
	Offset   int    `json:"offset"`
}

// ReadResult fits within the RPC frame limit, including base64 expansion.
type ReadResult struct {
	Generation string `json:"generation"`
	Revision   uint64 `json:"revision"`
	Snapshot   string `json:"snapshot,omitempty"`
	Offset     int    `json:"offset"`
	NextOffset int    `json:"nextOffset"`
	Total      int    `json:"total"`
	Data       []byte `json:"data,omitempty"`
	Unchanged  bool   `json:"unchanged"`
	Pending    bool   `json:"pending"`
}

// MarshalJSON follows protocol 1's unpadded base64url rule for byte fields.
func (r ReadResult) MarshalJSON() ([]byte, error) {
	type fields ReadResult
	return json.Marshal(struct {
		fields
		Data string `json:"data,omitempty"`
	}{fields: fields(r), Data: base64.RawURLEncoding.EncodeToString(r.Data)})
}

// AckParams reports an actual display pass, never receipt or parsing alone.
type AckParams struct {
	ID       string `json:"id"`
	Lease    string `json:"lease"`
	Surface  string `json:"surface"`
	Instance uint64 `json:"instance"`
	Sequence int64  `json:"sequence"`
}

// InputParams binds a semantic TSP event to its current surface instance.
type InputParams struct {
	ID           string          `json:"id"`
	Lease        string          `json:"lease"`
	Surface      string          `json:"surface"`
	Instance     uint64          `json:"instance"`
	Event        json.RawMessage `json:"event"`
	ExpectedText *string         `json:"expectedText,omitempty"`
}

// DispatchResult confirms transport only; the program must confirm the effect.
type DispatchResult struct {
	Dispatched bool `json:"dispatched"`
	// Consumed is set, and true, only for a send whose text the program's
	// own editor submitted. Absent means not proven: it does not mean the
	// send failed, and it is never a reason to send again.
	Consumed *bool `json:"consumed,omitempty"`
	// Refused is set, and true, when the program received the event and
	// did not take it because its screen had moved on. Nothing changed;
	// the scene read next is what the program shows.
	Refused *bool `json:"refused,omitempty"`
}
