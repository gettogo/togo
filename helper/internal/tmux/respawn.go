package tmux

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// RespawnPane kills the process in the target pane and starts argv in its
// place, in dir, keeping the pane, its session and the session's
// environment. argv is passed through tmux verbatim; it is never joined or
// shell-quoted. When no server is running the error is ErrNoSession.
func (c *Client) RespawnPane(ctx context.Context, target, dir string, argv []string) error {
	if len(argv) == 0 {
		return errors.New("tmux: respawn-pane needs a command")
	}
	args := []string{"respawn-pane", "-k"}
	if dir != "" {
		// -c is expanded as a format, like new-session's.
		args = append(args, "-c", escapeArg(strings.ReplaceAll(dir, "#", "##")))
	}
	args = append(args, "-t", target, "--")
	for _, a := range argv {
		args = append(args, escapeArg(a))
	}
	_, err := c.run(ctx, args...)
	if errors.Is(err, errNoServer) {
		return ErrNoSession
	}
	if err != nil {
		return fmt.Errorf("tmux: respawn pane: %w", err)
	}
	if dir != "" {
		// As after new-session, the pane's path can read as the tmux
		// server's own directory until the new process enters dir.
		c.waitPanePath(ctx, strings.TrimSuffix(strings.TrimPrefix(target, "="), ":"), dir)
	}
	return nil
}

// Proc is one process in a pane's foreground job.
type Proc struct {
	PID  int
	Name string // base name of the executable
}

// PaneForeground describes what runs in a pane: Dead when its process has
// exited (the pane is kept by remain-on-exit), else the pane's own process
// and the processes of the terminal's foreground process group.
type PaneForeground struct {
	Dead       bool
	PanePID    int
	Foreground []Proc
	// PaneAge is how long the pane's own process has run, so how long
	// since the pane was created or respawned; zero when ps did not say.
	PaneAge time.Duration
}

// psPaths are where ps lives on macOS and Linux.
var psPaths = []string{"/bin/ps", "/usr/bin/ps"}

// Foreground reports the foreground job of the session's active pane, read
// from tmux (pane_dead, pane_pid, pane_tty) and ps on the pane's terminal.
// A missing session or server is ErrNoSession.
func (c *Client) Foreground(ctx context.Context, session string) (PaneForeground, error) {
	out, err := c.run(ctx, "display-message", "-p", "-t", "="+escapeArg(session)+":", "#{pane_dead} #{pane_pid} #{pane_tty}")
	if errors.Is(err, errNoServer) {
		return PaneForeground{}, ErrNoSession
	}
	if err != nil {
		return PaneForeground{}, err
	}
	f := strings.Fields(out)
	if len(f) < 2 {
		return PaneForeground{}, fmt.Errorf("tmux: unexpected pane description %q", strings.TrimSpace(out))
	}
	if f[0] == "1" {
		return PaneForeground{Dead: true}, nil
	}
	pid, err := strconv.Atoi(f[1])
	if err != nil || pid < 1 || len(f) != 3 || !strings.HasPrefix(f[2], "/dev/") {
		return PaneForeground{}, fmt.Errorf("tmux: unexpected pane description %q", strings.TrimSpace(out))
	}
	ps := ""
	for _, p := range psPaths {
		if _, err := exec.LookPath(p); err == nil {
			ps = p
			break
		}
	}
	if ps == "" {
		return PaneForeground{}, errors.New("tmux: ps not found")
	}
	raw, err := exec.CommandContext(ctx, ps, "-t", f[2], "-o", "pid=,pgid=,tpgid=,etime=,comm=").Output()
	if err != nil {
		return PaneForeground{}, fmt.Errorf("ps: %w", err)
	}
	procs, age := parseForeground(string(raw), pid)
	return PaneForeground{PanePID: pid, Foreground: procs, PaneAge: age}, nil
}

// parseForeground keeps the processes whose group is their terminal's
// foreground group, from "pid pgid tpgid etime comm" lines, and returns
// the elapsed time of panePID.
func parseForeground(out string, panePID int) ([]Proc, time.Duration) {
	var procs []Proc
	var age time.Duration
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 5 {
			continue
		}
		pid, err1 := strconv.Atoi(f[0])
		pgid, err2 := strconv.Atoi(f[1])
		tpgid, err3 := strconv.Atoi(f[2])
		if err1 != nil || err2 != nil || err3 != nil {
			continue
		}
		if pid == panePID {
			age = parseEtime(f[3])
		}
		if pgid != tpgid {
			continue
		}
		name := strings.Join(f[4:], " ")
		procs = append(procs, Proc{PID: pid, Name: filepath.Base(strings.TrimPrefix(name, "-"))})
	}
	return procs, age
}

// parseEtime reads ps's elapsed time, [[dd-]hh:]mm:ss; zero when it is
// not in that form.
func parseEtime(v string) time.Duration {
	var days int
	if d, rest, ok := strings.Cut(v, "-"); ok {
		n, err := strconv.Atoi(d)
		if err != nil {
			return 0
		}
		days, v = n, rest
	}
	parts := strings.Split(v, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return 0
	}
	secs := 0
	for _, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return 0
		}
		secs = secs*60 + n
	}
	return time.Duration(days)*24*time.Hour + time.Duration(secs)*time.Second
}
