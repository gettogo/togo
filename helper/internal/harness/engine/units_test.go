package engine

import (
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

func TestTemplateFilters(t *testing.T) {
	Now = func() time.Time { return time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC) }
	defer func() { Now = time.Now }()
	data := map[string]any{
		"updated": "2026-11-03T10:00:00Z", "n": 200000.0, "big": 1500000.0, "name": "", "preview": "a long preview text",
		"branch": "main", "obj": map[string]any{"a": 1.0}, "state": map[string]any{"model": "opus"},
	}
	cases := map[string]string{
		"{{.updated | ago}}":                         "2h ago",
		"{{.n | tokens}}":                            "200K",
		"{{.big | tokens}}":                          "1.5M",
		"{{.name | default .preview | truncate 10}}": "a long pr…",
		"{{.preview | truncate 100}}":                "a long preview text",
		"{{.obj | json}}":                            `{"a":1}`,
		"{{.missing}}":                               "",
		"{{.state.model}}":                           "opus",
		"/model {{.state.model}}{{if .branch}} · {{.branch}}{{end}}": "/model opus · main",
		"plain": "plain",
	}
	for src, want := range cases {
		got, err := Render(src, data)
		if err != nil || got != want {
			t.Errorf("Render(%q) = %q, %v; want %q", src, got, err, want)
		}
	}
	if _, err := Render("{{.x", data); err == nil {
		t.Error("bad template rendered")
	}
	if Truthy("false") || Truthy("") || !Truthy("true") || !Truthy("x") {
		t.Error("Truthy")
	}
	raw, err := RenderJSON(json.RawMessage(`{"argv":["claude","{{.state.model}}"],"n":1}`), data)
	if err != nil || string(raw) != `{"argv":["claude","opus"],"n":1}` {
		t.Errorf("RenderJSON = %s %v", raw, err)
	}
}

func TestJQSelectWithState(t *testing.T) {
	input := []any{map[string]any{"id": "a", "hidden": true}, map[string]any{"id": "b", "hidden": false}}
	out, err := JQ(`[.[] | select(.hidden | not) | .id + $state.suffix]`, input, map[string]string{"suffix": "!"})
	if err != nil || len(out) != 1 {
		t.Fatal(out, err)
	}
	if b, _ := json.Marshal(out[0]); string(b) != `["b!"]` {
		t.Fatalf("select = %s", b)
	}
	if v, err := MapValue("$state.model", input, map[string]string{"model": "opus"}); err != nil || v != "opus" {
		t.Fatal(v, err)
	}
	if v, _ := MapValue(`{{.id}} ({{.state.x}})`, map[string]any{"id": "a"}, map[string]string{"x": "y"}); v != "a (y)" {
		t.Fatal(v)
	}
	if _, err := JQ(".[", nil, nil); err == nil {
		t.Fatal("bad jq compiled")
	}
	if _, err := JQ("error(\"boom\")", nil, nil); err == nil {
		t.Fatal("jq error ignored")
	}
}

func item(id string) harness.Item { return harness.Item{ID: id, Kind: harness.KindUser} }

func TestLogReplaceSeqRingAndPaging(t *testing.T) {
	l := &Log{NewEpoch: func() string { return "e1" }, Max: 4}
	got := l.Append("s", []harness.Item{item("a"), item("b"), item("c")})
	if got[2].Seq != 3 || l.Epoch("s") != "e1" {
		t.Fatalf("append = %+v", got)
	}
	l.Append("s", []harness.Item{item("a")}) // replaced, moves to the end
	items, cursor, more := l.Since("s", 0, 10)
	if cursor != 4 || more || len(items) != 3 || items[2].ID != "a" || items[2].Seq != 4 {
		t.Fatalf("since = %+v %d %v", items, cursor, more)
	}
	l.Append("s", []harness.Item{item("d"), item("e")}) // ring of 4 drops b
	items, _, _ = l.Since("s", 0, 10)
	if len(items) != 4 || items[0].ID != "c" {
		t.Fatalf("ring = %+v", items)
	}
	items, _, more = l.Since("s", 0, 2)
	if len(items) != 2 || items[0].ID != "d" || !more {
		t.Fatalf("since limit = %+v %v", items, more)
	}
	items, more = l.Before("s", 5, 1)
	if len(items) != 1 || items[0].ID != "a" || !more {
		t.Fatalf("before = %+v %v", items, more)
	}
	items, more = l.Before("s", 4, 10)
	if len(items) != 1 || items[0].ID != "c" || more {
		t.Fatalf("before 4 = %+v %v", items, more)
	}
	if it, ok := l.Get("s", "e"); !ok || it.Seq != 6 {
		t.Fatalf("get = %+v", it)
	}
	l.Forget("s")
	if items, cursor, _ := l.Since("s", 0, 10); len(items) != 0 || cursor != 0 {
		t.Fatal("forget kept items")
	}
}

