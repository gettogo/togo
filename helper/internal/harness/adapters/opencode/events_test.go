package opencode

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/engine"
	"github.com/gettogo/togo/helper/internal/harness/source/httpsrc"
	"github.com/gettogo/togo/helper/internal/harness/spec"
)

var update = flag.Bool("update", false, "rewrite golden files")

const recordedSession = "ses_e4c1e2d74ffe3rp8SaoRiithMv"

var t0 = time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)

// recorded replays testdata/events.sse through the sse parser, 50 ms apart.
func recorded(t *testing.T) []harness.Record {
	t.Helper()
	b, err := os.ReadFile("testdata/events.sse")
	if err != nil {
		t.Fatal(err)
	}
	p := httpsrc.NewParser(bytes.NewReader(b))
	var out []harness.Record
	for i := 0; ; i++ {
		ev, err := p.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, harness.Record{Source: "events", At: t0.Add(time.Duration(i) * 50 * time.Millisecond), Data: json.RawMessage(ev.Data)})
	}
	return out
}

type mapped struct {
	items []harness.Item
	ops   []harness.Op
}

func run(m harness.Mapper, recs []harness.Record) mapped {
	var out mapped
	for _, r := range recs {
		items, ops := m.Map(r)
		out.items = append(out.items, items...)
		out.ops = append(out.ops, ops...)
	}
	return out
}

func (m mapped) last() map[string]harness.Item {
	out := map[string]harness.Item{}
	for _, x := range m.items {
		out[x.ID] = x
	}
	return out
}

func TestAdapterFileParses(t *testing.T) {
	a, err := spec.Parse(AdapterFile())
	if err != nil {
		t.Fatal(err)
	}
	if a.Endpoint == nil || a.Endpoint.Kind != spec.EndpointHTTP {
		t.Fatalf("endpoint %+v", a.Endpoint)
	}
	if _, ok := harness.LookupMapper(a.Feeds["events"].Mapper); !ok {
		t.Fatal("mapper not registered")
	}
	for _, k := range []string{spec.KindHTTP, spec.KindSSE} {
		if _, ok := harness.LookupRunner(k); !ok {
			t.Fatalf("runner %s not registered", k)
		}
	}
	// Both sources that read providers carry the key paths, and the
	// providers screen's secret field reads only the keySet flag.
	for _, name := range []string{"models", "providers"} {
		red := strings.Join(a.Sources[name].Redact, " ")
		for _, want := range []string{".providers[].key", ".providers[].options.apiKey", ".providers[].options.headers"} {
			if !strings.Contains(red, want) {
				t.Errorf("source %s does not redact %s", name, want)
			}
		}
	}
	f := a.Screens["providers"].Fields[0]
	if f.Kind != "secret" || f.Value != ".keySet" {
		t.Fatalf("providers field %+v", f)
	}
}

