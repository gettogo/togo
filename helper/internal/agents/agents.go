// Package agents is the fixed registry of coding agents a session can run.
package agents

import (
	"os"
	"path/filepath"
)

// Agent is a known agent. Command is the program the session runs; it is
// empty for the plain shell. Resume holds the arguments that continue the
// agent's most recent conversation in the session's folder, or nil when
// the agent has none.
type Agent struct {
	ID, Name, Command string
	Resume            []string
}

// Registry lists the agents in the order clients show them. Commands stay
// constant literals: they are the first argv entry of every session that
// runs the agent. Resume follows the user's own arguments, so it must be
// accepted there: codex takes its global flags before a subcommand.
var Registry = []Agent{
	{ID: "claude", Name: "Claude Code", Command: "claude", Resume: []string{"--continue"}},
	{ID: "omp", Name: "OMP", Command: "omp"},
	{ID: "codex", Name: "Codex", Command: "codex", Resume: []string{"resume", "--last"}},
	{ID: "opencode", Name: "OpenCode", Command: "opencode"},
	{ID: "shell", Name: "Shell"},
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
// absolute directories of path. The shell is always installed.
func Installed(a Agent, path string) bool {
	if a.Command == "" {
		return true
	}
	_, ok := Find(a.Command, path)
	return ok
}

// Find returns the first regular executable file named name in the
// absolute directories of path. Relative PATH entries are ignored: they
// depend on the helper's working directory, not the one the session will
// start in. name must be a bare file name.
func Find(name, path string) (string, bool) {
	for _, dir := range filepath.SplitList(path) {
		if !filepath.IsAbs(dir) {
			continue
		}
		p := filepath.Join(dir, name)
		if Executable(p) {
			return p, true
		}
	}
	return "", false
}

// Executable reports whether p, following links, is a regular file with an
// execute bit set.
func Executable(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0
}
