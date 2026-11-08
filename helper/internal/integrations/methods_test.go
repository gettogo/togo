package integrations

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/hooks"
	"github.com/gettogo/togo/helper/internal/session"
)

func newService(t *testing.T) (*Service, hooks.Options) {
	t.Helper()
	root := t.TempDir()
	o := hooks.DefaultOptions(func(string) string { return "" }, root, filepath.Join(root, ".togo"), "/opt/togo/bin/togo")
	o.HasBinary = func(c string) bool { return c != "aider" }
	for _, d := range []string{o.ClaudeDir, o.FactoryDir, o.TogoHome} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	s := &Service{Options: func(context.Context) (hooks.Options, error) { return o, nil }}
	return s, o
}

func call(t *testing.T, s *Service, method, params string) (any, error) {
	t.Helper()
	return s.Methods()[method](context.Background(), json.RawMessage(params))
}

func rows(v any) map[string]hooks.Report {
	out := map[string]hooks.Report{}
	var list []hooks.Report
	switch r := v.(type) {
	case StatusResult:
		list = r.Integrations
	case ChangeResult:
		list = r.Integrations
	}
	for _, r := range list {
		out[r.ID] = r
	}
	return out
}

func TestStatusListsEveryHarness(t *testing.T) {
	s, _ := newService(t)
	res, err := call(t, s, MethodStatus, `{}`)
	if err != nil {
		t.Fatal(err)
	}
	got := rows(res)
	if len(got) != len(hooks.Registry) {
		t.Fatalf("%d rows", len(got))
	}
	if got["droid"].State != hooks.StateNeedsSetup || got["codex"].State != hooks.StateNotInstalled ||
		got["kimi"].State != hooks.StateNotNeeded || got["aider"].State != hooks.StateNotInstalled {
		t.Fatalf("%+v", got)
	}
	if _, err := call(t, s, MethodStatus, `{"x":1}`); err == nil {
		t.Fatal("unknown param accepted")
	}
}

func TestInstallOneThenUninstall(t *testing.T) {
	s, o := newService(t)
	res, err := call(t, s, MethodInstall, `{"agents":["droid"]}`)
	if err != nil {
		t.Fatal(err)
	}
	cr := res.(ChangeResult)
	if len(cr.Results) != 1 || cr.Results[0].ID != "droid" || cr.Results[0].State != "installed" {
		t.Fatalf("%+v", cr.Results)
	}
	if rows(res)["droid"].State != hooks.StateReady || rows(res)["claude"].State != hooks.StateNeedsSetup {
		t.Fatalf("rows after %+v", rows(res))
	}
	if _, err := os.Stat(filepath.Join(o.ClaudeDir, "settings.json")); err == nil {
		t.Fatal("installing droid touched claude")
	}
	res, err = call(t, s, MethodUninstall, `{"agents":["droid"]}`)
	if err != nil || res.(ChangeResult).Results[0].State != "removed" {
		t.Fatalf("%+v %v", res, err)
	}
}

func TestInstallAllSetsUpEveryInstalledHarness(t *testing.T) {
	s, _ := newService(t)
	res, err := call(t, s, MethodInstall, `{}`)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, r := range res.(ChangeResult).Results {
		ids = append(ids, r.ID)
	}
	if !reflect.DeepEqual(ids, []string{"claude", "droid"}) {
		t.Fatalf("installed %v", ids)
	}
}

func TestChangeValidatesAgents(t *testing.T) {
	s, _ := newService(t)
	for _, p := range []string{`{"agents":["kimi"]}`, `{"agents":["../etc"]}`, `{"agents":["droid","droid"]}`, `{"agents":["droid"],"path":"/x"}`} {
		_, err := call(t, s, MethodInstall, p)
		var inv *session.InvalidParamsError
		if err == nil || (!errors.As(err, &inv) && p != `{"agents":["droid"],"path":"/x"}`) {
			t.Errorf("%s: %v", p, err)
		}
	}
	if _, err := call(t, s, MethodUninstall, `{}`); err == nil {
		t.Error("uninstall without agents accepted")
	}
	_, err := call(t, s, MethodInstall, `{"agents":["kimi"]}`)
	var inv *session.InvalidParamsError
	if !errors.As(err, &inv) || inv.Reason != "unknown_agent" {
		t.Fatalf("%v", err)
	}
}

func TestObserveRecordsLastHook(t *testing.T) {
	s, o := newService(t)
	now := time.Date(2026, 11, 7, 9, 0, 0, 0, time.UTC)
	s.Now = func() time.Time { return now }
	calls := 0
	hook := s.Observe(o.TogoHome, func(context.Context, json.RawMessage) (any, error) {
		calls++
		return struct{}{}, nil
	})
	if _, err := hook(context.Background(), json.RawMessage(`{"agent":"claude","event":"meta","sessionId":null,"cwd":"","data":{},"hook":"statusline"}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := hook(context.Background(), json.RawMessage(`{"agent":"nope","hook":"Stop"}`)); err != nil {
		t.Fatal(err)
	}
	seen := hooks.LoadSeen(o.TogoHome)
	if calls != 2 || !seen["claude.statusline"].Equal(now) || len(seen) != 1 {
		t.Fatalf("calls %d seen %v", calls, seen)
	}
	res, _ := call(t, s, MethodStatus, `{}`)
	if rows(res)["claude"].StatusLineAt != "2026-11-07T09:00:00Z" {
		t.Fatalf("%+v", rows(res)["claude"])
	}
	failing := s.Observe(o.TogoHome, func(context.Context, json.RawMessage) (any, error) { return nil, errors.New("no") })
	if _, err := failing(context.Background(), json.RawMessage(`{"agent":"droid"}`)); err == nil {
		t.Fatal("error swallowed")
	}
	if _, ok := hooks.LoadSeen(o.TogoHome)["droid"]; ok {
		t.Fatal("a refused hook counted as received")
	}
}

// The Go types carry every field of the protocol fixtures.
func TestFixturesRoundTrip(t *testing.T) {
	for name, target := range map[string]any{
		"integrations.status.json":    &StatusResult{},
		"integrations.install.json":   &ChangeResult{},
		"integrations.uninstall.json": &ChangeResult{},
	} {
		raw, err := os.ReadFile(filepath.Join("../../../protocol/v1/fixtures", name))
		if err != nil {
			t.Fatal(err)
		}
		var fx struct {
			Request  struct{ Params json.RawMessage }
			Response struct{ Result json.RawMessage }
		}
		if err := json.Unmarshal(raw, &fx); err != nil {
			t.Fatal(err)
		}
		var params ChangeParams
		if err := json.Unmarshal(fx.Request.Params, &params); err != nil {
			t.Fatal(err)
		}
		dec := json.NewDecoder(bytesReader(fx.Response.Result))
		dec.DisallowUnknownFields()
		if err := dec.Decode(target); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		again, _ := json.Marshal(target)
		var a, b any
		_ = json.Unmarshal(again, &a)
		_ = json.Unmarshal(fx.Response.Result, &b)
		if !reflect.DeepEqual(a, b) {
			t.Errorf("%s: round trip\n%s\n%s", name, again, fx.Response.Result)
		}
	}
}

func bytesReader(b []byte) *bytes.Reader { return bytes.NewReader(b) }
