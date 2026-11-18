package awake

import (
	"testing"
	"time"
)

// A hold refused for good (logind saying no) is retried at the longest wait
// for as long as it fails: the doubling must not overflow back to nothing.
func TestHoldRetryWaitNeverFallsBelowItsCap(t *testing.T) {
	m := &Manager{cfg: Config{Logf: func(string, ...any) {}}}
	now := time.Now()
	for i := 0; i < 200; i++ {
		m.holdFailed(now)
		if i >= 8 && m.holdRetryAt.Sub(now) != holdRetryMax {
			t.Fatalf("after %d failures the wait is %s, want %s", m.holdFails, m.holdRetryAt.Sub(now), holdRetryMax)
		}
	}
}
