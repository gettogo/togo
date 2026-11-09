package codex

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/spec"
)

var update = flag.Bool("update", false, "rewrite golden files")

var t0 = time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)

// records reads a jsonl testdata file. A recorded call response that
// carries a thread becomes the feed's resume record, as the rpc runner
// emits it.
func records(t *testing.T, name string) []harness.Record {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(nil, 4<<20)
	var out []harness.Record
	for i := 0; sc.Scan(); i++ {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var m struct {
			Method string          `json:"method"`
			Result json.RawMessage `json:"result"`
		}
		json.Unmarshal(line, &m)
		data := append([]byte(nil), line...)
		if m.Method == "" && len(m.Result) > 0 {
			data, _ = json.Marshal(map[string]any{"method": "thread/resume", "params": map[string]json.RawMessage{"result": m.Result}})
		}
		out = append(out, harness.Record{Source: "appserver", At: t0.Add(time.Duration(i) * 100 * time.Millisecond), Data: data})
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

// last returns the final copy of each item id, as the item log keeps it.
func (m mapped) last() map[string]harness.Item {
	out := map[string]harness.Item{}
	for _, x := range m.items {
		out[x.ID] = x
	}
	return out
}

func golden(t *testing.T, name string, v any) {
	t.Helper()
	got, err := json.MarshalIndent(v, "", " ")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.WriteFile(path, append(got, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
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

func TestAdapterFileParses(t *testing.T) {
	a, err := spec.Parse(AdapterFile())
	if err != nil {
		t.Fatal(err)
	}
	if a.Adapter != "codex" || a.Endpoint == nil || a.Endpoint.Kind != spec.EndpointUnix {
		t.Fatalf("adapter %+v", a)
	}
	for name, f := range a.Feeds {
		if _, ok := harness.LookupMapper(f.Mapper); !ok {
			t.Errorf("feed %s: mapper %s is not registered", name, f.Mapper)
		}
	}
	if _, ok := harness.LookupRunner(spec.KindRPC); !ok {
		t.Error("rpc runner not registered")
	}
}

// The recorded attach (a second client joining a running thread, then
// that thread's turns) maps to stable items.
func TestRecordedAttachGolden(t *testing.T) {
	for _, name := range []string{"appserver-attach", "appserver-overrides"} {
		t.Run(name, func(t *testing.T) {
			m := NewAppServer(harness.Env{SessionID: "s_" + name})
			out := run(m, records(t, name+".jsonl"))
			golden(t, name+".golden.json", map[string]any{"items": out.items, "ops": out.ops, "state": m.State()})
		})
	}
}

func TestRecordedAttachShapes(t *testing.T) {
	m := NewAppServer(harness.Env{SessionID: "s_attach"})
	out := run(m, records(t, "appserver-attach.jsonl"))
	if m.State()["harness_session"] != "01a1b3df-6415-7362-9b2c-925f6dde129a" || m.State()["mode"] != "untrusted" {
		t.Fatalf("state %v", m.State())
	}
	items := out.last()
	cmd := items["02cfd9bf-4b40-4f17-837e-6fc25da762fb"]
	if cmd.Tool == nil || cmd.Tool.Title != "echo togo-spike-two" || cmd.Tool.State != harness.StateOK || cmd.Tool.Category != harness.ToolShell {
		t.Fatalf("command %+v", cmd.Tool)
	}
	if cmd.Tool.Output != "togo-spike-two\n" || cmd.Tool.ExitCode == nil || *cmd.Tool.ExitCode != 0 {
		t.Fatalf("output %+v", cmd.Tool)
	}
	turn := items["01a1b3df-d90c-7692-abdb-9447f4e3bc7d"]
	if turn.Turn == nil || turn.Turn.State != harness.StateOK || turn.Turn.Steps != 1 {
		t.Fatalf("turn %+v", turn.Turn)
	}
	failed := items["01a1b3df-e3e5-7783-913c-d46805e6408e"]
	if failed.Turn == nil || failed.Turn.State != harness.StateFailed {
		t.Fatalf("failed turn %+v", failed.Turn)
	}
	user := items["01a1b3df-e4b5-7712-924f-59fe8c3828f3"]
	if user.User == nil || user.User.Text != "say ok" {
		t.Fatalf("user %+v", user)
	}
	interrupted := items["01a1b3e0-469c-79e1-8997-e274907135f8"]
	if interrupted.Turn == nil || interrupted.Turn.State != harness.StateInterrupted {
		t.Fatalf("interrupted turn %+v", interrupted.Turn)
	}
	var sawError bool
	for _, x := range out.items {
		if x.Kind == harness.KindNotice && x.Notice.Code == harness.NoticeError {
			sawError = true
		}
	}
	if !sawError {
		t.Fatal("auth error notice missing")
	}
}

func TestTurnMapping(t *testing.T) {
	m := NewAppServer(harness.Env{SessionID: "s_turn"})
	out := run(m, records(t, "appserver-turn.jsonl"))
	items := out.last()
	st := m.State()
	if st["harness_session"] != "019a0000-0000-7000-8000-000000000001" || st["model"] != "gpt-5.5-mini" || st["effort"] != "high" || st["mode"] != "never" || st["working"] != "false" {
		t.Fatalf("state %v", st)
	}

	// History from the resume result.
	if r := items["call_old"]; r.Tool == nil || r.Tool.Title != "Read main.go" || r.Tool.Category != harness.ToolRead || r.TurnID != "019a0000-0000-7000-8000-0000000000a1" {
		t.Fatalf("history read %+v", r)
	}
	if a := items["msg_old"]; a.Assistant == nil || a.Assistant.Text != "It is a hello world." || a.Assistant.Partial {
		t.Fatalf("history reply %+v", a)
	}

	// Streaming: one partial copy (the second delta is inside the window),
	// then the final text.
	var partials int
	for _, x := range out.items {
		if x.ID == "msg_1" && x.Assistant != nil && x.Assistant.Partial {
			partials++
			if x.Assistant.Text != "Look" {
				t.Fatalf("partial %q", x.Assistant.Text)
			}
		}
	}
	if partials != 1 || items["msg_1"].Assistant.Text != "Looking at the tests." || items["msg_1"].Assistant.Partial {
		t.Fatalf("stream: %d partials, final %+v", partials, items["msg_1"].Assistant)
	}
	if th := items["rs_1"]; th.Thinking == nil || th.Thinking.Text != "Checking tests" {
		t.Fatalf("thinking %+v", th)
	}

	cmd := items["call_test"].Tool
	if cmd == nil || cmd.Title != "go test ./..." || cmd.State != harness.StateFailed || *cmd.ExitCode != 1 || cmd.Ended == nil {
		t.Fatalf("command %+v", cmd)
	}
	var sawWaiting bool
	for _, x := range out.items {
		if x.ID == "call_patch" && x.Tool.State == harness.StateWaiting {
			sawWaiting = true
		}
	}
	patch := items["call_patch"].Tool
	if !sawWaiting || patch.State != harness.StateOK || patch.Title != "Edit add.go" || len(patch.Files) != 1 || patch.Files[0].Added != 1 || patch.Files[0].Removed != 1 {
		t.Fatalf("patch waiting=%v %+v", sawWaiting, patch)
	}

	turn := items["019a0000-0000-7000-8000-0000000000a2"].Turn
	if turn == nil || turn.State != harness.StateOK || turn.Steps != 2 || turn.FilesChanged != 2 || turn.Ended == nil {
		t.Fatalf("turn %+v", turn)
	}
	plan := items["plan-019a0000-0000-7000-8000-0000000000a2"].Plan
	if plan == nil || plan.Done != 1 || plan.Total != 3 || plan.Current != "Fix Add" {
		t.Fatalf("plan %+v", plan)
	}

	q := items["rq-4"]
	if q.Question == nil || q.Question.Questions[0].ID != "scope" || !q.Question.Questions[0].AllowText || len(q.Question.Questions[0].Options) != 2 || q.Question.Answered == nil {
		t.Fatalf("question %+v", q.Question)
	}

	agent := items["agent-019a0000-0000-7000-8000-000000000002"]
	if agent.Agent == nil || agent.Parent != "" || agent.Agent.Name != "Scout" || agent.Agent.Role != "explorer" {
		t.Fatalf("agent %+v", agent)
	}
	child := items["msg_child"]
	if child.Parent != agent.ID || child.Assistant == nil {
		t.Fatalf("child item %+v", child)
	}

	var ops = map[string]int{}
	var status *harness.StatusBlock
	var changes []harness.Row
	for _, op := range out.ops {
		ops[op.Op+":"+op.Section]++
		if op.Op == harness.OpStatus {
			status = op.Status
		}
		if op.Op == harness.OpReplace && op.Section == harness.SectionChanges {
			changes = op.Rows
		}
	}
	if ops["upsert:needs"] != 1 || ops["remove:needs"] != 1 || ops["replace:plan"] != 1 || ops["upsert:agents"] < 1 {
		t.Fatalf("ops %v", ops)
	}
	if len(changes) != 2 || changes[0].Title != "add.go" || changes[0].Subtitle != "+1 -1" || changes[1].Title != "new.go" || changes[1].Open.Kind != harness.OpenDiff {
		t.Fatalf("changes %+v", changes)
	}
	if status == nil || status.Model != "gpt-5.5-mini" || status.Mode != "Full access" || status.ContextPercent == nil || *status.ContextPercent != 25 {
		t.Fatalf("status %+v", status)
	}
	if len(status.Meters) != 2 || status.Meters[0].Label != "5 hour limit" || status.Meters[1].Label != "Weekly limit" || status.Meters[0].Percent != 42 {
		t.Fatalf("meters %+v", status.Meters)
	}
	var rate bool
	for _, x := range out.items {
		if x.Kind == harness.KindNotice && x.Notice.Code == harness.NoticeRateLimit {
			rate = true
		}
	}
	if !rate {
		t.Fatal("rate limit notice missing")
	}
	golden(t, "appserver-turn.golden.json", map[string]any{"items": out.items, "ops": out.ops})
}

func TestMalformedRecordsAreSkipped(t *testing.T) {
	m := NewAppServer(harness.Env{})
	for _, d := range []string{`not json`, `{}`, `{"method":"item/started","params":{"item":{}}}`, `{"method":"turn/started","params":5}`, `{"id":1,"method":"item/tool/requestUserInput","params":{"questions":[]}}`} {
		items, ops := m.Map(harness.Record{Data: json.RawMessage(d)})
		if len(items)+len(ops) != 0 {
			t.Errorf("%s produced output", d)
		}
	}
}

// When the connection closes, open questions are closed and their Needs
// rows removed: the requests behind them are gone.
func TestClosedConnectionDropsQuestions(t *testing.T) {
	m := NewAppServer(harness.Env{SessionID: "s_closedq"})
	q := `{"id":7,"method":"item/tool/requestUserInput","params":{"threadId":"t","turnId":"u","questions":[{"id":"q","question":"Pick","options":[{"label":"A"}]}]}}`
	items, ops := m.Map(harness.Record{Data: json.RawMessage(q)})
	if len(items) != 1 || len(ops) != 1 || items[0].ID != "rq-7" {
		t.Fatalf("question %+v %+v", items, ops)
	}
	items, ops = m.Map(harness.Record{Data: json.RawMessage(`{"method":"rpc/closed"}`)})
	if len(items) != 1 || items[0].Question == nil || items[0].Question.Answered == nil {
		t.Fatalf("closed items %+v", items)
	}
	if len(ops) != 1 || ops[0].Op != harness.OpRemove || ops[0].ID != "rq-7" {
		t.Fatalf("closed ops %+v", ops)
	}
	if items, ops := m.Map(harness.Record{Data: json.RawMessage(`{"method":"rpc/closed"}`)}); len(items)+len(ops) != 0 {
		t.Fatal("closed twice produced output")
	}
}

// Turn errors (app-server v2 TurnError shapes, Codex 0.16x): a retried
// error is one warning per turn counting the attempts, the final one an
// error notice with the reason codexErrorInfo names, shown once even
// though turn/completed repeats it; a failed turn in resumed history gets
// its notice; a systemError ends the open turn failed.
func TestTurnErrors(t *testing.T) {
	m := NewAppServer(harness.Env{SessionID: "s_errors"})
	out := run(m, records(t, "appserver-errors.jsonl"))
	final := map[string]harness.Item{}
	count := map[string]int{}
	for _, x := range out.items {
		final[x.ID] = x
		count[x.ID]++
	}
	check := func(id, reason, level string) {
		t.Helper()
		x, ok := final[id]
		if !ok || x.Notice == nil || x.Notice.Reason != reason || x.Notice.Level != level || x.Notice.Text == "" {
			t.Fatalf("%s = %+v", id, x.Notice)
		}
	}
	check("error-old-ctx", harness.ErrContextOverflow, "error")
	check("retry-t-net", harness.ErrProviderUnreachable, "warning")
	check("error-t-net", harness.ErrProviderUnreachable, "error")
	check("error-t-model", harness.ErrModelNotFound, "error")
	if !strings.Contains(final["retry-t-net"].Notice.Text, "attempt 2") {
		t.Fatalf("retry = %q", final["retry-t-net"].Notice.Text)
	}
	if count["error-t-net"] != 1 || count["error-t-model"] != 1 {
		t.Fatalf("counts %v", count)
	}
	for _, id := range []string{"old-ctx", "t-net", "t-model", "t-crash"} {
		if x := final[id]; x.Turn == nil || x.Turn.State != harness.StateFailed || x.Turn.Ended == nil {
			t.Fatalf("turn %s = %+v", id, x.Turn)
		}
	}
	if st := m.State(); st["working"] != "false" {
		t.Fatalf("state %v", st)
	}
}

// The app-server feed starts as soon as the session's server is up. The
// engine holds a feed back until every state value its block names is
// known, and the thread id was known only from Codex's SessionStart hook;
// Codex 0.167 asks the user to trust new hooks before they run, so until
// then (or with no hooks installed) the chat stayed empty. Without a thread
// id the feed follows every thread the server has loaded and the mapper
// picks the top-level one.
func TestAppServerFeedNeedsNoHookState(t *testing.T) {
	a, err := spec.Parse(AdapterFile())
	if err != nil {
		t.Fatal(err)
	}
	f, ok := a.Feeds["appserver"]
	if !ok || f.RPC == nil {
		t.Fatal("no appserver rpc feed")
	}
	raw, err := json.Marshal(f.RPC)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), ".state.") {
		t.Fatalf("appserver feed waits on session state: %s", raw)
	}
}
