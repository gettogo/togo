// Package vibe is the Mistral Vibe harness adapter: its adapter file, the
// vibe.hooks mapper (hooks that speak Vibe's claude_code protocol, read by
// hookstyle) and the vibe.session mapper, which reads the recovery journal
// Vibe 3.1 keeps per session at
// ~/.vibe/logs/session/unified/<id>/journal/<sequence>.jsonl, next to
// meta.json and the snapshot it folds the journal into after each turn.
// The layout was read from Vibe 3.1.0's local harness
// (mistralai_vibe_local_harness/vibe/_storage.py, _projection.py) and
// checked against live sessions.
package vibe

import (
	"context"
	_ "embed"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/adapters/hookstyle"
	"github.com/gettogo/togo/helper/internal/harness/source"
)

//go:embed vibe.yaml
var adapterFile []byte

// Mapper names, as vibe.yaml uses them.
const (
	MapperHooks   = "vibe.hooks"
	MapperSession = "vibe.session"
)

func init() {
	harness.RegisterAdapterFile("vibe", adapterFile)
	hookstyle.Register(hookstyle.Options{Name: MapperHooks, Transcript: journalFor})
	harness.RegisterMapper(MapperSession, func(env harness.Env) harness.Mapper { return newSession(env) })
}

// AdapterFile returns the embedded adapter file.
func AdapterFile() []byte { return adapterFile }

// maxMeta bounds a meta.json read.
const maxMeta = 256 << 10

// journalFor finds the journal of the top-level Vibe session that works in
// the session's folder and was written since the session started, as a
// glob over its segments: Vibe starts a new segment after each turn and
// keeps the last two. A journal held open by a process outside this
// session's tree (a Vibe the user ran in a terminal) is never chosen, and
// one held by the session's own process comes first; otherwise the newest
// wins. hookstyle asks only until it has an answer, so the choice sticks.
func journalFor(env harness.Env) string {
	if env.Cwd == "" {
		return ""
	}
	home := source.Expand("~", env.Vars[source.VarHome])
	metas, _ := filepath.Glob(filepath.Join(home, ".vibe", "logs", "session", "unified", "*", "meta.json"))
	created, _ := time.Parse(time.RFC3339, env.Vars["created"])
	type cand struct {
		glob string
		at   time.Time
		segs []string
	}
	var cands []cand
	var all []string
	for _, m := range metas {
		st, err := os.Stat(m)
		if err != nil || st.Size() > maxMeta {
			continue
		}
		dir := filepath.Join(filepath.Dir(m), "journal")
		at := newestSegment(dir)
		if at.IsZero() || (!created.IsZero() && at.Before(created.Add(-time.Minute))) {
			continue
		}
		b, err := os.ReadFile(m)
		if err != nil {
			continue
		}
		var meta struct {
			Parent      *string `json:"parent_session_id"`
			Environment struct {
				WorkingDirectory string `json:"working_directory"`
			} `json:"environment"`
			Origin string `json:"origin_directory"`
		}
		if json.Unmarshal(b, &meta) != nil || meta.Parent != nil {
			continue
		}
		if filepath.Clean(meta.Environment.WorkingDirectory) != filepath.Clean(env.Cwd) && filepath.Clean(meta.Origin) != filepath.Clean(env.Cwd) {
			continue
		}
		segs, _ := filepath.Glob(filepath.Join(dir, "*.jsonl"))
		cands = append(cands, cand{filepath.Join(dir, "*.jsonl"), at, segs})
		all = append(all, segs...)
	}
	verdicts := source.Attribute(context.Background(), env, all)
	best, bestAt, bestMine := "", time.Time{}, false
	for _, c := range cands {
		mine, foreign := false, false
		for _, p := range c.segs {
			switch verdicts[p] {
			case source.Mine:
				mine = true
			case source.Foreign:
				foreign = true
			}
		}
		if foreign && !mine {
			continue
		}
		if (mine && !bestMine) || (mine == bestMine && c.at.After(bestAt)) {
			best, bestAt, bestMine = c.glob, c.at, mine
		}
	}
	return best
}

// newestSegment is when a journal folder's newest segment changed.
func newestSegment(dir string) time.Time {
	segs, _ := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	var at time.Time
	for _, p := range segs {
		if st, err := os.Stat(p); err == nil && st.ModTime().After(at) {
			at = st.ModTime()
		}
	}
	return at
}
