package session

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

const maxLockBackoff = 100 * time.Millisecond

// withLock runs fn while holding an exclusive flock on path. The lock is
// what keeps bibs unique when the daemon and a CLI process, or two Services
// in one process, create sessions at the same time: flock locks belong to
// the open file, so separate opens exclude each other even within a process.
// The lock is polled without blocking so a stuck holder cannot outlast ctx.
func withLock(ctx context.Context, path string, fn func() error) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("bib lock: %w", err)
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return fmt.Errorf("bib lock: %w", err)
	}
	defer f.Close()
	fd := int(f.Fd())
	backoff := time.Millisecond
	for {
		err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EINTR) {
			return fmt.Errorf("bib lock %s: %w", path, err)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("bib lock %s: %w", path, ctx.Err())
		case <-time.After(backoff):
		}
		backoff = min(2*backoff, maxLockBackoff)
	}
	defer syscall.Flock(fd, syscall.LOCK_UN)
	return fn()
}
