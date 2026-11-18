package surface

import (
	"context"
	"sync"

	"github.com/gettogo/togo/helper/internal/harness/procs"
	"github.com/gettogo/togo/helper/internal/session"
)

// Held includes opening and releasing renderers: raw typing must not race a
// renderer transition. It does no disk or process work under the service lock.
func (s *Service) Held(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.runs[id] != nil
}

// Available checks the original runtime without acquiring a renderer, waiting
// for startup, pruning caches or changing the terminal's modes.
func (s *Service) Available(ctx context.Context, id string) bool {
	managed, err := session.ValidateID(id)
	if err != nil || !managed || ctx.Err() != nil {
		return false
	}
	backend, ok := s.backend.(interface {
		available(context.Context, string) bool
	})
	return ok && backend.available(ctx, id)
}

func (b *paneBackend) available(ctx context.Context, id string) bool {
	sess, err := b.sessions.Resolve(ctx, id)
	if err != nil || !sess.Managed || sess.Agent != "omp" {
		return false
	}
	config, err := b.companions.Current(id)
	if err != nil {
		return false
	}
	state, err := b.companions.State(config)
	if err != nil || !state.Available || state.Conversation == "" {
		return false
	}
	birth, err := procs.Birth(state.PID)
	if err != nil {
		return false
	}
	run := runtimeInfo{config: config, state: state, birth: birth, control: &sync.Mutex{}}
	_, err = b.validate(ctx, id, run)
	return err == nil
}

func (s *Service) reserve(ctx context.Context, id string, r *renderer) error {
	if s.BeforeOpen != nil {
		defer s.BeforeOpen(id)()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped || s.runs[id] != nil || len(s.runs) >= MaxRenderers {
		return errBusy
	}
	s.runs[id] = r
	return nil
}
