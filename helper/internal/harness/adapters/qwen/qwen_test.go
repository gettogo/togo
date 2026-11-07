package qwen

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/adapters/hookstyle"
	"github.com/gettogo/togo/helper/internal/harness/spec"
)

func records(t *testing.T, name string) []harness.Record {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out []harness.Record
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		out = append(out, harness.Record{Source: "transcript", At: time.Unix(0, 0), Data: json.RawMessage(sc.Text())})
	}
	return out
}

func TestAdapterFileParses(t *testing.T) {
	a, err := spec.Parse(adapterFile)
	if err != nil {
		t.Fatal(err)
	}
	if a.Adapter != "qwen" || a.Feeds["hooks"].Mapper != MapperHooks || a.Feeds["transcript"].Mapper != MapperTranscript {
		t.Fatalf("unexpected adapter %+v", a.Feeds)
	}
}

// The fixture is real Qwen Code 0.21 chat log lines with every free text
// value replaced.
func TestTranscriptReal(t *testing.T) {
	env := harness.Env{SessionID: "s_qwentrans1"}
	defer hookstyle.UnmarkLive(env.SessionID)
	m := newTranscript(env)
	final := map[string]harness.Item{}
	var ops []harness.Op
	for _, rec := range records(t, "transcript-real.jsonl") {
		items, o := m.Map(rec)
		ops = append(ops, o...)
		for _, x := range items {
			final[x.ID] = x
		}
	}
	if !hookstyle.IsLive(env.SessionID) {
		t.Fatal("the transcript must silence the hook chat")
	}
	var users, turns, tools, notices, replies int
	for _, x := range final {
		switch x.Kind {
		case harness.KindUser:
			users++
		case harness.KindTurn:
			turns++
			if x.Turn.Ended == nil {
				t.Errorf("turn %s left open", x.ID)
			}
		case harness.KindTool:
			tools++
			if x.Tool.State == harness.StateRunning {
				t.Errorf("tool %s left running", x.ID)
			}
		case harness.KindNotice:
			notices++
		case harness.KindAssistant:
			replies++
		}
	}
	if users != 2 || turns != 2 || tools != 2 || notices < 1 || replies < 2 {
		t.Fatalf("users %d turns %d tools %d notices %d replies %d", users, turns, tools, notices, replies)
	}
	var plan []harness.Row
	for _, op := range ops {
		if op.Section == harness.SectionPlan && op.Op == harness.OpReplace {
			plan = op.Rows
		}
	}
	if len(plan) == 0 || plan[0].State != harness.RowActive {
		t.Fatalf("plan %+v", plan)
	}
	st := m.State()
	if st["harness_session"] == "" || st["model"] == "" || st["working"] != "false" {
		t.Fatalf("state %v", st)
	}
}

func TestTranscriptShellTurn(t *testing.T) {
	m := newTranscript(harness.Env{SessionID: "s_qwenshell1"})
	defer hookstyle.UnmarkLive("s_qwenshell1")
	lines := []string{
		`{"uuid":"u1","sessionId":"q1","timestamp":"2026-11-06T10:00:00Z","type":"user","provenance":"real_user","message":{"role":"user","parts":[{"text":"run tests"}]}}`,
		`{"uuid":"a1","sessionId":"q1","timestamp":"2026-11-06T10:00:01Z","type":"assistant","model":"qwen3.7-plus","message":{"role":"model","parts":[{"functionCall":{"id":"c1","name":"run_shell_command","args":{"command":"go test ./..."}}}]}}`,
		`{"uuid":"r1","sessionId":"q1","timestamp":"2026-11-06T10:00:03Z","type":"tool_result","message":{"role":"user","parts":[{"functionResponse":{"id":"c1","name":"run_shell_command","response":{"output":"Command: go test ./...\nok"}}}]},"toolCallResult":{"callId":"c1","status":"success"}}`,
		`{"uuid":"a3","sessionId":"q1","timestamp":"2026-11-06T10:00:03Z","type":"assistant","message":{"role":"model","parts":[{"functionCall":{"id":"c2","name":"web_fetch","args":{"url":"https://example.com/x"}}}]}}`,
		`{"uuid":"r2","sessionId":"q1","timestamp":"2026-11-06T10:00:03Z","type":"tool_result","message":{"role":"user","parts":[{"functionResponse":{"id":"c2","name":"web_fetch","response":{"error":"404 Not Found"}}}]},"toolCallResult":{"callId":"c2","status":"error"}}`,
		`{"uuid":"a2","sessionId":"q1","timestamp":"2026-11-06T10:00:04Z","type":"assistant","message":{"role":"model","parts":[{"text":"All green.","thought":false}]}}`,
		`{"uuid":"x","type":"user","provenance":"system","subtype":"notification","message":{"parts":[{"text":"<task-notification>"}]}}`,
	}
	final := map[string]harness.Item{}
	for _, l := range lines {
		items, _ := m.Map(harness.Record{Data: json.RawMessage(l)})
		for _, x := range items {
			final[x.ID] = x
		}
	}
	if c := final["c1"].Tool; c == nil || c.Title != "go test ./..." || c.Category != harness.ToolShell || c.State != harness.StateOK || !strings.HasSuffix(c.Output, "ok") {
		t.Fatalf("call %+v", c)
	}
	if c := final["c2"].Tool; c == nil || c.State != harness.StateFailed || c.Category != harness.ToolWeb || c.Output != "404 Not Found" {
		t.Fatalf("failed call %+v", c)
	}
	if tb := final["u1.turn"].Turn; tb == nil || tb.State != harness.StateOK || tb.Steps != 2 {
		t.Fatalf("turn %+v", tb)
	}
	if r := final["a2:text"]; r.Assistant == nil || r.Assistant.Text != "All green." {
		t.Fatalf("reply %+v", r)
	}
	if len(final) != 5 {
		t.Fatalf("items %d: system text must not show as a prompt", len(final))
	}
}

func TestReadMode(t *testing.T) {
	if got := ReadMode("> \n  auto-accept edits (shift + tab to cycle)\n"); got["mode"] != "auto_edit" {
		t.Fatalf("got %v", got)
	}
	if got := ReadMode("talking about plan mode here\n> "); got != nil {
		t.Fatalf("prose read as a mode: %v", got)
	}
}