func TestMissionRevOpsAndSummary(t *testing.T) {
	m := newMission([]string{harness.CapChat, harness.CapPlan, harness.CapAgents, harness.CapStatus, harness.CapNeedsApprovals})
	if len(m.sections) != 4 || m.sections[0].Kind != harness.SectionNeeds || m.sections[3].Kind != harness.SectionStatus {
		t.Fatalf("sections = %+v", m.sections)
	}
	row := func(id, state string) *harness.Row { return &harness.Row{ID: id, Title: id, State: state} }
	d := m.apply([]harness.Op{
		{Op: harness.OpUpsert, Section: harness.SectionPlan, Row: row("1", harness.RowDone)},
		{Op: harness.OpUpsert, Section: harness.SectionPlan, Row: row("2", harness.RowActive)},
		{Op: harness.OpUpsert, Section: harness.SectionPorts, Row: row("3000", "running")}, // unsupported: dropped
	})
	if d == nil || d.Rev != 1 || len(d.Ops) != 3 || d.Ops[2].Op != harness.OpSummary || d.Ops[2].Summary.Plan.Done != 1 {
		t.Fatalf("delta = %+v", d)
	}
	if again := m.apply([]harness.Op{{Op: harness.OpUpsert, Section: harness.SectionPlan, Row: row("1", harness.RowDone)}}); again != nil {
		t.Fatal("an unchanged row made a delta")
	}
	d = m.apply([]harness.Op{{Op: harness.OpUpsert, Section: harness.SectionAgents, Row: row("ag", harness.StateRunning)},
		{Op: harness.OpRemove, Section: harness.SectionPlan, ID: "2"}})
	if d.Rev != 2 || m.summary.Agents != 1 || m.summary.Plan.Total != 1 {
		t.Fatalf("summary = %+v", m.summary)
	}
	st := &harness.StatusBlock{Model: "Opus"}
	if d = m.apply([]harness.Op{{Op: harness.OpStatus, Status: st}}); d == nil || d.Rev != 3 {
		t.Fatal("status op")
	}
	if d = m.setWorking(true); d == nil || !d.Ops[0].Summary.Working || d.Rev != 4 {
		t.Fatalf("working = %+v", d)
	}
	d = m.apply([]harness.Op{{Op: harness.OpReplace, Section: harness.SectionPlan, Rows: nil}})
	if d == nil || m.summary.Plan != nil {
		t.Fatalf("replace = %+v %+v", d, m.summary)
	}
	snap := m.snapshot()
	snap.Sections[1].Rows[0].Title = "changed"
	if m.sections[1].Rows[0].Title == "changed" {
		t.Fatal("snapshot shares rows")
	}
}

type fakeSub struct {
	mu    sync.Mutex
	got   []harness.Notification
	block chan struct{}
	done  chan struct{}
}

func (f *fakeSub) Notify(n harness.Notification) error {
	if f.block != nil {
		<-f.block
	}
	f.mu.Lock()
	f.got = append(f.got, n)
	f.mu.Unlock()
	return nil
}

func (f *fakeSub) Done() <-chan struct{} { return f.done }

func (f *fakeSub) notes() []harness.Notification {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]harness.Notification(nil), f.got...)
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met in time")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestSubscriberHeldThenOverflowReset(t *testing.T) {
	sub := &fakeSub{done: make(chan struct{}), block: make(chan struct{})}
	dropped := make(chan struct{})
	q := newSubQueue(sub, "s", func() string { return "e1" }, time.Now, func() { close(dropped) })
	q.push(harness.Notification{Kind: harness.NoteItems})
	time.Sleep(20 * time.Millisecond)
	if len(sub.notes()) != 0 {
		t.Fatal("a held queue delivered")
	}
	for i := 0; i < QueueSize+5; i++ {
		q.push(harness.Notification{Kind: harness.NoteItems})
	}
	q.release()
	close(sub.block)
	<-dropped
	got := sub.notes()
	if len(got) != 1 || got[0].Kind != harness.NoteReset || got[0].Reason != "overflow" || got[0].Epoch != "e1" {
		t.Fatalf("overflow = %+v", got)
	}
	q.push(harness.Notification{Kind: harness.NoteItems})
	time.Sleep(20 * time.Millisecond)
	if len(sub.notes()) != 1 {
		t.Fatal("delivered after reset")
	}
}

