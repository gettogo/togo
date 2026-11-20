package procs

import (
	"os"
	"testing"
	"time"
)

// The test process started a moment ago, by the wall clock.
func TestStartedIsThisProcessesStart(t *testing.T) {
	at, err := Started(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	if d := time.Since(at); d < 0 || d > 10*time.Minute {
		t.Fatalf("started %v ago", d)
	}
	if _, err := Started(1 << 30); err == nil {
		t.Fatal("a pid that does not exist has a start time")
	}
}
