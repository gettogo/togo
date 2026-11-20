package omp

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

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
	record, err := os.Open(filepath.Join(terminalRoot(env), name))
	if err != nil {
		return ""
	}
	defer record.Close()
	info, err := record.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return ""
	}
	// A tty can be reused before the new agent updates its record. The
	// record must belong to this session's lifetime, not the old occupant.
	if since := recordSince(env); !since.IsZero() && info.ModTime().Before(since) {
		return ""
	}
	b, err := io.ReadAll(record)
	if err != nil {
		return ""
	}
	lines := strings.Split(string(b), "\n")
	if len(lines) < 2 || !sameDir(lines[0], env.Cwd) {
		return ""
	}
	file := filepath.Clean(strings.TrimSpace(lines[1]))
	rel, err := filepath.Rel(sessionsRoot(env), file)
	if err != nil || !filepath.IsLocal(rel) || filepath.Ext(rel) != ".jsonl" || strings.ContainsAny(rel, "*?[") {
		return ""
	}
	return filepath.ToSlash(rel)
}

// recordSince is the time a record for the pane's device must be written
// at or after: the session's start, or the last restart of omp in its pane,
// which tmux respawns on a device another terminal may have used (seen on
// macOS: ttys002, ttys008, then ttys002 again). The restart is cut to the
// second, as created is, for file systems that keep coarse times.
func recordSince(env harness.Env) time.Time {
	since, _ := time.Parse(time.RFC3339, env.Vars["created"])
	if r, err := time.Parse(time.RFC3339Nano, env.Vars[source.VarRelaunched]); err == nil && r.After(since) {
		since = r.Truncate(time.Second)
	}
	return since
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
