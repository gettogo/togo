package qwen

import (
	"encoding/json"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness/engine"
	"github.com/gettogo/togo/helper/internal/harness/spec"
)

// The todo file's content and status words become checklist rows; the
// mode verifies against the mapper's mode.
func TestTodosAndModeVerify(t *testing.T) {
	a, err := spec.Parse(AdapterFile())
	if err != nil {
		t.Fatal(err)
	}
	var todo any
	json.Unmarshal([]byte(`{"id":"1","content":"Write the test","status":"in_progress"}`), &todo)
	for key, want := range map[string]string{"item.id": "1", "item.title": "Write the test", "item.state": "active"} {
		got, err := engine.MapValue(a.Screens["plan"].Map[key], todo, nil)
		if err != nil || got != want {
			t.Errorf("screen %s = %q %v, want %q", key, got, err, want)
		}
		if got, _ := engine.MapValue(a.Mission["plan"].Map[key], todo, nil); got != want {
			t.Errorf("mission %s = %q, want %q", key, got, want)
		}
	}
	json.Unmarshal([]byte(`{"id":"2","content":"Ship","status":"completed"}`), &todo)
	if got, _ := engine.MapValue(a.Screens["plan"].Map["item.state"], todo, nil); got != "done" {
		t.Errorf("completed = %q", got)
	}
	if v := a.Actions["set_mode"].Verify; v == nil || v.Select != "$state.mode" {
		t.Errorf("verify %+v", v)
	}
	if a.Modes.Select != "$state.mode" {
		t.Errorf("modes select %q", a.Modes.Select)
	}
}
