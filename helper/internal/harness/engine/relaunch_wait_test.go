package engine

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// Right after a relaunch the agent is still drawing: typing waits for its
// input line instead of pasting into a screen that loses the Enter.
func TestTypingAfterRelaunchWaitsForTheInputLine(t *testing.T) {
	const id = "s_fakeinput9"
	p := &inputPane{enterSubmits: true}
	e := newInputEngine(t, id, p)
	p.idle = "starting…\n"
	s, _ := e.served(context.Background(), id)
	s.mu.Lock()
	s.relaunchedAt = time.Now()
	s.mu.Unlock()
	var ready atomic.Bool
	go func() {
		time.Sleep(300 * time.Millisecond)
		p.mu.Lock()
		p.idle = screenFile(t, "omp-idle")
		p.mu.Unlock()
		ready.Store(true)
	}()
	if res, err := login(e, id); err != nil || res.Result == "failed" {
		t.Fatalf("login = %+v %v", res, err)
	}
	if !ready.Load() {
		t.Fatal("typed before the input line showed")
	}
}
