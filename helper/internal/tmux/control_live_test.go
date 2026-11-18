package tmux_test

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/tmux"
	"github.com/gettogo/togo/helper/internal/tmux/tmuxtest"
)

func TestObservePaneLeavesExistingOutputPipeAndStopsOnCancel(t *testing.T) {
	c := tmuxtest.New(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := c.NewSession(ctx, tmux.NewSessionOpts{Name: "observe", Command: []string{"/bin/sh"}, Cols: 100, Rows: 32}); err != nil {
		t.Fatal(err)
	}
	pane, err := c.ActivePane(ctx, "observe")
	if err != nil {
		t.Fatal(err)
	}
	if err := c.PipePane(ctx, "=observe:", "cat >/dev/null"); err != nil {
		t.Fatal(err)
	}
	ready, done := make(chan struct{}), make(chan error, 1)
	got := make(chan []byte, 1)
	go func() {
		var out bytes.Buffer
		done <- c.ObservePane(ctx, "observe", pane, func() { close(ready) }, func(data []byte) error {
			out.Write(data)
			if bytes.Contains(out.Bytes(), []byte("\x1b_tsp;q;")) {
				select {
				case got <- bytes.Clone(out.Bytes()):
				default:
				}
			}
			return nil
		})
	}()
	select {
	case <-ready:
	case <-time.After(3 * time.Second):
		t.Fatal("control observer did not become ready")
	}
	cmd := `printf '\033_tsp;q;{"q":"hello"}\033\\'`
	if err := c.SendKeys(ctx, pane, true, cmd); err != nil {
		t.Fatal(err)
	}
	if err := c.SendKeys(ctx, pane, false, "Enter"); err != nil {
		t.Fatal(err)
	}
	select {
	case data := <-got:
		if !strings.Contains(string(data), "tsp;q;") {
			t.Fatal("raw APC was lost")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("pane output did not reach the observer")
	}
	piped, err := c.PipedSessions(ctx)
	if err != nil || !piped["observe"] {
		t.Fatalf("existing pipe replaced: %v %v", piped, err)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("control client survived cancellation")
	}
}
