package awake

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/api"
	"github.com/gettogo/togo/helper/internal/rpc"
)

const fixturesDir = "../../../protocol/v1/fixtures"

// fixtureManager is the host the awake fixtures describe: a supported Mac
// on AC power at 100%, nothing active, default settings, no sudoers rule.
func fixtureManager(t *testing.T) *Manager {
	t.Helper()
	m, err := New(Config{
		Home: t.TempDir(), Supported: true, Holder: &fakeHolder{}, Override: &fakeOverride{},
		AdminReady: func() bool { return false },
		Power:      &fakePower{p: Power{Source: PowerAC, Percent: intp(100)}},
		Now:        func() time.Time { return time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestFixtures(t *testing.T) {
	for _, name := range []string{"awake.status.json", "awake.set.json", "awake.set-needs-admin.json"} {
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join(fixturesDir, name))
			if err != nil {
				t.Fatal(err)
			}
			var fx struct {
				Setup    []json.RawMessage `json:"setup"`
				Request  json.RawMessage   `json:"request"`
				Response json.RawMessage   `json:"response"`
			}
			if err := json.Unmarshal(raw, &fx); err != nil {
				t.Fatal(err)
			}
			a := &api.API{Ext: fixtureManager(t).Methods()}
			var in bytes.Buffer
			for _, req := range append(append([]json.RawMessage{}, fx.Setup...), fx.Request) {
				if err := rpc.WriteFrame(&in, req); err != nil {
					t.Fatal(err)
				}
			}
			var out bytes.Buffer
			if err := rpc.Serve(context.Background(), &in, &out, a, rpc.HelperInfo{Version: "0.0.0-test"}); err != nil {
				t.Fatal(err)
			}
			var last json.RawMessage
			for out.Len() > 0 {
				if last, err = rpc.ReadFrame(&out); err != nil {
					t.Fatal(err)
				}
			}
			var got, want any
			_ = json.Unmarshal(last, &got)
			_ = json.Unmarshal(fx.Response, &want)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("response %s\nwant %s", last, fx.Response)
			}
		})
	}
}

func TestSetRejectsBadParams(t *testing.T) {
	m := fixtureManager(t)
	set := m.Methods()[MethodSet]
	for _, params := range []string{`{}`, `{"mode":"always"}`, `{"batteryFloor":5}`, `{"mode":1}`} {
		_, err := set(context.Background(), json.RawMessage(params))
		if err == nil {
			t.Errorf("%s accepted", params)
		}
		if _, ok := err.(*api.CodeError); ok {
			t.Errorf("%s: %v, want invalid params", params, err)
		}
	}
}
