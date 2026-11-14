package omp

import (
	"path/filepath"
	"strings"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/source"
)

// VarAgent is the template variable holding omp's agent folder, where its
// sessions, terminal records and config.yml live.
const VarAgent = "omp_agent"

func init() {
	harness.RegisterRoots("omp", func(env harness.Env) map[string]string {
		return map[string]string{VarAgent: agentDir(env)}
	})
}

// agentDir is the folder the session's omp keeps its data in, by the rules
// of omp's own resolver (packages/utils/src/dirs.ts, 18.20): a named
// profile lives under <config root>/profiles/<name>/agent; without one
// PI_CODING_AGENT_DIR names the folder; else it is <config root>/agent.
// The config root is ~/.omp, or ~/<PI_CONFIG_DIR>. The values come from
// the launch arguments and the login environment the session started in.
// omp's XDG layout on Linux (after `omp config migrate`) is not followed.
func agentDir(env harness.Env) string {
	home := source.Expand("~", env.Vars[source.VarHome])
	root := filepath.Join(home, ".omp")
	if name, ok := env.Getenv("PI_CONFIG_DIR"); ok && name != "" {
		root = filepath.Join(home, name)
	}
	if p := profile(env); p != "" {
		return filepath.Join(root, "profiles", p, "agent")
	}
	if dir, ok := env.Getenv("PI_CODING_AGENT_DIR"); ok && filepath.IsAbs(dir) {
		return filepath.Clean(dir)
	}
	return filepath.Join(root, "agent")
}

// profile is the session's omp profile: --profile on the command line,
// else OMP_PROFILE, else PI_PROFILE when OMP_PROFILE is unset (an empty
// OMP_PROFILE selects the default profile). A name that is not a plain
// folder name is no profile, as omp rejects it.
func profile(env harness.Env) string {
	name, found := "", false
	for i, arg := range env.Argv {
		if arg == "--" {
			break
		}
		if v, ok := strings.CutPrefix(arg, "--profile="); ok {
			name, found = v, true
		} else if arg == "--profile" && i+1 < len(env.Argv) {
			name, found = env.Argv[i+1], true
		}
	}
	if !found {
		if name, found = env.Getenv("OMP_PROFILE"); !found {
			name, _ = env.Getenv("PI_PROFILE")
		}
	}
	name = strings.TrimSpace(name)
	if name == "" || name == "default" || name == "." || name == ".." || strings.ContainsAny(name, `/\`) {
		return ""
	}
	return name
}

// agentRoot is the agent folder a mapper or helper reads from env, with
// the default for an environment built without the adapter's roots.
func agentRoot(env harness.Env) string {
	if dir := env.Vars[VarAgent]; dir != "" {
		return dir
	}
	return agentDir(env)
}
