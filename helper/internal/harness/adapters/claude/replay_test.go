package claude

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
)

// summary is what a chat ends up showing: each assistant message's text,
// the Changes rows and the Plan rows.
func summary(r run) (map[string]string, map[string]string, []harness.Row) {
	texts, changes := map[string]string{}, map[string]string{}
	var plan []harness.Row
	for id, it := range r.final {
		if it.Assistant != nil {
			texts[id] = it.Assistant.Text
		}
	}
	for _, op := range r.ops {
		switch {
		case op.Section == harness.SectionChanges && op.Row != nil:
			changes[op.Row.ID] = op.Row.Subtitle
		case op.Section == harness.SectionPlan && op.Op == harness.OpReplace:
			plan = op.Rows
		}
	}
	return texts, changes, plan
}

// Reading a transcript again (a conversation left and come back to) must
// leave the chat as one read does: no doubled text, plan rows or counts.
func TestTranscriptReadAgainChangesNothing(t *testing.T) {
	for _, name := range []string{"transcript-real.jsonl", "transcript-basic.jsonl"} {
		recs := records(t, name)
		once := newTranscript(env())
		t1, c1, p1 := summary(mapAll(once, recs))
		twice := newTranscript(env())
		mapAll(twice, recs)
		t2, c2, p2 := summary(mapAll(twice, recs))
		if !reflect.DeepEqual(t1, t2) {
			t.Errorf("%s: text after a second read\n%v\nwant\n%v", name, t2, t1)
		}
		if !reflect.DeepEqual(c1, c2) {
			t.Errorf("%s: changes after a second read %v, want %v", name, c2, c1)
		}
		if !reflect.DeepEqual(p1, p2) {
			t.Errorf("%s: plan after a second read %v, want %v", name, p2, p1)
		}
	}
}

// A retry warning is for the attempts that failed. Once the request goes
// through, the turn shows that it recovered, not that it is retrying.
func TestRetryNoticeSettlesWhenTheRequestSucceeds(t *testing.T) {
	errs := records(t, "transcript-errors.jsonl")
	basic := records(t, "transcript-basic.jsonl")
	m := newTranscript(env())
	r := mapAll(m, []harness.Record{errs[2], errs[3], basic[6]})
	retry := r.final["u2.retry"].Notice
	if retry == nil || retry.Level != "info" || strings.Contains(retry.Text, "retrying") {
		t.Fatalf("retry after success = %+v", retry)
	}
}

// A task notification can reach the transcript as the user record Claude Code
// writes for itself with no queued copy before it (a background shell or
// agent finished while the session restarted). The agent or shell it names
// still ends.
func TestTaskNotificationUserRecordSettlesBackgroundWork(t *testing.T) {
	var recs []harness.Record
	for _, rec := range records(t, "transcript-real.jsonl") {
		var d map[string]any
		if json.Unmarshal(rec.Data, &d) == nil && d["type"] == "queue-operation" {
			d["type"], d["origin"] = "user", map[string]any{"kind": "task-notification"}
			delete(d, "operation")
			d["message"] = map[string]any{"role": "user", "content": d["content"]}
			delete(d, "content")
			rec.Data, _ = json.Marshal(d)
		}
		recs = append(recs, rec)
	}
	r := mapAll(newTranscript(env()), recs)
	for _, id := range []string{"agent-a5c86855b3d4b6de7", "bg-b7nnmqbeo", "agent-a8b7ce01541ed1304"} {
		it := r.final[id]
		state := ""
		if it.Agent != nil {
			state = it.Agent.State
		} else if it.Background != nil {
			state = it.Background.State
		}
		if state != harness.StateOK {
			t.Errorf("%s state %q, want ok", id, state)
		}
	}
}

// A finished call's output goes out with its item; the mapper does not keep
// it for the rest of the session.
func TestMapperDoesNotKeepToolOutput(t *testing.T) {
	m := newTranscript(env())
	r := mapAll(m, records(t, "transcript-basic.jsonl"))
	if r.final["toolu_01"].Tool.Output == "" {
		t.Fatal("the published item lost its output")
	}
	for id, it := range m.tools {
		if it.Tool != nil && it.Tool.Output != "" {
			t.Errorf("mapper keeps the output of %s", id)
		}
	}
}
