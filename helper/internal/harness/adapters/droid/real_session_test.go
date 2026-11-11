package droid

import (
	"bufio"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

// realCalls reads the AskUser and ExitSpecMode calls of a real Droid 1.5.0
// session (Qwen custom model, 2026-11-10) with their results.
func realCalls(t *testing.T) map[string][2]json.RawMessage {
	t.Helper()
	f, err := os.Open("testdata/session-ask-spec.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	type block struct {
		Type    string          `json:"type"`
		ID      string          `json:"id"`
		Name    string          `json:"name"`
		Input   json.RawMessage `json:"input"`
		UseID   string          `json:"tool_use_id"`
		Content json.RawMessage `json:"content"`
	}
	names, inputs := map[string]string{}, map[string]json.RawMessage{}
	out := map[string][2]json.RawMessage{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<22)
	for sc.Scan() {
		var r struct {
			Message struct {
				Content []block `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(sc.Bytes(), &r) != nil {
			continue
		}
		for _, b := range r.Message.Content {
			switch b.Type {
			case "tool_use":
				names[b.ID], inputs[b.ID] = b.Name, b.Input
			case "tool_result":
				out[names[b.UseID]] = [2]json.RawMessage{inputs[b.UseID], b.Content}
			}
		}
	}
	return out
}

func TestRealAskUserQuestionnaire(t *testing.T) {
	c := realCalls(t)["AskUser"]
	q, ok := droidAsk("AskUser", c[0])
	if !ok || len(q.Questions) != 1 {
		t.Fatalf("question %+v %v", q, ok)
	}
	got := q.Questions[0]
	// The screen draws "Which color?", rows 1. Red … 3. Blue, then "Or
	// type your own answer...".
	if got.Prompt != "Which color?" || got.Header != "Color" || !got.AllowText || len(got.Options) != 3 || got.Options[1].ID != "Green" {
		t.Fatalf("question %+v", got)
	}
	if a := droidAnswered("AskUser", c[0], c[1]); !reflect.DeepEqual(a, map[string][]string{"0": {"Green"}}) {
		t.Fatalf("answered %v", a)
	}
}

func TestRealSpecApproval(t *testing.T) {
	c := realCalls(t)["ExitSpecMode"]
	plan, ok := specPlan("ExitSpecMode", c[0])
	if !ok || !strings.HasPrefix(plan, "Create `hello.txt`") {
		t.Fatalf("plan %q", plan)
	}
	q, ok := droidAsk("ExitSpecMode", c[0])
	if !ok || q.Questions[0].Prompt != "Propose Specification \"Create `hello.txt` in the working directory.\"" {
		t.Fatalf("question %+v", q)
	}
	// The real screen lists exactly these rows.
	screen, err := os.ReadFile("testdata/screens/real-spec-approval-80x24.txt")
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range q.Questions[0].Options {
		if !strings.Contains(string(screen), o.Label) {
			t.Errorf("row %q not on the real screen", o.Label)
		}
	}
	if a := droidAnswered("ExitSpecMode", c[0], c[1]); !reflect.DeepEqual(a, map[string][]string{"0": {"Proceed with the proposal"}}) {
		t.Fatalf("answered %v", a)
	}
	if a := droidAnswered("ExitSpecMode", c[0], json.RawMessage(`"{\"approved\":false}"`)); a["0"][0] != "No and explain why" {
		t.Fatalf("declined %v", a)
	}
}
