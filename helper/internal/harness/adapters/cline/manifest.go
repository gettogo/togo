package cline

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/gettogo/togo/helper/internal/harness/source"
)

type manifest struct {
	// Pid is the Cline process that runs the session.
	Pid int `json:"pid"`
	// Interactive is false for a one-shot `cline <task>` run, true for
	// the TUI (testdata/session.json, ask.json).
	Interactive *bool  `json:"interactive"`
	Cwd         string `json:"cwd"`
	StartedAt   string `json:"started_at"`
	Model       string `json:"model"`
	Metadata    struct {
		Mode string `json:"mode"`
	} `json:"metadata"`
}

func (m *messages) manifest(id string) (manifest, bool) {
	var mf manifest
	p := filepath.Join(m.dir(id), id+".json")
	st, err := os.Stat(p)
	if err != nil || !st.Mode().IsRegular() || st.Size() > maxManifest {
		return mf, false
	}
	b, err := os.ReadFile(p)
	if err != nil || json.Unmarshal(b, &mf) != nil {
		return mf, false
	}
	return mf, true
}

// belongs reports whether session id works in this session's folder and
// started, or was written to, since the Togo session began.
func (m *messages) belongs(id, updated string) bool {
	mf, ok := m.manifest(id)
	if !ok || !sameDir(mf.Cwd, m.env.Cwd) {
		return false
	}
	if m.created.IsZero() {
		return true
	}
	since := m.created.Add(-startSlack)
	for _, s := range []string{mf.StartedAt, updated} {
		if t, err := time.Parse(time.RFC3339Nano, s); err == nil && !t.Before(since) {
			return true
		}
	}
	return false
}

// owner says whether the session's Cline process is in this Togo session's
// process tree. Unknown when the manifest has no pid or the tree cannot be
// listed.
func (m *messages) owner(id string) source.Verdict {
	mf, ok := m.manifest(id)
	if !ok || mf.Pid <= 0 || m.env.Procs == nil {
		return source.Unknown
	}
	if time.Since(m.treeAt) > treeEvery {
		m.tree, m.treeAt = m.env.Procs.Tree(context.Background()), time.Now()
	}
	if len(m.tree) == 0 {
		return source.Unknown
	}
	if slices.Contains(m.tree, mf.Pid) {
		return source.Mine
	}
	return source.Foreign
}

// treeEvery bounds how often the process tree is listed.
const treeEvery = 2 * time.Second

func sameDir(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	ra, err := filepath.EvalSymlinks(a)
	if err != nil {
		ra = filepath.Clean(a)
	}
	rb, err := filepath.EvalSymlinks(b)
	if err != nil {
		rb = filepath.Clean(b)
	}
	return ra == rb
}
