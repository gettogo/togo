package tmuxtest_test

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/gettogo/togo/helper/internal/tmux"
	"github.com/gettogo/togo/helper/internal/tmux/tmuxtest"
)

func TestCleanupRemovesSocketFile(t *testing.T) {
	var socket string
	t.Run("server", func(t *testing.T) {
		c := tmuxtest.New(t)
		if err := c.NewSession(context.Background(), tmux.NewSessionOpts{Name: "s", Dir: t.TempDir()}); err != nil {
			t.Fatalf("NewSession: %v", err)
		}
		socket = tmuxtest.SocketPath(c.Socket[1])
		if _, err := os.Stat(socket); err != nil {
			t.Fatalf("socket %s missing while the server runs: %v", socket, err)
		}
	})
	if _, err := os.Stat(socket); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("socket %s still present after cleanup (err %v)", socket, err)
	}
}
