package engine

import (
	"bytes"
	"context"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/source"
)

// Hooks accepted while the session's feed is busy wait in its queue. The
// daemon's exit drains the bus before it stops anything (daemon/exit.go),
// and a session's feed runs on the session's own context, which no stop
// signal reaches (newSess): every queued hook is in the store once Drain
// returns.
func TestDrainedHooksAreStored(t *testing.T) {
	const id = "s_hookdrain1"
	home, togoHome := t.TempDir(), t.TempDir()
	var notes atomic.Int32
	e := storeEngine(t, home, togoHome, &notes, id)
	t.Cleanup(func() { e.Forget(id) })
	s, err := e.served(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	s.mu.Lock() // the feed is busy: what is delivered now waits in its queue
	for i := range 5 {
		deliver(id, turnRecord(i, harness.StateOK))
	}
	time.AfterFunc(100*time.Millisecond, s.mu.Unlock)
	if !source.Hooks.Drain(id, 5*time.Second) {
		t.Fatal("the queued hooks were not read")
	}
	raw, err := os.ReadFile(hookStorePath(togoHome, id))
	if err != nil {
		t.Fatal(err)
	}
	if n := bytes.Count(raw, []byte("\n")) - 1; n != 5 {
		t.Fatalf("%d records stored after the drain, want 5:\n%s", n, raw)
	}
}
