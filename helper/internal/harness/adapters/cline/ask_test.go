package cline

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/source"
)

// askID is the recorded session in testdata/ask.messages.json (Cline
// 3.0.74 TUI, --auto-approve false, a local model; paths scrubbed): three
// ask_question calls answered by a pick (Python), by typed text
// ("Howdy partner") and by Esc, an edit and a command denied at the
// [y] Approve / [n] Deny prompt, one approved, then a plan in Plan mode.
const askID = "1794334480911_meju1"

func askHome(t *testing.T) (string, document) {
	t.Helper()
	h := t.TempDir()
	dir := filepath.Join(h, ".cline", "data", "sessions", askID)
	_ = os.MkdirAll(dir, 0o755)
	msgs, err := os.ReadFile("testdata/ask.messages.json")
	if err != nil {
		t.Fatal(err)
	}
	man, _ := os.ReadFile("testdata/ask.json")
	_ = os.WriteFile(filepath.Join(dir, askID+".messages.json"), msgs, 0o600)
	_ = os.WriteFile(filepath.Join(dir, askID+".json"), man, 0o600)
	var d document
	if err := json.Unmarshal(msgs, &d); err != nil {
		t.Fatal(err)
	}
	return h, d
}

func askEnv(h string) harness.Env {
	return harness.Env{Cwd: "/Users/jamie/proj", Vars: map[string]string{source.VarHome: h, "created": "2026-11-10T18:14:40Z"}}
}

