package support

import (
	"runtime"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/enginetest"
)

// Helpers more than one agent family of the suite needs.

type QuietSub struct{ DoneCh chan struct{} }

func (q *QuietSub) Notify(harness.Notification) error { return nil }
func (q *QuietSub) Done() <-chan struct{}             { return q.DoneCh }

// fileLimit bounds each wait of the file-agent scenarios.
const FileLimit = 20 * time.Second

// nothingRuns: no item is left running or waiting, and the summary is
// not working.
func NothingRuns(t *testing.T, st enginetest.State) {
	t.Helper()
	for _, it := range Items(st, func(harness.Item) bool { return true }) {
		var state string
		switch {
		case it.Turn != nil:
			state = it.Turn.State
		case it.Tool != nil:
			state = it.Tool.State
		case it.Agent != nil:
			state = it.Agent.State
		}
		if state == harness.StateRunning || state == harness.StateWaiting {
			t.Errorf("%s (%s) is still %s", it.ID, it.Kind, state)
		}
	}
	if st.Mission.Summary.Working {
		t.Error("the summary still says working")
	}
}

// eventually waits for ok, failing with what after limit.
func Eventually(t *testing.T, limit time.Duration, what string, ok func() bool) {
	t.Helper()
	for deadline := time.Now().Add(limit); !ok(); time.Sleep(20 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("timed out: %s", what)
		}
	}
}

func CountKind(st enginetest.State, kind string) int {
	n := 0
	seen := map[string]bool{}
	for _, it := range st.Items {
		if it.Kind == kind && !seen[it.ID] {
			seen[it.ID] = true
			n++
		}
	}
	return n
}

// heavy bounds how many of the large scenarios run at once: each reads
// thousands of records and, under the race detector, uses a core for
// minutes, so on a small machine more than a few at a time only make the
// waits in every test of the package run out.
var heavy = make(chan struct{}, max(1, min(4, runtime.GOMAXPROCS(0)/2)))

// Heavy takes a place among the large scenarios, after t.Parallel, and
// gives it back when the test ends.
func Heavy(t testing.TB) {
	t.Helper()
	heavy <- struct{}{}
	t.Cleanup(func() { <-heavy })
}
