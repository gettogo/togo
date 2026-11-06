package daemon

import (
	"context"
	"encoding/json"
	"path/filepath"
	"slices"
	"testing"

	"github.com/gettogo/togo/helper/internal/api"
	"github.com/gettogo/togo/helper/internal/session"
	"github.com/gettogo/togo/helper/internal/state"
	"github.com/gettogo/togo/helper/internal/tmux/tmuxtest"
	"github.com/gettogo/togo/helper/internal/wire"
)

func TestComposeServesLaunch(t *testing.T) {
	togoHome := t.TempDir()
	recent := &state.Recent{File: filepath.Join(togoHome, "recent.json")}
	svc := &session.Service{
		Tmux: tmuxtest.New(t), Recent: recent, Shell: "/bin/sh", Path: t.TempDir(),
		LockFile: filepath.Join(togoHome, "run", "bib.lock"),
	}
	c, err := Compose(&api.API{Sessions: svc, Recent: recent, Home: togoHome}, ComposeOptions{Home: togoHome, TogoPath: "/opt/to go/togo"})
	if err != nil {
		t.Fatal(err)
	}
	if svc.TogoPath != "/opt/to go/togo" {
		t.Errorf("session TogoPath = %q", svc.TogoPath)
	}
	if !slices.Contains(Capabilities, wire.CapLaunch) {
		t.Errorf("capabilities %q lack %s", Capabilities, wire.CapLaunch)
	}
	res, rerr := c.Handler().Handle(context.Background(), wire.MethodAgentsCheck, json.RawMessage(`{"program":"nosuchprogram"}`))
	if rerr != nil {
		t.Fatal(rerr)
	}
	if b, _ := json.Marshal(res); string(b) != `{"installed":false,"path":null}` {
		t.Errorf("agents.check = %s", b)
	}
}
