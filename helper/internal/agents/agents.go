// Package agents is the fixed registry of coding agents a session can run.
package agents

import (
	"os"
	"path/filepath"
)

// Agent is a known agent. Command is the program run in the session's login
// shell; it is empty for the plain shell.
type Agent struct {
	ID, Name, Command string
}

// Registry lists the agents in the order clients show them. Command strings
// are placed into a shell command line, so they must stay constant literals.
var Registry = []Agent{
	{"claude", "Claude Code", "claude"},
	{"omp", "OMP", "omp"},
	{"codex", "Codex", "codex"},
	{"opencode", "OpenCode", "opencode"},
	{"shell", "Shell", ""},
}

// Lookup returns the registry entry with the given id.
func Lookup(id string) (Agent, bool) {
	for _, a := range Registry {
		if a.ID == id {
			return a, true
		}
	}
	return Agent{}, false
}

// Installed reports whether a's command is an executable file in one of the
// absolute directories of path. The shell is always installed. Relative PATH
// entries are ignored: they depend on the helper's working directory, not
// the one the session will start in.
func Installed(a Agent, path string) bool {
	if a.Command == "" {
		return true
	}
	for _, dir := range filepath.SplitList(path) {
		if !filepath.IsAbs(dir) {
			continue
		}
		info, err := os.Stat(filepath.Join(dir, a.Command))
		if err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0 {
			return true
		}
	}
	return false
}
