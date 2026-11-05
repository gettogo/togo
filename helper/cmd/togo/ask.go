package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/gettogo/togo/helper/internal/adapters/claude"
	"github.com/gettogo/togo/helper/internal/approval"
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
	addUsage("      [--approve L] [--deny L]                  the options a notification's Approve and Deny pick;\n" +
		"                                                by default Approve/Yes/Allow/Accept/Continue and\n" +
		"                                                Deny/No/Reject/Cancel/Stop. Prints the answer; exits 1\n" +
		"                                                for the deny option or a decline, 3 on timeout")
}

// runAsk asks the user and prints the chosen answer. Exit codes: 0 for any
// option but the deny one, 1 for the deny option (by --deny or inferred, so
// "No" of the default Yes/No) or a decline, 3 on timeout, 4 when the daemon is not
// reachable. Without --require-remote and with a terminal on stdin, an
// unreachable daemon falls back to asking here.
func runAsk(ctx context.Context, args []string, env map[string]string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := newFlagSet("ask")
	options := fs.String("options", "", "comma-separated answers, at most 9")
	timeout := fs.Int("timeout", askDefaultWait, "seconds to wait, 1-3600")
	remote := fs.Bool("require-remote", false, "never fall back to a prompt in this terminal")
	text := fs.Bool("text", false, "accept a typed answer (over SSH only)")
	approve := fs.String("approve", "", "the option a notification's Approve picks (default: inferred from labels)")
	denyFlag := fs.String("deny", "", "the option a notification's Deny picks (default: inferred from labels)")
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
	if *options != "" {
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
	if err := checkRoleLabels(opts, *approve, *denyFlag); err != nil {
		return usageError(stderr, err)
	}
	deny := denyLabel(opts, *approve, *denyFlag)
	p := wire.AskParams{
		SessionID: claude.SessionID(env), Title: question, Options: opts, AllowText: *text, TimeoutSec: *timeout,
		Approve: strings.TrimSpace(*approve), Deny: strings.TrimSpace(*denyFlag),
	}
	sock, err := tapSocketPath(env)
	if err != nil {
		return fail(stderr, err)
	}
	callCtx, cancel := context.WithTimeout(ctx, time.Duration(*timeout)*time.Second+askReplySlack)
	defer cancel()
	var res wire.AskResult
	if err := localCall(callCtx, sock, wire.MethodLocalAsk, p, &res); err != nil {
		if errors.Is(err, errUnreachable) && !*remote && isTerminal(stdin) {
			return askHere(stdin, stdout, stderr, question, opts, deny)
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
	return askExit(stdout, *res.Decision, deny)
}

// askExit prints answer and maps it to the exit status: 1 for the deny
// option (deny, which may be empty) or a decline with no deny option, else 0.
// A decline prints "No".
func askExit(stdout io.Writer, answer, deny string) int {
	if answer == askDeniedAnswer && !strings.EqualFold(deny, askDeniedAnswer) {
		fmt.Fprintln(stdout, askDefaultNo)
		return exitDenied
	}
	fmt.Fprintln(stdout, answer)
	if deny != "" && answer == deny {
		return exitDenied
	}
	return exitOK
}

// denyLabel is the option a notification's Deny picks, as the helper
// assigns it: --deny when given, else inferred from the labels; empty
// when no option takes the role.
func denyLabel(opts []string, approve, deny string) string {
	for i, role := range approval.AskRoles(opts, strings.TrimSpace(approve), strings.TrimSpace(deny)) {
		if role == wire.RoleDeny {
			return opts[i]
		}
	}
	return ""
}

// askHere prompts on the terminal: the answer's number or text.
func askHere(stdin io.Reader, stdout, stderr io.Writer, question string, opts []string, deny string) int {
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
		return askExit(stdout, opts[n-1], deny)
	}
	for _, o := range opts {
		if strings.EqualFold(o, line) {
			return askExit(stdout, o, deny)
		}
	}
	return askExit(stdout, askDeniedAnswer, deny)
}

func isTerminal(r io.Reader) bool {
	f, ok := r.(*os.File)
	if !ok {
		return false
	}
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

// checkRoleLabels makes sure --approve and --deny each name one of opts
// (case-insensitive) and not the same one.
func checkRoleLabels(opts []string, approve, deny string) error {
	approve, deny = strings.TrimSpace(approve), strings.TrimSpace(deny)
	for _, f := range []struct{ flag, label string }{{"--approve", approve}, {"--deny", deny}} {
		if f.label != "" && !slices.ContainsFunc(opts, func(o string) bool { return strings.EqualFold(o, f.label) }) {
			return fmt.Errorf("%s must name one of the options", f.flag)
		}
	}
	if approve != "" && strings.EqualFold(approve, deny) {
		return errors.New("--approve and --deny must name different options")
	}
	return nil
}
