package omp

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
)

// Shapes seen on omp 18.19 and 18.20: a task call names its tasks by "name"
// and "task", its result comes back at once with details.async running,
// and each agent settles later through an async-result custom_message.
const (
	bgCall   = `{"type":"message","id":"b001","parentId":"e011","timestamp":"2026-11-06T10:01:00.000Z","message":{"role":"assistant","content":[{"type":"toolCall","id":"call_bg","name":"task","arguments":{"i":"Delegating two audits","context":"shared","tasks":[{"name":"AuditOne","task":"Complete assignment thoroughly:\n\n# Target\nAudit one."},{"name":"AuditTwo","task":"Complete assignment thoroughly:\n\n# Target\nAudit two."}]}}],"stopReason":"toolUse","timestamp":1794045660000}}`
	bgResult = `{"type":"message","id":"b002","parentId":"b001","timestamp":"2026-11-06T10:01:01.000Z","message":{"role":"toolResult","toolCallId":"call_bg","toolName":"task","content":[{"type":"text","text":"Started 2 agents"}],"details":{"projectAgentsDir":null,"results":[],"totalDurationMs":3,"progress":[{"index":0,"id":"AuditOne","agent":"task","status":"pending"},{"index":1,"id":"AuditTwo","agent":"task","status":"pending"}],"async":{"state":"running","jobId":"AuditOne","type":"task"}},"isError":false,"timestamp":1794045661000}}`
	bgSaid   = `{"type":"message","id":"b003","parentId":"b002","timestamp":"2026-11-06T10:01:02.000Z","message":{"role":"assistant","content":[{"type":"text","text":"Both agents are running."}],"stopReason":"stop","timestamp":1794045662000}}`
	bgHead   = `{"type":"session","version":3,"id":"SUBID","timestamp":"2026-11-06T10:01:03.000Z","cwd":"/Users/dev/project","parentSession":"/Users/dev/.omp/agent/sessions/-project/2026-11-06T10-00-00-000Z_0b7c1f2a9e.jsonl"}`
	bgSubMsg = `{"type":"message","id":"SUBID-1","parentId":null,"timestamp":"2026-11-06T10:01:04.000Z","message":{"role":"user","content":"Audit.","timestamp":1794045664000}}`
	bgDone   = `{"type":"custom_message","customType":"async-result","display":true,"details":{"jobs":[{"jobId":"AuditOne","type":"task","status":"completed","agentId":"AuditOne","label":"AuditOne","durationMs":60000}]},"id":"b004","parentId":"b003","timestamp":"2026-11-06T10:02:00.000Z"}`
	bgAfter  = `{"type":"message","id":"b005","parentId":"b004","timestamp":"2026-11-06T10:02:01.000Z","message":{"role":"assistant","content":[{"type":"text","text":"AuditOne is back."}],"stopReason":"stop","timestamp":1794045721000}}`
	bgFailed = `{"type":"custom_message","customType":"async-result","display":true,"details":{"jobs":[{"jobId":"AuditTwo","type":"task","status":"failed","agentId":"AuditTwo","label":"AuditTwo","durationMs":90000}]},"id":"b006","parentId":"b005","timestamp":"2026-11-06T10:03:00.000Z"}`
)

func bgRecords(t *testing.T) []harness.Record {
	t.Helper()
	recs := records(t, "session.jsonl", "session")
	add := func(feed, line string) {
		recs = append(recs, harness.Record{Source: feed, At: t0, Data: json.RawMessage(line)})
	}
	add("session", bgCall)
	add("session", bgResult)
	add("session", bgSaid)
	for _, id := range []string{"sub-a", "sub-b"} {
		add("subagents", strings.ReplaceAll(bgHead, "SUBID", id))
		add("subagents", strings.ReplaceAll(bgSubMsg, "SUBID", id))
	}
	add("session", bgDone)
	add("session", bgAfter)
	add("session", bgFailed)
	return recs
}

// Background agents keep one row each, named by the task, running until
// omp reports that agent settled.
func TestBackgroundAgentsKeepOneRowEach(t *testing.T) {
	recs := bgRecords(t)
	before := len(run(newSession(env()), records(t, "session.jsonl", "session")).last())
	m := newSession(env())
	launched := run(m, recs[:len(recs)-3]).last()
	one, two := launched["agent-AuditOne"], launched["agent-AuditTwo"]
	if one.Agent == nil || two.Agent == nil {
		t.Fatal("no rows named by the tasks")
	}
	if one.Agent.Name != "AuditOne" || one.Agent.State != harness.StateRunning || one.Agent.Ended != nil {
		t.Fatalf("a launched background agent: %+v", one.Agent)
	}
	for id, it := range launched {
		if it.Kind == harness.KindAgent && it.Agent.Name == "Subagent" {
			t.Fatalf("a second row for an agent's file: %s", id)
		}
		if it.Kind == harness.KindBackground {
			t.Fatalf("a background-job row for a task call: %s", id)
		}
	}
	done := run(newSession(env()), recs).last()
	if a := done["agent-AuditOne"].Agent; a.State != harness.StateOK || a.Ended == nil {
		t.Fatalf("AuditOne after its result: %+v", a)
	}
	if a := done["agent-AuditTwo"].Agent; a.State != harness.StateFailed || a.Ended == nil {
		t.Fatalf("AuditTwo after its failure: %+v", a)
	}
	// Two agent rows, the call, three messages and the subagents' own two.
	if n := len(done) - before; n > 9 {
		t.Fatalf("%d new items, want no extra rows", n)
	}
}

// What the main agent says after a note from omp (a background result, a
// reminder) is the main conversation's, whichever file was read last.
func TestMainConversationContinuesAfterSubagentFiles(t *testing.T) {
	got := run(newSession(env()), bgRecords(t)).last()
	var it harness.Item
	ok := false
	for _, v := range got {
		if v.Assistant != nil && strings.Contains(v.Assistant.Text, "AuditOne is back") {
			it, ok = v, true
		}
	}
	if !ok {
		t.Fatal("the main agent's reply after a background result was not mapped")
	}
	if it.Parent != "" {
		t.Fatalf("the reply went to a subagent's conversation: parent %q", it.Parent)
	}
}

// An agent whose own conversation was aborted is over, though omp sends no
// result for it.
func TestAbortedBackgroundAgentEnds(t *testing.T) {
	recs := bgRecords(t)
	recs = recs[:len(recs)-3]
	stop := `{"type":"message","id":"sub-b-2","parentId":"sub-b-1","timestamp":"2026-11-06T10:01:30.000Z","message":{"role":"assistant","content":[],"stopReason":"aborted","timestamp":1794045690000}}`
	recs = append(recs, harness.Record{Source: "subagents", At: t0, Data: json.RawMessage(stop)})
	got := run(newSession(env()), recs).last()
	if a := got["agent-AuditTwo"].Agent; a.State != harness.StateCancelled || a.Ended == nil {
		t.Fatalf("the aborted agent: %+v", a)
	}
	if a := got["agent-AuditOne"].Agent; a.State != harness.StateRunning {
		t.Fatalf("the other agent: %+v", a)
	}
}
