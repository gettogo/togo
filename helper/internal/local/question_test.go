package local

import (
	"context"
	"testing"

	"github.com/gettogo/togo/helper/internal/wire"
)

type waitingApprovals struct {
	*fakeApprovals
	answers map[string]string
	prompts []string
}

func (w *waitingApprovals) WaitQuestion(_ context.Context, _ string, prompts []string) (map[string]string, bool) {
	w.prompts = prompts
	return w.answers, w.answers != nil
}

func TestQuestionReturnsTheAnswers(t *testing.T) {
	h, _, ap, _ := setup()
	w := &waitingApprovals{fakeApprovals: ap, answers: map[string]string{"A?": "x"}}
	h.Approvals = w
	id := sid
	res, err := call(t, h, wire.MethodLocalQuestion, wire.QuestionParams{SessionID: &id, Questions: []string{"A?"}, TimeoutSec: 5})
	if err != nil {
		t.Fatal(err)
	}
	r := res.(wire.QuestionResult)
	if r.Reason != wire.AskAnswered || r.Answers["A?"] != "x" || len(w.prompts) != 1 {
		t.Fatalf("result %+v", r)
	}
}

func TestQuestionWithoutReachTimesOutAtOnce(t *testing.T) {
	h, _, ap, _ := setup()
	h.Approvals = &waitingApprovals{fakeApprovals: ap, answers: map[string]string{"A?": "x"}}
	h.Reach = func(*string) bool { return false }
	id := sid
	res, err := call(t, h, wire.MethodLocalQuestion, wire.QuestionParams{SessionID: &id, Questions: []string{"A?"}, TimeoutSec: 5})
	if err != nil || res.(wire.QuestionResult).Reason != wire.AskTimeout {
		t.Fatalf("res %+v err %v", res, err)
	}
}

func TestQuestionValidatesParams(t *testing.T) {
	h, _, _, _ := setup()
	id := sid
	for _, p := range []wire.QuestionParams{
		{Questions: []string{"A?"}, TimeoutSec: 5},
		{SessionID: &id, TimeoutSec: 5},
		{SessionID: &id, Questions: []string{" "}, TimeoutSec: 5},
		{SessionID: &id, Questions: []string{"A?"}, TimeoutSec: 0},
	} {
		if _, err := call(t, h, wire.MethodLocalQuestion, p); err == nil {
			t.Errorf("accepted %+v", p)
		}
	}
}

func TestPermissionNotificationDuringAskUserQuestionOpensNothing(t *testing.T) {
	h, _, ap, _ := setup()
	for _, ev := range []wire.HookEvent{
		hookEv(EventTool, wire.HookData{Tool: "AskUserQuestion"}),
		hookEv(EventPermission, wire.HookData{Message: "Claude needs your permission"}),
	} {
		if _, err := call(t, h, wire.MethodLocalHook, ev); err != nil {
			t.Fatal(err)
		}
	}
	if len(ap.opened) != 0 {
		t.Fatalf("opened %d approvals for a question", len(ap.opened))
	}
	// Once the question is done, a real permission prompt opens again.
	for _, ev := range []wire.HookEvent{
		hookEv(EventToolDone, wire.HookData{Tool: "AskUserQuestion"}),
		hookEv(EventTool, wire.HookData{Tool: "Bash"}),
		hookEv(EventPermission, wire.HookData{Tool: "Bash"}),
	} {
		if _, err := call(t, h, wire.MethodLocalHook, ev); err != nil {
			t.Fatal(err)
		}
	}
	if len(ap.opened) != 1 {
		t.Fatalf("opened %d approvals, want 1", len(ap.opened))
	}
}
