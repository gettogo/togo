package hookstyle

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/local"
	"github.com/gettogo/togo/helper/internal/wire"
)

var t0 = time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC)

type call struct {
	Event   string          `json:"event"`
	Payload json.RawMessage `json:"payload"`
}

func calls(t *testing.T, name string) []call {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out []call
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var c call
		if err := json.Unmarshal(sc.Bytes(), &c); err != nil {
			t.Fatal(err)
		}
		out = append(out, c)
	}
	return out
}

type result struct {
	items []harness.Item
	ops   []harness.Op
	last  map[string]harness.Item
}

// replay normalizes each hook call the way `togo hook` does and feeds it
// to a fresh mapper.
func replay(t *testing.T, agent, file string, session string) (result, *Mapper) {
	t.Helper()
	d, ok := For(agent)
	if !ok {
		t.Fatalf("no dialect for %s", agent)
	}
	m := New(harness.Env{SessionID: session}, Options{Name: agent + ".hooks"})
	r := result{last: map[string]harness.Item{}}
	for i, c := range calls(t, file) {
		_, norm, err := Normalize(d, c.Event, c.Payload)
		if err != nil {
			t.Fatalf("%s line %d: %v", file, i+1, err)
		}
		items, ops := m.Map(harness.Record{Source: "hooks", At: t0.Add(time.Duration(i) * time.Second), Data: norm})
		r.items = append(r.items, items...)
		r.ops = append(r.ops, ops...)
		for _, x := range items {
			r.last[x.ID] = x
		}
	}
	return r, m
}

func tools(r result) []*harness.ToolBody {
	var out []*harness.ToolBody
	seen := map[string]bool{}
	for _, x := range r.items {
		if x.Tool != nil && !seen[x.ID] {
			seen[x.ID] = true
			out = append(out, r.last[x.ID].Tool)
		}
	}
	return out
}

func TestQwenHooks(t *testing.T) {
	r, m := replay(t, "qwen", "qwen.jsonl", "s_qwen00001")
	st := m.State()
	if st["harness_session"] != "qw-0001" || st["mode"] != "auto_edit" || st["model"] != "qwen3.7-plus" ||
		!strings.HasSuffix(st["transcript_path"], "qw-0001.jsonl") || st["working"] != "false" {
		t.Fatalf("state %v", st)
	}
	turn := "turn-" + itoa(t0.Add(time.Second).UnixMilli())
	if u := r.last[turn+":user"]; u.User == nil || u.User.Text != "run the tests" {
		t.Fatalf("user %+v", u)
	}
	shell := r.last["call_a1"].Tool
	if shell == nil || shell.Title != "go test ./..." || shell.Category != harness.ToolShell || shell.State != harness.StateOK || shell.Output != "ok  pkg 0.2s" {
		t.Fatalf("shell %+v", shell)
	}
	waited := false
	for _, x := range r.items {
		if x.ID == "call_a1" && x.Tool.State == harness.StateWaiting {
			waited = true
		}
	}
	if !waited {
		t.Fatal("the permission request did not mark the call waiting")
	}
	edit := r.last["call_a2"].Tool
	if edit.Category != harness.ToolEdit || edit.State != harness.StateFailed || edit.Title != "Edit /HOME/proj/main.go" {
		t.Fatalf("edit %+v", edit)
	}
	if reply := r.last[turn+":reply"]; reply.Assistant == nil || reply.Assistant.Text != "Tests pass." {
		t.Fatalf("reply %+v", reply)
	}
	if tb := r.last[turn].Turn; tb == nil || tb.State != harness.StateOK || tb.Steps != 2 || tb.Ended == nil {
		t.Fatalf("turn %+v", tb)
	}
	rows := map[string]harness.Row{}
	for _, op := range r.ops {
		rows[op.Section+"/"+op.Row.ID] = *op.Row
	}
	if p := rows["plan/1"]; p.State != harness.RowDone || p.Title != "Run go test" {
		t.Fatalf("plan rows %+v", rows)
	}
	if a := rows["agents/sub-1"]; a.State != harness.RowDone || a.Title != "reviewer" {
		t.Fatalf("agent rows %+v", rows)
	}
}

func TestCopilotCamelCaseHooks(t *testing.T) {
	r, m := replay(t, "copilot", "copilot.jsonl", "s_copilot001")
	ts := tools(r)
	if len(ts) < 2 {
		t.Fatalf("tools %d", len(ts))
	}
	var bash, view *harness.ToolBody
	for _, x := range ts {
		switch x.Name {
		case "bash":
			bash = x
		case "view":
			view = x
		}
	}
	if bash == nil || bash.Title != "ls -la" || bash.State != harness.StateOK || bash.Output != "main.go\ngo.mod" {
		t.Fatalf("bash %+v", bash)
	}
	if view == nil || view.State != harness.StateFailed || view.Category != harness.ToolRead || view.Subject.Text != "/HOME/proj/main.go" {
		t.Fatalf("view %+v", view)
	}
	var notice *harness.NoticeBody
	for _, x := range r.items {
		if x.Notice != nil {
			notice = x.Notice
		}
	}
	if notice == nil || notice.Text != "network timeout" || notice.Level != "warning" {
		t.Fatalf("notice %+v", notice)
	}
	if st := m.State(); st["working"] != "false" {
		t.Fatalf("state %v", st)
	}
}

