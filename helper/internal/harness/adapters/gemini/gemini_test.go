package gemini

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/engine"
	"github.com/gettogo/togo/helper/internal/harness/source"
	"github.com/gettogo/togo/helper/internal/harness/spec"
)

var update = flag.Bool("update", false, "rewrite golden files")

var t0 = time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC)

func env(home string) harness.Env {
	return harness.Env{Cwd: "/Users/dev/project", Vars: map[string]string{source.VarHome: home, "created": "2026-11-06T09:59:00Z"}}
}

func doc(t *testing.T, name string) harness.Record {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(v)
	return harness.Record{Source: "chat", At: t0, Data: raw}
}

type mapped struct {
	Items []harness.Item `json:"items"`
	Ops   []harness.Op   `json:"ops"`
}

func TestAdapterFileParses(t *testing.T) {
	a, err := spec.Parse(AdapterFile())
	if err != nil {
		t.Fatal(err)
	}
	if a.Actions["set_model"].Branch[0].Relaunch == nil || a.Actions["compact"].Type.Text != "/compress" {
		t.Fatal("model change must relaunch and compact must type /compress")
	}
}

func TestChatDiffsRewrites(t *testing.T) {
	c := newChat(env(t.TempDir()))
	var out mapped
	var counts []int
	for _, name := range []string{"chat-1.json", "chat-2.json", "chat-2.json", "other-session.json", "chat-3.json"} {
		items, ops := c.Map(doc(t, name))
		counts = append(counts, len(items))
		out.Items = append(out.Items, items...)
		out.Ops = append(out.Ops, ops...)
	}
	if counts[2] != 0 || counts[3] != 0 {
		t.Fatalf("unchanged copy or other session emitted items: %v", counts)
	}
	got, _ := json.MarshalIndent(out, "", " ")
	p := filepath.Join("testdata", "chat.golden.json")
	if *update {
		_ = os.WriteFile(p, append(got, '\n'), 0o644)
	}
	want, _ := os.ReadFile(p)
	if !bytes.Equal(bytes.TrimSpace(got), bytes.TrimSpace(want)) {
		t.Fatal("chat.golden.json differs; run go test -update and review")
	}
	last := map[string]harness.Item{}
	for _, it := range out.Items {
		last[it.ID] = it
	}
	if u := last["u1"].User; u.Text != "Summarise @README.md" {
		t.Fatalf("prompt = %q", u.Text)
	}
	if r := last["read_file_1"].Tool; r.State != harness.StateOK || r.Title != "Read README.md" || r.Output == "" {
		t.Fatalf("read = %+v", r)
	}
	if last["turn-u1"].Turn.State != harness.StateOK || last["turn-u1"].Turn.Steps != 1 {
		t.Fatalf("turn 1 = %+v", last["turn-u1"].Turn)
	}
	if s := last["shell_1"].Tool; s.State != harness.StateFailed || s.Category != harness.ToolShell {
		t.Fatalf("shell = %+v", s)
	}
	if p := last["plan-write_todos_1"].Plan; p.Total != 2 || p.Current != "Run the tests" {
		t.Fatalf("plan = %+v", p)
	}
	if last["turn-u2"].Turn.State != harness.StateFailed || last["e1"].Notice.Level != "error" {
		t.Fatal("error message must end the turn")
	}
	st := c.State()
	if st["model"] != "gemini-2.5-flash" || st["harness_session"] != "9b49-sess" || st["working"] != "false" {
		t.Fatalf("state = %v", st)
	}
}

func TestChatWorkingWhileToolRuns(t *testing.T) {
	c := newChat(env(t.TempDir()))
	c.Map(doc(t, "chat-1.json"))
	if c.State()["working"] != "true" {
		t.Fatal("turn with a running tool is not working")
	}
}

func TestHooksState(t *testing.T) {
	h := newHooks(env(t.TempDir()))
	if g := h.State()["chat_glob"]; g != "~/.gemini/tmp/project/chats/session-*.json" {
		t.Fatalf("glob before hooks = %q", g)
	}
	b, _ := os.ReadFile("testdata/hooks.jsonl")
	var notices int
	for i, raw := range source.Records(b, 100) {
		items, _ := h.Map(harness.Record{Source: "hooks", At: t0, Data: raw})
		notices += len(items)
		if i == 1 && h.State()["working"] != "true" {
			t.Fatal("BeforeAgent did not mark working")
		}
	}
	st := h.State()
	if st["working"] != "false" || st["harness_session"] != "9b49-sess" || st["chat_glob"] != st["transcript_path"] || notices != 1 {
		t.Fatalf("state = %v, notices %d", st, notices)
	}
}

func TestProjectDirReadsRegistry(t *testing.T) {
	home := t.TempDir()
	_ = os.MkdirAll(filepath.Join(home, ".gemini"), 0o700)
	_ = os.WriteFile(filepath.Join(home, ".gemini", "projects.json"), []byte(`{"projects":{"/Users/dev/project":"project-1","/x":"../evil"}}`), 0o600)
	if d := ProjectDir(home, "/Users/dev/project"); d != "project-1" {
		t.Fatalf("dir = %q", d)
	}
	if d := ProjectDir(home, "/x"); d != "x" {
		t.Fatalf("unsafe registry value used: %q", d)
	}
	if s := Slug("/Users/dev/AI Log Analysis V3"); s != "ai-log-analysis-v3" {
		t.Fatalf("slug = %q", s)
	}
}

func TestRelaunchArgvRenders(t *testing.T) {
	a, _ := spec.Parse(AdapterFile())
	argv := a.Actions["set_model"].Branch[0].Relaunch.Argv
	data := map[string]any{"id": "gemini-2.5-pro", "state": map[string]any{"harness_session": "9b49-sess"}}
	var got []string
	for _, s := range argv {
		v, err := engine.Render(s, data)
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, v)
	}
	want := []string{"gemini", "-m", "gemini-2.5-pro", "--resume", "9b49-sess"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("argv = %v", got)
		}
	}
}
