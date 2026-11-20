package tape

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// Runner runs scenario code outside `go test`: the rig wants a
// testing.TB, and this one fails by ending the goroutine, as a test does,
// and removes the folders it made when the run ends.
type Runner struct {
	// Scratch is the folder the run's temporary folders are made in.
	Scratch string
	// Verbose prints the engine's log lines to stderr.
	Verbose bool
}

// Do runs f, once. It returns Skip when f skips, the failure message as an
// error when f fails, and f's own error otherwise.
func (r Runner) Do(name string, f func(t testing.TB) error) error {
	t := &shim{name: name, runner: r}
	t.ctx, t.cancel = context.WithCancel(context.Background())
	var result error
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer t.finish()
		result = f(t)
	}()
	<-done
	switch {
	case t.skipped != "":
		return Skip(t.skipped)
	case t.failed:
		return errors.New(strings.TrimSpace(t.msg.String()))
	}
	return result
}

// shim is the testing.TB the rig sees. Methods it does not define panic
// through the embedded nil interface.
type shim struct {
	testing.TB
	name    string
	runner  Runner
	ctx     context.Context
	cancel  context.CancelFunc
	mu      sync.Mutex
	msg     strings.Builder
	failed  bool
	skipped string
	cleanup []func()
	dirs    int
}

func (t *shim) Helper()       {}
func (t *shim) Name() string  { return t.name }
func (t *shim) Failed() bool  { t.mu.Lock(); defer t.mu.Unlock(); return t.failed }
func (t *shim) Skipped() bool { return t.skipped != "" }
func (t *shim) Context() context.Context {
	return t.ctx
}

func (t *shim) Log(args ...any) { t.Logf("%s", fmt.Sprint(args...)) }
func (t *shim) Logf(format string, args ...any) {
	if t.runner.Verbose {
		fmt.Fprintf(os.Stderr, format+"\n", args...)
	}
}

func (t *shim) fail(msg string) {
	t.mu.Lock()
	t.failed = true
	if t.msg.Len() > 0 {
		t.msg.WriteString("; ")
	}
	t.msg.WriteString(msg)
	t.mu.Unlock()
}

func (t *shim) Fail()                             { t.fail("failed") }
func (t *shim) Error(args ...any)                 { t.fail(fmt.Sprint(args...)) }
func (t *shim) Errorf(format string, args ...any) { t.fail(fmt.Sprintf(format, args...)) }
func (t *shim) FailNow()                          { t.fail("failed"); runtime.Goexit() }
func (t *shim) Fatal(args ...any)                 { t.fail(fmt.Sprint(args...)); runtime.Goexit() }
func (t *shim) Fatalf(format string, args ...any) {
	t.fail(fmt.Sprintf(format, args...))
	runtime.Goexit()
}
func (t *shim) Skip(args ...any) { t.skipped = fmt.Sprint(args...); runtime.Goexit() }
func (t *shim) Skipf(format string, args ...any) {
	t.skipped = fmt.Sprintf(format, args...)
	runtime.Goexit()
}
func (t *shim) SkipNow() { t.skipped = "skipped"; runtime.Goexit() }

func (t *shim) Cleanup(f func()) {
	t.mu.Lock()
	t.cleanup = append(t.cleanup, f)
	t.mu.Unlock()
}

// TempDir makes a folder under the runner's scratch folder.
func (t *shim) TempDir() string {
	t.mu.Lock()
	t.dirs++
	n := t.dirs
	t.mu.Unlock()
	dir, err := os.MkdirTemp(t.runner.Scratch, fmt.Sprintf("%03d-", n))
	if err != nil {
		t.Fatalf("tempdir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// finish runs the cleanups, last first, and turns a panic into a failure.
func (t *shim) finish() {
	if p := recover(); p != nil {
		t.fail(fmt.Sprintf("panic: %v", p))
	}
	t.cancel()
	for i := len(t.cleanup) - 1; i >= 0; i-- {
		t.cleanup[i]()
	}
}
