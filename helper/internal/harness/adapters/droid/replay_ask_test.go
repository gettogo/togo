package droid

import (
	"bufio"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/adapters/hookstyle"
)

// replayAsks feeds a hook capture through the hookstyle mapper with this
// adapter's ask options, the way `togo hook droid` and the engine do, and
// returns the question items in order (latest copy per id last).
func replayAsks(t *testing.T, file string) (all []harness.Item, last map[string]harness.Item, tools []string) {
	t.Helper()
	d, ok := hookstyle.For("droid")
	if !ok {
		t.Fatal("no dialect")
	}
	m := hookstyle.New(harness.Env{SessionID: "replay-" + file}, hookstyle.Options{Name: MapperHooks, Ask: droidAsk, Answered: droidAnswered, Plan: specPlan})
	f, err := os.Open("testdata/" + file)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<22)
	last = map[string]harness.Item{}
	at := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	for i := 0; sc.Scan(); i++ {
		var c struct {
			Event   string          `json:"event"`
			Payload json.RawMessage `json:"payload"`
		}
		if json.Unmarshal(sc.Bytes(), &c) != nil || c.Event == "" {
			continue
		}
		_, norm, err := hookstyle.Normalize(d, c.Event, c.Payload)
		if err != nil {
			t.Fatalf("line %d: %v", i+1, err)
		}
		items, _ := m.Map(harness.Record{Source: "hooks", At: at.Add(time.Duration(i) * time.Second), Data: norm})
		for _, x := range items {
			switch {
			case x.Kind == harness.KindQuestion || x.Kind == harness.KindAssistant && len(x.ID) > 5 && x.ID[len(x.ID)-5:] == "-plan":
				all = append(all, x)
				last[x.ID] = x
			case x.Kind == harness.KindTool:
				tools = append(tools, x.Tool.Name)
			}
		}
	}
	return all, last, tools
}

// AskUser calls (no tool_use_id in Droid's hooks) become question items
// closed with their answers; a cancel answers every question with none.
func TestHooksReplayAsks(t *testing.T) {
	all, last, tools := replayAsks(t, "hooks-ask.jsonl")
	for _, n := range tools {
		if n == askTool {
			t.Fatal("AskUser drawn as a tool row")
		}
	}
	if len(all) != 4 || len(last) != 2 {
		t.Fatalf("items %d ids %d", len(all), len(last))
	}
	answered := last[all[1].ID].Question.Answered
	if all[0].Question.Answered != nil || len(answered) == 0 {
		t.Fatalf("first %+v answered %v", all[0].Question, answered)
	}
	for _, v := range last[all[3].ID].Question.Answered {
		if len(v) != 0 {
			t.Fatalf("cancel answered %v", last[all[3].ID].Question.Answered)
		}
	}
}

// ExitSpecMode draws its plan, then a question whose Approve and Deny are
// proceed and keep iterating.
func TestSpecModeIsPlanAndQuestion(t *testing.T) {
	m := hookstyle.New(harness.Env{SessionID: "spec-1"}, hookstyle.Options{Name: MapperHooks, Ask: droidAsk, Answered: droidAnswered, Plan: specPlan})
	rec := func(s string) []harness.Item {
		items, _ := m.Map(harness.Record{Source: "hooks", At: time.Now(), Data: json.RawMessage(s)})
		return items
	}
	rec(`{"hook_event_name":"UserPromptSubmit","prompt":"spec it"}`)
	items := rec(`{"hook_event_name":"PreToolUse","tool_name":"ExitSpecMode","tool_input":{"title":"Login","plan":"1. Add form"}}`)
	if len(items) < 2 || items[0].Kind != harness.KindAssistant || items[1].Kind != harness.KindQuestion {
		t.Fatalf("items %+v", items)
	}
	q := items[1].Question
	if items[0].ID != items[1].ID+"-plan" || q.Approve != specOptions[0] || q.Deny != "No and explain why" {
		t.Fatalf("plan %q question %+v", items[0].ID, q)
	}
}
