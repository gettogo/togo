// Command togo is the host helper: the desk-side commands for the sessions
// the phone sees, and the stdio server the phone connects through.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/gettogo/togo/helper/internal/session"
	"github.com/gettogo/togo/helper/internal/state"
	"github.com/gettogo/togo/helper/internal/version"
)

const usage = `Usage: togo <command> [arguments]

Commands:
  new [agent] [--dir D] [--name N] [--detach]   start a session and attach to it
  ls                                            list sessions
  attach <number|id>                            attach to a session
  serve --stdio [--log]                         serve the phone over stdin and stdout
  version                                       print the version
`

const (
	exitOK    = 0
	exitError = 1
	exitUsage = 2
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := runContext(ctx, os.Args[1:], environ(os.Environ()), os.Stdin, os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

func run(args []string, env map[string]string, stdout, stderr io.Writer) int {
	return runContext(context.Background(), args, env, strings.NewReader(""), stdout, stderr)
}

func runContext(ctx context.Context, args []string, env map[string]string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return exitUsage
	}
	rest := args[1:]
	switch args[0] {
	case "new":
		return runNew(ctx, rest, env, stdout, stderr)
	case "ls":
		return runLs(ctx, rest, stdout, stderr)
	case "attach":
		return runAttach(ctx, rest, env, stderr)
	case "serve":
		return runServe(ctx, rest, stdin, stdout, stderr)
	case "version":
		fmt.Fprintf(stdout, "togo %s\n", version.String())
		return exitOK
	default:
		fmt.Fprintf(stderr, "togo: unknown command %q\n\n%s", args[0], usage)
		return exitUsage
	}
}

// newService builds the production session service. The socket label and
// the state directory come from the environment, which tests point at
// private values. It is a variable so tests can model a host without tmux,
// which the service would otherwise find in its fixed install locations.
var newService = func(ctx context.Context) (*session.Service, error) {
	home, err := state.Home()
	if err != nil {
		return nil, err
	}
	return session.NewService(ctx, home)
}

func environ(kvs []string) map[string]string {
	env := make(map[string]string, len(kvs))
	for _, kv := range kvs {
		if k, v, ok := strings.Cut(kv, "="); ok {
			env[k] = v
		}
	}
	return env
}

func fail(stderr io.Writer, err error) int {
	fmt.Fprintf(stderr, "togo: %s\n", strings.ReplaceAll(err.Error(), "\n", " "))
	return exitError
}
