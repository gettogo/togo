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
// accepted there: codex and amp take their global flags before a
// subcommand. An agent whose resume needs a subcommand that takes no
// global flags before it, or a session id, has none.
var Registry = []Agent{
	{ID: "claude", Name: "Claude Code", Command: "claude", Resume: []string{"--continue"}},
	{ID: "omp", Name: "OMP", Command: "omp"},
	{ID: "codex", Name: "Codex", Command: "codex", Resume: []string{"resume", "--last"}},
	{ID: "opencode", Name: "OpenCode", Command: "opencode"},
	{ID: "copilot", Name: "Copilot CLI", Command: "copilot", Resume: []string{"--continue"}},
	{ID: "cursor", Name: "Cursor CLI", Command: "cursor-agent", Resume: []string{"--resume"}},
	{ID: "aider", Name: "Aider", Command: "aider", Resume: []string{"--restore-chat-history"}},
	{ID: "amp", Name: "Amp", Command: "amp"},
	{ID: "auggie", Name: "Auggie", Command: "auggie", Resume: []string{"--continue"}},
	{ID: "cline", Name: "Cline", Command: "cline"},
	{ID: "crush", Name: "Crush", Command: "crush", Resume: []string{"--continue"}},
	{ID: "droid", Name: "Droid", Command: "droid", Resume: []string{"--resume"}},
	{ID: "goose", Name: "Goose", Command: "goose"},
	{ID: "kilo", Name: "Kilo Code", Command: "kilo"},
	{ID: "kimi", Name: "Kimi Code", Command: "kimi", Resume: []string{"--continue"}},
	{ID: "kiro", Name: "Kiro CLI", Command: "kiro-cli"},
	{ID: "openhands", Name: "OpenHands", Command: "openhands"},
	{ID: "pi", Name: "Pi", Command: "pi", Resume: []string{"--continue"}},
	{ID: "vibe", Name: "Mistral Vibe", Command: "vibe", Resume: []string{"--continue"}},
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

// FallbackDirs are where package managers and installers put command line
// tools, searched after the login PATH when it does not name a program
// (the helper may run under launchd or a bare ssh command). home is the
// user's home directory; when empty, only the system directories remain.
func FallbackDirs(home string) []string {
	var dirs []string
	if home != "" {
		for _, d := range []string{
			".local/bin", "bin", ".claude/local", ".bun/bin", ".npm-global/bin",
			".volta/bin", ".cargo/bin", "go/bin", ".opencode/bin", ".amp/bin",
			".kimi-code/bin", // Kimi Code's installer (seen live: kimi was "not installed" without it)
		} {
			dirs = append(dirs, filepath.Join(home, d))
		}
	}
	dirs = append(dirs,
		"/opt/homebrew/bin", "/usr/local/bin",
		"/home/linuxbrew/.linuxbrew/bin", "/nix/var/nix/profiles/default/bin",
	)
	if home != "" {
		dirs = append(dirs, filepath.Join(home, ".nix-profile", "bin"))
	}
	return dirs
}

// Installed reports whether a's command is an executable file in one of the
// absolute directories of path or of fallbacks. The shell is always
// installed.
func Installed(a Agent, path string, fallbacks []string) bool {
	if a.Command == "" {
		return true
	}
	_, _, ok := Find(a.Command, path, fallbacks)
	return ok
}

// Find returns the first regular executable file named name in the
// absolute directories of path, then of fallbacks. offPath reports a hit in
// fallbacks. Relative entries are ignored: they depend on the helper's
// working directory, not the one the session will start in. name must be a
// bare file name.
func Find(name, path string, fallbacks []string) (found string, offPath, ok bool) {
	if p, ok := findIn(name, filepath.SplitList(path)); ok {
		return p, false, true
	}
	if p, ok := findIn(name, fallbacks); ok {
		return p, true, true
	}
	return "", false, false
}

func findIn(name string, dirs []string) (string, bool) {
	for _, dir := range dirs {
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

// Resolve is the registry id for a session's agent: id itself, except that
// a custom program (wire's "custom") whose argv names a known agent's
// program, by path or by name, is that agent. Notifications then name
// Kimi Code rather than "Custom".
func Resolve(id string, argv []string) string {
	if id != "custom" || len(argv) == 0 {
		return id
	}
	base := filepath.Base(argv[0])
	for _, a := range Registry {
		if a.Command != "" && a.Command == base {
			return a.ID
		}
	}
	return id
}
