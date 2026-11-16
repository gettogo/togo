package opencode

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
)

// indexOf finds the first recorded event of a type whose data holds sub.
func indexOf(t *testing.T, recs []harness.Record, typ, sub string) int {
	t.Helper()
	for i, r := range recs {
		var ev struct {
			Type string `json:"type"`
		}
		json.Unmarshal(r.Data, &ev)
		if ev.Type == typ && strings.Contains(string(r.Data), sub) {
			return i
		}
	}
	t.Fatalf("no %s event holding %q", typ, sub)
	return -1
}

// A tool that waited for an approval runs again once it is answered: its
// later progress must not show it as waiting.
func TestToolRunsAfterItsApproval2(t *testing.T) {
	recs := recorded2(t)
	end := indexOf(t, recs, "session.tool.progress", "7C4NfP4bD3rgmwKGOjO35KqdloGPUsVV")
	m := NewEvents2(harness.Env{SessionID: "s_oc2", Cwd: "/tmp/work"})
	got := run(m, recs[:end+1]).last()["7C4NfP4bD3rgmwKGOjO35KqdloGPUsVV"]
	if got.Tool == nil || got.Tool.State != harness.StateRunning {
		t.Fatalf("tool after its approval %+v", got.Tool)
	}
}

func TestToolRunsAfterItsApproval(t *testing.T) {
	recs := recorded(t)
	asked := indexOf(t, recs, "permission.asked", "KXscQAlidTyuoCUB0mF0fKv641KhETeO")
	running := indexOf(t, recs[:asked], "message.part.updated", `"status": "running", "input": {"filePath": "/etc/hosts"}`)
	replied := indexOf(t, recs, "permission.replied", "per_1b3e38b52001cSrKyzwMjuioNI")
	m := NewEvents(harness.Env{SessionID: "s_oc", Cwd: "/Users/me/proj"})
	feed := append([]harness.Record{}, recs[:replied+1]...)
	feed = append(feed, recs[running]) // the tool reports progress after the reply
	got := run(m, feed).last()["KXscQAlidTyuoCUB0mF0fKv641KhETeO"]
	if got.Tool == nil || got.Tool.State != harness.StateRunning {
		t.Fatalf("tool after its approval %+v", got.Tool)
	}
}

// without drops the records of one type, as when the agent never sends the
// end of a stream the user interrupted.
func without(recs []harness.Record, types ...string) []harness.Record {
	var out []harness.Record
next:
	for _, r := range recs {
		var ev struct {
			Type string `json:"type"`
		}
		json.Unmarshal(r.Data, &ev)
		for _, ty := range types {
			if ev.Type == ty {
				continue next
			}
		}
		out = append(out, r)
	}
	return out
}

// Interrupted text keeps its last words and stops streaming.
func TestInterruptFlushesStreamedText2(t *testing.T) {
	recs := without(recorded2(t), "session.text.ended", "session.reasoning.ended")
	end := indexOf(t, recs, "session.execution.interrupted", "ses_e486a146dffek2N07HBeeD8gCa")
	m := NewEvents2(harness.Env{SessionID: "s_oc2", Cwd: "/tmp/work"})
	got := run(m, recs[:end+1]).last()
	var found bool
	for id, x := range got {
		if strings.HasPrefix(id, "msg_1b797c415001RPsbgZYZ25vca1-t") && x.Assistant != nil {
			found = true
			if x.Assistant.Partial || !strings.HasSuffix(x.Assistant.Text, "1\n12\n13\n14\n15\n16\n17\n18\n19\n20\n2") {
				t.Fatalf("interrupted text %+v", x.Assistant)
			}
		}
	}
	if !found {
		t.Fatal("the interrupted text was never shown")
	}
}

func TestIdleFlushesStreamedText(t *testing.T) {
	recs := recorded(t)
	start := indexOf(t, recs, "message.part.delta", "prt_1b3e21b50001enXgCLCa7we0L4")
	m := NewEvents(harness.Env{SessionID: "s_oc", Cwd: "/Users/me/proj"})
	feed := append([]harness.Record{}, recs[:start+8]...)
	feed = append(feed, harness.Record{Data: json.RawMessage(`{"type":"session.idle","properties":{"sessionID":"` + recordedSession + `"}}`)})
	got := run(m, feed).last()["prt_1b3e21b50001enXgCLCa7we0L4"]
	if got.Assistant == nil && got.Thinking == nil {
		t.Fatalf("no text item %+v", got)
	}
	body := got.Assistant
	if body == nil {
		body = got.Thinking
	}
	if body.Partial || !strings.HasSuffix(body.Text, " tod") {
		t.Fatalf("text after idle %+v", body)
	}
}

func reconnected() harness.Record {
	return harness.Record{Data: json.RawMessage(`{"type":"stream.reconnected"}`)}
}

