package codex

import (
	"bufio"
	"encoding/json"
	"os"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
)

// testdata/permissions-0167.jsonl is a real Codex 0.167.0 app-server
// exchange (features.request_permissions_tool on, approvalPolicy
// on-request): the request_permissions tool asked for network access, once
// granted for the turn and once denied, with the replies that worked ("in"
// from Codex, "out" the client's reply; the model answered "granted" and
// "denied"). The request is a permission question; the answer replies
// with the reply Codex took, and serverRequest/resolved closes it.
func TestPermissionsRequestFromRealCapture(t *testing.T) {
	f, err := os.Open("testdata/permissions-0167.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	type line struct {
		Dir  string          `json:"dir"`
		Case string          `json:"case"`
		Msg  json.RawMessage `json:"msg"`
	}
	decision := map[string]string{"grant": "accept", "deny": "decline"}
	m := map[string]*AppServer{}
	var params json.RawMessage
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	seen := 0
	for sc.Scan() {
		var l line
		if err := json.Unmarshal(sc.Bytes(), &l); err != nil {
			t.Fatal(err)
		}
		if m[l.Case] == nil {
			m[l.Case] = NewAppServer(harness.Env{SessionID: "s_perm"})
		}
		var probe struct {
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
			Result json.RawMessage `json:"result"`
		}
		json.Unmarshal(l.Msg, &probe)
		if l.Dir == "out" {
			got, err := answerPermissions(params, map[string]json.RawMessage{"answers": json.RawMessage(`{"decision":["` + decision[l.Case] + `"]}`)})
			if err != nil || !sameJSON(t, got, probe.Result) {
				t.Fatalf("%s: reply %s, want %s (%v)", l.Case, got, probe.Result, err)
			}
			continue
		}
		items, _ := m[l.Case].Map(harness.Record{Data: l.Msg})
		switch probe.Method {
		case "item/permissions/requestApproval":
			params = probe.Params
			var q *harness.Item
			for i := range items {
				if items[i].Kind == harness.KindQuestion {
					q = &items[i]
				}
			}
			if q == nil || q.ID != "rq-0" || q.Question.Approve != "accept" || q.Question.Deny != "decline" || q.Question.Answered != nil {
				t.Fatalf("%s: items %+v", l.Case, items)
			}
			qu := q.Question.Questions[0]
			if qu.Prompt != "Codex asks for network access" || len(qu.Options) != 3 || qu.Options[0].Detail != "fetch example.com" {
				t.Fatalf("%s: question %+v", l.Case, qu)
			}
			seen++
		case "serverRequest/resolved":
			if len(items) != 1 || items[0].Question == nil || items[0].Question.Answered == nil {
				t.Fatalf("%s: resolved %+v", l.Case, items)
			}
		}
	}
	if seen != 2 {
		t.Fatalf("requests seen %d", seen)
	}
	raw, err := answerPermissions(params, map[string]json.RawMessage{"answers": json.RawMessage(`{"decision":["acceptForSession"]}`)})
	if err != nil || !sameJSON(t, raw, json.RawMessage(`{"permissions":{"network":{"enabled":true}},"scope":"session"}`)) {
		t.Fatalf("session %s %v", raw, err)
	}
	if _, err := answerPermissions(params, map[string]json.RawMessage{"answers": json.RawMessage(`{"decision":["maybe"]}`)}); err == nil {
		t.Fatal("an unknown decision was accepted")
	}
}

func sameJSON(t *testing.T, a, b json.RawMessage) bool {
	t.Helper()
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return false
	}
	ja, _ := json.Marshal(x)
	jb, _ := json.Marshal(y)
	return string(ja) == string(jb)
}
