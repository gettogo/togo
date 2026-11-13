package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/gettogo/togo/helper/internal/logfile"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/gettogo/togo/helper/internal/rpc"
	"github.com/gettogo/togo/helper/internal/sockd"
	"github.com/gettogo/togo/helper/internal/wire"
)

// StartWait is how long Connect waits for a daemon it started to bind.
const StartWait = 2 * time.Second

// probeTimeout bounds the hello a connecting serve sends first. A variable
// so tests can shorten it.
var probeTimeout = 2 * time.Second

// ErrUnresponsive means the socket accepted a connection but the daemon
// did not answer hello in time.
var ErrUnresponsive = errors.New("daemon did not answer hello")

const probeHello = `{"jsonrpc":"2.0","id":1,"method":"hello","params":{"protocol":1,"client":{"name":"togo-serve","version":"0"},"capabilities":[]}}`

// Connect dials the daemon socket. When nothing answers it calls start once
// and keeps dialing until wait elapses. A daemon that accepts but does not
// answer hello on a separate connection is reported as ErrUnresponsive
// rather than proxied to, since a bound socket queues connections even when
// nothing reads them.
func Connect(ctx context.Context, socket string, start func() error, wait time.Duration) (net.Conn, error) {
	c, _, err := ConnectVersion(ctx, socket, start, wait)
	return c, err
}

// ConnectVersion is Connect that also returns the version the daemon
// reported in its hello.
func ConnectVersion(ctx context.Context, socket string, start func() error, wait time.Duration) (net.Conn, string, error) {
	if c, err := sockd.Dial(ctx, socket); err == nil {
		return checked(ctx, socket, c)
	}
	if err := start(); err != nil {
		return nil, "", fmt.Errorf("start daemon: %w", err)
	}
	deadline := time.Now().Add(wait)
	for {
		c, err := sockd.Dial(ctx, socket)
		if err == nil {
			return checked(ctx, socket, c)
		}
		if time.Now().After(deadline) {
			return nil, "", fmt.Errorf("daemon did not come up within %v: %w", wait, err)
		}
		select {
		case <-ctx.Done():
			return nil, "", ctx.Err()
		case <-time.After(25 * time.Millisecond):
		}
	}
}

func checked(ctx context.Context, socket string, c net.Conn) (net.Conn, string, error) {
	version, err := probe(ctx, socket)
	if err != nil {
		c.Close()
		return nil, "", err
	}
	return c, version, nil
}

// probe greets the daemon on its own connection and returns the version
// it reports.
func probe(ctx context.Context, socket string) (string, error) {
	c, err := sockd.Dial(ctx, socket)
	if err != nil {
		return "", err
	}
	defer c.Close()
	if err := c.SetDeadline(time.Now().Add(probeTimeout)); err != nil {
		return "", err
	}
	if err := rpc.WriteFrame(c, []byte(probeHello)); err != nil {
		return "", fmt.Errorf("%w: %v", ErrUnresponsive, err)
	}
	payload, err := rpc.ReadFrame(c)
	if err != nil {
		return "", fmt.Errorf("%w within %v: %v", ErrUnresponsive, probeTimeout, err)
	}
	var reply struct {
		Result *struct {
			Helper struct {
				Version string `json:"version"`
			} `json:"helper"`
		} `json:"result"`
	}
	if err := json.Unmarshal(payload, &reply); err != nil || reply.Result == nil {
		return "", fmt.Errorf("%w: unexpected reply", ErrUnresponsive)
	}
	return reply.Result.Helper.Version, nil
}

// RequestShutdown sends local.shutdown to the daemon on socket over a
// connection of its own and returns the daemon's answer.
func RequestShutdown(ctx context.Context, socket string, p wire.ShutdownParams) (wire.ShutdownResult, error) {
	watch, err := BeginShutdown(ctx, socket, p)
	if err != nil {
		return wire.ShutdownResult{}, err
	}
	defer watch.Close()
	return watch.Result, nil
}

// Start launches `exe daemon` detached from the caller, in its own session
// and process group so a hangup or Ctrl-C aimed at the ssh session does not
// reach it, with stdout and stderr appended to $TOGO_HOME/log/daemon.log.
// The child inherits the environment, which carries TOGO_HOME and the tmux
// label.
func Start(exe, home string) (*os.Process, error) {
	dir := filepath.Join(home, "log")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("log directory: %w", err)
	}
	logFile, err := logfile.Open(filepath.Join(dir, "daemon.log"), logfile.MaxSize)
	if err != nil {
		return nil, fmt.Errorf("daemon log: %w", err)
	}
	defer logFile.Close()
	cmd := exec.Command(exe, "daemon")
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("exec daemon: %w", err)
	}
	// Reaping keeps a daemon that exits early (lost the lock race) from
	// lingering as a zombie under a long-lived serve.
	go cmd.Wait()
	return cmd.Process, nil
}

// Pump copies frames between the stdio pair and the daemon connection
// without parsing them, so notifications pass through as they arrive. It
// returns when the daemon side closes. End of stdin half-closes the
// connection so the daemon finishes its replies and then closes.
func Pump(ctx context.Context, conn net.Conn, stdin io.Reader, stdout io.Writer) error {
	defer conn.Close()
	stopOnCancel := context.AfterFunc(ctx, func() { conn.Close() })
	defer stopOnCancel()

	go func() {
		_, err := io.Copy(conn, stdin)
		if uc, ok := conn.(*net.UnixConn); ok && err == nil {
			uc.CloseWrite()
			return
		}
		conn.Close()
	}()
	_, err := io.Copy(stdout, conn)
	if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("proxy: %w", err)
	}
	return nil
}
