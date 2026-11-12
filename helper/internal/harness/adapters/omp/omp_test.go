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
	cases := []struct{ cwd, tmp, want string }{
		{"/home/u/Dev/Delta RE", "/tmp", "-Dev-Delta RE"},
		{"/home/u", "/tmp", "-"},
		{"/tmp/togo-capture-omp-setup", "/tmp", "-tmp-togo-capture-omp-setup"},
		{"/opt/work/x", "/tmp", "--opt-work-x--"},
		// omp's temp folder is TMPDIR: with it elsewhere (macOS), a
		// folder under /tmp is named by its absolute path (omp 18.16.0).
		{"/tmp/togo-conformance-x/w-omp", "/var/folders/ab/T", "--tmp-togo-conformance-x-w-omp--"},
	}
	for _, c := range cases {
		got := SessionDirNames(c.cwd, "/home/u", c.tmp, "/tmp")
		if len(got) == 0 || got[0] != c.want {
			t.Errorf("%s: %v, want %s first", c.cwd, got, c.want)
		}
	}
	// The other temp folders' names are still tried, last.
	got := SessionDirNames("/tmp/x", "/home/u", "/var/folders/ab/T", "/tmp")
	if got[len(got)-1] != "-tmp-x" {
		t.Errorf("fallbacks %v", got)
	}
}

// Of the folders that exist, the one omp wrote last is followed, whatever
// the name order says.
func TestSessionDirPicksNewestFolder(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".omp", "agent", "sessions")
	e := harness.Env{Cwd: "/opt/work/x", Environ: []string{"TMPDIR=/opt/work/"}, Vars: map[string]string{source.VarHome: home}}
	if got := sessionDir(e); got != "-tmp-x" {
		t.Fatalf("none yet: %q", got)
	}
	write := func(dir string, at time.Time) {
		d := filepath.Join(root, dir)
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		f := filepath.Join(d, "s.jsonl")
		if err := os.WriteFile(f, []byte("{}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		_ = os.Chtimes(f, at, at)
		_ = os.Chtimes(d, at, at)
	}
	now := time.Now()
	write("-tmp-x", now.Add(-time.Hour))
	write("--opt-work-x--", now)
	if got := sessionDir(e); got != "--opt-work-x--" {
		t.Fatalf("abs newer: %q", got)
	}
	write("-tmp-x", now.Add(time.Minute))
	if got := sessionDir(e); got != "-tmp-x" {
		t.Fatalf("tmp newer: %q", got)
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

// /model and /effort open the native picker and effort screens, as the other
// adapters' do; the screens exist only behind these commands.
func TestModelAndEffortCommandsOpenTheirScreens(t *testing.T) {
	a, err := spec.Parse(AdapterFile())
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"model": "model", "effort": "effort"}
	for _, c := range a.Commands.Static {
		if screen, ok := want[c.Name]; ok {
			if c.Screen != screen {
				t.Fatalf("/%s opens %q", c.Name, c.Screen)
			}
			if _, ok := a.Screens[screen]; !ok {
				t.Fatalf("no %s screen", screen)
			}
			delete(want, c.Name)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing commands %v", want)
	}
}

// A real omp 18.16.0 record of a turn the provider refused (a bogus key):
// the turn fails, ends, and says why in words.
func TestProviderErrorEndsTurnWithNotice(t *testing.T) {
	m := newSession(env())
	got := run(m, records(t, "error-401.jsonl", "session")).last()
	n := got["edd5d43e-error"]
	if n.Notice == nil || n.Notice.Level != "error" || n.Notice.Reason != harness.ErrAuth ||
		!strings.HasPrefix(n.Notice.Text, "The provider rejected the sign-in or API key: 401 Incorrect API key") {
		t.Fatalf("notice %+v", n.Notice)
	}
	if m.State()["working"] != "false" {
		t.Fatal("still working after the error")
	}
	for _, it := range got {
		if it.Kind == harness.KindTurn && it.Turn.State != harness.StateFailed {
			t.Fatalf("turn %+v", it.Turn)
		}
	}
}

// A subagent file in the shared folder belongs to the main session named
// in its header. One whose parent is another session (an omp the user ran
// in a terminal) is not part of this conversation.
func TestForeignSubagentFileIsDropped(t *testing.T) {
	m := newSession(env())
	sess := records(t, "session.jsonl", "session")
	other := strings.ReplaceAll(string(mustRead(t, "subagent.jsonl")), "_0b7c1f2a9e.jsonl", "_ffffffffff.jsonl")
	var foreign []harness.Record
	for _, raw := range source.Records([]byte(other), 1<<20) {
		foreign = append(foreign, harness.Record{Source: "subagents", At: t0, Data: raw})
	}
	if out := run(m, sess[:11], foreign); len(out.Items) != len(run(newSession(env()), sess[:11]).Items) {
		t.Fatal("a foreign session's subagent was mapped")
	}
}

func mustRead(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}
