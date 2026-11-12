package omp

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
)

func lastPlan(o mapped) []harness.Row {
	var rows []harness.Row
	for _, op := range o.Ops {
		if op.Section == harness.SectionPlan && op.Op == harness.OpReplace {
			rows = op.Rows
		}
	}
	return rows
}

// todoCall and todoResult build the session lines omp writes for a todo
// call and its result.
func todoCall(id string, args string) harness.Record {
	raw := fmt.Sprintf(`{"type":"message","id":"a-%s","timestamp":"2026-11-12T16:00:02.000Z","message":{"role":"assistant","content":[{"type":"toolCall","id":%q,"name":"todo","arguments":%s}],"stopReason":"toolUse"}}`, id, id, args)
	return harness.Record{Source: "session", At: t0, Data: json.RawMessage(raw)}
}

func todoResult(id string, isErr bool, details string, text string) harness.Record {
	d := ""
	if details != "" {
		d = `,"details":` + details
	}
	b, _ := json.Marshal(text)
	raw := fmt.Sprintf(`{"type":"message","id":"r-%s","timestamp":"2026-11-12T16:00:03.000Z","message":{"role":"toolResult","toolCallId":%q,"toolName":"todo","content":[{"type":"text","text":%s}]%s,"isError":%v}}`, id, id, b, d, isErr)
	return harness.Record{Source: "session", At: t0, Data: json.RawMessage(raw)}
}

func states(rows []harness.Row) string {
	var parts []string
	for _, r := range rows {
		s := r.Title + "=" + r.State
		if len(r.Children) > 0 {
			var kids []string
			for _, c := range r.Children {
				kids = append(kids, c.Title+"="+c.State)
			}
			s += "{" + strings.Join(kids, ",") + "}"
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, " ")
}

func TestTodoNewFormatCallAndResult(t *testing.T) {
	m := newSession(env())
	out := run(m, records(t, "todo-init.jsonl", "session"))
	rows := lastPlan(out)
	if len(rows) != 3 || rows[0].Title != "Context" || len(rows[0].Children) != 1 || len(rows[1].Children) != 2 {
		t.Fatalf("rows = %s", states(rows))
	}
	if rows[0].State != harness.RowActive || rows[0].Children[0].State != harness.RowActive || rows[1].State != harness.RowPending {
		t.Fatalf("states = %s", states(rows))
	}
	if rows[1].Children[0].Title != "Audit relay/ (TS worker) completely: src + config" {
		t.Fatalf("child = %+v", rows[1].Children[0])
	}
	p := out.last()["plan-todo_call_1"]
	if p.Plan == nil || p.Plan.Total != 5 || p.Plan.Done != 0 || !strings.HasPrefix(p.Plan.Current, "Read README.md") {
		t.Fatalf("plan item = %+v", p.Plan)
	}
	if s := out.last()["todo_call_1"]; s.Tool == nil || s.Tool.State != harness.StateOK || s.Tool.Title != "Update the plan" {
		t.Fatalf("tool step = %+v", s.Tool)
	}
	if m.PlanUnrecognised() {
		t.Fatal("recognised format flagged")
	}
}

func TestTodoOpsFromCallsAlone(t *testing.T) {
	init := `{"op":"init","list":[{"phase":"A","items":["a1","a2"]},{"phase":"B","items":["b1"]}]}`
	cases := []struct{ name, call, want string }{
		{"init", init, "A=active{a1=active,a2=pending} B=pending{b1=pending}"},
		{"start", `{"op":"start","task":"a2"}`, "A=active{a1=pending,a2=active} B=pending{b1=pending}"},
		{"done task", `{"op":"done","task":"a1"}`, "A=active{a1=done,a2=active} B=pending{b1=pending}"},
		{"done phase", `{"op":"done","phase":"A"}`, "A=done{a1=done,a2=done} B=active{b1=active}"},
		{"done all", `{"op":"done"}`, "A=done{a1=done,a2=done} B=done{b1=done}"},
		{"drop task", `{"op":"drop","task":"b1"}`, "A=active{a1=active,a2=pending} B=done{b1=skipped}"},
		{"rm task", `{"op":"rm","task":"a2"}`, "A=active{a1=active} B=pending{b1=pending}"},
		{"rm phase", `{"op":"rm","phase":"B"}`, "A=active{a1=active,a2=pending}"},
		{"append", `{"op":"append","phase":"B","items":["b2"]}`, "A=active{a1=active,a2=pending} B=pending{b1=pending,b2=pending}"},
		{"append new phase", `{"op":"append","phase":"C","items":["c1"]}`, "A=active{a1=active,a2=pending} B=pending{b1=pending} C=pending{c1=pending}"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := newSession(env())
			recs := []harness.Record{todoCall("c1", init)}
			if c.name != "init" {
				recs = append(recs, todoCall("c2", c.call))
			}
			got := states(lastPlan(run(m, recs)))
			if got != c.want {
				t.Fatalf("got  %s\nwant %s", got, c.want)
			}
		})
	}
}

