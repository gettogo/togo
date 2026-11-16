package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"text/tabwriter"

	"github.com/gettogo/togo/helper/internal/agents"
	"github.com/gettogo/togo/helper/internal/session"
)

const nestedTmuxNote = "togo: you are inside tmux, so this nests tmux in your current session"

// parseArgs parses flags and returns the positional arguments. Unlike
// flag.Parse it accepts flags after positionals, so both
// "togo new codex --detach" and "togo new --detach codex" work. A "--"
// ends flag parsing: everything after it is positional.
func parseArgs(fs *flag.FlagSet, args []string) ([]string, error) {
	return parseArgsMode(fs, args, false)
}

// parseTextArgs is parseArgs for commands whose positionals are free text
// ("togo notify '-3 tests failed'"): a word that starts with "-" but names
// no flag is text, not a usage error.
func parseTextArgs(fs *flag.FlagSet, args []string) ([]string, error) {
	return parseArgsMode(fs, args, true)
}

const undefinedFlag = "flag provided but not defined: -"

func parseArgsMode(fs *flag.FlagSet, args []string, text bool) ([]string, error) {
	var positional []string
	for {
		err := fs.Parse(args)
		if err != nil {
			name, ok := strings.CutPrefix(err.Error(), undefinedFlag)
			if !text || !ok {
				return nil, err
			}
			i := slices.IndexFunc(args, func(a string) bool {
				a = strings.TrimLeft(a, "-")
				a, _, _ = strings.Cut(a, "=")
				return a == name
			})
			if i < 0 {
				return nil, err
			}
			positional = append(positional, args[i])
			args = args[i+1:]
			continue
		}
		rest := fs.Args()
		if n := len(args) - len(rest); n > 0 && args[n-1] == "--" && !(n > 1 && takesValue(fs, args[n-2])) {
			return append(positional, rest...), nil
		}
		args = rest
		if len(args) == 0 {
			return positional, nil
		}
		positional = append(positional, args[0])
		args = args[1:]
	}
}

// takesValue reports whether tok is a defined non-boolean flag written
// without "=", so the word after it is its value.
func takesValue(fs *flag.FlagSet, tok string) bool {
	name, ok := strings.CutPrefix(strings.TrimPrefix(tok, "-"), "-")
	if !ok {
		name = strings.TrimPrefix(tok, "-")
	}
	if tok == name || strings.Contains(name, "=") {
		return false
	}
	f := fs.Lookup(name)
	if f == nil {
		return false
	}
	b, isBool := f.Value.(interface{ IsBoolFlag() bool })
	return !(isBool && b.IsBoolFlag())
}

func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return fs
}

func usageError(stderr io.Writer, err error) int {
	if errors.Is(err, flag.ErrHelp) {
		fmt.Fprint(stderr, usage)
		return exitOK
	}
	fmt.Fprintf(stderr, "togo: %s\n\n%s", err, usage)
	return exitUsage
}

func runNew(ctx context.Context, args []string, env map[string]string, stdout, stderr io.Writer) int {
	fs := newFlagSet("new")
	dir := fs.String("dir", "", "directory to start in (default: current directory)")
	name := fs.String("name", "", "session name (default: the agent's name)")
	detach := fs.Bool("detach", false, "start the session without attaching")
	positional, err := parseArgs(fs, args)
	if err != nil {
		return usageError(stderr, err)
	}
	if len(positional) > 1 {
		return usageError(stderr, fmt.Errorf("new takes at most one agent, got %d arguments", len(positional)))
	}
	var agent string
	if len(positional) == 1 {
		agent = positional[0]
	}
	cwd, err := absDir(*dir)
	if err != nil {
		return fail(stderr, err)
	}
	svc, err := newService(ctx)
	if err != nil {
		return fail(stderr, err)
	}
	sess, err := svc.Create(ctx, session.CreateOpts{Agent: agent, Cwd: cwd, Name: *name})
	if err != nil {
		return fail(stderr, err)
	}
	fmt.Fprintf(stdout, "Started %s %s in %s\n", *sess.Bib, sess.Name, tildePath(cwd))
	if *detach {
		return exitOK
	}
	return attach(ctx, svc, sess.ID, env, stderr)
}

