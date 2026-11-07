package kilo

import (
	"testing"

	"github.com/gettogo/togo/helper/internal/harness/engine"
	"github.com/gettogo/togo/helper/internal/harness/spec"
)

// Kilo shares OpenCode's command route and ordered question reply.
func TestCommandAndQuestionBodies(t *testing.T) {
	a, err := spec.Parse(AdapterFile())
	if err != nil {
		t.Fatal(err)
	}
	got, err := engine.Render(a.Actions["command"].HTTP.Body, map[string]any{"command": "init", "arguments": ""})
	if err != nil || got != `{"command": "init", "arguments": ""}` {
		t.Fatalf("command %s %v", got, err)
	}
	got, err = engine.Render(a.Actions["answer"].Branch[1].HTTP.Body, map[string]any{"answers": map[string]any{"2": []any{"x"}}})
	if err != nil || got != `{"answers": [[],[],["x"]]}` {
		t.Fatalf("answer %s %v", got, err)
	}
}
