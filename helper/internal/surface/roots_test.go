package surface

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/gettogo/togo/helper/internal/session"
	"github.com/gettogo/togo/helper/internal/tmux"
	"github.com/gettogo/togo/helper/internal/tmux/tmuxtest"
)

// Of the folders a companion lists, the ones used are absolute, bounded,
// existing, each once and not the session's own. Paths with spaces are
// kept. A list that had to leave an entry out for its form or the bounds,
// or that says itself it is partial, is not whole.
func TestAdditionalWorkspaceFoldersAreTakenWholeOrSaidPartial(t *testing.T) {
	base := t.TempDir()
	spaced, second, cwd := filepath.Join(base, "with space"), filepath.Join(base, "second"), filepath.Join(base, "cwd")
	for _, d := range []string{spaced, second, cwd} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	roots, whole := rootsOf(CompanionState{Cwd: cwd, Roots: []string{spaced, cwd, spaced, filepath.Join(base, "gone"), second + "/"}})
	if !whole || len(roots) != 2 || roots[0] != spaced || roots[1] != second {
		t.Fatalf("roots = %q whole %v", roots, whole)
	}
	for name, state := range map[string]CompanionState{
		"a relative entry":     {Cwd: cwd, Roots: []string{spaced, "relative/dir"}},
		"too long an entry":    {Cwd: cwd, Roots: []string{spaced, "/" + string(make([]byte, MaxRootBytes+1))}},
		"the program's own no": {Cwd: cwd, Roots: []string{spaced}, RootsIncomplete: true},
	} {
		roots, whole := rootsOf(state)
		if whole || len(roots) != 1 || roots[0] != spaced {
			t.Errorf("%s: roots = %q whole %v, want the usable one and not whole", name, roots, whole)
		}
	}
	var many []string
	for i := 0; i < MaxRoots+5; i++ {
		d := filepath.Join(base, fmt.Sprintf("root-%02d", i))
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		many = append(many, d)
	}
	if roots, whole := rootsOf(CompanionState{Cwd: cwd, Roots: many}); whole || len(roots) != MaxRoots {
		t.Errorf("too many folders: %d taken, whole %v", len(roots), whole)
	}
	if roots, whole := rootsOf(CompanionState{Cwd: cwd}); !whole || len(roots) != 0 {
		t.Errorf("no folders: %q whole %v", roots, whole)
	}
}

// A state file names a process and a pane. Folders are taken from it only
// when that process runs in the session's own active pane: another live
// process, or another pane, owns nothing here.
func TestACompanionStateOwnsOnlyItsOwnPane(t *testing.T) {
	c := tmuxtest.New(t)
	ctx := context.Background()
	const id = "s_abcdefghij"
	if err := c.NewSession(ctx, tmux.NewSessionOpts{Name: id, Command: []string{"/bin/sh"}}); err != nil {
		t.Fatal(err)
	}
	b := &paneBackend{sessions: &session.Service{Tmux: c}, companions: &Companions{Home: t.TempDir()}}
	pane, err := c.ActivePane(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	pids, err := c.PanePIDs(ctx, id)
	if err != nil || len(pids) == 0 {
		t.Fatalf("pane pids = %v %v", pids, err)
	}
	if !b.owns(ctx, id, CompanionState{PID: pids[0], Pane: pane}) {
		t.Fatal("the pane's own process is not recognised")
	}
	if b.owns(ctx, id, CompanionState{PID: os.Getpid(), Pane: pane}) {
		t.Error("another live process was taken for the session's program")
	}
	if b.owns(ctx, id, CompanionState{PID: pids[0], Pane: "%999"}) {
		t.Error("a state naming another pane was accepted")
	}
	if b.owns(ctx, id, CompanionState{PID: 2147483646, Pane: pane}) {
		t.Error("a process that does not exist was accepted")
	}
	// A session that is not an omp session has no folders, and no reason.
	if w := b.roots(ctx, id); len(w.Roots) != 0 || w.Incomplete != "" {
		t.Errorf("a non-omp session's roots = %+v", w)
	}
}
