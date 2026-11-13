package codex

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

// These activity packets came from a live Codex 0.167.2 parent/worker
// turn. Its wait call has no receiver IDs or agent-state dictionary.
func TestRecordedSubAgentActivityBuildsAndCompletesTheAgent(t *testing.T) {
	m := NewAppServer(harness.Env{})
	recs := records(t, "appserver-agent-0167-2.jsonl")
	out := run(m, recs)
	var agents []harness.Item
	for _, x := range out.items {
		if x.Agent != nil {
			agents = append(agents, x)
		}
	}
	if len(agents) != 2 {
		t.Fatalf("got %d agent updates, want one start and one completion", len(agents))
	}
	start, done := agents[0], agents[1]
	var first, last struct {
		Params struct {
			Item struct {
				Started int64 `json:"startedAtMs"`
			} `json:"item"`
		} `json:"params"`
	}
	json.Unmarshal(recs[0].Data, &first)
	json.Unmarshal(recs[3].Data, &last)
	if start.Agent.Name != "reply_ok" || start.Agent.State != harness.StateRunning || !start.Agent.Started.Equal(ms(first.Params.Item.Started)) {
		t.Fatal("activity did not name and start the worker")
	}
	if done.ID != start.ID || done.Agent.Name != start.Agent.Name || done.Agent.State != harness.StateOK || done.Agent.Ended == nil || !done.Agent.Ended.Equal(ms(last.Params.Item.Started)) || !done.Agent.Started.Equal(start.Agent.Started) {
		t.Fatal("activity completion lost the worker identity or clock")
	}
	for _, op := range out.ops {
		if op.Section == harness.SectionAgents && (op.Row.Title == "" || op.Row.Open.Ref != start.ID) {
			t.Fatal("activity emitted an incomplete agent row")
		}
	}
}

func TestActivityResumeHistoryUsesRecordedTimes(t *testing.T) {
	recs := records(t, "appserver-agent-0167-2.jsonl")
	var history []any
	for _, rec := range recs {
		var message map[string]any
		json.Unmarshal(rec.Data, &message)
		if message["method"] == "item/completed" {
			history = append(history, message["params"].(map[string]any)["item"])
		}
	}
	m := NewAppServer(harness.Env{})
	items, ops := agentEvent(t, m, "thread/resume", map[string]any{"result": map[string]any{"thread": map[string]any{"id": "parent-thread", "status": map[string]string{"type": "idle"}, "turns": []any{map[string]any{"id": "parent-turn", "status": "completed", "startedAt": t0.Unix(), "completedAt": t0.Add(time.Minute).Unix(), "items": history}}}}}, t0.Add(24*time.Hour))
	agent := requireAgent(t, items, "worker-thread")
	if agent.Agent.State != harness.StateRunning {
		t.Fatal("history did not restore the first worker activity")
	}
	var last harness.Item
	for _, x := range items {
		if x.Agent != nil {
			last = x
		}
	}
	var firstMessage struct {
		Params struct {
			Item struct {
				Started int64 `json:"startedAtMs"`
			} `json:"item"`
		} `json:"params"`
	}
	json.Unmarshal(recs[0].Data, &firstMessage)
	if last.Agent == nil || last.Agent.State != harness.StateOK || !last.Agent.Started.Equal(ms(firstMessage.Params.Item.Started)) {
		t.Fatal("resume substituted its own time or lost activity completion")
	}
	if len(ops) == 0 {
		t.Fatal("restored activity did not restore mission rows")
	}
}

func TestActivityDuplicatesDoNotResurrectOrRecompleteAWorker(t *testing.T) {
	m := NewAppServer(harness.Env{})
	recs := records(t, "appserver-agent-0167-2.jsonl")
	run(m, recs)
	if items, _ := m.Map(recs[0]); len(items) != 0 {
		t.Fatal("duplicate start resurrected a completed worker")
	}
	change := func(kind, id string, offset int64) harness.Record {
		var message map[string]any
		json.Unmarshal(recs[0].Data, &message)
		params := message["params"].(map[string]any)
		item := params["item"].(map[string]any)
		item["kind"], item["id"] = kind, id
		item["startedAtMs"] = item["startedAtMs"].(float64) + float64(offset)
		params["startedAtMs"] = item["startedAtMs"]
		raw, _ := json.Marshal(message)
		return harness.Record{Source: "appserver", At: t0, Data: raw}
	}
	items, _ := m.Map(change("interacted", "new-instruction", 10000))
	active := requireAgent(t, items, "worker-thread")
	if active.Agent.State != harness.StateRunning || active.Agent.Ended != nil {
		t.Fatal("new interaction did not reactivate the worker")
	}
	if items, _ := m.Map(recs[3]); len(items) != 0 {
		t.Fatal("duplicate old completion ended the new interaction")
	}
	items, _ = m.Map(change("interrupted", "interrupt-instruction", 11000))
	stopped := requireAgent(t, items, "worker-thread")
	if stopped.Agent.State != harness.StateInterrupted || stopped.Agent.Ended == nil || !stopped.Agent.Started.Equal(active.Agent.Started) {
		t.Fatal("interruption lost lifecycle or original clock")
	}
	if len(m.threads["worker-thread"].activities) > 4 {
		t.Fatal("duplicate tracking accumulated beyond the four activity kinds")
	}
}

func TestActivityKeepsTheResumedAgentNameRoleAndClock(t *testing.T) {
	m, before := recordedAgent(t)
	recs := records(t, "appserver-agent-0167-2.jsonl")
	for _, rec := range recs {
		rec.Data = bytes.ReplaceAll(rec.Data, []byte("worker-thread"), []byte(recordedChild))
		items, _ := m.Map(rec)
		for _, item := range items {
			if item.Agent == nil {
				continue
			}
			if item.Agent.Name != before.Agent.Name || item.Agent.Role != before.Agent.Role || !item.Agent.Started.Equal(before.Agent.Started) || item.ID != before.ID {
				t.Fatal("a partial activity replaced richer agent identity")
			}
		}
	}
	if before.Agent.Ended != nil || before.Agent.State != harness.StateRunning {
		t.Fatal("activity mutated the earlier emitted agent")
	}
}
