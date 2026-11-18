package surface

import (
	"context"
	"os"
	"path/filepath"
	"slices"

	"github.com/gettogo/togo/helper/internal/harness/procs"
	"github.com/gettogo/togo/helper/internal/session"
)

// Bounds on the additional workspace folders a companion reports.
const (
	MaxRoots      = 32
	MaxRootBytes  = 1024
	MaxRootsBytes = 8 << 10
)

// WorkspaceRoots are the additional workspace folders of a session's running
// program (omp's /add-dir). Incomplete, when not empty, says why the list is
// not all of them: files in the missing ones are not offered, and the caller
// must say so.
type WorkspaceRoots struct {
	Roots      []string
	Incomplete string
}

const (
	rootsTooMany     = "omp has more workspace folders than can be listed here. Files in some of them are not offered: type their path."
	rootsUnconfirmed = "omp's additional workspace folders could not be confirmed. Only files in this session's folder are offered: type other paths."
)

// rootsOf takes the folders a state lists that can be used: absolute, of
// bounded length, existing, each once, not the session's own folder. It
// reports false when the state says its list is partial or an entry had to
// be left out for its form or for the bounds.
func rootsOf(state CompanionState) (roots []string, whole bool) {
	whole = !state.RootsIncomplete
	seen := map[string]bool{}
	bytes := 0
	for _, r := range state.Roots {
		if !filepath.IsAbs(r) || len(r) > MaxRootBytes {
			whole = false
			continue
		}
		r = filepath.Clean(r)
		if seen[r] || r == filepath.Clean(state.Cwd) {
			continue
		}
		seen[r] = true
		if len(roots) >= MaxRoots || bytes+len(r) > MaxRootsBytes {
			whole = false
			continue
		}
		// A folder that has since been removed from disk holds no files:
		// leaving it out hides nothing.
		if st, err := os.Stat(r); err != nil || !st.IsDir() {
			continue
		}
		bytes += len(r)
		roots = append(roots, r)
	}
	return roots, whole
}

// Roots are the session's additional workspace folders. A program that does
// not report them (an older one, a launch without extensions) gives none and
// no reason: only the session's own folder was ever known for it.
func (s *Service) Roots(ctx context.Context, id string) WorkspaceRoots {
	managed, err := session.ValidateID(id)
	if err != nil || !managed || ctx.Err() != nil {
		return WorkspaceRoots{}
	}
	backend, ok := s.backend.(interface {
		roots(context.Context, string) WorkspaceRoots
	})
	if !ok {
		return WorkspaceRoots{}
	}
	return backend.roots(ctx, id)
}

func (b *paneBackend) roots(ctx context.Context, id string) WorkspaceRoots {
	sess, err := b.sessions.Resolve(ctx, id)
	if err != nil || !sess.Managed || sess.Agent != "omp" {
		return WorkspaceRoots{}
	}
	config, err := b.companions.Current(id)
	if err != nil {
		return WorkspaceRoots{}
	}
	state, err := b.companions.State(config)
	if err != nil || !slices.Contains(state.Features, "roots") {
		return WorkspaceRoots{}
	}
	// The state file says which process wrote it; that the process is this
	// session's program is checked against the session's own pane: a state
	// naming another live process or another pane gives no folders.
	if !b.owns(ctx, id, state) {
		if len(state.Roots) == 0 && !state.RootsIncomplete {
			return WorkspaceRoots{}
		}
		return WorkspaceRoots{Incomplete: rootsUnconfirmed}
	}
	roots, whole := rootsOf(state)
	out := WorkspaceRoots{Roots: roots}
	if !whole {
		out.Incomplete = rootsTooMany
	}
	return out
}

// owns reports whether the process a companion state names runs in the
// session's active pane.
func (b *paneBackend) owns(ctx context.Context, id string, state CompanionState) bool {
	if _, err := procs.Birth(state.PID); err != nil {
		return false
	}
	pane, err := b.sessions.Tmux.ActivePane(ctx, id)
	if err != nil || pane != state.Pane {
		return false
	}
	tree := (procs.Session{Panes: func(ctx context.Context) ([]int, error) {
		return b.sessions.Tmux.PanePIDs(ctx, id)
	}}).Tree(ctx)
	return slices.Contains(tree, state.PID)
}