func TestDroidAndVibeHooks(t *testing.T) {
	r, m := replay(t, "droid", "droid.jsonl", "s_droid0001")
	ts := tools(r)
	if len(ts) != 1 || ts[0].Category != harness.ToolEdit || ts[0].State != harness.StateOK || ts[0].Subject.Text != "/HOME/proj/README.md" {
		t.Fatalf("droid tools %+v", ts)
	}
	if st := m.State(); st["harness_session"] != "dr-0001" || st["mode"] != "default" {
		t.Fatalf("droid state %v", st)
	}

	r, _ = replay(t, "vibe", "vibe.jsonl", "s_vibe00001")
	ts = tools(r)
	if len(ts) != 1 || ts[0].Title != "git status" || ts[0].Output != "On branch main" {
		t.Fatalf("vibe tools %+v", ts)
	}
	var reply string
	for _, x := range r.items {
		if x.Assistant != nil {
			reply = x.Assistant.Text
		}
	}
	if reply != "Clean tree." {
		t.Fatalf("vibe reply %q", reply)
	}
}

func TestLiveTranscriptSilencesHookChat(t *testing.T) {
	MarkLive("s_live00001")
	defer UnmarkLive("s_live00001")
	r, m := replay(t, "qwen", "qwen.jsonl", "s_live00001")
	for _, x := range r.items {
		if x.Kind == harness.KindUser || x.Kind == harness.KindTool || x.Kind == harness.KindAssistant || x.Kind == harness.KindTurn {
			t.Fatalf("drew %s while the transcript is live", x.Kind)
		}
	}
	if m.State()["harness_session"] != "qw-0001" || len(r.ops) == 0 {
		t.Fatal("state and mission rows must still update")
	}
}

func TestNormalizeKeepsSnakeCaseAndNamesEvent(t *testing.T) {
	d, _ := For("copilot")
	canon, out, err := Normalize(d, "preToolUse", []byte(`{"toolName":"bash","tool_name":"kept","toolArgs":"not json"}`))
	if err != nil || canon != PreToolUse {
		t.Fatalf("canon %q err %v", canon, err)
	}
	var p map[string]any
	_ = json.Unmarshal(out, &p)
	if p["tool_name"] != "kept" || p["tool_input"] != "not json" || p["hook_event_name"] != PreToolUse {
		t.Fatalf("normalized %s", out)
	}
	if _, _, err := Normalize(d, "x", []byte(`[1]`)); err == nil {
		t.Fatal("accepted a non-object payload")
	}
}

func TestReduce(t *testing.T) {
	env := map[string]string{"TOGO_SESSION": "s_aaaaaaaaaa"}
	d, _ := For("copilot")
	f, err := Reduce(d, "permissionRequest", []byte(`{"cwd":"/w","toolName":"bash","toolArgs":"{\"command\":\"rm -rf build\"}","token":"ghp_0123456789abcdefghijklmnopqrstuvwxyz"}`), env)
	if err != nil {
		t.Fatal(err)
	}
	if f.Agent != "copilot" || f.Event != local.EventPermission || f.Data.Command != "rm -rf build" || f.Hook != PermissionRequest ||
		f.SessionID == nil || *f.SessionID != "s_aaaaaaaaaa" {
		t.Fatalf("forward %+v", f)
	}
	if strings.Contains(string(f.Payload), "ghp_") {
		t.Fatal("a token reached the forwarded payload")
	}
	q, _ := For("qwen")
	f, err = Reduce(q, "TodoCreated", []byte(`{"session_id":"qw","todo_id":"1"}`), env)
	if err != nil || f.Event != local.EventHarness || f.Agent != "qwen" || f.Data.AgentSessionID != "qw" {
		t.Fatalf("todo forward %+v err %v", f, err)
	}
	f, err = Reduce(d, "errorOccurred", []byte(`{"error":{"message":"boom"}}`), env)
	if err != nil || f.Event != local.EventHarness {
		t.Fatalf("an error the session survives must not fail it: %+v err %v", f, err)
	}
	if _, err := Reduce(d, "preToolUse", []byte(`nope SECRET`), env); err == nil || strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("err %v", err)
	}
}

func TestDecisions(t *testing.T) {
	q, _ := For("qwen")
	c, _ := For("copilot")
	dr, _ := For("droid")
	if !Blocks(q, PermissionRequest) || Blocks(q, PreToolUse) || Blocks(dr, PermissionRequest) {
		t.Fatal("blocking events")
	}
	var qa struct {
		HookSpecificOutput struct {
			Decision struct{ Behavior string } `json:"decision"`
		} `json:"hookSpecificOutput"`
	}
	if json.Unmarshal(Decision(q, "Allow"), &qa) != nil || qa.HookSpecificOutput.Decision.Behavior != "allow" {
		t.Fatal("qwen allow")
	}
	var ca map[string]string
	if json.Unmarshal(Decision(c, wire.DecisionDeny), &ca) != nil || ca["permissionDecision"] != "deny" {
		t.Fatal("copilot deny")
	}
	if Decision(c, "Later") != nil || Decision(dr, "Allow") != nil {
		t.Fatal("an unknown answer or a harness without answers must print nothing")
	}
}

func itoa(v int64) string { b, _ := json.Marshal(v); return string(b) }
