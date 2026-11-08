package cursor

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness/engine"
	"github.com/gettogo/togo/helper/internal/harness/spec"
)

func TestAdapterFileParses(t *testing.T) {
	a, err := spec.Parse(adapterFile)
	if err != nil {
		t.Fatal(err)
	}
	if a.Adapter != "cursor" || a.Feeds["hooks"].Mapper != MapperHooks || a.Feeds["hooks"].Stability != spec.StableUnverified {
		t.Fatalf("adapter %+v", a.Feeds)
	}
	var logs []string
	engine.New(engine.Config{Logf: func(f string, a ...any) { logs = append(logs, fmt.Sprintf(f, a...)) }})
	for _, l := range logs {
		if strings.Contains(l, "cursor") {
			t.Fatal(l)
		}
	}
}

// `cursor-agent models` 2026.11.05 output (trimmed): "<id> - <name>"
// lines, flags in parentheses; the header and the closing tip are not
// models.
func TestModelsFromList(t *testing.T) {
	a, _ := spec.Parse(adapterFile)
	text, err := os.ReadFile("testdata/models.txt")
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
	if len(rows) != 26 {
		t.Fatalf("%d rows: %v", len(rows), rows)
	}
	first := rows[0].(map[string]any)
	if first["id"] != "auto" || first["label"] != "Auto" || first["detail"] != "current, default" {
		t.Fatalf("first %v", first)
	}
	if r := rows[4].(map[string]any); r["id"] != "gpt-5.3-codex-fast" || r["label"] != "Codex 5.3 Fast" || r["detail"] != nil {
		t.Fatalf("plain %v", r)
	}
	json.Unmarshal([]byte(`"Not logged in"`), &in)
	if out, err = engine.JQ(a.Sources["models"].Select, in, nil); err != nil || len(out[0].([]any)) != 0 {
		t.Fatalf("logged out %v %v", out, err)
	}
}
