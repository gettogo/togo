// Package logfile keeps the helper's append-only logs bounded and private:
// a log past MaxSize moves to <name>.1 (replacing the older one) and every
// log file is 0600.
package logfile

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

// MaxSize is how large a log grows before it is rotated.
const MaxSize = 5 << 20

// Rotate moves path to path.1 when it is larger than max. A missing file
// is not an error.
func Rotate(path string, max int64) error {
	st, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil || st.Size() <= max {
		return err
	}
	return os.Rename(path, path+".1")
}

// Open rotates path if it is too large, then opens it for appending with
// mode 0600, tightening the mode of a file that already existed.
func Open(path string, max int64) (*os.File, error) {
	_ = Rotate(path, max)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	_ = f.Chmod(0o600)
	return f, nil
}

// Adopt is for a daemon whose stdout and stderr a service manager (launchd
// StandardOutPath) or a detached start pointed at path: it makes the file
// 0600 and, when it is past max, rotates it and points fds 1 and 2 at a
// fresh file. Output that goes anywhere else (a terminal) is left alone.
func Adopt(path string, max int64) error {
	if !isStdout(path) {
		return nil
	}
	_ = os.Chmod(path, 0o600)
	st, err := os.Stat(path)
	if err != nil || st.Size() <= max {
		return err
	}
	if err := os.Rename(path, path+".1"); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := unix.Dup2(int(f.Fd()), 1); err != nil {
		return err
	}
	return unix.Dup2(int(f.Fd()), 2)
}

// Keep runs Adopt now and then every interval until ctx ends, so a daemon
// that stays up for months still keeps its log bounded.
func Keep(ctx context.Context, path string, max int64, every time.Duration, logf func(string, ...any)) {
	run := func() {
		if err := Adopt(path, max); err != nil && logf != nil {
			logf("log rotate: %v", err)
		}
	}
	run()
	go func() {
		t := time.NewTicker(every)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				run()
			}
		}
	}()
}

// isStdout reports whether fd 1 is the file at path.
func isStdout(path string) bool {
	var out, file unix.Stat_t
	if unix.Fstat(1, &out) != nil || unix.Stat(path, &file) != nil {
		return false
	}
	return out.Dev == file.Dev && out.Ino == file.Ino
}
