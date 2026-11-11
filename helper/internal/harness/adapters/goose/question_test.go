package goose

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

// feedAll maps a fixture row by row and keeps every version of each item.
func feedAll(t *testing.T, m *db, name string) map[string][]harness.Item {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(nil, 1<<20)
	all := map[string][]harness.Item{}
	for sc.Scan() {
		items, _ := m.Map(harness.Record{At: time.Unix(1, 0), Data: json.RawMessage(sc.Text())})
		for _, x := range items {
			all[x.ID] = append(all[x.ID], x)
		}
	}
	return all
}

// approvals.jsonl is a real Goose 1.57 session in approve mode (Qwen3.8
// 27B, path scrubbed): three shell calls, each asked at the CLI's
// "Goose would like to call the above tool, do you allow?" select and
// answered there with Allow, Deny and Cancel (stored as deny_once).
func TestToolConfirmations(t *testing.T) {
	m := newDB(harness.Env{Cwd: "/w/proj"})
	all := feedAll(t, m, "approvals.jsonl")
	if m.State()["mode"] != "approve" {
		t.Fatalf("mode %q", m.State()["mode"])
	}
	cases := []struct{ call, cmd, answer string }{
		{"PVygv0TwNufKruxYsFB2f07glFlKwz0R", "make", "Allow"},
		{"8N1qhbFrXvJotFToWTQD3IZXTqmT3bG8", "make clean", "Deny"},
		{"RMzbgBxViblXOV4FjVvO5NBzxmi90hng", "make all", "Deny"},
	}
	for _, c := range cases {
		v := all["confirm-"+c.call]
		if len(v) != 2 {
			t.Fatalf("%s: %d versions %+v", c.call, len(v), v)
		}
		open, done := v[0], v[1]
		if open.Kind != harness.KindQuestion || open.Question.Answered != nil {
			t.Fatalf("open %+v", open)
		}
		qb := open.Question
		q := qb.Questions[0]
		if q.Prompt != "Allow shell: "+c.cmd+"?" || q.Header != "" || q.AllowText || qb.Approve != "Allow" || qb.Deny != "Deny" {
			t.Fatalf("question %+v body %+v", q, qb)
		}
		var labels []string
		for _, o := range q.Options {
			labels = append(labels, o.ID+"="+o.Label)
		}
		if !reflect.DeepEqual(labels, []string{"Allow=Allow", "Always Allow=Always Allow", "Deny=Deny", "Cancel=Cancel"}) {
			t.Fatalf("options %v", labels)
		}
		if !reflect.DeepEqual(done.Question.Answered, map[string][]string{"0": {c.answer}}) {
			t.Fatalf("%s answered %v", c.call, done.Question.Answered)
		}
		// The gated call itself stays a tool row.
		if tl := all[c.call]; len(tl) == 0 || tl[0].Tool == nil || tl[0].Tool.Name != "shell" {
			t.Fatalf("tool %+v", tl)
		}
	}
}

// elicitation.jsonl is a real Goose 1.57 session where a stdio MCP server
// (a test server whose one tool elicits) asked for a colour (enum), bold
// (boolean) and a note (text); the CLI took them as line prompts. Goose
// stores the response before the request.
func TestElicitation(t *testing.T) {
	m := newDB(harness.Env{Cwd: "/w/proj"})
	all := feedAll(t, m, "elicitation.jsonl")
	v := all["elicit-a632bfa7-e8ed-454e-9bef-990e1430b821"]
	if len(v) != 1 {
		t.Fatalf("versions %+v", v)
	}
	qb := v[0].Question
	if len(qb.Questions) != 3 {
		t.Fatalf("questions %+v", qb.Questions)
	}
	c, b, n := qb.Questions[0], qb.Questions[1], qb.Questions[2]
	if c.ID != "colour" || c.Prompt != "Which colour should the banner use?\nColour (Banner colour)" || len(c.Options) != 3 || c.Options[1].ID != "green" || c.AllowText {
		t.Fatalf("colour %+v", c)
	}
	if b.ID != "bold" || len(b.Options) != 2 || b.Options[0].ID != "Yes" {
		t.Fatalf("bold %+v", b)
	}
	if n.ID != "note" || !n.AllowText || len(n.Options) != 0 {
		t.Fatalf("note %+v", n)
	}
	want := map[string][]string{"colour": {"green"}, "bold": {"No"}, "note": {"none"}}
	if !reflect.DeepEqual(qb.Answered, want) {
		t.Fatalf("answered %v", qb.Answered)
	}
}

// A declined form closes with no answers; a confirmation left open when the
// next prompt comes closes too.
func TestActionsClose(t *testing.T) {
	m := newDB(harness.Env{Cwd: "/w/proj"})
	row := func(id int64, role, content, meta string) []harness.Item {
		b, _ := json.Marshal(map[string]any{"id": id, "session_id": "s", "role": role, "content_json": content,
			"created_timestamp": 1, "metadata_json": meta})
		items, _ := m.Map(harness.Record{Data: b})
		return items
	}
	m.Map(harness.Record{Data: json.RawMessage(`{"id":"s","working_dir":"/w/proj","updated_at":"1"}`)})
	row(1, "user", `[{"type":"text","text":"go"}]`, `{"userVisible":true}`)
	row(2, "user", `[{"type":"actionRequired","data":{"actionType":"elicitationResponse","id":"e","user_data":{},"action":"decline"}}]`, `{"userVisible":false}`)
	items := row(3, "assistant", `[{"type":"actionRequired","data":{"actionType":"elicitation","id":"e","message":"Name?","requested_schema":{"type":"object","properties":{"name":{"type":"string"}}}}}]`, `{"userVisible":true}`)
	if len(items) == 0 || !reflect.DeepEqual(items[0].Question.Answered, map[string][]string{"name": {}}) {
		t.Fatalf("declined %+v", items)
	}
	items = row(4, "assistant", `[{"type":"actionRequired","data":{"actionType":"toolConfirmation","id":"c","toolName":"edit","arguments":{},"prompt":"risky"}}]`, `{"userVisible":true}`)
	if q := items[0].Question.Questions[0]; q.Prompt != "Allow edit?" || q.Header != "risky" || len(q.Options) != 3 {
		t.Fatalf("with reason %+v", q)
	}
	items = row(5, "user", `[{"type":"text","text":"next"}]`, `{"userVisible":true}`)
	for _, x := range items {
		if x.ID == "confirm-c" && reflect.DeepEqual(x.Question.Answered, map[string][]string{"0": {}}) {
			return
		}
	}
	t.Fatalf("not closed %+v", items)
}
