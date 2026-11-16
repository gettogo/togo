package omp

import (
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
)

// Seen on omp 18.16 to 18.20: omp records its own exit, and a title it
// changes later, as entries of the main conversation.
const (
	exitLine     = `{"type":"custom","customType":"session_exit","data":{"reason":"sighup","kind":"signal","recordedAt":"2026-11-06T10:05:00.000Z"},"id":"x001","parentId":"b003","timestamp":"2026-11-06T10:05:00.000Z"}`
	retitled     = `{"type":"title_change","id":"x002","parentId":"b003","timestamp":"2026-11-06T10:04:00.000Z","title":"Audit the parser","source":"auto"}`
	subTitle     = `{"type":"title","v":1,"title":"AuditOne","source":"auto","updatedAt":"2026-11-06T10:01:03.000Z"}`
	bashBgCall   = `{"type":"message","id":"j001","parentId":"e011","timestamp":"2026-11-06T10:00:30.000Z","message":{"role":"assistant","content":[{"type":"toolCall","id":"call_sh","name":"bash","arguments":{"command":"sleep 600","async":true}}],"stopReason":"toolUse"}}`
	bashBgResult = `{"type":"message","id":"j002","parentId":"j001","timestamp":"2026-11-06T10:00:31.000Z","message":{"role":"toolResult","toolCallId":"call_sh","toolName":"bash","content":[{"type":"text","text":"Backgrounded as job bg_1"}],"details":{"async":{"state":"running","jobId":"bg_1","type":"bash"},"timeoutSeconds":30},"isError":false}}`
)

// Background agents and jobs run inside omp: once omp records that it
// exited, the ones it never reported are over, and a job id omp hands out
// again after a resume gets its own running row.
func TestExitEndsBackgroundAgentsAndJobs(t *testing.T) {
	recs := records(t, "session.jsonl", "session")
	recs = append(recs, line("session", bashBgCall), line("session", bashBgResult),
		line("session", bgCall), line("session", bgResult), line("session", bgSaid))
	m := newSession(env())
	run(m, recs)
	out := run(m, []harness.Record{line("session", exitLine)})
	last := out.last()
	for _, id := range []string{"agent-AuditOne", "agent-AuditTwo"} {
		a := last[id]
		if a.Agent == nil || a.Agent.State != harness.StateInterrupted || a.Agent.Ended == nil {
			t.Errorf("%s after the exit: %+v", id, a.Agent)
		}
	}
	if j := last["job-bg_1"]; j.Background == nil || j.Background.State != harness.StateInterrupted || j.Background.Ended == nil {
		t.Errorf("the job after the exit: %+v", j.Background)
	}
	rows := map[string]string{}
	for _, op := range out.Ops {
		if op.Row != nil {
			rows[op.Row.ID] = op.Row.State
		}
	}
	for _, id := range []string{"agent-AuditOne", "agent-AuditTwo", "job-bg_1"} {
		if rows[id] != harness.StateInterrupted {
			t.Errorf("row %s = %q, want interrupted", id, rows[id])
		}
	}
	again := run(m, []harness.Record{
		line("session", strings.NewReplacer("j001", "j003", "call_sh", "call_sh2", "e011", "x001").Replace(bashBgCall)),
		line("session", strings.NewReplacer("j002", "j004", "j001", "j003", "call_sh", "call_sh2").Replace(bashBgResult)),
	}).last()
	if j := again["job-bg_1"]; j.Background == nil || j.Background.State != harness.StateRunning {
		t.Errorf("a job id reused after a resume: %+v", j.Background)
	}
}

// A running job's row uses the run states the mission summary counts.
func TestRunningJobRowCountsAsRunning(t *testing.T) {
	recs := append(records(t, "session.jsonl", "session"), line("session", bashBgCall), line("session", bashBgResult))
	for _, op := range run(newSession(env()), recs).Ops {
		if op.Section == harness.SectionBackground && op.Row != nil && op.Row.ID == "job-bg_1" {
			if op.Row.State != harness.StateRunning {
				t.Fatalf("row state = %q, want running", op.Row.State)
			}
			return
		}
	}
	t.Fatal("no job row")
}

// The title follows omp's later changes, and a subagent's own title never
// replaces the conversation's.
func TestTitleFollowsChangesNotSubagents(t *testing.T) {
	m := newSession(env())
	run(m, records(t, "session.jsonl", "session"))
	was := m.State()["title"]
	if was == "" {
		t.Fatal("no title from the fixture")
	}
	run(m, []harness.Record{line("subagents", subTitle), line("subagents", strings.ReplaceAll(bgHead, "SUBID", "sub-a"))})
	if got := m.State()["title"]; got != was {
		t.Errorf("a subagent's title replaced the conversation's: %q", got)
	}
	run(m, []harness.Record{line("session", retitled)})
	if got := m.State()["title"]; got != "Audit the parser" {
		t.Errorf("title after a change = %q", got)
	}
}
