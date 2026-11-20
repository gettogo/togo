package apifed

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/e2e/support"
	"github.com/gettogo/togo/helper/internal/harness/enginetest"
	"github.com/gettogo/togo/helper/internal/harness/sessiongen"
)

// sendGoesTo sends a prompt from the phone and checks the agent was given
// it in session want.
func (s *apiSession) sendGoesTo(t *testing.T, text, want string) {
	t.Helper()
	if _, err := s.r.Send(text, harness.SendNow); err != nil {
		t.Fatalf("send %q: %v", text, err)
	}
	for _, p := range s.prompts() {
		if p[1] == text {
			if p[0] != want {
				t.Errorf("%q went to session %s, the terminal shows %s", text, p[0], want)
			}
			return
		}
	}
	t.Errorf("%q never reached the agent: %v", text, s.prompts())
}

// apiNewAndResume: a new conversation started in the agent's own terminal,
// then the phone resuming the first one; a prompt sent from the phone
// right after each goes to the conversation on screen.
func apiNewAndResume(t *testing.T, agent string) {
	s := apiStart(t, agent, 55, nil, sessiongen.Turn{Say: 20, Tools: 1}, sessiongen.Turn{Say: 20})
	support.SettleUntil(s.r, 2)
	first := s.p.Session()
	s.answerPrompts(t)
	s.add(t, sessiongen.Turn{New: true, Say: 20})
	second := s.p.Session()
	support.SettleUntil(s.r, 3)
	s.sendGoesTo(t, "After the new conversation", second)
	s.r.SettleUntil(support.Quick, func(st enginetest.State) bool { return !st.Mission.Summary.Working })
	before := s.r.Read().Epoch
	res, err := s.r.Act("resume", "", map[string]any{"id": first})
	if err != nil || !confirmedAct(res) {
		t.Fatalf("resume: %+v %v", res, err)
	}
	c := s.waitPost(t, "/tui/select-session")
	if !strings.Contains(c.Body, first) {
		t.Errorf("resume selected %s, want %s", c.Body, first)
	}
	// The agent's stream says the terminal shows the first one now: the
	// chat starts over on it. Until then the helper does not know, and a
	// prompt sent in the gap goes to the conversation it last knew (or is
	// refused, if the move arrives while it travels).
	s.r.SettleUntil(support.Quick, func(st enginetest.State) bool { return st.Epoch != before })
	s.sendGoesTo(t, "After the resume", first)
}

func confirmedAct(r harness.ActionResult) bool {
	return r.Result == harness.ResultApplied || r.Result == harness.ResultTyped || r.Result == harness.ResultRelaunched
}

// apiNeeds: a permission and a question from the agent's API reach the
// phone as needing the user; the phone's answers go back to the API as the
// agent takes them, the items close, and the turn carries on.
func apiNeeds(t *testing.T, agent string) {
	s := apiStart(t, agent, 57, nil, sessiongen.Turn{Say: 20})
	support.SettleUntil(s.r, 1)
	for _, q := range []struct {
		question     bool
		answer, body string
	}{
		{false, "once", `"reply":"once"`},
		{true, "blue", `"answers":[["blue"]]`},
	} {
		s.mu.Lock()
		support.Now(s.l)
		id, err := s.p.Ask(s.l, q.question)
		s.mu.Unlock()
		if err != nil {
			t.Fatal(err)
		}
		st := s.r.SettleUntil(support.Quick, func(st enginetest.State) bool { return st.Mission.Summary.NeedsYou > 0 })
		var item harness.Item
		for _, it := range support.Items(st, support.OfKind(harness.KindQuestion)) {
			if strings.HasPrefix(it.ID, id) || strings.Contains(it.ID, id) {
				item = it
			}
		}
		if item.Question == nil {
			t.Fatalf("no question item for %s", id)
		}
		res, err := s.r.Act("answer", item.ID, map[string]any{"answers": map[string][]string{"0": {q.answer}}})
		if err != nil || res.Result == harness.ResultFailed {
			t.Fatalf("answer %s: %+v %v", id, res, err)
		}
		c := s.waitPost(t, id+"/reply")
		if !strings.Contains(strings.ReplaceAll(c.Body, " ", ""), q.body) {
			t.Errorf("answer body %s, want %s", c.Body, q.body)
		}
		st = s.r.SettleUntil(support.Quick, func(st enginetest.State) bool { return st.Mission.Summary.NeedsYou == 0 })
		if st.Mission.Summary.NeedsYou != 0 {
			t.Errorf("still needs the user after the answer")
		}
		s.mu.Lock()
		support.Now(s.l)
		err = s.p.EndAsk(s.l)
		s.mu.Unlock()
		if err != nil {
			t.Fatal(err)
		}
	}
	st := s.r.SettleUntil(support.Quick, func(st enginetest.State) bool { return !st.Mission.Summary.Working && support.HasPrompt(3)(st) })
	support.NothingRuns(t, st)
	support.CheckEdge(t, "after the answers", st, s.l)
}

