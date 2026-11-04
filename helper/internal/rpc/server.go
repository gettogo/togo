package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
)

// protocolVersion is the only protocol version this helper speaks.
const protocolVersion = 1

// Handler serves every method except hello, which Serve answers itself.
type Handler interface {
	Handle(ctx context.Context, method string, params json.RawMessage) (any, *Error)
}

// HelperInfo describes this helper in the hello result.
type HelperInfo struct {
	Version      string
	OS           string
	Arch         string
	Hostname     string
	Capabilities []string
}

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
}

type helloParams struct {
	Protocol *int `json:"protocol"`
}

type helloResult struct {
	Protocol     int         `json:"protocol"`
	Helper       helloHelper `json:"helper"`
	Capabilities []string    `json:"capabilities"`
}

type helloHelper struct {
	Version  string `json:"version"`
	OS       string `json:"os"`
	Arch     string `json:"arch"`
	Hostname string `json:"hostname"`
}

// Serve reads framed JSON-RPC requests from r and writes responses to w until
// r reaches a clean EOF, in which case it returns nil. Requests are handled
// one at a time in arrival order. Calls other than hello get -32002 until a
// hello with a supported protocol version succeeds.
//
// An oversized or truncated frame leaves the stream unframed, so Serve stops
// without replying and returns ErrFrameTooLarge or io.ErrUnexpectedEOF.
func Serve(ctx context.Context, r io.Reader, w io.Writer, h Handler, info HelperInfo) error {
	ready := false
	for {
		payload, err := ReadFrame(r)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		resp, ok := respond(ctx, payload, h, info, &ready)
		if !ok {
			continue
		}
		if err := send(w, resp); err != nil {
			return err
		}
	}
}

// respond builds the reply to one frame. ok is false for notifications.
func respond(ctx context.Context, payload []byte, h Handler, info HelperInfo, ready *bool) (resp response, ok bool) {
	null := json.RawMessage("null")
	if !json.Valid(payload) {
		return failure(null, NewError(CodeParse, nil)), true
	}
	var req request
	if err := json.Unmarshal(payload, &req); err != nil || req.JSONRPC != "2.0" || req.Method == "" {
		return failure(null, NewError(CodeInvalidRequest, nil)), true
	}
	if len(req.ID) == 0 {
		return response{}, false
	}
	if !validID(req.ID) {
		return failure(null, NewError(CodeInvalidRequest, nil)), true
	}

	if req.Method == "hello" {
		result, rpcErr := hello(req.Params, info)
		if rpcErr != nil {
			return failure(req.ID, rpcErr), true
		}
		*ready = true
		return response{JSONRPC: "2.0", ID: req.ID, Result: result}, true
	}
	if !*ready {
		return failure(req.ID, NewError(CodeNotReady, nil)), true
	}
	result, rpcErr := h.Handle(ctx, req.Method, req.Params)
	if rpcErr != nil {
		return failure(req.ID, rpcErr), true
	}
	if result == nil {
		// A nil Result would be dropped by omitempty, leaving neither result nor error.
		result = json.RawMessage("null")
	}
	return response{JSONRPC: "2.0", ID: req.ID, Result: result}, true
}

func hello(params json.RawMessage, info HelperInfo) (*helloResult, *Error) {
	var p helloParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, InvalidParams(err.Error())
	}
	if p.Protocol == nil {
		return nil, InvalidParams("protocol is required")
	}
	if *p.Protocol != protocolVersion {
		return nil, NewError(CodeUnsupportedVersion, map[string][]int{"supported": {protocolVersion}})
	}
	caps := info.Capabilities
	if caps == nil {
		caps = []string{}
	}
	return &helloResult{
		Protocol:     protocolVersion,
		Helper:       helloHelper{Version: info.Version, OS: info.OS, Arch: info.Arch, Hostname: info.Hostname},
		Capabilities: caps,
	}, nil
}

// validID accepts the JSON string and number ids this protocol uses.
func validID(id json.RawMessage) bool {
	switch c := bytes.TrimSpace(id)[0]; {
	case c == '"', c == '-', c >= '0' && c <= '9':
		return true
	}
	return false
}

func failure(id json.RawMessage, e *Error) response {
	return response{JSONRPC: "2.0", ID: id, Error: e}
}

// send writes resp, falling back to an internal error when the result does
// not fit in a frame or cannot be encoded, so the client is never left
// waiting on an id.
func send(w io.Writer, resp response) error {
	payload, err := json.Marshal(resp)
	if err == nil {
		err = WriteFrame(w, payload)
		if err == nil || !errors.Is(err, ErrFrameTooLarge) {
			return err
		}
	}
	payload, err = json.Marshal(failure(resp.ID, NewError(CodeInternal, nil)))
	if err != nil {
		return err
	}
	return WriteFrame(w, payload)
}
