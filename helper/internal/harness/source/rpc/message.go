// Package rpc is a JSON-RPC 2.0 client for harness endpoints: the Codex
// app-server (one message per WebSocket text frame over a unix socket) and
// one-shot children that speak line-delimited JSON-RPC on stdio. It matches
// responses to calls by id, fans notifications out, and hands server
// requests to the caller untouched: the client never answers a server
// request on its own, because the harness may have sent the same request to
// its terminal client and the first answer wins.
package rpc

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
)

// Message is one JSON-RPC 2.0 message. A request has Method and ID, a
// notification has Method only, a response has ID and Result or Error.
type Message struct {
	JSONRPC string          `json:"jsonrpc,omitempty"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
}

// IsRequest reports whether m is a request from the other side.
func (m *Message) IsRequest() bool { return m.Method != "" && hasID(m.ID) }

// IsNotification reports whether m is a notification.
func (m *Message) IsNotification() bool { return m.Method != "" && !hasID(m.ID) }

// IsResponse reports whether m answers one of our calls.
func (m *Message) IsResponse() bool { return m.Method == "" && hasID(m.ID) }

func hasID(id json.RawMessage) bool {
	t := bytes.TrimSpace(id)
	return len(t) > 0 && !bytes.Equal(t, []byte("null"))
}

// Error is a JSON-RPC error object.
type Error struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *Error) Error() string { return fmt.Sprintf("rpc error %d: %s", e.Code, e.Message) }

// Standard error codes.
const (
	CodeMethodNotFound = -32601
	CodeInternal       = -32603
)

// idKey turns a raw id into a map key: numbers keep their text, strings
// their contents, so 5 and "5" stay distinct.
func idKey(id json.RawMessage) string {
	t := bytes.TrimSpace(id)
	var s string
	if len(t) > 0 && t[0] == '"' && json.Unmarshal(t, &s) == nil {
		return "s:" + s
	}
	return "n:" + string(t)
}

// RequestKey names a server request for items and actions: "rq-" and the
// request id. The mapper uses it as the question item id and the answer
// action passes it back as the target.
func RequestKey(id json.RawMessage) string {
	t := bytes.TrimSpace(id)
	var s string
	if len(t) > 0 && t[0] == '"' && json.Unmarshal(t, &s) == nil {
		return "rq-" + s
	}
	return "rq-" + string(t)
}

func numID(n int64) json.RawMessage { return json.RawMessage(strconv.FormatInt(n, 10)) }
