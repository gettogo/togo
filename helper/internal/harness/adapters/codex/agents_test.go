package codex

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

const recordedChild = "019a0000-0000-7000-8000-000000000002"
const recordedMain = "019a0000-0000-7000-8000-000000000001"

func recordedAgent(t *testing.T) (*AppServer, harness.Item) {
	t.Helper()
	m := NewAppServer(harness.Env{Cwd: "/Users/me/proj"})
	out := run(m, records(t, "appserver-turn.jsonl")[:28])
	var agent harness.Item
	for _, x := range out.items {
		if x.Agent != nil && x.Agent.AgentID == recordedChild {
			agent = x
		}
	}
	if agent.Agent == nil {
		t.Fatal("recorded child missing")
	}
	return m, agent
}

func agentEvent(t *testing.T, m *AppServer, method string, params any, at time.Time) ([]harness.Item, []harness.Op) {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"method": method, "params": params})
	if err != nil {
		t.Fatal(err)
	}
	return m.Map(harness.Record{Source: "appserver", At: at, Data: raw})
}

func requireAgent(t *testing.T, items []harness.Item, id string) harness.Item {
	t.Helper()
	for _, x := range items {
		if x.Agent != nil && x.Agent.AgentID == id {
			return x
		}
	}
	t.Fatal("agent item missing from update")
	return harness.Item{}
}

func TestAgentStatusKeepsIdentityAndOpeningTarget(t *testing.T) {
	for _, status := range []string{"active", "idle", "notLoaded"} {
		t.Run(status, func(t *testing.T) {
			m, before := recordedAgent(t)
			items, ops := agentEvent(t, m, "thread/status/changed", map[string]any{"threadId": recordedChild, "status": map[string]string{"type": status}}, t0.Add(time.Minute))
			after := requireAgent(t, items, recordedChild)
			if after.Agent.Name != before.Agent.Name || after.Agent.Role != before.Agent.Role || !after.Agent.Started.Equal(before.Agent.Started) || !after.At.Equal(before.At) {
				t.Fatal("status replaced agent identity or clock")
			}
			if len(ops) != 1 || ops[0].Row == nil {
				t.Fatal("missing complete row")
			}
			row := ops[0].Row
			if row.Title != before.Agent.Name || row.Subtitle != before.Agent.Role || row.Started == nil || !row.Started.Equal(before.Agent.Started) || row.Open == nil || row.Open.Ref != before.ID {
				t.Fatal("status erased agent row fields")
			}
			if row.State != harness.StateRunning {
				t.Fatalf("transport status %s changed an unfinished agent to %s", status, row.State)
			}
		})
	}
}

func TestAgentFollowUpAndDuplicateSpawnKeepMetadata(t *testing.T) {
	for _, tool := range []string{"wait", "sendInput", "spawnAgent"} {
		t.Run(tool, func(t *testing.T) {
			m, before := recordedAgent(t)
			items, _ := agentEvent(t, m, "item/completed", map[string]any{"threadId": recordedMain, "turnId": "turn-parent", "item": map[string]any{"type": "collabAgentToolCall", "id": "call-repeat", "tool": tool, "status": "completed", "prompt": "A follow-up instruction", "receiverThreadIds": []string{recordedChild}, "agentsStates": map[string]any{recordedChild: map[string]string{"status": "running"}}}}, t0.Add(time.Minute))
			after := requireAgent(t, items, recordedChild)
			if after.Agent.Name != before.Agent.Name || after.Agent.Role != before.Agent.Role || !after.Agent.Started.Equal(before.Agent.Started) || !after.At.Equal(before.At) {
				t.Fatal("follow-up renamed the agent or restarted its clock")
			}
		})
	}
}

