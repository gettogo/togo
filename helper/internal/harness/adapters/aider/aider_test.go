package aider

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/spec"
)

func TestAdapterFileParses(t *testing.T) {
	a, err := spec.Parse(adapterFile)
	if err != nil {
		t.Fatal(err)
	}
	if a.Adapter != "aider" {
		t.Fatalf("adapter %q", a.Adapter)
	}
}

// The fixture was written by aider 0.86's own InputOutput (user_input,
// ai_output, tool_output, confirm_ask, tool_error) twice, as two runs in
// one folder, with no model call.
func fixtureLines(t *testing.T) []string {
	t.Helper()
	b, err := os.ReadFile("testdata/chat.history.md")
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
}

func record(t *testing.T, lines []string) harness.Record {
	t.Helper()
	raw, _ := json.Marshal(lines)
	return harness.Record{Source: "history", At: time.Date(2026, 11, 7, 22, 33, 0, 0, time.UTC), Data: raw}
}

// latest keeps the last version of each item, in first-seen order.
func latest(items []harness.Item) []harness.Item {
	idx := map[string]int{}
	var out []harness.Item
	for _, it := range items {
		if i, ok := idx[it.ID]; ok {
			out[i] = it
			continue
		}
		idx[it.ID] = len(out)
		out = append(out, it)
	}
	return out
}

func TestHistoryMapsOnlyThisRun(t *testing.T) {
	second := time.Date(2026, 11, 7, 22, 32, 30, 0, time.Local)
	h := newHistory(harness.Env{Cwd: t.TempDir(), Vars: map[string]string{"created": second.UTC().Format(time.RFC3339)}})
	items, _ := h.Map(record(t, fixtureLines(t)))
	items = append(items, h.CloseIdle(time.Date(2026, 11, 7, 22, 34, 0, 0, time.UTC))...)
	got := latest(items)
	var desc []string
	for _, it := range got {
		switch it.Kind {
		case harness.KindUser:
			desc = append(desc, "user:"+it.User.Text)
		case harness.KindAssistant:
			desc = append(desc, "assistant:"+strings.SplitN(it.Assistant.Text, "\n", 2)[0])
		case harness.KindTool:
			desc = append(desc, "tool:"+it.Tool.Category+":"+it.Tool.Title)
		case harness.KindNotice:
			desc = append(desc, "notice:"+it.Notice.Text)
		case harness.KindTurn:
			desc = append(desc, "turn:"+it.Turn.State)
		}
	}
	want := []string{
		"user:add a hello function to hello.py",
		"turn:ok",
		"tool:other:Added hello.py to the chat",
		"assistant:I'll add the function.",
		"tool:edit:Edited hello.py",
		"tool:other:Commit 1a2b3c4 feat: add hello function",
		"tool:other:python hello.py",
		"tool:shell:python hello.py",
		"tool:other:Add command output to the chat? (Y)es/(N)o [Yes]: y",
		"user:/ask what does it print?\nanswer briefly",
		"turn:failed",
		"assistant:It prints `hi`.",
		"notice:Can't reach the provider: upstream timed out",
	}
	if strings.Join(desc, "|") != strings.Join(want, "|") {
		t.Fatalf("items:\n%q\nwant:\n%q", desc, want)
	}
	for _, it := range got {
		if it.Kind == harness.KindAssistant && strings.HasPrefix(it.Assistant.Text, "I'll") {
			if !strings.Contains(it.Assistant.Text, "> quoted line in the reply") || strings.HasSuffix(it.Assistant.Text, "\n") {
				t.Fatalf("reply = %q", it.Assistant.Text)
			}
		}
		if it.TurnID == "" {
			t.Fatalf("item %s has no turn", it.ID)
		}
	}
	if h.CloseIdle(time.Now()) != nil {
		t.Fatal("a closed turn closed again")
	}
}

// A reply that arrives after the pane went idle still joins its turn, and
// a record split mid-reply grows the same item.
func TestHistoryReplySpansRecords(t *testing.T) {
	h := newHistory(harness.Env{Cwd: t.TempDir(), Vars: map[string]string{}})
	h.Map(record(t, []string{"# aider chat started at 2026-11-07 22:32:30", "", "", "#### hi  "}))
	h.CloseIdle(time.Now())
	a, _ := h.Map(record(t, []string{"", "Hello"}))
	b, _ := h.Map(record(t, []string{"there.", ""}))
	if len(a) != 1 || len(b) != 1 || a[0].ID != b[0].ID || b[0].Assistant.Text != "Hello\nthere." || b[0].TurnID != a[0].TurnID || a[0].TurnID == "" {
		t.Fatalf("a=%+v b=%+v", a, b)
	}
}

func TestHistoryPath(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "a", "b")
	_ = os.MkdirAll(sub, 0o755)
	if p := historyPath(harness.Env{Cwd: sub}); p != filepath.Join(sub, historyName) {
		t.Fatalf("no git: %s", p)
	}
	_ = os.Mkdir(filepath.Join(root, ".git"), 0o755)
	if p := historyPath(harness.Env{Cwd: sub}); p != filepath.Join(root, historyName) {
		t.Fatalf("git root: %s", p)
	}
	if p := historyPath(harness.Env{Cwd: sub, Argv: []string{"aider", "--chat-history-file", "x.md"}}); p != filepath.Join(sub, "x.md") {
		t.Fatalf("flag: %s", p)
	}
	if p := historyPath(harness.Env{Cwd: sub, Argv: []string{"aider", "--chat-history-file=/tmp/y.md"}}); p != "/tmp/y.md" {
		t.Fatalf("flag=: %s", p)
	}
}
