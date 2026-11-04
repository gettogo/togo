package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gettogo/togo/helper/internal/adapters/claude"
	"github.com/gettogo/togo/helper/internal/wire"
)

const (
	exitDenied      = 1
	exitTimeout     = 3
	askDefaultWait  = 300
	askReplySlack   = 10 * time.Second
	askDefaultYes   = "Yes"
	askDefaultNo    = "No"
	askDeniedAnswer = wire.DecisionDeny
)

func init() {
	register("ask", func(ctx context.Context, args []string, env map[string]string, stdin io.Reader, stdout, stderr io.Writer) int {
		return runAsk(ctx, args, env, stdin, stdout, stderr)
	})
	addUsage("  ask <question> [--options a,b] [--timeout S]  ask on your phone and wait for the answer")
}

// runAsk asks the user and prints the chosen answer. Exit codes: 0 for the
// first option (or any other option chosen), 1 when declined (or "No" with
// the default Yes/No options), 3 on timeout, 4 when the daemon is not
// reachable. Without --require-remote and with a terminal on stdin, an
// unreachable daemon falls back to asking here.
func runAsk(ctx context.Context, args []string, env map[string]string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := newFlagSet("ask")
	options := fs.String("options", "", "comma-separated answers, at most 9")
	timeout := fs.Int("timeout", askDefaultWait, "seconds to wait, 1-3600")
	remote := fs.Bool("require-remote", false, "never fall back to a prompt in this terminal")
	text := fs.Bool("text", false, "accept a typed answer (over SSH only)")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return usageError(stderr, err)
	}
	question := strings.TrimSpace(strings.Join(pos, " "))
	if question == "" {
		return usageError(stderr, errors.New("ask needs a question"))
	}
	if *timeout < 1 || *timeout > 3600 {
		return usageError(stderr, errors.New("--timeout must be 1-3600 seconds"))
	}
	opts := []string{askDefaultYes, askDefaultNo}
	defaults := true
	if *options != "" {
		defaults = false
		opts = nil
		for _, o := range strings.Split(*options, ",") {
			if o = strings.TrimSpace(o); o != "" {
				opts = append(opts, o)
			}
		}
		if len(opts) == 0 || len(opts) > wire.MaxAskOptions {
			return usageError(stderr, errors.New("--options takes 1 to 9 answers"))
		}
	}
	p := wire.AskParams{SessionID: claude.SessionID(env), Title: question, Options: opts, AllowText: *text, TimeoutSec: *timeout}
	sock, err := tapSocketPath(env)
	if err != nil {
		return fail(stderr, err)
	}
	callCtx, cancel := context.WithTimeout(ctx, time.Duration(*timeout)*time.Second+askReplySlack)
	defer cancel()
	var res wire.AskResult
	if err := localCall(callCtx, sock, wire.MethodLocalAsk, p, &res); err != nil {
		if errors.Is(err, errUnreachable) && !*remote && isTerminal(stdin) {
			return askHere(stdin, stdout, stderr, question, opts, defaults)
		}
		fmt.Fprintf(stderr, "togo: %v\n", err)
		if errors.Is(err, errUnreachable) {
			return exitUnreachable
		}
		return exitError
	}
	if res.Decision == nil {
		fmt.Fprintln(stderr, "togo: no answer before the timeout")
		return exitTimeout
	}
	return askExit(stdout, *res.Decision, opts, defaults)
}

func askExit(stdout io.Writer, answer string, opts []string, defaults bool) int {
	if answer == askDeniedAnswer || defaults && answer == askDefaultNo {
		fmt.Fprintln(stdout, askDefaultNo)
		return exitDenied
	}
	fmt.Fprintln(stdout, answer)
	return exitOK
}

// askHere prompts on the terminal: the answer's number or text.
func askHere(stdin io.Reader, stdout, stderr io.Writer, question string, opts []string, defaults bool) int {
	fmt.Fprintln(stderr, question)
	for i, o := range opts {
		fmt.Fprintf(stderr, "  %d. %s\n", i+1, o)
	}
	fmt.Fprint(stderr, "Answer: ")
	line, err := bufio.NewReader(stdin).ReadString('\n')
	if err != nil && line == "" {
		return exitTimeout
	}
	line = strings.TrimSpace(line)
	if n, err := strconv.Atoi(line); err == nil && n >= 1 && n <= len(opts) {
		return askExit(stdout, opts[n-1], opts, defaults)
	}
	for _, o := range opts {
		if strings.EqualFold(o, line) {
			return askExit(stdout, o, opts, defaults)
		}
	}
	return askExit(stdout, askDeniedAnswer, opts, defaults)
}

func isTerminal(r io.Reader) bool {
	f, ok := r.(*os.File)
	if !ok {
		return false
	}
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}
