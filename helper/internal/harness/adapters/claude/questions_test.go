package claude

import (
	"reflect"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
)

// askUse is the spike's AskUserQuestion call (Claude Code 2.1.319).
const askUse = `{"type":"assistant","uuid":"a1","timestamp":"2026-11-07T10:00:00Z","message":{"id":"m1","role":"assistant","model":"claude-haiku-5-5","content":[{"type":"tool_use","id":"toolu_1","name":"AskUserQuestion","input":{"questions":[` +
	`{"question":"Which color do you prefer?","header":"Color","options":[{"label":"Red","description":"A warm color"},{"label":"Blue","description":"A cool color"}],"multiSelect":false},` +
	`{"question":"Which fruits do you like (pick any)","header":"Fruits","options":[{"label":"Apple","description":"Crisp"},{"label":"Pear","description":"Juicy"},{"label":"Plum","description":"Tart"}],"multiSelect":true}]}}]}}`

func askResult(result string, isError bool) string {
	errField := ""
	if isError {
		errField = `,"is_error":true`
	}
	return `{"type":"user","uuid":"u2","timestamp":"2026-11-07T10:00:09Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"The user answered."` + errField + `}]},"toolUseResult":` + result + `}`
}

func settled(t *testing.T, items []harness.Item) map[string][]string {
	t.Helper()
	if len(items) != 1 || items[0].ID != "toolu_1" || items[0].Question == nil {
		t.Fatalf("items %+v", items)
	}
	return items[0].Question.Answered
}

func TestQuestionSettlesFromToolResult(t *testing.T) {
	m := newTranscript(env())
	if items, _ := m.Map(rec(askUse)); len(items) != 1 || items[0].Question.Answered != nil {
		t.Fatalf("question = %+v", items)
	}
	items, _ := m.Map(rec(askResult(`{"answers":{"Which color do you prefer?":"Teal","Which fruits do you like (pick any)":"Apple, Plum"}}`, false)))
	want := map[string][]string{"q1": {"Teal"}, "q2": {"opt1", "opt3"}}
	if got := settled(t, items); !reflect.DeepEqual(got, want) {
		t.Fatalf("answered %v, want %v", got, want)
	}
}

func TestRejectedQuestionSettlesEmpty(t *testing.T) {
	m := newTranscript(env())
	m.Map(rec(askUse))
	items, _ := m.Map(rec(askResult(`"Error: User rejected tool use"`, true)))
	want := map[string][]string{"q1": {}, "q2": {}}
	if got := settled(t, items); !reflect.DeepEqual(got, want) {
		t.Fatalf("answered %v, want %v", got, want)
	}
}

func TestReplayedQuestionComesBackAnswered(t *testing.T) {
	// A daemon restart replays the transcript: the call and its result
	// arrive together, and the last copy of the item is the answered one.
	m := newTranscript(env())
	var last harness.Item
	for _, l := range []string{askUse, askResult(`{"answers":{"Which color do you prefer?":"Blue","Which fruits do you like (pick any)":"Pear"}}`, false)} {
		items, _ := m.Map(rec(l))
		for _, it := range items {
			if it.ID == "toolu_1" {
				last = it
			}
		}
	}
	if !reflect.DeepEqual(last.Question.Answered, map[string][]string{"q1": {"opt2"}, "q2": {"opt2"}}) {
		t.Fatalf("answered %v", last.Question.Answered)
	}
}
