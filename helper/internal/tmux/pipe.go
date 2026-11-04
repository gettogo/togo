package tmux

import (
	"context"
	"errors"
)

// PipePane starts piping the pane's output to the shell command cmd, in
// addition to the pane's normal display (-O). An empty cmd stops piping.
// target is passed as given, so callers use the exact-match form "=name:".
// This is the one call that hands tmux a shell string: pipe-pane has no argv
// form, so the caller must have built cmd with PipeCommand-style quoting.
func (c *Client) PipePane(ctx context.Context, target, cmd string) error {
	args := []string{"pipe-pane"}
	if cmd != "" {
		args = append(args, "-O")
	}
	args = append(args, "-t", target)
	if cmd != "" {
		args = append(args, cmd)
	}
	_, err := c.run(ctx, args...)
	if errors.Is(err, errNoServer) {
		return ErrNoSession
	}
	return err
}
