package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

type fakeHandler struct {
	calls []string
}

func (f *fakeHandler) Handle(_ context.Context, method string, _ json.RawMessage) (any, *Error) {
	f.calls = append(f.calls, method)
	if method == "session.list" {
		return map[string]any{"sessions": []any{}}, nil
	}
	return nil, NewError(CodeMethodNotFound, nil)
}

var testInfo = HelperInfo{
	Version:      "0.0.0-test",
	OS:           runtime.GOOS,
	Arch:         runtime.GOARCH,
	Hostname:     "testhost",
	Capabilities: []string{"session.v1", "agents.v1", "fs.v1"},
}

func frames(t *testing.T, payloads ...string) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	for _, p := range payloads {
		if err := WriteFrame(&buf, []byte(p)); err != nil {
			t.Fatal(err)
		}
	}
	return &buf
}

func serve(t *testing.T, h Handler, payloads ...string) []map[string]any {
	t.Helper()
	var out bytes.Buffer
	if err := Serve(context.Background(), frames(t, payloads...), &out, h, testInfo); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	var resps []map[string]any
	for out.Len() > 0 {
		raw, err := ReadFrame(&out)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatalf("response is not JSON: %v", err)
		}
		resps = append(resps, m)
	}
	return resps
}

func errorOf(t *testing.T, resp map[string]any) map[string]any {
	t.Helper()
	e, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error response, got %v", resp)
	}
	return e
}

const (
	helloV1  = `{"jsonrpc":"2.0","id":1,"method":"hello","params":{"protocol":1,"client":{"name":"t","version":"1"},"capabilities":[]}}`
	listCall = `{"jsonrpc":"2.0","id":2,"method":"session.list","params":{}}`
)

func TestHelloGating(t *testing.T) {
	h := &fakeHandler{}
	resps := serve(t, h,
		`{"jsonrpc":"2.0","id":1,"method":"session.list","params":{}}`,
		helloV1,
		listCall,
	)
	if len(resps) != 3 {
		t.Fatalf("got %d responses, want 3", len(resps))
	}
	if e := errorOf(t, resps[0]); e["code"] != float64(CodeNotReady) || e["message"] != "not_ready" {
		t.Errorf("pre-hello call: %v", e)
	}
	want := map[string]any{
		"jsonrpc": "2.0",
		"id":      float64(1),
		"result": map[string]any{
			"protocol": float64(1),
			"helper": map[string]any{
				"version": "0.0.0-test", "os": runtime.GOOS, "arch": runtime.GOARCH, "hostname": "testhost",
			},
			"capabilities": []any{"session.v1", "agents.v1", "fs.v1"},
		},
	}
	gotJSON, _ := json.Marshal(resps[1])
	wantJSON, _ := json.Marshal(want)
	if string(gotJSON) != string(wantJSON) {
		t.Errorf("hello response\n got %s\nwant %s", gotJSON, wantJSON)
	}
	if resps[2]["error"] != nil || resps[2]["result"] == nil {
		t.Errorf("post-hello call: %v", resps[2])
	}
	if len(h.calls) != 1 || h.calls[0] != "session.list" {
		t.Errorf("handler saw %v, want only the post-hello session.list", h.calls)
	}
}

func TestHelloBadVersion(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "protocol", "v1", "fixtures", "hello-bad-version.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fx struct {
		Request  json.RawMessage `json:"request"`
		Response json.RawMessage `json:"response"`
	}
	if err := json.Unmarshal(raw, &fx); err != nil {
		t.Fatal(err)
	}
	h := &fakeHandler{}
	resps := serve(t, h, string(fx.Request), listCall)
	if len(resps) != 2 {
		t.Fatalf("got %d responses, want 2", len(resps))
	}
	got, _ := json.Marshal(resps[0])
	var want, gotNorm any
	_ = json.Unmarshal(fx.Response, &want)
	_ = json.Unmarshal(got, &gotNorm)
	wantJSON, _ := json.Marshal(want)
	gotJSON, _ := json.Marshal(gotNorm)
	if string(gotJSON) != string(wantJSON) {
		t.Errorf("bad-version response\n got %s\nwant %s", gotJSON, wantJSON)
	}
	if e := errorOf(t, resps[1]); e["code"] != float64(CodeNotReady) {
		t.Errorf("call after bad hello: %v, want not_ready", e)
	}
	if len(h.calls) != 0 {
		t.Errorf("handler reached before a successful hello: %v", h.calls)
	}
}

