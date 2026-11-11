package vibe

import (
	"reflect"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
)

// The fixtures are real Vibe 3.1.0 journals (2026-11-10), scrubbed: a
// single question answered "SQLite" with the digit 2, a batch of a
// multi-select (Backend, Frontend) and a single answered by typing on its
// "Type your answer..." row, one cancelled with Esc and one with Ctrl+C.
func TestAskUserQuestion(t *testing.T) {
	final, order := replay(t, newTestSession(t, t.TempDir()), "journal-ask.jsonl")
	var asks []harness.Item
	for _, x := range order {
		if x.Kind == harness.KindTool && isAsk(x.Tool.Name) {
			t.Fatalf("ask drawn as a tool row too: %+v", x.Tool)
		}
		if x.Kind == harness.KindQuestion {
			asks = append(asks, x)
		}
	}
	if len(asks) != 4 {
		t.Fatalf("%d questions, want 4", len(asks))
	}

	single := final["effect-5902fa4a-7d0c-55c6-8ec2-a3883e6335cb"].Question
	want := []harness.Question{{ID: "0", Prompt: "Which database should we use?", AllowText: true, Options: []harness.Choice{
		{ID: "Postgres", Label: "Postgres", Detail: "Relational, mature"},
		{ID: "SQLite", Label: "SQLite", Detail: "Embedded file"},
		{ID: "MongoDB", Label: "MongoDB", Detail: "Document store"}}}}
	if single == nil || !reflect.DeepEqual(single.Questions, want) {
		t.Fatalf("single %+v", single)
	}
	if !reflect.DeepEqual(single.Answered, map[string][]string{"0": {"SQLite"}}) {
		t.Fatalf("single answered %v", single.Answered)
	}
	if single.Approve != "" || single.Deny != "" {
		t.Fatal("a question is no permission")
	}

	batch := final["effect-fcad95ee-474e-5e56-8786-7787bdd37404"].Question
	if batch == nil || len(batch.Questions) != 2 {
		t.Fatalf("batch %+v", batch)
	}
	q0, q1 := batch.Questions[0], batch.Questions[1]
	if q0.ID != "0" || q0.Header != "Areas" || !q0.Multi || !q0.AllowText || len(q0.Options) != 3 ||
		q1.ID != "1" || q1.Header != "Rollout" || q1.Multi || len(q1.Options) != 2 {
		t.Fatalf("batch questions %+v", batch.Questions)
	}
	if !reflect.DeepEqual(batch.Answered, map[string][]string{"0": {"Backend", "Frontend"}, "1": {"staged by region"}}) {
		t.Fatalf("batch answered %v", batch.Answered)
	}

	for _, id := range []string{"effect-9195cbf4-cb0c-552f-ac73-3b00edb009cc", "effect-8f6a2bb8-e850-5f7d-ae8e-58b6df83c3e7"} {
		q := final[id].Question
		if q == nil || !reflect.DeepEqual(q.Answered, map[string][]string{"0": {}}) {
			t.Fatalf("%s cancelled: %+v", id, q)
		}
	}
}

// While it is open, the question has no answer; the turn ending with it
// still open closes it.
func TestAskOpenUntilTurnEnds(t *testing.T) {
	s := newTestSession(t, t.TempDir())
	e := entry{Type: "effect", ID: "effect-1", TurnID: "turn-1", CreatedAt: 1000}
	e.Detail.ToolName = askTool
	e.Detail.Input = []byte(`{"questions":[{"question":"Pick","options":[{"label":"A"},{"label":"B"}],"hideOther":true}]}`)
	e.State.Status = "running"
	items := s.entry(e)
	q := last(items).Question
	if q == nil || q.Answered != nil || q.Questions[0].AllowText {
		t.Fatalf("open %+v", q)
	}
	items = s.end("turn-1", harness.StateInterrupted, ms(2000))
	var closed *harness.QuestionBody
	for _, x := range items {
		if x.ID == "effect-1" {
			closed = x.Question
		}
	}
	if closed == nil || !reflect.DeepEqual(closed.Answered, map[string][]string{"0": {}}) {
		t.Fatalf("closed %+v", closed)
	}
}

func last(items []harness.Item) harness.Item { return items[len(items)-1] }

