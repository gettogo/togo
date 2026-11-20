package tmux

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// PanePIDs returns the process id of the first process in every pane of
// every window of the named session.
func (c *Client) PanePIDs(ctx context.Context, session string) ([]int, error) {
	out, err := c.run(ctx, "list-panes", "-s", "-t", "="+escapeArg(session)+":", "-F", "#{pane_pid}")
	if err != nil {
		return nil, err
	}
	var pids []int
	for _, line := range strings.Fields(out) {
		pid, err := strconv.Atoi(line)
		if err != nil || pid < 1 {
			return nil, fmt.Errorf("tmux list-panes: bad pane pid %q", line)
		}
		pids = append(pids, pid)
	}
	return pids, nil
}

// PipedSessions reports, for every session on the server, whether any of
// its panes is piping output (pipe-pane). A tap that exits leaves its pane
// unpiped, which is how the watcher learns to attach it again. No server
// means no sessions.
func (c *Client) PipedSessions(ctx context.Context) (map[string]bool, error) {
	out, err := c.run(ctx, "list-panes", "-a", "-F", "#{session_name} #{pane_pipe}")
	if errors.Is(err, errNoServer) {
		return map[string]bool{}, nil
	}
	if err != nil {
		return nil, err
	}
	piped := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		name, flag, ok := strings.Cut(strings.TrimSpace(line), " ")
		if !ok {
			continue
		}
		piped[name] = piped[name] || flag == "1"
	}
	return piped, nil
}

// TapOption is the session option that records the pipe-pane command the
// helper armed, since tmux does not report a pipe's command.
const TapOption = "@togo_tap"

// TapCommands returns, for every session on the server, the command
// recorded in TapOption, or "" when none is. No server means no sessions.
func (c *Client) TapCommands(ctx context.Context) (map[string]string, error) {
	out, err := c.run(ctx, "list-sessions", "-F", "#{session_name}"+fieldSep+"#{"+TapOption+"}"+recSep)
	if errors.Is(err, errNoServer) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	cmds := map[string]string{}
	for _, record := range strings.Split(out, recSep) {
		record = strings.TrimPrefix(record, "\n")
		name, cmd, ok := strings.Cut(record, fieldSep)
		if !ok || name == "" {
			continue
		}
		cmds[name] = cmd
	}
	return cmds, nil
}

// Pane is the active pane of a session's current window: the one
// respawn-pane restarts the agent in.
type Pane struct {
	TTY string // its terminal device, such as /dev/ttys011
	PID int    // the process tmux started in it
	// StartCommand is the command it was started or respawned with, as
	// tmux prints it.
	StartCommand string
}

// AgentPane returns the active pane of the session's current window, the
// pane RespawnPane targets. list-panes is used rather than display-message,
// which prints empty fields for a target that does not exist.
func (c *Client) AgentPane(ctx context.Context, session string) (Pane, error) {
	out, err := c.run(ctx, "list-panes", "-t", "="+escapeArg(session)+":", "-F",
		"#{pane_active}"+fieldSep+"#{pane_tty}"+fieldSep+"#{pane_pid}"+fieldSep+"#{pane_start_command}"+recSep)
	if err != nil {
		return Pane{}, noSession(err)
	}
	for _, rec := range strings.Split(out, recSep) {
		f := strings.SplitN(strings.TrimPrefix(rec, "\n"), fieldSep, 4)
		if len(f) != 4 || f[0] != "1" || !strings.HasPrefix(f[1], "/dev/") {
			continue
		}
		pid, _ := strconv.Atoi(f[2])
		return Pane{TTY: f[1], PID: pid, StartCommand: f[3]}, nil
	}
	return Pane{}, nil
}
