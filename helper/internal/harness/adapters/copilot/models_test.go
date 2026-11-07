package copilot

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness/engine"
	"github.com/gettogo/togo/helper/internal/harness/spec"
)

// The model list is the ids `copilot help config` lists under `model`,
// after Auto; the next setting's values are not models.
func TestModelsFromHelpConfig(t *testing.T) {
	a, err := spec.Parse(AdapterFile())
	if err != nil {
		t.Fatal(err)
	}
	text, err := os.ReadFile("testdata/help-config.txt")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(string(text))
	var in any
	json.Unmarshal(raw, &in)
	out, err := engine.JQ(a.Sources["models"].Select, in, nil)
	if err != nil || len(out) != 1 {
		t.Fatalf("%v %v", out, err)
	}
	rows := out[0].([]any)
	first, last := rows[0].(map[string]any), rows[len(rows)-1].(map[string]any)
	if first["id"] != "auto" || last["id"] != "kimi-k3" || len(rows) < 4 {
		t.Fatalf("rows %v", rows)
	}
	for _, r := range rows {
		if r.(map[string]any)["id"] == "default" {
			t.Fatal("a contextTier value listed as a model")
		}
	}
	// Text with no model section still lists Auto.
	json.Unmarshal([]byte(`"not logged in"`), &in)
	out, err = engine.JQ(a.Sources["models"].Select, in, nil)
	if err != nil || len(out[0].([]any)) != 1 {
		t.Fatalf("no section: %v %v", out, err)
	}
}
