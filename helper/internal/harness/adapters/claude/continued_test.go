package claude

import (
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
)

// Claude Code continues on its own when a background shell or subagent it
// started finishes: a task notification arrives as a user message with no
// person behind it. That starts a new turn with a quiet marker naming the
// trigger, so the follow-up never runs on from the previous reply.
func TestTaskNotificationStartsItsOwnTurn(t *testing.T) {
	m := newTranscript(env())
	r := mapAll(m, records(t, "transcript-continued.jsonl"))

	if a := r.final["msg_1"]; a.TurnID != "u1" {
		t.Fatalf("first reply turn %q, want u1", a.TurnID)
	}
	n1 := r.final["n1"]
	if n1.Kind != harness.KindNotice || n1.TurnID != "n1" || n1.Notice.Code != harness.NoticeContinued ||
		n1.Notice.Level != "info" || n1.Notice.Text != "Subagent finished" {
		t.Fatalf("marker = %+v %+v", n1, n1.Notice)
	}
	if a := r.final["msg_3"]; a.TurnID != "n1" {
		t.Fatalf("follow-up turn %q, want n1", a.TurnID)
	}
	if s := r.final["toolu_9"]; s.TurnID != "n1" {
		t.Fatalf("follow-up step turn %q, want n1", s.TurnID)
	}
	turn := r.final["n1.turn"]
	if turn.Turn == nil || turn.Turn.Steps != 1 || turn.Turn.State != harness.StateOK || turn.Turn.Ended == nil ||
		!turn.Turn.Started.Equal(n1.At) {
		t.Fatalf("follow-up fold = %+v", turn.Turn)
	}
	if u1 := r.final["u1.turn"]; u1.Turn.Steps != 0 || u1.Turn.Ended == nil {
		t.Fatalf("first turn = %+v", u1.Turn)
	}

	// An older transcript has no origin; the tag alone marks it. A nonzero
	// exit reads as a failure.
	n2 := r.final["n2"]
	if n2.Kind != harness.KindNotice || n2.Notice.Text != "Background task failed" || r.final["msg_4"].TurnID != "n2" {
		t.Fatalf("second marker = %+v %+v, reply turn %q", n2, n2.Notice, r.final["msg_4"].TurnID)
	}
	if _, ok := r.final["n2.turn"]; !ok {
		t.Fatal("second follow-up has no fold")
	}

	// A notification inside a running turn is not a new turn.
	if _, ok := r.final["n3"]; ok {
		t.Fatal("a notification mid-turn drew a marker")
	}
	if a := r.final["msg_5"]; a.TurnID != "u2" {
		t.Fatalf("mid-turn reply turn %q, want u2", a.TurnID)
	}
	for _, it := range r.items {
		if it.Kind == harness.KindUser && (it.ID == "n1" || it.ID == "n2" || it.ID == "n3") {
			t.Fatalf("a task notification became a prompt: %+v", it)
		}
	}
}

func TestTaskTriggerText(t *testing.T) {
	cases := map[string]string{
		"<status>completed</status><summary>Agent \"x\" finished</summary>":                             "Subagent finished",
		"<status>failed</status><summary>Agent \"x\" failed</summary>":                                  "Subagent failed",
		"<status>killed</status><summary>Agent \"x\" was stopped</summary>":                             "Subagent stopped",
		"<status>completed</status><summary>Background command \"x\" completed (exit code 0)</summary>": "Background task finished",
		"<status>failed</status><summary>Background command failed</summary>":                           "Background task failed",
		"<status>completed</status>": "Task finished",
	}
	m := newTranscript(env())
	for in, want := range cases {
		if got := m.trigger("<task-notification><task-id>z</task-id>" + in + "</task-notification>"); got != want {
			t.Errorf("trigger(%q) = %q, want %q", in, got, want)
		}
	}
}
