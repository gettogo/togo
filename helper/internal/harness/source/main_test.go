package source

import (
	"os"
	"testing"
	"time"
)

// The poll tunables are set once, before any watcher runs: setting them
// in a test raced with the previous test's watcher goroutines still
// reading them.
func TestMain(m *testing.M) {
	PollEvery, globEvery = 20*time.Millisecond, 20*time.Millisecond
	os.Exit(m.Run())
}
