package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/gettogo/togo/helper/internal/api"
	"github.com/gettogo/togo/helper/internal/rpc"
	"github.com/gettogo/togo/helper/internal/session"
	"github.com/gettogo/togo/helper/internal/tmux"
)

func TestExtMethodDispatched(t *testing.T) {
	a, _ := harness(t, &tmux.Client{Bin: "tmux-not-installed"})
	var got string
	a.Ext = map[string]api.Method{
		"ports.list": func(_ context.Context, raw json.RawMessage) (any, error) {
			var p struct {
				ID string `json:"id"`
			}
			if err := api.Decode(raw, &p); err != nil {
				return nil, err
			}
			got = p.ID
			return map[string]any{"ports": []any{}}, nil
		},
	}
	res, e := call(t, a, "ports.list", `{"id":"s_abcdefghij"}`)
	if e != nil {
		t.Fatalf("error %+v", e)
	}
	if got != "s_abcdefghij" {
		t.Errorf("method saw id %q", got)
	}
	if m, ok := res.(map[string]any); !ok || m["ports"] == nil {
		t.Errorf("result %#v", res)
	}
}

func TestExtMethodOverridesBuiltin(t *testing.T) {
	a, _ := harness(t, &tmux.Client{Bin: "tmux-not-installed"})
	a.Ext = map[string]api.Method{
		"fs.listDir": func(context.Context, json.RawMessage) (any, error) { return "ext", nil },
	}
	if res, e := call(t, a, "fs.listDir", `{}`); e != nil || res != "ext" {
		t.Fatalf("got %v, %+v; want the extension's result", res, e)
	}
}

func TestCodeErrorPassesThrough(t *testing.T) {
	a, _ := harness(t, &tmux.Client{Bin: "tmux-not-installed"})
	data := map[string]any{"reason": "answered", "by": "desk"}
	a.Ext = map[string]api.Method{
		"approval.answer": func(context.Context, json.RawMessage) (any, error) {
			return nil, &api.CodeError{Code: rpc.CodeConflict, Data: data}
		},
	}
	_, e := call(t, a, "approval.answer", `{}`)
	if e == nil || e.Code != rpc.CodeConflict || e.Message != "conflict" {
		t.Fatalf("got %+v, want -32006 conflict", e)
	}
	b, _ := json.Marshal(e.Data)
	if string(b) != `{"by":"desk","reason":"answered"}` {
		t.Errorf("data = %s", b)
	}
}

func TestWrappedCodeErrorPassesThrough(t *testing.T) {
	a, _ := harness(t, &tmux.Client{Bin: "tmux-not-installed"})
	a.Ext = map[string]api.Method{
		"git.diff": func(context.Context, json.RawMessage) (any, error) {
			return nil, errors.Join(errors.New("git diff"), &api.CodeError{Code: rpc.CodeInvalidPath})
		},
	}
	_, e := call(t, a, "git.diff", `{}`)
	if e == nil || e.Code != rpc.CodeInvalidPath || e.Data != nil {
		t.Fatalf("got %+v, want -32003 without data", e)
	}
}

func TestExtInvalidParamsAndInternalErrors(t *testing.T) {
	a, _ := harness(t, &tmux.Client{Bin: "tmux-not-installed"})
	var logged string
	a.Logf = func(format string, args ...any) { logged = format }
	a.Ext = map[string]api.Method{
		"session.setMute": func(_ context.Context, raw json.RawMessage) (any, error) {
			var p struct {
				Muted bool `json:"muted"`
			}
			return nil, api.Decode(raw, &p)
		},
		"push.unpair": func(context.Context, json.RawMessage) (any, error) {
			return nil, errors.New("disk full")
		},
	}
	if _, e := call(t, a, "session.setMute", `{"muted":"yes"}`); e == nil || e.Code != rpc.CodeInvalidParams {
		t.Errorf("bad params: got %+v, want -32602", e)
	}
	if _, e := call(t, a, "session.setMute", `[]`); e == nil || e.Code != rpc.CodeInvalidParams {
		t.Errorf("non-object params: got %+v, want -32602", e)
	}
	if _, e := call(t, a, "push.unpair", `{}`); e == nil || e.Code != rpc.CodeInternal || e.Data != nil {
		t.Errorf("internal: got %+v, want -32603 without data", e)
	}
	if logged == "" {
		t.Error("internal error was not logged")
	}
	var invalid *session.InvalidParamsError
	if err := api.Decode(json.RawMessage(`1`), &struct{}{}); !errors.As(err, &invalid) {
		t.Errorf("Decode of a number: %v", err)
	}
}
