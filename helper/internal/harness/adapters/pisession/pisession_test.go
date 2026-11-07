package pisession

import (
	"bufio"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

func replay(t *testing.T, m *Session, lines []string) (map[string]harness.Item, []harness.Op) {
	t.Helper()
	items := map[string]harness.Item{}
	var ops []harness.Op
	for i, l := range lines {
		it, op := m.Map(harness.Record{At: time.Unix(int64(i), 0), Data: json.RawMessage(l)})
		for _, x := range it {
			items[x.ID] = x
		}
		ops = append(ops, op...)
	}
	return items, ops
}

func lines(t *testing.T) []string {
	f, err := os.Open("testdata/session.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		out = append(out, sc.Text())
	}
	return out
}

// The fixture follows docs/session-format.md of Pi 0.73: every entry type
// and message role the mapper reads.
func TestDocumentedSession(t *testing.T) {
	m := New("pi.session", harness.Env{Cwd: "/Users/user/proj"})
	items, ops := replay(t, m, lines(t))
	if u := items["a0000003"]; u.User == nil || u.User.Text != "Fix the failing test in parser.go" {
		t.Fatalf("user %+v", u)
	}
	if th := items["a0000004:0"]; th.Thinking == nil || th.Thinking.Text != "Look at the test first." {
		t.Fatalf("thinking %+v", th)
	}
	read := items["call_1"].Tool
	if read.State != harness.StateOK || read.Category != harness.ToolRead || read.Title != "read parser_test.go" {
		t.Fatalf("read %+v", read)
	}
	edit := items["call_2"].Tool
	if edit.Category != harness.ToolEdit || len(edit.Files) != 1 || edit.Files[0].Path != "parser.go" {
		t.Fatalf("edit %+v", edit)
	}
	if items["call_3"].Tool.State != harness.StateFailed || items["call_4"].Tool.State != harness.StateInterrupted {
		t.Fatal("bash states")
	}
	first := items["turn-a0000003"].Turn
	if first.State != harness.StateOK || first.Steps != 3 || first.FilesChanged != 1 || first.Ended == nil {
		t.Fatalf("turn %+v", first)
	}
	if second := items["turn-a000000d"].Turn; second.State != harness.StateInterrupted {
		t.Fatalf("aborted turn %+v", second)
	}
	if b := items["a000000c"].Tool; b == nil || b.Category != harness.ToolShell || *b.ExitCode != 0 {
		t.Fatalf("bang command %+v", b)
	}
	if n := items["a000000a"].Notice; n == nil || n.Code != harness.NoticeModel {
		t.Fatalf("model notice %+v", n)
	}
	if n := items["a000000f"].Notice; n == nil || n.Code != harness.NoticeCompacted {
		t.Fatalf("compaction %+v", n)
	}
	st := m.State()
	if st["harness_session"] != "7c1e2a90-3b4d-4c55-9e10-aa11bb22cc33" || st["model"] != "openai/gpt-5" || st["effort"] != "high" ||
		st["working"] != "false" || st["session_title"] != "Parser fix" {
		t.Fatalf("state %v", st)
	}
	if len(ops) == 0 || ops[len(ops)-1].Status == nil {
		t.Fatal("no status op")
	}
}

// A session of another folder is ignored when the feed sees it.
func TestOtherFolderIgnored(t *testing.T) {
	m := New("pi.session", harness.Env{Cwd: "/Users/user/other"})
	replay(t, m, lines(t)[:1])
	if m.State()["harness_session"] != "" {
		t.Fatal("followed another folder's session")
	}
}

func TestMalformed(t *testing.T) {
	m := New("pi.session", harness.Env{})
	replay(t, m, []string{``, `{`, `{"type":"message","id":"x","message":5}`, `{"type":"message","id":"y","message":{"role":"toolResult","toolCallId":"none"}}`,
		`{"type":"message","id":"z","message":{"role":"assistant","content":"plain text","stopReason":"stop"}}`})
}
