package daemon

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	"github.com/gettogo/togo/helper/internal/api"
	"github.com/gettogo/togo/helper/internal/awake"
	"github.com/gettogo/togo/helper/internal/session"
	"github.com/gettogo/togo/helper/internal/state"
	"github.com/gettogo/togo/helper/internal/tmux/tmuxtest"
	"github.com/gettogo/togo/helper/internal/wire"
)

type countingHolder struct {
	mu   sync.Mutex
	held bool
}

func (h *countingHolder) Hold() error    { h.mu.Lock(); h.held = true; h.mu.Unlock(); return nil }
func (h *countingHolder) Release() error { h.mu.Lock(); h.held = false; h.mu.Unlock(); return nil }
func (h *countingHolder) Err() error     { return nil }
func (h *countingHolder) Held() bool     { h.mu.Lock(); defer h.mu.Unlock(); return h.held }

// TestComposeHoldsAwakeWhileSessionWorks drives the awake service from the
// hub's session status, the same view the app sees.
func TestComposeHoldsAwakeWhileSessionWorks(t *testing.T) {
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
	svc := &session.Service{
		Tmux: tmuxtest.New(t), Recent: recent, Shell: "/bin/sh", Path: t.TempDir(),
		LockFile: filepath.Join(togoHome, "run", "bib.lock"),
	}
	holder := &countingHolder{}
	c, err := Compose(&api.API{Sessions: svc, Recent: recent, Home: home}, ComposeOptions{
		Home: togoHome, TogoPath: "/bin/true",
		Awake: &awake.Config{Supported: true, Holder: holder},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(Capabilities, awake.Capability) {
		t.Errorf("capabilities %q lack %s", Capabilities, awake.Capability)
	}
	var mu sync.Mutex
	var events []awake.Status
	c.Hub.OnEvent(func(ev wire.Event) {
		if ev.Kind == awake.EventKind {
			mu.Lock()
			events = append(events, ev.Payload.(awake.Event).Awake)
			mu.Unlock()
		}
	})
	sess, err := svc.Create(context.Background(), session.CreateOpts{Agent: "shell", Cwd: "~/proj"})
	if err != nil {
		t.Fatal(err)
	}

	c.Hub.SetStatus(sess.ID, "working", nil)
	c.Awake.Update(context.Background())
	if !holder.Held() {
		t.Fatal("a working session did not hold the machine")
	}
	res, rerr := c.Handler().Handle(context.Background(), awake.MethodStatus, json.RawMessage(`{}`))
	if rerr != nil {
		t.Fatal(rerr)
	}
	if st := res.(awake.Status); !st.Held || st.Reason != awake.ReasonActive {
		t.Fatalf("awake.status %+v", st)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(events) != 1 || !events[0].Held {
		t.Fatalf("awake events %+v, want one held", events)
	}
}
