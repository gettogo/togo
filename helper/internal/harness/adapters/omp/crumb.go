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
	// A tty can be reused before the new agent updates its record: one
	// older than the pane's process is the old occupant's, unless it names
	// the conversation the pane's command resumes. omp leaves a record
	// that already holds the same text as it is (seen on 19.0.1: resume A,
	// then B, then A again on A's old device, and the record keeps its
	// time), so its time alone would refuse it for good.
	since := recordSince(env)
	stale := !since.IsZero() && info.ModTime().Before(since)
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
	if stale && !resumes(env.Vars[source.VarPaneCommand], file) {
		return ""
	}
	return filepath.ToSlash(rel)
}

// PaneMoved takes the session's environment after omp was restarted in its
// pane (a resume, or a respawn by hand): the record to read is the new
// device's, and the old device's, which still names the conversation
// before the restart, is not read again. It is read at once.
func (s *session) PaneMoved(env harness.Env) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.env = env
	s.crumbAt, s.crumb = time.Time{}, ""
}

// recordSince is the time a record for the pane's device is written at or
// after when the agent in the pane wrote it: the session's start, or when
// the pane's process started, which tmux restarts on a device another
// terminal may have used (seen on macOS: ttys002, ttys008, then ttys002
// again). Both are read again after a helper restart. The process start is
// cut to the second, as created is, for file systems with coarse times.
func recordSince(env harness.Env) time.Time {
	since, _ := time.Parse(time.RFC3339, env.Vars["created"])
	if r, err := time.Parse(time.RFC3339Nano, env.Vars[source.VarPaneStarted]); err == nil && r.After(since) {
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
