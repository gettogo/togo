package sessiongen

import (
	"bufio"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"testing"
	"time"
)

// crushRow collects the key paths of a Crush row: a sessions row under
// "session", a message row under "message", and each part under
// "part:<type>.data".
func crushRow(row map[string]any, out map[string]bool) {
	if _, ok := row["parts"]; !ok {
		for k := range row {
			out["session."+k] = true
		}
		return
	}
	for k := range row {
		out["message."+k] = true
	}
	var parts []map[string]any
	if s, ok := row["parts"].(string); ok && json.Unmarshal([]byte(s), &parts) == nil {
		for _, p := range parts {
			typ, _ := p["type"].(string)
			pathsOf(p, "part:"+typ, out)
		}
	}
}

func realCrush(t *testing.T) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	files, _ := filepath.Glob(filepath.Join("..", "adapters", "crush", "testdata", "db-*.jsonl"))
	for _, f := range files {
		fh, err := os.Open(f)
		if err != nil {
			t.Fatal(err)
		}
		sc := bufio.NewScanner(fh)
		sc.Buffer(make([]byte, 1<<20), 64<<20)
		for sc.Scan() {
			var row map[string]any
			if json.Unmarshal(sc.Bytes(), &row) == nil {
				crushRow(row, out)
			}
		}
		fh.Close()
	}
	// question_test.go:113 quotes a finish part whose reason is "canceled".
	out["part:finish.data.reason"] = true
	return out
}

// crushReads are the paths the mapper reads (adapters/crush/session.go
// :58-92, question.go:27-43 and :153-206, errors.go:10-40).
var crushReads = []string{
	"session.id", "session.title", "session.created_at", "session.updated_at",
	"message.id", "message.session_id", "message.role", "message.parts", "message.created_at", "message.updated_at", "message.finished_at", "message.is_summary_message",
	"part:text.data.text", "part:reasoning.data.thinking", "part:tool_call.data.id", "part:tool_call.data.name", "part:tool_call.data.input",
	"part:tool_result.data.tool_call_id", "part:tool_result.data.content", "part:tool_result.data.is_error",
	"part:finish.data.reason", "part:finish.data.message", "part:finish.data.details",
}

func crushScript(home string) (Script, []Turn) {
	cwd := filepath.Join(home, "proj")
	_ = os.MkdirAll(cwd, 0o755)
	s := Script{Seed: 9, Start: time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC), Cwd: cwd}
	return s, []Turn{
		{Say: 30, Tools: 3, Output: 40, Hook: &HookTurn{Fail: 1}},
		{Say: 30, Hook: &HookTurn{Ask: &HookAsk{Prompt: "Which colour?", Options: []string{"red", "green"}, Answer: "green"}}},
		{Say: 30, Hook: &HookTurn{Ask: &HookAsk{Prompt: "Skip?", Options: []string{"a"}, Cancel: true}}},
		{Say: 30, Tools: 1, Interrupt: true},
		{Say: 30, Tools: 1, Error: true},
		{Say: 30, New: true},
		{Say: 30, Tools: 1, Hook: &HookTurn{Pending: true}},
		{Prompt: Continue, Say: 20},
	}
}

func TestCrushWritesOnlyWhatCrushWrites(t *testing.T) {
	if SQLiteBin() == "" {
		t.Skip("no sqlite3")
	}
	home := t.TempDir()
	s, turns := crushScript(home)
	l, err := Write(home, &Crush{}, Script{Seed: s.Seed, Start: s.Start, Cwd: s.Cwd, Turns: turns})
	if err != nil {
		t.Fatal(err)
	}
	real := realCrush(t)
	got := map[string]bool{}
	for _, q := range []string{"select * from sessions", "select * from messages order by created_at, rowid"} {
		out, err := exec.Command(SQLiteBin(), "-json", l.Main(), q).Output()
		if err != nil {
			t.Fatal(err)
		}
		var rows []map[string]any
		if err := json.Unmarshal(out, &rows); err != nil {
			t.Fatal(err)
		}
		for _, r := range rows {
			// Columns the captures do not select (no feed reads them).
			delete(r, "parent_session_id")
			delete(r, "message_count")
			crushRow(r, got)
		}
	}
	var extra []string
	for p := range got {
		if !real[p] {
			extra = append(extra, p)
		}
	}
	sort.Strings(extra)
	for _, p := range extra {
		t.Errorf("the profile writes %q, which no real Crush row has", p)
	}
	for _, p := range crushReads {
		if !got[p] {
			t.Errorf("the mapper reads %q, which the profile does not write", p)
		}
		if !real[p] {
			t.Errorf("%q is not in the captures (fix the list of reads)", p)
		}
	}
}

func TestCrushRefusesWhatNoCaptureHolds(t *testing.T) {
	if SQLiteBin() == "" {
		t.Skip("no sqlite3")
	}
	for name, turn := range map[string]Turn{
		"compaction": {Compact: true}, "subagent": {Agents: &Batch{Size: 1}}, "permission prompt": {Tools: 1, Hook: &HookTurn{Approve: 1}},
		"plan": {Hook: &HookTurn{Ask: &HookAsk{Prompt: "x", Plan: "p"}}},
	} {
		home := t.TempDir()
		s, _ := crushScript(home)
		if _, err := Write(home, &Crush{}, Script{Seed: 1, Start: s.Start, Cwd: s.Cwd, Turns: []Turn{turn}}); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}
