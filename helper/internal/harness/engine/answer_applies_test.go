package engine

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/session"
)

func init() {
	harness.RegisterAdapterFile("fakeform", []byte(`
adapter: fakeform
name: Fake form
revision: 1
match: { agents: [fakeform] }
capabilities: [chat, needs.questions]
actions:
  answer: { cli: { argv: [rec, '{{form .answers}}'] }, stability: documented }
`))
}

// conditional is the form of adapters/opencode/testdata/v2/forms.sse
// ("Release settings", recorded from OpenCode 2.2.5) as its mapper gives
// it: channel applies while notify is true, mac_note while targets
// includes mac, quiet_reason while notify is false.
var conditional = harness.Item{ID: "frm_1~ses_1", Kind: harness.KindQuestion, Question: &harness.QuestionBody{Questions: []harness.Question{
	{ID: "notify!", Prompt: "Send a notice", Required: boolPtr(true), Default: []string{"false"}, Options: []harness.Choice{{ID: "true", Label: "Yes"}, {ID: "false", Label: "No"}}},
	{ID: "channel", Prompt: "Channel", Required: boolPtr(true), Default: []string{"mail"}, Options: []harness.Choice{{ID: "mail", Label: "Mail"}, {ID: "chat", Label: "Chat"}},
		When: &harness.QuestionWhen{Question: "notify!", Equals: []string{"true"}}},
	{ID: "targets[]", Prompt: "Targets", Multi: true, Default: []string{"ios"}, Options: []harness.Choice{{ID: "ios", Label: "iOS"}, {ID: "mac", Label: "Mac"}}},
	{ID: "mac_note", Prompt: "Mac note", AllowText: true, When: &harness.QuestionWhen{Question: "targets[]", Equals: []string{"mac"}}},
	{ID: "retries#", Prompt: "Retries", AllowText: true, Default: []string{"2"}},
	{ID: "quiet_reason", Prompt: "Why no notice", AllowText: true, When: &harness.QuestionWhen{Question: "notify!", Equals: []string{"false"}}},
	{ID: "mac_team", Prompt: "Mac team", AllowText: true, When: &harness.QuestionWhen{Question: "mac_note", Equals: []string{"signed"}}},
}}}

func formEngine(t *testing.T, id string, item harness.Item) (*Engine, func() string) {
	t.Helper()
	bin, home := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "rec"), []byte("#!/bin/sh\nprintf '%s\\n' \"$1\" >> rec.log\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	e := New(Config{
		Sessions: fakeSessions{id: session.Session{ID: id, Agent: "fakeform", Managed: true, Cwd: home, Command: []string{"fakeform"}}},
		Typist:   &seqPane{}, Marker: &fakeMarker{}, Home: home,
		LoginPath: func(context.Context) string { return bin + ":/bin:/usr/bin" },
		Log:       &Log{NewEpoch: func() string { return "e1" }},
		Scale:     0.1,
	})
	t.Cleanup(func() { e.Forget(id) })
	s, err := e.served(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.publishLocked([]harness.Item{item}, nil)
	s.mu.Unlock()
	return e, func() string {
		b, _ := os.ReadFile(filepath.Join(home, "rec.log"))
		return string(b)
	}
}

