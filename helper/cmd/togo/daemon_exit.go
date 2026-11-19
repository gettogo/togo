package main

import (
	"os"
	"sync"
	"sync/atomic"
)

// hurryOn closes the returned channel on a stop signal that arrives while
// the daemon is already on its way out: a second one, or the first once
// its exit steps run (a restart's). The daemon then skips the rest of its
// waits for hooks and services (daemon/exit.go). The first signal of a
// running daemon is the daemon's own context's to act on.
func hurryOn(sig <-chan os.Signal, finishing *atomic.Bool) <-chan struct{} {
	hurry := make(chan struct{})
	var once sync.Once
	go func() {
		seen := 0
		for range sig {
			seen++
			if seen > 1 || finishing.Load() {
				once.Do(func() { close(hurry) })
			}
		}
	}()
	return hurry
}
