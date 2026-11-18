package opencode

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/engine"
	"github.com/gettogo/togo/helper/internal/harness/source/httpsrc"
	"github.com/gettogo/togo/helper/internal/harness/spec"
)

const shellsSession = "ses_e247b235fffeUZ5d0j2BqcfYEW"

func shellCreated(id, session, status string) harness.Record {
	raw := `{"id":"evt_` + id + `","created":1794684326366,"type":"shell.created","location":{"directory":"/tmp/work"},"data":{"info":{"id":"` + id +
		`","status":"` + status + `","command":"sleep 60","cwd":"/tmp/work","shell":"/bin/zsh","file":"/tmp/opencode/` + id + `.out","metadata":{"sessionID":"` + session + `"},"time":{"started":1794684326364}}}}`
	return harness.Record{Source: "events", At: time.Now(), Data: json.RawMessage(raw)}
}

func shellEvent(kind, id string) harness.Record {
	raw := `{"id":"evt_` + kind + id + `","type":"` + kind + `","location":{"directory":"/tmp/work"},"data":{"id":"` + id + `","status":"exited"}}`
	return harness.Record{Source: "events", At: time.Now(), Data: json.RawMessage(raw)}
}

// opLine is an op as "upsert id" or "remove id", for comparing sequences.
func opLines(ops []harness.Op) string {
	var out []string
	for _, op := range ops {
		id := op.ID
		if op.Row != nil {
			id = op.Row.ID
		}
		out = append(out, op.Op+" "+op.Section+" "+id)
	}
	return strings.Join(out, "\n")
}

// Seen live on 2.3.0: a shell command OpenCode ran in the background was
// running and mission control showed no background work at all.
// testdata/v2/shells.sse is what a 2.2.5 server sent for four commands
// started with POST /api/shell (folders renamed): one that ran to its end,
// one removed while running (exited as killed, then deleted), one that
// failed, all three tagged with the session, and one of no session that
// timed out.
func TestBackgroundShellsAreMissionRows(t *testing.T) {
	m := NewEvents2(harness.Env{Cwd: "/tmp/work"})
	out := run(m, recordedSSE(t, "testdata/v2/shells.sse"))
	if len(out.items) != 0 {
		t.Fatalf("shells added chat items: %+v", out.items)
	}
	const ran, removed, failed = "sh_1db8591dc001c1fvxQVvjdy1G9", "sh_1db85a58b001dHiZYKTWvoHtqi", "sh_1db85a9a3001Cm1m8o0xc3DlSh"
	bg := harness.SectionBackground
	want := strings.Join([]string{
		"upsert " + bg + " " + ran, "remove " + bg + " " + ran,
		"upsert " + bg + " " + removed, "remove " + bg + " " + removed,
		"upsert " + bg + " " + failed, "remove " + bg + " " + failed,
	}, "\n")
	if got := opLines(out.ops); got != want {
		t.Fatalf("ops:\n%s\nwant:\n%s", got, want)
	}
	row := out.ops[0].Row
	if row.Title != "printf TOGO_C_STARTED; sleep 3; printf TOGO_C_DONE" || row.State != harness.StateRunning ||
		row.Started == nil || !row.Started.Equal(time.UnixMilli(1794684326364)) {
		t.Fatalf("row %+v", row)
	}
}

