// Package tmuxtest provides helpers for tests that drive a real tmux server
// on a private socket, keeping the production code free of testing imports.
package tmuxtest

import (
	"cmp"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/gettogo/togo/helper/internal/tmux"
)

var counter uint64

// New returns a Client bound to a unique private socket label and registers
// cleanup that stops the server on that label and deletes its socket file. The test is skipped when tmux
// is not on PATH. The returned client never uses the default socket.
func New(t testing.TB) *tmux.Client {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not on PATH")
	}
	label := fmt.Sprintf("togo-test-%d-%d", os.Getpid(), atomic.AddUint64(&counter, 1))
	c := tmux.NewLabel(label)
	t.Cleanup(func() {
		_ = exec.Command(c.Bin, "-L", label, "kill-server").Run()
		// tmux 3.6 leaves the socket file behind after kill-server.
		_ = os.Remove(SocketPath(label))
	})
	return c
}

// SocketPath returns where tmux puts the socket for label:
// $TMUX_TMPDIR (default /tmp), then tmux-UID, then the label.
func SocketPath(label string) string {
	dir := cmp.Or(os.Getenv("TMUX_TMPDIR"), "/tmp")
	return filepath.Join(dir, fmt.Sprintf("tmux-%d", os.Getuid()), label)
}
