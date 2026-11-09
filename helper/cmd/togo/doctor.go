package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/gettogo/togo/helper/internal/agents"
	"github.com/gettogo/togo/helper/internal/doctor"
	"github.com/gettogo/togo/helper/internal/hooks"
)

// doctorIntegrations reports each harness's setup state. It looks for the
// harnesses' programs the way the other checks look for tmux; tests
// replace it.
var doctorIntegrations = func(e doctor.Env) []hooks.Report {
	o, err := hookOptions(osEnv(), false, false)
	if err != nil {
		return nil
	}
	home := e.Home
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	fallbacks := agents.FallbackDirs(home)
	o.HasBinary = func(command string) bool {
		_, _, ok := agents.Find(command, e.Path, fallbacks)
		return ok
	}
	return hooks.Reports(o, hooks.LoadSeen(o.TogoHome))
}

func osEnv() map[string]string {
	env := map[string]string{}
	for _, kv := range os.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok {
			env[k] = v
		}
	}
	return env
}

// runDoctor prints the host readiness report, then each harness's setup
// state. It exits 1 when a required check fails (doctor.Failed: tmux
// missing or too old), wherever that check sits in the report; every other
// finding is advice.
func runDoctor(args []string, e doctor.Env, out io.Writer) int {
	fs := newFlagSet("doctor")
	asJSON := fs.Bool("json", false, "print the report as JSON")
	positional, err := parseArgs(fs, args)
	if err != nil {
		return usageError(out, err)
	}
	if len(positional) > 0 {
		return usageError(out, fmt.Errorf("doctor takes no arguments"))
	}

	checks := doctor.Run(context.Background(), e)
	integrations := doctorIntegrations(e)
	if *asJSON {
		if integrations == nil {
			integrations = []hooks.Report{}
		}
		if err := json.NewEncoder(out).Encode(struct {
			Checks       []doctor.Check `json:"checks"`
			Integrations []hooks.Report `json:"integrations"`
		}{checks, integrations}); err != nil {
			return exitError
		}
	} else {
		for _, c := range checks {
			detail := c.Detail
			if c.Fix != "" {
				detail += "; fix: " + c.Fix
			}
			fmt.Fprintf(out, "%-13s %-9s %s\n", c.Name, c.Status, detail)
		}
		printIntegrations(out, integrations)
	}
	if doctor.AnyFailed(checks) {
		return exitError
	}
	return exitOK
}

// integrationWords are the state words doctor prints.
var integrationWords = map[string]string{
	hooks.StateReady: "ready", hooks.StateNeedsSetup: "needs setup", hooks.StateOutdated: "outdated",
	hooks.StateNotNeeded: "no setup needed", hooks.StateNotSupported: "not supported yet",
}

// printIntegrations prints one line per installed harness and the ones not
// installed on a last line.
func printIntegrations(out io.Writer, rs []hooks.Report) {
	if len(rs) == 0 {
		return
	}
	fmt.Fprintln(out, "\nintegrations")
	var absent []string
	for _, r := range rs {
		if !r.Installed {
			absent = append(absent, r.ID)
			continue
		}
		fmt.Fprintf(out, "  %-10s %-17s %s\n", r.ID, integrationWords[r.State], integrationDetail(r))
	}
	if len(absent) > 0 {
		fmt.Fprintf(out, "  not installed: %s\n", strings.Join(absent, ", "))
	}
}

func integrationDetail(r hooks.Report) string {
	var parts []string
	if r.Hooks != "" {
		parts = append(parts, "hooks "+r.Hooks)
	}
	if r.StatusLine != "" {
		parts = append(parts, "status line "+r.StatusLine)
	}
	switch r.Setup {
	case hooks.SetupHooks:
		if r.LastHookAt != "" {
			parts = append(parts, "last hook "+r.LastHookAt)
		} else {
			parts = append(parts, "no hook received yet")
		}
	case hooks.SetupPending:
		parts = append(parts, "its hooks are not confirmed yet; the terminal works as is")
	}
	if r.StatusLineAt != "" {
		parts = append(parts, "last status line "+r.StatusLineAt)
	}
	if r.Backup != "" {
		parts = append(parts, "backup "+r.Backup)
	}
	line := strings.Join(parts, "; ")
	if r.Fix != "" {
		line += "; fix: " + r.Fix
	}
	return line
}
