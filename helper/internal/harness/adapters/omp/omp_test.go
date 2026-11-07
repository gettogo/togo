package omp

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/engine"
	"github.com/gettogo/togo/helper/internal/harness/source"
	"github.com/gettogo/togo/helper/internal/harness/spec"
)

var update = flag.Bool("update", false, "rewrite golden files")

var t0 = time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC)

func records(t *testing.T, name, feed string) []harness.Record {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	var out []harness.Record
	for _, raw := range source.Records(b, 1<<20) {
		out = append(out, harness.Record{Source: feed, At: t0, Data: raw})
	}
	return out
}

type mapped struct {
	Items []harness.Item `json:"items"`
	Ops   []harness.Op   `json:"ops"`
}

func run(m harness.Mapper, recs ...[]harness.Record) mapped {
	var out mapped
	for _, batch := range recs {
		for _, r := range batch {
			items, ops := m.Map(r)
			out.Items = append(out.Items, items...)
			out.Ops = append(out.Ops, ops...)
		}
	}
	return out
}

func (m mapped) last() map[string]harness.Item {
	out := map[string]harness.Item{}
	for _, it := range m.Items {
		out[it.ID] = it
	}
	return out
}

func golden(t *testing.T, name string, v any) {
	t.Helper()
	got, _ := json.MarshalIndent(v, "", " ")
	p := filepath.Join("testdata", name)
	if *update {
		if err := os.WriteFile(p, append(got, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(bytes.TrimSpace(got), bytes.TrimSpace(want)) {
		t.Fatalf("%s differs; run go test -update and review", name)
	}
}

func env() harness.Env {
	return harness.Env{Cwd: "/Users/dev/project", Vars: map[string]string{source.VarHome: "/Users/dev"}}
}

func TestAdapterFileParses(t *testing.T) {
	a, err := spec.Parse(AdapterFile())
	if err != nil {
		t.Fatal(err)
	}
	if a.Sources["commands"].RPC.Framing != spec.FramingTyped {
		t.Fatal("commands must use the typed framing")
	}
	for _, r := range Roles {
		if _, ok := a.Actions["set_role_"+r]; !ok {
			t.Fatalf("no action for role %s", r)
		}
	}
}

func TestSessionGolden(t *testing.T) {
	m := newSession(env())
	out := run(m, records(t, "session.jsonl", "session")[:11], records(t, "subagent.jsonl", "subagents"), records(t, "session.jsonl", "session")[11:])
	golden(t, "session.golden.json", out)

	items := out.last()
	if u := items["e003"]; u.Kind != harness.KindUser || u.User.Text != "Fix the failing parser test" || u.TurnID != "turn-e003" {
		t.Fatalf("prompt = %+v", u)
	}
	if r := items["call_read_1"]; r.Tool.Title != "Read parser/parse_test.go" || r.Tool.Category != harness.ToolRead || r.Tool.State != harness.StateOK {
		t.Fatalf("read = %+v", r.Tool)
	}
	turn := items["turn-e003"].Turn
	if turn.State != harness.StateOK || turn.Steps != 4 || turn.FilesChanged != 1 || turn.Ended == nil {
		t.Fatalf("turn = %+v", turn)
	}
	if a := items["agent-FindCallers"].Agent; a.State != harness.StateOK || a.Role != "explore" || a.Name != "Find callers of Parse" {
		t.Fatalf("agent = %+v", a)
	}
	if a := items["agent-CheckDocs"].Agent; a.State != harness.StateFailed {
		t.Fatalf("second agent = %+v", a)
	}
	if g := items["sub_grep_1"]; g.Parent != "agent-FindCallers" || g.TurnID != "" {
		t.Fatalf("subagent tool = %+v", g)
	}
	if n := items["compaction-e015"].Notice; n.Code != harness.NoticeCompacted || n.Text != "Compacted with snapcompact, 120k to 31k tokens" {
		t.Fatalf("compaction = %+v", n)
	}
	if b := items["e012"].Tool; b.Category != harness.ToolShell || *b.ExitCode != 0 {
		t.Fatalf("bash = %+v", b)
	}
	if items["turn-e016"].Turn.State != harness.StateInterrupted {
		t.Fatal("aborted turn not interrupted")
	}
	st := m.State()
	want := map[string]string{"model": "moonshot/kimi-k3", "effort": "high", "harness_session": "0b7c1f2a9e",
		"title": "Fix the parser test", "working": "false", "compactions": "1"}
	for k, v := range want {
		if st[k] != v {
			t.Errorf("state %s = %q, want %q", k, st[k], v)
		}
	}
	plan := false
	for _, op := range out.Ops {
		if op.Section == harness.SectionPlan && op.Op == harness.OpReplace && len(op.Rows) == 3 && op.Rows[1].State == harness.RowActive {
			plan = true
		}
	}
	if !plan {
		t.Fatal("no plan from todo_write")
	}
}

func TestWorkingFollowsTurn(t *testing.T) {
	m := newSession(env())
	recs := records(t, "session.jsonl", "session")
	run(m, recs[:6])
	if m.State()["working"] != "true" {
		t.Fatal("not working mid turn")
	}
	run(m, recs[6:13])
	if m.State()["working"] != "false" {
		t.Fatal("still working after stop")
	}
}

func TestUnknownLinesAreSkipped(t *testing.T) {
	m := newSession(env())
	for _, raw := range []string{`not json`, `{"type":"nope"}`, `{"type":"message","message":7}`, `{"type":"message","message":{"role":"toolResult","toolCallId":"x"}}`} {
		if items, ops := m.Map(harness.Record{Data: json.RawMessage(raw)}); len(items)+len(ops) != 0 {
			t.Fatalf("%s mapped to %v %v", raw, items, ops)
		}
	}
}

func TestSessionDirNames(t *testing.T) {
	cases := []struct{ cwd, want string }{
		{"/home/u/Dev/Delta RE", "-Dev-Delta RE"},
		{"/home/u", "-"},
		{"/tmp/togo-capture-omp-setup", "-tmp-togo-capture-omp-setup"},
		{"/opt/work/x", "--opt-work-x--"},
	}
	for _, c := range cases {
		got := SessionDirNames(c.cwd, "/home/u", "/var/tmp-none")
		if len(got) == 0 || got[0] != c.want {
			t.Errorf("%s: %v, want %s first", c.cwd, got, c.want)
		}
	}
}

func TestConfigRolesAndRoleActionArgv(t *testing.T) {
	doc, _ := os.ReadFile("testdata/config.yml")
	raw, _ := json.Marshal(string(doc))
	c := newConfig(env())
	c.Map(harness.Record{Source: "config", Data: raw})
	st := c.State()
	if st["role_model_smol"] != "llama.cpp/qwen3.8-27b" || st["role_level_smol"] != ":low" {
		t.Fatalf("smol = %q %q", st["role_model_smol"], st["role_level_smol"])
	}
	if strings.Contains(st["roles_without_smol"], `"smol"`) {
		t.Fatal("roles_without_smol holds smol")
	}
	a, err := spec.Parse(AdapterFile())
	if err != nil {
		t.Fatal(err)
	}
	data := map[string]any{"id": "moonshot/kimi-k3", "state": map[string]any{}}
	for k, v := range st {
		data["state"].(map[string]any)[k] = v
	}
	for _, role := range Roles {
		argv := a.Actions["set_role_"+role].Branch[0].CLI.Argv
		val, err := engine.Render(argv[len(argv)-1], data)
		if err != nil {
			t.Fatal(err)
		}
		var roles map[string]string
		if err := json.Unmarshal([]byte(val), &roles); err != nil {
			t.Fatalf("%s: %s is not JSON: %v", role, val, err)
		}
		if len(roles) < 4 || roles["task"] != "llama.cpp/qwen3.8-27b:medium" {
			t.Fatalf("%s dropped roles: %v", role, roles)
		}
		want := "moonshot/kimi-k3" + st["role_level_"+role]
		if roles[role] != want {
			t.Fatalf("%s = %q, want %q", role, roles[role], want)
		}
	}
}

func TestModelPickerRoleFacets(t *testing.T) {
	b, _ := os.ReadFile("testdata/models.json")
	var doc any
	_ = json.Unmarshal(b, &doc)
	a, _ := spec.Parse(AdapterFile())
	list, err := engine.JQFirst(a.Sources["models"].Select, doc, nil)
	if err != nil {
		t.Fatal(err)
	}
	st := RoleState(map[string]string{"default": "llama.cpp/qwen3.8-27b", "smol": "apple/on-device:low"})
	holders := map[string]string{}
	for _, el := range list.([]any) {
		for _, f := range a.Screens["model"].Filters {
			if v, _ := engine.MapValue("."+f.Field, el, st); v == "true" && strings.HasPrefix(f.ID, "set_role_") {
				id, _ := engine.MapValue(".selector", el, st)
				holders[f.ID] = id
			}
		}
	}
	if holders["set_role_default"] != "llama.cpp/qwen3.8-27b" || holders["set_role_smol"] != "apple/on-device" || holders["set_role_slow"] != "" {
		t.Fatalf("holders = %v", holders)
	}
}

func TestCommandsSelect(t *testing.T) {
	b, _ := os.ReadFile("testdata/commands.json")
	var doc any
	_ = json.Unmarshal(b, &doc)
	a, _ := spec.Parse(AdapterFile())
	v, err := engine.JQFirst(a.Commands.Select, doc, nil)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range v.([]any) {
		m := c.(map[string]any)
		if m["name"] == "switch" && m["hint"] == "[model]" {
			found = true
		}
	}
	if !found {
		t.Fatalf("commands = %v", v)
	}
}

func TestOldSubagentFileIsDropped(t *testing.T) {
	e := env()
	e.Vars["created"] = "2026-11-06T11:00:00Z"
	m := newSession(e)
	if out := run(m, records(t, "subagent.jsonl", "subagents")); len(out.Items) != 0 {
		t.Fatalf("items from an earlier session's subagent: %d", len(out.Items))
	}
}