// A multi-select answer is labels joined by ", "; a label holding ", "
// stays whole and leftover text is one typed value.
func TestSplitLabels(t *testing.T) {
	opts := []harness.Choice{{ID: "A"}, {ID: "B, C"}, {ID: "D"}}
	for ans, want := range map[string][]string{
		"A, B, C, D": {"A", "B, C", "D"},
		"D":          {"D"},
		"A, my own":  {"A", "my own"},
	} {
		if got := splitLabels(ans, opts); !reflect.DeepEqual(got, want) {
			t.Errorf("%q: %v, want %v", ans, got, want)
		}
	}
}

// A permission is a question with the dialog's rows, Approve and Deny
// set, the gated tool waiting meanwhile; the recorded decision answers it.
func TestApprovalCallback(t *testing.T) {
	cases := []struct {
		file, id, tool, answer string
		ran                    string
	}{
		{"journal-approval.jsonl", "approval-f01jIrolGbyT9W6ZUPlV1tlwT9zi0Rhl-5744f85d",
			"effect-95d48586-f564-5124-8cc8-09d3693a578f", "Allow once", harness.StateOK},
		{"journal-approval-deny.jsonl", "approval-8kRFD5zlDU2D230nxftmH4FI5GYbm1mm-e98da1b8",
			"effect-d223cf69-c95c-56bc-935c-67a5ea971a77", "Deny", harness.StateDenied},
	}
	for _, c := range cases {
		t.Run(c.file, func(t *testing.T) {
			s := newTestSession(t, t.TempDir())
			final, order := replay(t, s, c.file)
			q := final[c.id].Question
			if q == nil {
				t.Fatalf("no question for %s", c.id)
			}
			labels := []string{}
			for _, o := range q.Questions[0].Options {
				labels = append(labels, o.ID)
			}
			if !reflect.DeepEqual(labels, []string{"Allow once", "Allow for remainder of this session", "Always allow", "Deny"}) {
				t.Fatalf("options %v", labels)
			}
			if q.Approve != "Allow once" || q.Deny != "Deny" {
				t.Fatalf("approve %q deny %q", q.Approve, q.Deny)
			}
			if !reflect.DeepEqual(q.Answered, map[string][]string{"0": {c.answer}}) {
				t.Fatalf("answered %v", q.Answered)
			}
			p := q.Questions[0].Prompt
			if p != "Permission for the file_system.bash tool (touch *)\ntouch x.txt" && p != "Permission for the file_system.bash tool (touch *)\ntouch y.txt" {
				t.Fatalf("prompt %q", p)
			}
			if tool := final[c.tool].Tool; tool == nil || tool.State != c.ran {
				t.Fatalf("tool %+v", tool)
			}
			// The question comes after the tool it gates, which waited.
			var sawTool bool
			for _, x := range order {
				if x.ID == c.tool {
					sawTool = true
				}
				if x.ID == c.id && !sawTool {
					t.Fatal("question before its tool")
				}
			}
		})
	}
}

// The gated tool shows waiting between the callback and its decision.
func TestApprovalMarksToolWaiting(t *testing.T) {
	s := newTestSession(t, t.TempDir())
	e := entry{Type: "effect", ID: "effect-9", TurnID: "turn-9", CreatedAt: 1000}
	e.Detail.ToolName = "file_system.bash"
	e.Detail.Input = []byte(`{"command":"touch z"}`)
	s.entry(e)
	var c callbackEntry
	c.ID, c.TurnID, c.Related, c.CreatedAt = "approval-9", "turn-9", "effect-9", 1001
	c.Detail.Kind = "approval"
	c.Detail.Choices = []string{"approve", "deny", "cancel_turn"}
	c.Detail.Effect.ToolName = "file_system.bash"
	c.Detail.Effect.Input = []byte(`{"command":"touch z"}`)
	c.Detail.Reason = "Writes outside the project"
	items := s.callbackOpened(c)
	if len(items) != 2 || items[0].Tool == nil || items[0].Tool.State != harness.StateWaiting {
		t.Fatalf("items %+v", items)
	}
	q := items[1].Question
	if len(q.Questions[0].Options) != 2 || q.Questions[0].Prompt != "Permission for the file_system.bash tool\ntouch z\nWrites outside the project" {
		t.Fatalf("question %+v", q.Questions[0])
	}
	var r callbackResult
	r.Output.Type = "approval"
	r.Output.Decision.Type = "cancel_turn"
	items = s.callbackResolved("approval-9", r)
	if len(items) != 2 || !reflect.DeepEqual(items[0].Question.Answered, map[string][]string{"0": {}}) ||
		items[1].Tool.State != harness.StateRunning {
		t.Fatalf("resolved %+v", items)
	}
}
