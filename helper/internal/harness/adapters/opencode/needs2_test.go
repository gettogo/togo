package opencode

import (
	"strconv"
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/engine"
	"github.com/gettogo/togo/helper/internal/harness/redact"
	"github.com/gettogo/togo/helper/internal/harness/spec"
)

// testdata/v2/forms.sse was recorded from OpenCode 2.2.5: forms made
// through its form API (POST /api/session/{id}/form) in the followed
// session, in a second session, and owned by "global" (as an MCP server's
// input request is) in the server's folder and in another one, then the
// replies. Folders are renamed /tmp/work and /tmp/other.
const (
	formsSession = "ses_e247b8436ffeV6g1yNuk6cwplw"
	formPlain    = "frm_1db847be3001EUHPZVCtr7SyE0~" + formsSession
	formOther    = "frm_1db847c17001w0DhAGMZ1EKd4y~ses_e247b8429ffeq25RK6fR3A6ojV"
	formGlobal   = "frm_1db847c21001wUQeOfqYcPypJT~global"
	formElse     = "frm_1db847c2a001ycdHOBW3Ood8d8~global"
)

// formsRun maps the recording as the engine would: scrubbed by the sse
// runner, then redacted. It returns every state each item went through.
func formsRun(t *testing.T) (*Events2, map[string][]harness.Item, []harness.Op) {
	t.Helper()
	e := NewEvents2(harness.Env{Cwd: "/tmp/work"})
	seen := map[string][]harness.Item{}
	var ops []harness.Op
	for _, rec := range recordedSSE(t, "testdata/v2/forms.sse") {
		rec.Data = redact.JSON(rec.Data)
		items, o := e.Map(rec)
		for _, x := range items {
			seen[x.ID] = append(seen[x.ID], x)
		}
		ops = append(ops, o...)
	}
	return e, seen, ops
}

func questionIDs(x harness.Item) string {
	var ids []string
	for _, q := range x.Question.Questions {
		ids = append(ids, q.ID)
	}
	return strings.Join(ids, " ")
}

// A form that is not the question tool's keeps its field keys, so it is
// shown and its reply names the fields.
func TestFormOfAnotherKindIsAQuestion(t *testing.T) {
	_, seen, _ := formsRun(t)
	got := seen[formPlain]
	if len(got) == 0 {
		t.Fatal("the form made through the form API was not shown")
	}
	if ids := questionIDs(got[0]); ids != "project_name region" {
		t.Fatalf("question ids %q", ids)
	}
	if a := got[0].Question.Answerable; a != nil && !*a {
		t.Fatal("the form cannot be answered")
	}
	region := got[0].Question.Questions[1]
	if len(region.Options) != 2 || region.Options[0].ID != "eu" || region.Options[0].Label != "Europe" || region.AllowText {
		t.Fatalf("region %+v", region)
	}
}

// A form owned by "global" has no session. It belongs to this chat when
// it was asked in the folder the followed conversation works in; one
// asked elsewhere, or owned by a session that is not followed, is not
// this chat's.
func TestGlobalFormOfThisFolderOnly(t *testing.T) {
	e, seen, ops := formsRun(t)
	got := seen[formGlobal]
	if len(got) != 2 {
		t.Fatalf("the global form of this folder went through %d states, want asked and answered", len(got))
	}
	if ids := questionIDs(got[0]); ids != "project_name region" {
		t.Fatalf("question ids %q", ids)
	}
	if v := got[1].Question.Answered; strings.Join(v["project_name"], ",") != "togo" || strings.Join(v["region"], ",") != "eu" {
		t.Fatalf("answered %v", v)
	}
	if len(seen[formElse]) != 0 {
		t.Fatal("a global form of another folder was shown")
	}
	if len(seen[formOther]) != 0 {
		t.Fatal("a form of a session that is not followed was shown")
	}
	added, removed := 0, 0
	for _, o := range ops {
		switch {
		case o.Section != harness.SectionNeeds:
		case o.Op == harness.OpUpsert && o.Row.ID == formGlobal:
			added++
		case o.Op == harness.OpRemove && o.ID == formGlobal:
			removed++
		}
	}
	if added != 1 || removed != 1 {
		t.Fatalf("needs row added %d, removed %d", added, removed)
	}
	if got := e.State()["location"]; got != "/tmp/work" {
		t.Fatalf("location %q", got)
	}
}

