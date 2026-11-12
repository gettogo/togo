package vibe

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/source"
)

type heldProcs struct {
	tree    []int
	holders map[string][]int
}

func (h heldProcs) Tree(context.Context) []int { return h.tree }
func (h heldProcs) Holders(_ context.Context, paths []string) (map[string][]int, error) {
	out := map[string][]int{}
	for _, p := range paths {
		if v := h.holders[p]; len(v) > 0 {
			out[p] = v
		}
	}
	return out, nil
}

// A journal a process outside the session's tree holds open is not
// chosen even when it is the newest; the session's own is.
func TestJournalForSkipsForeignProcess(t *testing.T) {
	home := t.TempDir()
	write := func(id string, age time.Duration) (glob, seg string) {
		p := filepath.Join(home, ".vibe", "logs", "session", "unified", id)
		_ = os.MkdirAll(filepath.Join(p, "journal"), 0o700)
		meta := `{"session_id":"` + id + `","parent_session_id":null,"environment":{"working_directory":"/w/proj"}}`
		_ = os.WriteFile(filepath.Join(p, "meta.json"), []byte(meta), 0o600)
		seg = filepath.Join(p, "journal", "0000000000000001.jsonl")
		_ = os.WriteFile(seg, []byte("{}\n"), 0o600)
		at := time.Now().Add(-age)
		_ = os.Chtimes(seg, at, at)
		return filepath.Join(p, "journal", "*.jsonl"), seg
	}
	mine, mineSeg := write("mine", time.Minute)
	_, foreignSeg := write("foreign", 0)
	env := harness.Env{Cwd: "/w/proj", Vars: map[string]string{source.VarHome: home}}
	env.Procs = heldProcs{tree: []int{10, 11}, holders: map[string][]int{foreignSeg: {99}, mineSeg: {11}}}
	if got := journalFor(env); got != mine {
		t.Fatalf("got %q want %q", got, mine)
	}
	env.Procs = heldProcs{tree: []int{10, 11}, holders: map[string][]int{foreignSeg: {99}}}
	if got := journalFor(env); got != mine {
		t.Fatalf("foreign-held journal chosen without evidence: %q", got)
	}
}

func (heldProcs) Others(context.Context, []string, string) (bool, error) { return false, nil }
