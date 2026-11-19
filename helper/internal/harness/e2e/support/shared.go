package support

import (
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
