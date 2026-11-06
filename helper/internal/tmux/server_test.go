package tmux_test

import (
	"context"
	"os/exec"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/tmux"
	"github.com/gettogo/togo/helper/internal/tmux/tmuxtest"
)

// TestServerStarted reports no server before the first session and after
// kill-server, and the server's start time while one runs.
func TestServerStarted(t *testing.T) {
	c := tmuxtest.New(t)
	ctx := context.Background()
	if _, ok, err := c.ServerStarted(ctx); err != nil || ok {
		t.Fatalf("before any session: ok %v err %v, want no server", ok, err)
	}
	before := time.Now().Truncate(time.Second)
	if err := c.NewSession(ctx, tmux.NewSessionOpts{Name: "s_abcdefghij", Command: []string{"/bin/sleep", "60"}}); err != nil {
		t.Fatal(err)
	}
	started, ok, err := c.ServerStarted(ctx)
	if err != nil || !ok {
		t.Fatalf("with a session: ok %v err %v", ok, err)
	}
	if started.Before(before) || started.After(time.Now()) {
		t.Errorf("started %v, want between %v and now", started, before)
	}
	if err := exec.Command(c.Bin, append(append([]string{}, c.Socket...), "kill-server")...).Run(); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := c.ServerStarted(ctx); err != nil || ok {
		t.Fatalf("after kill-server: ok %v err %v, want no server", ok, err)
	}
}
