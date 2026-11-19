package sessions

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/coverage"
	"github.com/gettogo/togo/helper/internal/harness/e2e/support"
	"github.com/gettogo/togo/helper/internal/harness/enginetest"
	"github.com/gettogo/togo/helper/internal/harness/sessiongen"
)

// omp's ask dialog as captured on 18.16.0 (detect/testdata/omp): the
// question of ask.jsonl with the cursor on the first option, and the screen
// after one Down.
const (
	ompAskScreen = "detect/testdata/omp/real-ask-single.txt"
	ompAskMoved  = "detect/testdata/omp/real-ask-single-moved.txt"
)

func ompNextFocus() map[int]sessiongen.OMPTurn {
	return map[int]sessiongen.OMPTurn{1: {Ask: &sessiongen.OMPAsk{ID: "next_focus", Header: "Focus", Question: "What should I work on next?",
		Options: []string{"Bug fixes", "New feature", "Refactoring"}}}}
}

// dialogChoice is the option the cursor is on in omp's ask dialog.
func dialogChoice(d *enginetest.Dialog) string {
	for _, row := range d.Screen.Rows {
		row = enginetest.Plain(row)
		if _, rest, ok := strings.Cut(row, "❯ ○ "); ok {
			rest = strings.TrimRight(strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(rest), "│")), " ")
			return strings.TrimSpace(strings.TrimSuffix(rest, "(Recommended)"))
		}
	}
	return ""
}

// A question arrives with its options and needs the user; the phone
// answers it; the keys that reach omp's dialog move to the chosen option
// and take it; omp's record of that answer closes the question.
func TestOMPQuestionAnsweredFromThePhone(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "omp/capability/needs.questions")
	a := newOMPAgent(t, ompNextFocus(), sessiongen.Turn{Say: 30})
	r := a.r
	moved := r.Term.MustCapture(ompAskMoved)
	r.Term.Open(&enginetest.Dialog{Screen: r.Term.MustCapture(ompAskScreen), Next: map[string]enginetest.Screen{"Down": moved}})
	var took string
	r.Term.OnKey = func(d *enginetest.Dialog, key string, _ bool) bool {
		if key == "Enter" && took == "" {
			// omp takes the option under the cursor and records the answer.
			took = dialogChoice(d)
			if err := a.o.Answer(a.l, took); err != nil {
				t.Error(err)
			}
		}
		return false
	}
	r.Look()
	st := r.SettleUntil(support.FileLimit, func(st enginetest.State) bool {
		return len(support.Items(st, support.OfKind(harness.KindQuestion))) == 1 && st.Mission.Summary.NeedsYou > 0
	})
	q := support.Items(st, support.OfKind(harness.KindQuestion))[0]
	qu := q.Question.Questions[0]
	var labels []string
	for _, o := range qu.Options {
		labels = append(labels, o.Label)
	}
	if len(q.Question.Answered) != 0 || q.Question.Answerable != nil || qu.Prompt != "What should I work on next?" ||
		strings.Join(labels, "|") != "Bug fixes|New feature|Refactoring" {
		t.Fatalf("the open question: %+v", q.Question)
	}

	res, err := r.Act("answer", q.ID, map[string]any{"answers": map[string][]string{"next_focus": {"New feature"}}})
	if err != nil || res.Result != harness.ResultApplied {
		t.Fatalf("answer: %+v %v", res, err)
	}
	if got := r.Term.KeysSent(); strings.Join(got, " ") != "Down Enter" {
		t.Errorf("keys sent to omp's dialog %v, want Down Enter", got)
	}
	if took != "New feature" || r.Term.Dialog() != nil {
		t.Errorf("omp took %q and the dialog is up: %v", took, r.Term.Dialog() != nil)
	}
	st = r.SettleUntil(support.FileLimit, func(st enginetest.State) bool {
		qs := support.Items(st, support.OfKind(harness.KindQuestion))
		return len(qs) == 1 && len(qs[0].Question.Answered) > 0 && enginetest.Has(a.l.Truth.Last)(st)
	})
	if got := support.Items(st, support.OfKind(harness.KindQuestion))[0].Question.Answered["next_focus"]; strings.Join(got, "+") != "New feature" {
		t.Errorf("answered %v, want New feature", got)
	}
	// The screen watcher sees the dialog gone and closes what it opened.
	r.Look()
	r.WaitPending(0, 3*time.Second)
	if st.Mission.Summary.NeedsYou != 0 || len(r.Pending()) != 0 {
		t.Errorf("summary %+v and %d approvals open after the answer", st.Mission.Summary, len(r.Pending()))
	}
	// A second answer to the closed question is refused.
	if res, _ := r.Act("answer", q.ID, map[string]any{"answers": map[string][]string{"next_focus": {"Refactoring"}}}); res.Result == harness.ResultApplied {
		t.Errorf("a second answer was applied: %+v", res)
	}
}

// omp 18.20's ask takes an HTML form (adapters/omp/ask_test.go has the
// call as omp wrote it). Its dialog is not the list the key driver reads.
//
// TestKnownFault_OMP_FormQuestionHasNoAnswerAction documents the gap seen
// in a live run: the question shows with its options but is marked
// unanswerable (adapters/omp/ask.go askQuestion), and the phone's answer
// action does not take it, so the user must go to the terminal.
//
// Correct: the phone answers a form question like any other, with keys
// for its fields. This test passes while the gap is there; when it fails
// the gap is closed: claim needs.questions for forms and delete it.
func TestKnownFault_OMP_FormQuestionHasNoAnswerAction(t *testing.T) {
	t.Parallel()
	a := newOMPAgent(t, nil, sessiongen.Turn{Say: 30})
	r := a.r
	st := r.SettleUntil(support.FileLimit, enginetest.Has(a.l.Truth.Last))
	const form = `{"html": "<form><fieldset><legend>Audit color</legend><label><input type=\"radio\" name=\"color\" value=\"red\" checked>Red</label><label><input type=\"radio\" name=\"color\" value=\"yellow\">Yellow</label></fieldset><button type=\"submit\">Submit</button></form>", "i": "Audit color selection"}`
	line := `{"type":"message","id":"formask1","parentId":"` + a.l.Truth.Last + `","timestamp":"` + time.Now().UTC().Format("2006-01-02T15:04:05.000Z") +
		`","message":{"role":"assistant","content":[{"type":"toolCall","id":"toolu_form1","name":"ask","arguments":` + form + `}],"provider":"llama.cpp","model":"qwen3.8-27b","stopReason":"toolUse"}}` + "\n"
	f, err := os.OpenFile(a.l.Main(), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(line); err != nil {
		t.Fatal(err)
	}
	f.Close()
	st = r.SettleUntil(support.FileLimit, func(st enginetest.State) bool {
		return len(support.Items(st, support.OfKind(harness.KindQuestion))) == 1
	})
	q := support.Items(st, support.OfKind(harness.KindQuestion))[0]
	if q.Question.Answerable == nil || *q.Question.Answerable || q.Question.Questions[0].ID != "color" {
		t.Fatalf("the form question: %+v", q.Question)
	}
	if res, _ := r.Act("answer", q.ID, map[string]any{"answers": map[string][]string{"color": {"Yellow"}}}); res.Result == harness.ResultApplied {
		t.Fatalf("the gap is closed: the answer was %+v", res)
	}
	_ = filepath.Join
}
