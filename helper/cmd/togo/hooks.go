package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/gettogo/togo/helper/internal/agents"
	"github.com/gettogo/togo/helper/internal/hooks"
	"github.com/gettogo/togo/helper/internal/state"
)

func init() {
	register("hooks", func(_ context.Context, args []string, env map[string]string, _ io.Reader, stdout, stderr io.Writer) int {
		return runHooks(args, env, stdout, stderr)
	})
	addUsage("  hooks install|uninstall|status [--agent <id>|all] [--local] [--block] [--no-statusline] [--json]\n" +
		"                                                set up every installed harness that takes hooks (Claude Code,\n" +
		"                                                Codex, Gemini CLI, Auggie, Qwen Code, Droid, Copilot CLI) and\n" +
		"                                                chain Claude's status line. Each file is backed up once\n" +
		"                                                (<file>.togo-backup) before its first change; install is safe\n" +
		"                                                to run again, and uninstall puts every file back")
}

// binaryCheck finds a harness's program on the login PATH and the usual
// install directories. Tests replace it.
var binaryCheck = func(ctx context.Context, home string) func(string) bool {
	path, fallbacks := state.LoginPath(ctx), agents.FallbackDirs(home)
	return func(command string) bool {
		_, _, ok := agents.Find(command, path, fallbacks)
		return ok
	}
}

// hookOptions resolves the harness config roots from the environment the
// same way the harnesses do.
func hookOptions(env map[string]string, local, block bool) (hooks.Options, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return hooks.Options{}, err
	}
	togoHome, err := state.Home()
	if err != nil {
		return hooks.Options{}, err
	}
	exe, err := executable()
	if err != nil {
		return hooks.Options{}, err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return hooks.Options{}, err
	}
	o := hooks.DefaultOptions(func(k string) string { return env[k] }, home, togoHome, exe)
	o.Cwd, o.Local, o.Block = cwd, local, block
	o.HasBinary = binaryCheck(context.Background(), home)
	return o, nil
}

func runHooks(args []string, env map[string]string, stdout, stderr io.Writer) int {
	if len(args) == 0 || !slices.Contains([]string{"install", "uninstall", "status"}, args[0]) {
		return usageError(stderr, fmt.Errorf("hooks needs one of install, uninstall, status"))
	}
	action := args[0]
	fs := newFlagSet("hooks " + action)
	agent := fs.String("agent", "all", "one harness ("+strings.Join(hooks.Agents, ", ")+") or all")
	local := fs.Bool("local", false, "edit ./.claude/settings.local.json (Claude only)")
	block := fs.Bool("block", false, "also hook the approval event, so the phone can answer")
	noLine := fs.Bool("no-statusline", false, "leave Claude's status line as it is")
	asJSON := fs.Bool("json", false, "print status as JSON")
	rest, err := parseArgs(fs, args[1:])
	if err != nil {
		return usageError(stderr, err)
	}
	if len(rest) > 0 {
		return usageError(stderr, fmt.Errorf("unexpected argument %q", rest[0]))
	}
	agents := hooks.Agents
	if *agent != "all" {
		if !hooks.Known(*agent) {
			return usageError(stderr, fmt.Errorf("unknown agent %q (one of %s)", *agent, strings.Join(hooks.Agents, ", ")))
		}
		agents = []string{*agent}
	}
	if *local && *agent != "all" && *agent != hooks.Claude {
		return usageError(stderr, fmt.Errorf("--local applies to Claude only"))
	}
	if *local && *agent == "all" {
		agents = []string{hooks.Claude}
	}
	o, err := hookOptions(env, *local, *block)
	if err != nil {
		return fail(stderr, err)
	}
	o.NoStatusLine = *noLine
	switch action {
	case "status":
		return printHookStatus(hooks.Status(o), *asJSON, stdout, stderr)
	case "install":
		return printHookResults(hooks.Install(o, agents), stdout, stderr)
	default:
		return printHookResults(hooks.Uninstall(o, agents), stdout, stderr)
	}
}

func printHookStatus(st map[string]string, asJSON bool, stdout, stderr io.Writer) int {
	if asJSON {
		if err := json.NewEncoder(stdout).Encode(st); err != nil {
			return fail(stderr, err)
		}
		return exitOK
	}
	outdated := false
	for _, a := range hooks.Agents {
		fmt.Fprintf(stdout, "%s: %s\n", a, st[a])
		outdated = outdated || st[a] == hooks.StatusOutdated
	}
	if outdated {
		fmt.Fprintln(stdout, "run togo hooks install (with the same --agent, --local and --block) to add the missing events")
	}
	return exitOK
}

// printHookResults prints one line per harness: installed (with its
// backup), unchanged, removed, skipped: not installed, or refused: why.
func printHookResults(rs []hooks.Result, stdout, stderr io.Writer) int {
	code := exitOK
	for _, r := range rs {
		switch {
		case r.Err != nil:
			fmt.Fprintf(stdout, "%s: refused: %s\n", r.Agent, r.Err)
			fmt.Fprintf(stderr, "togo: %s hooks left unchanged: %s\n", r.Agent, r.Err)
			code = exitError
		case r.State == "skipped":
			fmt.Fprintf(stdout, "%s: skipped: not installed\n", r.Agent)
		case r.State == "installed" && r.Backup != "":
			fmt.Fprintf(stdout, "%s: installed (%s, backup %s)\n", r.Agent, r.Path, r.Backup)
		case r.State == "installed" || r.State == "removed":
			fmt.Fprintf(stdout, "%s: %s (%s)\n", r.Agent, r.State, r.Path)
		default:
			fmt.Fprintf(stdout, "%s: %s\n", r.Agent, r.State)
		}
	}
	return code
}
