package session

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"slices"

	"github.com/gettogo/togo/helper/internal/agents"
	"github.com/gettogo/togo/helper/internal/fsx"
	"github.com/gettogo/togo/helper/internal/wire"
)

// ErrRunning is returned by Relaunch while the session's agent still runs.
var ErrRunning = errors.New("session: the agent is still running")

// Relaunch runs a managed session's agent again in its pane, keeping the
// session's id, bib, name, environment and current folder. Only a pane
// that shows the agent gone (see AgentGone) is started again, whatever
// status the session reads: a live agent, even one stopped at a usage
// limit, is never killed, and is ErrRunning. The command is the session's
// stored argv, or the registry command for sessions created before argv
// was stored, and is validated again as session.create would before it
// runs.
func (s *Service) Relaunch(ctx context.Context, id string) (Session, error) {
	if err := requireManaged(id); err != nil {
		return Session{}, err
	}
	sess, err := s.find(ctx, id)
	if err != nil {
		return Session{}, err
	}
	gone, err := s.AgentGone(ctx, id)
	if err != nil {
		return Session{}, err
	}
	if !gone {
		return Session{}, fmt.Errorf("%w: %s", ErrRunning, id)
	}
	launch, err := s.relaunchCommand(ctx, sess)
	if err != nil {
		return Session{}, err
	}
	dir, err := fsx.ValidateDir(sess.Cwd)
	if err != nil {
		return Session{}, err
	}
	launch = launch.WithEndpoint(sess.Agent, id)
	if err := s.setRunEnv(ctx, id, launch.Env); err != nil {
		return Session{}, notFoundOr(err, id)
	}
	if err := s.Tmux.RespawnPane(ctx, "="+id+":", dir, launchWrapper(s.shell(), s.TogoPath, launch)); err != nil {
		return Session{}, notFoundOr(err, id)
	}
	sess, err = s.find(ctx, id)
	if err == nil {
		s.remember(entryFor(sess))
	}
	return sess, err
}

// RelaunchWith restarts a managed session's agent in its pane with argv,
// stopping the run in progress: a harness adapter uses it to change a flag
// the agent reads only at start (a model) or to resume a conversation.
// argv[0] must be the program the session runs, named by its base name or
// by the stored path; the arguments are checked as session.create checks
// them. The session's stored argv is kept, so a later Relaunch or restore
// starts the agent as the user created it.
func (s *Service) RelaunchWith(ctx context.Context, id string, argv []string) error {
	if err := requireManaged(id); err != nil {
		return err
	}
	if len(argv) == 0 {
		return &InvalidParamsError{Reason: wire.ReasonArgs, Msg: "the command is empty"}
	}
	sess, err := s.find(ctx, id)
	if err != nil {
		return err
	}
	stored := sess.Command
	if stored == nil {
		if a, ok := agents.Lookup(sess.Agent); ok && a.Command != "" {
			stored = []string{a.Command}
		}
	}
	if len(stored) == 0 || filepath.Base(argv[0]) != filepath.Base(stored[0]) || (filepath.IsAbs(argv[0]) && argv[0] != stored[0]) {
		return &InvalidParamsError{Reason: wire.ReasonProgram, Msg: "only the session's own program can be restarted"}
	}
	run := append([]string{stored[0]}, argv[1:]...)
	launch, err := s.relaunchCommand(ctx, Session{Agent: sess.Agent, Command: run})
	if err != nil {
		return err
	}
	dir, err := fsx.ValidateDir(sess.Cwd)
	if err != nil {
		return err
	}
	launch = launch.WithEndpoint(sess.Agent, id)
	if err := s.setRunEnv(ctx, id, launch.Env); err != nil {
		return notFoundOr(err, id)
	}
	if err := s.Tmux.RespawnPane(ctx, "="+id+":", dir, launchWrapper(s.shell(), s.TogoPath, launch)); err != nil {
		return notFoundOr(err, id)
	}
	return nil
}

// relaunchCommand returns the validated Launch for sess.
func (s *Service) relaunchCommand(ctx context.Context, sess Session) (Launch, error) {
	argv := sess.Command
	if argv == nil {
		if a, ok := agents.Lookup(sess.Agent); ok && a.Command != "" {
			argv = []string{a.Command}
		}
	}
	if len(argv) == 0 {
		return Launch{}, &InvalidParamsError{Msg: "the session has no agent to run again"}
	}
	program := ""
	if sess.Agent == wire.AgentCustom {
		program = argv[0]
	}
	_, launch, err := ValidateLaunch(sess.Agent, program, argv[1:], s.loginPath(ctx), s.fallbackDirs())
	if err != nil {
		return Launch{}, err
	}
	if !storedProgramMatches(sess.Agent, argv[0], launch.Program) {
		return Launch{}, &InvalidParamsError{Reason: wire.ReasonProgram, Msg: "the stored program does not match the session's agent"}
	}
	return launch, nil
}

// storedProgramMatches reports whether a session's recorded program is the
// one the agent resolves to now. A known agent may be recorded by bare name
// ("claude") and resolve to a full path when the login shell's PATH was not
// available and a common install folder supplied it, or the other way round.
func storedProgramMatches(agent, stored, resolved string) bool {
	if stored == resolved {
		return true
	}
	if agent == wire.AgentCustom {
		return false
	}
	return filepath.Base(stored) == filepath.Base(resolved)
}

// setRunEnv puts a run's extra environment (an endpoint's server password)
// into the session's environment, which a respawned pane starts with.
func (s *Service) setRunEnv(ctx context.Context, id string, env map[string]string) error {
	for _, k := range slices.Sorted(maps.Keys(env)) {
		if err := s.Tmux.SetEnvironment(ctx, id, k, env[k]); err != nil {
			return err
		}
	}
	return nil
}

// withEnv returns base with extra added.
func withEnv(base, extra map[string]string) map[string]string {
	for k, v := range extra {
		base[k] = v
	}
	return base
}
