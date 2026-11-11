package kimi

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/engine"
	"github.com/gettogo/togo/helper/internal/harness/spec"
)

// wire-prompts.jsonl is a real Kimi Code 2.5.3 wire log (Qwen3.8 27B via a
// local provider; paths scrubbed, the system prompt and tool lists left
// out), every prompt answered in the TUI: a question picked with 2, one
// answered with Other ("Teal"), a batch of a multi-select (Auth, Search)
// and a single choice; a Bash approval rejected with feedback and one
// approved for the session; a plan review sent back with Revise, and the
// revised plan rejected.
func TestRecordedPrompts(t *testing.T) {
	m := NewWire(harness.Env{SessionID: "s1"})
	lines := fixture(t, "wire-prompts.jsonl")
	first := map[string]harness.Item{} // each item as first published
	out := mapped{items: map[string]harness.Item{}}
	for i, l := range lines {
		items, _ := m.Map(harness.Record{Source: "wire", At: t0.Add(time.Duration(i) * time.Second), Data: json.RawMessage(l)})
		for _, it := range items {
			if _, ok := first[it.ID]; !ok {
				first[it.ID] = it
			}
			out.items[it.ID] = it
		}
	}
	answered := func(id string) map[string][]string {
		t.Helper()
		it, ok := out.items[id]
		if !ok || it.Question == nil {
			t.Fatalf("%s missing", id)
		}
		if f := first[id]; f.Question.Answered != nil || f.Question.Answerable != nil {
			t.Fatalf("%s not published open: %+v", id, f.Question)
		}
		return it.Question.Answered
	}

	q := out.items["question_f5fc190b-c1a4-4de3-8cba-b89014615253"].Question
	if q == nil || q.Questions[0].Prompt != "Which colour should the banner use?" || !q.Questions[0].AllowText || len(q.Questions[0].Options) != 3 || q.Questions[0].Options[1].ID != "Green" {
		t.Fatalf("single %+v", q)
	}
	if got := answered("question_f5fc190b-c1a4-4de3-8cba-b89014615253"); !reflect.DeepEqual(got, map[string][]string{"0": {"Green"}}) {
		t.Fatalf("single answered %v", got)
	}
	if got := answered("question_93352955-5739-4d19-954c-7e8509551b64"); !reflect.DeepEqual(got, map[string][]string{"0": {"Teal"}}) {
		t.Fatalf("other answered %v", got)
	}
	b := out.items["question_c68f53de-4f58-4fe4-90c4-cb6e54be09c7"].Question
	if len(b.Questions) != 2 || !b.Questions[0].Multi || b.Questions[0].Header != "Features" || b.Questions[1].Multi {
		t.Fatalf("batch %+v", b.Questions)
	}
	if got := answered("question_c68f53de-4f58-4fe4-90c4-cb6e54be09c7"); !reflect.DeepEqual(got, map[string][]string{"0": {"Auth", "Search"}, "1": {"MongoDB"}}) {
		t.Fatalf("batch answered %v", got)
	}

	// Approvals: Kimi's own wording and options, Approve and Deny set.
	a := out.items["approval_283e3abf-762f-4224-b348-52c9a1fb2cc8"].Question
	aq := a.Questions[0]
	var labels []string
	for _, o := range aq.Options {
		labels = append(labels, o.Label)
	}
	if aq.Prompt != "Run this command?" || aq.Header != "Running: make" || !aq.AllowText || a.Approve != "Approve once" || a.Deny != "Reject" ||
		!reflect.DeepEqual(labels, []string{"Approve once", "Approve for this session", "Reject", "Reject with feedback"}) {
		t.Fatalf("approval %+v %v", a, labels)
	}
	if got := answered("approval_283e3abf-762f-4224-b348-52c9a1fb2cc8"); !reflect.DeepEqual(got, map[string][]string{"0": {"Use make clean instead"}}) {
		t.Fatalf("feedback %v", got)
	}
	if got := answered("approval_fa26bdcd-2c0e-4041-a1a3-3fdf57bfe72d"); !reflect.DeepEqual(got, map[string][]string{"0": {"Approve for this session"}}) {
		t.Fatalf("session %v", got)
	}

	// The plan review: the plan as an assistant item, then the question.
	plan := out.items["QhzMxTPw4OKJ1s5afsK9XZkBwcaEMQQX-plan"]
	if plan.Kind != harness.KindAssistant || !strings.Contains(plan.Assistant.Text, "Buy eggs") {
		t.Fatalf("plan %+v", plan)
	}
	p := out.items["approval_01195676-50f4-4519-9d9c-b32b26e2f1a7"].Question
	labels = nil
	for _, o := range p.Questions[0].Options {
		labels = append(labels, o.Label)
	}
	if p.Questions[0].Prompt != "Ready to build with this plan?" || p.Approve != "Approve" || p.Deny != "Reject" || !reflect.DeepEqual(labels, []string{"Approve", "Reject", "Revise"}) {
		t.Fatalf("plan question %+v", p)
	}
	if got := answered("approval_01195676-50f4-4519-9d9c-b32b26e2f1a7"); !reflect.DeepEqual(got, map[string][]string{"0": {"Also add Buy bread"}}) {
		t.Fatalf("revise %v", got)
	}
	if got := answered("approval_e0a63bce-e5bd-4876-925c-3c2ffc1b1bd9"); !reflect.DeepEqual(got, map[string][]string{"0": {"Reject"}}) {
		t.Fatalf("reject %v", got)
	}
	for id, it := range out.items {
		if it.Kind == harness.KindQuestion && it.Question.Answered == nil {
			t.Fatalf("%s left open", id)
		}
	}
}

