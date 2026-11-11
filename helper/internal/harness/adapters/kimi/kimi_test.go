package kimi

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/engine"
	"github.com/gettogo/togo/helper/internal/harness/spec"
)

var t0 = time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)

type mapped struct {
	items map[string]harness.Item
	ops   []harness.Op
}

func replay(t *testing.T, m harness.Mapper, lines []string) mapped {
	t.Helper()
	out := mapped{items: map[string]harness.Item{}}
	for i, l := range lines {
		items, ops := m.Map(harness.Record{Source: "wire", At: t0.Add(time.Duration(i) * time.Second), Data: json.RawMessage(l)})
		for _, it := range items {
			out.items[it.ID] = it
		}
		out.ops = append(out.ops, ops...)
	}
	return out
}

func fixture(t *testing.T, name string) []string {
	t.Helper()
	f, err := os.Open("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var lines []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	return lines
}

func (m mapped) kinds() map[string]int {
	k := map[string]int{}
	for _, it := range m.items {
		k[it.Kind]++
	}
	return k
}

func TestAdapterFileParses(t *testing.T) {
	a, err := spec.Parse(AdapterFile())
	if err != nil {
		t.Fatal(err)
	}
	if a.Feeds["wire"].Mapper != MapperWire {
		t.Fatal("wire mapper")
	}
	var logs []string
	engine.New(engine.Config{Logf: func(f string, a ...any) { logs = append(logs, fmt.Sprintf(f, a...)) }})
	for _, l := range logs {
		if strings.Contains(l, "kimi") {
			t.Fatal(l)
		}
	}
}

// The feed glob names the folder Kimi keeps for the working directory.
func TestFeedFolder(t *testing.T) {
	a, _ := spec.Parse(AdapterFile())
	got, err := engine.Render(a.Feeds["wire"].JSONL.Glob, map[string]any{"cwd": "/Users/jamie"})
	if err != nil || got != "~/.kimi-code/sessions/wd_*_79a25ac0eb0f/session_*/agents/main/wire.jsonl" {
		t.Fatalf("%q %v", got, err)
	}
}

// A real protocol 1.4 session, every string scrubbed: prompts, steps,
// tools, a plan and a background command that finishes by notification.
func TestRecordedSession(t *testing.T) {
	m := NewWire(harness.Env{Cwd: "/Users/user/proj", SessionID: "s1"})
	out := replay(t, m, fixture(t, "wire-h1.jsonl"))
	k := out.kinds()
	if k[harness.KindUser] != 2 || k[harness.KindTool] < 30 || k[harness.KindThinking] == 0 || k[harness.KindAssistant] == 0 || k[harness.KindPlan] == 0 {
		t.Fatalf("kinds %v", k)
	}
	var bgDone, planRows bool
	for _, op := range out.ops {
		if op.Section == harness.SectionBackground && op.Row.State == harness.RowDone {
			bgDone = true
		}
		if op.Op == harness.OpReplace && op.Section == harness.SectionPlan && len(op.Rows) > 3 {
			planRows = true
		}
	}
	if !bgDone || !planRows {
		t.Fatalf("background done %v, plan rows %v", bgDone, planRows)
	}
	for _, it := range out.items {
		if it.Kind == harness.KindTool && it.Tool.Category == "" {
			t.Fatalf("tool without category %+v", it.Tool)
		}
	}
	if st := m.State(); st["mode"] != "auto" || st["model"] == "" {
		t.Fatalf("state %v", st)
	}
}

// A protocol 1.5 session: a question asked and answered in the TUI, and
// turns that end with turn.ended.
func TestRecordedQuestion(t *testing.T) {
	m := NewWire(harness.Env{SessionID: "s1"})
	out := replay(t, m, fixture(t, "wire-question.jsonl"))
	var q harness.Item
	for _, it := range out.items {
		if it.Kind == harness.KindQuestion {
			q = it
		}
	}
	if q.Question == nil || len(q.Question.Questions) != 1 || len(q.Question.Questions[0].Options) != 4 || len(q.Question.Answered["0"]) != 1 {
		t.Fatalf("question %+v", q.Question)
	}
	// The Needs row is the engine's (every open question item is one).
	for _, op := range out.ops {
		if op.Section == harness.SectionNeeds {
			t.Fatalf("adapter Needs op %+v", op)
		}
	}
	if st := m.State(); st["working"] != "false" || st["model"] != "kimi-code/k3" || st["effort"] != "high" {
		t.Fatalf("state %v", st)
	}
	for _, it := range out.items {
		if it.Kind == harness.KindTurn && it.Turn.State == harness.StateRunning {
			t.Fatalf("turn left running %+v", it)
		}
	}
}

// An Agent call that runs in the background finishes when its task
// notification arrives.
func TestBackgroundAgent(t *testing.T) {
	m := NewWire(harness.Env{})
	out := replay(t, m, []string{
		`{"type":"turn.prompt","input":[{"type":"text","text":"audit it"}],"origin":{"kind":"user"},"promptId":"msg_1","turnId":0}`,
		`{"type":"context.append_loop_event","event":{"type":"tool.call","toolCallId":"tool_a","name":"Agent","args":{"description":"Audit the gem","subagent_type":"explore"},"display":{"kind":"agent_call"}}}`,
		`{"type":"context.append_loop_event","event":{"type":"tool.result","toolCallId":"tool_a","result":{"output":"task_id: agent-86r\nstatus: running\nagent_id: agent-0"}}}`,
		`{"type":"context.append_loop_event","event":{"type":"step.end","finishReason":"end_turn"}}`,
		`{"type":"turn.steer","input":[{"type":"text","text":"<notification/>"}],"origin":{"kind":"background_task","taskId":"agent-86r","status":"completed"}}`,
	})
	a := out.items["agent-tool_a"]
	if a.Agent == nil || a.Agent.Name != "Audit the gem" || a.Agent.State != harness.StateOK || a.Agent.Ended == nil {
		t.Fatalf("agent %+v", a.Agent)
	}
	if tool := out.items["tool_a"]; tool.Tool.State != harness.StateOK || tool.Tool.Category != harness.ToolAgent {
		t.Fatalf("tool %+v", tool.Tool)
	}
	if u := out.items["msg_1"]; u.User == nil || u.User.Text != "audit it" {
		t.Fatalf("user %+v", u)
	}
}

func TestMalformedRecordsSkipped(t *testing.T) {
	m := NewWire(harness.Env{})
	for _, l := range []string{``, `{`, `[]`, `{"type":"context.append_loop_event","event":5}`, `{"type":"interaction.resolved","id":"nope"}`} {
		m.Map(harness.Record{Data: json.RawMessage(l)})
	}
}

// Kimi Code 2.5.2 writes the ask mode as "manual" (seen live); the mode
// chip's choices call it default.
func TestManualModeIsDefault(t *testing.T) {
	m := NewWire(harness.Env{})
	m.Map(harness.Record{Data: json.RawMessage(`{"type":"permission.set_mode","agentId":"main","mode":"yolo","time":1794079438000}`)})
	m.Map(harness.Record{Data: json.RawMessage(`{"type":"permission.set_mode","agentId":"main","mode":"manual","time":1794079438353}`)})
	if got := m.State()["mode"]; got != "default" {
		t.Fatalf("mode = %q, want default", got)
	}
}

// Leaving plan mode types /plan: a relaunch with --continue keeps plan mode
// (seen live on Kimi 2.5.2), and entering it needs no restart.
func TestPlanModeTogglesInPlace(t *testing.T) {
	a, err := spec.Parse(AdapterFile())
	if err != nil {
		t.Fatal(err)
	}
	act := a.Actions["set_mode"]
	pick := func(id, mode string) spec.Routes {
		for _, br := range act.Branch {
			out, err := engine.Render(br.When, map[string]any{"id": id, "state": map[string]string{"mode": mode}})
			if br.When == "" || (err == nil && engine.Truthy(out)) {
				return br.Routes
			}
		}
		t.Fatalf("no branch for %s from %s", id, mode)
		return spec.Routes{}
	}
	if r := pick("default", "plan"); r.Type == nil || r.Type.Text != "/plan" {
		t.Errorf("plan -> default = %+v", r)
	}
	if r := pick("plan", "default"); r.Type == nil || r.Type.Text != "/plan" {
		t.Errorf("default -> plan = %+v", r)
	}
	if r := pick("yolo", "default"); r.Relaunch == nil {
		t.Errorf("default -> yolo = %+v", r)
	}
	if r := pick("default", "yolo"); r.Relaunch == nil {
		t.Errorf("yolo -> default = %+v", r)
	}
}
