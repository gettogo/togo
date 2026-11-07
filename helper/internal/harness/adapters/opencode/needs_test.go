package opencode

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/engine"
	"github.com/gettogo/togo/helper/internal/harness/spec"
)

// A request with more than four questions shows and answers every one, in
// order: a skipped question keeps its slot, so the next answer lands on its
// own question.
func TestFiveQuestionsKeepTheirSlots(t *testing.T) {
	m := NewEvents(harness.Env{SessionID: "s_oc"})
	m.Map(harness.Record{Data: json.RawMessage(`{"type":"tui.session.select","properties":{"sessionID":"ses_1"}}`)})
	var qs []string
	for i := range 5 {
		qs = append(qs, fmt.Sprintf(`{"question":"Q%d","header":"H%d","options":[{"label":"A"},{"label":"B"}]}`, i, i))
	}
	items, _ := m.Map(harness.Record{Data: json.RawMessage(`{"type":"question.asked","properties":{"id":"que_5","sessionID":"ses_1","questions":[` + strings.Join(qs, ",") + `]}}`)})
	if len(items) != 1 || len(items[0].Question.Questions) != 5 || items[0].Question.Questions[4].ID != "4" {
		t.Fatalf("asked %+v", items)
	}
	a, err := spec.Parse(AdapterFile())
	if err != nil {
		t.Fatal(err)
	}
	body := a.Actions["answer"].Branch[1].HTTP.Body
	args := map[string]any{"target": "que_5", "answers": map[string]any{
		"1": []any{"B"}, "2": []any{"A", "B"}, "3": "typed", "4": []any{"A"},
	}}
	got, err := engine.Render(body, args)
	if err != nil || got != `{"answers": [[],["B"],["A","B"],["typed"],["A"]]}` {
		t.Fatalf("body %s %v", got, err)
	}
}

// A / line runs the server's command with its arguments.
func TestCommandBody(t *testing.T) {
	a, err := spec.Parse(AdapterFile())
	if err != nil {
		t.Fatal(err)
	}
	cmd := a.Actions["command"].HTTP
	if cmd == nil || cmd.Method != "POST" {
		t.Fatalf("command action %+v", a.Actions["command"])
	}
	path, _ := engine.Render(cmd.Path, map[string]any{"state": map[string]any{"harness_session": "ses_1"}})
	got, err := engine.Render(cmd.Body, map[string]any{"command": "init", "arguments": `say "hi"`})
	if path != "/session/ses_1/command" || err != nil || got != `{"command": "init", "arguments": "say \"hi\""}` {
		t.Fatalf("command %s %s %v", path, got, err)
	}
}
