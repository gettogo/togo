package omp

import (
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
)

// Seen live on omp 18.20: a background agent's own conversation can say
// "stop" and then carry on (omp adds a note of its own and the agent goes
// back to its tools). Its row said finished while the agent worked on for
// minutes. And a helper restart reads the main file first: omp's recorded
// exit then marks every agent of that time interrupted, though the agent's
// own file, read a moment later, says it finished.
const (
	subStop = `{"type":"message","id":"SUBID-2","parentId":"SUBID-1","timestamp":"TS","message":{"role":"assistant","content":[{"type":"text","text":"Audited."}],"stopReason":"stop"}}`
	subTool = `{"type":"message","id":"SUBID-3","parentId":"SUBID-2","timestamp":"TS","message":{"role":"assistant","content":[{"type":"toolCall","id":"call_SUBID","name":"read","arguments":{"path":"a.go"}}],"stopReason":"toolUse"}}`
	subEnd  = `{"type":"message","id":"SUBID-5","parentId":"SUBID-3","timestamp":"TS","message":{"role":"assistant","content":[{"type":"text","text":"Done."}],"stopReason":"stop"}}`
)

func subLine(tmpl, id, ts string) harness.Record {
	return line("subagents", strings.NewReplacer("SUBID", id, "TS", ts).Replace(tmpl))
}

func agentState(t *testing.T, m *session, id string) string {
	t.Helper()
	a := m.agents[id]
	if a == nil || a.Agent == nil {
		t.Fatalf("no agent %s", id)
	}
	return a.Agent.State
}

func TestAnAgentThatStopsAndCarriesOnRunsAgain(t *testing.T) {
	m := newSession(env())
	run(m, bgRecords(t)[:len(bgRecords(t))-3]) // two agents launched, their files opened
	now := t0.UTC().Format("2006-01-02T15:04:05.000Z")
	run(m, []harness.Record{subLine(subStop, "sub-a", now)})
	if got := agentState(t, m, "agent-AuditOne"); got != harness.StateOK {
		t.Fatalf("after its stop = %s, want ok", got)
	}
	out := run(m, []harness.Record{subLine(subTool, "sub-a", now)})
	if got := agentState(t, m, "agent-AuditOne"); got != harness.StateRunning {
		t.Fatalf("after it went back to its tools = %s, want running", got)
	}
	if m.agents["agent-AuditOne"].Agent.Ended != nil {
		t.Fatal("a running agent still has an end time")
	}
	rowed := false
	for _, op := range out.Ops {
		if op.Row != nil && op.Row.ID == "agent-AuditOne" && op.Row.State == harness.StateRunning {
			rowed = true
		}
	}
	if !rowed {
		t.Fatal("no running row was sent for the agent that carried on")
	}
	run(m, []harness.Record{subLine(subEnd, "sub-a", now)})
	if got := agentState(t, m, "agent-AuditOne"); got != harness.StateOK {
		t.Fatalf("after its last stop = %s, want ok", got)
	}
}

// A file read long after it was written (a replay after a restart) does
// not bring an agent back: one that stopped, went on and was killed with
// omp would otherwise say running for good.
func TestOldToolCallsDoNotReviveAnAgent(t *testing.T) {
	m := newSession(env())
	run(m, bgRecords(t)[:len(bgRecords(t))-3])
	old := t0.Add(-2 * agentFresh).UTC().Format("2006-01-02T15:04:05.000Z")
	run(m, []harness.Record{subLine(subStop, "sub-a", old), subLine(subTool, "sub-a", old)})
	if got := agentState(t, m, "agent-AuditOne"); got != harness.StateOK {
		t.Fatalf("an old tool call after a stop = %s, want it left ok", got)
	}
}

// omp's exit marks the agents of that time interrupted; an agent's own
// file saying it finished is the better word, whichever is read first.
func TestAnAgentsOwnStopOutranksOmpsExit(t *testing.T) {
	m := newSession(env())
	run(m, bgRecords(t)[:len(bgRecords(t))-3])
	run(m, []harness.Record{line("session", exitLine)})
	if got := agentState(t, m, "agent-AuditOne"); got != harness.StateInterrupted {
		t.Fatalf("after omp's exit = %s, want interrupted", got)
	}
	now := t0.UTC().Format("2006-01-02T15:04:05.000Z")
	out := run(m, []harness.Record{subLine(subStop, "sub-a", now)})
	if got := agentState(t, m, "agent-AuditOne"); got != harness.StateOK {
		t.Fatalf("after its own file said stop = %s, want ok", got)
	}
	if len(out.Ops) == 0 {
		t.Fatal("no row update for the agent that had finished")
	}
	// The other agent's file says nothing of an end: it stays interrupted.
	run(m, []harness.Record{subLine(subTool, "sub-b", now)})
	if got := agentState(t, m, "agent-AuditTwo"); got != harness.StateInterrupted {
		t.Fatalf("an interrupted agent's tool call = %s, want it left interrupted", got)
	}
}