// docOf is the document cut after its first n messages, as Cline had
// written it at that point.
func docOf(t *testing.T, d document, n int) []byte {
	t.Helper()
	d.Messages = d.Messages[:n]
	b, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func latest(items []harness.Item) map[string]harness.Item {
	out := map[string]harness.Item{}
	for _, it := range items {
		out[it.ID] = it
	}
	return out
}

func TestAskQuestionMapsToAQuestionAndItsAnswers(t *testing.T) {
	h, d := askHome(t)
	m := newMessages(askEnv(h))
	items, _ := m.Map(harness.Record{Source: "messages", Data: docOf(t, d, len(d.Messages))})
	byID := latest(items)

	want := map[string]struct {
		prompt  string
		options []string
		answer  []string
	}{
		"cIXbfp0DFHkX3hoZ31yrcTN8wMMAfnG8": {"Which language would you like me to use for the hello world script?", []string{"Go", "Python", "Rust"}, []string{"Python"}},
		"nHaFkoDJkdclKxy71gLisEEOoTajAUoq": {"What should the greeting text say?", []string{"Hello", "Hi there"}, []string{"Howdy partner"}},
		"Wb7m7W1ppdX0FxJ4LWC7fEuuX0u4xP59": {"Would you like me to add a README file?", []string{"Yes", "No"}, []string{}},
	}
	for id, w := range want {
		it, ok := byID[id]
		if !ok || it.Kind != harness.KindQuestion || it.Question == nil {
			t.Fatalf("%s: %+v, want a question item", id, it)
		}
		if len(it.Question.Questions) != 1 {
			t.Fatalf("%s: %d questions", id, len(it.Question.Questions))
		}
		q := it.Question.Questions[0]
		var labels []string
		for _, o := range q.Options {
			if o.ID != o.Label {
				t.Errorf("%s: option id %q, label %q; the id is what the keys pick", id, o.ID, o.Label)
			}
			labels = append(labels, o.Label)
		}
		if q.ID != "0" || q.Prompt != w.prompt || !reflect.DeepEqual(labels, w.options) || q.Multi || !q.AllowText {
			t.Errorf("%s: question %+v", id, q)
		}
		if got := it.Question.Answered["0"]; !reflect.DeepEqual(got, w.answer) || it.Question.Answered == nil {
			t.Errorf("%s: answered %v, want %v", id, it.Question.Answered, w.answer)
		}
		if it.Question.Approve != "" || it.Question.Deny != "" || it.Question.Answerable != nil {
			t.Errorf("%s: approve/deny/answerable set: %+v", id, it.Question)
		}
	}
	var users []string
	for _, it := range items {
		if it.Kind == harness.KindTool && it.Tool.Name == askTool {
			t.Errorf("ask_question also drawn as a tool row: %s", it.ID)
		}
		if it.Kind == harness.KindUser {
			users = append(users, it.User.Text)
		}
	}
	// The <hook_context> Cline adds after an answer is not the user's, and
	// the Plan-mode prompt loses its <mode_notice>.
	if len(users) != 5 {
		t.Fatalf("user items %q", users)
	}
	for _, u := range users {
		if strings.Contains(u, "<hook_context") || strings.Contains(u, "mode_notice") {
			t.Errorf("user item %q", u)
		}
	}
	if users[4] != "Make a short 3-step plan to add a README to this project. Do not make changes." {
		t.Errorf("plan prompt %q", users[4])
	}
	if m.State()["mode"] != "plan" {
		t.Errorf("mode %q", m.State()["mode"])
	}
}

// While Cline waits on a question the item is open and the turn runs; the
// pane looking idle does not end the turn; the answer closes it.
func TestAskQuestionOpenUntilAnswered(t *testing.T) {
	h, d := askHome(t)
	m := newMessages(askEnv(h))
	const id = "cIXbfp0DFHkX3hoZ31yrcTN8wMMAfnG8"
	items, _ := m.Map(harness.Record{Source: "messages", Data: docOf(t, d, 2)})
	byID := latest(items)
	q := byID[id]
	if q.Kind != harness.KindQuestion || q.Question.Answered != nil {
		t.Fatalf("pending question %+v", q.Question)
	}
	if tr := byID[q.TurnID]; tr.Kind != harness.KindTurn || tr.Turn.State != harness.StateRunning {
		t.Fatalf("turn %+v", tr.Turn)
	}
	if out := m.CloseIdle(time.Now()); len(out) != 0 {
		t.Fatalf("idle while asking closed %+v", out)
	}
	items, _ = m.Map(harness.Record{Source: "messages", Data: docOf(t, d, 4)})
	q = latest(items)[id]
	if q.Question == nil || !reflect.DeepEqual(q.Question.Answered, map[string][]string{"0": {"Python"}}) {
		t.Fatalf("answered %+v", q.Question)
	}
	// Answered, the turn goes on: still running.
	if len(m.CloseIdle(time.Now())) == 0 {
		t.Fatal("idle after the answer closed nothing")
	}
}

// A question left open when the next prompt starts a new turn (Cline was
// quit, or the run stopped) closes unanswered.
func TestAskQuestionOpenAtTurnEndClosesEmpty(t *testing.T) {
	h, d := askHome(t)
	m := newMessages(askEnv(h))
	cut := d
	cut.Messages = append(append([]message{}, d.Messages[:2]...), d.Messages[7])
	b, _ := json.Marshal(cut)
	items, _ := m.Map(harness.Record{Source: "messages", Data: b})
	q := latest(items)["cIXbfp0DFHkX3hoZ31yrcTN8wMMAfnG8"]
	if q.Question == nil || !reflect.DeepEqual(q.Question.Answered, map[string][]string{"0": {}}) {
		t.Fatalf("answered %+v", q.Question)
	}
}

// The real question screen lists the record's options in order, then the
// typed-response row; the key driver counts on that order (digit n picks
// option n, the response row is one past the last option).
func TestAskScreenMatchesTheRecord(t *testing.T) {
	for _, tc := range []struct {
		file   string
		prompt string
		rows   []string
	}{
		{"real-ask-single", "Which language would you like me to use for the hello world script?", []string{"> Go", "Python", "Rust", "Type a response..."}},
		{"real-ask-single-moved", "Which language would you like me to use for the hello world script?", []string{"Go", "> Python", "Rust", "Type a response..."}},
		{"real-ask-text", "What should the greeting text say?", []string{"Hello", "Hi there", "> Howdy partner|"}},
	} {
		b, err := os.ReadFile(filepath.Join("testdata", "screens", tc.file+".txt"))
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(string(b), "\n")
		at := -1
		for i, l := range lines {
			if strings.TrimSpace(l) == "Cline is asking a question" {
				at = i
			}
		}
		if at < 0 || strings.TrimSpace(lines[at+2]) != tc.prompt {
			t.Fatalf("%s: no question box", tc.file)
		}
		var rows []string
		for _, l := range lines[at+4:] {
			if strings.TrimSpace(l) == "" {
				break
			}
			rows = append(rows, strings.TrimSpace(l))
		}
		if !reflect.DeepEqual(rows, tc.rows) {
			t.Errorf("%s: rows %q, want %q", tc.file, rows, tc.rows)
		}
	}
}
