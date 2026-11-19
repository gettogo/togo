package omp

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

// omp's report of a background agent read after the agent's own file
// recorded its exit (the feeds run apart) leaves the row as the agent
// ended it, and is not shown as a background job.
func TestReportAfterExitLeavesTheAgentRow(t *testing.T) {
	for name, report := range map[string]string{"completed": bgDone, "failed": strings.Replace(bgDone, `"completed"`, `"failed"`, 1)} {
		t.Run(name, func(t *testing.T) {
			exit := harness.Record{Source: "subagents", At: t0, File: subDir + "AuditOne.jsonl", Data: json.RawMessage(subExit)}
			out := run(newSession(env()), launched(t), fileRecs("AuditOne", "sub-one"), []harness.Record{exit, line("session", report)})
			for id, it := range out.last() {
				if it.Kind == harness.KindBackground {
					t.Fatalf("a background job %s for a subagent", id)
				}
			}
			for _, op := range out.Ops {
				if op.Section == harness.SectionBackground {
					t.Fatalf("a background row %+v", op.Row)
				}
			}
			a := out.last()["agent-AuditOne"].Agent
			want := time.Date(2026, 11, 6, 10, 1, 21, 0, time.UTC)
			if a == nil || a.State != harness.StateOK || a.Ended == nil || !a.Ended.Equal(want) {
				t.Fatalf("agent row %+v", a)
			}
		})
	}
}

// A task report for an agent this mapper never saw (its call was read
// before the mapper existed) is still shown finished, as before.
func TestReportOfAnUnknownTaskIsAFinishedJob(t *testing.T) {
	out := run(newSession(env()), opened(t), []harness.Record{line("session", bgDone)})
	j := out.last()["job-AuditOne"]
	if j.Background == nil || j.Background.State != harness.StateOK || j.Background.Ended == nil {
		t.Fatalf("job %+v", j)
	}
}