// A stream that drops and comes back in the middle of a turn: the turn is
// not known to run until the stream says so, and its next streamed text or
// tool update does.
func TestReconnectMidTurnWorksAgain(t *testing.T) {
	recs := recorded(t)
	at := indexOf(t, recs, "message.part.delta", "prt_1b3e21b50001enXgCLCa7we0L4")
	m := NewEvents(harness.Env{SessionID: "s_oc", Cwd: "/Users/me/proj"})
	run(m, recs[:at+4])
	if m.State()["working"] != "true" {
		t.Fatalf("not working mid-turn: %v", m.State())
	}
	m.Map(reconnected())
	if m.State()["working"] != "false" {
		t.Fatalf("working after reconnect: %v", m.State())
	}
	run(m, recs[at+4:at+6])
	if m.State()["working"] != "true" || m.State()["idle"] != "false" {
		t.Fatalf("not working after the turn went on: %v", m.State())
	}
	// A turn that ended while the stream was down stays ended.
	idle := indexOf(t, recs, "session.idle", recordedSession)
	m.Map(recs[idle])
	if m.State()["working"] != "false" {
		t.Fatalf("working after idle: %v", m.State())
	}
}

func TestReconnectMidTurnWorksAgain2(t *testing.T) {
	recs := recorded2(t)
	at := indexOf(t, recs, "session.text.delta", "1\\n2\\n3")
	m := NewEvents2(harness.Env{SessionID: "s_oc2", Cwd: "/tmp/work"})
	run(m, recs[:at+1])
	if m.State()["working"] != "true" {
		t.Fatalf("not working mid-turn: %v", m.State())
	}
	m.Map(reconnected())
	if m.State()["working"] != "false" {
		t.Fatalf("working after reconnect: %v", m.State())
	}
	run(m, recs[at+1:at+3])
	if m.State()["working"] != "true" || m.State()["idle"] != "false" {
		t.Fatalf("not working after the turn went on: %v", m.State())
	}
	run(m, recs[at+3:at+10]) // text ended ... execution.interrupted
	if m.State()["working"] != "false" {
		t.Fatalf("working after the turn ended: %v", m.State())
	}
}

// Switching to another session while a turn runs does not leave the old
// turn's working state on the new one.
func TestSwitchingSessionResetsWorking(t *testing.T) {
	recs := recorded(t)
	at := indexOf(t, recs, "message.part.delta", "prt_1b3e21b50001enXgCLCa7we0L4")
	m := NewEvents(harness.Env{SessionID: "s_oc", Cwd: "/Users/me/proj"})
	run(m, recs[:at+4])
	if m.State()["working"] != "true" || m.State()["turn"] == "" {
		t.Fatalf("not working mid-turn: %v", m.State())
	}
	m.Map(harness.Record{Data: json.RawMessage(`{"type":"tui.session.select","properties":{"sessionID":"ses_other"}}`)})
	st := m.State()
	if st["working"] != "false" || st["idle"] != "true" || st["turn"] != "" || st["harness_session"] != "ses_other" {
		t.Fatalf("state after the switch %v", st)
	}
}

func TestSwitchingSessionResetsWorking2(t *testing.T) {
	recs := recorded2(t)
	at := indexOf(t, recs, "session.text.delta", "1\\n2\\n3")
	m := NewEvents2(harness.Env{SessionID: "s_oc2", Cwd: "/tmp/work"})
	run(m, recs[:at+1])
	if m.State()["working"] != "true" {
		t.Fatalf("not working mid-turn: %v", m.State())
	}
	m.Map(harness.Record{Data: json.RawMessage(`{"type":"session.inbox.enqueued","data":{"inboxID":"msg_x","sessionID":"ses_other","item":{"type":"user","payload":{"text":"hi"}}}}`)})
	st := m.State()
	if st["working"] != "false" || st["turn"] != "" || st["harness_session"] != "ses_other" {
		t.Fatalf("state after the switch %v", st)
	}
}

// A mapper that starts after a turn ended gets the user message's later
// update (it carries a summary): that is not a prompt, so no turn opens.
func TestLateUserMessageUpdateOpensNoTurn(t *testing.T) {
	recs := recorded(t)
	late := indexOf(t, recs, "message.updated", `"summary": {"diffs": []}, "id": "msg_1b3e387240019POJUd4yMPPXV0"`)
	m := NewEvents(harness.Env{SessionID: "s_oc", Cwd: "/Users/me/proj"})
	m.Map(harness.Record{Data: json.RawMessage(`{"type":"tui.session.select","properties":{"sessionID":"` + recordedSession + `"}}`)})
	items, _ := m.Map(recs[late])
	if len(items) != 0 || m.State()["working"] != "false" {
		t.Fatalf("late update opened a turn: %+v %v", items, m.State())
	}
}

// Closed turns are forgotten, yet a late update of their prompt still does
// not open one.
func TestClosedTurnsAreForgotten(t *testing.T) {
	recs := recorded(t)
	m := NewEvents(harness.Env{SessionID: "s_oc", Cwd: "/Users/me/proj"})
	run(m, recs)
	open := 0
	for _, tu := range m.turns {
		if tu.state != "" {
			open++
		}
	}
	if len(m.turns) != open {
		t.Fatalf("%d turns kept, %d open", len(m.turns), open)
	}
	first := indexOf(t, recs, "message.updated", `"id": "msg_1b3e1efe3001r05Q37r0vH1Go0"`)
	if items, _ := m.Map(recs[first]); len(items) != 0 {
		t.Fatalf("a closed prompt opened a turn: %+v", items)
	}
}