func answerForm(t *testing.T, e *Engine, id, target, answers string) harness.ActionResult {
	t.Helper()
	res, err := e.Act(context.Background(), harness.ActionRequest{Session: id, Action: harness.ActionAnswer, Target: target,
		Args: map[string]json.RawMessage{"answers": json.RawMessage(answers)}})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// The released app does not know conditions: it shows every question and
// sends an answer to each. OpenCode refuses a reply that answers a field
// that is not active ("Form field is not active", 2.2.5), even with an
// empty answer, so those answers are left out, a list's included, and a
// question that depends on one left out goes with it.
func TestAnswersOfQuestionsThatDoNotApplyAreNotSent(t *testing.T) {
	const id = "s_fakeform01"
	e, sent := formEngine(t, id, conditional)
	res := answerForm(t, e, id, conditional.ID, `{"notify!":["false"],"channel":["mail"],"targets[]":["ios"],"mac_note":["signed"],
		"retries#":["2"],"quiet_reason":["a holiday"],"mac_team":["core"]}`)
	if res.Result != harness.ResultApplied {
		t.Fatalf("result %+v", res)
	}
	if got, want := sent(), `{"notify":false,"quiet_reason":"a holiday","retries":2,"targets":["ios"]}`+"\n"; got != want {
		t.Fatalf("sent %s want %s", got, want)
	}
}

// The other way round every dependent question applies and is sent.
func TestAnswersOfQuestionsThatApplyAreSent(t *testing.T) {
	const id = "s_fakeform02"
	e, sent := formEngine(t, id, conditional)
	answerForm(t, e, id, conditional.ID, `{"notify!":["true"],"channel":["chat"],"targets[]":["ios","mac"],"mac_note":["signed"],
		"retries#":[""],"quiet_reason":[""],"mac_team":["core"]}`)
	if got, want := sent(), `{"channel":"chat","mac_note":"signed","mac_team":"core","notify":true,"targets":["ios","mac"]}`+"\n"; got != want {
		t.Fatalf("sent %s want %s", got, want)
	}
}

// A question item without conditions is answered exactly as it was.
func TestAnswersWithoutConditionsAreUntouched(t *testing.T) {
	const id = "s_fakeform03"
	plain := harness.Item{ID: "frm_2~ses_1", Kind: harness.KindQuestion, Question: &harness.QuestionBody{Questions: []harness.Question{
		{ID: "a", Prompt: "A", AllowText: true}, {ID: "b[]", Prompt: "B", Multi: true}}}}
	e, sent := formEngine(t, id, plain)
	answerForm(t, e, id, plain.ID, `{"a":["x"],"b[]":[],"other":["kept"]}`)
	if got, want := sent(), `{"a":"x","b":[],"other":"kept"}`+"\n"; got != want {
		t.Fatalf("sent %s want %s", got, want)
	}
}

// An optional number or yes/no left empty is not an answer: it is neither
// sent as false nor a reason to refuse the whole reply.
func TestFormFilterLeavesEmptyAnswersOut(t *testing.T) {
	got, err := formFilter(map[string]any{"n#": []any{""}, "m#": []any{" "}, "ok!": []any{""}, "yes!": []any{"true"}, "no!": []any{"false"},
		"text": []any{""}, "list[]": []any{}, "k#": []any{"3"}})
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"k":3,"list":[],"no":false,"text":"","yes":true}`; got != want {
		t.Fatalf("form = %s, want %s", got, want)
	}
	if _, err := formFilter(map[string]any{"n#": []any{"many"}}); err == nil {
		t.Fatal("a number that is not one was accepted")
	}
}

// A form of 64 questions is answered whole (OpenCode 2.2.5 takes forms
// that long); one more answer than the app can show is refused, and no
// other arg may grow past the usual bound.
func TestAnswerActionTakesSixtyFourAnswers(t *testing.T) {
	const id = "s_fakeform04"
	item := harness.Item{ID: "frm_3~ses_1", Kind: harness.KindQuestion, Question: &harness.QuestionBody{}}
	answers, want := map[string][]string{}, map[string]string{}
	for i := 0; i < harness.MaxQuestions; i++ {
		key := "f" + itoa(int64(i))
		item.Question.Questions = append(item.Question.Questions, harness.Question{ID: key, Prompt: "Field", AllowText: true, Required: boolPtr(true)})
		answers[key], want[key] = []string{"v" + key}, "v"+key
	}
	e, sent := formEngine(t, id, item)
	raw, _ := json.Marshal(answers)
	if res := answerForm(t, e, id, item.ID, string(raw)); res.Result != harness.ResultApplied {
		t.Fatalf("result %+v", res)
	}
	var got map[string]string
	if err := json.Unmarshal([]byte(sent()), &got); err != nil || len(got) != harness.MaxQuestions || got["f63"] != "vf63" {
		t.Fatalf("sent %d answers (%v): %s", len(got), err, sent())
	}

	answers["extra"] = []string{"x"}
	raw, _ = json.Marshal(answers)
	if _, err := e.Act(context.Background(), harness.ActionRequest{Session: id, Action: harness.ActionAnswer, Target: item.ID,
		Args: map[string]json.RawMessage{"answers": raw}}); err == nil {
		t.Fatal("65 answers were accepted")
	}
	delete(answers, "extra")
	raw, _ = json.Marshal(answers)
	if _, err := checkArgs(map[string]json.RawMessage{"other": raw}); err == nil {
		t.Fatal("an arg that is not answers took 64 keys")
	}
	if _, err := checkArgs(map[string]json.RawMessage{"answers": json.RawMessage(`{"a":{"b":` + string(raw) + `}}`)}); err == nil {
		t.Fatal("an object inside an answer took 64 keys")
	}
	many := map[string]any{}
	for k := range answers {
		many[k] = []any{"x"}
	}
	if _, err := formFilter(many); err != nil {
		t.Fatalf("form filter refused 64 answers: %v", err)
	}
	many["extra"] = []any{"x"}
	if _, err := formFilter(many); err == nil {
		t.Fatal("form filter took 65 answers")
	}
}

func boolPtr(b bool) *bool { return &b }
