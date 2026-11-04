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
