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
