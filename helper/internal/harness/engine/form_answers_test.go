package engine

import (
	"reflect"
	"testing"

	"github.com/gettogo/togo/helper/internal/approval"
	"github.com/gettogo/togo/helper/internal/harness"
)

// An omp form says which questions are optional: one the phone leaves
// unanswered keeps what the dialog holds, while a required one, or one
// from a source that does not say, still needs an answer.
func TestKeyedAnswersKeepOptionalQuestions(t *testing.T) {
	yes, no := true, false
	q := &harness.QuestionBody{Questions: []harness.Question{
		{ID: "color", Prompt: "Which color?", Options: []harness.Choice{{ID: "Red", Label: "Red"}, {ID: "Green", Label: "Green"}}, Required: &yes},
		{ID: "notes", Prompt: "Notes", AllowText: true, Required: &no},
	}}
	keyed, _, _, err := keyedAnswers(q, map[string][]string{"color": {"Green"}})
	if err != nil {
		t.Fatal(err)
	}
	want := []approval.AskAnswer{{Prompt: "Which color?", Labels: []string{"Green"}}, {Prompt: "Notes", Keep: true}}
	if !reflect.DeepEqual(keyed, want) {
		t.Fatalf("keyed %+v\nwant %+v", keyed, want)
	}
	if _, _, _, err := keyedAnswers(q, map[string][]string{"notes": {"hi"}}); err == nil {
		t.Error("a required question left unanswered was taken")
	}
	q.Questions[1].Required = nil
	if _, _, _, err := keyedAnswers(q, map[string][]string{"color": {"Red"}}); err == nil {
		t.Error("a question that does not say it is optional was skipped")
	}
}
