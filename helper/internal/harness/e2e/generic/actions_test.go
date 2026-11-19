package generic

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/enginetest"
)

// Every action an adapter declares, called from the phone through
// harness.action on an idle session, reaches the agent the way its route
// says: a typed command given to the agent from its input line, keys in
// the pane, the agent restarted with the new argv, or the agent's own
// program run with the value. Answer, stop and interrupt have scenarios
// of their own (questions, take back, interrupt).

// elsewhere are the actions other scenarios prove.
var elsewhere = map[string]string{
	"answer":    "TestQuestions answers questions from the phone",
	"stop":      "TestSend's queue_and_take_back stops a queued prompt",
	"interrupt": "TestSend's interrupt and TestLifecycle's interrupt",
}

// choice is the value an action is called with: a mode the adapter lists
// for a mode action, else a plain id no flag parser takes for an option.
func choice(r *enginetest.Rig, action string) string {
	if action == "set_mode" {
		if m := r.Describe().Modes; len(m) > 0 {
			return m[len(m)-1].ID
		}
	}
	return "e2echoice"
}

// effect is what an action did, read from the rig.
type effect struct {
	typed     []string
	keys      []string
	relaunch  [][]string
	cli       []string
	result    harness.ActionResult
	errorText string
}

func act(t *testing.T, r *enginetest.Rig, name, value string) effect {
	t.Helper()
	typed, keys, rel, cli := len(r.Term.Submitted()), len(r.Term.KeysSent()), len(r.Relaunches()), len(r.CLICalls())
	res, err := r.Act(name, "", map[string]any{"id": value, "value": value, "focus": "the parser"})
	e := effect{result: res, typed: r.Term.Submitted()[typed:], keys: r.Term.KeysSent()[keys:],
		relaunch: r.Relaunches()[rel:], cli: r.CLICalls()[min(cli, len(r.CLICalls())):]}
	if err != nil {
		e.errorText = err.Error()
	}
	return e
}

func TestActions(t *testing.T) {
	for _, agent := range profiles(t) {
		t.Run(agent, func(t *testing.T) {
			t.Parallel()
			names := enginetest.ActionNames(agent)
			for _, name := range names {
				t.Run(name, func(t *testing.T) {
					t.Parallel()
					if why, ok := elsewhere[name]; ok {
						t.Skip(why)
					}
					lv := start(t, agent)
					lv.idle()
					value := choice(lv.r, name)
					e := act(t, lv.r, name, value)
					t.Logf("%s %s(%s): result %+v err %q typed %q keys %v relaunch %v cli %q", agent, name, value, e.result, e.errorText, e.typed, e.keys, e.relaunch, e.cli)
					checkEffect(t, lv, name, value, e)
					if cell, ok := actionCells[name]; ok && confirmed(e.result) {
						if _, ok := declared[agent+"/"+cell]; ok {
							declare(t, agent, cell)
						}
					}
				})
			}
		})
	}
}

// checkEffect: the action did one thing the agent can see, carrying the
// value where the action takes one, and the session is left usable.
func checkEffect(t *testing.T, lv *live, name, value string, e effect) {
	t.Helper()
	if e.errorText != "" {
		t.Fatalf("%s: %s", name, e.errorText)
	}
	var seen []string
	for _, s := range e.typed {
		seen = append(seen, "typed "+s)
	}
	for _, a := range e.relaunch {
		seen = append(seen, "relaunch "+strings.Join(a, " "))
	}
	for _, c := range e.cli {
		seen = append(seen, "ran "+c)
	}
	if len(e.keys) > 0 {
		seen = append(seen, "keys "+strings.Join(e.keys, " "))
	}
	if len(seen) == 0 && e.result.Result != harness.ResultFallback {
		t.Fatalf("%s reached nothing the agent sees (result %+v)", name, e.result)
	}
	for _, s := range e.typed {
		if !strings.HasPrefix(s, "/") {
			t.Errorf("%s typed %q, not a command", name, s)
		}
	}
	if len(e.relaunch) > 0 && !slices.Contains(e.relaunch[0], value) && !strings.Contains(strings.Join(e.relaunch[0], " "), value) {
		t.Errorf("%s restarted the agent with %q, without the chosen %q", name, e.relaunch[0], value)
	}
	if len(e.cli) > 0 && !strings.Contains(strings.Join(e.cli, "\n"), value) {
		t.Errorf("%s ran %q, without the chosen %q", name, e.cli, value)
	}
	switch e.result.Result {
	case harness.ResultFailed:
		// The scripted agent does not take a model or mode change it is
		// asked for, so an action that checks the change afterwards
		// (spec verify: the agent's records or its screen) must say it
		// did not happen, in words, and not report success (rule 3).
		if e.result.Message == "" {
			t.Errorf("%s failed with no reason for the phone", name)
		}
	case harness.ResultFallback:
		t.Errorf("%s fell back to the terminal (no route applies)", name)
	}
	// The session takes a prompt afterwards: nothing was left in the
	// input line or holding the typing lock.
	lv.r.SetHubStatus("idle")
	time.Sleep(50 * time.Millisecond)
	if _, err := lv.r.Send("After "+name, harness.SendQueue); err != nil {
		t.Errorf("send after %s: %v", name, err)
	}
}

// actionCells maps an action to the capability cell it proves when the
// agent was given the change (typed, restarted with it, or applied).
var actionCells = map[string]string{
	"set_model": "capability/model", "switch_model": "capability/model",
	"set_mode": "capability/mode", "set_effort": "capability/effort",
	"compact": "capability/compact", "snapcompact": "capability/compact",
	"resume": "capability/resume",
}

func confirmed(r harness.ActionResult) bool {
	switch r.Result {
	case harness.ResultTyped, harness.ResultRelaunched, harness.ResultApplied:
		return true
	}
	return false
}
