package approval

import (
	"context"
	"slices"
)

// questionWait is a blocking AskUserQuestion hook holding Claude's question
// dialog until the phone answers: the hook replies with the answers as the
// tool's updatedInput, so nothing is typed into the pane.
type questionWait struct {
	session string
	prompts []string
	ch      chan map[string]string
}

// WaitQuestion blocks until AnswerQuestion delivers answers for these
// question prompts in this session, or ctx ends (the hook timed out or was
// killed because the question was answered at the desk). A newer wait for
// the same questions replaces an older one, whose call returns false.
func (s *Store) WaitQuestion(ctx context.Context, sessionID string, prompts []string) (map[string]string, bool) {
	w := &questionWait{session: sessionID, prompts: slices.Clone(prompts), ch: make(chan map[string]string, 1)}
	s.mu.Lock()
	for id, old := range s.questions {
		if old.session == sessionID && slices.Equal(old.prompts, prompts) {
			close(old.ch)
			delete(s.questions, id)
		}
	}
	s.nextWait++
	id := s.nextWait
	s.questions[id] = w
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		if s.questions[id] == w {
			delete(s.questions, id)
		}
		s.mu.Unlock()
	}()
	select {
	case a, ok := <-w.ch:
		return a, ok
	case <-ctx.Done():
		return nil, false
	}
}

// AnswerQuestion hands answers, keyed by question prompt, to the hook
// waiting on exactly these prompts in this session. It reports false when
// no hook waits for them, so the caller answers another way.
func (s *Store) AnswerQuestion(sessionID string, prompts []string, answers map[string]string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, w := range s.questions {
		if w.session == sessionID && slices.Equal(w.prompts, prompts) {
			w.ch <- answers
			delete(s.questions, id)
			return true
		}
	}
	return false
}

// QuestionWaiting reports whether a hook holds a question in the session.
func (s *Store) QuestionWaiting(sessionID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.questionWaitingLocked(sessionID)
}

func (s *Store) questionWaitingLocked(sessionID string) bool {
	for _, w := range s.questions {
		if w.session == sessionID {
			return true
		}
	}
	return false
}
