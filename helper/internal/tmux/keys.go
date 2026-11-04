package tmux

import (
	"context"
	"errors"
)

// SendKeys types keys into the pane at target. With literal set, each
// argument is typed as text (-l) instead of being read as a key name such
// as Enter or Escape. "--" ends tmux's flags so text starting with "-" is
// typed, not parsed.
func (c *Client) SendKeys(ctx context.Context, target string, literal bool, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	args := []string{"send-keys", "-t", target}
	if literal {
		args = append(args, "-l")
	}
	args = append(args, "--")
	args = append(args, keys...)
	_, err := c.run(ctx, args...)
	if errors.Is(err, errNoServer) {
		return ErrNoSession
	}
	return err
}
