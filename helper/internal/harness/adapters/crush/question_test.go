package crush

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

// db-questions.jsonl is a real Crush 0.100.0 crush.db (Qwen3.8 27B): three
// question calls answered in the TUI (one single choice; a batch of single,
// multi with a typed "Something else", yes/no and free text; a yes/no
// dismissed with Escape), then a bash call denied at its permission
// dialog. Before each assistant row with a call, the same row as the feed
// sees it while the dialog waits (no finish part yet).
const qsid = "e6376ff5-8ea0-453e-9d00-1ea0208b854b"

func TestQuestionCalls(t *testing.T) {
	m := newDB(harness.Env{Cwd: "/w/proj"})
	f, err := os.Open(filepath.Join("testdata", "db-questions.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(nil, 1<<20)
	final := map[string]harness.Item{}
	var waiting *harness.Item
	for sc.Scan() {
		items, _ := m.Map(harness.Record{At: time.Unix(1, 0), Data: json.RawMessage(sc.Text())})
		for _, x := range items {
			if x.ID == "eSdiVeXdvmAfjtIHjCYbUmEPffqHa1hO" && waiting == nil {
				x := x
				waiting = &x
			}
			final[x.ID] = x
		}
	}
	if m.State()["harness_session"] != qsid {
		t.Fatalf("session %q", m.State()["harness_session"])
	}

	// Published open while the dialog waits, never as a tool row.
	if waiting == nil || waiting.Kind != harness.KindQuestion || waiting.Question.Answered != nil {
		t.Fatalf("waiting %+v", waiting)
	}
	q := waiting.Question.Questions
	if len(q) != 1 || q[0].ID != "0" || q[0].Prompt != "Which colour should the banner use?" || !q[0].AllowText || q[0].Multi {
		t.Fatalf("single %+v", q)
	}
	want := []harness.Choice{{ID: "Red", Label: "Red", Detail: "Bold and attention-grabbing."},
		{ID: "Green", Label: "Green", Detail: "Calm and fresh."}, {ID: "Blue", Label: "Blue", Detail: "Calm and professional."}}
	if !reflect.DeepEqual(q[0].Options, want) {
		t.Fatalf("options %+v", q[0].Options)
	}
	if got := final["eSdiVeXdvmAfjtIHjCYbUmEPffqHa1hO"].Question.Answered; !reflect.DeepEqual(got, map[string][]string{"0": {"Green"}}) {
		t.Fatalf("single answered %v", got)
	}

	// The batch: headers from the labels, each type, answers mapped back to labels.
	b := final["dTDZt2z0Lpbrs23FjHLZgxbhFWqGmDTy"].Question
	if b == nil || len(b.Questions) != 4 {
		t.Fatalf("batch %+v", b)
	}
	if h := []string{b.Questions[0].Header, b.Questions[1].Header, b.Questions[2].Header, b.Questions[3].Header}; !reflect.DeepEqual(h, []string{"Database", "Features", "Caching", "Notes"}) {
		t.Fatalf("headers %v", h)
	}
	if !b.Questions[1].Multi || len(b.Questions[1].Options) != 3 {
		t.Fatalf("multi %+v", b.Questions[1])
	}
	if yn := b.Questions[2]; yn.AllowText || len(yn.Options) != 2 || yn.Options[0].ID != "Yes" || yn.Options[1].ID != "No" {
		t.Fatalf("yes/no %+v", yn)
	}
	if ft := b.Questions[3]; !ft.AllowText || len(ft.Options) != 0 {
		t.Fatalf("free text %+v", ft)
	}
	wantB := map[string][]string{"0": {"PostgreSQL"}, "1": {"Auth", "Search", "Export"}, "2": {"No"}, "3": {"Keep it small"}}
	if !reflect.DeepEqual(b.Answered, wantB) {
		t.Fatalf("batch answered %v", b.Answered)
	}

	// Escape: the call fails "User cancelled this question"; it closes with none.
	if got := final["QE8z3h2sDVMOpd1mO04NJd6pYsMpOc8u"].Question.Answered; !reflect.DeepEqual(got, map[string][]string{"0": {}}) {
		t.Fatalf("cancelled %v", got)
	}

	// Other tools stay tool rows.
	if c := final["uu0a250nuZ8BJkiiIzIVZWNuK4Rmm3b8"]; c.Tool == nil || c.Tool.Name != "bash" {
		t.Fatalf("bash %+v", c)
	}
}

// A turn cancelled while the dialog waits closes the question.
func TestQuestionClosedByCancel(t *testing.T) {
	m := newDB(harness.Env{})
	m.Map(harness.Record{Data: json.RawMessage(`{"id":"s","created_at":1,"updated_at":1}`)})
	row := func(id, role, parts string) []harness.Item {
		b, _ := json.Marshal(map[string]any{"id": id, "session_id": "s", "role": role, "parts": parts, "created_at": 1, "updated_at": 1})
		items, _ := m.Map(harness.Record{Data: b})
		return items
	}
	row("u", "user", `[{"type":"text","data":{"text":"ask"}}]`)
	in, _ := json.Marshal(`{"questions":[{"type":"free_text","question":"Why?","description":"d"}]}`)
	items := row("a", "assistant", `[{"type":"tool_call","data":{"id":"c1","name":"question","input":`+string(in)+`}}]`)
	if len(items) == 0 || items[0].Kind != harness.KindQuestion || !items[0].Question.Questions[0].AllowText {
		t.Fatalf("asked %+v", items)
	}
	items = row("a", "assistant", `[{"type":"tool_call","data":{"id":"c1","name":"question","input":`+string(in)+`}},{"type":"finish","data":{"reason":"canceled"}}]`)
	for _, x := range items {
		if x.ID == "c1" && reflect.DeepEqual(x.Question.Answered, map[string][]string{"0": {}}) {
			return
		}
	}
	t.Fatalf("not closed: %+v", items)
}

// Some models send the questions as a JSON string; Crush accepts it.
func TestQuestionsAsString(t *testing.T) {
	qs := parseQuestions(`{"questions":"[{\"type\":\"yes_no\",\"question\":\"Go?\"}]"}`)
	if len(qs) != 1 || qs[0].Question != "Go?" {
		t.Fatalf("%+v", qs)
	}
}

func TestParseAnswersNotesAndSkip(t *testing.T) {
	got := parseAnswers("Q1: A?\nUser skipped this question\n\nQ2: B?\nUser selected: [\"x\"]\n\nNotes:\n- [x]: why", []map[string]string{{}, {"x": "Ex"}})
	if !reflect.DeepEqual(got, map[string][]string{"0": nil, "1": {"Ex"}}) {
		t.Fatalf("%#v", got)
	}
}
