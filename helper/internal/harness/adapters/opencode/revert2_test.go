package opencode

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/source/httpsrc"
)

// revertNotices are the undo notices among items, by id, in first-seen order.
func revertNotices(items []harness.Item) (ids []string, last map[string]harness.Item) {
	last = map[string]harness.Item{}
	for _, x := range items {
		if x.Kind != harness.KindNotice || !strings.HasPrefix(x.ID, "revert-") {
			continue
		}
		if _, ok := last[x.ID]; !ok {
			ids = append(ids, x.ID)
		}
		last[x.ID] = x
	}
	return ids, last
}

func stagedRecord(id, session, message, files string) harness.Record {
	raw := `{"id":"` + id + `","created":1794684324303,"type":"session.revert.staged","location":{"directory":"/tmp/work"},` +
		`"data":{"sessionID":"` + session + `","revert":{"messageID":"` + message + `","snapshot":"e60c60ddc4a2e67610cc8c55ec00df94a537da67","files":` + files + `}}}`
	return harness.Record{Source: "events", At: time.Now(), Data: json.RawMessage(raw)}
}

// Seen live on 2.3.0: /undo in the terminal took a turn back and the chat
// went on showing it as if it applied; /redo likewise changed nothing.
// testdata/v2/revert.sse is what a 2.2.5 server sent for one prompt, then a
// staged revert of it (undo), the revert cleared (redo), staged again and
// committed, which is what the terminal does before the next prompt
// (folders renamed, the provider's error text shortened).
func TestUndoAndRedoShowInTheChat(t *testing.T) {
	const prompt = "msg_1db84dd510014I4s7bYozTcTUn"
	m := NewEvents2(harness.Env{Cwd: "/tmp/work"})
	recs := recordedSSE(t, "testdata/v2/revert.sse")
	var items []harness.Item
	next := 0
	// through feeds records up to and including the nth one of a type.
	through := func(kind string) {
		t.Helper()
		for next < len(recs) {
			var ev event2
			_ = json.Unmarshal(recs[next].Data, &ev)
			got, _ := m.Map(recs[next])
			items = append(items, got...)
			next++
			if ev.Type == kind {
				return
			}
		}
		t.Fatalf("the recording has no further %s", kind)
	}

	through("session.revert.staged")
	ids, last := revertNotices(items)
	if len(ids) != 1 {
		t.Fatalf("after the undo: %d undo notices", len(ids))
	}
	undone := last[ids[0]]
	if undone.Notice.Level != "info" || undone.TurnID != "" || undone.Parent != "" {
		t.Fatalf("undo notice %+v", undone)
	}
	for _, want := range []string{"1 message undone", "Use the read and edit tools", "no longer applies", "/redo"} {
		if !strings.Contains(undone.Notice.Text, want) {
			t.Errorf("undo notice %q lacks %q", undone.Notice.Text, want)
		}
	}
	// The prompt itself stays in the chat: history is kept, not deleted.
	if u := run(NewEvents2(harness.Env{}), recs).last()[prompt]; u.User == nil {
		t.Fatal("the undone prompt left the chat")
	}

	through("session.revert.cleared")
	ids, last = revertNotices(items)
	if len(ids) != 1 {
		t.Fatalf("the redo added a notice beside the undo: %v", ids)
	}
	if text := last[ids[0]].Notice.Text; strings.Contains(text, "undone") || !strings.Contains(text, "applies again") {
		t.Fatalf("after the redo the notice reads %q", text)
	}

	through("session.revert.staged")
	ids, last = revertNotices(items)
	if len(ids) != 2 || !strings.Contains(last[ids[1]].Notice.Text, "1 message undone") {
		t.Fatalf("a second undo: %v %+v", ids, last)
	}
	if text := last[ids[0]].Notice.Text; !strings.Contains(text, "applies again") {
		t.Fatalf("the earlier redo's notice changed to %q", text)
	}

	through("session.revert.committed")
	ids, last = revertNotices(items)
	if len(ids) != 2 {
		t.Fatalf("the commit added a notice: %v", ids)
	}
	text := last[ids[1]].Notice.Text
	if strings.Contains(text, "/redo") || !strings.Contains(text, "1 message undone") || !strings.Contains(text, "removed") {
		t.Fatalf("after the commit the notice reads %q", text)
	}
}

