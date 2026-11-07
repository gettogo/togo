package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
)

// Typed framing: the child takes commands {"type": method, "id": id,
// ...params} one per line and answers {"type": "response", "id": id,
// "command": method, "success": bool, "data": ..., "error": "..."}. Other
// lines (a ready frame, events, UI requests) are skipped. This is how omp's
// --mode rpc speaks; it is not JSON-RPC 2.0.

// typedResponse is the part of a typed answer the client reads.
type typedResponse struct {
	Type    string          `json:"type"`
	ID      json.RawMessage `json:"id"`
	Success *bool           `json:"success"`
	Data    json.RawMessage `json:"data"`
	Error   string          `json:"error"`
}

// errTypedFailed is returned when the child answers success false. The
// child's own message is not passed on: it may quote the session.
var errTypedFailed = errors.New("rpc: the command failed")

// CallTyped sends one typed command on t and returns its data. Params are
// merged into the command object; "type" and "id" cannot be overridden.
func CallTyped(ctx context.Context, t Transport, method string, params map[string]string) (json.RawMessage, error) {
	const id = "togo-1"
	cmd := map[string]any{}
	for k, v := range params {
		cmd[k] = v
	}
	cmd["type"], cmd["id"] = method, id
	b, err := json.Marshal(cmd)
	if err != nil {
		return nil, err
	}
	type result struct {
		data json.RawMessage
		err  error
	}
	done := make(chan result, 2)
	// The write runs beside the reads: a child may print its own frames
	// before it reads the command.
	go func() {
		if err := t.Write(ctx, b); err != nil {
			done <- result{err: err}
		}
	}()
	go func() {
		for {
			line, err := t.Read(ctx)
			if err != nil {
				if errors.Is(err, io.EOF) {
					err = errors.New("rpc: the child closed before answering")
				}
				done <- result{err: err}
				return
			}
			var r typedResponse
			if json.Unmarshal(line, &r) != nil || r.Type != "response" || !sameID(r.ID, id) {
				continue
			}
			if r.Success != nil && !*r.Success {
				done <- result{err: errTypedFailed}
				return
			}
			if len(bytes.TrimSpace(r.Data)) == 0 {
				r.Data = json.RawMessage("null")
			}
			done <- result{data: r.Data}
			return
		}
	}()
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("rpc: %s: %w", method, ctx.Err())
	case r := <-done:
		return r.data, r.err
	}
}

func sameID(raw json.RawMessage, id string) bool {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s == id
	}
	return string(bytes.TrimSpace(raw)) == strconv.Quote(id)
}
