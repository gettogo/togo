package cursor

import (
	"bufio"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
)

type toolUse struct {
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

// transcriptCalls lists the tool_use blocks of a real agent transcript.
func transcriptCalls(t *testing.T, name string) []toolUse {
	t.Helper()
	f, err := os.Open("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out []toolUse
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 4<<20)
	for sc.Scan() {
		var rec struct {
			Message struct {
				Content []struct {
					Type string `json:"type"`
					toolUse
				} `json:"content"`
			} `json:"message"`
		}
		if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
			t.Fatal(err)
		}
		for _, c := range rec.Message.Content {
			if c.Type == "tool_use" {
				out = append(out, c.toolUse)
			}
		}
	}
	return out
}

func find(calls []toolUse, name string) toolUse {
	for _, c := range calls {
		if c.Name == name {
			return c
		}
	}
	return toolUse{}
}

// The real two-question call: Cursor's question ids, labels as option ids,
// allow_multiple as multi, Other as text, the title as the first header.
func TestAskQuestionFromRealTranscript(t *testing.T) {
	c := find(transcriptCalls(t, "transcript-ask-batch.jsonl"), "AskQuestion")
	q, ok := askQuestion(c.Name, c.Input)
	if !ok {
		t.Fatal("AskQuestion not read")
	}
	want := []harness.Question{
		{ID: "tone", Header: "README preferences", Prompt: "What tone should the README use?", AllowText: true,
			Options: []harness.Choice{{ID: "Formal", Label: "Formal"}, {ID: "Casual", Label: "Casual"}}},
		{ID: "sections", Prompt: "Which sections should the README include? (select all that apply)", Multi: true, AllowText: true,
			Options: []harness.Choice{{ID: "Usage", Label: "Usage"}, {ID: "License", Label: "License"}, {ID: "Credits", Label: "Credits"}}},
	}
	if !reflect.DeepEqual(q.Questions, want) {
		t.Fatalf("got  %+v\nwant %+v", q.Questions, want)
	}
	// The same labels the dialog drew (testdata/screens/real-ask-q2.txt).
	screen, err := os.ReadFile("testdata/screens/real-ask-q2.txt")
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range want[1].Options {
		if !strings.Contains(string(screen), "[ ] "+o.Label) {
			t.Errorf("screen has no row %q", o.Label)
		}
	}
}

func TestAskQuestionSingleAndOthers(t *testing.T) {
	calls := transcriptCalls(t, "transcript-ask-skipped.jsonl")
	q, ok := askQuestion(askTool, find(calls, "AskQuestion").Input)
	if !ok || len(q.Questions) != 1 || q.Questions[0].ID != "greeting_word" || q.Questions[0].Multi || len(q.Questions[0].Options) != 2 {
		t.Fatalf("single: %+v %v", q, ok)
	}
	if _, ok := askQuestion("Shell", json.RawMessage(`{"command":"ls"}`)); ok {
		t.Error("Shell read as a question")
	}
	if _, ok := askQuestion(askTool, json.RawMessage(`{"questions":[]}`)); ok {
		t.Error("no questions read as one")
	}
	q, _ = askQuestion(askTool, json.RawMessage(`{"questions":[{"prompt":"p","allowMultiple":true,"options":[{"id":"a","label":"A"}]}]}`))
	if q.Questions[0].ID != "0" || !q.Questions[0].Multi {
		t.Errorf("camelCase / no id: %+v", q.Questions[0])
	}
}

// The real hooks of a Shell call: tool_use_id on both, the answer only in
// tool_output; AskQuestion and CreatePlan never appear in them.
func TestRealHooksCarryNoAsk(t *testing.T) {
	b, err := os.ReadFile("testdata/hooks-real-shell.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	var pre int
	for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		var h struct {
			Event   string `json:"event"`
			Payload struct {
				Tool string `json:"tool_name"`
				ID   string `json:"tool_use_id"`
			} `json:"payload"`
		}
		if err := json.Unmarshal([]byte(l), &h); err != nil {
			t.Fatal(err)
		}
		if h.Event == "preToolUse" {
			pre++
			if h.Payload.Tool != "Shell" || h.Payload.ID == "" {
				t.Errorf("preToolUse %+v", h.Payload)
			}
		}
	}
	if pre != 1 {
		t.Fatalf("%d preToolUse, want 1", pre)
	}
}
