// Package sockd owns the helper's only listener: a unix socket restricted to
// the current user by file mode and by a peer uid check on every connection.
// It also provides the single-instance lock and the client-side dial.
package sockd

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net"
	"os"
	"path/filepath"
	"sync"
	"syscall"
)

// ErrInUse is returned by Listen when another process already answers on
// the socket path.
var ErrInUse = errors.New("sockd: socket already in use")

// ErrLocked is returned by Lock when another holder has the lock.
var ErrLocked = errors.New("sockd: lock held by another process")

// umaskMu serializes the process-wide umask change around bind, so the
// socket file is created 0600 with no window where it is wider.
var umaskMu sync.Mutex

// Listen binds a unix socket at path. The parent directory is created (or
// tightened) to 0700 and the socket is 0600 from creation. An existing file
// at path is removed only if it is a socket nobody answers on. Accepted
// connections from a different uid are closed before Accept returns.
func Listen(path string) (net.Listener, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create socket dir: %w", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return nil, fmt.Errorf("chmod socket dir: %w", err)
	}
	if err := clearStale(path); err != nil {
		return nil, err
	}

	umaskMu.Lock()
	old := syscall.Umask(0o177)
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	syscall.Umask(old)
	umaskMu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("listen %s: %w", path, err)
	}
	return &listener{UnixListener: ln, uid: os.Getuid(), peerUID: connPeerUID}, nil
}

// clearStale removes a leftover socket from a process that died without
// cleaning up. A live socket or a non-socket file is left alone.
func clearStale(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("stat socket: %w", err)
	}
	if info.Mode().Type() != fs.ModeSocket {
		return fmt.Errorf("%s exists and is not a socket", path)
	}
	if c, err := net.Dial("unix", path); err == nil {
		c.Close()
		return ErrInUse
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove stale socket: %w", err)
	}
	return nil
}

type listener struct {
	*net.UnixListener
	uid int
	// peerUID is a field so tests can simulate a foreign peer.
	peerUID func(net.Conn) (int, error)
}

// Accept returns the next connection whose peer runs as this user. Others
// are closed and logged by uid only.
func (l *listener) Accept() (net.Conn, error) {
	for {
		c, err := l.UnixListener.Accept()
		if err != nil {
			return nil, err
		}
		uid, err := l.peerUID(c)
		if err != nil {
			log.Printf("sockd: dropping connection: peer uid: %v", err)
			c.Close()
			continue
		}
		if uid != l.uid {
			log.Printf("sockd: dropping connection from uid %d", uid)
			c.Close()
			continue
		}
		return c, nil
	}
}

func connPeerUID(c net.Conn) (int, error) {
	uc, ok := c.(*net.UnixConn)
	if !ok {
		return 0, fmt.Errorf("not a unix connection: %T", c)
	}
	raw, err := uc.SyscallConn()
	if err != nil {
		return 0, err
	}
	var uid int
	var credErr error
	if err := raw.Control(func(fd uintptr) { uid, credErr = fdPeerUID(int(fd)) }); err != nil {
		return 0, err
	}
	return uid, credErr
}

// Lock takes an exclusive flock on path, creating it 0600 if needed, so only
// one daemon runs per TOGO_HOME. The kernel drops the lock if the process
// dies. The file is left in place on release: removing it would let a third
// process lock a fresh inode while a second still holds the old one.
func Lock(path string) (release func(), err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create lock dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open lock: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrLocked
		}
		return nil, fmt.Errorf("flock: %w", err)
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
			f.Close()
		})
	}, nil
}

// Dial connects to the daemon socket at path.
func Dial(ctx context.Context, path string) (net.Conn, error) {
	var d net.Dialer
	c, err := d.DialContext(ctx, "unix", path)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", path, err)
	}
	return c, nil
}
