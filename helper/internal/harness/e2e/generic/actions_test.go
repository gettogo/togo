package generic

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/enginetest"
	"github.com/gettogo/togo/helper/internal/harness/sessiongen"
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
	// An API agent's prompt and / line travel through these; the phone
	// reaches them by sending, not as actions of their own.
	"send":    "TestSend sends prompts; the OpenCode and Kilo scenarios check the calls",
	"command": "a / line sent from the phone; the OpenCode and Kilo scenarios check the call",
}

// choice is the value an action is called with: a mode the adapter lists
// for a mode action, else a plain id no flag parser takes for an option.
func choice(r *enginetest.Rig, action string) string {
	if action == "set_mode" {
		if m := r.Describe().Modes; len(m) > 0 {
			return m[len(m)-1].ID
		}
	}
	if action == "set_model" && r.API() != nil {
		// The model switch takes a model reference, an object: the
		// recordings' model.
		return `{"id":"` + sessiongen.OpenCodeModel + `","providerID":"` + sessiongen.OpenCodeProvider + `"}`
	}
	return "e2echoice"
}

// effect is what an action did, read from the rig.
type effect struct {
	typed     []string
	keys      []string
	relaunch  [][]string
	cli       []string
	api       []enginetest.APICall // calls to the agent's own API
	result    harness.ActionResult
	errorText string
}

func act(t *testing.T, r *enginetest.Rig, name, value string) effect {
	t.Helper()
	typed, keys, rel, cli := len(r.Term.Submitted()), len(r.Term.KeysSent()), len(r.Relaunches()), len(r.CLICalls())
	calls := 0
	if api := r.API(); api != nil {
		calls = len(api.Posts())
	}
	if c := r.Codex(); c != nil {
		calls = len(c.Calls())
	}
	res, err := r.Act(name, "", map[string]any{"id": value, "value": value, "focus": "the parser"})
	e := effect{result: res, typed: r.Term.Submitted()[typed:], keys: r.Term.KeysSent()[keys:],
		relaunch: r.Relaunches()[rel:], cli: r.CLICalls()[min(cli, len(r.CLICalls())):]}
	if api := r.API(); api != nil {
		e.api = api.Posts()[calls:]
	}
	if c := r.Codex(); c != nil {
		e.api = codexActions(c.Calls()[calls:])
	}
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
	for _, c := range e.api {
		seen = append(seen, "called "+c.Method+" "+c.Path)
	}
	if len(seen) == 0 && e.result.Result != harness.ResultFallback {
		t.Fatalf("%s reached nothing the agent sees (result %+v)", name, e.result)
	}
	for _, s := range e.typed {
		if !strings.HasPrefix(s, "/") {
			t.Errorf("%s typed %q, not a command", name, s)
		}
	}
	// show_session takes no value: it restarts the agent on the session
	// the helper started (opencode2.yaml show_session).
	if len(e.relaunch) > 0 && name != "show_session" && !slices.Contains(e.relaunch[0], value) && !strings.Contains(strings.Join(e.relaunch[0], " "), value) {
		t.Errorf("%s restarted the agent with %q, without the chosen %q", name, e.relaunch[0], value)
	}
	if len(e.cli) > 0 && !strings.Contains(strings.Join(e.cli, "\n"), value) {
		t.Errorf("%s ran %q, without the chosen %q", name, e.cli, value)
	}
	if takesValue[name] && len(e.api) > 0 && !apiCarries(e.api, value) {
		t.Errorf("%s called %+v, without the chosen %q", name, e.api, value)
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

// takesValue are the actions whose call carries the chosen value.
var takesValue = map[string]bool{"set_model": true, "set_mode": true, "resume": true, "add_provider_key": true,
	"set_provider_key": true, "command": true}

// apiCarries reports whether a call's path or body holds the value: the
// model reference may come back with its keys in another order.
func apiCarries(calls []enginetest.APICall, value string) bool {
	var want any
	isJSON := json.Unmarshal([]byte(value), &want) == nil && strings.HasPrefix(value, "{")
	for _, c := range calls {
		if strings.Contains(c.Path, value) || strings.Contains(c.Body, value) {
			return true
		}
		var body map[string]any
		if isJSON && json.Unmarshal([]byte(c.Body), &body) == nil {
			for _, v := range body {
				if reflect.DeepEqual(v, want) {
					return true
				}
			}
		}
	}
	return false
}

func confirmed(r harness.ActionResult) bool {
	switch r.Result {
	case harness.ResultTyped, harness.ResultRelaunched, harness.ResultApplied:
		return true
	}
	return false
}
