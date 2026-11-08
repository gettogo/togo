package rpc

import (
	"context"
	"encoding/json"
	"testing"
)

type panicHandler struct{}

func (panicHandler) Handle(_ context.Context, method string, _ json.RawMessage) (any, *Error) {
	if method == "boom" || method == "boom.async" {
		var m map[string]int
		m["x"] = 1 // nil map write
	}
	return "ok", nil
}

// A panicking handler answers that call with an internal error and the
// connection keeps serving, serial or concurrent.
func TestHandlerPanicIsRecovered(t *testing.T) {
	info := testInfo
	info.Concurrent = []string{"boom.async"}
	var out = serveInfo(t, panicHandler{}, info, helloV1,
		`{"jsonrpc":"2.0","id":2,"method":"boom","params":{}}`,
		`{"jsonrpc":"2.0","id":3,"method":"boom.async","params":{}}`,
		`{"jsonrpc":"2.0","id":4,"method":"fine","params":{}}`)
	byID := map[float64]map[string]any{}
	for _, r := range out {
		if id, ok := r["id"].(float64); ok {
			byID[id] = r
		}
	}
	for _, id := range []float64{2, 3} {
		r, ok := byID[id]
		if !ok {
			t.Fatalf("no reply for %v: %v", id, out)
		}
		if e := errorOf(t, r); e["code"].(float64) != CodeInternal {
			t.Errorf("id %v error = %v", id, e)
		}
	}
	if r := byID[4]; r == nil || r["result"] != "ok" {
		t.Errorf("after panic: %v", byID[4])
	}
}
