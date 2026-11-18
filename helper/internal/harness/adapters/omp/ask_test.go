package omp

import (
	"encoding/json"
	"fmt"
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

// Seen live on omp 18.20: the ask tool sent one HTML form, the terminal
// showed the "Audit color" field, and the chat had no question and needed
// nothing from the user. The records below are that session's.
func TestFormAskBecomesAQuestion(t *testing.T) {
	const call = `{"html": "<form><fieldset><legend>Audit color</legend><label><input type=\"radio\" name=\"color\" value=\"red\" checked>Red</label><label><input type=\"radio\" name=\"color\" value=\"yellow\">Yellow</label></fieldset><button type=\"submit\">Submit</button></form>", "i": "Audit color selection"}`
	var args map[string]any
	if err := json.Unmarshal([]byte(call), &args); err != nil {
		t.Fatal(err)
	}
	it := askQuestion("c1", "t1", "", t0, args)
	if it == nil || len(it.Question.Questions) != 1 {
		t.Fatalf("item = %+v, want one question", it)
	}
	q := it.Question.Questions[0]
	if q.ID != "color" || q.Prompt != "Audit color" || q.Multi || len(q.Options) != 2 || q.Options[0].Label != "Red" || q.Options[1].Label != "Yellow" {
		t.Fatalf("question = %+v", q)
	}
	if it.Question.Answerable == nil || *it.Question.Answerable {
		t.Error("the form is offered as answerable from the phone, which nothing can do yet")
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(`{"role":"toolResult","toolName":"ask","details":{"answers":[{"name":"color","question":"Audit color","answer":"Red"}]},"isError":false}`), &result); err != nil {
		t.Fatal(err)
	}
	if got := askAnswers(it.Question, result); len(got["color"]) != 1 || got["color"][0] != "Red" {
		t.Errorf("answers = %v, want color Red", got)
	}
	// Cancelled with Escape: an error result with no answers closes it empty.
	if err := json.Unmarshal([]byte(`{"role":"toolResult","toolName":"ask","content":[{"type":"text","text":"Ask tool was cancelled by the user"}],"details":{},"isError":true}`), &result); err != nil {
		t.Fatal(err)
	}
	if got := askAnswers(it.Question, result); len(got) != 1 || len(got["color"]) != 0 {
		t.Errorf("cancelled answers = %v, want an empty entry", got)
	}
}

func TestFormFieldsOfEveryKind(t *testing.T) {
	qs := formQuestions(`<form>
	  <label>Project name <input type="text" name="name" placeholder="my-app"></label>
	  <fieldset><legend>Targets &amp; platforms</legend>
	    <label><input type="checkbox" name="targets" value="ios">iOS</label>
	    <label><input type="checkbox" name="targets" value="mac">macOS</label></fieldset>
	  <label>Region <select name="region"><option value="eu">Europe</option><option value="us">United States</option></select></label>
	  <label>Notes <textarea name="notes"></textarea></label>
	  <input type="hidden" name="token" value="x"><button type="submit">Go</button></form>`)
	var got []string
	for _, q := range qs {
		got = append(got, fmt.Sprintf("%s|%s|multi=%v|text=%v|%d", q.ID, q.Prompt, q.Multi, q.AllowText, len(q.Options)))
	}
	want := []string{
		"name|Project name|multi=false|text=true|0",
		"targets|Targets & platforms|multi=true|text=false|2",
		"region|Region|multi=false|text=false|2",
		"notes|Notes|multi=false|text=true|0",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("fields:\n got %q\nwant %q", got, want)
	}
	if qs := formQuestions("not a form <<<"); len(qs) != 0 {
		t.Errorf("unreadable form gave %d questions", len(qs))
	}
}
