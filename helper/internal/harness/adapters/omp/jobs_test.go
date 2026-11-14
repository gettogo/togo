package omp

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/engine"
	"github.com/gettogo/togo/helper/internal/harness/spec"
)

// Jobs are keyed by the name `omp ps stop` takes; a global service's id
// carries its scope, and Stop passes it as --global.
func TestJobsStopByName(t *testing.T) {
	a, err := spec.Parse(AdapterFile())
	if err != nil {
		t.Fatal(err)
	}
	src := a.Sources["jobs"]
	if src.Timeout.Seconds() != 15 {
		t.Errorf("timeout %v", src.Timeout)
	}
	var in any
	json.Unmarshal([]byte(`[{"kind":"project","daemons":[{"name":"dev","command":"npm run dev","state":"running"}]},
		{"kind":"global","scope":"relay","daemons":[{"name":"browser","state":"running"}]}]`), &in)
	out, err := engine.JQ(src.Select, in, nil)
	if err != nil {
		t.Fatal(err)
	}
	rows := out[0].([]any)
	if rows[0].(map[string]any)["rowId"] != "dev" || rows[1].(map[string]any)["rowId"] != "global:relay:browser" {
		t.Fatalf("rows %v", rows)
	}
	if sub, _ := engine.MapValue(a.Mission["background"].Map["item.subtitle"], rows[1], nil); sub != "running" {
		t.Errorf("subtitle %q", sub)
	}
	stop := a.Actions["stop"].Branch
	cond, _ := engine.Render(stop[0].When, map[string]any{"target": "global:relay:browser"})
	if cond != "true" {
		t.Fatal("global branch not taken")
	}
	if c, _ := engine.Render(stop[0].When, map[string]any{"target": "dev"}); c != "" {
		t.Fatal("project job took the global branch")
	}
	var argv []string
	for _, x := range stop[0].CLI.Argv {
		r, _ := engine.Render(x, map[string]any{"target": "global:relay:browser"})
		argv = append(argv, r)
	}
	if want := "omp ps stop browser --global=relay"; strings.Join(argv, " ") != want {
		t.Errorf("argv %q, want %q", strings.Join(argv, " "), want)
	}
}

// Seen live on omp 18.20: bash with async: true came back "Backgrounded as
// job bg_1", /jobs showed it running, and the app's background section was
// empty. The records below are that session's result and settle message.
func TestAsyncJobShowsWhileItRunsAndCloses(t *testing.T) {
	m := newSession(env())
	rec := func(raw string) harness.Record {
		return harness.Record{Source: "session", At: t0, Data: json.RawMessage(raw)}
	}
	start := run(m, []harness.Record{
		rec(`{"type":"session","version":3,"id":"aaaa1111"}`),
		rec(`{"type":"message","id":"1b05f710","timestamp":"2026-11-14T00:51:15.992Z","message":{"role":"user","content":[{"type":"text","text":"run it"}]}}`),
		rec(`{"type":"message","id":"ce1b5408","parentId":"1b05f710","timestamp":"2026-11-14T00:51:21.013Z","message":{"role":"assistant","content":[{"type":"toolCall","id":"0F03617E","name":"bash","arguments":{"async":true,"command":"sleep 15; printf OMP_ASYNC_LOCAL","timeout":30}}]}}`),
		rec(`{"type":"message","id":"49aad0d5","parentId":"ce1b5408","timestamp":"2026-11-14T00:51:50.420Z","message":{"role":"toolResult","toolCallId":"0F03617E","toolName":"bash","content":[{"type":"text","text":"Backgrounded as job bg_1"}],"details":{"async":{"state":"running","jobId":"bg_1","type":"bash"},"timeoutSeconds":30},"isError":false}}`),
	})
	row := func(o mapped) *harness.Row {
		var r *harness.Row
		for _, op := range o.Ops {
			if op.Section == harness.SectionBackground && op.Row != nil && op.Row.ID == "job-bg_1" {
				r = op.Row
			}
		}
		return r
	}
	if r := row(start); r == nil || r.State != harness.RowActive || !strings.Contains(r.Subtitle, "sleep 15") {
		t.Fatalf("running job row = %+v, want an active row with the command", r)
	}
	done := run(m, []harness.Record{rec(`{"type":"custom_message","customType":"async-result","content":"<system-notice>\nBackground job bg_1 has completed.\n</system-notice>","display":true,"details":{"jobs":[{"jobId":"bg_1","type":"bash","status":"completed","label":"sleep 15; printf OMP_ASYNC_LOCAL","durationMs":15084}]},"id":"4b222f56","parentId":"f9287723","timestamp":"2026-11-14T00:52:05.504Z"}`)})
	if r := row(done); r == nil || r.State != harness.RowDone {
		t.Fatalf("settled job row = %+v, want done", r)
	}
	var ended bool
	for _, it := range done.Items {
		if it.Background != nil && it.Background.JobID == "bg_1" {
			ended = it.Background.State == harness.StateOK && it.Background.Ended != nil
		}
	}
	if !ended {
		t.Error("the job item did not end")
	}
}
