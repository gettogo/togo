package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/gettogo/togo/helper/internal/adapters/claude"
	"github.com/gettogo/togo/helper/internal/wire"
)

const (
	// exitUnreachable is what notify --strict, ask and job return when the
	// message did not reach the daemon or the user.
	exitUnreachable = 4
	localTimeout    = 5 * time.Second
)

func init() {
	register("notify", func(ctx context.Context, args []string, env map[string]string, _ io.Reader, _, stderr io.Writer) int {
		return runNotify(ctx, args, env, stderr)
	})
	addUsage("  notify <message> [--title T] [--strict]       send a notification to your phone")
}

// runNotify sends a notification for the current session (TOGO_SESSION) or
// for the host. Without --strict a missing daemon is reported but is not a
// failure, so scripts that notify as a courtesy keep going.
func runNotify(ctx context.Context, args []string, env map[string]string, stderr io.Writer) int {
	fs := newFlagSet("notify")
	title := fs.String("title", "", "notification title")
	strict := fs.Bool("strict", false, "exit 4 when the notification is not delivered")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return usageError(stderr, err)
	}
	msg := strings.TrimSpace(strings.Join(pos, " "))
	if msg == "" && strings.TrimSpace(*title) == "" {
		return usageError(stderr, errors.New("notify needs a message"))
	}
	p := wire.NotifyParams{SessionID: claude.SessionID(env), Title: msg, Strict: *strict}
	if strings.TrimSpace(*title) != "" {
		p.Title, p.Body = *title, msg
	}
	sock, err := tapSocketPath(env)
	if err != nil {
		return fail(stderr, err)
	}
	ctx, cancel := context.WithTimeout(ctx, localTimeout)
	defer cancel()
	var res wire.NotifyResult
	if err := localCall(ctx, sock, wire.MethodLocalNotify, p, &res); err != nil {
		fmt.Fprintf(stderr, "togo: notification not sent: %v\n", err)
		if *strict {
			return exitUnreachable
		}
		return exitOK
	}
	if *strict && !res.Delivered {
		fmt.Fprintln(stderr, "togo: notification not delivered: no phone is listening")
		return exitUnreachable
	}
	return exitOK
}
