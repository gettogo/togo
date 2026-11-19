package apifed

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/coverage"
	"github.com/gettogo/togo/helper/internal/harness/e2e/support"
	"github.com/gettogo/togo/helper/internal/harness/enginetest"
	"github.com/gettogo/togo/helper/internal/harness/sessiongen"
)

// typedOnly: no prompt went to the agent's API; 2.x prompts are typed
// into the terminal, so they land in the conversation it shows.
func (s *oc2) typedOnly(t *testing.T) {
	t.Helper()
	for _, c := range s.api.Posts() {
		if strings.Contains(c.Path, "/prompt") || strings.HasSuffix(strings.SplitN(c.Path, "?", 2)[0], "/message") {
			t.Errorf("a prompt was posted to a session id: %s %s", c.Method, c.Path)
		}
	}
}

// sendLands sends a prompt from the phone and waits for it in the chat,
// in the conversation the terminal shows.
func (s *oc2) sendLands(t *testing.T, text string) enginetest.State {
	t.Helper()
	if _, err := s.r.Send(text, harness.SendNow); err != nil {
		t.Fatalf("send %q: %v", text, err)
	}
	// The engine shows a sent prompt at once; the agent's turn for it
	// is what says where it landed.
	support.Eventually(t, support.DriveLimit, "the agent to take "+text, func() bool { return slices.Contains(s.r.Term.Submitted(), text) })
	s.mu.Lock()
	last, sess := s.l.Truth.Last, s.p.Session()
	s.mu.Unlock()
	return support.WaitFor(t, s.r, "the phone's prompt "+text+" answered in "+sess, func(st enginetest.State) bool {
		_, ok := latestByID(st)[last]
		return ok && shown(text)(st) && !st.Mission.Summary.Working && s.conversation() == sess
	})
}

// TestOpenCode2NewAndResume: /new at the desk, then a prompt from the
// phone, lands in the new conversation, and the chat starts over on it;
// resuming the first conversation from the phone restarts the agent on it
// (a new server on a new port), the chat shows its past again and the
// phone's next prompt lands there. No prompt is ever posted to a session
// id (the fault this product had with OpenCode). A conversation picked
// afterwards in the terminal's own list: see
// TestOpenCode2DeskResumeAfterPhoneResume.
func TestOpenCode2NewAndResume(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "opencode2/lifecycle/new_conversation", "opencode2/lifecycle/resume",
		"opencode2/capability/resume", "opencode2/capability/send.api")
	s := oc2Start(t, 71, nil, sessiongen.Turn{Say: 30, Tools: 1}, sessiongen.Turn{Say: 20})
	support.SettleUntil(s.r, 2)
	first := s.p.Session()
	fresh := false
	s.r.Term.OnSubmit = func(text string) {
		s.mu.Lock()
		defer s.mu.Unlock()
		support.Now(s.l)
		_ = s.l.Append(sessiongen.Turn{Prompt: text, Say: 12, New: fresh})
		fresh = false
	}
	// /new typed at the desk: the terminal shows an empty conversation,
	// made with the next prompt.
	s.mu.Lock()
	fresh = true
	s.mu.Unlock()
	st := s.sendLands(t, "After new from the phone")
	second := s.p.Session()
	if second == first || !slices.Equal(textsOf(st), []string{"After new from the phone"}) {
		t.Errorf("after /new: conversation %s (was %s), prompts %q", second, first, textsOf(st))
	}

	// Resume the first from the phone.
	scr, err := s.r.Screen("resume", nil)
	if err != nil || scr.Picker == nil {
		t.Fatalf("resume screen: %+v %v", scr, err)
	}
	var ids []string
	for _, it := range scr.Picker.Items {
		ids = append(ids, it.ID)
	}
	if !slices.Contains(ids, first) || !slices.Contains(ids, second) {
		t.Fatalf("the resume list %v lacks %s or %s", ids, first, second)
	}
	port := s.r.ServerPort()
	res, err := s.r.Act("resume", "", map[string]any{"id": first})
	if err != nil || res.Result != harness.ResultRelaunched {
		t.Fatalf("resume: %+v %v", res, err)
	}
	if rl := s.r.Relaunches(); len(rl) == 0 || !slices.Equal(rl[len(rl)-1], []string{"opencode", "--session", first}) {
		t.Fatalf("relaunches %q", rl)
	}
	s.p.SetMain(first)
	st = support.WaitFor(t, s.r, "the first conversation's past", func(st enginetest.State) bool { return support.HasPrompt(2)(st) && s.conversation() == first })
	if s.r.ServerPort() == port || s.r.ServerPort() == 0 {
		t.Errorf("the relaunch kept the server on port %d (now %d)", port, s.r.ServerPort())
	}
	st = s.sendLands(t, "After resume from the phone")
	if !support.HasPrompt(2)(st) {
		t.Errorf("the first conversation's past is not shown after the resume: %q", textsOf(st))
	}

	s.typedOnly(t)
}