// absDir returns dir as a cleaned absolute path, expanding a leading "~" and
// resolving relative paths against the process working directory, which is
// also the default.
func absDir(dir string) (string, error) {
	if dir == "" {
		return os.Getwd()
	}
	if dir == "~" || strings.HasPrefix(dir, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = home + dir[1:]
	}
	return filepath.Abs(dir)
}

func runLs(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("ls")
	positional, err := parseArgs(fs, args)
	if err != nil {
		return usageError(stderr, err)
	}
	if len(positional) > 0 {
		return usageError(stderr, fmt.Errorf("ls takes no arguments"))
	}
	svc, err := newService(ctx)
	if err != nil {
		return fail(stderr, err)
	}
	sessions, err := svc.List(ctx)
	if err != nil {
		return fail(stderr, err)
	}
	if len(sessions) == 0 {
		fmt.Fprintln(stdout, "No sessions. Start one with: togo new")
		return exitOK
	}
	w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "#\tAGENT\tNAME\tSTATUS\tPATH")
	for _, s := range sessions {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", bibOrDash(s), agentName(s.Agent), s.Name, s.Status, tildePath(s.Cwd))
	}
	if err := w.Flush(); err != nil {
		return fail(stderr, err)
	}
	return exitOK
}

func bibOrDash(s session.Session) string {
	if s.Bib == nil {
		return "-"
	}
	return *s.Bib
}

func agentName(id string) string {
	if a, ok := agents.Lookup(id); ok {
		return a.Name
	}
	return id
}

// tildePath shows the home directory as "~", so rows stay short and do not
// leak the user name into screenshots.
func tildePath(p string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return p
	}
	if p == home {
		return "~"
	}
	if rest, ok := strings.CutPrefix(p, home+string(filepath.Separator)); ok {
		return "~/" + rest
	}
	return p
}

func runAttach(ctx context.Context, args []string, env map[string]string, stderr io.Writer) int {
	fs := newFlagSet("attach")
	positional, err := parseArgs(fs, args)
	if err != nil {
		return usageError(stderr, err)
	}
	if len(positional) != 1 {
		return usageError(stderr, fmt.Errorf("attach takes one session number or id"))
	}
	svc, err := newService(ctx)
	if err != nil {
		return fail(stderr, err)
	}
	sess, err := svc.Resolve(ctx, positional[0])
	if err != nil {
		return fail(stderr, err)
	}
	return attach(ctx, svc, sess.ID, env, stderr)
}

// attach replaces this process with tmux. It returns only on failure.
func attach(ctx context.Context, svc *session.Service, id string, env map[string]string, stderr io.Writer) int {
	argv, childEnv, err := attachArgs(ctx, svc, id, env)
	if err != nil {
		return fail(stderr, err)
	}
	if env["TMUX"] != "" {
		fmt.Fprintln(stderr, nestedTmuxNote)
	}
	bin, err := exec.LookPath(argv[0])
	if err != nil {
		return fail(stderr, err)
	}
	if err := syscall.Exec(bin, argv, childEnv); err != nil {
		return fail(stderr, fmt.Errorf("exec %s: %w", bin, err))
	}
	return exitOK
}

// attachArgs returns the argv and environment for attaching to the session
// named by bib or id. TMUX is dropped from the environment because tmux
// refuses to attach from inside a session while it is set.
func attachArgs(ctx context.Context, svc *session.Service, bibOrID string, env map[string]string) (argv, childEnv []string, err error) {
	sess, err := svc.Resolve(ctx, bibOrID)
	if err != nil {
		return nil, nil, err
	}
	argv, err = svc.AttachCommand(ctx, sess.ID)
	if err != nil {
		return nil, nil, err
	}
	for _, k := range slices.Sorted(maps.Keys(env)) {
		if k != "TMUX" {
			childEnv = append(childEnv, k+"="+env[k])
		}
	}
	return argv, childEnv, nil
}