// Before any conversation exists the session's own folder decides.
func TestGlobalFormBeforeAConversation(t *testing.T) {
	created := func(dir string) harness.Record {
		return harness.Record{Data: []byte(`{"id":"evt_1","created":1794684255500,"type":"form.created","location":{"directory":"` + dir + `"},
			"data":{"form":{"id":"frm_1","sessionID":"global","title":"t","fields":[{"key":"name","type":"string"}]}}}`)}
	}
	if items, _ := NewEvents2(harness.Env{Cwd: "/tmp/work/"}).Map(created("/tmp/work")); len(items) != 1 || items[0].ID != "frm_1~global" {
		t.Fatalf("items %+v", items)
	}
	if items, _ := NewEvents2(harness.Env{Cwd: "/tmp/work"}).Map(created("/tmp/other")); len(items) != 0 {
		t.Fatalf("a form of another folder was shown: %+v", items)
	}
	if items, _ := NewEvents2(harness.Env{}).Map(created("")); len(items) != 0 {
		t.Fatalf("a form without a folder was shown: %+v", items)
	}
}

// The folder is the followed conversation's: after the chat moves to
// another conversation the old one's folder no longer decides.
func TestLocationFollowsTheConversation(t *testing.T) {
	e := NewEvents2(harness.Env{Cwd: "/tmp/work"})
	feed := func(s string) { e.Map(harness.Record{Data: []byte(s)}) }
	feed(`{"id":"evt_1","type":"session.created","location":{"directory":"/tmp/a"},"data":{"sessionID":"ses_a"}}`)
	if got := e.State()["location"]; got != "/tmp/a" {
		t.Fatalf("location %q", got)
	}
	// A prompt in another top-level session: the TUI moved there. This
	// event carries no folder (seen on 2.2.5).
	feed(`{"id":"evt_2","type":"session.inbox.enqueued","data":{"inboxID":"msg_1","sessionID":"ses_b","item":{"type":"user","payload":{"text":"hi"}}}}`)
	if got, ok := e.State()["location"]; ok {
		t.Fatalf("location %q kept from the conversation left behind", got)
	}
	feed(`{"id":"evt_3","type":"session.execution.started","location":{"directory":"/tmp/b"},"data":{"sessionID":"ses_b"}}`)
	if got := e.State()["location"]; got != "/tmp/b" {
		t.Fatalf("location %q", got)
	}
}

