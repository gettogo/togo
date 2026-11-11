package omp

import (
	"reflect"
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
)

// ask.jsonl is a real omp 18.16.0 session (scrubbed) with three ask calls:
// one single-choice question answered at the terminal, a batch of a
// multi-select and a single choice answered with typed words, and one
// cancelled with Escape.
func TestAskGolden(t *testing.T) {
	out := run(newSession(env()), records(t, "ask.jsonl", "session"))
	golden(t, "ask.golden.json", out)
}

// The ask call is a question item with the agent's ids, labels and
// descriptions, open until its result; the tool row stays beside it.
func TestAskCallOpensAQuestion(t *testing.T) {
	recs := records(t, "ask.jsonl", "session")
	s := newSession(env())
	var first *harness.Item
	for _, r := range recs {
		items, _ := s.Map(r)
		for i := range items {
			if items[i].Kind == harness.KindQuestion && first == nil {
				first = &items[i]
			}
		}
		if first != nil {
			break
		}
	}
	if first == nil {
		t.Fatal("no question item")
	}
	q := first.Question
	if first.ID != "ask-toolu_013TshyaqpekAzvP8VHASrFv" || len(q.Answered) != 0 || len(q.Questions) != 1 || q.Answerable != nil {
		t.Fatalf("%+v", first)
	}
	qu := q.Questions[0]
	var labels []string
	for _, o := range qu.Options {
		labels = append(labels, o.ID+"="+o.Label)
	}
	if qu.ID != "next_focus" || qu.Header != "Focus" || qu.Prompt != "What should I work on next?" || qu.Multi || !qu.AllowText ||
		strings.Join(labels, "|") != "Bug fixes=Bug fixes|New feature=New feature|Refactoring=Refactoring" ||
		!strings.HasPrefix(qu.Options[0].Detail, "Find and fix") {
		t.Fatalf("%+v", qu)
	}
	if first.TurnID == "" {
		t.Fatal("no turn")
	}
}

// The results close the items: the label picked, the boxes ticked and the
// words typed per question id, and every question empty when cancelled.
func TestAskResultsAnswerTheQuestions(t *testing.T) {
	out := run(newSession(env()), records(t, "ask.jsonl", "session"))
	last := out.last()
	single := last["ask-toolu_013TshyaqpekAzvP8VHASrFv"].Question.Answered
	batch := last["ask-toolu_01SGwQThmPS1cXSQZqffFwhS"].Question.Answered
	cancelled := last["ask-toolu_017PfFtnnJaaaQmSUvKwjHLk"].Question.Answered
	if !reflect.DeepEqual(single, map[string][]string{"next_focus": {"New feature"}}) {
		t.Fatalf("single %v", single)
	}
	if !reflect.DeepEqual(batch, map[string][]string{"feature_areas": {"Backend", "Frontend"}, "rollout": {"Canary to 5 percent first"}}) {
		t.Fatalf("batch %v", batch)
	}
	if !reflect.DeepEqual(cancelled, map[string][]string{"canary_auto_rollback": {}}) {
		t.Fatalf("cancelled %v", cancelled)
	}
	for _, it := range out.Items {
		if it.Kind == harness.KindTool && it.Tool.Name == "ask" && it.Tool.Ended != nil && !strings.HasPrefix(it.Tool.Title, "Ask") {
			t.Fatalf("tool row %+v", it.Tool)
		}
	}
}

// A turn that ends without the result (interrupted) closes the question.
func TestInterruptedAskCloses(t *testing.T) {
	s := newSession(env())
	recs := records(t, "ask.jsonl", "session")
	var closed *harness.Item
	for _, r := range recs {
		data := string(r.Data)
		if strings.Contains(data, `"toolCallId": "toolu_013TshyaqpekAzvP8VHASrFv"`) && strings.Contains(data, `"role": "toolResult"`) {
			// Instead of the result: the model's turn aborted.
			r.Data = []byte(`{"type":"message","id":"abort1","parentId":"23abaf26","timestamp":"2026-11-10T18:01:00.000Z","message":{"role":"assistant","content":[],"stopReason":"aborted"}}`)
		}
		items, _ := s.Map(r)
		for i := range items {
			if items[i].ID == "ask-toolu_013TshyaqpekAzvP8VHASrFv" && len(items[i].Question.Answered) > 0 {
				closed = &items[i]
			}
		}
		if closed != nil {
			break
		}
	}
	if closed == nil || !reflect.DeepEqual(closed.Question.Answered, map[string][]string{"next_focus": {}}) {
		t.Fatalf("%+v", closed)
	}
}

// The yaml declares the question capability and the key-driven answer.
func TestAdapterAnswersAskWithKeys(t *testing.T) {
	a := string(AdapterFile())
	if !strings.Contains(a, "needs.questions") || !strings.Contains(a, "builtin: screen_answer") {
		t.Fatal("omp.yaml lacks needs.questions or the screen_answer answer action")
	}
}
