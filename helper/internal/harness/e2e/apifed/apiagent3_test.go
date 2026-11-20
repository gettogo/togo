package apifed

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/e2e/support"
	"github.com/gettogo/togo/helper/internal/harness/enginetest"
	"github.com/gettogo/togo/helper/internal/harness/sessiongen"
)

// apiStreamDrop: the event stream drops and comes back, between turns and
// in the middle of one. What the agent did while it was back is shown once;
// nothing shown before is lost or doubled; no turn is left working after
// the agent went idle.
func apiStreamDrop(t *testing.T, agent string) {
	s := apiStart(t, agent, 65, nil, sessiongen.Turn{Say: 20, Tools: 1}, sessiongen.Turn{Say: 20})
	before := support.SettleUntil(s.r, 2)
	s.api.DropStreams()
	support.Eventually(t, 10*time.Second, "the stream to come back", func() bool { return s.api.Streams() == 1 })
	s.add(t, sessiongen.Turn{Say: 30, Tools: 1})
	after := support.SettleUntil(s.r, 3)
	support.CheckEdge(t, "after the drop", after, s.l)
	for id := range latestByID(before) {
		if _, ok := latestByID(after)[id]; !ok {
			t.Errorf("%s was lost over the drop", id)
		}
	}
	// In the middle of a turn: the stream drops while the agent works and
	// comes back before the turn ends.
	release := s.hold(t, 3, sessiongen.Turn{Say: 30, Tools: 1})
	s.r.SettleUntil(support.Quick, func(st enginetest.State) bool { return st.Mission.Summary.Working })
	s.api.DropStreams()
	support.Eventually(t, 10*time.Second, "the stream to come back", func() bool { return s.api.Streams() == 1 })
	release()
	st := s.r.SettleUntil(support.Quick, func(st enginetest.State) bool { return support.HasPrompt(4)(st) && !st.Mission.Summary.Working })
	support.CheckEdge(t, "after the drop mid-turn", st, s.l)
	if st.Mission.Summary.Working {
		t.Error("working after the agent went idle")
	}
	s.answerPrompts(t)
	s.sendGoesTo(t, "After the drops", s.p.Session())
}

func latestByID(st enginetest.State) map[string]harness.Item {
	out := map[string]harness.Item{}
	for _, it := range st.Items {
		if old, ok := out[it.ID]; !ok || it.Seq > old.Seq {
			out[it.ID] = it
		}
	}
	return out
}

// apiServerMoves: the agent is started again by the app while the helper
// runs (a relaunch is a new launch: a new port and password). The helper
// follows it to the new address; the old one is closed; a prompt from the
// phone reaches the new run, and its turn is shown.
func apiServerMoves(t *testing.T, agent string) {
	s := apiStart(t, agent, 67, nil, sessiongen.Turn{Say: 20})
	support.SettleUntil(s.r, 1)
	old := s.api.Port()
	s.r.Exit()
	s.r.Settle(2 * time.Second)
	s.r.Relaunch()
	s.r.SetHubStatus("idle")
	if s.api.Port() == old || s.api.Port() == 0 {
		t.Fatalf("the relaunch kept port %d", old)
	}
	support.Eventually(t, 10*time.Second, "the helper to connect to the new run", func() bool { return s.api.Streams() == 1 })
	s.add(t, sessiongen.Turn{Say: 20, Tools: 1})
	st := support.SettleUntil(s.r, 2)
	support.CheckEdge(t, "after the move", st, s.l)
	s.answerPrompts(t)
	s.r.SettleUntil(support.Quick, func(st enginetest.State) bool { return !st.Mission.Summary.Working })
	s.sendGoesTo(t, "To the new run", s.p.Session())
	st = s.r.SettleUntil(support.Quick, func(st enginetest.State) bool {
		return hasPromptText(st, "To the new run") && !st.Mission.Summary.Working
	})
	support.NothingRuns(t, st)
}

