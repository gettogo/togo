package omp

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/source"
)

// terminalSession reads omp's record of the session file the session's
// terminal uses (~/.omp/agent/terminal-sessions/<tty>: the folder, then the
// file, then markers). omp rewrites it when the terminal's session changes
// (start, /new, /resume), so it names the file without guessing among the
// folder's. The answer is relative to the sessions folder; "" when there is
// no record for this pane, or it is for another folder or outside omp's
// sessions.
func terminalSession(env harness.Env) string {
	tty := env.Vars[source.VarTTY]
	if !strings.HasPrefix(tty, "/dev/") {
		return ""
	}
	// omp names the record after the device without /dev/, slashes as dashes.
	name := strings.ReplaceAll(strings.TrimPrefix(tty, "/dev/"), "/", "-")
	home := source.Expand("~", env.Vars[source.VarHome])
	agent := filepath.Join(home, ".omp", "agent")
	b, err := os.ReadFile(filepath.Join(agent, "terminal-sessions", name))
	if err != nil {
		return ""
	}
	lines := strings.Split(string(b), "\n")
	if len(lines) < 2 || !sameDir(lines[0], env.Cwd) {
		return ""
	}
	file := filepath.Clean(strings.TrimSpace(lines[1]))
	rel, err := filepath.Rel(filepath.Join(agent, "sessions"), file)
	if err != nil || !filepath.IsLocal(rel) || filepath.Ext(rel) != ".jsonl" || strings.ContainsAny(rel, "*?[") {
		return ""
	}
	return filepath.ToSlash(rel)
}

func sameDir(a, b string) bool {
	a, b = filepath.Clean(strings.TrimSpace(a)), filepath.Clean(b)
	if a == b {
		return true
	}
	ra, err1 := filepath.EvalSymlinks(a)
	rb, err2 := filepath.EvalSymlinks(b)
	return err1 == nil && err2 == nil && ra == rb
}
