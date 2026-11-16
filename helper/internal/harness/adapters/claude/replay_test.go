package claude

import (
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
