package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"

	"github.com/gettogo/togo/helper/internal/hooks"
	"github.com/gettogo/togo/helper/internal/state"
)

func init() {
	register("hooks", func(_ context.Context, args []string, env map[string]string, _ io.Reader, stdout, stderr io.Writer) int {
		return runHooks(args, env, stdout, stderr)
	})
	addUsage("  hooks install|uninstall|status [--agent claude|codex|all] [--local] [--block] [--json]\n                                                add or remove agent hook entries")
}

// hookOptions resolves the agent config roots from the environment the same
// way the agents do.
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
	pick := func(key, fallback string) string {
		if v := env[key]; v != "" {
			return v
		}
		return filepath.Join(home, fallback)
	}
	return hooks.Options{
		TogoHome:  togoHome,
		Binary:    exe,
		ClaudeDir: pick("CLAUDE_CONFIG_DIR", ".claude"),
		CodexDir:  pick("CODEX_HOME", ".codex"),
		Cwd:       cwd,
		Local:     local,
		Block:     block,
	}, nil
}

func runHooks(args []string, env map[string]string, stdout, stderr io.Writer) int {
	if len(args) == 0 || !slices.Contains([]string{"install", "uninstall", "status"}, args[0]) {
		return usageError(stderr, fmt.Errorf("hooks needs one of install, uninstall, status"))
	}
	action := args[0]
	fs := newFlagSet("hooks " + action)
	agent := fs.String("agent", "all", "claude, codex or all")
	local := fs.Bool("local", false, "edit ./.claude/settings.local.json (Claude only)")
	block := fs.Bool("block", false, "also hook PermissionRequest so the phone can answer")
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
		if !slices.Contains(hooks.Agents, *agent) {
			return usageError(stderr, fmt.Errorf("unknown agent %q", *agent))
		}
		agents = []string{*agent}
	}
	if *local && *agent == hooks.Codex {
		return usageError(stderr, fmt.Errorf("--local applies to Claude only"))
	}
	if *local && *agent == "all" {
		agents = []string{hooks.Claude}
	}
	o, err := hookOptions(env, *local, *block)
	if err != nil {
		return fail(stderr, err)
	}
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
	for _, a := range hooks.Agents {
		fmt.Fprintf(stdout, "%s: %s\n", a, st[a])
	}
	return exitOK
}

func printHookResults(rs []hooks.Result, stdout, stderr io.Writer) int {
	code := exitOK
	for _, r := range rs {
		switch {
		case r.Err != nil:
			fmt.Fprintf(stderr, "togo: %s hooks left unchanged: %s\n", r.Agent, r.Err)
			code = exitError
		case r.State == "skipped":
			fmt.Fprintf(stdout, "%s: not found, skipped\n", r.Agent)
		default:
			fmt.Fprintf(stdout, "%s: %s\n", r.Agent, r.State)
		}
	}
	return code
}
