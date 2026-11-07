package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAskUserQuestionWaitsForThePhoneAndAllowsWithAnswers(t *testing.T) {
	home, sock := tapHome(t)
	d := startFakeDaemon(t, sock, func(method string, _ json.RawMessage) (any, int) {
		if method != "local.question" {
			return map[string]any{}, 0
		}
		return map[string]any{"reason": "answered", "answers": map[string]string{
			"Which color do you prefer?": "Teal\nish", "Which fruits do you like (pick any)": "Apple, Plum"}}, 0
	})
	env := map[string]string{"TOGO_HOME": home, "TOGO_SESSION": "s_aaaaaaaaaa"}
	// No --block: questions always take the question route.
	code, out, _ := runWith(t, env, fixtureBytes(t, "permission_request_ask.json"), "hook", "claude", "PermissionRequest")
	if code != 0 {
		t.Fatalf("code %d", code)
	}
	var got struct {
		HookSpecificOutput struct {
			HookEventName string `json:"hookEventName"`
			Decision      struct {
				Behavior     string `json:"behavior"`
				UpdatedInput struct {
					Questions []map[string]any  `json:"questions"`
					Answers   map[string]string `json:"answers"`
				} `json:"updatedInput"`
			} `json:"decision"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("out %q: %v", out, err)
	}
	dec := got.HookSpecificOutput.Decision
	if got.HookSpecificOutput.HookEventName != "PermissionRequest" || dec.Behavior != "allow" ||
		len(dec.UpdatedInput.Questions) != 2 || dec.UpdatedInput.Answers["Which fruits do you like (pick any)"] != "Apple, Plum" ||
		dec.UpdatedInput.Answers["Which color do you prefer?"] != "Teal\nish" {
		t.Fatalf("decision %+v", got)
	}
	calls := d.recorded()
	if len(calls) != 1 || calls[0].Method != "local.question" ||
		!strings.Contains(string(calls[0].Params), `"questions":["Which color do you prefer?","Which fruits do you like (pick any)"]`) {
		t.Fatalf("calls %+v", calls)
	}
}

func TestAskUserQuestionWithoutAnswerPrintsNothing(t *testing.T) {
	home, sock := tapHome(t)
	startFakeDaemon(t, sock, func(string, json.RawMessage) (any, int) {
		return map[string]any{"reason": "timeout"}, 0
	})
	env := map[string]string{"TOGO_HOME": home, "TOGO_SESSION": "s_aaaaaaaaaa"}
	if code, out, _ := runWith(t, env, fixtureBytes(t, "permission_request_ask.json"), "hook", "claude", "PermissionRequest"); code != 0 || out != "" {
		t.Fatalf("code %d out %q", code, out)
	}
}

func TestQuestionDecisionNeedsEveryAnswer(t *testing.T) {
	qs := json.RawMessage(`[{"question":"A?"},{"question":"B?"}]`)
	if questionDecision(qs, []string{"A?", "B?"}, map[string]string{"A?": "x"}) != nil {
		t.Fatal("a partial answer was allowed")
	}
	if questionDecision(qs, []string{"A?", "B?"}, map[string]string{"A?": "x", "B?": "y"}) == nil {
		t.Fatal("a full answer was refused")
	}
}
