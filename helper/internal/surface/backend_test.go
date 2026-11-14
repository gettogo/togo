package surface

import (
	"context"
	"os/exec"
	"strings"
	"sync"
	"testing"

	"github.com/gettogo/togo/helper/internal/session"
	"github.com/gettogo/togo/helper/internal/tmux"
	"github.com/gettogo/togo/helper/internal/tmux/tmuxtest"
)

func TestANSICleanupReleasesMarkerAfterRuntimeIsGone(t *testing.T) {
	c := tmuxtest.New(t)
	ctx := context.Background()
	id := "s_abcdefghij"
	owner := strings.Repeat("a", 64)
	if err := c.NewSession(ctx, tmux.NewSessionOpts{Name: id, Command: []string{"/bin/sh"}}); err != nil {
		t.Fatal(err)
	}
	if err := c.ClaimSurface(ctx, id, owner); err != nil {
		t.Fatal(err)
	}
	b := &paneBackend{sessions: &session.Service{Tmux: c}, companions: &Companions{Home: t.TempDir()}}
	if err := b.control(ctx, id, runtimeInfo{control: &sync.Mutex{}}, owner, "ansi"); err == nil {
		t.Fatal("absent original runtime was treated as live")
	}
	args := append(append([]string{}, c.Socket...), "show-options", "-qv", "-t", "="+id+":", "@tern-tsp")
	raw, err := exec.Command(c.Bin, args...).Output()
	if err != nil || strings.TrimSpace(string(raw)) != "" {
		t.Fatalf("stale discovery marker remained: %q %v", raw, err)
	}
}
