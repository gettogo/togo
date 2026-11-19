package hookstyle

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

// The transcript look walks folders and asks the process view, which can
// take seconds. It runs in its own goroutine: State, which the engine
// calls under the session's lock for every send and open, returns at once
// while a look is blocked, and the answer is applied when it comes, with
// the engine woken.
func TestTranscriptLookNeverBlocksState(t *testing.T) {
	release := make(chan struct{})
	var calls atomic.Int32
	m := New(harness.Env{Cwd: "/w"}, Options{Name: "t", Transcript: func(ctx context.Context, _ harness.Env, _ string, _ func() bool) string {
		calls.Add(1)
		select {
		case <-release:
		case <-ctx.Done():
		}
		return "/w/journal/*.jsonl"
	}})
	woke := make(chan struct{}, 1)
	m.Wake(func() { woke <- struct{}{} })
	start := time.Now()
	for i := 0; i < 20; i++ {
		if p := m.State()["transcript_path"]; p != "" {
			t.Fatalf("a path before the look ended: %q", p)
		}
	}
	if d := time.Since(start); d > 100*time.Millisecond {
		t.Fatalf("State blocked for %v behind the look", d)
	}
	for deadline := time.Now().Add(time.Second); calls.Load() == 0 && time.Now().Before(deadline); {
		time.Sleep(5 * time.Millisecond)
	}
	m.State()
	if n := calls.Load(); n != 1 {
		t.Fatalf("%d looks at once, want 1", n)
	}
	close(release)
	select {
	case <-woke:
	case <-time.After(3 * time.Second):
		t.Fatal("the engine was not woken")
	}
	if p := m.State()["transcript_path"]; p != "/w/journal/*.jsonl" {
		t.Fatalf("path %q after the look", p)
	}
}