// TestOpenCode2Interrupt: an interrupt from the phone in the middle of a
// turn is the agent's interrupt call; the agent stops the step and the
// turn ends interrupted, with the notice, and nothing runs. A prompt sent
// afterwards is taken.
func TestOpenCode2Interrupt(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "opencode2/capability/interrupt", "opencode2/lifecycle/interrupt")
	s := oc2Start(t, 72, nil, sessiongen.Turn{Say: 30})
	support.SettleUntil(s.r, 1)
	s.answerTyped(t)
	s.do(t, func() (err error) { _, err = s.p.Hold(s.l, "Stop me"); return err })
	support.WaitFor(t, s.r, "the turn to run", func(st enginetest.State) bool { return st.Mission.Summary.Working })
	if _, err := s.r.Interrupt(); err != nil {
		t.Fatalf("interrupt: %v", err)
	}
	s.waitPost(t, "/api/session/"+s.p.Session()+"/interrupt")
	st := support.WaitFor(t, s.r, "the turn to end interrupted", func(st enginetest.State) bool {
		return len(support.TurnsIn(st, harness.StateInterrupted)) == 1 && !st.Mission.Summary.Working
	})
	if len(support.Items(st, support.Notice(harness.NoticeInterrupted))) != 1 {
		t.Error("no interrupted notice")
	}
	support.NothingRuns(t, st)
	s.sendLands(t, "After the interrupt")
	s.typedOnly(t)
}

// question finds the question item of a request.
func question(st enginetest.State, id string) harness.Item {
	for _, it := range support.Items(st, support.OfKind(harness.KindQuestion)) {
		if strings.HasPrefix(it.ID, id+"~") {
			return it
		}
	}
	return harness.Item{}
}

// TestOpenCode2Needs: a permission, the question tool's form and a form
// with required, preset and conditional fields, as 2.2.5 recorded them,
// are answered from the phone. Each answer goes to the request's own
// route with the body the server takes; the needs leave, the turns end.
func TestOpenCode2Needs(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "opencode2/capability/needs.approvals", "opencode2/capability/needs.questions")
	s := oc2Start(t, 73, nil, sessiongen.Turn{Say: 20})
	support.SettleUntil(s.r, 1)
	for _, c := range []struct {
		kind    string
		answers map[string][]string
		want    string
	}{
		{sessiongen.AskPermission, map[string][]string{"0": {"once"}}, `{"decision":"once"}`},
		{sessiongen.AskQuestion, map[string][]string{"q0": {"Blue"}}, `{"answer":{"q0":"Blue"}}`},
		// forms.sse's own reply: notify false hides the channel and asks
		// why; Mac picked shows its note.
		{sessiongen.AskRelease, map[string][]string{"notify!": {"false"}, "targets[]": {"ios", "mac"}, "mac_note": {"needs signing"},
			"retries#": {"3"}, "quiet_reason": {"no time"}},
			`{"answer":{"mac_note":"needs signing","notify":false,"quiet_reason":"no time","retries":3,"targets":["ios","mac"]}}`},
	} {
		var id string
		s.do(t, func() (err error) { id, err = s.p.Ask(s.l, c.kind); return err })
		st := support.WaitFor(t, s.r, c.kind+" to need the user", func(st enginetest.State) bool { return question(st, id).Question != nil })
		it := question(st, id)
		if c.kind == sessiongen.AskRelease {
			when, preset := 0, 0
			for _, q := range it.Question.Questions {
				if q.When != nil {
					when++
				}
				if q.Default != nil {
					preset++
				}
			}
			if when != 3 || preset < 4 {
				t.Errorf("the release form shows %d conditional and %d preset fields: %+v", when, preset, it.Question.Questions)
			}
		}
		res, err := s.r.Act("answer", it.ID, map[string]any{"answers": c.answers})
		if err != nil || res.Result == harness.ResultFailed {
			t.Fatalf("answer %s: %+v %v", c.kind, res, err)
		}
		call := s.waitPost(t, id+"/reply")
		var got, want any
		_ = json.Unmarshal([]byte(call.Body), &got)
		_ = json.Unmarshal([]byte(c.want), &want)
		gb, _ := json.Marshal(got)
		wb, _ := json.Marshal(want)
		if string(gb) != string(wb) || call.Status != 204 {
			t.Errorf("%s answer %s (%d), want %s", c.kind, gb, call.Status, wb)
		}
		support.WaitFor(t, s.r, "the need to leave", func(st enginetest.State) bool { return st.Mission.Summary.NeedsYou == 0 })
		s.do(t, func() error { return s.p.EndAsk(s.l) })
	}
	st := support.WaitFor(t, s.r, "the turns to end", func(st enginetest.State) bool { return support.HasPrompt(4)(st) && !st.Mission.Summary.Working })
	support.NothingRuns(t, st)
	support.CheckEdge(t, "after the answers", st, s.l)
}