// Each /undo moves the boundary one prompt back: one notice follows it,
// counting the prompts from the boundary on and naming the files put back.
// The prompts are the two of testdata/v2/events.sse's second session; the
// revert is the one a 2.3.0 server reported for a real undone edit
// (GET /api/session, the session's "revert").
func TestUndoCountsMessagesAndNamesFiles(t *testing.T) {
	const first, second = "msg_1b7984c5b0014bFAVdQbxVkiSP", "msg_1b798a4e0001aWJL61RJsddgPA"
	const files = `[{"file":"notes.txt","patch":"diff --git a/notes.txt b/notes.txt\nindex b14fb35..85c3040 100644\n--- a/notes.txt\n+++ b/notes.txt\n@@ -1,3 +1,3 @@\n alpha\n-beta-updated\n+beta\n gamma\n","additions":1,"deletions":1,"status":"modified"}]`
	m := NewEvents2(harness.Env{Cwd: "/tmp/work"})
	run(m, recorded2(t))

	items, _ := m.Map(stagedRecord("evt_a", v2Probe, second, files))
	if len(items) != 1 || !strings.Contains(items[0].Notice.Text, "1 message undone") ||
		!strings.Contains(items[0].Notice.Text, "Reply with just: second") || !strings.Contains(items[0].Notice.Text, "notes.txt +1 -1") {
		t.Fatalf("first undo %+v", items)
	}
	id := items[0].ID
	// The same event again (a replay) changes nothing.
	if again, _ := m.Map(stagedRecord("evt_a", v2Probe, second, files)); len(again) != 1 || again[0].ID != id || again[0].Notice.Text != items[0].Notice.Text {
		t.Fatalf("replayed undo %+v", again)
	}
	items, _ = m.Map(stagedRecord("evt_b", v2Probe, first, "[]"))
	if len(items) != 1 || items[0].ID != id || !strings.Contains(items[0].Notice.Text, "2 messages undone") ||
		!strings.Contains(items[0].Notice.Text, "Reply with just: ok") {
		t.Fatalf("second undo %+v", items)
	}
	// An undo in a session this chat does not follow is not its own.
	if items, _ := m.Map(stagedRecord("evt_c", "ses_other", first, "[]")); len(items) != 0 {
		t.Fatalf("another session's undo %+v", items)
	}
	// The earlier conversation's prompts are not counted in this one.
	if strings.Contains(items[0].Notice.Text, "6 messages") {
		t.Fatalf("counted across conversations: %q", items[0].Notice.Text)
	}
}

// What ends an undo the mapper holds: the redo and the commit (above), the
// next prompt (the terminal commits first; the stream may have dropped
// that), and the terminal going to another conversation. A later undo then
// gets a notice of its own, where it happened, and a redo with no undo on
// record (the helper restarted in between) still says so.
func TestUndoStateEnds(t *testing.T) {
	enq := func(m *Events2, session, inbox string) {
		raw := `{"type":"session.inbox.enqueued","data":{"sessionID":"` + session + `","inboxID":"` + inbox + `","item":{"type":"user","payload":{"text":"hi"}}}}`
		m.Map(harness.Record{Source: "events", At: time.Now(), Data: json.RawMessage(raw)})
	}
	rec := func(raw string) harness.Record {
		return harness.Record{Source: "events", At: time.Now(), Data: json.RawMessage(raw)}
	}

	m := NewEvents2(harness.Env{})
	enq(m, "ses_a", "msg_1")
	a, _ := m.Map(stagedRecord("evt_a", "ses_a", "msg_1", "[]"))
	enq(m, "ses_a", "msg_2")
	b, _ := m.Map(stagedRecord("evt_b", "ses_a", "msg_2", "[]"))
	if len(a) != 1 || len(b) != 1 || a[0].ID == b[0].ID {
		t.Fatalf("an undo after the next prompt reused the notice: %+v %+v", a, b)
	}
	if !strings.Contains(b[0].Notice.Text, "1 message undone") {
		t.Fatalf("the committed prompt is still counted: %q", b[0].Notice.Text)
	}
	// A stream that came back keeps the notice it has: the redo that
	// follows still finds it.
	m.Map(rec(`{"type":"` + httpsrc.Reconnected + `"}`))
	c, _ := m.Map(rec(`{"id":"evt_c","type":"session.revert.cleared","data":{"sessionID":"ses_a"}}`))
	if len(c) != 1 || c[0].ID != b[0].ID {
		t.Fatalf("redo after a reconnect %+v", c)
	}
	// The same redo again is one notice, not two.
	if again, _ := m.Map(rec(`{"id":"evt_c","type":"session.revert.cleared","data":{"sessionID":"ses_a"}}`)); len(again) != 0 {
		t.Fatalf("replayed redo %+v", again)
	}

	m = NewEvents2(harness.Env{})
	enq(m, "ses_a", "msg_1")
	a, _ = m.Map(stagedRecord("evt_a", "ses_a", "msg_1", "[]"))
	enq(m, "ses_b", "msg_9")
	if c, _ := m.Map(rec(`{"id":"evt_d","type":"session.revert.cleared","data":{"sessionID":"ses_b"}}`)); len(c) != 1 || c[0].ID == a[0].ID {
		t.Fatalf("a redo in the next conversation touched the last one's notice: %+v", c)
	}

	// No undo on record: the redo is still told, the commit has nothing
	// to change.
	m = NewEvents2(harness.Env{Argv: []string{"opencode", "--session", "ses_a"}})
	if c, _ := m.Map(rec(`{"id":"evt_e","type":"session.revert.cleared","data":{"sessionID":"ses_a"}}`)); len(c) != 1 || !strings.Contains(c[0].Notice.Text, "apply again") {
		t.Fatalf("redo with no undo on record %+v", c)
	}
	if c, _ := m.Map(rec(`{"id":"evt_f","type":"session.revert.committed","data":{"sessionID":"ses_a","to":"msg_1"}}`)); len(c) != 0 {
		t.Fatalf("commit with no undo on record %+v", c)
	}
}
