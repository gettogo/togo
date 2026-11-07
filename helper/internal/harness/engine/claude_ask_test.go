package engine

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/gettogo/togo/helper/internal/approval"
	"github.com/gettogo/togo/helper/internal/harness"
)

// askApprovals records both Claude question routes.
type askApprovals struct {
	fakeApprovals
	hookWaits bool
	hook      map[string]string
	keyed     []approval.AskAnswer
	keyErr    error
}

func (a *askApprovals) AnswerQuestion(_ string, _ []string, answers map[string]string) bool {
	if !a.hookWaits {
		return false
	}
	a.hook = answers
	return true
}

func (a *askApprovals) AnswerAsk(_ context.Context, _ string, answers []approval.AskAnswer) error {
	a.keyed = answers
	return a.keyErr
}

var twoQuestions = harness.Item{ID: "toolu_1", Kind: harness.KindQuestion, Question: &harness.QuestionBody{Questions: []harness.Question{
	{ID: "q1", Prompt: "Which color?", AllowText: true, Options: []harness.Choice{{ID: "opt1", Label: "Red"}, {ID: "opt2", Label: "Blue"}}},
	{ID: "q2", Prompt: "Which fruits (pick any)", Multi: true, AllowText: true,
		Options: []harness.Choice{{ID: "opt1", Label: "Apple"}, {ID: "opt2", Label: "Pear"}, {ID: "opt3", Label: "Plum"}}},
}}}

func askEngine(t *testing.T, id string, ap *askApprovals) (*Engine, *sess) {
	t.Helper()
	e := newSeq(t, id, &seqPane{mode: "a"}, nil)
	e.cfg.Approvals = ap
	s, err := e.served(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	e.log.Append(id, []harness.Item{twoQuestions})
	return e, s
}

func TestClaudeQuestionHookGetsEveryAnswer(t *testing.T) {
	ap := &askApprovals{hookWaits: true}
	e, s := askEngine(t, "s_claudeask1", ap)
	res, err := e.answerClaudeQuestion(context.Background(), s, "toolu_1",
		map[string][]string{"q1": {"Teal\nish"}, "q2": {"opt1", "opt3"}})
	if err != nil || res.Result != harness.ResultApplied {
		t.Fatalf("res %+v err %v", res, err)
	}
	want := map[string]string{"Which color?": "Teal\nish", "Which fruits (pick any)": "Apple, Plum"}
	if !reflect.DeepEqual(ap.hook, want) || ap.keyed != nil {
		t.Fatalf("hook %v keyed %v", ap.hook, ap.keyed)
	}
}

func TestClaudeQuestionFallsBackToKeys(t *testing.T) {
	ap := &askApprovals{}
	e, s := askEngine(t, "s_claudeask2", ap)
	res, err := e.answerClaudeQuestion(context.Background(), s, "toolu_1",
		map[string][]string{"q1": {"opt2"}, "q2": {"opt2", "opt3"}})
	if err != nil || res.Result != harness.ResultApplied {
		t.Fatalf("res %+v err %v", res, err)
	}
	want := []approval.AskAnswer{{Prompt: "Which color?", Labels: []string{"Blue"}},
		{Prompt: "Which fruits (pick any)", Multi: true, Labels: []string{"Pear", "Plum"}}}
	if !reflect.DeepEqual(ap.keyed, want) {
		t.Fatalf("keyed %+v", ap.keyed)
	}
	ap.keyErr = &approval.AskError{Msg: "The terminal shows a different question; stopped there"}
	res, _ = e.answerClaudeQuestion(context.Background(), s, "toolu_1", map[string][]string{"q1": {"opt1"}, "q2": {"opt1"}})
	if res.Result != harness.ResultFailed || res.Message != ap.keyErr.Error() {
		t.Fatalf("res %+v, want the driver's message", res)
	}
}

func TestClaudeQuestionNeedsEveryQuestionAndOneSingleAnswer(t *testing.T) {
	e, s := askEngine(t, "s_claudeask3", &askApprovals{hookWaits: true})
	for _, a := range []map[string][]string{
		{"q1": {"opt1"}},
		{"q1": {"opt1", "opt2"}, "q2": {"opt1"}},
	} {
		if _, err := e.answerClaudeQuestion(context.Background(), s, "toolu_1", a); err == nil {
			t.Errorf("accepted %v", a)
		}
	}
}

func TestAnswersMayHoldNewlines(t *testing.T) {
	raw := map[string]json.RawMessage{"answers": json.RawMessage(`{"q1":["two\nlines\tok"]}`)}
	if _, err := checkArgs(raw); err != nil {
		t.Fatalf("answers with a newline refused: %v", err)
	}
	if _, err := checkArgs(map[string]json.RawMessage{"id": json.RawMessage(`"a\nb"`)}); err == nil {
		t.Fatal("a newline outside text and answers was accepted")
	}
}
