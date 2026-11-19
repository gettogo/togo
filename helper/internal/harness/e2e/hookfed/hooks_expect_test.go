package hookfed

import (
	"fmt"
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/e2e/support"
	"github.com/gettogo/togo/helper/internal/harness/enginetest"
	"github.com/gettogo/togo/helper/internal/harness/sessiongen"
)

// hookAgent describes how one hook-driven agent's calls map onto what the
// chat must show, so the scenarios can say what a session should look
// like without a second copy of the mapper.
type hookAgent struct {
	name    string
	profile func() sessiongen.Profile
	// classify says what a call is: "prompt" (text: the prompt), "tool"
	// (one tool step), "ask" (the agent's own question), "reply" (text: a
	// message the chat must show), "agent" (a subagent starts), or "" for
	// the rest. A call may be a reply as well; classify lists every kind.
	classify func(c sessiongen.HookCall) []kind
	// replies is false when the agent's hooks and logs carry no reply
	// text, so its chat shows none (see TestKnownFault_Droid_RepliesNotShown).
	replies bool
	// canAsk, canApprove, canAgents say which scenario features the agent
	// has hooks for.
	canAsk, canApprove, canAgents bool
	// waitEvent is the call after which the agent waits for the user (a
	// permission); "" when it has none. startEvent starts a conversation.
	waitEvent, startEvent string
	// slowStop is true when the mapper waits at the end of a turn that
	// has no final message.
	slowStop bool
	// long is the number of turns of the long session, enough to pass the
	// log's 2000 items.
	long int
}

type kind struct {
	what string
	text string
}

// turnExp is what one turn of the script must look like in the chat.
type turnExp struct {
	id      string
	prompt  string
	tools   int
	asks    int
	replies int
	end     string // ok, interrupted, failed; "" when no call ended it
}

// expected reads the script's calls into the turns the chat must show.
// A prompt that arrives while a turn runs (a steer, or the turn's end hook
// was never delivered) ends the turn interrupted.
func expected(a hookAgent, calls []sessiongen.HookCall) []turnExp {
	var out []turnExp
	cur := -1
	for _, c := range calls {
		for _, k := range a.classify(c) {
			switch k.what {
			case "prompt":
				if cur >= 0 && out[cur].end == "" {
					out[cur].end = "interrupted"
				}
				out = append(out, turnExp{id: support.TurnID(c), prompt: k.text})
				cur = len(out) - 1
			case "tool":
				if cur >= 0 {
					out[cur].tools++
				}
			case "ask":
				if cur >= 0 {
					out[cur].asks++
				}
			case "reply":
				if cur >= 0 {
					out[cur].replies++
				}
			}
		}
		if c.End != "" && cur >= 0 {
			out[cur].end = c.End
			cur = -1
		}
	}
	return out
}

// verify compares the chat with the turns the script says it holds. Only
// turns at or after index from are looked at (earlier ones may have left
// the log's ring or the engine's memory).
func verify(t *testing.T, where string, a hookAgent, st enginetest.State, exp []turnExp, from int) {
	t.Helper()
	by := map[string][]harness.Item{}
	for _, it := range st.Items {
		by[it.TurnID] = append(by[it.TurnID], it)
	}
	for i := from; i < len(exp); i++ {
		e := exp[i]
		var users, tools, asks, replies int
		var text, state string
		for _, it := range by[e.id] {
			switch {
			case it.User != nil:
				users++
				text = it.User.Text
			case it.Tool != nil:
				tools++
				if support.Running(it.Tool.State) && e.end != "" {
					t.Errorf("%s: turn %d (%s): tool %s is %s after the turn ended", where, i, e.id, it.ID, it.Tool.State)
				}
			case it.Question != nil:
				asks++
			case it.Assistant != nil:
				replies++
			case it.Turn != nil:
				state = it.Turn.State
			}
		}
		if users != 1 || !strings.HasPrefix(text, e.prompt) {
			t.Errorf("%s: turn %d (%s): %d prompts, text %.30q, want %.30q", where, i, e.id, users, text, e.prompt)
		}
		if tools != e.tools || asks != e.asks {
			t.Errorf("%s: turn %d: %d tools and %d questions, want %d and %d", where, i, tools, asks, e.tools, e.asks)
		}
		if a.replies && replies != e.replies {
			t.Errorf("%s: turn %d: %d replies, want %d", where, i, replies, e.replies)
		}
		want := map[string]string{"ok": harness.StateOK, "interrupted": harness.StateInterrupted, "failed": harness.StateFailed}[e.end]
		if e.end == "" {
			want = harness.StateRunning
		}
		if state != want {
			t.Errorf("%s: turn %d (%s): state %q, want %q", where, i, e.id, state, want)
		}
	}
}

// checkHook runs the engine-level rules on a hook-driven session. A prompt
// the user interrupted has no reply to show, and Droid's chat has none for
// any prompt (a known fault); those are the only rules left out.
func checkHook(t *testing.T, where string, a hookAgent, st enginetest.State, l *sessiongen.Layout, over bool) {
	t.Helper()
	vs := enginetest.Check(st, enginetest.Facts{Truth: l.Truth, Over: over})
	var keep []enginetest.Violation
	hasUser := map[string]bool{}
	for _, it := range st.Items {
		if it.User != nil {
			hasUser[it.TurnID] = true
		}
	}
	for _, v := range vs {
		// The log's ring may have dropped a turn's prompt and kept the
		// rest: that turn's records no longer reach back to its start.
		if v.Rule == "turn-start-outside" && st.Discarded > 0 && !hasUser[strings.Fields(v.Detail)[0]] {
			continue
		}
		if v.Rule == "prompt-unanswered" && (!a.replies || strings.Contains(v.Detail, ", interrupted)")) {
			continue
		}
		keep = append(keep, v)
	}
	enginetest.Fail(t, where, keep)
}

// settled waits for the engine to read everything delivered: the last
// prompt's turn has left running (or, with open true, the state is quiet).
func settled(r *enginetest.Rig, last sessiongen.HookCall) enginetest.State {
	return r.SettleUntil(20*sec, support.TurnDone(last))
}

func lastPrompt(a hookAgent, calls []sessiongen.HookCall) sessiongen.HookCall {
	var p sessiongen.HookCall
	for _, c := range calls {
		for _, k := range a.classify(c) {
			if k.what == "prompt" {
				p = c
			}
		}
	}
	return p
}

const sec = 1000 * 1000 * 1000

func (e turnExp) String() string { return fmt.Sprintf("%s tools=%d end=%s", e.id, e.tools, e.end) }
