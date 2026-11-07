package pi

import (
	"encoding/json"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness/engine"
	"github.com/gettogo/togo/helper/internal/harness/spec"
)

// pi prints its version on stderr without a terminal, and lists models
// through its rpc child.
func TestVersionAndModelsWithoutATerminal(t *testing.T) {
	a, err := spec.Parse(AdapterFile())
	if err != nil {
		t.Fatal(err)
	}
	if a.Harness.Stderr != spec.StderrMerge {
		t.Fatal("version read from stdout only")
	}
	src := a.Sources["models"]
	if src.RPC == nil || src.RPC.Method != "get_available_models" || src.RPC.Framing != spec.FramingTyped || src.Fallback == nil {
		t.Fatalf("models %+v", src)
	}
	var in any
	json.Unmarshal([]byte(`{"models":[{"id":"kimi-k2","name":"Kimi K2","provider":"moonshotai","contextWindow":262144,"reasoning":true},
		{"id":"tiny","provider":"local","contextWindow":512}]}`), &in)
	out, err := engine.JQ(src.Select, in, nil)
	if err != nil {
		t.Fatal(err)
	}
	rows := out[0].([]any)
	first, second := rows[0].(map[string]any), rows[1].(map[string]any)
	if first["id"] != "moonshotai/kimi-k2" || first["context"] != "262.1K" || first["thinking"] != true || second["name"] != "tiny" || second["context"] != "512" {
		t.Fatalf("rows %v", rows)
	}
}
