package wire

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"
)

// Event is the params of an `event` notification. On the wire the payload's
// fields sit beside seq, ts, kind and sessionId in one flat object.
type Event struct {
	Seq       int64
	TS        time.Time
	Kind      string
	SessionID *string
	// Payload is one of the *Event structs in this package when encoding.
	// After decoding it holds the whole event object as json.RawMessage, to
	// be decoded again into the struct for Kind.
	Payload any
}

type eventHeader struct {
	Seq       int64     `json:"seq"`
	TS        time.Time `json:"ts"`
	Kind      string    `json:"kind"`
	SessionID *string   `json:"sessionId"`
}

// MarshalJSON writes the header fields followed by the payload's fields. A
// payload that is not an object, or that reuses a header name, is an error
// rather than a silently malformed event.
func (e Event) MarshalJSON() ([]byte, error) {
	head, err := json.Marshal(eventHeader{e.Seq, e.TS, e.Kind, e.SessionID})
	if err != nil {
		return nil, err
	}
	if e.Payload == nil {
		return head, nil
	}
	body, err := json.Marshal(e.Payload)
	if err != nil {
		return nil, fmt.Errorf("event %s payload: %w", e.Kind, err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil || fields == nil {
		return nil, fmt.Errorf("event %s payload is not an object", e.Kind)
	}
	for _, k := range []string{"seq", "ts", "kind", "sessionId"} {
		if _, clash := fields[k]; clash {
			return nil, fmt.Errorf("event %s payload reuses header field %q", e.Kind, k)
		}
	}
	body = bytes.TrimSpace(body)
	if len(fields) == 0 {
		return head, nil
	}
	out := append(head[:len(head)-1], ',')
	return append(out, body[1:]...), nil
}

// UnmarshalJSON reads the header fields and keeps the whole object in
// Payload.
func (e *Event) UnmarshalJSON(b []byte) error {
	var h eventHeader
	if err := json.Unmarshal(b, &h); err != nil {
		return err
	}
	*e = Event{Seq: h.Seq, TS: h.TS, Kind: h.Kind, SessionID: h.SessionID, Payload: json.RawMessage(bytes.Clone(b))}
	return nil
}
