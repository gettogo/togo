package tmux

import (
	"context"
	"errors"
)

// CapturePane returns the visible text of the pane at target without escape
// sequences. target is passed as given, so callers use the exact-match form
// "=name:".
func (c *Client) CapturePane(ctx context.Context, target string) (string, error) {
	out, err := c.run(ctx, "capture-pane", "-p", "-t", target)
	if errors.Is(err, errNoServer) {
		return "", ErrNoSession
	}
	return out, err
}
