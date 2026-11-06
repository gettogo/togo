package main

import (
	"context"
	"io"
	"strconv"
	"time"

	"github.com/gettogo/togo/helper/internal/adapters/claude"
	"github.com/gettogo/togo/helper/internal/wire"
)

// sessionExitTimeout bounds the report, so a slow daemon never holds up
// the login shell the pane falls back to.
const sessionExitTimeout = 2 * time.Second

// session-exit is run by a session's launch wrapper with the agent's exit
// status. It has no usage line: people never run it.
func init() {
	register("session-exit", func(ctx context.Context, args []string, env map[string]string, _ io.Reader, _, _ io.Writer) int {
		runSessionExit(ctx, args, env)
		return exitOK
	})
}

// runSessionExit sends local.signal kind exit for the session in
// TOGO_SESSION. It prints nothing and never fails: the pane it runs in
// belongs to the user, and bad input or an absent daemon is dropped.
func runSessionExit(ctx context.Context, args []string, env map[string]string) {
	id := claude.SessionID(env)
	if id == nil || len(args) != 1 {
		return
	}
	code, ok := exitStatus(args[0])
	if !ok {
		return
	}
	sock, err := tapSocketPath(env)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, sessionExitTimeout)
	defer cancel()
	var res struct{}
	_ = localCall(ctx, sock, wire.MethodLocalSignal, wire.Signal{SessionID: *id, Kind: wire.SignalExit, Code: &code}, &res)
}

// exitStatus parses a shell exit status: 1-3 ASCII digits, 0-255.
func exitStatus(s string) (int, bool) {
	if len(s) < 1 || len(s) > 3 {
		return 0, false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	n, err := strconv.Atoi(s)
	if err != nil || n > 255 {
		return 0, false
	}
	return n, true
}
