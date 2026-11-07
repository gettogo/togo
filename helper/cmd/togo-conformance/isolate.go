package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/gettogo/togo/helper/internal/daemon"
)

// sandbox is one harness's private helper: its own TOGO_HOME (so its own
// daemon, built from this tree, with no paired devices and so no pushes)
// and its own tmux server label, so a harness's sessions never mix with
// the owner's and killing the server (the restore scenario) touches only it.
type sandbox struct {
	harness string
	home    string // TOGO_HOME
	work    string // the session's folder
	label   string // tmux -L label
}

func newSandbox(root, runID, harness string) (*sandbox, error) {
	sb := &sandbox{
		harness: harness,
		home:    filepath.Join(root, "homes", harness),
		work:    filepath.Join(root, runID+"-w-"+harness),
		label:   labelPrefix(runID) + harness,
	}
	for _, d := range []string{sb.home, sb.work} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return nil, err
		}
	}
	return sb, nil
}

func labelPrefix(runID string) string { return "tc-" + runID + "-" }

// env is the environment for `togo serve --stdio` and the daemon it starts.
func (sb *sandbox) env() []string {
	var out []string
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "TOGO_HOME=") || strings.HasPrefix(kv, "TOGO_TMUX_LABEL=") || strings.HasPrefix(kv, "TMUX=") {
			continue
		}
		out = append(out, kv)
	}
	return append(out, "TOGO_HOME="+sb.home, "TOGO_TMUX_LABEL="+sb.label)
}

// tmux runs tmux against the sandbox's server.
func (sb *sandbox) tmux(ctx context.Context, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, "tmux", append([]string{"-L", sb.label}, args...)...).CombinedOutput()
	return string(out), err
}

func (sb *sandbox) killServer() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _ = sb.tmux(ctx, "kill-server")
}

// stopDaemon ends the sandbox daemon: SIGTERM to its group, then SIGKILL.
func stopDaemon(home string) {
	raw, err := os.ReadFile(daemon.PIDPath(home))
	if err != nil {
		return
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil || pid <= 1 {
		return
	}
	for _, sig := range []syscall.Signal{syscall.SIGTERM, syscall.SIGKILL} {
		if pgid, err := syscall.Getpgid(pid); err == nil && pgid == pid {
			_ = syscall.Kill(-pid, sig)
		} else {
			_ = syscall.Kill(pid, sig)
		}
		for i := 0; i < 50; i++ {
			if syscall.Kill(pid, 0) != nil {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
}

// teardown kills every tmux server and daemon this run started. It is
// safe to call more than once and from -cleanup after a crash.
func teardown(root, runID string) {
	homes, _ := filepath.Glob(filepath.Join(root, "homes", "*"))
	for _, h := range homes {
		sb := &sandbox{label: labelPrefix(runID) + filepath.Base(h)}
		sb.killServer()
		stopDaemon(h)
	}
	// Servers whose home folder is gone (or never made) still have sockets.
	socks, _ := filepath.Glob(filepath.Join(os.TempDir(), "tmux-"+strconv.Itoa(os.Getuid()), labelPrefix(runID)+"*"))
	socks2, _ := filepath.Glob(filepath.Join("/private/tmp", "tmux-"+strconv.Itoa(os.Getuid()), labelPrefix(runID)+"*"))
	for _, s := range append(socks, socks2...) {
		sb := &sandbox{label: filepath.Base(s)}
		sb.killServer()
		_ = os.Remove(s)
	}
}
