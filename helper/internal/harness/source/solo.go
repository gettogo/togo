package source

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

// soloEvery is how long a "not alone" answer is reused before another
// lookup: each costs a process listing. An "alone" answer is never reused
// for a conversation seen after its lookup began.
const soloEvery = 10 * time.Second

// soloWait bounds one lookup.
const soloWait = 2 * time.Second

// Solo answers, for an agent whose files or rows name no process, whether
// a new conversation in the session's folder can only be this session's:
// no other process of the agent's kind (env.Agent, or one of Also, the
// programs that write the same store) runs there outside the session's
// process tree. Without a process view, or when the lookup fails, the
// answer is no: two agents in one folder must not share a guess.
//
// Lookups never run on the caller's goroutine (Alone), or run on one that
// holds no session lock (Check). A move may rest only on a lookup that
// began after the conversation it moves to was first seen: a second agent
// that writes that conversation was running by then, so the lookup saw
// it, unless it has already exited.
type Solo struct {
	// Also names other programs that write the agent's store.
	Also []string

	mu     sync.Mutex
	asking bool
	done   bool
	began  time.Time // when the last finished lookup began
	alone  bool
	logged bool
}

// Alone reports whether a lookup that began at or after since found the
// session alone. It never blocks: without such an answer it starts a
// lookup in the background, unless one runs or a "not alone" answer is
// younger than soloEvery, and reports no; wake, when set, is called once
// the lookup ends.
func (s *Solo) Alone(env harness.Env, since time.Time, wake func()) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.done && s.alone && !s.began.Before(since) {
		return true
	}
	if s.asking || (s.done && !s.alone && time.Since(s.began) < soloEvery) {
		return false
	}
	s.asking = true
	go func() {
		began := time.Now()
		ok := s.lookup(env)
		s.mu.Lock()
		s.asking, s.done, s.began, s.alone = false, true, began, ok
		s.mu.Unlock()
		if wake != nil {
			wake()
		}
	}()
	return false
}

// Check runs a lookup now, for a caller that holds no session lock.
func (s *Solo) Check(env harness.Env) bool {
	return s.lookup(env)
}

// lookup asks the process view once and logs, once per Solo, a lookup
// that could not be made.
func (s *Solo) lookup(env harness.Env) bool {
	if env.Procs == nil || env.Agent == "" || env.Cwd == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), soloWait)
	defer cancel()
	others, err := env.Procs.Others(ctx, append([]string{env.Agent}, s.Also...), env.Cwd)
	if err != nil {
		s.mu.Lock()
		first := !s.logged
		s.logged = true
		s.mu.Unlock()
		if first {
			log.Printf("harness: %s: cannot tell whether another %s runs in the session's folder (%v); a conversation it starts by itself is not followed", env.SessionID, env.Agent, err)
		}
		return false
	}
	return !others
}
