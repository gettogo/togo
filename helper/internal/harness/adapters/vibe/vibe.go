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
	// vibe-acp and vibe-app-server, installed with Vibe 3.1, run the same
	// local harness and write the same session store.
	hookstyle.Register(hookstyle.Options{Name: MapperHooks, Transcript: journalFor, Siblings: []string{"vibe-acp", "vibe-app-server"}})
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
// wins.
//
// current is the journal found before. Vibe started again without
// --continue, or resuming another session, writes another session's
// journal; the session moves there when its own process holds it, or when
// it was written after current's and no other Vibe runs in the folder to
// have written it (alone, a lookup begun after the journal was found).
// Otherwise current stays. It walks folders and asks the process view:
// the caller runs it away from the session's lock (hookstyle guess.go).
func journalFor(ctx context.Context, env harness.Env, current string, alone func() bool) string {
	if env.Cwd == "" {
		return ""
	}
	home := source.Expand("~", env.Vars[source.VarHome])
	metas, _ := filepath.Glob(filepath.Join(home, ".vibe", "logs", "session", "unified", "*", "meta.json"))
	created, _ := time.Parse(time.RFC3339, env.Vars["created"])
	var curAt time.Time
	if current != "" {
		curAt = newestSegment(filepath.Dir(current))
	}
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
		if glob := filepath.Join(dir, "*.jsonl"); current != "" && glob != current && !at.After(curAt) {
			continue // not written since the one followed: nothing to move to
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
	if current != "" && (len(cands) == 0 || len(cands) == 1 && cands[0].glob == current) {
		return current // the cheap case: no other journal was written since
	}
	verdicts := source.Attribute(ctx, env, all)
	best, bestAt, bestMine := "", time.Time{}, false
	curMine := false
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
		if c.glob == current {
			curMine = mine
			continue
		}
		if foreign && !mine {
			continue
		}
		if (mine && !bestMine) || (mine == bestMine && c.at.After(bestAt)) {
			best, bestAt, bestMine = c.glob, c.at, mine
		}
	}
	switch {
	case current == "":
		return best
	case best == "", curMine && !bestMine, !bestMine && !alone():
		return current
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
