package engine

import (
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
)

// The engine marks a question the phone cannot answer on its own copy:
// the item a mapper or a caller published, and may keep, is not changed.
// Changing it made a package-level question in one test unanswerable for
// every later run (TestEveryOpenQuestionIsOffered under -count).
func TestUnanswerableMarkLeavesThePublishedQuestionAlone(t *testing.T) {
	_, s := questionEngine(t, "s_perm09", &fakeApprovals{})
	q := harness.QuestionBody{Questions: []harness.Question{{ID: "c", Prompt: "Which?",
		Options: []harness.Choice{{ID: "a", Label: "A"}}}}}
	s.mu.Lock()
	delete(s.ad.Actions, harness.ActionAnswer)
	s.publishLocked([]harness.Item{{ID: "q9", Kind: harness.KindQuestion, Question: &q}}, nil)
	stored := s.needs.open["q9"]
	s.mu.Unlock()
	if q.Answerable != nil {
		t.Fatal("the published question was changed")
	}
	if stored.Answerable == nil || *stored.Answerable {
		t.Fatal("the stored question is not marked unanswerable")
	}
}