// The reply to a global form names the folder it was asked in: the server
// finds such a form only there (404 otherwise, seen on 2.2.5).
func TestAnswerPathOfAGlobalForm(t *testing.T) {
	a, err := spec.Parse(AdapterFile2())
	if err != nil {
		t.Fatal(err)
	}
	path := a.Actions["answer"].Branch[1].HTTP.Path
	render := func(data map[string]any) string {
		t.Helper()
		out, err := engine.Render(path, data)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	state := map[string]any{"location": "/tmp/a b"}
	if got := render(map[string]any{"target": "frm_1~global", "cwd": "/tmp/work", "state": state}); got != "/api/session/global/form/frm_1/reply?location%5Bdirectory%5D=%2Ftmp%2Fa+b" {
		t.Fatalf("global path %s", got)
	}
	if got := render(map[string]any{"target": "frm_1~global", "cwd": "/tmp/work", "state": map[string]any{}}); got != "/api/session/global/form/frm_1/reply?location%5Bdirectory%5D=%2Ftmp%2Fwork" {
		t.Fatalf("global path without a known folder %s", got)
	}
	if got := render(map[string]any{"target": "frm_1~ses_2", "cwd": "/tmp/work", "state": state}); got != "/api/session/ses_2/form/frm_1/reply" {
		t.Fatalf("session path %s", got)
	}
}

// A field whose name a reply cannot carry (redaction took it, or it is not
// a plain name) makes the form one for the terminal: it is shown whole and
// not offered for an answer that the server would refuse.
func TestFormWithAnUnusableFieldNameIsForTheTerminal(t *testing.T) {
	e := NewEvents2(harness.Env{SessionID: "s1", Cwd: "/tmp/work"})
	e.Map(harness.Record{Data: []byte(`{"id":"evt_1","type":"session.created","location":{"directory":"/tmp/work"},"data":{"sessionID":"ses_a"}}`)})
	raw := `{"id":"evt_2","created":1794684255500,"type":"form.created","location":{"directory":"/tmp/work"},
		"data":{"form":{"id":"frm_1","sessionID":"ses_a","title":"t","metadata":{"kind":"mcp-elicitation"},"fields":[
		{"key":"name","title":"Name","type":"string"},{"key":"user name","title":"User","type":"string"},{"key":"quiet","type":"string","hidden":true}]}}}`
	items, ops := e.Map(harness.Record{Data: redact.JSON([]byte(raw))})
	if len(items) != 1 || len(ops) != 1 {
		t.Fatalf("items %+v ops %+v", items, ops)
	}
	q := items[0].Question
	if q.Answerable == nil || *q.Answerable {
		t.Fatal("offered for an answer")
	}
	if len(q.Questions) != 2 || q.Questions[0].ID != "name" || q.Questions[1].Prompt != "User" || q.Questions[1].ID == "" || q.Questions[1].ID == "name" {
		t.Fatalf("questions %+v", q.Questions)
	}
	row := ops[0].Row
	if len(row.Actions) != 0 || row.Open == nil || row.Open.Kind != harness.OpenTerminal || row.Open.Ref != "s1" {
		t.Fatalf("row %+v", row)
	}
}

const formConditional = "frm_1db847bed001hTnyKeq6w1UenA~" + formsSession

// The recorded form has a required boolean with a default, a required
// choice shown only while the boolean is true, a list with a default, a
// note shown only while the list includes "mac", a number with a default
// and a note shown only while the boolean is not true.
func TestFormFieldMeaningIsKept(t *testing.T) {
	_, seen, _ := formsRun(t)
	got := seen[formConditional]
	if len(got) == 0 {
		t.Fatal("the form was not shown")
	}
	if a := got[0].Question.Answerable; a != nil && !*a {
		t.Fatal("the form cannot be answered")
	}
	type want struct {
		id       string
		required bool
		def      string
		when     string
	}
	wants := []want{
		{"notify!", true, "false", ""},
		{"channel", true, "mail", "notify!=true"},
		{"targets[]", false, "ios", ""},
		{"mac_note", false, "", "targets[]=mac"},
		{"retries#", false, "2", ""},
		{"quiet_reason", false, "", "notify!=false"},
	}
	qs := got[0].Question.Questions
	if len(qs) != len(wants) {
		t.Fatalf("%d questions: %s", len(qs), questionIDs(got[0]))
	}
	for i, w := range wants {
		q := qs[i]
		when := ""
		if q.When != nil {
			when = q.When.Question + "=" + strings.Join(q.When.Equals, "|")
		}
		if q.ID != w.id || q.Required == nil || *q.Required != w.required || strings.Join(q.Default, ",") != w.def || when != w.when {
			t.Errorf("question %d = %s default %v when %q, want %+v", i, q.ID, q.Default, when, w)
		}
	}
	// The recorded reply left the fields that did not apply unanswered.
	done := got[len(got)-1].Question.Answered
	if strings.Join(done["notify!"], ",") != "false" || len(done["channel"]) != 0 {
		t.Fatalf("answered %v", done)
	}
}

func condForm(fields string) harness.Record {
	return harness.Record{Data: []byte(`{"id":"evt_2","created":1794684255500,"type":"form.created","location":{"directory":"/tmp/work"},
		"data":{"form":{"id":"frm_1","sessionID":"ses_a","title":"t","fields":[` + fields + `]}}}`)}
}

// A condition the question model cannot say exactly (several at once, "is
// not" on free text or a list, one on a field that is not shown) is never
// approximated: the form is shown and answered in the terminal.
func TestFormConditionThatCannotBeKeptIsForTheTerminal(t *testing.T) {
	const flag, kind = `{"key":"flag","type":"boolean"}`, `{"key":"kind","type":"string","options":[{"value":"a","label":"A"},{"value":"b","label":"B"},{"value":"c","label":"C"}],"custom":false}`
	cases := []struct {
		name, fields string
		answerable   bool
		when         string
	}{
		{"is, on a choice", kind + `,{"key":"x","type":"string","when":[{"key":"kind","op":"eq","value":"b"}]}`, true, "kind=b"},
		{"is not, on a closed choice", kind + `,{"key":"x","type":"string","when":[{"key":"kind","op":"neq","value":"b"}]}`, true, "kind=a|c"},
		{"is, on a number", `{"key":"n","type":"number"},{"key":"x","type":"string","when":[{"key":"n","op":"eq","value":2.50}]}`, true, "n#=2.5"},
		{"no conditions", flag + `,{"key":"x","type":"string","when":[]}`, true, ""},
		{"two conditions", flag + `,` + kind + `,{"key":"x","type":"string","when":[{"key":"flag","op":"eq","value":true},{"key":"kind","op":"eq","value":"a"}]}`, false, ""},
		{"is not, on free text", `{"key":"name","type":"string"},{"key":"x","type":"string","when":[{"key":"name","op":"neq","value":"a"}]}`, false, ""},
		{"is not, on a list", `{"key":"tags","type":"multiselect","options":[{"value":"a","label":"A"}]},{"key":"x","type":"string","when":[{"key":"tags","op":"neq","value":"a"}]}`, false, ""},
		{"on a hidden field", `{"key":"flag","type":"boolean","hidden":true},{"key":"x","type":"string","when":[{"key":"flag","op":"eq","value":true}]}`, false, ""},
		{"on a later field", `{"key":"x","type":"string","when":[{"key":"flag","op":"eq","value":true}]},` + flag, false, ""},
		{"unknown comparison", flag + `,{"key":"x","type":"string","when":[{"key":"flag","op":"gt","value":true}]}`, false, ""},
	}
	for _, c := range cases {
		e := NewEvents2(harness.Env{Cwd: "/tmp/work"})
		e.Map(harness.Record{Data: []byte(`{"id":"evt_1","type":"session.created","location":{"directory":"/tmp/work"},"data":{"sessionID":"ses_a"}}`)})
		items, _ := e.Map(condForm(c.fields))
		if len(items) != 1 {
			t.Fatalf("%s: items %+v", c.name, items)
		}
		body := items[0].Question
		if got := body.Answerable == nil || *body.Answerable; got != c.answerable {
			t.Errorf("%s: answerable %v", c.name, got)
		}
		x := body.Questions[len(body.Questions)-1]
		if c.name == "on a later field" {
			x = body.Questions[0]
		}
		when := ""
		if x.When != nil {
			when = x.When.Question + "=" + strings.Join(x.When.Equals, "|")
		}
		if when != c.when {
			t.Errorf("%s: when %q, want %q", c.name, when, c.when)
		}
	}
}

// Defaults are bounded and typed; a secret field's default is not shown.
func TestFormDefaults(t *testing.T) {
	long := strings.Repeat("x", 5000)
	e := NewEvents2(harness.Env{Cwd: "/tmp/work"})
	e.Map(harness.Record{Data: []byte(`{"id":"evt_1","type":"session.created","location":{"directory":"/tmp/work"},"data":{"sessionID":"ses_a"}}`)})
	items, _ := e.Map(condForm(`{"key":"a","type":"string","default":"` + long + `"},{"key":"b","type":"boolean","default":true},
		{"key":"c","type":"number","default":1.50},{"key":"d","type":"string","default":7},{"key":"api_token","type":"string","default":"abc"},
		{"key":"e","type":"multiselect","options":[{"value":"x","label":"X"}],"default":["x","y"]},{"key":"f","type":"string","default":""}`))
	if len(items) != 1 {
		t.Fatalf("items %+v", items)
	}
	want := []string{"", "true", "1.5", "", "", "x,y", ""}
	for i, q := range items[0].Question.Questions {
		if got := strings.Join(q.Default, ","); got != want[i] {
			t.Errorf("%s default %q, want %q", q.ID, got, want[i])
		}
	}
}

// Recorded forms of 17, 64 and 65 required fields. Up to 64 are shown and
// answered whole; a longer form is shown as far as the app takes it and
// answered in the terminal, never cut and offered for an answer that
// OpenCode would refuse ("Missing required form field").
func TestFormFieldLimit(t *testing.T) {
	_, seen, ops := formsRun(t)
	for _, c := range []struct {
		id         string
		questions  int
		answerable bool
	}{
		{"frm_1db847bf8001ppfcLBvsjTQGa8~" + formsSession, 17, true},
		{"frm_1db847c03001CKM0gMXqcjko6A~" + formsSession, 64, true},
		{"frm_1db847c0d001xZZuE1Ay00mVFp~" + formsSession, 64, false},
	} {
		got := seen[c.id]
		if len(got) == 0 {
			t.Fatalf("%s was not shown", c.id)
		}
		body := got[0].Question
		if len(body.Questions) != c.questions || body.Questions[c.questions-1].ID != "f"+strconv.Itoa(c.questions-1) {
			t.Errorf("%s: %d questions, last %s", c.id, len(body.Questions), body.Questions[len(body.Questions)-1].ID)
		}
		if answerable := body.Answerable == nil || *body.Answerable; answerable != c.answerable {
			t.Errorf("%s: answerable %v", c.id, answerable)
		}
		for _, o := range ops {
			if o.Op == harness.OpUpsert && o.Row != nil && o.Row.ID == c.id && (len(o.Row.Actions) == 1) != c.answerable {
				t.Errorf("%s: row actions %+v", c.id, o.Row.Actions)
			}
		}
	}
}

// Hidden fields are not shown and do not count towards the limit.
func TestFormFieldLimitCountsShownFields(t *testing.T) {
	var fields []string
	for i := 0; i < 64; i++ {
		fields = append(fields, `{"key":"f`+strconv.Itoa(i)+`","type":"string"}`)
	}
	fields = append(fields, `{"key":"quiet","type":"string","hidden":true}`)
	e := NewEvents2(harness.Env{Cwd: "/tmp/work"})
	e.Map(harness.Record{Data: []byte(`{"id":"evt_1","type":"session.created","location":{"directory":"/tmp/work"},"data":{"sessionID":"ses_a"}}`)})
	items, _ := e.Map(condForm(strings.Join(fields, ",")))
	if len(items) != 1 || len(items[0].Question.Questions) != 64 || items[0].Question.Answerable != nil {
		t.Fatalf("items %+v", items)
	}
}
