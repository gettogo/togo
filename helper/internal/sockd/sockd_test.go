package sockd

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// sockPath returns a socket path under a short temp dir, because macOS caps
// unix socket paths at 104 bytes and t.TempDir paths run longer.
func sockPath(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "tg")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return filepath.Join(dir, "run", "togo.sock")
}

func listen(t *testing.T, path string) net.Listener {
	t.Helper()
	ln, err := Listen(path)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })
	return ln
}

func TestListenSocketMode0600AndDir0700(t *testing.T) {
	path := sockPath(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	listen(t, path)

	dir, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if got := dir.Mode().Perm(); got != 0o700 {
		t.Errorf("dir mode = %o, want 700", got)
	}
	sock, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if sock.Mode().Type() != os.ModeSocket {
		t.Errorf("%s is not a socket", path)
	}
	if got := sock.Mode().Perm(); got != 0o600 {
		t.Errorf("socket mode = %o, want 600", got)
	}
}

func TestListenReplacesStaleSocket(t *testing.T) {
	path := sockPath(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	stale, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	stale.SetUnlinkOnClose(false)
	stale.Close()
	if _, err := os.Lstat(path); err != nil {
		t.Fatalf("stale socket missing before Listen: %v", err)
	}

	ln := listen(t, path)
	go func() {
		if c, err := ln.Accept(); err == nil {
			c.Close()
		}
	}()
	c, err := Dial(context.Background(), path)
	if err != nil {
		t.Fatalf("Dial after replacing stale socket: %v", err)
	}
	c.Close()
}

func TestListenRefusesLiveSocket(t *testing.T) {
	path := sockPath(t)
	listen(t, path)
	if _, err := Listen(path); !errors.Is(err, ErrInUse) {
		t.Fatalf("second Listen = %v, want ErrInUse", err)
	}
	if _, err := os.Lstat(path); err != nil {
		t.Fatalf("live socket removed: %v", err)
	}
}

func TestListenRefusesNonSocketFile(t *testing.T) {
	path := sockPath(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Listen(path); err == nil {
		t.Fatal("Listen replaced a regular file")
	}
	if b, err := os.ReadFile(path); err != nil || string(b) != "keep" {
		t.Fatalf("regular file changed: %q, %v", b, err)
	}
}

func TestAcceptDropsForeignUID(t *testing.T) {
	path := sockPath(t)
	ln := listen(t, path)
	var calls atomic.Int32
	ln.(*listener).peerUID = func(c net.Conn) (int, error) {
		if calls.Add(1) == 1 {
			return os.Getuid() + 1, nil
		}
		return connPeerUID(c)
	}
	accepted := make(chan net.Conn, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			t.Errorf("Accept: %v", err)
			close(accepted)
			return
		}
		accepted <- c
	}()

	foreign, err := Dial(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer foreign.Close()
	foreign.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := foreign.Read(make([]byte, 1)); !errors.Is(err, io.EOF) {
		t.Fatalf("foreign peer read = %v, want EOF from a dropped connection", err)
	}

	own, err := Dial(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer own.Close()
	select {
	case c := <-accepted:
		if c == nil {
			t.Fatal("no connection accepted")
		}
		c.Close()
	case <-time.After(5 * time.Second):
		t.Fatal("same-uid peer was not accepted")
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("peer uid checked %d times, want 2", got)
	}
}

func TestPeerUIDIsCurrentUser(t *testing.T) {
	path := sockPath(t)
	ln := listen(t, path)
	go func() {
		if c, err := Dial(context.Background(), path); err == nil {
			defer c.Close()
			c.Read(make([]byte, 1))
		}
	}()
	c, err := ln.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	uid, err := connPeerUID(c)
	if err != nil {
		t.Fatalf("connPeerUID: %v", err)
	}
	if uid != os.Getuid() {
		t.Fatalf("peer uid = %d, want %d", uid, os.Getuid())
	}
}

func TestLockSecondHolderFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run", "daemon.lock")
	release, err := Lock(path)
	if err != nil {
		t.Fatalf("first Lock: %v", err)
	}
	if _, err := Lock(path); !errors.Is(err, ErrLocked) {
		t.Fatalf("second Lock = %v, want ErrLocked", err)
	}
	release()
	release2, err := Lock(path)
	if err != nil {
		t.Fatalf("Lock after release: %v", err)
	}
	release2()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("lock file mode = %o, want 600", got)
	}
}
