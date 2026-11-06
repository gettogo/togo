package session

import (
	"context"
	"errors"
	"fmt"

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
	if err := s.Tmux.RespawnPane(ctx, "="+id+":", dir, launchWrapper(s.shell(), s.TogoPath, launch)); err != nil {
		return Session{}, notFoundOr(err, id)
	}
	sess, err = s.find(ctx, id)
	if err == nil {
		s.remember(entryFor(sess))
	}
	return sess, err
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
	_, launch, err := ValidateLaunch(sess.Agent, program, argv[1:], s.loginPath(ctx))
	if err != nil {
		return Launch{}, err
	}
	if launch.Program != argv[0] {
		return Launch{}, &InvalidParamsError{Reason: wire.ReasonProgram, Msg: "the stored program does not match the session's agent"}
	}
	return launch, nil
}