// A dismissed question resolves with null, a cancelled approval with
// decision cancelled: both close with no answer. A plan's own options
// replace Approve.
func TestDismissedAndCustomPlan(t *testing.T) {
	m := NewWire(harness.Env{})
	mapOne := func(s string) []harness.Item {
		items, _ := m.Map(harness.Record{Data: json.RawMessage(s)})
		return items
	}
	mapOne(`{"type":"interaction.request","id":"q","kind":"question","request":{"questions":[{"question":"A?","options":[{"label":"x"},{"label":"y"}]}]}}`)
	if it := mapOne(`{"type":"interaction.resolved","id":"q","response":null}`); !reflect.DeepEqual(it[0].Question.Answered, map[string][]string{"0": {}}) {
		t.Fatalf("dismissed %+v", it)
	}
	mapOne(`{"type":"interaction.request","id":"a","kind":"approval","request":{"toolName":"Edit","action":"Editing a.go","display":{"kind":"diff"}}}`)
	if it := mapOne(`{"type":"interaction.resolved","id":"a","response":{"decision":"cancelled","feedback":"turn_ended"}}`); !reflect.DeepEqual(it[0].Question.Answered, map[string][]string{"0": {}}) {
		t.Fatalf("cancelled %+v", it)
	}
	it := mapOne(`{"type":"interaction.request","id":"p","kind":"approval","toolCallId":"c","request":{"toolName":"ExitPlanMode","display":{"kind":"plan_review","plan":"Do it","options":[{"label":"Ship it"},{"label":"Ship with tests"}]}}}`)
	if len(it) != 2 || it[0].ID != "c-plan" || it[1].Question.Approve != "Ship it" || len(it[1].Question.Questions[0].Options) != 4 {
		t.Fatalf("custom plan %+v", it)
	}
	if it := mapOne(`{"type":"interaction.resolved","id":"p","response":{"decision":"approved","selectedLabel":"Ship with tests"}}`); !reflect.DeepEqual(it[0].Question.Answered, map[string][]string{"0": {"Ship with tests"}}) {
		t.Fatalf("picked %+v", it)
	}
}

// The answer action picks the approval path for approval_… ids.
func TestAnswerBranch(t *testing.T) {
	a, err := spec.Parse(AdapterFile())
	if err != nil {
		t.Fatal(err)
	}
	act := a.Actions["answer"]
	if len(act.Branch) != 2 || act.Branch[0].Builtin != "approval_answer" || act.Branch[1].Builtin != "screen_answer" {
		t.Fatalf("answer %+v", act)
	}
	for target, want := range map[string]string{"approval_283e": "true", "question_f5fc": "false"} {
		got, err := engine.Render(act.Branch[0].When, map[string]any{"target": target})
		if err != nil || got != want {
			t.Fatalf("%s: %q %v", target, got, err)
		}
	}
}
