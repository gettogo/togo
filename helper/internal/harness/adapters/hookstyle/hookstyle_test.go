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
	r, m := replay(t, "droid", "droid.jsonl", "s_live00001")
	for _, x := range r.items {
		if x.Kind == harness.KindUser || x.Kind == harness.KindTool || x.Kind == harness.KindAssistant || x.Kind == harness.KindTurn {
			t.Fatalf("drew %s while the transcript is live", x.Kind)
		}
	}
	d, _ := For("droid")
	_, norm, _ := Normalize(d, SubagentStart, []byte(`{"session_id":"dr-0001","agent_id":"a1","agent_type":"explore"}`))
	items, ops := m.Map(harness.Record{Source: "hooks", At: t0, Data: norm})
	if m.State()["harness_session"] != "dr-0001" || len(items) != 0 || len(ops) == 0 {
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
	f, err = Reduce(d, "errorOccurred", []byte(`{"error":{"message":"boom"}}`), env)
	if err != nil || f.Event != local.EventHarness {
		t.Fatalf("an error the session survives must not fail it: %+v err %v", f, err)
	}
	if _, err := Reduce(d, "preToolUse", []byte(`nope SECRET`), env); err == nil || strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("err %v", err)
	}
}

func TestDecisions(t *testing.T) {
	// No shipped dialect answers in Claude Code's PermissionRequest shape
	// now; the shape stays for the next harness that does.
	q := Dialect{Agent: "claude-shaped", Answer: AnswerPermissionRequest}
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
		t.Fatal("claude-shaped allow")
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

// Every Claude-shaped harness forwards its idle notification as idle, not
// as a needs-you request; a real permission prompt still opens one.
func TestIdleNotificationIsNotNeedsYou(t *testing.T) {
	env := map[string]string{"TOGO_SESSION": "s_aaaaaaaaaa"}
	for _, agent := range Agents() {
		d, _ := For(agent)
		f, err := Reduce(d, "Notification", []byte(`{"session_id":"x","message":"Waiting for your input","notification_type":"idle_prompt"}`), env)
		if err != nil {
			t.Fatalf("%s: %v", agent, err)
		}
		if f.Event != local.EventIdle {
			t.Fatalf("%s idle notification reduced to %q", agent, f.Event)
		}
		f, err = Reduce(d, "Notification", []byte(`{"session_id":"x","message":"Needs permission","notification_type":"permission_prompt"}`), env)
		if err != nil || f.Event != local.EventPermission {
			t.Fatalf("%s permission notification %q err %v", agent, f.Event, err)
		}
	}
}