func TestSubscriberDoneDrops(t *testing.T) {
	sub := &fakeSub{done: make(chan struct{})}
	dropped := make(chan struct{})
	q := newSubQueue(sub, "s", func() string { return "e" }, time.Now, func() { close(dropped) })
	q.release()
	q.push(harness.Notification{Kind: harness.NoteMission})
	waitFor(t, func() bool { return len(sub.notes()) == 1 })
	close(sub.done)
	<-dropped
}

func TestVersionGate(t *testing.T) {
	cases := []struct {
		out, pattern, rng string
		ok                bool
		version           string
	}{
		{"2.1.319 (Claude Code)\n", `^(\d+\.\d+\.\d+)`, ">=2.1.250 <3", true, "2.1.319"},
		{"2.1.200 (Claude Code)", `^(\d+\.\d+\.\d+)`, ">=2.1.250 <3", false, "2.1.200"},
		{"3.0.0", `^(\d+\.\d+\.\d+)`, ">=2.1.250 <3", false, "3.0.0"},
		{"codex-cli 0.144.1", `(\d+\.\d+\.\d+)`, ">=0.140 <0.150", true, "0.144.1"},
		{"omp/18.16.0-beta", `omp/(\S+)`, ">=18.10 <19", true, "18.16.0-beta"},
		{"garbage", `^(\d+\.\d+)`, ">=1", false, ""},
	}
	for _, c := range cases {
		r := judgeVersion(c.out, c.pattern, c.rng)
		if r.ok != c.ok || r.version != c.version {
			t.Errorf("judge(%q, %q) = %+v", c.out, c.rng, r)
		}
	}
	if _, err := parseRange("~2"); err == nil {
		t.Error("bad range parsed")
	}
}

func TestCheckArgs(t *testing.T) {
	ok := map[string]json.RawMessage{"id": json.RawMessage(`"opus"`), "text": json.RawMessage(`"a\nb"`)}
	if _, err := checkArgs(ok); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []map[string]json.RawMessage{
		{"id": json.RawMessage(`"a\nb"`)},
		{"id": json.RawMessage(`"` + string(make([]byte, 0)) + `\u0007"`)},
		{"x": json.RawMessage(`{`)},
	} {
		if _, err := checkArgs(bad); err == nil {
			t.Errorf("accepted %v", bad)
		}
	}
	many := map[string]json.RawMessage{}
	for i := 0; i < 17; i++ {
		many[string(rune('a'+i))] = json.RawMessage(`1`)
	}
	if _, err := checkArgs(many); err == nil {
		t.Error("accepted 17 args")
	}
	long := make([]byte, maxArgString+1)
	for i := range long {
		long[i] = 'x'
	}
	b, _ := json.Marshal(string(long))
	if _, err := checkArgs(map[string]json.RawMessage{"v": b}); err == nil {
		t.Error("accepted a long arg")
	}
	var inv interface{ Error() string }
	_, err := checkArgs(map[string]json.RawMessage{"x": json.RawMessage(`{`)})
	if !errors.As(err, &inv) {
		t.Error("not an error")
	}
}

func init() {
	harness.RegisterAdapterFile("fakeapi", []byte(`
adapter: fakeapi
name: Fake API
revision: 1
match: { agents: [fakeapi] }
endpoint: { kind: http, launch_args: [--port, '{{.port}}'] }
capabilities: [chat]
`))
}

func TestEndpointArgs(t *testing.T) {
	e := New(Config{TogoHome: t.TempDir()})
	args := e.EndpointArgs("fakeapi", "s_abcdefghij")
	if len(args) != 2 || args[0] != "--port" || args[1] == "" || args[1] == "0" {
		t.Fatalf("args = %v", args)
	}
	ep, ok := e.endpointFor("s_abcdefghij")
	if !ok || ep.url() != "http://127.0.0.1:"+args[1] {
		t.Fatalf("endpoint = %+v", ep)
	}
	if e.EndpointArgs("claude-less", "s_abcdefghij") != nil || e.EndpointArgs("fakeapi", "../x") != nil {
		t.Fatal("args for an agent with no endpoint or a bad id")
	}
}