// What ends a shell's row: its exit, its removal, the stream coming back
// (an exit may have been missed; the shells source lists what still runs)
// and the terminal going to another conversation.
func TestBackgroundShellRowsEnd(t *testing.T) {
	bg := harness.SectionBackground
	start := func() *Events2 {
		return NewEvents2(harness.Env{Argv: []string{"opencode", "--session", "ses_a"}})
	}

	m := start()
	_, ops := m.Map(shellCreated("sh_1", "ses_a", "running"))
	if opLines(ops) != "upsert "+bg+" sh_1" {
		t.Fatalf("created: %s", opLines(ops))
	}
	// The same event again is the same row.
	if _, ops := m.Map(shellCreated("sh_1", "ses_a", "running")); len(ops) != 0 {
		t.Fatalf("created twice: %s", opLines(ops))
	}
	m.Map(shellCreated("sh_2", "ses_a", "running"))
	_, ops = m.Map(harness.Record{Source: "events", At: time.Now(), Data: json.RawMessage(`{"type":"` + httpsrc.Reconnected + `"}`)})
	if opLines(ops) != "remove "+bg+" sh_1\nremove "+bg+" sh_2" {
		t.Fatalf("reconnect: %s", opLines(ops))
	}
	if _, ops := m.Map(shellEvent("shell.exited", "sh_1")); len(ops) != 0 {
		t.Fatalf("exit of a row already gone: %s", opLines(ops))
	}

	// Removal alone (no exit seen) ends the row too.
	m = start()
	m.Map(shellCreated("sh_1", "ses_a", "running"))
	if _, ops := m.Map(shellEvent("shell.deleted", "sh_1")); opLines(ops) != "remove "+bg+" sh_1" {
		t.Fatalf("deleted: %s", opLines(ops))
	}

	// An exit that overtook its start leaves no row behind, and a shell
	// that is not running gets none.
	m = start()
	m.Map(shellEvent("shell.exited", "sh_1"))
	if _, ops := m.Map(shellCreated("sh_1", "ses_a", "running")); len(ops) != 0 {
		t.Fatalf("created after its exit: %s", opLines(ops))
	}
	if _, ops := m.Map(shellCreated("sh_2", "ses_a", "exited")); len(ops) != 0 {
		t.Fatalf("created already exited: %s", opLines(ops))
	}

	// Another session's shell is not this chat's; a subagent's is.
	m = start()
	if _, ops := m.Map(shellCreated("sh_1", "ses_other", "running")); len(ops) != 0 {
		t.Fatalf("another session's shell: %s", opLines(ops))
	}
	child := `{"type":"session.created","data":{"sessionID":"ses_child","parentID":"ses_a","title":"Child"}}`
	m.Map(harness.Record{Source: "events", At: time.Now(), Data: json.RawMessage(child)})
	if _, ops := m.Map(shellCreated("sh_2", "ses_child", "running")); opLines(ops) != "upsert "+bg+" sh_2" {
		t.Fatalf("a subagent's shell: %s", opLines(ops))
	}

	// In the next conversation the last one's rows are gone with its
	// mission: nothing is left to remove.
	m = start()
	m.Map(shellCreated("sh_1", "ses_a", "running"))
	enq := `{"type":"session.inbox.enqueued","data":{"sessionID":"ses_b","inboxID":"msg_1","item":{"type":"user","payload":{"text":"hi"}}}}`
	m.Map(harness.Record{Source: "events", At: time.Now(), Data: json.RawMessage(enq)})
	if _, ops := m.Map(shellEvent("shell.exited", "sh_1")); len(ops) != 0 {
		t.Fatalf("exit after the conversation changed: %s", opLines(ops))
	}
}

// The background section is on, and its source lists the session's running
// shells under the ids the mapper uses, so one row stands for one shell.
// testdata/v2/shells.json is what a 2.2.5 server answered on /api/shell
// with two commands running, one of them of this session (folders renamed).
func TestShellsSourceListsTheSessionsRunningShells(t *testing.T) {
	a, err := spec.Parse(AdapterFile2())
	if err != nil {
		t.Fatal(err)
	}
	if !harness.Has(a.Capabilities, harness.CapBackground) {
		t.Fatal("no background section: the rows would be dropped")
	}
	ms := a.Mission[harness.SectionBackground]
	if ms.Source != "shells" || ms.Refresh <= 0 {
		t.Fatalf("background rows come from %+v", ms)
	}
	state := map[string]string{"harness_session": shellsSession}
	v, err := engine.JQFirst(a.Sources["shells"].Select, fixture2(t, "shells.json"), state)
	if err != nil {
		t.Fatal(err)
	}
	rows := v.([]any)
	if len(rows) != 1 {
		t.Fatalf("shells source lists %v", rows)
	}
	listed := rows[0].(map[string]any)
	if listed["id"] != "sh_1db864447001c83N6QIvDM7Tkd" || listed["title"] != "sleep 4" || listed["state"] != "running" {
		t.Fatalf("shells source row %v", listed)
	}
	m := NewEvents2(harness.Env{Argv: []string{"opencode", "--session", shellsSession}})
	_, ops := m.Map(shellCreated(listed["id"].(string), shellsSession, "running"))
	if len(ops) != 1 || ops[0].Row == nil || ops[0].Row.ID != listed["id"] {
		t.Fatalf("the mapper names the shell %+v, the source %q", ops, listed["id"])
	}
}
