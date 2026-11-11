package hookstyle

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

// A stub ask tool: input {"q": "...", "choices": [...]}, response {"answer": "..."}.
func stubAsk(tool string, input json.RawMessage) (*harness.QuestionBody, bool) {
	if tool != "ask" {
		return nil, false
	}
	var in struct {
		Q       string   `json:"q"`
		Choices []string `json:"choices"`
	}
	if json.Unmarshal(input, &in) != nil {
		return nil, false
	}
	q := harness.Question{ID: "0", Prompt: in.Q, AllowText: true}
	for _, c := range in.Choices {
		q.Options = append(q.Options, harness.Choice{ID: c, Label: c})
	}
	return &harness.QuestionBody{Questions: []harness.Question{q}}, true
}

func stubAnswered(tool string, input, response json.RawMessage) map[string][]string {
	var r struct {
		Answer string `json:"answer"`
	}
	if json.Unmarshal(response, &r) != nil || r.Answer == "" {
		return nil
	}
	return map[string][]string{"0": {r.Answer}}
}

func askFeed(t *testing.T, m *Mapper, recs ...string) []harness.Item {
	t.Helper()
	var out []harness.Item
	for i, r := range recs {
		items, _ := m.Map(harness.Record{Source: "hooks", At: t0.Add(time.Duration(i) * time.Second), Data: json.RawMessage(r)})
		out = append(out, items...)
	}
	return out
}

func questions(items []harness.Item) []harness.Item {
	var out []harness.Item
	for _, x := range items {
		if x.Kind == harness.KindQuestion {
			out = append(out, x)
		}
		if x.Kind == harness.KindTool && x.Tool.Name == "ask" {
			panic("the ask tool was drawn as a tool row")
		}
	}
	return out
}

func TestAskToolBecomesQuestionAndCloses(t *testing.T) {
	m := New(harness.Env{SessionID: "s-ask-1"}, Options{Name: "t.hooks", Ask: stubAsk, Answered: stubAnswered})
	got := questions(askFeed(t, m,
		`{"hook_event_name":"UserPromptSubmit","prompt":"pick"}`,
		`{"hook_event_name":"PreToolUse","tool_name":"ask","tool_use_id":"c1","tool_input":{"q":"Which DB?","choices":["Postgres","SQLite"]}}`,
		`{"hook_event_name":"PostToolUse","tool_name":"ask","tool_use_id":"c1","tool_input":{"q":"Which DB?","choices":["Postgres","SQLite"]},"tool_response":{"answer":"SQLite"}}`,
	))
	if len(got) != 2 || got[0].ID != "c1" || got[1].ID != "c1" {
		t.Fatalf("questions %+v", got)
	}
	open := got[0].Question
	if open.Answered != nil || open.Questions[0].Prompt != "Which DB?" || len(open.Questions[0].Options) != 2 || got[0].TurnID == "" {
		t.Fatalf("open %+v", open)
	}
	if want := map[string][]string{"0": {"SQLite"}}; !reflect.DeepEqual(got[1].Question.Answered, want) {
		t.Fatalf("answered %v", got[1].Question.Answered)
	}
}

// No tool_use_id (Cursor, Droid at times): the post hook still finds the
// ask by tool name; a failed call and an unreadable response close it with
// every question [].
func TestAskWithoutCallIDFailedCloses(t *testing.T) {
	m := New(harness.Env{SessionID: "s-ask-2"}, Options{Name: "t.hooks", Ask: stubAsk, Answered: stubAnswered})
	got := questions(askFeed(t, m,
		`{"hook_event_name":"PreToolUse","tool_name":"ask","tool_input":{"q":"Go on?","choices":["Yes","No"]}}`,
		`{"hook_event_name":"PostToolUseFailure","tool_name":"ask","tool_input":{},"tool_response":{"answer":"Yes"}}`,
	))
	if len(got) != 2 || got[0].ID != got[1].ID {
		t.Fatalf("questions %+v", got)
	}
	if want := map[string][]string{"0": {}}; !reflect.DeepEqual(got[1].Question.Answered, want) {
		t.Fatalf("answered %v", got[1].Question.Answered)
	}
}

// A turn that stops with the ask open (Esc at the desk) closes it.
func TestAskOpenAtStopCloses(t *testing.T) {
	m := New(harness.Env{SessionID: "s-ask-3"}, Options{Name: "t.hooks", Ask: stubAsk, Answered: stubAnswered})
	items := askFeed(t, m,
		`{"hook_event_name":"UserPromptSubmit","prompt":"pick"}`,
		`{"hook_event_name":"PreToolUse","tool_name":"ask","tool_use_id":"c9","tool_input":{"q":"Which?","choices":["A","B"]}}`,
		`{"hook_event_name":"Stop","stop_interrupted":true}`,
	)
	got := questions(items)
	if len(got) != 2 || len(got[1].Question.Answered["0"]) != 0 || got[1].Question.Answered == nil {
		t.Fatalf("questions %+v", got)
	}
	for _, x := range items {
		if x.Kind == harness.KindTool {
			t.Fatalf("tool row %+v", x.Tool)
		}
	}
}

// Without Options.Ask every tool stays a tool row.
func TestNoAskOptionKeepsToolRow(t *testing.T) {
	m := New(harness.Env{SessionID: "s-ask-4"}, Options{Name: "t.hooks"})
	items := askFeed(t, m, `{"hook_event_name":"PreToolUse","tool_name":"ask","tool_use_id":"c2","tool_input":{"q":"x"}}`)
	if len(items) == 0 || items[0].Kind != harness.KindTool {
		t.Fatalf("items %+v", items)
	}
}

// A plan-approval tool draws its plan before the question.
func TestAskPlanDrawnFirst(t *testing.T) {
	plan := func(tool string, input json.RawMessage) (string, bool) {
		if tool != "ask" {
			return "", false
		}
		return "1. Do it", true
	}
	m := New(harness.Env{SessionID: "s-ask-5"}, Options{Name: "t.hooks", Ask: stubAsk, Answered: stubAnswered, Plan: plan})
	items := askFeed(t, m,
		`{"hook_event_name":"UserPromptSubmit","prompt":"plan"}`,
		`{"hook_event_name":"PreToolUse","tool_name":"ask","tool_use_id":"c5","tool_input":{"q":"Implement?","choices":["Yes","No"]}}`,
	)
	var ids []string
	for _, x := range items[2:] {
		ids = append(ids, x.ID)
	}
	if len(ids) < 2 || ids[0] != "c5-plan" || ids[1] != "c5" || items[2].Assistant.Text != "1. Do it" {
		t.Fatalf("ids %v", ids)
	}
}
