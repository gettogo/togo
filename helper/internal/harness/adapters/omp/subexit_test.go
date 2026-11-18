package omp

import (
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
)

// Seen on omp 18.21: a subagent's file ends with the record of its own
// exit, like the main conversation's.
const (
	subHeader = `{"type":"session","version":3,"id":"01a1eb44-6a54","timestamp":"2026-11-06T10:01:00.000Z","cwd":"/Users/dev/project","parentSession":"/Users/dev/.omp/agent/sessions/-project/2026-11-06T10-00-00-000Z_0b7c1f2a9e.jsonl"}`
	subAsk    = `{"type":"message","id":"q001","parentId":null,"timestamp":"2026-11-06T10:01:01.000Z","message":{"role":"user","content":"Check the parser."}}`
	subSaid   = `{"type":"message","id":"q002","parentId":"q001","timestamp":"2026-11-06T10:01:20.000Z","message":{"role":"assistant","content":[{"type":"text","text":"The parser is fine."}],"stopReason":"stop"}}`
	subExit   = `{"type":"custom","customType":"session_exit","data":{"reason":"dispose","kind":"normal","recordedAt":"2026-11-06T10:01:21.000Z"},"id":"q003","parentId":"q002","timestamp":"2026-11-06T10:01:21.000Z"}`
	subKilled = `{"type":"custom","customType":"session_exit","data":{"reason":"sighup","kind":"signal","recordedAt":"2026-11-06T10:01:21.000Z"},"id":"q003","parentId":"q002","timestamp":"2026-11-06T10:01:21.000Z"}`
)

func agentRows(out mapped) map[string]string {
	rows := map[string]string{}
	for _, op := range out.Ops {
		if op.Row != nil && op.Section == harness.SectionAgents {
			rows[op.Row.ID] = op.Row.State
		}
	}
	return rows
}

// A subagent's file read with no task call waiting for it (after a helper
// restart the files can be read before the conversation that started them)
// gets a row of its own. Nothing the main conversation says later names that
// row, so the file's own exit has to end it, or it shows as running for good
// and holds the session as working.
func TestSubagentFileEndsItsOwnRow(t *testing.T) {
	m := newSession(env())
	run(m, records(t, "session.jsonl", "session")[:2]) // title and header: the conversation is known, no call read yet
	out := run(m, []harness.Record{line("subagents", subHeader), line("subagents", subAsk), line("subagents", subSaid)})
	rows := agentRows(out)
	if len(rows) != 1 || rows["agent-01a1eb44-6a54"] != harness.StateRunning {
		t.Fatalf("rows before the exit: %v", rows)
	}
	out = run(m, []harness.Record{line("subagents", subExit)})
	if rows := agentRows(out); rows["agent-01a1eb44-6a54"] != harness.StateOK {
		t.Fatalf("rows after the subagent's exit: %v", rows)
	}
	a := out.last()["agent-01a1eb44-6a54"]
	if a.Agent == nil || a.Agent.Ended == nil {
		t.Fatalf("the agent item was not ended: %+v", a.Agent)
	}
	// Reading the file again (a replay) changes nothing.
	if again := run(m, []harness.Record{line("subagents", subHeader), line("subagents", subExit)}); len(agentRows(again)) != 0 {
		t.Fatalf("a second read moved the row: %v", agentRows(again))
	}
}

// A subagent that was killed is interrupted, not finished.
func TestKilledSubagentIsInterrupted(t *testing.T) {
	m := newSession(env())
	run(m, records(t, "session.jsonl", "session")[:2])
	out := run(m, []harness.Record{line("subagents", subHeader), line("subagents", subAsk), line("subagents", subKilled)})
	if rows := agentRows(out); rows["agent-01a1eb44-6a54"] != harness.StateInterrupted {
		t.Fatalf("rows: %v", rows)
	}
}

// A background agent omp never reports (its result was collected another
// way) still ends when its own file records the exit.
func TestBackgroundAgentWithNoReportEndsAtItsExit(t *testing.T) {
	m := newSession(env())
	run(m, records(t, "session.jsonl", "session"), []harness.Record{line("session", bgCall), line("session", bgResult)})
	head := `{"type":"session","version":3,"id":"aud1","timestamp":"2026-11-06T10:01:03.000Z","cwd":"/Users/dev/project","parentSession":"/Users/dev/.omp/agent/sessions/-project/2026-11-06T10-00-00-000Z_0b7c1f2a9e.jsonl"}`
	out := run(m, []harness.Record{line("subagents", head), line("subagents", subAsk), line("subagents", subSaid), line("subagents", subExit)})
	if rows := agentRows(out); rows["agent-AuditOne"] != harness.StateOK {
		t.Fatalf("rows: %v", rows)
	}
	// The main conversation's exit, read later, leaves the finished agent as it ended.
	out = run(m, []harness.Record{line("session", exitLine)})
	if rows := agentRows(out); rows["agent-AuditOne"] != "" || rows["agent-AuditTwo"] != harness.StateInterrupted {
		t.Fatalf("rows after omp's exit: %v", rows)
	}
}
