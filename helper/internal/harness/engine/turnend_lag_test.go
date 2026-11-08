package engine

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

// The hub's status follows the pane and settles seconds after a turn ends.
// When the feed itself said the turn ended, working clears at once instead
// of waiting 6-10 s for the hub (Stop, the queue and the pill act on it).
func TestWorkingClearsWhenTheFeedEndsTheTurn(t *testing.T) {
	const id = "s_turnlag001"
	var status atomic.Value
	status.Store("working")
	e := statusEngine(t, id, &seqPane{mode: "a"}, &status)
	s, _ := e.served(context.Background(), id)
	start := time.Now().Add(-2 * time.Second)
	deliver(id, map[string]any{"state": map[string]string{"working": "true", harness.StateWorkingAt: harness.StampWorking(start)}})
	waitFor(t, func() bool { return s.sum.Load().Working })
	deliver(id, map[string]any{"state": map[string]string{"working": "false", harness.StateWorkingAt: harness.StampWorking(time.Now())}})
	waitFor(t, func() bool { return !s.sum.Load().Working })
	// A new turn the feed reports shows at once.
	deliver(id, map[string]any{"state": map[string]string{"working": "true", harness.StateWorkingAt: harness.StampWorking(time.Now())}})
	waitFor(t, func() bool { return s.sum.Load().Working })
}
