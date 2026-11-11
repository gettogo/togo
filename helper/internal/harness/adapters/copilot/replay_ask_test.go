package copilot

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
// adapter's ask options, the way `togo hook copilot` and the engine do, and
// returns the question items in order (latest copy per id last).
func replayAsks(t *testing.T, file string) (all []harness.Item, last map[string]harness.Item, tools []string) {
	t.Helper()
	d, ok := hookstyle.For("copilot")
	if !ok {
		t.Fatal("no dialect")
	}
	m := hookstyle.New(harness.Env{SessionID: "replay-" + file}, hookstyle.Options{Name: MapperHooks, Ask: askQuestion, Answered: askAnswered, Plan: planText})
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

// The real hook capture: six ask_user calls and one exit_plan_mode become
// question items (never tool rows), each closed with its answer.
func TestHooksReplayAsks(t *testing.T) {
	all, last, tools := replayAsks(t, "hooks-ask.jsonl")
	for _, n := range tools {
		if n == toolAskUser || n == toolExitPlan {
			t.Fatalf("%s drawn as a tool row", n)
		}
	}
	var qs, plans int
	for id, x := range last {
		if x.Kind == harness.KindAssistant {
			plans++
			continue
		}
		qs++
		if len(x.Question.Answered) == 0 {
			t.Errorf("%s still open: %+v", id, x.Question)
		}
	}
	if qs != 7 || plans != 1 || len(all) < 14 {
		t.Fatalf("questions %d plans %d items %d", qs, plans, len(all))
	}
	first := all[0].Question
	if first == nil || first.Answered != nil || len(first.Questions[0].Options) < 2 {
		t.Fatalf("first ask %+v", first)
	}
	if got := last[all[0].ID].Question.Answered; len(got) != 1 {
		t.Fatalf("first answer %v", got)
	}
}