func TestRecordedSession(t *testing.T) {
	m := NewEvents(harness.Env{SessionID: "s_oc", Cwd: "/Users/me/proj"})
	out := run(m, recorded(t))
	st := m.State()
	if st["harness_session"] != recordedSession || st["model"] != "local/qwen3.8-27b" || st["mode"] != "build" || st["working"] != "false" {
		t.Fatalf("state %v", st)
	}
	if st["model_ref"] != `{"id":"qwen3.8-27b","providerID":"local"}` {
		t.Fatalf("model_ref %s", st["model_ref"])
	}
	items := out.last()
	var users, turns []harness.Item
	tools := map[string]harness.Item{}
	for _, x := range items {
		switch x.Kind {
		case harness.KindUser:
			users = append(users, x)
		case harness.KindTurn:
			turns = append(turns, x)
		case harness.KindTool:
			tools[x.Tool.Name+":"+x.Tool.Title] = x
		}
	}
	if len(users) != 4 || len(turns) != 4 {
		t.Fatalf("%d users, %d turns", len(users), len(turns))
	}
	for _, tu := range turns {
		if tu.Turn.State != harness.StateOK || tu.Turn.Ended == nil {
			t.Fatalf("turn not closed: %+v", tu.Turn)
		}
	}
	bash := tools["bash:echo togo-spike"].Tool
	if bash == nil || bash.State != harness.StateOK || bash.Category != harness.ToolShell || bash.Output != "togo-spike\n" || *bash.ExitCode != 0 {
		t.Fatalf("bash %+v", bash)
	}
	if td := tools["todowrite:2 todos"].Tool; td == nil || td.State != harness.StateOK {
		t.Fatalf("todowrite %+v", tools)
	}
	read := items["KXscQAlidTyuoCUB0mF0fKv641KhETeO"].Tool
	if read == nil || read.State != harness.StateDenied || read.Title != "Read hosts" {
		t.Fatalf("read %+v", read)
	}
	var waited, partial bool
	for _, x := range out.items {
		if x.ID == "KXscQAlidTyuoCUB0mF0fKv641KhETeO" && x.Tool.State == harness.StateWaiting {
			waited = true
		}
		if (x.Assistant != nil && x.Assistant.Partial) || (x.Thinking != nil && x.Thinking.Partial) {
			partial = true
		}
	}
	if !waited || !partial {
		t.Fatalf("waited=%v partial=%v", waited, partial)
	}
	q := items["que_1b3e311f6001e8PS8BvJqH47pF"].Question
	if q == nil || q.Questions[0].Options[1].ID != "blue" || !q.Questions[0].AllowText || len(q.Answered["0"]) != 1 || q.Answered["0"][0] != "blue" {
		t.Fatalf("question %+v", q)
	}
	perm := items["per_1b3e38b52001cSrKyzwMjuioNI"].Question
	if perm == nil || perm.Questions[0].Prompt != "Allow external directory: /etc/*" || perm.Answered["0"][0] != "reject" {
		t.Fatalf("permission %+v", perm)
	}
	ops := map[string]int{}
	for _, op := range out.ops {
		ops[op.Op+":"+op.Section]++
	}
	if ops["upsert:needs"] != 2 || ops["remove:needs"] != 2 || ops["replace:plan"] != 2 {
		t.Fatalf("ops %v", ops)
	}
	var plan *harness.PlanBody
	for _, x := range items {
		if x.Kind == harness.KindPlan {
			plan = x.Plan
		}
	}
	if plan == nil || plan.Total != 2 || plan.Done != 2 {
		t.Fatalf("plan %+v", plan)
	}
	golden(t, "events.golden.json", map[string]any{"items": out.items, "ops": out.ops})
}

func golden(t *testing.T, name string, v any) {
	t.Helper()
	got, _ := json.MarshalIndent(v, "", " ")
	path := filepath.Join("testdata", name)
	if *update {
		os.WriteFile(path, append(got, '\n'), 0o644)
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run with -update)", err)
	}
	if !bytes.Equal(bytes.TrimSpace(got), bytes.TrimSpace(want)) {
		t.Fatalf("%s differs; run go test -update and review the diff", name)
	}
}

// Events of sessions the TUI is not showing are ignored; a child session
// of the main one becomes an agent with its own conversation.
func TestSessionsFollowed(t *testing.T) {
	m := NewEvents(harness.Env{})
	recs := []string{
		`{"type":"session.updated","properties":{"info":{"id":"ses_main","agent":"build","model":{"id":"m","providerID":"p"}}}}`,
		`{"type":"session.updated","properties":{"info":{"id":"ses_other"}}}`,
		`{"type":"message.updated","properties":{"info":{"id":"msg_o","role":"assistant","sessionID":"ses_other","parentID":"msg_x"}}}`,
		`{"type":"session.created","properties":{"info":{"id":"ses_child","parentID":"ses_main","title":"Explore the tests","agent":"general","time":{"created":1794019000000}}}}`,
		`{"type":"message.updated","properties":{"info":{"id":"msg_c","role":"assistant","sessionID":"ses_child","parentID":"msg_cu"}}}`,
		`{"type":"message.part.updated","properties":{"part":{"id":"prt_c","messageID":"msg_c","sessionID":"ses_child","type":"text","text":"found 3 tests","time":{"start":1,"end":2}}}}`,
		`{"type":"session.idle","properties":{"sessionID":"ses_child"}}`,
		`{"type":"tui.session.select","properties":{"sessionID":"ses_other"}}`,
	}
	var rs []harness.Record
	for i, r := range recs {
		rs = append(rs, harness.Record{At: t0.Add(time.Duration(i) * time.Second), Data: json.RawMessage(r)})
	}
	out := run(m, rs)
	items := out.last()
	agent := items["agent-ses_child"]
	if agent.Agent == nil || agent.Agent.Name != "Explore the tests" || agent.Agent.State != harness.StateOK || agent.Agent.Ended == nil {
		t.Fatalf("agent %+v", agent)
	}
	if !agent.Agent.Started.Equal(time.UnixMilli(1794019000000)) {
		t.Fatalf("agent start %v", agent.Agent.Started)
	}
	if c := items["prt_c"]; c.Parent != "agent-ses_child" || c.Assistant.Text != "found 3 tests" {
		t.Fatalf("child text %+v", c)
	}
	if _, ok := items["msg_o"]; ok {
		t.Fatal("other session drawn")
	}
	if m.State()["harness_session"] != "ses_other" {
		t.Fatalf("select did not switch: %v", m.State())
	}
}

