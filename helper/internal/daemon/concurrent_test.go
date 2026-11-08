package daemon

import (
	"slices"
	"testing"
)

// Slow read-only methods must run off the read loop, so Approve and Send
// never wait behind a ports or git listing.
func TestSlowReadsRunConcurrently(t *testing.T) {
	for _, m := range []string{"ports.list", "git.status", "git.diff", "fs.read", "harness.describe", "harness.history", "harness.send"} {
		if !slices.Contains(Concurrent, m) {
			t.Errorf("%s is not concurrent", m)
		}
	}
	for _, m := range []string{"git.commit", "git.stage", "session.create", "harness.open"} {
		if slices.Contains(Concurrent, m) {
			t.Errorf("%s must stay ordered", m)
		}
	}
}
