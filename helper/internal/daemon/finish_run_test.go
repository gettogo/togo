package daemon

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/sockd"
)

// A daemon on its way out still writes: queued hooks into their store,
// the open approvals into theirs. It keeps the lock and the pid file until
// that is done, so a daemon started meanwhile (`togo daemon restart` or a
// phone's serve with no service installed) cannot run beside it; and it
// no longer answers, so a hook sent then fails at once and is not
// acknowledged.
func TestRunKeepsTheLockUntilItsExitStepsEnd(t *testing.T) {
	home := shortHome(t)
	ctx, cancel := context.WithCancel(context.Background())
	finishing := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, Config{Home: home, Build: buildPing, Info: testInfo, Logf: t.Logf,
			Finish: func() { close(finishing); <-release }})
	}()
	deadline := time.Now().Add(2 * time.Second)
	for {
		c, err := sockd.Dial(context.Background(), SocketPath(home))
		if err == nil {
			c.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("daemon socket never came up: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case <-finishing:
	case <-time.After(5 * time.Second):
		t.Fatal("the exit steps never ran")
	}
	second, stop := context.WithTimeout(context.Background(), 2*time.Second)
	defer stop()
	if err := Run(second, Config{Home: home, Build: buildPing, Info: testInfo}); !errors.Is(err, ErrRunning) {
		close(release)
		t.Fatalf("a second daemon during the exit steps: %v, want ErrRunning", err)
	}
	if _, err := os.Stat(PIDPath(home)); err != nil {
		t.Errorf("the pid file is gone before the exit steps ended: %v", err)
	}
	dial, stopDial := context.WithTimeout(context.Background(), time.Second)
	if c, err := sockd.Dial(dial, SocketPath(home)); err == nil {
		c.Close()
		t.Error("the daemon still accepts connections during its exit steps")
	}
	stopDial()
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("Run: %v", err)
	}
	if _, err := os.Stat(PIDPath(home)); !os.IsNotExist(err) {
		t.Errorf("pid file after the exit: %v", err)
	}
}
