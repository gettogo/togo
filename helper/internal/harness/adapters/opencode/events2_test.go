package opencode

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/engine"
	"github.com/gettogo/togo/helper/internal/harness/source/httpsrc"
	"github.com/gettogo/togo/helper/internal/harness/spec"
)

const (
	v2Session = "ses_e486a146dffek2N07HBeeD8gCa"
	v2Probe   = "ses_togoprobe0000000000000001"
)

// recorded2 replays testdata/v2/events.sse, recorded from OpenCode 2.2.5
// (a shell call behind a permission, the question tool, agent and model
// switches, an interrupt, then prompts in a second session).
func recorded2(t *testing.T) []harness.Record {
	t.Helper()
	b, err := os.ReadFile("testdata/v2/events.sse")
	if err != nil {
		t.Fatal(err)
	}
	p := httpsrc.NewParser(bytes.NewReader(b))
	var out []harness.Record
	for {
		ev, err := p.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		// Scrubbed as the sse runner does: it strips "key" fields.
		out = append(out, harness.Record{Source: "events", At: time.Now(), Data: httpsrc.Scrub(json.RawMessage(ev.Data))})
	}
	return out
}

func TestAdapter2Parses(t *testing.T) {
	a, err := spec.Parse(AdapterFile2())
	if err != nil {
		t.Fatal(err)
	}
	ep := a.Endpoint
	if ep == nil || len(ep.Server) == 0 || ep.PasswordEnv != "OPENCODE_PASSWORD" || ep.Username != "opencode" {
		t.Fatalf("endpoint %+v", ep)
	}
	if a.Harness.Range != ">=2 <3" {
		t.Fatalf("range %q", a.Harness.Range)
	}
	if _, ok := harness.LookupMapper(a.Feeds["events"].Mapper); !ok {
		t.Fatal("mapper not registered")
	}
	// Every source that reads providers or models drops what holds keys.
	for name, want := range map[string]string{"models": ".data[].settings", "providers": ".data[].settings.apiKey"} {
		red := strings.Join(a.Sources[name].Redact, " ")
		if !strings.Contains(red, want) || !strings.Contains(red, ".data[].headers") {
			t.Errorf("source %s redacts %q", name, red)
		}
	}
	// The engine compiles it: jq, templates, mapper and runners.
	var logs []string
	e := engine.New(engine.Config{Logf: func(f string, args ...any) { logs = append(logs, f) }})
	_ = e
	for _, l := range logs {
		if strings.Contains(l, "built-in adapter") {
			t.Fatalf("adapter refused: %v", logs)
		}
	}
}

func TestRecordedSession2(t *testing.T) {
	m := NewEvents2(harness.Env{SessionID: "s_oc2", Cwd: "/tmp/work"})
	got := run(m, recorded2(t)).last()

	tool := got["7C4NfP4bD3rgmwKGOjO35KqdloGPUsVV"]
	if tool.Tool == nil || tool.Tool.State != harness.StateOK || tool.Tool.Category != harness.ToolShell ||
		tool.Tool.Subject == nil || tool.Tool.Subject.Text != "echo hi > a.txt" {
		t.Fatalf("shell tool %+v", tool.Tool)
	}
	perm := got["per_1b795f8510011Fzg5ty26Nuu2X~"+v2Session]
	if perm.Question == nil || strings.Join(perm.Question.Answered["0"], ",") != "once" ||
		!strings.Contains(perm.Question.Questions[0].Prompt, "echo hi > a.txt") {
		t.Fatalf("permission %+v", perm.Question)
	}
	// A notification's Approve allows it once and Deny rejects it; a form is
	// not a permission and is offered only in the app.
	if perm.Question.Approve != "once" || perm.Question.Deny != "reject" {
		t.Fatalf("permission approve/deny %q %q", perm.Question.Approve, perm.Question.Deny)
	}
	form := got["frm_1b796ecee00128y1cvfI6jEono~"+v2Session]
	if form.Question == nil || len(form.Question.Questions) != 1 {
		t.Fatalf("form %+v", form.Question)
	}
	if form.Question.Approve != "" || form.Question.Deny != "" {
		t.Fatalf("a form marked as a permission: %q %q", form.Question.Approve, form.Question.Deny)
	}
	q := form.Question.Questions[0]
	if q.ID != "q0" || q.Prompt != "Do you prefer red or blue?" || len(q.Options) != 2 || q.Options[1].ID != "Blue" || !q.AllowText {
		t.Fatalf("question %+v", q)
	}
	if strings.Join(form.Question.Answered["q0"], ",") != "Blue" {
		t.Fatalf("answered %v", form.Question.Answered)
	}
	var answer, interrupted, user bool
	turns := 0
	for _, x := range got {
		switch {
		case x.Assistant != nil && x.Assistant.Text == "Answer: blue." && !x.Assistant.Partial:
			answer = true
		case x.Notice != nil && x.Notice.Code == harness.NoticeInterrupted:
			interrupted = true
		case x.User != nil && x.User.Text == "Reply with just: second":
			user = true
		case x.Turn != nil:
			turns++
			if x.Turn.Ended == nil || x.Turn.State == harness.StateRunning {
				t.Errorf("turn %s left open: %+v", x.ID, x.Turn)
			}
		}
	}
	if !answer || !interrupted || !user || turns < 4 {
		t.Fatalf("answer %v, interrupted %v, user %v, turns %d", answer, interrupted, user, turns)
	}
	st := m.State()
	if st["harness_session"] != v2Probe || st["working"] != "false" || st["mode"] != "plan" ||
		st["model_ref"] != modelRef("local", "qwen3.8-27b") {
		t.Fatalf("state %v", st)
	}
}