func TestTodoResultIsAuthoritative(t *testing.T) {
	m := newSession(env())
	details := `{"op":"done","phases":[{"name":"A","tasks":[{"content":"a1","status":"completed"},{"content":"a2","status":"in_progress"}]},{"name":"B","tasks":[{"content":"b1","status":"abandoned"},{"content":"b2","status":"blocked"}]}]}`
	out := run(m, []harness.Record{todoCall("c1", `{"op":"done","task":"zzz"}`), todoResult("c1", false, details, "ok")})
	got := states(lastPlan(out))
	want := "A=active{a1=done,a2=active} B=active{b1=skipped,b2=pending}"
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
	if p := out.last()["plan-c1"].Plan; p.Total != 4 || p.Done != 2 || p.Current != "a2" {
		t.Fatalf("plan = %+v", p)
	}
}

func TestTodoResultOnlyKeepsUnknownCallShape(t *testing.T) {
	m := newSession(env())
	details := `{"op":"init","phases":[{"name":"A","tasks":[{"content":"a1","status":"pending"}]}]}`
	out := run(m, []harness.Record{todoCall("c1", `{"something":"new"}`), todoResult("c1", false, details, "ok")})
	if got := states(lastPlan(out)); got != "A=pending{a1=pending}" {
		t.Fatalf("got %s", got)
	}
	if m.PlanUnrecognised() {
		t.Fatal("result gave rows, yet flagged")
	}
}

func TestTodoResultTextOnly(t *testing.T) {
	m := newSession(env())
	text := "Remaining items (1):\n  - b1 [in_progress] (B)\nOverall: 1/3 done, 1 open.\nActive phase 2/2 \"B\" (1/2).\n  A:\n    - [X] a1\n  B:\n    - [X] b0\n    - [ ] b1 (in progress)\n    - [ ] b2 (dropped)\n"
	out := run(m, []harness.Record{todoCall("c1", `{"something":"new"}`), todoResult("c1", false, "", text)})
	got := states(lastPlan(out))
	want := "A=done{a1=done} B=active{b0=done,b1=active,b2=skipped}"
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

func TestTodoErrorResultKeepsPlan(t *testing.T) {
	m := newSession(env())
	out := run(m, []harness.Record{
		todoCall("c1", `{"op":"init","list":[{"phase":"A","items":["a1"]}]}`),
		todoCall("c2", `{"op":"done","task":"nope"}`),
		todoResult("c2", true, `{"phases":[{"name":"A","tasks":[{"content":"a1","status":"in_progress"}]}]}`, `Errors: Task "nope" not found`),
	})
	if got := states(lastPlan(out)); got != "A=active{a1=active}" {
		t.Fatalf("got %s", got)
	}
	if m.PlanUnrecognised() {
		t.Fatal("a failed op is not an unrecognised format")
	}
}

func TestTodoLegacyShapesStayFlat(t *testing.T) {
	m := newSession(env())
	out := run(m, []harness.Record{todoCall("c1", `{"todos":[{"content":"x","status":"completed"},{"content":"y","status":"in_progress"}]}`)})
	if got := states(lastPlan(out)); got != "x=done y=active" {
		t.Fatalf("got %s", got)
	}
}

func TestTodoSubagentStaysOutOfPlan(t *testing.T) {
	m := newSession(env())
	blk := map[string]any{"id": "s1", "name": "todo", "arguments": map[string]any{"op": "init", "list": []any{map[string]any{"phase": "P", "items": []any{"x"}}}}}
	items, ops := m.toolCall(blk, "agent-1", "", t0)
	if len(ops) != 0 || len(items) != 1 {
		t.Fatalf("subagent todo reached the plan: %v %v", items, ops)
	}
	items, ops = m.toolResult(map[string]any{"toolCallId": "s1", "content": []any{map[string]any{"type": "text", "text": "ok"}},
		"details": map[string]any{"phases": []any{map[string]any{"name": "P", "tasks": []any{map[string]any{"content": "x", "status": "pending"}}}}}}, t0)
	if len(ops) != 0 {
		t.Fatalf("subagent todo result reached the plan: %v", ops)
	}
	for _, it := range items {
		if it.Kind == harness.KindPlan {
			t.Fatal("subagent plan item")
		}
	}
}

func TestUnrecognisedTodoStaysAToolStep(t *testing.T) {
	m := newSession(env())
	out := run(m, []harness.Record{todoCall("c1", `{"checklist":"a;b"}`), todoResult("c1", false, "", "saved")})
	if len(lastPlan(out)) != 0 {
		t.Fatal("rows from nothing")
	}
	if s := out.last()["c1"]; s.Tool == nil || s.Tool.State != harness.StateOK {
		t.Fatalf("tool step missing: %+v", s)
	}
	if !m.PlanUnrecognised() {
		t.Fatal("not flagged")
	}
}
