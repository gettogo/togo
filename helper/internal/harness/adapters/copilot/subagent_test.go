package copilot

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/adapters/hookstyle"
	"github.com/gettogo/togo/helper/internal/harness/source"
	"github.com/gettogo/togo/helper/internal/local"
)

const subMain = "00575c31-adb0-4c0b-be0a-c3500095f354"

// subagentRun feeds testdata/subagent-hooks.jsonl (Copilot CLI 1.0.114, a
// real turn that starts one subagent with the task tool) through the
// mapper, with the same run's own log (testdata/subagent.events.jsonl,
// trimmed to the records the reader looks at) written as it was: each
// record before the first hook stamped after it. It returns every item in
// order, the last version of each by id, and the mapper.
func subagentRun(t *testing.T) ([]harness.Item, map[string]harness.Item, harness.Mapper) {
	t.Helper()
	all, last, m, _ := capturedRun(t, capture{main: subMain, hooks: "subagent-hooks.jsonl", events: "subagent.events.jsonl"})
	return all, last, m
}

// capture is a recorded Copilot run: its hooks and its own log.
type capture struct {
	main, hooks, events string
	// readAt gives the time (ms) the log had been written up to when the
	// hook for event, stamped ms, was read; nil, or a zero return, is ms.
	readAt func(event string, ms int64) int64
}

// capturedRun feeds a capture through the mapper as subagentRun does. It
// also returns a replay: the same hook records, with the marks kept when
// they were first mapped, through a new mapper, as after a helper restart.
func capturedRun(t *testing.T, c capture) ([]harness.Item, map[string]harness.Item, harness.Mapper, func() []harness.Item) {
	t.Helper()
	home := t.TempDir()
	dir := filepath.Join(home, ".copilot", "session-state", c.main)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(dir, "events.jsonl")
	events := hookLines(t, c.events)
	written := 0
	writeUpTo := func(ms int64) {
		for ; written < len(events); written++ {
			var ev struct {
				Timestamp time.Time `json:"timestamp"`
			}
			if err := json.Unmarshal(events[written], &ev); err != nil {
				t.Fatal(err)
			}
			if ev.Timestamp.UnixMilli() > ms {
				return
			}
			appendLines(t, log, string(events[written]))
		}
	}
	newMapper, _ := harness.LookupMapper(MapperHooks)
	env := harness.Env{SessionID: "s_sub", Vars: map[string]string{source.VarHome: home}}
	m := newMapper(env)
	d, _ := hookstyle.For("copilot")
	mk := m.(harness.ReplayMarker)
	var all []harness.Item
	var recs []marked
	last := map[string]harness.Item{}
	for i, l := range hookLines(t, c.hooks) {
		var h struct {
			Event   string          `json:"event"`
			Payload json.RawMessage `json:"payload"`
		}
		if err := json.Unmarshal(l, &h); err != nil {
			t.Fatal(err)
		}
		if h.Event == "sessionEnd" {
			continue // the user's exit, not part of the turn
		}
		var p struct {
			SessionID string `json:"sessionId"`
			Timestamp int64  `json:"timestamp"`
		}
		_ = json.Unmarshal(h.Payload, &p)
		upTo := p.Timestamp
		if c.readAt != nil {
			if ms := c.readAt(h.Event, p.Timestamp); ms != 0 {
				upTo = ms
			}
		}
		writeUpTo(upTo)
		_, norm, err := hookstyle.Normalize(d, h.Event, h.Payload)
		if err != nil {
			t.Fatalf("line %d: %v", i+1, err)
		}
		rec := harness.Record{Source: "hooks", At: time.UnixMilli(p.Timestamp), Data: norm}
		pre := mk.ReplayMark()
		items, _ := m.Map(rec)
		recs = append(recs, marked{rec, pre, mk.ReplayMark()})
		all = append(all, items...)
		for _, x := range items {
			last[x.ID] = x
		}
	}
	replay := func() []harness.Item {
		r := newMapper(env)
		rk := r.(harness.ReplayMarker)
		var again []harness.Item
		for _, x := range recs {
			rk.Replaying(x.pre, x.post)
			items, _ := r.Map(x.rec)
			again = append(again, items...)
		}
		return again
	}
	return all, last, m, replay
}

