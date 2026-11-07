package engine

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/wire"
)

type fakeApprovals struct {
	mu      sync.Mutex
	pending []wire.Approval
	answers []string
}

func (f *fakeApprovals) Pending(string) []wire.Approval {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]wire.Approval(nil), f.pending...)
}

func (f *fakeApprovals) Answer(_ context.Context, id, raw, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.answers = append(f.answers, id+" "+raw)
	return nil
}

func questionEngine(t *testing.T, id string, ap *fakeApprovals) (*Engine, *sess) {
	t.Helper()
	pane := &seqPane{mode: "a"}
	e := newSeq(t, id, pane, nil)
	e.cfg.Approvals = ap
	s, err := e.served(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return e, s
}

var pickColor = harness.Item{ID: "q1", Kind: harness.KindQuestion, Question: &harness.QuestionBody{Questions: []harness.Question{{
	ID: "color", Prompt: "Which color?",
	Options: []harness.Choice{{ID: "c1", Label: "Red"}, {ID: "c2", Label: "Blue"}},
}}}}

var bashAsk = wire.Approval{RequestID: "rq_bash", Title: "Bash", Snippet: "rm -rf build", CreatedAt: time.Unix(2, 0),
	Options: []wire.Option{{N: 1, Label: "Yes"}, {N: 2, Label: "No"}}}

var colorAsk = wire.Approval{RequestID: "rq_color", Title: "Which color?", CreatedAt: time.Unix(1, 0),
	Options: []wire.Option{{N: 1, Label: "Red"}, {N: 2, Label: "Blue"}, {N: 3, Label: "Type something"}}}

// A question answer reaches the question's own request even when a newer
// approval is pending in the same session.
func TestQuestionAnswerGoesToItsOwnRequest(t *testing.T) {
	const id = "s_question01"
	ap := &fakeApprovals{pending: []wire.Approval{colorAsk, bashAsk}}
	e, s := questionEngine(t, id, ap)
	e.log.Append(id, []harness.Item{pickColor})
	res, err := e.answerQuestion(context.Background(), s, "q1", map[string][]string{"color": {"c2"}})
	if err != nil || res.Result != harness.ResultApplied {
		t.Fatalf("res %+v %v", res, err)
	}
	if len(ap.answers) != 1 || ap.answers[0] != "rq_color option:2" {
		t.Fatalf("answers %q", ap.answers)
	}
}

// With no request of its own pending, a question is never answered
// through another approval, by label or by position.
func TestQuestionAnswerNeverFallsBack(t *testing.T) {
	const id = "s_question02"
	ap := &fakeApprovals{pending: []wire.Approval{bashAsk}}
	e, s := questionEngine(t, id, ap)
	e.log.Append(id, []harness.Item{pickColor})
	for _, c := range []struct {
		target  string
		answers map[string][]string
	}{
		{"q1", map[string][]string{"color": {"c1"}}},
		{"missing", map[string][]string{"color": {"opt1"}}},
		{"q1", map[string][]string{"other": {"opt1"}}},
	} {
		res, err := e.answerQuestion(context.Background(), s, c.target, c.answers)
		if err == nil && res.Result == harness.ResultApplied {
			t.Errorf("%s %v answered: %+v", c.target, c.answers, res)
		}
	}
	if len(ap.answers) != 0 {
		t.Fatalf("answers sent %q", ap.answers)
	}
}

// Two pending requests that both fit the question are ambiguous: nothing
// is answered.
func TestQuestionAnswerRefusesAmbiguity(t *testing.T) {
	const id = "s_question03"
	other := colorAsk
	other.RequestID = "rq_color2"
	ap := &fakeApprovals{pending: []wire.Approval{colorAsk, other}}
	e, s := questionEngine(t, id, ap)
	e.log.Append(id, []harness.Item{pickColor})
	if res, _ := e.answerQuestion(context.Background(), s, "q1", map[string][]string{"color": {"c1"}}); res.Result == harness.ResultApplied {
		t.Fatalf("ambiguous answer applied: %+v", res)
	}
	if len(ap.answers) != 0 {
		t.Fatalf("answers sent %q", ap.answers)
	}
}

// An approval whose close was handled before its open adds no item and no
// Needs row; one still pending does, and its close resolves it.
func TestApprovalOpenAfterCloseLeavesNothing(t *testing.T) {
	const id = "s_apporder01"
	ap := &fakeApprovals{}
	e, s := questionEngine(t, id, ap)
	closed := wire.Event{Kind: wire.EventApprovalClosed, Payload: wire.ApprovalClosedEvent{RequestID: "rq_fast", Resolution: "allow"}}
	e.approvalClosed(closed)
	e.addApproval(s, wire.Approval{RequestID: "rq_fast", Title: "Bash"})
	if _, ok := e.log.Get(id, "approval-rq_fast"); ok {
		t.Fatal("a closed approval got an item")
	}
	ap.pending = []wire.Approval{{RequestID: "rq_slow", Title: "Bash"}}
	e.addApproval(s, ap.pending[0])
	if _, ok := e.log.Get(id, "approval-rq_slow"); !ok {
		t.Fatal("a pending approval got no item")
	}
	ap.pending = nil
	e.approvalClosed(wire.Event{Kind: wire.EventApprovalClosed, Payload: wire.ApprovalClosedEvent{RequestID: "rq_slow", Resolution: "deny"}})
	if it, _ := e.log.Get(id, "approval-rq_slow"); it.Approval == nil || it.Approval.Resolved != "deny" {
		t.Fatalf("not resolved: %+v", it.Approval)
	}
}

// Phone args and query keys cannot replace the helper's own template
// values: the session folder, its key, the endpoint or state.
func TestPhoneInputCannotOverrideSessionValues(t *testing.T) {
	for _, k := range []string{"cwd_key", "cwd", "state", "port", "home", "session"} {
		if _, err := checkArgs(map[string]json.RawMessage{k: json.RawMessage(`"x"`)}); err == nil {
			t.Errorf("arg %s accepted", k)
		}
	}
	const id = "s_reserved01"
	e, s := questionEngine(t, id, &fakeApprovals{})
	if _, _, err := e.fetch(context.Background(), s, "models", map[string]string{"cwd_key": "../../x"}, true); err == nil {
		t.Error("query cwd_key accepted")
	}
	s.mu.Lock()
	data := s.templateDataLocked(map[string]any{"cwd": "/etc", "state": map[string]any{"harness_session": "other"}, "id": "m1"})
	s.mu.Unlock()
	if data["cwd"] == "/etc" || data["id"] != "m1" {
		t.Fatalf("data %v", data)
	}
	if st, _ := data["state"].(map[string]any); st["harness_session"] == "other" {
		t.Fatal("state replaced")
	}
}

func TestFlagFromInput(t *testing.T) {
	for _, c := range []struct {
		tmpl, out string
		want      bool
	}{
		{"{{.id}}", "--dangerously-skip-permissions", true},
		{"{{.id}}", "abc", false},
		{"--model={{.id}}", "--model=x", false},
		{"-m", "-m", false},
	} {
		if got := flagFromInput(c.tmpl, c.out); got != c.want {
			t.Errorf("%q -> %q: %v", c.tmpl, c.out, got)
		}
	}
}

func TestJQSeesNoEnvironment(t *testing.T) {
	t.Setenv("TOGO_JQ_SECRET", "plain")
	for _, expr := range []string{`env.TOGO_JQ_SECRET`, `$ENV.TOGO_JQ_SECRET`} {
		out, err := JQ(expr, map[string]any{}, nil)
		if err == nil && len(out) > 0 && out[0] == "plain" {
			t.Errorf("%s read the environment", expr)
		}
	}
}