// apiRestartLosesChat runs the helper restart of a session with history,
// at a seeded point between turns and in the middle of one. It returns
// how many items the chat held before and after the first restart; the
// rest of the session is checked to work on.
func apiRestartLosesChat(t *testing.T, agent string) (before, after int) {
	s := apiStart(t, agent, 69, nil, sessiongen.Turn{Say: 30, Tools: 1}, sessiongen.Turn{Say: 20, Tools: 1})
	st := support.SettleUntil(s.r, 2)
	before = len(latestByID(st))
	s.r.Restart()
	support.Eventually(t, 10*time.Second, "the stream to come back", func() bool { return s.api.Streams() == 1 })
	after = len(latestByID(s.r.Settle(time.Second)))
	// Mid-turn: the helper restarts while the agent works, the turn ends
	// while it is down, and the next turn is followed.
	release := s.hold(t, 3, sessiongen.Turn{Say: 30, Tools: 1})
	s.r.SettleUntil(support.Quick, func(st enginetest.State) bool { return st.Mission.Summary.Working })
	s.r.Stop()
	release()
	s.r.Start()
	// What the agent says before the new helper's stream is up is not
	// sent to it: the next turn comes after.
	support.Eventually(t, 10*time.Second, "the stream to come back", func() bool { return s.api.Streams() == 1 })
	s.add(t, sessiongen.Turn{Say: 20})
	st = s.r.SettleUntil(support.Quick, func(st enginetest.State) bool { return support.HasPrompt(4)(st) && !st.Mission.Summary.Working })
	if !support.HasPrompt(4)(st) {
		t.Fatal("the turn after the restart is not shown")
	}
	support.NothingRuns(t, st)
	s.answerPrompts(t)
	s.sendGoesTo(t, "After the restart", s.p.Session())
	return before, after
}

// apiStream: a reply streamed in pieces is shown while it streams, as a
// partial text that grows, and once whole as the final text.
func apiStream(t *testing.T, agent string) {
	s := apiStart(t, agent, 71, nil, sessiongen.Turn{Say: 20})
	support.SettleUntil(s.r, 1)
	// A reply of 200 bytes: the prompt, busy, the message, its step, the
	// empty text part and two of its four pieces.
	release := s.hold(t, 8, sessiongen.Turn{Say: 200})
	partial := func(st enginetest.State) (harness.Item, bool) {
		for _, it := range support.Items(st, support.OfKind(harness.KindAssistant)) {
			// Streamed updates are spaced (opencode.go partialEvery): the
			// first piece is shown at once, later ones with the next update.
			if it.TurnID != "" && support.HasPrompt(2)(st) && it.Assistant.Partial && it.Assistant.Text != "" {
				return it, true
			}
		}
		return harness.Item{}, false
	}
	// Not found in time: say what the chat held, so a failure on a slow
	// machine names the item that was missing or different.
	defer func() {
		if !t.Failed() {
			return
		}
		for _, it := range s.r.Read().Items {
			var text string
			switch {
			case it.Assistant != nil:
				text = fmt.Sprintf("assistant partial=%v %d bytes", it.Assistant.Partial, len(it.Assistant.Text))
			case it.User != nil:
				text = fmt.Sprintf("user %q", it.User.Text[:min(len(it.User.Text), 12)])
			}
			t.Logf("chat held: %s %s turn=%q parent=%q %s", it.Kind, it.ID, it.TurnID, it.Parent, text)
		}
		t.Logf("API calls: %v", s.api.Calls())
	}()
	st := s.r.SettleUntil(support.Quick, func(st enginetest.State) bool { _, ok := partial(st); return ok })
	it, ok := partial(st)
	if !ok || len(it.Assistant.Text) > 128 {
		t.Fatalf("no partial reply while it streams: %+v", it.Assistant)
	}
	release()
	st = s.r.SettleUntil(support.Quick, func(st enginetest.State) bool { return !st.Mission.Summary.Working })
	final := latestByID(st)[it.ID]
	if final.Assistant == nil || final.Assistant.Partial || len(final.Assistant.Text) != 200 || !strings.HasPrefix(final.Assistant.Text, it.Assistant.Text) {
		t.Errorf("the finished reply is %+v", final.Assistant)
	}
	support.CheckEdge(t, "streamed", st, s.l)
}