func TestParseError(t *testing.T) {
	resps := serve(t, &fakeHandler{}, "not json", helloV1)
	if len(resps) != 2 {
		t.Fatalf("got %d responses, want 2", len(resps))
	}
	e := errorOf(t, resps[0])
	if e["code"] != float64(CodeParse) || e["message"] != "parse error" {
		t.Errorf("parse error response: %v", e)
	}
	if id, present := resps[0]["id"]; !present || id != nil {
		t.Errorf("id = %v (present=%v), want explicit null", id, present)
	}
	if resps[1]["result"] == nil {
		t.Errorf("connection did not recover: %v", resps[1])
	}
}

func TestInvalidRequest(t *testing.T) {
	for name, payload := range map[string]string{
		"array":          `[1,2]`,
		"scalar":         `42`,
		"wrong version":  `{"jsonrpc":"1.0","id":1,"method":"hello"}`,
		"numeric method": `{"jsonrpc":"2.0","id":1,"method":7}`,
		"missing method": `{"jsonrpc":"2.0","id":1}`,
	} {
		t.Run(name, func(t *testing.T) {
			resps := serve(t, &fakeHandler{}, payload)
			if len(resps) != 1 {
				t.Fatalf("got %d responses, want 1", len(resps))
			}
			e := errorOf(t, resps[0])
			if e["code"] != float64(CodeInvalidRequest) || e["message"] != "invalid request" {
				t.Errorf("got %v", e)
			}
		})
	}
}

func TestNotificationsAreIgnored(t *testing.T) {
	resps := serve(t, &fakeHandler{},
		`{"jsonrpc":"2.0","method":"hello","params":{"protocol":1}}`,
		listCall,
	)
	if len(resps) != 1 {
		t.Fatalf("got %d responses, want 1 (notification must not answer)", len(resps))
	}
	if e := errorOf(t, resps[0]); e["code"] != float64(CodeNotReady) {
		t.Errorf("notification hello must not make the connection ready: %v", e)
	}
}

func TestHelloBadParams(t *testing.T) {
	resps := serve(t, &fakeHandler{}, `{"jsonrpc":"2.0","id":1,"method":"hello","params":{"protocol":"one"}}`)
	if len(resps) != 1 {
		t.Fatalf("got %d responses, want 1", len(resps))
	}
	e := errorOf(t, resps[0])
	if e["code"] != float64(CodeInvalidParams) || e["message"] != "invalid params" {
		t.Fatalf("got %v", e)
	}
	data, _ := e["data"].(map[string]any)
	if reason, _ := data["reason"].(string); reason == "" {
		t.Errorf("missing data.reason: %v", e)
	}
}

func TestHandlerErrorPassesThrough(t *testing.T) {
	resps := serve(t, &fakeHandler{}, helloV1, `{"jsonrpc":"2.0","id":"x","method":"nope"}`)
	e := errorOf(t, resps[1])
	if e["code"] != float64(CodeMethodNotFound) || e["message"] != "method not found" || resps[1]["id"] != "x" {
		t.Errorf("got %v", resps[1])
	}
}

type nilHandler struct{}

func (nilHandler) Handle(context.Context, string, json.RawMessage) (any, *Error) { return nil, nil }

func TestNilResultIsExplicitNull(t *testing.T) {
	resps := serve(t, nilHandler{}, helloV1, listCall)
	if v, present := resps[1]["result"]; !present || v != nil {
		t.Errorf("result = %v (present=%v), want explicit null", v, present)
	}
}

func TestNilCapabilitiesAreAnEmptyArray(t *testing.T) {
	var out bytes.Buffer
	if err := Serve(context.Background(), frames(t, helloV1), &out, nilHandler{}, HelperInfo{}); err != nil {
		t.Fatal(err)
	}
	raw, err := ReadFrame(&out)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte(`"capabilities":[]`)) {
		t.Errorf("hello result: %s", raw)
	}
}

func TestServeStopsOnUnrecoverableInput(t *testing.T) {
	oversized := []byte{0x00, 0x10, 0x00, 0x01}
	truncated := append([]byte{0, 0, 0, 20}, `{"jsonrpc":`...)
	for name, tc := range map[string]struct {
		in   []byte
		want error
	}{
		"oversized frame":  {oversized, ErrFrameTooLarge},
		"truncated frame":  {truncated, io.ErrUnexpectedEOF},
		"truncated header": {[]byte{0, 0}, io.ErrUnexpectedEOF},
	} {
		t.Run(name, func(t *testing.T) {
			var out bytes.Buffer
			err := Serve(context.Background(), bytes.NewReader(tc.in), &out, &fakeHandler{}, testInfo)
			if !errors.Is(err, tc.want) {
				t.Errorf("Serve = %v, want %v", err, tc.want)
			}
			if out.Len() != 0 {
				t.Errorf("wrote %d bytes for unreadable input", out.Len())
			}
		})
	}
}
