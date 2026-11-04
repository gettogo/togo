package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/gettogo/togo/helper/internal/wire"
)

func init() {
	register("job", func(ctx context.Context, args []string, env map[string]string, _ io.Reader, stdout, stderr io.Writer) int {
		return runJob(ctx, args, env, stdout, stderr)
	})
	addUsage("  job start <name> | update <id> | end <id>     report a long task to your phone")
}

// runJob reports a long-running task:
//
//	togo job start <name> [--progress N] [--message M]   prints the job id
//	togo job update <id> [--progress N] [--message M]
//	togo job end <id> --status ok|failed [--message M]
func runJob(ctx context.Context, args []string, env map[string]string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		return usageError(stderr, errors.New("job needs start, update or end"))
	}
	op := args[0]
	fs := newFlagSet("job " + op)
	progress := fs.Int("progress", -1, "percent done, 0-100")
	message := fs.String("message", "", "short status message")
	status := fs.String("status", "", "ok or failed (end only)")
	pos, err := parseArgs(fs, args[1:])
	if err != nil {
		return usageError(stderr, err)
	}
	p := wire.JobParams{Op: op}
	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
	if set["progress"] {
		if *progress < 0 || *progress > 100 {
			return usageError(stderr, errors.New("--progress must be 0-100"))
		}
		p.Progress = progress
	}
	if set["message"] {
		p.Message = message
	}
	switch op {
	case wire.JobStart:
		p.Name = strings.TrimSpace(strings.Join(pos, " "))
		if p.Name == "" {
			return usageError(stderr, errors.New("job start needs a name"))
		}
	case wire.JobUpdate, wire.JobEnd:
		if len(pos) != 1 {
			return usageError(stderr, fmt.Errorf("job %s needs one job id", op))
		}
		p.JobID = pos[0]
		if op == wire.JobEnd {
			if *status != wire.JobOK && *status != wire.JobFailed {
				return usageError(stderr, errors.New("job end needs --status ok or --status failed"))
			}
			p.Status = *status
		}
	default:
		return usageError(stderr, fmt.Errorf("unknown job operation %q", op))
	}
	sock, err := tapSocketPath(env)
	if err != nil {
		return fail(stderr, err)
	}
	ctx, cancel := context.WithTimeout(ctx, localTimeout)
	defer cancel()
	var res wire.JobResult
	if err := localCall(ctx, sock, wire.MethodLocalJob, p, &res); err != nil {
		var de *daemonError
		if errors.As(err, &de) && de.Code == -32001 {
			return fail(stderr, fmt.Errorf("unknown job %s", p.JobID))
		}
		fmt.Fprintf(stderr, "togo: job not reported: %v\n", err)
		return exitUnreachable
	}
	if op == wire.JobStart {
		fmt.Fprintln(stdout, res.JobID)
	}
	return exitOK
}
