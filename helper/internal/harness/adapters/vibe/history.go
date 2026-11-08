package vibe

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/source"
)

// Vibe folds its journal into a snapshot after each turn and keeps only
// the last two journal segments, so a session's earlier history lives in
// the snapshot: CURRENT names the generation, whose manifest lists the
// content-addressed chunks (JSON arrays of history entries) that make up
// the public history, and whose projection-state.json holds the envelope.
var (
	idPattern  = regexp.MustCompile(`^[0-9a-f][0-9a-f-]{7,63}$`)
	genPattern = regexp.MustCompile(`^[0-9]{16}$`)
	shaPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

const (
	maxSnapshotFile   = 8 << 20
	maxHistoryEntries = 1000
)

// sessionRoot is the folder of a unified session, or "" for an id that
// is not one.
func sessionRoot(env harness.Env, id string) string {
	if !idPattern.MatchString(id) {
		return ""
	}
	home := source.Expand("~", env.Vars[source.VarHome])
	return filepath.Join(home, ".vibe", "logs", "session", "unified", id)
}

// history maps the session's snapshot: its entries, then its envelope.
// The journal records that follow it repeat nothing it holds, and items
// keep their entry ids, so reading both is safe.
func (s *session) history(id string) []harness.Item {
	root := sessionRoot(s.env, id)
	if root == "" {
		return nil
	}
	s.readMeta(root)
	var cur struct {
		Generation string `json:"generation"`
	}
	if readJSON(filepath.Join(root, "CURRENT"), &cur) != nil || !genPattern.MatchString(cur.Generation) {
		return nil
	}
	gen := filepath.Join(root, "generations", cur.Generation)
	var man struct {
		Projection struct {
			Chunks []string `json:"chunks"`
		} `json:"projection_state"`
	}
	if readJSON(filepath.Join(gen, "manifest.json"), &man) != nil {
		return nil
	}
	var entries []entry
	for _, c := range man.Projection.Chunks {
		if !shaPattern.MatchString(c) {
			return nil
		}
		var part []entry
		if readJSON(filepath.Join(root, "chunks", c+".json"), &part) != nil {
			return nil
		}
		entries = append(entries, part...)
	}
	if len(entries) > maxHistoryEntries {
		entries = entries[len(entries)-maxHistoryEntries:]
	}
	var items []harness.Item
	for _, e := range entries {
		items = append(items, s.entry(e)...)
	}
	var ps struct {
		Snapshot envelope `json:"snapshot"`
	}
	if readJSON(filepath.Join(gen, "projection-state.json"), &ps) == nil {
		items = append(items, s.envelope(ps.Snapshot)...)
	}
	return items
}

// readMeta keeps the model meta.json names.
func (s *session) readMeta(root string) {
	var meta struct {
		Config struct {
			ActiveModel string `json:"active_model"`
			ModelName   string `json:"model_name"`
		} `json:"config"`
	}
	if readJSON(filepath.Join(root, "meta.json"), &meta) != nil {
		return
	}
	if m := meta.Config.ActiveModel; m != "" {
		s.state["model"] = m
	} else if m := meta.Config.ModelName; m != "" {
		s.state["model"] = m
	}
}

func readJSON(p string, v any) error {
	st, err := os.Lstat(p)
	if err != nil {
		return err
	}
	if !st.Mode().IsRegular() || st.Size() > maxSnapshotFile {
		return os.ErrInvalid
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}
