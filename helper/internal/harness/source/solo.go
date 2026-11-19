package source

import (
	"context"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

// soloEvery is how long a Solo answer is reused: the answer costs a
// process listing.
const soloEvery = 10 * time.Second

// soloWait bounds one lookup.
const soloWait = 2 * time.Second

// Solo answers, for an agent whose files or rows name no process, whether
// a new conversation in the session's folder can only be this session's:
// no other process of the agent's kind runs there outside the session's
// process tree. Without a process view, or when the lookup fails, the
// answer is no: two agents in one folder must not share a guess. A Solo
// is not safe for concurrent use; its owner's lock guards it.
type Solo struct {
	at    time.Time
	alone bool
}

// Alone asks at most once per soloEvery.
func (s *Solo) Alone(env harness.Env) bool {
	if !s.at.IsZero() && time.Since(s.at) < soloEvery {
		return s.alone
	}
	s.at = time.Now()
	s.alone = alone(env)
	return s.alone
}

// alone is one Solo lookup.
func alone(env harness.Env) bool {
	if env.Procs == nil || env.Agent == "" || env.Cwd == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), soloWait)
	defer cancel()
	others, err := env.Procs.Others(ctx, []string{env.Agent}, env.Cwd)
	return err == nil && !others
}
