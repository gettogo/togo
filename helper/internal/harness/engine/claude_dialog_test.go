package engine

import (
	"testing"

	"github.com/gettogo/togo/helper/internal/detect"
	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/spec"
	"github.com/gettogo/togo/helper/internal/wire"
)

// claudeTwoQuestions is Claude Code's two-question AskUserQuestion as its
// adapter draws it from the transcript (a real 2.1.329 run).
func claudeTwoQuestions(answered map[string][]string) harness.Item {
	return harness.Item{ID: "toolu_ask", Kind: harness.KindQuestion, Question: &harness.QuestionBody{Questions: []harness.Question{
		{ID: "q1", Header: "Colour", Prompt: "Which colour do you prefer?", AllowText: true,
			Options: []harness.Choice{{ID: "opt1", Label: "Red"}, {ID: "opt2", Label: "Blue"}}},
		{ID: "q2", Header: "Fruit", Prompt: "Which fruit do you prefer?", AllowText: true,
			Options: []harness.Choice{{ID: "opt1", Label: "Apple"}, {ID: "opt2", Label: "Pear"}}},
	}, Answered: answered}}
}

func claudeAnswering(s *sess, builtin string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ad.Actions == nil {
		s.ad.Actions = map[string]spec.Action{}
	}
	s.ad.Actions[harness.ActionAnswer] = spec.Action{Routes: spec.Routes{Builtin: builtin}}
}

// While Claude Code's question item is open, every step of its dialog
// the watcher finds (each question, the review) is that item: the
// watcher opens no approval of its own for it. Before the item and after
// its answer, and for any other prompt, the watcher reports as before.
func TestClaudeQuestionDialogIsTheQuestionItem(t *testing.T) {
	const id = "s_dialog01"
	e, s := questionEngine(t, id, &fakeApprovals{})
	claudeAnswering(s, "claude_ask")
	step := detect.Prompt{Kind: detect.KindQuestion, Title: "Which colour do you prefer?", Ask: true}
	if e.QuestionDialog(id, step) {
		t.Fatal("a dialog with no question item was claimed")
	}
	s.mu.Lock()
	s.publishLocked([]harness.Item{claudeTwoQuestions(nil)}, nil)
	s.mu.Unlock()
	review := detect.Prompt{Title: "Review your answers", Ask: true}
	if !e.QuestionDialog(id, step) || !e.QuestionDialog(id, review) {
		t.Fatal("a step of the open question's dialog was not claimed")
	}
	if e.QuestionDialog(id, detect.Prompt{Kind: detect.KindPermission, Title: "Bash command"}) {
		t.Fatal("a permission prompt was claimed")
	}
	if e.QuestionDialog("s_other0001", step) {
		t.Fatal("another session's dialog was claimed")
	}
	s.mu.Lock()
	s.publishLocked([]harness.Item{claudeTwoQuestions(map[string][]string{"q1": {"opt1"}, "q2": {"opt2"}})}, nil)
	s.mu.Unlock()
	if e.QuestionDialog(id, step) {
		t.Fatal("the dialog was claimed after its question was answered")
	}
}

// Only Claude Code's question tool is paired this way: an agent whose
// dialogs are answered as screen prompts keeps them.
func TestOtherAgentsDialogsStayScreenPrompts(t *testing.T) {
	const id = "s_dialog02"
	e, s := questionEngine(t, id, &fakeApprovals{})
	claudeAnswering(s, "screen_answer")
	s.mu.Lock()
	s.publishLocked([]harness.Item{claudeTwoQuestions(nil)}, nil)
	s.mu.Unlock()
	if e.QuestionDialog(id, detect.Prompt{Title: "Which colour do you prefer?", Ask: true}) {
		t.Fatal("a screen_answer agent's dialog was claimed")
	}
}

// The watcher found the first step before the question item arrived, so
// the item was linked to that screen request. A later step unlinks it:
// the caller closes the request, and the item is offered on its own.
func TestPairedStepUnlinksAnEarlierScreenRequest(t *testing.T) {
	const id = "s_dialog03"
	ap := &fakeApprovals{pending: []wire.Approval{{RequestID: "rq_step1", Source: wire.SourceScreen,
		Title: "Which colour do you prefer?", Options: []wire.Option{{N: 1, Label: "Red"}, {N: 2, Label: "Blue"}}}}}
	e, s := questionEngine(t, id, ap)
	claudeAnswering(s, "claude_ask")
	s.mu.Lock()
	s.publishLocked([]harness.Item{claudeTwoQuestions(nil)}, nil)
	linked := s.needs.request["toolu_ask"]
	s.mu.Unlock()
	if linked != "rq_step1" {
		t.Fatalf("linked to %q", linked)
	}
	if !e.QuestionDialog(id, detect.Prompt{Title: "Which fruit do you prefer?", Ask: true}) {
		t.Fatal("the step was not claimed")
	}
	s.mu.Lock()
	linked = s.needs.request["toolu_ask"]
	s.mu.Unlock()
	if linked != "" {
		t.Fatalf("still linked to %q", linked)
	}
}
