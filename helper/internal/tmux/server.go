package tmux

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ServerStarted returns when the server on the client's socket started.
// ok is false, with a nil error, when no server is running. It never starts
// a server: display-message needs one to answer.
func (c *Client) ServerStarted(ctx context.Context) (started time.Time, ok bool, err error) {
	out, err := c.run(ctx, "display-message", "-p", "#{start_time}")
	if errors.Is(err, errNoServer) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, err
	}
	secs, err := strconv.ParseInt(strings.TrimSpace(out), 10, 64)
	if err != nil || secs <= 0 {
		return time.Time{}, false, fmt.Errorf("tmux: server start time %q is not a Unix time", strings.TrimSpace(out))
	}
	return time.Unix(secs, 0), true, nil
}

// SetServerOption sets a server option (set-option -s) on the server
// already running on the client's socket. With no server running it does
// nothing and returns nil: it never starts one.
func (c *Client) SetServerOption(ctx context.Context, key, value string) error {
	if err := checkOption(key, value); err != nil {
		return err
	}
	_, err := c.run(ctx, "set-option", "-s", escapeArg(key), escapeArg(value))
	if errors.Is(err, errNoServer) {
		return nil
	}
	return err
}

// RootBinding is one root-table key binding. An empty Cmd unbinds Key.
type RootBinding struct {
	Key string
	// Cmd is one tmux command in its text form.
	Cmd string
}

// BindRoot sets the root-table bindings (bind-key -n, or unbind-key -n for
// an empty Cmd) on the server already running on the client's socket, in
// one tmux invocation. With no server running it does nothing and returns
// nil.
func (c *Client) BindRoot(ctx context.Context, bindings []RootBinding) error {
	var args []string
	for _, b := range bindings {
		if len(args) > 0 {
			args = append(args, ";")
		}
		if b.Cmd == "" {
			args = append(args, "unbind-key", "-n", escapeArg(b.Key))
		} else {
			args = append(args, "bind-key", "-n", escapeArg(b.Key), escapeArg(b.Cmd))
		}
	}
	if len(args) == 0 {
		return nil
	}
	_, err := c.run(ctx, args...)
	if errors.Is(err, errNoServer) {
		return nil
	}
	return err
}
