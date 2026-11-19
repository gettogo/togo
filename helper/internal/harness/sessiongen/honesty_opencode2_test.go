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

// readOpenCode2 lists the paths of an event that OpenCode 2.x's mapper
// reads (adapters/opencode/events2.go event2 and ev2, the fields its
// handlers use: parts2.go, needs2.go, fields2.go, shells2.go,
// revert2.go).
var readOpenCode2 = []string{
	"id", "created", "type", "location.directory",
	"data.sessionID", "data.parentID", "data.title", "data.agent",
	"data.model.id", "data.model.providerID", "data.model.variant",
	"data.assistantMessageID", "data.ordinal", "data.delta", "data.text",
	"data.id", "data.name", "data.input.*", "data.content[text].text", "data.metadata.*",
	"data.error.type", "data.error.message", "data.error.status",
	"data.inboxID", "data.item.type", "data.item.payload.text",
	"data.requestID", "data.reply", "data.action", "data.resources[]", "data.source.type", "data.source.id",
	"data.form.id", "data.form.sessionID", "data.form.title", "data.form.metadata.*", "data.form.fields[].*",
	"data.answer.*", "data.location.directory", "data.revert.*",
	"data.info.id", "data.info.status", "data.info.command", "data.info.metadata.sessionID", "data.info.time.started",
}

// quotedOpenCode2 are real payloads the adapter's tests quote where the
// recordings have none: a child session's creation
// (sources2_test.go:164-166, from the real children.json).
var quotedOpenCode2 = []string{
	`{"id":"evt_1d72c8a33001","created":1794611274611,"type":"session.created","data":{"sessionID":"ses_e28d51c92ffeuZKPOJEAMrXUPw","parentID":"ses_e28f4c58affePFJ6qUEQH9BCR7","projectID":"be040d472edc93e9f486658824b9d19137761562","agent":"general","slug":"audit-child","version":"2.3.0","title":"Audit read-only child","permissions":[],"location":{"directory":"/w/project"}}}`,
}

func openCode2Kind(r map[string]any) string {
	k, _ := r["type"].(string)
	d, _ := r["data"].(map[string]any)
	if _, ok := d["parentID"]; ok {
		k += "/child"
	}
	if f, ok := d["form"].(map[string]any); ok {
		if m, ok := f["metadata"].(map[string]any); ok {
			k += "/" + m["kind"].(string)
		}
	}
	return k
}

// readServerEvents2 reads every 2.x recording: SSE "data:" lines and JSON
// lines, each an event with data.
func readServerEvents2(t *testing.T) []map[string]any {
	t.Helper()
	files, _ := filepath.Glob("../adapters/opencode/testdata/v2/*.sse")
	more, _ := filepath.Glob("../adapters/opencode/testdata/v2/*.jsonl")
	var out []map[string]any
	add := func(line string) {
		var m map[string]any
		if json.Unmarshal([]byte(line), &m) == nil && m["type"] != nil && m["data"] != nil {
			out = append(out, m)
		}
	}
	for _, f := range append(files, more...) {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		sc := bufio.NewScanner(bytes.NewReader(raw))
		sc.Buffer(make([]byte, 1<<20), 64<<20)
		for sc.Scan() {
			add(strings.TrimPrefix(sc.Text(), "data: "))
		}
	}
	for _, q := range quotedOpenCode2 {
		add(q)
	}
	if len(out) < 100 {
		t.Fatalf("only %d recorded 2.x events", len(out))
	}
	return out
}

func openCode2Script(cwd string) Script {
	return Script{Seed: 7, Start: time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC), Cwd: cwd, Turns: []Turn{
		{Say: 140, Tools: 2, Output: 30, Jobs: 1},
		{Say: 20, Agents: &Batch{Size: 2, Steps: 1}},
		{Interrupt: true, Tools: 1},
		{Error: true},
		{New: true, Say: 10},
	}}
}

// The OpenCode 2.x profile writes the events of OpenCode 2.2.5's real
// stream (adapters/opencode/testdata/v2) and nothing else, each with the
// fields the mapper reads that every recording of its kind has.
func TestOpenCode2MatchesRecordedEvents(t *testing.T) {
	home := t.TempDir()
	p := &OpenCode2{}
	l, err := Write(home, p, openCode2Script(filepath.Join(home, "proj")))
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{AskPermission, AskQuestion, AskRelease} {
		if _, err := p.Ask(l, kind); err != nil {
			t.Fatal(err)
		}
		if err := p.EndAsk(l); err != nil {
			t.Fatal(err)
		}
	}
	msg, err := p.Hold(l, "held")
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Finish(l, msg); err != nil {
		t.Fatal(err)
	}
	if err := p.EndShells(l); err != nil {
		t.Fatal(err)
	}
	checkHonest(t, readServerEvents2(t), readRecords(t, l.Mains), openCode2Kind, readOpenCode2)
}

// Through the real mapper, the generated stream is the session the script
// says: one turn per prompt, the subagents as agent items that end, the
// background shell shown while it runs, the last reply where Truth says.
func TestOpenCode2StreamMaps(t *testing.T) {
	home := t.TempDir()
	cwd := filepath.Join(home, "proj")
	l, err := Write(home, &OpenCode2{}, openCode2Script(cwd))
	if err != nil {
		t.Fatal(err)
	}
	m := opencode.NewEvents2(harness.Env{Cwd: cwd})
	items := map[string]harness.Item{}
	shells := map[string]bool{}
	for _, r := range readRecords(t, l.Mains) {
		raw, _ := json.Marshal(r)
		its, ops := m.Map(harness.Record{Data: raw})
		for _, it := range its {
			items[it.ID] = it
		}
		for _, o := range ops {
			if o.Section == harness.SectionBackground && o.Row != nil {
				shells[o.Row.ID] = true
			}
		}
	}
	turns, agents, states := 0, 0, map[string]int{}
	for _, it := range items {
		switch {
		case it.Turn != nil && it.Parent == "":
			turns++
			states[it.Turn.State]++
		case it.Agent != nil:
			agents++
			if it.Agent.State != harness.StateOK {
				t.Errorf("agent %s is %s", it.ID, it.Agent.State)
			}
		}
	}
	// The failed turn has no execution.started: the prompt is shown, no turn.
	if turns != l.Truth.Prompts-1 || states[harness.StateInterrupted] != 1 || states[harness.StateRunning] != 0 {
		t.Errorf("%d turns %v, want %d with one interrupted", turns, states, l.Truth.Prompts-1)
	}
	if agents != 2 {
		t.Errorf("%d agents, want 2", agents)
	}
	if len(shells) != 1 || !shells[l.Truth.Jobs[0]] {
		t.Errorf("background rows %v, want %v", shells, l.Truth.Jobs)
	}
	if it, ok := items[l.Truth.Last]; !ok || it.Assistant == nil {
		t.Errorf("the last reply %s is not a text item: %+v", l.Truth.Last, it)
	}
	if st := m.State(); st["working"] != "false" || st["model_id"] != OpenCodeModel || st["mode"] != "build" {
		t.Errorf("state %v", st)
	}
}

func TestOpenCode2RefusesWhatItLacks(t *testing.T) {
	home := t.TempDir()
	for _, turn := range []Turn{{Agents: &Batch{Size: 1, Background: true}}, {Compact: true}, {Exit: true}} {
		_, err := Write(home, &OpenCode2{}, Script{Seed: 1, Start: time.Now(), Cwd: filepath.Join(home, "p"), Turns: []Turn{turn}})
		if err == nil {
			t.Errorf("%+v: written, but there is no real recording of it", turn)
		}
	}
}
