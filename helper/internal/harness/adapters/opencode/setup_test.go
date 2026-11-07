package opencode

import (
	"testing"

	"github.com/gettogo/togo/helper/internal/harness/engine"
	"github.com/gettogo/togo/helper/internal/harness/spec"
)

// The model picker's "Not set up" section lists every provider OpenCode
// knows that is not connected, by name, and connects one with an API key.
func TestUnconnectedProviders(t *testing.T) {
	a, err := spec.Parse(AdapterFile())
	if err != nil {
		t.Fatal(err)
	}
	doc := map[string]any{
		"all": []any{
			map[string]any{"id": "zai", "name": "Z.AI", "key": "secret"},
			map[string]any{"id": "anthropic", "name": "anthropic"},
			map[string]any{"id": "moonshotai", "name": "Moonshot AI"},
		},
		"connected": []any{"moonshotai"},
	}
	v, err := engine.JQFirst(a.Sources["unconnected"].Select, doc, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := v.([]any)
	if len(got) != 2 || got[0].(map[string]any)["id"] != "anthropic" || got[1].(map[string]any)["id"] != "zai" {
		t.Fatalf("unconnected = %v", got)
	}
	if _, leaked := got[1].(map[string]any)["key"]; leaked {
		t.Fatal("a provider key reached the picker")
	}
	su := a.Screens["model"].Setup
	if su == nil || su.Action != "add_provider_key" || su.Input != "secret" {
		t.Fatalf("setup = %+v", su)
	}
	if h := a.Actions["add_provider_key"].HTTP; h == nil || h.Path != "/auth/{{.id}}" {
		t.Fatalf("add_provider_key = %+v", a.Actions["add_provider_key"])
	}
}
