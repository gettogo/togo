package tmux

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// PasteText puts text into the pane at target as one bracketed paste. The
// text reaches tmux on the standard input of load-buffer, never as an
// argument or a key name, and goes into a buffer named for this call alone,
// so two calls at once never paste each other's text. paste-buffer -p
// wraps it in the bracketed paste markers when the program in the pane has
// asked for them, and -d deletes the buffer once pasted. A failed paste
// deletes the buffer too.
func (c *Client) PasteText(ctx context.Context, target, text string) error {
	name := bufferName()
	if _, err := c.runInput(ctx, text, "load-buffer", "-b", name, "-"); err != nil {
		return fmt.Errorf("tmux: load buffer: %w", noSession(err))
	}
	if _, err := c.run(ctx, "paste-buffer", "-p", "-d", "-b", name, "-t", target); err != nil {
		_, _ = c.run(context.WithoutCancel(ctx), "delete-buffer", "-b", name)
		return fmt.Errorf("tmux: paste: %w", noSession(err))
	}
	return nil
}

// PaneCommand returns the name of the program in the foreground of the
// active pane at target, as tmux reports it in pane_current_command.
// list-panes is used rather than display-message, which prints empty
// fields for a target that does not exist instead of failing.
func (c *Client) PaneCommand(ctx context.Context, target string) (string, error) {
	out, err := c.run(ctx, "list-panes", "-t", target, "-F", "#{pane_active}\x1f#{pane_current_command}")
	if err != nil {
		return "", noSession(err)
	}
	for line := range strings.SplitSeq(out, "\n") {
		if cmd, ok := strings.CutPrefix(line, "1\x1f"); ok {
			return cmd, nil
		}
	}
	return "", fmt.Errorf("tmux: no active pane in %s", target)
}

// runInput is run with input on the standard input of tmux.
func (c *Client) runInput(ctx context.Context, input string, args ...string) (string, error) {
	full := append([]string{"-u"}, c.Socket...)
	if c.Config != "" {
		full = append(full, "-f", c.Config)
	}
	cmd := exec.CommandContext(ctx, c.Bin, append(full, args...)...)
	var out, errBuf strings.Builder
	cmd.Stdin = strings.NewReader(input)
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		return out.String(), c.mapError(err, errBuf.String())
	}
	return out.String(), nil
}

// bufferName returns "togo-" and 16 random hex digits.
func bufferName() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b) // crypto/rand.Read never fails since Go 1.24.
	return "togo-" + hex.EncodeToString(b)
}

func noSession(err error) error {
	if errors.Is(err, errNoServer) {
		return ErrNoSession
	}
	return err
}

// PaneGeometry returns the size of the active pane at target and the number
// of clients attached to its session, as "WxH/clients". A change means a
// client attached, detached or resized, which makes the program redraw.
func (c *Client) PaneGeometry(ctx context.Context, target string) (string, error) {
	out, err := c.run(ctx, "list-panes", "-t", target, "-F", "#{pane_active}\x1f#{pane_width}x#{pane_height}/#{session_attached}")
	if err != nil {
		return "", noSession(err)
	}
	for line := range strings.SplitSeq(out, "\n") {
		if g, ok := strings.CutPrefix(line, "1\x1f"); ok {
			return g, nil
		}
	}
	return "", fmt.Errorf("tmux: no active pane in %s", target)
}
