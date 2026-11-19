package sessiongen

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/adapters/opencode"
)

// readOpenCode lists the paths of an event that OpenCode 1.x's helper side
// reads: the opencode.events mapper (events.go sessionPayload,
// messagePayload, sessionStatus; parts.go part, toolState, partDelta,
// diff, todos) and the adapter's sources over what the test server serves
// back from the same events (opencode.yaml sessions: .parentID,
// .time.updated, .title; children, todo, diff).
var readOpenCode = []string{
	"type", "properties.sessionID",
	"properties.info.id", "properties.info.parentID", "properties.info.title", "properties.info.agent",
	"properties.info.model.id", "properties.info.model.providerID", "properties.info.time.created",
	// The table is by path, not by kind: a prompt's model sits at the
	// path of a session's, so every real prompt's model is written whole.
	"properties.info.model.modelID",
	"properties.info.time.updated", "properties.info.time.completed",
	"properties.info.role", "properties.info.sessionID", "properties.info.modelID", "properties.info.providerID",
	"properties.info.mode",
	"properties.part.id", "properties.part.messageID", "properties.part.sessionID", "properties.part.type",
	"properties.part.text", "properties.part.time.start", "properties.part.time.end", "properties.part.tool",
	"properties.part.callID", "properties.part.state.status", "properties.part.state.input.*",
	"properties.part.state.output", "properties.part.state.title", "properties.part.state.metadata.*",
	"properties.part.state.time.start", "properties.part.state.time.end", "properties.part.state.raw",
	"properties.partID", "properties.field", "properties.delta",
	"properties.status.type",
	"properties.diff[].file", "properties.diff[].additions", "properties.diff[].deletions",
	"properties.todos[].content", "properties.todos[].status",
}

func openCodeKind(r map[string]any) string {
	k, _ := r["type"].(string)
	p, _ := r["properties"].(map[string]any)
	if part, ok := p["part"].(map[string]any); ok {
		k += "/" + part["type"].(string)
		if st, ok := part["state"].(map[string]any); ok {
			k += "/" + st["status"].(string)
		}
	}
	if info, ok := p["info"].(map[string]any); ok {
		if role, ok := info["role"].(string); ok {
			k += "/" + role
		}
		if tm, ok := info["time"].(map[string]any); ok && tm["completed"] != nil {
			k += "/done"
		}
	}
	return k
}

// quotedOpenCode are real payloads the adapter's tests quote where the
// recordings have none: a subagent session (events_test.go:213) and a
// diff entry (revert2_test.go:117, the diff route of OpenCode 2.2.5,
// whose entries have the fields 1.x's session.diff carries).
var quotedOpenCode = []string{
	`{"type":"session.created","properties":{"info":{"id":"ses_child","parentID":"ses_main","title":"Explore the tests","agent":"general","time":{"created":1794019000000}}}}`,
	`{"type":"session.diff","properties":{"sessionID":"ses_1","diff":[{"file":"notes.txt","additions":1,"deletions":1}]}}`,
}

// readServerEvents reads recorded events: SSE "data:" lines or JSON
// lines.
func readServerEvents(t *testing.T, files ...string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		sc := bufio.NewScanner(bytes.NewReader(raw))
		sc.Buffer(make([]byte, 1<<20), 64<<20)
		for sc.Scan() {
			line := strings.TrimPrefix(sc.Text(), "data: ")
			var m map[string]any
			if json.Unmarshal([]byte(line), &m) == nil && m["type"] != nil && m["properties"] != nil {
				out = append(out, m)
			}
		}
	}
	for _, q := range quotedOpenCode {
		var m map[string]any
		if err := json.Unmarshal([]byte(q), &m); err != nil {
			t.Fatal(err)
		}
		out = append(out, m)
	}
	return out
}

func openCodeScript(cwd string) Script {
	return Script{Seed: 7, Start: time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC), Cwd: cwd, Turns: []Turn{
		{Say: 140, Tools: 2, Output: 30},
		{Say: 20, Agents: &Batch{Size: 2, Steps: 1}},
		{Interrupt: true, Tools: 1},
		{New: true, Say: 10},
	}}
}