// The adapter's answer and model templates render to the bodies OpenCode
// accepts. json stands in for the engine's filter of the same name.
func TestActionTemplates(t *testing.T) {
	a, err := spec.Parse(AdapterFile())
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
	perm := map[string]any{"target": "per_abc", "answers": map[string]any{"0": []any{"once"}}}
	if render(ans[0].When, perm) != "true" || render(ans[0].When, map[string]any{"target": "que_abc"}) != "" {
		t.Fatal("permission branch condition")
	}
	if got := render(ans[0].HTTP.Body, perm); got != `{"reply": "once"}` {
		t.Fatalf("permission body %s", got)
	}
	qs := map[string]any{"target": "que_1", "answers": map[string]any{"0": []any{"blue"}, "1": []any{"a", "b"}}}
	if got := render(ans[1].HTTP.Body, qs); got != `{"answers": [["blue"], ["a","b"]]}` {
		t.Fatalf("question body %s", got)
	}
	ref := modelRef("local", "qwen3.8-27b")
	got := render(a.Actions["set_model"].HTTP.Body, map[string]any{"id": ref})
	var body struct {
		Model struct{ ID, ProviderID string }
	}
	if err := json.Unmarshal([]byte(got), &body); err != nil || body.Model.ID != "qwen3.8-27b" || body.Model.ProviderID != "local" {
		t.Fatalf("model body %s %v", got, err)
	}
	if _, err := engine.Render(a.Actions["set_model"].HTTP.Body, map[string]any{"id": `{"id":"x"},"extra":{"y":1}`}); err == nil {
		t.Fatal("a model id added a field to the body")
	}
}

// The providers screen's source never hands a key to anything after the
// runner, whatever the engine does next.
func TestProvidersCarryNoKey(t *testing.T) {
	raw, err := os.ReadFile("testdata/providers.json")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(raw) }))
	defer srv.Close()
	a, _ := spec.Parse(AdapterFile())
	block, _ := json.Marshal(a.Sources["providers"].HTTP)
	out, err := httpsrc.HTTP{}.Fetch(context.Background(), harness.SourceSpec{Name: "providers", Kind: spec.KindHTTP, Raw: block}, harness.Env{Endpoint: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{"sk-", "secret-value", "local-secret", "Bearer"} {
		if strings.Contains(string(out), leak) {
			t.Fatalf("%q reached the screen pipeline: %s", leak, out)
		}
	}
	var got struct {
		Providers []struct {
			ID      string         `json:"id"`
			Set     []string       `json:"_set"`
			Options map[string]any `json:"options"`
		} `json:"providers"`
	}
	json.Unmarshal(out, &got)
	set := map[string]bool{}
	for _, p := range got.Providers {
		opt, _ := p.Options["_set"].([]any)
		set[p.ID] = len(p.Set) > 0 || len(opt) > 0
	}
	if !set["moonshotai"] || !set["togofake"] || !set["local"] {
		t.Fatalf("providers with a key must say so: %v", set)
	}
}

// After the event stream reconnects, open questions are closed: a reply
// made at the desk meanwhile was missed.
func TestReconnectClosesOpenQuestions(t *testing.T) {
	m := NewEvents(harness.Env{SessionID: "s_oc"})
	m.Map(harness.Record{Data: json.RawMessage(`{"type":"tui.session.select","properties":{"sessionID":"ses_1"}}`)})
	items, ops := m.Map(harness.Record{Data: json.RawMessage(`{"type":"question.asked","properties":{"id":"que_1","sessionID":"ses_1","questions":[{"question":"Pick","header":"Pick","options":[{"label":"A","description":""}]}]}}`)})
	if len(items) != 1 || len(ops) != 1 {
		t.Fatalf("asked %+v %+v", items, ops)
	}
	items, ops = m.Map(harness.Record{Data: json.RawMessage(`{"type":"stream.reconnected"}`)})
	if len(items) != 1 || items[0].Question.Answered == nil || len(ops) != 1 || ops[0].Op != harness.OpRemove || ops[0].ID != "que_1" {
		t.Fatalf("reconnect %+v %+v", items, ops)
	}
}
