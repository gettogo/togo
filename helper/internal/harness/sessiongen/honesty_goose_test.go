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

// gooseRows collects the key paths of Goose rows: sessions rows under
// "session", message rows under "message", metadata under "meta", and each
// content part under "part:<type>", with an actionRequired part told apart
// by its actionType.
func gooseRow(row map[string]any, out map[string]bool) {
	if _, ok := row["content_json"]; !ok {
		pathsOf(row, "session", out)
		return
	}
	for k := range row {
		out["message."+k] = true
	}
	var meta map[string]any
	if s, ok := row["metadata_json"].(string); ok && json.Unmarshal([]byte(s), &meta) == nil {
		pathsOf(meta, "meta", out)
	}
	var parts []map[string]any
	if s, ok := row["content_json"].(string); ok && json.Unmarshal([]byte(s), &parts) == nil {
		for _, p := range parts {
			typ, _ := p["type"].(string)
			if d, ok := p["data"].(map[string]any); ok {
				typ += ":" + d["actionType"].(string)
			}
			pathsOf(p, "part:"+typ, out)
		}
	}
}

func realGoose(t *testing.T) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	files, _ := filepath.Glob(filepath.Join("..", "adapters", "goose", "testdata", "*.jsonl"))
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
				gooseRow(row, out)
			}
		}
		fh.Close()
	}
	return out
}

// gooseReads are the paths the mapper reads (adapters/goose/session.go
// :61-114 and question.go:42-53).
var gooseReads = []string{
	"session.id", "session.working_dir", "session.goose_mode", "session.created_at", "session.updated_at",
	"message.id", "message.message_id", "message.session_id", "message.role", "message.content_json", "message.created_timestamp", "message.metadata_json",
	"meta.userVisible",
	"part:text.text", "part:thinking.thinking", "part:toolRequest.id", "part:toolRequest.toolCall.value.name", "part:toolRequest.toolCall.value.arguments",
	"part:toolRequest.toolCall.status",
	"part:toolResponse.id", "part:toolResponse.toolResult.value.content[].text", "part:toolResponse.toolResult.value.isError",
	"part:error.kind", "part:error.message",
	"part:actionRequired:toolConfirmation.data.id", "part:actionRequired:toolConfirmation.data.toolName", "part:actionRequired:toolConfirmation.data.arguments",
	"part:actionRequired:toolConfirmationResponse.data.permission",
	"part:actionRequired:elicitation.data.message", "part:actionRequired:elicitation.data.requested_schema.properties",
	"part:actionRequired:elicitationResponse.data.user_data", "part:actionRequired:elicitationResponse.data.action",
}

func gooseScript(home string) (Script, []Turn) {
	cwd := filepath.Join(home, "proj")
	_ = os.MkdirAll(cwd, 0o755)
	s := Script{Seed: 9, Start: time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC), Cwd: cwd}
	return s, []Turn{
		{Say: 30, Tools: 3, Output: 40, Hook: &HookTurn{Approve: 2, Fail: 1}},
		{Say: 30, Hook: &HookTurn{Ask: &HookAsk{Prompt: "Which colour?", Options: []string{"red", "green"}, Answer: "green"}}},
		{Say: 30, Tools: 1, Interrupt: true},
		{Say: 30, Tools: 1, Error: true},
		{Say: 30, New: true},
		{Say: 30, Tools: 1, Hook: &HookTurn{Pending: true, Approve: 1}},
		{Prompt: Continue, Say: 20},
	}
}

func TestGooseWritesOnlyWhatGooseWrites(t *testing.T) {
	if SQLiteBin() == "" {
		t.Skip("no sqlite3")
	}
	home := t.TempDir()
	s, turns := gooseScript(home)
	l, err := Write(home, &Goose{Mode: "approve"}, Script{Seed: s.Seed, Start: s.Start, Cwd: s.Cwd, Turns: turns})
	if err != nil {
		t.Fatal(err)
	}
	real := realGoose(t)
	got := map[string]bool{}
	for _, q := range []string{"select * from sessions", "select * from messages order by id"} {
		out, err := exec.Command(SQLiteBin(), "-json", l.Main(), q).Output()
		if err != nil {
			t.Fatal(err)
		}
		var rows []map[string]any
		if err := json.Unmarshal(out, &rows); err != nil {
			t.Fatal(err)
		}
		for _, r := range rows {
			// Columns the captures do not select (the feeds do not read them).
			for _, k := range []string{"name", "description", "session_type"} {
				delete(r, k)
			}
			gooseRow(r, got)
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
		t.Errorf("the profile writes %q, which no real Goose row has", p)
	}
	for _, p := range gooseReads {
		if !got[p] {
			t.Errorf("the mapper reads %q, which the profile does not write", p)
		}
		if !real[p] {
			t.Errorf("%q is not in the captures (fix the list of reads)", p)
		}
	}
}

func TestGooseRefusesWhatNoCaptureHolds(t *testing.T) {
	if SQLiteBin() == "" {
		t.Skip("no sqlite3")
	}
	for name, turn := range map[string]Turn{
		"compaction": {Compact: true}, "subagent": {Agents: &Batch{Size: 1}}, "interrupt without a call": {Interrupt: true},
		"cancelled question": {Hook: &HookTurn{Ask: &HookAsk{Prompt: "x", Options: []string{"a"}, Cancel: true}}},
		"open question":      {Hook: &HookTurn{Pending: true, Ask: &HookAsk{Prompt: "x", Options: []string{"a"}, Answer: "a"}}},
	} {
		home := t.TempDir()
		s, _ := gooseScript(home)
		if _, err := Write(home, &Goose{}, Script{Seed: 1, Start: s.Start, Cwd: s.Cwd, Turns: []Turn{turn}}); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}