// apiSettings: model, mode, a / command, compact and a provider key from
// the phone are calls to the agent's API carrying what was picked, and
// the status shows the model and mode the agent then runs.
func apiSettings(t *testing.T, agent string) {
	s := apiStart(t, agent, 59, nil, sessiongen.Turn{Say: 20})
	support.SettleUntil(s.r, 1)
	sess := s.p.Session()
	ref := `{"id":"other-model","providerID":"other"}`
	if res, err := s.r.Act("set_model", "", map[string]any{"id": ref}); err != nil || res.Result == harness.ResultFailed {
		t.Fatalf("set_model: %+v %v", res, err)
	}
	c := s.waitPost(t, "/api/session/"+sess+"/model")
	var body struct {
		Model map[string]string `json:"model"`
	}
	if json.Unmarshal([]byte(c.Body), &body) != nil || body.Model["id"] != "other-model" || body.Model["providerID"] != "other" {
		t.Errorf("model call body %s", c.Body)
	}
	mode := "plan"
	if res, err := s.r.Act("set_mode", "", map[string]any{"id": mode}); err != nil || res.Result == harness.ResultFailed {
		t.Fatalf("set_mode: %+v %v", res, err)
	}
	if c := s.waitPost(t, "/api/session/"+sess+"/agent"); !strings.Contains(c.Body, `"plan"`) {
		t.Errorf("mode call body %s", c.Body)
	}
	// The agent runs the next turn with them.
	s.p.Mode, s.p.Provider, s.p.Model = mode, "other", "other-model"
	s.add(t, sessiongen.Turn{Say: 20})
	st := support.SettleUntil(s.r, 2)
	st = s.r.SettleUntil(support.Quick, func(st enginetest.State) bool { return strings.Contains(support.StatusOf(st).Model, "other-model") })
	if got := support.StatusOf(st); !strings.Contains(got.Model, "other-model") || !strings.EqualFold(got.Mode, mode) {
		t.Errorf("status %+v, want the new model and mode", got)
	}
	s.r.SettleUntil(support.Quick, func(st enginetest.State) bool { return !st.Mission.Summary.Working })
	if _, err := s.r.Send("/init", harness.SendNow); err != nil {
		t.Fatalf("send /init: %v", err)
	}
	if c := s.waitPost(t, "/session/"+sess+"/command"); !strings.Contains(c.Body, `"init"`) {
		t.Errorf("command body %s", c.Body)
	}
	for _, p := range s.prompts() {
		if strings.HasPrefix(p[1], "/init") {
			t.Errorf("the / line went to the model as prompt text")
		}
	}
	if res, err := s.r.Act("compact", "", nil); err != nil || res.Result == harness.ResultFailed {
		t.Fatalf("compact: %+v %v", res, err)
	}
	if c := s.waitPost(t, "/session/"+sess+"/summarize"); !strings.Contains(c.Body, "other-model") {
		t.Errorf("compact body %s, want the session's model", c.Body)
	}
	if res, err := s.r.Act("set_provider_key", "openai", map[string]any{"value": "sk-test-0000"}); err != nil || res.Result == harness.ResultFailed {
		t.Fatalf("set_provider_key: %+v %v", res, err)
	}
	if c := s.waitPost(t, "/auth/openai"); !strings.Contains(c.Body, "sk-test-0000") {
		t.Errorf("key body %s", c.Body)
	}
	scr, err := s.r.Screen("providers", nil)
	if err != nil {
		t.Fatalf("providers screen: %v", err)
	}
	raw, _ := json.Marshal(scr)
	if strings.Contains(string(raw), "sk-") {
		t.Errorf("the providers screen carries a key: %s", raw)
	}
}

