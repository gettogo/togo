package tmux

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var surfaceSession = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
var surfaceOwner = regexp.MustCompile(`^[0-9a-f]{64}$`)

// ClaimSurface sets discovery with an ownership token, preserving a marker
// that existed before the first helper renderer. Commands are tmux commands;
// if-shell -F evaluates a format and never starts an operating-system shell.
func (c *Client) ClaimSurface(ctx context.Context, session, owner string) error {
	if !surfaceSession.MatchString(session) || !surfaceOwner.MatchString(owner) {
		return errors.New("tmux: invalid surface owner")
	}
	target := "=" + session + ":"
	save := fmt.Sprintf("set-option -F -t %s @togo-surface-previous '#{@tern-tsp}'", target)
	_, err := c.run(ctx, "if-shell", "-F", "-t", target, "#{==:#{@togo-surface-owner},}", save,
		";", "set-option", "-t", target, "@togo-surface-owner", owner,
		";", "set-option", "-t", target, "@tern-tsp", "1")
	return noSession(err)
}

// ReleaseSurface is safe even after the original process exits or switches
// conversations. An old lease cannot clear a newer owner's discovery marker.
func (c *Client) ReleaseSurface(ctx context.Context, session, owner string) error {
	if !surfaceSession.MatchString(session) || !surfaceOwner.MatchString(owner) {
		return errors.New("tmux: invalid surface owner")
	}
	target := "=" + session + ":"
	restore := fmt.Sprintf("if-shell -F -t %s '#{==:#{@togo-surface-previous},}' 'set-option -u -t %s @tern-tsp' 'set-option -F -t %s @tern-tsp \"#{@togo-surface-previous}\"' ; set-option -u -t %s @togo-surface-owner ; set-option -u -t %s @togo-surface-previous", target, target, target, target, target)
	_, err := c.run(ctx, "if-shell", "-F", "-t", target, "#{==:#{@togo-surface-owner},"+owner+"}", restore)
	return noSession(err)
}

// ReleaseStaleSurfaces runs before the daemon admits a new renderer. Only
// markers carrying this helper's ownership token are restored.
func (c *Client) ReleaseStaleSurfaces(ctx context.Context) error {
	out, err := c.run(ctx, "list-sessions", "-F", "#{session_name}|#{@togo-surface-owner}")
	if errors.Is(err, errNoServer) {
		return nil
	}
	if err != nil {
		return err
	}
	var failures []error
	for line := range strings.SplitSeq(out, "\n") {
		name, owner, ok := strings.Cut(line, "|")
		if !ok || !surfaceSession.MatchString(name) || !surfaceOwner.MatchString(owner) {
			continue
		}
		if err := c.ReleaseSurface(ctx, name, owner); err != nil && !errors.Is(err, ErrNoSession) {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}