// TestOpenCode2QueueAndErrors: a prompt sent while the agent works waits
// and is typed after the turn, in order; a turn the provider refuses
// shows its error and ends; an action the server fails or answers slowly
// reports so and does not hang; a send to an agent that exited is not
// reported as sent.
func TestOpenCode2QueueAndErrors(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "opencode2/capability/send.queue")
	s := oc2Start(t, 74, nil, sessiongen.Turn{Say: 20})
	support.SettleUntil(s.r, 1)
	s.answerTyped(t)
	var msg string
	s.do(t, func() (err error) { msg, err = s.p.Hold(s.l, "Working"); return err })
	support.WaitFor(t, s.r, "the turn to run", func(st enginetest.State) bool { return st.Mission.Summary.Working })
	res, err := s.r.Send("Queued behind", harness.SendQueue)
	if err != nil || res.Item.User == nil || !res.Item.User.Queued {
		t.Fatalf("queue: %+v %v", res, err)
	}
	if slices.Contains(s.r.Term.Submitted(), "Queued behind") {
		t.Fatal("the queued prompt was typed while the agent worked")
	}
	s.do(t, func() error { return s.p.Finish(s.l, msg) })
	st := support.WaitFor(t, s.r, "the queued prompt's turn", func(st enginetest.State) bool {
		return shown("Queued behind")(st) && !st.Mission.Summary.Working
	})
	if got := textsOf(st); len(got) < 2 || got[len(got)-2] != "Working" || got[len(got)-1] != "Queued behind" {
		t.Errorf("prompts in order %q", got)
	}

	s.add(t, sessiongen.Turn{Prompt: "Refused", Error: true})
	support.WaitFor(t, s.r, "the provider's error", func(st enginetest.State) bool {
		return noticeWith(st, "Quota exceeded") && !st.Mission.Summary.Working
	})

	sess := s.p.Session()
	s.api.Fail("/api/session/"+sess+"/agent", 500, 1)
	res2, err := s.r.Act("set_mode", "", map[string]any{"id": "plan"})
	if err == nil && res2.Result != harness.ResultFailed {
		t.Errorf("a mode change the server refused reads as %+v", res2)
	}
	s.api.Slow(1500 * time.Millisecond)
	begun := time.Now()
	res2, err = s.r.Act("set_mode", "", map[string]any{"id": "plan"})
	s.api.Heal()
	if err != nil || res2.Result == harness.ResultFailed || time.Since(begun) > 20*time.Second {
		t.Errorf("a slow mode change: %+v %v after %s", res2, err, time.Since(begun))
	}

	s.r.Exit()
	before := len(s.r.Term.Submitted())
	if res, err := s.r.Send("To nobody", harness.SendNow); err == nil && res.Item.User != nil && !res.Item.User.Queued {
		t.Errorf("a prompt to an agent that exited reads as sent: %+v", res.Item)
	}
	if len(s.r.Term.Submitted()) != before {
		t.Error("a prompt was typed into a pane with no agent")
	}
}

