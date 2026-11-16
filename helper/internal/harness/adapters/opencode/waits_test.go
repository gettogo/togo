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