// apiQueue: prompts sent while the agent works wait in the helper and go
// to the agent one at a time, in order, each after the turn before it
// ended; none skips the queue.
func apiQueue(t *testing.T, agent string) {
	s := apiStart(t, agent, 61, nil, sessiongen.Turn{Say: 20})
	support.SettleUntil(s.r, 1)
	s.answerPrompts(t)
	release := s.hold(t, 1, sessiongen.Turn{Say: 30, Tools: 1})
	// The prompts below are queued only while a turn runs: an idle agent
	// takes them at once, which is right, and is not what this checks.
	if st := s.r.SettleUntil(support.Quick, func(st enginetest.State) bool { return st.Mission.Summary.Working }); !st.Mission.Summary.Working {
		t.Fatal("the held turn never showed as working")
	}
	want := []string{"First queued", "Second queued", "Third queued"}
	for _, w := range want {
		res, err := s.r.Send(w, harness.SendQueue)
		if err != nil || res.Item.User == nil || !res.Item.User.Queued {
			t.Fatalf("queue %q: %+v %v", w, res.Item.User, err)
		}
	}
	time.Sleep(300 * time.Millisecond)
	if got := s.prompts(); len(got) != 0 {
		t.Fatalf("a prompt went to the agent while it worked: %v", got)
	}
	release()
	support.Eventually(t, 15*time.Second, "the queued prompts to reach the agent", func() bool { return len(s.prompts()) >= len(want) })
	got := s.prompts()
	for i, w := range want {
		if got[i][1] != w {
			t.Fatalf("prompts reached the agent as %v, want %v", got, want)
		}
	}
	st := s.r.SettleUntil(support.Quick, func(st enginetest.State) bool { return !st.Mission.Summary.Working && hasPromptText(st, want[2]) })
	support.NothingRuns(t, st)
}

func hasPromptText(st enginetest.State, text string) bool {
	for _, it := range st.Items {
		if it.User != nil && it.User.Text == text && !it.User.Queued {
			return true
		}
	}
	return false
}

// apiErrors: a send the agent's API refuses, or that finds the server
// gone, is reported to the phone and not shown as sent; a slow answer is
// waited for, not given up on, and reported once it comes.
func apiErrors(t *testing.T, agent string) {
	s := apiStart(t, agent, 63, nil, sessiongen.Turn{Say: 20})
	support.SettleUntil(s.r, 1)
	s.api.Fail("/session/", 500, 1)
	if _, err := s.r.Send("Refused by the agent", harness.SendNow); err == nil {
		t.Error("a send the agent refused was reported as sent")
	}
	start := time.Now()
	s.api.Slow(1500 * time.Millisecond)
	res, err := s.r.Send("Answered slowly", harness.SendNow)
	s.api.Heal()
	if err != nil || res.Item.User == nil || res.Item.User.Text != "Answered slowly" {
		t.Errorf("slow send: %+v %v", res.Item.User, err)
	}
	if d := time.Since(start); d < 1400*time.Millisecond || d > 10*time.Second {
		t.Errorf("slow send returned after %v", d)
	}
	st := s.r.Settle(time.Second)
	for _, it := range st.Items {
		if it.User != nil && it.User.Text == "Refused by the agent" {
			t.Errorf("the refused prompt is in the chat as %+v", it.User)
		}
	}
	s.api.Stop()
	done := make(chan error, 1)
	go func() { _, err := s.r.Send("To a server that is gone", harness.SendNow); done <- err }()
	select {
	case err := <-done:
		if err == nil {
			t.Error("a send to a stopped server was reported as sent")
		}
	case <-time.After(20 * time.Second):
		t.Fatal("a send to a stopped server hung")
	}
}