// The OpenCode profile writes the events of OpenCode 1.18.20's real stream
// (adapters/opencode/testdata/events.sse) and nothing else; Kilo's, of
// Kilo 7.8.27's (adapters/kilo/testdata/events.sse,
// opencode/testdata/kilo/offline.jsonl) or, where those have no event of a
// kind, OpenCode's: Kilo serves the same stream (kilo.yaml:1-6).
func TestOpenCodeMatchesRecordedEvents(t *testing.T) {
	for _, kilo := range []bool{false, true} {
		home := t.TempDir()
		l, err := Write(home, &OpenCode{Kilo: kilo}, openCodeScript(filepath.Join(home, "proj")))
		if err != nil {
			t.Fatal(err)
		}
		files := []string{"../adapters/opencode/testdata/events.sse"}
		if kilo {
			files = append(files, "../adapters/kilo/testdata/events.sse", "../adapters/opencode/testdata/kilo/offline.jsonl")
		}
		checkHonest(t, readServerEvents(t, files...), readRecords(t, l.Mains), openCodeKind, readOpenCode)
	}
}

// Through the real mapper, the generated stream is the session the script
// says: one turn per prompt, the last reply where Truth says, the
// subagents as agent items, the plan and the changed files.
func TestOpenCodeStreamMaps(t *testing.T) {
	home := t.TempDir()
	cwd := filepath.Join(home, "proj")
	l, err := Write(home, &OpenCode{}, openCodeScript(cwd))
	if err != nil {
		t.Fatal(err)
	}
	m := opencode.NewEvents(harness.Env{Cwd: cwd})
	items := map[string]harness.Item{}
	var ops []harness.Op
	for _, r := range readRecords(t, l.Mains) {
		raw, _ := json.Marshal(r)
		its, o := m.Map(harness.Record{Data: raw})
		for _, it := range its {
			items[it.ID] = it
		}
		ops = append(ops, o...)
	}
	turns, agents := 0, 0
	for _, it := range items {
		switch {
		case it.Turn != nil && it.Parent == "":
			turns++
			if it.Turn.State == harness.StateRunning {
				t.Errorf("turn %s still running", it.ID)
			}
		case it.Agent != nil:
			agents++
			if it.Agent.State != harness.StateOK {
				t.Errorf("agent %s is %s", it.ID, it.Agent.State)
			}
		}
	}
	// The /new turn's chat starts over in the mapper's eyes only by its
	// session; the four turns are all mapped.
	if turns != l.Truth.Prompts {
		t.Errorf("%d turns, want %d", turns, l.Truth.Prompts)
	}
	if agents != 2 {
		t.Errorf("%d agents, want 2", agents)
	}
	if it, ok := items[l.Truth.Last]; !ok || it.Assistant == nil {
		t.Errorf("the last reply %s is not a text item: %+v", l.Truth.Last, it)
	}
	sections := map[string]bool{}
	for _, o := range ops {
		sections[o.Section] = true
	}
	for _, s := range []string{harness.SectionPlan, harness.SectionChanges, harness.SectionAgents} {
		if !sections[s] {
			t.Errorf("no %s rows", s)
		}
	}
	if st := m.State(); st["working"] != "false" || st["model_id"] != OpenCodeModel || st["mode"] != "build" {
		t.Errorf("state %v", st)
	}
}

func TestOpenCodeRefusesWhatItLacks(t *testing.T) {
	home := t.TempDir()
	for _, turn := range []Turn{{Agents: &Batch{Size: 1, Background: true}}, {Jobs: 1}, {Compact: true}, {Error: true}} {
		_, err := Write(home, &OpenCode{}, Script{Seed: 1, Start: time.Now(), Cwd: filepath.Join(home, "p"), Turns: []Turn{turn}})
		if err == nil {
			t.Errorf("%+v: written, but there is no real recording of it", turn)
		}
	}
}
