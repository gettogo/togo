package ports

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
	"github.com/gettogo/togo/helper/internal/session"
	"github.com/gettogo/togo/helper/internal/state"
	"github.com/gettogo/togo/helper/internal/tmux/tmuxtest"
)

// TestListFixture replays the frozen ports.list exchange against the real
// API with this package registered as an extension.
func TestListFixture(t *testing.T) {
	raw, err := os.ReadFile("../../../protocol/v1/fixtures/ports.list.json")
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

	// The real ps and lsof run here; under -race with other packages
	// running they took 16.5 s, past the 15 s product bound. The bound is
	// not what this pins.
	saved := lookupTimeout
	lookupTimeout = time.Minute
	t.Cleanup(func() { lookupTimeout = saved })

	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(home, "proj"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("SHELL", "/bin/sh")
	togoHome := t.TempDir()
	recent := &state.Recent{File: filepath.Join(togoHome, "recent.json")}
	tm := tmuxtest.New(t)
	svc := &session.Service{
		Tmux: tm, Recent: recent, Shell: "/bin/sh", Path: t.TempDir(),
		LockFile: filepath.Join(togoHome, "run", "bib.lock"),
		NewID:    func() string { return "s_abcdefghij" },
	}
	a := &api.API{Sessions: svc, Recent: recent, Home: home}
	a.Ext = (&Service{Sessions: svc, Panes: tm}).Methods()

	var in bytes.Buffer
	for _, req := range append(append([]json.RawMessage{}, fx.Setup...), fx.Request) {
		if err := rpc.WriteFrame(&in, req); err != nil {
			t.Fatal(err)
		}
	}
	var out bytes.Buffer
	if err := rpc.Serve(context.Background(), &in, &out, a, rpc.HelperInfo{Version: "0.0.0-test"}); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	var last json.RawMessage
	for out.Len() > 0 {
		if last, err = rpc.ReadFrame(&out); err != nil {
			t.Fatal(err)
		}
	}
	var got, want any
	if err := json.Unmarshal(last, &got); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(fx.Response, &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("response %s, want %s", last, fx.Response)
	}
}
