package sessiongen

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// readCline lists the paths the Cline mapper reads once a message is split
// into one record per content block (adapters/cline/messages.go document,
// message, block; ask.go; text.go toolTitle for tool inputs).
var readCline = []string{
	"updated_at", "agent", "sessionId", "type",
	"id", "role", "ts", "metadata.displayOnly", "metadata.displayRole",
	"content[text].text", "content[thinking].thinking",
	"content[tool_use].id", "content[tool_use].name", "content[tool_use].input.*",
	"content[tool_result].tool_use_id", "content[tool_result].name", "content[tool_result].content", "content[tool_result].is_error",
	"content[tool_result].content[].query", "content[tool_result].content[].result", "content[tool_result].content[].success",
	"cwd", "started_at", "model", "metadata.mode", "pid", "interactive",
}

// clineRecords splits Cline's documents into records: each document's
// header (type "doc", messages left out), each message once per content
// block, and each manifest (type "manifest").
func clineRecords(t *testing.T, docs, manifests []string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, f := range docs {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var d map[string]any
		if err := json.Unmarshal(raw, &d); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		msgs, _ := d["messages"].([]any)
		delete(d, "messages")
		d["type"] = "doc"
		out = append(out, d)
		for _, m := range msgs {
			msg := m.(map[string]any)
			blocks, _ := msg["content"].([]any)
			for _, b := range blocks {
				rec := map[string]any{"type": "message"}
				for k, v := range msg {
					rec[k] = v
				}
				rec["content"] = []any{b}
				out = append(out, rec)
			}
		}
	}
	for _, f := range manifests {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		m["type"] = "manifest"
		out = append(out, m)
	}
	return out
}

func clineKind(r map[string]any) string {
	if r["type"] != "message" {
		return r["type"].(string)
	}
	b := r["content"].([]any)[0].(map[string]any)
	k := "message/" + r["role"].(string) + "/" + b["type"].(string)
	if n, ok := b["name"].(string); ok && b["type"] != "tool_result" {
		k += ":" + n
	}
	if md, ok := r["metadata"].(map[string]any); ok {
		k += "/" + md["displayRole"].(string)
	}
	return k
}

// The Cline profile writes the documents of Cline's real sessions
// (adapters/cline/testdata) and nothing else.
func TestClineMatchesRecordedFields(t *testing.T) {
	home := t.TempDir()
	cwd := filepath.Join(home, "proj")
	l, err := Write(home, &Cline{Extra: map[int]ClineTurn{
		1: {Edits: 1, Commands: 1, Think: true, Mode: "plan"},
		2: {Question: &ClineQuestion{Question: "Which?", Options: []string{"A", "B"}, Answer: "A"}},
		3: {Question: &ClineQuestion{Question: "Which?", Options: []string{"A", "B"}, Cancelled: true}},
	}}, Script{Seed: 7, Start: time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC), Cwd: cwd, Turns: []Turn{
		{Say: 40, Tools: 1, Output: 30}, {Say: 20}, {Say: 20}, {Error: true}, {New: true, Say: 10},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Mains) != 2 {
		t.Fatalf("%d sessions, want 2 (one /new)", len(l.Mains))
	}
	var manifests []string
	for _, p := range l.Mains {
		manifests = append(manifests, p[:len(p)-len(".messages.json")]+".json")
	}
	docs, _ := filepath.Glob("../adapters/cline/testdata/*.messages.json")
	mans, _ := filepath.Glob("../adapters/cline/testdata/*.json")
	var realMans []string
	for _, m := range mans {
		if filepath.Base(m) == "session.json" || filepath.Base(m) == "ask.json" {
			realMans = append(realMans, m)
		}
	}
	checkHonest(t, clineRecords(t, docs, realMans), clineRecords(t, l.Mains, manifests), clineKind, readCline)
}

func TestClineRefusesWhatItCannotShow(t *testing.T) {
	home := t.TempDir()
	for _, turn := range []Turn{{Agents: &Batch{Size: 1}}, {Jobs: 1}, {Exit: true}, {Interrupt: true}, {Compact: true}} {
		_, err := Write(home, &Cline{}, Script{Seed: 1, Start: time.Now(), Cwd: filepath.Join(home, "p"), Turns: []Turn{turn}})
		if err == nil {
			t.Errorf("%+v written without a real recording", turn)
		}
	}
}