// After the phone resumes another conversation, the chat holds the
// resumed conversation only: nothing of the one the terminal left stays.
// The resume relaunches OpenCode on a new server, and the helper starts
// the session's adapter again there with its chat kept; the chat starts
// over because the new run names another conversation than the chat
// showed (engine/conversation.go convCarry). The phone's next prompt lands
// in the resumed conversation once, and a helper restart keeps it.
func TestOpenCode2PhoneResumeShowsOnlyTheResumedConversation(t *testing.T) {
	t.Parallel()
	s := oc2Start(t, 75, nil, sessiongen.Turn{Prompt: "In the first", Say: 20})
	support.WaitFor(t, s.r, "the first conversation", shown("In the first"))
	first := s.p.Session()
	s.do(t, func() error { return s.l.Append(sessiongen.Turn{Prompt: "In the second", Say: 20, New: true}) })
	support.WaitFor(t, s.r, "the second conversation", func(st enginetest.State) bool {
		return shown("In the second")(st) && !shown("In the first")(st)
	})
	if res, err := s.r.Act("resume", "", map[string]any{"id": first}); err != nil || res.Result != harness.ResultRelaunched {
		t.Fatalf("resume: %+v %v", res, err)
	}
	s.p.SetMain(first)
	support.WaitFor(t, s.r, "the first conversation's past", shown("In the first"))
	if st := s.r.Settle(2 * time.Second); shown("In the second")(st) {
		t.Fatalf("the chat still holds the conversation the terminal left: %q", textsOf(st))
	}
	s.r.Term.OnSubmit = func(text string) {
		s.mu.Lock()
		defer s.mu.Unlock()
		support.Now(s.l)
		_ = s.l.Append(sessiongen.Turn{Prompt: text, Say: 12})
	}
	st := s.sendLands(t, "After the resume, from the phone")
	if got := textsOf(st); !slices.Equal(got, []string{"In the first", "After the resume, from the phone"}) {
		t.Fatalf("after the phone's prompt the chat holds %q", got)
	}
	s.r.Restart()
	st = support.WaitFor(t, s.r, "the resumed conversation after a restart", shown("After the resume, from the phone"))
	if got := textsOf(st); !slices.Equal(got, []string{"In the first", "After the resume, from the phone"}) {
		t.Fatalf("after a restart the chat holds %q", got)
	}
}

// After the phone resumed one conversation, the user picks another in the
// terminal's own list and types there: the chat shows that conversation's
// past and the prompt just typed, and nothing of the one left.
//
// This was F30: the resume relaunches OpenCode with --session, and the
// fresh mapper that read the new conversation's past started on the
// resumed one, dropped the past, and the engine emptied the chat a second
// time (adapters/opencode/history2.go). Whether the typed prompt survived
// depended on whether its event was read before or after that reset, so
// the race detector's timing showed it and a plain run did not.
func TestOpenCode2DeskResumeAfterPhoneResume(t *testing.T) {
	t.Parallel()
	s := oc2Start(t, 76, nil, sessiongen.Turn{Prompt: "In the first", Say: 20})
	support.WaitFor(t, s.r, "the first conversation", shown("In the first"))
	first := s.p.Session()
	s.do(t, func() error { return s.l.Append(sessiongen.Turn{Prompt: "In the second", Say: 20, New: true}) })
	support.WaitFor(t, s.r, "the second conversation", func(st enginetest.State) bool { return shown("In the second")(st) })
	second := s.p.Session()
	if res, err := s.r.Act("resume", "", map[string]any{"id": first}); err != nil || res.Result != harness.ResultRelaunched {
		t.Fatalf("resume: %+v %v", res, err)
	}
	s.p.SetMain(first)
	support.WaitFor(t, s.r, "the first conversation's past", shown("In the first"))
	s.do(t, func() error {
		s.p.SetMain(second)
		return s.l.Append(sessiongen.Turn{Prompt: "Typed at the desk", Say: 10})
	})
	support.Eventually(t, support.DriveLimit, "the chat to follow the desk", func() bool { return s.conversation() == second })
	st := support.WaitFor(t, s.r, "the desk's conversation and its new prompt", func(st enginetest.State) bool {
		return shown("In the second")(st) && shown("Typed at the desk")(st)
	})
	st = s.r.Settle(2 * time.Second)
	if !shown("In the second")(st) || !shown("Typed at the desk")(st) || shown("In the first")(st) {
		t.Fatalf("the chat shows %q, want the second conversation's past and the prompt typed at the desk only", textsOf(st))
	}
}
