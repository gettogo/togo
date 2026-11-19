package main

import (
	"os"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// A stop signal while the daemon is already on its way out (a second
// SIGTERM, or the first during a restart's exit steps) asks it to skip
// the rest of its waits; the first signal of a running daemon starts the
// exit as before.
func TestStopAgainHurriesTheExit(t *testing.T) {
	for _, tc := range []struct {
		name      string
		finishing bool
		signals   int
		hurry     bool
	}{
		{"first signal of a running daemon", false, 1, false},
		{"second signal", false, 2, true},
		{"a signal during a restart's exit steps", true, 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sig := make(chan os.Signal, 4)
			var finishing atomic.Bool
			finishing.Store(tc.finishing)
			hurry := hurryOn(sig, &finishing)
			for range tc.signals {
				sig <- syscall.SIGTERM
			}
			select {
			case <-hurry:
				if !tc.hurry {
					t.Fatal("hurried")
				}
			case <-time.After(100 * time.Millisecond):
				if tc.hurry {
					t.Fatal("not hurried")
				}
			}
		})
	}
}
