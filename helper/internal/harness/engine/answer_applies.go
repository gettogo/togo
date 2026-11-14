package engine

import "github.com/gettogo/togo/helper/internal/harness"

// dropInapplicable removes, from an answer action's args, the answers to
// questions of the target item that do not apply given the other answers
// (harness.Question.When). An app that does not know conditions shows
// every question and answers each; a harness refuses a reply that answers
// a field that is not active, even with an empty answer, so the helper
// leaves those out whichever app sent them. A question that depends on one
// left out goes with it. Items without conditions, and answers that name
// no question, are not touched.
func (s *sess) dropInapplicable(log *Log, target string, args map[string]any) {
	raw, ok := args["answers"].(map[string]any)
	if !ok {
		return
	}
	s.mu.Lock()
	it, found := log.Get(s.id, target)
	s.mu.Unlock()
	if !found || it.Question == nil {
		return
	}
	qs := it.Question.Questions
	answers := answerArgs(args)
	// A condition names an earlier question, so one pass in order is
	// enough for a well-formed form; the loop also settles any other.
	for range qs {
		dropped := false
		for _, q := range qs {
			if _, has := raw[q.ID]; has && !q.Applies(answers) {
				delete(raw, q.ID)
				delete(answers, q.ID)
				dropped = true
			}
		}
		if !dropped {
			return
		}
	}
}

// checkAnswers bounds the answers arg of an answer action: one answer per
// question of an item, so as many keys as an item has questions, each
// value held to what any text arg is.
func checkAnswers(answers map[string]any) error {
	if len(answers) > harness.MaxQuestions {
		return invalidArgs("too many answers")
	}
	for k, v := range answers {
		if err := checkArgValue(k, v, true); err != nil {
			return err
		}
	}
	return nil
}
