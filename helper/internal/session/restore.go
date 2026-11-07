package session

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/gettogo/togo/helper/internal/agents"
	"github.com/gettogo/togo/helper/internal/fsx"
	"github.com/gettogo/togo/helper/internal/tmux"
	"github.com/gettogo/togo/helper/internal/wire"
)

const (
	// restorableFor is how long a session a restart ended stays on offer.
	restorableFor = 7 * 24 * time.Hour
	// serverStartSlack absorbs the second boundary between a session's
	// creation time and the start of the server its own creation started.
	serverStartSlack = 10 * time.Second
)

// verdict is what became of a recorded session.
type verdict int

const (
	running    verdict = iota // its tmux session exists
	restorable                // a restart ended it
	gone                      // ended at the desk, or on offer too long
)

// judged is a recorded session with its verdict and, when restorable, the
// time it ended.
type judged struct {
	entry   ManifestEntry
	verdict verdict
	endedAt time.Time
}

// Restorable returns the sessions a host or tmux server restart ended,
// newest first. A recorded session whose tmux session is missing under a
// server that outlived it was ended at the desk; it, and one on offer for
// more than seven days, is dropped from the record.
func (s *Service) Restorable(ctx context.Context) ([]wire.RestorableSession, error) {
	if s.Manifest == nil {
		return nil, nil
	}
	if err := s.checkTmux(ctx); err != nil {
		return nil, err
	}
	verdicts, err := s.judge(ctx)
	if err != nil {
		return nil, err
	}
	out := []wire.RestorableSession{}
	for _, j := range verdicts {
		if j.verdict != restorable {
			continue
		}
		e := j.entry
		out = append(out, wire.RestorableSession{
			ID: e.ID, Bib: e.Bib, Name: e.Name, Agent: e.Agent, Command: e.Command, Cwd: e.Cwd,
			CreatedAt: e.CreatedAt.UTC(), EndedAt: j.endedAt.UTC(), Resumable: resumable(e.Agent, e.Command),
		})
	}
	slices.SortFunc(out, func(a, b wire.RestorableSession) int {
		return cmp.Or(b.EndedAt.Compare(a.EndedAt), cmp.Compare(a.Bib, b.Bib))
	})
	return out, nil
}