func TestAgentCompletionSurvivesLateIdleAndUnknownUpdates(t *testing.T) {
	m, before := recordedAgent(t)
	params := map[string]any{"threadId": recordedMain, "item": map[string]any{"type": "collabAgentToolCall", "id": "call-wait", "tool": "wait", "receiverThreadIds": []string{recordedChild}, "agentsStates": map[string]any{recordedChild: map[string]string{"status": "completed"}}}}
	ended := t0.Add(time.Minute)
	items, _ := agentEvent(t, m, "item/completed", params, ended)
	after := requireAgent(t, items, recordedChild)
	if after.Agent.State != harness.StateOK || after.Agent.Ended == nil || !after.Agent.Ended.Equal(ended) || !after.Agent.Started.Equal(before.Agent.Started) {
		t.Fatal("completion lost the original clock")
	}
	items, _ = agentEvent(t, m, "thread/status/changed", map[string]any{"threadId": recordedChild, "status": map[string]string{"type": "idle"}}, ended.Add(time.Second))
	after = requireAgent(t, items, recordedChild)
	if after.Agent.State != harness.StateOK || after.Agent.Ended == nil || !after.Agent.Ended.Equal(ended) {
		t.Fatal("late idle erased completion")
	}
	delete(params["item"].(map[string]any), "agentsStates")
	items, _ = agentEvent(t, m, "item/completed", params, ended.Add(2*time.Second))
	after = requireAgent(t, items, recordedChild)
	if after.Agent.State != harness.StateOK || after.Agent.Ended == nil || !after.Agent.Ended.Equal(ended) {
		t.Fatal("an update without agent state resurrected completion")
	}
}

func TestAgentReplayKeepsClockAndOtherAgentsSeparate(t *testing.T) {
	m, before := recordedAgent(t)
	recs := records(t, "appserver-turn.jsonl")
	replay := recs[27]
	replay.At = t0.Add(time.Hour)
	items, _ := m.Map(replay)
	after := requireAgent(t, items, recordedChild)
	if !after.Agent.Started.Equal(before.Agent.Started) || !after.At.Equal(before.At) {
		t.Fatal("resume replay restarted the clock")
	}
	items, _ = agentEvent(t, m, "item/completed", map[string]any{"threadId": recordedMain, "item": map[string]any{"type": "collabAgentToolCall", "id": "call-second", "tool": "spawnAgent", "prompt": "Check the documentation", "receiverThreadIds": []string{"other-thread"}}}, t0.Add(2*time.Hour))
	other := requireAgent(t, items, "other-thread")
	if other.ID == before.ID || other.Agent.Name == before.Agent.Name {
		t.Fatal("independent agents shared identity")
	}
	items, _ = agentEvent(t, m, "thread/status/changed", map[string]any{"threadId": recordedChild, "status": map[string]string{"type": "active"}}, t0.Add(3*time.Hour))
	after = requireAgent(t, items, recordedChild)
	if after.Agent.Name != before.Agent.Name {
		t.Fatal("another agent overwrote the first")
	}
}

func TestResumedAgentUsesItsCreationClockAfterMapperReplacement(t *testing.T) {
	rec := records(t, "appserver-turn.jsonl")[27]
	var message map[string]any
	if err := json.Unmarshal(rec.Data, &message); err != nil {
		t.Fatal(err)
	}
	thread := message["params"].(map[string]any)["result"].(map[string]any)["thread"].(map[string]any)
	created := t0.Add(-time.Minute)
	thread["createdAt"] = created.Unix()
	rec.Data, _ = json.Marshal(message)
	for _, at := range []time.Time{t0, t0.Add(time.Hour)} {
		rec.At = at
		m := NewAppServer(harness.Env{})
		items, _ := m.Map(rec)
		agent := requireAgent(t, items, recordedChild)
		if !agent.Agent.Started.Equal(created) || !agent.At.Equal(created) {
			t.Fatal("replacement mapper substituted its own arrival time")
		}
	}
}

func TestAgentFailureDoesNotMutateTheEarlierPublishedIdentity(t *testing.T) {
	m, before := recordedAgent(t)
	items, ops := agentEvent(t, m, "thread/status/changed", map[string]any{"threadId": recordedChild, "status": map[string]string{"type": "systemError"}}, t0.Add(time.Minute))
	after := requireAgent(t, items, recordedChild)
	if before.Agent.State != harness.StateRunning || before.Agent.Ended != nil {
		t.Fatal("an update mutated the earlier emitted agent")
	}
	if after.Agent.State != harness.StateFailed || after.Agent.Ended == nil || after.Agent.Name != before.Agent.Name || after.Agent.Role != before.Agent.Role {
		t.Fatal("failure lost the agent identity")
	}
	if len(ops) != 1 || ops[0].Row.Title != before.Agent.Name || ops[0].Row.Open.Ref != before.ID {
		t.Fatal("failure replaced the row with partial data")
	}
}
