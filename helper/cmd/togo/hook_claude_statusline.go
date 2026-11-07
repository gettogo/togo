package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os/exec"
	"sort"
	"time"

	"github.com/gettogo/togo/helper/internal/adapters/claude"
	"github.com/gettogo/togo/helper/internal/wire"
)

// statuslineThenWait bounds the user's own status line command. Claude
// Code redraws the status line often and cancels slow ones itself.
var statuslineThenWait = 10 * time.Second

// cutThen takes `--then <command>` out of the hook args.
func cutThen(args []string) (string, []string, bool) {
	for i, a := range args {
		if a == "--then" && i+1 < len(args) {
			rest := append(append([]string{}, args[:i]...), args[i+2:]...)
			return args[i+1], rest, true
		}
	}
	return "", args, false
}

// runStatuslineChain is `togo hook claude statusline --then '<command>'`:
// the user's own status line, chained so Togo also sees the payload. The
// payload goes to the daemon (as the plain status line hook would send it)
// and, unchanged, to the user's command on stdin; only that command's
// stdout is printed, byte for byte, and its exit code is returned. Either
// side failing leaves the other alone.
func runStatuslineChain(ctx context.Context, args []string, then string, env map[string]string, stdin io.Reader, stdout, stderr io.Writer) int {
	log := hookLogger(env)
	payload, err := readBounded(stdin, claude.MaxPayload, hookStdinWait)
	if err != nil {
		log("hook claude statusline: %v", err)
		payload = nil
	}
	reported := make(chan struct{})
	go func() {
		defer close(reported)
		reportStatusline(ctx, args, payload, env, log)
	}()
	code := runThen(ctx, then, payload, env, stdout, stderr)
	select {
	case <-reported:
	case <-time.After(hookCallTimeout):
	}
	return code
}

// reportStatusline sends the payload to the daemon the way the plain
// status line hook does, printing nothing.
func reportStatusline(ctx context.Context, args []string, payload []byte, env map[string]string, log func(string, ...any)) {
	if len(args) != 2 || args[0] != "claude" || args[1] != claude.Statusline || payload == nil {
		log("hook: --then only chains claude statusline")
		return
	}
	ev, _, err := claude.Map(claude.Statusline, payload, env)
	if err != nil || ev.SessionID == nil {
		return
	}
	sock, err := tapSocketPath(env)
	if err != nil {
		return
	}
	callCtx, cancel := context.WithTimeout(ctx, hookCallTimeout)
	defer cancel()
	if err := localCall(callCtx, sock, wire.MethodLocalHook, claude.Forward(ev, claude.Statusline, payload), nil); err != nil {
		log("hook claude statusline: %v", err)
	}
}

// runThen runs the user's command with sh -c, the same payload on stdin
// and the hook's environment, and copies its stdout and stderr through.
func runThen(ctx context.Context, then string, payload []byte, env map[string]string, stdout, stderr io.Writer) int {
	ctx, cancel := context.WithTimeout(ctx, statuslineThenWait)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", then)
	cmd.Stdin = bytes.NewReader(payload)
	cmd.Env = envList(env)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, stderr
	cmd.WaitDelay = time.Second
	err := cmd.Run()
	_, _ = stdout.Write(out.Bytes())
	var exit *exec.ExitError
	switch {
	case err == nil:
		return exitOK
	case errors.As(err, &exit) && exit.ExitCode() >= 0:
		return exit.ExitCode()
	}
	return 1
}

func envList(env map[string]string) []string {
	out := make([]string, 0, len(env))
	for k, v := range env {
		out = append(out, k+"="+v)
	}
	sort.Strings(out)
	return out
}