// A Copilot subagent fires its own userPromptSubmitted and agentStop under
// its own id. They belong to the subagent: its prompt is not a prompt of
// the session's chat, its stop does not end the main turn, and the main
// agent's reply after it shows.
func TestSubagentHooksStayWithTheSubagent(t *testing.T) {
	all, last, m := subagentRun(t)
	var top []string
	var agent *harness.Item
	sawRunning := false
	for _, x := range all {
		if x.Kind == harness.KindUser && x.Parent == "" {
			top = append(top, x.User.Text)
		}
		if x.Kind == harness.KindAgent {
			sawRunning = sawRunning || x.Agent.State == harness.StateRunning
		}
	}
	for _, x := range last {
		switch x.Kind {
		case harness.KindTurn:
			if x.Turn.State != harness.StateOK {
				t.Errorf("turn %s ended %s", x.ID, x.Turn.State)
			}
		case harness.KindTool:
			if x.Tool.State != harness.StateOK {
				t.Errorf("tool %s ended %s", x.Tool.Name, x.Tool.State)
			}
		case harness.KindAgent:
			a := x
			agent = &a
		}
	}
	if len(top) != 1 {
		t.Fatalf("top-level prompts %q, want the user's one", top)
	}
	turns := 0
	for _, x := range last {
		if x.Kind == harness.KindTurn {
			turns++
		}
	}
	if turns != 1 {
		t.Errorf("%d turns, want 1", turns)
	}
	if agent == nil || !sawRunning || agent.Agent.State != harness.StateOK || agent.TurnID == "" {
		t.Fatalf("agent item %+v (ran: %v)", agent, sawRunning)
	}
	if agent.Agent.Summary != "ok" {
		t.Errorf("agent summary %q", agent.Agent.Summary)
	}
	for _, x := range all {
		if x.Kind == harness.KindUser && x.Parent != "" && x.Parent != agent.ID {
			t.Errorf("subagent prompt under %q, agent is %q", x.Parent, agent.ID)
		}
	}
	var said []string
	for _, x := range all {
		if x.Kind == harness.KindAssistant && x.Parent == "" {
			said = append(said, x.Assistant.Text)
		}
	}
	// The subagent's "ok" is in the session's log too, as its own
	// interaction: it is the subagent's answer, not the main agent's.
	if len(said) != 1 || said[0] != "done" {
		t.Errorf("main agent said %q, want just done", said)
	}
	if got := m.State()["harness_session"]; got != subMain {
		t.Errorf("harness_session %q, want the main session", got)
	}
}

// The summary the hook process reduces (status, done push) takes the
// subagent's prompt and stop as forward-only: the session is not idle, and
// no "finished" push fires, while the main turn runs on.
func TestSubagentHooksDoNotEndTheSessionTurn(t *testing.T) {
	d, _ := hookstyle.For("copilot")
	var stops []string
	for _, l := range hookLines(t, "subagent-hooks.jsonl") {
		var c struct {
			Event   string          `json:"event"`
			Payload json.RawMessage `json:"payload"`
		}
		_ = json.Unmarshal(l, &c)
		fw, err := hookstyle.Reduce(d, c.Event, c.Payload, map[string]string{"TOGO_SESSION": "s_sub"})
		if err != nil {
			t.Fatal(err)
		}
		var p struct {
			SessionID string `json:"sessionId"`
		}
		_ = json.Unmarshal(c.Payload, &p)
		if p.SessionID != subMain && fw.Event != local.EventHarness {
			t.Errorf("subagent %s reduced to %q", c.Event, fw.Event)
		}
		if fw.Event == local.EventStop || fw.Event == local.EventPrompt {
			stops = append(stops, fw.Event)
		}
	}
	if len(stops) != 2 || stops[0] != local.EventPrompt || stops[1] != local.EventStop {
		t.Fatalf("prompt and stop events %v, want the main turn's one each", stops)
	}
}