// Restore starts a session a restart ended again: the same id, name,
// agent, command and folder, and its bib when no running session holds it,
// else the lowest free one. resume appends the agent's Resume arguments
// when it has them; the recorded command stays as the user gave it. A
// folder that is gone wraps fsx.ErrInvalidPath; an agent no longer
// installed is an *InvalidParamsError with Reason wire.ReasonNotInstalled.
func (s *Service) Restore(ctx context.Context, id string, resume bool) (Session, error) {
	if err := requireManaged(id); err != nil {
		return Session{}, err
	}
	if err := s.checkTmux(ctx); err != nil {
		return Session{}, err
	}
	if s.Manifest == nil {
		return Session{}, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	var (
		e       ManifestEntry
		bib     string
		dir     string
		launch  Launch
		created = s.now().UTC().Truncate(time.Second)
	)
	err := withLock(ctx, s.LockFile, func() error {
		var err error
		if e, err = s.restorableEntry(ctx, id); err != nil {
			return err
		}
		if dir, err = fsx.ValidateDir(e.Cwd); err != nil {
			return err
		}
		if launch, err = s.restoreCommand(ctx, e, resume); err != nil {
			return err
		}
		infos, err := s.Tmux.ListSessions(ctx)
		if err != nil {
			return err
		}
		used := make(map[string]bool, len(infos))
		for _, info := range infos {
			used[info.Bib] = true
		}
		if bib = e.Bib; used[bib] {
			if bib, err = lowestFreeBib(used); err != nil {
				return err
			}
		}
		options := map[string]string{optionAgent: e.Agent, optionName: e.Name, optionBib: bib, optionCreated: created.Format(time.RFC3339)}
		if e.Command != nil {
			if options[optionArgv], err = encodeArgv(e.Command); err != nil {
				return err
			}
		}
		run := launch.WithEndpoint(e.Agent, id)
		err = s.Tmux.NewSession(ctx, tmux.NewSessionOpts{
			Name: id, Dir: dir, Cols: defaultCols, Rows: defaultRows,
			Command: launchWrapper(s.shell(), s.TogoPath, run),
			Options: options,
			Env:     withEnv(s.sessionEnv(id, bib, e.Agent), run.Env),
		})
		if err != nil {
			return err
		}
		s.remember(ManifestEntry{ID: id, Bib: bib, Name: e.Name, Agent: e.Agent, Cwd: dir, Command: e.Command, CreatedAt: created})
		return nil
	})
	if err != nil {
		return Session{}, err
	}
	return s.started(ctx, id, bib, e.Name, e.Agent, dir, created, e.Command)
}

// Dismiss drops a restorable session from the record without running it.
func (s *Service) Dismiss(ctx context.Context, id string) error {
	if err := requireManaged(id); err != nil {
		return err
	}
	if err := s.checkTmux(ctx); err != nil {
		return err
	}
	if s.Manifest == nil {
		return fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	return withLock(ctx, s.LockFile, func() error {
		if _, err := s.restorableEntry(ctx, id); err != nil {
			return err
		}
		return s.Manifest.Remove(id)
	})
}

// restorableEntry returns id's record when it is restorable, else
// ErrNotFound.
func (s *Service) restorableEntry(ctx context.Context, id string) (ManifestEntry, error) {
	verdicts, err := s.judge(ctx)
	if err != nil {
		return ManifestEntry{}, err
	}
	for _, j := range verdicts {
		if j.entry.ID == id && j.verdict == restorable {
			return j.entry, nil
		}
	}
	return ManifestEntry{}, fmt.Errorf("%w: %s", ErrNotFound, id)
}

// restoreCommand validates the recorded command as session.create would
// and adds the agent's Resume arguments when asked and resumable allows;
// otherwise the recorded command runs unchanged.
func (s *Service) restoreCommand(ctx context.Context, e ManifestEntry, resume bool) (Launch, error) {
	if e.Command == nil {
		_, launch, err := ValidateLaunch(e.Agent, "", nil, s.loginPath(ctx), s.fallbackDirs())
		return launch, err
	}
	launch, err := s.relaunchCommand(ctx, Session{Agent: e.Agent, Command: e.Command})
	if err != nil {
		return Launch{}, err
	}
	if resume && resumable(e.Agent, e.Command) {
		a, _ := agents.Lookup(e.Agent)
		launch.Args = append(launch.Args, a.Resume...)
	}
	return launch, nil
}

// resumable reports whether the agent's Resume arguments can follow argv's
// arguments: the agent has them, the arguments do not already hold them,
// and none is positional, since a prompt would be sent again or take the
// resume arguments as text. "--" ends the flags, so it counts as
// positional. A word not starting with "-" is taken as a flag's value only
// directly after a flag without "="; that cannot tell a boolean flag from
// one with a value, so "--yes word" passes.
func resumable(agentID string, argv []string) bool {
	a, ok := agents.Lookup(agentID)
	if !ok || len(a.Resume) == 0 || len(argv) == 0 {
		return false
	}
	afterFlag := false
	for _, arg := range argv[1:] {
		switch {
		case arg == a.Resume[0]:
			return false
		case len(arg) > 1 && arg[0] == '-' && arg != "--":
			afterFlag = !strings.Contains(arg, "=")
		case afterFlag:
			afterFlag = false
		default:
			return false
		}
	}
	return true
}

// Settle drops the record of sessions that ended while the tmux server
// stayed up, and notes when restorable ones were first found gone. The
// watcher runs it when it sees sessions vanish, so a session ended at the
// desk is never offered after a later restart.
func (s *Service) Settle(ctx context.Context) error {
	if s.Manifest == nil {
		return nil
	}
	if err := s.checkTmux(ctx); err != nil {
		return err
	}
	_, err := s.judge(ctx)
	return err
}

// judge reads the record, then the sessions and server on the togo socket,
// and decides each entry. It records when a restorable entry was first
// found gone and drops the gone ones. The record is read before tmux, so
// an entry written meanwhile, whose session exists by then, is left alone;
// the write-back only touches entries still as they were read.
func (s *Service) judge(ctx context.Context) ([]judged, error) {
	entries, err := s.Manifest.All()
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, nil
	}
	infos, err := s.Tmux.ListSessions(ctx)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	started, serverUp, err := s.Tmux.ServerStarted(ctx)
	if err != nil {
		return nil, fmt.Errorf("tmux server: %w", err)
	}
	live := make(map[string]bool, len(infos))
	for _, info := range infos {
		live[info.Name] = true
	}
	now := s.now().UTC().Truncate(time.Second)
	out := make([]judged, 0, len(entries))
	changed := map[string]judged{}
	for _, e := range entries {
		j := judged{entry: e, verdict: running}
		switch {
		case live[e.ID]:
		case serverUp && !started.After(e.CreatedAt.Add(serverStartSlack)):
			j.verdict = gone
		default:
			j.verdict, j.endedAt = restorable, e.EndedAt
			if j.endedAt.IsZero() {
				j.endedAt = now
				if serverUp {
					j.endedAt = started.UTC()
				}
			}
			if now.Sub(j.endedAt) > restorableFor {
				j.verdict = gone
			}
		}
		if j.verdict == gone || (j.verdict == restorable && e.EndedAt.IsZero()) {
			changed[e.ID] = j
		}
		out = append(out, j)
	}
	if len(changed) > 0 {
		s.settle(changed)
	}
	return out, nil
}

// settle writes judge's findings back: gone entries leave the record and
// restorable ones keep the time they ended.
func (s *Service) settle(changed map[string]judged) {
	err := s.Manifest.update(func(list []ManifestEntry) []ManifestEntry {
		out := list[:0]
		for _, e := range list {
			j, ok := changed[e.ID]
			if !ok || !e.CreatedAt.Equal(j.entry.CreatedAt) {
				out = append(out, e)
				continue
			}
			if j.verdict == restorable {
				e.EndedAt = j.endedAt
				out = append(out, e)
			}
		}
		return out
	})
	if err != nil {
		s.logf("session records: %v", err)
	}
}
