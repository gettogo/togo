package tmux

import (
	"context"
	"errors"
	"strings"
)

// SendKeys types keys into the pane at target. With literal set, each
// argument is typed as text (-l) instead of being read as a key name such
// as Enter or Escape. "--" ends tmux's flags so text starting with "-" is
// typed, not parsed. A pane left in copy mode is taken out of it first:
// there tmux reads keys as navigation and the program never sees them.
func (c *Client) SendKeys(ctx context.Context, target string, literal bool, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	args := append(leaveMode(target), "send-keys", "-t", target)
	if literal {
		args = append(args, "-l")
	}
	args = append(args, "--")
	for _, k := range keys {
		// Typed words ending in ";" would end the command instead.
		args = append(args, escapeArg(k))
	}
	_, err := c.run(ctx, args...)
	if errors.Is(err, errNoServer) {
		return ErrNoSession
	}
	return err
}

// leaveMode is the start of a tmux command line that takes the pane at
// target out of copy mode, or does nothing when it is not in a mode. A
// scroll or drag from an attached client enters copy mode and nothing
// leaves it until a person does. The inner command names the target
// again, because without one tmux cancels the mode in its current pane.
func leaveMode(target string) []string {
	quoted := "'" + strings.ReplaceAll(target, "'", `'\''`) + "'"
	return []string{"if-shell", "-F", "-t", target, "#{pane_in_mode}", "send-keys -X -t " + quoted + " cancel", ";"}
}
