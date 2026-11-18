package session

import (
	"context"
	"errors"
)

func (s *Service) prepareRun(ctx context.Context, id, agent, dir string, launch Launch) Launch {
	run := launch.WithEndpoint(agent, id, dir)
	if s.PrepareLaunch == nil {
		return run
	}
	next, err := s.PrepareLaunch(ctx, id, agent, run)
	if err == nil && next.Program != run.Program {
		err = errors.New("integration changed the session program")
	}
	if err == nil {
		err = validateArgs(next.Args)
	}
	if err != nil {
		if s.Logf != nil {
			s.Logf("session %s: optional launch integration unavailable: %v", id, err)
		}
		return run
	}
	return next
}
