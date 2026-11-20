package hookstyle

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/local"
)

// A background subagent runs on after the main agent's stop: its tool
// hooks reach the summary as forward-only events, so the session does not
// go back to working for good; its permission requests still count.
func TestSubagentToolsDoNotMoveTheSummary(t *testing.T) {
	d, _ := For("copilot")
	env := map[string]string{"TOGO_SESSION": "s_aaaaaaaaaa"}
	for event, want := range map[string]string{
		"preToolUse":        local.EventHarness,
		"postToolUse":       local.EventHarness,
		"permissionRequest": local.EventPermission,
	} {
		fw, err := Reduce(d, event, []byte(`{"sessionId":"a1","parentAgentId":"main-1","toolCallId":"s1","toolName":"bash","toolArgs":{"command":"ls"}}`), env)
		if err != nil {
			t.Fatal(err)
		}
		if fw.Event != want {
			t.Errorf("%s reduced to %q, want %q", event, fw.Event, want)
		}
	}
	fw, _ := Reduce(d, "preToolUse", []byte(`{"sessionId":"main-1","toolCallId":"c1","toolName":"bash","toolArgs":{"command":"ls"}}`), env)
	if fw.Event != local.EventTool {
		t.Errorf("the main agent's tool reduced to %q", fw.Event)
	}
}

// A subagent's own subagent: its start and stop carry the first
// subagent's id as sessionId. They draw the nested agent in the first
// one's conversation and leave the session's id alone.
func TestNestedSubagentKeepsTheSessionID(t *testing.T) {
	items, m := feed(t, "copilot",
		[2]string{"userPromptSubmitted", `{` + subParent + `,"prompt":"go"}`},
		[2]string{"subagentStart", `{` + subParent + `,"agentId":"a1","agentName":"explore"` + subOf + `}`},
		subCall("subagentStart", "a1", subOf+`,"agentId":"a2","agentName":"explore"`),
		subCall("subagentStop", "a1", subOf+`,"agentId":"a2"`))
	if got := m.State()["harness_session"]; got != "main-1" {
		t.Fatalf("harness_session %q", got)
	}
	var nested *harness.Item
	for i := range items {
		if items[i].ID == "agent-a2" {
			nested = &items[i]
		}
	}
	if nested == nil || nested.Parent != "agent-a1" || nested.Agent.State != harness.StateOK {
		t.Fatalf("nested agent %+v", nested)
	}
}

// The hook store keeps each record with the mapper's marks and cuts its
// oldest quarter when full; a replay of what is left after a restart
// names the notices, agents and subagent prompts as the live run did. (A
// cut inside a turn opens the kept part as a turn of its own, as the hook
// store says; its tools are named after that turn.)
func TestTrimmedReplayKeepsItemIDs(t *testing.T) {
	d, _ := For("copilot")
	calls := [][2]string{
		{"userPromptSubmitted", `{` + subParent + `,"prompt":"go"}`},
		{"errorOccurred", `{` + subParent + `,"error":{"message":"Request failed with status 500"}}`},
		{"preToolUse", `{` + subParent + `,"toolCallId":"c1","toolName":"task","toolArgs":{"description":"One"}}`},
		{"subagentStart", `{` + subParent + `,"agentId":"a1","agentName":"general-purpose"}`},
		subCall("userPromptSubmitted", "a1", subOf+`,"prompt":"one"`),
		{"errorOccurred", `{` + subParent + `,"error":{"message":"Request failed with status 502"}}`},
		subCall("preToolUse", "a1", subOf+`,"toolName":"bash","toolArgs":{"command":"ls"}`),
		{"subagentStop", `{` + subParent + `,"agentId":"a1","response":"1"}`},
	}
	type rec struct {
		data      json.RawMessage
		at        time.Time
		pre, post json.RawMessage
		ids       []string
	}
	live := New(harness.Env{SessionID: "s-trim-live"}, Options{Name: "copilot.hooks"})
	var recs []rec
	for i, c := range calls {
		_, norm, err := Normalize(d, c[0], []byte(c[1]))
		if err != nil {
			t.Fatal(err)
		}
		r := rec{data: norm, at: t0.Add(time.Duration(i) * time.Second), pre: live.ReplayMark()}
		items, _ := live.Map(harness.Record{Source: "hooks", At: r.at, Data: norm})
		r.post = live.ReplayMark()
		for _, x := range items {
			r.ids = append(r.ids, x.ID)
		}
		recs = append(recs, r)
	}
	again := New(harness.Env{SessionID: "s-trim-again"}, Options{Name: "copilot.hooks"})
	for i, r := range recs[2:] {
		again.Replaying(r.pre, r.post)
		items, _ := again.Map(harness.Record{Source: "hooks", At: r.at, Data: r.data})
		var ids []string
		for _, x := range items {
			ids = append(ids, x.ID)
		}
		for _, id := range ids {
			if !strings.HasPrefix(id, "hook-notice-") && !strings.HasPrefix(id, "agent-") {
				continue
			}
			found := false
			for _, want := range r.ids {
				found = found || id == want
			}
			if !found {
				t.Errorf("record %d: replay named %q, live named %v", i+2, id, r.ids)
			}
		}
	}
}
