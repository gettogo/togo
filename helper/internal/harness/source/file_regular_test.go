package source

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// A FIFO where a file source expects a file is refused at once instead of
// blocking the read.
func TestFIFORefused(t *testing.T) {
	p := filepath.Join(t.TempDir(), "pipe")
	if err := syscall.Mkfifo(p, 0o600); err != nil {
		t.Skip(err)
	}
	done := make(chan error, 1)
	go func() { _, err := readCapped(p); done <- err }()
	select {
	case err := <-done:
		if !errors.Is(err, errNotRegular) {
			t.Fatalf("err %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("read blocked on a FIFO")
	}
	f := filepath.Join(t.TempDir(), "f")
	os.WriteFile(f, []byte("{}"), 0o600)
	if _, err := readCapped(f); err != nil {
		t.Fatal(err)
	}
}
