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
// mapper. The main agent's closing message is written to its log just
// before its own agentStop, as Copilot does. It returns every item in
// order, the last version of each by id, and the mapper.
func subagentRun(t *testing.T) ([]harness.Item, map[string]harness.Item, harness.Mapper) {
	t.Helper()
	home := t.TempDir()
	dir := filepath.Join(home, ".copilot", "session-state", subMain)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(dir, "events.jsonl")
	appendLines(t, log, `{"type":"session.start","data":{}}`)
	newMapper, _ := harness.LookupMapper(MapperHooks)
	m := newMapper(harness.Env{SessionID: "s_sub", Vars: map[string]string{source.VarHome: home}})
	d, _ := hookstyle.For("copilot")
	var all []harness.Item
	last := map[string]harness.Item{}
	for i, l := range hookLines(t, "subagent-hooks.jsonl") {
		var c struct {
			Event   string          `json:"event"`
			Payload json.RawMessage `json:"payload"`
		}
		if err := json.Unmarshal(l, &c); err != nil {
			t.Fatal(err)
		}
		if c.Event == "sessionEnd" {
			continue // the user's exit, not part of the turn
		}
		var p struct {
			SessionID string `json:"sessionId"`
			Timestamp int64  `json:"timestamp"`
		}
		_ = json.Unmarshal(c.Payload, &p)
		if c.Event == "agentStop" && p.SessionID == subMain {
			appendLines(t, log, `{"type":"assistant.message","data":{"messageId":"m9","content":"done","toolRequests":[],"turnId":"3"},"id":"e9"}`)
		}
		_, norm, err := hookstyle.Normalize(d, c.Event, c.Payload)
		if err != nil {
			t.Fatalf("line %d: %v", i+1, err)
		}
		items, _ := m.Map(harness.Record{Source: "hooks", At: time.UnixMilli(p.Timestamp), Data: norm})
		all = append(all, items...)
		for _, x := range items {
			last[x.ID] = x
		}
	}
	return all, last, m
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
	done := false
	for _, x := range all {
		if x.Kind == harness.KindAssistant && x.Parent == "" && x.Assistant.Text == "done" {
			done = true
		}
	}
	if !done {
		t.Error("the main agent's reply did not show")
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
