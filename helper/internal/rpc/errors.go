package rpc

// JSON-RPC standard codes and the Togo protocol v1 extensions.
const (
	CodeParse          = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
	CodeInternal       = -32603

	CodeNotFound           = -32001
	CodeNotReady           = -32002
	CodeInvalidPath        = -32003
	CodeTmuxUnavailable    = -32004
	CodeUnsupportedVersion = -32005
	CodeConflict           = -32006
	CodeNotManaged         = -32007
	CodeRefused            = -32008
)

// Error is a JSON-RPC error object.
type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// messages are fixed per code because the protocol schema enforces them.
var messages = map[int]string{
	CodeParse:              "parse error",
	CodeInvalidRequest:     "invalid request",
	CodeMethodNotFound:     "method not found",
	CodeInvalidParams:      "invalid params",
	CodeInternal:           "internal error",
	CodeNotFound:           "not_found",
	CodeNotReady:           "not_ready",
	CodeInvalidPath:        "invalid_path",
	CodeTmuxUnavailable:    "tmux_unavailable",
	CodeUnsupportedVersion: "unsupported_version",
	CodeConflict:           "conflict",
	CodeNotManaged:         "not_managed",
	CodeRefused:            "refused",
}

// NewError returns an Error with the protocol's canonical message for code.
func NewError(code int, data any) *Error {
	return &Error{Code: code, Message: messages[code], Data: data}
}

// InvalidParams returns a -32602 error whose data.reason explains the problem.
func InvalidParams(reason string) *Error {
	return NewError(CodeInvalidParams, map[string]string{"reason": reason})
}
