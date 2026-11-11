package droid

import (
	"bufio"
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
)

type hookLine struct {
	Event   string `json:"event"`
	Payload struct {
		ToolName     string          `json:"tool_name"`
		ToolInput    json.RawMessage `json:"tool_input"`
		ToolResponse json.RawMessage `json:"tool_response"`
		Notification string          `json:"notification_type"`
		Message      string          `json:"message"`
	} `json:"payload"`
}

func readHooks(t *testing.T, name string) []hookLine {
	t.Helper()
	f, err := os.Open("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out []hookLine
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		var h hookLine
		if err := json.Unmarshal(sc.Bytes(), &h); err != nil {
			t.Fatal(err)
		}
		if h.Event != "" {
			out = append(out, h)
		}
	}
	return out
}

// The questionnaire becomes one question per [question], topic as the
// header, labels as ids, (multi) honoured, own answer allowed; the result
// text maps back to labels, or to the typed answer.
func TestAskQuestionnaireAndAnswers(t *testing.T) {
	hooks := readHooks(t, "hooks-ask.jsonl")
	pre, post := hooks[0], hooks[2]
	if pre.Event != "PreToolUse" || post.Event != "PostToolUse" {
		t.Fatalf("fixture order: %s %s", pre.Event, post.Event)
	}
	q, ok := askQuestion(pre.Payload.ToolName, pre.Payload.ToolInput)
	if !ok {
		t.Fatal("AskUser not read")
	}
	want := []harness.Question{
		{ID: "0", Header: "Features", Prompt: "Which features do you want to enable?", Multi: true, AllowText: true,
			Options: []harness.Choice{{ID: "Auth handling", Label: "Auth handling"}, {ID: "Login Page", Label: "Login Page"}, {ID: "Dark mode", Label: "Dark mode"}}},
		{ID: "1", Header: "Date-library", Prompt: "Which library should we use for date formatting?", AllowText: true,
			Options: []harness.Choice{{ID: "date-fns", Label: "date-fns"}, {ID: "Day.js", Label: "Day.js"}}},
	}
	if !reflect.DeepEqual(q.Questions, want) {
		t.Fatalf("questions\n got %+v\nwant %+v", q.Questions, want)
	}
	got := askAnswered(post.Payload.ToolName, post.Payload.ToolInput, post.Payload.ToolResponse)
	wantA := map[string][]string{"0": {"Auth handling", "Dark mode"}, "1": {"Luxon, it is already a dependency"}}
	if !reflect.DeepEqual(got, wantA) {
		t.Fatalf("answers %v, want %v", got, wantA)
	}
	if n := hooks[1]; n.Event != "Notification" || n.Payload.Notification != "elicitation_dialog" {
		t.Fatalf("notification %+v", n)
	}
}

// The questions-array form keeps descriptions; a cancel answers nothing.
func TestAskQuestionsArrayAndCancel(t *testing.T) {
	hooks := readHooks(t, "hooks-ask.jsonl")
	pre, post := hooks[3], hooks[4]
	q, ok := askQuestion(pre.Payload.ToolName, pre.Payload.ToolInput)
	if !ok || len(q.Questions) != 1 {
		t.Fatalf("got %+v %v", q, ok)
	}
	x := q.Questions[0]
	if x.Header != "Ship" || x.Multi || !x.AllowText || len(x.Options) != 2 || x.Options[1] != (harness.Choice{ID: "Not yet", Label: "Not yet", Detail: "Keep testing"}) {
		t.Fatalf("question %+v", x)
	}
	got := askAnswered(post.Payload.ToolName, post.Payload.ToolInput, post.Payload.ToolResponse)
	if !reflect.DeepEqual(got, map[string][]string{"0": {}}) {
		t.Fatalf("cancel answers %v", got)
	}
}

func TestAskQuestionnaireEdges(t *testing.T) {
	in := func(s string) json.RawMessage { b, _ := json.Marshal(map[string]any{"questionnaire": s}); return b }
	// Text before the first question, a continued question, no options
	// (Yes/No), no topic (Q<n>), "(multi)" on its own continuation line.
	q, ok := askQuestion("AskUser", in("Some context first.\n```\n[question] Delete the old cache\nfolder too?\n\n2. [question] Pick targets\n(multi)\n[option] ios\n[option] mac"))
	if !ok || len(q.Questions) != 2 {
		t.Fatalf("got %+v", q)
	}
	a, b := q.Questions[0], q.Questions[1]
	if a.Prompt != "Delete the old cache\nfolder too?" || a.Header != "Q1" || len(a.Options) != 2 || a.Options[0].ID != "Yes" || a.Multi {
		t.Fatalf("first %+v", a)
	}
	if b.Prompt != "Pick targets" || !b.Multi || b.Header != "Q2" || len(b.Options) != 2 {
		t.Fatalf("second %+v", b)
	}
	// Implicit numbered questions.
	q, ok = askQuestion("AskUser", in("1. Which database?\n[topic] DB\n[option] Postgres\n[option] SQLite"))
	if !ok || q.Questions[0].Prompt != "Which database?" || q.Questions[0].Header != "DB" {
		t.Fatalf("implicit %+v %v", q, ok)
	}
	// Non-blocking calls queue, nothing is on screen; other tools and
	// unreadable input are not questions.
	if _, ok := askQuestion("AskUser", json.RawMessage(`{"questionnaire":"[question] x?","blocking":false,"context":"c"}`)); ok {
		t.Error("non-blocking AskUser read as a prompt")
	}
	if _, ok := askQuestion("Execute", json.RawMessage(`{"command":"ls"}`)); ok {
		t.Error("Execute read as a question")
	}
	if _, ok := askQuestion("AskUser", json.RawMessage(`[`)); ok {
		t.Error("bad JSON read")
	}
	if got := askAnswered("AskUser", in("[question] x?\n[option] a\n[option] b"), json.RawMessage(`""`)); got != nil {
		t.Errorf("empty result read as %v", got)
	}
	// A single-choice answer that is a label in other case maps to it.
	got := askAnswered("AskUser", in("[question] x?\n[option] Alpha\n[option] Beta"), json.RawMessage(`"1. [question] x?\n[answer] beta"`))
	if !reflect.DeepEqual(got, map[string][]string{"0": {"Beta"}}) {
		t.Errorf("answer %v", got)
	}
}

func TestSpecPlan(t *testing.T) {
	p, ok := specPlan("ExitSpecMode", json.RawMessage(`{"plan":"1. Add README\n2. Test","title":"Docs"}`))
	if !ok || p != "# Docs\n\n1. Add README\n2. Test" {
		t.Fatalf("%q %v", p, ok)
	}
	if _, ok := specPlan("ExitSpecMode", json.RawMessage(`{}`)); ok {
		t.Error("empty plan read")
	}
}

// The real exec run's hooks: no tool call id, a text tool_response.
func TestRealExecHooksShape(t *testing.T) {
	hooks := readHooks(t, "hooks-real-exec.jsonl")
	var saw bool
	for _, h := range hooks {
		if h.Event == "PostToolUse" {
			saw = true
			var s string
			if json.Unmarshal(h.Payload.ToolResponse, &s) != nil || s == "" {
				t.Errorf("tool_response not text: %s", h.Payload.ToolResponse)
			}
		}
	}
	if !saw {
		t.Fatal("no PostToolUse in the real run")
	}
}