// A TUI started with --session is followed from the start.
func TestSessionFlag(t *testing.T) {
	for argv, want := range map[string]string{
		"opencode --session ses_abc --server http://x": "ses_abc",
		"opencode -s ses_abc":                          "ses_abc",
		"opencode --session=ses_abc":                   "ses_abc",
		"opencode --continue":                          "",
	} {
		if got := sessionFlag(strings.Fields(argv)); got != want {
			t.Errorf("%s: %q, want %q", argv, got, want)
		}
	}
	m := NewEvents2(harness.Env{Argv: []string{"opencode", "--session", "ses_abc"}})
	if m.State()["harness_session"] != "ses_abc" {
		t.Fatal("resumed session not followed")
	}
}

func TestActionTemplates2(t *testing.T) {
	a, err := spec.Parse(AdapterFile2())
	if err != nil {
		t.Fatal(err)
	}
	render := func(src string, ctx map[string]any) string {
		t.Helper()
		out, err := engine.Render(src, ctx)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	ans := a.Actions["answer"].Branch
	perm := map[string]any{"target": "per_abc~ses_1", "answers": map[string]any{"0": []any{"once"}}}
	if render(ans[0].When, perm) != "true" || render(ans[0].When, map[string]any{"target": "frm_abc~ses_1"}) != "" {
		t.Fatal("permission branch condition")
	}
	if got := render(ans[0].HTTP.Path, perm); got != "/api/session/ses_1/permission/per_abc/reply" {
		t.Fatalf("permission path %s", got)
	}
	if got := render(ans[0].HTTP.Body, perm); got != `{"decision": "once"}` {
		t.Fatalf("permission body %s", got)
	}
	form := map[string]any{"target": "frm_1~ses_2", "answers": map[string]any{
		"q0": []any{"Blue"}, "tags[]": []any{"a", "b"}, "ok!": []any{"true"}, "n#": []any{"3"}, "skip": []any{}}}
	if got := render(ans[1].HTTP.Path, form); got != "/api/session/ses_2/form/frm_1/reply" {
		t.Fatalf("form path %s", got)
	}
	var body struct {
		Answer map[string]any `json:"answer"`
	}
	if err := json.Unmarshal([]byte(render(ans[1].HTTP.Body, form)), &body); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(body.Answer)
	if string(b) != `{"n":3,"ok":true,"q0":"Blue","tags":["a","b"]}` {
		t.Fatalf("form answer %s", b)
	}
	if _, err := engine.Render(ans[1].HTTP.Body, map[string]any{"answers": map[string]any{`a"b`: []any{"x"}}}); err == nil {
		t.Fatal("a field key with a quote was accepted")
	}
	cwd := map[string]any{"cwd": "/tmp/a b&c"}
	if got := render(a.Sources["models"].HTTP.Path, cwd); got != "/api/model?location%5Bdirectory%5D=%2Ftmp%2Fa+b%26c" {
		t.Fatalf("models path %s", got)
	}
}

// A stream that drops while a turn ends never delivers the end: after it
// comes back the feed no longer says the turn runs, so the pane decides and
// the done is not held back for good.
func TestReconnectForgetsWorking(t *testing.T) {
	m := NewEvents2(harness.Env{Cwd: "/w"})
	for _, rec := range recorded2(t) {
		m.Map(rec)
		if m.State()["working"] == "true" {
			break
		}
	}
	if m.State()["working"] != "true" {
		t.Fatal("the recording never started a turn")
	}
	m.Map(harness.Record{Source: "events", At: time.Now(), Data: json.RawMessage(`{"type":"` + httpsrc.Reconnected + `"}`)})
	if st := m.State(); st["working"] != "false" || st["idle"] != "true" {
		t.Fatalf("after the reconnect: %v", st)
	}
}
